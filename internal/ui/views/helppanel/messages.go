package helppanel

import tea "charm.land/bubbletea/v2"

// CommandSelectedMsg is emitted by the overlay when a user selects a slash command.
type CommandSelectedMsg struct {
	Command string
}

// DismissedMsg is emitted when the overlay is closed or cancelled.
type DismissedMsg struct {
	EchoLine bool // true if the dismissal should echo "/help: Exited /help command"
}

// SelectCommandCmd wraps CommandSelectedMsg into a tea.Cmd.
func SelectCommandCmd(cmd string) tea.Cmd {
	return func() tea.Msg {
		return CommandSelectedMsg{Command: cmd}
	}
}

// DismissCmd wraps DismissedMsg into a tea.Cmd.
func DismissCmd(echoLine bool) tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{EchoLine: echoLine}
	}
}
