package panels

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
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
