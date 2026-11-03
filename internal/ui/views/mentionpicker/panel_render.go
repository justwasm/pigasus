// panel_render.go — the dropdown's rows.
//
// The dropdown's colours are hard-coded: they are not the palette's
// scheme slots, they are the reference's own values, recovered from
// the live binary's truecolour capture. A scheme change does not
// repaint them.
//
// 【一手】 agy 1.4.3, live: the listing renders five visible rows, the
// "↑ N more" / "↓ N more" tails, a blank separator and the hint row.

package mentionpicker

import (
	"fmt"
	"strings"
)

// The dropdown's colours.
//
// They are the reference's own values, not the palette's scheme slots:
// the live binary emits these escapes whatever scheme the terminal
// wears.
const (
	colorNameHi   = "\x1b[38;2;66;133;244m"
	colorDescDim  = "\x1b[38;2;95;99;104m"
	colorReset    = "\x1b[0m"
	colorKeyOpen  = "\x1b[38;2;66;133;244m"
	colorKeyClose = "\x1b[38;2;95;99;104m"
)

// visibleRows is the number of candidate rows the dropdown draws.
//
// 【一手】 agy 1.4.3, live: the listing shows five rows and the tail
// rows carry the number of rows the window hides.
const visibleRows = 5

// Rows renders the dropdown's overlay rows.
//
// A hidden dropdown writes nothing.
func (p *Panel) Rows() string {
	if p == nil || !p.visible {
		return ""
	}
	rowWidth := p.width()
	if rowWidth <= 0 {
		rowWidth = 120
	}
	nameWidth := p.calcNameWidth(rowWidth)
	var b strings.Builder
	if len(p.candidates) == 0 {
		return p.renderEmpty(nameWidth, rowWidth, visibleRows)
	}

	total := len(p.candidates)
	hi := p.idx
	if hi < 0 {
		hi = 0
	}
	wantOffset := computeOffset(total, hi, visibleRows)
	if wantOffset > 0 {
		renderUpMore(&b, wantOffset)
	}
	p.renderRows(&b, wantOffset, visibleRows, total, hi, nameWidth, rowWidth)
	renderDownMore(&b, total-(wantOffset+visibleRows))
	b.WriteString("\n\n" + hintRow())
	return b.String()
}

// calcNameWidth returns the width of the name column.
//
// The query mode has no name column — the row is the relative path
// alone — and the bare "@" listing uses the reference's fixed 33
// columns.
func (p *Panel) calcNameWidth(rowWidth int) int {
	if p.queryMode {
		return 0
	}
	if p.prefix == "" {
		return 33
	}
	nameWidth := rowWidth - 18
	if nameWidth < 20 {
		return 20
	}
	return nameWidth
}

// renderEmpty renders the placeholder listing — the row the reference
// shows before the user has typed a query.
func (p *Panel) renderEmpty(nameWidth, rowWidth, visible int) string {
	var b strings.Builder
	renderRow(&b, 33, rowWidth, true,
		"Type to search workspace files...",
		"File", "Keep typing to filter files or directories", true)
	for i := 1; i < visible; i++ {
		b.WriteByte('\n')
	}
	b.WriteString("\n\n" + hintRow())
	return b.String()
}

// computeOffset returns the index of the first row the window shows.
func computeOffset(total, hi, visible int) int {
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
		return 0
	}
	return wantOffset
}

// renderUpMore renders the "↑ N more" tail.
func renderUpMore(b *strings.Builder, more int) {
	b.WriteString(colorDescDim)
	if more == 1 {
		b.WriteString("   \u2191 1 more")
	} else {
		fmt.Fprintf(b, "   \u2191 %d more", more)
	}
	b.WriteString(colorReset + "\n")
}

// renderDownMore renders the "↓ N more" tail.
func renderDownMore(b *strings.Builder, rowsBelow int) {
	if rowsBelow <= 0 {
		return
	}
	b.WriteString(colorDescDim)
	if rowsBelow == 1 {
		b.WriteString("   \u2193 1 more")
	} else {
		fmt.Fprintf(b, "   \u2193 %d more", rowsBelow)
	}
	b.WriteString(colorReset + "\n")
}

// renderRows renders the window's candidate rows.
func (p *Panel) renderRows(b *strings.Builder, wantOffset, visible, total, hi, nameWidth, rowWidth int) {
	for i := 0; i < visible; i++ {
		row := i + wantOffset
		selected := row == hi
		if row >= total {
			b.WriteByte('\n')
			continue
		}
		e := p.candidates[row]
		if p.queryMode {
			renderQueryRow(b, selected, e.Rel)
			continue
		}
		kind := "File"
		if e.IsDir {
			kind = "Directory"
		}
		renderRow(b, nameWidth, rowWidth, selected, e.Rel, kind, e.Path, false)
	}
}

// renderQueryRow renders one row of the query mode — the relative path
// alone.
func renderQueryRow(b *strings.Builder, selected bool, rel string) {
	prefix := "  "
	if selected {
		prefix = "> "
		b.WriteString(colorNameHi)
	}
	b.WriteString(prefix + rel)
	if selected {
		b.WriteString(colorReset)
	}
	b.WriteByte('\n')
}

// renderRow emits one column-mode row: name, type, path.
func renderRow(b *strings.Builder, nameWidth, rowWidth int, selected bool, name, kind, path string, placeholder bool) {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	label := prefix + name
	if !placeholder && nameWidth > 0 && len(label) > nameWidth {
		renderOverflowLabel(b, label, rowWidth, selected)
		return
	}
	pad := calcPadding(nameWidth, len(label), placeholder)
	writeColored(b, label, selected)
	b.WriteString(pad + colorDescDim)
	fmt.Fprintf(b, "%-12s", kind)
	avail := rowWidth - len(label) - len(pad) - 12
	if avail < 4 {
		avail = 4
	}
	if len(path) > avail {
		path = "/..."
	}
	b.WriteString(path + colorReset + "\n")
}

// renderOverflowLabel renders a name too long for its column.
func renderOverflowLabel(b *strings.Builder, label string, rowWidth int, selected bool) {
	if len(label) > rowWidth {
		label = label[:rowWidth]
	}
	writeColored(b, label, selected)
	b.WriteByte('\n')
}

// calcPadding returns the spaces that pad the name column.
func calcPadding(nameWidth, labelLen int, placeholder bool) string {
	if nameWidth > 0 {
		if labelLen < nameWidth {
			return strings.Repeat(" ", nameWidth-labelLen)
		} else if placeholder {
			return "  "
		}
	}
	return ""
}

// writeColored writes the text, highlighted when the row is selected.
func writeColored(b *strings.Builder, text string, selected bool) {
	if selected {
		b.WriteString(colorNameHi)
	}
	b.WriteString(text)
	if selected {
		b.WriteString(colorReset)
	}
}

// hintRow is the keyboard hint strip rendered under the candidate list.
func hintRow() string {
	var b strings.Builder
	key := func(s string) string { return colorKeyOpen + s + colorKeyClose }
	label := func(s string) string { return colorDescDim + s }
	b.WriteString("  ")
	b.WriteString(key("\u2191/\u2193") + label(" Navigate \u00b7 "))
	b.WriteString(key("enter") + label(" Select \u00b7 "))
	b.WriteString(key("tab") + label(" Complete"))
	b.WriteString(colorReset + "\n")
	return b.String()
}
