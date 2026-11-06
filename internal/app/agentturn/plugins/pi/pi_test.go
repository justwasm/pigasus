package pi_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/agentturn/plugins/pi"
)

func TestPiDriverRegistration(t *testing.T) {
	d, ok := agentturn.Lookup("pi")
	if !ok {
		t.Fatalf("expected pi driver to be registered")
	}
	if d.Name != "pi" {
		t.Errorf("expected driver name 'pi', got %q", d.Name)
	}

	alias, ok := agentturn.Lookup("pi-coding-agent")
	if !ok {
		t.Fatalf("expected pi-coding-agent alias to be registered")
	}
	if alias.Name != "pi" {
		t.Errorf("expected alias name 'pi', got %q", alias.Name)
	}

	pigDriver, ok := agentturn.Lookup("pig")
	if !ok {
		t.Fatalf("expected pig driver to be registered")
	}
	if pigDriver.Name != "pig" {
		t.Errorf("expected driver name 'pig', got %q", pigDriver.Name)
	}
}

type mockCloser struct {
	io.Writer
	closed bool
}

func (m *mockCloser) Close() error {
	m.closed = true
	return nil
}

func TestPiRemote_NDJSONStream(t *testing.T) {
	ndjsonInput := strings.Join([]string{
		`{"type":"session","version":3,"id":"test-session-123","cwd":"/workspace"}`,
		`{"type":"message_update","usage":{"input":100,"output":20,"totalTokens":120,"cacheRead":5},"assistantMessageEvent":{"type":"thinking_delta","delta":"Let me think..."}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"Hello world!"}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo test"}}`,
		`{"type":"tool_execution_update","toolCallId":"call_1","toolName":"bash","partialResult":{"content":[{"type":"text","text":"running..."}]}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"test output\n"}]},"isError":false}`,
		`{"type":"compaction_start","reason":"threshold"}`,
		`{"type":"turn_end"}`,
		`{"type":"agent_settled","aborted":false}`,
	}, "\n") + "\n"

	inBuf := &bytes.Buffer{}
	closer := &mockCloser{Writer: inBuf}
	outBuf := bytes.NewBufferString(ndjsonInput)

	remote, err := pi.NewPiRemote(closer, outBuf, closer, nil, "test-session-123")
	if err != nil {
		t.Fatalf("failed to create PiRemote: %v", err)
	}
	defer remote.Close()

	if remote.SessionID() != "test-session-123" {
		t.Errorf("expected sessionID 'test-session-123', got %q", remote.SessionID())
	}
	if remote.EngineName() != "pi" {
		t.Errorf("expected engine name 'pi', got %q", remote.EngineName())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	turn := agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "echo test"},
		},
	}

	ch, err := remote.Turn(ctx, turn)
	if err != nil {
		t.Fatalf("Turn returned error: %v", err)
	}

	var frames []agentlink.Frame
	for f := range ch {
		frames = append(frames, f)
	}

	if len(frames) == 0 {
		t.Fatalf("expected frames, got 0")
	}

	// Verify kinds received
	var hasThought, hasText, hasToolStart, hasToolUpdate, hasToolEnd, hasCompact, hasResult bool
	for _, f := range frames {
		switch f.Kind {
		case agentlink.KindThought:
			hasThought = true
			if f.Text != "Let me think..." {
				t.Errorf("unexpected thought text: %q", f.Text)
			}
		case agentlink.KindText:
			hasText = true
			if f.Text != "Hello world!" {
				t.Errorf("unexpected text: %q", f.Text)
			}
		case agentlink.KindTool:
			if f.Tool != nil {
				if f.Tool.Phase == agentlink.ToolStart && f.Tool.Args != "" {
					hasToolStart = true
					if f.Tool.Name != "bash" || f.ID != "call_1" {
						t.Errorf("unexpected tool start: %+v", f)
					}
				} else if f.Tool.Phase == agentlink.ToolStart && f.Tool.Result != "" {
					hasToolUpdate = true
				} else if f.Tool.Phase == agentlink.ToolEnd {
					hasToolEnd = true
					if f.Tool.Result != "test output\n" {
						t.Errorf("unexpected tool output: %q", f.Tool.Result)
					}
				}
			}
		case agentlink.KindCompact:
			hasCompact = true
		case agentlink.KindResult:
			hasResult = true
			if f.Usage == nil || f.Usage.TotalTokens != 120 {
				t.Errorf("unexpected usage in result: %+v", f.Usage)
			}
		}
	}

	if !hasThought {
		t.Errorf("expected KindThought frame")
	}
	if !hasText {
		t.Errorf("expected KindText frame")
	}
	if !hasToolStart {
		t.Errorf("expected tool_start frame")
	}
	if !hasToolUpdate {
		t.Errorf("expected tool_update frame")
	}
	if !hasToolEnd {
		t.Errorf("expected tool_end frame")
	}
	if !hasCompact {
		t.Errorf("expected KindCompact frame")
	}
	if !hasResult {
		t.Errorf("expected KindResult frame")
	}

	// Verify written prompt to stdin
	written := inBuf.String()
	if !strings.Contains(written, `"type":"prompt"`) || !strings.Contains(written, `"echo test"`) {
		t.Errorf("expected prompt written to stdin, got %q", written)
	}
}

func TestPiRemote_ModelsAndCommands(t *testing.T) {
	inBuf := &bytes.Buffer{}
	closer := &mockCloser{Writer: inBuf}
	remote, err := pi.NewPiRemote(closer, nil, closer, nil, "ses-1")
	if err != nil {
		t.Fatalf("NewPiRemote: %v", err)
	}
	defer remote.Close()

	models, err := remote.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) == 0 {
		t.Errorf("expected at least one model")
	}

	cmds := remote.AvailableCommands()
	if len(cmds) == 0 {
		t.Errorf("expected commands from PiRemote")
	}

	caps := remote.Capabilities()
	if caps&agentturn.CapModelSwitch == 0 {
		t.Errorf("expected CapModelSwitch")
	}
}
