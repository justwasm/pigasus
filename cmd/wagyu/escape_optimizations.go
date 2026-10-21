// escape_optimizations.go - cycle 256: AGY 1.1.19 release-note parity.
//
// AGY 1.1.19 changelog:
//
//	"Added `AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS` to bypass
//	 the renderer's dirty-rectangle and diffing optimizations."
//
// The reference binary's terminal renderer (the one that streams
// normalized escape sequences into the PTY when running the live
// TUI) tracks the previous frame and emits only the dirty rows
// for the next frame, instead of redrawing the entire viewport.
// On terminals that mishandle one of the cursor-optimization
// escapes the diffing path produces visible artefacts (the
// print-changelog string at
// `internal/cli/testdata/changelog_reference.txt:459` is the
// release-note line itself). Setting
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=1 forces the
// renderer to skip diffing and redraw the full viewport on every
// frame, at the cost of throughput.
//
// Cycle 256 ships the gate the same way cycle 239 shipped the
// OSC-7501 gates (`envOSC7501Off()` / `envOSC7501On()` in
// program_status.go): a parseBoolish helper, an env-var accessor,
// a public inverse for callers that prefer the positive form, and
// a test-only reset hook so each unit test can drive a fresh env
// value through t.Setenv without carrying state across cases.
//
// The actual optimize/bypass decision lives in renderer.go's
// applyRenderer; bubbletea/v2's cursed_renderer does the
// frame-level diffing internally and is not replaced here - the
// chasm.land/bubbletea stack is a third-party dependency we don't
// fork. applyRenderer pins the visible behaviour cycle 256 needs:
// optimizations on + frame unchanged -> no-op; everything else ->
// emit the full current view. Print mode skips the renderer
// entirely and is unaffected (see shouldConsultRenderer), which
// matches the cycle-256 instruction "print mode (`-p`) doesn't
// trigger the renderer code path (it's a TUI concern)".
package main

import (
	"os"
)

// envDisableEscapeSequenceOptimizations is the AGY_CLI_* spelling
// verbatim from the AGY 1.1.19 release note. Held as a constant
// so the test file and the production reader cannot drift.
const envDisableEscapeSequenceOptimizations = "AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS"

// parseBoolish returns true for the canonical AGY_CLI_* truthy
// set after a whitespace-trimmed lowercase fold:
//
//	"1"   -> true
//	"true" -> true
//	"yes" -> true
//
// Anything else - empty string, "on", "garbage", mixed case
// without trimming, surrounding tabs, etc. - returns false. The
// shape is the same `parseBoolish` pattern cycle 255's
// AGY_CLI_HIDE_LOGO helper used (cycle 256 was not merged at
// the time cycle 255 ran, so cycle 256 ships its own copy; both
// gates will collapse to a shared helper if a future cycle
// merges them). The set deliberately excludes "on" so the parser
// is symmetric with the existing on/off semantics AGY exposes
// for OSC 7501 (program_status.go's envOSC7501Off accepts
// "off"/"false"/"0"/"no" but the truthy side there accepts
// "on"/"true"/"1"/"yes"). Future cycles that want "on" can
// extend the case list without changing behaviour for the
// existing strings - the parser is intentionally additive.
//
// Cycle 259: parseBoolish was previously defined locally in this
// file (cycle 256) AND in banner_env.go (cycle 255) — two
// byte-identical definitions that broke the package once both
// merges landed on main (the cycle 256 comment above anticipated
// this). The duplicate was consolidated: the canonical definition
// lives in banner_env.go and this file calls it. The package is
// `main`, so both files compile to the same binary and the test
// files (banner_test.go / renderer_test.go) see one symbol.

// escapeSequenceOptimizationsBypassed reports whether
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS requests that
// the renderer skip dirty-rectangle and diffing optimizations.
// False on unset (the default), true when the parsed env value
// matches the parseBoolish truthy set.
//
// The AGY 1.1.19 changelog pins the read "at startup". Reading
// os.Getenv per call is stdlib-cheap enough that cycle 256
// accepts the variance over the literal wording; the cycle-256
// deliverable's 硬约束 section documents the deviation and the
// reasoning (test determinism - a once-cached read would force
// each test to call a reset hook, complicating t.Setenv-based
// fixtures).
//
// Named with the "Bypassed" past participle to distinguish it
// from the envDisableEscapeSequenceOptimizations constant
// above (Go does not differentiate symbols by intended kind).
func escapeSequenceOptimizationsBypassed() bool {
	return parseBoolish(os.Getenv(envDisableEscapeSequenceOptimizations))
}

// escapeSequenceOptimizationsEnabled is the positive-form inverse
// of escapeSequenceOptimizationsBypassed. True when the renderer
// is allowed to use optimizations (the default), false when the
// env var has asked to bypass them. Test code and the renderer
// seam in renderer.go prefer this form because the natural
// branch is "if optimizations are on, do the fast thing".
func escapeSequenceOptimizationsEnabled() bool {
	return !escapeSequenceOptimizationsBypassed()
}
