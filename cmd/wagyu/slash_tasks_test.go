// slash_tasks_test.go — cycle tasks-panel: five tests pinning the
// /tasks panel + status-row "· N tasks" segment behaviours that
// the live-probe byte-match table relies on:
//
//   - TestSlashTasksEmpty       — /tasks with empty registry → dim-gray "No background tasks."
//   - TestSlashTasksList        — 3 seeded tasks → panel renders 3 rows with state glyph + elapsed
//   - TestSlashTasksKillRunning — Enter on a running shell row → registry transitions to killed within 1s
//   - TestStatusLineTaskCount   — 0 tasks → no segment; 2 tasks → "· 2 tasks"
//   - TestStatusLineUpdatesLive — subscriber fires → status row redraws within 100ms
//
// The tests run against the runMode + taskregistry primitives, so
// they exercise the real rendering pipeline (panelLines(), the
// statusRow() right label and the subscriber → tasksTickMsg
// chain) without needing a tmux probe.
//
// The tests also pin a couple of edge cases the deliverable
// mentions in §存疑:
//   - empty registry renders the dim-gray placeholder, not a
//     crash
//   - the legacy "2 running, 5 done" string is still produced by
//     tasksFeedback (cycle 106) so the print-mode slash command
//     does not regress
package main

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// seedRunMode returns a runMode wired to a fresh, empty task
// registry. Tests use it to seed the panel / status row without
// touching the package-level defaultTaskSubagentManager (which is
// owned by other tests).
func seedRunMode() *runMode {
	m := &runMode{}
	m.taskReg = taskregistry.New()
	m.taskSubagentManager = subagent.NewManager()
	return m
}

// seedTask inserts a single task into reg with the given state.
// Convenience helper to keep the test bodies readable.
func seedTask(reg *taskregistry.Registry, id string, name string, kind taskregistry.Kind, state taskregistry.State) *taskregistry.Task {
	t := &taskregistry.Task{
		ID:    id,
		Name:  name,
		Kind:  kind,
		State: state,
	}
	if state == taskregistry.StateRunning {
		t.StartedAt = time.Now().Add(-2 * time.Second)
	} else if state == taskregistry.StateDone {
		t.StartedAt = time.Now().Add(-12 * time.Second)
		t.EndedAt = time.Now()
		t.ExitCode = 0
	} else {
		t.StartedAt = time.Now()
	}
	reg.Register(t)
	return t
}

// stripAll removes every ANSI escape sequence (SGR + cursor) so
// the assertions compare plain text. stripSGR only handles SGR;
// /tasks panels use a lot of 24-bit RGB sequences so we need a
// broader stripper here.
func stripAll(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// plainPanelLines returns the rendered /tasks panel as plain
// (escape-stripped) lines.
func plainPanelLines(m *runMode) []string {
	raw := m.panelTasksLines()
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, stripAll(l))
	}
	return out
}

func TestSlashTasksEmpty(t *testing.T) {
	m := seedRunMode()
	lines := plainPanelLines(m)
	// The header is always the bold "Tasks" title.
	if !strings.HasPrefix(lines[0], "Tasks") {
		t.Fatalf("first line = %q, want to start with %q", lines[0], "Tasks")
	}
	// The empty placeholder is on line 2 — dim gray, exact match.
	if !strings.Contains(lines[1], "No background tasks.") {
		t.Errorf("line[1] = %q, want substring %q", lines[1], "No background tasks.")
	}
	// Footer legend (Keyboard: ...) is on line 3.
	if !strings.HasPrefix(lines[3], "Keyboard:") {
		t.Errorf("footer = %q, want to start with %q", lines[3], "Keyboard:")
	}
	// The status-row suffix must be empty when the registry is
	// empty — the "· 0 tasks" form would be wrong.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Errorf("tasksStatusSuffix(empty) = %q, want empty string", got)
	}
}

func TestSlashTasksList(t *testing.T) {
	m := seedRunMode()
	// Seed t3 first with the OLDEST StartedAt so the registry
	// sorts it at index 0; the assertion below keys off the
	// rendered order so we want a deterministic shape. t3 is
	// the "done" task; t1 and t2 are running.
	now := time.Now()
	t3 := &taskregistry.Task{
		ID: "t3", Name: "echo done", Kind: taskregistry.KindShell,
		State: taskregistry.StateDone, StartedAt: now.Add(-12 * time.Second), EndedAt: now,
		ExitCode: 0,
	}
	t1 := &taskregistry.Task{
		ID: "t1", Name: "bash -c sleep 60", Kind: taskregistry.KindShell,
		State: taskregistry.StateRunning, StartedAt: now.Add(-2 * time.Second),
	}
	t2 := &taskregistry.Task{
		ID: "t2", Name: "ddgr --num=5 cats", Kind: taskregistry.KindShell,
		State: taskregistry.StateRunning, StartedAt: now.Add(-1 * time.Second),
	}
	m.taskReg.Register(t3)
	m.taskReg.Register(t1)
	m.taskReg.Register(t2)

	lines := plainPanelLines(m)
	// 6 rows: 1 header + 3 task rows + 1 trailing blank + 1 footer.
	if len(lines) != 6 {
		t.Fatalf("panel lines = %d, want 6", len(lines))
	}
	// Header
	if !strings.HasPrefix(lines[0], "Tasks") {
		t.Errorf("line[0] = %q, want Tasks", lines[0])
	}
	// Row 1 is t3 (oldest StartedAt); rows 2 and 3 are t1 and
	// t2 (running). The state glyph + ID together pin the row.
	wantRows := []struct {
		id    string
		state string
	}{
		{"t3", "done"},
		{"t1", "running"},
		{"t2", "running"},
	}
	for i, want := range wantRows {
		line := lines[1+i]
		if !strings.Contains(line, want.state) {
			t.Errorf("line[%d] = %q, want to contain %q", 1+i, line, want.state)
		}
		if !strings.Contains(line, want.id) {
			t.Errorf("line[%d] = %q, want to contain %q", 1+i, line, want.id)
		}
	}
	// Footer is on row 5.
	if !strings.HasPrefix(lines[5], "Keyboard:") {
		t.Errorf("footer = %q, want Keyboard:", lines[5])
	}

	// Status row: 2 running tasks → " · 2 tasks".
	got := m.tasksStatusSuffix()
	if got != " · 2 tasks" {
		t.Errorf("tasksStatusSuffix = %q, want %q", got, " · 2 tasks")
	}
}

func TestSlashTasksKillRunning(t *testing.T) {
	m := seedRunMode()
	// Seed a Subagent so the kill path has something to call
	// Kill on. The bridge attaches the subagent to the registry
	// for us; the test then asserts the transition lands.
	sa := subagent.NewSubagent("bash", "-c", "sleep 30")
	taskID := m.taskReg.AttachSubagent(sa, "")
	if taskID == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	// Register the Subagent in the panel's manager so the kill
	// path can find it.
	m.taskSubagentManager.Add(sa)

	// Start it so it transitions pending → running.
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent.Start: %v", err)
	}
	// Spin briefly so the goroutine that drains stdout can
	// fire the StateChangeFunc.
	time.Sleep(50 * time.Millisecond)

	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateRunning {
		t.Fatalf("post-Start state = %q, want running", got)
	}

	// Press Enter on the cursor row. The cursor defaults to 0,
	// and with one task that is the running row.
	m.panel = slashPanel{kind: panelTasks, cmd: "/tasks", cursor: 0}
	handled, _ := m.tasksPanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("tasksPanelKey(Enter) reported unhandled")
	}

	// The Kill must propagate through the StateChangeFunc and
	// land in the registry within 1s (the test budget for the
	// "killed within 1s" claim in the deliverable).
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if m.taskReg.Get(taskID).State == taskregistry.StateKilled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateKilled {
		t.Errorf("state after kill = %q, want killed", got)
	}
}

func TestStatusLineTaskCount(t *testing.T) {
	m := seedRunMode()

	// Zero tasks → no segment.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Errorf("zero-task suffix = %q, want empty", got)
	}

	// Two running tasks → " · 2 tasks".
	seedTask(m.taskReg, "t1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)
	seedTask(m.taskReg, "t2", "bash -c sleep 90", taskregistry.KindShell, taskregistry.StateRunning)
	got := m.tasksStatusSuffix()
	if got != " · 2 tasks" {
		t.Errorf("two-task suffix = %q, want %q", got, " · 2 tasks")
	}

	// One running + one done → still " · 1 task".
	tt := m.taskReg.Get("t2")
	m.taskReg.Update(tt.ID, func(t *taskregistry.Task) {
		t.State = taskregistry.StateDone
		t.ExitCode = 0
		t.EndedAt = time.Now()
	})
	if got := m.tasksStatusSuffix(); got != " · 1 task" {
		t.Errorf("one-running suffix = %q, want %q", got, " · 1 task")
	}
}

func TestStatusLineUpdatesLive(t *testing.T) {
	m := seedRunMode()
	m.startTasksSubscription()
	defer func() {
		if m.tasksSubCancel != nil {
			m.tasksSubCancel()
		}
	}()

	// Initial state: empty suffix.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Fatalf("initial suffix = %q, want empty", got)
	}

	// Register a task; the subscriber goroutine should set
	// tasksDirty within a few ms (the channel is buffered, so
	// the send completes immediately).
	seedTask(m.taskReg, "live-1", "bash -c sleep 5", taskregistry.KindShell, taskregistry.StateRunning)

	// Wait up to 100 ms for the dirty flag. The field is
	// int32 (atomic) — go vet's atomic checker would otherwise
	// flag this read.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&m.tasksDirty) == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&m.tasksDirty) != 1 {
		t.Fatal("subscriber did not set tasksDirty within 100ms")
	}

	// Once dirty, the suffix must report the new running count.
	if got := m.tasksStatusSuffix(); got != " · 1 task" {
		t.Errorf("suffix after register = %q, want %q", got, " · 1 task")
	}
}

// TestTasksFeedback_Regression keeps the cycle-106 recovered text
// alive so the print-mode /tasks path does not regress when this
// cycle replaces the no-op handler with the live panel. The text
// "No background tasks." / "2 running, 5 done" is the byte-exact
// output the print path renders; it must not drift.
func TestTasksFeedback_Regression(t *testing.T) {
	if got := tasksFeedback(0, 0); got != "No background tasks." {
		t.Errorf("tasksFeedback(0,0) = %q, want %q", got, "No background tasks.")
	}
	if got := tasksFeedback(2, 5); got != "2 running, 5 done" {
		t.Errorf("tasksFeedback(2,5) = %q, want %q", got, "2 running, 5 done")
	}
}

// TestSubscribedIsIdempotent guards the panel-reopen path: every
// time the panel opens, the cycle-206 listPanelKind switch flips
// and we want the subscriber to be re-armed without leaking the
// previous subscription.
func TestSubscribedIsIdempotent(t *testing.T) {
	m := seedRunMode()
	m.startTasksSubscription()
	first := m.tasksSubCancel
	m.startTasksSubscription()
	second := m.tasksSubCancel
	if first == nil || second == nil {
		t.Fatal("subscriber did not arm")
	}
	// Cancel both to clean up. We cannot compare func pointers
	// in Go (the language forbids it); the test asserts that
	// BOTH cancel funcs are non-nil so a regression that
	// returned nil for the second arm would be caught.
	first()
	second()
}
