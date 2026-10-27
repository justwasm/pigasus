// Package render provides terminal-rendering helpers that work around
// two bubbletea v2 quirks the AGY clone hits when migrating to v2.0.9.
//
// Cycle terminal-rendering-bbt-v209 (AGY 1.2.10 changelog gap): the
// clone previously shipped bubbletea v2.1.0; the AGY 1.2.10 changelog
// line claims a bump to v2.0.9. v2.0.9 is the patch that tightened
// bubbletea's executeQuery path so terminal probes that are dropped by
// tmux or never arrive (CSI-14 t, OSC 11 background-color query) no
// longer leak the probe into the shell once the program exits. The
// two quirks the cycle documents:
//
//  1. tea.WindowSizeMsg.Width can be 0 when the program starts before
//     the first resize probe completes, or when the probe is dropped.
//     lipgloss's Width(0) collapses columns to nothing, which breaks
//     tables and styled boxes. The workarounds in this file pre-validate
//     the requested width and substitute a sane fallback (DefaultBoxWidth
//     when the caller passes 0, or a clamped floor when the caller
//     passes a negative number) so a missing-width terminal still
//     renders something legible.
//
//  2. OSC 8 hyperlinks emitted into a constrained-width cell overflow
//     without a trailing ST terminator, leaking the underline attribute
//     into the next line. The Hyperlink helper clamps the link text to
//     the cell width before emitting the closing ST, and Divider /
//     Table / Box are explicit about which cell widths they honour.
//
// render.Box, render.Divider, render.Table and render.Hyperlink emit
// ANSI / OSC bytes directly. They do not pull in lipgloss or
// colorprofile — the clone's byte-level-1:1-with-ref surfaces
// (cmd/wagyu/tui_run.go, cmd/wagyu/slash_skills_detail.go,
// internal/onboarding/wizard.go) continue to emit raw ANSI themselves,
// and this package only owns the helper layer the cycle adds. The
// package has no third-party dependencies; stdlib strings + bytes
// only, consistent with the research-tool scope rule recorded in
// AGENTS.md §Conventions.
//
// Cycle charmland-component-adoption (cycle 01): Table is now a thin
// wrapper around charm.land/lipgloss/v2/table, which is the same
// package AGY 1.4.2 ships (verified by `strings` against
// `~/.local/bin/agy | grep -c lipgloss/v2/table` = 82 hits). The
// wrapper preserves the public API (NewTable/SetColumnWidths/Row/Cell/
// Render/Widths/TotalWidth) so existing callers don't change. The
// internal implementation now uses table.New() from
// charm.land/lipgloss/v2, which means we gain lipgloss styling
// (style-per-cell via StyleFunc), bordered headers (BorderHeader),
// row alternation, and proper width-clamping without writing it
// ourselves. The output separator changed from " │ " (the old
// hand-rolled 3-cell "space-bar-space") to "│" (lipgloss's native
// 1-cell border column) — that is the AGY reference's separator as
// well. New gain: WithHeaders().WithStyles() chainable helpers for
// callers that want headers + per-cell styles.
package render

import (
	"github.com/charmbracelet/x/ansi"
)

// DefaultBoxWidth is the column width Box / Divider / Table use when
// the caller passes a non-positive width. It matches the recovered
// v1.3.6 wizard panel width (the wizard's box renders at 70 cells,
// and the divider pad is the same 70-cell budget — both recovered
// from /tmp/agycmp/ref-color.txt capture). A 70-cell default keeps the
// terminal-rendering helpers byte-compatible with the existing
// hand-rolled ASCII box in internal/onboarding/wizard.go when bubbletea
// hasn't reported a width yet.
const DefaultBoxWidth = 70

// MinColumnWidth is the smallest column width a Table cell is allowed
// to occupy. Cycle 218's markdown-table renderer uses 3 as its floor
// (so a shrunk URL column still reads "htt…" instead of being
// truncated to a single ellipsis). The same floor is exported here so
// render.Table's column-distribution fallback agrees with the existing
// markdown table.
const MinColumnWidth = 3

// clampRunes returns the first n runes of s as a string. ASCII fast
// path: when s is single-byte and len(s) <= n the original string is
// returned. The clamp is rune-aware so multi-byte glyphs don't get
// sliced mid-codepoint, which would otherwise produce a broken UTF-8
// byte in the divider / box border.
//
// Cycle 245: clampRunes is rune-count based (one rune = one slot). For
// CJK / emoji surfaces where each codepoint consumes two cells, the
// rune-count clamp over-trims (cuts after ~N/2 visible cells). Use
// truncateToCells instead for any surface that has visible cell
// width budget; clampRunes stays for byte-safe raw clamping (the byte
// budget the rune-count comment originally documented).
func clampRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// cellWidth returns the visible-cell width of s using ansi.StringWidth,
// which honours East Asian wide-char widths (CJK ideographs, hiragana,
// katakana, fullwidth forms each consume two cells), Unicode emoji
// (each emoji codepoint consumes two cells), and ignores ANSI escape codes.
func cellWidth(s string) int {
	return ansi.StringWidth(s)
}

// truncateToCells returns s shortened to at most maxCells terminal
// cells, with no ellipsis appended (the caller chooses the tail).
// The cutoff lands on a grapheme/rune boundary and does not break
// ANSI escape codes.
func truncateToCells(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	return ansi.Truncate(s, maxCells, "")
}
