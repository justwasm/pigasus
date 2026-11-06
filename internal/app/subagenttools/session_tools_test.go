package subagenttools

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/actor"
)

// stubTool is a minimal actor tool: the test only needs the name to show up
// in the session's callable set.
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
	return actor.ToolResult{}, nil
}

func TestSubagentSessionCarriesTheInvocationTools(t *testing.T) {
	mock := actor.NewMockProvider()
	tools := []actor.Tool{&stubTool{name: "browser_click_element"}}
	act, err := actor.Spawn(actor.Config{
		ID:       "test-subagent",
		Provider: mock,
		Tools:    tools,
	})
	if err != nil {
		t.Fatalf("actor.Spawn: %v", err)
	}
	defer act.Close()

	list := act.Tools().List()
	found := false
	for _, tl := range list {
		if tl.Name() == "browser_click_element" {
			found = true
		}
	}
	if !found {
		t.Errorf("actor tools do not carry browser_click_element: got %v", list)
	}
}

func TestSubagentSessionWithoutToolsHasNone(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{
		ID:       "test-subagent-none",
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("actor.Spawn: %v", err)
	}
	defer act.Close()

	if list := act.Tools().List(); len(list) != 0 {
		t.Errorf("actor tools = %v, want none", list)
	}
}
