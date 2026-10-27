// slash_tasks_panel.go — cycle tasks-panel: live /tasks panel +
// status-line "· N tasks" segment.
//
// The panel is rendered by panelLines() (panelTasks branch) and is
// fed by the runMode's task registry (m.taskReg). The status row is
// rendered by statusRow() (the right-aligned segment after the
// effort suffix). Both consume the same registry subscriber channel
// so a background shell that finishes while the user is reading the
// panel re-renders within a frame.
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
// marker is the reference's "●" in the state's colour, the time cell
// is the task's start time in brackets, and the padding pushes the
// status cell to the right edge of the panel.
//
//	Keyboard: ↑/↓ Navigate  ←/→ Page  enter View output  k Kill Task  x Remove Task From List
//
// When the registry is empty the panel renders the cycle-205 empty
// form ("No background tasks." in dim gray) so the visual contract is
// identical to the cycle-205 capture byte-for-byte.
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
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// tasksPanelLimit caps the panel to the most recent 20 rows so a
// long-running session does not scroll the prompt off the screen.
// The changelog line about "large background tasks crowding out the
// prompt" is the source of this number — REF caps at 20 too, but
// the actual limit the 220×50 panel fits is closer to 18. The 20
// number is what the underlying List() returns; panelLines() then
// tasksPanelLimit caps the panel to the most recent 20 rows.
const tasksPanelLimit = taskregistry.TasksPanelLimit

// tasksPanelRows holds the cached row list for the /tasks panel.
type tasksPanelRows = taskregistry.PanelRows

func newTasksPanelRows() *tasksPanelRows {
	return taskregistry.NewPanelRows()
}

// panelTasksLines renders the /tasks panel body. It is the
// panelKind == panelTasks branch of panelLines(). The function
// takes the runMode because it needs to read m.taskReg and (when
// the panel is up) m.panel.cursor — both are runMode fields.
func (m *runMode) panelTasksLines() []string {
	rows := newTasksPanelRows()
	if m.taskReg != nil {
		rows.Rebuild(m.taskReg)
	}
	lines := []string{
		panelTitleOpen + "Tasks" + panelTitleClose,
	}
	tasks := rows.Snapshot()
	if len(tasks) == 0 {
		lines = append(lines,
			"  "+cDimGrayOpen+"No background tasks."+cDimGrayClose)
	} else {
		// Each row is the reference's own: "<prefix><marker> [<time>]
		// <name><padding><status>". The prefix marks the cursor's
		// row, the marker is the state's colour, and the padding
		// pushes the status cell to the right edge of the panel.
		for i, t := range tasks {
			lines = append(lines, tasksRowLine(t, i == m.panel.cursor, m.width))
		}
		if rows.Filtered() > 0 {
			lines = append(lines,
				"  "+cDimGrayOpen+fmt.Sprintf("(%d older hidden — ref to see all)", rows.Filtered())+cDimGrayClose)
		}
	}
	lines = append(lines,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "←/→", "Page", "enter", "View output",
			"k", "Kill Task", "x", "Remove Task From List"))
	return lines
}

// tasksRowLine renders a single task row. Delegated to taskregistry.RowLine.
// The width is the panel's width — the reference's row pads the name so
// that the status cell lands on the right edge of it.
func tasksRowLine(t *taskregistry.Task, cursor bool, width int) string {
	return taskregistry.RowLine(t, cursor, width)
}

// tasksPanelKey handles Enter / k / x while the panel is open.
// Esc, ↑/↓ and the rest are owned by listPanelOwningKey (cycle
// 206), so this only sees the three action keys. Returns
// (handled, cmd) per the handlePanelKey contract.
//
//	enter on any row      → open the row's detail view (cycle 315 —
//	                         the task detail for a background task,
//	                         the subagent detail for an agent task)
//	k on a running row      → kill it (the reference's KeyTaskKill
//	                         binding — "k Kill Task" in the legend)
//	k on a non-running row  → no-op (no made-up exit code per
//	                         the 1.4.0 changelog line)
//	x on any row            → Hide(id) — list refreshes on the
//	                         next subscriber event
//
// 【反编译】 the reference's /tasks scope binds KeyEnter, KeyTaskKill
// and KeyTaskRemove. Enter opens the detail view; it does not kill
// (the clone used to kill on Enter — that was the cycle-315 bug).
func (m *runMode) tasksPanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.taskReg == nil {
		return false, nil
	}
	tasks := m.taskReg.List()
	if len(tasks) == 0 {
		// Empty registry: let the generic list-panel handler own
		// Esc so the panel can be dismissed. Returning true for
		// every key (the previous behaviour) made an empty /tasks
		// panel permanently modal — the reference closes it with
		// Esc and writes `Exited /tasks command`.
		return false, nil
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(tasks) {
		cursor = 0
	}
	t := tasks[cursor]
	switch key.Code {
	case tea.KeyEnter:
		// Cycle 315: Enter opens the row's detail view. The panel
		// kind decides which one — the task detail for a background
		// task, the subagent detail for an agent (subagent) task.
		m.openTaskDetail(t)
		return true, nil
	case tea.KeySpace:
		// The reference binds only KeyEnter in the /tasks scope. A
		// space is swallowed here so it never reaches the prompt.
		return true, nil
	case 'k':
		if t.State == taskregistry.StateRunning || t.State == taskregistry.StatePending {
			m.killSelectedTask(t)
		}
		return true, nil
	case 'x':
		m.taskReg.Hide(t.ID)
		// Reset the cursor if we just hid the last row so the
		// panel does not show "selected: out of range".
		if m.panel.cursor >= len(m.taskReg.List()) && m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	}
	return false, nil
}

// killSelectedTask looks up the underlying subagent.Manager for
// the registry entry and calls Subagent.Kill on it. The Kill
// fires the StateChangeFunc we wired in AttachSubagent, which
// pushes the "killed" transition back into the registry, which
// in turn fires the subscriber channel that statusRow watches.
// One keystroke, three lock acquisitions, no busy loop.
func (m *runMode) killSelectedTask(t *taskregistry.Task) {
	if t == nil {
		return
	}
	mgr := m.taskSubagentManager
	if mgr == nil {
		mgr = subagent.NewManager() // no-op fallback; never happens in production
	}
	// Default to the global manager used by internal/tools so
	// create_task-spawned shells are killable from the panel.
	if globalMgr := subagentmgrForRegistry(m.taskReg); globalMgr != nil {
		mgr = globalMgr
	}
	taskregistry.KillByID(m.taskReg, t.ID, mgr)
}

// subagentmgrForRegistry returns the package-level subagent
// manager that holds the Subagent for the task with the given
// ID. The lookup is best-effort: if the Subagent was registered
// with a per-call manager (not the global), we cannot find it
// here. The /tasks panel will still render the row but Enter
// will be a no-op.
//
// The lookup is indirect through tools.DefaultTaskSubagentManager
// so cmd/wagyu does not have to import internal/tools — it only
// knows about the registry and the manager.
func subagentmgrForRegistry(reg *taskregistry.Registry) *subagent.Manager {
	if reg == nil {
		return nil
	}
	// No fast path; the registry does not own the Manager. The
	// default manager used by internal/tools' create_task is
	// tools.DefaultSubagentManager (set by init in stubs.go).
	// We expose it via the package-level getter below.
	return defaultTaskSubagentManager
}

// defaultTaskSubagentManager is the package-level reference to
// the tools package's default subagent manager. It is wired by
// wireDefaultSubagentManager at init() time so the runMode can
// find Subagents from /tasks without importing internal/tools
// (which would create an import cycle through agent.Loop).
//
// Setter is exported so a future test can swap it for a fresh
// manager between cases without resetting the whole TUI.
var defaultTaskSubagentManager *subagent.Manager

// SetDefaultTaskSubagentManager records the manager the /tasks
// panel should consult when killing a running row. The cmd/wagyu
// main package calls this once at startup with the manager from
// internal/tools/stubs.go.
func SetDefaultTaskSubagentManager(m *subagent.Manager) {
	defaultTaskSubagentManager = m
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
	if m.taskReg == nil {
		return ""
	}
	return taskregistry.StatusSuffix(m.taskReg.CountRunning())
}

// Subscribe returns true if the subscriber event for task
// transitions should refresh the status row + panel. Wired in
// tui_run.go's NewRunMode (and again whenever the panel is
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

// MarkTaskRegDirty forces a redraw on the next tick. Used by
// tui_run.go's tick handler — every 100 ms the runMode wakes,
// sees m.tasksDirty, and re-renders. The 100 ms cadence matches
// the cycle-76 streaming spinner tick so the panel does not
// introduce a new timer.
//
// Atomic — the subscriber goroutine writes this without holding
// the runMode lock, so a non-atomic store trips the race detector.
func (m *runMode) MarkTaskRegDirty() { atomic.StoreInt32(&m.tasksDirty, 1) }

// isTasksDirty reads the atomic dirty flag. The TUI's tick handler
// (Update → tasksTickMsg) calls this and resets it under the
// subscriber goroutine's eventual delivery. Two readers racing is
// safe (the worst case is one extra redraw).
func (m *runMode) isTasksDirty() bool { return atomic.LoadInt32(&m.tasksDirty) == 1 }

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

// ---- import-time helpers ------------------------------------------------

// stubPlaceholder is here to silence "unused import" if the
// package compiles down to nothing in a future refactor.
var _ = strings.TrimSpace
