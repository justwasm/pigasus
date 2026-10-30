// Package remotetransport — tcp_connectstream.go: the Connect
// streaming protocol over plain HTTP.
//
// The reference answers a streaming proxy request with the Connect
// streaming protocol: the response body is a sequence of envelopes —
// one flag byte, a four byte big-endian length, then the message —
// and the last envelope carries the end-of-stream marker. The
// reference rewrites the request's `Content-Type` to
// `application/connect+proto` because its language server speaks
// protobuf; the clone's dispatcher speaks JSON, so the clone answers
// with the JSON flavour of the same protocol.
//
// Evidence:
//   - out/local-macos/dec/10250ef10_newProxiedHTTPRequest.c 【反编译】
//   - out/local-macos/dec/10250de80_executeRequest.c         【反编译】
package remotetransport

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/rctoken"
)

const (
	// headerViaRemoteControl marks a request that reached the server
	// through the remote-control tunnel. The reference sets it on
	// every proxied request.
	//
	// Cycle rc-auth-bearer-required: the spelling now lives in
	// rctoken, so the transport, the web UI and the tunnel
	// read one name for it.
	headerViaRemoteControl = rctoken.ViaRemoteControlHeader

	// headerStreamingFlag marks a request whose response is a server
	// stream. The reference reads the same header.
	headerStreamingFlag = "x-is-streaming"
)

// writeConnectEnvelope writes one enveloped message and flushes it,
// so a streaming client sees the frame as it is produced.
func writeConnectEnvelope(w http.ResponseWriter, flusher http.Flusher, flags byte, payload []byte) error {
	if _, err := w.Write(ConnectEnvelope(flags, payload)); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

// isStreamingRequest reports whether the caller asked for a server
// stream. The tunnel sets `x-is-streaming: true` on the proxied
// request; a browser client asks for the same thing with the Connect
// streaming content type — `application/connect+json` or
// `application/connect+proto`.
//
// gRPC-Web does not mark the stream in the request: its content type is
// the same for a unary and a server-streaming call, so the method's own
// descriptor decides. The reference's web bundle relies on it.
func (s *TCPServer) isStreamingRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(headerStreamingFlag)), "true") {
		return true
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "connect+") {
		return true
	}
	if strings.Contains(ct, "grpc-web") {
		return s.protoCodec != nil && s.protoCodec.IsServerStreaming(methodFromPath(r.URL.Path))
	}
	if s.protoCodec != nil && s.protoCodec.IsServerStreaming(methodFromPath(r.URL.Path)) {
		return true
	}
	return false
}

// isRemoteControlRequest reports whether the request arrived through
// the remote-control tunnel.
//
// Cycle rc-auth-bearer-required: the answer now comes from
// rctoken, which owns the marker's spelling.
func isRemoteControlRequest(r *http.Request) bool {
	return rctoken.IsTunnelRequest(r)
}

// handleConnectStreamCall serves a server-streaming Connect call over
// plain HTTP. The dispatcher owns the frame cadence: each frame it
// emits becomes one envelope, and the end-of-stream envelope closes
// the response.
//
// The dialect follows the request's content type, so a binary-proto
// caller gets binary-proto frames.
func (s *TCPServer) handleConnectStreamCall(w http.ResponseWriter, r *http.Request, method string, body []byte) {
	dialect := dialectFor(r.Header.Get("Content-Type"))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", dialect.contentTypeFor(true))
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	// The headers go out now, before the first frame: a stream that
	// holds the connection open has to look open to the caller. The
	// reference answers a stream it has nothing to say with the
	// status line and the content type, and then says nothing.
	//
	// 【一手】 2026-10-27, measured against the reference binary: a
	// StreamAgentStateUpdates call with an empty body answers HTTP
	// 200 with application/grpc-web+json and no frame at all.
	if flusher != nil {
		flusher.Flush()
	}

	payload := s.decodeRequestBody(method, dialect, body, true)
	emit := func(raw json.RawMessage) error {
		return writeConnectEnvelope(w, flusher, ConnectFlagMessage, s.encodeResponseBody(method, dialect, raw))
	}

	sd, ok := s.dispatcher.(StreamDispatcher)
	if !ok {
		s.writeConnectStreamFallback(r.Context(), w, flusher, method, payload, emit)
		endStream(w, flusher, dialect, grpcWebStatusOK, "")
		return
	}
	handled, err := sd.DispatchStream(r.Context(), method, string(payload), emit)
	if !handled {
		s.writeConnectStreamFallback(r.Context(), w, flusher, method, payload, emit)
	}
	if err != nil {
		endStream(w, flusher, dialect, grpcWebStatusInternal, "stream failed")
		return
	}
	endStream(w, flusher, dialect, grpcWebStatusOK, "")
}

// endStream writes the frame that closes a stream: the Connect
// end-of-stream envelope, or the gRPC-Web trailers envelope.
//
// The two protocols disagree on how a stream ends. Connect closes with
// an end-of-stream envelope — the flag byte 0x02 — which carries the
// error when there is one. gRPC-Web closes with the trailers envelope
// — the flag byte 0x80 — whose payload is the header block with the
// status; the reference writes `grpc-status: 0\r\n`.
func endStream(w http.ResponseWriter, flusher http.Flusher, dialect connectDialect, status int, message string) {
	if dialect.isGRPCWeb() {
		_ = writeGRPCWebTrailers(w, flusher, status, message)
		return
	}
	if status != grpcWebStatusOK {
		_ = writeConnectEnvelope(w, flusher, ConnectFlagEndStream, []byte(`{"error":{"code":"internal","message":"stream failed"}}`))
		return
	}
	_ = writeConnectEnvelope(w, flusher, ConnectFlagEndStream, []byte("{}"))
}

// writeConnectStreamFallback answers a streaming method the dispatcher
// does not stream: the dispatcher's own answer for the method becomes
// the stream's first frame.
//
// The reference answers ProjectUpdatesStream with {"projectList":{}} —
// the oneof's member, as proto-JSON spells it — and JetboxSubscribeToState
// with the app state and the user config. An empty frame would carry
// neither.
func (s *TCPServer) writeConnectStreamFallback(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, method string, body []byte, emit func(json.RawMessage) error) {
	res, err := s.dispatcher.DispatchDirect(ctx, method, string(body))
	if err != nil || strings.TrimSpace(res) == "" {
		res = "{}"
	}
	_ = emit(json.RawMessage(res))
}
