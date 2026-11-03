// panel.go — the /feature-diff panel's own rows: the document it
// scrolls and the frame the panel shows when it has no document.
//
// # Where the facts live
//
// The panel's document is the feature table (see table.go) and the
// frame under it is the panel's. The host keeps the window offset —
// its panel struct carries one offset that several panels share — and
// the terminal's height, which decides how many document rows one
// window shows.

package feat

import (
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The window's height when the terminal's height is unknown, and the
// number of rows the panel's chrome takes off the top of it.
const (
	fallbackVisible = 17
	chromeRows      = 4
)

// PanelLines renders the /feature-diff document, or the "data file not
// found" frame when the clone has no feature table.
func PanelLines() []string {
	t := SourceData()
	if t == nil {
		return notFoundLines()
	}
	return PanelDoc(t)
}

// notFoundLines is the frame the panel shows when the clone has no
// feature table: the title, the answer and the way out.
func notFoundLines() []string {
	return []string{
		PanelTitle(),
		"",
		palette.DimGrayOpen + "  data file not found (run from the project root)." + palette.DimGrayClose,
		"",
		tuilib.PanelDotHint("  ", "esc", "Close"),
	}
}

// Spec is the /feature-diff document and the window over it.
//
// A terminal whose height is known gives the reference's own geometry —
// the panel's title and chrome take four rows — and an unknown one
// falls back to the window's fixed height.
func Spec(height int) tuiviewer.Spec {
	t := SourceData()
	if t == nil {
		return tuiviewer.Spec{}
	}
	doc := PanelDoc(t)
	vis := fallbackVisible
	if height > 0 {
		vis = height - chromeRows
		if vis < 1 {
			vis = 1
		}
	}
	return tuiviewer.Spec{Lines: doc, Visible: vis, Total: len(doc)}
}
