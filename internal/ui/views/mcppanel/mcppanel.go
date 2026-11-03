// Package mcppanel holds the /mcp panel's frame: the title, the empty
// state and the legend.
//
// # What it is
//
// The reference (`agy`) answers `/mcp` with a panel that lists the
// configured MCP servers. The clone's panel renders the *empty* frame —
// "No MCP servers configured." — because the clone's own /mcp command
// answers the configured servers in the transcript (see the host's
// mcpServersFeedback) and the panel itself has never listed them.
//
// Cycle 205 deliberately left the frame as it was, and the layout below
// is the reference's own, verbatim from
// /tmp/agycmp/panels/ppref.mcp.raw.txt.
//
// # Where it lives
//
// The rows live here, so the host holds only the panel's kind and the
// fact that the panel closes silently. The frame has no state: the
// panel is a fixed set of rows, so there is nothing to rebuild on open
// and no cursor to keep.
package mcppanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent one is read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"

	// panelGray238C closes the dim grey's span.
	panelGray238C = "\x1b[39m"
)

// panelGray238 is the dim grey the empty state is painted in — the
// selected scheme's muted foreground.
var panelGray238 = palette.SubtleOpen

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	panelGray238 = palette.SubtleOpen
}

// Lines renders the /mcp panel.
//
// The five rows are the reference's own: the title, a blank, the empty
// state, a blank and the legend.
func Lines() []string {
	return []string{
		panelTitleOpen + "MCP Servers" + panelTitleClose,
		"",
		panelGray238 + "  No MCP servers configured." + panelGray238C,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Actions", "esc", "Exit"),
	}
}
