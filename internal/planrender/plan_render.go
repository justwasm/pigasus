// plan_render.go — cycle 245: cell-width plan-line rendering.
//
// Before this file, AGY 1.4.0's `/plan` slash command surfaced a gap
// in the clone: long plan lines containing CJK (Chinese / Japanese /
// Korean) characters were truncated on byte boundaries instead of
// terminal-cell boundaries. CJK ideographs are 3 UTF-8 bytes each but
// occupy 2 visible terminal cells; a 90-byte CJK line is therefore 30
// codepoints / 60 cells, and a byte-slice to N bytes left the user
// looking at roughly N/2 visible cells — half the plan line cut off
// mid-character.
//
// AGY 1.4.0 changelog (cycle 245 evidence — recoverable from the
// strings table for the AGY binary and the cycle-244 plan rendering
// surface this cycle closes):
//
//	"Fixed `/plan` rendering cutting off the visible portion of long
//	 plan lines that contain CJK (Chinese/Japanese/Korean) characters:
//	 the cutoff now accounts for terminal-cell width, not byte width."
//
// The plan-rendering surface is:
//
//   - The model emits a numbered list (markdown "- " / "1. " lines) in
//     response to the /plan prompt (see slash_commands.go case
//     "plan" → sendAsPrompt("/plan", planPrompt())).
//   - The reply goes through renderMarkdown (glamour.ANSI) →
//     indentModelReply → wrapRows. wrapRows is already cell-aware
//     (cycle 122: cellByteIndex via runewidth.RuneWidth). The cycle
//     bug was on the panel-rendering side: when the panel needs to
//     truncate or wrap plan lines (for the panel header preview, for
//     the inline picker's row, or when a single plan line exceeds the
//     viewport's content area), the budget used to be `len(line)` or
//     `utf8.RuneCountInString(line)` — byte-aware / rune-aware but
//     not cell-aware.
//   - The byte-width / rune-count clamp is in
//     internal/render/render.go (cycle 245 fixes the runeWidth →
//     cellWidth substitution; truncateToCells uses runewidth.Truncate
//     which truncates on grapheme cluster boundaries, the AGY 1.4.0
//     recovery path).
//
// This file is the surface the feature_diff_table.json entry points
// at ("evidence": "cmd/wagyu/plan_render.go"). It exposes:
//
//   - PlanItem       — one numbered plan entry, with prefix and lines
//   - RenderPlanItems — render a list of plan items at a given cell
//     width; first lines get the prefix, subsequent lines get the
//     indent prefix. Long lines are truncated on cell-width using
//     runewidth.Truncate; lines that exceed the wrap budget are
//     soft-wrapped at cell-width.
//   - wrapPlanLine    — soft-wrap one plan line at cell-width, used by
//     the slash-panel / inline-picker surfaces.
//   - truncatePlanLine — cell-width truncation, used by the cycle-244
//     panel preview path.
//
// The byte-vs-cell distinction is the AGY 1.4.0 fix in one sentence:
// ASCII is 1 byte = 1 cell (the bug never showed), CJK ideographs are
// 3 bytes = 2 cells (the bug cut plan lines in half), and emoji are
// 4 bytes = 2 cells (the bug truncated emoji sequences too).
// runewidth.StringWidth returns the byte-faithful measure for all
// three; runewidth.Truncate cuts at a rune (grapheme) boundary so a
// multi-byte codepoint is never split mid-byte — that is the "ellipsis
// at grapheme boundary" recovery rule the cycle-244 changelog
// description hints at.

package planrender

import (
	"strings"

	rwidth "github.com/mattn/go-runewidth"
)

// planNumberWidth is the cell budget consumed by the "N. " / "• "
// prefix the render prepends to each plan item. The ASCII fast path
// is "N. " = 3 cells; numbered plans use up to two digits in
// practice. cycle 245 sticks with 3 to keep the renderer simple and
// let longer prefixes opt in via PlanItem.Prefix.
const planNumberWidth = 3

// planIndent is the cell budget for the indent on continuation lines
// of a plan item (after the first wrapped line). Two spaces matches
// the v1.4.0 plan transcript surface (indentModelReply uses two
// spaces too; the AGY 1.4.0 plan panel nests under the indent so the
// continuation lines share the same column).
const planIndent = "  "

// ellipsisTail is the cell-1 trailing marker RenderPlanItems appends
// when it has to truncate a plan line to fit the cell budget. ASCII
// so its visible width is exactly 1 cell, which keeps the cell math
// deterministic across all runewidth conditions.
const ellipsisTail = "…"

// PlanItem is one numbered entry in the agent's plan reply. Prefix is
// the cell-budgeted prefix the render prepends to the first line
// (typically "1. " / "2. " / "• " — the render does not interpret
// the prefix, only the first rune). Lines is the body of the item,
// one logical line per slice element.
//
// Items are rendered top-to-bottom in slice order; the resulting
// slice has len(items) entries plus any extra rows that overflowed
// the cell budget (so a 3-line item with each line wrapped at width
// 20 produces 3 entries of width-20 rows).
type PlanItem struct {
	Prefix string
	Lines  []string
}

// planRow is one rendered terminal row. Indent is "  " (continuation)
// or "" (the item's first line — the prefix is prepended by the
// caller). Content is the visible text.
type planRow struct {
	Indent  string
	Content string
}

// RenderPlanItems renders items into terminal rows. The result is a
// flat slice of "prefix+content" strings in render order; the caller
// is responsible for any styling (the plan render does not colour
// rows — colouring is the panel's job).
//
// width is the total cell budget per visible row — the row, including
// the prefix and indent, must fit within width cells. The content
// budget is computed as width minus the cell-width of the prefix
// (first row) or the indent (continuation rows). Lines that exceed
// the content budget are wrapped (long line) and then truncated if
// even a single cell-row is over budget.
//
// width <= 0 disables truncation; lines longer than the terminal
// width pass through untouched (matches the pre-cycle-245 "no
// budget" behaviour, and lets the call site opt out when the
// downstream renderer has its own wrap).
//
// Cycle 245 budget rules (AGY 1.4.0):
//
//  1. The first line of each item: prefix + cell-truncated/wrapped
//     content. The total row width = width cells. Truncation
//     appends ellipsisTail when content exceeds the cell budget.
//     Wrapping inserts newlines at the budget edge.
//  2. Continuation lines: contIndent + cell-truncated/wrapped
//     content. Same rules; the contIndent hangs wrapped lines under
//     the body's start.
//  3. Cell math uses runewidth.StringWidth — a CJK ideograph counts
//     as 2 cells, ASCII as 1, emoji as 2. The cycle's regression
//     tests pin this behaviour.
//
// Empty items (Prefix == "" && len(Lines) == 0) emit zero rows.
// Empty lines inside an item (Lines[i] == "") emit a row with the
// prefix / indent and no content, so the panel layout preserves the
// blank-line rhythm the model reply carried.
func RenderPlanItems(items []PlanItem, width int) []string {
	if len(items) == 0 {
		return nil
	}
	var out []string
	for _, it := range items {
		rows := renderPlanItem(it, width)
		for _, r := range rows {
			out = append(out, r.Indent+r.Content)
		}
	}
	return out
}

// renderPlanItem renders one PlanItem into terminal rows, including
// the prefix on the first row and contIndent on continuation rows.
// Returns nil for an item with no prefix and no lines so the caller
// can drop empty entries from the output.
//
// Cycle 245 row invariant: every row is exactly `width` cells wide
// (including prefix / indent + content + trailing padding). Empty
// rows are padded with spaces so the panel sees a uniform-width
// layout. Content that overflows its row budget is truncated with
// the ellipsis tail at a grapheme-cluster boundary.
func renderPlanItem(item PlanItem, width int) []planRow {
	if item.Prefix == "" && len(item.Lines) == 0 {
		return nil
	}
	prefixWidth := rwidth.StringWidth(item.Prefix)
	contIndent := strings.Repeat(" ", prefixWidth) + planIndent
	contIndentWidth := rwidth.StringWidth(contIndent)
	if len(item.Lines) == 0 {
		// Item has a prefix but no body — emit the prefix as a row so
		// the panel layout sees a marker even when the body is empty.
		// Pad with spaces to fill the budget.
		return []planRow{{Indent: "", Content: padCells(item.Prefix, width)}}
	}
	// contentBudget is the cells available for the body of each row.
	// width <= 0 disables the budget entirely (passthrough mode).
	contentBudget := width - prefixWidth
	contBudget := width - contIndentWidth
	if width <= 0 {
		contentBudget = width
		contBudget = width
	}
	var rows []planRow
	for i, line := range item.Lines {
		indent := item.Prefix
		rowBudget := contentBudget
		if i > 0 {
			indent = contIndent
			rowBudget = contBudget
		}
		// Empty line — preserve the rhythm. The cycle-245 budget
		// rules say "empty line is its own row" so the panel sees a
		// blank between paragraphs. Pad to width so every row is the
		// same total cell count.
		if line == "" {
			rows = append(rows, planRow{Indent: indent, Content: padCells("", rowBudget)})
			continue
		}
		rows = append(rows, renderWrappedPlanLine(line, indent, contIndent, rowBudget, contBudget)...)
	}
	return rows
}

func renderWrappedPlanLine(line, indent, contIndent string, rowBudget, contBudget int) []planRow {
	var rows []planRow
	wrapped := wrapPlanLine(line, rowBudget)
	for j, w := range wrapped {
		rowIndent := indent
		rowRowBudget := rowBudget
		if j > 0 {
			rowIndent = contIndent
			rowRowBudget = contBudget
		}
		content := truncatePlanLine(w, rowRowBudget)
		content = padCells(content, rowRowBudget)
		rows = append(rows, planRow{Indent: rowIndent, Content: content})
	}
	return rows
}

// padCells returns s padded with trailing spaces to at least
// width cells. width <= 0 returns s unchanged. The padding is cell-
// width aware (CJK ideographs in s consume 2 cells each), so the
// returned string's cellWidth is at least width.
func padCells(s string, width int) string {
	if width <= 0 {
		return s
	}
	w := rwidth.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// wrapPlanLine soft-wraps line into rows of at most width terminal
// cells each. The width budget is the *content* budget — the caller
// prepended the prefix / indent on the caller side, so width here is
// the same budget the render handed in.
//
// width <= 0 disables wrapping; the line passes through unchanged.
//
// Cycle 245 budget rule: width is in terminal cells, not bytes. A
// 60-byte CJK line ("修复修复..." 20 chars) is 40 cells, so it fits
// in width=40 without wrap; in width=30 it wraps to two rows at the
// cell boundary, not at byte / rune boundary.
//
// Soft-wrap algorithm: walk runes, accumulate cell count via
// runewidth.RuneWidth, emit a row each time the next rune would
// exceed the budget. Combining marks (width 0) are absorbed into the
// preceding row's last rune so they don't desynchronise the wrap
// boundary.
func wrapPlanLine(line string, width int) []string {
	if width <= 0 || line == "" {
		return []string{line}
	}
	if rwidth.StringWidth(line) <= width {
		return []string{line}
	}
	var rows []string
	var cur strings.Builder
	used := 0
	flush := func() {
		rows = append(rows, cur.String())
		cur.Reset()
		used = 0
	}
	for _, r := range line {
		rw := rwidth.RuneWidth(r)
		if rw < 0 {
			// Negative widths (control chars) — treat as zero so
			// they don't accidentally count toward the budget.
			rw = 0
		}
		if rw == 0 {
			// Combining mark / ZWJ-joiner. Always attaches to the
			// preceding rune so it doesn't desync the wrap boundary.
			cur.WriteRune(r)
			continue
		}
		if used+rw > width {
			flush()
		}
		cur.WriteRune(r)
		used += rw
	}
	if cur.Len() > 0 || len(rows) == 0 {
		flush()
	}
	return rows
}

// truncatePlanLine shortens line to at most width terminal cells,
// appending ellipsisTail (a 1-cell ASCII ellipsis) when truncation
// occurs. Width <= 0 returns line unchanged; a line that already
// fits returns line unchanged.
//
// Cycle 245 budget rule: cell-width, not byte-width. The pre-cycle
// behaviour was either `line[:width]` (byte-slice — splits CJK
// mid-codepoint and produces U+FFFD replacement chars) or
// `clampRunes(line, width)` (rune-count — cuts at half the visible
// cells for CJK). The cycle's contract:
//
//	width = 10
//	input = "修复修复..."  (12 chars = 36 bytes = 24 cells)
//	output = "修复修复…"  (4 chars + ellipsis = 9 cells;
	                            // cannot reach 10 cells because 5
	                            // CJK chars + ellipsis would be 11
	                            // cells, exceeding the budget)
//
// runewidth.Truncate handles the cell math and the grapheme-cluster
// boundary (a multi-byte codepoint is never split mid-codepoint, the
// AGY 1.4.0 fix the changelog description documents).
func truncatePlanLine(line string, width int) string {
	if width <= 0 || line == "" {
		return line
	}
	if rwidth.StringWidth(line) <= width {
		return line
	}
	// runewidth.Truncate handles the cell math + grapheme boundary;
	// we just supply the tail. width<=0 returns "" from the
	// library (matches clampRunes' empty contract).
	return rwidth.Truncate(line, width, ellipsisTail)
}
