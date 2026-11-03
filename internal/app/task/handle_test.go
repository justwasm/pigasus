package task

import (
	"context"
	"testing"
	"time"
)

func TestRecordActivity_ReentrancySafety(t *testing.T) {
	r := New()
	taskID := "task-recursive"
	r.Register(&Task{
		ID:        taskID,
		Name:      "test-recursion",
		Kind:      KindSubagent,
		State:     StateRunning,
		StartedAt: time.Now().Add(-10 * time.Second),
	})

	callCount := 0
	r.RegisterHandle(taskID, &TaskHandle{
		ID: taskID,
		OnActivity: func() {
			callCount++
			// Deliberately simulate circular activity trigger
			r.RecordActivity(taskID)
		},
	})

	// Must not panic or cause infinite recursion / stack overflow
	r.RecordActivity(taskID)

	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (reentrancy guard should stop second invocation)", callCount)
	}

	task := r.Get(taskID)
	if task == nil {
		t.Fatalf("task was nil")
	}
	if time.Since(task.LastActivityAt) > 2*time.Second {
		t.Errorf("task.LastActivityAt not updated: %v", task.LastActivityAt)
	}
}

func TestTaskHandle_LifecycleControls(t *testing.T) {
	r := New()
	taskID := "task-control"
	r.Register(&Task{
		ID:        taskID,
		Name:      "test-control",
		Kind:      KindSubagent,
		State:     StateRunning,
		StartedAt: time.Now(),
	})

	aborted := false
	cancelled := false
	resumed := false

	r.RegisterHandle(taskID, &TaskHandle{
		ID:     taskID,
		Cancel: func() { cancelled = true },
		Abort:  func() { aborted = true },
		Resume: func(ctx context.Context) error {
			resumed = true
			return nil
		},
	})

	if !r.Abort(taskID) {
		t.Error("Abort returned false, want true")
	}
	if !aborted {
		t.Error("Abort callback was not executed")
	}

	if err := r.Resume(context.Background(), taskID); err != nil {
		t.Errorf("Resume failed: %v", err)
	}
	if !resumed {
		t.Error("Resume callback was not executed")
	}

	if !r.Kill(taskID) {
		t.Error("Kill returned false, want true")
	}
	if !cancelled {
		t.Error("Cancel callback was not executed on Kill")
	}
	task := r.Get(taskID)
	if task.State != StateKilled {
		t.Errorf("task.State = %v, want %v", task.State, StateKilled)
	}
}
