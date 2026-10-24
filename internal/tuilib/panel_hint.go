// Package tuilib panel_hint.go — keyboard hints, legend rows, and slash-echo lines for panels.
package tuilib

import (
	"strings"

	"github.com/justwasm/pigasus/internal/palette"
)

// PanelKeyboardHint builds the `Keyboard: <key> <label>  <key> <label>`
// legend row.
func PanelKeyboardHint(pairs ...string) string {
	var b strings.Builder
	b.WriteString("\x1b[1mKeyboard: \x1b[0m")
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(palette.BlueOpen)
		b.WriteString(pairs[i])
		b.WriteString(palette.BlueClose)
		b.WriteString(" ")
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(pairs[i+1])
		b.WriteString(palette.DimGrayClose)
	}
	return b.String()
}

// PanelDotHint builds the `<lead><key> <label> · <key> <label>` legend row.
func PanelDotHint(lead string, pairs ...string) string {
	var b strings.Builder
	b.WriteString(lead)
	if len(pairs) >= 2 {
		b.WriteString(palette.PanelBoldBlueOpen)
		b.WriteString(pairs[0])
		b.WriteString(palette.PanelBoldBlueClose)
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(" ")
		b.WriteString(pairs[1])
	}
	for i := 2; i+1 < len(pairs); i += 2 {
		b.WriteString(" · ")
		b.WriteString(palette.PanelBoldBlueOpen)
		b.WriteString(pairs[i])
		b.WriteString(palette.PanelBoldBlueClose)
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(" ")
		b.WriteString(pairs[i+1])
	}
	b.WriteString(palette.DimGrayClose)
	return b.String()
}

// SlashEchoLines renders the transcript block for one slash command.
func SlashEchoLines(cmd, msg string) []string {
	return []string{
		palette.WarningBlueOpen + "> " + cmd + palette.WarningBlueClose,
		palette.WarningBlueOpen + "  \u23bf  " + palette.WarningBlueClose + palette.SubtleOpen + msg + "\x1b[39m",
		"",
	}
}
