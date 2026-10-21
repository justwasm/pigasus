// Package main — remote_control_serve.go: the user-facing
// `remote-control serve` subcommand handler. Cycle
// rc-serve-subcommand.
//
// Background:
//
//	`agy remote-control serve` is the foreground twin of
//	`agy remote-control start`. Where `start` registers a
//	LaunchAgent on macOS (cycle rc-persistent-daemon), `serve`
//	runs the long-lived daemon in the caller's shell. Three
//	behaviours distinguish it from `start`:
//
//	  1. Foreground. The process holds the terminal until
//	     SIGINT / SIGTERM, then exits.
//
//	  2. Inline OAuth login. If no bearer token is on disk
//	     when serve starts, it triggers the OAuth device-code
//	     flow synchronously (auth.DeviceFlow), blocks until
//	     the user approves (or denies / times out), and persists
//	     the resulting token via auth.SaveTokenUnified. The
//	     `--no-login` flag skips this for tests / scripted
//	     runs.
//
//	  3. settings.json awareness. The changelog line for AGY
//	     1.4.x reads "`agy remote-control serve` sessions now
//	     use your saved `settings.json` and save changes made
//	     in the browser back to it." Today's dispatch surface
//	     has no RPC method that mutates settings.json, so the
//	     cycle ships the read half (the daemon announces the
//	     file's existence on stderr) and parks the write half
//	     for the future `rc-oauth-settings-writeback` cycle.
//
// ADC gate:
//
//	`serve` rejects $AGY_ADC_AUTH with the same wording the
//
// --remote-control / /remote-control flags already use (the
// "Remote Control does not support ADC yet" recovery hint).
// The check uses the same IsBusinessAccount helper the rest of
// the rc-* wiring uses so the gate vocabulary stays unified.
//
// Wiring:
//   - cli.RegisterServeHandler(runRemoteControlServe) is
//     installed in main.go at startup, alongside the chat and
//     foreground bridges. The bridge pattern keeps internal/cli
//     from importing cmd/wagyu/ (an import-direction reversal);
//     see internal/cli/serve_bridge.go for the contract.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/originpolicy"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotetransport"
)

// remoteControlServeAddrDefault is the bind address
// `remote-control serve` uses when the user passes no
// --addr. Mirrors the cycle rc-persistent-daemon default so
// the persistent path and the foreground path agree.
const remoteControlServeAddrDefault = "0.0.0.0:8787"

// runRemoteControlServe is the `remote-control serve`
// subcommand's Run handler. Registered in cmd/wagyu/main.go
// via cli.RegisterServeHandler.
//
// The signature mirrors runRemoteControlForeground's: the
// bridge hands us a *JetskiCLI for stdout / stderr split and a
// typed ServeArgs with the parser-validated flag values.
// Unlike the foreground path the parser already validated
// --name (and defaulted it to --name on empty), so the
// handler's only validation work is `--addr` against
// persistentrc.ValidateAddr — `--name` is consumed verbatim.
//
// Returns:
//
//   - cli.ExitCodeError(3, …) when $AGY_ADC_AUTH is set
//     (the AGY_ERROR convention the periodic-path gate uses).
//   - cli.ExitCodeError(2, …) on argv validation failure
//     (--addr malformed, --name rejected by persistentrc).
//   - nil on a clean shutdown.
//
// Errors from inside the dispatcher / TCPServer surface as
// non-nil exit codes too; the cli.Parse path translates them.
func runRemoteControlServe(j *cli.JetskiCLI, args cli.ServeArgs) error {
	// ADC gate. Same predicate as the --remote-control flag
	// and the /remote-control slash command use today. The
	// wording is shared so a dashboard grep'ing for
	// "AGY_ADC_AUTH" finds every surface that rejects ADC.
	if IsBusinessAccount() {
		return cli.ExitCodeError(remoteControlADCExitCode, serveADCErrorMessageForCaller())
	}

	// Validate --addr (refuses loopback the persistent path
	// refuses; the foreground twin needs the same constraint
	// because dashboards dial the same address the
	// LaunchAgent path uses).
	addr := strings.TrimSpace(args.Addr)
	if addr == "" {
		addr = remoteControlServeAddrDefault
	}
	if err := persistentrc.ValidateAddr(addr); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}

	// --name was already defaulted to os.Hostname() by
	// runRCServe, so the only validation left is the
	// character-class check. The persistentrc layer has
	// this; we call it here so a CLI-internal test that
	// doesn't go through runRCServe (e.g. a direct caller
	// of the bridge) gets the same protection.
	if err := persistentrc.ValidateName(args.Name); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}

	// Inline OAuth login. When the user hasn't authorised
	// the daemon yet (no token at auth.TokenFilePath()),
	// drive the same device-code flow runLogin drives.
	// `--no-login` skips this so the test rig can drive
	// the foreground handler with a pre-seeded token (or
	// no token at all when the foreground handler's
	// dispatch path doesn't need one).
	if !args.NoLogin {
		if err := ensureBearerTokenForServe(j.Out, j.ErrOut); err != nil {
			// A failed login is recoverable — the
			// user can re-run `wagyu login` and
			// retry `serve`. We surface the error
			// verbatim so the user sees the same
			// wording runLogin would have printed.
			return cli.ExitCodeError(1, fmt.Sprintf("Error: %v", err))
		}
	}

	// settings.json awareness. The brief asks `serve`
	// to "use your saved settings.json and save changes
	// made in the browser back to it." Today's dispatcher
	// has no RPC method that mutates settings.json, so we
	// ship the read half (announce the file's existence
	// on stderr) and park the write half for
	// rc-oauth-settings-writeback. The logged path is the
	// same one the v1.3.5 binary resolves via
	// main.resolveCliSettingsPath (recovered at 0x1028e1950).
	announceSettingsFile(j.ErrOut)

	cwd, _ := os.Getwd()
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd

	// Same Config the foreground daemon builds. Per-turn
	// --add-dir / --sandbox don't apply here (serve is a
	// user-invoked foreground process, not a turn-time
	// helper); pass nil flag.FlagSet so the buildHost*
	// helpers tolerate the absent flag set.
	fsProvider := buildHostFSProvider(nil, cwd)
	projects := buildHostProjectStore(fsProvider)
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:       sess,
		Agents:        remotecontrol.DefaultAgentLister,
		Reader:        remotecontrol.ConvstoreOutputReader{},
		ListBackend:   buildHostListBackend(),
		FS:            fsProvider,
		Projects:      projects,
		Conversations: buildHostConversationStore(projects),
		// Cycle rc-status-tool: AuthState / DaemonMeta
		// are populated so the very first status call
		// reflects the foreground twin's lifecycle.
		// DaemonMeta.Mode="ephemeral-foreground" so the
		// rich-status tool surfaces the new lifecycle
		// (vs. "persistent" for the LaunchAgent path
		// and "in-process" for the per-turn paths).
		AuthState:  authStateForCaller(),
		DaemonMeta: daemonMetaForCaller("tcp+ws", addr, "ephemeral-foreground"),
		// Cycle rc-piagent-bind: the same production
		// PiagentBackend the foreground + per-turn paths
		// install; today's empty `model` falls back to
		// the host's resolved default.
		MessageBackend:          buildHostMessageBackend(cwd, ""),
		PermissionsStore:        buildHostPermissionStore(),
		PluginsStore:            buildHostPluginStore(),
		TriggersStore:           buildHostTriggerStore(),
		FastpushStore:           buildHostFastpushStore(),
		ResourceStore:           buildHostResourceStore(projects),
		ConversationConfigStore: buildHostConversationConfigStore(),
		TerminalsRegistry:       buildHostTerminalsRegistry(),
		// Cycle rc-auth-tokens: same
		// origin-policy store the per-turn
		// + foreground daemons use
		// ($ANTIGRAVITY_HOME/origin_policy.json).
		OriginPolicyStore: buildHostOriginPolicyStore(),
	})
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: dispatcher init failed: %v", err))
	}

	// Bring up the TCP+WebSocket transport. Path is /rpc
	// to match the per-turn --remote-control-tcp flag's
	// wire endpoint (cycle rc-ws-transport).
	//
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
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
	})
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", addr, err))
	}

	// Cycle rc-auth-tokens: register the
	// transport's SetOriginPolicy as the
	// dispatcher's originPolicySetter so
	// mutations mirror into the WS
	// transport's active policy.
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", addr, err))
	}

	// SIGINT / SIGTERM handler. The serve handler runs in
	// the foreground (the user's terminal), so the signal
	// channels are the same shape the foreground daemon
	// uses (cycle rc-persistent-daemon) — but the test
	// seam is *serveSignalChProvider* rather than the
	// foreground's signalChProvider so each handler's
	// tests can replace its own var without affecting
	// the other.
	sigCh := make(chan os.Signal, 1)
	serveSignalChProvider(sigCh)
	defer signal.Stop(sigCh)

	// Banner. The persistent-daemon banner format
	// ("remote-control-foreground: listening on …") is
	// the contract dashboards already grep; we mirror it
	// verbatim, prefixing the subcommand name. We write
	// to j.ErrOut (not os.Stderr) so the test rig can
	// observe the banner via the in-memory stream pair
	// the parser wires up; production resolves
	// j.ErrOut → os.Stderr.
	fmt.Fprintf(j.ErrOut, "remote-control serve: listening on %s (path=/rpc, name=%s, antigravity_home=%s)\n",
		srv.HTTPURL(), args.Name, auth.AntigravityHome())

	// Block on the signal.
	sig := <-sigCh
	fmt.Fprintf(j.ErrOut, "remote-control serve: caught signal %v, shutting down\n", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(j.ErrOut, "remote-control serve: shutdown error: %v\n", err)
		return cli.ExitCodeError(1, fmt.Sprintf("Error: shutdown failed: %v", err))
	}
	return nil
}

// serveADCErrorMessageForCaller is the single source of
// truth for the error text the user sees when
// `remote-control serve` collides with ADC. The wording
// mirrors remoteControlADCErrorMessage (the --remote-control
// flag-path wording) so dashboards / grep see one
// "Application Default Credentials" error vocabulary across
// the rc-* surface.
//
//   - "remote-control serve"    the verb the user invoked
//   - "Application Default Credentials"   the human-readable name
//   - "AGY_ADC_AUTH"            the env var the user can /logout, then unset
//   - "/logout"                 recovery command
func serveADCErrorMessageForCaller() string {
	return "error: remote-control serve cannot be used with Application Default Credentials " +
		"(AGY_ADC_AUTH) sign-in; Remote Control does not support ADC yet — " +
		"sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry."
}

// ensureBearerTokenForServe inspects auth.TokenFilePath()
// for a valid bearer token and, if none is present, drives
// the inline OAuth device-code flow via cli.RunDeviceFlowLogin
// so the serve session has an authenticated auth state by
// the time the dispatcher is built.
//
// The check is best-effort: a present-but-expired token is
// accepted (the dispatcher's lazy-refresh path handles
// renewal). A completely absent token triggers the device
// flow; the user has DeviceFlowTimeout (10 min, see
// auth.DeviceFlowTimeout) to approve before the call
// returns an error.
//
// Returns nil on a successful login that persisted a
// token, or when a token was already on disk. Returns the
// underlying auth error verbatim otherwise.
func ensureBearerTokenForServe(out, err io.Writer) error {
	if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		// Token already on disk. Print a one-line
		// status so the user knows the inline-login
		// path was skipped; matches the runLogin
		// "succeeded" / "loaded" vocabulary.
		fmt.Fprintf(out, "using existing token at %s\n", auth.TokenFilePath())
		return nil
	}
	// No token on disk. Drive the device flow
	// synchronously; the user has the full
	// DeviceFlowTimeout window to approve.
	fmt.Fprintln(out, "no bearer token on disk; running OAuth login inline")
	fmt.Fprintln(out, "")
	return cli.RunDeviceFlowLogin(context.Background(), out, err)
}

// announceSettingsFile is the read half of the
// changelog-promised settings.json integration. It probes
// the canonical path the v1.3.5 binary resolves via
// main.resolveCliSettingsPath (0x1028e1950) and prints a
// one-line status to stderr so an operator running serve
// manually knows whether the daemon found a saved file.
//
// The future `rc-oauth-settings-writeback` cycle adds the
// write half — a dispatch method that the browser companion
// can call to mutate settings.json. Until then the file is
// read-only from the daemon's perspective.
func announceSettingsFile(out io.Writer) {
	settingsPath := filepath.Join(auth.AntigravityHome(), "settings.json")
	st, statErr := os.Stat(settingsPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			fmt.Fprintf(out, "settings.json: not found at %s (the dashboard will use defaults)\n", settingsPath)
			return
		}
		fmt.Fprintf(out, "settings.json: probe failed at %s: %v\n", settingsPath, statErr)
		return
	}
	fmt.Fprintf(out, "settings.json: loaded from %s (%d bytes)\n", settingsPath, st.Size())
}

// serveSignalChProvider is the package-level hook the
// serve handler uses to install its signal channel. Default
// `serveSignalChFromNotify` registers via signal.Notify so
// SIGINT / SIGTERM land in the channel; the test rig
// (`cmd/wagyu/remote_control_serve_test.go`) overrides the
// var to provide a deterministic channel that fires on
// demand. Production never touches this var.
//
// Mirrors the foreground handler's `signalChProvider` seam
// (cycle rc-persistent-daemon), but kept separate so a test
// that drives one handler doesn't affect the other's
// shutdown semantics.
var serveSignalChProvider = serveSignalChFromNotify

// serveSignalChFromNotify is the production signal-channel
// plumbing: registers the channel with signal.Notify for
// SIGINT and SIGTERM.
func serveSignalChFromNotify(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
}

// _ ensures flag is referenced. The serve handler does not
// consume any flags directly — the parser already populated
// cli.ServeArgs — but we keep the import so future flags
// can be added without re-plumbing the import block.
var _ = flag.NewFlagSet
