package adddirpanel

import tea "charm.land/bubbletea/v2"

// AddedMsg is emitted when a trusted workspace directory path is submitted.
type AddedMsg struct {
	Path string
}

// DismissedMsg is emitted when the add-dir panel is dismissed.
type DismissedMsg struct{}

// AddCmd creates a tea.Cmd that emits AddedMsg.
func AddCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return AddedMsg{Path: path}
	}
}

// DismissCmd creates a tea.Cmd that emits DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
