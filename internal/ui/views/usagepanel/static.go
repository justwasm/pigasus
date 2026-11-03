// static.go — the /usage panel's frame for the shape it shows while the
// terminal's size is unknown.
//
// # Where the facts live
//
// The panel has two shapes: the height-aware one (see panel.go) and the
// cycle-193 fallback the unit tests render, where the terminal's size
// is not known yet. The fallback's document, its window and the legend
// under it are the panel's as well.
//
// The window's offset is the host's — the host's panel struct carries
// one offset that several panels share — so the rows it produced arrive
// as an argument, with the numbers the counter quotes.

package usagepanel

import (
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// The static document's window: the reference's own figures — the
// document is twenty logical lines and the window shows seventeen of
// them.
const (
	staticVisible = 17
	staticTotal   = 20
)

// StaticSpec is the /usage document and the window over it for the
// shape the panel shows while the terminal's size is unknown.
func StaticSpec() tuiviewer.Spec {
	return tuiviewer.Spec{
		Lines:   ViewerLinesStatic(),
		Visible: staticVisible,
		Total:   staticTotal,
	}
}

// StaticLines is the /usage panel's frame for the height-unknown shape:
// the window over the static document, a blank, the counter, a blank
// and the legend.
//
// The window itself is the host's — the host's panel struct carries one
// offset that several panels share — so the rows it produced arrive as
// an argument, with the numbers the counter quotes.
func StaticLines(rows []string, start, end, total int) []string {
	lines := append([]string{}, rows...)
	lines = append(lines,
		"",
		tuiviewer.Counter(start, end, total),
		"",
		tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"))
	return lines
}
