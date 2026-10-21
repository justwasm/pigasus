// Package remotecontrol —
// conversation_config_dispatcher.go: the per-method
// JSON-RPC handler for the single
// UpsertRemoteAgentConversationConfig method
// introduced in cycle rc-conversation-config.
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the handler shares the
// ConversationConfigStore seam + the
// ValidateConversationConfig validator + a small
// sentinel-error map
// (ErrConversationConfigPersistenceFailed → -32603),
// and the mutation needs to emit a
// conversation_config/changed broadcast
// notification. Putting the handler here, next to
// the type definitions (conversation_config.go) and
// the store (conversation_config_store.go), keeps
// the per-method body focused on the JSON-RPC
// envelope shape rather than the dispatcher's
// switch table.
//
// Sentinel map:
//
//	(other / I/O)              → -32603 "Internal error"
//	empty / non-string conversation_id
//	                          → -32602 "Invalid params"
//	ValidateConversationConfig failure
//	                          → -32602 "Invalid params"
//	missing / wrong-type config
//	                          → -32602 "Invalid params"
//
// -32602 ("Invalid params") is shared with every
// other method's "bad input" envelope and is
// emitted directly from the handler, not via the
// mapConversationConfigError helper.
//
// Cycle rc-conversation-config also adds the
// "broadcast on change" path: every successful
// Upsert emits a `conversation_config/changed`
// notification to every open ws connection so a
// dashboard that subscribed to the channel sees the
// new state. The broadcast uses the existing
// (*Dispatcher).BroadcastNotify seam added in cycle
// rc-permissions-grants — the handler here doesn't
// have to know which transport is registered, it
// just calls BroadcastNotify and the registered
// broadcaster fans the frame out. In-process
// callers without a transport fall back to a no-op
// broadcast (the mutation still lands in the store;
// the UpsertRemoteAgentConversationConfig handler
// returns the post-mutation state in the response
// envelope so polling is always possible).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
)

// upsertRemoteAgentConversationConfigParams is the
// JSON shape the dashboard posts for
// UpsertRemoteAgentConversationConfig.
//
// conversation_id is required, non-empty after trim.
// config is required (the brief pins it as a JSON
// object — not null). Missing / empty / wrong-type
// values map to -32602 before the store sees them.
type upsertRemoteAgentConversationConfigParams struct {
	ConversationID string             `json:"conversation_id"`
	Config         ConversationConfig `json:"config"`
}

// handleUpsertRemoteAgentConversationConfig is
// the JSON-RPC handler for
// "UpsertRemoteAgentConversationConfig". Cycle
// rc-conversation-config.
//
// Param validation:
//
//   - conversation_id: required, non-empty string.
//     Missing / empty / non-string → -32602 (the
//     "dashboard bug" envelope). The store's
//     defence-in-depth normaliseConversationID
//     guard catches whitespace-only ids here too.
//
//   - config: required, non-null object. The brief
//     pins config as a JSON object — the handler
//     rejects {"conversation_id":"...",} without
//     a config key as -32602. Missing config
//     field → -32602.
//
// Validation chain (after the param shape check):
//
//   - ValidateConversationConfig walks every
//     closed-set enum / bounded-range field. A
//     failure here lands as -32602 with the
//     validator's verbatim message in `data`.
//     The handler short-circuits before the
//     store call so a dashboard with bad inputs
//     never triggers an on-disk write.
//
// Store call:
//
//   - cfg.ConversationConfigStore == nil → -32603
//     (no store installed; production wiring in
//     cmd/wagyu/remote_control.go always
//     installs one — nil means the dispatcher
//     was constructed in a test that didn't
//     bother).
//   - ErrConversationConfigPersistenceFailed (the
//     store refuses an empty conversation_id
//     post-validation) → -32603.
//   - other error → -32603.
//
// On success the handler emits a
// `conversation_config/changed` notification to
// every open ws connection so a dashboard watching
// the channel re-renders without polling. The
// response envelope is the brief's
// `{ok:true, conversation_id, created, config,
// updated_at}` shape — subscribers get the same
// shape via the broadcast notification.
//
// The store stamps UpdatedAt server-side so a
// dashboard that posts a pre-populated value gets
// the value overwritten with the server's clock
// (matches the rc-fastpush-toggle / rc-resources
// precedent). The handler echoes the post-stamp
// value in the response envelope so polling
// callers see the same timestamp a notification
// subscriber would.
func (d *Dispatcher) handleUpsertRemoteAgentConversationConfig(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.ConversationConfigStore == nil {
		return nil, internalError("no conversation-config store installed")
	}
	p, perr := decodeUpsertConversationConfigParams(params)
	if perr != nil {
		return nil, perr
	}
	created, err := d.cfg.ConversationConfigStore.Upsert(nil, p.ConversationID, p.Config)
	if err != nil {
		return nil, mapConversationConfigError(err, "upsert conversation config")
	}
	// Post-stamp fetch + zero-fallback so the wire
	// envelope carries the server's UpdatedAt, not
	// whatever the dashboard posted. Matches the
	// rc-resources Bind path that uses the
	// post-stamp shape.
	post, _ := d.cfg.ConversationConfigStore.Get(nil, p.ConversationID)
	if post.UpdatedAt.IsZero() {
		post = p.Config
	}
	resp := conversationConfigChangedToWire(p.ConversationID, created, post)
	d.broadcastConversationConfigChanged(resp)
	return resp, nil
}

// broadcastConversationConfigChanged fans a
// `conversation_config/changed` notification out to
// every registered transport's open connection via
// the BroadcastNotify seam. Pure fanout helper —
// extracted from
// handleUpsertRemoteAgentConversationConfig to keep
// the handler body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func (d *Dispatcher) broadcastConversationConfigChanged(params map[string]any) {
	if d == nil {
		return
	}
	d.BroadcastNotify(context.Background(), NotifConversationConfigChangedMethod, params)
}

// nonNullObjectInJSON reports whether the
// supplied params block has a non-null object at
// field. Mirrors nonStringFieldInJSON's shape but
// is stricter: the field must be present (per the
// dispatcher's "params is JSON object" assumption)
// AND parseable as a JSON object ({...}). A
// missing / null / non-object value returns false.
//
// The dispatcher uses this to enforce the brief's
// "config is a JSON object, not null" rule — a
// dashboard that posts {"conversation_id":"..."}
// without a config key (or with config:null) lands
// as -32602 "config is required" before any
// validation runs.
func nonNullObjectInJSON(raw json.RawMessage, field string) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	v, ok := m[field]
	if !ok {
		return false
	}
	if len(v) == 0 {
		return false
	}
	// "null" → false (treat as absent).
	if string(v) == "null" {
		return false
	}
	// Must parse as a JSON object — that's the
	// brief's "config is a JSON object" rule.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(v, &probe); err != nil {
		return false
	}
	return true
}
