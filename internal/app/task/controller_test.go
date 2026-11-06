package task

import (
	"testing"
	"time"
)

func TestController_Lifecycle(t *testing.T) {
	reg := New()
	ctrl := NewController(reg)

	if ctrl.Registry() != reg {
		t.Fatalf("expected registry %v, got %v", reg, ctrl.Registry())
	}

	if ctrl.Subscribed() {
		t.Fatalf("expected not subscribed initially")
	}

	ctrl.StartSubscription()
	if !ctrl.Subscribed() {
		t.Fatalf("expected subscribed after StartSubscription")
	}

	// Trigger a task state transition and observe dirty flag
	task := &Task{
		ID:        "t1",
		Kind:      KindShell,
		State:     StateRunning,
		StartedAt: time.Now(),
	}
	reg.Register(task)

	time.Sleep(50 * time.Millisecond)
	if !ctrl.ConsumeDirty() {
		t.Fatalf("expected dirty flag to be set after task registration")
	}
	if ctrl.ConsumeDirty() {
		t.Fatalf("expected dirty flag to be cleared after ConsumeDirty")
	}

	// Inbox notification test
	ctrl.Inbox().Add(TaskNotification{
		TaskID:  "t1",
		Body:    "test output",
	})
	if ctrl.Inbox().Len() != 1 {
		t.Fatalf("expected 1 notification in inbox, got %d", ctrl.Inbox().Len())
	}
	drained := ctrl.Inbox().Drain()
	if len(drained) != 1 || drained[0].TaskID != "t1" {
		t.Fatalf("unexpected drained notifications: %+v", drained)
	}
	if ctrl.Inbox().Len() != 0 {
		t.Fatalf("expected inbox to be empty after Drain")
	}

	ctrl.Close()
	if ctrl.Subscribed() {
		t.Fatalf("expected not subscribed after Close")
	}
}
