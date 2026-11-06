package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
)

// DiffHost defines the host operations needed by the /diff panel.
type DiffHost interface {
	SessionCwd() string
	TerminalSize() (width int, height int)
	DiffTurnCount() int
	VimEnabled() bool
	ClosePanel() tea.Cmd
}

// NewDiffPanel builds the /diff panel with the given host seams.
func NewDiffPanel(h DiffHost) *diffpanel.Panel {
	return diffpanel.New(diffpanel.Seams{
		Cwd:        h.SessionCwd,
		Size:       h.TerminalSize,
		Turns:      h.DiffTurnCount,
		VimEnabled: h.VimEnabled,
		Close:      h.ClosePanel,
	})
}
