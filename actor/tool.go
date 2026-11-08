package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ToolDefinition describes a callable capability for the LLM.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall represents a specific invocation of a tool requested by the model.
type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

// ToolResult captures the return payload from a tool execution.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	Terminate  bool   `json:"terminate,omitempty"`
}

type toolCallIDKey struct{}

type toolStepIndexKey struct{}

// ContextWithToolCallID stores the tool call ID into the context.
func ContextWithToolCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallIDFromContext retrieves the tool call ID from the context.
func ToolCallIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(toolCallIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ContextWithToolStepIndex stores the global step index into the context.
//
// A tool that outlives its step — a background task, a subagent — records the
// step it started from, and a checkpoint reports that index the way the
// reference's TaskSnapshot.step_index and SubagentSnapshot.spawn_step_index do.
func ContextWithToolStepIndex(ctx context.Context, index int) context.Context {
	return context.WithValue(ctx, toolStepIndexKey{}, index)
}

// ToolStepIndexFromContext retrieves the global step index from the context.
//
// The second result is false when the context carries no step index, which is
// the case outside an actor turn.
func ToolStepIndexFromContext(ctx context.Context) (int, bool) {
	v, ok := ctx.Value(toolStepIndexKey{}).(int)
	return v, ok
}

// Tool is the interface for capabilities executable by an Actor.
type Tool interface {
	Name() string
	Description() string
	Definition() ToolDefinition
	RequiresApproval(input string) bool
	Execute(ctx context.Context, input string) (ToolResult, error)
}

// FuncTool allows wrapping an arbitrary Go function into a Tool.
type FuncTool struct {
	name             string
	description      string
	parameters       map[string]any
	requiresApproval func(input string) bool
	execute          func(ctx context.Context, input string) (string, error)
}

// FuncToolOption configures a FuncTool.
type FuncToolOption func(*FuncTool)

// WithParameters sets JSON schema parameters for a FuncTool.
func WithParameters(params map[string]any) FuncToolOption {
	return func(ft *FuncTool) {
		ft.parameters = params
	}
}

// WithApprovalGate sets an approval requirement predicate for a FuncTool.
func WithApprovalGate(gate func(input string) bool) FuncToolOption {
	return func(ft *FuncTool) {
		ft.requiresApproval = gate
	}
}

// NewFuncTool constructs a Tool from a Go closure.
func NewFuncTool(
	name string,
	description string,
	exec func(ctx context.Context, input string) (string, error),
	opts ...FuncToolOption,
) *FuncTool {
	ft := &FuncTool{
		name:        name,
		description: description,
		execute:     exec,
	}
	for _, opt := range opts {
		opt(ft)
	}
	return ft
}

func (f *FuncTool) Name() string        { return f.name }
func (f *FuncTool) Description() string { return f.description }

func (f *FuncTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        f.name,
		Description: f.description,
		Parameters:  f.parameters,
	}
}

func (f *FuncTool) RequiresApproval(input string) bool {
	if f.requiresApproval != nil {
		return f.requiresApproval(input)
	}
	return false
}

func (f *FuncTool) Execute(ctx context.Context, input string) (ToolResult, error) {
	if f.execute == nil {
		return ToolResult{Output: "", Error: "no executor implemented"}, nil
	}
	out, err := f.execute(ctx, input)
	if err != nil {
		return ToolResult{
			Output: out,
			Error:  err.Error(),
		}, nil
	}
	return ToolResult{
		Output: out,
	}, nil
}

// ToolRegistry manages registered tools for an Actor.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry initializes an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds one or more tools to the registry.
func (r *ToolRegistry) Register(tools ...Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range tools {
		if t != nil {
			r.tools[t.Name()] = t
		}
	}
}

// Set replaces all registered tools with the provided list.
func (r *ToolRegistry) Set(tools ...Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = make(map[string]Tool, len(tools))
	for _, t := range tools {
		if t != nil {
			r.tools[t.Name()] = t
		}
	}
}

// Get looks up a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Definitions returns the schema definitions for all registered tools.
func (r *ToolRegistry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, t)
	}
	return list
}

// ParseJSONInput parses a tool's JSON string input into a target struct.
func ParseJSONInput(input string, target any) error {
	if input == "" {
		input = "{}"
	}
	if err := json.Unmarshal([]byte(input), target); err != nil {
		return fmt.Errorf("failed to parse tool arguments: %w", err)
	}
	return nil
}
