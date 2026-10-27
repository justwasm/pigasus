package remotetransport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestTCPServer_ConnectRPC_UnaryAsStream_Completes pins the Connect
// protocol's end frame for a unary method the caller sent as a stream.
//
// The Connect protocol allows a unary call to travel as a stream; the
// protocol then still expects a "data" frame followed by an "end"
// frame. Without the end frame the caller's promise never settles and
// the caller waits until its deadline expires.
//
// Evidence grade: 一手 for the protocol (the Connect stream protocol over
// /connect-websocket, see the package comment). What the reference's
// own bundle sends is a separate measurement: it sends "stream":false
// for every unary call and "stream":true only for the six long-lived
// subscriptions (out/web-ui-working-stuck/websocket_frames.txt). So this
// test pins the transport's behaviour for a frame the reference's bundle
// does not itself produce — it is hardening, not the fix for the stall
// the Web UI showed.
func TestTCPServer_ConnectRPC_UnaryAsStream_Completes(t *testing.T) {
	srv, _, cleanup := newTestTCPServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://"+srv.Addr().String()+"/rpc", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	startReq := map[string]any{
		"streamId":  "test-stream-unary-as-stream",
		"type":      "start",
		"procedure": "/exa.language_server_pb.LanguageServerService/GetLocalUserInfo",
		"headers": map[string][]string{
			"Content-Type": {"application/json"},
		},
		"payload": map[string]any{},
		"stream":  true,
	}
	raw, _ := json.Marshal(startReq)
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("Write start: %v", err)
	}

	// Frame 1: the answer.
	_, dataRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read data frame: %v", err)
	}
	var dataResp struct {
		Type    string `json:"type"`
		Payload struct {
			Username string `json:"username"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(dataRaw, &dataResp); err != nil {
		t.Fatalf("Unmarshal data frame: %v", err)
	}
	if dataResp.Type != "data" {
		t.Fatalf("expected type data, got %q", dataResp.Type)
	}
	if dataResp.Payload.Username == "" {
		t.Errorf("expected username in payload, got %v", dataResp.Payload)
	}

	// Frame 2: the end frame. Without it the call never completes.
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	_, endRaw, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("Read end frame: %v", err)
	}
	var endResp struct {
		Type       string `json:"type"`
		StatusCode int    `json:"statusCode"`
	}
	if err := json.Unmarshal(endRaw, &endResp); err != nil {
		t.Fatalf("Unmarshal end frame: %v", err)
	}
	if endResp.Type != "end" {
		t.Fatalf("expected type end, got %q", endResp.Type)
	}
	if endResp.StatusCode != 0 {
		t.Errorf("expected statusCode 0, got %d", endResp.StatusCode)
	}
}
