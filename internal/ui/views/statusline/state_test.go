// state_test.go — the status area's failure bookkeeping.
//
// Evidence: the reference's own status line documentation — "After 30
// failed runs in a row, it stops running the script for the rest of the
// session and shows `⚠ Statusline disabled after 30 consecutive failures.`"
package statusline_test

import (
	"errors"
	"testing"

	"github.com/justwasm/pigasus/internal/statusline"
)

// TestState_FailureCounting walks the consecutive-failure counter up to
// the reference's limit and back down.
func TestState_FailureCounting(t *testing.T) {
	var s statusline.State
	for i := 1; i < statusline.MaxConsecutiveFailures; i++ {
		count, disabled := s.RecordFailure()
		if count != i {
			t.Errorf("failure %d counted as %d", i, count)
		}
		if disabled {
			t.Fatalf("disabled after %d failures, want %d", i, statusline.MaxConsecutiveFailures)
		}
		if s.Failures() != i {
			t.Errorf("Failures = %d, want %d", s.Failures(), i)
		}
		if s.Disabled() {
			t.Errorf("Disabled = true after %d failures", i)
		}
	}
	count, disabled := s.RecordFailure()
	if count != statusline.MaxConsecutiveFailures {
		t.Errorf("count = %d, want %d", count, statusline.MaxConsecutiveFailures)
	}
	if !disabled {
		t.Error("disabled = false at the limit")
	}
	if !s.Disabled() {
		t.Error("Disabled = false at the limit")
	}

	// A success clears the counter — but not the disabled flag: the
	// reference stays off for the rest of the session.
	s.RecordSuccess()
	if s.Failures() != 0 {
		t.Errorf("Failures = %d after a success", s.Failures())
	}
	if !s.Disabled() {
		t.Error("a success must not re-enable a disabled status line")
	}

	// ResetDisabled is what a new command does.
	s.ResetDisabled()
	if s.Disabled() {
		t.Error("Disabled = true after ResetDisabled")
	}
	if s.Failures() != 0 {
		t.Errorf("Failures = %d after ResetDisabled", s.Failures())
	}
}

// TestState_SetRendered keeps the last error until a successful run
// clears it.
func TestState_SetRendered(t *testing.T) {
	var s statusline.State
	s.SetRendered("", errors.New("boom"))
	out, err := s.Current()
	if out != "" {
		t.Errorf("output = %q", out)
	}
	if err == nil {
		t.Fatal("expected the error to be kept")
	}
	s.SetRendered("rendered", nil)
	out, err = s.Current()
	if out != "rendered" {
		t.Errorf("output = %q", out)
	}
	if err != nil {
		t.Errorf("error = %v, want nil after a successful run", err)
	}
}

// TestState_Ticks counts the tick events.
func TestState_Ticks(t *testing.T) {
	var s statusline.State
	if s.MarkTick() != 1 || s.MarkTick() != 2 {
		t.Errorf("ticks = %d", s.SnapshotTick())
	}
	s.ResetTick()
	if s.SnapshotTick() != 0 {
		t.Errorf("ticks = %d after the reset", s.SnapshotTick())
	}
}
