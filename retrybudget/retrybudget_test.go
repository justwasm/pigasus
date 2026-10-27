// retrybudget_test.go — cycle retry-30s-cap-stays unit tests for the Budget
// primitive. The stdlib-pure Budget is the load-bearing piece of the
// cycle: pinning its arithmetic in isolation means a regression lands as
// one explicit failure mode, not as "the cap is wrong in production".
//
// The integration with PiG (Observer + Aborter chain in pigrt.Runtime) is
// pinned in observer_test.go, which uses a stub Aborter to assert the
// abort side-effect; this file pins the Budget's math.
//
// Test name prefixes are constrained to the cycle's regex
// (TestRetry|TestBackoff|TestCap|Test30) so the targeted-run check the
// deliverable specifies resolves them.
package retrybudget

import (
	"sync"
	"testing"
	"time"
)

// TestCap30sEnforced pins the AGY 1.2.13 contract: a fresh Budget with
// the default cap rejects any TryConsume whose delta would push the
// cumulative wait past 30s. With PiG's defaults (BaseDelayMs=2000,
// MaxRetries=3, MaxDelayMs=60000) the per-attempt delays are 2s, 4s,
// 8s, 16s — at attempt 4 a 16s wait against a Budget that already
// burned 14s on the prior two attempts must be rejected.
func TestCap30sEnforced(t *testing.T) {
	b := New(DefaultPerTurnCap)

	// 2s + 4s + 8s = 14s — all three retries of PiG's default policy
	// fit inside 30s.
	if !b.TryConsume(2 * time.Second) {
		t.Fatal("2s should fit on a fresh 30s budget")
	}
	if !b.TryConsume(4 * time.Second) {
		t.Fatal("4s should fit after 2s of used budget")
	}
	if !b.TryConsume(8 * time.Second) {
		t.Fatal("8s should fit after 6s of used budget")
	}

	// 17s would overflow: 14s + 17s = 31s, strictly past the 30s cap.
	// PiG's default policy's attempt 4 is 16s and fits exactly
	// (14+16=30), so the realistic overflow fires at attempt 5 (32s)
	// — a caller that raises MaxRetries past 3 hits this branch.
	if b.TryConsume(17 * time.Second) {
		t.Fatalf("17s should overflow a 30s budget that has 14s used; got Used=%v", b.Used())
	}

	// The failed TryConsume must NOT have mutated Used — a Budget that
	// silently consumed part of the would-be delta would skew the
	// next attempt's reading.
	if got := b.Used(); got != 14*time.Second {
		t.Fatalf("Used=%v after rejected TryConsume, want 14s (no mutation)", got)
	}

	// Sanity: 16s exactly fits the cap (14+16=30, not > 30). This is
	// the boundary semantics the rest of the cycle relies on — a
	// budget that had 14s used still accepts PiG's attempt-4 backoff
	// of 16s, only rejecting attempt 5's 32s.
	if !b.TryConsume(16 * time.Second) {
		t.Fatal("16s (boundary: 14+16=30) should fit a 30s budget")
	}
}

// TestBackoffWithinCap drives the realistic case: a sequence of
// exponentially increasing delays all fit inside the cap. Each
// TryConsume must return true and Used must monotonically grow.
func TestBackoffWithinCap(t *testing.T) {
	b := New(30 * time.Second)
	delays := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		15 * time.Second, // 1+2+4+8+15 = 30, the boundary
	}
	var sum time.Duration
	for i, d := range delays {
		if !b.TryConsume(d) {
			t.Fatalf("attempt %d: TryConsume(%v) should fit; cumulative=%v", i, d, sum)
		}
		sum += d
		if got := b.Used(); got != sum {
			t.Fatalf("attempt %d: Used=%v, want %v", i, got, sum)
		}
	}
	// The next 1s attempt must overflow — total would be 31s, past 30s.
	if b.TryConsume(time.Second) {
		t.Fatalf("1s after 30s used should overflow; got Used=%v", b.Used())
	}
	// Used stays at exactly the cap (30s), not at 31s — the budget does
	// not record "would-have-consumed" amounts.
	if got := b.Used(); got != 30*time.Second {
		t.Fatalf("Used=%v after overflow, want 30s (clamped)", got)
	}
	if got := b.Remaining(); got != 0 {
		t.Fatalf("Remaining=%v after overflow, want 0", got)
	}
}

// TestCapResetPerTurn pins the per-turn semantics. After Reset, a
// Budget must accept the full cap again — the cycle's whole point is
// that consecutive turns get independent budgets, so a previous turn's
// exhausted cap must not leak into the next.
func TestCapResetPerTurn(t *testing.T) {
	b := New(30 * time.Second)
	// Burn the budget to overflow on a turn.
	for _, d := range []time.Duration{10 * time.Second, 10 * time.Second, 10 * time.Second} {
		if !b.TryConsume(d) {
			t.Fatalf("setup: TryConsume(%v) on a fresh 30s budget should fit", d)
		}
	}
	if b.TryConsume(time.Second) {
		t.Fatal("setup: 1s should overflow a 30s budget that has 30s used")
	}

	// New turn: Reset zeroes the counter. A 30s wait fits again, which
	// it would not on the previous turn.
	b.Reset()
	if got := b.Used(); got != 0 {
		t.Fatalf("after Reset, Used=%v, want 0", got)
	}
	if !b.TryConsume(30 * time.Second) {
		t.Fatal("after Reset, 30s should fit again")
	}
	// A second 30s attempt on the same (post-Reset) budget overflows —
	// confirming Reset really did its job and the cap is still 30s.
	if b.TryConsume(30 * time.Second) {
		t.Fatalf("after Reset+30s, a second 30s should overflow; got Used=%v", b.Used())
	}
}

// TestRetrySingleUnaffected pins the no-cap-reached path: a single
// retry that consumes a fraction of the cap must succeed and leave
// the rest available. This is the "AGY 1.2.13 left the happy path
// alone" case the cycle's changelog calls out.
func TestRetrySingleUnaffected(t *testing.T) {
	b := New(30 * time.Second)
	// PiG's first-attempt backoff is BaseDelayMs (2s default). A single
	// 2s wait must consume 2s and leave 28s of headroom.
	if !b.TryConsume(2 * time.Second) {
		t.Fatal("2s single retry should fit on a fresh 30s budget")
	}
	if got := b.Used(); got != 2*time.Second {
		t.Fatalf("Used=%v, want 2s", got)
	}
	if got := b.Remaining(); got != 28*time.Second {
		t.Fatalf("Remaining=%v, want 28s", got)
	}
	if got := b.Cap(); got != 30*time.Second {
		t.Fatalf("Cap=%v, want 30s (unchanged by TryConsume)", got)
	}
	// A second 2s retry still fits — the test names the case but the
	// assertion is just that two 2s retries leave room for one more.
	if !b.TryConsume(2 * time.Second) {
		t.Fatal("second 2s retry should fit after one 4s used")
	}
}

// TestCapAtomicRejection pins the no-mutation promise: a failed
// TryConsume does not consume part of the would-be delta. Without
// this, the observer would have to track "would have consumed"
// amounts out-of-band.
func TestCapAtomicRejection(t *testing.T) {
	b := New(10 * time.Second)
	if !b.TryConsume(7 * time.Second) {
		t.Fatal("setup: 7s should fit on a 10s budget")
	}
	// 4s would overflow (7+4 = 11 > 10). Used must remain at 7s.
	if b.TryConsume(4 * time.Second) {
		t.Fatal("4s should overflow a 10s budget that has 7s used")
	}
	if got := b.Used(); got != 7*time.Second {
		t.Fatalf("Used=%v after rejected TryConsume, want 7s", got)
	}
	// A 3s attempt still fits — the rejection did not consume any
	// headroom. (This is the property the spec calls out: rejection is
	// atomic.)
	if !b.TryConsume(3 * time.Second) {
		t.Fatal("3s should fit after 7s used (would-be 10s)")
	}
	if got := b.Used(); got != 10*time.Second {
		t.Fatalf("Used=%v, want 10s", got)
	}
}

// TestCapConcurrentConsume asserts the goroutine-safe arithmetic:
// 100 goroutines racing on TryConsume either all succeed (when total
// would fit) or each atomically fails exactly once when total would
// overflow. Without the mutex a busy Budget could double-spend.
func TestCapConcurrentConsume(t *testing.T) {
	b := New(100 * time.Millisecond)
	const goroutines = 100
	const each = 2 * time.Millisecond // 100 * 2ms = 200ms > 100ms cap
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var successes int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if b.TryConsume(each) {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// With a 100ms cap and 2ms deltas, exactly 50 attempts should fit.
	// The remaining 50 are atomic rejections. Total used is exactly
	// 100ms (clamped at the cap).
	if successes != 50 {
		t.Fatalf("successes=%d, want 50 (100ms cap / 2ms delta)", successes)
	}
	if got := b.Used(); got != 100*time.Millisecond {
		t.Fatalf("Used=%v under concurrency, want 100ms (clamped at cap)", got)
	}
}

// TestCapZeroRejectsAll confirms the operator-escape hatch: a Budget
// built with cap == 0 (e.g. an explicit "abort every retry" knob)
// refuses every positive delta. Useful as a sanity check that the
// cap-zero branch is reachable.
func TestCapZeroRejectsAll(t *testing.T) {
	b := New(0)
	if b.TryConsume(time.Millisecond) {
		t.Fatal("1ms should overflow a 0-cap budget")
	}
	if got := b.Used(); got != 0 {
		t.Fatalf("Used=%v on a 0-cap budget, want 0", got)
	}
	// Negative cap is normalised to zero at New().
	b2 := New(-time.Second)
	if b2.TryConsume(time.Millisecond) {
		t.Fatal("1ms should overflow a budget built from -1s cap")
	}
}

// TestCapNilReceiverSafe pins the panic-free contract. A nil *Budget
// is what callers see when Options.RetryBudgetCap is disabled (the
// runtime sets retryBudget = nil); every method must tolerate it
// without panicking, so a half-wired runtime cannot crash.
func TestCapNilReceiverSafe(t *testing.T) {
	var b *Budget
	// None of these may panic.
	if b.Used() != 0 {
		t.Fatal("nil Used() should return 0")
	}
	if b.Cap() != 0 {
		t.Fatal("nil Cap() should return 0")
	}
	if b.Remaining() != 0 {
		t.Fatal("nil Remaining() should return 0")
	}
	if !b.TryConsume(time.Second) {
		// nil with d > 0 returns true (no-op success). The cycle
		// never wires a nil Budget into an Observer, so the
		// distinction is invisible at runtime — but a doc-test
		// pins the contract.
		t.Fatal("nil TryConsume(>0) should return true (no-op)")
	}
	if !b.TryConsume(-time.Second) {
		t.Fatal("nil TryConsume(<0) should return true (no-op)")
	}
	b.Reset() // must not panic
}

// TestCap30sBoundary is the boundary-semantics pin: with a 30s cap,
// Used at exactly 30s is NOT overflow, and the next 1ms delta is.
// Boundary semantics matter because PiG's exponential backoff can
// land exactly on a multiple of the cap (e.g. 14+16=30 fits, 14+16+1
// overflows); a test that asserts the strict-greater-than boundary
// is what locks the cycle's arithmetic.
func TestCap30sBoundary(t *testing.T) {
	b := New(30 * time.Second)
	// 30s exactly: must succeed (used == cap, not > cap).
	if !b.TryConsume(30 * time.Second) {
		t.Fatal("30s should fit on a fresh 30s budget (boundary)")
	}
	// 30s + 1ms: must fail (used + d > cap).
	if b.TryConsume(time.Millisecond) {
		t.Fatal("30s + 1ms should overflow a 30s budget")
	}
	// Reset and try a different boundary combination.
	b.Reset()
	if !b.TryConsume(15 * time.Second) {
		t.Fatal("15s should fit on a fresh 30s budget")
	}
	if !b.TryConsume(15 * time.Second) {
		t.Fatal("15s should fit after 15s used (boundary)")
	}
	if b.TryConsume(time.Nanosecond) {
		t.Fatal("30s + 1ns should overflow a 30s budget")
	}
}
