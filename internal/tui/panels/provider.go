package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
)

// ProviderHost defines the host operations needed by the /provider panel.
type ProviderHost interface {
	OpenBrowser(url string) error
	ClipboardRead() string
	ClipboardWrite(text string) error
	CurrentModelLabel() string
	ClosePanel() tea.Cmd
}

// NewProviderPanel builds the /provider panel with the given host seams.
func NewProviderPanel(h ProviderHost) *providerpanel.Panel {
	return providerpanel.New(providerpanel.Seams{
		OpenBrowser:    h.OpenBrowser,
		ClipboardRead:  h.ClipboardRead,
		ClipboardWrite: h.ClipboardWrite,
		CurrentModel:   h.CurrentModelLabel,
		Close:          h.ClosePanel,
	})
}
