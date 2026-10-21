// Package remotecontrol — plugins_dispatcher.go: the
// per-method JSON-RPC handlers for the two plugin methods
// introduced in cycle rc-plugins:
//
//	ListRemoteAgentPlugins
//	DisconnectRemoteAgentPlugin
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the two handlers share the PluginStore
// seam + a small sentinel-error map (ErrPluginNotFound →
// -32015), and the destructive handler needs to emit a
// plugins/changed broadcast notification. Putting the
// handlers here, next to the type definitions (plugins.go)
// and the store (plugins_store.go), keeps the per-method
// bodies focused on the JSON-RPC envelope shape rather
// than the dispatcher's switch table.
//
// Sentinel map:
//
//	ErrPluginNotFound → -32015 "Plugin not found"
//	(other / I/O)     → -32603 "Internal error"
//
// The -32015 code is brand-new this cycle; -32602 ("Invalid
// params") is shared with every other method's "bad input"
// envelope (the brief explicitly says so). The "empty
// plugin_id" case maps to -32602 because it's a dashboard
// bug; the "unknown id" case maps to -32015 because it's
// a plausible retry-after-disconnect race.
//
// Cycle rc-plugins also adds the "broadcast on change"
// path: every successful Disconnect emits a
// `plugins/changed` notification to every open ws
// connection so a dashboard that subscribed to the
// channel sees the new state. The broadcast uses the
// existing (*Dispatcher).BroadcastNotify seam added in
// cycle rc-permissions-grants — the inner-case methods
// here don't have to know which transport is registered,
// they just call BroadcastNotify and the registered
// broadcaster fans the frame out. In-process callers
// without a transport fall back to a no-op broadcast (the
// mutation still lands in the store; the
// DisconnectRemoteAgentPlugin handler returns the
// post-mutation state in the response envelope so polling
// is always possible).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// PluginsChangedMethod is the JSON-RPC method name the
// dispatcher uses for the server-push notification emitted
// on every successful Disconnect. Wired via
// Dispatcher.Notify(method, params) once per changed
// conn.
//
// A "plugins/changed" notification carries:
//
//	{"version": 1, "plugins": [{plugin_id, name,
//	 version, connected_at, capabilities, origin?}, ...],
//	 "updated_at": "<RFC3339Nano>"}
//
// — the same shape the two methods' return envelopes
// emit. The notification lets a dashboard that watches
// the channel re-render without re-polling.
const PluginsChangedMethod = "plugins/changed"

// PluginStateWireFormat is the wire / on-disk format
// version. Must match onDiskPluginState's `version` key.
// Surfaced in the wire envelope so a future dashboard can
// branch on the version before unmarshalling.
const PluginStateWireFormat = 1

// disconnectRemoteAgentPluginParams is the JSON shape the
// dashboard posts for DisconnectRemoteAgentPlugin.
// plugin_id is a required string; missing/empty values
// map to -32602 before the store sees them.
type disconnectRemoteAgentPluginParams struct {
	PluginID string `json:"plugin_id"`
}

// handleListRemoteAgentPlugins is the JSON-RPC handler
// for "ListRemoteAgentPlugins". Cycle rc-plugins.
//
// Param validation:
//
//   - none required; the brief pins this as a pure read.
//
// Store call:
//
//   - cfg.PluginsStore == nil → the dispatcher has
//     fabricated a fresh MemPluginStore (NewDispatcher
//     nil-guard), so this case only trips a dispatcher
//     that explicitly cleared the field post-construction
//     (the negative test). We surface -32603.
//   - the store's error → -32603 (no store-sent
//     sentinel error today; future cycles can add one).
//
// Returns the post-snapshot state in the
// `{ok:true, plugins:[{plugin_id, name, version,
// connected_at, capabilities, origin?}, ...],
// updated_at}` envelope the brief pins. The
// plugins slice is sorted by plugin_id ascending
// (the store's Snapshot pins the order) so the
// dashboard walks a stable sequence.
//
// On error returns `-32603` (no store-sent
// sentinel error today; future cycles can add
// one). The wire envelope includes the `ok`
// boolean on the happy path so dashboards that
// fan rpc responses can short-circuit the
// isError check on a single field read.
func (d *Dispatcher) handleListRemoteAgentPlugins(params json.RawMessage) (any, *rpcErrorBody) {
	_ = params // List methods have no required params; ignore.
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.PluginsStore == nil {
		return nil, internalError("no plugin store installed")
	}
	state, err := d.cfg.PluginsStore.Snapshot(nil)
	if err != nil {
		return nil, mapPluginsError(err, "list plugins")
	}
	return pluginStateToWire(state), nil
}

// handleDisconnectRemoteAgentPlugin is the JSON-RPC
// handler for "DisconnectRemoteAgentPlugin". Cycle
// rc-plugins.
//
// Param validation:
//
//   - plugin_id: required, non-empty string. Missing /
//     empty / non-string → -32602 (the "dashboard bug"
//     envelope). The store's defence-in-depth
//     normalisePluginID guard catches whitespace-only
//     ids here too.
//
// Store call:
//
//   - cfg.PluginsStore == nil → -32603.
//   - ErrPluginNotFound (the store refuses an unknown
//     id) → -32015.
//   - other error → -32603.
//
// On success the handler emits a plugins/changed
// notification to every open ws connection so a
// dashboard watching the channel re-renders without
// polling. The response envelope is the brief's
// lean `{ok:true, plugin_id, disconnected_at}`
// shape — subscribers get the full post-state via
// the plugins/changed broadcast for any
// re-render they need.
func (d *Dispatcher) handleDisconnectRemoteAgentPlugin(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.PluginsStore == nil {
		return nil, internalError("no plugin store installed")
	}
	var p disconnectRemoteAgentPluginParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "plugin_id", p.PluginID) {
		return nil, invalidParams("string fields must be strings")
	}
	id := normalisePluginID(p.PluginID)
	if id == "" {
		return nil, invalidParams("plugin_id is required and must be non-empty")
	}
	state, err := d.cfg.PluginsStore.Disconnect(nil, id)
	if err != nil {
		return nil, mapPluginsError(err, "disconnect plugin")
	}
	// Disconnect the broadcast BEFORE returning the
	// response envelope; matches the
	// rc-permissions-grants pattern
	// (broadcastPermissionsChanged), so a
	// dashboard's response handler can rely on the
	// notification having already been enqueued.
	d.broadcastPluginsChanged(state)
	return map[string]any{
		"ok":              true,
		"plugin_id":       id,
		"disconnected_at": state.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// pluginStateToWire renders a PluginState as the
// dashboard-facing wire shape. The envelope is the
// brief's `{ok:true, plugins:[...]}` shape; the
// broadcast notification's params reuses the same
// helper so a dashboard that listens for
// plugins/changed and one that polls
// ListRemoteAgentPlugins see byte-for-byte
// identical envelopes.
//
// Per-plugin fields (per the brief):
//
//	{
//	  "plugin_id":     "<id>",
//	  "name":          "<display>",
//	  "version":       "<semver>",
//	  "connected_at":  "<RFC3339Nano>",
//	  "capabilities":  ["...", "..."],
//	  "origin":        "<provenance>"   // omitempty
//	}
//
// Top-level (per the brief):
//
//	{
//	  "ok":         true,
//	  "plugins":    [{…}, …]
//	}
//
// The PluginStateWireFormat / Version stamp is
// intentionally NOT surfaced on the wire so the
// brief's shape is byte-for-byte preserved. A
// future cycle that wants versioning can extend
// the envelope and bump the wire-format version
// in PLAN.md.
//
// nil plugins → empty slice (`[]`) on the wire
// so dashboards never see `plugins: null`.
//
// `plugins` is `[]any` (not `[]map[string]any`) so
// the in-process return value matches what the
// JSON round-trip produces: `encoding/json`
// decodes a JSON array into `[]interface{}` (`==
// `[]any`), so an in-process caller (no JSON
// round-trip) and a wire caller (post JSON) see
// the same shape. Without this an in-process
// caller would see `res["plugins"].([]map[string]any)`
// and a wire caller would see
// `res["plugins"].([]any)` — the same field, two
// types, and the in-process tests would fail to
// cast. Same rationale as permissionStateToWire.
//
// Capabilities: nil → []; non-nil → as-is.
// Capability entries that the JSON unmarshal
// round-trip would emit as floats (e.g. an entry
// that was a number, not a string) are handled by
// the store's own validation (only strings reach
// the state from valid snapshots); if a future
// cycle adds richer capability encoding, it's a
// wire-format break that warrants bumping
// PluginStateWireFormat.
//
// Origin: empty → field omitted (omitempty); non-
// empty → emitted verbatim. The store's origin
// field carries the provenance note
// ("config" / "auto-discovered") so a dashboard
// can distinguish fixture-seeded plugins from
// operator-supplied ones.
func pluginStateToWire(state PluginState) map[string]any {
	plugins := state.Plugins
	if plugins == nil {
		plugins = []Plugin{}
	}
	pluginList := make([]any, 0, len(plugins))
	for _, p := range plugins {
		caps := p.Capabilities
		if caps == nil {
			caps = []string{}
		}
		capsAny := make([]any, 0, len(caps))
		for _, c := range caps {
			capsAny = append(capsAny, c)
		}
		entry := map[string]any{
			"plugin_id":    p.PluginID,
			"name":         p.Name,
			"version":      p.Version,
			"connected_at": p.ConnectedAt.UTC().Format(time.RFC3339Nano),
			"capabilities": capsAny,
		}
		if p.Origin != "" {
			entry["origin"] = p.Origin
		}
		pluginList = append(pluginList, entry)
	}
	return map[string]any{
		"ok":      true,
		"plugins": pluginList,
	}
}

// mapPluginsError maps the PluginStore sentinel errors to
// the JSON-RPC envelopes the brief calls for:
//
//	ErrPluginNotFound → -32015 "Plugin not found"
//	(other / I/O)     → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed. Mirrors
// mapPermissionsError's shape; the rc-permissions-grants
// cycle established the pattern.
func mapPluginsError(err error, op string) *rpcErrorBody {
	switch {
	case errors.Is(err, ErrPluginNotFound):
		return &rpcErrorBody{Code: -32015, Message: "Plugin not found", Data: err.Error()}
	default:
		return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
}

// broadcastPluginsChanged fans a plugins/changed
// notification out to every registered transport's open
// connection.
//
// Why a dispatcher-side helper rather than a
// transport-side one: the two plugin handlers both live
// on the dispatcher, and the broadcaster (Set Broadcaster
// registered it) is the seam a transport installs to
// deliver "frame to every open conn" without the
// dispatcher knowing the concrete transport type.
//
// SetBroadcaster is the seam documented in notify.go. The
// internal/remotetransport package wires its own
// broadcaster at TCPServer construction (see the
// rc-permissions-grants cycle commit). Without a
// registered broadcaster the method is a no-op —
// in-process dispatchers and the Unix-socket transport
// have no notion of "every open connection". The mutation
// still landed in the store; ListRemoteAgentPlugins
// always reflects the post-state, so polling is always
// possible.
func (d *Dispatcher) broadcastPluginsChanged(state PluginState) {
	if d == nil {
		return
	}
	params := pluginStateToWire(state)
	d.BroadcastNotify(context.Background(), PluginsChangedMethod, params)
}
