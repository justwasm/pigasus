// slash_panel_wiring.go — the host's facts for the panel registry.
//
// The registry owns the fan-out over the panels: the dispatcher keeps
// the fan-out, the domain keeps the table (invariant 75). The host owns
// the facts the registry cannot answer itself:
//
//   - the row count of every panel that has rows: the cursor bound of
//     a vertical list panel;
//   - the /codesearch panel, which owns the keys of its result list and
//     lets the rest through;
//   - the two seams: how a panel is left, and how a document viewer
//     takes a key.
//
// The facts are wired once, when the registry is first asked for a
// decision or for a row count.

package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// panelRegistry returns the panel registry with the host's facts wired
// into it.
//
// The registry itself is built once and kept (see getPanelRegistry); the
// wiring is what the registry cannot answer on its own.
func (m *runMode) panelRegistry() *panelregistry.Registry {
	reg := m.getPanelRegistry()
	if !reg.Wired() {
		m.wirePanelRegistry(reg)
	}
	return reg
}

// wirePanelRegistry attaches the host's facts to the panel registry.
//
// The row counts move with the panels that own the rows: each panel
// counts its own rows, and the registry asks the panel. The /codesearch
// panel answers whether it claimed the key it was given, because the
// keys it does not own belong to the list rule.
func (m *runMode) wirePanelRegistry(reg *panelregistry.Registry) {
	reg.Wire(panelregistry.Wiring{
		Rows: map[panelregistry.Kind]func() int{
			// Cycle 381: the /tasks panel owns its rows, and the count
			// is the number of task rows it renders — the registry's
			// most recent tasks, capped at the panel's limit.
			panelregistry.Tasks: func() int { return m.tasksPanel().RowCount() },
			// Cycle 381: the /effort panel owns its marker, and the
			// count is the number of tiers the slider offers.
			panelregistry.Effort: func() int { return m.effortPanel().RowCount() },
			// Cycle 381: the /hooks panel owns its rows, and the count
			// depends on the stage: the hook types, a type's
			// matcher rows (two for a tool hook, one for a lifecycle
			// hook) or the form's single input.
			panelregistry.Hooks: func() int { return m.hooksPanel().RowCount() },
			panelregistry.Permissions: func() int {
				return m.permissionsPanel().RowCount()
			},
			// Cycle 381: the /agents panel owns its rows: the built-in
			// default plus the markdown agents the catalogue holds.
			panelregistry.Agents: func() int { return m.agentsPanel().RowCount() },
			// Cycle 381: the /credits panel owns its rows: the two
			// actions.
			panelregistry.Credits: func() int { return m.creditsPanel().RowCount() },
			// Cycle 381: the /feedback panel owns its rows: the six
			// categories of the picker.
			panelregistry.Feedback: func() int { return m.feedbackPanel().RowCount() },
			// Cycle 381: the /rewind picker owns its rows, and the
			// count is the number of turns the conversation has.
			panelregistry.Rewind: func() int { return m.rewindPanel().RowCount() },
			// The row list is the reference's five static rows plus the
			// conditional 5.5 rows plus the models the user configured,
			// so the count is not a constant: a hard-coded 6 made every
			// row past the sixth unreachable (the cursor stopped on
			// "MiniMax M3" and could never reach "MiniMax M3.1 Flash").
			// It also follows the search filter, so it shrinks with the
			// query.
			panelregistry.Model: func() int { return m.modelPanel().RowCount() },
			panelregistry.Config: func() int {
				return m.configPanel().RowCount()
			},
			panelregistry.Plugin: func() int { return m.pluginPanel().RowCount() },
			panelregistry.Skills: func() int { return m.skillsPanel().RowCount() },
			panelregistry.Provider: func() int {
				return m.providerPanel().RowCount()
			},
			panelregistry.Accounts: func() int {
				return m.accountsPanel().RowCount()
			},
		},
		Claims: map[panelregistry.Kind]func(tea.Key) (bool, tea.Cmd, bool){
			// Cycle 381: the /codesearch panel owns the navigation of a
			// live result list — ↑/↓ move the cursor over the matches
			// and ←/→ jump between the files — so the generic list
			// navigation must not see them. A key the panel does not
			// own falls through, so the global handlers (Ctrl+C) still
			// see it.
			panelregistry.CodeSearch: func(key tea.Key) (bool, tea.Cmd, bool) {
				return m.codesearchPanel().HandleKey(key)
			},
		},
		Seams: panelregistry.Seams{
			// The list rule's Esc leaves the panel, the way every
			// other panel's Esc does.
			Close: m.closePanel,
			// The document viewer owns the viewport's scroll state; the
			// host owns the shared window offset, so the offset goes in
			// and the clamped one comes back.
			ViewerKey: func(key tea.Key) (bool, tea.Cmd) {
				return m.handleViewerPanelKey(key)
			},
		},
	})
}
