// panel_helpers.go — the small predicates the panel's keys share.
//
// The two chorded bindings are the ones the reference names in its picker
// legend: "ctrl+g open in editor" and the global "esc"/Ctrl+C way out. They are
// spelled here so the list and the viewer agree on what counts as a chord.

package artifactpanel

import (
	tea "charm.land/bubbletea/v2"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// panelCursor moves the picker's cursor.
//
// It is the shared list arithmetic the other list panels use (ListNav via
// tuiviewport.PanelCursor), so a page key moves ten rows and the home/end keys
// park the cursor on the first or last artifact.
func panelCursor(cursor int, action tuilib.NavAction, total int) int {
	return tuiviewport.PanelCursor(cursor, action, total)
}

// isCtrlC reports whether the key is Ctrl+C — the global "leave" key, which no
// panel swallows.
func isCtrlC(key tea.Key) bool {
	return key.Code == 'c' && key.Mod.Contains(tea.ModCtrl)
}

// isCtrlG reports whether the key is Ctrl+G — the reference's "open in editor"
// binding.
//
// Bubbletea may report the chord either as the letter with the modifier or as
// its string form, so both are accepted.
func isCtrlG(key tea.Key) bool {
	if key.String() == "ctrl+g" {
		return true
	}
	return key.Code == 'g' && key.Mod.Contains(tea.ModCtrl)
}
