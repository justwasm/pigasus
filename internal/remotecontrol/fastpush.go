// Package remotecontrol — fastpush.go: the per-cascade
// fast-push toggle the JSON-RPC method
// `SetRemoteAgentFastpushEnabled` introduced in cycle
// rc-fastpush-toggle mutates.
//
// Why split: the fast-push toggle is a per-cascade bit
// the dashboard sets so a connected cascade can opt into
// the (deferred, parked) fastpush delivery engine. The
// data model is a tiny CRUD-shaped seam: a
// FastpushSetting (one row per cascade), a FastpushState
// (the on-disk / wire envelope the store holds), and a
// FastpushStore interface (the seam the dispatcher's
// single SetRemoteAgentFastpushEnabled handler calls).
//
// Storage shape:
//
//	$ANTIGRAVITY_HOME/fastpush.json
//
// One file, versioned ({"version":1,
// "settings":[{cascade_id, enabled, channel,
// push_delivery, updated_at}, ...], "updated_at":...}).
// The file format is intentionally forward-compatible so
// the future rc-fastpush-channel cycle can add fields
// (e.g. delivery_target / heartbeat_ms) without rewriting
// history (the version stamp bumps only on breaking
// changes).
//
// What "fast-push" means here:
//
// The decompiled agy binary's `RemoteAgentFastpushClient`
// pushes mid-turn updates over a separate server-push
// channel (cf. out/local-macos/dec/1018c7c40_remoteAgentFastpushClient.c
// / 1018c7a00_newFastpushTransport.c). Today the clone
// doesn't ship a real transport — the rc-fastpush-toggle
// cycle is persistence-only:
//
//   - SetRemoteAgentFastpushEnabled(cascade_id, enabled)
//     flips a per-cascade bit, persists it, and emits a
//     fastpush/setting_changed broadcast notification
//     the dispatcher fans out to every open ws
//     connection.
//   - The wire envelope always carries
//     `push_delivery:"deferred"` — a forward-compat
//     sentinel so dashboards do not assume an actual
//     delivery path is wired (the rc-fastpush-channel
//     cycle will flip the sentinel to `live` once a real
//     transport lands).
//
// Concurrency / wire-level invariants:
//   - empty / non-string cascade_id → -32602
//     "Invalid params";
//   - missing `enabled` field → -32602;
//   - unknown cascade_id → -32004 "Cascade not found";
//   - store error → -32603 "Internal error".
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
	"strings"
	"time"
)

// FastpushChannelName is the broadcast channel the
// SetRemoteAgentFastpushEnabled handler fans a
// notification out to. Wire value:
// "fastpush/cascade" — matches the agy-side
// `remoteAgentFastpushClient` channel naming, modulo
// the "remoteAgent" → "fastpush" rename the clone
// adopted in cycle rc-fastpush-toggle. The
// rc-fastpush-channel cycle (forward) will add a
// per-cascade channel suffix (e.g.
// "fastpush/cascade/<cas_...>") once a real delivery
// path lands; today's single-channel value is the
// sentinel.
const FastpushChannelName = "fastpush/cascade"

// FastpushPushDeliveryDeferred is the explicit
// `push_delivery` sentinel the wire envelope always
// carries today. The cycle brief pins this string so
// dashboards can render a clear "no live delivery" pill
// without confusing the toggle with a transport
// feature.
//
// Cycle rc-fastpush-channel will introduce a parallel
// FastpushPushDeliveryLive sentinel ("live") and flip
// the wire value once a real transport lands. Until
// then, every setting row carries push_delivery =
// "deferred" verbatim.
const FastpushPushDeliveryDeferred = "deferred"

// NotifFastpushSettingChangedMethod is the JSON-RPC
// method name the dispatcher uses for the server-push
// notification emitted on every successful
// SetRemoteAgentFastpushEnabled. Wired via
// Dispatcher.BroadcastNotify(method, params) once per
// changed conn.
//
// A "fastpush/setting_changed" notification carries the
// same shape the response envelope emits (see
// fastpushSettingToWire below).
const NotifFastpushSettingChangedMethod = "fastpush/setting_changed"

// FastpushSetting is one row of the fastpush registry
// the dashboard sees on a successful
// SetRemoteAgentFastpushEnabled. One instance per
// cascade_id; the brief pins these fields:
//
//   - CascadeID: the cascade this setting applies to
//     (the dispatcher's (*CascadeRegistry).Get
//     validates it on every Set call so an unknown
//     id lands as -32004 before any persistence
//     happens). Wire-mapped to "cascade_id".
//
//   - Enabled: the dashboard's toggle value. false
//     means "fast-push off" (today, this is the
//     default and equivalent to "no row exists");
//     true means "fast-push on". Wire-mapped to
//     "enabled".
//
//   - Channel: the broadcast channel the dispatcher's
//     notification routes through. Today the constant
//     FastpushChannelName ("fastpush/cascade"); a
//     future cycle that wants per-cascade channel
//     suffixes only has to extend the FastpushState
//     on-disk format and bump FastpushStateVersion.
//     Wire-mapped to "channel".
//
//   - PushDelivery: the explicit sentinel. Today
//     always FastpushPushDeliveryDeferred
//     ("deferred"); future rc-fastpush-channel sets
//     this to "live" once a real transport lands.
//     Wire-mapped to "push_delivery".
//
//   - UpdatedAt: RFC3339Nano UTC stamp the store
//     stamped at the most-recent Set call. Wire-mapped
//     to "updated_at".
type FastpushSetting struct {
	// CascadeID is the cascade this setting applies
	// to. Required and unique within the FastpushState
	// (the store's SetEnabled upserts).
	CascadeID string `json:"cascade_id"`

	// Enabled is the dashboard's toggle value. true
	// → fast-push on; false → fast-push off.
	Enabled bool `json:"enabled"`

	// Channel is the broadcast channel the
	// dispatcher's notification routes through.
	// Today always FastpushChannelName.
	Channel string `json:"channel"`

	// PushDelivery is the explicit delivery-status
	// sentinel. Today always
	// FastpushPushDeliveryDeferred.
	PushDelivery string `json:"push_delivery"`

	// UpdatedAt is when the store recorded the most-
	// recent Set. RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// FastpushState is the on-disk / wire shape the JSON
// file holds. One instance per
// $ANTIGRAVITY_HOME/fastpush.json; the store is
// single-document by design — there is no "list of
// states" because there is exactly one registry per
// host.
//
// The struct is intentionally JSON-serialised with
// lowercase keys so the on-disk format matches the wire
// format the dashboard reads. The Version field is
// bumped on breaking changes (the brief pins v1).
type FastpushState struct {
	// Version is the on-disk format version. Bumped
	// on breaking changes (renamed keys, removed
	// fields); non-breaking additions (new optional
	// fields with omitempty) leave the version alone.
	Version int `json:"version"`

	// Settings is the live fastpush slice, keyed by
	// CascadeID (one row per cascade). nil → empty
	// ([]FastpushSetting{}) so the wire always sees
	// "[]".
	Settings []FastpushSetting `json:"settings"`

	// UpdatedAt is the wall-clock stamp the store
	// bumped at the most-recent mutation. RFC3339Nano
	// UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// FastpushStore is the seam the single
// SetRemoteAgentFastpushEnabled handler calls into.
// Cycle rc-fastpush-toggle adds Snapshot / SetEnabled /
// Get; future cycles (rc-fastpush-channel) will extend
// it with a list-by-channel lookup + the delivery
// hooks that consume the on-disk state.
//
// Snapshot returns the current state. The dispatcher
// never checks for a "not-found" envelope — a fresh
// host with no fastpush.json yet returns the empty
// state (version=1, settings=[]) so the dashboard
// always has a coherent answer.
//
// SetEnabled upserts the entry for cascadeID. Returns
// ErrCascadeNotFound (the existing -32004 sentinel) on
// an unknown cascade_id so the dispatcher can map that
// to "Cascade not found" without a string compare.
// Returns the post-mutation setting so the dispatcher
// can echo the new value in the response envelope and
// embed the same shape in the broadcast notification.
//
// Get returns the setting for cascadeID, or
// ErrCascadeNotFound on an unknown id. Returns the
// post-mutation setting; returns a zero-valued
// FastpushSetting (no error) when the cascade has no
// stored row yet — callers that need a strict "has it
// been touched?" answer can compare UpdatedAt.IsZero().
//
// Both methods take a context the dispatcher already
// passes through; neither blocks on I/O today (the FS
// store uses an in-process mutex + synchronous write),
// but the signature is future-proof for a cycle that
// promotes the write path to async / batched.
type FastpushStore interface {
	Snapshot(ctx context.Context) (FastpushState, error)
	SetEnabled(ctx context.Context, cascadeID string, enabled bool) (FastpushSetting, error)
	Get(ctx context.Context, cascadeID string) (FastpushSetting, error)
}

// ErrCascadeNotFound is the sentinel FastpushStore.SetEnabled
// and FastpushStore.Get return when the caller tries to
// address a cascade_id that is not in the dispatcher's
// registry. The dispatcher translates to JSON-RPC
// -32004 "Cascade not found" via the existing
// ErrCodeCascadeNotFound constant in backend.go (the
// sentinel value is *not* re-exported as a JSON-RPC
// code; the dispatcher's switch maps via the constant).
//
// Cycle rc-fastpush-toggle adds this sentinel.
//
// The cycle reuses the dispatcher's existing
// ErrCascadeNotFound path (declared in cascade.go's
// CascadeRegistry) rather than introducing a new
// fastpush-specific not-found code: a missing cascade
// on a fastpush toggle is the same lifecycle error a
// SendMessage / CancelRemoteAgentCascade call surfaces,
// so the same -32004 envelope is the right code.
var ErrFastpushCascadeNotFound = errors.New("rc-fastpush: cascade not found")

// FastpushStateVersion is the on-disk format version
// the current store writes. Bumped on breaking changes
// (renamed keys, removed fields); non-breaking
// additions (new optional fields with omitempty) leave
// the version alone so older readers still parse the
// file.
//
// v1 (cycle rc-fastpush-toggle): the initial layout.
// See FastpushState for the field set.
const FastpushStateVersion = 1

// normaliseCascadeID trims whitespace from the user-
// supplied cascade_id. Empty after trim → "". Used by
// both the dispatcher layer (before calling SetEnabled
// / Get) and the store layer (when comparing for
// membership). Returns "" when the trimmed result is
// empty so the same check trips everywhere.
//
// Today the store does not auto-normalise (SetEnabled
// that pass "  cas_xxx  " and "cas_xxx" are considered
// distinct), but the helper is here so a future cycle
// that wants server-side canonicalisation (typical for
// id-shaped fields) only has to swap the lookup line
// in fastpush_store.go for normaliseCascadeID(...).
// The dispatcher path is the only validation site today
// (it surfaces the trimmed result in error envelopes),
// so the helper is intentionally not part of the public
// FastpushStore interface.
func normaliseCascadeID(id string) string {
	return strings.TrimSpace(id)
}

// fastpushSettingToWire renders a FastpushSetting as
// the dashboard-facing wire shape. The envelope is the
// brief's
// `{ok:true, cascade_id, enabled, channel,
// push_delivery, updated_at}` shape; the broadcast
// notification's params reuses the same helper so a
// dashboard that listens for fastpush/setting_changed
// and one that polls SetRemoteAgentFastpushEnabled see
// byte-for-byte identical envelopes.
//
// Channel and PushDelivery use the empty-string → ""
// convention so the wire emits the verbatim sentinel
// (today always FastpushChannelName /
// FastpushPushDeliveryDeferred; the helper is forward-
// compatible with the rc-fastpush-channel cycle's
// per-row override values).
//
// `settings` is `map[string]any` (not `map[any]any`)
// so the in-process return value matches what the JSON
// round-trip produces: `encoding/json` decodes a JSON
// object into `map[string]any`, so an in-process caller
// (no JSON round-trip) and a wire caller (post JSON) see
// the same shape. Without this an in-process caller
// would see `res["settings"].(map[any]any)` and a wire
// caller would see `res["settings"].(map[string]any)` —
// the same field, two types, and the in-process tests
// would fail to cast.
func fastpushSettingToWire(setting FastpushSetting) map[string]any {
	return map[string]any{
		"ok":            true,
		"cascade_id":    setting.CascadeID,
		"enabled":       setting.Enabled,
		"channel":       setting.Channel,
		"push_delivery": setting.PushDelivery,
		"updated_at":    setting.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// mapFastpushError maps the FastpushStore sentinel
// errors to the JSON-RPC envelopes the brief calls
// for:
//
//	ErrFastpushCascadeNotFound → -32004 "Cascade not found"
//	(other / I/O)              → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed. Mirrors mapPluginsError /
// mapTriggersError's shape; the rc-plugins / rc-triggers
// cycles established the pattern. -32602 ("Invalid
// params") is shared with every other method's "bad
// input" envelope and is emitted directly from the
// handler, not via this helper.
func mapFastpushError(err error, op string) *rpcErrorBody {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrFastpushCascadeNotFound) {
		return &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: err.Error()}
	}
	return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
}
