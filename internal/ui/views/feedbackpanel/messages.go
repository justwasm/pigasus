package feedbackpanel

import tea "charm.land/bubbletea/v2"

// SubmittedMsg is emitted when a feedback category and text are submitted.
type SubmittedMsg struct {
	Category string
	Text     string
}

// DismissedMsg is emitted when the feedback panel is closed/dismissed.
type DismissedMsg struct{}

// SubmitCmd creates a tea.Cmd that emits SubmittedMsg.
func SubmitCmd(category, text string) tea.Cmd {
	return func() tea.Msg {
		return SubmittedMsg{
			Category: category,
			Text:     text,
		}
	}
}

// DismissCmd creates a tea.Cmd that emits DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
