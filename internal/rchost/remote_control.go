// Package main — remote_control.go: --remote-control daemon wiring.
//
// AGY 1.2.16 changelog: "Added the `--remote-control` daemon flag that
// exposes a small set of MCP tools (status, list_agents, get_output)
// over a Unix socket so external dashboards can read live session
// state."
package rchost

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/remotetransport"
)

// remoteControlOnce guards the package-level daemon pointer so a
// double-call does not bind two daemons on the same socket.
var remoteControlOnce sync.Once

// maybeStartRemoteControlDaemon brings up the Unix-socket daemon
// when --remote-control is set and returns the running *Daemon.
func MaybeStartDaemon(ctx context.Context, fs *flag.FlagSet, sess *remotecontrol.SessionState) *remotecontrol.Daemon {
	if !flagBoolValue(fs, "remote-control") {
		return nil
	}
	if flagStringValue(fs, "remote-control-tcp") != "" {
		fmt.Fprintf(os.Stderr, "[remote-control] --remote-control-tcp is set; ignoring --remote-control's Unix-socket listener\n")
		return nil
	}
	if flagBoolValue(fs, "experimental-rc-v2") {
		fmt.Fprintf(os.Stderr, "[remote-control] --experimental-rc-v2 is set; the deferred v2 features (WebRTC mesh / Antigravity Hub) are not enabled in this branch — see docs/deferred-rc-transport.md\n")
	}
	sockPath := flagStringValue(fs, "remote-control-socket")
	cfg := buildHostConfig(fs, sess, sockPath, "unix", sockPath)
	d, err := remotecontrol.StartHostSession(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control] daemon failed to start: %v\n", err)
		return nil
	}
	return d
}

// maybeStartRemoteControlTCPDaemon brings up the TCP+WebSocket
// daemon when --remote-control-tcp=<addr> is set and returns the
// running *remotetransport.TCPServer.
func MaybeStartTCPDaemon(ctx context.Context, fs *flag.FlagSet, sess *remotecontrol.SessionState) *remotetransport.TCPServer {
	addr := flagStringValue(fs, "remote-control-tcp")
	if addr == "" {
		return nil
	}
	cfg := buildHostConfig(fs, sess, "", "tcp+ws", addr)
	// The TCP host has no Unix socket; the dispatcher is the same one
	// the Unix host builds, from the same translation — cycle 346, when
	// this path carried its own, shorter translation that dropped the
	// Web UI's terminal and voice registries.
	disp, err := remotecontrol.NewDispatcher(remotecontrol.ConfigFromHost(cfg, ""))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control-tcp] dispatcher init failed: %v\n", err)
		return nil
	}
	seed := originpolicy.LoadOriginPolicyFromEnv()
	// Cycle rc-auth-bearer-required: the secret a client from another
	// machine has to present. The flag is documented as a way to
	// reach the daemon from elsewhere, so the daemon asks for a
	// credential.
	token, _, err := resolveRCToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control-tcp] could not mint the remote-control secret: %v\n", err)
		return nil
	}
	srv, err := remotetransport.NewTCPServer(ctx, disp, remotetransport.TCPConfig{
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
		CSRFToken:    token,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[remote-control-tcp] daemon failed to start on %s: %v\n", addr, err)
		return nil
	}
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	fmt.Fprintf(os.Stderr, "[remote-control-tcp] ws daemon listening on: %s\n", srv.HTTPURL())
	announceRCToken(os.Stderr, "[remote-control-tcp]", addr, token)
	return srv
}

// buildHostConfig constructs the HostConfig shared across daemon transports.
func buildHostConfig(fs *flag.FlagSet, sess *remotecontrol.SessionState, sockPath, transport, addr string) remotecontrol.HostConfig {
	cwd, _ := os.Getwd()
	fsProv := buildHostFSProvider(fs, cwd)
	projStore := buildHostProjectStore(fsProv)
	// One broker for the whole daemon: the dispatcher's three
	// LanguageServerService RPCs and the turn's `webview_browser` tool
	// have to meet in the same instance.
	broker := agentbrowser.NewBroker()
	return remotecontrol.HostConfig{
		Version:                 cli.ReferenceVersion,
		Backend:                 "",
		Model:                   flagStringValue(fs, "model"),
		Workspace:               cwd,
		Session:                 sess,
		SocketPath:              sockPath,
		Out:                     os.Stderr,
		ListBackend:             buildHostListBackend(),
		FS:                      fsProv,
		Projects:                projStore,
		Conversations:           buildHostConversationStore(projStore),
		AuthState:               authStateForCaller(),
		DaemonMeta:              daemonMetaForCaller(transport, addr, "in-process"),
		MessageBackend:          buildHostMessageBackend(cwd, flagStringValue(fs, "model"), broker),
		Browser:                 broker,
		PermissionsStore:        buildHostPermissionStore(),
		PluginsStore:            buildHostPluginStore(),
		TriggersStore:           buildHostTriggerStore(),
		FastpushStore:           buildHostFastpushStore(),
		ResourceStore:           buildHostResourceStore(projStore),
		ConversationConfigStore: buildHostConversationConfigStore(),
		TerminalsRegistry:       buildHostTerminalsRegistry(),
		WebTerminals:            buildHostWebTerminalsRegistry(),
		WebAudio:                buildHostWebAudioRegistry(),
		OriginPolicyStore:       buildHostOriginPolicyStore(),
	}
}

// daemonMetaForCaller returns the DaemonMeta block a host wires into Config.DaemonMeta.
func daemonMetaForCaller(transport, addr, mode string) *remotecontrol.DaemonMeta {
	return &remotecontrol.DaemonMeta{
		Transport: transport,
		Addr:      addr,
		Mode:      mode,
	}
}

// maybeInstallDaemonMeta wires DaemonMeta onto a freshly-built dispatcher Config.
func maybeInstallDaemonMeta(cfg *remotecontrol.Config, transport, addr, mode string) {
	if cfg == nil {
		return
	}
	cfg.DaemonMeta = daemonMetaForCaller(transport, addr, mode)
}

// resolvedAuthMethodForCaller decides which auth method the host should report on the rich-status tool.
func resolvedAuthMethodForCaller() string {
	return remotecontrol.AuthMethodShim
}

// authStateForCaller returns a fresh AuthState seeded with the host's resolved auth method.
func authStateForCaller() *remotecontrol.AuthState {
	return remotecontrol.NewAuthState(
		resolvedAuthMethodForCaller(),
		"", "", nil, time.Time{},
	)
}

// bindRemoteControlSession wires a host's already-running session into a started daemon.
func BindSession(d *remotecontrol.Daemon, sess *remotecontrol.SessionState) {
	if d == nil || sess == nil {
		return
	}
	if cwd, err := os.Getwd(); err == nil {
		sess.Workspace = cwd
	}
}

// shutdownRemoteControlDaemon is the symmetric teardown for the Unix-socket daemon.
func ShutdownDaemon(d *remotecontrol.Daemon, w io.Writer) {
	if d == nil {
		return
	}
	d.Shutdown()
	if w != nil {
		fmt.Fprintln(w, "Remote control daemon stopped.")
	}
}

// shutdownRemoteControlTCPDaemon is the symmetric teardown for the TCP+WebSocket daemon.
func ShutdownTCPDaemon(srv *remotetransport.TCPServer, w io.Writer) {
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
