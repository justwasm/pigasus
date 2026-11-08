// tasks_host_test.go — the host's side of the /tasks panel.
//
// The panel itself is tested in internal/ui/views/taskspanel: this file
// covers what only the host owns — the registry the panel reads, the
// kill that needs the session's subagent manager, the panel's rebuild
// on open and the status row's "· N tasks" segment.

package tui

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
)

// seedRootModel returns a RootModel wired to a fresh, empty task
// registry and the session's own subagent manager. Tests use it to
// seed the panel / status row.
func seedRootModel() *RootModel {
	m := &RootModel{}
	m.tasks.reg = taskregistry.New()
	m.tasks.subagentManager = subagent.NewManager()
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

// TestTasksPanelIsRebuiltOnOpen — the panel is rebuilt every time it
// opens, so a stale cursor or a stale detail view is never shown.
func TestTasksPanelIsRebuiltOnOpen(t *testing.T) {
	m := seedRootModel()
	seedTask(m.tasks.reg, "t1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)

	m.openSlashPanel("tasks")
	if m.overlays.panel.tp == nil {
		t.Fatal("the /tasks panel was not built on open")
	}
	m.tasksPanel().HandleKey(tea.Key{Code: tea.KeyDown})
	m.tasksPanel().HandleKey(tea.Key{Code: tea.KeyEnter})
	if !m.tasksPanel().Detail() {
		t.Fatal("Enter did not open the detail view")
	}

	// Re-opening the panel starts from a clean one.
	m.openSlashPanel("tasks")
	if m.tasksPanel().Detail() {
		t.Error("the reopened panel still shows the detail view")
	}
	if got := m.tasksPanel().Cursor(); got != 0 {
		t.Errorf("the reopened panel's cursor = %d, want 0", got)
	}
}

// TestTasksPanelKeyReachesThePanel — the host routes the panel's keys
// to the panel, and the panel's "k" reaches the host's kill.
func TestTasksPanelKeyReachesThePanel(t *testing.T) {
	m := seedRootModel()
	sa := subagent.NewSubagent("bash", "-c", "sleep 30")
	taskID := m.tasks.reg.AttachSubagent(sa, "")
	if taskID == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	m.tasks.subagentManager.Add(sa)
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent.Start: %v", err)
	}
	// Spin briefly so the goroutine that drains stdout can fire the
	// StateChangeFunc.
	time.Sleep(50 * time.Millisecond)
	if got := m.tasks.reg.Get(taskID).State; got != taskregistry.StateRunning {
		t.Fatalf("post-Start state = %q, want running", got)
	}

	m.openSlashPanel("tasks")
	handled, _ := m.handlePanelKey(tea.Key{Code: 'k'})
	if !handled {
		t.Fatal("the 'k' key reported unhandled")
	}

	// The Kill must propagate through the StateChangeFunc and land in
	// the registry within 1s.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if m.tasks.reg.Get(taskID).State == taskregistry.StateKilled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.tasks.reg.Get(taskID).State; got != taskregistry.StateKilled {
		t.Errorf("state after kill = %q, want killed", got)
	}
}

// TestSlashTasksKillRunning — the cycle-315 change: Enter no longer
// kills, it opens the row's detail view. Killing moved to "k", the
// reference's KeyTaskKill binding (the /tasks legend says
// "k Kill Task").
func TestSlashTasksKillRunning(t *testing.T) {
	m := seedRootModel()
	sa := subagent.NewSubagent("bash", "-c", "sleep 30")
	taskID := m.tasks.reg.AttachSubagent(sa, "")
	if taskID == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	m.tasks.subagentManager.Add(sa)
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent.Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := m.tasks.reg.Get(taskID).State; got != taskregistry.StateRunning {
		t.Fatalf("post-Start state = %q, want running", got)
	}

	m.openSlashPanel("tasks")
	handled, _ := m.handlePanelKey(tea.Key{Code: 'k'})
	if !handled {
		t.Fatal("the 'k' key reported unhandled")
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if m.tasks.reg.Get(taskID).State == taskregistry.StateKilled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.tasks.reg.Get(taskID).State; got != taskregistry.StateKilled {
		t.Errorf("state after kill = %q, want killed", got)
	}
}

// TestTasksPanel_EnterOpensDetailAndArmsTick pins that pressing Enter on a task
// opens its detail view and emits a DetailOpenedMsg which triggers armTasksTick
// to keep live output refreshing.
func TestTasksPanel_EnterOpensDetailAndArmsTick(t *testing.T) {
	m := seedRootModel()
	seedTask(m.tasks.reg, "sub-live", "bash -c sleep 60", taskregistry.KindSubagent, taskregistry.StateRunning)

	m.openSlashPanel("tasks")
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected Enter to be handled and return a tea.Cmd")
	}

	msg := cmd()
	detailMsg, ok := msg.(taskspanel.DetailOpenedMsg)
	if !ok || detailMsg.Task.ID != "sub-live" {
		t.Fatalf("expected DetailOpenedMsg with ID sub-live, got %T: %+v", msg, msg)
	}

	// Update handles DetailOpenedMsg and arms tick
	_, tickCmd := m.Update(detailMsg)
	if tickCmd == nil {
		t.Fatalf("expected Update(DetailOpenedMsg) to arm tasks tick")
	}

	// In detail mode, shouldTickTasks must be true
	if !m.shouldTickTasks() {
		t.Fatalf("expected shouldTickTasks() to be true when tasks panel detail is open")
	}
}

// TestTasksPanel_ActiveItemsEnterOpensDetail drives the real key path:
// ↓ moves the focus into the active-items block, then Enter opens the
// selected row's detail view in /tasks.
//
// The keybindings are explicit because the regression only shows with a
// keymap loaded: cli.enter used to be dispatched before the block, so
// Enter submitted the prompt and the block never saw it.
func TestTasksPanel_ActiveItemsEnterOpensDetail(t *testing.T) {
	path := t.TempDir() + "/keybindings.json"
	if err := os.WriteFile(path, []byte(`{"cli.enter": ["enter"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, err := keybind.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.prompt.keybindings = kb
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tasks.reg = taskregistry.New()
	seedTask(m.tasks.reg, "t1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)

	m.Update(downKey())
	if !m.activeItems.Panel.Focused() {
		t.Fatal("↓ did not move the focus into the active-items block")
	}

	m.Update(enterKey())
	if m.overlays.panel.kind != panelregistry.Tasks {
		t.Fatalf("Enter did not open /tasks, panel kind = %v", m.overlays.panel.kind)
	}
	tp := m.overlays.panel.tp
	if tp == nil || !tp.Detail() {
		t.Fatal("Enter did not open the task's detail view")
	}
	if got := tp.DetailID(); got != "t1" {
		t.Errorf("detail ID = %q, want t1", got)
	}
	if m.activeItems.Panel.Focused() {
		t.Error("the active-items block kept the focus after opening the detail view")
	}
}

// TestTasksPanel_ActiveItemsMsgOpenDetail pins that opening detail from active items
// arms the live tasks tick and does not crash or double-create the panel.
func TestTasksPanel_ActiveItemsMsgOpenDetail(t *testing.T) {
	m := seedRootModel()
	seedTask(m.tasks.reg, "sub-item", "live task", taskregistry.KindSubagent, taskregistry.StateRunning)

	_, cmd := m.Update(activeitemspanel.MsgOpenDetail{
		ID:         "sub-item",
		IsSubagent: true,
	})
	if cmd == nil {
		t.Fatalf("expected MsgOpenDetail to return armTasksTick cmd")
	}

	if m.overlays.panel.tp == nil || !m.overlays.panel.tp.Detail() {
		t.Fatalf("expected /tasks panel to be in detail view")
	}
	if m.overlays.panel.tp.DetailID() != "sub-item" {
		t.Fatalf("expected detail ID 'sub-item', got %q", m.overlays.panel.tp.DetailID())
	}
}
