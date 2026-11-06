package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
)

// EffortHost defines the host operations needed by the /effort panel.
type EffortHost interface {
	EffortTiers() []string
	CurrentEffortName() string
	ApplyEffortSelection(level int) tea.Cmd
	ClosePanel() tea.Cmd
}

// NewEffortPanel builds the /effort panel with the given host seams.
func NewEffortPanel(h EffortHost) *effortpanel.Panel {
	return effortpanel.New(effortpanel.Seams{
		Tiers:   h.EffortTiers,
		Current: h.CurrentEffortName,
		Apply:   h.ApplyEffortSelection,
		Close:   h.ClosePanel,
	})
}
