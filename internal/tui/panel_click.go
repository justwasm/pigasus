// panel_click.go — the mouse's half of the /logs surface.
//
// # What a click means here
//
// The surface has two regions a pointer can act on: the view bar, whose
// labels switch view, and a body row, whose click puts the cursor there
// and unfolds or folds it. Every other cell is text, and the host's drag
// selection owns it.
//
// The split between the press and the release is the point. A press on a
// body row can only be an *anchor*: the pointer may be about to drag
// across the text, and a press that folded the row would have consumed
// the drag before it began. So the press places the cursor and arms the
// row, and the release either runs the armed action — the click was a
// click — or discards it because the drag that happened is a selection.
//
// # Why the mapping lives in the host
//
// The panel is a Tier-5 view: it holds no screen coordinates and knows
// nothing about the frame's origin. The host owns the translation from a
// terminal cell to a panel-relative row, exactly as it does for the
// transcript, so the panel is given a row and a column that are already
// its own.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// panelPress is what the panel did with a press: whether it claimed the
// press at all, whether a drag may start from here, and any command the
// press itself produced (a view switch, a bar click).
type panelPress struct {
	// claimed is false when the press is not the panel's, so the ordinary
	// drag selection starts and the panel is not involved.
	claimed bool
	// draggable is true when the press landed on a body row and the host
	// should start a selection from it. The row's own action waits for
	// the release.
	draggable bool
	// cmd is what the press itself produced, if anything.
	cmd tea.Cmd
}

// panelClick routes a press inside the panel's rectangle to the panel that
// owns clickable rows.
//
// The panel's rows answer a press in two ways. A press on the view bar is
// an action and claims the press outright. A press on a body row only
// moves the cursor: the fold it means happens on the release, which is
// what lets the same press be the anchor of a drag. It used to fold on
// the press and return "handled", so a press on a body row never started
// a selection and the text under the pointer could not be dragged from.
func (m *RootModel) panelClick(ev tea.Mouse) panelPress {
	if m.overlays.panel.kind != panelregistry.Debug {
		return panelPress{}
	}
	row := ev.Y - m.overlays.panelScreenY
	col := ev.X
	p := m.debugPanel()
	before := p.View()
	handled, cmd := p.Click(row, col)
	if !handled {
		return panelPress{}
	}
	m.invalidateViewCache()
	if p.View() != before || cmd != nil {
		m.overlays.panelPressArmed = false
		return panelPress{claimed: true, cmd: cmd}
	}
	// The panel placed its cursor on a body row and has an action waiting
	// for the release.
	m.overlays.panelPressArmed = true
	return panelPress{claimed: true, draggable: true}
}

// panelRelease runs the action a press on a panel row means, when the
// press did not turn into a drag.
func (m *RootModel) panelRelease() tea.Cmd {
	if !m.overlays.panelPressArmed {
		return nil
	}
	m.overlays.panelPressArmed = false
	if m.overlays.panel.kind != panelregistry.Debug {
		return nil
	}
	handled, cmd := m.debugPanel().Act()
	if !handled {
		return nil
	}
	m.invalidateViewCache()
	return cmd
}
