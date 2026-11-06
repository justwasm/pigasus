package actor_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func TestActor_ScheduleDelay(t *testing.T) {
	mock := actor.NewMockProvider()
	var turnCount int32
	mock.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		atomic.AddInt32(&turnCount, 1)
		return []actor.StreamChunk{
			{Delta: "Wakeup turn processed", FinishReason: "stop"},
		}, nil
	})

	act, err := actor.Spawn(actor.Config{
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	events := act.Events()

	// Schedule a delay wakeup after 50ms
	schID, err := act.Schedule(50*time.Millisecond, "Perform background check")
	if err != nil {
		t.Fatalf("failed to schedule: %v", err)
	}
	if schID == "" {
		t.Fatal("expected non-empty schedule ID")
	}

	// Verify it shows up in ActiveSchedules
	active := act.ActiveSchedules()
	if len(active) != 1 || active[0].ID != schID {
		t.Fatalf("expected 1 active schedule %s, got %v", schID, active)
	}

	// Wait for EventScheduleFired and EventTurnCompleted
	firedReceived := false
	turnCompleted := false

	timeout := time.After(2 * time.Second)
	for !turnCompleted {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for scheduled wakeup turn")
		case ev := <-events:
			if ev.Type == actor.EventScheduleFired && ev.ScheduleRecord.ID == schID {
				firedReceived = true
			}
			if ev.Type == actor.EventTurnCompleted {
				turnCompleted = true
			}
		}
	}

	if !firedReceived {
		t.Error("did not receive EventScheduleFired")
	}
	if atomic.LoadInt32(&turnCount) != 1 {
		t.Errorf("expected 1 turn executed by wakeup, got %d", atomic.LoadInt32(&turnCount))
	}

	// Verify schedule was cleaned up
	if len(act.ActiveSchedules()) != 0 {
		t.Errorf("expected 0 active schedules after one-shot fired, got %d", len(act.ActiveSchedules()))
	}
}

func TestActor_CancelSchedule(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	schID, err := act.Schedule(500*time.Millisecond, "Should not fire")
	if err != nil {
		t.Fatalf("failed to schedule: %v", err)
	}

	if err := act.CancelSchedule(schID); err != nil {
		t.Fatalf("failed to cancel schedule: %v", err)
	}

	if len(act.ActiveSchedules()) != 0 {
		t.Errorf("expected 0 active schedules after cancel, got %d", len(act.ActiveSchedules()))
	}

	// Canceling non-existent schedule returns error
	if err := act.CancelSchedule("non_existent"); err == nil {
		t.Error("expected error canceling non existent schedule")
	}
}

func TestActor_ScheduleCronEvery(t *testing.T) {
	mock := actor.NewMockProvider()
	var turnCount int32
	mock.SetDefaultHandler(func(ctx context.Context, req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		atomic.AddInt32(&turnCount, 1)
		return []actor.StreamChunk{
			{Delta: "Cron tick", FinishReason: "stop"},
		}, nil
	})

	act, err := actor.Spawn(actor.Config{
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	// Recurring cron every 30ms
	schID, err := act.Cron("@every 30ms", "Periodic ping", true)
	if err != nil {
		t.Fatalf("failed to schedule cron: %v", err)
	}

	// Wait until at least 2 ticks fire
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for atomic.LoadInt32(&turnCount) < 2 {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for 2 cron ticks, got %d", atomic.LoadInt32(&turnCount))
		case <-ticker.C:
		}
	}

	// Cancel recurring cron
	if err := act.CancelSchedule(schID); err != nil {
		t.Fatalf("failed to cancel cron: %v", err)
	}
}
