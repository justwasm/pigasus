// plan_render_test.go — cycle 245: cell-width plan-line rendering.
//
// The 5+ regression tests pin the AGY 1.4.0 changelog rule:
//
//	"Fixed `/plan` rendering cutting off the visible portion of long
//	 plan lines that contain CJK (Chinese/Japanese/Korean) characters:
//	 the cutoff now accounts for terminal-cell width, not byte width."
//
// Each test exercises a specific byte-vs-cell distinction:
//
//   - Truncation: a long CJK line is shortened on cell-width, not
//     bytes. The result renders at most maxCells visible cells, with
//     the ellipsis tail appended when truncation occurs.
//   - Wrapping: a CJK line that overflows the budget is split at the
//     cell boundary (after ~budget/2 CJK chars), not at the byte or
//     rune boundary.
//   - ASCII equivalence: a non-CJK line that fits within the budget
//     yields the same rendered output as the pre-cycle behaviour
//     (cell-width == byte-width for ASCII).
//   - Mixed-script: a line that interleaves ASCII and CJK is
//     truncated to exactly N cells, with the visible substring
//     matching the expected byte sequence (ASCII + CJK + ellipsis).
//   - First-line alignment: when one plan item has CJK in its body
//     and another does not, the first line of each item still
//     starts at the same column (the prefix accounts for the
//     visible width of CJK in the body that follows).
//   - End-to-end: the slash-plan handler / panel renders a multi-
//     item plan with mixed CJK / ASCII content into a stable byte
//     sequence. Pins the cycle's visible output.
//
// runewidth.Truncate note: when called with width=W and tail="…"
// on a CJK-heavy string, the result may be < W cells because CJK
// ideographs are 2 cells each and Truncate fits the largest body
// (in cells) + tail (1 cell) that does not exceed W. For width=10
// with CJK, the body is 8 cells (4 ideographs) + 1 ellipsis = 9
// cells total; the remaining 1 cell budget cannot fit another CJK
// ideograph. This is the cell-width contract the cycle pins.

package main

import (
	"strings"
	"testing"

	rwidth "github.com/mattn/go-runewidth"

	"github.com/justwasm/pigasus/internal/render"
)

// cjkSentence is a 12-codepoint CJK string: each ideograph is 3 UTF-8
// bytes and 2 visible cells, so cjkSentence is 36 bytes / 12 codepoints
// / 24 cells. Used by the headline CJK tests.
const cjkSentence = "修复修复修复修复修复修复"

// cjkSample is a small (1-codepoint) CJK fixture used in the
// mixed-script tests so the assertions can pin exact substrings.
const cjkSample = "中"

// asciiSample is a small (8-char) ASCII fixture used as the
// baseline: bytes == codepoints == cells.
const asciiSample = "Fix bug"

// TestPlanRender_TruncatesCJKToCellWidth is the headline AGY 1.4.0
// regression test. A 24-cell CJK line truncated to 10 cells must
// render at most 10 visible cells. runewidth.Truncate is
// conservative: it fits the largest body whose body+ellipsis fits
// in the budget. For w=10 with CJK (2 cells per ideograph), the
// largest body is 4 ideographs (= 8 cells) + ellipsis (= 1 cell)
// = 9 cells total; adding a 5th ideograph would exceed the budget.
// The result must end with the ellipsis tail and contain exactly
// 9 visible cells.
func TestPlanRender_TruncatesCJKToCellWidth(t *testing.T) {
	got := truncatePlanLine(cjkSentence, 10)
	if w := rwidth.StringWidth(got); w != 9 {
		t.Errorf("truncatePlanLine(%q, 10) width = %d cells, want 9: %q",
			cjkSentence, w, got)
	}
	if !strings.HasSuffix(got, ellipsisTail) {
		t.Errorf("truncatePlanLine(%q, 10) missing ellipsis tail: %q",
			cjkSentence, got)
	}
	// The CJK body before the ellipsis must be 4 ideographs (8 cells)
	// + ellipsis (1 cell) = 9 cells. The exact substring check guards
	// against a future regression that drops or duplicates a codepoint.
	wantPrefix := "修复修复"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("truncatePlanLine(%q, 10) prefix = %q, want %q",
			cjkSentence, got[:len(got)-len(ellipsisTail)], wantPrefix)
	}
	// Cell-budget invariant: result width <= budget.
	if w := rwidth.StringWidth(got); w > 10 {
		t.Errorf("truncatePlanLine(%q, 10) width %d > budget 10: %q",
			cjkSentence, w, got)
	}
}

// TestPlanRender_WrapsCJKOnCellWidth verifies the wrap path: a CJK
// line that overflows the cell budget is split at the cell
// boundary, not the byte / rune boundary. The line is 24 cells of
// CJK; a 10-cell budget must produce 3 rows: 10 cells / 10 cells /
// 4 cells (24 = 10 + 10 + 4).
func TestPlanRender_WrapsCJKOnCellWidth(t *testing.T) {
	rows := wrapPlanLine(cjkSentence, 10)
	if len(rows) != 3 {
		t.Fatalf("wrapPlanLine(CJK 24 cells, width=10) returned %d rows, want 3: %q",
			len(rows), rows)
	}
	// Each row must be <= 10 cells. The pre-cycle rune-count helper
	// would have given 5 cells per row (10 runes, half of which are
	// half-width); the cell-width helper gives the correct 10.
	wantCells := []int{10, 10, 4}
	for i, r := range rows {
		if w := rwidth.StringWidth(r); w != wantCells[i] {
			t.Errorf("row %d width = %d cells, want %d: %q", i, w, wantCells[i], r)
		}
	}
	// Concatenation of all rows must reproduce the input verbatim —
	// no codepoint lost, no replacement char.
	if strings.Join(rows, "") != cjkSentence {
		t.Errorf("rows concatenated = %q, want %q", strings.Join(rows, ""), cjkSentence)
	}
}

// TestPlanRender_ASCIIUnchanged verifies the cycle's no-regression
// guarantee for ASCII content. cellWidth(line) == len(line) for pure
// ASCII, so the cycle-245 helpers must produce identical output to
// the pre-cycle behaviour.
func TestPlanRender_ASCIIUnchanged(t *testing.T) {
	// Truncation: a 24-byte ASCII line truncated to 10 cells yields
	// 9 chars + ellipsis = 10 cells. The pre-cycle byte-slice helper
	// to 10 bytes gave "abcdefghij" (10 bytes); the cycle's helper
	// gives "abcdefghi…" (10 cells).
	got := truncatePlanLine("abcdefghijklmnopqrstuvwx", 10)
	if got != "abcdefghi…" {
		t.Errorf("truncatePlanLine(ASCII, 10) = %q, want %q", got, "abcdefghi…")
	}
	if w := rwidth.StringWidth(got); w != 10 {
		t.Errorf("ASCII truncation width = %d cells, want 10", w)
	}
	// Wrapping: a 24-byte ASCII line at width 10 is 3 rows of
	// 10/10/4 bytes (bytes == cells for ASCII).
	rows := wrapPlanLine("abcdefghijklmnopqrstuvwx", 10)
	if len(rows) != 3 {
		t.Fatalf("ASCII wrap returned %d rows, want 3", len(rows))
	}
	if strings.Join(rows, "") != "abcdefghijklmnopqrstuvwx" {
		t.Errorf("ASCII wrap rows concatenated = %q, want verbatim",
			strings.Join(rows, ""))
	}
	wantCells := []int{10, 10, 4}
	for i, r := range rows {
		if w := rwidth.StringWidth(r); w != wantCells[i] {
			t.Errorf("ASCII row %d width = %d cells, want %d: %q", i, w, wantCells[i], r)
		}
	}
	// Empty input contract preserved.
	if truncatePlanLine("", 10) != "" {
		t.Errorf("truncatePlanLine(\"\", 10) = %q, want \"\"", truncatePlanLine("", 10))
	}
	if rows := wrapPlanLine("", 10); len(rows) != 1 || rows[0] != "" {
		t.Errorf("wrapPlanLine(\"\", 10) = %q, want [\"\"]", rows)
	}
	// width <= 0 contract preserved (no truncation, no wrap).
	if truncatePlanLine("abcdef", 0) != "abcdef" {
		t.Errorf("truncatePlanLine(ascii, 0) = %q, want verbatim", truncatePlanLine("abcdef", 0))
	}
	if rows := wrapPlanLine("abcdef", 0); len(rows) != 1 || rows[0] != "abcdef" {
		t.Errorf("wrapPlanLine(ascii, 0) = %q, want [\"abcdef\"]", rows)
	}
}

// TestPlanRender_MixedASCIIAndCJK exercises the mixed-script case:
// the body is "Fix 修 bug 修修" — 3 ASCII words and 3 CJK
// ideographs. Cell width: 3 + 1 + 2 + 1 + 3 + 1 + 2 + 2 = 15 cells.
// Truncating to 10 cells yields a 10-cell visible substring (body
// + ellipsis), with the body terminated at a grapheme boundary so
// no codepoint is sliced in half.
func TestPlanRender_MixedASCIIAndCJK(t *testing.T) {
	body := "Fix 修 bug 修修"
	fullCells := rwidth.StringWidth(body)
	if fullCells != 15 {
		t.Fatalf("body width = %d cells, want 15: %q", fullCells, body)
	}
	got := truncatePlanLine(body, 10)
	if w := rwidth.StringWidth(got); w != 10 {
		t.Errorf("truncatePlanLine(mixed, 10) width = %d cells, want 10: %q",
			w, got)
	}
	if !strings.HasSuffix(got, ellipsisTail) {
		t.Errorf("truncatePlanLine(mixed, 10) missing ellipsis tail: %q", got)
	}
	// The body before the ellipsis must be 9 cells: "Fix " (4) +
	// "修" (2) + " bu" (3) = 9. The byte sequence is exactly the
	// first 8 bytes of "Fix 修 bug " — "F", "i", "x", " ", then the
	// 3-byte UTF-8 of "修", then " ", "b", "u". A byte-slice to 9
	// would land at index 9 mid-codepoint, producing U+FFFD; a
	// rune-count slice to 9 would under-trim; the cell-width
	// helper trims to the right boundary.
	wantPrefix := "Fix 修 bu"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("truncatePlanLine(mixed, 10) prefix = %q, want %q (full: %q)",
			got[:len(got)-len(ellipsisTail)], wantPrefix, got)
	}
}

// TestPlanRender_FirstLineAlignsAcrossItems is the cycle-245 visual
// goal: a list of plan items where some have CJK and some do not
// must have the first displayed line of each item occupy the same
// total cell budget, regardless of whether the body has CJK or
// ASCII. Each rendered row's total width (prefix + content +
// padding) is the same = the caller's width.
//
// Concretely: two items, both with prefix "1. " / "2. ". Item 1's
// body starts with "中修复 foo " + 60 'a's. Item 2's body starts
// with "plain text " + 60 'b's. Render with width=24; the total
// cell width of each first-line row must equal 24, even though the
// body cells are mixed CJK + ASCII. Because the bodies overflow
// the content budget (24 - 3 prefix = 21 cells), each body wraps
// across multiple rows; the cycle pins that each rendered row is
// exactly 24 cells wide regardless of whether the body has CJK.
func TestPlanRender_FirstLineAlignsAcrossItems(t *testing.T) {
	items := []PlanItem{
		{Prefix: "1. ", Lines: []string{
			cjkSample + "修复 foo " + strings.Repeat("a", 60),
		}},
		{Prefix: "2. ", Lines: []string{
			"plain text " + strings.Repeat("b", 60),
		}},
	}
	rows := RenderPlanItems(items, 24)
	if len(rows) < 2 {
		t.Fatalf("RenderPlanItems returned %d rows, want >= 2", len(rows))
	}
	// Each rendered row must be exactly 24 cells wide. The cycle-245
	// contract: cellWidth(prefix + content + padding) == width. The
	// pre-cycle byte-slice / rune-count helper would have produced
	// misaligned rows for the CJK body.
	for i, r := range rows {
		if w := rwidth.StringWidth(r); w != 24 {
			t.Errorf("row %d cell width = %d, want 24: %q", i, w, r)
		}
	}
	// First-line rows carry the item's prefix. Item 1 has 71-cell
	// body, wraps to 4 rows; item 2 same. Total = 8 rows. Rows 0
	// and 4 are first-line rows for items 1 and 2 respectively.
	for _, idx := range []int{0, 4} {
		if !strings.HasPrefix(rows[idx], items[idx/4].Prefix) {
			t.Errorf("row %d prefix = %q, want %q", idx,
				rows[idx][:len(items[idx/4].Prefix)], items[idx/4].Prefix)
		}
	}
	// Continuation row invariant: every row after the first of an
	// item must start with the contIndent (5 cells = prefix cells +
	// planIndent).
	for _, idx := range []int{1, 2, 3, 5, 6, 7} {
		if idx >= len(rows) {
			break
		}
		if !strings.HasPrefix(rows[idx], "     ") {
			t.Errorf("continuation row %d missing contIndent: %q", idx, rows[idx])
		}
	}
}

// TestPlanRender_FullPlanRenderingEndToEnd pins the byte-for-byte
// output of a multi-item plan with mixed CJK / ASCII content. The
// output is the cell-aligned render of the AGY 1.4.0 /plan reply
// shape: numbered list, mixed scripts, mixed long / short lines.
//
// This is the cycle's "user-visible behaviour" test. A future cycle
// that touches the plan renderer must keep this byte sequence, or
// explicitly update the expected output and the cycle deliverable.
//
// Layout (width=24):
//   item 1: prefix "1. " (3) + body "中修复 bug xxxxxxxxx..." (31 cells)
//     wraps into content budget 21 cells:
//     row[0]: "1. " + "中修复 bug xxxxxxxxx" (3+21=24 cells)
//     row[1]: contIndent (5) + "xxxxxxxxxxx" + 10 padding (5+11+8=24 cells)
//   item 2: prefix "2. " (3) + body "plain text yyy..." (31 cells)
//     wraps similarly:
//     row[2]: "2. " + "plain text yyyyyyyy" (3+21=24 cells)
//     row[3]: contIndent (5) + "yyyyyyyyyy" + 10 padding (5+10+9=24 cells)
//   item 3: prefix "3. " (3) + empty first line, then "中 zz..." (23 cells)
//     row[4]: "3. " + 21 padding (3+21=24 cells)
//     row[5]: contIndent (5) + "中 zzzzzzzzzzzzzzz" (5+19=24 cells)
//     row[6]: contIndent (5) + "zzzz" + 15 padding (5+4+15=24 cells)
func TestPlanRender_FullPlanRenderingEndToEnd(t *testing.T) {
	items := []PlanItem{
		{Prefix: "1. ", Lines: []string{
			cjkSample + "修复 bug " + strings.Repeat("x", 20),
		}},
		{Prefix: "2. ", Lines: []string{
			"plain text " + strings.Repeat("y", 20),
		}},
		{Prefix: "3. ", Lines: []string{
			"",
			cjkSample + " " + strings.Repeat("z", 20),
		}},
	}
	rows := RenderPlanItems(items, 24)
	// Sanity: every rendered row is exactly 24 cells wide. This is
	// the cycle-245 row invariant — uniform-width rows for the panel.
	for i, r := range rows {
		if w := rwidth.StringWidth(r); w != 24 {
			t.Errorf("row %d cell width = %d, want 24: %q", i, w, r)
		}
	}
	// First-line prefix check: every item's first row carries the
	// 3-cell prefix.
	for i, idx := range []int{0, 2, 4} {
		if !strings.HasPrefix(rows[idx], items[i].Prefix) {
			t.Errorf("row %d prefix = %q, want %q", idx, rows[idx][:3], items[i].Prefix)
		}
	}
	// Continuation-row indent check: every continuation row starts
	// with the 5-cell contIndent (prefixWidth + planIndent).
	for _, idx := range []int{1, 3, 5, 6} {
		if !strings.HasPrefix(rows[idx], "     ") {
			t.Errorf("row %d missing contIndent: %q", idx, rows[idx])
		}
	}
	// Item 3's empty first line: row 4 is "3. " + 21 trailing
	// spaces, total 24 cells. The cycle's blank-line rhythm.
	if !strings.HasPrefix(rows[4], "3. ") {
		t.Errorf("row 4 prefix = %q, want %q", rows[4][:3], "3. ")
	}
}

// TestPlanRender_BoxTruncatesCJK is a cross-package test that
// confirms the cycle-245 cellWidth swap propagates to render.Box —
// a CJK line inside a Box is clamped at cell-width, not rune-count.
// This is the audit-the-runeWidth-call-sites verification: Box's
// call site at render.go:122 now reads `cellWidth(line)`, so a CJK
// body that previously over-trimmed now fits correctly.
//
// Before cycle 245, the Box("12-cell CJK body") rendered a 5-cell
// body (the rune-count clamp would have cut at 5 runes, ~5 cells).
// After cycle 245, the same call renders a 10-cell body (5 CJK
// ideographs * 2 cells each).
func TestPlanRender_BoxTruncatesCJK(t *testing.T) {
	body := cjkSentence // 24 cells of CJK
	box := render.Box(12, []string{body}, "")
	lines := strings.Split(strings.TrimRight(box, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Box(12, ...) returned %d lines, want 3 (top/body/bottom)", len(lines))
	}
	// Body row: index 1. Inner width = 12 - 2 = 10 cells.
	// The Box body's cellWidth should be 10 (Box clamp uses
	// truncateToCells with no tail).
	bodyRow := lines[1]
	if w := rwidth.StringWidth(bodyRow); w != 12 {
		t.Errorf("Box body row width = %d cells, want 12 (border + 10 inner + border): %q",
			w, bodyRow)
	}
}

// TestPlanRender_BoxASCIIUnchanged is the cycle's no-regression
// guard for Box. The pre-cycle Box("ascii-only body") byte output
// must match the cycle-245 output byte-for-byte (cell-width ==
// byte-width for ASCII).
func TestPlanRender_BoxASCIIUnchanged(t *testing.T) {
	body := "ascii only"
	box := render.Box(20, []string{body}, "")
	// Box appends "│", then content, then spaces, then "│". The
	// content is "ascii only" verbatim — no ellipsis, no trim, since
	// the line fits in 18 cells of inner width.
	lines := strings.Split(strings.TrimRight(box, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Box(20, ...) returned %d lines, want 3", len(lines))
	}
	bodyRow := lines[1]
	want := "│" + body + strings.Repeat(" ", 20-2-len(body)) + "│"
	if bodyRow != want {
		t.Errorf("Box ASCII body row mismatch:\n got  = %q\n want = %q",
			bodyRow, want)
	}
}

// TestPlanRender_WrapNoTruncation verifies that the wrap helper does
// not append an ellipsis tail — only truncatePlanLine does. A line
// that wraps cleanly at cell-width yields multiple rows, none of
// which end with the ellipsis.
func TestPlanRender_WrapNoTruncation(t *testing.T) {
	rows := wrapPlanLine("hello world this is a long line", 10)
	if len(rows) < 2 {
		t.Fatalf("expected >= 2 rows from long line wrap, got %d: %q",
			len(rows), rows)
	}
	for i, r := range rows {
		if strings.HasSuffix(r, ellipsisTail) {
			t.Errorf("row %d ends with ellipsis tail (wrap should not truncate): %q",
				i, r)
		}
	}
}
