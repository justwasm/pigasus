package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// defineTool registers a conversation-scoped subagent definition. 【一手】
// 1.4.4 result:
//
//	Subagent "cycletest" defined successfully. It can now be invoked via
//	invoke_subagent.
type defineTool struct{ defs *Store }

func (t *defineTool) Name() string  { return "define_subagent" }
func (t *defineTool) Label() string { return "Define Subagent" }
func (t *defineTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *defineTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        "define_subagent",
		Description: defineSubagentDescription,
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"name":          pi.JsonObject{"type": "string", "description": "Unique name for the subagent."},
				"description":   pi.JsonObject{"type": "string", "description": "What the subagent is for."},
				"system_prompt": pi.JsonObject{"type": "string", "description": "The subagent's system instruction."},
				"enable_write_tools": pi.JsonObject{
					"type":        "boolean",
					"description": "Grant tools to create and edit files, and run commands.",
				},
				"enable_mcp_tools": pi.JsonObject{
					"type":        "boolean",
					"description": "Grant access to MCP tools.",
				},
				"enable_subagent_tools": pi.JsonObject{
					"type":        "boolean",
					"description": "Grant tools to define and invoke its own subagents.",
				},
			},
			"required": []string{"name", "system_prompt"},
		},
	}
}

// defineSubagentDescription is the clone's own wording for the tool.
//
// 【一手】 the reference's captured description is
//
//	"Define a new subagent type that can then be invoked repeatedly.
//	 Subagents always have tools to research the codebase and to
//	 communicate with other agents."
//
// and that sentence is the base set this package implements: the
// research tools and the messaging tool are always granted, and the
// three switches add the rest.
const defineSubagentDescription = "Define a subagent for the duration of this conversation. " +
	"Subagents always have tools to research the codebase and to communicate with other agents; " +
	"the grants add the write tools, the MCP tools and the tools to define and invoke its own subagents."

type defineArgs struct {
	Name           string `json:"name"`
	NameCap        string `json:"Name,omitempty"`
	Description    string `json:"description,omitempty"`
	DescriptionCap string `json:"Description,omitempty"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	SystemCap      string `json:"SystemPrompt,omitempty"`
	// The three tool grants the reference's definition carries. They
	// decide what the defined subagent may call: the research tools and
	// the messaging tool are always there, and the switches add the
	// write tools, the MCP tools and the subagent tools.
	//
	// 【一手】 the reference stores them capitalised:
	// {"Name","Description","SystemPrompt","EnableWriteTools",…}.
	EnableMCPTools    *bool `json:"EnableMCPTools,omitempty"`
	EnableMCPToolsAlt *bool `json:"enable_mcp_tools,omitempty"`
	EnableWriteTools  *bool `json:"EnableWriteTools,omitempty"`
	// EnableWriteToolsAlt is the snake_case spelling of the write
	// grant.
	EnableWriteToolsAlt *bool `json:"enable_write_tools,omitempty"`
	EnableSubagentTools *bool `json:"EnableSubagentTools,omitempty"`
	// EnableSubagentToolsAlt is the snake_case spelling of the
	// subagent grant.
	EnableSubagentToolsAlt *bool `json:"enable_subagent_tools,omitempty"`
}

// grant reads the three switches off the parsed arguments.
//
// Both spellings are accepted: the reference's own declaration uses the
// capitalised names (the model sends those, because the captured
// declaration is what goes on the wire) and the clone's own schema uses
// the snake_case ones.
func (a defineArgs) grant() Grant {
	return Grant{
		Write:    boolOf(a.EnableWriteTools, a.EnableWriteToolsAlt),
		MCP:      boolOf(a.EnableMCPTools, a.EnableMCPToolsAlt),
		Subagent: boolOf(a.EnableSubagentTools, a.EnableSubagentToolsAlt),
	}
}

// boolOf reads a boolean switch that may be spelled two ways.
func boolOf(values ...*bool) bool {
	for _, v := range values {
		if v != nil {
			return *v
		}
	}
	return false
}

func (t *defineTool) Execute(_ context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args defineArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("define_subagent: parse args: %w", err)
	}
	name := firstNonEmpty(args.Name, args.NameCap)
	prompt := firstNonEmpty(args.SystemPrompt, args.SystemCap)
	_ = firstNonEmpty(args.Description, args.DescriptionCap)
	if name == "" || prompt == "" {
		return piagent.AgentToolResult{}, errors.New("define_subagent: Name and SystemPrompt are required")
	}
	switch name {
	case "self", "research",
		"DeepCoder", "DeepCoderWorkerL0", "DeepCoderImprovementWorker",
		"DeepInvestigator", "DeepInvestigatorWorkerL0", "DeepInvestigatorImprovementWorker":
		return piagent.AgentToolResult{}, fmt.Errorf("define_subagent: %q is a reserved built-in subagent name and cannot be redefined", name)
	}
	if t.defs != nil {
		t.defs.Put(name, Invocation{Prompt: prompt, SystemPrompt: prompt, Grant: args.grant()})
	}
	text := resultStamp(time.Now()) + fmt.Sprintf("Subagent %q defined successfully. It can now be invoked via invoke_subagent.", name)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
