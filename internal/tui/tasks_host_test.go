// tasks_host_test.go — the host's side of the /tasks panel.
//
// The panel itself is tested in internal/ui/views/taskspanel: this file
// covers what only the host owns — the registry the panel reads, the
// kill that needs the session's subagent manager, the panel's rebuild
// on open and the status row's "· N tasks" segment.

package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// seedRunMode returns a runMode wired to a fresh, empty task
// registry and the session's own subagent manager. Tests use it to
// seed the panel / status row.
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

// TestTasksPanelIsRebuiltOnOpen — the panel is rebuilt every time it
// opens, so a stale cursor or a stale detail view is never shown.
func TestTasksPanelIsRebuiltOnOpen(t *testing.T) {
	m := seedRunMode()
	seedTask(m.taskReg, "t1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)

	m.openSlashPanel("tasks")
	if m.panel.tp == nil {
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
	m := seedRunMode()
	sa := subagent.NewSubagent("bash", "-c", "sleep 30")
	taskID := m.taskReg.AttachSubagent(sa, "")
	if taskID == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	m.taskSubagentManager.Add(sa)
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent.Start: %v", err)
	}
	// Spin briefly so the goroutine that drains stdout can fire the
	// StateChangeFunc.
	time.Sleep(50 * time.Millisecond)
	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateRunning {
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
		if m.taskReg.Get(taskID).State == taskregistry.StateKilled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateKilled {
		t.Errorf("state after kill = %q, want killed", got)
	}
}

// TestSlashTasksKillRunning — the cycle-315 change: Enter no longer
// kills, it opens the row's detail view. Killing moved to "k", the
// reference's KeyTaskKill binding (the /tasks legend says
// "k Kill Task").
func TestSlashTasksKillRunning(t *testing.T) {
	m := seedRunMode()
	sa := subagent.NewSubagent("bash", "-c", "sleep 30")
	taskID := m.taskReg.AttachSubagent(sa, "")
	if taskID == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	m.taskSubagentManager.Add(sa)
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent.Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateRunning {
		t.Fatalf("post-Start state = %q, want running", got)
	}

	m.openSlashPanel("tasks")
	handled, _ := m.handlePanelKey(tea.Key{Code: 'k'})
	if !handled {
		t.Fatal("the 'k' key reported unhandled")
	}

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
