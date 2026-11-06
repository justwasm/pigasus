package panels

import (
	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
)

// TasksHost defines the host operations needed by the /tasks panel.
type TasksHost interface {
	TaskRegistry() *taskregistry.Registry
	KillSelectedTask(t *taskregistry.Task)
	TerminalSize() (width int, height int)
	ClosePanel() tea.Cmd
}

// NewTasksPanel builds the /tasks panel with the given host seams.
func NewTasksPanel(h TasksHost) *taskspanel.Panel {
	return taskspanel.New(taskspanel.Seams{
		Registry: h.TaskRegistry,
		Kill:     h.KillSelectedTask,
		Size:     h.TerminalSize,
		Close:    h.ClosePanel,
	})
}
