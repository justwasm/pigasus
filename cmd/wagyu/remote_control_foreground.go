// Package main — remote_control_foreground.go: the hidden
// `remote-control-foreground` subcommand the LaunchAgent plist
// invokes.
//
// Cycle rc-persistent-daemon: this is the long-running daemon
// process `wagyu remote-control start` registers with launchd.
// The plist (built by internal/persistentrc) runs:
//
//	wagyu remote-control-foreground --addr <addr> --name <n> --log-file <path>
//
// The foreground handler:
//
//   - builds a remotecontrol.Config with the same closures
//     the cmd/wagyu/ remote_control.go per-turn wiring uses
//     (ListBackend, FS, Projects, Conversations), but with
//     no --add-dir / --sandbox inheritance (the foreground
//     daemon inherits the workspace from cwd at boot time);
//   - brings up a remotetransport.TCPServer on <addr>;
//   - installs a SIGINT / SIGTERM handler that triggers
//     graceful shutdown (TCPServer.Shutdown with a 5 s cap);
//   - blocks on the signal.
//
// The handler does NOT call StartHostSession — that path is
// for the in-process per-turn --remote-control flag.  The
// foreground path uses a TCP+WebSocket transport so external
// dashboards can connect over the LAN; the per-turn path uses
// a Unix socket so local dashboards can keep working without
// touching the firewall.
//
// Log file: --log-file redirects this process's stdout AND
// stderr to the given file (the plist's StandardOutPath /
// StandardErrorPath both point at the same path).  Anything
// the daemon prints lands there; launchd reads it through
// `log show --predicate …`.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/originpolicy"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotetransport"
)

// runRemoteControlForeground is the `remote-control-foreground`
// hidden subcommand's Run handler.  Registered in
// internal/cli/parser_specs.go; not in --help.
//
// The flag set is a fresh stdlib flag.FlagSet so the handler
// runs without inheriting any of the parent's flags.  Three
// flags matter:
//
//   - --addr      bind address (host:port)
//   - --name      machine label (used as the launchd label
//     suffix and as the log-file basename)
//   - --log-file  absolute path the foreground daemon
//     redirects its own stdout / stderr to
//
// Errors return through cli.ExitCodeError so the parent
// process exits non-zero and launchd restarts the agent (the
// KeepAlive=true plist setting).
func runRemoteControlForeground(j *cli.JetskiCLI, args []string) error {
	fs := flag.NewFlagSet("remote-control-foreground", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // match the root parser's quiet-error policy
	addr := fs.String("addr", "", "Bind address for the JSON-RPC-over-WebSocket transport (host:port)")
	name := fs.String("name", "", "Machine instance label (matches the launchd label suffix)")
	logFile := fs.String("log-file", "", "Absolute log file path for stdout / stderr")
	model := fs.String("model", "", "Default model label forwarded to PiagentBackend; empty uses the host's resolved model")

	if err := fs.Parse(args); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: bad remote-control-foreground flags: %v", err))
	}

	// Redirect stdout / stderr to the log file when the
	// caller passed --log-file.  launchd's plist already
	// redirects both via StandardOutPath / StandardErrorPath;
	// this is belt-and-suspenders for the case where the
	// foreground was invoked manually (e.g. for debugging).
	if *logFile != "" {
		if err := redirectToLogFile(*logFile); err != nil {
			fmt.Fprintf(j.ErrOut, "remote-control-foreground: failed to open log file %s: %v\n", *logFile, err)
		}
	}

	// Validate the inputs that the persistentrc layer would
	// have validated — but the foreground path skips that
	// layer entirely, so we duplicate the checks here.
	if *name == "" {
		return cli.ExitCodeError(2, "Error: --name is required for remote-control-foreground")
	}
	if *addr == "" {
		return cli.ExitCodeError(2, "Error: --addr is required for remote-control-foreground")
	}
	if err := persistentrc.ValidateAddr(*addr); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}
	if err := persistentrc.ValidateName(*name); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}

	cwd, _ := os.Getwd()
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd
	sess.Model = *model

	// Build the dispatcher with the same Config the
	// per-turn --remote-control wiring uses, so dashboards
	// see the same surface regardless of which transport
	// they dialed.  Pass nil flag.FlagSet; the buildHost*
	// helpers tolerate that (the flag accessors return
	// zero values when the flag is missing).
	fsProvider := buildHostFSProvider(nil, cwd)
	projects := buildHostProjectStore(fsProvider)
	// Cycle rc-status-tool: populate DaemonMeta
	// with mode="persistent" so the rich-status
	// tool reports the LaunchAgent lifecycle rather
	// than the per-turn "in-process" mode.
	// AuthState is seeded with the host's resolved
	// method so the very first status call surfaces
	// the right auth block.
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:        sess,
		Agents:         remotecontrol.DefaultAgentLister,
		Reader:         remotecontrol.ConvstoreOutputReader{},
		ListBackend:    buildHostListBackend(),
		FS:             fsProvider,
		Projects:       projects,
		Conversations:  buildHostConversationStore(projects),
		AuthState:      authStateForCaller(),
		DaemonMeta:     daemonMetaForCaller("tcp+ws", *addr, "persistent"),
		MessageBackend: buildHostMessageBackend(cwd, *model),
		// Cycle rc-resources: the foreground
		// LaunchAgent daemon uses the same
		// $ANTIGRAVITY_HOME/resources.json store
		// as the per-turn --remote-control paths.
		// The broadcaster registered by
		// NewTCPServer fans "resources/changed"
		// notifications to every open ws conn (the
		// same BroadcastNotify seam
		// rc-permissions-grants added; shared with
		// rc-plugins, rc-triggers, and
		// rc-fastpush-toggle).
		//
		// The Bind path writes back to the
		// project's `Resources` field via the
		// bound ProjectStore. We pass the same
		// *FSProjectStore buildHostProjectStore
		// builds so the foreground daemon and the
		// per-turn daemons share the on-disk
		// project file the Bind path mutates.
		ResourceStore: buildHostResourceStore(projects),
		// Cycle rc-conversation-config: the
		// foreground LaunchAgent daemon uses the
		// same
		// $ANTIGRAVITY_HOME/conversation_config.json
		// store as the per-turn --remote-control
		// paths. The broadcaster registered by
		// NewTCPServer fans
		// "conversation_config/changed"
		// notifications to every open ws conn (the
		// same BroadcastNotify seam
		// rc-permissions-grants added; shared
		// with rc-plugins, rc-triggers,
		// rc-fastpush-toggle, rc-resources).
		//
		// The cycle is write-only: the single
		// UpsertRemoteAgentConversationConfig
		// method is the only consumer; the future
		// `rc-conversation-config-read` cycle
		// adds the read methods so dashboards can
		// render the post-upsert state without
		// their own copy.
		ConversationConfigStore: buildHostConversationConfigStore(),
		// Cycle rc-terminals: same process-singleton
		// registry the per-turn and persistent daemons
		// use (buildHostTerminalsRegistry memoises
		// once per process). The five
		// RemoteAgentTerminal* methods route through
		// this single registry.
		TerminalsRegistry: buildHostTerminalsRegistry(),
		// Cycle rc-auth-tokens: same
		// origin-policy store the per-turn
		// daemons use
		// ($ANTIGRAVITY_HOME/origin_policy.json).
		// The SetRemoteControlOriginPatterns
		// handler invokes the
		// originPolicySetter below so the WS
		// transport's active policy mirrors
		// the on-disk state.
		OriginPolicyStore: buildHostOriginPolicyStore(),
	})
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: dispatcher init failed: %v", err))
	}

	// Bring up the TCP+WebSocket transport.  Path is
	// "/rpc" — matches the per-turn --remote-control-tcp
	// flag's wire endpoint.
	// Cycle rc-auth-tokens: seed the WS
	// transport with the canonical
	// production allowlist at startup so
	// the very first Accept already sees
	// the brief-pinned defaults (or the
	// env-var override). The
	// dispatcher-side setter (registered
	// below) keeps the policy mirrored
	// after every
	// SetRemoteControlOriginPatterns RPC.
	seed := originpolicy.LoadOriginPolicyFromEnv()
	srv, err := remotetransport.NewTCPServer(context.Background(), disp, remotetransport.TCPConfig{
		Addr:         *addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
	})
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", *addr, err))
	}
	// Cycle rc-auth-tokens: register the
	// transport's SetOriginPolicy as the
	// dispatcher's originPolicySetter so
	// mutations mirror into the WS
	// transport's active policy.
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)

	// Install SIGINT / SIGTERM handler that triggers a
	// graceful shutdown.  launchd sends SIGTERM when the
	// user calls `launchctl bootout` or the system shuts
	// down; `Ctrl+C` from a foreground shell test sends
	// SIGINT.  Both end up at the same channel.
	//
	// The signal-channel install is delegated to a package
	// hook so the test rig (cmd/wagyu/remote_control_foreground_test.go)
	// can replace `signal.Notify` with a deterministic
	// channel that closes on demand — production never
	// touches the var, so behaviour is identical.
	sigCh := make(chan os.Signal, 1)
	signalChProvider(sigCh)
	defer signal.Stop(sigCh)

	// Banner line on stderr so an operator running the
	// foreground manually sees the bound address.  The
	// plist's launchd-managed child redirects stderr to
	// the log file, so production users see this in
	// <log-file>.
	fmt.Fprintf(os.Stderr, "remote-control-foreground: listening on %s (path=/rpc, name=%s, antigravity_home=%s)\n",
		srv.HTTPURL(), *name, auth.AntigravityHome())

	// Block on the signal.
	sig := <-sigCh
	fmt.Fprintf(os.Stderr, "remote-control-foreground: caught signal %v, shutting down\n", sig)

	// Graceful shutdown — 5 s cap (matches the
	// TCPServer.Shutdown default context budget the
	// per-turn path uses).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "remote-control-foreground: shutdown error: %v\n", err)
		return cli.ExitCodeError(1, fmt.Sprintf("Error: shutdown failed: %v", err))
	}
	return nil
}

// redirectToLogFile opens logFile in append mode and dups the
// file descriptor onto stdout (fd 1) and stderr (fd 2).  Used
// by the foreground handler when --log-file is set.
//
// On success, any subsequent fmt.Fprintln(os.Stderr, …) lands
// in logFile instead of the terminal.  We do this by reopening
// fd 1 and fd 2 to point at the log file via os.Dup2 — a
// trick launchd itself uses for its own service manager.
//
// Errors are returned to the caller; the caller decides
// whether to surface them (the foreground handler logs to
// stderr first and continues, so a log-file failure doesn't
// prevent the daemon from starting).
func redirectToLogFile(logFile string) error {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stdout.Fd())); err != nil {
		_ = f.Close()
		return err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = f.Close()
		return err
	}
	// Best-effort close of the original fd; the duplicated
	// fd copies are already on stdout/stderr.
	_ = f.Close()
	return nil
}

// signalChProvider is the package-level hook the foreground
// handler uses to install its signal channel.  Default
// `signalChFromNotify` registers via signal.Notify so SIGINT
// / SIGTERM land in the channel; the test rig
// (cmd/wagyu/remote_control_foreground_test.go) overrides
// the var to provide a deterministic channel that closes on
// demand.  Production never touches this var.
//
// This is a small test seam: the alternative is sending a
// real SIGTERM to the test process.  signal.Notify can
// suppress the default-kill behaviour, but the test still
// races on signal delivery order across goroutines and
// across `go test -count=N` repeats — a deterministic
// channel close is a much cleaner shutdown trigger.
var signalChProvider = signalChFromNotify

// signalChFromNotify is the production signal-channel
// plumbing: registers the channel with signal.Notify for
// SIGINT and SIGTERM.  The signal package's "Notify
// suppresses default action" rule means SIGTERM to the
// foreground daemon (which is what launchd sends on
// bootout) is delivered to this channel rather than
// terminating the process.
func signalChFromNotify(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
}

// _ keeps convstore referenced so the import survives a
// future refactor that removes the explicit builder chain.
// (buildHostConversationStore uses convstore.ConversationBoundToProject.)
var _ = convstore.ConversationBoundToProject
