// panel_style.go — the /provider panel's colours and glyphs.
//
// The colours come from the palette when a row is drawn, so they follow
// the selected scheme: the panel does not cache them at the time it
// opens.

package providerpanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own frame. The title is bold on the default foreground.
const (
	panelTitleOpen  = "\x1b[1m\x1b[39m"
	panelTitleClose = "\x1b[0m"

	// keyClose ends a styled span.
	keyClose = "\x1b[0m"

	// panelGray238C ends the muted grey the panel's own rows use.
	panelGray238C = "\x1b[39m"
)

// The verification line's colours: green for a pass, red for a failure.
// They are the repo's true-colour constants (the effort slider's green
// and the bash-mode failure red), not new ones.
const (
	verifyOKOpen    = "\x1b[38;2;133;153;0m"
	verifyOKClose   = "\x1b[39m"
	verifyFailOpen  = "\x1b[38;2;220;50;47m"
	verifyFailClose = "\x1b[39m"
)

// searchCursorBlock is the `Search:` input cursor the panel draws as one
// reverse-video cell.
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// searchUnderline is the 20-cell `─` row under the Search: input, in the
// selected scheme's separator colour.
var searchUnderline = palette.SeparatorOpen + "────────────────────\x1b[39m"

// panelKeyboardHint builds the `Keyboard: <key> <label>  <key> <label>`
// legend row.
func panelKeyboardHint(pairs ...string) string {
	return tuilib.PanelKeyboardHint(pairs...)
}
