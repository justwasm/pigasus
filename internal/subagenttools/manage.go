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

// manageSubagentsTool answers manage_subagents: "Manage the direct
// subagents of this conversation."
//
// The reference's own texts are the reference's:
//
//	You have %d active subagent(s):\n%s
//	Successfully killed %d subagent(s) and their descendants.\nKilled roles: %s
//	No active subagents to kill.
//	conversation %q is not an active subagent of this conversation, so nothing was killed; run the %q action to see the active subagents and their ids
//
// 【反编译】 agy 1.4.8's
// `oneharness/internal/tools/tools.(*ManageSubagentsTool).list`
// @0x101deeef0 and `.kill` @0x101def5f0
// (docs/cycles/async-subagent-framing/deliverable.md). In 1.4.8 the
// CLI's tool package moved from `cortex/core/tools/tools` to
// `oneharness/internal/tools/tools`.
// The list entry carries four fields — type, role, conversationId, state —
// and the role falls back to the type's name when the call named none.
type manageSubagentsTool struct {
	// registry is the conversation's live-subagent registry.
	registry *Registry
	// owner is the conversation id of the agent that runs this tool. The
	// `list_peers` action answers differently for the conversation that
	// owns the registry (it has no parent conversation, so it has no
	// peers) and for a subagent (whose peers are its siblings).
	owner string
}

func (t *manageSubagentsTool) Name() string  { return "manage_subagents" }
func (t *manageSubagentsTool) Label() string { return "Manage Subagents" }
func (t *manageSubagentsTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *manageSubagentsTool) Schema() pi.ToolSchema {
	// 【反编译】 agy 1.4.8's declaration: the Action field's jsonschema tag
	// reads "required,enum=list,enum=kill,enum=kill_all" (the CLI-side tag
	// lives at file offset 0x9404b5a) and the description is the two kill
	// clauses only. A live 1.4.8 session sends exactly these three values
	// (internal/tools/model_tools_1.4.6.json, re-verified byte-identical at
	// 1.4.8). `status` is not in the reference's enum; the executor still
	// accepts it as an alias for `list` (clone-only).
	//
	// The reference's own handler knows a fourth action - `list_peers` -
	// although its schema never advertises it (the clone's executor keeps
	// that parity; see handleManageAction). The four-value schema with the
	// long description is the *harness* side of the same binary
	// (oneharness/internal/tools, file offset 0x4f35ddc), not the CLI's.
	return pi.ToolSchema{
		Name:        "manage_subagents",
		Description: manageSubagentsDescription,
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"Action": pi.JsonObject{
					"type":        "string",
					"enum":        []string{"list", "kill", "kill_all"},
					"description": manageSubagentsActionDescription,
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

// manageSubagentsActionDescription is the reference's own description of the
// Action field.
//
// 【反编译】 agy 1.4.8 — the jsonschema_description tag on the CLI-side
// field reads this sentence verbatim (file offset 0x9404b5a).
const manageSubagentsActionDescription = "'kill' terminates the subagents named in ConversationIds; " +
	"'kill_all' terminates all."

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

	body, err := t.handleManageAction(action, ids, args.effectiveAction())
	if err != nil {
		return piagent.AgentToolResult{}, err
	}
	text := resultStamp(time.Now()) + body
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

func (t *manageSubagentsTool) handleManageAction(action string, ids []string, effectiveAction string) (string, error) {
	switch action {
	case "list", "status":
		return t.list()
	case "list_peers":
		return t.listPeers()
	case "kill":
		return t.kill(ids, effectiveAction)
	case "kill_all":
		return t.killAll()
	case "stop", "pause":
		return t.stop(ids, effectiveAction)
	case "resume", "revive":
		return t.resume(ids, effectiveAction)
	default:
		// 【反编译】 agy 1.4.8 — the reference's own sentence (the literal
		// is at 0x103670e2d) names three actions even though its handler
		// knows the fourth. The reference's text is the reference's text.
		return "", fmt.Errorf("manage_subagents: unknown action %q (must be 'list', 'kill', or 'kill_all')", effectiveAction)
	}
}

// listPeers reports the sibling subagents.
//
// 【反编译】 agy 1.4.8's `handlers.(*ManageSubagentsSubHandler).handleListPeers`
// (0x101db9560) answers with one of two texts:
//
//	You have no parent conversation, so you have no peer subagents:\n[]
//	You have %d active peer subagent(s) (parent %s):\n%s
//
// The first is the answer for a conversation that has no parent — the root
// conversation, whose own subagents are its *children*, not its peers. The
// second lists the subagents of the parent conversation, marshalled as the
// list action marshals them.
func (t *manageSubagentsTool) listPeers() (string, error) {
	parentID := t.parentConversationID()
	if parentID == "" {
		return noPeerSubagentsText, nil
	}
	states := []StateEntry{}
	if t.registry != nil {
		states = t.registry.States()
	}
	encoded, err := json.Marshal(states)
	if err != nil {
		return "", fmt.Errorf("manage_subagents list_peers: failed to marshal peer states: %w", err)
	}
	return fmt.Sprintf("You have %d active peer subagent(s) (parent %s):\n%s",
		len(states), parentID, string(encoded)), nil
}

// noPeerSubagentsText is the reference's answer for a conversation that has
// no parent conversation.
//
// 【反编译】 agy 1.4.8 — the literal is 66 bytes at 0x10365bc95.
const noPeerSubagentsText = "You have no parent conversation, so you have no peer subagents:\n[]"

// parentConversationID returns the parent conversation's id, or "" when the
// agent running the tool has none.
//
// The registry belongs to the conversation that owns it, so its ParentID is
// that conversation's id. The agent running the tool is either that
// conversation — the root, which has no parent — or one of its subagents.
func (t *manageSubagentsTool) parentConversationID() string {
	if t.registry == nil {
		return ""
	}
	parentID := t.registry.ParentID()
	if parentID == "" || t.owner == "" || t.owner == parentID {
		return ""
	}
	return parentID
}

// list reports the live subagents.
//
// The states are marshalled as the reference marshals them: one JSON
// object per subagent, with the type, the role, the conversation id and
// the state.
func (t *manageSubagentsTool) list() (string, error) {
	states := []StateEntry{}
	if t.registry != nil {
		states = t.registry.States()
	}
	encoded, err := json.Marshal(states)
	if err != nil {
		return "", fmt.Errorf("manage_subagents: marshal the subagent states: %w", err)
	}
	return fmt.Sprintf("You have %d active subagent(s):\n%s", len(states), string(encoded)), nil
}

// kill terminates the named subagents.
//
// The reference validates the ids first: a name that is not an active
// subagent answers with its own sentence, and nothing is killed.
//
// 【反编译】 agy 1.4.8 — a `kill` with no ids answers with its own
// sentence (the literal is 116 bytes at 0x103695f3b):
//
//	the %q action requires ConversationIds; run %q to get the ids of the active subagents, or use %q to kill all of them
func (t *manageSubagentsTool) kill(ids []string, effectiveAction string) (string, error) {
	if len(ids) == 0 {
		action := effectiveAction
		if action == "" {
			action = "kill"
		}
		return fmt.Sprintf(
			"the %q action requires ConversationIds; run %q to get the ids of the active subagents, "+
				"or use %q to kill all of them",
			action, "list", "kill_all"), nil
	}
	if t.registry == nil {
		return fmt.Sprintf(
			"conversation %q is not an active subagent of this conversation, so nothing was killed; "+
				"run the %q action to see the active subagents and their ids",
			ids[0], "list"), nil
	}
	children := make([]*Child, 0, len(ids))
	for _, id := range ids {
		child, ok := t.registry.Get(id)
		if !ok {
			return fmt.Sprintf(
				"conversation %q is not an active subagent of this conversation, so nothing was killed; "+
					"run the %q action to see the active subagents and their ids",
				id, "list"), nil
		}
		children = append(children, child)
	}
	roles := make([]string, 0, len(children))
	for _, child := range children {
		role := child.Role
		if role == "" {
			role = child.TypeName
		}
		roles = append(roles, role)
		child.Kill()
	}
	return fmt.Sprintf("Successfully killed %d subagent(s) and their descendants.\nKilled roles: %s",
		len(children), strings.Join(roles, ", ")), nil
}

// killAll terminates every live subagent.
func (t *manageSubagentsTool) killAll() (string, error) {
	if t.registry == nil {
		return "No active subagents to kill.", nil
	}
	children := t.registry.List()
	if len(children) == 0 {
		return "No active subagents to kill.", nil
	}
	roles := make([]string, 0, len(children))
	for _, child := range children {
		role := child.Role
		if role == "" {
			role = child.TypeName
		}
		roles = append(roles, role)
		child.Kill()
	}
	return fmt.Sprintf("Successfully killed %d subagent(s) and their descendants.\nKilled roles: %s",
		len(children), strings.Join(roles, ", ")), nil
}

// stop halts the named subagents or all active subagents if no IDs are given.
func (t *manageSubagentsTool) stop(ids []string, effectiveAction string) (string, error) {
	if t.registry == nil {
		return "No active subagents to stop.", nil
	}
	if len(ids) == 0 {
		children := t.registry.List()
		stopped := 0
		for _, child := range children {
			if child.State() == StateRunning || child.State() == StateIdle {
				child.Stop()
				stopped++
			}
		}
		if stopped == 0 {
			return "No active subagents to stop.", nil
		}
		return fmt.Sprintf("Successfully stopped %d subagent(s).", stopped), nil
	}
	stopped := 0
	for _, id := range ids {
		if child, ok := t.registry.Get(id); ok {
			child.Stop()
			stopped++
		}
	}
	return fmt.Sprintf("Successfully stopped %d subagent(s).", stopped), nil
}

// resume restarts the named subagents or all stopped subagents without losing progress.
func (t *manageSubagentsTool) resume(ids []string, effectiveAction string) (string, error) {
	if t.registry == nil {
		return "No stopped subagents to resume.", nil
	}
	if len(ids) == 0 {
		children := t.registry.List()
		resumed := 0
		for _, child := range children {
			if child.State() == StateStopped {
				if child.Revive(context.Background(), nil) {
					resumed++
				}
			}
		}
		if resumed == 0 {
			return "No stopped subagents to resume.", nil
		}
		return fmt.Sprintf("Successfully resumed %d subagent(s).", resumed), nil
	}
	resumed := 0
	for _, id := range ids {
		if child, ok := t.registry.Get(id); ok {
			if child.Revive(context.Background(), nil) {
				resumed++
			}
		}
	}
	return fmt.Sprintf("Successfully resumed %d subagent(s).", resumed), nil
}
