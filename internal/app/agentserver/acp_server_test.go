package agentserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func TestACPServer_WebSocketEndToEnd_Auto(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model:      "test-model",
		Token:      "acp-secret-token",
		ACPVersion: "auto",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "acp-secret-token", t.TempDir(), "auto")
	if err != nil {
		t.Fatalf("DialACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello over ACP WebSocket auto!"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var textReceived string
	var sawResult bool
	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			textReceived += f.Text
		case agentlink.KindResult:
			sawResult = true
		}
	}

	if !sawResult {
		t.Errorf("expected result frame from ACP WebSocket turn")
	}
	if !strings.Contains(textReceived, "Hello over ACP WebSocket auto!") {
		t.Logf("got text response: %q", textReceived)
	}
}

func TestACPServer_WebSocketEndToEnd_V1(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model:      "test-model-v1",
		Token:      "v1-secret-token",
		ACPVersion: "1",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "v1-secret-token", t.TempDir(), "1")
	if err != nil {
		t.Fatalf("DialACPRemote (v1) failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Testing ACP v1"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var sawResult bool
	for f := range frames {
		if f.Kind == agentlink.KindResult {
			sawResult = true
		}
	}
	if !sawResult {
		t.Errorf("expected result frame from ACP v1 turn")
	}
}

func TestACPServer_WebSocketEndToEnd_V2(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model:      "test-model-v2",
		Token:      "v2-secret-token",
		ACPVersion: "2",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "v2-secret-token", t.TempDir(), "2")
	if err != nil {
		t.Fatalf("DialACPRemote (v2) failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Testing ACP v2 draft"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var sawResult bool
	for f := range frames {
		if f.Kind == agentlink.KindResult {
			sawResult = true
		}
	}
	if !sawResult {
		t.Errorf("expected result frame from ACP v2 turn")
	}
}
