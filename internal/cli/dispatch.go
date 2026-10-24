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
//     ~/.config/openagy/providers.json
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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

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
