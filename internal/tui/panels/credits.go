package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
)

// CreditsHost defines the host operations needed by the /credits panel.
type CreditsHost interface {
	ClosePanel() tea.Cmd
	OpenURL(url string) error
}

// NewCreditsPanel builds the /credits panel with the given host seams.
func NewCreditsPanel(h CreditsHost) *creditspanel.Panel {
	return creditspanel.New(creditspanel.Seams{
		Close:   h.ClosePanel,
		OpenURL: h.OpenURL,
	})
}
