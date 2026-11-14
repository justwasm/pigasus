package shell

import (
	"path/filepath"
	"testing"
	"time"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	tools "github.com/justwasm/pigasus/internal/app/tools"
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
