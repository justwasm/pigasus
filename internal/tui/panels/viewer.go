package panels

import (
	tea "charm.land/bubbletea/v2"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
)

// ViewerHost defines the host operations needed by viewer panels.
type ViewerHost interface {
	ViewerSpec() (tuiviewer.Spec, bool)
	ViewerWidth() int
	ViewerPageRows(spec tuiviewer.Spec) int
	ClosePanel() tea.Cmd
}

// NewViewerPanel builds the viewer panel with the given host seams.
func NewViewerPanel(h ViewerHost) *viewerpanel.Panel {
	return viewerpanel.New(viewerpanel.Seams{
		Spec:     h.ViewerSpec,
		Width:    h.ViewerWidth,
		PageRows: h.ViewerPageRows,
		Close:    h.ClosePanel,
	})
}
