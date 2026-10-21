package piagentbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/tools"
)

// Tool adapts one clone tools.Tool into an agent.AgentTool, so PiG's loop can
// advertise and run the recovered Antigravity tool surface unchanged.
//
// The two interfaces line up almost exactly:
//
//	tools.Tool          agent.AgentTool
//	------------------  --------------------------------
//	Name() string       Name() string
//	Schema() RawMessage Schema() ai.ToolSchema
//	Run(...) (any, err) Execute(..., onUpdate) (AgentToolResult, error)
//
// The only real asymmetry is the description: tools.Tool has no Description
// method (the clone carries descriptions only in the reference binary's
// protobuf catalog), so Schema synthesises one from the tool name.
func Tool(tool tools.Tool) piagent.AgentTool {
	return toolAdapter{inner: tool}
}

// Tools wraps every tool in the registry. Names are sorted so the advertised
// tool list is deterministic across runs (Registry.List is map-ordered).
func Tools(registry *tools.Registry) []piagent.AgentTool {
	if registry == nil {
		return nil
	}
	names := registry.List()
	sort.Strings(names)
	out := make([]piagent.AgentTool, 0, len(names))
	for _, name := range names {
		if tool, ok := registry.Get(name); ok {
			out = append(out, Tool(tool))
		}
	}
	return out
}

// SurfaceTools returns the reference's twelve model-facing declarations, each
// wired to an executor. extras (PiG's subagenttools) win over the registry for
// a name they own, so invoke_subagent / send_message / manage_subagents keep
// their real implementations while advertising the captured schema.
//
// This is the print path's equivalent of internal/agent's declaration
// override: print mode hands PiG a flat list of AgentTools, so the wrapper has
// to carry both halves.
func SurfaceTools(registry *tools.Registry, extras []piagent.AgentTool) []piagent.AgentTool {
	byName := make(map[string]piagent.AgentTool, len(extras))
	for _, t := range extras {
		if t != nil {
			byName[t.Name()] = t
		}
	}
	decls := tools.ModelDeclarations()
	out := make([]piagent.AgentTool, 0, len(decls))
	for _, d := range decls {
		exec := byName[d.Name]
		if exec == nil && registry != nil {
			if t, ok := registry.Get(d.Name); ok {
				exec = Tool(t)
			}
		}
		out = append(out, &surfaceTool{decl: d, exec: exec})
	}
	return out
}

// surfaceTool advertises one captured declaration and executes through exec.
// A nil exec means no executor is registered; the tool still appears on the
// wire (the declaration set is fixed) and reports the missing implementation
// the same way a pending catalogue entry does.
type surfaceTool struct {
	decl tools.ModelToolDecl
	exec piagent.AgentTool
}

func (t *surfaceTool) Name() string { return t.decl.Name }

func (t *surfaceTool) Label() string {
	if t.exec != nil && t.exec.Label() != "" {
		return t.exec.Label()
	}
	return t.decl.Name
}

func (t *surfaceTool) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{Name: t.decl.Name, Description: t.decl.Description}
	if params := tools.SchemaParams(t.decl.Parameters); params != nil {
		schema.Parameters = params
	}
	return schema
}

func (t *surfaceTool) ExecutionMode() piagent.ToolExecutionMode {
	if t.exec != nil {
		return t.exec.ExecutionMode()
	}
	return piagent.ToolModeSequential
}

func (t *surfaceTool) Execute(ctx context.Context, id string, args json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	if t.exec == nil {
		return piagent.AgentToolResult{}, fmt.Errorf("%s: %w", t.decl.Name, tools.ErrNotImplemented)
	}
	return t.exec.Execute(ctx, id, args, onUpdate)
}

type toolAdapter struct {
	inner tools.Tool
}

func (t toolAdapter) Name() string { return t.inner.Name() }

// Label falls back to the tool name; the clone has no separate display name.
func (t toolAdapter) Label() string { return t.inner.Name() }

func (t toolAdapter) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{
		Name:        t.inner.Name(),
		Description: t.inner.Name() + " (openagy tool)",
	}
	if text := tools.Describe(t.inner); text != "" {
		schema.Description = text
	}
	raw := t.inner.Schema()
	if len(raw) == 0 {
		return schema
	}
	var parameters map[string]any
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return schema
	}
	schema.Parameters = parameters
	return schema
}

// ExecutionMode is sequential: the clone's loop runs one tool call at a time,
// and the fs/shell tools mutate shared workspace state.
func (t toolAdapter) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

// Execute runs the wrapped tool. The clone's tools return a Go error for
// anything that is not a normal result; returning it here lets PiG produce its
// standard error tool-result rather than the bridge inventing one.
func (t toolAdapter) Execute(ctx context.Context, _ string, params json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	_ = onUpdate
	out, err := t.inner.Run(ctx, params)
	if err != nil {
		return piagent.AgentToolResult{}, err
	}
	text := RenderResult(out)
	budget := tools.ApplyOutputBudget(text, "", tools.DefaultMaxResultTokens)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: budget.Content}},
	}, nil
}

// RenderResult flattens a clone tool's return value into the text block PiG's
// tool-result message carries. The clone's tools return either a string or an
// arbitrary JSON-marshalable struct.
func RenderResult(out any) string {
	switch value := out.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	case json.RawMessage:
		return string(value)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", out)
	}
	return string(encoded)
}
