package agent

import (
	"context"
	"encoding/json"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// Cycle 293: the reference's captured declaration must win over a PiG extra
// that owns the same name, while the extra still executes. The captured schema
// uses protojson's uppercase type names; PiG's metaschema validator rejects
// those, so Schema() hands it the lowercase spelling the transport uppercases
// again on the way out.
func TestPigToolsKeepsDeclarationOverExtra(t *testing.T) {
	extra := &fakeExtra{name: "invoke_subagent"}
	l := &Loop{opts: Options{
		Tools: []backend.ToolDecl{{
			Name:        "invoke_subagent",
			Description: "reference description",
			Parameters:  json.RawMessage(`{"type":"OBJECT","properties":{"TypeName":{"type":"STRING"}},"required":["TypeName"]}`),
		}},
		ToolRunner:      runnerStub{},
		ExtraAgentTools: []piagent.AgentTool{extra},
	}}

	tools := l.pigTools()
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	if got := tools[0].Name(); got != "invoke_subagent" {
		t.Fatalf("name = %q", got)
	}
	schema := tools[0].Schema()
	if schema.Description != "reference description" {
		t.Errorf("description = %q", schema.Description)
	}
	if got := schema.Parameters["type"]; got != "object" {
		t.Errorf("parameter type = %v, want lowercase object", got)
	}
	props, _ := schema.Parameters["properties"].(map[string]any)
	typeName, _ := props["TypeName"].(map[string]any)
	if got := typeName["type"]; got != "string" {
		t.Errorf("TypeName.type = %v, want lowercase string", got)
	}

	if _, err := tools[0].Execute(context.Background(), "call_1", json.RawMessage(`{"TypeName":"x"}`), nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !extra.called {
		t.Fatal("extra did not execute")
	}
}

// An extra with no declaration of its own is still appended — the override is
// for name collisions, not a filter.
func TestPigToolsAppendsUnclaimedExtras(t *testing.T) {
	extra := &fakeExtra{name: "extra_only"}
	l := &Loop{opts: Options{
		Tools:           []backend.ToolDecl{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}},
		ToolRunner:      runnerStub{},
		ExtraAgentTools: []piagent.AgentTool{extra},
	}}
	tools := l.pigTools()
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	if tools[0].Name() != "read_file" || tools[1].Name() != "extra_only" {
		t.Fatalf("order = %q, %q", tools[0].Name(), tools[1].Name())
	}
}

type fakeExtra struct {
	name   string
	called bool
}

func (f *fakeExtra) Name() string  { return f.name }
func (f *fakeExtra) Label() string { return f.name }
func (f *fakeExtra) Schema() pi.ToolSchema {
	return pi.ToolSchema{Name: f.name}
}
func (f *fakeExtra) ExecutionMode() piagent.ToolExecutionMode { return piagent.ToolModeSequential }
func (f *fakeExtra) Execute(context.Context, string, json.RawMessage, piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	f.called = true
	return piagent.AgentToolResult{}, nil
}

type runnerStub struct{}

func (runnerStub) Run(context.Context, backend.ToolCall) (string, error) { return "", nil }
