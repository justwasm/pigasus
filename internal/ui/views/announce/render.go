package announce

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

const (
	colorSeparatorClose = "\x1b[0m"
	colorWarningClose   = "\x1b[39m"
	defaultCardWidth    = 70
)

// RenderCard renders one announcement card as a fully-enclosed rectangular box
// with standard box-drawing borders (5 lines total), matching upstream agy's prompt card framing.
func RenderCard(card *Card) string {
	return RenderBoxedCard(card, defaultCardWidth)
}

// RenderBoxedCard renders one announcement card as a fully-enclosed rectangular box with
// balanced top/bottom borders, right vertical borders, and proper inner padding, matching
// PermissionPrompt and SideQuestionCard framing.
func RenderBoxedCard(card *Card, width int) string {
	if card == nil {
		return ""
	}
	if width <= 0 {
		width = defaultCardWidth
	}
	if width < 50 {
		width = 50
	}
	innerWidth := width - 8
	if innerWidth < 20 {
		innerWidth = 20
	}

	var b strings.Builder

	// 1. Top border: "  ┌─ announcement ──...──┐"
	prefix := "  ┌─ announcement "
	padLen := width - 19
	if padLen < 2 {
		padLen = 2
	}
	b.WriteString(palette.DimGrayOpen + prefix + strings.Repeat("─", padLen) + "┐" + colorSeparatorClose + "\n")

	// 2. Title row: "  │  ⓘ <Title>  │"
	titleText := "ⓘ " + card.Title
	titleVis := ansi.StringWidth(titleText)
	titlePad := innerWidth - titleVis
	if titlePad < 0 {
		titlePad = 0
	}
	b.WriteString(palette.DimGrayOpen + "  │  " + palette.BlueOpen + "ⓘ " + colorWarningClose + card.Title + strings.Repeat(" ", titlePad) + palette.DimGrayOpen + "  │" + colorSeparatorClose + "\n")

	// 3. Body row: "  │  <Body>  │"
	bodyVis := ansi.StringWidth(card.Body)
	bodyPad := innerWidth - bodyVis
	if bodyPad < 0 {
		bodyPad = 0
	}
	b.WriteString(palette.DimGrayOpen + "  │  " + colorWarningClose + card.Body + strings.Repeat(" ", bodyPad) + palette.DimGrayOpen + "  │" + colorSeparatorClose + "\n")

	// 4. Hint row: "  │  <Hint>  │"
	hint := card.Hint
	if hint == "" {
		hint = "Esc to dismiss · Send a message to hide"
	}
	hintVis := ansi.StringWidth(hint)
	hintPad := innerWidth - hintVis
	if hintPad < 0 {
		hintPad = 0
	}
	b.WriteString(palette.DimGrayOpen + "  │  " + hint + strings.Repeat(" ", hintPad) + palette.DimGrayOpen + "  │" + colorSeparatorClose + "\n")

	// 5. Bottom border: "  └────────────────...──┘"
	b.WriteString(palette.DimGrayOpen + "  └" + strings.Repeat("─", width-4) + "┘" + colorSeparatorClose + "\n")

	return b.String()
}
