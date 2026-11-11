package panels

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
)

// RewindHost defines host seams for the /rewind panel.
type RewindHost interface {
	RewindSteps() []rewind.Step
	TerminalHeight() int
	RewindPickStep(index int) int
	ClosePanel() tea.Cmd
}

// NewRewindPanel builds the /rewind panel with the host's seams.
func NewRewindPanel(h RewindHost) *rewindpanel.Panel {
	return rewindpanel.New(rewindpanel.Seams{
		Steps:  h.RewindSteps,
		Height: h.TerminalHeight,
		Pick:   h.RewindPickStep,
		Close:  h.ClosePanel,
	})
}
