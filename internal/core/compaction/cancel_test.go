package compaction

import (
	"context"
	"testing"
)

func TestCompactionCancel(t *testing.T) {
	t.Cleanup(ResetForTest)
	// TriggerCancel with no active cancel is a safe no-op.
	TriggerCancel()

	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())
	cleanup := InstallCancel(func() {
		cancelled = true
		cancel()
	})

	TriggerCancel()
	if !cancelled {
		t.Errorf("expected cancel func to be triggered")
	}
	if ctx.Err() == nil {
		t.Errorf("expected context to be cancelled")
	}

	cleanup()
	cancelled = false
	TriggerCancel()
	if cancelled {
		t.Errorf("expected cancel func not to be triggered after cleanup")
	}
}

// TestInstallCancel_OverlappingCompactionsKeepTheirOwnSlot pins the slot's
// ownership rule: an overlapping compaction's cleanup must not clear a newer
// one's cancel.
//
// Two compactions can run at once — an automatic checkpoint and a manual
// /compact, or two sessions — and the older one finishing first used to leave
// the newer one's "esc to cancel" hint dead.
func TestInstallCancel_OverlappingCompactionsKeepTheirOwnSlot(t *testing.T) {
	t.Cleanup(ResetForTest)
	olderFired, newerFired := false, false

	cleanupOlder := InstallCancel(func() { olderFired = true })
	cleanupNewer := InstallCancel(func() { newerFired = true })

	// The older compaction finishes while the newer one is still running.
	cleanupOlder()

	TriggerCancel()
	if !newerFired {
		t.Errorf("the older compaction's cleanup wiped the newer one's cancel slot")
	}
	if olderFired {
		t.Errorf("the older compaction's cancel fired after it finished")
	}

	// The newer one's own cleanup still clears it.
	cleanupNewer()
	newerFired = false
	TriggerCancel()
	if newerFired {
		t.Errorf("the newer compaction's cleanup did not clear its slot")
	}
}

// TestResetForTestClearsTheSlot pins the test seam that keeps one test's
// abandoned slot out of the next one's run.
//
// The shape it guards: a test installs a cancel, fires Esc, reads the result,
// and returns without the worker's deferred cleanup having run. The slot still
// points at that dead cancel. Without a reset the *next* test's Esc fires it,
// and its own "esc to cancel" hint never does anything — the ordering bug that
// made the TUI's compaction test pass alone and fail in a shuffled suite.
func TestResetForTestClearsTheSlot(t *testing.T) {
	t.Cleanup(ResetForTest)
	// Leave a slot behind, exactly as an early-returning test would.
	staleFired := false
	InstallCancel(func() { staleFired = true }) // cleanup deliberately not called
	if CancelSlotIsEmpty() {
		t.Fatal("the slot should be occupied before the reset; the test is not exercising the leak")
	}

	ResetForTest()

	if !CancelSlotIsEmpty() {
		t.Errorf("ResetForTest left the slot occupied")
	}
	TriggerCancel()
	if staleFired {
		t.Errorf("TriggerCancel fired a cancel left over from before the reset; the leak is still reachable")
	}
}

// TestResetForTestReArmsTheSlot — a reset must not poison the slot for the
// next install: a fresh compaction has to be cancellable again.
func TestResetForTestReArmsTheSlot(t *testing.T) {
	t.Cleanup(ResetForTest)
	InstallCancel(func() {}) // an abandoned slot
	ResetForTest()

	fired := false
	cleanup := InstallCancel(func() { fired = true })
	defer cleanup()

	TriggerCancel()
	if !fired {
		t.Errorf("the slot installed after a reset did not fire")
	}
}

// TestResetForTestIsIdempotent — calling it twice, or on an empty slot, is
// safe. Tests cleanup in t.Cleanup can run it whether or not the slot was
// ever filled.
func TestResetForTestIsIdempotent(t *testing.T) {
	ResetForTest()
	ResetForTest()
	if !CancelSlotIsEmpty() {
		t.Errorf("repeated resets left the slot occupied")
	}
}
