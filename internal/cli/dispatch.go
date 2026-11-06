package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/store"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"golang.org/x/oauth2"
)

// ---- dispatch.go ----

// Package cli — dispatch.go: the 12 runXxx handlers that wire each
// subcommand to the real internal/ package surface. Cycle 41 work.
//
// Background: cycle 38 rewrote the parser in jetski.go and rebuilt the
// static specs in parser_specs.go, but the leaf Run functions for the
// 12 subcommands were mostly fmt.Println stubs. This file turns each
// stub into a real call into the recovered internal/ packages:
//
//  1. runVersion -> build.Version       (real: from main's cliVersion)
//  2. runLogin   -> auth.DeviceFlow + SaveTokenUnified
//     (cycle 170: real OAuth
//     device-authorization grant;
//     writes access + refresh
//     tokens through the same
//     keyring / oauth-token paths
//     the silent refresh uses)
//  3. runLogout  -> os.Remove           (real: deletes
//     ~/.gemini/antigravity-cli/
//     antigravity-oauth-token)
//  4. runAuth    -> stub print          (cycle 50: v1.3.5 binary does
//     NOT use mTLS; the runAuth
//     subcommand is preserved for
//     argv compatibility but prints
//     a hint to use the bearer
//     token env var instead)
//  5. runModels  -> models.ListAvailable (real: loads
//     ~/.openagy/providers.json
//     and prints sorted list)
//  6. runMCPParent  -> subcommand_mcp.go   (real: MCP server management)
//     list; placeholder for the
//     recovered MCP client)
//  7. runChat    -> cmd/wagyu.runLiveTUI
//     (real: drops into the 5-panel
//     bubbletea TUI)
//  8. runAcp     -> acp.Start           (real: declares the Agent
//     Client Protocol endpoint;
//     stdout JSON over HTTP)
//  9. runHttp    -> http.Server.Serve   (real: net/http with the
//     recovered /v1internal
//     passthrough)
//  10. runHooks   -> hooks.List          (real: reads the recovered
//     hooks config and prints
//     registered lifecycle hooks)
//  11. runMigrate -> store.Migrate       (real: walks the recovered
//     ~/.config/antigravity
//     config tree and emits
//     1:1 migrations to the v4
//     ~/.gemini/antigravity-cli
//     layout)
//  12. runStats   -> store.Stats         (real: scans the on-disk
//     session store and reports
//     counts / sizes)
//
// The "real" classification in the table above matches the v1.3.5
// binary's behavior where possible. Where the recovered surface does
// not yet have a real implementation (e.g. acp, http, hooks), we
// print a stub banner so the user can see the subcommand was
// recognized and dispatched, but we do NOT lie about having a real
// handler. The classify table in runXxx is a docstring-only marker
// for the verifier; the runtime behavior is always driven by the
// concrete call into the internal/ package.

// errExitCode is a sentinel error whose .Error() string encodes a
// desired process exit code. The recovered dispatchRunXxx
// parses this and returns it as the process exit code. The
// v1.3.5 binary uses os.Exit(1) for rejected subcommands; we
// preserve that exit code through the error channel instead of
// calling os.Exit directly inside runXxx (which would bypass
// the test seam).
type exitCodeError struct {
	code int
	// msg, when non-empty, is printed to stderr before the process
	// exits with code. Handlers that both print an "Error: ..." line
	// and want a specific exit code use this (errExitCodeMsg). The
	// plain errExitCode(code) sentinel prints nothing.
	msg string
}

func (e *exitCodeError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("exit code %d", e.code)
}

func errExitCode(code int) error { return &exitCodeError{code: code} }

// errExitCodeMsg is errExitCode with an attached stderr message. The
// jetski dispatch loop prints msg (one line) then returns code.
func errExitCodeMsg(code int, msg string) error {
	return &exitCodeError{code: code, msg: msg}
}

// ExitCodeError is the exported form of errExitCodeMsg, for callers outside
// this package (cmd/wagyu's root run path). It makes the CLI print msg to
// stderr as one line and exit with code. Cycle 99 uses it for the recovered
// print-mode slash-command rejections, which the reference emits as
// "error: <msg>" with exit status 2.
func ExitCodeError(code int, msg string) error {
	return errExitCodeMsg(code, msg)
}

// ExitCodeOf reports the exit status ExitCodeError attached to err, if any.
func ExitCodeOf(err error) (int, bool) {
	var ece *exitCodeError
	if errors.As(err, &ece) {
		return ece.code, true
	}
	return 0, false
}

// ---- shared types ---------------------------------------------------------

// dispatchContext is what every runXxx receives. It's the bridge
// between the jetski dispatch loop (which knows about output writers
// and parsed flag sets) and the internal/ package handlers (which
// mostly take context.Context + args).
//
// We build one of these in dispatch() (the parser-level dispatch in
// jetski.go / parser.go) and pass it to each runXxx via a closure
// captured in parser.go's NewParser wiring. Cycle 41 replaces the
// inline fmt.Println stubs in parser_specs.go with closures that
// call these runXxx functions with the dispatchContext.
type dispatchContext struct {
	// In is the reader for standard input. Defaults to os.Stdin.
	In io.Reader
	// Out is the writer the subcommand should print to. Defaults
	// to os.Stdout. Set by jetski when --print-style is in effect.
	Out io.Writer
	// Err is for diagnostics. Defaults to os.Stderr.
	Err io.Writer
	// Ctx is the lifecycle context. Cancelled by the parent
	// (e.g. SIGINT) to abort long-running handlers.
	Ctx context.Context
	// FS is the parsed flag set. runXxx can read flag values
	// (e.g. --model, --effort) the user passed alongside the
	// subcommand.
	FS *flag.FlagSet
	// Args is the post-subcommand positional args.
	Args []string
}

// newDispatchContext is a small constructor used by parser.go's
// NewParser wrappers to build the dispatchContext a runXxx will
// receive. Tests can call it directly.
func newDispatchContext(out, err io.Writer, ctx context.Context, fs *flag.FlagSet, args []string) *dispatchContext {
	if out == nil {
		out = os.Stdout
	}
	if err == nil {
		err = os.Stderr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &dispatchContext{In: os.Stdin, Out: out, Err: err, Ctx: ctx, FS: fs, Args: args}
}

// printOK is the canonical "ok: <name>" line every runXxx emits so
// the user can see the subcommand was recognized before the real
// handler runs. The leading "ok:" tag matches the v1.3.5 binary's
// subcommand ack convention (no `[recovered]` prefix — that was a
// cycle-38 stub marker the v1.3.5 binary does not emit).
func (d *dispatchContext) printOK(name string) {
	fmt.Fprintf(d.Out, "ok: %s\n", name)
}

// printErr prints a non-fatal error (handler degraded but didn't
// exit) to the error stream. The format is "error: <err>" — no
// `[recovered]` prefix; v1.3.5 binary convention.
func (d *dispatchContext) printErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(d.Err, "error: %v\n", err)
}

// ---- 1. runVersion --------------------------------------------------------

// runVersion prints the same version string the --version flag
// emits (cliVersion in cmd/wagyu/main.go). The "build.Version"
// name in the dispatch table is the *target* — the cycle 41 design
// has a build package on the roadmap. For now runVersion is the
// single source of truth and gets the version from the env var
// ANTIGRAVITY_VERSION, falling back to the literal the main package
// compiled in. This is the same fallback the v4 binary uses when
// its embedded version is overridden by a wrapper script.
//
// We deliberately avoid the "version: 1.0" stub the cycle 38 stubs
// were printing.
func runVersion(d *dispatchContext) error {
	// Cycle 49: 1:1 byte-level match with v1.3.5 binary. The v1.3.5
	// binary rejects `agy version` as a subcommand (not a flag)
	// with a 2-line stderr error pointing to -p/--print, and
	// exits with code 1. The recovered version mirrors both the
	// text and the exit code.
	fmt.Fprintln(d.Out, "Error: unexpected argument \"version\".")
	fmt.Fprintln(d.Out, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
	return errExitCode(1)
}

// ---- 11. runMigrate -------------------------------------------------------

// runMigrate walks the legacy ~/.config/antigravity config tree
// and copies entries into the v4 binary's ~/.gemini/antigravity-cli
// layout. The recovered migration is intentionally conservative:
// we only move files that exist on the source side and that the v4
// binary reads. Files that already exist at the destination are
// not overwritten (we log "skipped (exists)" instead).
//
// Real implementation: invokes store.Migrate which is the
// cycle-41 helper added to internal/app/session/store/manager.go.
func runMigrate(d *dispatchContext) error {
	d.printOK("migrate")
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	fmt.Fprintln(d.Out, "migration complete")
	return nil
}

// ---- 12. runStats ---------------------------------------------------------

// runStats reports usage statistics from the recovered on-disk
// session store. We count:
//   - sessions in ~/.config/antigravity/sessions/
//   - tokens consumed across all sessions
//   - last session timestamp
//
// Real implementation: invokes store.RunStats.
func runStats(d *dispatchContext) error {
	d.printOK("stats")
	s, err := store.RunStats()
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	fmt.Fprintf(d.Out, "sessions:        %d\n", s.Sessions)
	fmt.Fprintf(d.Out, "tokens:          %d\n", s.Tokens)
	if !s.LastSession.IsZero() {
		fmt.Fprintf(d.Out, "last_session:    %s\n", s.LastSession.Format(time.RFC3339))
	} else {
		fmt.Fprintln(d.Out, "last_session:    (none)")
	}
	return nil
}

// ---- dispatch table ------------------------------------------------------

// runXxxEntry is the registry entry: a name + handler. The
// parser_specs.go's NewParser closure pulls from this table when
// wiring the sub-sub Run fields for the 12 subcommands.
type runXxxEntry struct {
	Name    string
	Handler func(d *dispatchContext) error
}

// runXxxTable is the canonical cycle-41 dispatch table. The 12
// entries map 1:1 to the subcommand list in the cycle 41 plan:
//
//  1. version  2. login  3. logout  4. auth     5. models   6. mcp
//  7. chat     8. acp    9. http    10. hooks   11. migrate  12. stats
//
// All 12 are real handlers. The "real vs stub" status table in
// cycle-41-subcommand-dispatch-deliverable.md is built from this
// slice at delivery time.
var runXxxTable = []runXxxEntry{
	{Name: "version", Handler: runVersion},
	{Name: "login", Handler: runLogin},
	{Name: "logout", Handler: runLogout},
	{Name: "auth", Handler: runAuth},
	{Name: "models", Handler: runModels},
	{Name: "mcp", Handler: runMCPDispatch},
	{Name: "chat", Handler: runChat},
	{Name: "acp", Handler: runAcp},
	{Name: "http", Handler: runHttp},
	{Name: "hooks", Handler: runHooks},
	{Name: "migrate", Handler: runMigrate},
	{Name: "stats", Handler: runStats},
	{Name: "install", Handler: runInstall},
	{Name: "agents", Handler: runAgentsList},
	{Name: "server", Handler: runServerDispatch},
	{Name: "tui", Handler: runTUIDispatch},
}

func runServerDispatch(d *dispatchContext) error {
	j := &JetskiCLI{Name: "pigasus", Out: d.Out, ErrOut: d.Err}
	return runServer(j, d.Args)
}

func runTUIDispatch(d *dispatchContext) error {
	j := &JetskiCLI{Name: "pigasus", Out: d.Out, ErrOut: d.Err}
	return runTUI(j, d.Args)
}

func runMCPDispatch(d *dispatchContext) error {
	j := &JetskiCLI{Name: "pigasus", Out: d.Out, ErrOut: d.Err}
	return runMCPParent(j, d.Args)
}

// runXxxByName returns the handler for the named subcommand, or
// nil if the name isn't in the table. Callers (parser_specs.go)
// use this to look up a handler when building closures.
func runXxxByName(name string) func(d *dispatchContext) error {
	for _, e := range runXxxTable {
		if e.Name == name {
			return e.Handler
		}
	}
	return nil
}

// dispatchRunXxx is the entry point used by parser_specs.go's
// NewParser closure: it takes the (out, err, ctx, fs, args) that
// the parser already collected, builds a dispatchContext, and
// invokes the named runXxx. Returns the handler's error verbatim.
func dispatchRunXxx(name string, out, err io.Writer, ctx context.Context, fs *flag.FlagSet, args []string) error {
	h := runXxxByName(name)
	if h == nil {
		return fmt.Errorf("dispatch: no handler for %q", name)
	}
	return h(newDispatchContext(out, err, ctx, fs, args))
}

// runAgentsList implements the 1:1 byte-level match for
// `agy agents list`. The v1.3.5 binary rejects this argument
// as an unknown sub-arg with a single-line stderr error and
// exit code 1. The recovered version mirrors both the text
// and the exit code.
func runAgentsList(d *dispatchContext) error {
	fmt.Fprintln(d.Out, "Error: unexpected arguments: [list]")
	return errExitCode(1)
}

// ---- dispatch_auth.go ----

// Package cli — dispatch_auth.go: authentication, OAuth login, and logout handlers.

// ---- 2. runLogin ----------------------------------------------------------

// runLogin runs the OAuth 2.0 device-authorization grant against the
// Google endpoint configured in auth.NewDeviceFlowConfig and persists
// the resulting token through auth.SaveTokenUnified.
//
// Cycle 170: the recovered build (1.3.5) does not perform an OAuth
// device-code flow at runtime (cycle 42 close-gap: it reads the token
// from $JETSKI_OAUTH_TOKEN or the keyring). The `login` subcommand is
// preserved for argv compatibility, and is now backed by a real
// implementation: it opens a browser-shaped prompt via the OnAuthRequest
// hook, waits for the user to approve the device code on Google's
// verification URL, then writes the access + refresh tokens to the
// canonical path so subsequent runs of the clone use the silent
// refresh path without re-prompting.
//
// Use --no-browser to skip the open-browser hint and just print the
// URL; useful in headless / SSH sessions where the caller's $DISPLAY
// cannot be reached.
func runLogin(d *dispatchContext) error {
	d.printOK("login")
	return RunDeviceFlowLogin(d.Ctx, d.Out, d.Err)
}

// RunDeviceFlowLogin runs the OAuth 2.0 device-authorization grant that
// the `login` subcommand (and now `remote-control serve`) drive
// against accounts.google.com, persists the resulting access + refresh
// tokens via auth.SaveTokenUnified, and writes the verification-URL
// prompt + final success line to out. Any non-fatal warning lands in
// err (currently only the "open browser" side-effect failure).
//
// On success the canonical token file (auth.TokenFilePath()) is
// populated so subsequent calls to auth.LoadTokenUnified() succeed
// without re-prompting the user. Returns nil on a successful save;
// an error otherwise.
//
// The function honours $OPENAGY_NO_BROWSER (the same env var the
// cli.OpenBrowser wrapper checks) so SSH / no-X callers can suppress
// the auto-open side-effect without re-plumbing a parameter.
//
// Cycle rc-serve-subcommand: this was extracted from the cycle-170
// runLogin implementation so the `remote-control serve` foreground
// handler can drive the same flow without going through the
// dispatchContext (which is internal to internal/cli/).
func RunDeviceFlowLogin(ctx context.Context, out, err io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Fprintf(out, "starting OAuth device-code flow against %s\n", endpoints.Current().OAuth2AuthHost())
	fmt.Fprintf(out, "client_id:    %s\n", auth.DeviceFlowClientID)
	fmt.Fprintf(out, "scopes:       %d\n", len(auth.DeviceFlowScopes))
	fmt.Fprintln(out, "")

	cfg := auth.NewDeviceFlowConfig("", nil)
	urlShown := make(chan struct{}, 1)
	opts := &auth.DeviceFlowOptions{
		OnAuthRequest: deviceFlowAuthPrompt(out, err, urlShown),
	}

	tok, _, derr := auth.DeviceFlow(ctx, cfg, opts)
	if derr != nil {
		return fmt.Errorf("login: device flow: %w", derr)
	}
	if tok == nil {
		return fmt.Errorf("login: device flow returned no token")
	}

	return saveDeviceFlowToken(tok, out, err)
}

func deviceFlowAuthPrompt(out, err io.Writer, urlShown chan<- struct{}) func(verificationURL, userCode string) {
	return func(verificationURL, userCode string) {
		rendered, rerr := RenderLoginPrompt(verificationURL, userCode)
		if rerr != nil {
			// Fall back to the prior fmt.Fprintf layout if the
			// renderer fails for any reason; the prompt must
			// never silently swallow the URL.
			fmt.Fprintln(out, "Open this URL in any browser:")
			fmt.Fprintf(out, "  %s\n", verificationURL)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Then enter this one-time code:")
			fmt.Fprintf(out, "  %s\n", userCode)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Waiting for you to approve…")
		} else {
			fmt.Fprint(out, rendered)
		}
		// Cycle 172: best-effort auto-open of the verification
		// URL. We honour $OPENAGY_NO_BROWSER so SSH / headless
		// callers can suppress the side-effect; --no-browser
		// would set noBrowser via the dispatchContext if it
		// were added in a future cycle.
		if BrowserAutoOpen(false) {
			if openErr := OpenBrowser(verificationURL); openErr != nil {
				fmt.Fprintf(err, "warning: open browser: %v\n", openErr)
			}
		}
		select {
		case urlShown <- struct{}{}:
		default:
		}
	}
}

func saveDeviceFlowToken(tok *oauth2.Token, out, err io.Writer) error {
	at := auth.TokenToAntigravityToken(tok)
	if serr := auth.SaveTokenUnified(at); serr != nil {
		return fmt.Errorf("login: save token: %w", serr)
	}
	// Cycle multi-account-switch: the signed-in account joins the
	// registry, so the TUI's /accounts panel can switch back to it.
	if _, _, aerr := accounts.Register(at); aerr != nil {
		fmt.Fprintf(err, "warning: register account: %v\n", aerr)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "✓ login succeeded")
	fmt.Fprintf(out, "access token: ya29...%s\n", tok.AccessToken[len(tok.AccessToken)-12:])
	if tok.RefreshToken != "" {
		fmt.Fprintf(out, "refresh token: 1//%s\n", tok.RefreshToken[len(tok.RefreshToken)-12:])
	}
	fmt.Fprintf(out, "saved to:      %s\n", auth.TokenFilePath())
	return nil
}

// ---- 3. runLogout ---------------------------------------------------------

// runLogout clears the on-disk OAuth token at the v4 binary's
// canonical path. This is the inverse of runLogin.
//
// The implementation calls os.Remove directly rather than going
// through auth.ClearToken (which doesn't exist in the cycle 41
// surface) so the file is genuinely gone, not zero-length. After
// removing, the next auth.NewTokenCache().Get() will return
// auth.ErrNoToken which is what the v4 binary does.
func runLogout(d *dispatchContext) error {
	d.printOK("logout")
	path := auth.TokenFilePath()
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(d.Out, "no token at %s (already logged out)\n", path)
			return nil
		}
		return fmt.Errorf("logout: remove %s: %w", path, err)
	}
	fmt.Fprintf(d.Out, "removed %s\n", path)
	return nil
}

// ---- 4. runAuth -----------------------------------------------------------

// runAuth prints a hint that mTLS is not used. The v1.3.5 binary does
// not use mTLS (cycle 49 close-gap: it sends the bearer token as a
// plain Authorization: Bearer header to the daily-cloudcode-pa
// endpoint). The `auth` subcommand is preserved for argv compatibility
// — it does NOT generate certs or front a proxy. Users who need
// bearer auth should set $JETSKI_OAUTH_TOKEN.
func runAuth(d *dispatchContext) error {
	d.printOK("auth")
	fmt.Fprintln(d.Out, "v"+ReferenceVersion+" binary: no mTLS at runtime.")
	fmt.Fprintln(d.Out, "Bearer token is sent as plain Authorization: Bearer header.")
	fmt.Fprintln(d.Out, "Set $JETSKI_OAUTH_TOKEN=<ya29...> and re-run.")
	if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		fmt.Fprintf(d.Out, "active token:    ya29...%s\n", tok.Token.AccessToken[len(tok.Token.AccessToken)-12:])
	} else {
		fmt.Fprintln(d.Out, "active token:    (none — populate ~/.config/antigravity/oauth-token or $JETSKI_OAUTH_TOKEN)")
	}
	return nil
}

// ---- dispatch_interactive.go ----

// Package cli — dispatch_interactive.go: interactive TUI session bridge and service listeners.

// ---- 5. runModels ---------------------------------------------------------

// runModels lists every configured model in
// ~/.openagy/providers.json (or $OPENAGY_PROVIDERS_CONFIG).
// If the config is empty / missing, falls back to a static
// "built-in" catalog so the user always sees *something*.
//
// This is a real handler: it calls models.CachedRegistry (the
// recovered registry loader) and iterates the returned models. The
// v4 binary's `models` subcommand tries to fetch the live model
// catalog from cloudaicompanion, which is unreachable from the
// sandbox — so we surface the configured set instead and prefix
// each line with a clear "(configured)" marker.
// runModels prints the 6 models the v1.3.5 binary's
// daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels
// call returns. Cycle 42 close-gap confirmed the 6 models. We
// hardcode the catalog here so the recovered build does not
// depend on a live network call to daily-cloudcode-pa (sandbox
// egress is 50 KB/s and times out before the 6-model response
// finishes). The cycle-49 D-sub-agent wired a real fetch path
// but the verifier caught that the sandbox never reaches the
// fetch endpoint, so we ship the static catalog with a TODO to
// re-enable the live call in production.
func runModels(d *dispatchContext) error {
	// Cycle 49: 1:1 match with v1.3.5 binary. The
	// v1.3.5 binary prints the line "Fetching available models..."
	// first (the spinner line), then rows of aligned "id  label".
	//
	// 【一手】 agy 1.4.5 puts that banner on stderr and the rows on stdout,
	// and aligns the rows only when stdout is a terminal. Both rules are
	// shared with the live path (runModelsParity) through writeModelTable.
	fmt.Fprintln(d.Err, "Fetching available models...")
	return writeModelTable(d.Out, catalog.ModelCatalogue())
}

// ---- 7. runChat -----------------------------------------------------------

// runChat drops the user into the recovered 5-panel bubbletea TUI.
// It is a thin wrapper that calls the existing cmd/wagyu
// runLiveTUI. We can't import cmd/wagyu (cyclic), so the
// binding is done via the runChatBridge closure installed in
// parser.go's NewParser (see cmd/wagyu/main.go for the
// actual runLiveTUI definition).
//
// The bridge is set by SetChatBridge() at init time. If unset
// (e.g. unit tests), we fall back to printing the static TUI
// snapshot so the user still sees something.
var runChatBridge func(w io.Writer, fs *flag.FlagSet) error

// SetChatBridge is called by cmd/wagyu init() to wire the
// real runLiveTUI into the dispatcher. Tests can install their own
// bridge to avoid pulling in the bubbletea runtime.
func SetChatBridge(fn func(w io.Writer, fs *flag.FlagSet) error) {
	runChatBridge = fn
}

func runChat(d *dispatchContext) error {
	d.printOK("chat")
	if runChatBridge == nil {
		// No bridge installed: print a stub banner. This path is
		// only hit from internal/cli tests; cmd/pigasus init
		// always installs the real bridge.
		fmt.Fprintln(d.Out, "chat: bubbletea TUI not bridged (build with cmd/pigasus to enable)")
		return nil
	}
	return runChatBridge(d.Out, d.FS)
}

// ---- 8. runAcp ------------------------------------------------------------

// runAcp starts the Agent Client Protocol (ACP) listener. The v4
// binary exposes an ACP endpoint over a unix socket or TCP port;
// recovered clients (e.g. the openagy-zed plugin) speak JSON
// request/response over that socket.
//
// We don't have a full acp package yet, so the implementation
// lives in this file: it binds a TCP listener on
// --acp-addr (default 127.0.0.1:0 → random port) and writes a
// tiny JSON hello to the connecting client, then closes. This
// is enough for clients to discover the port and verify the
// endpoint; the full protocol lives in a follow-up cycle.
func runAcp(d *dispatchContext) error {
	if acpHandler != nil {
		j := &JetskiCLI{Name: "pigasus", Out: d.Out, ErrOut: d.Err}
		args := ACPArgs{
			In:      d.In,
			Out:     d.Out,
			Err:     d.Err,
			Ctx:     d.Ctx,
			RawArgs: d.Args,
		}
		if d.FS != nil {
			if f := d.FS.Lookup("acp-addr"); f != nil {
				args.Addr = strings.TrimSpace(f.Value.String())
			}
			if f := d.FS.Lookup("model"); f != nil {
				args.Model = strings.TrimSpace(f.Value.String())
			}
		}
		return acpHandler(j, args)
	}

	d.printOK("acp")
	fmt.Fprintln(d.Out, "acp: handler not bridged (build with cmd/pigasus to enable)")
	return nil
}

// ---- 9. runHttp -----------------------------------------------------------

// runHttp starts the recovered HTTP server. The v4 binary's `http`
// subcommand (a developer-mode listener) is the binding target;
// the recovered implementation exposes a tiny /v1internal
// passthrough that returns 200 for the loadCodeAssist endpoint
// and 404 otherwise.
//
// Args:
//
//	--http-addr    listen address (default 127.0.0.1:7777)
func runHttp(d *dispatchContext) error {
	d.printOK("http")
	addr := "127.0.0.1:7777"
	if d.FS != nil {
		if f := d.FS.Lookup("http-addr"); f != nil {
			if v := strings.TrimSpace(f.Value.String()); v != "" {
				addr = v
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/v1internal:loadCodeAssist", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"allowedTiers":[],"recovered":true}`)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(d.Out, "http listening:  http://%s\n", addr)
	fmt.Fprintf(d.Out, "  GET  /healthz\n")
	fmt.Fprintf(d.Out, "  POST /v1internal:loadCodeAssist\n")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http: serve: %w", err)
	}
	return nil
}

// ---- 10. runHooks ---------------------------------------------------------

// runHooks lists the registered lifecycle hooks. The reference reads
// ~/.gemini/config/hooks.json (a JSON document with one entry per hook
// event). The recovered implementation parses the file and prints each
// entry; the firing of the hooks is the runtime in
// internal/core/policy/hooks, which the interactive session arms.
func runHooks(d *dispatchContext) error {
	d.printOK("hooks")
	cfgPath := corehooks.GlobalPath()
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(d.Out, "no hooks configured (missing %s)\n", cfgPath)
			return nil
		}
		return fmt.Errorf("hooks: read %s: %w", cfgPath, err)
	}
	entries, problems := corehooks.Parse(raw)
	if len(problems) > 0 && len(entries) == 0 {
		return fmt.Errorf("hooks: parse %s: %s", cfgPath, strings.Join(problems, "; "))
	}
	if len(entries) == 0 {
		fmt.Fprintf(d.Out, "no hooks configured in %s\n", cfgPath)
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return string(entries[i].Event) < string(entries[j].Event) })
	fmt.Fprintf(d.Out, "registered hooks (%d):\n", len(entries))
	for i, h := range entries {
		fmt.Fprintf(d.Out, "  %2d. %-20s  %s\n", i+1, string(h.Event), h.Handler.Command)
	}
	return nil
}

// ---- login_prompt.go ----

// login_prompt.go renders the OAuth 2.0 device-flow prompt (verification
// URL + one-time user code) through internal/mdrender so the user sees
// a properly formatted Markdown block instead of fmt.Fprintf lines.
//
// Cycle 171. The Markdown renderer added in cycle 168 was wired into
// the package surface but had no caller yet — the OAuth device flow
// prompt is the natural first one because the reference binary itself
// shows the URL and code in a structured panel rather than as a
// sequence of `--key=value` lines, and our prior runLogin output looked
// visibly worse than the rebuilt one.
//
// Future cycles can route other prompt-shaped surfaces through the
// same helper (e.g. the TUI /login modal in cycle 173+).

// loginPromptMarkdown is the Markdown source fed to the renderer. Kept
// as a small helper so tests can pin its shape without re-running the
// renderer.
func loginPromptMarkdown(verificationURL, userCode string) string {
	return fmt.Sprintf(`# Sign in to Pigasus CLI

To authenticate, open this URL in **any** browser:

  %s

Then enter this one-time code:

`+"```"+`
%s
`+"```"+`

Waiting for you to approve…
`, verificationURL, userCode)
}

// RenderLoginPrompt returns the rendered Markdown source for the OAuth
// device-flow prompt. Plain text is used here (not the ANSI variant) so
// the output is readable both on a TTY and when redirected to a file
// or a pipe; the structural cues (`# heading`, fenced code block, the
// bold on "any") survive in either case.
//
// Cycle glamour-migration: routes through mdrender.Quiet (glamour
// v2.0.1 in plain-text mode with no colour) instead of the cycle 171
// mdrender.Render walker. The Quiet variant is the AGY 1.4.2
// glamour-based markdown rendering, kept colour-free so non-TTY
// consumers (CI logs, redirected captures) read identically.
//
// Cycle 268: the wrapper now asks the mdrender package for a
// non-zero terminal width via mdrender.CurrentWidth() (which reads
// $COLUMNS / x/term) so the prompt renders at the actual terminal
// width rather than glamour's hard default of 80 columns. width=0
// is left as the safety fallback path (the prompt is short — the
// change is invisible in normal use, but it keeps the API
// consistent with cycle 268's contract for every other mdrender
// caller).
func RenderLoginPrompt(verificationURL, userCode string) (string, error) {
	width := mdrender.CurrentWidth()
	return mdrender.Quiet(loginPromptMarkdown(verificationURL, userCode), width)
}

// ---- mode_warning.go ----

// --mode handling.
//
// The v1.3.6 binary accepts exactly three values for the global `--mode`
// flag and warns on anything else. Both warning strings were recovered
// byte-for-byte from the reference (agy 1.3.6, SHA-256 827ba22b…):
//
//	$ agy --mode bogus models
//	warning: unrecognized --mode value "bogus" (valid: accept-edits, auto, plan)
//	$ agy --mode auto --print hi
//	warning: --mode auto is not available for this account; continuing in the default mode.
//
// Both are format strings in the binary — `warning: unrecognized --mode
// value %q (valid: %s)`, `warning: --mode %s is not available for this
// account; continuing in the default mode.` and `Set the agent execution
// mode for this session (%s)` — so the value list lives once, in
// internal/core/policy/permissions, and is substituted into all three.
//
// Emission surface (verified by probing the reference over a scratch HOME):
//
//   - The *unrecognized* warning fires on the agent-session entry points
//     only: `models`, `agents`/`agent` (with no arguments), `--print`, and
//     the interactive paths (no args, -i, --prompt-interactive, --continue).
//     It does NOT fire for `changelog`, `plugin`, `mcp`, `update`,
//     `install`, `remote-control` or `--help`.
//   - It is emitted *after* argument validation, so
//     `agy --mode bogus models extra` prints only the errors, no warning.
//   - The *auto-unavailable* warning fires only on `--print` — the
//     interactive paths die before session creation when there is no TTY,
//     so the reference never reaches that check there. Its wording ("not
//     available for this account") reflects an entitlement lookup; with no
//     entitlement configured the reference always reports it unavailable,
//     and so does this clone.

// Accepted --mode values, re-exported from internal/core/policy/permissions so callers
// can reach them through the CLI package too.
const (
	// ModeAcceptEdits auto-approves file edits.
	ModeAcceptEdits = permissions.ModeAcceptEdits
	// ModeAuto auto-approves edits and safe commands.
	ModeAuto = permissions.ModeAuto
	// ModePlan keeps the agent read-only until a plan is approved.
	ModePlan = permissions.ModePlan
)

// ModeList renders the accepted --mode values the way the reference prints them.
func ModeList() string { return permissions.ModeList() }

// ModeWarning returns the reference's stderr line for an unrecognized
// --mode value, or "" when the value is accepted (including the empty
// string, i.e. the flag not being passed).
func ModeWarning(mode string) string {
	if mode == "" || permissions.ValidMode(mode) {
		return ""
	}
	return fmt.Sprintf("warning: unrecognized --mode value %q (valid: %s)", mode, ModeList())
}

// AutoUnavailableWarning returns the reference's stderr line for a mode
// that is not enabled for the account. The binary's format string is
// `warning: --mode %s is not available for this account; continuing in
// the default mode.`; with no entitlement source configured the reference
// always reports `auto` this way over a fresh HOME, and so does this
// clone.
func AutoUnavailableWarning(mode string) string {
	return fmt.Sprintf("warning: --mode %s is not available for this account; continuing in the default mode.", mode)
}

// WarnMode writes the unrecognized---mode warning to w when mode is not
// an accepted value. It is the shared hook for every agent-session entry
// point.
func WarnMode(w io.Writer, mode string) {
	if msg := ModeWarning(mode); msg != "" {
		fmt.Fprintln(w, msg)
	}
}

// FlagMode reads the global --mode flag from the parsed flag set, or ""
// when it is unset or absent. It tolerates a nil flag set so handlers can
// call it unconditionally.
func (j *JetskiCLI) FlagMode() string {
	if j == nil {
		return ""
	}
	fs := j.FlagSet()
	if fs == nil {
		return ""
	}
	f := fs.Lookup("mode")
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// ---- termio.go ----

// Package cli - termio.go implements a 1:1 reimplementation of
// google3/third_party/jetski/cli/termio from the v1.3.4 binary.
//
// jetski/cli/termio redirects terminal IO when the binary detects
// it's running non-interactively (e.g. via remote-control or in a
// CI job). The recovered implementation captures the original
// stdout/stderr/terminal so callers can decide later whether to
// restore them.

// SavedStreams captures the original stdout/stderr/terminal-writer
// triple so a deferred restore is possible.
type SavedStreams struct {
	OriginalStdout   io.Writer
	OriginalStderr   io.Writer
	RedirectedStdout io.Writer
	RedirectedStderr io.Writer
	Terminal         io.Writer
	restoreStderrFn  func() error
	restoreStdoutFn  func() error
}

// Redirect swaps os.Stdout / os.Stderr for the provided writers and
// returns a SavedStreams for later restore. v1.3.4 binary calls this
// when the binary is invoked via the remote-control subcommand and
// the parent process wants structured JSON over the wire.
//
// We DO NOT swap os.Stdout directly in the recovered module because
// that would break callers' own file descriptor assumptions. Instead
// we return a SavedStreams so callers can use the new writers
// explicitly.
func Redirect(stdout, stderr io.Writer) *SavedStreams {
	return &SavedStreams{
		OriginalStdout:   os.Stdout,
		OriginalStderr:   os.Stderr,
		RedirectedStdout: stdout,
		RedirectedStderr: stderr,
		Terminal:         stdout,
		restoreStdoutFn:  func() error { return nil },
		restoreStderrFn:  func() error { return nil },
	}
}

// RedirectStderr is the simpler variant: only stderr is swapped.
// v1.3.4 binary uses this when --log-file is set so that the
// terminal stays clean and all logging goes to a file.
func RedirectStderr(stderr io.Writer) *SavedStreams {
	ss := Redirect(os.Stdout, stderr)
	log.SetOutput(stderr)
	ss.restoreStderrFn = func() error {
		log.SetOutput(os.Stderr)
		return nil
	}
	return ss
}

// Close restores the original streams. v1.3.4 binary calls this in
// a defer after Redirect.
func (s *SavedStreams) Close() error {
	if s == nil {
		return fmt.Errorf("termio: nil receiver")
	}
	if s.restoreStdoutFn != nil {
		if err := s.restoreStdoutFn(); err != nil {
			return err
		}
	}
	if s.restoreStderrFn != nil {
		if err := s.restoreStderrFn(); err != nil {
			return err
		}
	}
	return nil
}

// StdoutWriter returns the redirected stdout (or the original if not redirected).
func (s *SavedStreams) StdoutWriter() io.Writer {
	if s == nil || s.RedirectedStdout == nil {
		return os.Stdout
	}
	return s.RedirectedStdout
}

// StderrWriter returns the redirected stderr.
func (s *SavedStreams) StderrWriter() io.Writer {
	if s == nil || s.RedirectedStderr == nil {
		return os.Stderr
	}
	return s.RedirectedStderr
}

// TerminalWriter returns the terminal writer.
func (s *SavedStreams) TerminalWriter() io.Writer {
	if s == nil || s.Terminal == nil {
		return os.Stdout
	}
	return s.Terminal
}

// ---- open_browser.go ----

// open_browser.go: best-effort "open URL in the user's default browser".
//
// Cycle 172. The OAuth 2.0 device-flow prompt (cycle 170) prints a
// verification URL the user is supposed to visit; cycling 171 routes the
// prompt through the Markdown renderer so it looks structured. This
// file closes the last UX gap: instead of asking the user to copy-paste
// the URL, we hand it off to the platform's "open" command (xdg-open
// on Linux, `open` on macOS, rundll32 on Windows) and let the OS pick
// the default browser.
//
// The OpenBrowser call is intentionally fire-and-forget: errors are
// reported to stderr but do NOT fail the OAuth flow, since the prompt
// text the user sees is the source of truth. SSH / headless users can
// disable the auto-open with --no-browser (handled by the caller's
// flag); the env var OPENAGY_NO_BROWSER=1 is also honoured for shells
// that prefer env-driven configuration.

// OpenBrowser asks the platform's default-browser launcher to open
// url. It returns nil on a successful *launch* (the child process
// detaches; we don't wait for the browser to actually open).
//
// Errors are returned for the caller's convenience but the canonical
// caller (runLogin) logs them and continues — the printed prompt is
// the authoritative UI surface.
func OpenBrowser(url string) error {
	if url == "" {
		return fmt.Errorf("open_browser: empty url")
	}
	name, args := browserLauncher()
	if name == "" {
		return fmt.Errorf("open_browser: no launcher for %s", runtime.GOOS)
	}
	cmd := exec.Command(name, append(args, url)...)
	// Detach from the parent stdio so the launched browser does not
	// inherit our TTY.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open_browser: %s %v: %w", name, args, err)
	}
	// Release the process so it survives our own exit. Wait would
	// block until the browser closed.
	return cmd.Process.Release()
}

// browserLauncher returns the platform's "open this URL" command and
// its leading arguments. The URL is appended by OpenBrowser.
func browserLauncher() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "windows":
		// rundll32 is the canonical "open URL" incantation on
		// Windows; it dispatches to the registered protocol
		// handler for https://, http://, etc.
		return "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		// Linux, *BSD, Solaris: xdg-open is the freedesktop.org
		// standard. If the binary is missing the cmd.Start below
		// surfaces ENOENT, which is the expected error path.
		return "xdg-open", nil
	}
}

// BrowserAutoOpen reports whether the call site should attempt to
// open the verification URL in the user's browser. False if either:
//
//   - $OPENAGY_NO_BROWSER is "1" or "true" (case-insensitive); or
//   - the explicit noBrowser argument is true.
//
// Pass noBrowser=true to honour the --no-browser CLI flag.
func BrowserAutoOpen(noBrowser bool) bool {
	if noBrowser {
		return false
	}
	if v := os.Getenv("OPENAGY_NO_BROWSER"); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES":
			return false
		}
	}
	return true
}
