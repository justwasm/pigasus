// Package contextpanel holds the pure layout rules of the /context
// usage panel: the 44-cell `□` grid, the token-count labels, the
// percentage formatting and the category rows.
//
// The panel itself is a runMode method (cmd/wagyu/slash_panel_context.go)
// because it reads the live session's token counts; everything here is a
// pure function of its arguments, so it can be exercised without a TUI.
//
// The byte shapes were recovered from ppref.context.empty.raw.txt on an
// empty session (cycle 190) and extended to non-empty sessions in
// cycle 224 / cycle 289.
package contextpanel

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// MaxContextTokens is the 1.0M-token window the panel reports. The
// reference keeps the magic number in the panel itself.
const MaxContextTokens = 1_000_000

// The panel's colours follow the selected colour scheme, so the
// painted glyphs are rebuilt whenever the scheme changes.
var (
	// Cell is one `□` of the usage grid, painted in the dim foreground.
	Cell = palette.DimGrayOpen + "□\x1b[39m"
)

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	Cell = palette.DimGrayOpen + "□\x1b[39m"
	UserGlyph = palette.UserFgOpen + "◉\x1b[39m"
	AgentGlyph = palette.AgentFgOpen + "◉\x1b[39m"
	SystemGlyph = palette.SubtleOpen + "⛁\x1b[39m"
	SystemCellGlyph = palette.SubtleOpen + "◉\x1b[39m"
	CheckpointGlyph = palette.DimGrayOpen + "⊠\x1b[39m"
	FreeSpaceRow = palette.DimGrayOpen + "□\x1b[39m Free space: 1.0M %s"
	Header = "\x1b[1m" + palette.SubtleOpen + "└ Context Usage\x1b[0m"
}

// DefaultCols is the historical 220-column shape: one cell per five
// columns.
const DefaultCols = 44

// ColsFor returns the number of grid cells for a terminal width. A zero
// width keeps the historical 220-column shape for tests and callers that
// have not measured a window yet.
func ColsFor(width int) int {
	cols := width / 5
	if width <= 0 {
		cols = DefaultCols
	}
	if cols < 1 {
		cols = 1
	}
	return cols
}

// GridRow renders one row of the usage grid with the right-side descriptor
// plain (default fg). The cells use dim gray 241, matching
// ppref.context.empty.raw.txt rows 15–21. There is no SGR between the last
// cell and the descriptor (so the right-side text inherits the default fg,
// which is also what REF does).
func GridRow(text string) string {
	return GridRowPadded(text, 5)
}

// GridRowPadded is GridRow with explicit padding width. REF uses 5 spaces
// for most rows and 6 for the `(0.0%)` row, which shifts the percentage one
// column further right (ppref row 16).
func GridRowPadded(text string, pad int) string {
	return GridRowPaddedCols(text, pad, DefaultCols)
}

// GridRowPaddedCols renders the grid with an explicit cell count.
func GridRowPaddedCols(text string, pad, cols int) string {
	var b strings.Builder
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(Cell)
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(text)
	return b.String()
}

// GridRowFilled renders the grid with the first len(fill) cells painted
// with their category glyph instead of the empty `□`. offset is the index
// of the grid's first cell in the fill sequence.
func GridRowFilled(text string, pad, cols int, fill []string, offset int) string {
	var b strings.Builder
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		if idx := offset + i; idx >= 0 && idx < len(fill) {
			b.WriteString(fill[idx])
		} else {
			b.WriteString(Cell)
		}
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(text)
	return b.String()
}

// Pct renders the percentage the panel shows next to a token count.
func Pct(x, contextMax int) string {
	if x <= 0 {
		return "(0.0%)"
	}
	return fmt.Sprintf("(%.1f%%)", float64(x)/float64(contextMax)*100)
}

// TokenLabel renders a token count the way the panel does: raw below 1000,
// `1.2k` up to 999,950 and `1.0M` above that.
func TokenLabel(n int) string {
	if n >= 999_950 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.Itoa(n)
}

// Row is one descriptor row of the usage table: the text plus the pad
// that separates it from the grid.
type Row struct {
	Text string
	Pad  int
}

// The category glyphs the panel paints for a non-empty category.
//
// The user's, the agent's and the system's glyphs come from the
// selected scheme's slots; the tool's glyph is a colour the reference
// hard-codes.
var (
	UserGlyph    = palette.UserFgOpen + "◉\x1b[39m"
	AgentGlyph   = palette.AgentFgOpen + "◉\x1b[39m"
	SystemGlyph  = palette.SubtleOpen + "⛁\x1b[39m"
	FreeSpaceRow = palette.DimGrayOpen + "□\x1b[39m Free space: 1.0M %s"
)

// SystemCellGlyph is the dim grey ◉: the grid cell the system and the
// customization categories paint, and the marker the system-messages row
// carries.
//
// The rows' own marker is the ⛁ (SystemGlyph); the cells they paint in
// the grid are a ◉ in the same dim grey.
var SystemCellGlyph = palette.SubtleOpen + "◉\x1b[39m"

// CheckpointGlyph is the marker the checkpoint-buffer row carries.
//
// The reference's own marker is the ⊠ — the row is outside the grid, so
// its glyph is not one of the grid's cells.
var CheckpointGlyph = palette.DimGrayOpen + "⊠\x1b[39m"

// UserRowMarker is the marker the user row carries.
//
// The reference hard-codes the bold 26 colour for the user row's ◉ — it
// is the one row marker that does not follow the scheme.
const UserRowMarker = "\x1b[1m\x1b[38;5;26m◉\x1b[0m"

// ToolRowMarker is the marker the tool row carries.
//
// The reference hard-codes the orange 172 colour for the tool row's ◉.
// It is not the grid cell the tool category paints (ToolGlyph): the
// row's marker resets the colour, the cell does not.
const ToolRowMarker = "\x1b[38;5;172m◉\x1b[0m"

// EmptyToolRowMarker is the tool row's marker in the empty layout.
//
// The empty session's capture (cycle 190) carries the bold variant; the
// completed turn's capture (cycle 224) does not.
const EmptyToolRowMarker = "\x1b[1m\x1b[38;5;172m◉\x1b[0m"

// ToolGlyph is the tool category's grid cell — a colour the reference
// hard-codes, so it does not move with the scheme.
const ToolGlyph = "\x1b[38;5;172m◉\x1b[39m"

// StreamingRows are the descriptor rows of the in-flight layout (cycle 289):
// a turn in flight shows only the user/agent/tool rows and one grid cell
// per non-empty category — the system categories appear once the turn
// completes.
func StreamingRows(model string, used, free, contextMax, user, agent, tool int) []Row {
	return []Row{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, TokenLabel(used)), 5},
		{Pct(used, contextMax), 6},
		{"\x1b[1m" + palette.SubtleOpen + "Estimated usage (awaiting generation)\x1b[0m", 5},
		{fmt.Sprintf("%s User messages: %s tokens %s", UserRowMarker, TokenLabel(user), Pct(user, contextMax)), 5},
		{fmt.Sprintf("%s Agent responses: %s tokens %s", AgentGlyph, TokenLabel(agent), Pct(agent, contextMax)), 5},
		{fmt.Sprintf("%s Tool calls: %s tokens %s", ToolRowMarker, TokenLabel(tool), Pct(tool, contextMax)), 5},
		{fmt.Sprintf(FreeSpaceRow, Pct(free, contextMax)), 5},
	}
}

// LiveRows are the descriptor rows of the completed-turn layout.
//
// The rows are the categories the reference's documentation names, in
// its order, and the free-space row that closes the grid. A category
// that consumes no tokens is not a row (see Category).
func LiveRows(model string, used, free, contextMax int, cats []Category) []Row {
	rows := []Row{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, TokenLabel(used)), 5},
		{Pct(used, contextMax), 6},
		{"\x1b[1m" + palette.SubtleOpen + "Token usage by category\x1b[0m", 5},
	}
	for _, c := range cats {
		rows = append(rows, Row{
			Text: fmt.Sprintf("%s %s: %s tokens %s", c.Marker, c.Label, TokenLabel(c.Tokens), Pct(c.Tokens, contextMax)),
			Pad:  5,
		})
	}
	return append(rows, Row{Text: fmt.Sprintf(FreeSpaceRow, Pct(free, contextMax)), Pad: 5})
}

// CheckpointRow is the checkpoint-buffer row.
//
// The reference tracks the checkpoint's tokens outside the grid: they
// stand in for turns that have already been compacted, so counting them
// in the usage percentage would inflate it. The row carries no
// percentage — the reference's own format string is
// `%s Checkpoint buffer: %s tokens (not counted in usage)`.
func CheckpointRow(tokens int) string {
	return fmt.Sprintf("%s Checkpoint buffer: %s tokens (not counted in usage)",
		CheckpointGlyph, TokenLabel(tokens))
}

// StreamingFill is the grid-cell fill of the in-flight layout: one cell
// per 4000 tokens of each non-empty category, painted with that
// category's glyph.
func StreamingFill(user, agent, tool int) []string {
	var fill []string
	if user > 0 {
		fill = append(fill, UserGlyph)
	}
	if agent > 0 {
		fill = append(fill, AgentGlyph)
	}
	if tool > 0 {
		fill = append(fill, ToolGlyph)
	}
	return fill
}

// LiveFill is the grid-cell fill of the completed-turn layout.
//
// The cells are painted one per 4000 tokens of each category, and the
// categories are painted in the reference's own order: the system
// prompt first, then the rest in their row order (cycle 224).
func LiveFill(cats []Category) []string {
	var fill []string
	for _, c := range cats {
		if c.Lead {
			fill = append(fill, categoryCells(c)...)
		}
	}
	for _, c := range cats {
		if !c.Lead {
			fill = append(fill, categoryCells(c)...)
		}
	}
	return fill
}

// categoryCells is the number of grid cells one category paints: one per
// 4000 tokens, and none at all when it consumes no tokens.
func categoryCells(c Category) []string {
	if c.Tokens <= 0 {
		return nil
	}
	n := (c.Tokens + 3999) / 4000
	cells := make([]string, 0, n)
	for i := 0; i < n; i++ {
		cells = append(cells, c.Cell)
	}
	return cells
}
