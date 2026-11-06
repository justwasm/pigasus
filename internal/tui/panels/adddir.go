package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/policy/trust"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
)

// AddDirHost defines the host operations needed by the /add-dir panel.
type AddDirHost interface {
	SubmitAddDir(path string) tea.Cmd
	ClosePanel() tea.Cmd
}

// NewAddDirPanel builds the /add-dir panel with the given host seams.
func NewAddDirPanel(h AddDirHost) *adddirpanel.Panel {
	return adddirpanel.New(adddirpanel.Seams{
		Trusted: trust.LoadTrustedWorkspaces,
		Submit:  h.SubmitAddDir,
		Close:   h.ClosePanel,
	})
}
