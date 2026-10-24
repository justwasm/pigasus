package subagenttools

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The metadata keys the rehydrators write and the appliers read.
//
// 【反编译】 the reference's `rehydrateDefineSubagent` (LS 0x100f3e870),
// `rehydrateManageSubagents` (LS 0x100f3e430) and `rehydrateSendMessage`
// (LS 0x100f3e6f0) build a `map[string]string` and attach it to the step
// they rebuild (`rehydratedGenericWithMetadata`, LS 0x100f3dfd0). The
// reference writes both spellings of the text fields — the canonical
// snake_case and the capitalised native-payload name — and both
// spellings of the three tool grants.
//
// 【一手】 the reference stores `define_subagent` as
// {"Name","Description","SystemPrompt","EnableWriteTools",…},
// `manage_subagents` as {"Action","ConversationIds"} and `send_message`
// as {"Recipient","Message"} — the capitalised native-payload names.
const (
	metaName              = "name"
	metaNameCap           = "Name"
	metaDescription       = "description"
	metaDescriptionCap    = "Description"
	metaSystemPrompt      = "system_prompt"
	metaSystemPromptCap   = "SystemPrompt"
	metaEnableMCPTools    = "EnableMCPTools"
	metaEnableMCPToolsAlt = "enable_mcp_tools"
	metaEnableWriteTools  = "EnableWriteTools"
	// metaEnableWriteToolsAlt is the snake_case spelling of the write
	// grant.
	metaEnableWriteToolsAlt = "enable_write_tools"
	metaEnableSubagentTools = "EnableSubagentTools"
	// metaEnableSubagentToolsAlt is the snake_case spelling of the
	// subagent grant.
	metaEnableSubagentToolsAlt = "enable_subagent_tools"

	metaAction                = "action"
	metaConversationIDs       = "conversation_ids"
	metaKilledConversationIDs = "killed_conversation_ids"
	metaRecipient             = "recipient"
	metaRecipientName         = "recipient_name"
	// metaMessage is the message content, under the name the reference
	// stores it under.
	metaMessage = "Message"
)

// RehydrateDefineSubagent rebuilds the state delta of a define_subagent
// call from its stored arguments.
//
// The delta carries the definition's name, description and system
// prompt plus the three tool grants. The clone's define_subagent
// handler reads the name and the system prompt (and ignores the
// description), so those two are what the applier restores; the
// description and the grants travel with the delta so a later cycle
// that implements them does not have to re-read the trajectory.
//
// The arguments are parsed with the *same* struct the live handler
// uses, so a rehydrated definition and a live one agree by
// construction.
func RehydrateDefineSubagent(args json.RawMessage) (map[string]string, bool) {
	var a defineArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, false
	}
	name := firstNonEmpty(a.Name, a.NameCap)
	prompt := firstNonEmpty(a.SystemPrompt, a.SystemCap)
	if name == "" || prompt == "" {
		return nil, false
	}
	meta := map[string]string{
		metaName:            name,
		metaNameCap:         name,
		metaSystemPrompt:    prompt,
		metaSystemPromptCap: prompt,
	}
	if desc := firstNonEmpty(a.Description, a.DescriptionCap); desc != "" {
		meta[metaDescription] = desc
		meta[metaDescriptionCap] = desc
	}
	setBool(meta, metaEnableMCPTools, metaEnableMCPToolsAlt, a.EnableMCPTools, a.EnableMCPToolsAlt)
	setBool(meta, metaEnableWriteTools, metaEnableWriteToolsAlt, a.EnableWriteTools, a.EnableWriteToolsAlt)
	setBool(meta, metaEnableSubagentTools, metaEnableSubagentToolsAlt, a.EnableSubagentTools, a.EnableSubagentToolsAlt)
	return meta, true
}

// RehydrateManageSubagents rebuilds the state delta of a
// manage_subagents call from its stored arguments.
//
// The delta carries the action, the conversation ids the action named
// and — for a kill — the ids that were killed.
//
// 【反编译】 the reference reads the killed ids from the tool call's
// *result* metadata (`shared.ManageSubagentsResultMetadata`, LS
// 0x100e6fb70 / 0x100e6fcf0), not from the arguments. The clone has no
// result metadata, so it derives them from the kill action's arguments:
// a successful kill names the ids it killed.
func RehydrateManageSubagents(args json.RawMessage) (map[string]string, bool) {
	var a manageSubagentsArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, false
	}
	action := strings.ToLower(firstNonEmpty(a.Action, a.ActionCap))
	if action == "" {
		return nil, false
	}
	meta := map[string]string{metaAction: action}
	ids := a.ConversationIds
	if len(ids) == 0 {
		ids = a.ConversationIdsCap
	}
	if len(ids) == 0 && strings.TrimSpace(a.SubagentID) != "" {
		ids = []string{strings.TrimSpace(a.SubagentID)}
	}
	if len(ids) > 0 {
		meta[metaConversationIDs] = strings.Join(ids, ",")
		if action == "kill" || action == "kill_all" {
			killed, err := json.Marshal(ids)
			if err == nil {
				meta[metaKilledConversationIDs] = string(killed)
			}
		}
	}
	return meta, true
}

// RehydrateSendMessage rebuilds the state delta of a send_message call
// from its stored arguments.
//
// The delta carries the recipient and the message.
//
// 【反编译】 the reference's `rehydrateSendMessage` (LS 0x100f3e6f0)
// writes `recipient` and `recipient_name`; the recipient's name is a
// field of the reference's native payload that the clone's arguments do
// not carry, so the clone writes the recipient and the message only.
//
// The clone's delegations are synchronous, so there is never a live
// recipient to deliver a pending message to — the applier has nothing
// to restore.
func RehydrateSendMessage(args json.RawMessage) (map[string]string, bool) {
	var a sendMessageArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, false
	}
	recipient := firstNonEmpty(a.SubagentID, a.Recipient, a.RecipientCap)
	if recipient == "" {
		return nil, false
	}
	meta := map[string]string{metaRecipient: recipient}
	if msg := firstNonEmpty(a.Message, a.MessageCap); msg != "" {
		meta[metaMessage] = msg
	}
	return meta, true
}

// RestoreDefinition writes a rehydrated define_subagent delta back
// into the store.
//
// The definition comes back exactly as the live handler stored it: the
// name is the key, and the system prompt is both the prompt and the
// system prompt of the invocation (that is what the live handler does
// — see define.go).
func (d *Store) RestoreDefinition(meta map[string]string) bool {
	name := strings.TrimSpace(meta[metaName])
	if name == "" {
		name = strings.TrimSpace(meta[metaNameCap])
	}
	prompt := meta[metaSystemPrompt]
	if prompt == "" {
		prompt = meta[metaSystemPromptCap]
	}
	if name == "" || prompt == "" {
		return false
	}
	d.Put(name, Invocation{Prompt: prompt, SystemPrompt: prompt})
	return true
}

// RestoreManageSubagents writes a rehydrated manage_subagents delta
// back into the store: the killed conversation ids are recorded.
//
// The clone has no live subagents — a delegation is synchronous, so no
// subagent is alive while the parent runs a tool — so there is nothing
// to resurrect. What survives the resume is the record of what was
// killed, which is what the reference keeps too
// (`utils.killedSubagentIDs`, LS 0x10140a900).
func (d *Store) RestoreManageSubagents(meta map[string]string) bool {
	killed := meta[metaKilledConversationIDs]
	if killed == "" {
		return false
	}
	var ids []string
	if err := json.Unmarshal([]byte(killed), &ids); err != nil {
		return false
	}
	for _, id := range ids {
		d.Kill(id)
	}
	return len(ids) > 0
}

// RestoreSendMessage has nothing to restore.
//
// The reference rehydrates a send_message call into a *pending*
// delivery: its delegations are asynchronous, so a message sent to a
// subagent that has not answered yet is still queued when the
// conversation is resumed. The clone's delegations are synchronous —
// the subagent's answer is the tool's result — so there is no queue
// and no live recipient. The delta is still produced, so the pass
// reports the same shape as the reference's.
func (d *Store) RestoreSendMessage(map[string]string) bool { return false }

// setBool writes a boolean grant under both spellings, and only when
// the argument set it.
func setBool(meta map[string]string, key, alt string, values ...*bool) {
	for _, v := range values {
		if v == nil {
			continue
		}
		meta[key] = strconv.FormatBool(*v)
		meta[alt] = strconv.FormatBool(*v)
		return
	}
}
