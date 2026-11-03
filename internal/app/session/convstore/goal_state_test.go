package convstore

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m 0s"},
		{83 * time.Second, "1m 23s"},
		{3600 * time.Second, "1h 0m"},
		{3665 * time.Second, "1h 1m"},
		{7320 * time.Second, "2h 2m"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.d)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestCheckGoalOutcome(t *testing.T) {
	replyComplete := "All files updated and tests pass.\n" + GoalCompleteToken
	comp, canc := CheckGoalOutcome(replyComplete)
	if !comp || canc {
		t.Errorf("expected complete, got comp=%v canc=%v", comp, canc)
	}

	replyCancel := "User requested stop.\n" + GoalCancelledToken
	comp, canc = CheckGoalOutcome(replyCancel)
	if comp || !canc {
		t.Errorf("expected cancel, got comp=%v canc=%v", comp, canc)
	}

	replyNormal := "Working on it..."
	comp, canc = CheckGoalOutcome(replyNormal)
	if comp || canc {
		t.Errorf("expected none, got comp=%v canc=%v", comp, canc)
	}
}

func TestBuildGoalReminderMessage(t *testing.T) {
	started := time.Now().Add(-90 * time.Second)
	msg := BuildGoalReminderMessage("Refactor the parser", started, 2)

	if !strings.Contains(msg, "You are still working toward the user's goal.") {
		t.Errorf("missing lead-in in %q", msg)
	}
	if !strings.Contains(msg, "Continuation: #2") {
		t.Errorf("missing continuation in %q", msg)
	}
	if !strings.Contains(msg, GoalCompleteToken) {
		t.Errorf("missing complete token in %q", msg)
	}
	if !strings.Contains(msg, GoalCancelledToken) {
		t.Errorf("missing cancel token in %q", msg)
	}
	if !strings.Contains(msg, "Original request: Refactor the parser") {
		t.Errorf("missing original request in %q", msg)
	}
}

func TestGoalStateLifecycle(t *testing.T) {
	gs := NewGoalState("Fix weather command", 5)
	if !gs.IsActive() {
		t.Error("new goal should be active")
	}
	if gs.ContinuationCount != 0 {
		t.Errorf("initial count = %d, want 0", gs.ContinuationCount)
	}

	gs.State = GoalStateCompleted
	if gs.IsActive() {
		t.Error("completed goal should not be active")
	}
}
