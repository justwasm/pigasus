// rows.go — the transcript's own rows.
//
// This is the helper that renders a line of the conversation
// transcript: the user's own prompt row. It belongs to the transcript,
// not to a panel.

package transcript

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// UserPromptRow renders the user's own message in the transcript.
// 【反编译】 agy 1.4.8 render.UserPromptText styles the prompt row with
// Scheme.UserFg + bold. Continuation lines in multi-line prompts are
// indented with 2 spaces to align with the "> " prefix.
func UserPromptRow(text string) string {
	if !strings.Contains(text, "\n") {
		return palette.UserFgBoldOpen + "> " + text + palette.BlueBoldClose
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = palette.UserFgBoldOpen + "> " + l + palette.BlueBoldClose
		} else if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}

// RenderUserPrompt formats and word-wraps a user prompt to fit width.
// The first row starts with "> " in Scheme.UserFg + bold (or WarningBlue for slash commands).
// Subsequent rows are indented by 2 spaces to align with the text.
// Each row is returned as an independent line in the slice (no embedded newlines).
func RenderUserPrompt(text string, width int) []string {
	if text == "" {
		return []string{palette.UserFgBoldOpen + "> " + palette.BlueBoldClose}
	}
	openStyle := palette.UserFgBoldOpen
	closeStyle := palette.BlueBoldClose
	if strings.HasPrefix(text, "/") {
		openStyle = palette.WarningBlueOpen
		closeStyle = palette.WarningBlueClose
	}

	if width <= 0 {
		var out []string
		lines := strings.Split(text, "\n")
		for i, l := range lines {
			if i == 0 {
				out = append(out, openStyle+"> "+l+closeStyle)
			} else if l != "" {
				out = append(out, "  "+l)
			} else {
				out = append(out, "")
			}
		}
		return out
	}

	avail := width - 2
	if avail < 10 {
		avail = width
	}

	rawLines := strings.Split(text, "\n")
	var out []string
	firstRow := true

	for _, rawLine := range rawLines {
		if rawLine == "" {
			if firstRow {
				out = append(out, openStyle+"> "+closeStyle)
				firstRow = false
			} else {
				out = append(out, "")
			}
			continue
		}

		wrapped := tuilib.WrapRows(rawLine, avail)
		for _, wLine := range wrapped {
			if firstRow {
				out = append(out, openStyle+"> "+wLine+closeStyle)
				firstRow = false
			} else if wLine != "" {
				out = append(out, "  "+wLine)
			} else {
				out = append(out, "")
			}
		}
	}
	if len(out) == 0 {
		out = append(out, openStyle+"> "+closeStyle)
	}
	return out
}
