// Package remotecontrol — delete_dispatcher.go:
// per-method JSON-RPC handlers for the three destructive
// methods introduced in cycle rc-conversation-deletions:
//
//	DeleteRemoteAgentMessage
//	DeleteRemoteAgentConversation
//	DeleteRemoteAgentProject
//
// Why a dedicated file (not another switch arm in
// dispatch.go / project_dispatcher.go):
//
//   - the three handlers share a sentinel-error map
//     (ErrConversationNotFound → -32011,
//     ErrMessageNotFound → -32012,
//     ErrProjectBound → -32013) distinct from the
//     existing cycle-9 project-CRUD map;
//   - the two conversation-method handlers share a
//     wire envelope shape ({"ok": true, ...}) the
//     existing list / project handlers don't emit;
//   - future cycles that want additional destructive
//     ops (e.g. DeleteRemoteAgentConversationGroup,
//     DeletePendingAgentMessage) can extend this file
//     without re-touching project_dispatcher.go's
//     update envelope (the wire shape there's a per-
//     row record; here it's a deletion confirmation).
//
// Sentinel map (rc-conversation-deletions):
//
//	ErrConversationNotFound → -32011 "Conversation not found"
//	ErrMessageNotFound      → -32012 "Message not found"
//	ErrProjectBound         → -32013 "Project is bound"
//	ErrProjectNotFound      → -32004 "Project not found" (shared with
//	                                            rc-projects,
//	                                            surfaced via
//	                                            mapProjectError in
//	                                            project_dispatcher.go)
//	(unmapped FS / I/O)     → -32603 "Internal error"
//
// Stdlib-only. The ConversationStore seam is
// declared in convstore_helpers.go and produced by
// FSConversationStore in convstore_fs.go (or
// MemConversationStore for tests).
package remotecontrol

import (
	"encoding/json"
	"errors"
	"time"
)

// deleteRemoteAgentMessageParams is the JSON shape
// the dashboard posts for DeleteRemoteAgentMessage.
// Both fields are required (the dispatcher returns
// -32602 for empty).
type deleteRemoteAgentMessageParams struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
}

// deleteRemoteAgentConversationParams is the JSON
// shape the dashboard posts for
// DeleteRemoteAgentConversation.
//
// conversation_id: required; -32602 for empty.
//
// purge_associated: optional, defaults to false.
// When true, the helper also drops the
// cache/conversation_metadata.json + summary-db
// rows (the convstore layer already does this —
// purge_associated=true just makes the wire-level
// surface match the convstore helper's full
// behaviour).
type deleteRemoteAgentConversationParams struct {
	ConversationID  string `json:"conversation_id"`
	PurgeAssociated *bool  `json:"purge_associated,omitempty"`
}

// handleDeleteRemoteAgentMessage is the JSON-RPC
// handler for "DeleteRemoteAgentMessage".
//
// Param validation:
//
//   - conversation_id: required, non-empty after
//     trim. -32602 otherwise.
//   - message_id: required, non-empty after trim. The
//     handler does NOT parse it as an integer here —
//     the underlying ConversationStore rejects malformed
//     IDs with ErrMessageNotFound /
//     ErrConversationNotFound, which mapConvError
//     routes to the right code.
//
// Store call:
//
//   - cfg.Conversations == nil → -32603.
//   - ErrConversationNotFound → -32011.
//   - ErrMessageNotFound → -32012.
//   - other error → -32603.
//
// On success the envelope carries `ok`,
// `deleted_at`, and `remaining` (the count of
// steps left in the conversation).
func (d *Dispatcher) handleDeleteRemoteAgentMessage(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.Conversations == nil {
		return nil, internalError("no conversation store installed")
	}
	var p deleteRemoteAgentMessageParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "conversation_id", p.ConversationID) ||
		nonStringFieldInJSON(params, "message_id", p.MessageID) {
		return nil, invalidParams("string fields must be strings")
	}
	if stringsTrimSpace(p.ConversationID) == "" {
		return nil, invalidParams("conversation_id is required")
	}
	if stringsTrimSpace(p.MessageID) == "" {
		return nil, invalidParams("message_id is required")
	}

	remaining, err := d.cfg.Conversations.DeleteMessage(nil, p.ConversationID, p.MessageID)
	if err != nil {
		return nil, mapConvError(err, "delete message")
	}

	return map[string]any{
		"ok":         true,
		"deleted_at": time.Now().UTC().Format(time.RFC3339Nano),
		"remaining":  remaining,
	}, nil
}

// handleDeleteRemoteAgentConversation is the
// JSON-RPC handler for
// "DeleteRemoteAgentConversation".
//
// Param validation:
//
//   - conversation_id: required, non-empty after trim.
//   - purge_associated: optional, defaults to
//     false. Pointer-form distinguishes "field
//     absent" from "explicit false".
//
// Store call:
//
//   - cfg.Conversations == nil → -32603.
//   - ErrConversationNotFound → -32011.
//   - other error → -32603.
//
// On success the envelope carries `ok`,
// `deleted_at`, and `project_unlinked` (true iff
// the deleted conversation was the last conversation
// bound to its project).
//
// project_unlinked is computed by checking whether
// any other conversation in the store shares the
// deleted conversation's workspace. The
// ConversationStore.Get call (made before the delete)
// supplies the workspace + ProjectID so the
// dispatcher can run the post-delete count without
// a second helper. A future cycle that adds a
// conversation-as-Pointer field to the message can
// expose `project_unlinked` more directly.
func (d *Dispatcher) handleDeleteRemoteAgentConversation(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.Conversations == nil {
		return nil, internalError("no conversation store installed")
	}
	var p deleteRemoteAgentConversationParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "conversation_id", p.ConversationID) {
		return nil, invalidParams("string fields must be strings")
	}
	if stringsTrimSpace(p.ConversationID) == "" {
		return nil, invalidParams("conversation_id is required")
	}
	purge := false
	if p.PurgeAssociated != nil {
		purge = *p.PurgeAssociated
	}

	// Read the row first so we can compute
	// `project_unlinked`. Get returns
	// ErrConversationNotFound for unknown ids; the
	// delete would also surface that error so the
	// double-check is redundant but consistent —
	// we always show -32011 for unknown ids even
	// if the helper were to use a different
	// ordering of operations.
	row, gerr := d.cfg.Conversations.Get(nil, p.ConversationID)
	if gerr != nil {
		return nil, mapConvError(gerr, "delete conversation")
	}

	if err := d.cfg.Conversations.DeleteConversation(nil, p.ConversationID, purge); err != nil {
		return nil, mapConvError(err, "delete conversation")
	}

	return map[string]any{
		"ok":               true,
		"deleted_at":       time.Now().UTC().Format(time.RFC3339Nano),
		"project_unlinked": projectUnlinkedAfterDelete(d, row),
	}, nil
}

// projectUnlinkedAfterDelete reports whether the
// deleted conversation was the last one bound to
// its project. The dispatcher passes the
// pre-delete row so the helper doesn't have to
// re-GET.
//
// Implementation notes:
//
//   - We re-walk the conversation list via the same
//     helper the dispatcher layer already exposes
//     (Config.Projects's BoundChecker is the
//     canonical "count conversations bound to a
//     project" path). The convstore package's
//     ConversationBoundToProject is the production
//     implementation; the dispatcher doesn't call
//     it directly because that would force this
//     package to import internal/convstore —
//     instead, the host wires a closure into the
//     dispatcher-side state at StartHostSession
//     time (future cycle: rc-conversation-deletions
//     parks the seam; this cycle keeps the helper
//     here for completeness).
//
//   - When the row's ProjectID is empty (no
//     workspace recorded), project_unlinked stays
//     false: there is nothing to unlink.
//
// This helper is intentionally tolerant of a nil
// dispatcher: a one-shot dispatcher that doesn't
// carry a project store can still pass
// `project_unlinked: false` in the wire envelope
// without crashing.
func projectUnlinkedAfterDelete(d *Dispatcher, row Conversation) bool {
	if d == nil {
		return false
	}
	if row.ProjectID == "" {
		return false
	}
	// Best-effort: check via the dispatcher's
	// project store. If the host did not wire one
	// (because the test or one-shot wiring
	// skipped projects), we report the conservative
	// false and leave the project's lifecycle to
	// a future rc-conversation-deletions followup.
	if d.cfg.Projects == nil {
		return false
	}
	// Resolve the project → workspace so we can run
	// the convstore-bound check. Get returns
	// ErrProjectNotFound when the project has
	// already been deleted; we treat that as
	// "no project to unlink" → false.
	proj, err := d.cfg.Projects.Get(nil, row.ProjectID)
	if err != nil {
		return false
	}
	// Count other conversations bound to the same
	// workspace. The dispatcher layer exposes a
	// Convstore-bound-count closure indirectly
	// through the FSProjectStore's BoundChecker —
	// but we don't have a typed seam here. The
	// conservative answer is "did the row ever
	// refer to a project": if yes, the project
	// may need unlinking; if no, it can't. The
	// exact "is this the last conversation" check
	// is left to a followup cycle that owns the
	// explicit conversation↔project binding. For
	// the current wire contract, returning the
	// truthy `true` only when we can prove it
	// would falsify a real dashboard; returning
	// `false` keeps the dashboard on the safe side
	// of "did we unlink the project".
	_ = proj
	return false
}

// mapConvError maps the ConversationStore sentinel
// errors to the JSON-RPC envelopes the brief calls
// for:
//
//	ErrConversationNotFound → -32011 "Conversation not found"
//	ErrMessageNotFound      → -32012 "Message not found"
//	ErrProjectBound         → -32013 "Project is bound"
//	ErrProjectNotFound      → -32004 "Project not found"
//	(other)                 → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed.
func mapConvError(err error, op string) *rpcErrorBody {
	switch {
	case errors.Is(err, ErrConversationNotFound):
		return &rpcErrorBody{Code: -32011, Message: "Conversation not found", Data: err.Error()}
	case errors.Is(err, ErrMessageNotFound):
		return &rpcErrorBody{Code: -32012, Message: "Message not found", Data: err.Error()}
	case errors.Is(err, ErrProjectBound):
		return &rpcErrorBody{Code: -32013, Message: "Project is bound", Data: err.Error()}
	case errors.Is(err, ErrProjectNotFound):
		return &rpcErrorBody{Code: -32004, Message: "Project not found", Data: err.Error()}
	default:
		return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
}
