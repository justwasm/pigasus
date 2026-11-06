package agentserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// TestAHPServer_ClientInterop_DialAndTurn verifies end-to-end WebSocket attach and turn execution
// between AHPRemote client and AHPServer.
func TestAHPServer_ClientInterop_DialAndTurn(t *testing.T) {
	token := "ahp-secret-test-token"
	srv, addr := startServer(t, Config{
		Model: "test-model",
		Token: token,
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialAHPRemote(ctx, addr, token, t.TempDir())
	if err != nil {
		t.Fatalf("DialAHPRemote failed: %v", err)
	}
	defer remote.Close()

	if remote.EngineName() != "ahp" {
		t.Errorf("expected EngineName() = ahp, got %s", remote.EngineName())
	}
	if remote.SessionURI() == "" || !strings.HasPrefix(remote.SessionURI(), "ahp-session:/") {
		t.Errorf("unexpected SessionURI: %s", remote.SessionURI())
	}
	if remote.ChatURI() == "" || !strings.HasPrefix(remote.ChatURI(), "ahp-chat:/") {
		t.Errorf("unexpected ChatURI: %s", remote.ChatURI())
	}

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello from AHPRemote client!"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var (
		textReceived string
		sawResult    bool
		sawThought   bool
	)
	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			textReceived += f.Text
		case agentlink.KindThought:
			sawThought = true
		case agentlink.KindResult:
			sawResult = true
			if f.Error != "" {
				t.Fatalf("turn returned error: %s", f.Error)
			}
		}
	}

	if !sawResult {
		t.Errorf("expected result frame from AHP turn")
	}
	t.Logf("turn completed. textReceived: %q (thought=%v)", textReceived, sawThought)
}

// TestAHPServer_ClientInterop_SessionLifecycle verifies session discovery, multi-chat creation,
// and attaching to an existing session via AHPRemote.
func TestAHPServer_ClientInterop_SessionLifecycle(t *testing.T) {
	token := "ahp-lifecycle-token"
	srv, addr := startServer(t, Config{
		Model: "test-model",
		Token: token,
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialAHPRemote(ctx, "ws://"+addr+"/ahp", token, t.TempDir())
	if err != nil {
		t.Fatalf("DialAHPRemote failed: %v", err)
	}
	defer remote.Close()

	// 1. List sessions
	sessions, err := remote.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Resource != remote.SessionURI() {
		t.Errorf("expected session URI %s, got %s", remote.SessionURI(), sessions[0].Resource)
	}

	// 2. Create chat
	newChat, err := remote.CreateChat(ctx, "Secondary Chat")
	if err != nil {
		t.Fatalf("CreateChat failed: %v", err)
	}
	if !strings.HasPrefix(newChat, "ahp-chat:/") {
		t.Errorf("expected ahp-chat:/ prefix, got %s", newChat)
	}

	// 3. Attach another AHPRemote to the existing session
	remote2, err := agentturn.DialAHPRemote(ctx, "ws://"+addr+"/ahp", token, t.TempDir(), agentturn.AHPRemoteOptions{
		SessionURI: remote.SessionURI(),
	})
	if err != nil {
		t.Fatalf("DialAHPRemote attach to existing session failed: %v", err)
	}
	defer remote2.Close()
	if remote2.SessionURI() != remote.SessionURI() {
		t.Errorf("expected remote2 attached to %s, got %s", remote.SessionURI(), remote2.SessionURI())
	}
}

// TestAHPServer_ClientInterop_TurnCancellation verifies that context cancellation cleanly aborts
// an in-flight AHP turn.
func TestAHPServer_ClientInterop_TurnCancellation(t *testing.T) {
	token := "ahp-cancel-token"
	srv, addr := startServer(t, Config{
		Model: "test-model",
		Token: token,
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialAHPRemote(ctx, addr, token, t.TempDir())
	if err != nil {
		t.Fatalf("DialAHPRemote failed: %v", err)
	}
	defer remote.Close()

	turnCtx, turnCancel := context.WithCancel(ctx)
	// Cancel turn almost immediately
	time.AfterFunc(50*time.Millisecond, turnCancel)

	frames, err := remote.Turn(turnCtx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Cancel this turn quickly"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	// Frames channel must close cleanly without hang
	done := make(chan struct{})
	go func() {
		for range frames {
		}
		close(done)
	}()

	select {
	case <-done:
		// Succeeded
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for cancelled turn frames to drain")
	}
}

// TestAHPServer_ClientInterop_ToolCallStreaming verifies end-to-end tool call execution
// from mock backend through AHPServer to AHPRemote, asserting exactly 1 ToolStart and 1 ToolEnd.
func TestAHPServer_ClientInterop_ToolCallStreaming(t *testing.T) {
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 4)
			go func() {
				defer close(out)
				hasToolOutput := false
				for _, m := range req.Messages {
					if m.Role == "tool" {
						hasToolOutput = true
						break
					}
				}
				if !hasToolOutput {
					args, _ := json.Marshal(map[string]any{
						"CommandLine":       "echo 'ahp-client-interop-tool-success'",
						"toolSummary":       "Echo test",
						"WaitMsBeforeAsync": 1000,
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{
							{
								ID:        "call_interop_1",
								Name:      "run_command",
								Arguments: args,
							},
						},
						Done: true,
					}
				} else {
					out <- backend.StreamChunk{
						Text: "tool finished and result received by model",
						Done: true,
					}
				}
			}()
			return out, nil
		},
	}

	token := "ahp-tool-test-token"
	srv, addr := startServer(t, Config{
		Model:           "tool-test-model",
		Token:           token,
		SkipPermissions: true,
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialAHPRemote(ctx, addr, token, t.TempDir())
	if err != nil {
		t.Fatalf("DialAHPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Run echo tool"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var (
		toolStarts []agentlink.Tool
		toolEnds   []agentlink.Tool
		textParts  []string
		sawResult  bool
	)

	for f := range frames {
		switch f.Kind {
		case agentlink.KindTool:
			if f.Tool != nil {
				if f.Tool.Phase == agentlink.ToolStart {
					toolStarts = append(toolStarts, *f.Tool)
				} else if f.Tool.Phase == agentlink.ToolEnd {
					toolEnds = append(toolEnds, *f.Tool)
				}
			}
		case agentlink.KindText:
			textParts = append(textParts, f.Text)
		case agentlink.KindResult:
			sawResult = true
		}
	}

	// Verify exactly 1 ToolStart was emitted (no duplicate ToolStart on ReadyAction)
	if len(toolStarts) != 1 {
		t.Errorf("expected exactly 1 ToolStart, got %d", len(toolStarts))
	} else if toolStarts[0].Name != "run_command" {
		t.Errorf("expected tool name run_command, got %s", toolStarts[0].Name)
	}

	// Verify exactly 1 ToolEnd was emitted
	if len(toolEnds) != 1 {
		t.Fatalf("expected exactly 1 ToolEnd, got %d", len(toolEnds))
	}
	if toolEnds[0].Name != "run_command" {
		t.Errorf("expected tool name run_command, got %s", toolEnds[0].Name)
	}

	fullText := strings.Join(textParts, "")
	if !strings.Contains(fullText, "tool finished and result received by model") {
		t.Errorf("expected model follow-up text, got %q", fullText)
	}
	if !sawResult {
		t.Errorf("expected KindResult at turn end")
	}
}

// TestAHPServer_ClientInterop_ModelOverride verifies that specifying a Model on Turn
// overrides the server default model.
func TestAHPServer_ClientInterop_ModelOverride(t *testing.T) {
	token := "ahp-model-override-token"
	srv, addr := startServer(t, Config{
		Model: "default-server-model",
		Token: token,
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialAHPRemote(ctx, addr, token, t.TempDir())
	if err != nil {
		t.Fatalf("DialAHPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		Model: "custom-override-model",
		History: []agentlink.Message{
			{Role: "user", Content: "Which model are you?"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var sb strings.Builder
	for f := range frames {
		if f.Kind == agentlink.KindText {
			sb.WriteString(f.Text)
		}
	}

	output := sb.String()
	if !strings.Contains(output, "model=custom-override-model") {
		t.Errorf("expected output to reflect model=custom-override-model, got: %s", output)
	}
}
