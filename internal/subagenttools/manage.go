package subagenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// manageSubagentsTool answers manage_subagents. The clone has no subagent
// registry: delegations are synchronous, so no subagent is ever live while the
// parent runs a tool. That makes every answer deterministic, and the texts
// below are the reference's own (【反编译】 `tools.(*ManageSubagentsTool)`
// `.list` @0x101d5fff0 and `.kill` @0x101d60560; see
// docs/cycles/async-subagent-framing/deliverable.md).
type manageSubagentsTool struct{}

func (t *manageSubagentsTool) Name() string  { return "manage_subagents" }
func (t *manageSubagentsTool) Label() string { return "Manage Subagents" }
func (t *manageSubagentsTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *manageSubagentsTool) Schema() pi.ToolSchema {
	// Mirrors the captured 1.4.4 declaration: Action ∈ {list, kill, kill_all}
	// plus ConversationIds. `status` is not in the reference's enum; the
	// executor still accepts it as an alias for `list` (clone-only).
	return pi.ToolSchema{
		Name:        "manage_subagents",
		Description: manageSubagentsDescription,
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"Action": pi.JsonObject{
					"type":        "string",
					"enum":        []string{"list", "kill", "kill_all"},
					"description": "'kill' terminates the subagents named in ConversationIds; 'kill_all' terminates all.",
				},
				"ConversationIds": pi.JsonObject{
					"type":        "array",
					"description": "Subagents to kill. Required for 'kill'.",
					"items":       pi.JsonObject{"type": "string"},
				},
			},
			"required": []string{"Action"},
		},
	}
}

const manageSubagentsDescription = "Manage the direct subagents of this conversation. " +
	"'list' reports each one's conversationId (the id used to kill or message it) " +
	"and live state. Killing a subagent also kills its descendants and deletes " +
	"their branched workspaces; logs and artifacts are preserved."

type manageSubagentsArgs struct {
	Action             string   `json:"action"`
	ActionCap          string   `json:"Action,omitempty"`
	SubagentID         string   `json:"subagent_id,omitempty"`
	ConversationIds    []string `json:"conversation_ids,omitempty"`
	ConversationIdsCap []string `json:"ConversationIds,omitempty"`
}

// effectiveAction is the action as the model spelled it, preferring the
// reference's capitalised form. Used for error texts only.
func (a manageSubagentsArgs) effectiveAction() string {
	if s := strings.TrimSpace(a.Action); s != "" {
		return s
	}
	return strings.TrimSpace(a.ActionCap)
}

func (t *manageSubagentsTool) Execute(_ context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args manageSubagentsArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("manage_subagents: parse args: %w", err)
	}
	action := strings.ToLower(firstNonEmpty(args.Action, args.ActionCap))
	ids := args.ConversationIds
	if len(ids) == 0 {
		ids = args.ConversationIdsCap
	}
	if len(ids) == 0 && strings.TrimSpace(args.SubagentID) != "" {
		ids = []string{strings.TrimSpace(args.SubagentID)}
	}

	body, err := handleManageAction(action, ids, args.effectiveAction())
	if err != nil {
		return piagent.AgentToolResult{}, err
	}
	text := resultStamp(time.Now()) + body
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

func handleManageAction(action string, ids []string, effectiveAction string) (string, error) {
	switch action {
	case "list", "status":
		return fmt.Sprintf("You have %d active subagent(s):\n%s", 0, "[]"), nil
	case "kill", "kill_all":
		if len(ids) > 0 {
			return fmt.Sprintf(
				"conversation %q is not an active subagent of this conversation, so nothing was killed; "+
					"run the %q action to see the active subagents and their ids",
				ids[0], "list"), nil
		}
		return "No active subagents to kill.", nil
	default:
		return "", fmt.Errorf("manage_subagents: action must be one of list, kill, kill_all (got %q)", effectiveAction)
	}
}
