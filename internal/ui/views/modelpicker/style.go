// style.go — the model picker's own chrome.
//
// The picker's styles are its own: the panel title, the dim grey the
// "(current)" annotation is painted in and the escape that closes it.
//
// The effort slider's styles are not here — they live with the slider,
// in internal/ui/views/effortpanel, which the picker borrows for the
// embedded slider it draws under its row list.

package modelpicker

import (
	"github.com/justwasm/pigasus/palette"
)

const (
	// PanelGray238C closes the dim grey span.
	PanelGray238C = "\x1b[39m"

	// PanelTitleOpen is the panel title: bold + default fg.
	PanelTitleOpen = "\x1b[1m\x1b[39m"
	// PanelTitleClose closes the title's span.
	PanelTitleClose = "\x1b[0m"
)

// PanelGray238 is the dim grey the row annotations are painted in — the
// selected scheme's muted foreground.
//
// It is an alias of the palette's colour, refreshed when the scheme
// changes: an alias copied once would keep the old scheme's escape.
var PanelGray238 = palette.SubtleOpen

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	PanelGray238 = palette.SubtleOpen
}
