// Package fastpush — dispatcher.go: the per-method
// JSON-RPC handler for the single
// SetRemoteAgentFastpushEnabled method introduced in
// cycle rc-fastpush-toggle.
//
// Why a dedicated file (not another switch arm in the
// dispatcher's route table): the handler shares the
// Store seam + a small sentinel-error map
// (ErrCascadeNotFound → -32004 "Cascade not found",
// reusing the wire's own rpc.ErrCodeCascadeNotFound
// constant), and the mutation needs to emit a
// fastpush/setting_changed broadcast notification.
// Putting the handler here, next to the type
// definitions (fastpush.go) and the store (store.go),
// keeps the per-method body focused on the JSON-RPC
// envelope shape rather than the dispatcher's switch
// table.
//
// Sentinel map:
//
//	ErrCascadeNotFound → -32004 "Cascade not found"
//	(other / I/O)              → -32603 "Internal error"
//	empty / non-string cascade_id
//	                          → -32602 "Invalid params"
//	missing / wrong-type enabled
//	                          → -32602 "Invalid params"
//
// The -32004 code is reused (not brand-new) because
// an unknown cascade_id on a fastpush toggle is the
// same lifecycle error SendRemoteAgentCascadeMessage
// / CancelRemoteAgentCascade / HandleRemoteAgentCascadeUserInteraction
// surfaces; sharing the code keeps dashboard error
// filters uniform. -32602 ("Invalid params") is
// shared with every other method's "bad input"
// envelope.
//
// The code table itself lives in the sibling package
// rpc — the wire's error codes are the envelope's
// fact, not this domain's.
//
// Cycle rc-fastpush-toggle also adds the "broadcast on
// change" path: every successful SetEnabled emits a
// `fastpush/setting_changed` notification to every
// open ws connection so a dashboard that subscribed
// to the channel sees the new state. The broadcast
// uses the Seams.Broadcast callback the host
// installs — the handler here doesn't have to know
// which transport is registered, it just calls
// Broadcast and the registered broadcaster fans the
// frame out.
// In-process callers without a transport fall back to
// a no-op broadcast (the mutation still lands in the
// store; the SetRemoteAgentFastpushEnabled handler
// returns the post-mutation setting in the response
// envelope so polling is always possible).
//
// Stdlib-only. No third-party deps.
package fastpush

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// setRemoteAgentFastpushEnabledParams is the JSON
// shape the dashboard posts for
// SetRemoteAgentFastpushEnabled. cascade_id is a
// required string; enabled is a required bool.
// Missing / empty / wrong-type values map to -32602
// before the store sees them.
type setRemoteAgentFastpushEnabledParams struct {
	CascadeID string `json:"cascade_id"`
	Enabled   *bool  `json:"enabled"`
}

// HandleSetEnabled is the JSON-RPC handler for
// "SetRemoteAgentFastpushEnabled". Cycle
// rc-fastpush-toggle.
//
// Param validation:
//
//   - cascade_id: required, non-empty string.
//     Missing / empty / non-string → -32602
//     (the "dashboard bug" envelope). The store's
//     defence-in-depth normaliseCascadeID guard
//     catches whitespace-only ids here too.
//
//   - enabled: required, bool. Missing or wrong
//     type → -32602 "Invalid params" with
//     "enabled is required". Today the dashboard
//     can send either true or false; the handler
//     upserts the row unconditionally — flipping
//     the toggle off is a legitimate state.
//
// Cascade resolution (after validation):
//
//   - look up via dispatcher.cascades.Get; missing →
//     -32004 "Cascade not found" (matches
//     SendRemoteAgentCascadeMessage's envelope);
//   - the cascade's per-cascade Fastpush flag is
//     flipped via (*Cascade).SetFastpush AFTER the
//     store mutation lands, so the in-memory flag
//     and the on-disk row stay in lockstep.
//
// Store call:
//
//   - s.Store == nil → -32603 (the
//     nil-fabrication in NewDispatcher means this
//     only trips a dispatcher that explicitly
//     cleared the field post-construction).
//   - ErrCascadeNotFound (the store
//     refuses an unknown id) → -32004 "Cascade not
//     found".
//   - other error → -32603.
//
// On success the handler emits a
// `fastpush/setting_changed` notification to every
// open ws connection so a dashboard watching the
// channel re-renders without polling. The response
// envelope is the brief's lean
// `{ok:true, cascade_id, enabled, channel,
// push_delivery, updated_at}` shape — subscribers
// get the same shape via the broadcast notification.
//
// The wire envelope always carries
// `push_delivery:"deferred"` (the
// PushDeliveryDeferred constant) — an
// explicit sentinel so dashboards do not assume an
// actual delivery path is wired. The future
// rc-fastpush-channel cycle will flip this to a
// "live" sentinel once a real transport lands.
func HandleSetEnabled(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no fastpush store installed")
	}
	cascadeID, enabled, perr := decodeFastpushParams(params)
	if perr != nil {
		return nil, perr
	}
	if s.Cascades == nil {
		return nil, rpc.InternalError("registry not initialised")
	}
	if _, ok := s.Cascades(cascadeID); !ok {
		return nil, &rpc.ErrorBody{Code: rpc.ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}
	}
	// Store mutation: upsert the row for cascadeID
	// with the supplied enabled value. The store
	// stamps Channel / PushDelivery / UpdatedAt
	// with the cycle's sentinel values.
	setting, err := s.Store.SetEnabled(nil, cascadeID, enabled)
	if err != nil {
		return nil, mapFastpushError(err, "set fastpush enabled")
	}
	// Cascade in-memory flip + broadcast happen in
	// applyFastpushSetting; matches the rc-plugins /
	// rc-triggers pattern so a dashboard's response
	// handler can rely on the broadcast having
	// already been enqueued.
	return applySetting(s, setting), nil
}

// broadcastSettingChanged fans a
// fastpush/setting_changed notification out to every
// registered transport's open connection.
//
// Why a domain-side helper rather than a
// transport-side one: the single fastpush handler
// lives on the domain, and the broadcaster
// (Seams.Broadcast) is the seam a transport installs
// to deliver "frame to every open conn" without the
// domain knowing the concrete transport type.
//
// Without a registered broadcaster the method is a
// no-op — in-process dispatchers and the Unix-socket
// transport have no notion of "every open
// connection". The mutation still landed in the
// store; the SetRemoteAgentFastpushEnabled handler
// echoes the post-state in the response envelope, so
// polling is always possible.
func broadcastSettingChanged(s Seams, setting Setting) {
	if s.Broadcast == nil {
		return
	}
	params := fastpushSettingToWire(setting)
	s.Broadcast(context.Background(), NotifSettingChangedMethod, params)
}

// applySetting performs the post-store-mutation
// side effects: flips the cascade's in-memory fastpush
// flag and broadcasts a `fastpush/setting_changed`
// notification, then returns the response envelope. The
// single-handler fastpath keeps the handler body in
// dispatcher.go under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func applySetting(s Seams, setting Setting) map[string]any {
	if s.Cascades != nil {
		if c, ok := s.Cascades(setting.CascadeID); ok && c != nil {
			// Flip the cascade's per-cascade fastpush
			// flag AFTER the store mutation lands,
			// so the in-memory flag and the
			// on-disk row stay in lockstep. The
			// future rc-fastpush-channel cycle's
			// delivery path reads this via
			// IsFastpushEnabled() to decide whether
			// to route a notification through the
			// fast-push transport.
			c.SetFastpush(setting.Enabled)
		}
	}
	broadcastSettingChanged(s, setting)
	// Return the brief's lean envelope. The
	// response carries the post-mutation state
	// verbatim (the same shape the broadcast
	// notification emits) so polling is always
	// possible — the broadcaster may not be
	// registered on every transport (Unix-socket
	// doesn't have one).
	return fastpushSettingToWire(setting)
}
