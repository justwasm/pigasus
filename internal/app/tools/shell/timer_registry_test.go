package shell

import (
	"path/filepath"
	"testing"
	"time"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
)

// TestKilledTimerLeavesTheTaskRegistry pins the cancellation contract of a
// scheduled timer: killing it must move its shared-registry row out of
// StateRunning.
//
// The registry is package-level and the active-items tray draws whatever is
// running in it, so a row that stayed "running" after its timer was gone put
// a phantom row under the prompt for the rest of the process — which is how
// it showed up: a TUI test that drew an empty screen still had three tray
// rows, from a timer a *different* test had scheduled and killed.
func TestKilledTimerLeavesTheTaskRegistry(t *testing.T) {
	convID := "test-conv-killed-timer"
	taskID := "task-killed"
	fullID := convID + "/" + taskID
	logPath := filepath.Join(t.TempDir(), taskID+".log")

	prev := SetTaskNotifier(func(TaskNotification) {})
	defer SetTaskNotifier(prev)

	if _, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:   convID,
		TaskID:   taskID,
		LogPath:  logPath,
		Prompt:   "far future",
		Duration: time.Hour,
	}); err != nil {
		t.Fatalf("StartScheduledTask: %v", err)
	}

	reg := tools.SharedTaskRegistry()
	tk := reg.Get(fullID)
	if tk == nil {
		t.Fatalf("the timer was not registered as %q", fullID)
	}
	if tk.State != taskregistry.StateRunning {
		t.Fatalf("state after arming = %q, want running", tk.State)
	}

	if err := KillBackgroundTask(fullID); err != nil {
		t.Fatalf("KillBackgroundTask: %v", err)
	}

	tk = reg.Get(fullID)
	if tk == nil {
		t.Fatalf("the row vanished instead of ending")
	}
	if tk.State == taskregistry.StateRunning {
		t.Fatalf("a killed timer is still %q: the tray and /tasks would keep drawing it", tk.State)
	}
	for _, active := range reg.Active() {
		if active.ID == fullID {
			t.Fatalf("a killed timer is still an active row")
		}
	}
}

// TestFiredTimerLeavesTheTaskRegistry pins that a one-shot timer transitions
// its shared-registry row to StateDone (with ExitCodeUnset) when it fires, so
// the active-items tray stops showing it once the wakeup has fired.
func TestFiredTimerLeavesTheTaskRegistry(t *testing.T) {
	convID := "test-conv-fired-timer"
	taskID := "task-fired"
	fullID := convID + "/" + taskID
	logPath := filepath.Join(t.TempDir(), taskID+".log")

	notified := make(chan TaskNotification, 1)
	prev := SetTaskNotifier(func(n TaskNotification) {
		if n.TaskID == fullID {
			notified <- n
		}
	})
	defer SetTaskNotifier(prev)

	if _, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:   convID,
		TaskID:   taskID,
		LogPath:  logPath,
		Prompt:   "fire soon",
		Duration: 25 * time.Millisecond,
	}); err != nil {
		t.Fatalf("StartScheduledTask: %v", err)
	}

	reg := tools.SharedTaskRegistry()
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timer notification")
	}

	tk := reg.Get(fullID)
	if tk == nil {
		t.Fatalf("the fired timer row vanished instead of ending")
	}
	if tk.State != taskregistry.StateDone {
		t.Fatalf("fired timer state = %q, want %q", tk.State, taskregistry.StateDone)
	}
	if tk.ExitCode != taskregistry.ExitCodeUnset {
		t.Fatalf("fired timer ExitCode = %d, want ExitCodeUnset (%d)", tk.ExitCode, taskregistry.ExitCodeUnset)
	}
	if tk.EndedAt.IsZero() {
		t.Fatalf("fired timer EndedAt is zero")
	}
	for _, active := range reg.Active() {
		if active.ID == fullID {
			t.Fatalf("a fired timer is still an active row")
		}
	}
}

// TestCompletedCronLeavesTheTaskRegistry pins that a recurring cron job
// transitions its shared-registry row to StateDone once MaxIterations completes.
func TestCompletedCronLeavesTheTaskRegistry(t *testing.T) {
	convID := "test-conv-completed-cron"
	taskID := "task-cron-done"
	fullID := convID + "/" + taskID
	logPath := filepath.Join(t.TempDir(), taskID+".log")

	notified := make(chan TaskNotification, 4)
	prev := SetTaskNotifier(func(n TaskNotification) {
		if n.TaskID == fullID {
			notified <- n
		}
	})
	defer SetTaskNotifier(prev)

	if _, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:        convID,
		TaskID:        taskID,
		LogPath:       logPath,
		Prompt:        "cron tick",
		CronExpr:      "@every 25ms",
		MaxIterations: 2,
	}); err != nil {
		t.Fatalf("StartScheduledTask: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-notified:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for cron iteration %d", i+1)
		}
	}

	reg := tools.SharedTaskRegistry()
	tk := reg.Get(fullID)
	if tk == nil {
		t.Fatalf("the completed cron row vanished instead of ending")
	}
	if tk.State != taskregistry.StateDone {
		t.Fatalf("completed cron state = %q, want %q", tk.State, taskregistry.StateDone)
	}
	for _, active := range reg.Active() {
		if active.ID == fullID {
			t.Fatalf("a completed cron is still an active row")
		}
	}
}
