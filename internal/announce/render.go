package announce

import (
	"strings"
)

const (
	colorSeparatorOpen  = "\x1b[38;2;95;99;104m"
	colorSeparatorClose = "\x1b[0m"
	colorDimGrayOpen    = "\x1b[38;2;95;99;104m"
	colorWarningBlue    = "\x1b[38;2;66;133;244m"
	colorWarningClose   = "\x1b[39m"
)

// RenderCard renders one announcement card as a 3-row box (5 lines total).
func RenderCard(card *Card) string {
	if card == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(colorSeparatorOpen)
	b.WriteString("  ┌─ announcement ")
	padLen := 56 - len("  ┌─ announcement ")
	if padLen < 0 {
		padLen = 0
	}
	b.WriteString(strings.Repeat("─", padLen))
	b.WriteString(colorSeparatorClose + "\n")

	b.WriteString(colorDimGrayOpen + "  │  " + colorWarningBlue + "ⓘ " + colorWarningClose + card.Title + "\n")
	b.WriteString(colorDimGrayOpen + "  │  " + card.Body + "\n")

	hint := card.Hint
	if hint == "" {
		hint = "Esc to dismiss · Send a message to hide"
	}
	b.WriteString(colorDimGrayOpen + "  │  " + hint + "\n")

	b.WriteString(colorSeparatorOpen + "  └" + strings.Repeat("─", 70) + colorSeparatorClose + "\n")
	return b.String()
}
