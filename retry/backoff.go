// Package retry is the clone's canonical retry/backoff helper.
//
// Background. AGY 1.3.4 (changelog line 29) lowered the per-retry backoff
// cap from 4 minutes to 30 seconds so a single stuck upstream request can no
// longer monopolise the user's session for several minutes at a time:
//
//	"Cancelled requests are retried with the maximum retry time lowered
//	 from 4 minutes to 30 seconds so a single stuck request can't
//	 monopolise the user's session for several minutes."
//
// AGY 1.3.6 (line 5) repeats the wording because the change was silent in
// earlier patch releases. This package makes that cap a first-class
// constant in the clone: every retry/backoff path the clone owns (or
// routes through PiG's coding session) consults MaxInterval, so the cap
// is the same number wherever the backoff is applied.
//
// What the package owns:
//
//   - The constants (MaxInterval, InitialInterval, Multiplier, MaxAttempts)
//     that describe the canonical backoff schedule.
//   - A small Backoff struct that takes a starting attempt index and
//     returns the next sleep duration, or sleeps until the duration
//     elapses / the context is cancelled.
//   - A one-call helper (ComputeDelay) for code that doesn't want the
//     stateful struct.
//
// What the package deliberately does NOT own:
//
//   - Provider / SDK retry configuration. That is owned by PiG
//     (github.com/MichaelKinsy/PiG/ai.ConfigureProviderRetry) and the
//     clone passes MaxInterval into it via pigrt.BuildRetryPolicy. The
//     cap is the same number — 30 s — because it is the same policy.
//   - The agent retry budget. That is the user-configurable count
//     (defaults to 3 in PiG, the upstream AGY uses 5). pigrt.BuildRetryPolicy
//     honours MaxAttempts here so the clone stays at 5 even when
//     settings.json is absent.
//
// The shape mirrors PiG's assistant_retry.go (RetryPolicy{MaxRetries,
// BaseDelayMs, MaxAgentDelayMs}) — exponential growth with a hard cap,
// starting at InitialInterval, growing by Multiplier per attempt, never
// exceeding MaxInterval.
package retry

import (
	"context"
	"errors"
	"time"
)

// MaxInterval is the per-retry backoff cap. It is the value AGY 1.3.4+
// uses: every exponential growth step clamps to this number so a
// runaway backoff cannot park the session on a stuck upstream for
// several minutes.
//
// Pre-1.3.4 the cap was 4 * time.Minute (240 000 ms). The clone never
// had the 4-minute cap, but it inherited PiG's 60 s default which is
// still twice the AGY target; the constants below bring both numbers
// in line with the AGY 1.3.4 changelog.
const MaxInterval = 30 * time.Second

// MaxIntervalMs is MaxInterval expressed in milliseconds. PiG's
// provider retry transport reads its cap from a settings JSON key in
// milliseconds (`retry.provider.maxRetryDelayMs`); this constant
// keeps the unit conversion out of every reader.
const MaxIntervalMs = 30000

// InitialInterval is the delay before the first retry. AGY kept this
// unchanged across the 1.3.4 cut; the changelog explicitly calls out
// "Initial retry wait (1s) ... unchanged".
const InitialInterval = 1 * time.Second

// InitialIntervalMs is InitialInterval in milliseconds for the same
// reason as MaxIntervalMs — settings.json keys are in milliseconds.
const InitialIntervalMs = 1000

// Multiplier is the growth factor between successive retries. 2.0 is
// the doubling the AGY changelog names ("exponential multiplier ... unchanged").
const Multiplier = 2.0

// MaxAttempts is the retry budget. AGY's effective budget is 5; the
// 1.3.4 changelog says only the cap changes, not the budget, so the
// clone keeps 5 here as the source of truth.
const MaxAttempts = 5

// ErrAttemptOutOfRange is returned by ComputeDelay when asked for an
// attempt index outside [0, MaxAttempts). It is a programmer error:
// the caller usually has a loop counter that should never exceed the
// budget, and surfacing it is more useful than silently capping.
var ErrAttemptOutOfRange = errors.New("retry: attempt index out of range")

// ComputeDelay returns the delay that precedes retry attempt `attempt`
// (zero-indexed: attempt=0 is the first retry, after the initial
// failure). It is the one-call helper for code that doesn't want the
// stateful struct: callers compute the duration and apply it
// themselves, usually with a context-aware sleep.
//
// The schedule is exponential with Multiplier, clamped to MaxInterval:
//
//	attempt 0 -> InitialInterval (1s)
//	attempt 1 -> 2s
//	attempt 2 -> 4s
//	attempt 3 -> 8s
//	attempt 4 -> 16s
//	attempt 5 -> 30s (clamped from 32s, MaxAttempts is the upper bound)
//
// Negative attempts return ErrAttemptOutOfRange. Attempts ≥ MaxAttempts
// also return ErrAttemptOutOfRange — the budget is enforced at the
// caller, but ComputeDelay refuses to compute a delay for an attempt
// the budget would never allow.
func ComputeDelay(attempt int) (time.Duration, error) {
	if attempt < 0 || attempt >= MaxAttempts {
		return 0, ErrAttemptOutOfRange
	}
	return nextDelay(attempt), nil
}

// Cap returns d clamped to [0, MaxInterval]. Negative inputs are
// returned unchanged so a caller that passes a "no sleep" sentinel
// (time.Duration(0) or a negative context-bounded value) keeps its
// original meaning — the cap only flattens the upper end of the
// schedule.
func Cap(d time.Duration) time.Duration {
	if d < 0 {
		return d
	}
	if d > MaxInterval {
		return MaxInterval
	}
	return d
}

// Backoff is the stateful iterator. New(0) starts at the first retry's
// delay; Next() advances the cursor and returns the next delay. The
// cursor is bounded by MaxAttempts — Next returns false once the
// budget is exhausted, so callers can use it as a `for b.Next()` loop
// without an index variable of their own.
//
// Backoff is safe for single-goroutine use only. Concurrency is the
// caller's problem; the struct is three integers and a duration.
type Backoff struct {
	attempt int
}

// New returns a Backoff positioned at attempt 0. The first call to
// Next returns InitialInterval (the delay before the first retry).
func New() *Backoff {
	return &Backoff{attempt: 0}
}

// Next returns the delay for the next retry attempt, then advances
// the cursor. The boolean is false once the cursor has reached
// MaxAttempts — the caller has exhausted the budget.
//
// The delay is exponential with Multiplier, clamped to MaxInterval.
// Negative attempts are not reachable from Next; the cursor only
// advances forward, so the returned delay is always >= 0.
func (b *Backoff) Next() (time.Duration, bool) {
	if b.attempt >= MaxAttempts {
		return 0, false
	}
	d := nextDelay(b.attempt)
	b.attempt++
	return d, true
}

// Attempt returns the cursor's current position. It is the value the
// NEXT call to Next will use as its attempt index, which is also the
// 0-based count of how many retries have already happened.
func (b *Backoff) Attempt() int { return b.attempt }

// Wait blocks for the delay returned by Next, or until ctx is
// cancelled (whichever comes first). It is sugar for the
// `delay, ok := b.Next(); if !ok { return ErrBudgetExhausted };
// return sleep(ctx, delay)` pattern that the auto-continue and
// provider-retry sites use.
//
// On a cancelled context, Wait returns ctx.Err() so the caller can
// distinguish a user-driven abort from a budget exhaustion.
func (b *Backoff) Wait(ctx context.Context) error {
	d, ok := b.Next()
	if !ok {
		return ErrBudgetExhausted
	}
	return Sleep(ctx, d)
}

// Reset rewinds the cursor to 0. Useful for one-shot loops that
// reuse the struct across multiple error categories, or for tests
// that need a deterministic starting position.
func (b *Backoff) Reset() { b.attempt = 0 }

// ErrBudgetExhausted is returned by Backoff.Wait when the budget has
// been used up. Callers that want a typed error to match against can
// import it directly; the auto-continue path treats it the same as a
// non-retryable error.
var ErrBudgetExhausted = errors.New("retry: budget exhausted")

// Sleep blocks for d or until ctx is cancelled. A negative or zero
// duration returns nil immediately (no sleep, no error) — that is the
// "first attempt has no delay" semantics a caller expects when the
// backoff object decides no wait is needed.
//
// On a cancelled context, Sleep returns ctx.Err() and the goroutine
// returns without blocking for the remainder of d.
func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		// Still honour cancellation so a tight loop with d=0 cannot
		// starve the runtime's cancellation signal.
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Step returns the wait for attempt `attempt` (zero-indexed) under an
// exponential schedule: initial * 2^attempt, clamped to cap.
//
// It is the one schedule every backoff in the clone shares: the
// canonical one (InitialInterval, MaxInterval), the connection retry's
// and the stall recovery's. The three used to keep their own copy of
// the same doubling loop.
//
// A negative attempt is clamped to 0. A non-positive initial falls back
// to InitialInterval and a non-positive cap to MaxInterval, so a caller
// that passes a zero knob gets the canonical schedule rather than a
// zero wait.
//
// The doubling stops at the cap, so the loop cannot overflow: the delay
// is at most cap*2 when the comparison runs.
func Step(attempt int, initial, cap time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if initial <= 0 {
		initial = InitialInterval
	}
	if cap <= 0 {
		cap = MaxInterval
	}
	delay := initial
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= cap {
			return cap
		}
	}
	if delay > cap {
		return cap
	}
	return delay
}

// nextDelay is the canonical schedule's single source of truth: it is
// Step with the canonical knobs.
func nextDelay(attempt int) time.Duration {
	return Step(attempt, InitialInterval, MaxInterval)
}
