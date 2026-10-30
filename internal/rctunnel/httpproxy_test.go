package rctunnel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBuildProxiedRequestMatchesReference(t *testing.T) {
	req := &ProxyRequestV2{
		RPCPath: "/exa.language_server_pb.LanguageServerService/GetStatus",
		Headers: map[string]string{"content-type": "application/json"},
	}
	httpReq, err := buildProxiedRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildProxiedRequest: %v", err)
	}
	if httpReq.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", httpReq.Method)
	}
	if httpReq.URL.Path != req.RPCPath {
		t.Errorf("path = %q, want %q", httpReq.URL.Path, req.RPCPath)
	}
	if got := httpReq.Header.Get(headerViaRemoteControl); got != "true" {
		t.Errorf("%s = %q, want true", headerViaRemoteControl, got)
	}
	if got := httpReq.Header.Get(headerRemoteTransport); got != transportWebChannel {
		t.Errorf("%s = %q, want %q", headerRemoteTransport, got, transportWebChannel)
	}
	// The caller named the JSON dialect, so the local handler has to be
// told JSON: the answer comes back in the dialect the caller asked
// for.
if got := httpReq.Header.Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	if got := httpReq.Header.Get(headerConnectVersion); got != "1" {
		t.Errorf("%s = %q, want 1", headerConnectVersion, got)
	}
}

func TestBuildProxiedRequestKeepsCallerConnectVersion(t *testing.T) {
	req := &ProxyRequestV2{
		RPCPath: "/x",
		Headers: map[string]string{headerConnectVersion: "2"},
	}
	httpReq, err := buildProxiedRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildProxiedRequest: %v", err)
	}
	if got := httpReq.Header.Get(headerConnectVersion); got != "2" {
		t.Errorf("%s = %q, want 2", headerConnectVersion, got)
	}
}

// TestBuildProxiedRequestStreamingContentType pins the dialect a
// *streaming* caller that names none gets: binary protobuf.
//
// 【一手】 2026-10-26, live: the CLI bundle's own stream readers — the
// MessagePort reader (`mHc`) and the webchannel reader (`xHc`) — decode
// a frame with `Kc(output, data)`, the binary protobuf decoder; neither
// has a JSON branch. The app's stream request names no content type
// (the bundle sets `x-is-streaming: true` and nothing else), so a JSON
// frame is dropped as a malformed message: the app state store never
// initialises, the manager layout never mounts, and the app never posts
// its `uiReady` message — the dashboard spins on "Connecting to
// instance…" forever.
func TestBuildProxiedRequestStreamingContentType(t *testing.T) {
	req := &ProxyRequestV2{
		RPCPath: "/exa.language_server_pb.LanguageServerService/StreamAgentStateUpdates",
		Headers: map[string]string{"X-Is-Streaming": "true"},
	}
	httpReq, err := buildProxiedRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildProxiedRequest: %v", err)
	}
	if got := httpReq.Header.Get("Content-Type"); got != contentTypeConnectProto {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeConnectProto)
	}
}

// TestBuildProxiedRequestKeepsCallerContentType pins the rule that
// decides the dialect: the caller's own content type wins, because the
// answer has to come back in the dialect the caller asked for.
func TestBuildProxiedRequestKeepsCallerContentType(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{
			name:    "binary unary",
			headers: map[string]string{"Content-Type": "application/proto"},
			want:    contentTypeProto,
		},
		{
			name:    "json unary",
			headers: map[string]string{"Content-Type": "application/json"},
			want:    contentTypeJSON,
		},
		{
			name:    "binary unary on a stream",
			headers: map[string]string{"Content-Type": "application/proto", "X-Is-Streaming": "true"},
			want:    contentTypeConnectProto,
		},
		{
			name:    "json unary on a stream",
			headers: map[string]string{"Content-Type": "application/json", "X-Is-Streaming": "true"},
			want:    contentTypeConnectJSON,
		},
		{
			name:    "an explicit streaming content type",
			headers: map[string]string{"Content-Type": "application/connect+proto"},
			want:    contentTypeConnectProto,
		},
		{
			name:    "gRPC-web, binary",
			headers: map[string]string{"Content-Type": "application/grpc-web+proto"},
			want:    contentTypeProto,
		},
		{
			name:    "gRPC-web, JSON",
			headers: map[string]string{"Content-Type": "application/grpc-web+json"},
			want:    contentTypeJSON,
		},
		{
			// A caller that names no dialect gets Connect's own
			// default. 【一手】 2026-10-26: the dashboard's host
			// page names none, and the reference answers in JSON.
			name:    "no content type",
			headers: map[string]string{},
			want:    contentTypeJSON,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ProxyRequestV2{RPCPath: "/x", Headers: tc.headers}
			httpReq, err := buildProxiedRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("buildProxiedRequest: %v", err)
			}
			if got := httpReq.Header.Get("Content-Type"); got != tc.want {
				t.Errorf("Content-Type = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildProxiedRequestBodyFromPayloadText(t *testing.T) {
	req := &ProxyRequestV2{RPCPath: "/x", PayloadText: "{}"}
	httpReq, err := buildProxiedRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildProxiedRequest: %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "{}" {
		t.Errorf("body = %q, want {}", string(body))
	}
}

func TestCleanRoutePath(t *testing.T) {
	cases := map[string]string{
		"":                                 "/",
		"main.js":                          "/main.js",
		"/a/b/../c":                        "/a/c",
		"/exa.language_server_pb.Foo/Bar/": "/exa.language_server_pb.Foo/Bar/",
	}
	for in, want := range cases {
		if got := cleanRoutePath(in); got != want {
			t.Errorf("cleanRoutePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProxyResponseChunkPrefersTextForUTF8(t *testing.T) {
	rec := newRecordedResponse(nil)
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(http.StatusAccepted)
	rec.Write([]byte(`{"ok":true}`))

	resp := proxyResponseChunk("req-1", rec, []byte(`{"ok":true}`))
	if resp.RequestID != "req-1" {
		t.Errorf("RequestID = %q, want req-1", resp.RequestID)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if resp.PayloadText != `{"ok":true}` {
		t.Errorf("PayloadText = %q, want {\"ok\":true}", resp.PayloadText)
	}
	if len(resp.Payload) != 0 {
		t.Errorf("Payload = %v, want empty", resp.Payload)
	}
	if got := resp.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Headers[Content-Type] = %q, want application/json", got)
	}
	if resp.IsFinal {
		t.Error("IsFinal = true, want false on a chunk frame")
	}
}

func TestProxyResponseChunkFallsBackToBytes(t *testing.T) {
	rec := newRecordedResponse(nil)
	chunk := []byte{0xff, 0xfe, 0x00}

	resp := proxyResponseChunk("req-2", rec, chunk)
	if resp.PayloadText != "" {
		t.Errorf("PayloadText = %q, want empty", resp.PayloadText)
	}
	if len(resp.Payload) != 3 || resp.Payload[0] != 0xff {
		t.Errorf("Payload = %v, want the raw bytes", resp.Payload)
	}
}

func TestProxyResponseFinalMarksTheEnd(t *testing.T) {
	rec := newRecordedResponse(nil)
	rec.WriteHeader(http.StatusOK)

	resp := proxyResponseFinal("req-3", rec)
	if !resp.IsFinal {
		t.Error("IsFinal = false, want true")
	}
	if resp.PayloadText != "" || len(resp.Payload) != 0 {
		t.Errorf("final frame carries a payload: %q / %v", resp.PayloadText, resp.Payload)
	}
}

// TestRecordedResponseStreamsChunks checks that every write becomes its
// own chunk, in order — the property the tunnel's live streaming
// depends on.
func TestRecordedResponseStreamsChunks(t *testing.T) {
	var chunks [][]byte
	rec := newRecordedResponse(func(chunk []byte) {
		chunks = append(chunks, chunk)
	})
	rec.Write([]byte("first"))
	rec.Write([]byte("second"))
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if string(chunks[0]) != "first" || string(chunks[1]) != "second" {
		t.Errorf("chunks = %q / %q, want first / second", chunks[0], chunks[1])
	}
}

func TestIsStreamingRequestIsCaseInsensitive(t *testing.T) {
	if !isStreamingRequest(map[string]string{"X-IS-STREAMING": "TRUE"}) {
		t.Error("isStreamingRequest = false, want true")
	}
	if isStreamingRequest(map[string]string{"x-is-streaming": "false"}) {
		t.Error("isStreamingRequest = true, want false")
	}
	if isStreamingRequest(nil) {
		t.Error("isStreamingRequest(nil) = true, want false")
	}
}

func TestRecordedResponseJoinsRepeatedHeaders(t *testing.T) {
	rec := newRecordedResponse(nil)
	rec.Header().Add("Set-Cookie", "a=1")
	rec.Header().Add("Set-Cookie", "b=2")

	headers := flattenHeaders(rec.Header())
	if !strings.Contains(headers["Set-Cookie"], "a=1") {
		t.Errorf("Set-Cookie = %q, want it to contain a=1", headers["Set-Cookie"])
	}
}

func TestProxyResponseChunkBinaryProtoUsesPayload(t *testing.T) {
	rec := newRecordedResponse(nil)
	rec.Header().Set("Content-Type", "application/proto")
	// ASCII bytes (valid UTF-8) that represent protobuf
	chunk := []byte("\nZ\n$7b2e2299-7c44-44da-b548-ed67b006831c")

	resp := proxyResponseChunk("req-proto", rec, chunk)
	if resp.PayloadText != "" {
		t.Errorf("PayloadText = %q, want empty for proto response", resp.PayloadText)
	}
	if string(resp.Payload) != string(chunk) {
		t.Errorf("Payload = %q, want %q", string(resp.Payload), string(chunk))
	}
}

func TestProxyResponseChunkConnectStreamWithNULUsesPayload(t *testing.T) {
	rec := newRecordedResponse(nil)
	rec.Header().Set("Content-Type", "application/connect+proto")
	// Connect envelope with NUL bytes followed by title:bash
	chunk := []byte{0x00, 0x00, 0x00, 0x00, 0x06, 0x1a, 0x04, 'b', 'a', 's', 'h'}

	resp := proxyResponseChunk("req-stream", rec, chunk)
	if resp.PayloadText != "" {
		t.Errorf("PayloadText = %q, want empty for Connect stream frame", resp.PayloadText)
	}
	if len(resp.Payload) != len(chunk) {
		t.Errorf("len(Payload) = %d, want %d", len(resp.Payload), len(chunk))
	}
}

func TestIsStreamingRPCIdentifiesKnownStreamingMethods(t *testing.T) {
	if !isStreamingRPC(nil, "/exa.language_server_pb.LanguageServerService/StreamTerminalOutput") {
		t.Error("StreamTerminalOutput want streaming = true")
	}
	if !isStreamingRPC(nil, "StreamTerminalOutput") {
		t.Error("StreamTerminalOutput want streaming = true")
	}
	if isStreamingRPC(nil, "/exa.language_server_pb.LanguageServerService/CreateTerminal") {
		t.Error("CreateTerminal want streaming = false")
	}
}
