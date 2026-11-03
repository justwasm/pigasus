// slash_tasks_panel.go — cycle tasks-panel: the /tasks panel's host
// side — the status-line "· N tasks" segment, the registry
// subscription and the kill the panel asks for.
//
// The panel itself lives in internal/ui/views/taskspanel: it owns the
// rows, the cursor, the two detail views and their scroll window, the
// keys and the row renderer. What is left here is what the *session*
// owns: the registry subscription that makes the status row tick, the
// dirty flag that redraws it, and the kill — which needs the session's
// subagent manager.
//
// The status row is rendered by statusRow() (the right-aligned segment
// after the effort suffix). It consumes the same registry subscriber
// channel as the panel, so a background shell that finishes while the
// user is reading the panel re-renders within a frame.
//
// Layout (the row is the reference's own — recovered from
// model.TasksModel.View (0x102bed580) in the 1.4.7 binary; see
// docs/cycles/cycle-319-tui-tasks-row-layout/deliverable.md):
//
//	Tasks
//	> ● [15:04:05] bash -c sleep 60                running
//	  ● [15:04:05] bash -c ls -la /tmp | head -50    running
//	  ● [15:04:05] bash -c echo done               completed
//
// The row is "<prefix><marker> [<time>] <name><padding><status>": the
// prefix is "> " on the cursor's row and "  " on the others, the
// marker is the reference's "●" in the state's colour, the time cell is
// the task's start time in brackets, and the padding pushes the status
// cell to the right edge of the panel.
//
//	Keyboard: ↑/↓ Navigate  ←/→ Page  enter View output  k Kill Task  x Remove Task From List
//
// State glyphs:
//
//	●   running
//	●   completed
//	●   failed  (red — cToolRemoveOpen)
//	●   cancelled  (orange — cWarningOpen)
//
// The marker is the reference's own — render.StatusMarker (0x1028c8040)
// returns the filled circle "●" in the state's colour, the same glyph
// for every state. The panel reads "in family" with the transcript,
// whose tool-step rows carry the same circle.
package cliapp

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// killSelectedTask looks up the underlying subagent.Manager for
// the registry entry and calls Subagent.Kill on it. The Kill
// fires the StateChangeFunc we wired in AttachSubagent, which
// pushes the "killed" transition back into the registry, which
// in turn fires the subscriber channel that statusRow watches.
// One keystroke, three lock acquisitions, no busy loop.
//
// The kill belongs to the host: the /tasks panel asks for it through a
// seam, because the manager the registry's tasks are attached to is the
// session's.
func (m *runMode) killSelectedTask(t *taskregistry.Task) {
	if t == nil {
		return
	}
	mgr := m.taskSubagentManager
	if mgr == nil {
		mgr = subagent.NewManager() // no-op fallback; never happens in production
	}
	taskregistry.KillByID(m.taskReg, t.ID, mgr)
}

// openTasksDetail opens the /tasks panel navigated directly to the detail view of the task.
func (m *runMode) openTasksDetail(id string, isSubagent bool) {
	m.openSlashPanel("tasks")
	if m.panel.tp != nil {
		m.panel.tp.OpenTaskDetail(id, isSubagent)
	}
	m.activeItems.SetFocus(false)
}

// ---- status-row "· N tasks" segment -------------------------------------

// tasksStatusSuffix returns the right-side "· N tasks" segment.
// It returns "" when N == 0 so the segment disappears cleanly.
// The segment is rendered with the same dim gray as the effort
// suffix, separated by a single " · " so the row reads:
//
//	? for shortcuts         Gemini 3.8 Flash · high · 2 tasks
//
// on a model that has an effort tier, and:
//
//	? for shortcuts         MiniMax M3 · 2 tasks
//
// on a model that does not (no "· high" suffix). The effort and
// tasks suffixes are concatenated by statusRow() — see the
// callers in tui_run.go.
func (m *runMode) tasksStatusSuffix() string {
	taskPart := ""
	if m.taskReg != nil {
		taskPart = taskregistry.StatusSuffix(m.taskReg.CountRunning())
	}
	planPart := m.enginePlan()
	if planPart != "" {
		if taskPart != "" {
			return taskPart + " · " + planPart
		}
		return " · " + planPart
	}
	return taskPart
}

// Subscribe returns true if the subscriber event for task
// transitions should refresh the status row + panel. Wired in
// tui_run.go's newRunMode (and again whenever the panel is
// re-opened, so a late subscriber does not miss events that
// happened while the panel was closed).
//
// The function is idempotent: calling it twice replaces the
// previous subscription.
func (m *runMode) startTasksSubscription() {
	if m.taskReg == nil || m.tasksSubscribed {
		return
	}
	ch, cancel := m.taskReg.Subscribe()
	m.tasksSubCancel = cancel
	m.tasksSubscribed = true
	go func() {
		for ev := range ch {
			_ = ev // we don't need the event payload; List()/CountRunning() are re-read on tick
			atomic.StoreInt32(&m.tasksDirty, 1)
		}
	}()
}

// consumeTasksDirty atomically reads-and-clears the dirty flag.
// Returns true if the flag was set; the caller must re-emit a
// Tick so the chain self-tunes.
func (m *runMode) consumeTasksDirty() bool {
	return atomic.SwapInt32(&m.tasksDirty, 0) == 1
}

// tasksTickMsg is delivered on every state-change event the
// registry emits. The handler in tui_run.go bumps a dirty flag
// and returns a Tick() command so the bubbletea runtime re-renders
// the View(). We do NOT push the raw *Task through the message
// channel because bubbletea serialises every Update; copying the
// pointer is unnecessary because the panel re-reads List() on
// every render.
type tasksTickMsg struct{}

// armTasksTick returns the tea.Cmd that fires a tasksTickMsg on
// the next 100 ms tick. It is the analog of armUsageTick in the
// /usage panel.
func armTasksTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tasksTickMsg{} })
}
