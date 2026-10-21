// Tests for the macOS Seatbelt profile generator (cycle
// macos-library-sandbox). The orchestrator session mandated unit tests
// of the profile string rather than executing the profile on the dev
// machine — the brief was explicit about not running sandbox-exec on
// this checkout. Each test pins one of the five properties the brief
// names: default deny, user override, platform no-op, $HOME
// interpolation, and Seatbelt syntax validity.
package permissions

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDefaultDenyPaths_IncludesApplicationSupport pins AGY 1.3.3
// changelog line 44: the default-deny list contains
// "Library/Application Support". If a future cycle removes it, this test
// fails — a deliberate tripwire so the security claim stays auditable.
func TestDefaultDenyPaths_IncludesApplicationSupport(t *testing.T) {
	want := "Library/Application Support"
	for _, p := range DefaultDenyPaths {
		if p == want {
			return
		}
	}
	t.Errorf("DefaultDenyPaths = %v, missing %q (AGY 1.3.3 line 44)", DefaultDenyPaths, want)
}

// TestRender_ReturnsEmptyOnNonDarwin pins the platform no-op property.
// On Linux/Windows the runner does not invoke sandbox-exec at all, so
// Render must return "". The same gate guards future cycles that wire
// the runner through to sandbox-exec.
func TestRender_ReturnsEmptyOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin assertion; this machine is darwin")
	}
	p := SeatbeltProfile{HomeDir: "/Users/example", UserOverrides: "; user"}
	if got := p.Render(); got != "" {
		t.Errorf("non-darwin Render() = %q, want empty", got)
	}
}

// TestRender_DenyForLibraryApplicationSupport pins the default-profile
// deny: the generated profile on darwin contains a
// `(deny file-read* (subpath ... "/Library/Application Support"))`
// rule, using `(param "HOME")` so the path follows the user's actual
// home directory.
func TestRender_DenyForLibraryApplicationSupport(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	out := SeatbeltProfile{HomeDir: "/Users/example"}.Render()
	if out == "" {
		t.Fatal("Render() returned empty on darwin")
	}
	// The Seatbelt form is split across lines; check the key tokens are
	// present in order rather than exact whitespace.
	wantSubstrs := []string{
		"(version 1)",
		"(deny default)",
		"(deny file-read*",
		`(subpath (string-append (param "HOME") "/Library/Application Support"))`,
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(out, s) {
			t.Errorf("Render() missing %q\n--- profile ---\n%s", s, out)
		}
	}
}

// TestRender_UsesUsersHomeNotLiteral pins $HOME interpolation: the deny
// rule references the home directory via `(param "HOME")`, never a
// hard-coded /Users/<name>. A literal would break the cycle's claim
// that the rule applies "to the user's actual $HOME".
func TestRender_UsesUsersHomeNotLiteral(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	for _, home := range []string{"/Users/alice", "/Users/bob", "/home/carol"} {
		out := SeatbeltProfile{HomeDir: home}.Render()
		if strings.Contains(out, home+"/Library/Application Support") {
			t.Errorf("Render(%q) hard-codes home in deny path:\n%s", home, out)
		}
		if !strings.Contains(out, `(param "HOME")`) {
			t.Errorf("Render(%q) missing (param \"HOME\") token:\n%s", home, out)
		}
	}
}

// TestRender_UserOverridesAppearAfterDefaultDeny pins the layering rule:
// the user's sandbox.sb contents go AFTER the default-deny so an
// explicit user `(allow file-read* ...)` for
// ~/Library/Application Support wins. Seatbelt evaluates rules in order
// and later allow wins over earlier deny.
func TestRender_UserOverridesAppearAfterDefaultDeny(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	// The user override repeats the path the default-deny denies. The
	// profile therefore has TWO `(deny file-read*` mentions — the
	// default one and the user one. To assert ordering we look at the
	// default-deny signature (the `(string-append ... "/Library/
	// Application Support")` half of the default deny) and check it
	// comes BEFORE the user override's `(allow file-read*` line.
	override := "(allow file-read*\n\t(subpath (string-append (param \"HOME\") \"/Library/Application Support\")))\n"
	out := SeatbeltProfile{
		HomeDir:       "/Users/example",
		UserOverrides: override,
	}.Render()

	denyIdx := strings.Index(out, `(subpath (string-append (param "HOME") "/Library/Application Support"))`)
	if denyIdx < 0 {
		t.Fatalf("profile missing default-deny signature:\n%s", out)
	}
	// The baseline allow rule `(allow file-read*)` (no path) is the
	// first allow in the profile; we want the SECOND one, which is the
	// user override. Locate by signature: the override's allow rule is
	// immediately preceded by the user-overrides comment marker.
	overrideStart := strings.Index(out, "; --- user-provided overrides")
	if overrideStart < 0 {
		t.Fatalf("profile missing user-overrides section marker:\n%s", out)
	}
	if overrideStart <= denyIdx {
		t.Fatalf("user override section appears before default-deny (override=%d deny=%d):\n%s",
			overrideStart, denyIdx, out)
	}
	if !strings.Contains(out[overrideStart:], override) {
		t.Fatalf("user override text not found after override marker:\n%s", out)
	}
}

// TestRender_EmptyHomeFailsClosed pins the belt-and-braces: passing an
// empty HomeDir (e.g. caller forgot os.UserHomeDir()) must return ""
// rather than emitting `(string-append "" "/Library/Application Support")`
// which would silently deny reads of /Library/Application Support —
// the opposite of the cycle's intent.
func TestRender_EmptyHomeFailsClosed(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	if got := (SeatbeltProfile{HomeDir: ""}).Render(); got != "" {
		t.Errorf("Render(\"\") = %q, want empty (fail closed)", got)
	}
}

// TestRender_ProfileIsValidSeatbeltSyntax pins the syntactic shape of
// the output. The check is intentionally lightweight: it confirms
// every top-level `(` is matched by a `)` at column 0 (the canonical
// Seatbelt top-level form). A profile with a stray paren would be
// rejected by sandbox-exec with a parse error — the test catches that
// here, in unit-test land, rather than at runtime.
func TestRender_ProfileIsValidSeatbeltSyntax(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	out := SeatbeltProfile{
		HomeDir:       "/Users/example",
		UserOverrides: "(allow file-read* (subpath \"/tmp/extra\"))\n",
	}.Render()
	if out == "" {
		t.Fatal("Render() empty on darwin")
	}
	open, close := strings.Count(out, "("), strings.Count(out, ")")
	if open != close {
		t.Fatalf("Render() paren imbalance: %d open vs %d close\n%s", open, close, out)
	}
	// Sanity: walk past leading `;` comments and blank lines; the first
	// non-comment, non-blank line must be `(version 1)`. Seatbelt
	// requires the version declaration on the very first top-level
	// form; comments before it are tolerated but blank/comment lines
	// after it are not.
	firstTopLevel := ""
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		firstTopLevel = trimmed
		break
	}
	if firstTopLevel != "(version 1)" {
		t.Fatalf("Render() first non-comment, non-blank line = %q, want \"(version 1)\"\n--- profile ---\n%s",
			firstTopLevel, out)
	}
}

// TestRender_ExtraDenyPathsLayered pins that ExtraDenyPaths is layered
// in addition to DefaultDenyPaths. A future cycle that adds more
// default-deny entries (e.g. ~/Library/Logs) should pass them via this
// field rather than mutating the package-level DefaultDenyPaths slice.
func TestRender_ExtraDenyPathsLayered(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	out := SeatbeltProfile{
		HomeDir:        "/Users/example",
		ExtraDenyPaths: []string{"Library/Logs/MyApp"},
	}.Render()
	if !strings.Contains(out, `/Library/Logs/MyApp`) {
		t.Errorf("Render() missing extra deny path:\n%s", out)
	}
	if !strings.Contains(out, `/Library/Application Support`) {
		t.Errorf("Render() missing baseline deny path:\n%s", out)
	}
}

// TestLoadUserSandboxProfile_MissingFileIsEmpty pins the load helper:
// a missing ~/.gemini/config/sandbox.sb is NOT an error, so the runner
// proceeds with the default-deny only.
func TestLoadUserSandboxProfile_MissingFileIsEmpty(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	home := t.TempDir()
	body, found, err := LoadUserSandboxProfile(home)
	if err != nil {
		t.Fatalf("LoadUserSandboxProfile: %v", err)
	}
	if found {
		t.Errorf("found = true for missing file, body = %q", body)
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

// TestLoadUserSandboxProfile_ReadsFile pins the happy path: when
// ~/.gemini/config/sandbox.sb exists, its contents come back verbatim.
func TestLoadUserSandboxProfile_ReadsFile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only assertion")
	}
	home := t.TempDir()
	dir := filepath.Join(home, filepath.Dir(DefaultSandboxProfilePath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := "; user allow rule\n(allow file-read* (subpath \"/tmp/foo\"))\n"
	if err := os.WriteFile(filepath.Join(home, DefaultSandboxProfilePath), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	body, found, err := LoadUserSandboxProfile(home)
	if err != nil {
		t.Fatalf("LoadUserSandboxProfile: %v", err)
	}
	if !found {
		t.Errorf("found = false, want true")
	}
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// TestLoadUserSandboxProfile_NoopOnNonDarwin pins the platform guard:
// on Linux/Windows the file is not read and the function returns
// "", false, nil. Same shape as the Render non-darwin assertion.
func TestLoadUserSandboxProfile_NoopOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin assertion; this machine is darwin")
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, DefaultSandboxProfilePath), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, found, err := LoadUserSandboxProfile(home)
	if err != nil || body != "" || found {
		t.Errorf("non-darwin LoadUserSandboxProfile = (%q, %v, %v), want (\"\", false, nil)", body, found, err)
	}
}
