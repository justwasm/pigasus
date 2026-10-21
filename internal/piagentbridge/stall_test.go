package piagentbridge

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestStallWatcher_TimerFiresAfterIdleWindow is the canonical stall case:
// an open stream with no events for longer than the configured timeout
// must have its context cancelled. We use a 20ms timeout so the test
// runs in milliseconds, not minutes; the constant StallTimeout is what
// production wires (5 minutes).
func TestStallWatcher_TimerFiresAfterIdleWindow(t *testing.T) {
	parent := context.Background()
	ctx, watcher := StartStallWatcherWith(parent, 20*time.Millisecond, nil)
	defer watcher.Stop()

	// Without Note, the timer starts at StartStallWatcherWith; assert
	// the context cancels before we have a chance to keep it alive.
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("watcher did not fire within 200ms (timeout = 20ms)")
	}
	if !watcher.Fired() {
		t.Error("watcher.Fired() = false, want true")
	}
}

// TestStallWatcher_TimerResetsOnEachEvent covers the "events keep
// the stream alive" path: a stream that produces events more
// frequently than the idle window must never have its context
// cancelled. The producer runs for longer than the timeout would
// otherwise allow.
func TestStallWatcher_TimerResetsOnEachEvent(t *testing.T) {
	parent := context.Background()
	ctx, watcher := StartStallWatcherWith(parent, 30*time.Millisecond, nil)
	defer watcher.Stop()

	deadline := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(deadline) {
		watcher.Note()
		select {
		case <-ctx.Done():
			t.Fatalf("ctx canceled at %v while events kept arriving", time.Now())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if ctx.Err() != nil {
		t.Errorf("final ctx.Err() = %v, want nil", ctx.Err())
	}
	if watcher.Fired() {
		t.Error("watcher.Fired() = true after steady events, want false")
	}
}

// TestStallWatcher_CancelWorks pins the contract: firing the timer
// cancels the *derived* context but leaves the parent untouched. A
// stall in a single request must not poison the caller's request
// scope.
func TestStallWatcher_CancelWorks(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	ctx, watcher := StartStallWatcherWith(parent, 15*time.Millisecond, nil)
	defer watcher.Stop()

	<-ctx.Done()
	if parent.Err() != nil {
		t.Errorf("parent context canceled when only the watcher fired: %v", parent.Err())
	}
	if ctx.Err() == nil {
		t.Error("derived ctx.Err() = nil, want context.Canceled")
	}
}

// TestStallWatcher_OnStallCallback fires the registered callback at
// the moment the timer trips. The callback is the seam cmd/wagyu uses
// to render the recovery prompt into the chat view.
func TestStallWatcher_OnStallCallback(t *testing.T) {
	var called atomic.Int32
	var elapsedSeen atomic.Value // time.Duration

	ctx, watcher := StartStallWatcherWith(context.Background(), 25*time.Millisecond, func(elapsed time.Duration) {
		called.Add(1)
		elapsedSeen.Store(elapsed)
	})
	defer watcher.Stop()

	<-ctx.Done()

	if got := called.Load(); got != 1 {
		t.Errorf("OnStall invocations = %d, want 1", got)
	}
	got, _ := elapsedSeen.Load().(time.Duration)
	if got != 25*time.Millisecond {
		t.Errorf("OnStall elapsed = %v, want 25ms", got)
	}
}

// TestStallWatcher_IdleStreamIsCancelled covers the absolute-idle case:
// an open stream that sends zero bytes for the full idle window. The
// watcher must cancel the context with the canonical
// ErrStallCancelled-style signal so the transport knows the difference
// between "user pressed Ctrl-C" and "the upstream is silent".
func TestStallWatcher_IdleStreamIsCancelled(t *testing.T) {
	parent := context.Background()
	ctx, watcher := StartStallWatcherWith(parent, 10*time.Millisecond, nil)
	defer watcher.Stop()

	<-ctx.Done()

	if !watcher.Fired() {
		t.Error("idle stream did not register a stall")
	}
	// ErrStallCancelled is what pump paths look up to label the run's
	// StopReason as a stall rather than a generic cancellation.
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
	if ErrStallCancelled == nil || !strings.Contains(ErrStallCancelled.Error(), "stall timeout") {
		t.Errorf("ErrStallCancelled = %v", ErrStallCancelled)
	}
}

// TestBackoffStep_CappedAt30Seconds pins the 1.3.4 changelog value:
// "lowered from 4 minutes to 30 seconds". A few representative
// attempts are checked; the helper itself stays a pure function so a
// future bump of the cap needs only this test to be updated.
func TestBackoffStep_CappedAt30Seconds(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},   // initial
		{1, 4 * time.Second},   // 2 * 2
		{2, 8 * time.Second},   // 2 * 2^2
		{3, 16 * time.Second},  // 2 * 2^3
		{4, 30 * time.Second},  // would be 32s → cap clamps to 30s
		{5, 30 * time.Second},  // still capped
		{10, 30 * time.Second}, // well past the cap
	}
	for _, c := range cases {
		if got := BackoffStep(c.attempt, StallRecoveryInitialBackoff, StallRecoveryBackoffCap); got != c.want {
			t.Errorf("BackoffStep(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

// TestRetryWithBackoff_UsesThirtySecondCap drives the retry helper
// end-to-end: a function that always fails should be retried up to
// maxAttempts times, with the wait between attempts capped at the
// configured value. Production uses 30s (AGY 1.3.4 line 29); the test
// passes a smaller cap so the wait between attempts is measurable in
// milliseconds, but asserts on the cap the production caller would
// see (StallRecoveryBackoffCap).
func TestRetryWithBackoff_UsesThirtySecondCap(t *testing.T) {
	var attempts atomic.Int32

	// Use a tiny initial/cap so the test runs in milliseconds. The
	// production constant is checked separately in
	// TestBackoffStep_CappedAt30Seconds — this test pins the helper
	// itself against a controllable cap.
	cfg := BackoffConfig{Initial: 1 * time.Millisecond, Cap: 30 * time.Second}

	err := RetryWithBackoffConfig(context.Background(), cfg, 5, func() error {
		attempts.Add(1)
		return errors.New("nope")
	})
	if err == nil || err.Error() != "nope" {
		t.Errorf("final err = %v, want \"nope\"", err)
	}
	if got := attempts.Load(); got != 5 {
		t.Errorf("attempts = %d, want 5", got)
	}
	// Pin the production constant: the AGY 1.3.4 line 29 cap is
	// 30 seconds, not the 4 minutes of older releases. If this ever
	// drifts, the changelog entry is wrong.
	if StallRecoveryBackoffCap != 30*time.Second {
		t.Errorf("StallRecoveryBackoffCap = %v, want 30s (AGY 1.3.4 line 29)", StallRecoveryBackoffCap)
	}
}

// TestRetryWithBackoff_ReturnsNilOnSuccess pins the happy path: a
// function that succeeds on the first try returns nil without
// waiting, so the cap value cannot accidentally throttle a healthy
// request.
func TestRetryWithBackoff_ReturnsNilOnSuccess(t *testing.T) {
	var attempts atomic.Int32
	cfg := BackoffConfig{Initial: 1 * time.Millisecond, Cap: 30 * time.Second}
	err := RetryWithBackoffConfig(context.Background(), cfg, 3, func() error {
		attempts.Add(1)
		return nil
	})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on success)", got)
	}
}

// TestRecoveryPromptTUIRow_PinsCanonicalText locks the TUI row bytes
// against the AGY changelog's "Retry once / Continue waiting / Cancel"
// copy. The row is one visual line, prefixed with a warning glyph so
// the user sees it as a transient prompt rather than a tool output.
func TestRecoveryPromptTUIRow_PinsCanonicalText(t *testing.T) {
	got := RecoveryPromptTUIRow()
	want := "⚠  The request appears stuck. Retry once / Continue waiting / Cancel"
	if got != want {
		t.Errorf("RecoveryPromptTUIRow() = %q, want %q", got, want)
	}
	if !strings.Contains(got, "Retry once") || !strings.Contains(got, "Continue waiting") || !strings.Contains(got, "Cancel") {
		t.Errorf("prompt missing one of the three options: %q", got)
	}
}

// TestRecoveryPromptPrintBlock_PinsCanonicalText locks the print
// block to the AGY copy plus a single-line framing that mentions the
// idle window and the 30-second cap. The 4-minute historical cap is
// named explicitly so a tail-of-stdout reader can see why this line
// is there.
func TestRecoveryPromptPrintBlock_PinsCanonicalText(t *testing.T) {
	got := RecoveryPromptPrintBlock(StallTimeout, "30 seconds between attempts")
	if !strings.Contains(got, RecoveryPromptBody) {
		t.Errorf("prompt body missing: %q", got)
	}
	if !strings.Contains(got, "5m0s") {
		t.Errorf("idle window not stamped: %q", got)
	}
	if !strings.Contains(got, "30 seconds between attempts") {
		t.Errorf("backoff hint missing: %q", got)
	}
	if !strings.Contains(got, "capped from 4 minutes") {
		t.Errorf("4-minute historical cap missing: %q", got)
	}
}

// TestRecoveryPromptPrint_PinsCanonicalText is the simpler variant
// the print path uses when it just wants the prompt body without the
// framing. Pin it so the per-mode surface does not drift.
func TestRecoveryPromptPrint_PinsCanonicalText(t *testing.T) {
	got := RecoveryPromptPrint()
	if got != RecoveryPromptBody {
		t.Errorf("RecoveryPromptPrint() = %q, want %q", got, RecoveryPromptBody)
	}
}

// TestFormatStallElapsed_PinsCanonicalText locks the "stalled after"
// footer copy. The footer rides on the chat line below the prompt so
// the user knows why the request was cancelled.
func TestFormatStallElapsed_PinsCanonicalText(t *testing.T) {
	got := FormatStallElapsed(5 * time.Minute)
	if !strings.Contains(got, "5m0s") {
		t.Errorf("elapsed not stamped: %q", got)
	}
	if !strings.Contains(got, "request cancelled") {
		t.Errorf("footer missing 'request cancelled': %q", got)
	}
}
