// Package remotecontrol — host_helpers.go: helpers
// backing StartHostSession in host.go. Extracted to
// keep StartHostSession's body under the 50-fn
// ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// applyHostSessionFields: mutates the HostConfig's
// SessionState with the caller-supplied
// Backend / Model / Workspace overrides. A
// pre-populated SessionState is the caller's signal
// that they already know the values; the helper
// does not overwrite them, on the principle that
// the host is closer to the source of truth than
// the helper is.
//
// resolveHostSocketPath: resolves the unix-socket
// path, with the env override (OPENAGY_SOCKET)
// winning over the explicit field. Non-empty
// non-absolute paths are rejected so the dispatcher's
// bind step never sees a relative path.
//
// ConfigFromHost: translates a HostConfig into the
// dispatcher's Config. The conversion is non-trivial
// because HostConfig exposes a curated subset (the
// daemon wire-bound surface) while Config exposes the
// full dispatcher surface (used by in-process
// embedders).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"fmt"
	"os"
	"path/filepath"
)

// applyHostSessionFields mutates hc.Session with the
// fields the caller supplied on HostConfig. A
// pre-populated SessionState is the caller's signal
// that they already know the values — in that case
// the helper does not overwrite them, on the
// principle that the host is closer to the source of
// truth than the helper is.
//
// Extracted from StartHostSession to keep that
// function under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func applyHostSessionFields(hc *HostConfig) {
	if hc.Session == nil {
		hc.Session = NewSessionState(hc.Version)
	}
	if hc.Backend != "" {
		hc.Session.Backend = hc.Backend
	}
	if hc.Model != "" {
		hc.Session.Model = hc.Model
	}
	if hc.Workspace != "" {
		hc.Session.Workspace = hc.Workspace
	}
}

// resolveHostSocketPath resolves the unix socket
// path the daemon will listen on. Env override
// (OPENAGY_SOCKET) wins over the explicit field;
// matches the OPENAGY_BACKEND convention. Empty →
// caller falls through to Start's per-platform
// default. A non-empty non-absolute path is
// rejected — Start's unix-socket bind accepts only
// absolute paths.
//
// Extracted from StartHostSession to keep that
// function under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func resolveHostSocketPath(hc HostConfig) (string, error) {
	sock := hc.SocketPath
	if env := os.Getenv(EnvSocketPath); env != "" {
		sock = env
	}
	if sock != "" && !filepath.IsAbs(sock) {
		return "", fmt.Errorf("remotecontrol: %s=%q must be an absolute path", EnvSocketPath, sock)
	}
	return sock, nil
}

// ConfigFromHost translates a HostConfig into the dispatcher's Config.
// The conversion is non-trivial because HostConfig exposes a curated
// subset (the daemon wire-bound surface) while Config exposes the full
// dispatcher surface (used by in-process embedders).
//
// Cycle 346: this was the package-private hostConfigToDispatcherConfig.
// It is exported now because two callers need it — the Unix-socket host
// (StartHostSession) and the TCP host (internal/rchost, the
// `--remote-control-tcp` path) — and the second one used to carry its
// own, shorter translation that silently dropped the Web UI's terminal
// and voice registries. One translation, one set of fields.
//
// The socket path is a parameter because the Unix host binds one and
// the TCP host does not ("" means "no Unix socket").
func ConfigFromHost(hc HostConfig, sock string) Config {
	return Config{
		SocketPath:              sock,
		Session:                 hc.Session,
		Agents:                  DefaultAgentLister,
		Reader:                  ConvstoreOutputReader{},
		ListBackend:             hc.ListBackend,
		FS:                      hc.FS,
		Projects:                hc.Projects,
		Conversations:           hc.Conversations,
		AuthState:               hc.AuthState,
		DaemonMeta:              hc.DaemonMeta,
		Commit:                  hc.Commit,
		BuildLabel:              hc.BuildLabel,
		MessageBackend:          hc.MessageBackend,
		PermissionsStore:        hc.PermissionsStore,
		PermissionsPath:         hc.PermissionsPath,
		PluginsStore:            hc.PluginsStore,
		PluginsPath:             hc.PluginsPath,
		TriggersStore:           hc.TriggersStore,
		TriggersPath:            hc.TriggersPath,
		FastpushStore:           hc.FastpushStore,
		FastpushPath:            hc.FastpushPath,
		ResourceStore:           hc.ResourceStore,
		ResourcePath:            hc.ResourcePath,
		ConversationConfigStore: hc.ConversationConfigStore,
		ConversationConfigPath:  hc.ConversationConfigPath,
		TerminalsRegistry:       hc.TerminalsRegistry,
		// Cycle rc-web-terminals: the Web UI's terminal
		// surface (a live shell per terminal).
		WebTerminals: hc.WebTerminals,
		// Cycle rc-web-terminals: the Web UI's voice input.
		WebAudio: hc.WebAudio,
		// The agent↔browser bridge (cycle 347): the three
		// agent-browser RPCs answer through this seam. It was
		// missing here, so the Unix-socket host came up with a
		// nil broker and the bridge answered nothing.
		Browser: hc.Browser,
		// Cycle rc-auth-tokens: production
		// wires an FSOriginPolicyStore rooted
		// at $ANTIGRAVITY_HOME/origin_policy.json
		// via cmd/wagyu's
		// buildHostOriginPolicyStore;
		// tests leave this nil so
		// NewDispatcher's nil-fabrication
		// installs a fresh empty
		// MemOriginPolicyStore.
		OriginPolicyStore: hc.OriginPolicyStore,
		OriginPolicyPath:  hc.OriginPolicyPath,
		// Cycle rc-webrtc-mesh-defer: nil
		// today — the canonical all-false
		// default is what the branch ships.
		DeferredFeatures: hc.DeferredFeatures,
	}
}
