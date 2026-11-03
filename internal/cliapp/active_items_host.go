// active_items_host.go — the host's side of the active-items block.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/activeitemspanel: it owns the
// cursor, the scroll window, the rows it writes and the keys it
// handles.
//
// The host keeps the facts the panel cannot know — the task registry
// the rows come from, the terminal's size, whether vim mode is on and
// the way into the /tasks detail view — and hands them to the panel as
// seams.
//
// The host's job is to route: it asks the panel for the rows when it
// paints a frame, it asks the panel before it routes a key the panel
// owns, and it asks the panel whether there is anything to focus when
// the prompt's ↓ hands the keyboard over.

package cliapp

import (
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
)

// newActiveItemsPanel builds the active-items block with the host's
// seams.
func (m *runMode) newActiveItemsPanel() *activeitemspanel.Panel {
	return activeitemspanel.New(activeitemspanel.Seams{
		Registry:   func() *taskregistry.Registry { return m.taskReg },
		Width:      func() int { return m.width },
		Height:     func() int { return m.height },
		VimEnabled: func() bool { return m.vimEnabled() },
		OpenDetail: func(id string, isSubagent bool) { m.openTasksDetail(id, isSubagent) },
	})
}
