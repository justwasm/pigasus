// panel_render.go — the /rewind picker's rows.
//
// The frames are the reference's own: the title, the blank row, the
// step rows, the blank row, the counter and the legend. The empty
// state is the cycle-189 form — the literal `(current)` row.

package rewindpanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// Lines renders the /rewind picker.
//
// The empty state is the cycle-189 form (the literal `(current)` row);
// the non-empty form is cycle 222's — one row per user-anchored turn,
// newest first, with the cursor row in brand blue and a `(current)`
// annotation on the most recent turn.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	steps := p.steps()
	if len(steps) == 0 {
		return []string{
			panelTitleOpen + "Rewind Conversation" + panelTitleClose,
			"",
			blueOpen + "> (current)" + colorClose,
			"",
			tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
		}
	}
	visible := p.visible(len(steps))
	total := len(steps)
	p.clamp()
	window := p.window
	allRows := renderStepRows(steps, p.cursor, blueOpen, colorClose, gray238, colorClose)

	lines := []string{
		panelTitleOpen + "Rewind Conversation" + panelTitleClose,
		"",
	}
	lines = append(lines, allRows[window:window+visible]...)
	lines = append(lines, "")
	if total > visible {
		lines = append(lines, stepCounter(window+1, window+visible, total, dimGrayOpen, colorClose))
	}
	lines = append(lines, tuilib.PanelKeyboardHint("↑/↓", "Navigate",
		"enter", "Select", "esc", "Cancel"))
	return lines
}
