package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
)

// HooksHost defines the host operations needed by the /hooks panel.
type HooksHost interface {
	ClosePanel() tea.Cmd
}

// NewHooksPanel builds the /hooks panel with the given host seams.
func NewHooksPanel(h HooksHost) *hookspanel.Panel {
	return hookspanel.New(hookspanel.Seams{
		Close: h.ClosePanel,
	})
}
