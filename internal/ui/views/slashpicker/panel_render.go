// panel_render.go — the popup's rows.
//
// The rendering is byte for byte the reference's: the highlighted row
// is "> " plus the command name in the brand blue, the other rows are
// "  " plus the name, the description follows in the dim gray, and
// the "↑ N more" / "↓ N more" tails carry the count.
//
// 【一手】 agy 1.4.6, live tmux capture:
//
//	> /
//	───────────────────────────────────────────────────────────────────────
//	> /add-dir          Add a directory to the workspace
//	  /agents           List available custom agents
//	  /artifact         View and review artifacts
//	  /btw              Ask a side question without interrupting the current task
//	  /changelog        Show release notes and changes
//	  /compact          Compact the conversation now (cycle 237, agy 1.4.2)
//	   ↓ 89 more
//
//	  \u2191/\u2193 Navigate \u00b7 enter Select \u00b7 tab Complete
//
// Cycle 126 alignment with the v1.3.6 ref capture:
//
//   - the highlighted row's "> " is part of the command name in
//     true-colour blue (38;2;66;133;244) — the brand blue
//   - the other rows carry a "  " prefix and no colour on the name,
//     with a dim gray description
//   - the name column is padded to 51 chars ("> " + 49 or "  " + 49)
//   - the "↓ N more" tail row is dim gray (the descriptions' gray)
//   - a blank row separates the "↓ N more" tail from the hint row
//   - "esc to cancel" stays in the status row while the popup is up
//
// Cycle 283: the reference renders the descriptions and the "↓ N more"
// counter in Solarized base01 #586E75 (verified from a truecolour
// capture of the `/` popup: 38;2;88;110;117), one step dimmer than
// the labels.
package slashpicker

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/palette"
)

// The popup's colours are the palette's: the palette follows the
// selected colour scheme, so the aliases are refreshed whenever the
// scheme changes.
var (
	nameHi   = palette.BlueOpen
	descDim  = palette.SubtleOpen
	labelDim = palette.DimGrayOpen
	keyOpen  = "\x1b[1m" + palette.BlueOpen
)

func init() {
	palette.OnSchemeChange(refreshPaletteAliases)
}

// refreshPaletteAliases re-reads the palette's colours after the
// selected colour scheme changed.
func refreshPaletteAliases() {
	nameHi = palette.BlueOpen
	descDim = palette.SubtleOpen
	labelDim = palette.DimGrayOpen
	keyOpen = "\x1b[1m" + palette.BlueOpen
}

const (
	keyClose = "\x1b[0m"
	reset    = "\x1b[39m"
)

// Cycle 131: the description column width. The reference's
// description column is exactly 48 characters wide; descriptions that
// exceed it are truncated to 45 chars + "..." (recovered from
// /tmp/agycmp/hotpath/picker/ref.001-down5.txt where descriptions
// like `Ask a side question without interrupting the current task`
// (57 chars) truncate to `Ask a side question without interrupting the ...`
// (45 + `...`), and `Search code in the workspace (usage: /codesearch <query>)`
// (57 chars) truncates to `Search code in the workspace (usage: /codesea...`
// (45 + `...`)).
const descWidth = 48

// truncateDesc returns desc with at most descWidth characters,
// appending "..." when truncation occurred. Descriptions that already
// fit are returned unchanged. Cycle 131's width cap keeps the popup's
// rows from overflowing into the hint row below.
func truncateDesc(desc string) string {
	if len(desc) <= descWidth {
		return desc
	}
	return desc[:descWidth-3] + "..."
}

// The name column's width, and the model popup's label column.
const (
	nameWidth = 51
	// modelNameWidth is the width of the label column in the
	// "/model <query>" popup.
	//
	// 【一手】 agy 1.4.6, live tmux capture: the reference pads
	// "marker + label" to 30 columns and puts the catalogue id two
	// columns further right, in the same dim gray the command
	// descriptions use:
	//
	//	> Gemini 3.8 Flash (High)     gemini-3.8-flash-high
	//	  Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
	//
	// The highlighted row colours the 30-column field (and only that
	// field); the two-column gap stays uncoloured.
	modelNameWidth = 30
)

// computeOffset returns the index of the first row the popup draws.
//
// Cycle 129: when the highlight advances past the visible window (5
// rows), the renderer has to scroll the window so the highlight stays
// visible.
//
// Cycle 130: the reference centres the highlight in the visible window
// when the highlight passes the bottom row, instead of pinning it to
// the bottom (the previous pager-style behaviour). After pressing
// Down 5× with the popup on "/", the highlight lands on /clear (idx 5)
// and the reference's window shows rows [3..7] — `/btw`, `/changelog`,
// `> /clear (new)` (highlighted), `/codesearch`, `/config` — with
// "↑ 3 more" above the window and "↓ 91 more" below. Net: the
// window's offset = idx - (visible/2).
func computeOffset(hi, visible, total int) int {
	if hi < 0 {
		hi = 0
	}
	wantOffset := 0
	if hi >= visible-1 {
		wantOffset = hi - visible/2
	}
	if wantOffset > hi {
		wantOffset = hi
	}
	if wantOffset > total-visible {
		wantOffset = total - visible
	}
	if wantOffset < 0 {
		wantOffset = 0
	}
	return wantOffset
}

// Rows renders the popup's rows. It returns "" when the popup is
// hidden.
//
// As a side effect it fills in the popup's layout (the row count, the
// list row range, the hint row and the scroll-tail flags). The host
// reads the rectangle back through ClipBody so the mouse handlers can
// map a (x, y) screen coordinate onto a popup row.
func (p *Panel) Rows() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	const visible = 5
	const rowWidth = 220
	p.layout = layout{}
	if !p.visible {
		return ""
	}
	total := len(p.all)
	p.offset = computeOffset(p.idx, visible, total)
	if p.Empty() {
		return p.renderEmpty(&b, rowWidth)
	}
	rowIdx := p.renderList(&b, visible, total)
	p.renderFooter(&b, rowIdx)
	return b.String()
}

// renderEmpty writes the "No matches" row and the hint row.
//
// 【一手】 agy 1.4.6, live: a query that matches nothing keeps the
// popup up with a single dim row that reads "   No matches".
func (p *Panel) renderEmpty(b *strings.Builder, rowWidth int) string {
	b.WriteString(descDim)
	b.WriteString("   No matches")
	b.WriteString(strings.Repeat(" ", rowWidth-13))
	b.WriteString(reset)
	b.WriteByte('\n')
	b.WriteByte('\n')
	b.WriteString("  ")
	b.WriteString(keyOpen + "\u2191/\u2193" + keyClose)
	b.WriteString(labelDim + " Navigate \u00b7 ")
	b.WriteString(keyOpen + "enter" + keyClose)
	b.WriteString(labelDim + " Select \u00b7 ")
	b.WriteString(keyOpen + "tab" + keyClose)
	b.WriteString(labelDim + " Complete" + reset + "\n")
	p.layout.Rows = 3
	p.layout.HintRow = 2
	return b.String()
}

// renderRow writes one command row: the name in the 51-column field,
// the description in the dim gray.
// For SourceAgent commands, it styles them in palette.AgentFgOpen (green/sage)
// and displays a subtle badge so the user can easily distinguish agent commands from core commands.
func renderRow(b *strings.Builder, c *slashcommands.Command, isHi bool) {
	aliasSuffix := ""
	if len(c.Aliases) > 0 {
		aliasSuffix = " (" + c.Aliases[0] + ")"
	}
	badge := ""
	if c.Badge != "" {
		badge = " [" + c.Badge + "]"
	}
	desc := truncateDesc(c.Description)
	if isHi {
		name := "> " + c.Name + aliasSuffix + badge
		for len(name) < nameWidth {
			name += " "
		}
		if c.Source == slashcommands.SourceAgent {
			b.WriteString("\x1b[1m" + palette.AgentFgOpen)
		} else {
			b.WriteString(nameHi)
		}
		b.WriteString(name)
		b.WriteString(reset)
		b.WriteString(descDim)
		b.WriteString(desc)
		b.WriteString(reset)
		b.WriteString("\n")
		return
	}
	name := "  " + c.Name + aliasSuffix + badge
	for len(name) < nameWidth {
		name += " "
	}
	if c.Source == slashcommands.SourceAgent {
		b.WriteString(palette.AgentFgOpen)
		b.WriteString(name)
		b.WriteString(reset)
	} else {
		b.WriteString(name)
	}
	b.WriteString(descDim)
	b.WriteString(desc)
	b.WriteString(reset)
	b.WriteString("\n")
}

// renderModelRow writes one row of the "/model <query>" popup: the
// display label in the 30-column field, the catalogue id in dim gray.
func renderModelRow(b *strings.Builder, c *slashcommands.Command, isHi bool) {
	name := "  " + c.Name
	if isHi {
		name = "> " + c.Name
	}
	if len(name) < modelNameWidth {
		name += strings.Repeat(" ", modelNameWidth-len(name))
	}
	if isHi {
		b.WriteString(nameHi)
		b.WriteString(name)
		b.WriteString(reset)
	} else {
		b.WriteString(name)
	}
	b.WriteString("  ")
	b.WriteString(descDim)
	b.WriteString(c.Description)
	b.WriteString(reset)
	b.WriteString("\n")
}

// renderList writes the visible rows and the scroll tails. It returns
// the number of rows it wrote.
func (p *Panel) renderList(b *strings.Builder, visible, total int) int {
	rowIdx := 0
	if p.offset > 0 {
		more := p.offset
		b.WriteString(descDim + "   \u2191 ")
		if more == 1 {
			b.WriteString("1 more" + reset + "\n")
		} else {
			fmt.Fprintf(b, "%d more%s\n", more, reset)
		}
		p.layout.UpMore = true
		rowIdx++
	}
	p.layout.ListTop = rowIdx
	for i := 0; i < visible && i+p.offset < total; i++ {
		row := i + p.offset
		c := p.SelectedAt(row)
		if c == nil {
			break
		}
		if p.modelMode {
			renderModelRow(b, c, row == p.idx)
		} else {
			renderRow(b, c, row == p.idx)
		}
		rowIdx++
	}
	p.layout.ListBottom = rowIdx - 1
	p.layout.HasList = total > 0
	rowsBelowWindow := total - (p.offset + visible)
	if rowsBelowWindow > 0 {
		b.WriteString(descDim + "   \u2193 ")
		if rowsBelowWindow == 1 {
			b.WriteString("1 more" + reset + "\n")
		} else {
			fmt.Fprintf(b, "%d more%s\n", rowsBelowWindow, reset)
		}
		p.layout.DownMore = true
		rowIdx++
	}
	return rowIdx
}

// renderFooter writes the blank separator and the keyboard hint row.
func (p *Panel) renderFooter(b *strings.Builder, rowIdx int) {
	b.WriteString("\n")
	rowIdx++
	b.WriteString("  ")
	b.WriteString(keyOpen + "\u2191/\u2193" + keyClose)
	b.WriteString(labelDim + " Navigate \u00b7 ")
	b.WriteString(keyOpen + "enter" + keyClose)
	b.WriteString(labelDim + " Select \u00b7 ")
	b.WriteString(keyOpen + "tab" + keyClose)
	b.WriteString(labelDim + " Complete" + reset + "\n")
	p.layout.HintRow = rowIdx
	rowIdx++
	p.layout.Rows = rowIdx
}
