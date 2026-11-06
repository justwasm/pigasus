package renamepanel

import tea "charm.land/bubbletea/v2"

// RenamedMsg is emitted when a new conversation name is submitted.
type RenamedMsg struct {
	Name string
}

// DismissedMsg is emitted when the rename panel is dismissed without committing.
type DismissedMsg struct{}

// RenameCmd creates a tea.Cmd that emits RenamedMsg.
func RenameCmd(name string) tea.Cmd {
	return func() tea.Msg {
		return RenamedMsg{Name: name}
	}
}

// DismissCmd creates a tea.Cmd that emits DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
