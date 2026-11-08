package subagenttools

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/actor"
)

// stubTool is a minimal actor tool: the test only needs a call to reach it.
type stubTool struct {
	name string
}

func (t *stubTool) Name() string        { return t.name }
func (t *stubTool) Description() string { return t.name }
func (t *stubTool) Definition() actor.ToolDefinition {
	return actor.ToolDefinition{
		Name:        t.name,
		Description: t.name,
	}
}
func (t *stubTool) RequiresApproval(_ string) bool { return false }
func (t *stubTool) Execute(context.Context, string) (actor.ToolResult, error) {
	return actor.ToolResult{Output: "clicked"}, nil
}

// toolCallProvider answers the first step with a call to name, then finishes.
func toolCallProvider(name string) *actor.MockProvider {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{ToolCalls: []actor.ToolCall{{ID: "call-1", Name: name, Input: "{}"}}},
	})
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "done", FinishReason: "stop"},
	})
	return mock
}

func TestSubagentSessionCarriesTheInvocationTools(t *testing.T) {
	mock := toolCallProvider("browser_click_element")
	act, err := actor.Spawn(actor.Config{
		ID: "test-subagent",
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{&stubTool{name: "browser_click_element"}},
		},
	})
	if err != nil {
		t.Fatalf("actor.Spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "click it")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	sawCall := false
	for ev := range turn.Events() {
		if ev.Type == actor.EventToolCallFinished && ev.ToolResult != nil {
			sawCall = true
			if ev.ToolResult.Error != "" {
				t.Errorf("tool call failed: %s", ev.ToolResult.Error)
			}
			if ev.ToolResult.Output != "clicked" {
				t.Errorf("tool output = %q, want 'clicked'", ev.ToolResult.Output)
			}
		}
	}
	if !sawCall {
		t.Fatal("the turn never executed the invocation tool")
	}
}

func TestSubagentSessionWithoutToolsHasNone(t *testing.T) {
	mock := toolCallProvider("browser_click_element")
	act, err := actor.Spawn(actor.Config{
		ID:      "test-subagent-none",
		Default: actor.TurnConfig{Provider: mock},
	})
	if err != nil {
		t.Fatalf("actor.Spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "click it")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	sawUnregistered := false
	for ev := range turn.Events() {
		if ev.Type == actor.EventToolCallFinished && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Error, "not registered") {
				sawUnregistered = true
			}
		}
	}
	if !sawUnregistered {
		t.Fatal("a turn without tools still executed the call")
	}
}
