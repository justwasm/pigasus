package taskspanel

import (
	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// TaskKilledMsg is emitted when a task is killed.
type TaskKilledMsg struct {
	Task *taskregistry.Task
}

// TaskRemovedMsg is emitted when a task is removed/hidden from the list.
type TaskRemovedMsg struct {
	Task *taskregistry.Task
}

// DetailOpenedMsg is emitted when the detail view is opened for a task.
type DetailOpenedMsg struct {
	Task *taskregistry.Task
}

// DismissedMsg is emitted when the tasks panel is closed.
type DismissedMsg struct{}

// KillTaskCmd wraps TaskKilledMsg into a tea.Cmd.
func KillTaskCmd(t *taskregistry.Task) tea.Cmd {
	return func() tea.Msg {
		return TaskKilledMsg{Task: t}
	}
}

// RemoveTaskCmd wraps TaskRemovedMsg into a tea.Cmd.
func RemoveTaskCmd(t *taskregistry.Task) tea.Cmd {
	return func() tea.Msg {
		return TaskRemovedMsg{Task: t}
	}
}

// OpenDetailCmd wraps DetailOpenedMsg into a tea.Cmd.
func OpenDetailCmd(t *taskregistry.Task) tea.Cmd {
	return func() tea.Msg {
		return DetailOpenedMsg{Task: t}
	}
}

// DismissCmd wraps DismissedMsg into a tea.Cmd.
func DismissCmd() tea.Cmd {
	return func() tea.Msg {
		return DismissedMsg{}
	}
}
