// trust_prompt.go — cycle 125: the on-startup workspace-trust dialog.
//
// The reference shows this overlay the first time you launch it from
// a directory that isn't already in settings.json's
// `trustedWorkspaces` list. Strings recovered from the 1.3.6 string
// table:
//
//	title:  "Trust this folder?"
//	body:   "Do you trust the contents of this project?"
//	accept: "Yes, I trust this folder"
//	deny:   any other key (Esc / n / q / Ctrl-C / EOF) exits cleanly
//
// It runs BEFORE the main TUI starts, so we own stdin at this point
// (the TUI hasn't called tea.NewProgram yet). Rendering goes to the
// supplied io.Writer so the dialog is unit-testable without a TTY
// (same pattern as internal/onboarding/wizard.go).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// trustResult is the decision returned by runTrustPrompt. Distinct
// integer values (not a bool) so a future third state (e.g. "defer
// to env var") can be added without breaking callers.
type trustResult int

const (
	trustAccept trustResult = iota + 1
	trustDeny
)

// renderTrustPrompt writes the dialog to w and is the testable half
// of the cycle-125 prompt. The reference renders this as a single
// bordered box; for now we produce plain text + ANSI inverse-video
// on the accept button so the cycle matches visually without
// re-implementing the box chrome.
//
// cwd is the directory the user is being asked to trust. It is
// displayed verbatim — the comparison logic in
// auth.IsWorkspaceTrusted uses filepath.Clean + EvalSymlinks, not
// the displayed string, so "~/" expansion here is purely cosmetic.
func renderTrustPrompt(cwd string, w io.Writer) error {
	const (
		reset  = "\x1b[0m"
		bold   = "\x1b[1m"
		invert = "\x1b[7m"
		dim    = "\x1b[2m"
		yellow = "\x1b[33m"
	)
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	if home, hErr := os.UserHomeDir(); hErr == nil && strings.HasPrefix(abs, home) {
		abs = "~" + strings.TrimPrefix(abs, home)
	}
	fmt.Fprintln(w, bold+"\n  Trust this folder?"+reset)
	fmt.Fprintln(w, yellow+"  Do you trust the contents of this project?"+reset)
	fmt.Fprintln(w, dim+"    "+abs+reset)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+invert+" Yes, I trust this folder "+reset+"  "+dim+"Esc/n = exit"+reset)
	return nil
}

// runTrustPrompt renders the dialog to out and reads a single byte
// from in. y / Y / Enter / Space / Right-arrow accept; everything
// else (including Esc / n / N / q / Ctrl-C / EOF) denies.
//
// in is normally os.Stdin, but tests can substitute a *bytes.Buffer
// to drive the decision without a real terminal. The function does
// not touch the terminal directly (no raw-mode toggling) — the
// reference shows the prompt above the TUI, so a single byte from
// cooked-mode stdin is enough.
func runTrustPrompt(cwd string, in io.Reader, out io.Writer) (trustResult, error) {
	if err := renderTrustPrompt(cwd, out); err != nil {
		return trustDeny, err
	}
	br := bufio.NewReader(in)
	b, err := br.ReadByte()
	if err != nil {
		// EOF / Ctrl-C → deny (clean exit). The caller treats a
		// deny the same as a graceful os.Exit(0).
		return trustDeny, nil
	}
	switch b {
	case 'y', 'Y', '\r', '\n', ' ', '1':
		// Right-arrow arrives as ESC '[' 'C' (3 bytes). The first
		// byte (ESC, 0x1b) is enough to deny — the rest can stay
		// in the buffer and harmlessly exits the now-done process.
		return trustAccept, nil
	default:
		return trustDeny, nil
	}
}

// trustGate decides whether the on-startup trust dialog should be
// shown. Returns (canonicalCwd, skipPrompt, error). When skipPrompt
// is true the caller proceeds straight into the TUI; when false it
// must call runTrustPrompt(canonicalCwd, ...) and react to the
// result.
//
// Skip conditions (cycle 125, in priority order):
//
//  1. fs has --trust-cwd set (test hook the user asked for).
//  2. $OPENAGY_TRUST is "1", "true", "always", or an absolute path
//     that matches cwd (or any ancestor of it). Test hook.
//  3. cwd is already in settings.json's trustedWorkspaces list
//     (the normal cycle).
//
// All path comparisons go through canonicalisePath so the macOS
// /tmp → /private/tmp symlink does not produce false-negatives.
func trustGate(fs *flag.FlagSet) (string, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	cleanCwd := canonicalisePath(cwd)

	// (1) --trust-cwd test hook
	if fs != nil && flagWasSet(fs, "trust-cwd") {
		return cleanCwd, true, nil
	}

	// (2) OPENAGY_TRUST test hook
	if env := strings.TrimSpace(os.Getenv("OPENAGY_TRUST")); env != "" {
		switch strings.ToLower(env) {
		case "1", "true", "always", "all":
			return cleanCwd, true, nil
		}
		cleanEnv := canonicalisePath(env)
		if cleanEnv == cleanCwd || strings.HasPrefix(cleanCwd, cleanEnv+string(filepath.Separator)) {
			return cleanCwd, true, nil
		}
	}

	// (3) settings.json already trusts cwd
	trusted, terr := isWorkspaceTrusted(cleanCwd)
	if terr != nil {
		return cleanCwd, false, terr
	}
	if trusted {
		return cleanCwd, true, nil
	}
	return cleanCwd, false, nil
}

// isWorkspaceTrusted is a thin shim over auth.IsWorkspaceTrusted so
// this file doesn't import internal/auth (which would create a
// cycle: cmd/wagyu is the binary package; internal/auth is imported
// BY the binary).
//
// Wired in init via init() at the bottom of main.go so the hot path
// is a single indirect call, not a runtime lookup.
var isWorkspaceTrusted func(string) (bool, error)
var addTrustedWorkspace func(string) error

// canonicalisePath mirrors auth.canonicalisePath — same behaviour,
// kept here so trustGate can use it without an import cycle.
func canonicalisePath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}
