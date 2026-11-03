// Package remotecontrol —
// conversation_config_dispatcher.go: the per-method
// JSON-RPC handler for the single
// UpsertRemoteAgentConversationConfig method
// introduced in cycle rc-conversation-config.
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the handler shares the
// Store seam + the
// Validate validator + a small
// sentinel-error map
// (ErrPersistenceFailed → -32603),
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
//	Validate failure
//	                          → -32602 "Invalid params"
//	missing / wrong-type config
//	                          → -32602 "Invalid params"
//
// -32602 ("Invalid params") is shared with every
// other method's "bad input" envelope and is
// emitted directly from the handler, not via the
// mapError helper.
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
package convconfig

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// upsertParams is the
// JSON shape the dashboard posts for
// UpsertRemoteAgentConversationConfig.
//
// conversation_id is required, non-empty after trim.
// config is required (the brief pins it as a JSON
// object — not null). Missing / empty / wrong-type
// values map to -32602 before the store sees them.
type upsertParams struct {
	ConversationID string `json:"conversation_id"`
	Config         Config `json:"config"`
}

// HandleUpsert is
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
//   - Validate walks every
//     closed-set enum / bounded-range field. A
//     failure here lands as -32602 with the
//     validator's verbatim message in `data`.
//     The handler short-circuits before the
//     store call so a dashboard with bad inputs
//     never triggers an on-disk write.
//
// Store call:
//
//   - cfg.Store == nil → -32603
//     (no store installed; production wiring in
//     cmd/wagyu/remote_control.go always
//     installs one — nil means the dispatcher
//     was constructed in a test that didn't
//     bother).
//   - ErrPersistenceFailed (the
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
func HandleUpsert(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no conversation-config store installed")
	}
	p, perr := decodeUpsertParams(params)
	if perr != nil {
		return nil, perr
	}
	created, err := s.Store.Upsert(nil, p.ConversationID, p.Config)
	if err != nil {
		return nil, mapError(err, "upsert conversation config")
	}
	// Post-stamp fetch + zero-fallback so the wire
	// envelope carries the server's UpdatedAt, not
	// whatever the dashboard posted. Matches the
	// rc-resources Bind path that uses the
	// post-stamp shape.
	post, _ := s.Store.Get(nil, p.ConversationID)
	if post.UpdatedAt.IsZero() {
		post = p.Config
	}
	resp := changedToWire(p.ConversationID, created, post)
	broadcastChanged(s, resp)
	return resp, nil
}

// broadcastChanged fans a
// `conversation_config/changed` notification out to
// every registered transport's open connection via
// the Seams.Broadcast seam. Pure fanout helper —
// extracted from HandleUpsert to keep the handler
// body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func broadcastChanged(s Seams, params map[string]any) {
	if s.Broadcast == nil {
		return
	}
	s.Broadcast(context.Background(), NotifChangedMethod, params)
}
