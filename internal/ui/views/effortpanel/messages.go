package effortpanel

import tea "charm.land/bubbletea/v2"

// AppliedMsg is emitted when the user selects and applies an effort level.
type AppliedMsg struct {
	Level int
	Tier  string
}

// DismissedMsg is emitted when the user cancels or dismisses the effort panel.
type DismissedMsg struct{}

// ApplyCmd creates a tea.Cmd that emits an AppliedMsg.
func ApplyCmd(level int, tier string) tea.Cmd {
	return func() tea.Msg {
		return AppliedMsg{
			Level: level,
			Tier:  tier,
		}
	}
}

// DismissCmd creates a tea.Cmd that emits a DismissedMsg.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
