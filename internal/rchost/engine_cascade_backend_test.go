package rchost

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

type mockEngine struct {
	frames []agentlink.Frame
	err    error
}

func (m *mockEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan agentlink.Frame, len(m.frames))
	for _, f := range m.frames {
		ch <- f
	}
	close(ch)
	return ch, nil
}

func (m *mockEngine) Close() error {
	return nil
}

type recordingSink struct {
	textDeltas []string
	toolStarts []cascades.ToolCall
	toolCalls  []cascades.ToolCall
	states     []string
}

func (s *recordingSink) OnState(c *cascades.Cascade, frame cascades.StateFrame) {
	s.states = append(s.states, frame.Phase)
}

func (s *recordingSink) OnTextDelta(c *cascades.Cascade, delta string) {
	s.textDeltas = append(s.textDeltas, delta)
}

func (s *recordingSink) OnToolCallStart(c *cascades.Cascade, call cascades.ToolCall) {
	s.toolStarts = append(s.toolStarts, call)
}

func (s *recordingSink) OnToolCall(c *cascades.Cascade, call cascades.ToolCall) {
	s.toolCalls = append(s.toolCalls, call)
}

func (s *recordingSink) OnError(c *cascades.Cascade, err error) {}

func (s *recordingSink) OnUserInteraction(c *cascades.Cascade, intr *cascades.PendingInteraction) {}

func TestEngineCascadeBackend_SendStream(t *testing.T) {
	engine := &mockEngine{
		frames: []agentlink.Frame{
			{Kind: agentlink.KindText, Text: "Hello "},
			{Kind: agentlink.KindThought, Text: "Thinking... "},
			{Kind: agentlink.KindText, Text: "World!"},
			{
				Kind: agentlink.KindTool,
				Tool: &agentlink.Tool{
					Name:   "read_file",
					Args:   `{"path":"foo.go"}`,
					Result: "contents",
					Err:    false,
				},
			},
		},
	}

	backend := NewEngineCascadeBackend(engine, "test-model")
	sink := &recordingSink{}

	res, err := backend.SendStream(context.Background(), cascades.Prompt{Text: "hi"}, sink, cascades.SendOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Reply != "Hello World!" {
		t.Errorf("got reply %q, want %q", res.Reply, "Hello World!")
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "read_file" {
		t.Errorf("unexpected tools: %+v", res.ToolCalls)
	}

	if len(sink.toolCalls) != 1 || sink.toolCalls[0].Name != "read_file" {
		t.Errorf("sink did not record tool call: %+v", sink.toolCalls)
	}
	if len(sink.textDeltas) != 3 { // "Hello ", "Thinking... ", "World!"
		t.Errorf("expected 3 text deltas, got %d: %+v", len(sink.textDeltas), sink.textDeltas)
	}
}

func TestEngineCascadeBackend_SendStream_ToolStart(t *testing.T) {
	engine := &mockEngine{
		frames: []agentlink.Frame{
			{
				Kind: agentlink.KindTool,
				Tool: &agentlink.Tool{
					Name:  "run_command",
					Args:  `{"command":"ls"}`,
					Phase: agentlink.ToolStart,
				},
			},
			{
				Kind: agentlink.KindTool,
				Tool: &agentlink.Tool{
					Name:   "run_command",
					Args:   `{"command":"ls"}`,
					Result: "file.txt\n",
					Err:    false,
				},
			},
		},
	}

	backend := NewEngineCascadeBackend(engine, "test-model")
	sink := &recordingSink{}

	res, err := backend.SendStream(context.Background(), cascades.Prompt{Text: "run ls"}, sink, cascades.SendOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sink.toolStarts) != 1 {
		t.Fatalf("expected 1 tool start, got %d", len(sink.toolStarts))
	}
	if sink.toolStarts[0].Name != "run_command" || string(sink.toolStarts[0].Args) != `{"command":"ls"}` {
		t.Errorf("unexpected tool start: %+v", sink.toolStarts[0])
	}

	if len(sink.toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(sink.toolCalls))
	}
	if sink.toolCalls[0].Name != "run_command" || sink.toolCalls[0].Result != "file.txt\n" {
		t.Errorf("unexpected tool call: %+v", sink.toolCalls[0])
	}

	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Result != "file.txt\n" {
		t.Errorf("unexpected result tool calls: %+v", res.ToolCalls)
	}
}
