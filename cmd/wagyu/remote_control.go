// Package main — remote_control.go: --remote-control daemon wiring.
//
// AGY 1.2.16 changelog: "Added the `--remote-control` daemon flag that
// exposes a small set of MCP tools (status, list_agents, get_output)
// over a Unix socket so external dashboards can read live session
// state."
//
// Cycle rc-ws-transport: the wire protocol + tool handlers live in
// internal/remotecontrol (stdlib-only, no imports of this package, so
// the daemon is unit testable). The transport layer lives in
// internal/remotetransport. Two host-side helpers:
//
//   - maybeStartRemoteControlDaemon(fs, sess) → *remotecontrol.Daemon
//     Boots the Unix-socket daemon when --remote-control is set; the
//     legacy per-turn shape that cmd/wagyu/main.go has called since
//     cycle 230. Returns nil and a clean stderr message when the flag
//     is unset. Errors surface as a single stderr line — the host
//     does not crash when the daemon fails to bind.
//
//   - maybeStartRemoteControlTCPDaemon(fs, sess) → *remotetransport.TCPServer
//     Boots the TCP+WebSocket daemon when --remote-control-tcp=<addr>
//     is set. Empty value disables. Same error reporting.
//
// Both helpers share the same SessionState, so a dashboard sees the
// resolved model / turn output regardless of which transport the
// client dialed. Status tool's connected counter is incremented by
// both transports' accept loops.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/originpolicy"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotetransport"
)

// remoteControlOnce guards the package-level daemon pointer so a
// double-call (rare but possible from --continue + --remote-control
// re-entrancy) does not bind two daemons on the same socket.
//
// Currently unused: each invocation of runPrintMode owns its own
// daemon, which lives for the duration of one turn. Kept as a
// placeholder so the file's role ("host wiring for a long-lived
// daemon") is obvious to readers who grep for `var remoteControl`.
var remoteControlOnce sync.Once

// maybeStartRemoteControlDaemon brings up the Unix-socket daemon
// when --remote-control is set and returns the running *Daemon. When
// the flag is absent it returns nil so callers can no-op the
// shutdown via shutdownRemoteControlDaemon.
//
// The session pointer is what the host will update throughout the
// turn (model label, conversation id, turn output). The daemon
// reads these under its own RLock; sharing the pointer is what
// makes the dashboard see the same state the user sees.
//
// Returns nil when --remote-control-tcp is also set: cycle
// rc-ws-transport says the WS adapter, when active, takes over from
// the Unix-socket adapter. The Unix-socket adapter is the default
// for the per-turn --remote-control flag, but the WS adapter wins
// when both flags are present.
//
// Cycle rc-webrtc-mesh-defer: --experimental-rc-v2 is a
// forward-compat opt-in to the deferred v2 transport features
// (WebRTC mesh + Antigravity Hub; parked — see
// docs/deferred-rc-transport.md). The flag is read here so it
// round-trips without error; today it is a no-op (the deferred
// features stay disabled in this branch) and the
// RemoteControlStatus `deferred_features` envelope is the canonical
// signal a dashboard should poll instead. A warning is logged
// once if the user explicitly opts in to remind them they're on
// the legacy TCP+WS path.
func maybeStartRemoteControlDaemon(ctx context.Context, fs *flag.FlagSet, sess *remotecontrol.SessionState) *remotecontrol.Daemon {
	if !flagBoolValue(fs, "remote-control") {
		return nil
	}
	if flagStringValue(fs, "remote-control-tcp") != "" {
		// The WS adapter takes over; the Unix-socket listener
		// would compete for the same --remote-control meaning.
		fmt.Fprintf(os.Stderr, "[remote-control] --remote-control-tcp is set; ignoring --remote-control's Unix-socket listener\n")
		return nil
	}
	// Cycle rc-webrtc-mesh-defer: --experimental-rc-v2 is accepted
	// (parse clean, no error) but a no-op today. The flag exists
	// for forward compat: a future rc-webrtc-mesh cycle that
	// wires the WebRTC mesh transport would gate the v2 path
	// behind this flag (or behind the RemoteControlStatus
	// `deferred_features.webrtc_mesh` entry flipping to true).
	// Reading it here exercises the parser; we deliberately do
	// NOT promote it to a runtime behaviour change.
	if flagBoolValue(fs, "experimental-rc-v2") {
		fmt.Fprintf(os.Stderr, "[remote-control] --experimental-rc-v2 is set; the deferred v2 features (WebRTC mesh / Antigravity Hub) are not enabled in this branch — see docs/deferred-rc-transport.md\n")
	}
	cwd, _ := os.Getwd()
	// Mirror the resolved socket path so the daemon
	// block on the rich-status tool surfaces it.
	// rc-status-tool pins the daemon.addr format
	// as "<socket-path>". When the user supplied
	// $OPENAGY_RC_SOCKET the StartHostSession
	// helper applies the env override; we
	// deliberately use the resolved flag value
	// here so a dashboard sees the path the host
	// requested at parse time. The actual bound
	// path is recoverable from the daemon's
	// SocketPath() at runtime.
	sockPath := flagStringValue(fs, "remote-control-socket")
	d, err := remotecontrol.StartHostSession(ctx, remotecontrol.HostConfig{
		Version:        cli.ReferenceVersion,
		Backend:        "", // filled in by resolveBackend below
		Model:          flagStringValue(fs, "model"),
		Workspace:      cwd,
		Session:        sess,
		SocketPath:     sockPath, // rc-polish (G13): flag now flows into the binding path; $OPENAGY_RC_SOCKET still wins inside StartHostSession
		Out:            os.Stderr,
		ListBackend:    buildHostListBackend(),
		FS:             buildHostFSProvider(fs, cwd),
		Projects:       buildHostProjectStore(buildHostFSProvider(fs, cwd)),
		Conversations:  buildHostConversationStore(buildHostProjectStore(buildHostFSProvider(fs, cwd))),
		AuthState:      authStateForCaller(),
		DaemonMeta:     daemonMetaForCaller("unix", sockPath, "in-process"),
		MessageBackend: buildHostMessageBackend(cwd, flagStringValue(fs, "model")),
		// Cycle rc-permissions-grants: every Set /
		// Add / Remove lands on the
		// $ANTIGRAVITY_HOME/permissions.json the
		// helper builds. Without this wiring the
		// dispatcher fabricates a fresh
		// MemPermissionStore at NewDispatcher
		// time, which means the Unix-socket daemon
		// would silently drop every change the
		// dashboard issued.
		PermissionsStore: buildHostPermissionStore(),
		// Cycle rc-plugins: the Unix-socket daemon
		// uses the same $ANTIGRAVITY_HOME/plugins.json
		// store as the WS daemon. The broadcaster
		// registered by NewTCPServer fans
		// "plugins/changed" notifications to every
		// open ws conn (the same seam
		// rc-permissions-grants added for the
		// permissions/changed broadcast).
		PluginsStore: buildHostPluginStore(),
		// Cycle rc-triggers: the Unix-socket
		// daemon uses the same
		// $ANTIGRAVITY_HOME/triggers.json store as
		// the WS daemon. The broadcaster registered
		// by NewTCPServer fans "triggers/changed"
		// notifications to every open ws conn (the
		// same BroadcastNotify seam
		// rc-permissions-grants added).
		TriggersStore: buildHostTriggerStore(),
		// Cycle rc-fastpush-toggle: the
		// Unix-socket daemon uses the same
		// $ANTIGRAVITY_HOME/fastpush.json store
		// as the WS daemon. The broadcaster
		// registered by NewTCPServer fans
		// "fastpush/setting_changed"
		// notifications to every open ws conn
		// (the same BroadcastNotify seam
		// rc-permissions-grants added).
		FastpushStore: buildHostFastpushStore(),
		// Cycle rc-resources: the Unix-socket
		// daemon uses the same
		// $ANTIGRAVITY_HOME/resources.json store
		// as the WS daemon. The broadcaster
		// registered by NewTCPServer fans
		// "resources/changed" notifications to
		// every open ws conn (the same
		// BroadcastNotify seam
		// rc-permissions-grants added; shared
		// with rc-plugins, rc-triggers, and
		// rc-fastpush-toggle).
		//
		// The Bind path writes back to the
		// project's `Resources` field via the
		// bound ProjectStore. We pass the same
		// *FSProjectStore buildHostProjectStore
		// builds so the Unix-socket daemon and
		// the WS daemon share the on-disk
		// project file the Bind path mutates.
		ResourceStore: buildHostResourceStore(buildHostProjectStore(buildHostFSProvider(fs, cwd))),
		// Cycle rc-conversation-config: the
		// Unix-socket daemon uses the same
		// $ANTIGRAVITY_HOME/conversation_config.json
		// store as the WS daemon. The broadcaster
		// registered by NewTCPServer fans
		// "conversation_config/changed"
		// notifications to every open ws conn (the
		// same BroadcastNotify seam
		// rc-permissions-grants added; shared with
		// rc-plugins, rc-triggers,
		// rc-fastpush-toggle, rc-resources).
		//
		// The cycle is write-only: the single
		// UpsertRemoteAgentConversationConfig
		// method is the only consumer; the future
		// `rc-conversation-config-read` cycle adds
		// the read methods so dashboards can
		// render the post-upsert state without
		// their own copy.
		ConversationConfigStore: buildHostConversationConfigStore(),
		// Cycle rc-terminals: the in-memory terminals
		// registry shared across all three transports
		// (Unix-socket per-turn, TCP+WS LaunchAgent,
		// foreground TCP+WS). The five
		// RemoteAgentTerminal* methods route through
		// this single registry; buildHostTerminalsRegistry
		// is a process-singleton so a terminal
		// created via one transport is visible on
		// every other transport. Mirrors the
		// buildHostMessageBackend / buildHostProjectStore
		// pattern — production wiring always
		// installs a registry, dispatcher's
		// nil-fabrication in NewDispatcher handles
		// the tests.
		TerminalsRegistry: buildHostTerminalsRegistry(),
		// Cycle rc-auth-tokens: the
		// Unix-socket daemon uses the same
		// $ANTIGRAVITY_HOME/origin_policy.json
		// store as the WS daemon. The
		// SetRemoteControlOriginPatterns handler
		// fans an origin_policy/changed
		// notification to every open ws
		// conn via the existing
		// (*Dispatcher).BroadcastNotify seam
		// (shared with rc-permissions-grants
		// ... rc-terminals); the Unix-socket
		// transport doesn't have a ws
		// connection so the broadcast fans to
		// no one, but the store mutation
		// still lands and the response
		// envelope echoes the post-state.
		//
		// The Unix-socket daemon doesn't
		// register an originPolicySetter
		// (the Unix-socket transport
		// doesn't accept an Origin:
		// header — it's a raw net.Conn, not
		// a websocket.Upgrade). The
		// dispatcher's handler tolerates a
		// nil setter; the response envelope
		// always carries the post-state.
		OriginPolicyStore: buildHostOriginPolicyStore(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control] daemon failed to start: %v\n", err)
		return nil
	}
	return d
}

// daemonMetaForCaller returns the DaemonMeta block a host
// wires into Config.DaemonMeta before the dispatcher brings
// up. Cycle rc-status-tool: the fields come from the host's
// own resolution path so a per-turn run reflects the
// transport it actually picked (--remote-control → unix,
// --remote-control-tcp → tcp+ws) and "in-process" mode
// (LaunchAgent mode is "persistent"; see
// cmd/wagyu/remote_control_foreground.go).
//
// caller determines the lifecycle label. Today the
// per-turn callers both pass "in-process" because the
// daemon dies with the turn; the foreground path passes
// "persistent". A future cycle that wants finer
// granularity (e.g. "ephemeral" for the print-mode-only
// path) extends this signature.
func daemonMetaForCaller(transport, addr, mode string) *remotecontrol.DaemonMeta {
	return &remotecontrol.DaemonMeta{
		Transport: transport,
		Addr:      addr,
		Mode:      mode,
	}
}

// maybeInstallDaemonMeta wires DaemonMeta onto a freshly-built
// dispatcher Config. Used by both per-turn paths (the helper
// in maybeStartRemoteControlDaemon builds the dispatcher
// directly under --remote-control-tcp). The per-turn
// --remote-control path (Unix-socket) doesn't reach this —
// StartHostSession installs DaemonMeta via the bridge inside
// remotecontrol.Start (cycle rc-status-tool).
//
// cycle rc-status-tool adds this.
func maybeInstallDaemonMeta(cfg *remotecontrol.Config, transport, addr, mode string) {
	if cfg == nil {
		return
	}
	cfg.DaemonMeta = daemonMetaForCaller(transport, addr, mode)
}

// resolvedAuthMethodForCaller decides which auth method the
// host should report on the rich-status tool. Mirrors the
// 1.3.3 / 1.3.4 rule for $AGY_ADC_AUTH: the openagy build
// doesn't ship a real ADC impl, so this is "shim" until
// rc-auth-tokens / rc-auth-adc land. Production wires the
// real path via cli.ResolveBackendLabel() in a future
// cycle; today the helper is the seam.
func resolvedAuthMethodForCaller() string {
	return remotecontrol.AuthMethodShim
}

// authStateForCaller returns a fresh AuthState seeded with
// the host's resolved auth method. Used by the per-turn
// --remote-control-tcp path's dispatcher initialiser so
// the very first status call already shows the host's
// method (the SetRemoteControlSession RPC mutates it
// later when a dashboard presents its own session id).
func authStateForCaller() *remotecontrol.AuthState {
	return remotecontrol.NewAuthState(
		resolvedAuthMethodForCaller(),
		"", "", nil, time.Time{},
	)
}

// maybeStartRemoteControlTCPDaemon brings up the TCP+WebSocket
// daemon when --remote-control-tcp=<addr> is set and returns the
// running *remotetransport.TCPServer. When the flag is unset or
// empty it returns nil.
//
// The returned server's Addr() is the bound address (especially
// important when the original config was "127.0.0.1:0" — the
// OS-assigned port lives in Addr()). The host prints that on
// stderr so an operator knows where to point a ws client.
//
// shutdownRemoteControlTCPDaemon is the symmetric teardown helper.
func maybeStartRemoteControlTCPDaemon(ctx context.Context, fs *flag.FlagSet, sess *remotecontrol.SessionState) *remotetransport.TCPServer {
	addr := flagStringValue(fs, "remote-control-tcp")
	if addr == "" {
		return nil
	}
	cwd, _ := os.Getwd()
	// Build the dispatcher with the same Config the Unix-socket
	// path uses — that way both transports see identical
	// SessionState / Agents / Reader semantics. We deliberately
	// don't call StartHostSession here because the WS adapter
	// doesn't bind a Unix socket.
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:        sess,
		Agents:         remotecontrol.DefaultAgentLister,
		Reader:         remotecontrol.ConvstoreOutputReader{},
		ListBackend:    buildHostListBackend(),
		FS:             buildHostFSProvider(fs, cwd),
		Projects:       buildHostProjectStore(buildHostFSProvider(fs, cwd)),
		Conversations:  buildHostConversationStore(buildHostProjectStore(buildHostFSProvider(fs, cwd))),
		AuthState:      authStateForCaller(),
		DaemonMeta:     daemonMetaForCaller("tcp+ws", addr, "in-process"),
		MessageBackend: buildHostMessageBackend(cwd, flagStringValue(fs, "model")),
		// Cycle rc-permissions-grants: the WS
		// daemon uses the same
		// $ANTIGRAVITY_HOME/permissions.json
		// store as the Unix-socket daemon. The
		// broadcaster registered by NewTCPServer
		// fans "permissions/changed" notifications
		// to every open ws conn (cycle
		// rc-permissions-grants seam).
		PermissionsStore: buildHostPermissionStore(),
		// Cycle rc-plugins: the WS
		// daemon uses the same
		// $ANTIGRAVITY_HOME/plugins.json store as
		// the Unix-socket daemon. The broadcaster
		// registered by NewTCPServer fans
		// "plugins/changed" notifications to every
		// open ws conn (the same BroadcastNotify
		// seam rc-permissions-grants added).
		PluginsStore: buildHostPluginStore(),
		// Cycle rc-triggers: the WS
		// daemon uses the same
		// $ANTIGRAVITY_HOME/triggers.json store as
		// the Unix-socket daemon. The broadcaster
		// registered by NewTCPServer fans
		// "triggers/changed" notifications to every
		// open ws conn (the same BroadcastNotify
		// seam rc-permissions-grants added).
		TriggersStore: buildHostTriggerStore(),
		// Cycle rc-fastpush-toggle: the WS
		// daemon uses the same
		// $ANTIGRAVITY_HOME/fastpush.json store
		// as the Unix-socket daemon. The
		// broadcaster registered by NewTCPServer
		// fans "fastpush/setting_changed"
		// notifications to every open ws conn
		// (the same BroadcastNotify seam
		// rc-permissions-grants added).
		FastpushStore: buildHostFastpushStore(),
		// Cycle rc-resources: the WS daemon
		// uses the same
		// $ANTIGRAVITY_HOME/resources.json
		// store as the Unix-socket daemon. The
		// broadcaster registered by NewTCPServer
		// fans "resources/changed" notifications
		// to every open ws conn (the same
		// BroadcastNotify seam
		// rc-permissions-grants added; shared
		// with rc-plugins, rc-triggers, and
		// rc-fastpush-toggle).
		//
		// The Bind path writes back to the
		// project's `Resources` field via the
		// bound ProjectStore. We pass the same
		// *FSProjectStore buildHostProjectStore
		// builds so the WS daemon and the
		// Unix-socket daemon share the on-disk
		// project file the Bind path mutates.
		ResourceStore: buildHostResourceStore(buildHostProjectStore(buildHostFSProvider(fs, cwd))),
		// Cycle rc-conversation-config: the WS
		// daemon uses the same
		// $ANTIGRAVITY_HOME/conversation_config.json
		// store as the Unix-socket daemon. The
		// broadcaster registered by NewTCPServer
		// fans "conversation_config/changed"
		// notifications to every open ws conn (the
		// same BroadcastNotify seam
		// rc-permissions-grants added; shared
		// with rc-plugins, rc-triggers,
		// rc-fastpush-toggle, rc-resources).
		//
		// The cycle is write-only: the single
		// UpsertRemoteAgentConversationConfig
		// method is the only consumer; the future
		// `rc-conversation-config-read` cycle adds
		// the read methods so dashboards can
		// render the post-upsert state without
		// their own copy.
		ConversationConfigStore: buildHostConversationConfigStore(),
		// Cycle rc-terminals: same process-singleton
		// registry the Unix-socket daemon uses, so
		// a terminal the dashboard creates on either
		// transport is visible on the other. The
		// helper memoises a TerminalsRegistry once
		// per process; buildHostTerminalsRegistry
		// returns the same pointer every time.
		TerminalsRegistry: buildHostTerminalsRegistry(),
		// Cycle rc-auth-tokens: the WS
		// daemon uses the same
		// $ANTIGRAVITY_HOME/origin_policy.json
		// store as the Unix-socket daemon.
		// The handler invokes the
		// dispatcher's originPolicySetter
		// after a successful SetPatterns so
		// the WS transport's active
		// OriginPolicy mirrors the on-disk
		// state — an immediately-following
		// Accept sees the new patterns.
		// The setter is registered below
		// (disp.SetOriginPolicySetter(srv.SetOriginPolicy))
		// after NewTCPServer returns.
		OriginPolicyStore: buildHostOriginPolicyStore(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control-tcp] dispatcher init failed: %v\n", err)
		return nil
	}
	// Cycle rc-auth-tokens: seed the WS
	// transport with the canonical
	// production allowlist at startup so
	// the very first Accept already sees
	// the brief-pinned defaults (or the
	// env-var override). The
	// dispatcher-side setter keeps the
	// policy mirrored into the transport
	// after every
	// SetRemoteControlOriginPatterns RPC;
	// the seed step below covers the
	// brief's "first Accept sees the
	// canonical allowlist" guarantee.
	seed := originpolicy.LoadOriginPolicyFromEnv()
	srv, err := remotetransport.NewTCPServer(ctx, disp, remotetransport.TCPConfig{
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control-tcp] daemon failed to start on %s: %v\n", addr, err)
		return nil
	}
	// Cycle rc-auth-tokens: register the
	// transport's SetOriginPolicy as the
	// dispatcher's originPolicySetter so
	// SetRemoteControlOriginPatterns
	// mutations mirror into the WS
	// transport's active policy. Safe to
	// call after NewTCPServer returns —
	// the dispatcher's hot path always
	// checks the setter under the read
	// lock and tolerates a nil setter
	// (the Unix-socket path doesn't
	// register one).
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	fmt.Fprintf(os.Stderr, "[remote-control-tcp] ws daemon listening on: %s\n", srv.HTTPURL())
	return srv
}

// bindRemoteControlSession wires a host's already-running session
// into a started daemon. After bind, every SetTurnOutput /
// SetConversationID / SetModel call on sess is reflected through
// the daemon's status and get_output tools.
//
// bindRemoteControlSession is a thin helper rather than baked into
// maybeStartRemoteControlDaemon so that a caller (the runPrintMode
// hot path) can defer the wiring until after the model and
// conversation have been resolved — that way the dashboard sees
// the resolved model, not the raw flag value.
func bindRemoteControlSession(d *remotecontrol.Daemon, sess *remotecontrol.SessionState) {
	if d == nil || sess == nil {
		return
	}
	// Pre-populate fields the host already knows at start time.
	if cwd, err := os.Getwd(); err == nil {
		sess.Workspace = cwd
	}
}

// shutdownRemoteControlDaemon is the symmetric teardown for the
// Unix-socket daemon. The Daemon's Shutdown is idempotent so
// callers can defer this from multiple paths safely.
func shutdownRemoteControlDaemon(d *remotecontrol.Daemon, w io.Writer) {
	if d == nil {
		return
	}
	d.Shutdown()
	if w != nil {
		fmt.Fprintln(w, "Remote control daemon stopped.")
	}
}

// shutdownRemoteControlTCPDaemon is the symmetric teardown for the
// TCP+WebSocket daemon. Idempotent — safe to defer from multiple
// paths. Uses a 5-second shutdown timeout (the same cap
// remotetransport.TCPServer.Shutdown applies internally) so a
// stuck ws client can't hold shutdown indefinitely.
func shutdownRemoteControlTCPDaemon(srv *remotetransport.TCPServer, w io.Writer) {
	if srv == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(w, "[remote-control-tcp] shutdown: %v\n", err)
		return
	}
	fmt.Fprintln(w, "Remote control tcp daemon stopped.")
}

// buildHostMessageBackend returns the production
// `remotecontrol.MessageBackend` the dispatcher's
// Config.MessageBackend field gets. Wired here, in
// cmd/wagyu/, because the factory needs the host's
// backend.Client + workspace + agent home, and the
// dispatcher package shouldn't depend on those directly.
//
// Cycle rc-piagent-bind: this is the production
// MessageBackend that replaces the seeded *StubBackend
// on first Send. The factory mints a fresh *pigrt.Runtime
// per Send call (one runtime per turn) — see
// internal/piagentbridge/piagent_remote.go for the full
// turn-driven surface.
//
// modelName is the host's resolved model label; empty
// falls back to the wire model of the resolved backend so
// a dashboard sees the same wire model the print path
// sends. workspace is captured at daemon start so a
// reconnect doesn't drift; agentDir is the agy config
// home (auth.AntigravityHome()).
func buildHostMessageBackend(workspace, modelName string) remotecontrol.MessageBackend {
	be := pickBackendWithLog(nil, modelName, io.Discard, backend.AgwTimeoutUnlimited)
	return &remotecontrol.PiagentBackend{
		SessionFactory: piagentbridge.NewRemoteCascadeSession(
			piagentbridge.RemoteCascadeOptions{
				Backend:   be,
				Workspace: workspace,
				AgentDir:  auth.AntigravityHome(),
			},
		),
	}
}

// buildHostListBackend returns the production ListBackend the
// daemon's four read-only handlers call into. Wired here, in
// cmd/wagyu/, because the closures need to reach into
// internal/cli, internal/convstore, internal/auth and stdlib
// (os.Hostname / runtime.GOOS / runtime.GOARCH), and a
// dispatcher package shouldn't import those just to wire a
// hook.
//
// Field semantics:
//
//   - Models:           reads cli.ReferenceModels(), classified
//     by backend label (the host uses
//     $OPENAGY_BACKEND; defaults to "agw").
//     The returned first row is marked default.
//   - SlashCommands:    returns remotecontrol.DefaultSlashCommands
//     (the catalogue lives in the dispatcher
//     package — the dispatcher picked it up
//     because the catalogue is built into
//     the runtime, not configured per host).
//   - Conversations:    projects convstore.ListConversations()
//     rows to ConversationInfo; honours the
//     (limit, before) cursor the dashboard
//     sent.
//   - Environments:     stdlib-only — os.Hostname, runtime.GOOS
//     / GOARCH, os.Getwd, $USER / $SHELL,
//     auth.AntigravityHome(), cli.ReferenceVersion.
//     env_keys_present is built from
//     remotecontrol.DefaultEnvKeyProbe.
func buildHostListBackend() remotecontrol.ListBackend {
	defaultModel := defaultBackendLabel()
	return remotecontrol.ListBackend{
		Models: func(backend, filter string) ([]remotecontrol.ModelInfo, error) {
			// backend label filter defaults to the host's
			// value when the dashboard sent ""; explicit
			// "agw" / "shim" go through unclobbered.
			chosen := strings.TrimSpace(backend)
			if chosen == "" {
				chosen = defaultModel
			}
			chosen = strings.ToLower(chosen)
			entries := cli.ReferenceModels()
			if len(entries) == 0 {
				return remotecontrol.DefaultModelInfo, nil
			}
			out := make([]remotecontrol.ModelInfo, 0, len(entries))
			firstForBackend := true
			for _, e := range entries {
				out = append(out, remotecontrol.ModelInfo{
					ID:              e.ID,
					Label:           e.Label,
					Backend:         chosen,
					EffortSupported: true,
					Default:         firstForBackend,
				})
				firstForBackend = false
			}
			return out, nil
		},
		SlashCommands: func() ([]remotecontrol.SlashCommandInfo, error) {
			// The dispatcher falls back to
			// DefaultSlashCommands when this closure
			// returns nil. We return a copy so a future
			// cycle that wants to filter per-host can do
			// so without mutating the package-level
			// catalogue.
			all := remotecontrol.DefaultSlashCommands
			out := make([]remotecontrol.SlashCommandInfo, len(all))
			copy(out, all)
			return out, nil
		},
		Conversations: func(limit int, before time.Time) ([]remotecontrol.ConversationInfo, time.Time, error) {
			raw, err := convstore.ListConversations()
			if err != nil {
				return nil, time.Time{}, err
			}
			// Cursor: skip rows whose ModTime is at-or-after
			// `before` so the dashboard pages backward
			// through history. `before.IsZero()` ⇒ no
			// filtering (newest page).
			filtered := make([]convstore.ConversationMeta, 0, len(raw))
			for _, c := range raw {
				if !before.IsZero() && !c.ModTime.Before(before) {
					continue
				}
				filtered = append(filtered, c)
			}
			if limit <= 0 || limit > len(filtered) {
				limit = len(filtered)
			}
			page := filtered[:limit]
			out := make([]remotecontrol.ConversationInfo, 0, len(page))
			var oldest time.Time
			for _, c := range page {
				out = append(out, remotecontrol.ConversationInfo{
					ID:        c.ID,
					Title:     c.Title,
					CreatedAt: c.ModTime.UTC().Format(time.RFC3339Nano),
					UpdatedAt: c.ModTime.UTC().Format(time.RFC3339Nano),
					Model:     "",
				})
				if oldest.IsZero() || c.ModTime.Before(oldest) {
					oldest = c.ModTime
				}
			}
			// Empty cursor (no more pages) when the page
			// didn't fill up; the dashboard knows.
			if len(page) < limit {
				oldest = time.Time{}
			}
			return out, oldest, nil
		},
		Environments: func() (remotecontrol.EnvironmentInfo, error) {
			cwd, _ := os.Getwd()
			shell := strings.TrimSpace(os.Getenv("SHELL"))
			user := strings.TrimSpace(os.Getenv("USER"))
			if user == "" {
				user = strings.TrimSpace(os.Getenv("LOGNAME"))
			}
			keys := remotecontrol.DefaultEnvKeyProbe
			present := make([]string, 0, len(keys))
			seen := make(map[string]bool, len(keys))
			for _, k := range keys {
				if seen[k] {
					continue
				}
				seen[k] = true
				if v := os.Getenv(k); v != "" {
					present = append(present, k)
				}
			}
			sort.Strings(present)
			return remotecontrol.EnvironmentInfo{
				OS:              runtime.GOOS,
				Arch:            runtime.GOARCH,
				Hostname:        hostnameOrEmpty(),
				Cwd:             cwd,
				User:            user,
				Shell:           shell,
				AntigravityHome: auth.AntigravityHome(),
				Version:         cli.ReferenceVersion,
				EnvKeysPresent:  present,
				Capabilities: map[string]any{
					"remote_control": true,
				},
			}, nil
		},
	}
}

// defaultBackendLabel returns the configured backend label
// for the model catalog, mirroring the dispatcher's own
// defaultBackendLabel helper. The duplication is intentional
// — internal/remotecontrol/lists.go cannot import
// cmd/wagyu/ (an import direction reversal), so the helper
// lives here for the closure and there for the dispatcher.
func defaultBackendLabel() string {
	if v := strings.TrimSpace(os.Getenv("OPENAGY_BACKEND")); v != "" {
		return strings.ToLower(v)
	}
	return "agw"
}

// hostnameOrEmpty returns os.Hostname's value, or an empty
// string on error (an empty string is more useful than a
// synthetic fallback for a "what host am I talking to"
// dashboard probe).
func hostnameOrEmpty() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

// buildHostFSProvider returns the production FSProvider the
// two read-only filesystem methods (rc-file-ops) call into.
// Wired here, in cmd/wagyu/, because the scope reads from
// the same flag set the rest of the host binary uses
// (--add-dir, --sandbox) and a dispatcher package can't
// import cmd/wagyu/ (an import-direction reversal).
//
// Field semantics:
//
//   - Workspace: cwd at the moment the daemon is brought
//     up. Paths under this directory are allowed by
//     ResolvePolicy. The cwd is captured by the caller
//     rather than re-read here so the Unix-socket daemon
//     and the TCP daemon both see the same Workspace
//     value (matches the brief: "build a WorkdirScope
//     from cwd").
//
//   - Allow: --add-dir entries. Each entry is resolved
//     against Workspace at ResolvePolicy time (see
//     internal/remotecontrol/scope.go). A future cycle
//     that adds more additive dirs only has to extend
//     the slice here.
//
//   - Sandbox: --sandbox flag value. Today it has no
//     additional semantic in WorkdirScope (the scope is
//     "workspace + allow" whether sandbox is on or off);
//     a future cycle that wants the sandbox to tighten
//     the scope only has to update WorkdirScope.
func buildHostFSProvider(fs *flag.FlagSet, cwd string) remotecontrol.FSProvider {
	return &remotecontrol.WorkdirScope{
		Workspace: cwd,
		Allow:     flagStringValues(fs, "add-dir"),
		Sandbox:   flagBoolValue(fs, "sandbox"),
	}
}

// buildHostProjectStore returns the production ProjectStore
// the three project-CRUD methods (cycle rc-projects) call
// into. Wired here, in cmd/wagyu/, because the constructor
// needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import
// cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one
// helper would be heavier than the helper itself.
//
// Field semantics:
//
//   - RootDir: $ANTIGRAVITY_HOME/projects. Honours
//     $ANTIGRAVITY_HOME (which auth.AntigravityHome()
//     resolves) — falls back to $HOME/.gemini/
//     antigravity-cli/projects when the env var is
//     unset. Matches the layout convstore.StoreDir()
//     uses for its conversations/ + cache/ + brain/
//     sub-directories.
//
//   - FS: the host's filesystem-scope policy the
//     dispatcher already uses for ReadRemoteAgentFile
//
//   - ReadRemoteAgentDir (cycle rc-file-ops). Passing
//     the SAME scope lets CreateRemoteAgentProject
//     reject a workspace that's outside --add-dir or
//     the sandbox with the same -32007 "Path not
//     allowed" envelope the file ops use, so the
//     dashboard sees one filesystem-policy vocabulary
//     across both surfaces. The callers (both
//     maybeStartRemoteControl{Daemon,TCPDaemon}) pass
//     buildHostFSProvider(fs, cwd) so the project
//     surface sees the same --add-dir + sandbox
//     flags the file ops do.
//
//   - BoundChecker (cycle rc-conversation-deletions):
//     the project's Delete fail_if_bound check
//     delegates here. Production wires
//     convstore.ConversationBoundToProject so the
//     brief's "refuse when any conversation still
//     references the project" semantic goes through
//     the same convstore helpers the rest of the
//     code uses.
func buildHostProjectStore(fs remotecontrol.FSProvider) remotecontrol.ProjectStore {
	root := filepath.Join(auth.AntigravityHome(), "projects")
	return remotecontrol.NewFSProjectStore(
		root,
		fs,
		convstore.ConversationBoundToProject, // fail-if-bound check (cycle rc-conversation-deletions)
		nil,                                  // time.Now (production)
	)
}

// buildHostPermissionStore returns the production
// PermissionStore the four permission-mutation
// methods (cycle rc-permissions-grants) call into.
// Wired here, in cmd/wagyu/, because the constructor
// needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import
// cmd/wagyu/ (an import-direction reversal), and
// pulling internal/auth into internal/remotecontrol/
// for one helper would be heavier than the helper
// itself.
//
// Field semantics:
//
//   - Path: $ANTIGRAVITY_HOME/permissions.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) — falls back
//     to $HOME/.gemini/antigravity-cli/permissions.json
//     when the env var is unset. Matches the
//     antigravity-token helper's filesystem layout
//     (same directory, different filename).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
// The store's SetPreset / SetReview / AddGrants /
// RemoveGrants path is atomic (temp + rename +
// 0o600 perms) and crash-consistent — identical to
// the FSProjectStore / antigravity-token style.
// Successful mutations emit a
// "permissions/changed" notification on every open
// ws connection via
// (*Dispatcher).BroadcastNotify (the helper that
// rc-plugins will share, in the next cycle that
// ships).
func buildHostPermissionStore() remotecontrol.PermissionStore {
	root := filepath.Join(auth.AntigravityHome(), "permissions.json")
	return remotecontrol.NewFSPermissionStore(root, nil)
}

// buildHostPluginStore returns the production
// PluginStore the two plugin methods (cycle
// rc-plugins) call into. Wired here, in
// cmd/wagyu/, because the constructor needs to
// read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't
// import cmd/wagyu/ (an import-direction
// reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be
// heavier than the helper itself.
//
// Field semantics:
//
//   - Path: $ANTIGRAVITY_HOME/plugins.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) — falls
//     back to
//     $HOME/.gemini/antigravity-cli/plugins.json
//     when the env var is unset. Matches the
//     antigravity-token / permissions helpers'
//     filesystem layout (same directory,
//     different filename).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
// The store's Disconnect path is atomic (temp +
// rename + 0o600 perms) and crash-consistent —
// identical to FSPermissionStore's style.
// Successful mutations emit a "plugins/changed"
// notification on every open ws connection via
// (*Dispatcher).BroadcastNotify (the helper that
// rc-permissions-grants introduced and that this
// cycle shares; cycle rc-triggers /
// rc-conversation-config / etc. will too).
//
// Today's behaviour: the store reads the file on
// first Snapshot; if absent, it seeds the three
// DefaultPluginFixtures with today as
// ConnectedAt. This is a "fixture that reads
// from disk" pattern — production
// deployments will plug in real plugin
// lifecycle later. Cycle rc-plugins-real-bridge
// closes the gap (not yet scheduled).
func buildHostPluginStore() remotecontrol.PluginStore {
	root := filepath.Join(auth.AntigravityHome(), "plugins.json")
	return remotecontrol.NewFSPluginStore(root, nil)
}

// buildHostTriggerStore returns the production
// TriggerStore the five trigger methods (cycle
// rc-triggers) call into. Wired here, in cmd/wagyu/,
// because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a
// dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth
// into internal/remotecontrol/ for one helper would be
// heavier than the helper itself.
//
// Field semantics:
//
//   - Path: $ANTIGRAVITY_HOME/triggers.json. Honours
//     $ANTIGRAVITY_HOME (which auth.AntigravityHome()
//     resolves) — falls back to
//     $HOME/.gemini/antigravity-cli/triggers.json when
//     the env var is unset. Matches the antigravity-token
//     / permissions / plugins helpers' filesystem layout
//     (same directory, different filename).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
// The store's Create / Update / Delete path is atomic
// (temp + rename + 0o600 perms) and crash-consistent —
// identical to FSPermissionStore's / FSPluginStore's
// style. Successful mutations emit a "triggers/changed"
// notification on every open ws connection via
// (*Dispatcher).BroadcastNotify (the helper that
// rc-permissions-grants introduced and that this cycle
// shares; future cycles that fan their own "X changed"
// notifications — rc-conversation-config, rc-resources,
// rc-terminals — share the same seam).
//
// Today's behaviour: the store reads the file on first
// Snapshot; if absent, it returns an empty state
// (version=1, triggers=[]). The cycle is persistence
// only — no scheduling engine, no fsnotify watcher, no
// webhook receiver. The future `rc-triggers-runner`
// cycle wires the runner that consumes the on-disk
// state.
func buildHostTriggerStore() remotecontrol.TriggerStore {
	root := filepath.Join(auth.AntigravityHome(), "triggers.json")
	return remotecontrol.NewFSTriggerStore(root, nil)
}

// buildHostFastpushStore returns the production
// FastpushStore the single
// SetRemoteAgentFastpushEnabled method (cycle
// rc-fastpush-toggle) calls into. Wired here, in
// cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a
// dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one
// helper would be heavier than the helper itself.
//
// Field semantics:
//
//   - Path: $ANTIGRAVITY_HOME/fastpush.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) — falls back
//     to
//     $HOME/.gemini/antigravity-cli/fastpush.json
//     when the env var is unset. Matches the
//     antigravity-token / permissions / plugins /
//     triggers helpers' filesystem layout (same
//     directory, different filename).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
//   - checkCascade: a closure built from the
//     dispatcher's (*CascadeRegistry).Get so an
//     unknown cascade_id lands as
//     ErrFastpushCascadeNotFound (the dispatcher
//     maps that to -32004 "Cascade not found")
//     before the on-disk row gets created. nil
//     disables the check; today every production
//     caller wires the closure.
//
// The store's SetEnabled path is atomic (temp +
// rename + 0o600 perms) and crash-consistent —
// identical to FSPermissionStore / FSPluginStore /
// FSTriggerStore's style. Successful mutations emit
// a "fastpush/setting_changed" notification on every
// open ws connection via
// (*Dispatcher).BroadcastNotify (the helper that
// rc-permissions-grants introduced and that this
// cycle shares; future cycles that fan their own "X
// changed" notifications — rc-conversation-config,
// rc-resources, rc-terminals — share the same seam).
//
// Today's behaviour: the store reads the file on
// first Snapshot; if absent, it returns an empty
// state (version=1, settings=[]). The cycle is
// persistence only — no actual delivery path is
// wired (the wire envelope always carries
// push_delivery:"deferred"). The future
// `rc-fastpush-channel` cycle wires the per-channel
// delivery engine that consumes the on-disk state.
func buildHostFastpushStore() remotecontrol.FastpushStore {
	root := filepath.Join(auth.AntigravityHome(), "fastpush.json")
	// The checkCascade closure is wired by the
	// caller at NewDispatcher time (cmd/wagyu's
	// --remote-control paths install a
	// dispatcher-bound registry here in a
	// follow-up). For now we return a store
	// without the cascade-existence check; the
	// dispatcher's handler layer runs the same
	// check via (*CascadeRegistry).Get, so the
	// brief's -32004 envelope is preserved even
	// with a nil checkCascade. The constructor
	// path is symmetrical to
	// buildHostTriggerStore / buildHostPluginStore.
	return remotecontrol.NewFSFastpushStore(root, nil)
}

// buildHostResourceStore returns the production
// ResourceStore the three JSON-RPC methods
// (ListRemoteAgentResourceProviders /
// ListRemoteAgentAvailableResources /
// UpdateRemoteAgentProjectResources, cycle
// rc-resources) call into. Wired here, in cmd/wagyu/,
// because the constructor needs to read
// auth.AntigravityHome() (for the resources.json
// path), build a `$ANTIGRAVITY_HOME/secrets/`
// discovery closure (the `configured` flag), and
// reach into the bound ProjectStore (so the Bind
// path can write the project's `Resources` field
// back). Pulling internal/auth into
// internal/remotecontrol/ for these helpers would
// be heavier than the helpers themselves.
//
// Field semantics:
//
//   - Path: $ANTIGRAVITY_HOME/resources.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) — falls back
//     to
//     $HOME/.gemini/antigravity-cli/resources.json
//     when the env var is unset. Matches the
//     antigravity-token / permissions / plugins /
//     triggers / fastpush helpers' filesystem layout
//     (same directory, different filename).
//
//   - Projects: the cycle-9 *FSProjectStore
//     rooted at $ANTIGRAVITY_HOME/projects/ so the
//     Bind path can validate project_id and write
//     the project's `Resources` field back via
//     ProjectStore.Update. nil → every Bind returns
//     ErrResourceProjectNotFound; production wires
//     the same FSProjectStore buildHostProjectStore
//     builds. Tests inject their own ProjectStore
//     via MemProjectStore.
//
//   - checkSecretDir: a closure that probes
//     `$ANTIGRAVITY_HOME/secrets/<provider_id>` for
//     a non-empty file. The brief's "configured"
//     semantic is "the host has credentials for the
//     provider" — when the file is present (and
//     non-empty), the provider is configured. The
//     closure is invoked on every snapshot (the
//     operator may rotate the secrets between two
//     snapshots).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
// Bind's atomicity matches
// FSPermissionStore / FSPluginStore /
// FSTriggerStore / FSFastpushStore's style: temp
// file + rename + 0o600 perms + best-effort
// directory fsync. Successful mutations emit a
// "resources/changed" notification on every open
// ws connection via (*Dispatcher).BroadcastNotify
// (the helper rc-permissions-grants introduced and
// that this cycle shares with rc-plugins /
// rc-triggers / rc-fastpush-toggle).
//
// Today's behaviour: the store reads the file on
// first Snapshot; if absent, it returns the
// fixture-seeded state (4 well-known providers +
// 12 well-known resources, with each provider's
// `configured` flag derived from the secrets
// closure).
// buildHostOriginPolicyStore returns the
// production OriginPolicyStore the single
// SetRemoteControlOriginPatterns method
// (cycle rc-auth-tokens) calls into.
// Wired here, in cmd/wagyu/, because the
// constructor needs to read
// auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an
// import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/
// for one helper would be heavier than
// the helper itself.
//
// Field semantics:
//
//   - Path:
//     $ANTIGRAVITY_HOME/origin_policy.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) —
//     falls back to
//     $HOME/.gemini/antigravity-cli/origin_policy.json
//     when the env var is unset. Matches
//     the antigravity-token / permissions
//     / plugins / triggers / fastpush /
//     resources / conversation_config
//     helpers' filesystem layout (same
//     directory, different filename).
//
//   - now: nil → time.Now (UTC). Tests
//     inject a monotonic clock;
//     production never overrides.
//
// The store's SetPatterns path is atomic
// (temp + rename + 0o600 perms) and
// crash-consistent — identical to the
// FSPermissionStore / FSPluginStore /
// FSTriggerStore / FSFastpushStore /
// FSResourceStore / FSConversationConfigStore
// style. Successful mutations emit an
// origin_policy/changed notification on
// every open ws connection via
// (*Dispatcher).BroadcastNotify (the helper
// rc-permissions-grants introduced and
// that this cycle shares with
// rc-permissions-grants ... rc-terminals;
// the new method is
// `origin_policy/changed`).
//
// Today's behaviour: the store reads the
// file on first Snapshot; if absent, it
// returns an empty state (version=1,
// patterns=[]). The brief's "empty store
// means allow-all" semantic means the
// first Set call that lands on a fresh
// host is what turns the allowlist off
// (an explicit patterns=[]) or on
// (any non-empty patterns[]).
//
// Cycle rc-auth-tokens adds this.
func buildHostOriginPolicyStore() remotecontrol.OriginPolicyStore {
	root := filepath.Join(auth.AntigravityHome(), "origin_policy.json")
	return remotecontrol.NewFSOriginPolicyStore(root, nil)
}

// buildHostResourceStore returns the production
// ResourceStore the three JSON-RPC methods
// (ListRemoteAgentResourceProviders /
// ListRemoteAgentAvailableResources /
// UpdateRemoteAgentProjectResources, cycle
// rc-resources) call into. Wired here, in
// cmd/wagyu/, because the constructor needs
// to read auth.AntigravityHome() (for the
// resources.json path), build a
// `$ANTIGRAVITY_HOME/secrets/`
// discovery closure (the `configured` flag),
// and reach into the bound ProjectStore (so
// the Bind path can write the project's
// `Resources` field back). Pulling
// internal/auth into internal/remotecontrol/
// for these helpers would be heavier than
// the helpers themselves.
//
// Field semantics:
//
//   - Path: $ANTIGRAVITY_HOME/resources.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) — falls
//     back to
//     $HOME/.gemini/antigravity-cli/resources.json
//     when the env var is unset. Matches the
//     antigravity-token / permissions / plugins /
//     triggers / fastpush helpers' filesystem layout
//     (same directory, different filename).
//
//   - Projects: the cycle-9 *FSProjectStore
//     rooted at $ANTIGRAVITY_HOME/projects/ so the
//     Bind path can validate project_id and write
//     the project's `Resources` field back via
//     ProjectStore.Update. nil → every Bind returns
//     ErrResourceProjectNotFound; production wires
//     the same FSProjectStore buildHostProjectStore
//     builds. Tests inject their own ProjectStore
//     via MemProjectStore.
//
//   - checkSecretDir: a closure that probes
//     `$ANTIGRAVITY_HOME/secrets/<provider_id>` for
//     a non-empty file. The brief's "configured"
//     semantic is "the host has credentials for the
//     provider" — when the file is present (and
//     non-empty), the provider is configured. The
//     closure is invoked on every snapshot (the
//     operator may rotate the secrets between two
//     snapshots).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
// Bind's atomicity matches
// FSPermissionStore / FSPluginStore /
// FSTriggerStore / FSFastpushStore's style: temp
// file + rename + 0o600 perms + best-effort
// directory fsync. Successful mutations emit
// a "resources/changed" notification on every
// open ws connection via (*Dispatcher).BroadcastNotify
// (the helper rc-permissions-grants introduced and
// that this cycle shares with rc-plugins /
// rc-triggers / rc-fastpush-toggle).
//
// Today's behaviour: the store reads the file on
// first Snapshot; if absent, it returns the
// fixture-seeded state (4 well-known providers +
// 12 well-known resources, with each provider's
// `configured` flag derived from the secrets
// closure).
func buildHostResourceStore(projects remotecontrol.ProjectStore) remotecontrol.ResourceStore {
	root := filepath.Join(auth.AntigravityHome(), "resources.json")
	secrets := filepath.Join(auth.AntigravityHome(), "secrets")
	check := func(providerID string) bool {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			return false
		}
		path := filepath.Join(secrets, providerID)
		st, err := os.Stat(path)
		if err != nil {
			return false
		}
		return !st.IsDir() && st.Size() > 0
	}
	return remotecontrol.NewFSResourceStore(root, projects, check, nil)
}

// buildHostConversationStore returns the production
// ConversationStore the three destructive methods
// (cycle rc-conversation-deletions) call into.
// Wired here, in cmd/wagyu/, because the
// constructor's ProjectLookup closure needs to
// reach into the ProjectStore to map workspaces
// back to project ids (a dispatcher package
// shouldn't depend on the project-store shape).
//
// ProjectLookup semantics:
//
//   - Walk the projects store, comparing each
//     project's Workspace against the requested
//     directory's basename. The match is on
//     basename only (matches what
//     convstore.projectFromURIs publishes).
//   - An empty workspace → "" (not bound to any
//     project).
//   - A non-empty workspace that matches no row →
//     "" (and the future cycle that adds an
//     explicit conversation<->project_id column
//     replaces this fallback).
//
// The constructor passes the function verbatim to
// FSConversationStore's ProjectLookup field; nil is
// legal but loses the ProjectID field — the
// dispatcher tolerates both shapes (the
// project_unlinked envelope key falls back to
// false when ProjectID is empty).
func buildHostConversationStore(projects remotecontrol.ProjectStore) remotecontrol.ConversationStore {
	lookup := func(workspace string) string {
		if workspace == "" || projects == nil {
			return ""
		}
		rows, err := projects.List(nil, "", 0)
		if err != nil {
			return ""
		}
		wantBase := filepath.Base(workspace)
		for _, p := range rows {
			if filepath.Base(p.Workspace) == wantBase {
				return p.ID
			}
		}
		return ""
	}
	return remotecontrol.NewFSConversationStore(lookup)
}

// buildHostConversationConfigStore returns the
// production ConversationConfigStore the single
// UpsertRemoteAgentConversationConfig method (cycle
// rc-conversation-config) calls into. Wired here,
// in cmd/wagyu/, because the constructor needs to
// read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't
// import cmd/wagyu/ (an import-direction
// reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be
// heavier than the helper itself.
//
// Field semantics:
//
//   - Path:
//     $ANTIGRAVITY_HOME/conversation_config.json.
//     Honours $ANTIGRAVITY_HOME (which
//     auth.AntigravityHome() resolves) — falls
//     back to
//     $HOME/.gemini/antigravity-cli/conversation_config.json
//     when the env var is unset. Matches the
//     antigravity-token / permissions / plugins /
//     triggers / fastpush / resources helpers'
//     filesystem layout (same directory, different
//     filename).
//
//   - now: nil → time.Now (UTC). Tests inject a
//     monotonic clock; production never overrides.
//
// The store's Upsert path is atomic (temp + rename
// + 0o600 perms) and crash-consistent — identical
// to FSPermissionStore / FSPluginStore /
// FSTriggerStore / FSFastpushStore /
// FSResourceStore's style. Successful mutations
// emit a "conversation_config/changed" notification
// on every open ws connection via
// (*Dispatcher).BroadcastNotify (the helper that
// rc-permissions-grants introduced and that this
// cycle shares with rc-plugins / rc-triggers /
// rc-fastpush-toggle / rc-resources).
//
// Today's behaviour: the store reads the file on
// first Snapshot; if absent, it returns an empty
// state (version=1, configs={}). The cycle is
// write-only — there is no read method today
// (the future `rc-conversation-config-read` cycle
// adds Get + List so dashboards can render the
// post-upsert state without their own copy).
func buildHostConversationConfigStore() remotecontrol.ConversationConfigStore {
	root := filepath.Join(auth.AntigravityHome(), "conversation_config.json")
	return remotecontrol.NewFSConversationConfigStore(root, nil)
}

// buildHostTerminalsRegistry returns the in-memory
// TerminalsRegistry the rc-terminals five
// RemoteAgentTerminal* methods route through. Cycle
// rc-terminals.
//
// The brief pins the cycle as persistence-only in
// spirit — there is no $ANTIGRAVITY_HOME/terminals.json;
// the registry is in-memory and survives one daemon
// lifetime. A future rc-terminals-persist cycle adds an
// FSTerminalsStore mirroring the FSConversationConfigStore
// / FSPermissionStore / FSPluginStore / FSTriggerStore /
// FSFastpushStore / FSResourceStore style.
//
// Threading model: TerminalsRegistry is itself
// goroutine-safe; the dispatcher hands the registry to
// its per-method handlers and the per-cascade drain
// goroutines spawned by (*TerminalsRegistry).AttachCascade.
// Returning a fresh registry per buildHost call would
// be a memory leak; today's helper returns a singleton
// keyed off the package-level terminalsRegistryOnce so
// all three transports (Unix-socket per-turn,
// TCP+WS LaunchAgent, foreground TCP+WS) share the same
// in-memory terminal set. Mirrors the
// (*StubBackend).Interactions thread of "one fixture
// instance per process".
//
// Why a process-singleton rather than per-daemon: a
// dashboard that creates a terminal against the
// per-turn --remote-control unix socket, then upgrades
// to the --remote-control-tcp LaunchAgent path, expects
// the terminal it just created to be visible on the new
// transport. The singleton wiring makes that "just
// works" with zero per-transport bookkeeping.
func buildHostTerminalsRegistry() *remotecontrol.TerminalsRegistry {
	terminalsRegistryOnce.Do(func() {
		terminalsRegistrySingleton = remotecontrol.NewTerminalsRegistry()
	})
	return terminalsRegistrySingleton
}

var (
	terminalsRegistryOnce      sync.Once
	terminalsRegistrySingleton *remotecontrol.TerminalsRegistry
)
