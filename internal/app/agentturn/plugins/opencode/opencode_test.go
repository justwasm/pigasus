package opencode

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

func TestOpencodeRemote_DriverRegistration(t *testing.T) {
	d, ok := agentturn.Lookup("opencode")
	if !ok {
		t.Fatal("expected 'opencode' driver to be registered")
	}
	if d.Name != "opencode" {
		t.Errorf("expected driver name 'opencode', got %q", d.Name)
	}
	dAlias, ok := agentturn.Lookup("open-code")
	if !ok || dAlias.Name != "opencode" {
		t.Fatalf("expected 'open-code' alias to resolve to 'opencode'")
	}
}

func TestOpencodeRemote_MockNDJSON(t *testing.T) {
	inBuf := new(bytes.Buffer)
	outPipeR, outPipeW := io.Pipe()

	closer := &mockReadWriteCloser{in: inBuf}
	r, err := NewOpencodeRemote(closer, outPipeR, closer, nil, "ses_test_mock_1")
	if err != nil {
		t.Fatalf("NewOpencodeRemote failed: %v", err)
	}
	defer r.Close()

	if r.SessionID() != "ses_test_mock_1" {
		t.Errorf("expected session id ses_test_mock_1, got %s", r.SessionID())
	}
	if name := r.EngineName(); name != "opencode" {
		t.Errorf("expected EngineName 'opencode', got %s", name)
	}
	if caps := r.Capabilities(); caps&agentturn.CapModelSwitch == 0 || caps&agentturn.CapReasoningTier == 0 {
		t.Errorf("expected CapModelSwitch and CapReasoningTier, got %v", caps)
	}

	turnCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	framesCh, err := r.Turn(turnCtx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "List directory files"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	go func() {
		defer outPipeW.Close()
		lines := []string{
			`{"type":"step_start","timestamp":1793708242390,"sessionID":"ses_test_mock_1","part":{"type":"step-start"}}`,
			`{"type":"reasoning","timestamp":1793708242885,"sessionID":"ses_test_mock_1","part":{"type":"reasoning","text":"Thinking about running ls command"}}`,
			`{"type":"tool_use","timestamp":1793708242923,"sessionID":"ses_test_mock_1","part":{"id":"call_opencode_1","tool":"shell","state":{"status":"completed","input":{"command":"ls"},"output":"fileA\nfileB"}}}`,
			`{"type":"text","timestamp":1793708253298,"sessionID":"ses_test_mock_1","part":{"type":"text","text":"Here are the files: fileA and fileB."}}`,
			`{"type":"step_finish","timestamp":1793708253378,"sessionID":"ses_test_mock_1","part":{"type":"step-finish","reason":"stop","tokens":{"input":150,"output":30,"reasoning":10}}}`,
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
			if strings.Contains(f.Text, "Thinking about running ls") {
				hasThought = true
			}
		case agentlink.KindText:
			if strings.Contains(f.Text, "Here are the files") {
				hasText = true
			}
		case agentlink.KindTool:
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolStart && f.Tool.Name == "shell" {
				hasToolStart = true
			}
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolEnd && strings.Contains(f.Tool.Result, "fileA") {
				hasToolEnd = true
			}
		case agentlink.KindResult:
			if f.Usage != nil && f.Usage.TotalTokens == 180 {
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

func TestOpencodeRemote_ListModelsAndCommands(t *testing.T) {
	r := &OpencodeRemote{}
	models, err := r.ListModels(context.Background())
	if err != nil || len(models) == 0 {
		t.Fatalf("expected OpenCode models, got error %v", err)
	}
	cmds := r.AvailableCommands()
	if len(cmds) == 0 {
		t.Fatal("expected OpenCode commands, got empty")
	}
}

func TestOpencodeRemote_RealSpawn(t *testing.T) {
	if os.Getenv("OPENAGY_LIVE_TEST") == "" {
		t.Skip("skipping live opencode test without OPENAGY_LIVE_TEST=1")
	}
	opencodeBin, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("real opencode binary not found on PATH, skipping live spawn test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := SpawnOpencodeRemote(ctx, opencodeBin, cwd, "local/gemini-3.8-flash-high", "")
	if err != nil {
		t.Fatalf("SpawnOpencodeRemote failed: %v", err)
	}
	defer remote.Close()

	framesCh, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "remember the secret word: pineapple-99"},
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
		t.Fatal("expected text output from real opencode turn 1, got none")
	}
	t.Logf("Turn 1 output: %s", text1)

	// Turn 2 resumes the session
	framesCh2, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "remember the secret word: pineapple-99"},
			{Role: "assistant", Content: text1},
			{Role: "user", Content: "what was the secret word?"},
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
		t.Fatal("expected text output from real opencode turn 2, got none")
	}
	t.Logf("Turn 2 output: %s", text2)
	if !strings.Contains(strings.ToLower(text2), "pineapple") {
		t.Errorf("expected Turn 2 to resume and remember 'pineapple', got: %s", text2)
	}
}
