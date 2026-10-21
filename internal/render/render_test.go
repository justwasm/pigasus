// render_test.go — unit tests for internal/render.
//
// Cycle terminal-rendering-bbt-v209: the four scenarios below are the
// minimum the cycle promises. Each test exercises one of the two bbt-
// v2 quirks the package documents (missing column width, OSC 8 link
// underflow) and one of the two helper categories (box / divider /
// table / hyperlink). Run targeted via:
//
//	go test -vet=off -count=1 -run 'TestRender|TestBox|TestTable|TestHyperlink' ./internal/render/
//
// (the targeted selector picks these four up alongside any future
// render.* tests, and the existing cmd/wagyu render tests).
//
// Cycle charmland-component-adoption (cycle 01): the Table tests were
// regenerated when the wrapper was rewritten on top of
// charm.land/lipgloss/v2/table. The output separator changed from the
// hand-rolled " │ " (3 cells) to lipgloss's "│" (1 cell) — that is
// the AGY reference's separator format. Total cell widths therefore
// dropped: where the old tests expected 24 cells for [6,6,6], the new
// tests expect 20 (= 6+1+6+1+6). Behavioural changes to assert:
//
//   - WithHeaders(...): header row renders before the data rows and
//     uses the WithStyles() header style (or Bold by default).
//   - Empty table (Render on a Table with no headers, no rows): ""
//     — matches the cycle-225 /model catalogue empty-state.
//   - Render clamps long cells to colW[c] and pads short cells with
//     spaces; widths set via SetColumnWidths are preserved exactly
//     (e.g. [6,6,6] with width=20 stays [6,6,6] and the table
//     underfills rather than growing the trailing column).
//   - Cell.Width override is honoured by the renderer even though
//     lipgloss's StyleFunc only sees (row, col) — the cell text is
//     pre-padded / pre-clamped before being passed to the table.
package render

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// visibleWidth returns the cell width of s as it would render in the
// terminal — ANSI escapes are ignored. Using this instead of
// utf8.RuneCountInString is required now that Table cells can carry
// ANSI escapes from lipgloss styles.
func visibleWidth(s string) int {
	return lipgloss.Width(s)
}

// TestBox_Borders verifies a styled box renders with the recovered
// v1.3.6 wizard box border glyphs (┌ ─ ┐ │ └ ┘) and that the body
// lines are padded with spaces so the right edge is flush with the
// right border. This is the "styled box with borders renders"
// contract.
func TestBox_Borders(t *testing.T) {
	got := Box(20, []string{"hi", "longer body line"}, "")
	// Expect 4 lines: top, "hi" row, "longer body line" row, bottom.
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("Box returned %d lines, want 4: %q", len(lines), got)
	}
	// width=20 → inner=18; top edge is "┌" + 18×"─" + "┐".
	topWant := "┌" + strings.Repeat("─", 18) + "┐"
	if lines[0] != topWant {
		t.Errorf("top edge wrong: %q want %q", lines[0], topWant)
	}
	botWant := "└" + strings.Repeat("─", 18) + "┘"
	if lines[3] != botWant {
		t.Errorf("bottom edge wrong: %q want %q", lines[3], botWant)
	}
	// Body row "hi" should be │hi + 16 spaces + │ (inner=18).
	bodyWant := "│hi" + strings.Repeat(" ", 16) + "│"
	if lines[1] != bodyWant {
		t.Errorf("row 'hi' wrong: %q want %q", lines[1], bodyWant)
	}
	bodyWant2 := "│longer body line" + strings.Repeat(" ", 2) + "│"
	if lines[2] != bodyWant2 {
		t.Errorf("row 'longer body line' wrong: %q want %q", lines[2], bodyWant2)
	}
	// Sanity: each line has the same rune width (= caller width).
	for i, l := range lines {
		if w := cellWidth(l); w != 20 {
			t.Errorf("line %d width=%d, want 20: %q", i, w, l)
		}
	}
}

// TestBox_NegativeWidthFallsBack confirms Box accepts a non-positive
// width (the bbt-v2 "missing column width" quirk: WindowSizeMsg.Width
// can be 0 before the first resize probe) without rendering an empty
// grid. The default width is DefaultBoxWidth; the test asserts each
// rendered line matches that budget exactly.
func TestBox_NegativeWidthFallsBack(t *testing.T) {
	// width=0 is the literal bbt-v2 quirk case. width=-3 is a
	// hostile caller — both should produce the default.
	for _, w := range []int{0, -1, -3} {
		got := Box(w, []string{"x"}, "")
		lines := strings.Split(got, "\n")
		if len(lines) != 3 {
			t.Fatalf("width=%d: Box returned %d lines, want 3: %q", w, len(lines), got)
		}
		for i, l := range lines {
			if rw := cellWidth(l); rw != DefaultBoxWidth {
				t.Errorf("width=%d line %d cell width=%d, want %d: %q",
					w, i, rw, DefaultBoxWidth, l)
			}
		}
	}
}

// TestDivider_FallbackWidth verifies Divider accepts width=0 (the
// bbt-v2 "missing column width" quirk) and emits DefaultBoxWidth
// cells of ─ in a single line, with no panic and no empty output.
// This is the "missing column width falls back gracefully" contract
// for dividers.
func TestDivider_FallbackWidth(t *testing.T) {
	got := Divider(0, "")
	want := strings.Repeat("─", DefaultBoxWidth)
	if got != want {
		t.Fatalf("Divider(0) = %q, want %q", got, want)
	}
	// Negative width — same fallback.
	got = Divider(-7, "")
	if got != want {
		t.Fatalf("Divider(-7) = %q, want %q", got, want)
	}
	// Positive width — pass-through.
	got = Divider(5, "")
	if got != strings.Repeat("─", 5) {
		t.Fatalf("Divider(5) = %q, want 5 ─ cells", got)
	}
}

// TestDividerWithLabel_CentredLabel verifies the "─── Preview ───"
// pattern renders with the label centred, the right-side padding
// shrunk to absorb the label width, and the total width matching the
// caller's budget.
func TestDividerWithLabel_CentredLabel(t *testing.T) {
	got := DividerWithLabel(20, "Preview", "")
	if w := cellWidth(got); w != 20 {
		t.Fatalf("DivderWithLabel(20) width=%d, want 20: %q", w, got)
	}
	if !strings.Contains(got, "Preview") {
		t.Fatalf("label missing: %q", got)
	}
	// The pattern is "─── Preview ───────" — three leading ─, a
	// space, the label, a space, then the rest of the ─. Width 20
	// minus 8 fixed chars (= 3+space+7+space+trailing) leaves 12
	// trailing ─. Total length: 3+1+7+1+12 = 24 bytes in one rune? no,
	// 3+1+7+1+12 = 24 runes? 3+1+7+1+12 = 24 cells, but the helper
	// accounts for "label width" separately so the math is correct.
	// Sanity-check: leading "─── " present.
	if !strings.HasPrefix(got, "─── ") {
		t.Fatalf("missing leading '─── ': %q", got)
	}
}

// TestHyperlink_Underflow verifies the OSC 8 hyperlink helper clamps
// the link text to maxWidth before emitting the closing ST. The
// bubbletea v2 quirk: OSC 8 frames whose body overflows the column
// width lose the trailing ST and leak the underline attribute into
// the next line. The helper's fix is "clamp the body to maxWidth
// first, then emit the closing ST".
func TestHyperlink_Underflow(t *testing.T) {
	const url = "https://example.com/very-long/url/that/overflows"
	// Long text that overflows the cell width; without the clamp
	// the OSC 8 frame's closing ST would land outside the cell.
	longText := strings.Repeat("a", 100)
	// Cycle osc-hyperlinks: the helper takes a caps argument.
	// OSC8=true here — the underflow fix is part of the OSC 8
	// framing path; the OSC 8 disabled case is plainer ("label
	// (url)") and is tested separately in
	// TestHyperlink_PlainTextFallback in hyperlink_caps_test.go.
	caps := HyperlinkCaps{OSC8: true}
	got := Hyperlink(url, longText, 5, caps)
	// Expected shape: \x1b[8;;<url>\x1b\\<body-clamped-to-5>\x1b[8;;\x1b\\
	openSeq := "\x1b]8;;" + url + "\x1b\\"
	closeSeq := "\x1b]8;;\x1b\\"
	if !strings.HasPrefix(got, openSeq) {
		t.Fatalf("missing OSC 8 opener: %q", got)
	}
	if !strings.HasSuffix(got, closeSeq) {
		t.Fatalf("missing OSC 8 closer: %q", got)
	}
	// Body is exactly 5 'a's between opener and closer.
	body := strings.TrimPrefix(got, openSeq)
	body = strings.TrimSuffix(body, closeSeq)
	if len(body) != 5 || body != "aaaaa" {
		t.Fatalf("body not clamped to 5 cells: %q", body)
	}
	// Empty URL — the helper returns the text unchanged (the
	// cycle-218 markdown renderer relies on this escape hatch).
	got = Hyperlink("", "plain text", 5, caps)
	if got != "plain text" {
		t.Fatalf("Hyperlink empty url = %q, want %q", got, "plain text")
	}
	// maxWidth=0 disables the clamp — long text is preserved
	// verbatim. Callers that have already validated the width use
	// this path.
	got = Hyperlink(url, longText, 0, caps)
	body = strings.TrimPrefix(got, openSeq)
	body = strings.TrimSuffix(body, closeSeq)
	if body != longText {
		t.Fatalf("Hyperlink(..., 0) body = len %d, want len %d", len(body), len(longText))
	}
}

// TestTable_ColumnWidths verifies the Table helper honours per-column
// widths, falls back to the column's natural width when a cell's
// Width is 0 (the bbt-v2 "missing column width" workaround), and
// pads/clamps cells so the grid stays aligned.
//
// Cycle 01 charmland-component-adoption: the new wrapper uses
// charm.land/lipgloss/v2/table with BorderColumn(true) and all other
// borders off. The separator is therefore a single "│" cell instead
// of the old hand-rolled " │ " (3 cells). Total line width is sum
// of cell widths + (ncols-1) separators.
func TestTable_ColumnWidths(t *testing.T) {
	tbl := NewTable(20, 3)
	// With width=20 and 3 columns, the helper distributes
	// MinColumnWidth=3 to the leading ncols-1 (=2) and the final
	// column absorbs the remainder: 3, 3, 14.
	w := tbl.Widths()
	if len(w) != 3 {
		t.Fatalf("Widths len=%d, want 3", len(w))
	}
	if w[0] != 3 || w[1] != 3 || w[2] != 14 {
		t.Fatalf("default column widths wrong: %v", w)
	}
	// Custom widths via SetColumnWidths. SetColumnWidths preserves
	// explicit per-column widths — when their sum is less than
	// `width` the table underfills rather than growing the trailing
	// column (matches the original v1.2.10 semantics).
	tbl.SetColumnWidths([]int{6, 6, 6})
	w = tbl.Widths()
	for i, want := range []int{6, 6, 6} {
		if w[i] != want {
			t.Errorf("col %d width = %d, want %d", i, w[i], want)
		}
	}
	// Render two rows; row[1] has a Cell with Width=0 (the missing
	// column-width workaround) — it should fall back to the
	// column's resolved width and still align.
	rows := []Cell{
		{Text: "A", Width: 0}, // width 0 → use col width 6
		{Text: "B", Width: 0},
		{Text: "longer C cell", Width: 6}, // clamped to 6 cells
	}
	row2 := []Cell{
		{Text: "x", Width: 0},
		{Text: "y", Width: 0},
		{Text: "z", Width: 0},
	}
	got := tbl.Render([]Row{{Cells: rows}, {Cells: row2}})
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("Render produced %d lines, want 2: %q", len(lines), got)
	}
	// Each line should have the same total width:
	// col1 (6) + "│" (1) + col2 (6) + "│" (1) + col3 (6) = 20.
	for i, l := range lines {
		if rw := visibleWidth(l); rw != 20 {
			t.Errorf("line %d rune width=%d, want 20: %q", i, rw, l)
		}
		// Border column separator on every interior column.
		if strings.Count(l, "│") != 2 {
			t.Errorf("line %d has %d │ separators, want 2: %q",
				i, strings.Count(l, "│"), l)
		}
	}
	// First row, third column: text clamped to 6 cells, padded to 6.
	if !strings.HasSuffix(lines[0], "longer") {
		t.Errorf("row 0 col 2 not clamped: %q", lines[0])
	}
	// Second row's short cells should pad with spaces.
	if !strings.Contains(lines[1], "y ") {
		t.Errorf("row 1 col 2 missing pad: %q", lines[1])
	}
}

// TestTable_MissingColumnWidthFallback ensures a row that omits the
// trailing cells (shorter than ncols) still renders without panic,
// and that a row whose Cell.Width is 0 uses the column's resolved
// width. This is the explicit "missing column width falls back
// gracefully" scenario the cycle promises.
//
// Cycle 01: separator is now "│" (1 cell). For widths [4,8] the
// total visible width is 4 + 1 + 8 = 13 cells.
func TestTable_MissingColumnWidthFallback(t *testing.T) {
	tbl := NewTable(12, 2)
	// Set widths to 4 and 8.
	tbl.SetColumnWidths([]int{4, 8})
	// Row shorter than ncols — the missing cell pads with the
	// column's width (4 or 8 spaces). Width=0 in the first cell
	// triggers the fallback: render uses col width = 4.
	got := tbl.Render([]Row{
		{Cells: []Cell{
			{Text: "ab", Width: 0}, // width 0 → use col width 4
		}},
	})
	// Expected: "ab  " (padded to 4) + "│" (1) + 8 spaces = 4+1+8 = 13.
	if w := visibleWidth(got); w != 13 {
		t.Fatalf("Render visible width = %d, want 13: %q", w, got)
	}
	// The padded "ab" should be present.
	if !strings.HasPrefix(got, "ab  ") {
		t.Fatalf("short cell not padded: %q", got)
	}
	// Exactly one column separator.
	if c := strings.Count(got, "│"); c != 1 {
		t.Fatalf("Render has %d │ separators, want 1: %q", c, got)
	}
}

// TestTable_WithHeaders exercises the new WithHeaders() chainable
// helper that AGY's /keybindings panel uses (the recovered table
// layout puts "Action" and "Key" as header columns). The header row
// is rendered with the WithStyles header style (default: Bold),
// and the per-column widths are still honoured.
//
// Cycle 01: total visible line width is sum(colW) + (ncols-1) —
// here sum=14+6=20 + 1 separator = 21 cells.
func TestTable_WithHeaders(t *testing.T) {
	tbl := NewTable(20, 2).
		WithHeaders("Action", "Key").
		WithStyles(
			lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(238)),
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.ANSIColor(244)),
		)
	// Set explicit widths so the output is deterministic.
	tbl.SetColumnWidths([]int{14, 6})
	got := tbl.Render([]Row{
		{Cells: []Cell{{Text: "Submit"}, {Text: "Enter"}}},
		{Cells: []Cell{{Text: "Exit"}, {Text: "Ctrl-C"}}},
	})
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("WithHeaders rendered %d lines, want 3 (header + 2 data): %q",
			len(lines), got)
	}
	// Header row carries the lipgloss bold+fg escapes; data rows
	// carry the dim-gray style.
	for i, l := range lines {
		if rw := visibleWidth(l); rw != 21 {
			t.Errorf("line %d visible width=%d, want 21: %q", i, rw, l)
		}
		if !strings.Contains(l, "│") {
			t.Errorf("line %d missing │ separator: %q", i, l)
		}
	}
	// Header line contains both header labels.
	if !strings.Contains(lines[0], "Action") || !strings.Contains(lines[0], "Key") {
		t.Errorf("header line missing labels: %q", lines[0])
	}
}

// TestTable_FiveColumnMixedWidths exercises the model catalogue
// scenario from the task brief: 5 columns of different widths, no
// headers, no styles. Asserts each line has the same width and that
// long cells are clamped.
func TestTable_FiveColumnMixedWidths(t *testing.T) {
	tbl := NewTable(60, 5)
	// Mixed widths: 4, 8, 12, 6, 30 = 60 cells exactly.
	tbl.SetColumnWidths([]int{4, 8, 12, 6, 30})
	rows := []Row{
		{Cells: []Cell{
			{Text: "1"}, {Text: "Opus"}, {Text: "5.5"},
			{Text: "high"}, {Text: "very-long-description-text"},
		}},
		{Cells: []Cell{
			{Text: "2"}, {Text: "Sonnet"}, {Text: "5.5"},
			{Text: "med"}, {Text: "shorter"},
		}},
	}
	got := tbl.Render(rows)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("Render produced %d lines, want 2: %q", len(lines), got)
	}
	// Total width = sum(colW) + (ncols-1) = 60 + 4 = 64.
	want := 64
	for i, l := range lines {
		if rw := visibleWidth(l); rw != want {
			t.Errorf("line %d rune width=%d, want %d: %q", i, rw, want, l)
		}
		if c := strings.Count(l, "│"); c != 4 {
			t.Errorf("line %d has %d │ separators, want 4: %q", i, c, l)
		}
	}
}

// TestTable_Empty covers the empty-state cases the task brief calls
// out explicitly:
//
//   - Render on a Table with no headers and no rows returns "".
//   - NewTable(0, 1) (the bbt-v2 width=0 quirk) still produces a
//     1-column table of DefaultBoxWidth cells.
//   - NewTable(60, 0) (pathological ncols) clamps ncols to 1.
func TestTable_Empty(t *testing.T) {
	// No headers, no rows → empty output.
	tbl := NewTable(40, 3)
	if got := tbl.Render(nil); got != "" {
		t.Errorf("Render(nil) = %q, want empty", got)
	}
	if got := tbl.Render([]Row{}); got != "" {
		t.Errorf("Render([]) = %q, want empty", got)
	}
	// Headers only, no rows → header is still emitted.
	tbl.WithHeaders("A", "B", "C")
	got := tbl.Render(nil)
	if got == "" {
		t.Errorf("Render with headers but no rows = empty, want header line")
	}
	// ncols=0 → collapses to 1 column (cycle 01 guard).
	tbl = NewTable(60, 0)
	w := tbl.Widths()
	if len(w) != 1 || w[0] != 60 {
		t.Errorf("NewTable(60, 0) widths = %v, want [60]", w)
	}
	// width=0 (the bbt-v2 quirk) falls back to DefaultBoxWidth.
	tbl = NewTable(0, 4)
	w = tbl.Widths()
	if len(w) != 4 || w[0] != 3 || w[1] != 3 || w[2] != 3 || w[3] != DefaultBoxWidth-9 {
		t.Errorf("NewTable(0, 4) widths = %v, want [3,3,3,%d]", w, DefaultBoxWidth-9)
	}
}

// TestTable_PadOrClamp covers the cell-padding helper directly so a
// future refactor of Render's pre-clamp step still honours the
// cell-width override semantics. Cell.Width overrides the column
// width (the explicit "use this many cells" contract).
func TestTable_PadOrClamp(t *testing.T) {
	if got := padOrClamp("ab", 4); got != "ab  " {
		t.Errorf("padOrClamp short = %q, want %q", got, "ab  ")
	}
	if got := padOrClamp("hello world", 5); got != "hello" {
		t.Errorf("padOrClamp long = %q, want %q", got, "hello")
	}
	if got := padOrClamp("exact", 5); got != "exact" {
		t.Errorf("padOrClamp exact = %q, want %q", got, "exact")
	}
	if got := padOrClamp("ignored", 0); got != "" {
		t.Errorf("padOrClamp 0-width = %q, want empty", got)
	}
}
