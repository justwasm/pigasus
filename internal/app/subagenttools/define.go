package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/actor"
	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
)

// defineTool registers a conversation-scoped subagent definition. 【一手】
// 1.4.4 result:
//
//	Subagent "cycletest" defined successfully. It can now be invoked via
//	invoke_subagent.
type defineTool struct{ defs *Store }

func (t *defineTool) Name() string                   { return "define_subagent" }
func (t *defineTool) Description() string            { return defineSubagentDescription }
func (t *defineTool) RequiresApproval(_ string) bool { return false }

func (t *defineTool) Definition() actor.ToolDefinition {
	return actor.ToolDefinition{
		Name:        "define_subagent",
		Description: defineSubagentDescription,
		Parameters: map[string]any{
			"type": "OBJECT",
			"properties": map[string]any{
				"Description": map[string]any{
					"type":        "STRING",
					"description": "What this subagent does and when it should be used.",
					"minLength":   "1",
				},
				"EnableMCPTools": map[string]any{
					"type":        "BOOLEAN",
					"description": "Grant access to MCP tools.",
				},
				"EnableSubagentTools": map[string]any{
					"type":        "BOOLEAN",
					"description": "Grant tools to define and invoke its own subagents.",
				},
				"EnableWriteTools": map[string]any{
					"type":        "BOOLEAN",
					"description": "Grant tools to create and edit files, and run commands.",
				},
				"Name": map[string]any{
					"type":        "STRING",
					"description": "Unique name used to invoke the subagent.",
					"minLength":   "1",
				},
				"SystemPrompt": map[string]any{
					"type":        "STRING",
					"description": "System prompt for the subagent.",
					"minLength":   "1",
				},
			},
			"required": []string{"Name", "Description", "SystemPrompt"},
		},
	}
}

// defineSubagentDescription is the reference's captured description.
const defineSubagentDescription = "Define a new subagent type that can then be invoked repeatedly. " +
	"Subagents always have tools to research the codebase and to communicate with other agents."

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

func (t *defineTool) Execute(_ context.Context, input string) (actor.ToolResult, error) {
	var args defineArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return actor.ToolResult{Error: err.Error()}, fmt.Errorf("define_subagent: parse args: %w", err)
	}
	name := firstNonEmpty(args.Name, args.NameCap)
	prompt := firstNonEmpty(args.SystemPrompt, args.SystemCap)
	_ = firstNonEmpty(args.Description, args.DescriptionCap)
	if name == "" || prompt == "" {
		return actor.ToolResult{Error: "define_subagent: Name and SystemPrompt are required"}, errors.New("define_subagent: Name and SystemPrompt are required")
	}
	switch name {
	case "self", "research",
		"DeepCoder", "DeepCoderWorkerL0", "DeepCoderImprovementWorker",
		"DeepInvestigator", "DeepInvestigatorWorkerL0", "DeepInvestigatorImprovementWorker":
		err := fmt.Errorf("define_subagent: %q is a reserved built-in subagent name and cannot be redefined", name)
		return actor.ToolResult{Error: err.Error()}, err
	}
	if _, ok := builtinsubagent.LookupTeamworkRole(name); ok {
		err := fmt.Errorf("define_subagent: %q is a reserved built-in subagent name and cannot be redefined", name)
		return actor.ToolResult{Error: err.Error()}, err
	}
	if t.defs != nil {
		t.defs.Put(name, Invocation{Prompt: prompt, SystemPrompt: prompt, Grant: args.grant()})
	}
	text := resultStamp(time.Now()) + fmt.Sprintf("Subagent %q defined successfully. It can now be invoked via invoke_subagent.", name)
	return actor.ToolResult{
		Output: text,
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
