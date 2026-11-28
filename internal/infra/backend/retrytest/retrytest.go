// Package retrytest shortens the production retry schedule for a test binary.
//
// # Why it exists
//
// `retry.MaxConnectionRetries`, `retry.InitialConnectionRetryInterval` and
// `retry.MaxConnectionRetryInterval` are package variables so a test can
// shrink them; the AGY schedule is 1s, 2s, 4s … capped at 4 minutes, and nine
// retries sum to **8m15s**. Tests used to shrink the schedule one function at
// a time — save the globals, restore them with a defer — which works only
// while the test that uses them is *expected* to retry. A classification test
// whose fake server answers the same failing frame forever retries for as long
// as the schedule says, so the moment the transport started retrying a
// mid-stream 5xx (AGY 1.5.0's rule, see `docs/guide/agent-loop.md`), three
// gateway classification subtests started spending eight minutes each.
//
// # Using it
//
//	func TestMain(m *testing.M) {
//		restore := retrytest.Short()
//		code := testguard.Run(m)
//		restore()
//		os.Exit(code)
//	}
//
//	func TestSomethingThatRetries(t *testing.T) {
//		retrytest.Fast(t)   // milliseconds; the retry stays observable
//	}
//
// Two knobs, two jobs. `Short` makes an *unexpected* retry cheap for a whole
// binary, which is safety: no test can cost eight minutes just because a
// predicate widened. `NoRetries` is the *assertion* a classification test
// wants — one attempt, reported as-is — because it pins the number of attempts
// rather than merely making them cheap.
package retrytest

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// production is the pair the binary started with; Short restores exactly this.
var production = [2]time.Duration{retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval}

// Short sets the retry schedule to milliseconds for the rest of the binary and
// returns the restore function. A package's TestMain calls it around m.Run:
//
//	restore := retrytest.Short()
//	code := testguard.Run(m)
//	restore()
//
// The budget (`retry.MaxConnectionRetries`) is left alone: a test that asserts
// the budget reads it, and pinning the *budget* through the context is the
// sharper signal where it matters.
func Short() func() {
	retry.InitialConnectionRetryInterval = time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	initial, maximum := production[0], production[1]
	return func() {
		retry.InitialConnectionRetryInterval = initial
		retry.MaxConnectionRetryInterval = maximum
	}
}

// Fast shortens the schedule for one test that *wants* to observe a retry.
// The values in force before the call are restored when the test ends.
func Fast(t *testing.T) {
	t.Helper()
	initial, maximum := retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		retry.InitialConnectionRetryInterval = initial
		retry.MaxConnectionRetryInterval = maximum
	})
}

// Production restores the production schedule for one test — the one that
// *asserts* the schedule rather than exercising a retry:
//
//	retrytest.Production(t)
//	if got := retry.NextConnectionRetryDelay(0); got != time.Second { ... }
//
// Without it, a package whose TestMain called Short would leave that assertion
// comparing against milliseconds.
func Production(t *testing.T) {
	t.Helper()
	retry.InitialConnectionRetryInterval = production[0]
	retry.MaxConnectionRetryInterval = production[1]
	t.Cleanup(func() {
		retry.InitialConnectionRetryInterval = time.Millisecond
		retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	})
}

// NoRetries returns a context that pins the transport budget to zero retries:
// the request is sent once and its failure is reported as-is.
func NoRetries(ctx context.Context) context.Context {
	return backend.ContextWithMaxRetries(ctx, 0)
}
