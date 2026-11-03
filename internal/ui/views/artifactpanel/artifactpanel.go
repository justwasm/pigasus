// Package artifactpanel holds the /artifact panel's frame: the title,
// the empty state and the legend.
//
// # What it is
//
// The reference (`agy`) answers `/artifact` with a panel that lists the
// artifacts of the turn and offers to preview, open or edit them. The
// clone's panel renders the *empty* frame — "No artifacts" — because
// the clone's own /artifact command answers "No artifacts" in the
// transcript and the panel itself has never listed any.
//
// # Where it lives
//
// The rows live here, so the host holds only the panel's kind and the
// fact that the panel echoes the "⎿  Exited /artifact command" notice
// when it closes. The frame has no state: the panel is a fixed set of
// rows, so there is nothing to rebuild on open and no cursor to keep.
package artifactpanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"
)

// Lines renders the /artifact panel.
//
// The four rows are the reference's own: the title, the empty state, a
// blank and the legend.
func Lines() []string {
	return []string{
		panelTitleOpen + "Artifacts" + panelTitleClose,
		"  " + palette.DimGrayOpen + "No artifacts" + palette.DimGrayClose,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "p", "preview", "enter", "open",
			"ctrl+g", "open in editor", "esc", "Dismiss"),
	}
}
