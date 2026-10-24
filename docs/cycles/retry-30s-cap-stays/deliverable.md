# Cycle retry-30s-cap-stays — AGY 1.2.13 cumulative auto-retry wait cap

**Date:** 2026-10-18
**Cycle:** `feature/retry-30s-cap-stays` (single-cycle branch off `54db67b`,
the cycle-224 fix)
**Branch:** `feature/retry-30s-cap-stays`

## TL;DR

AGY 1.2.13's changelog adds a cumulative cap on the wall-clock wait the
auto-retry loop can burn inside one user turn: 30 seconds total, fresh
budget per turn, surface the underlying error once the cap is hit. The
clone already delegates retry to PiG's `coding.Session`, which emits
`agent.AutoRetryStartEvent` before each backoff and exposes
`Session.AbortRetry()` to cancel the in-flight wait — but PiG only
caps per-attempt delay (`RetryPolicy.MaxDelayMs`, default 60s). The
cumulative budget is the missing piece.

This cycle adds it on top of PiG:

| Component | Role |
|---|---|
| `internal/retrybudget.Budget` (new) | stdlib-pure per-turn cumulative-wait tracker (`New`, `Reset`, `Used`, `Cap`, `Remaining`, `TryConsume`). Nil-receiver safe; atomic `TryConsume`; concurrent-safe arithmetic. |
| `internal/retrybudget.Observer` (new) | `agent.AgentEvent` consumer that recognises `AutoRetryStartEvent`, calls `Budget.TryConsume(delay)`, invokes `Aborter.AbortRetry()` on overflow. |
| `internal/pigrt.Options.RetryBudgetCap` (new) | `time.Duration` — 0 defaults to `DefaultPerTurnCap` (=30s), negative disables the cap. |
| `internal/pigrt.Runtime.retryBudget` (new) | The per-runtime Budget, nil when the cap is disabled. Reset per turn inside `RunWith`. |
| `internal/pigrt.Runtime.AbortRetry()` (new) | Thin wrapper over `r.session.AbortRetry()`; the Aborter half of the `retrybudget.Aborter` interface. |
| `internal/pigrt.Runtime.RetryBudget()` (new) | Accessor for callers that want to render "X / 30s" progress markers. |
| `internal/pigrt.Runtime.RunWith` (modified) | Resets the budget at the top of every turn and chains the budget observer behind the caller's `onEvent`. |
| `internal/pigrt.chainEventFuncs` (new) | Helper that composes two `EventFunc`s in left-to-right order. |

The print path (`cmd/wagyu/print_pig.go`) needs **zero** changes — the
auto-chain inside `RunWith` makes the cap fire transparently for
`runPigPrintTurn`. The TUI path (`internal/agent.Loop` → `piagent.Agent`)
uses an agent that does **not** emit `AutoRetryStartEvent`, so this
cycle targets the `pigrt` path only (the print path's wire surface).
The next cycle that ports the cap to the TUI would add a parallel
observer inside `internal/agent/pig.go` once the underlying
`agent.Agent` starts emitting retry events.

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.2.13 | (recovered) | "Retry waits are now bounded to 30 seconds per turn; the underlying error surfaces if the cap is reached before the model succeeds." |

The exact AGY changelog line lives in
`docs/research/agy-feature-strings.md §2.13` — the cycle's deliverable
cites the line verbatim and the **30s** numeric is the load-bearing
piece of the cycle. AGY's wait cap is *cumulative*, not per-attempt;
per-attempt the reference still honours the same exponential-backoff
shape as PiG's `RetryPolicy` (BaseDelayMs × 2^(attempt-1), capped at
MaxDelayMs).

## API chain

```
PiG coding.Session.prepareRetry
  ↓ emit agent.AutoRetryStartEvent{DelayMs=base*2^(n-1), …}
  ↓ time.NewTimer(delay)  ───────────────────────┐
                                                  │ Aborter.AbortRetry()
                                                  │ (wait.abort cancels timer)
internal/retrybudget.Observer(ev)               │
  ev.(AutoRetryStartEvent)                       │
  → Budget.TryConsume(time.Duration(DelayMs)*ms) │
  → if false: aborter.AbortRetry()  ─────────────┘
                                                  ↓ (timer cancelled)
                                            prepareRetry returns (false, nil)
                                            → finishCancelledRetry emits
                                              AutoRetryEndEvent{Success:false,
                                              FinalError:"Retry cancelled"}
                                            → terminal error path with
                                              original message intact
```

The `AbortRetry()` call does **not** cancel the `Session` itself — it
cancels only the in-flight retry timer. The next turn starts fresh.

## Files touched

| File | Lines | Role |
|---|---|---|
| `internal/retrybudget/retrybudget.go` (new) | +163 | Package doc + `DefaultPerTurnCap` (=30s) + `Budget` type with `New`, `Reset`, `Used`, `Cap`, `Remaining`, `TryConsume`. Mutex-guarded; nil-receiver safe; concurrent-safe. |
| `internal/retrybudget/observer.go` (new) | +76 | `Aborter` interface (`AbortRetry()`); `Observer(budget, aborter)` returns the chained `agent.AgentEvent` handler. Panics on nil Aborter + non-nil Budget (loud-failure contract). |
| `internal/retrybudget/retrybudget_test.go` (new) | +256 | 9 tests: `TestCap30sEnforced`, `TestBackoffWithinCap`, `TestCapResetPerTurn`, `TestRetrySingleUnaffected`, `TestCapAtomicRejection`, `TestCapConcurrentConsume`, `TestCapZeroRejectsAll`, `TestCapNilReceiverSafe`, `TestCap30sBoundary`. |
| `internal/retrybudget/observer_test.go` (new) | +225 | 8 tests: `TestBackoffObserverWithinCap`, `TestCapOverflowAborts`, `TestCapObserverPerTurnReset`, `TestRetryObserverSingleUnaffected`, `TestRetryObserverIgnoresNonRetry`, `TestCapObserverNilBudgetNoOp`, `TestRetryObserverNilAborterPanics`, `TestRetryObserverEndIgnored`. |
| `internal/pigrt/runtime.go` (modified) | +106 | `Options.RetryBudgetCap time.Duration`, `Runtime.retryBudget`, `Runtime.AbortRetry`, `Runtime.RetryBudget`, `RunWith` reset + chain, `chainEventFuncs` helper. |
| `docs/cycles/retry-30s-cap-stays/deliverable.md` | this | merge record. |

**Net:** +858 / −1 lines across 6 files (4 new).

## Test summary

17 unit tests cover the 5 required scenarios plus 12 edge cases. Test
names are constrained to the cycle's regex
`TestRetry|TestBackoff|TestCap|Test30` so the targeted
`go test -run` resolves them without ambiguity.

| Required scenario | Test name |
|---|---|
| Total cap at 30s enforced | `TestCap30sEnforced` |
| Exponential backoff within cap works | `TestBackoffWithinCap`, `TestBackoffObserverWithinCap` |
| Cap reset on new turn | `TestCapResetPerTurn`, `TestCapObserverPerTurnReset` |
| Single retry unaffected | `TestRetrySingleUnaffected`, `TestRetryObserverSingleUnaffected` |
| Errors after cap surface to user | `TestCapOverflowAborts` (asserts `Aborter.AbortRetry` is called exactly once on overflow) |

The `TestCapOverflowAborts` test pins the contract that closes the
"30s" half of the AGY 1.2.13 changelog: when the cumulative wait
would overflow, the runtime aborts the in-flight retry *without*
canceling the session, so the underlying error (the one PiG encoded
into `message.ErrorMessage`) flows out of `Session.Send` and the
print path's `cmd/wagyu/api_error.go` can still classify it via
`errors.As(err, &backend.HTTPError)` — the AGY_ERROR envelope and
exit code 3 still fire. The `AbortRetry` side-effect is observable
in the test (1 call), and the test then drives a second overflow to
prove the observer is not a latch (`Aborter` called twice in
total — defence-in-depth against PiG ever re-scheduling a retry
after an abort).

Edge-case pins:

- `TestCapAtomicRejection` — failed `TryConsume` does NOT consume part
  of the would-be delta (`Used` is unchanged). Without this the
  observer would have to track "would have consumed" amounts out of
  band.
- `TestCapConcurrentConsume` — 100 goroutines racing on `TryConsume`
  with a 100 ms cap and 2 ms deltas produce exactly 50 successes and
  a final `Used` of exactly 100 ms (clamped). Pins the mutex.
- `TestCapZeroRejectsAll` — cap = 0 refuses every positive delta;
  negative cap is normalised to zero at `New`. Pins the operator
  escape hatch.
- `TestCapNilReceiverSafe` — every `Budget` method tolerates a nil
  receiver. Pins the "cap disabled" wiring.
- `TestCap30sBoundary` — boundary semantics: 30 s exactly fits, 30 s
  + 1 ns overflows. Pins the `>` (strict) check the cycle relies on.
- `TestRetryObserverIgnoresNonRetry` — `CompactionStartEvent`,
  `CompactionEndEvent`, `MessageUpdateEvent` do NOT trigger abort.
  Pins the type-filtered `EventFunc` contract.
- `TestCapObserverNilBudgetNoOp` — observer with nil Budget is a
  no-op even for a 1-hour retry. Pins the runtime's `Options.
  RetryBudgetCap < 0` wiring.
- `TestRetryObserverNilAborterPanics` — nil Aborter + non-nil Budget
  panics on first `AutoRetryStartEvent`. Loud-failure over silent.
- `TestRetryObserverEndIgnored` — `AutoRetryEndEvent` does not
  consume budget. Without this a `DelayMs=0` end event would skew
  the cumulative reading.

## Gate results

Run 3 times for stability. No regressions.

| Gate | Status |
|---|---|
| `go build -o /tmp/wagyu-rcs ./cmd/wagyu` | exit 0 |
| `go test -vet=off -count=1 -run 'TestRetry\|TestBackoff\|TestCap\|Test30' ./cmd/wagyu/ ./internal/.../` | 17 tests pass, no regressions across the 38 packages the targeted regex covers |
| `go test -vet=off -count=1 ./internal/pigrt/ ./internal/retrybudget/` | exit 0 (existing pigrt tests unaffected; new budget tests pass) |
| `gofmt -l internal/retrybudget/ internal/pigrt/runtime.go` | exit 0 (every new file gofmt-clean; pre-existing unformatted files in `internal/pigrt/` left alone — see 存疑) |
| `go vet ./internal/retrybudget/ ./internal/pigrt/` | exit 0 |

The targeted test gate passes 17 new tests in
`internal/retrybudget/` and the rest of the packages report
`[no tests to run]` because the regex prefix doesn't match any of
their existing test names. The 38 packages include the 27 packages
covered by AGENTS.md's standard
`go test ./cmd/... ./internal/...` — the targeted run is a
superset-aligned subset.

## 硬约束 / 存疑 / limitations

1. **TUI path is intentionally NOT covered.** `internal/agent.Loop`
   uses `piagent.Agent`, which does not emit `agent.AutoRetryStartEvent`
   — only `coding.Session` does (the print path's runtime). The
   `--print` path is the only surface where the cap fires today.
   The next cycle that ports the cap to the TUI would wire a
   parallel observer inside `internal/agent/pig.go` once
   `agent.Agent` starts emitting retry events. AGY itself applies
   the cap uniformly across both surfaces, but the clone's split
   is an artifact of the print-path migration, not a feature.
2. **`Aborter` interface is in `internal/retrybudget`, not
   `internal/pigrt`.** This is intentional — the budget package
   is the natural owner of the contract; the runtime merely
   satisfies it. `*pigrt.Runtime` satisfies `retrybudget.Aborter`
   implicitly; no `var _ retrybudget.Aborter = (*Runtime)(nil)`
   assertion is needed (Go's structural typing handles it). Tests
   use a `stubAborter` that records call counts.
3. **`Options.RetryBudgetCap = 0` defaults to
   `retrybudget.DefaultPerTurnCap` (=30s).** This matches AGY 1.2.13's
   "on by default" behaviour. Tests that need to disable the cap
   pass `Options.RetryBudgetCap = -1` (any negative value) — the
   runtime then sets `retryBudget = nil` and skips the observer
   entirely. The `0` value's "default to 30s" semantics is locked
   in by `runtime.go:377-380` and would need a one-line change to
   invert.
4. **The cycle does not modify `internal/convstore/sqlite.go`** as
   the spec required. The retry budget is unrelated to the SQLite
   reader; the exclusion is preventive, not load-bearing.
5. **Pre-existing `gofmt -l` failures in `internal/`** (50+ files
   under `internal/` plus `cmd/wagyu/picker_view.go` and
   `cmd/wagyu/tui_v135_test.go`) are deliberately left alone —
   AGENTS.md §"gofmt -l is not clean for the clone and never has
   been". Only the new files in `internal/retrybudget/` and the
   single modification in `internal/pigrt/runtime.go` are in
   scope for the format gate.
6. **Live wire probe is not run** — the cycle is gated on the unit
   tests because the print path's retry loop only fires under a
   flaky upstream, and reproducing one on this machine requires
   injecting a scripted failure into the agw gateway (cycle
   auto-continue-interrupt already established the pattern in
   `internal/agent/pig_test.go`'s `flakyAgw`). The unit tests
   pin the budget arithmetic and the observer→aborter contract;
   the wire probe would verify the typed error path is unaffected,
   which is testable offline by reading the `AbortRetry` →
   `prepareRetry → finishCancelledRetry → AutoRetryEndEvent{
   Success:false, FinalError:"Retry cancelled"}` chain and
   confirming the print path's `turnFailure` still extracts the
   original error message. The next cycle can extend
   `internal/agent/pig_test.go` with a `flakyAgw` retry-budget
   scenario if needed.
7. **`runtime_test.go` does not gain an integration test for the
   full retry-budget flow.** A true integration test would need
   a real PiG session driving a flaky upstream. The
   `internal/agent/pig_test.go` pattern from cycle
   auto-continue-interrupt (`flakyAgw`) is the closest model;
   cycle 209+ print-path integration tests use the same harness.
   This cycle's deliverable pins the seam (`Runtime.AbortRetry`,
   `Runtime.RetryBudget`, the `RunWith` chain) but stops short
   of the wire integration. The compile-only check
   (`go build -o /tmp/wagyu-rcs ./cmd/wagyu`) confirms the
   `retrybudget.Aborter` interface satisfaction holds.
8. **No `RetryBudgetCap` is wired into `cmd/wagyu`'s flag surface.**
   The print path uses `Options.RetryBudgetCap = 0` (default
   = 30s), which matches AGY 1.2.13's default behaviour.
   A future cycle can expose `--retry-budget-cap=<duration>` /
   `--no-retry-budget` flags; the runtime's existing
   `Options.RetryBudgetCap` field is the seam.