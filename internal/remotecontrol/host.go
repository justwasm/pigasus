// Package remotecontrol — host.go: the host-side helpers that
// cmd/wagyu/main.go calls to bring up and tear down the daemon.
//
// Why a separate file: the daemon itself (daemon.go) is pure stdlib
// so the unit tests can run without a full env. The host helpers
// live here because they read internal/auth (for the version
// string and $HOME layout) — which is the production wiring.
package remotecontrol

import (
	"context"
	"fmt"
	"io"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/rcaudio"
	"github.com/justwasm/pigasus/internal/rcterminal"
)

// EnvSocketPath is the env var the host honours to override the
// default socket path. Mirrors the convention OPENAGY_BACKEND uses
// for the model layer: a single env var, all-caps, OPENAGY_
// prefix, with a fallback to a per-user temp file.
const EnvSocketPath = "OPENAGY_RC_SOCKET"

// HostConfig is the wiring the host's main() passes to Start.
//
// Version is the CLI version (the same string the binary's --version
// prints). Backend / Model are populated by main() as it resolves
// them so the status tool reflects the resolved choice, not the
// raw flag value.
type HostConfig struct {
	// Version is the CLI version string ("1.3.4", "1.3.4-test", …).
	Version string
	// Backend is the resolved backend ("gateway" | "shim").
	Backend string
	// Model is the resolved model label.
	Model string
	// Workspace is the cwd at startup; pinned so a dashboard sees
	// the same path the user sees.
	Workspace string
	// Session is an optional pre-existing SessionState; the host
	// typically leaves this nil so StartHostSession() builds one.
	Session *SessionState
	// SocketPath is the override socket path; empty means "use
	// $TMPDIR/openagy-remote-control-<pid>.sock".
	SocketPath string
	// Out is where the daemon's "listening on …" line goes. nil
	// means os.Stdout — the daemon never blocks on user IO.
	Out io.Writer
	// ListBackend is the seam the four read-only List… methods
	// (rc-list-readonly) call into. nil → StartHostSession
	// installs the per-method defaults (DefaultModelInfo /
	// DefaultSlashCommands / empty slices / a stdlib-only
	// Environments snapshot). Production passes the closure
	// built by the cmd/wagyu/ layer (it has access to
	// internal/cli, internal/convstore, internal/auth).
	ListBackend ListBackend

	// FS is the seam the two read-only filesystem methods
	// (ReadRemoteAgentFile / ReadRemoteAgentDir, cycle
	// rc-file-ops) call into. nil → Start wires nothing
	// into Config.FS, and the dispatcher rejects every
	// read with -32603. Production passes a *WorkdirScope
	// built from cwd + --add-dir + sandbox (cmd/wagyu/
	// owns the WorkdirScope constructor).
	FS FSProvider

	// Projects is the seam the three project-CRUD
	// methods (ListRemoteAgentProjects /
	// CreateRemoteAgentProject / UpdateRemoteAgentProject,
	// cycle rc-projects) call into. nil → Start wires
	// nothing into Config.Projects, and the dispatcher
	// rejects every project call with -32603.
	// Production passes an FSProjectStore rooted at
	// $ANTIGRAVITY_HOME/projects/.
	Projects ProjectStore

	// Conversations is the seam the three destructive
	// methods introduced in cycle
	// rc-conversation-deletions —
	// DeleteRemoteAgentMessage /
	// DeleteRemoteAgentConversation — call into. nil →
	// Start wires nothing into Config.Conversations,
	// and the dispatcher rejects every
	// conversation-delete call with -32603.
	// Production passes an FSConversationStore that
	// wraps internal/convstore (see convstore_fs.go);
	// cmd/wagyu/remote_control.go owns the
	// constructor.
	Conversations ConversationStore

	// AuthState is the threadsafe holder the
	// rich-status tool reads from and
	// SetRemoteControlSession writes to. nil →
	// StartHostSession fabricates a fresh
	// AuthState (so the very first status call has
	// an auth block, even if it shows the
	// "none" default).
	// Cycle rc-status-tool adds this.
	AuthState *AuthState

	// DaemonMeta is the small block the rich-status
	// tool emits as the `daemon` field on the wire.
	// nil → StartHostSession fabricates a
	// zero-valued DaemonMeta so the daemon block is
	// always present.
	// Cycle rc-status-tool adds this.
	DaemonMeta *DaemonMeta

	// Commit is the build-time git SHA the
	// remoteControlVersion RPC surfaces as
	// `daemon_sha`. Forward-compatible with the
	// build_label below.
	// Cycle rc-status-tool adds this.
	Commit string

	// BuildLabel is the build-time label the
	// remoteControlVersion RPC surfaces as
	// `build_label`. Empty → the dispatcher emits
	// "unknown" so a dashboard's renderer never has
	// to nil-check.
	// Cycle rc-status-tool adds this.
	BuildLabel string

	// MessageBackend is the cascade's runtime seam
	// (cycle rc-piagent-bind). Start wires it into
	// Config.MessageBackend so every cascade the
	// dispatcher mints ships with the production
	// backend on first Send. nil keeps the cascade
	// on the seeded *StubBackend.
	MessageBackend MessageBackend

	// Browser is the agent↔browser bridge the three
	// LanguageServerService RPCs serve and the IDE turn's
	// `webview_browser` tool runs its actions in.
	//
	// The two halves have to share one instance: the window answers a
	// browser action over the subscribe stream, and the turn waits for
	// that answer in the broker. nil → the dispatcher has no browser
	// surface and the turn advertises no browser tool.
	//
	// Cycle rc-agent-browser adds this.
	Browser *agentbrowser.Broker

	// Permissions is the seam the four
	// permission-mutation methods (cycle
	// rc-permissions-grants) call into. nil →
	// Start wires nothing into Config.PermissionsStore,
	// and the dispatcher fabricates a fresh
	// MemPermissionStore seeded with the default
	// preset + mode + empty grants. Production
	// passes an FSPermissionStore rooted at
	// $ANTIGRAVITY_HOME/permissions.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor — buildHostPermissionStore).
	PermissionsStore PermissionStore

	// PermissionsPath is the absolute path the
	// production FSPermissionStore loads from /
	// saves to. Empty → the helper at
	// buildHostPermissionStore defaults to
	// auth.AntigravityHome()/permissions.json.
	// Cycle rc-permissions-grants adds this.
	PermissionsPath string

	// PluginsStore is the seam the two plugin
	// methods (ListRemoteAgentPlugins /
	// DisconnectRemoteAgentPlugin, cycle
	// rc-plugins) call into. nil → Start wires
	// nothing into Config.PluginsStore, and the
	// dispatcher fabricates a fresh
	// MemPluginStore seeded with
	// DefaultPluginFixtures. Production passes an
	// FSPluginStore rooted at
	// $ANTIGRAVITY_HOME/plugins.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor — buildHostPluginStore).
	PluginsStore PluginStore

	// PluginsPath is the absolute path the
	// production FSPluginStore loads from /
	// saves to. Empty → the helper at
	// buildHostPluginStore defaults to
	// auth.AntigravityHome()/plugins.json.
	// Cycle rc-plugins adds this.
	PluginsPath string

	// TriggersStore is the seam the five trigger
	// methods (ListRemoteAgentTriggers /
	// ListRemoteAgentTriggerOptions /
	// CreateRemoteAgentTrigger /
	// UpdateRemoteAgentTrigger /
	// DeleteRemoteAgentTrigger, cycle rc-triggers)
	// call into. nil → Start wires nothing into
	// Config.TriggersStore, and the dispatcher
	// fabricates a fresh empty MemTriggerStore.
	// Production passes an FSTriggerStore rooted
	// at $ANTIGRAVITY_HOME/triggers.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor — buildHostTriggerStore).
	TriggersStore TriggerStore

	// TriggersPath is the absolute path the
	// production FSTriggerStore loads from /
	// saves to. Empty → the helper at
	// buildHostTriggerStore defaults to
	// auth.AntigravityHome()/triggers.json.
	// Cycle rc-triggers adds this.
	TriggersPath string

	// FastpushStore is the seam the single
	// SetRemoteAgentFastpushEnabled method
	// (cycle rc-fastpush-toggle) calls into.
	// nil → Start wires nothing into
	// Config.FastpushStore, and the dispatcher
	// fabricates a fresh empty MemFastpushStore.
	// Production passes an FSFastpushStore
	// rooted at
	// $ANTIGRAVITY_HOME/fastpush.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor — buildHostFastpushStore).
	//
	// Cycle rc-fastpush-toggle adds this.
	FastpushStore FastpushStore

	// FastpushPath is the absolute path the
	// production FSFastpushStore loads from /
	// saves to. Empty → the helper at
	// buildHostFastpushStore defaults to
	// auth.AntigravityHome()/fastpush.json.
	// Cycle rc-fastpush-toggle adds this.
	FastpushPath string

	// ResourceStore is the seam the three
	// resource-management methods
	// (ListRemoteAgentResourceProviders /
	// ListRemoteAgentAvailableResources /
	// UpdateRemoteAgentProjectResources,
	// cycle rc-resources) call into. nil →
	// Start wires nothing into
	// Config.ResourceStore, and the dispatcher
	// fabricates a fresh empty MemResourceStore
	// seeded with the brief's fixture set (4
	// well-known providers + 12 well-known
	// resources). Production passes an
	// FSResourceStore rooted at
	// $ANTIGRAVITY_HOME/resources.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor — buildHostResourceStore).
	//
	// The Bind path the store exposes writes
	// back to the project's `Resources` field
	// via the ProjectStore seam (HostConfig.Projects).
	// A ResourceStore that has no Projects
	// installed returns
	// ErrResourceProjectNotFound for every Bind
	// call — the dispatcher's -32004 envelope
	// matches the cycle-9/-22 project-not-found
	// envelope so dashboards see a uniform
	// envelope regardless of which kind of
	// unknown-id they hit.
	//
	// Cycle rc-resources adds this.
	ResourceStore ResourceStore

	// ResourcePath is the absolute path the
	// production FSResourceStore loads from /
	// saves to. Empty → the helper at
	// buildHostResourceStore defaults to
	// auth.AntigravityHome()/resources.json.
	//
	// Cycle rc-resources adds this.
	ResourcePath string

	// ConversationConfigStore is the seam the
	// single UpsertRemoteAgentConversationConfig
	// method (cycle rc-conversation-config) calls
	// into. nil → Start wires nothing into
	// Config.ConversationConfigStore, and the
	// dispatcher fabricates a fresh empty
	// MemConversationConfigStore. Production
	// passes an FSConversationConfigStore rooted
	// at
	// $ANTIGRAVITY_HOME/conversation_config.json
	// (cmd/wagyu/remote_control.go owns the
	// constructor —
	// buildHostConversationConfigStore).
	//
	// The cycle is **write-only** today: the
	// single UpsertRemoteAgentConversationConfig
	// method is the only consumer; the future
	// `rc-conversation-config-read` cycle adds
	// GetRemoteAgentConversationConfig +
	// ListRemoteAgentConversationConfigs so
	// dashboards can render the post-upsert
	// state without their own copy.
	//
	// Cycle rc-conversation-config adds this.
	ConversationConfigStore ConversationConfigStore

	// ConversationConfigPath is the absolute path
	// the production FSConversationConfigStore
	// loads from / saves to. Empty → the helper
	// at buildHostConversationConfigStore
	// defaults to
	// auth.AntigravityHome()/conversation_config.json.
	//
	// Cycle rc-conversation-config adds this.
	ConversationConfigPath string

	// TerminalsRegistry is the seam the five
	// RemoteAgentTerminal* methods (cycle
	// rc-terminals) call into. nil → Start
	// wires nothing into Config.TerminalsRegistry
	// and the dispatcher fabricates a fresh
	// empty TerminalsRegistry. Production passes
	// an in-memory registry (the cycle is
	// persistence-only in spirit; the brief pins
	// the data model without an on-disk file).
	//
	// The host-side helper at
	// buildHostTerminalsRegistry (cycle
	// rc-terminals) constructs the standard
	// in-memory registry; cmd/wagyu/remote_control.go
	// owns the constructor.
	//
	// Cycle rc-terminals adds this.
	TerminalsRegistry *TerminalsRegistry

	// WebTerminals is the seam the Web UI's terminal methods
	// (CreateTerminal, ListTerminals, SendTerminalInput,
	// StreamTerminalOutput, CloseTerminal) call into. nil →
	// Start wires nothing into Config.WebTerminals and the
	// dispatcher answers "terminals are not wired". Production
	// passes a registry whose terminals are live shells on a
	// pty.
	//
	// This is a *different* surface from TerminalsRegistry: the
	// dashboard's RemoteAgentTerminal* contract stays as it is.
	//
	// Cycle rc-web-terminals adds this.
	WebTerminals *rcterminal.Registry

	// WebAudio is the seam the Web UI's voice methods
	// (StreamAudioTranscription, SendAudioChunk, EndAudioSession)
	// call into. nil → Start wires nothing into Config.WebAudio
	// and the dispatcher answers "audio is not wired". Production
	// passes a registry whose sessions transcribe with the Gemini
	// transport.
	//
	// Cycle rc-web-terminals adds this.
	WebAudio *rcaudio.Registry

	// OriginPolicyStore is the seam the
	// single
	// SetRemoteControlOriginPatterns
	// method (cycle rc-auth-tokens)
	// calls into. nil → Start wires
	// nothing into Config.OriginPolicyStore
	// and the dispatcher fabricates a
	// fresh empty MemOriginPolicyStore.
	// Production passes an
	// FSOriginPolicyStore rooted at
	// $ANTIGRAVITY_HOME/origin_policy.json
	// (cmd/wagyu/remote_control.go owns
	// the constructor —
	// buildHostOriginPolicyStore).
	//
	// Cycle rc-auth-tokens adds this.
	OriginPolicyStore OriginPolicyStore

	// OriginPolicyPath is the absolute
	// path the production
	// FSOriginPolicyStore loads from /
	// saves to. Surfaced on HostConfig so
	// a startup check can read it without
	// fabricating a store. "" — the
	// default — means "use
	// auth.AntigravityHome() +
	// "origin_policy.json"" at
	// NewFSOriginPolicyStore time.
	//
	// Cycle rc-auth-tokens adds this.
	OriginPolicyPath string

	// DeferredFeatures is the
	// forward-compat map (see Config for
	// the full key list) the host can
	// override at Start time. nil →
	// Start wires nothing into
	// Config.DeferredFeatures and the
	// dispatcher's topLevelStatus helper
	// treats the absent map as the
	// canonical all-false default (so
	// dashboards always see the four
	// keys, never a missing envelope).
	//
	// Production wiring leaves this nil
	// (the all-false default is exactly
	// what the branch ships today); a
	// future rc-webrtc-mesh /
	// rc-hub-server / rc-persistentrc-linux
	// / rc-enterprise-tunnel cycle would
	// install the relevant key here.
	//
	// Cycle rc-webrtc-mesh-defer adds
	// this.
	DeferredFeatures map[string]bool
}

// StartHostSession brings up the daemon with the production wiring
// (ConvstoreOutputReader, DefaultAgentLister, /tmp socket path).
// Returns the *Daemon or an error.
//
// StartHostSession does NOT block on the accept loop. The caller
// is expected to defer d.Shutdown() and to print the socket path
// to the operator so a dashboard knows where to connect.
//
// Field propagation: the hc.Backend / hc.Model / hc.Workspace
// fields are *set on the SessionState* the caller passed in (when
// non-empty), so the dashboard sees the resolved values rather
// than the zero values from NewSessionState. A pre-populated
// SessionState is the caller's signal that they already know the
// values — in that case the helper does not overwrite them, on
// the principle that the host is closer to the source of truth
// than the helper is.
func StartHostSession(ctx context.Context, hc HostConfig) (*Daemon, error) {
	applyHostSessionFields(&hc)
	sock, err := resolveHostSocketPath(hc)
	if err != nil {
		return nil, err
	}
	d, err := Start(ctx, ConfigFromHost(hc, sock))
	if err != nil {
		return nil, err
	}
	if hc.Out != nil {
		fmt.Fprintf(hc.Out, "Remote control daemon listening on: %s\n", d.SocketPath())
	}
	return d, nil
}
