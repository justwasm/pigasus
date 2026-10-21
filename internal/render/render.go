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
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/mattn/go-runewidth"
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

// Box renders a styled box around content. The box's outer width is
// the caller's `width`; if width is 0 or negative, DefaultBoxWidth is
// used. Each line of `content` is centred inside the box and padded
// with spaces; lines longer than (width - 2) are clamped to width-2
// cells so they never overflow the border (the "missing column width"
// workaround — bubbletea v2 sometimes reports 0 mid-render).
//
// The border glyphs are the recovered v1.3.6 wizard box characters
// (┌ ─ ┐ │ └ ┘). Style is an optional ANSI prefix that opens the
// border colour and is closed before the trailing \n; an empty style
// emits no colour escapes.
func Box(width int, content []string, style string) string {
	if width <= 0 {
		width = DefaultBoxWidth
	}
	inner := width - 2 // account for "│" on each side
	if inner < 1 {
		// Even at DefaultBoxWidth this branch is unreachable
		// (70 - 2 = 68), but a hostile caller could ask for
		// width=3 and still expect *something* — return a
		// minimum-width box so the call site never renders
		// nothing.
		inner = 1
	}
	open, close := style, ""
	if style != "" {
		open = style
		close = "\x1b[0m"
	}
	var b strings.Builder
	b.Grow(len(content)*(width+1) + 16)
	// Top edge: ┌───┐
	b.WriteString(open)
	b.WriteString("┌")
	for i := 0; i < inner; i++ {
		b.WriteString("─")
	}
	b.WriteString("┐")
	b.WriteString(close)
	b.WriteByte('\n')
	// Body: each line padded with spaces inside │...│
	for _, line := range content {
		w := cellWidth(line)
		pad := inner - w
		b.WriteString(open)
		b.WriteString("│")
		if pad > 0 {
			b.WriteString(line)
			b.WriteString(strings.Repeat(" ", pad))
		} else if pad == 0 {
			b.WriteString(line)
		} else {
			// line longer than inner — clamp to inner cells so
			// the box border stays flush. Cycle 245: cell-width
			// clamp, not rune-count — a CJK ideograph is 2 cells,
			// and the rune-count helper over-trimmed (cut at half
			// the visible cells for CJK content).
			b.WriteString(truncateToCells(line, inner))
		}
		b.WriteString("│")
		b.WriteString(close)
		b.WriteByte('\n')
	}
	// Bottom edge: └───┘
	b.WriteString(open)
	b.WriteString("└")
	for i := 0; i < inner; i++ {
		b.WriteString("─")
	}
	b.WriteString("┘")
	b.WriteString(close)
	return b.String()
}

// Divider renders a horizontal rule of width cells using the recovered
// separator glyph (─). width <= 0 falls back to DefaultBoxWidth. The
// style parameter is an optional ANSI prefix; empty means no colour.
// Divider pads short content with trailing spaces so the rule aligns
// with the box above / below it in the panel layout.
func Divider(width int, style string) string {
	if width <= 0 {
		width = DefaultBoxWidth
	}
	open, close := style, ""
	if style != "" {
		open = style
		close = "\x1b[0m"
	}
	var b strings.Builder
	b.Grow(3*width + len(open) + len(close) + 4)
	b.WriteString(open)
	for i := 0; i < width; i++ {
		b.WriteString("─")
	}
	b.WriteString(close)
	return b.String()
}

// DividerWithLabel renders a horizontal rule with a centred label,
// matching the recovered "─── Preview ───" pattern from the v1.3.6
// onboarding wizard. The label is wrapped in two short rule runs (3
// cells each, plus a space). If the label is wider than (width - 8)
// it is clamped so the rule still draws; if width is non-positive
// DefaultBoxWidth is used.
func DividerWithLabel(width int, label, style string) string {
	if width <= 0 {
		width = DefaultBoxWidth
	}
	open, close := style, ""
	if style != "" {
		open = style
		close = "\x1b[0m"
	}
	// Total budget is consumed by: "─── " (4) + label + " " (1) +
	// rightRule. If the leftover for the right rule would be
	// non-positive, fall back to a plain rule — labelling is
	// decorative, the rule is what the panel relies on.
	rw := cellWidth(label)
	right := width - 5 - rw
	if right < 1 || rw > width-5 {
		return Divider(width, style)
	}
	var b strings.Builder
	b.Grow(3*width + len(label) + 16)
	b.WriteString(open)
	b.WriteString("─── ")
	b.WriteString(label)
	b.WriteString(" ")
	for i := 0; i < right; i++ {
		b.WriteString("─")
	}
	b.WriteString(close)
	return b.String()
}

// Cell is one rendered column of a Table. Text is the cell payload
// (plain text — no ANSI escapes). Width is the cell's budget in cells
// (rune count, not byte count); the renderer pads short cells with
// trailing spaces and clamps long cells to Width. Width <= 0 falls
// back to the column's resolved width.
type Cell struct {
	Text  string
	Width int
}

// Row is a horizontal slice of Cells. The number of cells must equal
// Table's column count; shorter rows are padded with blank cells of
// the column's width, longer rows have their trailing cells dropped.
type Row struct {
	Cells []Cell
}

// Table is a column-aligned table whose data is rendered by the
// charm.land/lipgloss/v2/table package (cycle 01 of the
// charmland-component-adoption). NewTable distributes a budget of
// `width` cells across `ncols` columns with at least MinColumnWidth
// cells per column. If width is non-positive DefaultBoxWidth is used.
//
// Render walks the rows and emits one line per row, padding short
// cells with spaces and clamping long cells to the column width so
// the grid stays aligned. Cells whose Width field is 0 fall back to
// the column width — this is the "missing column width falls back
// gracefully" workaround: a caller that hands in cells without
// explicit widths (e.g. a markdown table whose source had no
// separator row) gets the column's natural width, not a panic or a
// zero-width cell.
//
// The output is built around table.Table.String() with all borders
// off except BorderColumn (which produces a single-cell "│" separator
// between columns — matching the AGY reference's separator). The
// separator changed from the old hand-rolled " │ " (3 cells) to "│"
// (1 cell); that is intentional — AGY's table.String() output is
// the same single-cell separator. Render returns the table as a
// string with no trailing newline.
type Table struct {
	width   int
	ncols   int
	colW    []int
	headers []string
	// styles applied per cell via StyleFunc. Defaults: lipgloss's
	// bare style — no padding, no margin, no colour. Callers can
	// opt in via WithStyles.
	cellStyle   lipgloss.Style
	headerStyle lipgloss.Style
}

// NewTable returns a Table with `ncols` columns sharing `width` cells.
// If width is non-positive DefaultBoxWidth is used. The first
// `ncols-1` columns each get MinColumnWidth cells; the final column
// absorbs any remainder so the table exactly fills `width`. Callers
// that want content-driven widths should call SetColumnWidths after
// construction.
func NewTable(width, ncols int) *Table {
	if width <= 0 {
		width = DefaultBoxWidth
	}
	if ncols < 1 {
		ncols = 1
	}
	t := &Table{
		width:       width,
		ncols:       ncols,
		colW:        computeColumnWidths(width, ncols),
		cellStyle:   lipgloss.NewStyle(),
		headerStyle: lipgloss.NewStyle().Bold(true),
	}
	return t
}

// WithHeaders installs a header row. The header cells follow the same
// column widths as data rows. Empty strings are valid (" " is NOT —
// the lipgloss renderer treats whitespace as content but escapes it).
// Pass empty to clear.
func (t *Table) WithHeaders(headers ...string) *Table {
	t.headers = headers
	return t
}

// WithStyles sets the per-cell and per-header styles. Both are
// applied via lipgloss's StyleFunc; Width(N) overrides per cell
// remain honoured by the renderer (the cell text is pre-clamped to
// its Width before being passed to the table, so the style Width is
// the *visible* cell width after clamping).
//
// A common case:
//
//	tbl.WithStyles(
//	    lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(238)),
//	    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.ANSIColor(244)),
//	)
func (t *Table) WithStyles(cell, header lipgloss.Style) *Table {
	t.cellStyle = cell
	t.headerStyle = header
	return t
}

// SetColumnWidths overwrites the per-column widths. The slice must
// have length ncols; out-of-range widths are clamped to [MinColumnWidth,
// width]. This is the helper callers use when they have content-
// driven widths from a markdown separator row or a recovered spec.
func (t *Table) SetColumnWidths(widths []int) {
	if len(widths) != t.ncols {
		return
	}
	t.colW = clampWidths(widths, t.width, t.ncols)
}

// Widths returns a copy of the column widths — useful for tests and
// for callers that want to echo the resolved grid into a panel header.
func (t *Table) Widths() []int {
	out := make([]int, t.ncols)
	copy(out, t.colW)
	return out
}

// TotalWidth returns the cells the table will draw (sum of colW). It
// is the same value NewTable received (after the non-positive
// fallback); SetColumnWidths is not allowed to grow the table past
// its starting budget.
func (t *Table) TotalWidth() int {
	n := 0
	for _, w := range t.colW {
		n += w
	}
	return n
}

// Render draws the rows. Each row's cells must number t.ncols;
// shorter rows are padded with blank cells, longer rows have their
// trailing cells dropped. Cells whose Width is 0 use the column's
// resolved width; this is the explicit "missing column width falls
// back gracefully" path. Lines are separated with \n and the result
// has no trailing newline.
//
// Render delegates to charm.land/lipgloss/v2/table.Table.String().
// The style for each cell is built via StyleFunc, and column
// borders are enabled (BorderColumn(true)) so the output is a
// grid separated by single-cell "│" characters. Other borders
// (Top/Bottom/Left/Right/Row/Header) are off — callers that want
// a fully-bordered table should wrap render.Table around their
// own styling or use lipgloss/table directly.
//
// When no headers and no rows are passed, Render returns an empty
// string — this matches the cycle-225 panel state where an empty
// /model catalogue is "no output" not "an empty grid".
func (t *Table) Render(rows []Row) string {
	if t.ncols < 1 {
		return ""
	}
	if len(rows) == 0 && len(t.headers) == 0 {
		return ""
	}

	// Build the lipgloss/table.Table lazily on each Render so the
	// wrapper stays immutable from the caller's POV (set widths /
	// headers once, call Render many times).
	//
	// NOTE on Width(t.width): t.width is the *cell budget* (sum of
	// column widths without separators). lipgloss/table's Width(N)
	// is the *total visible width including separators*. With
	// BorderColumn(true) inserting one "│" cell per interior gap,
	// the total visible width is sum(colW) + (ncols-1). The actual
	// budget passed to lipgloss/table is therefore sum(colW) +
	// (ncols-1). When SetColumnWidths shrinks colW to less than
	// the original t.width, the rendered total is correspondingly
	// shorter than t.width; the t.width field is the *requested*
	// budget and remains the API contract.
	totalCells := t.TotalWidth()
	if t.ncols > 1 {
		totalCells += t.ncols - 1
	}
	tbl := table.New().
		Width(totalCells).
		// BorderColumn(true) draws a single "│" between columns —
		// this is the AGY reference's separator. Keep the other
		// borders off so the wrapper's output is a clean column
		// grid without frame lines, matching the previous
		// hand-rolled render (no top/bottom rules).
		BorderColumn(true).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderHeader(false).
		BorderRow(false).
		// Wrap is off because the "missing column width falls back
		// gracefully" path expects short cells to be padded, not
		// wrapped. The renderer still truncates long cells via
		// its internal truncateCell() + style.MaxWidth.
		Wrap(false)

	if len(t.headers) > 0 {
		headers := make([]string, t.ncols)
		copy(headers, t.headers)
		// Pad / truncate each header to its column width.
		for i := 0; i < t.ncols; i++ {
			headers[i] = padOrClamp(safeAt(headers, i), t.colW[i])
		}
		tbl = tbl.Headers(headers...)
	}

	// Each cell is pre-padded / pre-truncated to its column width
	// (or to its Cell.Width if the caller overrode it) before being
	// handed to lipgloss/table, because lipgloss's StyleFunc only
	// sees row+col indices, not the per-cell data.
	for _, row := range rows {
		cells := make([]string, t.ncols)
		for ci := 0; ci < t.ncols; ci++ {
			width := t.colW[ci]
			text := ""
			if ci < len(row.Cells) {
				text = row.Cells[ci].Text
				if row.Cells[ci].Width > 0 {
					width = row.Cells[ci].Width
				}
			}
			cells[ci] = padOrClamp(text, width)
		}
		tbl = tbl.Row(cells...)
	}

	// StyleFunc: header row gets headerStyle; data rows get cellStyle.
	// The styles are intentionally *not* given a Width — the resizer
	// computes per-column widths from the (already pre-padded) cell
	// content, and the per-cell render uses `style.Width(t.widths[c])`
	// internally. Setting Width here as well would conflict with the
	// resizer and produce shorter-than-expected columns.
	tbl = tbl.StyleFunc(func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return t.headerStyle
		}
		return t.cellStyle
	})

	// String() with default borders is non-empty as soon as there is
	// at least one header or row. We strip any trailing newline for
	// output-shape parity with the previous hand-rolled renderer.
	out := tbl.String()
	return strings.TrimRight(out, "\n")
}

// Hyperlink returns a clickable link, OSC 8-framed when caps.OSC8 is
// true, plain-text "label (url)" otherwise.
//
// Shape when OSC 8 enabled (the canonical "click here" hyperlink):
//
//	ESC ] 8 ; ; URL ESC \ TEXT ESC ] 8 ; ; ESC \
//
// Shape when OSC 8 disabled (the AGY 1.4.0 fallback path —
// recovered from the strings table row "Fixed links printing as raw
// escape sequences on terminals that do not implement OSC 8
// hyperlinks, such as Terminal.app, and extended the same detection
// to command output and alert bodies, which still emitted
// hyperlinks unconditionally"):
//
//	LABEL (URL)
//
// Cycle terminal-rendering-bbt-v209: bubbletea v2's internal
// hyperlink emitter can drop the closing ST when the text overflows
// the column width, which leaks the underline attribute into the
// next line — the "OSC 8 underflow" symptom. Hyperlink clamps the
// text to maxWidth before emitting the closing ST so the OSC 8
// frame always terminates even if the caller passed an overlong
// label. maxWidth <= 0 disables the clamp (use only when the caller
// has already validated the width).
//
// Cycle osc-hyperlinks (cycle 2 of the OSC pair): the function
// signature is keyed on caps (HyperlinkCaps). Production callers
// thread caps from cmd/wagyu/main.go's start-up path; the linked
// cosmetics (text width, OSC 8 frame shape, clamping) are unchanged
// for the OSC 8 enabled branch.
//
// If url is empty, Hyperlink returns text unchanged — the
// no-hyperlink case is a plain string and the OSC 8 frame would just
// produce two ESC ]8;;ST sequences around the text. The cycle-218
// markdown renderer uses this escape hatch.
func Hyperlink(url, text string, maxWidth int, caps HyperlinkCaps) string {
	if url == "" {
		return text
	}
	if !caps.OSC8 {
		// Plain-text fallback: AGY 1.4.0 emits "<label> (<url>)" so
		// the URL is still visible (and copy-pasteable) when the
		// terminal cannot parse OSC 8. The space-and-paren form
		// matches the recovered Terminal detection log's
		// "osc8=false" branch.
		return text + " (" + url + ")"
	}
	body := text
	if maxWidth > 0 {
		w := cellWidth(body)
		if w > maxWidth {
			body = truncateToCells(body, maxWidth)
		}
	}
	// ESC ] 8 ; ; URL ST   TEXT   ESC ] 8 ; ; ST
	const osc = "\x1b]8;;"
	const st = "\x1b\\"
	// Cycle osc8-semicolon-1.4.1 (cycle 240): percent-encode `;` and
	// `\\` in the URI segment before embedding it in the OSC 8 frame.
	// Some terminals / multiplexers (tmux without hyperlink-passthrough,
	// the GNU screen fork, certain web-based emulators) split OSC
	// sequences at the first `;` they see inside the URI, which would
	// otherwise truncate the URL at e.g. `https://example.com/?a=1;b=2`
	// to `https://example.com/?a=1` and silently drop the query string.
	// The label text (body) is left verbatim — semicolons in display
	// text are not an OSC 8 framing hazard. See
	// internal/render/url_osc8_encode.go for the full rationale and
	// the explicit non-goals (no full RFC 3986 pass).
	urlForOSC := urlForOSC8(url)
	var b strings.Builder
	b.Grow(len(osc)*2 + len(urlForOSC) + len(body) + len(st)*2 + 4)
	b.WriteString(osc)
	b.WriteString(urlForOSC)
	b.WriteString(st)
	b.WriteString(body)
	b.WriteString(osc)
	b.WriteString(st)
	return b.String()
}

// safeAt returns s[i] if i is in range, "" otherwise. Used so we
// don't panic when the headers slice is shorter than ncols (the
// lipgloss renderer pads automatically; this helper only matters
// when we pre-pad).
func safeAt(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// padOrClamp returns text padded with trailing spaces or clamped to
// exactly `width` cells (rune-count, not byte-count). This is the
// "missing column width falls back gracefully" helper for the new
// lipgloss-backed render.
func padOrClamp(text string, width int) string {
	if width <= 0 {
		return ""
	}
	w := cellWidth(text)
	if w == width {
		return text
	}
	if w > width {
		return truncateToCells(text, width)
	}
	return text + strings.Repeat(" ", width-w)
}

// computeColumnWidths distributes a `width`-cell budget across `ncols`
// columns. The first ncols-1 columns each get MinColumnWidth cells;
// the final column absorbs any remainder so the table exactly fills
// `width`. A pathological caller (width so small that ncols*MinColumnWidth
// exceeds it) collapses every column to MinColumnWidth; the caller
// should clamp width >= ncols*MinColumnWidth for the grid to draw
// cleanly.
func computeColumnWidths(width, ncols int) []int {
	colW := make([]int, ncols)
	if ncols == 1 {
		colW[0] = width
		return colW
	}
	minTotal := MinColumnWidth * ncols
	if width < minTotal {
		for i := range colW {
			colW[i] = MinColumnWidth
		}
		return colW
	}
	for i := 0; i < ncols-1; i++ {
		colW[i] = MinColumnWidth
	}
	colW[ncols-1] = width - MinColumnWidth*(ncols-1)
	return colW
}

// clampWidths mirrors the original SetColumnWidths semantics: each
// width is clamped to [MinColumnWidth, available budget for column i]
// where the budget for column i is `width - MinColumnWidth * (ncols-i-1)`.
// This preserves explicit per-column widths (e.g. [6,6,6] under width=20
// stays [6,6,6] — the table underfills rather than growing the trailing
// column). Sum > width shrinks trailing columns; pathological
// allocations where MinColumnWidth*ncols already exceeds `width`
// collapse every column to MinColumnWidth.
func clampWidths(widths []int, width, ncols int) []int {
	out := make([]int, ncols)
	for i := 0; i < ncols; i++ {
		budget := width - MinColumnWidth*(ncols-i-1)
		if budget < MinColumnWidth {
			budget = MinColumnWidth
		}
		w := MinColumnWidth
		if i < len(widths) {
			w = widths[i]
		}
		if w < MinColumnWidth {
			w = MinColumnWidth
		}
		if w > budget {
			w = budget
		}
		if w < MinColumnWidth {
			w = MinColumnWidth
		}
		out[i] = w
	}
	return out
}

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
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := make([]rune, 0, n)
	for i, r := range s {
		if len(runes) == n {
			// Drop any partial trailing rune by stopping at the
			// previous rune's byte boundary.
			_ = i
			break
		}
		runes = append(runes, r)
	}
	return string(runes)
}

// cellWidth returns the visible-cell width of s using the
// go-runewidth library, which honours East Asian wide-char widths
// (CJK ideographs, hiragana, katakana, fullwidth forms each consume
// two cells) and the Unicode emoji property (each emoji codepoint
// consumes two cells).
//
// Cycle 245: replaces runeWidth (which was rune-count based) as the
// budget unit for Box / DividerWithLabel / Hyperlink / padOrClamp.
// ASCII fast-path: every byte advances exactly one cell, so a pure
// ASCII string yields the same answer as the rune-count helper.
// The visible-cell math is what AGY 1.4.0 ships for plan-line cutoffs:
// the changelog description (AGY 1.4.0: "cutoff now accounts for
// terminal-cell width, not byte width") is exactly what cellWidth
// implements. The fallback to runewidth.StringWidth defaults to
// EastAsianWidth = false (the library's zero value); a caller that
// wants East Asian wide widths on (CJK = 2 cells) does not need to
// opt in — that's the default for any non-East-Asian terminal, but
// runewidth's StringWidth still returns 2 cells per CJK ideograph
// under that default. (runewidth documents: "Note that EastAsianWidth
// only affects ambiguous-width characters; non-ambiguous characters
// always use the spec-mandated width." CJK ideographs are
// non-ambiguous in East Asian Width — always 2 cells.)
func cellWidth(s string) int {
	return runewidth.StringWidth(s)
}

// truncateToCells returns s shortened to at most maxCells terminal
// cells, with no ellipsis appended (the caller chooses the tail).
// The cutoff always lands on a rune boundary so a multi-byte glyph
// is never split mid-codepoint, which would otherwise produce a
// U+FFFD replacement character at the trim edge (the AGY 1.4.0 fix
// the cycle-244 plan-CJK changelog line documents).
//
// Cycle 245: this is the cell-width counterpart to clampRunes. Use it
// for any surface with a visible-cell budget (Box inner padding,
// DividerWithLabel label, Hyperlink text clamp, padOrClamp cell
// width). Empty input returns "". maxCells <= 0 returns "" so a
// hostile caller asking for "clamp to a non-positive width" gets an
// empty string — matches clampRunes' "<=0 → empty" contract so the
// two helpers can be swapped without caller-side branching.
//
// runewidth.Truncate handles the cell-width math; the empty-string
// and non-positive-width guards are local because the underlying
// library returns s unchanged for maxCells <= 0 (we want "" to
// match clampRunes's contract).
func truncateToCells(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxCells {
		return s
	}
	return runewidth.Truncate(s, maxCells, "")
}
