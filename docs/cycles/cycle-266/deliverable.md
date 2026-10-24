# cycle 266 — auto-continue has a separate, higher budget for transient network/socket errors

## TL;DR

The agent loop's auto-continue recovery used a single flat cap
(`AutoContMaxDepth = 3`), so a long tool-call chain that hit two
mid-flight wifi handoffs and one transient container restart would
die with `ErrAutoContMaxDepth{Depth: 3}` instead of riding out the
brief turbulence. Cycle 266 splits the cap by error class:
**transient network/socket errors get 8 retries before giving up;
every other class keeps 3.**

This is the clone's reading of the AGY 1.4.x line in
`internal/cli/testdata/changelog_reference.txt:10`:

> "Fixed socket disconnect errors such as `broken pipe`, `software
> caused connection abort`, `network is down`, and `http2: client
> connection lost` (for example after laptop sleep or a Wi-Fi change)
> stopping the turn with `Agent execution terminated due to error.`
> instead of retrying automatically and reporting a network error if
> retries fail."

The new typed error `*agent.ErrAutoContNetworkFailed{Attempts, Last}`
is what the loop returns *after* the network budget exhausts — a
clear "network error after N retries" banner instead of the previous
vague "user input required". The typed error still preserves the
underlying `*backend.HTTPError` (when the upstream cause is one)
through `errors.As`, so the TUI / print-path classification in
`cmd/wagyu/api_error.go` is unchanged.

## Trigger matrix

| StopReason / error                           | Action (cycle 266)                                                                                  |
|----------------------------------------------|------------------------------------------------------------------------------------------------------|
| `StopReasonToolUse`                          | no auto-continue — surface tool calls                                                                |
| `StopReasonStop`                             | no auto-continue — clean completion                                                                   |
| `StopReasonError`                            | **transient network?** AutoContMaxNetworkDepth (=8) retries; otherwise AutoContMaxDepth (=3)          |
| `StopReasonAborted`                          | same as above                                                                                          |
| `StopReasonLength`                           | same as above                                                                                          |
| `ErrMaxTurnsReached` (iteration cap)         | AUTO-CONTINUE with `**Status**: Cancelled after N iterations` banner (always AutoContMaxDepth)        |
| `*backend.HTTPError{IsQuotaReached}`         | NO auto-continue (changelog 1.3.3; surfaces immediately)                                                |
| `context.Canceled` / `context.Deadline…`     | no auto-continue (caller owns the abort)                                                                |
| Other error (HTTP 4xx, scope, marshal, …)    | AUTO-CONTINUE for AutoContMaxDepth (=3); after which `*ErrAutoContMaxDepth`                            |
| **Transient network** (broken pipe, etc.)   | AUTO-CONTINUE for AutoContMaxNetworkDepth (=8); after which `*ErrAutoContNetworkFailed`                |

Cycle 266 only widens the budget for the bottom row; every other
class keeps the pre-cycle contract.

## Files

| Path                                                   | Role                                                              |
|--------------------------------------------------------|-------------------------------------------------------------------|
| `internal/agent/transient_network.go`                  | NEW — `IsTransientNetworkErr(err)` predicate + verbatim-phrase list |
| `internal/agent/transient_network_test.go`             | NEW — 27 tests (phrases, wrapped chain, context-precedence, typed error contract) |
| `internal/agent/auto_continue.go`                      | MODIFIED — `AutoContMaxNetworkDepth`, `*ErrAutoContNetworkFailed`, branched-budget loop |
| `internal/agent/auto_continue_test.go`                 | MODIFIED — `TestAutoContBudgetDefaults`, `TestIsTransientNetworkErr_PresetMatrix` |
| `internal/cli/testdata/feature_diff_table.json`         | MODIFIED — flipped `clone: false → true` on the row "Auto-continue has separate network retry budget (8)" |
| `docs/cycles/cycle-266/repro/main.go`                  | NEW — focused harness `go run ./docs/cycles/cycle-266/repro -mode {repro1,repro2,repro3}` |

## Reproduction

The user described a hang in tmux after a few retries, where a long
research/save-as-page prompt kept hitting transient stream errors.
The harness in `docs/cycles/cycle-266/repro/main.go` reproduces the
shape without real auth — three modes, one harness:

```bash
# Reproduce the user's bug pattern with a single short command:
go run ./docs/cycles/cycle-266/repro -mode repro1

# Expected output (cycle 266 in effect):
# === repro1: tool success → transient network stream error → 8 retries ===
#   ↻ auto-continuing (depth=1)
#   ↻ auto-continuing (depth=2)
#   ↻ auto-continuing (depth=3)
#   ↻ auto-continuing (depth=4)
#   ↻ auto-continuing (depth=5)
#   ↻ auto-continuing (depth=6)
#   ↻ auto-continuing (depth=7)
# Send error: agent: network error after 8 retries (last error: ...)
# Send error type: *agent.ErrAutoContNetworkFailed
```

Without the cycle 266 fix, the same command produces:

```
agent: auto-continue reached max depth (3); user input required
*agent.ErrAutoContMaxDepth
```

after only 3 retries. That is the cliff the user reported.

## Test status

`go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` is green:

| Package                  | Status  | Notes                                                       |
|--------------------------|---------|-------------------------------------------------------------|
| `cmd/wagyu`              | ok 42.5s| includes the pre-existing suite                              |
| `internal/agent`         | ok 0.7s | 27 cycle-266 tests + the existing suite re-passes            |
| `internal/backend`       | ok 0.9s |                                                                 |
| `internal/piagentbridge` | ok 1.0s |                                                                 |
| `internal/pigrt`         | ok 1.4s | print-path counterpart — no changes needed (`pigrt` reuses `AutoContMaxNetworkDepth` indirectly via the agent it wraps) |
| `internal/cli`           | ok 21.8s|                                                                 |
| (everything else)        | ok      | 27 packages ok; see cycle-256 baseline                        |

## What I deliberately did NOT change

- **The flat quote of 3 kept its name.** Renaming `AutoContMaxDepth`
  to `AutoContMaxGenericDepth` or similar would have rippled through
  the existing tests' type assertions (`*ErrAutoContMaxDepth`,
  `tc.Depth == AutoContMaxDepth`). The new constant
  `AutoContMaxNetworkDepth` is the additive knob; the existing
  constant is untouched.

- **No non-network budget widening.** Cycle 266 widens ONE
  category. HTTP 4xx, scope mismatch, marshal errors, and quota are
  still on `AutoContMaxDepth = 3` (or fail-fast, in the quota case).
  Widening any of those would burn upstream quota unnecessarily
  on errors the user can't lift mid-session.

- **No backoff between retries.** PiG already handles internal
  exponential-backoff (see `internal/retrybudget/agent.go`,
  per-attempt cap of 30s, and the per-turn cumulative cap of 30s).
  Cycle 266 widens the *number* of retries, not the *time between*
  retries — that cap is still in `internal/retrybudget/`.

- **Did not touch the print-path counterpart.** `internal/pigrt`
  has its own `auto_continue.go` (per the AGENTS.md note about
  print/TUI parity); it shares the same `AgentLoop` indirectly
  through `coding.Session`. The print path's `Send` ultimately
  delegates to `agent.Loop.Send`, which now dispatches through the
  cycle-266 fixed `sendPigAuto`, so the print path picks up the
  wider budget for free. A future cycle can wire the typed
  `ErrAutoContNetworkFailed` into the print-path's
  `AGY_ERROR_JSON` envelope if 1.4.x's print-mode contract demands
  it (the current `--print` exit-code contract, set by cycle 250,
  maps *any* `agent.Loop` failure to exit code 3 — that includes
  the new typed error, so print mode "just works" for this row).

- **Did NOT fix the unrelated quota-fast-fail regression.**
  Verified during this cycle's repro: PiG drops the concrete type
  on the stream boundary (per the documented note in AGENTS.md
  §"The agent loop"), so `backend.IsQuotaReachedErr` reaches a
  string-only `errors.errorString` in the auto-continue call and
  the quota bail-out does NOT fire. The previous cycle (no 266
  column) had the same behaviour. The user's primary complaint
  was the network-error one; leaving the quota-fast-fail to its
  own cycle (with the right fix in `piagentbridge.BackendStreamFn
  WithObserver` to preserve the typed error) keeps this cycle
  focused.

## 硬约束 / 存疑 / limitations

- **Detective predicate, not type-based.** `IsTransientNetworkErr`
  substring-matches against `errors.Unwrap(err).Error()`. A future
  upstream error with the phrase "connection reset" in a non-network
  context (e.g. a sentinel string the gateway emits on retry) would
  be misclassified as transient. The negative-case tests pin three
  such exclusions (`insufficient_scope`, `individual quota reached`,
  `401 unauthorized`); a future cycle that adds new phrases needs
  to extend that test set too.

- **EOF is broad.** "eof" matches every error that contains the
  string `eof`; the upstream errors that matter (`unexpected eof`,
  `eof reading from socket`, the typical `io.EOF` cases) all carry
  additional context, but a malicious or accidental string "weof"
  in an upstream error body would false-positive. This is the
  trade-off the AGY binary makes too (its recovery code is also
  substring-based; verified by reading the binary's
  `internal/cli/testdata/recovery.s` recovered symbols).

- **8 retries on a permanent failure still burns quota.** If the
  classifier mistakenly tags a permanent failure as transient, the
  loop now retries 8 times instead of 3. Mitigation: the
  `*ErrAutoContNetworkFailed` typed error preserves the original
  via `Unwrap`, so a future TUI / print-path improvement that
  matches against it can still classify "network error" vs
  "permanent failure" as needed (e.g. "should I tell the user to
  check their wifi or to wait for the server to come back?").

- **No instrumented diff against the AGY binary.** The cycle ships
  source-level matching against the verbatim phrases in
  `changelog_reference.txt`, but does not run a side-by-side
  execution against the real `agy` 1.4.x binary in print mode. That
  parity check belongs to a follow-up cycle (likely 268 or 269)
  that uses the test harness to compare the AGY + clone outputs
  on an identical prompt + identical wifi-blip injection.
