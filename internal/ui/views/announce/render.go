package announce

import (
	"strings"

	"github.com/justwasm/pigasus/palette"
)

const (
	colorSeparatorClose = "\x1b[0m"
	colorWarningClose   = "\x1b[39m"
)

// RenderCard renders one announcement card as a 3-row box (5 lines total).
func RenderCard(card *Card) string {
	if card == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(palette.DimGrayOpen)
	b.WriteString("  ┌─ announcement ")
	padLen := 56 - len("  ┌─ announcement ")
	if padLen < 0 {
		padLen = 0
	}
	b.WriteString(strings.Repeat("─", padLen))
	b.WriteString(colorSeparatorClose + "\n")

	b.WriteString(palette.DimGrayOpen + "  │  " + palette.BlueOpen + "ⓘ " + colorWarningClose + card.Title + "\n")
	b.WriteString(palette.DimGrayOpen + "  │  " + card.Body + "\n")

	hint := card.Hint
	if hint == "" {
		hint = "Esc to dismiss · Send a message to hide"
	}
	b.WriteString(palette.DimGrayOpen + "  │  " + hint + "\n")

	b.WriteString(palette.DimGrayOpen + "  └" + strings.Repeat("─", 70) + colorSeparatorClose + "\n")
	return b.String()
}
