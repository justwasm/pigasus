// Package remotecontrol — fastpush_dispatcher.go: the
// per-method JSON-RPC handler for the single
// SetRemoteAgentFastpushEnabled method introduced in
// cycle rc-fastpush-toggle.
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the handler shares the FastpushStore
// seam + a small sentinel-error map
// (ErrFastpushCascadeNotFound → -32004 "Cascade not
// found", reusing the dispatcher's existing
// ErrCodeCascadeNotFound constant), and the mutation
// needs to emit a fastpush/setting_changed broadcast
// notification. Putting the handler here, next to the
// type definitions (fastpush.go) and the store
// (fastpush_store.go), keeps the per-method body
// focused on the JSON-RPC envelope shape rather than
// the dispatcher's switch table.
//
// Sentinel map:
//
//	ErrFastpushCascadeNotFound → -32004 "Cascade not found"
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
// Cycle rc-fastpush-toggle also adds the "broadcast on
// change" path: every successful SetEnabled emits a
// `fastpush/setting_changed` notification to every
// open ws connection so a dashboard that subscribed
// to the channel sees the new state. The broadcast
// uses the existing (*Dispatcher).BroadcastNotify
// seam added in cycle rc-permissions-grants — the
// handler here doesn't have to know which transport is
// registered, it just calls BroadcastNotify and the
// registered broadcaster fans the frame out.
// In-process callers without a transport fall back to
// a no-op broadcast (the mutation still lands in the
// store; the SetRemoteAgentFastpushEnabled handler
// returns the post-mutation setting in the response
// envelope so polling is always possible).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
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

// handleSetRemoteAgentFastpushEnabled is the JSON-RPC
// handler for "SetRemoteAgentFastpushEnabled". Cycle
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
//   - cfg.FastpushStore == nil → -32603 (the
//     nil-fabrication in NewDispatcher means this
//     only trips a dispatcher that explicitly
//     cleared the field post-construction).
//   - ErrFastpushCascadeNotFound (the store
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
// FastpushPushDeliveryDeferred constant) — an
// explicit sentinel so dashboards do not assume an
// actual delivery path is wired. The future
// rc-fastpush-channel cycle will flip this to a
// "live" sentinel once a real transport lands.
func (d *Dispatcher) handleSetRemoteAgentFastpushEnabled(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.FastpushStore == nil {
		return nil, internalError("no fastpush store installed")
	}
	cascadeID, enabled, perr := decodeFastpushParams(params)
	if perr != nil {
		return nil, perr
	}
	if d.cascades == nil {
		return nil, internalError("registry not initialised")
	}
	if _, ok := d.cascades.Get(cascadeID); !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}
	}
	// Store mutation: upsert the row for cascadeID
	// with the supplied enabled value. The store
	// stamps Channel / PushDelivery / UpdatedAt
	// with the cycle's sentinel values.
	setting, err := d.cfg.FastpushStore.SetEnabled(nil, cascadeID, enabled)
	if err != nil {
		return nil, mapFastpushError(err, "set fastpush enabled")
	}
	// Cascade in-memory flip + broadcast happen in
	// applyFastpushSetting; matches the rc-plugins /
	// rc-triggers pattern so a dashboard's response
	// handler can rely on the broadcast having
	// already been enqueued.
	return d.applyFastpushSetting(setting), nil
}

// broadcastFastpushSettingChanged fans a
// fastpush/setting_changed notification out to every
// registered transport's open connection.
//
// Why a dispatcher-side helper rather than a
// transport-side one: the single fastpush handler
// lives on the dispatcher, and the broadcaster
// (SetBroadcaster registered it) is the seam a
// transport installs to deliver "frame to every open
// conn" without the dispatcher knowing the concrete
// transport type.
//
// SetBroadcaster is the seam documented in notify.go.
// The internal/remotetransport package wires its own
// broadcaster at TCPServer construction (see the
// rc-permissions-grants cycle commit). Without a
// registered broadcaster the method is a no-op —
// in-process dispatchers and the Unix-socket
// transport have no notion of "every open
// connection". The mutation still landed in the
// store; the SetRemoteAgentFastpushEnabled handler
// echoes the post-state in the response envelope, so
// polling is always possible.
func (d *Dispatcher) broadcastFastpushSettingChanged(setting FastpushSetting) {
	if d == nil {
		return
	}
	params := fastpushSettingToWire(setting)
	d.BroadcastNotify(context.Background(), NotifFastpushSettingChangedMethod, params)
}

// applyFastpushSetting performs the post-store-mutation
// side effects: flips the cascade's in-memory fastpush
// flag and broadcasts a `fastpush/setting_changed`
// notification, then returns the response envelope. The
// single-handler fastpath keeps the handler body in
// fastpush_dispatcher.go under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func (d *Dispatcher) applyFastpushSetting(setting FastpushSetting) map[string]any {
	if d != nil {
		if c, ok := d.cascades.Get(setting.CascadeID); ok && c != nil {
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
		d.broadcastFastpushSettingChanged(setting)
	}
	// Return the brief's lean envelope. The
	// response carries the post-mutation state
	// verbatim (the same shape the broadcast
	// notification emits) so polling is always
	// possible — the broadcaster may not be
	// registered on every transport (Unix-socket
	// doesn't have one).
	return fastpushSettingToWire(setting)
}
