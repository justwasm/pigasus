package panels

import (
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	tea "charm.land/bubbletea/v2"
)

// ModelHost defines the host operations needed by the /model panel.
type ModelHost interface {
	CurrentModelLabel() string
	ApplyModel(label string)
	ClosePanel() tea.Cmd
	CustomModelRows() []modelpicker.Row
}

// NewModelPanel builds the /model panel with the given host seams.
func NewModelPanel(h ModelHost) *modelpicker.Panel {
	return modelpicker.New(modelpicker.Seams{
		CurrentModelLabel: h.CurrentModelLabel,
		ApplyModel:        h.ApplyModel,
		Close:             h.ClosePanel,
		CustomRows:        h.CustomModelRows,
	})
}
