// hooks_host.go — the host's side of the /hooks panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/hookspanel: it owns the hook-type
// list, the per-type matcher list, the add-matcher and add-hook forms,
// the cursor, the keys and the state. The host supplies the one thing
// the panel cannot answer itself — the way out of the panel — and
// nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the cursor, the stage
// or the form's text is never shown.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
)

// newHooksPanel builds the /hooks panel with the host's seams.
func (m *runMode) newHooksPanel() *hookspanel.Panel {
	return hookspanel.New(hookspanel.Seams{
		// Esc on the type list leaves the panel.
		Close: m.closePanel,
	})
}

// hooksPanel returns the open /hooks panel.
//
// The panel is built when the /hooks panel opens — the host's slashPanel
// struct is reset whenever a panel closes — so the accessor is here so
// that a caller which finds the panel missing still gets one that
// answers.
func (m *runMode) hooksPanel() *hookspanel.Panel {
	if m.panel.hk == nil {
		m.panel.hk = m.newHooksPanel()
	}
	return m.panel.hk
}
