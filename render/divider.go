package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Divider renders a horizontal rule of width cells using the recovered
// separator glyph (─). width <= 0 falls back to DefaultBoxWidth. The
// style parameter is an optional ANSI prefix; empty means no colour.
// Divider pads short content with trailing spaces so the rule aligns
// with the box above / below it in the panel layout.
func Divider(width int, style string) string {
	if width <= 0 {
		width = DefaultBoxWidth
	}
	rule := strings.Repeat("─", width)
	if style != "" {
		return style + rule + "\x1b[0m"
	}
	return rule
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
	rw := ansi.StringWidth(label)
	right := width - 5 - rw
	if right < 1 || rw > width-5 {
		return Divider(width, style)
	}
	rule := "─── " + label + " " + strings.Repeat("─", right)
	if style != "" {
		return style + rule + "\x1b[0m"
	}
	return rule
}
