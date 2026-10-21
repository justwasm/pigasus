package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// scriptedBackend replays a canned sequence of chunk sets, one per Stream
// call, and records every request it received.
type scriptedBackend struct {
	turns [][]backend.StreamChunk
	reqs  []backend.StreamRequest
	i     int
}

func (s *scriptedBackend) Stream(_ context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	s.reqs = append(s.reqs, req)
	var chunks []backend.StreamChunk
	if s.i < len(s.turns) {
		chunks = s.turns[s.i]
	} else {
		chunks = []backend.StreamChunk{{Done: true}}
	}
	s.i++
	out := make(chan backend.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		out <- c
	}
	close(out)
	return out, nil
}

// fakeRunner records the calls it is asked to execute.
type fakeRunner struct{ calls []backend.ToolCall }

func (f *fakeRunner) Run(_ context.Context, c backend.ToolCall) (string, error) {
	f.calls = append(f.calls, c)
	return "result-of-" + c.Name, nil
}

func toolCallChunks(id, name, args string) []backend.StreamChunk {
	return []backend.StreamChunk{
		{ToolCalls: []backend.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}},
		{Done: true},
	}
}

// TestLoop_ExecutesToolCallsThenStreamsFinal is the core cycle-97 behaviour:
// the model asks for a tool, the loop runs it, feeds the result back on a
// second turn, and the model then answers with text.
func TestLoop_ExecutesToolCallsThenStreamsFinal(t *testing.T) {
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		toolCallChunks("c1", "run_command", `{"command":"date"}`),
		{{Text: "It is noon."}, {Done: true}},
	}}
	runner := &fakeRunner{}
	var got strings.Builder

	l, err := New(Options{
		Backend:      be,
		DefaultModel: "m",
		Handler: func(c backend.StreamChunk) error {
			got.WriteString(c.Text)
			return nil
		},
		Tools:      []backend.ToolDecl{{Name: "run_command"}},
		ToolRunner: runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	err = l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "what time is it"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if runner.calls[0].Name != "run_command" {
		t.Errorf("ran %q, want run_command", runner.calls[0].Name)
	}
	if got.String() != "It is noon." {
		t.Errorf("model text = %q, want %q", got.String(), "It is noon.")
	}
	if len(be.reqs) != 2 {
		t.Fatalf("backend Stream calls = %d, want 2", len(be.reqs))
	}
	// The advertised tools must be sent on every turn.
	if len(be.reqs[0].Tools) != 1 || be.reqs[0].Tools[0].Name != "run_command" {
		t.Errorf("first request tools = %+v, want [run_command]", be.reqs[0].Tools)
	}
	// The second request must carry the assistant call and the tool result.
	req2 := be.reqs[1]
	var sawAssistantCall, sawToolResult bool
	for _, m := range req2.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "c1" {
			sawAssistantCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "c1" && m.Content == "result-of-run_command" {
			sawToolResult = true
		}
	}
	if !sawAssistantCall {
		t.Error("second request is missing the assistant tool_calls message")
	}
	if !sawToolResult {
		t.Error("second request is missing the tool result message")
	}
}

// TestLoop_NoToolCallsIsSingleTurn guards the pre-cycle-97 behaviour: with no
// tool calls the loop streams exactly once.
func TestLoop_NoToolCallsIsSingleTurn(t *testing.T) {
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		{{Text: "hello"}, {Done: true}},
	}}
	runner := &fakeRunner{}
	var got strings.Builder

	l, err := New(Options{
		Backend:      be,
		DefaultModel: "m",
		Handler:      func(c backend.StreamChunk) error { got.WriteString(c.Text); return nil },
		ToolRunner:   runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.String() != "hello" {
		t.Errorf("text = %q, want hello", got.String())
	}
	if len(be.reqs) != 1 {
		t.Errorf("Stream calls = %d, want 1", len(be.reqs))
	}
	if len(runner.calls) != 0 {
		t.Errorf("runner called %d times, want 0", len(runner.calls))
	}
}

// TestLoop_ToolCallsWithoutRunnerAreSurfacedNotExecuted pins the nil-runner
// contract: calls are forwarded to the handler but never executed.
func TestLoop_ToolCallsWithoutRunnerAreSurfacedNotExecuted(t *testing.T) {
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		toolCallChunks("c1", "run_command", `{}`),
	}}
	var calls int
	l, err := New(Options{
		Backend:      be,
		DefaultModel: "m",
		Handler: func(c backend.StreamChunk) error {
			calls += len(c.ToolCalls)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if calls != 1 {
		t.Errorf("surfaced %d calls, want 1", calls)
	}
	if len(be.reqs) != 1 {
		t.Errorf("Stream calls = %d, want 1 (no re-stream without a runner)", len(be.reqs))
	}
}

// TestLoop_ToolIterationBudgetStopsRunaway pins the safety cap.
func TestLoop_ToolIterationBudgetStopsRunaway(t *testing.T) {
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		toolCallChunks("c1", "run_command", `{}`),
		toolCallChunks("c2", "run_command", `{}`),
		toolCallChunks("c3", "run_command", `{}`),
		toolCallChunks("c4", "run_command", `{}`),
	}}
	runner := &fakeRunner{}
	l, err := New(Options{
		Backend:           be,
		DefaultModel:      "m",
		Handler:           func(backend.StreamChunk) error { return nil },
		Tools:             []backend.ToolDecl{{Name: "run_command"}},
		ToolRunner:        runner,
		MaxToolIterations: 3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	err = l.Send(context.Background(), TurnInput{})
	if err == nil {
		t.Fatal("Send returned nil, want budget-exhausted error")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Errorf("error = %v, want budget mention", err)
	}
	// Cycle 279: the budget error must stay classifiable. Auto-continue
	// picks the AGY "**Status**: Cancelled after %d iterations" recovery
	// for an iteration cap and the stream-interrupted copy for everything
	// else, and that branch keys off errors.Is — the pre-cycle
	// fmt.Errorf("… (%d)") lost the sentinel and replayed a cap as a
	// dropped stream.
	if !errors.Is(err, piagent.ErrMaxTurnsReached) {
		t.Errorf("error = %v, want it to wrap piagent.ErrMaxTurnsReached", err)
	}
	// …and the classification that keys off it must agree: an iteration
	// cap gets the "**Status**: Cancelled after N iterations" recovery,
	// not the dropped-stream copy.
	if got, want := recoveryForError(err), fmt.Sprintf(AutoContIterationCapFormat, 3); got != want {
		t.Errorf("recoveryForError(cap) = %q, want %q", got, want)
	}
	if len(runner.calls) != 3 {
		t.Errorf("runner called %d times, want 3", len(runner.calls))
	}
}

// TestLoop_NoToolIterationCapByDefault pins the cycle-279 default: a long
// tool chain is not cut at eight iterations.
//
// The old default (8) is what the reference does NOT do — a real A/B on a
// 30-step task showed agy running 11 model turns where the clone stopped at 8
// with no reply ("it suddenly can't proceed"). PiG's own default is MaxTurns 0
// = unlimited (agent/agent.go:747, "as upstream, which has no turn cap").
func TestLoop_NoToolIterationCapByDefault(t *testing.T) {
	turns := make([][]backend.StreamChunk, 0, 12)
	for i := 0; i < 11; i++ {
		turns = append(turns, toolCallChunks(fmt.Sprintf("c%d", i), "run_command", `{}`))
	}
	turns = append(turns, []backend.StreamChunk{{Text: "done"}, {Done: true}})
	be := &scriptedBackend{turns: turns}
	runner := &fakeRunner{}
	l, err := New(Options{
		Backend:      be,
		DefaultModel: "m",
		Handler:      func(backend.StreamChunk) error { return nil },
		Tools:        []backend.ToolDecl{{Name: "run_command"}},
		ToolRunner:   runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{}); err != nil {
		t.Fatalf("Send: %v (an uncapped loop must finish the 12-turn chain)", err)
	}
	if len(runner.calls) != 11 {
		t.Errorf("runner called %d times, want 11", len(runner.calls))
	}
	if len(be.reqs) != 12 {
		t.Errorf("Stream calls = %d, want 12", len(be.reqs))
	}
}

type fakeCustomAgentTool struct {
	name   string
	called bool
}

func (f *fakeCustomAgentTool) Name() string                          { return f.name }
func (f *fakeCustomAgentTool) Label() string                         { return f.name }
func (f *fakeCustomAgentTool) ExecutionMode() piagent.ToolExecutionMode { return piagent.ToolModeSequential }
func (f *fakeCustomAgentTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{Name: f.name}
}
func (f *fakeCustomAgentTool) Execute(_ context.Context, _ string, _ json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	f.called = true
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: "custom-result"}},
	}, nil
}

// TestLoop_ExtraAgentToolsPrecedence verifies that an extra tool
// overrides a placeholder tool with the same name in opts.Tools,
// preventing shadowing.
func TestLoop_ExtraAgentToolsPrecedence(t *testing.T) {
	customTool := &fakeCustomAgentTool{name: "invoke_subagent"}
	runner := &fakeRunner{}

	l, err := New(Options{
		Backend: &scriptedBackend{turns: [][]backend.StreamChunk{
			toolCallChunks("c1", "invoke_subagent", `{}`),
			{{Text: "done"}, {Done: true}},
		}},
		DefaultModel:    "m",
		Handler:         func(backend.StreamChunk) error { return nil },
		Tools:           []backend.ToolDecl{{Name: "invoke_subagent"}},
		ToolRunner:      runner,
		ExtraAgentTools: []piagent.AgentTool{customTool},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	// Verify pigTools filtered out runnerTool for invoke_subagent
	tools := l.pigTools()
	var invokeCount int
	for _, tool := range tools {
		if tool.Name() == "invoke_subagent" {
			invokeCount++
			if _, ok := tool.(*runnerTool); ok {
				t.Fatalf("runnerTool for invoke_subagent was not filtered out by ExtraAgentTools")
			}
		}
	}
	if invokeCount != 1 {
		t.Errorf("want exactly 1 invoke_subagent tool, got %d", invokeCount)
	}
}
