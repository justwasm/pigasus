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

func TestACPServer_MultiTurnHistory_V1(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model:      "test-model-v1",
		Token:      "v1-hist-token",
		ACPVersion: "1",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "v1-hist-token", t.TempDir(), "1")
	if err != nil {
		t.Fatalf("DialACPRemote (v1) failed: %v", err)
	}
	defer remote.Close()

	// Turn 1
	frames1, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "This is turn one of the conversation"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 1 failed: %v", err)
	}

	var promptTokens1 int
	for f := range frames1 {
		if f.Kind == agentlink.KindResult && f.Usage != nil {
			promptTokens1 = f.Usage.PromptTokens
		}
	}

	// Turn 2
	frames2, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "This is turn two of the conversation with some more words"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 2 failed: %v", err)
	}

	var promptTokens2 int
	for f := range frames2 {
		if f.Kind == agentlink.KindResult && f.Usage != nil {
			promptTokens2 = f.Usage.PromptTokens
		}
	}

	if promptTokens2 <= promptTokens1 {
		t.Errorf("expected promptTokens2 (%d) > promptTokens1 (%d) due to retained history", promptTokens2, promptTokens1)
	}

	// Turn 3: /clear resets history
	frames3, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "/clear"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 3 failed: %v", err)
	}
	for range frames3 {
	}

	// Turn 4: fresh prompt after clear
	frames4, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Fresh prompt"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 4 failed: %v", err)
	}

	var promptTokens4 int
	for f := range frames4 {
		if f.Kind == agentlink.KindResult && f.Usage != nil {
			promptTokens4 = f.Usage.PromptTokens
		}
	}

	if promptTokens4 >= promptTokens2 {
		t.Errorf("expected promptTokens4 (%d) < promptTokens2 (%d) after /clear", promptTokens4, promptTokens2)
	}
}

func TestACPServer_MultiTurnHistory_V2(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model:      "test-model-v2",
		Token:      "v2-hist-token",
		ACPVersion: "2",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "v2-hist-token", t.TempDir(), "2")
	if err != nil {
		t.Fatalf("DialACPRemote (v2) failed: %v", err)
	}
	defer remote.Close()

	// Turn 1
	frames1, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "This is turn one of the conversation"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 1 failed: %v", err)
	}

	var totalTokens1 int
	for f := range frames1 {
		if f.Kind == agentlink.KindResult && f.Usage != nil {
			totalTokens1 = f.Usage.TotalTokens
		}
	}

	// Turn 2
	frames2, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "This is turn two of the conversation with some more words"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 2 failed: %v", err)
	}

	var totalTokens2 int
	for f := range frames2 {
		if f.Kind == agentlink.KindResult && f.Usage != nil {
			totalTokens2 = f.Usage.TotalTokens
		}
	}

	if totalTokens2 <= totalTokens1 {
		t.Errorf("expected totalTokens2 (%d) > totalTokens1 (%d) due to retained history", totalTokens2, totalTokens1)
	}

	// Turn 3: /clear resets history
	frames3, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "/clear"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 3 failed: %v", err)
	}
	for range frames3 {
	}

	// Turn 4: fresh prompt after clear
	frames4, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Fresh prompt"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn 4 failed: %v", err)
	}

	var totalTokens4 int
	for f := range frames4 {
		if f.Kind == agentlink.KindResult && f.Usage != nil {
			totalTokens4 = f.Usage.TotalTokens
		}
	}

	if totalTokens4 >= totalTokens2 {
		t.Errorf("expected totalTokens4 (%d) < totalTokens2 (%d) after /clear", totalTokens4, totalTokens2)
	}
}

func TestACPServer_ToolCallStreaming_V2(t *testing.T) {
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
						"CommandLine":       "echo 'acp-v2-tool-ok'",
						"toolSummary":       "Echo test",
						"WaitMsBeforeAsync": 1000,
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{
							{
								ID:        "call_1",
								Name:      "run_command",
								Arguments: args,
							},
						},
						Done: true,
					}
				} else {
					out <- backend.StreamChunk{
						Text: "tool finished successfully in ACP v2",
						Done: true,
					}
				}
			}()
			return out, nil
		},
	}

	srv, addr := startServer(t, Config{
		Model:           "tool-model-v2",
		Token:           "v2-tool-token",
		ACPVersion:      "2",
		SkipPermissions: true,
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "v2-tool-token", t.TempDir(), "2")
	if err != nil {
		t.Fatalf("DialACPRemote (v2) failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Run echo please"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var sawToolStart bool
	var sawToolEnd bool
	var toolResult string
	var replyText string
	var sawResult bool

	for f := range frames {
		switch f.Kind {
		case agentlink.KindTool:
			if f.Tool != nil {
				if f.Tool.Phase == agentlink.ToolStart {
					sawToolStart = true
				} else if f.Tool.Phase == agentlink.ToolEnd {
					sawToolEnd = true
					toolResult = f.Tool.Result
				}
			}
		case agentlink.KindText:
			replyText += f.Text
		case agentlink.KindResult:
			sawResult = true
			if f.Error != "" {
				t.Fatalf("unexpected turn error: %s", f.Error)
			}
		}
	}

	if !sawToolStart {
		t.Errorf("expected ToolStart frame")
	}
	if !sawToolEnd {
		t.Errorf("expected ToolEnd frame")
	}
	if !strings.Contains(toolResult, "acp-v2-tool-ok") {
		t.Errorf("expected tool output to contain 'acp-v2-tool-ok', got: %q", toolResult)
	}
	if !strings.Contains(replyText, "tool finished successfully in ACP v2") {
		t.Errorf("expected reply text, got: %q", replyText)
	}
	if !sawResult {
		t.Errorf("expected result frame from ACP v2 turn")
	}
}

func TestACPServer_ToolCallStreaming_V1(t *testing.T) {
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
						"CommandLine":       "echo 'acp-v1-tool-ok'",
						"toolSummary":       "Echo test v1",
						"WaitMsBeforeAsync": 1000,
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{
							{
								ID:        "call_v1_1",
								Name:      "run_command",
								Arguments: args,
							},
						},
						Done: true,
					}
				} else {
					out <- backend.StreamChunk{
						Text: "tool finished successfully in ACP v1",
						Done: true,
					}
				}
			}()
			return out, nil
		},
	}

	srv, addr := startServer(t, Config{
		Model:           "tool-model-v1",
		Token:           "v1-tool-token",
		ACPVersion:      "1",
		SkipPermissions: true,
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := agentturn.DialACPRemote(ctx, addr, "v1-tool-token", t.TempDir(), "1")
	if err != nil {
		t.Fatalf("DialACPRemote (v1) failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Run echo please in v1"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var sawToolStart bool
	var sawToolEnd bool
	var toolResult string
	var replyText string
	var sawResult bool

	for f := range frames {
		switch f.Kind {
		case agentlink.KindTool:
			if f.Tool != nil {
				if f.Tool.Phase == agentlink.ToolStart {
					sawToolStart = true
				} else if f.Tool.Phase == agentlink.ToolEnd {
					sawToolEnd = true
					toolResult = f.Tool.Result
				}
			}
		case agentlink.KindText:
			replyText += f.Text
		case agentlink.KindResult:
			sawResult = true
			if f.Error != "" {
				t.Fatalf("unexpected turn error: %s", f.Error)
			}
		}
	}

	if !sawToolStart {
		t.Errorf("expected ToolStart frame")
	}
	if !sawToolEnd {
		t.Errorf("expected ToolEnd frame")
	}
	if !strings.Contains(toolResult, "acp-v1-tool-ok") {
		t.Errorf("expected tool output to contain 'acp-v1-tool-ok', got: %q", toolResult)
	}
	if !strings.Contains(replyText, "tool finished successfully in ACP v1") {
		t.Errorf("expected reply text, got: %q", replyText)
	}
	if !sawResult {
		t.Errorf("expected result frame from ACP v1 turn")
	}
}
