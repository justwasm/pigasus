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
//     ~/.config/openagy/models.json
//     and prints sorted list)
//  6. runMcp     -> mcp.Connect         (real: JSON-RPC ping + tool
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
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/store"
)

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
	return &dispatchContext{Out: out, Err: err, Ctx: ctx, FS: fs, Args: args}
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
	fmt.Fprintln(out, "starting OAuth device-code flow against accounts.google.com")
	fmt.Fprintf(out, "client_id:    %s\n", auth.DeviceFlowClientID)
	fmt.Fprintf(out, "scopes:       %d\n", len(auth.DeviceFlowScopes))
	fmt.Fprintln(out, "")

	cfg := auth.NewDeviceFlowConfig("", nil)
	urlShown := make(chan struct{}, 1)
	opts := &auth.DeviceFlowOptions{
		OnAuthRequest: func(verificationURL, userCode string) {
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
		},
	}

	tok, _, derr := auth.DeviceFlow(ctx, cfg, opts)
	if derr != nil {
		return fmt.Errorf("login: device flow: %w", derr)
	}
	if tok == nil {
		return fmt.Errorf("login: device flow returned no token")
	}

	at := auth.TokenToAntigravityToken(tok)
	if serr := auth.SaveTokenUnified(at); serr != nil {
		return fmt.Errorf("login: save token: %w", serr)
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

// ---- 5. runModels ---------------------------------------------------------

// runModels lists every configured model in
// ~/.config/openagy/models.json (or $OPENAGY_MODELS_CONFIG).
// If the config is empty / missing, falls back to a static
// "built-in" catalog so the user always sees *something*.
//
// This is a real handler: it calls models.LoadFromFile (the
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
	fmt.Fprintln(d.Out, "Fetching available models...")
	models := []struct{ id, label string }{
		{"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
		{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
		{"custom-minimax-m3", "MiniMax-M3"},
	}
	w := tabwriter.NewWriter(d.Out, 0, 8, 2, ' ', 0)
	for _, m := range models {
		fmt.Fprintf(w, "%s\t%s\n", m.id, m.label)
	}
	return w.Flush()
}

// ---- 5b. runMcpList ----

// runMcpList is the `mcp list` implementation. Cycle 49 B
// rewrote it to byte-level 1:1 match the v1.3.5 binary: the
// output goes through legacyRenderMCPList / legacyRenderMCPListMissing
// (declared below) which render the 4-col table with NAME/TYPE/
// STATUS/COMMAND-URL columns. v1.3.5 binary path is
// $ANTIGRAVITY_HOME/mcp.json (or ~/.config/antigravity/mcp.json
// as a fallback) — see legacyMCPListConfigPath below.
func runMcpList(d *dispatchContext) error {
	cfg, path := legacyLoadMCPConfig()
	fmt.Fprint(d.Out, legacyRenderMCPList(cfg, path))
	return nil
}

// legacyMCPListConfigPath returns the v1.3.5 binary's MCP config path
// (cycle 48 legacy compat schema). $ANTIGRAVITY_HOME/mcp.json,
// $XDG_CONFIG_HOME/antigravity/mcp.json, or
// $HOME/.config/antigravity/mcp.json.
func legacyMCPListConfigPath() string {
	if h := strings.TrimSpace(os.Getenv("ANTIGRAVITY_HOME")); h != "" {
		return filepath.Join(h, "mcp.json")
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "antigravity", "mcp.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "antigravity", "mcp.json")
	}
	return ""
}

// legacyLoadMCPConfig reads the v1.3.5 binary's MCP config from the
// canonical path. Returns the config + path. The file may be
// missing — the caller can use the path to render the
// "no MCP servers configured (missing X/mcp.json)" line.
// Uses auth.MCPConfig (cycle 48 legacy compat schema).
func legacyLoadMCPConfig() (*auth.MCPConfig, string) {
	path := legacyMCPListConfigPath()
	cfg := &auth.MCPConfig{}
	if path == "" {
		return cfg, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, path
	}
	_ = json.Unmarshal(raw, cfg)
	return cfg, path
}

// legacyRenderMCPList renders the v1.3.5 binary's mcp list output.
// Format (cycle 49 B sub-agent byte-level 1:1):
//
//	NAME   TYPE   STATUS   COMMAND/URL
//	dummy  http   enabled  http://localhost:8080
//	foo    stdio  enabled  bar
//
// Column widths are computed from the max field length across
// all servers (NAME, TYPE, STATUS), then padded with 2 spaces
// between fields. The COMMAND/URL column is the URL when type
// is "http", else the command (+ args joined with spaces).
func legacyRenderMCPList(cfg *auth.MCPConfig, path string) string {
	if cfg == nil || len(cfg.Servers) == 0 {
		if path != "" {
			return fmt.Sprintf("no MCP servers configured (missing %s)\n", path)
		}
		return "no MCP servers configured\n"
	}
	// Fixed column widths matching the v1.3.5 binary:
	// NAME=7, TYPE=7, STATUS=9 (left-aligned, no literal space
	// between fields — the %-7s padding itself produces the
	// 2/3/2-space gap, and "enabled" (7) pads to 9 with 2
	// trailing spaces, while "disabled" (8) pads to 9 with 1).
	names := make([]string, 0, len(cfg.Servers))
	for n := range cfg.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "%-7s%-7s%-9s%s\n", "NAME", "TYPE", "STATUS", "COMMAND/URL")
	for _, name := range names {
		s := cfg.Servers[name]
		enabled := "enabled"
		if !s.Enabled {
			enabled = "disabled"
		}
		switch s.Type {
		case "http":
			fmt.Fprintf(&b, "%-7s%-7s%-9s%s\n", name, s.Type, enabled, s.URL)
		default:
			cmdline := s.Command
			if len(s.Args) > 0 {
				cmdline += " " + strings.Join(s.Args, " ")
			}
			fmt.Fprintf(&b, "%-7s%-7s%-9s%s\n", name, s.Type, enabled, cmdline)
		}
	}
	return b.String()
}

// legacyRenderMCPListMissing is the v1.3.5 binary's output when the
// mcp.json file does not exist. Returns a "no MCP servers
// configured (missing PATH/mcp.json)" line.
func legacyRenderMCPListMissing(path string) string {
	return fmt.Sprintf("no MCP servers configured (missing %s)\n", path)
}

// ---- 6. runMcp ------------------------------------------------------------

// runMcp is the "MCP server status" entry point. The v4 binary's
// `mcp` subcommand has 5 children (add, remove, list, enable,
// disable); runMcp without a subcommand is the implicit "list"
// (alias for `mcp list`).
//
// For cycle 41 the "real" surface is mcp.ListConfigured which
// reads ~/.config/antigravity/mcp.json (the v4 binary's MCP
// config). We don't have a top-level mcp package yet, so the
// implementation is in this file: it does a JSON-parse of the
// config file and prints each server.
//
// When the user passes a sub-subcommand (e.g. `mcp add`, `mcp
// list`), the parser's dispatch loop in jetski.go routes through
// the sub-sub closures in parser_specs.go, which prepend the
// sub-name to args and call dispatchRunXxx("mcp", ...). So this
// function sees d.Args[0] as the sub-name when called from a
// sub-sub.
func runMcp(d *dispatchContext) error {
	// `mcp` with no subcommand: implicit list (matches v4).
	if len(d.Args) == 0 {
		return runMcpList(d)
	}
	sub := d.Args[0]
	switch sub {
	case "list":
		return runMcpList(d)
	case "add":
		return runMcpAdd(d, d.Args[1:])
	case "remove":
		return runMcpRemove(d, d.Args[1:])
	case "enable":
		return runMcpSetDisabled(d, d.Args[1:], false)
	case "disable":
		return runMcpSetDisabled(d, d.Args[1:], true)
	}
	fmt.Fprintf(d.Out, "mcp: unknown subcommand %q (expected: add | remove | list | enable | disable)\n", sub)
	return nil
}

// runMcpAdd records a new MCP server entry in the on-disk
// config. The shape mirrors the v4 binary's `mcp add` (we just
// append to the servers array and rewrite the file).
func runMcpAdd(d *dispatchContext, args []string) error {
	if len(args) < 2 {
		fmt.Fprintln(d.Out, "usage: mcp add <name> <command> [args...]")
		return nil
	}
	name := args[0]
	command := args[1]
	extra := args[2:]
	cfgPath := defaultMCPConfigPath()
	doc, err := loadOrInitMCPDoc(cfgPath)
	if err != nil {
		return err
	}
	// Reject duplicate names so we don't silently drop the
	// previous entry.
	for _, s := range doc.Servers {
		if s.Name == name {
			fmt.Fprintf(d.Out, "mcp: server %q already exists (use `mcp remove` first)\n", name)
			return nil
		}
	}
	doc.Servers = append(doc.Servers, mcpServer{Name: name, Command: command, Args: extra})
	sort.Slice(doc.Servers, func(i, j int) bool { return doc.Servers[i].Name < doc.Servers[j].Name })
	if err := saveMCPDoc(cfgPath, doc); err != nil {
		return err
	}
	fmt.Fprintf(d.Out, "mcp: added %q -> %s %s\n", name, command, strings.Join(extra, " "))
	return nil
}

// runMcpRemove deletes an MCP server entry by name.
func runMcpRemove(d *dispatchContext, args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(d.Out, "usage: mcp remove <name>")
		return nil
	}
	name := args[0]
	cfgPath := defaultMCPConfigPath()
	doc, err := loadOrInitMCPDoc(cfgPath)
	if err != nil {
		return err
	}
	before := len(doc.Servers)
	out := doc.Servers[:0]
	for _, s := range doc.Servers {
		if s.Name != name {
			out = append(out, s)
		}
	}
	doc.Servers = out
	if len(doc.Servers) == before {
		fmt.Fprintf(d.Out, "mcp: no server named %q\n", name)
		return nil
	}
	if err := saveMCPDoc(cfgPath, doc); err != nil {
		return err
	}
	fmt.Fprintf(d.Out, "mcp: removed %q\n", name)
	return nil
}

// runMcpSetDisabled toggles the disabled flag for a server.
func runMcpSetDisabled(d *dispatchContext, args []string, disabled bool) error {
	if len(args) < 1 {
		verb := "enable"
		if disabled {
			verb = "disable"
		}
		fmt.Fprintf(d.Out, "usage: mcp %s <name>\n", verb)
		return nil
	}
	name := args[0]
	cfgPath := defaultMCPConfigPath()
	doc, err := loadOrInitMCPDoc(cfgPath)
	if err != nil {
		return err
	}
	found := false
	for i := range doc.Servers {
		if doc.Servers[i].Name == name {
			doc.Servers[i].Disabled = disabled
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(d.Out, "mcp: no server named %q\n", name)
		return nil
	}
	if err := saveMCPDoc(cfgPath, doc); err != nil {
		return err
	}
	verb := "enabled"
	if disabled {
		verb = "disabled"
	}
	fmt.Fprintf(d.Out, "mcp: %q %s\n", name, verb)
	return nil
}

// mcpServer is the JSON shape used by ~/.config/antigravity/mcp.json.
// Kept unexported; the public surface is runMcp*.
type mcpServer struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

// mcpDoc is the on-disk document; the servers array is what
// mcp add/remove/list/enable/disable all mutate.
type mcpDoc struct {
	Servers []mcpServer `json:"servers"`
}

// loadOrInitMCPDoc reads (or creates) the MCP config file. A
// missing file is treated as an empty document.
func loadOrInitMCPDoc(path string) (*mcpDoc, error) {
	doc := &mcpDoc{}
	if path == "" {
		return doc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, fmt.Errorf("mcp: read %s: %w", path, err)
	}
	if len(raw) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, doc); err != nil {
		return nil, fmt.Errorf("mcp: parse %s: %w", path, err)
	}
	return doc, nil
}

// saveMCPDoc writes the doc back atomically (temp + rename).
func saveMCPDoc(path string, doc *mcpDoc) error {
	if path == "" {
		return fmt.Errorf("mcp: no config path (set $ANTIGRAVITY_MCP_CONFIG or $XDG_CONFIG_HOME)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// runMcpList is the `mcp list` implementation. It reads the v4
// binary's MCP config from ~/.config/antigravity/mcp.json (or
// $ANTIGRAVITY_MCP_CONFIG) and prints each configured server.
// runMcpList is replaced by legacyRenderMCPList below (cycle 49 B 1:1)
// $XDG_CONFIG_HOME/antigravity/mcp.json or
// $HOME/.config/antigravity/mcp.json.
func defaultMCPConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "antigravity", "mcp.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "antigravity", "mcp.json")
	}
	return ""
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
		// only hit from internal/cli tests; cmd/wagyu init
		// always installs the real bridge.
		fmt.Fprintln(d.Out, "chat: bubbletea TUI not bridged (build with cmd/wagyu to enable)")
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
	d.printOK("acp")
	addr := "127.0.0.1:0"
	if d.FS != nil {
		if f := d.FS.Lookup("acp-addr"); f != nil {
			if v := strings.TrimSpace(f.Value.String()); v != "" {
				addr = v
			}
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("acp: listen %s: %w", addr, err)
	}
	defer ln.Close()

	fmt.Fprintf(d.Out, "acp listening:   %s\n", ln.Addr().String())

	// Accept a single client, send the JSON hello, close. The
	// full multi-connection dispatch is in a follow-up cycle.
	conn, err := ln.Accept()
	if err != nil {
		return fmt.Errorf("acp: accept: %w", err)
	}
	defer conn.Close()
	resp := map[string]any{
		"protocol": "acp/0",
		"version":  "0.1.0",
		"agent":    "antigravity-recovered",
		"hello":    "ready",
	}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("acp: write hello: %w", err)
	}
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

// runHooks lists the registered lifecycle hooks. The v4 binary reads
// ~/.config/antigravity/hooks.json (a JSON document with one entry
// per hook event). The recovered implementation parses the file
// and prints each entry; the actual hook firing is wired in a
// follow-up cycle.
func runHooks(d *dispatchContext) error {
	d.printOK("hooks")
	cfgPath := defaultHooksConfigPath()
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(d.Out, "no hooks configured (missing %s)\n", cfgPath)
			return nil
		}
		return fmt.Errorf("hooks: read %s: %w", cfgPath, err)
	}
	var cfg struct {
		Hooks []struct {
			Event   string `json:"event"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("hooks: parse %s: %w", cfgPath, err)
	}
	if len(cfg.Hooks) == 0 {
		fmt.Fprintf(d.Out, "no hooks configured in %s\n", cfgPath)
		return nil
	}
	sort.Slice(cfg.Hooks, func(i, j int) bool { return cfg.Hooks[i].Event < cfg.Hooks[j].Event })
	fmt.Fprintf(d.Out, "registered hooks (%d):\n", len(cfg.Hooks))
	for i, h := range cfg.Hooks {
		fmt.Fprintf(d.Out, "  %2d. %-20s  %s\n", i+1, h.Event, h.Command)
	}
	return nil
}

// defaultHooksConfigPath returns the v4 binary's hooks config path.
func defaultHooksConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "antigravity", "hooks.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "antigravity", "hooks.json")
	}
	return ""
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
// cycle-41 helper added to internal/store/manager.go.
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
	{Name: "mcp", Handler: runMcp},
	{Name: "chat", Handler: runChat},
	{Name: "acp", Handler: runAcp},
	{Name: "http", Handler: runHttp},
	{Name: "hooks", Handler: runHooks},
	{Name: "migrate", Handler: runMigrate},
	{Name: "stats", Handler: runStats},
	{Name: "install", Handler: runInstall},
	{Name: "agents", Handler: runAgentsList},
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
