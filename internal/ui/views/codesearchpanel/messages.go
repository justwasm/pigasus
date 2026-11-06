package codesearchpanel

import tea "charm.land/bubbletea/v2"

// OpenEditorMsg is emitted when a search result should be opened in the external editor.
type OpenEditorMsg struct {
	Path string
	Line int
}

// SendCommentsMsg is emitted when the user submits their unsent comments to the agent.
type SendCommentsMsg struct {
	Text string
}

// DismissedMsg is emitted when the codesearch panel is closed.
type DismissedMsg struct{}

// FileOpenedMsg is emitted when a result is opened in the inline file viewer.
type FileOpenedMsg struct {
	Path string
	Line int
}

// OpenEditorCmd wraps OpenEditorMsg into a tea.Cmd.
func OpenEditorCmd(path string, line int) tea.Cmd {
	return func() tea.Msg {
		return OpenEditorMsg{Path: path, Line: line}
	}
}

// SendCommentsCmd wraps SendCommentsMsg into a tea.Cmd.
func SendCommentsCmd(text string) tea.Cmd {
	return func() tea.Msg {
		return SendCommentsMsg{Text: text}
	}
}

// DismissCmd wraps DismissedMsg into a tea.Cmd.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}

// OpenFileCmd wraps FileOpenedMsg into a tea.Cmd.
func OpenFileCmd(path string, line int) tea.Cmd {
	return func() tea.Msg {
		return FileOpenedMsg{Path: path, Line: line}
	}
}
