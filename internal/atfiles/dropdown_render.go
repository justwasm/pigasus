package atfiles

import (
	"fmt"
	"strings"
)

const (
	ColorNameHi   = "\x1b[38;2;66;133;244m"
	ColorDescDim  = "\x1b[38;2;95;99;104m"
	ColorReset    = "\x1b[0m"
	ColorKeyOpen  = "\x1b[38;2;66;133;244m"
	ColorKeyClose = "\x1b[38;2;95;99;104m"
)

// View renders the dropdown overlay rows.
func (d *Dropdown) View(width int) string {
	if !d.visible {
		return ""
	}
	const visible = 5
	rowWidth := width
	if rowWidth <= 0 {
		rowWidth = 120
	}
	nameWidth := d.calcNameWidth(rowWidth)
	var b strings.Builder
	if len(d.candidates) == 0 {
		return d.renderEmpty(nameWidth, rowWidth, visible)
	}

	total := len(d.candidates)
	hi := d.idx
	if hi < 0 {
		hi = 0
	}
	wantOffset := computeOffset(total, hi, visible)
	if wantOffset > 0 {
		renderUpMore(&b, wantOffset)
	}
	d.renderRows(&b, wantOffset, visible, total, hi, nameWidth, rowWidth)
	renderDownMore(&b, total-(wantOffset+visible))
	b.WriteString("\n\n" + HintRow())
	return b.String()
}

func (d *Dropdown) calcNameWidth(rowWidth int) int {
	if d.queryMode {
		return 0
	}
	if d.prefix == "" {
		return 33
	}
	nameWidth := rowWidth - 18
	if nameWidth < 20 {
		return 20
	}
	return nameWidth
}

func (d *Dropdown) renderEmpty(nameWidth, rowWidth, visible int) string {
	var b strings.Builder
	RenderRow(&b, 33, rowWidth, true,
		"Type to search workspace files...",
		"File", "Keep typing to filter files or directories", true)
	for i := 1; i < visible; i++ {
		b.WriteByte('\n')
	}
	b.WriteString("\n\n" + HintRow())
	return b.String()
}

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

func renderUpMore(b *strings.Builder, more int) {
	b.WriteString(ColorDescDim)
	if more == 1 {
		b.WriteString("   \u2191 1 more")
	} else {
		fmt.Fprintf(b, "   \u2191 %d more", more)
	}
	b.WriteString(ColorReset + "\n")
}

func renderDownMore(b *strings.Builder, rowsBelow int) {
	if rowsBelow <= 0 {
		return
	}
	b.WriteString(ColorDescDim)
	if rowsBelow == 1 {
		b.WriteString("   \u2193 1 more")
	} else {
		fmt.Fprintf(b, "   \u2193 %d more", rowsBelow)
	}
	b.WriteString(ColorReset + "\n")
}

func (d *Dropdown) renderRows(b *strings.Builder, wantOffset, visible, total, hi, nameWidth, rowWidth int) {
	for i := 0; i < visible; i++ {
		row := i + wantOffset
		selected := row == hi
		if row >= total {
			b.WriteByte('\n')
			continue
		}
		e := d.candidates[row]
		if d.queryMode {
			renderQueryRow(b, selected, e.Rel)
			continue
		}
		kind := "File"
		if e.IsDir {
			kind = "Directory"
		}
		RenderRow(b, nameWidth, rowWidth, selected, e.Rel, kind, e.Path, false)
	}
}

func renderQueryRow(b *strings.Builder, selected bool, rel string) {
	prefix := "  "
	if selected {
		prefix = "> "
		b.WriteString(ColorNameHi)
	}
	b.WriteString(prefix + rel)
	if selected {
		b.WriteString(ColorReset)
	}
	b.WriteByte('\n')
}

// RenderRow emits one column-mode row: name, type, path.
func RenderRow(b *strings.Builder, nameWidth, rowWidth int, selected bool, name, kind, path string, placeholder bool) {
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
	b.WriteString(pad + ColorDescDim)
	fmt.Fprintf(b, "%-12s", kind)
	avail := rowWidth - len(label) - len(pad) - 12
	if avail < 4 {
		avail = 4
	}
	if len(path) > avail {
		path = "/..."
	}
	b.WriteString(path + ColorReset + "\n")
}

func renderOverflowLabel(b *strings.Builder, label string, rowWidth int, selected bool) {
	if len(label) > rowWidth {
		label = label[:rowWidth]
	}
	writeColored(b, label, selected)
	b.WriteByte('\n')
}

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

func writeColored(b *strings.Builder, text string, selected bool) {
	if selected {
		b.WriteString(ColorNameHi)
	}
	b.WriteString(text)
	if selected {
		b.WriteString(ColorReset)
	}
}

// HintRow is the keyboard hint strip rendered under the candidate list.
func HintRow() string {
	var b strings.Builder
	key := func(s string) string { return ColorKeyOpen + s + ColorKeyClose }
	label := func(s string) string { return ColorDescDim + s }
	b.WriteString("  ")
	b.WriteString(key("\u2191/\u2193") + label(" Navigate \u00b7 "))
	b.WriteString(key("enter") + label(" Select \u00b7 "))
	b.WriteString(key("tab") + label(" Complete"))
	b.WriteString(ColorReset + "\n")
	return b.String()
}
