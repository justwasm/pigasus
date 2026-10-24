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
)

// MaxContextTokens is the 1.0M-token window the panel reports. The
// reference keeps the magic number in the panel itself.
const MaxContextTokens = 1_000_000

// Cell is one `□` of the usage grid, painted in dim gray 241.
const Cell = "\x1b[38;2;101;123;131m□\x1b[39m"

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

// GridRowWidth is the terminal-width-aware form: the reference paints one
// `□` cell per five columns (24 at 120x40, 40 at 200x50, 44 at 220x50).
func GridRowWidth(text string, pad, width int) string {
	return GridRowPaddedCols(text, pad, ColsFor(width))
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
const (
	UserGlyph    = "\x1b[38;2;38;139;210m◉\x1b[39m"
	AgentGlyph   = "\x1b[38;2;133;153;0m◉\x1b[39m"
	ToolGlyph    = "\x1b[38;5;172m◉\x1b[39m"
	SystemGlyph  = "\x1b[38;2;88;110;117m⛁\x1b[39m"
	FreeSpaceRow = "\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M %s"
)

// grayGlyph is the dim-gray variant of the system-prompt glyph.
func grayGlyph(glyph string) string { return "\x1b[38;2;88;110;117m" + glyph + "\x1b[39m" }

// StreamingRows are the descriptor rows of the in-flight layout (cycle 289):
// a turn in flight shows only the user/agent/tool rows and one grid cell
// per non-empty category — the system categories appear once the turn
// completes.
func StreamingRows(model string, used, free, contextMax, user, agent, tool int) []Row {
	return []Row{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, TokenLabel(used)), 5},
		{Pct(used, contextMax), 6},
		{"\x1b[1m\x1b[38;2;88;110;117mEstimated usage (awaiting generation)\x1b[0m", 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: %s tokens %s", TokenLabel(user), Pct(user, contextMax)), 5},
		{fmt.Sprintf("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: %s tokens %s", TokenLabel(agent), Pct(agent, contextMax)), 5},
		{fmt.Sprintf("\x1b[38;5;172m◉\x1b[0m Tool calls: %s tokens %s", TokenLabel(tool), Pct(tool, contextMax)), 5},
		{fmt.Sprintf(FreeSpaceRow, Pct(free, contextMax)), 5},
	}
}

// LiveRows are the descriptor rows of the completed-turn layout.
func LiveRows(model string, used, free, contextMax int, user, agent, tool, sysPrompt, sysTools, skills, subagents int) []Row {
	return []Row{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, TokenLabel(used)), 5},
		{Pct(used, contextMax), 6},
		{"\x1b[1m\x1b[38;2;88;110;117mToken usage by category\x1b[0m", 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: %s tokens %s", TokenLabel(user), Pct(user, contextMax)), 5},
		{fmt.Sprintf("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: %s tokens %s", TokenLabel(agent), Pct(agent, contextMax)), 5},
		{fmt.Sprintf("\x1b[38;5;172m◉\x1b[0m Tool calls: %s tokens %s", TokenLabel(tool), Pct(tool, contextMax)), 5},
		{fmt.Sprintf("%s System prompt: %s tokens %s", grayGlyph("⛁"), TokenLabel(sysPrompt), Pct(sysPrompt, contextMax)), 5},
		{fmt.Sprintf("%s System tools: %s tokens %s", grayGlyph("⛁"), TokenLabel(sysTools), Pct(sysTools, contextMax)), 5},
		{fmt.Sprintf("%s Skills: %s tokens %s", grayGlyph("⛁"), TokenLabel(skills), Pct(skills, contextMax)), 5},
		{fmt.Sprintf("%s Subagents: %s tokens %s", grayGlyph("⛁"), TokenLabel(subagents), Pct(subagents, contextMax)), 5},
		{fmt.Sprintf(FreeSpaceRow, Pct(free, contextMax)), 5},
	}
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
func LiveFill(sysPrompt, user, agent, tool, sysTools, skills, subagents int) []string {
	gray := func(glyph string) string { return grayGlyph(glyph) }
	cats := []struct {
		tokens int
		cell   string
	}{
		{sysPrompt, gray("◉")},
		{user, UserGlyph},
		{agent, AgentGlyph},
		{tool, ToolGlyph},
		{sysTools, gray("◉")},
		{skills, gray("◉")},
		{subagents, gray("◉")},
	}
	var fill []string
	for _, c := range cats {
		if c.tokens <= 0 {
			continue
		}
		n := (c.tokens + 3999) / 4000
		if n < 1 {
			n = 1
		}
		for i := 0; i < n; i++ {
			fill = append(fill, c.cell)
		}
	}
	return fill
}
