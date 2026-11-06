package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
)

// RenameHost defines the host operations needed by the /rename panel.
type RenameHost interface {
	ConversationName() string
	SubmitRename(name string) tea.Cmd
	ClosePanel() tea.Cmd
}

// NewRenamePanel builds the /rename panel with the given host seams.
func NewRenamePanel(h RenameHost) *renamepanel.Panel {
	return renamepanel.New(renamepanel.Seams{
		Seed:   h.ConversationName,
		Submit: h.SubmitRename,
		Close:  h.ClosePanel,
	})
}
