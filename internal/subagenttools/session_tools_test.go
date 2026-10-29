// session_tools_test.go — the cycle-311 test for the subagent session's own
// tool set.
//
// The reference's browser agent is a subagent whose tools are the browser
// tools. The delegation machinery therefore has to be able to hand a
// subagent a tool set of its own; Invocation.Tools is that seam.
package subagenttools

import (
	"context"
	"encoding/json"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// stubTool is a minimal PiG tool: the test only needs the name to show up
// in the session's callable set.
type stubTool struct {
	name string
}

func (t *stubTool) Name() string  { return t.name }
func (t *stubTool) Label() string { return t.name }
func (t *stubTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        t.name,
		Description: t.name,
		Parameters:  pi.JsonObject{"type": "object"},
	}
}
func (t *stubTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}
func (t *stubTool) Execute(context.Context, string, json.RawMessage, piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	return piagent.AgentToolResult{}, nil
}

// TestCreateSubagentSessionCarriesTheInvocationTools pins that a subagent
// gets the tools the caller handed it, and no others.
func TestCreateSubagentSessionCarriesTheInvocationTools(t *testing.T) {
	deps := Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}
	ctx := context.Background()
	rt, session, err := createSubagentSession(ctx, deps, deps.Model, "you are a test", deps.CWD,
		[]piagent.AgentTool{&stubTool{name: "browser_click_element"}}, nil)
	if err != nil {
		t.Fatalf("createSubagentSession: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = rt.Close()
	})

	names := session.CallableToolNames()
	found := false
	for _, name := range names {
		if name == "browser_click_element" {
			found = true
		}
	}
	if !found {
		t.Errorf("the session's callable tools %v do not carry the handed-in tool", names)
	}
}

// TestCreateSubagentSessionWithoutToolsHasNone pins the default: a subagent
// with no tools of its own has none.
func TestCreateSubagentSessionWithoutToolsHasNone(t *testing.T) {
	deps := Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}
	ctx := context.Background()
	rt, session, err := createSubagentSession(ctx, deps, deps.Model, "you are a test", deps.CWD, nil, nil)
	if err != nil {
		t.Fatalf("createSubagentSession: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = rt.Close()
	})

	if names := session.CallableToolNames(); len(names) != 0 {
		t.Errorf("the session's callable tools = %v, want none", names)
	}
}
