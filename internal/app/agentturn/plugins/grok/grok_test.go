package grok

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
	in *bytes.Buffer
}

func (m *mockReadWriteCloser) Write(p []byte) (n int, err error) {
	return m.in.Write(p)
}

func (m *mockReadWriteCloser) Close() error {
	return nil
}

func TestGrokRemote_DriverRegistration(t *testing.T) {
	d, ok := agentturn.Lookup("grok")
	if !ok {
		t.Fatal("expected 'grok' driver to be registered")
	}
	if d.Name != "grok" {
		t.Errorf("expected driver name 'grok', got %q", d.Name)
	}
	dAlias, ok := agentturn.Lookup("grok-build")
	if !ok || dAlias.Name != "grok" {
		t.Fatalf("expected 'grok-build' alias to resolve to 'grok'")
	}
}

func TestGrokRemote_MockTurnNDJSON(t *testing.T) {
	inBuf := new(bytes.Buffer)
	outPipeR, outPipeW := io.Pipe()

	closer := &mockReadWriteCloser{in: inBuf}
	r, err := NewGrokRemote(closer, outPipeR, closer, nil, "grok-sess-100")
	if err != nil {
		t.Fatalf("NewGrokRemote failed: %v", err)
	}
	defer r.Close()

	if r.SessionID() != "grok-sess-100" {
		t.Errorf("expected session id grok-sess-100, got %s", r.SessionID())
	}
	if name := r.EngineName(); name != "grok" {
		t.Errorf("expected EngineName 'grok', got %s", name)
	}
	if caps := r.Capabilities(); caps&agentturn.CapModelSwitch == 0 || caps&agentturn.CapReasoningTier == 0 {
		t.Errorf("expected CapModelSwitch and CapReasoningTier, got %v", caps)
	}

	turnCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	framesCh, err := r.Turn(turnCtx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Explain the architecture and list files"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	// Verify request was written
	if !strings.Contains(inBuf.String(), "Explain the architecture") {
		t.Fatalf("expected prompt in stdin, got: %s", inBuf.String())
	}

	// Emit simulated Grok streaming-json NDJSON events
	go func() {
		defer outPipeW.Close()
		lines := []string{
			`{"type":"plan","entries":[{"step":"Inspect directories","status":"in_progress"}]}`,
			`{"type":"thought","text":"The user wants to know the project architecture."}`,
			`{"type":"tool_call","tool":"terminal","command":"ls -la","id":"call_grok_1"}`,
			`{"type":"tool_call_update","id":"call_grok_1","status":"completed","rawOutput":"total 8\ndrwxr-xr-x 2 user staff 64 ."}`,
			`{"type":"text","text":"The project is organized into "}`,
			`{"type":"text","text":"several modular tiers."}`,
			`{"type":"usage","usage":{"prompt_tokens":120,"completion_tokens":45,"total_tokens":165}}`,
			`{"type":"end","stopReason":"end_turn","spend":{"cost":0.002,"currency":"USD"}}`,
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

	var hasThought, hasText, hasToolStart, hasToolEnd, hasResult bool
	for _, f := range received {
		switch f.Kind {
		case agentlink.KindThought:
			if strings.Contains(f.Text, "project architecture") {
				hasThought = true
			}
		case agentlink.KindText:
			if strings.Contains(f.Text, "modular tiers") {
				hasText = true
			}
		case agentlink.KindTool:
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolStart && f.Tool.Name == "terminal" {
				hasToolStart = true
			}
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolEnd && strings.Contains(f.Tool.Result, "total 8") {
				hasToolEnd = true
			}
		case agentlink.KindResult:
			if f.Usage != nil && f.Usage.TotalTokens == 165 {
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

func TestGrokRemote_ListModelsAndCommands(t *testing.T) {
	r := &GrokRemote{}
	models, err := r.ListModels(context.Background())
	if err != nil || len(models) == 0 {
		t.Fatalf("expected Grok models, got error %v", err)
	}
	cmds := r.AvailableCommands()
	if len(cmds) == 0 {
		t.Fatal("expected Grok commands, got empty")
	}
}

func TestGrokRemote_NativeFormatNDJSON(t *testing.T) {
	inBuf := new(bytes.Buffer)
	outPipeR, outPipeW := io.Pipe()

	closer := &mockReadWriteCloser{in: inBuf}
	r, err := NewGrokRemote(closer, outPipeR, closer, nil, "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("NewGrokRemote failed: %v", err)
	}
	defer r.Close()

	turnCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	framesCh, err := r.Turn(turnCtx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Run ls"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	go func() {
		defer outPipeW.Close()
		lines := []string{
			`{"type":"available_commands","tools":["run_terminal_command"],"commands":["compact","always-approve"]}`,
			`{"type":"thought","data":"Running the ls command in terminal."}`,
			`{"type":"text","data":"I will run "}`,
			`{"type":"text","data":"ls."}`,
			`{"type":"tool_call","toolCallId":"call-123","toolName":"run_terminal_command","rawInput":{"command":"ls"}}`,
			`{"type":"tool_call_update","toolCallId":"call-123","status":"completed","content":[{"type":"content","content":{"type":"text","text":"file1\nfile2"}}],"rawOutput":{"type":"Bash","output":[102,105,108,101,49],"exit_code":0,"command":"ls"}}`,
			`{"type":"usage","usage":{"input_tokens":100,"output_tokens":25,"total_tokens":125}}`,
			`{"type":"end","stopReason":"end_turn","sessionId":"11111111-2222-3333-4444-555555555555","usage":{"input_tokens":100,"output_tokens":25,"total_tokens":125}}`,
		}
		for _, l := range lines {
			_, _ = outPipeW.Write([]byte(l + "\n"))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	var received []agentlink.Frame
	for f := range framesCh {
		received = append(received, f)
	}

	var hasThought, hasText, hasToolStart, hasToolEnd, hasResult bool
	for _, f := range received {
		switch f.Kind {
		case agentlink.KindThought:
			if strings.Contains(f.Text, "Running the ls command") {
				hasThought = true
			}
		case agentlink.KindText:
			if strings.Contains(f.Text, "I will run") || strings.Contains(f.Text, "ls.") {
				hasText = true
			}
		case agentlink.KindTool:
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolStart && f.Tool.Name == "run_terminal_command" {
				hasToolStart = true
			}
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolEnd && strings.Contains(f.Tool.Result, "file1") {
				hasToolEnd = true
			}
		case agentlink.KindResult:
			if f.Usage != nil && f.Usage.TotalTokens == 125 {
				hasResult = true
			}
		}
	}

	if !hasThought {
		t.Error("missing thought frame with 'data' field")
	}
	if !hasText {
		t.Error("missing text frame with 'data' field")
	}
	if !hasToolStart {
		t.Error("missing tool start frame with toolName/rawInput")
	}
	if !hasToolEnd {
		t.Error("missing tool end frame with content blocks")
	}
	if !hasResult {
		t.Error("missing result frame with token usage")
	}
}

func TestGrokRemote_RealSpawn(t *testing.T) {
	if os.Getenv("OPENAGY_LIVE_TEST") == "" {
		t.Skip("skipping live grok test without OPENAGY_LIVE_TEST=1")
	}
	grokBin, err := exec.LookPath("grok")
	if err != nil {
		grokBin, err = exec.LookPath("grok-build")
	}
	if err != nil {
		t.Skip("real grok binary not found on PATH, skipping live spawn test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := SpawnGrokRemote(ctx, grokBin, cwd, "grok-4.7", "", "")
	if err != nil {
		t.Fatalf("SpawnGrokRemote failed: %v", err)
	}
	defer remote.Close()

	framesCh, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "remember the secret code word: avocado-42"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	var text1 string
	for f := range framesCh {
		if f.Kind == agentlink.KindText {
			text1 += f.Text
		}
	}
	if text1 == "" {
		t.Fatal("expected text output from real grok turn 1, got none")
	}
	t.Logf("Turn 1 output: %s", text1)

	// Turn 2 resumes the session
	framesCh2, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "remember the secret code word: avocado-42"},
			{Role: "assistant", Content: text1},
			{Role: "user", Content: "what was the secret code word?"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	var text2 string
	for f := range framesCh2 {
		t.Logf("Turn 2 frame: kind=%s text=%q err=%s", f.Kind, f.Text, f.Error)
		if f.Kind == agentlink.KindText {
			text2 += f.Text
		}
	}
	if text2 == "" {
		t.Fatal("expected text output from real grok turn 2, got none")
	}
	t.Logf("Turn 2 output: %s", text2)
	if !strings.Contains(strings.ToLower(text2), "avocado") {
		t.Errorf("expected Turn 2 to resume and remember 'avocado', got: %s", text2)
	}
}
