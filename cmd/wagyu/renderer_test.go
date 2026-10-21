// renderer_test.go - cycle 256: pin
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS behaviour.
//
// The seven test cases below cover the AGY 1.1.19 release-note
// spec end-to-end:
//
//  1. TestRenderer_OptimizationsEnabledByDefault: env unset
//     -> escapeSequenceOptimizationsEnabled() returns true
//     (optimisations are on, by default).
//  2. TestRenderer_OptimizationsDisabledOn1: env = "1" -> false.
//  3. TestRenderer_OptimizationsDisabledOnTrue: env = "true" -> false.
//  4. TestRenderer_OptimizationsDisabledOnYes: env = "yes" -> false.
//  5. TestRenderer_OptimizationsCaseInsensitive: env = "TRUE" / "Yes"
//     / "1"  -> all false.
//  6. TestRenderer_OptimizationsEnabledOnGarbage: env = "garbage" ->
//     true (unrecognised values silently default to optimisations
//     on - matches the AGY 1.1.19 reading).
//  7. TestRenderer_PrintModeUnaffected: print mode (-p / --print /
//     "print") routes around the renderer seam; consultRenderer's
//     gate returns false and applyRenderer's output is ignored
//     regardless of the env var.
//
// Two further sections pin the applyRenderer / parseBoolish
// helpers' contracts and exercise the print-mode negation
// directly so a future regression on either helper is caught.
//
// test env handling: t.Setenv is the standard testing.T helper
// and isolates the env var to the test; cycle 256 deliberately
// does NOT cache the parsed value into a sync.Once (the
// cycle-256 deliverable's 硬约束 documents that variance), so
// each test sees its own value.
package main

import (
	"os"
	"strings"
	"testing"
)

// clearDisableEscapeSeqOptEnv unsets the cycle-256 env var for
// tests that exercise the unset default. Mirrors the
// blockedMCPClearGate helper in slash_mcp_blocked_test.go:59-65
// so a stray shell export does not silently leak into the test.
func clearDisableEscapeSeqOptEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envDisableEscapeSequenceOptimizations, "")
}

// TestRenderer_OptimizationsEnabledByDefault pins requirement #1:
// when AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS is unset,
// optimizations are enabled (the default). The accessor returns
// true; the underlying env reader returns false.
func TestRenderer_OptimizationsEnabledByDefault(t *testing.T) {
	clearDisableEscapeSeqOptEnv(t)

	// Defensive: also clear from os.Environ if a parent process
	// exported it. t.Setenv already covers the cwd-scoped env,
	// but the production code path (escape_optimizations.go:71)
	// reads os.Getenv, which consults the process environ. A test
	// that forgot to t.Setenv would fail here visibly.
	if got, want := os.Getenv(envDisableEscapeSequenceOptimizations), ""; got != want {
		t.Fatalf("env var leaked into default test: got %q want %q", got, want)
	}

	if !escapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be enabled by default; got disabled")
	}
	if escapeSequenceOptimizationsBypassed() {
		t.Fatal("env reader should report false on unset; got true")
	}
}

// TestRenderer_OptimizationsDisabledOn1 pins requirement #2:
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=1 disables
// optimizations.
func TestRenderer_OptimizationsDisabledOn1(t *testing.T) {
	t.Setenv(envDisableEscapeSequenceOptimizations, "1")

	if escapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be disabled when env = \"1\"; got enabled")
	}
	if !escapeSequenceOptimizationsBypassed() {
		t.Fatal("env reader should report true on \"1\"; got false")
	}
}

// TestRenderer_OptimizationsDisabledOnTrue pins requirement #3:
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=true disables
// optimizations.
func TestRenderer_OptimizationsDisabledOnTrue(t *testing.T) {
	t.Setenv(envDisableEscapeSequenceOptimizations, "true")

	if escapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be disabled when env = \"true\"; got enabled")
	}
}

// TestRenderer_OptimizationsDisabledOnYes pins requirement #4:
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=yes disables
// optimizations.
func TestRenderer_OptimizationsDisabledOnYes(t *testing.T) {
	t.Setenv(envDisableEscapeSequenceOptimizations, "yes")

	if escapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be disabled when env = \"yes\"; got enabled")
	}
}

// TestRenderer_OptimizationsCaseInsensitive pins requirement #5:
// the truthy match is case-insensitive AND whitespace-trimmed.
// Exercises TRUE, Yes, " 1 " (with surrounding whitespace), and
// 1 to make sure no canonicalisation slips through.
func TestRenderer_OptimizationsCaseInsensitive(t *testing.T) {
	cases := []struct {
		in   string
		want bool // expected envDisableEscapeSequenceOptimizations result
	}{
		{"TRUE", true},
		{"True", true},
		{"Yes", true},
		{"YES", true},
		{" 1 ", true}, // surrounding whitespace
		{"  true  ", true},
		{"yes\t", true}, // trailing tab
	}
	for _, tc := range cases {
		tc := tc
		t.Run("env="+tc.in, func(t *testing.T) {
			t.Setenv(envDisableEscapeSequenceOptimizations, tc.in)
			got := escapeSequenceOptimizationsBypassed()
			if got != tc.want {
				t.Fatalf("escapeSequenceOptimizationsBypassed() = %v, want %v (input %q)", got, tc.want, tc.in)
			}
			if got == escapeSequenceOptimizationsEnabled() {
				t.Fatalf("escapeSequenceOptimizationsEnabled() must be the inverse of env reader for %q", tc.in)
			}
		})
	}
}

// TestRenderer_OptimizationsEnabledOnGarbage pins requirement #6:
// unrecognized values silently default to the optimisations-on
// behaviour. This matches the AGY 1.1.19 reading of the spec
// ("bypass on recognized truthy values; otherwise use the
// optimisation path") and means mistyping the env name
// (e.g. AGY_CLI_DISABLE_ESCAPE_OPTIMIZATIONS) does not silently
// flip the renderer into the slow mode.
func TestRenderer_OptimizationsEnabledOnGarbage(t *testing.T) {
	cases := []string{
		"garbage",
		"on",     // "on" is NOT in the cycle-256 truthy set; OSC 7501 uses it (program_status.go:124), this gate does not.
		"no",     // "no" is in OSC 7501's off set; cycle 256 is one-sided.
		"2",      // only "1" trips the gate; "2" and similar look truthy but aren't.
		"truely", // close but not "true"
		"",       // empty (same as unset, but exercised explicitly)
		" ",      // whitespace-only -> trim yields "" -> false
		"\t\n",   // tabs+newlines only -> trim yields "" -> false
	}
	for _, in := range cases {
		in := in
		t.Run("env="+in, func(t *testing.T) {
			t.Setenv(envDisableEscapeSequenceOptimizations, in)
			if escapeSequenceOptimizationsBypassed() {
				t.Fatalf("env reader must return false on %q; got true", in)
			}
			if !escapeSequenceOptimizationsEnabled() {
				t.Fatalf("escapeSequenceOptimizationsEnabled() must remain true on %q; got false", in)
			}
		})
	}
}

// TestRenderer_PrintModeUnaffected pins requirement #7: print
// mode does not trigger the renderer code path. The
// shouldConsultRenderer gate returns false for the spelled-out
// "print", the shorthand "-p", and any case/whitespace variant
// of those, so the env var's value is irrelevant on -p. Every
// other mode (the default TUI, --tui, --prompt-interactive,
// mode omissions, etc.) returns true.
//
// Defensive: also confirm applyRenderer's output is ignored in
// print mode by combining the two helpers - this is the failure
// mode where a future cycle wires applyRenderer into runPrintMode
// by mistake.
func TestRenderer_PrintModeUnaffected(t *testing.T) {
	// Even with the gate active, print mode ignores it.
	t.Setenv(envDisableEscapeSequenceOptimizations, "1")

	printModes := []string{
		"print",
		"-p",
		"PRINT",  // case-insensitive
		"  -p  ", // surrounding whitespace
		"Print",  // mixed case
	}
	for _, mode := range printModes {
		mode := mode
		t.Run("mode="+mode, func(t *testing.T) {
			if shouldConsultRenderer(mode) {
				t.Fatalf("shouldConsultRenderer(%q) must be false; got true", mode)
			}
			// Optimisations are off globally, but print mode
			// doesn't consult the renderer at all. applyRenderer
			// is never called on the print path.
			if !escapeSequenceOptimizationsEnabled() {
				// sanity - the env reader reports the bypass is
				// armed. The point of this test is that shouldConsultRenderer
				// short-circuits BEFORE applyRenderer is reached.
				if shouldConsultRenderer(mode) {
					t.Fatalf("mode %q leaked into renderer; shouldConsultRenderer returned true", mode)
				}
			}
		})
	}

	// And every non-print mode DOES consult the renderer.
	consultingModes := []string{
		"", // empty string (e.g. mode flag omitted)
		"tui",
		"interactive",
		"--prompt-interactive",
		"-i",
		"live",
	}
	for _, mode := range consultingModes {
		mode := mode
		t.Run("consult="+mode, func(t *testing.T) {
			if !shouldConsultRenderer(mode) {
				t.Fatalf("shouldConsultRenderer(%q) must be true; got false", mode)
			}
		})
	}
}

// TestApplyRenderer_NoChangeOptimized: when previous and current
// view are byte-identical and optimisations are on, the renderer
// signals "no-op" by returning the empty string. This is the
// cycle-256 fast-path: bubbletea/v2's cursed_renderer would
// emit zero bytes when the screen state does not change, and
// applyRenderer pins that contract.
func TestApplyRenderer_NoChangeOptimized(t *testing.T) {
	view := "\x1b[2Jfull viewport\x1b[H"
	got := applyRenderer(view, view, true)
	if got != "" {
		t.Fatalf("expected empty (no-op) when prev == current and optimizations on; got %q", got)
	}
}

// TestApplyRenderer_NoChangeBypassed: even when the views match,
// the bypass (optimisations OFF) forces a full redraw. The
// "must still produce correct output" guarantee lives here: the
// emitted bytes are exactly `current`, with no diffing applied.
func TestApplyRenderer_NoChangeBypassed(t *testing.T) {
	view := "\x1b[2Jfull viewport\x1b[H"
	got := applyRenderer(view, view, false)
	if got != view {
		t.Fatalf("bypass mode must emit the full view; got %q want %q", got, view)
	}
}

// TestApplyRenderer_ChangeOptimized: when the view differs and
// optimisations are on, applyRenderer emits the full current view
// (the diff itself is deferred to bubbletea/v2's downstream
// cursed_renderer; cycle 256 exercises the gate, not the diff).
func TestApplyRenderer_ChangeOptimized(t *testing.T) {
	prev := "old frame"
	current := "\x1b[2Jnew frame\x1b[H"
	got := applyRenderer(prev, current, true)
	if got != current {
		t.Fatalf("view changed under optimizations must emit current; got %q want %q", got, current)
	}
}

// TestApplyRenderer_ChangeBypassed: view changed AND bypass
// active -> full redraw. Sanity pin that the bypass doesn't
// accidentally suppress the change.
func TestApplyRenderer_ChangeBypassed(t *testing.T) {
	prev := "old frame"
	current := "\x1b[2Jnew frame\x1b[H"
	got := applyRenderer(prev, current, false)
	if got != current {
		t.Fatalf("bypass mode must always emit current; got %q want %q", got, current)
	}
}

// TestApplyRenderer_EmptyViews: edge case where both previous
// and current frames are empty. With optimisations on, the
// match is still exact and empty, which returns "" (no-op).
// With optimisations off, the bypass still emits the current
// view, which is also "". This keeps the contract honest for
// the empty-string case.
func TestApplyRenderer_EmptyViews(t *testing.T) {
	if got := applyRenderer("", "", true); got != "" {
		t.Fatalf("empty views, optimized: expected \"\"; got %q", got)
	}
	if got := applyRenderer("", "", false); got != "" {
		t.Fatalf("empty views, bypassed: expected \"\"; got %q", got)
	}
	// Empty -> non-empty (or vice versa) is always a redraw.
	if got := applyRenderer("", "non-empty", true); got != "non-empty" {
		t.Fatalf("empty -> non-empty, optimized: expected \"non-empty\"; got %q", got)
	}
	if got := applyRenderer("non-empty", "", true); got != "" {
		t.Fatalf("non-empty -> empty, optimized: expected \"\"; got %q", got)
	}
}

// TestParseBoolish_EscapeSequenceOptimizations: small table test
// for the shared truthy parser from cycle 256's perspective. The
// canonical TestParseBoolish now lives in banner_test.go (cycle
// 255's home — the function itself moved to banner_env.go when
// cycle 259 consolidated the two byte-identical copies); this
// local copy keeps the cycle-256-specific case coverage that the
// shared test would otherwise lose ("on" explicitly rejected,
// "2" / "-1" rejected, etc.).
//
// Same case list the optimization tests use, plus a few extras
// the cycle-256 deliverable calls out specifically
// (mixed-case, surrounding whitespace, canonical pairs the OS
// 7501 parser handles differently).
func TestParseBoolish_EscapeSequenceOptimizations(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Truthy
		{"1", true},
		{"true", true},
		{"yes", true},
		{"TRUE", true},
		{"True", true},
		{"YES", true},
		{" 1 ", true},
		{"  true  ", true},
		{"\tyes\n", true},
		{"tRuE", true},
		// Falsy
		{"", false},
		{"garbage", false},
		{"on", false}, // cycle 256 does NOT accept "on"; OSC 7501 does.
		{"no", false}, // 1-sided parser; cycle 256 is opt-out via OTHER env vars.
		{"2", false},  // not "1"
		{"-1", false},
		{"0", false},     // note "0" is OSC 7501's off-set; cycle 256 is one-sided on "1".
		{"false", false}, // not "true"
		{"yesplease", false},
		{"   ", false}, // whitespace-only -> trim -> "" -> false
	}
	for _, tc := range cases {
		tc := tc
		name := strings.ReplaceAll(tc.in, "\t", "\\t")
		name = strings.ReplaceAll(name, "\n", "\\n")
		t.Run("in="+name, func(t *testing.T) {
			if got := parseBoolish(tc.in); got != tc.want {
				t.Fatalf("parseBoolish(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
