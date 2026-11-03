// permissions_host.go — the host's side of the /permissions panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/permissionspanel: it owns the
// scope picker, the per-scope permission lists, the add/edit rule form,
// the cursor, the keys and the writes. The host supplies the one thing
// the panel cannot answer itself — the way out of the panel — and
// nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the cursor, the
// scope, the tab or the form's text is never shown.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
)

// newPermissionsPanel builds the /permissions panel with the host's seams.
func (m *runMode) newPermissionsPanel() *permissionspanel.Panel {
	return permissionspanel.New(permissionspanel.Seams{
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// permissionsPanel returns the open /permissions panel.
//
// The panel is built when the /permissions panel opens — the host's
// slashPanel struct is reset whenever a panel closes — so the accessor
// is here so that a caller which finds the panel missing still gets one
// that answers.
func (m *runMode) permissionsPanel() *permissionspanel.Panel {
	if m.panel.perm == nil {
		m.panel.perm = m.newPermissionsPanel()
	}
	return m.panel.perm
}
