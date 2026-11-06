package rewindpanel

import tea "charm.land/bubbletea/v2"

// RewoundMsg is emitted when a user turn is selected for rewinding.
type RewoundMsg struct {
	Index int
}

// DismissedMsg is emitted when the rewind picker is dismissed.
type DismissedMsg struct{}

// RewindCmd creates a tea.Cmd that emits RewoundMsg.
func RewindCmd(index int) tea.Cmd {
	return func() tea.Msg {
		return RewoundMsg{Index: index}
	}
}

// DismissCmd creates a tea.Cmd that emits DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
