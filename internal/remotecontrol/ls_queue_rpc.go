// Package remotecontrol — ls_queue_rpc.go: the RPCs the reference
// puts behind the queue.
//
// The Web UI manages the messages it is waiting for: a card per
// queued message, with "Edit" and "Send now" on it, and a "Send all
// (N)" button over the list. Those buttons are the reference's
// LanguageServerService methods:
//
//   - SendAllQueuedMessages — send every message the conversation is
//     holding, now.
//   - DeleteQueuedUserInputStep — drop one of them, by its position.
//   - DeleteAgentMessage — drop one of them, by its id.
//
// 【反编译】 exa.language_server_pb.LanguageServerService carries all
// three; the requests are:
//
//	SendAllQueuedMessagesRequest: cascade_id = 2, cascade_config = 3
//	DeleteQueuedUserInputStepRequest: cascade_id = 1, step_index = 2 (int32)
//	DeleteAgentMessageRequest: message_id = 1, recipient = 2
//
// 【一手】 the Web UI's own bundle calls the first one from the "Send
// all (N)" button:
//
//	function x6b(...)  … A.createElement(Pz,{variant:"primary",size:"sm",onClick:()=>{e({type:5,cascadeId:c})},className:"mt-4"},`Send all (${g.length})`)
//
// and the *card's* delete action calls the third one, with the
// pending message's id and the conversation it waits in — see
// lsDeleteAgentMessageParams below. The second one
// (DeleteQueuedUserInputStep) is in the bundle's client as well, but
// the delete action does not reach for it.
package remotecontrol

import (
	"encoding/json"
	"log"
	"strings"
)

// lsSendAllQueuedMessagesParams is the SendAllQueuedMessages request
// shape (proto-JSON, camelCase).
type lsSendAllQueuedMessagesParams struct {
	CascadeID string `json:"cascadeId"`
	// CascadeConfig carries the model the Web UI's picker chose, the
	// same way the message request carries it.
	CascadeConfig lsCascadeConfig `json:"cascadeConfig"`
}

// lsDeleteQueuedUserInputStepParams is the DeleteQueuedUserInputStep
// request shape (proto-JSON, camelCase).
type lsDeleteQueuedUserInputStepParams struct {
	CascadeID string `json:"cascadeId"`
	// StepIndex is the position of the queued message to drop.
	StepIndex int `json:"stepIndex"`
}

// lsDeleteAgentMessageParams is the LanguageServerService's
// DeleteAgentMessage request shape (proto-JSON, camelCase).
//
// 【反编译】 exa.language_server_pb.DeleteAgentMessageRequest:
// message_id = 1, recipient = 2. The response is empty.
//
// This is the RPC the Web UI's queued card actually calls: its delete
// action hands the *id* of the pending message and the conversation it
// waits in.
//
//	function w6b(a,b){var {deleteAgentMessage:c}=PB(),…
//	  …r(await p(c({messageId:m,recipient:a})))…}
//
// where `a` is the cascade id and `m` is the pending message's id —
// the `id` the pendingAgentMessages update carries.
//
// It is *not* the clone's own DeleteRemoteAgentMessage: that one is the
// dashboard's JSON-RPC dialect and it names the conversation and the
// stored message instead (conversation_id + message_id). The alias table
// (reference_surface.go) maps the reference's name onto the clone's
// legacy one, and the legacy handler then rejects the IDE's shape with
// "conversation_id is required" — the queued message the user deleted
// stayed in the queue.
type lsDeleteAgentMessageParams struct {
	MessageID string `json:"messageId"`
	Recipient string `json:"recipient"`
	// ConversationID is the *other* dialect's shape: the reference
	// CLI's own DeleteAgentMessage
	// (google.gca.aicode.v1main.DeleteAgentMessageRequest) names the
	// conversation and the stored message — conversation_id = 1,
	// message_id = 2 — and the clone's alias table maps that name
	// onto its legacy DeleteRemoteAgentMessage.
	//
	// 【反编译】 out/local-macos/protos/remote_agent_proxy_1.4.4.txt,
	// the FileDescriptorProto recovered from the agy 1.4.4 binary.
	ConversationID string `json:"conversation_id"`
	// ConversationIDCamel is the same field in proto-JSON spelling.
	ConversationIDCamel string `json:"conversationId"`
}

// legacyShape reports whether the request names a conversation
// instead of a recipient — the reference CLI's dialect, which the
// clone's own DeleteRemoteAgentMessage owns.
func (p lsDeleteAgentMessageParams) legacyShape() bool {
	return strings.TrimSpace(p.ConversationID) != "" || strings.TrimSpace(p.ConversationIDCamel) != ""
}

// handleLSSendAllQueuedMessages sends the messages the conversation is
// holding, now.
//
// The clone's queue already drains itself when the turn in flight ends
// — that is the reference's own setting, "Queue until after the current
// turn." — so the button's job here is to take the turn away from the
// work in flight: the queued messages run next.
//
// The reference says the same thing in its own words when it cannot
// honour a send-now request: "Send-now ignored: the active turn
// cannot take the %d queued input(s) now". The clone has no such
// refusal: the turn in flight is cancelled and the queue runs.
func (d *Dispatcher) handleLSSendAllQueuedMessages(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req lsSendAllQueuedMessagesParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}, true
	}
	traj := d.trajectories.Get(c.ID)
	if traj == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "no trajectory store"}, true
	}
	if n := len(traj.Pending()); n > 0 {
		log.Printf("remotecontrol: conversation %s: sending %d queued message(s) now", c.ID, n)
	}
	traj.Interrupt()
	return map[string]any{}, nil, true
}

// handleLSDeleteQueuedUserInputStep drops one of the messages the
// conversation is holding.
//
// The index is the position in the queue — the same position the
// pendingAgentMessages update's indices carry, so the card the Web UI
// renders and the step the user deleted are the same message.
func (d *Dispatcher) handleLSDeleteQueuedUserInputStep(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req lsDeleteQueuedUserInputStepParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}, true
	}
	traj := d.trajectories.Get(c.ID)
	if traj == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "no trajectory store"}, true
	}
	if !traj.DropPending(req.StepIndex) {
		log.Printf("remotecontrol: conversation %s: no queued message at index %d", c.ID, req.StepIndex)
	}
	return map[string]any{}, nil, true
}

// handleLSDeleteAgentMessage drops the queued message the Web UI's
// queued card names.
//
// The request carries the pending message's id and the conversation it
// waits in — the reference's own DeleteAgentMessage shape. The message
// is looked up by id: the queue's *index* is not stable (it moves when
// another message is queued or dropped), the id is.
//
// A message that is not in the queue anymore — it ran while the user
// was clicking — is not an error: the delete is idempotent, and the
// reference's own answer is empty.
//
// The reference CLI's own DeleteAgentMessage names a conversation
// instead of a recipient. That dialect belongs to the clone's legacy
// DeleteRemoteAgentMessage, so the request is handed back to the
// router (ok=false) and the alias table takes it there.
func (d *Dispatcher) handleLSDeleteAgentMessage(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req lsDeleteAgentMessageParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	recipient := strings.TrimSpace(req.Recipient)
	if recipient == "" {
		if req.legacyShape() {
			return nil, nil, false
		}
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "recipient is required"}, true
	}
	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "messageId is required"}, true
	}
	c, ok := d.cascades.Get(recipient)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: recipient}, true
	}
	traj := d.trajectories.Get(c.ID)
	if traj == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "no trajectory store"}, true
	}
	if !traj.DropPendingByID(messageID) {
		log.Printf("remotecontrol: conversation %s: no queued message with the id %q", c.ID, messageID)
	}
	return map[string]any{}, nil, true
}
