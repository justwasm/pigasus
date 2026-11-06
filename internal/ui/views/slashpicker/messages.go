package slashpicker

import tea "charm.land/bubbletea/v2"

// CompletedMsg is emitted when a command or model completion is confirmed (e.g. via Tab).
type CompletedMsg struct {
	Text      string
	IsModel   bool
	ModelSlug string
}

// DismissedMsg is emitted when the slash picker is cancelled or closed.
type DismissedMsg struct{}

// CompletedCmd creates a tea.Cmd for CompletedMsg.
func CompletedCmd(text string, isModel bool, slug string) tea.Cmd {
	return func() tea.Msg {
		return CompletedMsg{Text: text, IsModel: isModel, ModelSlug: slug}
	}
}

// DismissCmd creates a tea.Cmd for DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
