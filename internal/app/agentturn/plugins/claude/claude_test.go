package claude

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

type mockReadWriteCloser struct {
	in  *bytes.Buffer
	out io.Reader
}

func (m *mockReadWriteCloser) Write(p []byte) (n int, err error) {
	return m.in.Write(p)
}

func (m *mockReadWriteCloser) Close() error {
	return nil
}

func TestClaudeRemote_DriverRegistration(t *testing.T) {
	d, ok := agentturn.Lookup("claude")
	if !ok {
		t.Fatal("expected 'claude' driver to be registered")
	}
	if d.Name != "claude" {
		t.Errorf("expected driver name 'claude', got %q", d.Name)
	}
	dAlias, ok := agentturn.Lookup("claude-code")
	if !ok || dAlias.Name != "claude" {
		t.Fatalf("expected 'claude-code' alias to resolve to 'claude'")
	}
}

func TestClaudeRemote_ListModels(t *testing.T) {
	r := &ClaudeRemote{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := r.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least 1 model from ClaudeRemote")
	}

	foundSonnet := false
	for _, m := range models {
		if m.ID == "sonnet" || m.ID == "claude-sonnet-4-6" {
			foundSonnet = true
			break
		}
	}
	if !foundSonnet {
		t.Errorf("expected sonnet model in claude model list, got: %+v", models)
	}
}

func TestClaudeRemote_AvailableCommands(t *testing.T) {
	r := &ClaudeRemote{}
	cmds := r.AvailableCommands()
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty available commands for ClaudeRemote")
	}

	foundCost := false
	foundReview := false
	for _, c := range cmds {
		if c.Name == "cost" {
			foundCost = true
		}
		if c.Name == "review" {
			foundReview = true
			if c.InputHint != "[branch]" {
				t.Errorf("expected review InputHint to be [branch], got %q", c.InputHint)
			}
		}
	}
	if !foundCost || !foundReview {
		t.Errorf("expected cost and review in ClaudeRemote commands: %+v", cmds)
	}
}

func TestClaudeRemote_MockTurn(t *testing.T) {
	inBuf := new(bytes.Buffer)
	outPipeR, outPipeW := io.Pipe()

	closer := &mockReadWriteCloser{in: inBuf}
	r, err := NewClaudeRemote(closer, outPipeR, closer, nil, "mock-session-123")
	if err != nil {
		t.Fatalf("NewClaudeRemote failed: %v", err)
	}
	defer r.Close()

	if r.SessionID() != "mock-session-123" {
		t.Errorf("expected session ID mock-session-123, got %s", r.SessionID())
	}

	turnCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	framesCh, err := r.Turn(turnCtx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello Claude"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	// Verify request was written to stdin
	if !strings.Contains(inBuf.String(), `"text":"Hello Claude"`) {
		t.Fatalf("expected prompt in stdin, got: %s", inBuf.String())
	}

	// Emit simulated NDJSON stream from Claude Code
	go func() {
		defer outPipeW.Close()
		lines := []string{
			`{"type":"system","subtype":"init","session_id":"updated-session-456"}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"thinking about it..."}}}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello there!"}}}`,
			`{"type":"assistant","message":{"content":[{"id":"call_99","name":"Bash","type":"tool_use","input":{"command":"ls"}}]}}`,
			`{"type":"user","message":{"content":[{"tool_use_id":"call_99","type":"tool_result","content":"file1.txt\nfile2.txt","is_error":false}]}}`,
			`{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":10,"output_tokens":25}}`,
		}
		for _, l := range lines {
			_, _ = outPipeW.Write([]byte(l + "\n"))
			time.Sleep(10 * time.Millisecond)
		}
	}()

	var received []agentlink.Frame
	for f := range framesCh {
		received = append(received, f)
	}

	if r.SessionID() != "updated-session-456" {
		t.Errorf("expected updated session ID updated-session-456, got %s", r.SessionID())
	}

	// Verify frames
	var hasThought, hasText, hasToolStart, hasToolEnd, hasResult bool
	for _, f := range received {
		switch f.Kind {
		case agentlink.KindThought:
			if f.Text == "thinking about it..." {
				hasThought = true
			}
		case agentlink.KindText:
			if f.Text == "Hello there!" {
				hasText = true
			}
		case agentlink.KindTool:
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolStart && f.Tool.Name == "Bash" {
				hasToolStart = true
			}
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolEnd && strings.Contains(f.Tool.Result, "file1.txt") {
				hasToolEnd = true
			}
		case agentlink.KindResult:
			if f.Usage != nil && f.Usage.TotalTokens == 35 {
				hasResult = true
			}
		}
	}

	if !hasThought {
		t.Error("missing thought frame")
	}
	if !hasText {
		t.Error("missing text frame")
	}
	if !hasToolStart {
		t.Error("missing tool start frame")
	}
	if !hasToolEnd {
		t.Error("missing tool end frame")
	}
	if !hasResult {
		t.Error("missing result frame with token usage")
	}
}

func TestClaudeRemote_LiveTurn(t *testing.T) {
	if os.Getenv("OPENAGY_LIVE_TEST") == "" {
		t.Skip("skipping live claude test without OPENAGY_LIVE_TEST=1")
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	remote, err := SpawnClaudeRemote(ctx, claudePath, "", "gemini-3.8-flash-high", "")
	if err != nil {
		t.Skipf("skipping live claude test: %v", err)
	}
	defer remote.Close()

	framesCh, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "echo 12345"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	var hasOutput bool
	for f := range framesCh {
		if f.Text != "" || (f.Tool != nil && f.Tool.Result != "") {
			hasOutput = true
		}
	}

	if !hasOutput {
		t.Error("expected output from live Claude Code turn")
	}
}
