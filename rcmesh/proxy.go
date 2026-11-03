package rcmesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// These are the proto-JSON messages in the reference's api.proto. Request IDs
// are int64 strings on the wire, never JSON floating-point numbers.
type ProxyRequest struct {
	RequestID   string            `json:"requestId,omitempty"`
	RPCPath     string            `json:"rpcPath,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	PayloadText string            `json:"payloadText,omitempty"`
	IsFinal     *bool             `json:"isFinal,omitempty"`
}
type RequestEnvelope struct {
	InstanceUUID string        `json:"instanceUuid,omitempty"`
	ProxyRequest *ProxyRequest `json:"proxyRequest,omitempty"`
}
type ProxyResponse struct {
	RequestID   string            `json:"requestId,omitempty"`
	StatusCode  int               `json:"statusCode,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	IsFinal     bool              `json:"isFinal,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	PayloadText string            `json:"payloadText,omitempty"`
}

// MarshalJSON retains the body oneof's presence, including an empty text
// body on the final frame. Emitting both body members fails protojson decode.
func (r ProxyResponse) MarshalJSON() ([]byte, error) {
	var payload *[]byte
	var text *string
	if r.Payload != nil {
		payload = &r.Payload
	} else {
		text = &r.PayloadText
	}
	return json.Marshal(struct {
		RequestID   string            `json:"requestId,omitempty"`
		StatusCode  int               `json:"statusCode,omitempty"`
		Headers     map[string]string `json:"headers,omitempty"`
		IsFinal     bool              `json:"isFinal,omitempty"`
		Payload     *[]byte           `json:"payload,omitempty"`
		PayloadText *string           `json:"payloadText,omitempty"`
	}{r.RequestID, r.StatusCode, r.Headers, r.IsFinal, payload, text})
}

type ResponseEnvelope struct {
	ProxyResponse *ProxyResponse `json:"proxyResponse,omitempty"`
}

// ErrorResponse builds the frame that answers a rejected request.
//
// 【反编译】 errorResponseV2 (0x1025ac8f0) builds a ProxyResponseV2 carrying
// the request id, the status code, is_final true and the message as
// payload_text, wraps it in a ConnectInstanceRequestV2 and sendErrorResponse
// (0x1025b22a0) marshals it with protojson. The wire shape is therefore the
// same envelope a normal response uses:
//
//	{"proxyResponse":{"requestId":"42","statusCode":400,"isFinal":true,"payloadText":"…"}}
func ErrorResponse(requestID string, status int, message string) ([]byte, error) {
	response := &ProxyResponse{
		RequestID:   requestID,
		StatusCode:  status,
		IsFinal:     true,
		PayloadText: message,
	}
	return json.Marshal(ResponseEnvelope{ProxyResponse: response})
}

const MaxFrameBytes = 64 << 10
const responseChunkBytes = 16 << 10

// CancelRequestHeader is checked before dispatch by the reference's
// handleWebRTCMessage (agy 1.4.11). A cancellation names an existing request ID.
const CancelRequestHeader = "x-cancel-request"

func IsCancelRequest(req *ProxyRequest) bool {
	if req != nil {
		for key, value := range req.Headers {
			if strings.EqualFold(key, CancelRequestHeader) && value == "true" {
				return true
			}
		}
	}
	return false
}

// HTTPBridge serves authenticated DataChannel RPCs against a local HTTP
// handler. It neither opens sockets nor obtains credentials. The caller must
// authenticate the channel before invoking it. Each response is one reference
// ConnectInstanceRequestV2 JSON envelope, including a final empty frame.
type HTTPBridge struct{ Handler http.Handler }

// DecodeProxyFrame validates an envelope frame (whether final or intermediate).
func DecodeProxyFrame(frame []byte) (*ProxyRequest, error) {
	if len(frame) > MaxFrameBytes {
		return nil, errors.New("rcmesh: incoming frame too large")
	}
	var envelope RequestEnvelope
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("rcmesh: invalid proxy frame: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("rcmesh: trailing data in proxy frame")
	}
	req := envelope.ProxyRequest
	if req == nil {
		return nil, errors.New("rcmesh: missing proxy request")
	}
	if _, err := strconv.ParseInt(req.RequestID, 10, 64); err != nil {
		return nil, errors.New("rcmesh: invalid request ID")
	}
	var raw struct {
		ProxyRequest map[string]json.RawMessage `json:"proxyRequest"`
	}
	_ = json.Unmarshal(frame, &raw) // The typed decode above already validated JSON.
	payload, hasPayload := raw.ProxyRequest["payload"]
	text, hasText := raw.ProxyRequest["payloadText"]
	if hasPayload && hasText && string(payload) != "null" && string(text) != "null" {
		return nil, errors.New("rcmesh: multiple proxy body oneof members")
	}
	return req, nil
}

// DecodeRequest validates a complete, unchunked envelope. A chunked request
// (isFinal false) needs the ChunkReassembler: it folds the frames back into
// one request before the bridge sees it.
func DecodeRequest(frame []byte) (*ProxyRequest, error) {
	req, err := DecodeProxyFrame(frame)
	if err != nil {
		return nil, err
	}
	if req.IsFinal != nil && !*req.IsFinal && !IsCancelRequest(req) {
		return nil, errors.New("rcmesh: chunked requests need the chunk reassembler")
	}
	return req, nil
}

func (b HTTPBridge) Handle(ctx context.Context, frame []byte, send func([]byte) error) error {
	req, err := DecodeRequest(frame)
	if err != nil {
		return err
	}
	return b.HandleRequest(ctx, req, send)
}

// HandleRequest serves one decoded proxy request. The caller owns the
// reassembly: a chunked request arrives here already assembled.
func (b HTTPBridge) HandleRequest(ctx context.Context, req *ProxyRequest, send func([]byte) error) error {
	if b.Handler == nil || send == nil {
		return errors.New("rcmesh: missing HTTP handler or sender")
	}
	if req == nil {
		return errors.New("rcmesh: missing proxy request")
	}
	if IsCancelRequest(req) {
		return errors.New("rcmesh: cancellation must be handled by the session")
	}
	target, err := url.Parse(req.RPCPath)
	const prefix = "/exa.language_server_pb.LanguageServerService/"
	if err != nil || target.IsAbs() || target.Host != "" || target.RawQuery != "" || target.Fragment != "" || !strings.HasPrefix(target.Path, prefix) || !validMethod(strings.TrimPrefix(target.Path, prefix)) {
		return errors.New("rcmesh: RPC path is outside LanguageServerService")
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = []byte(req.PayloadText)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	for key, value := range req.Headers {
		request.Header.Set(key, value)
	}
	// These headers are assigned after caller headers. Authentication is the
	// reason this transport may enter the local handler's tunnel-auth path.
	request.Header.Set("X-Jetski-Via-Remote-Control", "true")
	request.Header.Set("X-Jetski-Remote-Control-Transport", "datachannel")
	ct := request.Header.Get("Content-Type")
	streaming := strings.EqualFold(request.Header.Get("x-is-streaming"), "true") || strings.Contains(ct, "connect+")
	if ct == "" {
		ct = "application/json"
		if streaming {
			ct = "application/connect+proto"
		}
		request.Header.Set("Content-Type", ct)
	}
	if request.Header.Get("Connect-Protocol-Version") == "" {
		request.Header.Set("Connect-Protocol-Version", "1")
	}
	writer := &proxyWriter{header: make(http.Header), id: req.RequestID, send: send}
	return serveProxy(b.Handler, writer, request)
}
func validMethod(method string) bool {
	if method == "" {
		return false
	}
	for _, c := range method {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func serveProxy(handler http.Handler, writer *proxyWriter, request *http.Request) (err error) {
	defer func() {
		if recover() != nil {
			if writer.status == 0 {
				writer.status = http.StatusInternalServerError
			}
			err = errors.New("rcmesh: RPC handler panicked")
		}
		if finalErr := writer.emit(nil, true); err == nil {
			err = finalErr
		}
	}()
	handler.ServeHTTP(writer, request)
	return writer.err
}

type proxyWriter struct {
	header http.Header
	status int
	id     string
	send   func([]byte) error
	err    error
}

func (w *proxyWriter) Header() http.Header { return w.header }
func (w *proxyWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *proxyWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written := 0
	for len(data) > 0 {
		n := min(len(data), responseChunkBytes)
		if err := w.emit(data[:n], false); err != nil {
			w.err = err
			return written, err
		}
		data = data[n:]
		written += n
	}
	return written, nil
}
func (w *proxyWriter) Flush() {
	if w.err == nil {
		w.err = w.emit(nil, false)
	}
}
func (w *proxyWriter) emit(data []byte, final bool) error {
	if w.err != nil {
		return w.err
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	headers := make(map[string]string, len(w.header))
	for key, values := range w.header {
		headers[key] = strings.Join(values, ",")
	}
	response := &ProxyResponse{RequestID: w.id, StatusCode: w.status, Headers: headers, IsFinal: final}
	if utf8.Valid(data) {
		response.PayloadText = string(data)
	} else {
		response.Payload = data
	}
	frame, err := json.Marshal(ResponseEnvelope{response})
	if err != nil {
		return err
	}
	if len(frame) > MaxFrameBytes {
		return errors.New("rcmesh: outgoing frame too large")
	}
	return w.send(frame)
}
