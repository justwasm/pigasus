package cliapp

import (
	tea "charm.land/bubbletea/v2"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)


func (m *runMode) handleNavAndEnterKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.panel.kind == panelregistry.Config {
		// Cycle 381: the /config panel owns every key it receives — the
		// search box takes the letters and the arrow keys change the
		// setting's value — so the generic list navigation must not see
		// them. A key the panel does not own falls through, so the
		// global handlers (Ctrl+C) still see it.
		if handled, cmd := m.configPanel().HandleKey(key); handled {
			return true, cmd, true
		}
	}
	switch action := tuilib.ClassifyListNavKey(key); action {
	case tuilib.NavUp, tuilib.NavDown, tuilib.NavPageUp, tuilib.NavPageDown,
		tuilib.NavTop, tuilib.NavBottom:
		// Cycle 381: the cursor arithmetic is the viewport's — the same
		// ListNav the other list panels use — so a panel cursor moves by
		// the reference's rules and is clamped into the panel's rows.
		m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, action, n)
		return true, nil, true
	case tuilib.NavCancel:
		return true, m.closePanel(), true
	}
	return false, nil, false
}

func (m *runMode) handlePanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.panel.kind == panelregistry.None {
		return false, nil
	}
	// Cycle 381: the registry owns the fan-out over the panels. It knows
	// which route claims a key for the open panel — the panel's own
	// handler, the search route of /codesearch, the list rule, or the
	// document viewer — and it answers whether the key was claimed. A key
	// no route claims reaches the host's own handlers below.
	if handled, cmd, claimed := m.panelRegistry().HandleKey(m.panel.kind, key); claimed {
		return handled, cmd
	}
	n := m.panelRegistry().RowCount(m.panel.kind)
	if m.panel.kind == panelregistry.Model {
		// Cycle 381: the /model panel owns every key it receives — the
		// search box takes the letters and the arrow keys move the
		// effort slider — so the generic list navigation must not see
		// them. A key the panel does not own falls through, so the
		// global handlers (Ctrl+C) still see it.
		if handled, cmd := m.modelPanel().HandleKey(key); handled {
			return true, cmd
		}
	}
	if handled, cmd, ok := m.handleNavAndEnterKey(key, n); ok {
		return handled, cmd
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, m.closePanel()
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

// listPanelKind reports whether k is one of the four key-owning list
// panels: /tasks, /artifact, /codesearch, /context.
//
// /tasks routes to its own panel first (the panel owns the cursor, the
// detail view and the kill), but it stays on the list: the gate in
// tui_update_keys.go hands a list panel's keys to the panel *before*
// the global handlers see them.
//
// Cycle 381: the set itself is the registry's (Kind.IsList) — it is the
// registry's table, next to the routes it drives. This forwarder stays
// because the host's own key pipeline asks the question by name; a
// follow-up should ask the registry at the call site and drop it.
func listPanelKind(k panelregistry.Kind) bool {
	return k.IsList()
}
