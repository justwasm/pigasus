package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

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
	renderBoxBorder(&b, "┌", "┐", inner, open, close)
	b.WriteByte('\n')
	// Body: each line padded with spaces inside │...│
	for _, line := range content {
		renderBoxLine(&b, line, inner, open, close)
	}
	// Bottom edge: └───┘
	renderBoxBorder(&b, "└", "┘", inner, open, close)
	return b.String()
}

func renderBoxBorder(b *strings.Builder, left, right string, inner int, open, close string) {
	b.WriteString(open)
	b.WriteString(left)
	b.WriteString(strings.Repeat("─", inner))
	b.WriteString(right)
	b.WriteString(close)
}

func renderBoxLine(b *strings.Builder, line string, inner int, open, close string) {
	w := ansi.StringWidth(line)
	b.WriteString(open)
	b.WriteString("│")
	if w > inner {
		b.WriteString(truncateToCells(line, inner))
	} else {
		b.WriteString(line)
		if pad := inner - w; pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	b.WriteString("│")
	b.WriteString(close)
	b.WriteByte('\n')
}
