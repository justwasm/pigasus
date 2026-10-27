// Retry-policy wiring for the clone's PiG runtime.
//
// Background. AGY 1.3.4 (changelog line 29) and 1.3.6 (line 5) lowered
// the per-retry backoff cap from 4 minutes to 30 seconds so a single
// stuck upstream request cannot monopolise the user's session for
// several minutes at a time:
//
//	"Cancelled requests are retried with the maximum retry time lowered
//	 from 4 minutes to 30 seconds so a single stuck request can't
//	 monopolise the user's session for several minutes."
//
// The clone owns no retry path of its own — it delegates to PiG
// (github.com/MichaelKinsy/PiG) for the assistant loop and the
// provider-request retry transport. There are two knobs to twist:
//
//  1. ai.ConfigureProviderRetry — process-global (atomic.Int64) for
//     the provider HTTP retry loop (PiG's provider_retry.go:51). Sets
//     maxRetries and maxRetryDelayMs in milliseconds.
//  2. The SettingsManager the session consults at every Send, which
//     surfaces Retry.MaxAgentDelayMs *int (icodingagent/settings.go:168)
//     through SettingsManager.GetRetrySettings.MaxDelayMs.
//
// The clone wants both knobs to read the same number — 30 s, the AGY
// 1.3.4 cap. This file is the single seam: ConfigureProviderRetry is
// the provider knob, and the SettingsManager is built with the same
// constant in runtime.buildDefaultSettingsManager. The retry package
// itself (retry) owns the number; everything in this file
// reads from it so the cap cannot drift between paths.
package pigrt

import (
	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/retry"
)

// init pins the provider-request retry transport to the clone's
// canonical policy at process start. ai.ConfigureProviderRetry stores
// maxRetries / maxRetryDelayMs into package-global atomics (PiG's
// provider_retry.go:32), so the call only has effect once per
// process — re-running it from a second Runtime.New would be a no-op
// for goroutines that already saw the first value.
//
// The 5 attempts / 30 s cap match AGY 1.3.4+; PiG's defaults are
// 0 attempts (retry transport inert) for maxRetries and 60 s for
// maxRetryDelayMs. We diverge to match the upstream AGY posture so a
// stuck upstream retries with the same schedule the user's recovery
// expectations are calibrated to.
func init() {
	if err := ai.ConfigureProviderRetry(retry.MaxAttempts, retry.MaxIntervalMs); err != nil {
		// The integer arguments come from constants; an error here is
		// a programmer error, not a user input, but we still avoid
		// panicking on the import-only path so a future refactor that
		// makes one of those constants computed cannot crash a CLI
		// that already started up. The agent-level cap (set via the
		// SettingsManager in buildDefaultSettingsManager) remains the
		// load-bearing one — provider retry is best-effort.
		_ = err
	}
}

// ConfigureProviderRetry re-applies the canonical retry policy. It is
// exported so tests that have stomped on the package-global atomics
// (with their own ConfigureProviderRetry call) can reset the state
// between subtests. Production code does not need to call this —
// init() has already run.
//
// Returns whatever ai.ConfigureProviderRetry returns, so a caller
// that wants to fail loudly on a misconfiguration can do so without
// the import-only path also panicking.
func ConfigureProviderRetry() error {
	return ai.ConfigureProviderRetry(retry.MaxAttempts, retry.MaxIntervalMs)
}
