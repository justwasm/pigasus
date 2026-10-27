// browser_tool.go: the reference's `webview_browser` tool on the IDE
// turn's wire.
//
// 【一手】 2026-10-27, the reference's language server ships exactly
// one browser tool — `webview_browser` — with a twenty-value `action`
// enum. The 23 legacy browser names the catalogue advertises are a
// different surface (a catalogue list, not a declaration).
//
// The tool itself lives in agentbrowser; this file is the
// adapter that puts it on PiG's wire:
//
//   - the declaration is the reference's own, byte for byte;
//   - the executor asks the broker to run the action in the IDE half.
//
// The import direction is safe: internal/piagentbridge imports
// agentbrowser, and agentbrowser imports nothing of
// the clone's.
package piagentbridge

import (
	"context"
	"encoding/json"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/tools"
)

// browserTool advertises the reference's `webview_browser` tool and
// runs it through the agent↔browser broker.
type browserTool struct {
	decl tools.ModelToolDecl
	tool *agentbrowser.Tool
}

// BrowserTool returns the reference's `webview_browser` tool as a PiG
// tool.
//
// The broker is the same instance the dispatcher's three
// LanguageServerService RPCs serve, so the agent half and the IDE half
// of the bridge meet in one place.
func BrowserTool(broker *agentbrowser.Broker, opts agentbrowser.Options) piagent.AgentTool {
	decl := agentbrowser.ToolDeclaration()
	return &browserTool{
		decl: tools.ModelToolDecl{
			Name:        decl.Name,
			Description: decl.Description,
			Parameters:  decl.Parameters,
		},
		tool: agentbrowser.NewTool(broker, opts),
	}
}

// BrowserDeclaration returns the reference's `webview_browser`
// declaration, so a caller can advertise it next to the reference's
// twelve captured ones.
func BrowserDeclaration() tools.ModelToolDecl {
	decl := agentbrowser.ToolDeclaration()
	return tools.ModelToolDecl{
		Name:        decl.Name,
		Description: decl.Description,
		Parameters:  decl.Parameters,
	}
}

func (t *browserTool) Name() string { return t.decl.Name }

func (t *browserTool) Label() string { return t.decl.Name }

// Schema advertises the reference's own parameter schema. The runtime
// schema is PiG's lowercased form, so its validator accepts it — the
// reference's wire form carries the schema types in upper case.
func (t *browserTool) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{Name: t.decl.Name, Description: t.decl.Description}
	if params := tools.SchemaParams(t.decl.Parameters); params != nil {
		schema.Parameters = params
	}
	return schema
}

// ExecutionMode is sequential: one browser action at a time, and the
// reference's own browser fence refuses a second owner anyway.
func (t *browserTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

// Execute runs one browser action.
//
// 【反编译】 2026-10-27, the reference renders "Running browser
// action" while the action runs.
func (t *browserTool) Execute(ctx context.Context, _ string, params json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	if onUpdate != nil {
		onUpdate(piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: "Running browser action"}},
		})
	}
	result, err := t.tool.Execute(ctx, params)
	if err != nil {
		if result.Text == "" {
			return piagent.AgentToolResult{}, err
		}
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: result.Text}},
			IsError: true,
		}, nil
	}
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: result.Text}},
	}, nil
}

// Compile-time assertion: the browser tool is a PiG tool.
var _ piagent.AgentTool = (*browserTool)(nil)
