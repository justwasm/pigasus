package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
)

// FeedbackHost defines the host operations needed by the /feedback panel.
type FeedbackHost interface {
	SubmitFeedback()
	ClosePanel() tea.Cmd
}

// NewFeedbackPanel builds the /feedback panel with the given host seams.
func NewFeedbackPanel(h FeedbackHost) *feedbackpanel.Panel {
	return feedbackpanel.New(feedbackpanel.Seams{
		Submit: h.SubmitFeedback,
		Close:  h.ClosePanel,
	})
}
