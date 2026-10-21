// Package remotecontrol — top_level.go: data model +
// shared helpers for the three "top-level" JSON-RPC
// methods promoted in cycle rc-top-level-rpc.
//
// What this file owns:
//
//   - TopLevelStatus — the wire envelope for the
//     RemoteControlStatus method. Captures the
//     single-call dashboard-on-connect snapshot the brief
//     pins: version, uptime_seconds, started_at,
//     connected_clients, open_cascades, dispatch_methods
//     (sorted), and a server_fingerprint that dashboards
//     can render as a build-id badge.
//
//   - RemoteControlOnOutputResult — the wire envelope
//     for the RemoteControlOnOutput method. Carries the
//     resolved sink_id, subscribed_at timestamp, and the
//     canonical filter (the list of notification topics
//     the server publishes; future cycles that add
//     notifications append to the canonical list without
//     changing the wire shape).
//
//   - RemoteControlOnMessageResult — the wire envelope
//     for the remoteControlOnMessage method. Always
//     returns {ok:true, …}; the dispatcher uses
//     error envelopes (-32602 / -32603) when the
//     message is bad or rejected by the gatekeeper,
//     so the result type only covers the accepted
//     case.
//
//   - RemoteControlOnMessageAllowList — the
//     closed-set gatekeeper: which methods the
//     remoteControlOnMessage gate will dispatch
//     through. Defaults to read-only `RemoteAgent*`
//     methods + RemoteAgentStatus; cycle-22 stubs
//     keep this list narrow on purpose so a dashboard
//     cannot use the message-injection surface as a
//     way to side-channel write operations.
//
//   - DefaultNotificationFilter — the canonical
//     topic set the RemoteControlOnOutput response
//     advertises. Sinks receive ALL notifications in
//     practice (not topic-filtered) once the dispatcher
//     integrates a future `rc-all-sinks-fanout` cycle
//     that re-broadcasts every per-topic notification
//     through AllSinks; today the cycle delivers the
//     seam so future wiring has nothing to migrate.
//
//   - DispatchMethods — a package-level sorted slice
//     listing every JSON-RPC method the dispatcher
//     currently routes. Maintained by hand, one entry
//     per switch arm in dispatch.go. The brief pins
//     this as the single source of truth so the
//     RemoteControlStatus response can't drift away
//     from the dispatcher's actual switch table. A
//     future `rc-dispatch-table-gen` cycle could
//     auto-generate this from a switch-arm registry
//     to remove the manual-sync burden.
//
//   - topLevelStatus — the read-only snapshot
//     builder. Walks SessionState for uptime / version
//     / connected_clients, the cascade registry for
//     open_cascades, and the package-level
//     DispatchMethods slice for the method list.
//
//   - serverFingerprint — derives a short
//     "wagyu-rc/<version>/<short-hash>" string from
//     the Config.Commit / Session.Version pair.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - top_level.go         — package doc + envelope types
//   - AllowList +
//     DefaultNotificationFilter +
//     DefaultDispatchMethods +
//     isRemoteControlOnMessageAllowed
//   - top_level_status.go  — topLevelStatus +
//     deferredFeaturesSnapshot +
//     serverFingerprint
//
// Cycle rc-top-level-rpc is stdlib-only — no
// third-party deps beyond the existing `coder/websocket`
// (transitive through remotetransport, not imported
// here).
package remotecontrol

import (
	"sort"
)

// TopLevelStatus is the wire envelope the
// RemoteControlStatus JSON-RPC method returns. The
// brief pins the field set; every field is required
// (non-nil / non-empty) so dashboards never have to
// nil-check before rendering.
//
// Field semantics:
//
//   - Version: the resolved CLI version. Mirrors
//     internal/cli/version.go's ReferenceVersion
//     ("1.4.2") unless Session.Version is
//     overridden at runtime by the host.
//   - UptimeSeconds: integer seconds since the
//     SessionState was created (NewSessionState
//     stamps SessionState.StartedAt).
//   - StartedAt: RFC3339Nano-encoded UTC. Matches
//     the format the rich-status tool already
//     emits in its `started_at` field so dashboards
//     can parse both with the same routine.
//   - ConnectedClients: SessionState.Connected().
//     Atomic — read without the SessionState mutex.
//   - OpenCascades: (*CascadeRegistry).Len(). All
//     cascades (open + closed) live there; the
//     brief pins the broader "open" count so the
//     response stays useful for capacity dashboards
//     before the connected_clients interpretation
//     stabilises.
//   - DispatchMethods: sorted ascending. The brief
//     pins the sort order so dashboards can do
//     `binarySearch(allowed)` without first
//     sorting the response.
//   - ServerFingerprint: short build-id badge
//     dashboards render as the daemon's identity.
//     Shape: "wagyu-rc/<version>/<short-hash>"
//     where <short-hash> is the first 8 hex chars
//     of Config.Commit (or "unknown" when empty).
type TopLevelStatus struct {
	Version           string   `json:"version"`
	UptimeSeconds     int64    `json:"uptime_seconds"`
	StartedAt         string   `json:"started_at"`
	ConnectedClients  int      `json:"connected_clients"`
	OpenCascades      int      `json:"open_cascades"`
	DispatchMethods   []string `json:"dispatch_methods"`
	ServerFingerprint string   `json:"server_fingerprint"`

	// DeferredFeatures is the
	// forward-compat map the
	// rc-webrtc-mesh-defer cycle
	// surfaces so dashboards can
	// detect whether the daemon is on
	// the legacy TCP+WS path or on
	// the deferred v2 transport (v2
	// features stay disabled in this
	// branch — every key is `false`
	// at the moment). The four
	// canonical keys:
	//
	//   - "webrtc_mesh": WebRTC
	//     peer-to-peer mesh transport
	//   - "antigravity_hub": the
	//     second Hub process
	//   - "systemd_user": Linux
	//     systemd-user service-manager
	//     backend
	//   - "enterprise_tunnel":
	//     session-scoped tunnel
	//     (enterprise gateway)
	//
	// Always emitted (never nil) so a
	// dashboard renderer does not have
	// to nil-check before iterating.
	// Source: cfg.DeferredFeatures,
	// with the missing-key canonical
	// default (all false) filled in by
	// topLevelStatus on every call.
	DeferredFeatures map[string]bool `json:"deferred_features"`
}

// RemoteControlOnOutputResult is the wire envelope
// the RemoteControlOnOutput JSON-RPC method returns.
// The brief pins a lean shape: ok, sink_id (the
// resolved id — defaults to the calling connection's
// connID when the dashboard omits sink_id),
// subscribed_at (RFC3339Nano), and filter (the
// canonical notification topics; future cycles that
// add new notifications extend the canonical list
// here without changing the field shape).
//
// AllSinks is a server-side set, not on the wire —
// the dashboard learns it's now registered by the OK
// flag in this envelope.
type RemoteControlOnOutputResult struct {
	OK           bool     `json:"ok"`
	SinkID       string   `json:"sink_id"`
	SubscribedAt string   `json:"subscribed_at"`
	Filter       []string `json:"filter"`
}

// RemoteControlOnMessageResult is the wire envelope
// the remoteControlOnMessage JSON-RPC method returns
// on the ACCEPTED path. The dispatcher's gatekeeper
// returns -32602 / -32603 envelopes for bad params
// or rejected methods, so this struct is the
// accepted-only return shape.
//
// Accepted is always true on this path — the
// gatekeeper is the only thing that decides whether
// the inner message fires; the wire envelope is
// confirmation that it did.
//
// ReplayedAt is RFC3339Nano: the wall-clock at
// which the dispatcher received and re-dispatched
// the inner message. Dashboards use this to render
// latency-to-dispatch in their replay UI.
type RemoteControlOnMessageResult struct {
	OK         bool   `json:"ok"`
	Accepted   bool   `json:"accepted"`
	ReplayedAt string `json:"replayed_at"`
}

// RemoteControlOnMessageAllowList is the closed
// gatekeeper for the remoteControlOnMessage method.
// A method that is NOT in this set is rejected with
// -32603 ("rejected by gatekeeper"). The list is
// deliberately narrow:
//
//   - RemoteAgentStatus (read-only probe)
//   - ListRemoteAgent* (read-only list surface)
//   - GetRemoteAgent* (read-only get surface)
//
// A future cycle that wants to expose more methods
// through the message-injection surface extends
// this slice (the gatekeeper picks up the change
// automatically — no per-method code). Per the
// brief's "no arbitrary remote-execution" rule, the
// list today contains no write methods.
var RemoteControlOnMessageAllowList = []string{
	"RemoteAgentStatus",
	"ListRemoteAgentModels",
	"ListRemoteAgentSlashCommands",
	"ListRemoteAgentConversations",
	"ListRemoteAgentEnvironments",
	"ListRemoteAgentProjects",
	"ListRemoteAgentPlugins",
	"ListRemoteAgentTriggers",
	"ListRemoteAgentTriggerOptions",
	"ListRemoteAgentTerminals",
	"ListRemoteAgentResourceProviders",
	"ListRemoteAgentAvailableResources",
	"GetRemoteAgentFile",      // reserved; not yet dispatched
	"GetRemoteAgentDir",       // reserved; not yet dispatched
	"remoteControlVersion",    // already a top-level method, included for parity
	"RemoteAgentErrorStatus",  // read-only probe of the cascade error ring
	"ListRemoteAgentChannels", // reserved; not yet dispatched
}

// isRemoteControlOnMessageAllowed reports whether
// the given method is in
// RemoteControlOnMessageAllowList. Used by
// handleRemoteControlOnMessage to gate the inner
// re-dispatch; nil-safe (returns false on a nil
// method).
func isRemoteControlOnMessageAllowed(method string) bool {
	if method == "" {
		return false
	}
	for _, m := range RemoteControlOnMessageAllowList {
		if m == method {
			return true
		}
	}
	return false
}

// DefaultNotificationFilter is the canonical
// notification-topic list the RemoteControlOnOutput
// response advertises. Sinks in practice receive
// every notification that flows through the
// dispatcher (not topic-filtered) — the brief calls
// this out explicitly. Today the dispatcher
// publishes these topics:
//
//   - cascade/state
//   - cascade/text_delta
//   - cascade/tool_call
//   - cascade/user_interaction_required
//   - permissions/changed
//   - plugins/changed
//   - triggers/changed
//   - resources/changed
//   - conversation_config/changed
//   - fastpush/setting_changed
//   - terminal/output
//   - terminal/closed
//
// Future cycles that add a new server-push topic
// append to this slice without changing the wire
// shape (the dashboard's UI re-renders the list).
// The slice is sorted alphabetically on the wire
// so the dashboard's renderer doesn't have to
// re-sort.
var DefaultNotificationFilter = []string{
	"cascade/state",
	"cascade/text_delta",
	"cascade/tool_call",
	"cascade/user_interaction_required",
	"conversation_config/changed",
	"fastpush/setting_changed",
	"permissions/changed",
	"plugins/changed",
	"resources/changed",
	"terminal/closed",
	"terminal/output",
	"triggers/changed",
}

// sortedDefaultNotificationFilter returns a copy of
// DefaultNotificationFilter sorted alphabetically.
// The package-level slice is already sorted, but a
// future edit could reorder it; this helper
// guarantees the wire shape.
func sortedDefaultNotificationFilter() []string {
	out := make([]string, len(DefaultNotificationFilter))
	copy(out, DefaultNotificationFilter)
	sort.Strings(out)
	return out
}

// DispatchMethods is the package-level sorted slice
// listing every JSON-RPC method the dispatcher's
// switch routes. Maintained by hand; one entry per
// switch arm in dispatch.go's Dispatch.
//
// Why a slice and not reflection: the dispatcher's
// switch is dynamic — some arms fan out to a sub-
// switch for sub-methods (e.g. "ListRemoteAgent*"
// covers 5+ method names). The slice mirrors the
// full method-name set, not the arm count. The
// brief pins "package var populated from each
// switch arm — prefer the package var so it can't
// drift". The trade-off is manual sync: a new
// switch arm must add its method name(s) here
// before the next release.
//
// Sorted ascending so the wire shape is stable and
// dashboards can binary-search the response.
var DefaultDispatchMethods = []string{
	// MCP-compat surface
	"initialize",
	"tools/call",
	"tools/list",
	"notifications/initialized",
	"ping",

	// Cascade lifecycle
	"CancelRemoteAgentCascade",
	"HandleRemoteAgentCascadeUserInteraction",
	"SendRemoteAgentCascadeMessage",
	"StartRemoteAgentCascade",

	// Reads — list / get
	"GetRemoteAgentDir",  // reserved (future rc-file-ops-read)
	"GetRemoteAgentFile", // reserved (future rc-file-ops-read)
	"ListRemoteAgentAvailableResources",
	"ListRemoteAgentChannels", // reserved (future rc-channels-list)
	"ListRemoteAgentConversations",
	"ListRemoteAgentEnvironments",
	"ListRemoteAgentModels",
	"ListRemoteAgentPlugins",
	"ListRemoteAgentProjects",
	"ListRemoteAgentResourceProviders",
	"ListRemoteAgentSlashCommands",
	"ListRemoteAgentTerminals",
	"ListRemoteAgentTriggerOptions",
	"ListRemoteAgentTriggers",
	"ReadRemoteAgentDir",
	"ReadRemoteAgentFile",

	// Writes — destructive
	"AddRemoteAgentPermissionGrants",
	"CloseRemoteAgentTerminal",
	"CreateRemoteAgentProject",
	"CreateRemoteAgentTerminal",
	"CreateRemoteAgentTrigger",
	"DeleteRemoteAgentConversation",
	"DeleteRemoteAgentMessage",
	"DeleteRemoteAgentProject",
	"DisconnectRemoteAgentPlugin",
	"RemoveRemoteAgentPermissionGrants",
	"ResizeRemoteAgentTerminal",
	"SendRemoteAgentTerminalInput",
	"SetRemoteAgentArtifactReviewMode",
	"SetRemoteAgentFastpushEnabled",
	"SetRemoteAgentPermissionPreset",
	"UpdateRemoteAgentProject",
	"UpdateRemoteAgentProjectResources",
	"UpdateRemoteAgentTrigger",
	"UpsertRemoteAgentConversationConfig",

	// Errors / status / session
	"RemoteAgentErrorStatus",
	"RemoteAgentStatus",
	"RemoteControlStatus",
	"SetRemoteControlSession",

	// Top-level RPC (this cycle)
	"RemoteControlOnOutput",
	"remoteControlOnMessage",

	// Other top-level (pre-existing)
	"remoteControlDeepLink",
	"remoteControlVersion",
}

// sortedDispatchMethods returns a sorted copy of
// DefaultDispatchMethods. The package-level slice
// is already sorted, but a future edit could
// reorder it; this helper guarantees the wire
// shape is monotonic.
func sortedDispatchMethods() []string {
	out := make([]string, len(DefaultDispatchMethods))
	copy(out, DefaultDispatchMethods)
	sort.Strings(out)
	return out
}
