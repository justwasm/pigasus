package cli

import (
	"context"
	"fmt"
	"io"
)

// ---- acp_bridge.go ----

// Package cli — acp_bridge.go: internal/cliapp → internal/cli callback for
// the `pigasus acp` subcommand.

// ACPArgs holds the arguments passed to the acpHandler.
type ACPArgs struct {
	// Stdio is true when ACP should communicate over standard I/O (stdin/stdout).
	Stdio bool

	// Addr is the bind address (e.g. "127.0.0.1:0" or "127.0.0.1:7777").
	Addr string

	// Workspace is the root project workspace path to serve.
	Workspace string

	// Model is the model name.
	Model string

	// Effort is the reasoning effort.
	Effort string

	// Token is an optional bearer token.
	Token string

	// In is standard input.
	In io.Reader

	// Out is standard output.
	Out io.Writer

	// Err is diagnostics / stderr.
	Err io.Writer

	// Ctx is the cancellation context.
	Ctx context.Context

	// RawArgs are any unprocessed command-line arguments.
	RawArgs []string
}

// acpHandler is the handler implementation wired by cliapp.
var acpHandler func(j *JetskiCLI, args ACPArgs) error

// RegisterACPHandler registers the implementation for `pigasus acp`.
func RegisterACPHandler(fn func(j *JetskiCLI, args ACPArgs) error) {
	acpHandler = fn
}

// ---- agents_bridge.go ----

// agents_bridge.go: cmd/wagyu -> internal/cli callback for the
// `agent` and `agents` subcommands.

var agentsLister func() []string

// RegisterAgentsLister installs the custom-agent discovery provider.
func RegisterAgentsLister(fn func() []string) {
	agentsLister = fn
}

// ---- foreground_bridge.go ----

// foreground_bridge.go: cmd/wagyu → internal/cli callback for the
// hidden `remote-control-foreground` subcommand.
//
// The hidden subcommand lives in `parser_specs.go` because parser
// registration has to happen before NewParser returns.  The actual
// handler — the function that brings up the remotecontrol.Dispatcher
// + remotetransport.TCPServer — lives in cmd/wagyu/ where it has
// access to buildHostFSProvider, buildHostListBackend, etc.  We can't
// have internal/cli/ import cmd/wagyu/ (cyclic), so this file
// provides the package-level hook the same way SetChatBridge does:
//
//   - parser_specs.go's RunJ is `runRCForegroundPlaceholder`,
//     which is defined here.
//   - `runRCForegroundPlaceholder` calls into the package-level
//     `foregroundHandler` var.
//   - cmd/wagyu/main.go sets that var via
//     `cli.RegisterForegroundHandler(runRemoteControlForeground)`
//     at startup, alongside the cli.SetChatBridge call.
//
// When the bridge is unset (the cli-internal test rig never
// invokes main, so it stays unit-test-friendly) we emit a one-line
// notice to stderr and exit 0 — matches the "do nothing" behaviour
// the parent subcommand's "RunJ var was nil at parse time" path
// already has.

// foregroundHandler is the cmd/wagyu-supplied entry point for the
// hidden `remote-control-foreground` subcommand.  Set via
// RegisterForegroundHandler at process start; nil in tests that
// only exercise the parser surface.
//
// Signature mirrors JetskiCLI.RunJ: the handler receives the
// parser's stream pair (so the daemon can log to the same place
// the parser would have) and the argv tail after the subcommand
// name is stripped.
var foregroundHandler func(j *JetskiCLI, args []string) error

// RegisterForegroundHandler installs the cmd/wagyu-supplied
// foreground daemon handler.  Called exactly once from
// cmd/wagyu/main.go at process init (next to
// cli.SetChatBridge).  Tests that exercise the parser surface
// without the main package leave it nil; runRCForegroundPlaceholder
// handles that case gracefully.
func RegisterForegroundHandler(fn func(j *JetskiCLI, args []string) error) {
	foregroundHandler = fn
}

// runRCForegroundPlaceholder is the RunJ bound to the
// `remote-control-foreground` spec in parser_specs.go.  It
// dispatches into the registered foreground handler, falling
// back to a one-line stderr notice when the bridge is unset
// (the cli-internal test rig).
func runRCForegroundPlaceholder(j *JetskiCLI, args []string) error {
	if foregroundHandler == nil {
		// Match the RunJ-nil convention used elsewhere in
		// the parser: the parent subcommand still resolves
		// and exits 0.  We emit a banner so an operator
		// running the bare CLI sees why nothing happened.
		fmt.Fprintln(j.ErrOut, "remote-control-foreground: no handler registered (binary was built without the foreground bridge)")
		return nil
	}
	return foregroundHandler(j, args)
}

// io is referenced so gofmt preserves the import block while
// the only reference is the j.ErrOut path above.  (Removing
// the io import here would break the file once a future refactor
// moves stream writers behind an io.Writer parameter.)
var _ io.Writer = io.Discard

// ---- logo_bridge.go ----

// logoHandler is the cmd/wagyu-supplied entry point for the
// hidden `logo` subcommand. Set via RegisterLogoHandler at process
// start; nil in tests that only exercise the parser surface.
var logoHandler func(w io.Writer, args []string) error

// RegisterLogoHandler installs the cmd/wagyu-supplied logo printer.
// Called from cmd/pigasus/main.go at process init.
func RegisterLogoHandler(fn func(w io.Writer, args []string) error) {
	logoHandler = fn
}

// runLogoHandler is the Run bound to the hidden `logo` spec in parser_specs.go.
// When unbridged (e.g. standalone cli tests), it prints fallback text.
func runLogoHandler(w io.Writer, args []string) error {
	if logoHandler == nil {
		fmt.Fprintln(w, "logo: not bridged (build with cmd/pigasus to enable)")
		return nil
	}
	return logoHandler(w, args)
}

// ---- serve_bridge.go ----

// serve_bridge.go: cmd/wagyu → internal/cli callback for the
// `remote-control serve` subcommand.
//
// Cycle rc-serve-subcommand. The same bridge pattern
// `foreground_bridge.go` uses for the hidden
// `remote-control-foreground` subcommand: the parser-level
// RunJ lives in internal/cli/ (so the spec is registered in
// parser_specs.go alongside the rest of the remote-control
// siblings), but the handler body — which has to reach into
// buildHostFSProvider / buildHostListBackend / buildHostMessageBackend
// and the rest of the cmd/wagyu wiring — lives in cmd/wagyu/.
//
// Why a fresh file rather than extending foreground_bridge.go:
// the foreground subcommand is hidden (launchd invokes it
// internally), so it never sees the parser's argv flow; serve
// is a user-facing subcommand that needs flag validation
// (`--addr`, `--name`, `--no-login`) which the parser does
// before the bridge runs. Putting the bridge for the two
// subcommands in the same file would muddle the two surfaces'
// contracts.
//
// Set via RegisterServeHandler at process init, alongside
// the chat + foreground bridges in cmd/wagyu/main.go. Tests
// that exercise the parser surface without the main package
// leave it nil; runRCServe handles that case gracefully.

// ServeArgs is the typed argv slice the parser-supplied
// runRCServe hands to the cmd/wagyu-supplied serveHandler.
// We pass a struct (rather than the raw argv tail) so the
// handler body in cmd/wagyu/ doesn't have to re-parse the
// flags — and so adding a flag in runRCServe surfaces here
// automatically without a coordinated edit.
type ServeArgs struct {
	// Addr is the resolved --addr value. Empty means
	// "use the foreground default (0.0.0.0:8787)"; the
	// handler body enforces that empty → default and
	// validates the result with persistentrc.ValidateAddr.
	Addr string

	// Name is the resolved --name value. Always non-empty
	// after runRCServe's hostname fallback.
	Name string

	// NoLogin suppresses the inline OAuth device-code
	// login when no bearer token is on disk. Tests use
	// this; production leaves it false so the brief's
	// "trigger OAuth login inline" semantic holds.
	NoLogin bool

	// Desktop asks the daemon for the reference's *desktop*
	// UI: the voice input, the jetbox terminal, the
	// redesigned aux pane.
	//
	// The page and the bundle are the same either way — the
	// switch is the identity the page's Mendel read carries:
	// the backend serves a different flag set per product,
	// and the desktop features are in the hub's set.
	//
	// 【一手】 measured 2026-10-27 — the same token, the same
	// body, the same endpoint, only the User-Agent differing:
	// `antigravity/hub/…` answers enable-audio-transcription:
	// true, `antigravity/cli/…` answers it false. See
	// internal/auth.
	//
	// This is the `remote-control serve` twin of
	// `pigasus web --desktop` (WebArgs.Desktop).
	Desktop bool

	// Label is the name the handler prefixes its progress lines
	// with ("remote-control serve"). It is empty for the `serve`
	// subcommand itself, and the handler falls back to that name;
	// `remote-control start` on a platform without launchd reuses
	// the same handler and passes its own label, so the user sees
	// the subcommand they actually typed.
	Label string
}

// serveHandler is the cmd/wagyu-supplied entry point for
// the user-facing `remote-control serve` subcommand. Set via
// RegisterServeHandler at process start; nil in tests that
// only exercise the parser surface.
//
// The handler receives the parser's JetskiCLI (so it can write
// results to j.Out and usage/errors to j.ErrOut — the split
// the v1.3.5 binary uses) and a typed ServeArgs with the
// already-validated flag values. Returning a non-nil error
// surfaces it through the parser's exit-code path; the
// canonical "Application Default Credentials" gate lives in
// the handler and returns *ExitCodeError with code 3 on the
// ADC path.
var serveHandler func(j *JetskiCLI, args ServeArgs) error

// RegisterServeHandler installs the cmd/wagyu-supplied
// `remote-control serve` handler. Called exactly once from
// cmd/wagyu/main.go at process init, alongside the chat and
// foreground bridges. Tests that exercise the parser surface
// without the main package leave it nil; runRCServe
// handles that case gracefully.
func RegisterServeHandler(fn func(j *JetskiCLI, args ServeArgs) error) {
	serveHandler = fn
}

// ---- server_bridge.go ----

// Package cli — server_bridge.go: internal/cliapp → internal/cli callback
// for the `pigasus server` subcommand.
//
// The server is the host half of the client/server split: it owns the
// workspace, the tools and the credentials, and it serves the turns a
// client asks for over a websocket.
//
// The parser-level half lives in internal/cli (the spec is registered
// next to the `web` sibling), and the handler body lives in
// internal/cliapp — it needs the clone's own session setup, which is
// where the tools, the permission policy and the prompt are built.

// ServerArgs is the typed argument set passed to the serverHandler.
type ServerArgs struct {
	// Addr is the bind address (host:port, e.g. "127.0.0.1:7777").
	Addr string

	// Port is an optional port override (e.g. 7777, 8080).
	Port int

	// Host is an optional host interface (e.g. "0.0.0.0" or "127.0.0.1").
	Host string

	// Workspace is the root project workspace path to serve.
	Workspace string

	// Model is the model the served turns run under.
	Model string

	// Effort is the reasoning effort the served turns ask for.
	Effort string

	// Mode is the session's permission mode: "auto", "plan" or
	// "acceptEdits". Empty means the configured default.
	Mode string

	// SkipPermissions runs the served tools without the permission gate.
	SkipPermissions bool

	// Token is the bearer token a client has to present. Empty means
	// the server mints one.
	Token string

	// Agent is the custom agent the served turns run under. Empty means
	// the default agent.
	Agent string

	// Backend is the transport the served turns run on ("gateway",
	// "cloudcode", "shim"). Empty means the configured default.
	Backend string

	// AddDirs are extra workspaces the served tools may act on.
	AddDirs []string

	// ACPVersion is the ACP protocol version to serve ("auto", "1", "2").
	ACPVersion string
}

// serverHandler is the handler implementation wired by cliapp.
var serverHandler func(j *JetskiCLI, args ServerArgs) error

// RegisterServerHandler registers the implementation for `pigasus server`.
func RegisterServerHandler(fn func(j *JetskiCLI, args ServerArgs) error) {
	serverHandler = fn
}

// ---- tui_bridge.go ----

// Package cli — tui_bridge.go: internal/cliapp → internal/cli callback for
// the `pigasus tui` subcommand.
//
// The interactive session is the client half of the client/server split.
// Without --server it is the ordinary local session; with it, the turn
// runs on the server and the session renders the server's frames.

// TUIArgs is the typed argument set passed to the tuiHandler.
type TUIArgs struct {
	// Server is the address of the pigasus server to attach to
	// ("ws://host:port"). Empty means the local session.
	Server string

	// Token is the bearer token the server demands.
	Token string

	// Protocol is the wire protocol to speak ("agentlink", "acp", or "ahp").
	Protocol string

	// Session is the remote session ID or URI to attach to.
	Session string

	// Backend is the agent backend to run ("local", "codex", etc.).
	Backend string

	// ACPVersion is the ACP protocol version to negotiate ("auto", "1", "2").
	ACPVersion string

	// ACPCmd is an optional custom command to spawn an external ACP agent over stdio.
	ACPCmd string

	// ACPManifest is an optional path to an agent.json manifest file or folder.
	ACPManifest string

	// Args holds the arguments the subcommand does not own: the
	// session's own flags, which the session parses.
	Args []string
}

// tuiHandler is the handler implementation wired by cliapp.
var tuiHandler func(j *JetskiCLI, args TUIArgs) error

// RegisterTUIHandler registers the implementation for `pigasus tui`.
func RegisterTUIHandler(fn func(j *JetskiCLI, args TUIArgs) error) {
	tuiHandler = fn
}

// ---- web_bridge.go ----

// Package cli — web_bridge.go: internal/cliapp → internal/cli callback for the
// standalone `pigasus web` subcommand.

// WebArgs is the typed argument set passed to the webHandler.
type WebArgs struct {
	// Addr is the bind address (host:port, e.g. "0.0.0.0:8787", "127.0.0.1:8787", or ":8787").
	Addr string

	// Port is an optional port override (e.g. 8787, 8080).
	Port int

	// Host is an optional host interface (e.g. "0.0.0.0" or "127.0.0.1").
	Host string

	// Workspace is the root project workspace path to serve.
	Workspace string

	// Open indicates whether to automatically launch the default browser.
	Open bool

	// ReferenceBackend makes the web server proxy its RPCs to a
	// reference `language_server` instead of answering them itself.
	//
	// The value is either an endpoint ("host:port", "http://host:port")
	// of a running reference, or the path of a reference binary to
	// start. Empty means the clone answers the RPCs itself.
	ReferenceBackend string

	// Desktop asks the web UI for the reference's *desktop* UI: the
	// voice input, the jetbox terminal, the redesigned aux pane.
	//
	// The page and the bundle are the same either way — the switch
	// is the identity the page's Mendel read carries: the backend
	// serves a different flag set per product, and the desktop
	// features are in the hub's set.
	//
	// 【一手】 measured 2026-10-27 — the same token, the same body,
	// the same endpoint, only the User-Agent differing:
	// `antigravity/hub/…` answers enable-audio-transcription: true,
	// `antigravity/cli/…` answers it false. See internal/auth.
	Desktop bool

	// ApiOnly serves only the backend Connect-RPC and WebSocket APIs
	// without serving the frontend static web UI assets.
	ApiOnly bool

	// UiOnly serves only the frontend static web UI assets without binding
	// the local agent backend RPC services.
	UiOnly bool
}

// webHandler is the handler implementation wired by rchost/cliapp.
var webHandler func(j *JetskiCLI, args WebArgs) error

// RegisterWebHandler registers the implementation for `pigasus web`.
func RegisterWebHandler(fn func(j *JetskiCLI, args WebArgs) error) {
	webHandler = fn
}
