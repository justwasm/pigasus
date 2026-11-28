package compaction

import (
	"context"
	"sync"
)

// CompactionCancelledLiteral is the reference binary's exact copy when Esc
// cancels a compaction (rodata offset 0x1034fb1aa in agy 1.4.10, 0x103647b2e in 1.4.8):
// "Compaction cancelled. Conversation was not compacted."
const CompactionCancelledLiteral = "Compaction cancelled. Conversation was not compacted."

var (
	compactCancelMu sync.Mutex
	compactCancel   context.CancelFunc
	// compactCancelGen identifies the slot's current owner: an install bumps
	// it, and a cleanup only clears the slot when the generation is still the
	// one it installed. See InstallCancel.
	compactCancelGen uint64
)

// InstallCancel atomically installs cancel as the active compaction cancel
// func. The returned cleanup clears the slot when *that* compaction finishes.
//
// The cleanup only clears the slot while it still holds its own func. Two
// compactions can overlap — an automatic checkpoint and a manual /compact, or
// two sessions — and a cleanup that cleared the slot unconditionally would
// wipe the newer one's, leaving its "esc to cancel" hint dead. Each cleanup
// therefore compares identity before it clears.
func InstallCancel(cancel context.CancelFunc) func() {
	// The identity is the func value itself; a func cannot be compared, so
	// the slot carries a generation that the cleanup claims.
	compactCancelMu.Lock()
	compactCancel = cancel
	compactCancelGen++
	gen := compactCancelGen
	compactCancelMu.Unlock()
	return func() {
		compactCancelMu.Lock()
		if compactCancelGen == gen {
			compactCancel = nil
		}
		compactCancelMu.Unlock()
	}
}

// TriggerCancel fires the in-flight compaction cancellation if one is active.
// Calling it when no compaction is running is a safe no-op.
//
// The cycle-237 TUI wires this into the Esc handler in a future cycle
// (see 硬約束 in docs/cycles/cycle-237-compact/deliverable.md).
func TriggerCancel() {
	compactCancelMu.Lock()
	cancel := compactCancel
	compactCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ResetForTest clears the cancel slot and its generation.
//
// The slot is package state, so it outlives the compaction that installed it:
// a test that fires Esc and reads the result channel without waiting for the
// worker's deferred cleanup leaves the slot pointing at a cancel from a
// compaction that is already over. The next test's Esc then fires *that*, and
// its "esc to cancel" hint goes dead — which is why the TUI's compaction test
// passed alone but failed once another test had run first.
//
// A test that starts a compaction calls this in t.Cleanup so the slot cannot
// leak past it. It is a test-only seam, named and documented as one, in the
// same shape as clilog.ResetForTest.
func ResetForTest() {
	compactCancelMu.Lock()
	compactCancel = nil
	// Bump rather than rewind. A worker that is still running holds a cleanup
	// closure carrying the generation it installed; if the counter went back
	// to zero the next install would reissue a number that closure recognises,
	// and a compaction that is already over would clear the live slot — the
	// very bug this seam exists to prevent, reintroduced from the other side.
	// Bumping keeps every outstanding cleanup mismatched for good.
	compactCancelGen++
	compactCancelMu.Unlock()
}

// CancelSlotIsEmpty reports whether no compaction currently owns the slot.
//
// ResetForTest's companion: a test that calls t.Cleanup(ResetForTest) can
// assert the slot really is clean afterwards instead of trusting it, which is
// what turns "the leak is fixed" into something the suite checks.
func CancelSlotIsEmpty() bool {
	compactCancelMu.Lock()
	defer compactCancelMu.Unlock()
	return compactCancel == nil
}
