// Package remotecontrol — daemon_config.go: the *Daemon's
// Start-time Config struct. Pure file split out of daemon.go;
// every per-cycle story documented inline below.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import "os"

// DefaultPermissions is the file mode used when Config.Permissions is zero.
const DefaultPermissions os.FileMode = 0o600

// Config is the Start-time wiring. Every field has a sensible
// default; only SocketPath is typically set explicitly (to put
// the socket somewhere predictable from a script).
//
// Cycle rc-ws-transport: the SocketPath + Permissions fields are
// the only transport-level concerns Config carries. The TCP+WS
// transport (added in this cycle) reads its bind address from a
// separate remotetransport.TCPConfig and reuses the SessionState,
// Agents, and Reader the dispatcher already holds.
//
// Cycle rc-cascade-start: the Cascades field is the per-process
// cascade registry. nil → NewDispatcher builds a fresh one with
// CascadeRegistryDefaultMax capacity. Every ws connection a
// dashboard opens on a given dispatcher shares this registry
// (matches the brief's "the dispatcher is the cascade home").
type Config struct {
	// SocketPath is the absolute Unix socket path. Empty means
	// "$TMPDIR/openagy-remote-control-<pid>.sock" or, when
	// $TMPDIR is unset, os.TempDir().
	SocketPath string
	// Permissions is the file mode applied to the socket file.
	// 0o600 by default — the daemon only needs to be reachable
	// from the same user, and the dashboard connects via the
	// user's own session. Tests may lower it.
	Permissions os.FileMode
	// Session is the live snapshot. Required for the status and
	// get_output tools to be useful; tests pass a freshly-built
	// SessionState.
	Session *SessionState
	// Agents is the AgentLister. nil falls back to DefaultAgentLister.
	Agents AgentLister
	// Reader is the OutputReader. nil falls back to DefaultOutputReader,
	// which returns ErrNotFound for any id (good enough for a
	// dashboard smoke test; production wiring swaps it in).
	Reader OutputReader
	// Cascades is the cascade registry. nil → NewDispatcher
	// builds a fresh one with CascadeRegistryDefaultMax capacity.
	// Cycles that need a different cap (the rc-cascade-start
	// test that asserts overflow behavior, for instance) inject
	// a custom-built registry here.
	Cascades *CascadeRegistry

	// ListBackend is the seam the four read-only List… methods
	// (rc-list-readonly) call into. nil closures fall back to
	// the per-method DefaultX slice; a fully-zero value is a
	// usable configuration that returns DefaultModelInfo /
	// DefaultSlashCommands / empty slices.
	ListBackend ListBackend

	// FS is the seam the two read-only filesystem methods
	// (ReadRemoteAgentFile / ReadRemoteAgentDir, cycle
	// rc-file-ops) call into. nil → the dispatcher rejects
	// every read with a -32603 "Internal error" (no FS
	// provider installed); production wiring installs a
	// WorkdirScope built from the host's cwd + --add-dir +
	// sandbox flag. Tests inject a MemFS (files.go).
	FS FSProvider

	// Projects is the seam the three project-CRUD methods
	// (ListRemoteAgentProjects / CreateRemoteAgentProject /
	// UpdateRemoteAgentProject, cycle rc-projects) call into.
	// nil → the dispatcher rejects every project call with
	// a -32603 "Internal error"; production wiring installs
	// an FSProjectStore rooted at $ANTIGRAVITY_HOME/projects/.
	Projects ProjectStore

	// Conversations is the seam the three destructive
	// methods introduced in cycle
	// rc-conversation-deletions —
	// DeleteRemoteAgentMessage /
	// DeleteRemoteAgentConversation — call into. nil →
	// the dispatcher rejects every conversation-delete
	// call with a -32603. Production wires an
	// FSConversationStore (convstore_fs.go); tests
	// inject a MemConversationStore (convstore_helpers.go).
	Conversations ConversationStore

	// AuthState is the threadsafe holder the rich-status
	// tool reads from and SetRemoteControlSession
	// writes to. nil → the dispatcher fabricates a
	// fresh AuthState so the auth block on the wire is
	// present but empty.
	//
	// Cycle rc-status-tool adds this.
	AuthState *AuthState

	// DaemonMeta is the small block the rich-status
	// tool emits as the `daemon` field on the wire.
	// nil → the dispatcher fabricates a
	// zero-valued DaemonMeta.
	//
	// Cycle rc-status-tool adds this.
	DaemonMeta *DaemonMeta

	// Commit is the build-time git SHA the
	// remoteControlVersion RPC surfaces as
	// `daemon_sha`. Empty → the dispatcher returns
	// "unknown". Forward-compatible with a future
	// build-label string via `BuildLabel` below.
	//
	// Cycle rc-status-tool adds this.
	Commit string

	// BuildLabel is the build-time label the
	// remoteControlVersion RPC surfaces as
	// `build_label`. Empty → the dispatcher returns
	// "unknown".
	BuildLabel string

	// MessageBackend is the cascade's runtime seam.
	// nil → every cascade routes its turns through the
	// `StubBackend` seeded by `NewCascade`. A non-nil
	// value is what the cascade's lazy `Backend()`
	// accessor picks on first Send call so the cascade
	// doesn't pin a transport at construction time.
	//
	// Cycle rc-piagent-bind adds this.
	MessageBackend MessageBackend

	// LastDeepLink is the threadsafe holder the
	// remoteControlDeepLink handler stamps on
	// every "open" verb. nil → NewDispatcher
	// fabricates a fresh *DeepLinkState so the
	// dispatcher always has one.
	//
	// Cycle rc-deep-link adds this.
	LastDeepLink *DeepLinkState

	// PermissionsStore is the seam the four
	// permission-mutation methods
	// (SetRemoteAgentPermissionPreset /
	// SetRemoteAgentArtifactReviewMode /
	// AddRemoteAgentPermissionGrants /
	// RemoveRemoteAgentPermissionGrants,
	// cycle rc-permissions-grants) call into.
	// nil → the dispatcher rejects every
	// permission call with -32603; production
	// wiring installs an FSPermissionStore
	// rooted at $ANTIGRAVITY_HOME/permissions.json.
	// Tests inject a MemPermissionStore
	// (permissions_store.go).
	//
	// The field is named PermissionsStore (rather
	// than Permissions) because Config already
	// carries a `Permissions os.FileMode` field
	// for the Unix-socket file mode. The two
	// fields are unrelated.
	PermissionsStore PermissionStore

	// PermissionsPath is the absolute path the
	// production FSPermissionStore loads from /
	// saves to. "" — the default — means
	// "use auth.AntigravityHome() +
	// "permissions.json"".
	//
	// Cycle rc-permissions-grants adds this.
	PermissionsPath string

	// PluginsStore is the seam the two plugin
	// methods (ListRemoteAgentPlugins /
	// DisconnectRemoteAgentPlugin, cycle
	// rc-plugins) call into. nil → the dispatcher
	// fabricates a fresh MemPluginStore seeded
	// with DefaultPluginFixtures; production
	// wiring installs an FSPluginStore rooted at
	// $ANTIGRAVITY_HOME/plugins.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor — buildHostPluginStore).
	//
	// Cycle rc-plugins adds this.
	PluginsStore PluginStore

	// PluginsPath is the absolute path the
	// production FSPluginStore loads from /
	// saves to. "" — the default — means "use
	// auth.AntigravityHome() + "plugins.json"".
	//
	// Cycle rc-plugins adds this.
	PluginsPath string

	// TriggersStore is the seam the five trigger
	// methods (ListRemoteAgentTriggers /
	// ListRemoteAgentTriggerOptions /
	// CreateRemoteAgentTrigger /
	// UpdateRemoteAgentTrigger /
	// DeleteRemoteAgentTrigger, cycle rc-triggers)
	// call into. nil → the dispatcher fabricates a
	// fresh empty MemTriggerStore (no entries,
	// version=1); production wiring installs an
	// FSTriggerStore rooted at
	// $ANTIGRAVITY_HOME/triggers.json.
	//
	// This cycle is **persistence only** — no
	// scheduling engine, no fsnotify watcher, no
	// webhook receiver. The future
	// `rc-triggers-runner` cycle wires the
	// runner that consumes the on-disk state.
	//
	// Cycle rc-triggers adds this.
	TriggersStore TriggerStore

	// TriggersPath is the absolute path the
	// production FSTriggerStore loads from /
	// saves to. "" — the default — means "use
	// auth.AntigravityHome() + "triggers.json"".
	//
	// Cycle rc-triggers adds this.
	TriggersPath string

	// FastpushStore is the seam the single
	// SetRemoteAgentFastpushEnabled method
	// (cycle rc-fastpush-toggle) calls into.
	// nil → the dispatcher fabricates a fresh
	// empty MemFastpushStore; production wiring
	// installs an FSFastpushStore rooted at
	// $ANTIGRAVITY_HOME/fastpush.json.
	//
	// The fastpush surface is persistence-only
	// this cycle: the wire envelope always
	// carries push_delivery:"deferred" as an
	// explicit sentinel.
	//
	// Cycle rc-fastpush-toggle adds this.
	FastpushStore FastpushStore

	// FastpushPath is the absolute path the
	// production FSFastpushStore loads from /
	// saves to. "" — the default — means "use
	// auth.AntigravityHome() + "fastpush.json"".
	//
	// Cycle rc-fastpush-toggle adds this.
	FastpushPath string

	// ResourceStore is the seam the three
	// resource-management methods
	// (ListRemoteAgentResourceProviders /
	// ListRemoteAgentAvailableResources /
	// UpdateRemoteAgentProjectResources,
	// cycle rc-resources) call into. nil →
	// the dispatcher fabricates a fresh empty
	// MemResourceStore seeded with the brief's
	// fixture set (4 well-known providers + 12
	// well-known resources); production wiring
	// installs an FSResourceStore rooted at
	// $ANTIGRAVITY_HOME/resources.json.
	//
	// The Bind path the store exposes writes
	// back to the project's `Resources` field
	// via the ProjectStore seam (Config.Projects).
	// A ResourceStore that has no Projects
	// installed returns
	// ErrResourceProjectNotFound for every Bind
	// call — the dispatcher's -32004 envelope
	// matches the cycle-9/-22 project-not-found
	// envelope.
	//
	// Cycle rc-resources adds this.
	ResourceStore ResourceStore

	// ResourcePath is the absolute path the
	// production FSResourceStore loads from /
	// saves to. "" — the default — means "use
	// auth.AntigravityHome() + "resources.json"".
	//
	// Cycle rc-resources adds this.
	ResourcePath string

	// ConversationConfigStore is the seam the
	// single UpsertRemoteAgentConversationConfig
	// method (cycle rc-conversation-config) calls
	// into. nil → the dispatcher fabricates a
	// fresh empty MemConversationConfigStore;
	// production wiring installs an
	// FSConversationConfigStore rooted at
	// $ANTIGRAVITY_HOME/conversation_config.json.
	//
	// The cycle is **write-only**: the single
	// UpsertRemoteAgentConversationConfig method
	// is the only consumer; the future
	// `rc-conversation-config-read` cycle adds
	// GetRemoteAgentConversationConfig +
	// ListRemoteAgentConversationConfigs.
	//
	// Cycle rc-conversation-config adds this.
	ConversationConfigStore ConversationConfigStore

	// ConversationConfigPath is the absolute path
	// the production FSConversationConfigStore
	// loads from / saves to. "" — the default —
	// means "use auth.AntigravityHome() +
	// "conversation_config.json"".
	//
	// Cycle rc-conversation-config adds this.
	ConversationConfigPath string

	// TerminalsRegistry is the seam the five
	// RemoteAgentTerminal* methods (cycle
	// rc-terminals) call into. nil → the
	// dispatcher fabricates a fresh empty
	// TerminalsRegistry; production wires an
	// in-memory registry.
	//
	// CreateRemoteAgentTerminal wires the
	// terminal to a cascade (when the dashboard
	// supplies a cascade_id) by calling
	// (*TerminalsRegistry).AttachCascade on the
	// cascade's TerminalOutput() chan.
	TerminalsRegistry *TerminalsRegistry

	// OriginPolicyStore is the seam the
	// single SetRemoteControlOriginPatterns
	// method (cycle rc-auth-tokens) calls
	// into. nil → the dispatcher fabricates
	// a fresh empty MemOriginPolicyStore;
	// production wiring installs an
	// FSOriginPolicyStore rooted at
	// $ANTIGRAVITY_HOME/origin_policy.json.
	//
	// Cycle rc-auth-tokens adds this.
	OriginPolicyStore OriginPolicyStore

	// OriginPolicyPath is the absolute
	// path the production
	// FSOriginPolicyStore loads from /
	// saves to. "" — the default —
	// means "use auth.AntigravityHome() +
	// "origin_policy.json"".
	//
	// Cycle rc-auth-tokens adds this.
	OriginPolicyPath string

	// DeferredFeatures is the
	// forward-compat map the
	// RemoteControlStatus handler
	// (cycle rc-webrtc-mesh-defer)
	// surfaces on the wire as the
	// `deferred_features` block. The
	// keys are the four deferred
	// transports the branch does NOT
	// ship:
	//
	//   - "webrtc_mesh": WebRTC
	//     peer-to-peer mesh transport
	//     (webChannelHandlerV2 /
	//     PeerSession / MeshDaemon /
	//     TURN/STUN). Deferred to a
	//     future `rc-webrtc-mesh` cycle.
	//   - "antigravity_hub": second
	//     Hub process (runHubServer /
	//     configureHeadlessServer).
	//     Deferred; the clone is the
	//     daemon.
	//   - "systemd_user": Linux
	//     systemd-user service-manager
	//     backend (parallel port of
	//     cycle-12 rc-persistent-daemon
	//     to Linux). Deferred.
	//   - "enterprise_tunnel":
	//     /remote-control on
	//     session-scoped tunnel
	//     (RemoteAgentOnDemandTunnel /
	//     RemoteAgentEndTunnel).
	//     Deferred; explicitly
	//     enterprise-only.
	//
	// Defaults: every key maps to
	// `false` — the daemon is on the
	// legacy TCP+WS path until a future
	// cycle flips the relevant entry to
	// `true`. nil maps are treated the
	// same as the all-false default.
	//
	// Dashboards read this envelope via
	// the existing RemoteControlStatus
	// JSON-RPC method. See
	// docs/deferred-rc-transport.md for
	// the rationale and the forward
	// contract.
	//
	// Cycle rc-webrtc-mesh-defer adds
	// this.
	DeferredFeatures map[string]bool
}
