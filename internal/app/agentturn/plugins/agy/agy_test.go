package agy

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

func TestAgyRemote_MockTurn(t *testing.T) {
	inBuf := new(bytes.Buffer)
	outPipeR, outPipeW := io.Pipe()

	closer := &mockReadWriteCloser{in: inBuf}
	r, err := NewAgyRemote(closer, outPipeR, closer, nil, "mock-agy-cid-1")
	if err != nil {
		t.Fatalf("NewAgyRemote failed: %v", err)
	}
	defer r.Close()

	if r.ConversationID() != "mock-agy-cid-1" {
		t.Errorf("expected cid mock-agy-cid-1, got %s", r.ConversationID())
	}

	turnCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	framesCh, err := r.Turn(turnCtx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "echo 456"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	// Verify request was written to stdin
	if !strings.Contains(inBuf.String(), `"content":"echo 456"`) {
		t.Fatalf("expected prompt in stdin, got: %s", inBuf.String())
	}

	// Emit simulated NDJSON stream from agy
	go func() {
		defer outPipeW.Close()
		lines := []string{
			`{"event":"init","conversation_id":"agy-real-cid-99"}`,
			`{"event":"step_update","step_update":{"conversation_id":"agy-real-cid-99","step_index":0,"state":"DONE","step_type":"user_input"}}`,
			`{"event":"step_update","step_update":{"conversation_id":"agy-real-cid-99","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"I will run "}}`,
			`{"event":"step_update","step_update":{"conversation_id":"agy-real-cid-99","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"the command.\n"}}`,
			`{"event":"step_update","step_update":{"conversation_id":"agy-real-cid-99","step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"echo 456"}}}}`,
			`{"event":"step_update","step_update":{"conversation_id":"agy-real-cid-99","step_index":2,"state":"DONE","step_type":"tool","tool_name":"run_command","duration_seconds":0.05,"tool_info":{"name":"run_command","parameters":{"CommandLine":"echo 456"},"output":"456\n"}}}`,
			`{"event":"result","result":{"conversation_id":"agy-real-cid-99","status":"SUCCESS","response":"Done: 456","usage":{"input_tokens":100,"output_tokens":20,"thinking_tokens":15,"total_tokens":120}}}`,
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

	if r.ConversationID() != "agy-real-cid-99" {
		t.Errorf("expected updated cid agy-real-cid-99, got %s", r.ConversationID())
	}

	// Verify frames
	var hasText, hasToolStart, hasToolEnd, hasResult bool
	for _, f := range received {
		switch f.Kind {
		case agentlink.KindText:
			if strings.Contains(f.Text, "I will run") {
				hasText = true
			}
		case agentlink.KindTool:
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolStart && f.Tool.Name == "run_command" {
				hasToolStart = true
			}
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolEnd && strings.Contains(f.Tool.Result, "456") {
				hasToolEnd = true
			}
		case agentlink.KindResult:
			if f.Usage != nil && f.Usage.TotalTokens == 120 {
				hasResult = true
			}
		}
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

func TestAgyRemote_ListModels(t *testing.T) {
	r := &AgyRemote{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := r.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least 1 model from AgyRemote")
	}

	foundFlash := false
	for _, m := range models {
		if m.ID == "gemini-3.8-flash-high" || m.ID == "gemini-3.8-flash" {
			foundFlash = true
			break
		}
	}
	if !foundFlash {
		t.Errorf("expected gemini-3.8-flash model in agy model list, got: %+v", models)
	}
}

func TestAgyRemote_DriverRegistration(t *testing.T) {
	d, ok := agentturn.Lookup("agy")
	if !ok {
		t.Fatal("expected 'agy' driver to be registered")
	}
	if d.Name != "agy" {
		t.Errorf("expected driver name 'agy', got %q", d.Name)
	}
	dPig, ok := agentturn.Lookup("pigasus")
	if !ok {
		t.Fatal("expected 'pigasus' driver to be registered")
	}
	if dPig.Name != "pigasus" {
		t.Errorf("expected driver name 'pigasus', got %q", dPig.Name)
	}
}

func TestAgyRemote_LiveTurn(t *testing.T) {
	if os.Getenv("OPENAGY_LIVE_TEST") == "" {
		t.Skip("skipping live agy test without OPENAGY_LIVE_TEST=1")
	}
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("agy binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	remote, err := SpawnAgyRemote(ctx, agyPath, "", "", "", "")
	if err != nil {
		t.Skipf("skipping live agy test: %v", err)
	}
	defer remote.Close()

	framesCh, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "echo test_live_agy"},
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
		t.Error("expected output from live agy turn")
	}
}

func TestPigasusRemote_SpawnAndEngineName(t *testing.T) {
	if os.Getenv("OPENAGY_LIVE_TEST") == "" {
		t.Skip("skipping live pigasus test without OPENAGY_LIVE_TEST=1")
	}
	pigasusPath, err := exec.LookPath("pigasus")
	if err != nil {
		t.Skip("pigasus binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := SpawnPigasusRemote(ctx, pigasusPath, "", "", "", "")
	if err != nil {
		t.Skipf("skipping pigasus test: %v", err)
	}
	defer remote.Close()

	if name := remote.EngineName(); name != "pigasus" {
		t.Errorf("expected EngineName 'pigasus', got %q", name)
	}
	if caps := remote.Capabilities(); caps != agentturn.CapAll {
		t.Errorf("expected CapAll, got %v", caps)
	}
	// Wait for init event to be read
	var cid string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cid = remote.ConversationID()
		if cid != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cid == "" {
		t.Error("expected conversation ID from pigasus init event, got empty")
	}
}
