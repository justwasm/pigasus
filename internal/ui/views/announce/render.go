package announce

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/palette"
)

const (
	defaultCardWidth = 70
)

// RenderCard renders one announcement card with a thick left border (┃),
// matching upstream agy's renderInputCard prompt card framing.
func RenderCard(card *Card) string {
	return RenderBoxedCard(card, defaultCardWidth)
}

// RenderBoxedCard renders one announcement card using upstream agy's renderInputCard
// layout: a thick left border (┃) in accent color, padded content with Title, Body, and Hint.
func RenderBoxedCard(card *Card, width int) string {
	if card == nil {
		return ""
	}
	if width <= 0 {
		width = defaultCardWidth
	}
	if width < 30 {
		width = 30
	}

	hint := card.Hint
	if hint == "" {
		hint = "Esc to dismiss · Send a message to hide"
	}

	var content strings.Builder
	content.WriteString(palette.BoldOpen + card.Title + palette.SeparatorClose + "\n")
	if card.Body != "" {
		content.WriteString(card.Body + "\n")
	}
	content.WriteString(palette.DimGrayOpen + hint + palette.SeparatorClose)

	cardWidth := width - 3
	if cardWidth < 10 {
		cardWidth = 10
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("#268BD2")).
		PaddingLeft(1).
		Width(cardWidth)

	return style.Render(content.String()) + "\n"
}
