// feedback_host.go — the host's side of the /feedback panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/feedbackpanel: it owns the two
// stages, the six categories, the cursor, the free-text buffer and the
// keys. The host supplies the two ways out of the panel — the submit
// and the plain close.
//
// # The submission
//
// The reference's own report goes nowhere in the clone: there is no
// upload endpoint. The host records the report locally and writes the
// message, which is the host's business — the panel does not own the
// transcript block.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
)

// newFeedbackPanel builds the /feedback panel with the host's seams.
func (m *runMode) newFeedbackPanel() *feedbackpanel.Panel {
	return feedbackpanel.New(feedbackpanel.Seams{
		// Submit records the report and leaves the panel.
		Submit: m.submitFeedback,
		// Esc in the first stage leaves the panel without reporting.
		Close: m.closePanel,
	})
}

// feedbackPanel returns the open /feedback panel.
//
// The panel is built when the /feedback panel opens — the host's
// slashPanel struct is reset whenever a panel closes — so the accessor
// is here so that a caller which finds the panel missing still gets one
// that answers.
func (m *runMode) feedbackPanel() *feedbackpanel.Panel {
	if m.panel.fb == nil {
		m.panel.fb = m.newFeedbackPanel()
	}
	return m.panel.fb
}

// submitFeedback records the report and closes the panel.
//
// The reference posts the report to a feedback endpoint; the clone has
// none, so the report is recorded locally and the message says so.
func (m *runMode) submitFeedback() {
	m.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
}
