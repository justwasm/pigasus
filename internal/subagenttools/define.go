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
		Description: "Define a subagent for the duration of this conversation.",
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"name":          pi.JsonObject{"type": "string", "description": "Unique name for the subagent."},
				"description":   pi.JsonObject{"type": "string", "description": "What the subagent is for."},
				"system_prompt": pi.JsonObject{"type": "string", "description": "The subagent's system instruction."},
			},
			"required": []string{"name", "system_prompt"},
		},
	}
}

type defineArgs struct {
	Name           string `json:"name"`
	NameCap        string `json:"Name,omitempty"`
	Description    string `json:"description,omitempty"`
	DescriptionCap string `json:"Description,omitempty"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	SystemCap      string `json:"SystemPrompt,omitempty"`
	// The three tool grants the reference's definition carries. The
	// live handler does not act on them (the clone's subagent session
	// gets the parent's tool set), but the rehydrator carries them in
	// the delta so a later cycle that implements them does not have to
	// read the trajectory again.
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
	if t.defs != nil {
		t.defs.Put(name, Invocation{Prompt: prompt, SystemPrompt: prompt})
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
