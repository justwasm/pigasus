package shell

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStartScheduledTask_OneShot(t *testing.T) {
	convID := "test-conv-timer"
	taskID := "task-1"
	logPath := filepath.Join(t.TempDir(), "task-1.log")

	notifications := make(chan TaskNotification, 1)
	prev := SetTaskNotifier(func(n TaskNotification) {
		notifications <- n
	})
	defer SetTaskNotifier(prev)

	bg, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:   convID,
		TaskID:   taskID,
		LogPath:  logPath,
		Prompt:   "Wake up now!",
		Duration: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("StartScheduledTask failed: %v", err)
	}

	select {
	case n := <-notifications:
		if n.TaskID != convID+"/"+taskID {
			t.Errorf("expected task ID %q, got %q", convID+"/"+taskID, n.TaskID)
		}
		if n.Body != "Wake up now!" {
			t.Errorf("expected body 'Wake up now!', got %q", n.Body)
		}
		if n.Priority != TaskPriorityHigh {
			t.Errorf("expected priority %q, got %q", TaskPriorityHigh, n.Priority)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timer notification")
	}

	if !bg.HasCompleted() {
		t.Errorf("expected background task to be marked completed")
	}
}

func TestStartScheduledTask_ManageTaskKill(t *testing.T) {
	convID := "test-conv-kill"
	taskID := "task-2"
	logPath := filepath.Join(t.TempDir(), "task-2.log")

	notifications := make(chan TaskNotification, 1)
	prev := SetTaskNotifier(func(n TaskNotification) {
		notifications <- n
	})
	defer SetTaskNotifier(prev)

	bg, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:   convID,
		TaskID:   taskID,
		LogPath:  logPath,
		Prompt:   "Should not fire",
		Duration: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("StartScheduledTask failed: %v", err)
	}

	out, err := bg.Kill()
	if err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	if out != "Task "+convID+"/"+taskID+" killed." {
		t.Errorf("unexpected kill output: %q", out)
	}

	select {
	case n := <-notifications:
		t.Fatalf("unexpected notification from killed timer: %+v", n)
	case <-time.After(200 * time.Millisecond):
		// OK
	}
}

func TestStartScheduledTask_EarlyConditionCancellation(t *testing.T) {
	convID := "test-conv-cond"
	taskID := "task-3"
	logPath := filepath.Join(t.TempDir(), "task-3.log")

	timerNotifications := make(chan TaskNotification, 10)
	prev := SetTaskNotifier(func(n TaskNotification) {
		timerNotifications <- n
	})
	defer SetTaskNotifier(prev)

	bg, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:         convID,
		TaskID:         taskID,
		LogPath:        logPath,
		Prompt:         "Fallback reminder",
		Duration:       5 * time.Second,
		TimerCondition: "subagent-worker",
	})
	if err != nil {
		t.Fatalf("StartScheduledTask failed: %v", err)
	}

	// Trigger early cancellation via notification from the matching sender
	NotifyTask(TaskNotification{
		TaskID:   convID + "/subagent-worker",
		Priority: TaskPriorityHigh,
		Body:     "Subagent finished early!",
		At:       time.Now(),
	})

	time.Sleep(100 * time.Millisecond)

	if !bg.HasCompleted() {
		t.Errorf("expected timer task to be cancelled early and completed")
	}
}

func TestStartScheduledTask_CronEvery(t *testing.T) {
	convID := "test-conv-cron"
	taskID := "task-4"
	logPath := filepath.Join(t.TempDir(), "task-4.log")

	notifications := make(chan TaskNotification, 10)
	prev := SetTaskNotifier(func(n TaskNotification) {
		notifications <- n
	})
	defer SetTaskNotifier(prev)

	bg, err := StartScheduledTask(ScheduledTaskOptions{
		ConvID:        convID,
		TaskID:        taskID,
		LogPath:       logPath,
		Prompt:        "Cron tick",
		CronExpr:      "@every 40ms",
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("StartScheduledTask failed: %v", err)
	}

	var count int
	timeout := time.After(2 * time.Second)
	for count < 2 {
		select {
		case n := <-notifications:
			if n.Body == "Cron tick" {
				count++
			}
		case <-timeout:
			t.Fatalf("timed out waiting for 2 cron iterations, got %d", count)
		}
	}

	time.Sleep(100 * time.Millisecond)
	if !bg.HasCompleted() {
		t.Errorf("expected cron to complete after MaxIterations reached")
	}
}
