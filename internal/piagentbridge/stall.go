// Package piagentbridge — stall.go: 5-minute idle-detection on a streaming
// response, plus the retry-with-30s-backoff-cap that recovery prompts hand
// to a retrying caller. Both pieces live here so the native provider's pump
// and the StreamFn bridge pump wire them through exactly the same primitives
// (see gatewayprovider.go pump and stream.go pump).
//
// Why a dedicated watcher: AGY 1.3.6 changelog line 5 + 1.3.4 line 29 —
// "Added a 5-minute stall detection for stuck API requests. When the API
// hasn't sent any events for 5 minutes the request is cancelled, and a
// recovery prompt is shown offering to retry once or continue. Cancelled
// requests are retried with the maximum retry time lowered from 4 minutes
// to 30 seconds so a single stuck request can't monopolise the user's
// session for several minutes."
//
// The watcher is intentionally transport-neutral: it sees `events` of any
// shape (the caller passes an opaque idler-reset), and the only thing it
// emits is a context.Cancel when no event has arrived inside the configured
// idle window. Both providers then route that cancellation into their own
// end-of-stream logic — the native pump's state.finish reports a StopReasonAborted
// with a "stalled after Ns" message; the bridge pump emits an ErrorEvent the
// same way.
//
// The recovery prompt and the retry helper are also here because they are
// the only two consumers of "the request got cancelled by the stall watcher".
// Keeping them with the watcher means cmd/wagyu's TUI/print paths don't reach
// into piagentbridge internals to render the prompt or to back off a retry.
package piagentbridge

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/justwasm/pigasus/retry"
)

// StallTimeout is the canonical idle window the watcher's default uses.
// Five minutes matches the AGY 1.3.6 changelog entry verbatim; tests
// override it with a much smaller value.
const StallTimeout = 5 * time.Minute

// StallRecoveryBackoffCap is the maximum per-attempt backoff the retry
// helper waits between cancelled-request retries. AGY 1.3.4 line 29 —
// "lowered from 4 minutes to 30 seconds". The cap is exported because
// cmd/wagyu's TUI prompt formats it into the user-facing copy.
const StallRecoveryBackoffCap = 30 * time.Second

// StallRecoveryInitialBackoff is the starting backoff before the cap
// starts clamping. Two seconds mirrors PiG's own retry seed.
const StallRecoveryInitialBackoff = 2 * time.Second

// ErrStallCancelled is what the watcher attaches to the context it cancels
// when the idle window expires. Callers that need to distinguish "user hit
// Ctrl-C" from "the upstream stalled" read this with errors.Is.
var ErrStallCancelled = errors.New("piagentbridge: stall timeout, no event for " + StallTimeout.String())

// StallWatcher cancels its context after Timeout elapses with no event.
//
// The watcher is started by Start and torn down by Stop. Note resets the
// idle timer to Timeout from "now"; feeding events at intervals shorter
// than Timeout keeps the underlying request alive indefinitely.
//
// A watcher outliving its caller is safe: Stop closes the timer goroutine
// and waits for it to drain, so a hot Stream that races Stop never leaks
// a goroutine. Cancel-on-fire is idempotent (context.CancelFunc tolerates
// repeated calls) so two timers racing are harmless.
type StallWatcher struct {
	// Timeout is the idle window. A zero value is replaced with
	// StallTimeout on first use, so a literal `&StallWatcher{}` is
	// usable in tests that mean "the AGY default".
	Timeout time.Duration

	// OnStall, when non-nil, is invoked synchronously from the timer
	// goroutine *just before* the watcher cancels the context. The
	// callback runs with the watcher's mutex released, so it may
	// safely call back into the provider. A nil callback is fine —
	// most callers learn about the stall through context.Err().
	OnStall func(elapsed time.Duration)

	parent context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	timer   *time.Timer
	stopped bool
	fired   bool
}

// StartStallWatcher wires the watcher into parent and returns it. The
// returned cancel function is also exposed so callers that want to abort
// early (e.g. user Ctrl-C) do not have to reach through the watcher.
//
// Start does not begin counting idle time — call Note on the first event.
// The intent: a request that opens and stalls before its first byte still
// counts as a stall, but the timer is reset to Timeout from "Note, not
// Start", so a caller that pumps a pre-flight setup phase before the
// stream opens is not penalised for setup latency.
func StartStallWatcher(parent context.Context) (context.Context, *StallWatcher) {
	return StartStallWatcherWith(parent, StallTimeout, nil)
}

// StartStallWatcherWith is StartStallWatcher with explicit timeout and
// callback. Tests use it to shrink the window and to observe firing.
func StartStallWatcherWith(parent context.Context, timeout time.Duration, onStall func(time.Duration)) (context.Context, *StallWatcher) {
	if timeout <= 0 {
		timeout = StallTimeout
	}
	ctx, cancel := context.WithCancel(parent)
	w := &StallWatcher{
		Timeout: timeout,
		OnStall: onStall,
		parent:  parent,
		cancel:  cancel,
	}
	// Arm the timer immediately so an idle stream (no events at all)
	// still gets cancelled. Note() resets the deadline; Stop() drains.
	w.timer = time.AfterFunc(timeout, w.fire)
	return ctx, w
}

// Note records that an event arrived "now" and resets the idle timer.
// It is the only method the watcher's hot path calls. A Note after
// Stop is a no-op; Note after the timer fired is also a no-op so a
// late-arriving event does not un-cancel an already-cancelled stream.
func (w *StallWatcher) Note() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.fired {
		return
	}
	if w.timer != nil {
		w.timer.Reset(w.Timeout)
	}
}

// Fired reports whether the idle timer has already cancelled the
// stream. The transport uses it to avoid double-cancelling when
// context.Err() and watcher.Fired() tell the same story.
func (w *StallWatcher) Fired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fired
}

// Stop cancels the watcher without firing. It is safe to call more than
// once and after the timer has already fired; the underlying timer is
// drained so a concurrent fire never races a Stop caller.
func (w *StallWatcher) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	timer := w.timer
	w.mu.Unlock()
	if timer != nil {
		if !timer.Stop() {
			// Drain the channel so the timer's goroutine exits.
			// Reset ensures any pending fire does not reach the
			// callback after Stop returned.
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

// fire is the timer callback. It cancels the parent context, fires the
// OnStall hook (if any), and is idempotent.
func (w *StallWatcher) fire() {
	w.mu.Lock()
	if w.stopped || w.fired {
		w.mu.Unlock()
		return
	}
	w.fired = true
	callback := w.OnStall
	w.mu.Unlock()
	if callback != nil {
		callback(w.Timeout)
	}
	w.cancel()
}

// ─── backoff helper ───────────────────────────────────────────────────────────

// BackoffStep returns the wait for attempt n (zero-indexed) using
// exponential growth: initial * 2^n, capped at cap. The 30-second cap is
// the AGY 1.3.4 changelog value; callers that need a different cap pass
// it explicitly so BackoffStep stays a pure function and stays unit-
// testable without time.Sleep.
//
// The schedule is retry's Step — the clone's one exponential
// backoff, shared with the canonical schedule and the connection
// retry's.
func BackoffStep(attempt int, initial, cap time.Duration) time.Duration {
	if initial <= 0 {
		initial = StallRecoveryInitialBackoff
	}
	if cap <= 0 {
		cap = StallRecoveryBackoffCap
	}
	return retry.Step(attempt, initial, cap)
}

// RetryWithBackoff runs fn at most maxAttempts times. After a non-nil
// error it waits BackoffStep before the next attempt, capped at
// StallRecoveryBackoffCap (30s). The supplied context can cut the wait
// short — a Ctrl-C during a retry leaves without delivering the recovery.
//
// fn is responsible for distinguishing "stalled" from any other failure;
// RetryWithBackoff retries on every non-nil return. Callers that need to
// bail on specific errors (a 401 that retries cannot fix, say) compose
// their own check inside fn.
func RetryWithBackoff(ctx context.Context, maxAttempts int, fn func() error) error {
	return RetryWithBackoffConfig(ctx, DefaultBackoffConfig(), maxAttempts, fn)
}

// DefaultBackoffConfig returns the AGY 1.3.4 production values:
// initial 2s, cap 30s. Exported so callers building their own
// recovery wrapper can use the same numbers without restating the
// constants.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		Initial: StallRecoveryInitialBackoff,
		Cap:     StallRecoveryBackoffCap,
	}
}

// BackoffConfig is the knob set for the retry helper. Zero values fall
// back to the AGY production defaults (2s initial, 30s cap) so a
// literal `BackoffConfig{}` is usable in callers that want the
// canonical behaviour.
type BackoffConfig struct {
	Initial time.Duration
	Cap     time.Duration
}

// RetryWithBackoffConfig is RetryWithBackoff with an explicit backoff
// configuration. Tests use it to drive the cap value down so they run
// in milliseconds rather than seconds; production callers go through
// RetryWithBackoff.
func RetryWithBackoffConfig(ctx context.Context, cfg BackoffConfig, maxAttempts int, fn func() error) error {
	if cfg.Initial <= 0 {
		cfg.Initial = StallRecoveryInitialBackoff
	}
	if cfg.Cap <= 0 {
		cfg.Cap = StallRecoveryBackoffCap
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt+1 == maxAttempts {
			break
		}
		wait := BackoffStep(attempt, cfg.Initial, cfg.Cap)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}
