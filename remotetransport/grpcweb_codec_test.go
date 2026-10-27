package remotetransport

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// grpcWebEnvelope wraps a payload in the gRPC-Web envelope: one flag
// byte, a four byte big-endian length, then the payload.
func grpcWebEnvelope(payload []byte) []byte {
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	return buf
}

// TestDialectForGRPCWeb checks the content-type → dialect mapping for
// the gRPC-Web flavours: the reference's own web bundle announces
// `application/grpc-web+json`, and the binary flavour is the same
// framing with a protobuf payload.
func TestDialectForGRPCWeb(t *testing.T) {
	cases := map[string]connectDialect{
		"application/grpc-web+json":                dialectGRPCWebJSON,
		"application/grpc-web+proto":               dialectGRPCWebProto,
		"application/grpc-web+json; charset=utf-8": dialectGRPCWebJSON,
	}
	for ct, want := range cases {
		if got := dialectFor(ct); got != want {
			t.Errorf("dialectFor(%q) = %v, want %v", ct, got, want)
		}
	}
	if got := dialectGRPCWebJSON.contentTypeFor(false); got != contentTypeGRPCWebJSON {
		t.Errorf("contentTypeFor(false) = %q, want %q", got, contentTypeGRPCWebJSON)
	}
	if got := dialectGRPCWebJSON.contentTypeFor(true); got != contentTypeGRPCWebJSON {
		t.Errorf("contentTypeFor(true) = %q, want %q", got, contentTypeGRPCWebJSON)
	}
}

// TestTCPServer_GRPCWebUnaryAnswersMessageAndTrailers pins the shape
// of a unary gRPC-Web answer.
//
// 【一手】 2026-10-26, live against the reference at 127.0.0.1:8802:
// the answer is the message envelope and then the trailers envelope,
// whose payload is the header block `grpc-status: 0\r\n`. The bundle's
// gRPC-Web reader ends with "missing trailer" when the trailers
// envelope is absent and with "missing message" when the message
// envelope is.
func TestTCPServer_GRPCWebUnaryAnswersMessageAndTrailers(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	body := grpcWebEnvelope([]byte(`{"conversationId":"c1"}`))
	resp := postRPC(t, srv, "/exa.language_server_pb.LanguageServerService/GetStatus", contentTypeGRPCWebJSON, body)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != contentTypeGRPCWebJSON {
		t.Fatalf("Content-Type = %q, want %q", got, contentTypeGRPCWebJSON)
	}
	envelopes := parseConnectEnvelopes(t, readAll(t, resp))
	if len(envelopes) != 2 {
		t.Fatalf("got %d envelopes, want 2 (the message and the trailers)", len(envelopes))
	}
	if envelopes[0].flags != ConnectFlagMessage {
		t.Errorf("the first envelope's flags = %#x, want %#x", envelopes[0].flags, ConnectFlagMessage)
	}
	var got map[string]any
	if err := json.Unmarshal(envelopes[0].payload, &got); err != nil {
		t.Fatalf("the message envelope is not JSON: %v (%q)", err, envelopes[0].payload)
	}
	if _, ok := got["status"]; !ok {
		t.Errorf("the message envelope has no status block: %s", envelopes[0].payload)
	}
	if envelopes[1].flags != grpcWebFlagTrailers {
		t.Errorf("the last envelope's flags = %#x, want %#x", envelopes[1].flags, grpcWebFlagTrailers)
	}
	if string(envelopes[1].payload) != "grpc-status: 0\r\n" {
		t.Errorf("the trailers = %q, want the reference's header block", envelopes[1].payload)
	}
}

// TestTCPServer_GRPCWebUnaryFailureCarriesTheStatusInTheHeaders pins
// the shape of a failed gRPC-Web call: the reference answers with no
// body at all and the status in the HTTP headers, and the bundle reads
// the status from there.
func TestTCPServer_GRPCWebUnaryFailureCarriesTheStatusInTheHeaders(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	resp := postRPC(t, srv, "/exa.language_server_pb.LanguageServerService/NoSuchMethod", contentTypeGRPCWebJSON, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Grpc-Status"); got != "13" {
		t.Errorf("Grpc-Status = %q, want 13 (internal)", got)
	}
	if resp.Header.Get("Grpc-Message") == "" {
		t.Error("Grpc-Message is empty; want the dispatcher's error")
	}
	if body := readAll(t, resp); len(body) != 0 {
		t.Errorf("the body = %q, want none", body)
	}
}

// TestEndStreamWritesTheProtocolsOwnClosingFrame checks how each
// protocol closes a stream: Connect with the end-of-stream envelope,
// gRPC-Web with the trailers envelope.
//
// The two protocols disagree here, and the client notices: the
// bundle's gRPC-Web reader ends with "missing trailer" when the
// trailers envelope is absent.
func TestEndStreamWritesTheProtocolsOwnClosingFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	endStream(rec, rec, dialectGRPCWebJSON, grpcWebStatusOK, "")
	envelopes := parseConnectEnvelopes(t, rec.Body.Bytes())
	if len(envelopes) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envelopes))
	}
	if envelopes[0].flags != grpcWebFlagTrailers {
		t.Errorf("the gRPC-Web closing frame's flags = %#x, want %#x", envelopes[0].flags, grpcWebFlagTrailers)
	}
	if string(envelopes[0].payload) != "grpc-status: 0\r\n" {
		t.Errorf("the gRPC-Web trailers = %q, want the reference's header block", envelopes[0].payload)
	}

	rec = httptest.NewRecorder()
	endStream(rec, rec, dialectJSON, grpcWebStatusOK, "")
	envelopes = parseConnectEnvelopes(t, rec.Body.Bytes())
	if len(envelopes) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envelopes))
	}
	if envelopes[0].flags != ConnectFlagEndStream {
		t.Errorf("the Connect closing frame's flags = %#x, want %#x", envelopes[0].flags, ConnectFlagEndStream)
	}
}

// TestTCPServer_GRPCWebStripsTheRequestEnvelope checks the request
// side: a gRPC-Web request always carries its message inside an
// envelope, unary calls too — the two protocols differ exactly there.
func TestTCPServer_GRPCWebStripsTheRequestEnvelope(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	payload := []byte(`{"conversationId":"c1"}`)
	got := srv.decodeRequestBody("GetStatus", dialectGRPCWebJSON, grpcWebEnvelope(payload), false)
	if string(got) != string(payload) {
		t.Errorf("decodeRequestBody = %q, want %q", got, payload)
	}
	// A bare body is left alone: the envelope is optional.
	got = srv.decodeRequestBody("GetStatus", dialectGRPCWebJSON, payload, false)
	if string(got) != string(payload) {
		t.Errorf("decodeRequestBody(bare) = %q, want %q", got, payload)
	}
}

// TestTCPServer_GRPCWebUnaryStaysUnary checks the routing: a gRPC-Web
// request for a method the schema does not call server-streaming is a
// unary call, even though the content type is the streaming one.
func TestTCPServer_GRPCWebUnaryStaysUnary(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	req, err := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/exa.language_server_pb.LanguageServerService/GetStatus",
		http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set("Content-Type", contentTypeGRPCWebJSON)
	if srv.isStreamingRequest(req) {
		t.Error("a unary method is a stream; want the unary path")
	}

	req, err = http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/exa.language_server_pb.LanguageServerService/StreamAgentStateUpdates",
		http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set("Content-Type", contentTypeGRPCWebJSON)
	if !srv.isStreamingRequest(req) {
		t.Error("a server-streaming method is not a stream; want the streaming path")
	}
}
