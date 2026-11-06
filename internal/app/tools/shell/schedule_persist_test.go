package shell

import (
	"testing"
	"time"
)

func TestActiveSchedules_Persistence(t *testing.T) {
	home := t.TempDir()
	convID := "test-sched-conv"
	now := time.Now().Truncate(time.Second)
	target := now.Add(10 * time.Minute)

	sched := PersistedSchedule{
		TaskID:      "task-1",
		Prompt:      "Check build status",
		DurationSec: 600,
		CreatedAt:   now,
		TargetTime:  target,
	}

	// Add
	if err := AddActiveSchedule(home, convID, sched); err != nil {
		t.Fatalf("AddActiveSchedule failed: %v", err)
	}

	// Load
	loaded := LoadActiveSchedules(home, convID)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(loaded))
	}
	if loaded[0].TaskID != "task-1" || loaded[0].Prompt != "Check build status" {
		t.Errorf("unexpected loaded schedule: %+v", loaded[0])
	}
	if !loaded[0].TargetTime.Equal(target) {
		t.Errorf("expected TargetTime %v, got %v", target, loaded[0].TargetTime)
	}

	// Remove
	if err := RemoveActiveSchedule(home, convID, "task-1"); err != nil {
		t.Fatalf("RemoveActiveSchedule failed: %v", err)
	}
	afterRemove := LoadActiveSchedules(home, convID)
	if len(afterRemove) != 0 {
		t.Errorf("expected 0 schedules after remove, got %d", len(afterRemove))
	}
}
