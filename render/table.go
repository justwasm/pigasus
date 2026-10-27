package render

import (
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
)

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

	tbl := t.newLipglossTable()
	if len(t.headers) > 0 {
		t.applyHeaders(tbl)
	}
	t.applyRows(tbl, rows)

	tbl = tbl.StyleFunc(func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return t.headerStyle
		}
		return t.cellStyle
	})

	out := tbl.String()
	return strings.TrimRight(out, "\n")
}

func (t *Table) newLipglossTable() *table.Table {
	totalCells := t.TotalWidth()
	if t.ncols > 1 {
		totalCells += t.ncols - 1
	}
	return table.New().
		Width(totalCells).
		BorderColumn(true).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderHeader(false).
		BorderRow(false).
		Wrap(false)
}

func (t *Table) applyHeaders(tbl *table.Table) {
	headers := make([]string, t.ncols)
	copy(headers, t.headers)
	for i := 0; i < t.ncols; i++ {
		headers[i] = padOrClamp(safeAt(headers, i), t.colW[i])
	}
	tbl.Headers(headers...)
}

func (t *Table) applyRows(tbl *table.Table, rows []Row) {
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
		tbl.Row(cells...)
	}
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
// exactly `width` cells. This is the "missing column width falls back
// gracefully" helper for the lipgloss-backed render.
func padOrClamp(text string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(text)
	if w > width {
		text = ansi.Truncate(text, width, "")
		w = ansi.StringWidth(text)
	}
	if pad := width - w; pad > 0 {
		return text + strings.Repeat(" ", pad)
	}
	return text
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
