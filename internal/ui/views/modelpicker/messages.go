package modelpicker

import tea "charm.land/bubbletea/v2"

// ModelSelectedMsg is emitted when a model is selected by the user.
type ModelSelectedMsg struct {
	Label string
	Slug  string
}

// DismissedMsg is emitted when the model picker is closed or cancelled.
type DismissedMsg struct{}

// SelectModelCmd wraps ModelSelectedMsg into a tea.Cmd.
func SelectModelCmd(label, slug string) tea.Cmd {
	return func() tea.Msg {
		return ModelSelectedMsg{Label: label, Slug: slug}
	}
}

// DismissCmd wraps DismissedMsg into a tea.Cmd.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
