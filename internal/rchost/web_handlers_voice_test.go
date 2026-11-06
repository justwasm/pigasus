package rchost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/internal/voice"
)

func TestWebHandlers_VoiceEndpoints(t *testing.T) {
	tmpDir := t.TempDir()

	handler, cleanup, err := NewWebHandler(WebHandlerOptions{
		Workspace: tmpDir,
		CSRFToken: "test-csrf-token",
	})
	if err != nil {
		t.Fatalf("NewWebHandler failed: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	// 1. GET /voice/health
	resHealth, err := client.Get(ts.URL + "/voice/health")
	if err != nil {
		t.Fatalf("GET /voice/health failed: %v", err)
	}
	defer resHealth.Body.Close()
	if resHealth.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /voice/health, got %d", resHealth.StatusCode)
	}
	ct := resHealth.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	var healthMap map[string]string
	if err := json.NewDecoder(resHealth.Body).Decode(&healthMap); err != nil || healthMap["status"] != "ok" {
		t.Fatalf("unexpected health response: %+v", healthMap)
	}

	// 2. GET /voice/chats
	resChats, err := client.Get(ts.URL + "/voice/chats?conversation_id=conv-test-123")
	if err != nil {
		t.Fatalf("GET /voice/chats failed: %v", err)
	}
	defer resChats.Body.Close()
	if resChats.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /voice/chats, got %d", resChats.StatusCode)
	}
	body, _ := io.ReadAll(resChats.Body)
	var chatsResp struct {
		Chats []any `json:"chats"`
	}
	if err := json.Unmarshal(body, &chatsResp); err != nil {
		t.Fatalf("failed to decode chats json: %v", err)
	}

	// 3. GET /voice/recordings
	resRec, err := client.Get(ts.URL + "/voice/recordings")
	if err != nil {
		t.Fatalf("GET /voice/recordings failed: %v", err)
	}
	defer resRec.Body.Close()
	if resRec.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /voice/recordings, got %d", resRec.StatusCode)
	}

	// 4. WebSocket /voice/ws
	wsURL := "ws" + ts.URL[4:] + "/voice/ws?conversation_id=conv-test-123"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Dial with subprotocol
	opts := &websocket.DialOptions{
		Subprotocols: []string{voice.Subprotocol},
	}
	c, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if c.Subprotocol() != voice.Subprotocol {
		t.Fatalf("expected subprotocol %q, got %q", voice.Subprotocol, c.Subprotocol())
	}
}
