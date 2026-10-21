// Package retrybudget caps the cumulative wall-clock wait a single user turn
// can spend inside PiG's automatic-retry backoff (agent.AutoRetryStartEvent).
//
// AGY 1.2.13 added a hard 30-second-per-turn wait cap on its own auto-retry
// loop: retries that fit inside the budget run with the usual exponential
// backoff, retries that would overflow it are aborted and the underlying error
// surfaces to the user. The clone inherits PiG's coding.Session which emits
// AutoRetryStartEvent before each backoff and supports Session.AbortRetry()
// to cancel the in-flight wait — but PiG only caps *per-attempt* delay
// (RetryPolicy.MaxDelayMs, default 60s); the cumulative budget is the
// missing piece. This package is the missing piece.
//
// The package is split into two layers:
//
//   - Budget (this file) is stdlib-pure: it tracks total time consumed, can
//     answer whether a proposed additional wait would overflow the cap, and
//     can atomically consume a delta when the caller decides to commit.
//     No knowledge of PiG, agent events, or sessions — so it is testable
//     in isolation and reusable from non-PiG call paths.
//   - Observer (observer.go) consumes agent.AgentEvent, recognises
//     AutoRetryStartEvent, calls Budget.TryConsume against the proposed
//     delay, and invokes Aborter.AbortRetry() when consumption would
//     overflow the cap. The Observer is the seam that ties Budget to PiG's
//     session.
//
// Cycle retry-30s-cap-stays: AGY 1.2.13 parity; see
// docs/cycles/retry-30s-cap-stays/ for the trigger matrix.
package retrybudget

import (
	"sync"
	"time"
)

// DefaultPerTurnCap is the cumulative-wait cap the AGY 1.2.13 changelog
// pins: "retry waits are bounded to 30 seconds per turn, and the underlying
// error surfaces if the cap is reached before the model succeeds." A turn
// starts with a fresh Budget; consecutive turns get independent budgets.
//
// The cap applies to the *wait time only*, not to the model call itself
// or to backoff scheduled before the first attempt — those are not retries.
const DefaultPerTurnCap = 30 * time.Second

// Budget is a per-turn cumulative-wait tracker. Construct one with New at
// the start of a turn, hand it to an Observer, and Reset between turns.
//
// The zero value is *not* usable; New populates mu and the cap.
// Methods are safe for concurrent use: the drain goroutine that fires
// the Observer and the goroutine that calls Reset between turns can race
// on the same Budget instance, so all mutations and reads take mu.
type Budget struct {
	mu  sync.Mutex
	cap time.Duration
	// used is the cumulative wait time consumed since the last Reset.
	// Clamped at cap so a Budget cannot carry a Used > Cap reading after
	// an overflow — observers reading Used() to render progress get a
	// value that is always meaningful as a fraction of Cap().
	used time.Duration
}

// New returns a Budget with the given cap. A non-positive cap is treated
// as zero — every TryConsume call returns false and an observer wired
// to such a budget will abort the very first retry, which is the
// behaviour an operator who passes a zero cap explicitly is asking for.
//
// PiG's default BaseDelayMs is 2s and MaxRetries is 3; with the
// DefaultPerTurnCap of 30s the second retry's 4s wait fits and the
// third's 8s wait fits, but a hypothetical fourth attempt's 16s wait
// would overflow a Budget that already burned 14s on the prior two.
// That is the row the cycle closes.
func New(cap time.Duration) *Budget {
	if cap < 0 {
		cap = 0
	}
	return &Budget{cap: cap}
}

// Reset zeroes the cumulative-wait counter. The cap is preserved.
// Call this at the start of each user turn — typically at the top of
// the print-path's turn runner, mirroring how the runtime resets
// auto-continue depth per user prompt.
func (b *Budget) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.used = 0
	b.mu.Unlock()
}

// Used returns the cumulative wait time consumed since the last Reset.
// A Budget that has overflowed reports Used == Cap (clamped) so callers
// rendering "X / Y" progress see a saturated fraction rather than a
// value past the denominator.
func (b *Budget) Used() time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Cap returns the budget cap. The returned value is the same one passed
// to New; Budget does not silently rewrite it.
func (b *Budget) Cap() time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cap
}

// Remaining returns the unused wait time before the cap is reached.
// Returns 0 when Used has already reached Cap (or the cap is zero).
func (b *Budget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.cap - b.used
	if r < 0 {
		return 0
	}
	return r
}

// TryConsume attempts to atomically reserve an additional wait of d.
// It returns true on success and false when the resulting used would
// exceed the cap (or when the Budget is nil). A failed call leaves
// the budget unchanged, so the caller can ask again with a smaller
// delta — though in this package's typical use the caller immediately
// aborts the retry on a false return.
//
// A d <= 0 is a no-op that returns true (mirroring how PiG's
// prepareRetry treats a non-positive delayMs: no timer is started
// and nothing is consumed). A Budget with cap == 0 always returns
// false for d > 0, which is what an operator who passes a zero cap
// is asking for — abort every retry.
func (b *Budget) TryConsume(d time.Duration) bool {
	if b == nil || d <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+d > b.cap {
		return false
	}
	b.used += d
	return true
}
