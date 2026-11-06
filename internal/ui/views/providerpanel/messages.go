package providerpanel

import tea "charm.land/bubbletea/v2"

// DismissedMsg is emitted when the provider panel is closed.
type DismissedMsg struct{}

// DismissCmd creates a tea.Cmd that emits DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
