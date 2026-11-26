package tui

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Registry notifications are not timer ticks: otherwise every notification
// becomes an independent, self-renewing 100 ms redraw chain.
type tasksChangedMsg struct{}
type tasksTickMsg struct{ generation uint64 }

func isTasksRefreshMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case tasksChangedMsg, tasksTickMsg:
		return true
	}
	return false
}

// sendTasksChanged runs on the registry subscriber. Coalesce wakeups, not the
// dirty state: a change racing with Update is either consumed there or wakes
// the program again. No timer/model state is accessed from this goroutine.
func (m *RootModel) sendTasksChanged() {
	if m.turn.prog == nil || !m.tasks.queueWake() {
		return
	}
	m.turn.prog.Send(tasksChangedMsg{})
}

func (t *TasksModel) queueWake() bool {
	return atomic.CompareAndSwapInt32(&t.wakePending, 0, 1)
}

// ensureTasksTick is called only by Init/Update. All entry points share one
// outstanding timer. A generation also makes late/duplicate ticks harmless.
func (m *RootModel) ensureTasksTick() tea.Cmd {
	if m.tasks.tickArmed {
		return nil
	}
	m.tasks.tickArmed = true
	m.tasks.tickGeneration++
	generation := m.tasks.tickGeneration
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return tasksTickMsg{generation: generation}
	})
}
