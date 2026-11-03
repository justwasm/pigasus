package remotetransport

// Connect-Go owns the standard HTTP protocols. The codec is deliberately a
// boundary adapter: the dispatcher still speaks the recovered proto-JSON,
// including its declaration order and lenient handling of unknown fields.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type rpcPayload struct{ data []byte }
type rpcCodec struct {
	name, method string
	server       *TCPServer
}

func (c rpcCodec) Name() string { return c.name }
func (c rpcCodec) Marshal(value any) ([]byte, error) {
	if p, ok := value.(*rpcPayload); ok {
		data := p.data
		if len(data) == 0 {
			data = []byte(`{}`)
		}
		if c.name == "proto" {
			return c.server.encodeResponseBody(c.method, dialectProto, data), nil
		}
		return data, nil
	}
	// Connect also serializes its own error-detail messages through the codec.
	if p, ok := value.(proto.Message); ok {
		if c.name == "proto" {
			return proto.Marshal(p)
		}
		return protojson.Marshal(p)
	}
	return nil, errors.New("remote-control: unsupported RPC message")
}
func (c rpcCodec) Unmarshal(data []byte, value any) error {
	if p, ok := value.(*rpcPayload); ok {
		if c.name == "proto" {
			data = c.server.decodeRequestBody(c.method, dialectProto, data, false)
		}
		p.data = bytes.Clone(data)
		return nil
	}
	if p, ok := value.(proto.Message); ok {
		if c.name == "proto" {
			return proto.Unmarshal(data, p)
		}
		return protojson.Unmarshal(data, p)
	}
	return errors.New("remote-control: unsupported RPC message")
}

// The unary dispatch error is retained separately because the reference has
// compatibility rules beyond Connect: unnamed errors are masked and gRPC-Web
// failures carry a fresh error ID in the trailers-only response headers.
type rpcCallState struct {
	writer  http.ResponseWriter
	err     error
	dialect connectDialect
}
type rpcCallKey struct{}

func (s *TCPServer) serveHTTPRPC(w http.ResponseWriter, r *http.Request, method string, body []byte) {
	streaming := s.isStreamingRequest(r)
	dialect := dialectFor(r.Header.Get("Content-Type"))
	normalized := normalizeRPCRequest(r, body, dialect, streaming)
	state := &rpcCallState{writer: w, dialect: dialect}
	normalized = normalized.WithContext(context.WithValue(r.Context(), rpcCallKey{}, state))
	opts := []connect.HandlerOption{
		connect.WithCodec(rpcCodec{"json", method, s}),
		connect.WithCodec(rpcCodec{"proto", method, s}),
		connect.WithReadMaxBytes(16 << 20),
		// Reference responses are uncompressed unless the caller explicitly asks.
		connect.WithCompressMinBytes(1024),
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if streaming {
		connect.NewServerStreamHandler(r.URL.Path, s.rpcStream(method), opts...).ServeHTTP(w, normalized)
		return
	}
	// A unary response is bounded by the dispatcher, so buffering here lets the
	// small reference error adapter preserve the exact pre-migration error wire.
	response := &unaryRPCResponse{header: make(http.Header)}
	connect.NewUnaryHandler(r.URL.Path, s.rpcUnary(method), opts...).ServeHTTP(response, normalized)
	if state.err != nil {
		s.writeDispatchError(w, r, method, state.err)
		return
	}
	for key, values := range response.header {
		w.Header()[key] = values
	}
	if response.status == 0 {
		response.status = http.StatusOK
	}
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body.Bytes())
}

func (s *TCPServer) rpcUnary(method string) func(context.Context, *connect.Request[rpcPayload]) (*connect.Response[rpcPayload], error) {
	return func(ctx context.Context, req *connect.Request[rpcPayload]) (*connect.Response[rpcPayload], error) {
		result, err := s.dispatcher.DispatchDirect(ctx, method, string(req.Msg.data))
		if err != nil {
			ctx.Value(rpcCallKey{}).(*rpcCallState).err = err
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		return connect.NewResponse(&rpcPayload{[]byte(result)}), nil
	}
}
func (s *TCPServer) rpcStream(method string) func(context.Context, *connect.Request[rpcPayload], *connect.ServerStream[rpcPayload]) error {
	return func(ctx context.Context, req *connect.Request[rpcPayload], stream *connect.ServerStream[rpcPayload]) error {
		emit := func(raw json.RawMessage) error { return stream.Send(&rpcPayload{raw}) }
		if sd, ok := s.dispatcher.(StreamDispatcher); ok {
			handled, err := sd.DispatchStream(ctx, method, string(req.Msg.data), emit)
			if err != nil {
				return toConnectStreamError(err)
			}
			if handled {
				return nil
			}
		}
		result, err := s.dispatcher.DispatchDirect(ctx, method, string(req.Msg.data))
		if err != nil || strings.TrimSpace(result) == "" {
			result = "{}"
		}
		return emit(json.RawMessage(result))
	}
}

func toConnectStreamError(err error) error {
	if err == nil {
		return nil
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}
	code := connect.CodeInternal
	if mapped, ok := connectErrorFor(err); ok {
		code = connect.Code(mapped.grpc)
	}
	return connect.NewError(code, errors.New(grpcWebErrorMessage(err)))
}

type unaryRPCResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *unaryRPCResponse) Header() http.Header { return w.header }
func (w *unaryRPCResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *unaryRPCResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(data)
}

// The tunnel accepts bare messages and x-is-streaming alongside standard
// framed requests. Normalize only those reference extensions before Connect.
func normalizeRPCRequest(r *http.Request, body []byte, dialect connectDialect, streaming bool) *http.Request {
	normalized := r.Clone(r.Context())
	normalized.Header = r.Header.Clone()
	normalized.Header.Set("Content-Type", dialect.contentTypeFor(streaming))
	if streaming || dialect.isGRPCWeb() {
		if len(body) == 0 {
			if !dialect.isProto() {
				body = []byte(`{}`)
			}
			body = ConnectEnvelope(0, body)
		} else if body[0] != 0 && body[0] != 1 {
			body = ConnectEnvelope(0, body)
		}
	} else if len(body) == 0 && !dialect.isProto() {
		body = []byte(`{}`)
	}
	normalized.Body = io.NopCloser(bytes.NewReader(body))
	normalized.ContentLength = int64(len(body))
	return normalized
}
