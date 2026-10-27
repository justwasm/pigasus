// backoff_step_test.go — the shared exponential schedule.
//
// retry's Step is the clone's one exponential backoff: the
// canonical schedule, the connection retry's and the stall recovery's
// all run on it. The test pins the schedule itself and the knobs' fall
// backs, so a caller that passes a zero knob cannot silently get a zero
// wait.
package retry

import (
	"testing"
	"time"
)

// TestStepSchedule pins the exponential growth and the cap.
func TestStepSchedule(t *testing.T) {
	cases := []struct {
		attempt int
		initial time.Duration
		cap     time.Duration
		want    time.Duration
	}{
		{0, time.Second, 30 * time.Second, time.Second},
		{1, time.Second, 30 * time.Second, 2 * time.Second},
		{2, time.Second, 30 * time.Second, 4 * time.Second},
		{3, time.Second, 30 * time.Second, 8 * time.Second},
		{4, time.Second, 30 * time.Second, 16 * time.Second},
		// 32s is past the cap: the schedule clamps.
		{5, time.Second, 30 * time.Second, 30 * time.Second},
		{9, time.Second, 30 * time.Second, 30 * time.Second},
		// The stall recovery's own knobs: a 2s seed.
		{0, 2 * time.Second, 30 * time.Second, 2 * time.Second},
		{1, 2 * time.Second, 30 * time.Second, 4 * time.Second},
		{2, 2 * time.Second, 30 * time.Second, 8 * time.Second},
		{3, 2 * time.Second, 30 * time.Second, 16 * time.Second},
		{4, 2 * time.Second, 30 * time.Second, 30 * time.Second},
		// A negative attempt is clamped to 0.
		{-1, time.Second, 30 * time.Second, time.Second},
		// An initial above the cap is clamped too.
		{0, time.Minute, 30 * time.Second, 30 * time.Second},
	}
	for _, tc := range cases {
		got := Step(tc.attempt, tc.initial, tc.cap)
		if got != tc.want {
			t.Errorf("Step(%d, %v, %v) = %v, want %v", tc.attempt, tc.initial, tc.cap, got, tc.want)
		}
	}
}

// TestStepFallsBackToTheCanonicalKnobs pins the fall backs: a
// non-positive initial or cap yields the canonical schedule, not a zero
// wait and not an uncapped wait.
func TestStepFallsBackToTheCanonicalKnobs(t *testing.T) {
	if got := Step(0, 0, 30*time.Second); got != InitialInterval {
		t.Errorf("Step(0, 0, cap) = %v, want the canonical %v", got, InitialInterval)
	}
	if got := Step(0, time.Second, 0); got != InitialInterval {
		t.Errorf("Step(0, initial, 0) = %v, want the canonical %v", got, InitialInterval)
	}
	// With the canonical cap the schedule clamps at MaxInterval.
	if got := Step(9, time.Second, 0); got != MaxInterval {
		t.Errorf("Step(9, initial, 0) = %v, want the canonical cap %v", got, MaxInterval)
	}
}

// TestStepMatchesTheCanonicalSchedule pins that the canonical schedule
// and the shared step agree — the canonical one is the shared one with
// the canonical knobs.
func TestStepMatchesTheCanonicalSchedule(t *testing.T) {
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		d, err := ComputeDelay(attempt)
		if err != nil {
			t.Fatalf("ComputeDelay(%d) err = %v", attempt, err)
		}
		if got := Step(attempt, InitialInterval, MaxInterval); got != d {
			t.Errorf("Step(%d) = %v, want the canonical %v", attempt, got, d)
		}
	}
}
