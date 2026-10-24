package agent

import (
	"context"
	"encoding/json"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// pigTools adapts the advertised tool declarations into PiG tools, each backed
// by the caller's ToolRunner.
//
// A nil ToolRunner advertises nothing. The clone's loop still surfaced the
// model's tool calls to the handler in that case, and it still does — the calls
// ride on the assistant message, which the translator reads regardless of
// whether anything was advertised.
//
// ExtraAgentTools are appended verbatim. Cycle 228 uses this seam for
// subagent tools (internal/subagenttools) that drive their own LLM turn and
// therefore cannot share the clone's runner pipeline.
func (l *Loop) pigTools() []piagent.AgentTool {
	extraByName := make(map[string]piagent.AgentTool, len(l.opts.ExtraAgentTools))
	for _, t := range l.opts.ExtraAgentTools {
		if t != nil {
			extraByName[t.Name()] = t
		}
	}
	used := make(map[string]bool, len(extraByName))

	var tools []piagent.AgentTool
	if l.opts.ToolRunner != nil {
		tools = make([]piagent.AgentTool, 0, len(l.opts.Tools)+len(l.opts.ExtraAgentTools))
		for _, decl := range l.opts.Tools {
			if extra, ok := extraByName[decl.Name]; ok {
				// Cycle 293: the declaration is the reference's captured
				// schema, which is what goes on the wire. The extra keeps its
				// real implementation behind that declaration (its own schema
				// describes a different, clone-shaped wire).
				used[decl.Name] = true
				tools = append(tools, &declOverrideTool{decl: decl, exec: extra})
				continue
			}
			tools = append(tools, &runnerTool{decl: decl, runner: l.opts.ToolRunner})
		}
	} else if len(l.opts.Tools) == 0 {
		// No runner + no decls: still append any extras the caller wired in,
		// so a single-turn loop with subagenttools.Extras is not silently
		// empty.
		if len(l.opts.ExtraAgentTools) == 0 {
			return nil
		}
		tools = make([]piagent.AgentTool, 0, len(l.opts.ExtraAgentTools))
	}
	for _, t := range l.opts.ExtraAgentTools {
		if t != nil && !used[t.Name()] {
			tools = append(tools, t)
		}
	}
	return tools
}

// declOverrideTool advertises one captured backend.ToolDecl while executing
// through a PiG AgentTool that owns the same name. Cycle 293 introduced it for
// the three subagent tools: their clone-shaped PiG schemas (prompt /
// subagent_id / action) must not replace the reference's captured ones
// (Prompt / Recipient+Message / Action+ConversationIds) on the wire, but the
// implementations are still the live PiG ones.
type declOverrideTool struct {
	decl backend.ToolDecl
	exec piagent.AgentTool
}

func (t *declOverrideTool) Name() string { return t.decl.Name }

func (t *declOverrideTool) Label() string { return t.exec.Label() }

func (t *declOverrideTool) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{Name: t.decl.Name, Description: t.decl.Description}
	if params := tools.SchemaParams(t.decl.Parameters); params != nil {
		schema.Parameters = params
	}
	return schema
}

func (t *declOverrideTool) ExecutionMode() piagent.ToolExecutionMode {
	return t.exec.ExecutionMode()
}

func (t *declOverrideTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	return t.exec.Execute(ctx, toolCallID, args, onUpdate)
}

// runnerTool is one advertised tool declaration wired to the loop's
// ToolRunner. It preserves the clone's error convention: a failing tool is
// reported to the model as the text "tool error: …" and never aborts the turn.
type runnerTool struct {
	decl   backend.ToolDecl
	runner ToolRunner
}

func (t *runnerTool) Name() string  { return t.decl.Name }
func (t *runnerTool) Label() string { return t.decl.Name }

func (t *runnerTool) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{Name: t.decl.Name, Description: t.decl.Description}
	if params := tools.SchemaParams(t.decl.Parameters); params != nil {
		schema.Parameters = params
	}
	return schema
}

// ExecutionMode is sequential — the clone's loop ran one call at a time, and
// the fs/shell tools mutate shared workspace state.
func (t *runnerTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *runnerTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	result, err := t.runner.Run(ctx, backend.ToolCall{
		ID:        toolCallID,
		Name:      t.decl.Name,
		Arguments: args,
	})
	if err != nil {
		result = "tool error: " + err.Error()
	}
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: result}},
	}, nil
}
