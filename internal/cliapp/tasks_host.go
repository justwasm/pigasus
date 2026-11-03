// tasks_host.go — the host's side of the /tasks panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/taskspanel: it owns the task
// list, the cursor, the two detail views and their scroll window, the
// keys and the rows. The host supplies the four things the panel cannot
// answer itself — the registry the rows come from, the kill, the
// terminal's size and the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the cursor, of the
// detail view or of the scroll window is never shown.
//
// The status row's "· N tasks" segment and the registry subscription
// stay in the host: they belong to the session, not to the panel.

package cliapp

import (
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
)

// newTasksPanel builds the /tasks panel with the host's seams.
func (m *runMode) newTasksPanel() *taskspanel.Panel {
	return taskspanel.New(taskspanel.Seams{
		// The registry is the host's: the rows are read fresh on every
		// draw, so a task that started or finished while the panel is
		// open shows up without the panel being rebuilt.
		Registry: func() *taskregistry.Registry {
			return m.taskReg
		},
		// The kill belongs to the host: it owns the subagent manager the
		// registry's tasks are attached to.
		Kill: m.killSelectedTask,
		// The panel's rows are laid out for the terminal's size; before
		// the first size message the panel falls back to the capture's
		// height.
		Size: func() (int, int) {
			return m.width, m.height
		},
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// tasksPanel returns the /tasks panel, building it if the host has not
// opened one yet.
func (m *runMode) tasksPanel() *taskspanel.Panel {
	if m.panel.tp == nil {
		m.panel.tp = m.newTasksPanel()
	}
	return m.panel.tp
}
