// panel_render.go — the /feedback panel's rows.
//
// The frames are the reference's own: the category picker with its six
// rows and the dot legend, and the review form with the echoed
// category, the one-line buffer and the submit row.

package feedbackpanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"

	// panelGray241 is the dim grey the reference uses for the
	// secondary half of the review form's title and for the "esc
	// Back" hint.
	panelGray241 = "\x1b[38;2;101;123;131m"
	// panelGray241C closes the dim grey's span.
	panelGray241C = "\x1b[39m"

	// searchCursorBlock is the `Search:` input cursor REF draws as one
	// reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`). The form's
	// buffer ends with it.
	searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

	// submitRowOpen is the reverse-video span of the form's
	// "ctrl+s Submit Feedback" row.
	submitRowOpen = "\x1b[7m\x1b[38;2;133;153;0m"
	// submitRowClose closes the reverse-video span.
	submitRowClose = "\x1b[0m"
)

// Lines renders the panel: the category picker in the first stage, the
// review form in the second.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.stage > 0 {
		return p.formLines()
	}
	return p.pickerLines()
}

// pickerLines is the first stage: the six categories and the legend.
func (p *Panel) pickerLines() []string {
	lines := []string{
		panelTitleOpen + "Submit Feedback" + panelTitleClose,
		"",
		"What went wrong? Select a category to assist your report:",
		"",
	}
	for i, c := range Categories {
		if i == p.cursor {
			lines = append(lines, palette.BlueOpen+"> "+c+palette.BlueClose)
		} else {
			lines = append(lines, "  "+c)
		}
	}
	lines = append(lines,
		"",
		tuilib.PanelDotHint("  ", "esc", "Cancel", "1-6", "Select & Continue", "enter", "Continue"),
	)
	return lines
}

// formLines is the second stage: the chosen category, the one-line
// buffer and the submit row.
func (p *Panel) formLines() []string {
	cursor := p.cursor
	if cursor < 0 || cursor >= len(Categories) {
		cursor = 0
	}
	return []string{
		panelTitleOpen + "Review Submission Data" + panelTitleClose + "  " +
			panelGray241 + "(You can always provide manual feedback via /feedback)" + panelGray241C,
		"",
		"Your feedback helps improve Pigasus CLI. The following sanitized data will be sent:",
		"",
		"Feedback: " + palette.BlueOpen + CategoryLabel(Categories[cursor]) + palette.BlueClose,
		"",
		"Additional Details (optional):",
		p.text + searchCursorBlock,
		"",
		"",
		"",
		submitRowOpen + " ctrl+s Submit Feedback " + submitRowClose + "  " +
			panelGray241 + "esc Back" + panelGray241C,
	}
}
