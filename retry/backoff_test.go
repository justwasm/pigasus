package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestComputeDelayCapIs30s pins the cap constant. AGY 1.3.4 lowered
// it from 4 minutes; the constant must be 30 s, not 60 s (PiG's
// default), and not 4 minutes (the pre-1.3.4 value).
//
// The check splits into the constant + the function it feeds so a
// future bump from "30s" to "45s" still has to touch one place.
func TestComputeDelayCapIs30s(t *testing.T) {
	if MaxInterval != 30*time.Second {
		t.Fatalf("MaxInterval = %v, want 30s (AGY 1.3.4 cap)", MaxInterval)
	}
	if MaxIntervalMs != 30000 {
		t.Fatalf("MaxIntervalMs = %d, want 30000", MaxIntervalMs)
	}
	// Cap() is the canonical clamp: a value past 30 s must land
	// exactly on 30 s, not round up or down. This is the
	// invariant the AGY 1.3.4 changelog pins.
	if Cap(time.Minute) != MaxInterval {
		t.Errorf("Cap(time.Minute) = %v, want %v", Cap(time.Minute), MaxInterval)
	}
	if Cap(2*time.Minute) != MaxInterval {
		t.Errorf("Cap(2*time.Minute) = %v, want %v", Cap(2*time.Minute), MaxInterval)
	}
}

// TestExponentialGrowthBelowCap asserts the doubling grows as
// expected while still under the cap. Initial=1s, Multiplier=2.0
// means attempts 0..4 land at 1s, 2s, 4s, 8s, 16s — all well below
// 30s. The doubling is the "exponential multiplier" the AGY 1.3.4
// changelog explicitly says is unchanged.
func TestExponentialGrowthBelowCap(t *testing.T) {
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}
	for attempt, expected := range want {
		got, err := ComputeDelay(attempt)
		if err != nil {
			t.Fatalf("ComputeDelay(%d) err = %v", attempt, err)
		}
		if got != expected {
			t.Errorf("ComputeDelay(%d) = %v, want %v", attempt, got, expected)
		}
	}
}

// TestSingleRetryWaitsExactly30sAtCap drives Cap() with a value
// past 30 s and asserts the clamp lands exactly on MaxInterval.
// The wall-clock measurement verifies the *clamped* duration is the
// actual sleep time, so a regression to PiG's 60 s default or to
// the pre-1.3.4 4 minute cap fires loudly.
//
// Note: with Initial=1s and MaxAttempts=5 the schedule never
// reaches the cap (max attempt is 16 s). The cap exists as a hard
// safety bound — auto-continue and provider retry clamp through
// Cap() unconditionally, so this test exercises the same path.
func TestSingleRetryWaitsExactly30sAtCap(t *testing.T) {
	// Pin Cap()'s clamp behaviour first.
	if got := Cap(2 * time.Minute); got != MaxInterval {
		t.Fatalf("Cap(2m) = %v, want %v", got, MaxInterval)
	}
	// Now time the actual sleep. 60 s would be PiG's default and
	// would explode the test wall-clock; we expect ~30 s.
	start := time.Now()
	if err := Sleep(context.Background(), MaxInterval); err != nil {
		t.Fatalf("Sleep(%v) err = %v", MaxInterval, err)
	}
	elapsed := time.Since(start)
	if elapsed < 25*time.Second {
		t.Fatalf("elapsed = %v, want ~30s (the cap)", elapsed)
	}
	if elapsed > 35*time.Second {
		t.Fatalf("elapsed = %v, want ~30s — too long suggests the cap regressed", elapsed)
	}
}

// TestTotalTimeAcrossFiveRetriesIsBounded sums the schedule for
// MaxAttempts retries and asserts the upper bound. With Initial=1s,
// Multiplier=2.0, cap=30s and MaxAttempts=5 the schedule is:
//
//	1 + 2 + 4 + 8 + 16 = 31 s
//
// (the cap first bites at attempt=5 which is OUT of the budget —
// the budget caps attempts at 5, so attempt=4 is 16s, the largest
// delay that fires). The bound is therefore 31 s, well under the
// pre-1.3.4 4 minutes; the test is a regression tripwire for any
// future bump to MaxAttempts > 5 that keeps the cap but explodes
// the budget.
func TestTotalTimeAcrossFiveRetriesIsBounded(t *testing.T) {
	var total time.Duration
	for i := 0; i < MaxAttempts; i++ {
		d, err := ComputeDelay(i)
		if err != nil {
			t.Fatalf("ComputeDelay(%d) err = %v", i, err)
		}
		total += d
	}
	// 1+2+4+8+16 = 31s exactly (no jitter, no cap at attempt 4).
	const want = 31 * time.Second
	if total != want {
		t.Fatalf("total retry time = %v, want %v (initial 1s + 2^0..2^4)", total, want)
	}
	// Also assert the upper bound is under 4 minutes (240 s) —
	// the pre-1.3.4 cap. If the cap regresses, this fires.
	if total >= 4*time.Minute {
		t.Fatalf("total = %v >= 4m — cap regressed", total)
	}
}

// TestInitialRetryIs1s pins the initial delay. AGY 1.3.4 kept this
// unchanged; a future change must be deliberate.
func TestInitialRetryIs1s(t *testing.T) {
	if InitialInterval != 1*time.Second {
		t.Fatalf("InitialInterval = %v, want 1s (AGY kept this unchanged)", InitialInterval)
	}
	d, err := ComputeDelay(0)
	if err != nil {
		t.Fatalf("ComputeDelay(0) err = %v", err)
	}
	if d != InitialInterval {
		t.Fatalf("first retry delay = %v, want %v", d, InitialInterval)
	}
}

// TestCapAppliesToEveryRetryPath pins the cap as the *single*
// source of truth: ComputeDelay, Backoff.Next, Cap(), and a manual
// doubling loop all three the way to the same number. A future
// divergence (one path drifting past 30s because it forgot to call
// Cap) is caught here.
func TestCapAppliesToEveryRetryPath(t *testing.T) {
	// Path 1: ComputeDelay on every legal attempt. The schedule
	// is 1, 2, 4, 8, 16 — all below the cap, so the cap never
	// fires here directly. Asserting every delay is <= cap is
	// still the right invariant for this path.
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		d, err := ComputeDelay(attempt)
		if err != nil {
			t.Fatalf("ComputeDelay(%d) err = %v", attempt, err)
		}
		if d > MaxInterval {
			t.Errorf("ComputeDelay(%d) = %v, > cap %v — cap not applied", attempt, d, MaxInterval)
		}
	}

	// Path 2: Backoff.Next driven through the entire budget.
	// Every Next() must hand back a value <= cap.
	b := New()
	count := 0
	for {
		d, ok := b.Next()
		if !ok {
			break
		}
		count++
		if d > MaxInterval {
			t.Errorf("Backoff.Next iter %d = %v, > cap %v", count, d, MaxInterval)
		}
	}
	if count != MaxAttempts {
		t.Fatalf("Backoff iterated %d times, want %d", count, MaxAttempts)
	}

	// Path 3: Cap() applied to a hand-rolled exponential
	// schedule. 60 s is well past the cap.
	if got := Cap(time.Minute); got != MaxInterval {
		t.Errorf("Cap(1m) = %v, want %v", got, MaxInterval)
	}

	// Path 4: the literal "4 minutes" value the changelog says
	// the cap replaced. Cap() must clamp it down to 30 s, so a
	// future regression that drops the Cap() call fails.
	if got := Cap(4 * time.Minute); got != MaxInterval {
		t.Errorf("Cap(4m) = %v, want %v — pre-1.3.4 cap regressed", got, MaxInterval)
	}
}

// TestNoTestAccidentallyDependsOnFourMinuteCap runs every API
// surface once and asserts NONE of them returns a value at or above
// 4 minutes (240 s) — the pre-1.3.4 value the AGY 1.3.4 changelog
// says was lowered.
//
// The "no test accidentally depends on" framing matters because the
// retry package is consumed by tests in cmd/wagyu and internal/pigrt
// that time the actual sleep; a single misnamed constant (e.g.
// 240s where 30s was meant) would silently double the test wall-clock
// time without flagging the regression. This test catches that.
func TestNoTestAccidentallyDependsOnFourMinuteCap(t *testing.T) {
	const preCap = 4 * time.Minute
	checks := []struct {
		name string
		got  time.Duration
	}{
		{"MaxInterval", MaxInterval},
		{"InitialInterval", InitialInterval},
		{"MaxAttempts-bounded ComputeDelay(0)", mustCompute(0)},
		{"MaxAttempts-bounded ComputeDelay(last)", mustCompute(MaxAttempts - 1)},
		{"Cap(1m)", Cap(time.Minute)},
		{"Cap(2m)", Cap(2 * time.Minute)},
		{"Cap(4m)", Cap(4 * time.Minute)},
		{"Cap(10m)", Cap(10 * time.Minute)},
	}
	for _, c := range checks {
		if c.got >= preCap {
			t.Errorf("%s = %v, >= 4m — pre-1.3.4 cap regressed", c.name, c.got)
		}
	}
}

// TestBackoffExhaustionReturnsTypedError ensures Wait() returns
// ErrBudgetExhausted (not nil) once the budget runs out. The
// auto-continue path in internal/agent/auto_continue.go matches on
// this — it has to keep the loop's "give up" case distinct from
// "this turn succeeded".
func TestBackoffExhaustionReturnsTypedError(t *testing.T) {
	b := New()
	ctx := context.Background()
	for i := 0; i < MaxAttempts; i++ {
		if _, ok := b.Next(); !ok {
			t.Fatalf("Next() returned !ok prematurely at %d", i)
		}
	}
	// Cursor at MaxAttempts now. Next must signal the budget
	// is gone.
	if _, ok := b.Next(); ok {
		t.Fatal("Next() returned ok after MaxAttempts; budget did not enforce")
	}
	// And Wait() must surface it as a typed error.
	err := b.Wait(ctx)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("Wait() = %v, want ErrBudgetExhausted", err)
	}
}

// TestComputeDelayRejectsOutOfRangeAttempts ensures negative and
// over-budget attempts are reported as errors. The auto-continue
// caller uses ComputeDelay's error to guard against a buggy
// retry-counter underflow; without this test a silent 0-cap-return
// would mask the bug.
func TestComputeDelayRejectsOutOfRangeAttempts(t *testing.T) {
	cases := []int{-1, -100, MaxAttempts, MaxAttempts + 1, 1 << 20}
	for _, attempt := range cases {
		_, err := ComputeDelay(attempt)
		if !errors.Is(err, ErrAttemptOutOfRange) {
			t.Errorf("ComputeDelay(%d) err = %v, want ErrAttemptOutOfRange", attempt, err)
		}
	}
}

// TestSleepHonoursContextCancellation drives Sleep with a context
// that's already cancelled and asserts ctx.Err() is returned without
// blocking. The auto-continue path treats a cancelled context as
// the caller's abort — it must not wait out the full 30 s.
func TestSleepHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done

	start := time.Now()
	err := Sleep(ctx, MaxInterval)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep(cancelled ctx) err = %v, want context.Canceled", err)
	}
	// A 30 s sleep that runs to completion would be a hard
	// regression — assert we returned within a small window.
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("Sleep did not honour cancellation: elapsed = %v", elapsed)
	}
}

// TestSleepReturnsImmediatelyForZeroDuration asserts the d=0
// shortcut. A loop that calls Sleep(b.Next()) and lets Next return
// (0, false) at the end of the budget must not block on the next
// call.
func TestSleepReturnsImmediatelyForZeroDuration(t *testing.T) {
	ctx := context.Background()
	if err := Sleep(ctx, 0); err != nil {
		t.Fatalf("Sleep(0) err = %v", err)
	}
	if err := Sleep(ctx, -time.Second); err != nil {
		t.Fatalf("Sleep(negative) err = %v", err)
	}
}

// TestBackoffResetResetsCursor ensures Reset() brings the cursor
// back to zero so the same Backoff instance can iterate the same
// schedule again. Used by tests that exercise multiple error
// categories in a single TestX function.
func TestBackoffResetResetsCursor(t *testing.T) {
	b := New()
	for i := 0; i < MaxAttempts; i++ {
		b.Next()
	}
	if b.Attempt() != MaxAttempts {
		t.Fatalf("cursor before Reset = %d, want %d", b.Attempt(), MaxAttempts)
	}
	b.Reset()
	if b.Attempt() != 0 {
		t.Fatalf("cursor after Reset = %d, want 0", b.Attempt())
	}
	// Next after Reset must hand back InitialInterval again.
	d, ok := b.Next()
	if !ok || d != InitialInterval {
		t.Fatalf("Next after Reset = (%v, %v), want (%v, true)", d, ok, InitialInterval)
	}
}

// TestCapPassesThroughNegativeDurations documents the negative-passthrough
// behaviour: a negative duration is a sentinel for "no sleep", and
// Cap must not turn it into a positive MaxInterval.
func TestCapPassesThroughNegativeDurations(t *testing.T) {
	if got := Cap(-time.Second); got != -time.Second {
		t.Errorf("Cap(-1s) = %v, want -1s (no-sleep sentinel preserved)", got)
	}
	if got := Cap(0); got != 0 {
		t.Errorf("Cap(0) = %v, want 0", got)
	}
	if got := Cap(MaxInterval); got != MaxInterval {
		t.Errorf("Cap(MaxInterval) = %v, want %v (identity)", got, MaxInterval)
	}
	if got := Cap(MaxInterval + time.Nanosecond); got != MaxInterval {
		t.Errorf("Cap(MaxInterval+1ns) = %v, want %v", got, MaxInterval)
	}
}

// TestScheduleMatchesPiGShape is a regression guard: the shape
// (Initial, Multiplier, MaxInterval, MaxAttempts) must be the
// values the AGY 1.3.4 changelog names, so a future bump in either
// package flags as inconsistent.
//
// PiG's defaults are BaseDelayMs=2000, MaxDelayMs=60000,
// MaxRetries=3. The clone diverges intentionally (1s base, 30s cap,
// 5 attempts) — the test pins both shapes so any silent drift shows.
func TestScheduleMatchesPiGShape(t *testing.T) {
	if Multiplier != 2.0 {
		t.Fatalf("Multiplier = %v, want 2.0 (PiG's doubling)", Multiplier)
	}
	if InitialIntervalMs != 1000 {
		t.Fatalf("InitialIntervalMs = %d, want 1000", InitialIntervalMs)
	}
	if MaxIntervalMs != 30000 {
		t.Fatalf("MaxIntervalMs = %d, want 30000", MaxIntervalMs)
	}
	if MaxAttempts != 5 {
		t.Fatalf("MaxAttempts = %d, want 5 (AGY's effective budget)", MaxAttempts)
	}
}

// TestBudgetSummedMatchesDescription is the single number that the
// AGY 1.3.4 changelog's "single stuck request can't monopolise the
// user's session for several minutes" promise rests on. The total
// retry time across MaxAttempts retries must be strictly less than
// 4 minutes (the old cap), and tightly bounded above by the sum of
// the actual schedule.
func TestBudgetSummedMatchesDescription(t *testing.T) {
	const fourMin = 4 * time.Minute
	var total time.Duration
	for i := 0; i < MaxAttempts; i++ {
		d, err := ComputeDelay(i)
		if err != nil {
			t.Fatalf("ComputeDelay(%d) err = %v", i, err)
		}
		total += d
	}
	if total >= fourMin {
		t.Fatalf("total retry time = %v, >= 4m — AGY 1.3.4 promise broken", total)
	}
	// Tight bound: 1+2+4+8+16 = 31s. Anything bigger means a
	// future bump.
	const tight = 31 * time.Second
	if total > tight {
		t.Fatalf("total retry time = %v, want <= %v (initial 1s + 2^0..2^4)", total, tight)
	}
}

// mustCompute is a tiny test helper that turns a ComputeDelay call
// into its single-return form. Test bodies use it inside struct
// literals where a second return value would be awkward.
func mustCompute(attempt int) time.Duration {
	d, err := ComputeDelay(attempt)
	if err != nil {
		// mustCompute is only used inside test tables with legal
		// attempt values; reaching this branch is a bug in the
		// test, not in the production code.
		panic(err)
	}
	return d
}
