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
// Layout (verified against REF v1.4.0 220×50):
//
//	Tasks
//	⎿  ⠿ running  sub-12345…  bash -c sleep 60                   2s
//	⎿  ⠿ running  sub-67890…  bash -c ls -la /tmp | head -50    0s
//	⎿  ⠿ done     sub-11111…  bash -c echo done                 12s
//
//	Keyboard: ↑/↓ Navigate  enter View output  k Kill Task  x Remove Task From List
//
// When the registry is empty the panel renders the cycle-205 empty
// form ("No background tasks." in dim gray) so the visual contract is
// identical to the cycle-205 capture byte-for-byte.
//
// State glyphs:
//
//	⠿   running
//	⠿   pending
//	⠿   done
//	⠿   failed  (red — cToolRemoveOpen)
//	⠿   killed  (orange — cWarningOpen)
//
// The bullets use the same Braille character (U+283F "⠿") that the
// transcript tool-step renderer uses for the leading ⏺ step marker
// (cycle 127's bullet char), so the panel reads "in family" with the
// transcript when the user scrolls. We pick the Braille glyph rather
// than a circle because a circle is already the *tool step* marker
// in the transcript — using a circle here would confuse users into
// thinking the panel itself was a tool call.
package main

import (
	"fmt"
	"strings"
	"sync"
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
// takes the *first* 18 (or however many fit given panel.height).
const tasksPanelLimit = 20

// tasksPanelRows holds the cached row list so a state-change event
// that fires while the panel is closed does not waste work. The
// cache is rebuilt by panelTasksLines() on every render; a stale
// cache (e.g. when no events have arrived yet) is acceptable
// because the panel re-reads from the registry every render
// anyway.
type tasksPanelRows struct {
	mu       sync.Mutex
	tasks    []*taskregistry.Task
	updated  time.Time
	filtered int // number of tasks filtered out for the panel cap
}

func newTasksPanelRows() *tasksPanelRows {
	return &tasksPanelRows{}
}

// snapshot returns a copy of the cached row list. The slice header
// is shared but the contents are not; callers must treat the
// elements as read-only.
func (r *tasksPanelRows) snapshot() []*taskregistry.Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*taskregistry.Task, len(r.tasks))
	copy(out, r.tasks)
	return out
}

// rebuild re-reads the registry and updates the cache. Called by
// panelTasksLines() at the top of every render.
func (r *tasksPanelRows) rebuild(reg *taskregistry.Registry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := reg.List()
	if len(all) > tasksPanelLimit {
		r.filtered = len(all) - tasksPanelLimit
		all = all[len(all)-tasksPanelLimit:]
	} else {
		r.filtered = 0
	}
	r.tasks = all
	r.updated = time.Now()
}

// panelTasksLines renders the /tasks panel body. It is the
// panelKind == panelTasks branch of panelLines(). The function
// takes the runMode because it needs to read m.taskReg and (when
// the panel is up) m.panel.cursor — both are runMode fields.
func (m *runMode) panelTasksLines() []string {
	rows := newTasksPanelRows()
	if m.taskReg != nil {
		rows.rebuild(m.taskReg)
	}
	lines := []string{
		panelTitleOpen + "Tasks" + panelTitleClose,
	}
	tasks := rows.snapshot()
	if len(tasks) == 0 {
		lines = append(lines,
			"  "+cDimGrayOpen+"No background tasks."+cDimGrayClose)
	} else {
		// Each row: ⎿  ⠿ <state>  <name>  <duration>
		// The "⎿" prefix is the cycle-205 brand blue bullet,
		// the state glyph and the row text are in the runMode's
		// recovered palette (cycle 126). Rows above the cursor
		// are styled like idle rows; the cursor row is bold so
		// the user can see the focus even on the 220×30 panel
		// where rows are tightly packed.
		for i, t := range tasks {
			lines = append(lines, tasksRowLine(t, i == m.panel.cursor))
		}
		if rows.filtered > 0 {
			lines = append(lines,
				"  "+cDimGrayOpen+fmt.Sprintf("(%d older hidden — ref to see all)", rows.filtered)+cDimGrayClose)
		}
	}
	lines = append(lines,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "←/→", "Page", "enter", "View output",
			"k", "Kill Task", "x", "Remove Task From List"))
	return lines
}

// tasksRowLine renders a single task row. cursor is true when this
// row is the one under m.panel.cursor; the cursor row gets the bold
// wrapper so the panel focus is unambiguous.
func tasksRowLine(t *taskregistry.Task, cursor bool) string {
	stateStr, color := tasksStateStyle(t.State)
	glyph := "⠿" // U+283F Braille pattern dots-12345678
	elapsed := taskElapsed(t)
	// Keep the row to <= 100 visible runes; truncate the name
	// (the most likely offender) and append "…" if we have to.
	name := t.Name
	if n := runeCount(name); n > 60 {
		name = truncateRunes(name, 57) + "..."
	}
	id := t.ID
	if n := runeCount(id); n > 20 {
		id = truncateRunes(id, 17) + "..."
	}
	row := fmt.Sprintf("  ⎿  %s%s %s%s  %s  %s  %s",
		color, glyph, stateStr, tasksStateClose(t.State),
		cDimGrayOpen, id, cDimGrayClose)
	row += "  " + name + "  " + elapsed
	// AGY 1.3.1: a failed task with an error reason renders
	// "Error: <reason>" instead of the bare "failed" the panel
	// used to show. The trailing reason is what the changelog
	// line "they now show `Error:` with the reason" requires.
	// A task with no captured error keeps the bare "failed"
	// label so the visual layout does not shift.
	if t.State == taskregistry.StateFailed && t.Err != nil {
		reason := t.Err.Error()
		// Truncate the reason to keep the row under the
		// 100-rune budget; "…" marks the cut.
		if n := runeCount(reason); n > 40 {
			reason = truncateRunes(reason, 37) + "..."
		}
		row += "  " + color + "Error: " + reason + tasksStateClose(t.State)
	}
	if cursor {
		// Bold the whole row when it's the cursor focus. The
		// bold open goes at the start so the closing dim-gray
		// escapes still take effect; the panel-content reader
		// sees the row as bold-blue-on-default.
		row = "\033[1m" + row + "\033[22m"
	}
	return row
}

// tasksStateStyle picks the glyph's foreground colour and the
// state label string for each State value. We do not use Unicode
// state glyphs (⏵ ⏸ ✓ ✕) because the reference itself uses the
// plain text "running"/"done"/etc. — see the panelKeyboardHint
// column widths above and the changelog line about "shown as
// `completed` or `failed` without a made-up exit code".
func tasksStateStyle(s taskregistry.State) (label, color string) {
	switch s {
	case taskregistry.StateRunning:
		return "running ", cBlueOpen
	case taskregistry.StatePending:
		return "pending ", cBlueOpen
	case taskregistry.StateDone:
		return "done    ", cToolAddOpen
	case taskregistry.StateFailed:
		return "failed  ", cToolRemoveOpen
	case taskregistry.StateKilled:
		return "killed  ", cWarningOpen
	default:
		// Unknown / future state — fall back to dim gray so
		// the panel does not crash on a state we haven't seen.
		return string(s) + " ", cDimGrayOpen
	}
}

// tasksStateClose returns the SGR close for the chosen colour.
// Each branch matches tasksStateStyle's color; an unknown state
// falls back to the default-close so a stray "x" escape does not
// leak.
func tasksStateClose(s taskregistry.State) string {
	switch s {
	case taskregistry.StateRunning, taskregistry.StatePending:
		return "\033[39m"
	case taskregistry.StateDone:
		return "\033[39m"
	case taskregistry.StateFailed:
		return "\033[39m"
	case taskregistry.StateKilled:
		return "\033[39m"
	default:
		return "\033[39m"
	}
}

// taskElapsed returns "2s" / "1m3s" / "1h02m" — the same compact
// form the transcript's spinner uses (cycle 76). A zero time (the
// task was registered but never started) renders "—".
func taskElapsed(t *taskregistry.Task) string {
	if t.StartedAt.IsZero() {
		return "—"
	}
	end := t.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	d := end.Sub(t.StartedAt)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// runeCount is utf8.RuneCountInString inlined to avoid the import.
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// truncateRunes returns the first n runes of s, never splitting a
// rune. Used by the row renderer to cap long names / IDs.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// tasksPanelKey handles Enter / k / x while the panel is open.
// Esc, ↑/↓ and the rest are owned by listPanelOwningKey (cycle
// 206), so this only sees the three action keys. Returns
// (handled, cmd) per the handlePanelKey contract.
//
//	enter on a running row → SIGTERM-equivalent via subagent.Kill,
//	                         registry will emit "updated" → killed
//	enter on a done row    → expand the row (cycle 226's /diff
//	                         viewer pattern — TBD in cycle 230)
//	k on a running row      → same as Enter
//	k on a non-running row  → no-op (no made-up exit code per
//	                         the 1.4.0 changelog line)
//	x on any row            → Hide(id) — list refreshes on the
//	                         next subscriber event
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
	case tea.KeyEnter, tea.KeySpace:
		// Enter: kill if running, otherwise expand (the expand
		// half is a TODO — cycle 230 wires the per-row detail
		// viewer, mirroring cycle 226's /diff subview).
		if t.State == taskregistry.StateRunning || t.State == taskregistry.StatePending {
			m.killSelectedTask(t)
		}
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
	n := m.taskReg.CountRunning()
	if n == 0 {
		return ""
	}
	if n == 1 {
		return " · 1 task"
	}
	return fmt.Sprintf(" · %d tasks", n)
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
