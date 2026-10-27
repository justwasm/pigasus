package remotetransport

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTCPServer_ConnectRPC_PingPong(t *testing.T) {
	srv, _, cleanup := newTestTCPServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + srv.Addr().String() + "/rpc"
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	pingReq := map[string]any{
		"streamId": "test-ping-123",
		"type":     "ping",
	}
	raw, _ := json.Marshal(pingReq)
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("Write ping: %v", err)
	}

	_, respRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read pong: %v", err)
	}

	var pongResp struct {
		StreamID string `json:"streamId"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal(respRaw, &pongResp); err != nil {
		t.Fatalf("Unmarshal pong: %v", err)
	}

	if pongResp.StreamID != "test-ping-123" {
		t.Errorf("expected streamId test-ping-123, got %q", pongResp.StreamID)
	}
	if pongResp.Type != "pong" {
		t.Errorf("expected type pong, got %q", pongResp.Type)
	}
}

func TestTCPServer_ConnectRPC_GetLocalUserInfo(t *testing.T) {
	srv, _, cleanup := newTestTCPServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws://" + srv.Addr().String() + "/rpc"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	startReq := map[string]any{
		"streamId":  "test-stream-456",
		"type":      "start",
		"procedure": "/exa.language_server_pb.LanguageServerService/GetLocalUserInfo",
		"headers": map[string][]string{
			"Content-Type": {"application/json"},
		},
		"payload": map[string]any{},
		"stream":  false,
	}
	raw, _ := json.Marshal(startReq)
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("Write start: %v", err)
	}

	// Frame 1: data frame
	_, dataRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read data frame: %v", err)
	}

	var dataResp struct {
		StreamID string         `json:"streamId"`
		Type     string         `json:"type"`
		Payload  map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(dataRaw, &dataResp); err != nil {
		t.Fatalf("Unmarshal data frame: %v", err)
	}
	if dataResp.StreamID != "test-stream-456" {
		t.Errorf("expected streamId test-stream-456, got %q", dataResp.StreamID)
	}
	if dataResp.Type != "data" {
		t.Errorf("expected type data, got %q", dataResp.Type)
	}
	if dataResp.Payload["username"] == nil || dataResp.Payload["username"] == "" {
		t.Errorf("expected username in payload, got %v", dataResp.Payload)
	}

	// Frame 2: end frame
	_, endRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read end frame: %v", err)
	}

	var endResp struct {
		StreamID   string `json:"streamId"`
		Type       string `json:"type"`
		StatusCode int    `json:"statusCode"`
	}
	if err := json.Unmarshal(endRaw, &endResp); err != nil {
		t.Fatalf("Unmarshal end frame: %v", err)
	}
	if endResp.StreamID != "test-stream-456" {
		t.Errorf("expected streamId test-stream-456, got %q", endResp.StreamID)
	}
	if endResp.Type != "end" {
		t.Errorf("expected type end, got %q", endResp.Type)
	}
	if endResp.StatusCode != 0 {
		t.Errorf("expected statusCode 0, got %d", endResp.StatusCode)
	}
}
