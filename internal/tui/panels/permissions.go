package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
)

// PermissionsHost defines the host operations needed by the /permissions panel.
type PermissionsHost interface {
	ClosePanel() tea.Cmd
}

// NewPermissionsPanel builds the /permissions panel with the given host seams.
func NewPermissionsPanel(h PermissionsHost) *permissionspanel.Panel {
	return permissionspanel.New(permissionspanel.Seams{
		Close: h.ClosePanel,
	})
}
