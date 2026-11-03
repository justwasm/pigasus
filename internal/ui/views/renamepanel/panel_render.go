// panel_render.go — the /rename panel's rows.
//
// The frame is the reference's own: the title, a blank, the input row
// and the keyboard legend.
//
//	Rename Conversation
//
//	  Rename: ▋
//
//	Keyboard: enter Rename  esc Cancel
//
// The input row uses the textinput's View(), so the virtual cursor and
// the placeholder styling match the reference's other panel surfaces.

package renamepanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// The panel's own SGR spans.
//
// The title span is the reference's: bold on the default foreground.
const (
	panelTitleOpen  = "\x1b[1m\x1b[39m"
	panelTitleClose = "\x1b[0m"
)

// Lines renders the panel.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	return []string{
		panelTitleOpen + "Rename Conversation" + panelTitleClose,
		"",
		"  " + p.input.View(),
		"",
		tuilib.PanelKeyboardHint("enter", "Rename", "esc", "Cancel"),
	}
}
