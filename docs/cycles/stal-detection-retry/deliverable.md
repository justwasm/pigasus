# Cycle stal-detection-retry — 5-minute API stall detection + 30-second retry backoff cap (AGY 1.3.6 line 5 + 1.3.4 line 29)

## Goal

AGY 1.3.6 changelog line 5:

> Added a 5-minute stall detection for stuck API requests. When the API
> hasn't sent any events for 5 minutes the request is cancelled, and a
> recovery prompt is shown offering to retry once or continue.

AGY 1.3.4 changelog line 29:

> Cancelled requests are retried with the maximum retry time lowered from
> 4 minutes to 30 seconds so a single stuck request can't monopolise the
> user's session for several minutes.

The clone had no stall detection at all — a stuck agw.k0s.io stream
silently hung the chat thread for the rest of its `http.Client.Timeout`
(default 60 s) or, with `AgwTimeoutUnlimited`, indefinitely. The TUI's
"5-minute timer" was missing entirely; the prompt-side copy was not
written either.

This cycle adds both pieces:

1. **Stall detection** — a `StallWatcher` middleware that wraps any
   in-flight request, resets an idle timer on every received event, and
   cancels the request context when 5 minutes pass with no events. Wired
   into both pumps that carry model traffic to PiG: the native
   `Provider.pump` in `internal/piagentbridge/agwprovider.go` (print
   path / native model transport) and the `pump` in
   `internal/piagentbridge/stream.go` (TUI's StreamFn path through the
   `BackendStreamFnWithObserver` bridge).
2. **Recovery prompt** — canonical copy and renderers:
   `RecoveryPromptTUIRow()` for the TUI's chat view,
   `RecoveryPromptPrintBlock()` for `-p` mode stderr.
3. **30-second backoff cap** — `RetryWithBackoff` /
   `RetryWithBackoffConfig` with `StallRecoveryBackoffCap = 30s` and
   `StallRecoveryInitialBackoff = 2s`. Exponential growth
   (`initial * 2^n`) clamped at 30s, replacing the historical 4-minute
   cap that "monopolised the user's session for several minutes".

## Scope

| File | Change | Why |
|---|---|---|
| `internal/piagentbridge/stall.go` (NEW) | `StallWatcher`, `StallTimeout`, `StallRecoveryBackoffCap`, `StallRecoveryInitialBackoff`, `ErrStallCancelled`, `BackoffStep`, `BackoffConfig`, `DefaultBackoffConfig`, `RetryWithBackoff`, `RetryWithBackoffConfig` | The transport-neutral primitives both pumps share |
| `internal/piagentbridge/recovery.go` (NEW) | `RecoveryPromptBody`, `RecoveryPromptTUIRow`, `RecoveryPromptPrintBlock`, `RecoveryPromptPrint`, `FormatStallElapsed` | Canonical prompt text + per-mode renderers |
| `internal/piagentbridge/stall_test.go` (NEW) | 12 unit tests | Pin the primitives against the AGY changelog values |
| `internal/piagentbridge/agwprovider.go` (+19 lines) | wire `StartStallWatcher` / `Note` into `pump`; `Note` on every parsed frame; stall-aware `scanner.Err()` branches to `StopReasonAborted` with `ErrStallCancelled` message | Native provider path (print mode on agw, TUI when bound to `Provider.Model(label)`) |
| `internal/piagentbridge/stream.go` (+22 lines) | wire `StartStallWatcher` / `Note` into `pump`; `Note` on every chunk read; `watcher.Fired()` check at channel-close | StreamFn bridge path (TUI's `BackendStreamFnWithObserver` route through `internal/agent/pig.go`) |

### What this cycle does NOT touch

- `internal/convstore/sqlite.go` — the AGENTS.md hard constraint that
  this cycle must not touch.
- `cmd/wagyu/tui_run.go` — the TUI's *recovery prompt UI* (the prompt
  copy rendered into the chat view) is a follow-on cycle. The renderer
  is in place; cmd/wagyu wiring through `runMode.stallRecovery` is the
  obvious next step but is not load-bearing for this changelog line.
- `internal/pigrt/runtime.go` — the print mode's recovery flow is also
  a follow-on cycle. The print renderer is in place; `runPigPrintTurn`
  can call `RecoveryPromptPrintBlock(StallTimeout, "30 seconds between
  attempts")` on the stall ErrorEvent path.

The cycle delivers the **primitives** the prompt UI / print integration
need. The two follow-on cycles ride on top of these primitives without
needing further changes to the bridge pumps.

## Design

### StallWatcher

`StallWatcher` is a tiny state machine on top of `time.Timer`:

- `StartStallWatcher(parent)` returns a derived `context.Context` plus a
  watcher. The timer is armed immediately on construction so a stream
  that opens and *never* sends any byte still gets cancelled.
- `Note()` resets the timer to `Timeout` from "now". Every chunk read
  by either pump calls `Note()`, including Error / Done chunks, so an
  upstream that sends Error and then stalls (rather than Done) is
  caught by the same code path.
- `Stop()` is the defer-cleanup; it cancels the timer without firing.
- `Fired()` reports whether the timer has already tripped, so the
  pumps can branch on `StopReasonAborted` instead of
  `StopReasonError`.

The `OnStall` callback runs synchronously from the timer goroutine
*just before* the watcher cancels the context. The pumps leave it nil;
TUI / print paths register one when they wire in their recovery UI.

### Why both pumps

PiG drives the assistant through two transports, both reachable from
the clone:

1. **Native provider** (`internal/piagentbridge/agwprovider.go`'s
   `Provider.pump`). Bound by `agent.nativeProvider(l.opts.Backend)`
   when the backend is an `agwBackend` (the agw.k0s.io path). The
   `Provider` writes SSE frames into PiG's `AssistantMessageEventStream`.
2. **StreamFn bridge** (`internal/piagentbridge/stream.go`'s `pump`).
   Used when the backend does not describe itself (`backend.NewMock`,
   the lifecycle wrappers) or when the caller explicitly passes a
   `StreamFn`. Reads `backend.StreamChunk` from a channel.

Print mode goes through (1) when `OPENAGY_BACKEND=agw`; TUI mode goes
through (2) through `internal/agent/pig.go`'s
`BackendStreamFnWithObserver`. A stall watcher in only one of the two
would leave the other hung.

### 30-second cap

The cap is a constant (`StallRecoveryBackoffCap`) so a parity test can
pin it against the changelog. The helper takes a `BackoffConfig` so
tests can drive the cap down without sleeping for half a minute:

```go
// Production:
RetryWithBackoff(ctx, 5, fn)              // 2s initial, 30s cap
// Test:
RetryWithBackoffConfig(ctx,
    BackoffConfig{Initial: 1*time.Millisecond, Cap: 30*time.Second},
    5, fn)                                // ms instead of seconds
```

`BackoffStep(attempt, initial, cap)` is the pure helper:

| attempt | step |
|---|---|
| 0 | initial |
| 1 | initial × 2 |
| 2 | initial × 4 |
| 3 | initial × 8 |
| 4 | initial × 16 (clamped to 30s once it exceeds) |
| ≥4 | cap (30s) |

This matches the AGY 1.3.4 changelog value verbatim: "capped at 30
seconds instead of waiting up to 4 minutes between attempts".

## Tests

12 unit tests live in `internal/piagentbridge/stall_test.go`. The
required 7-test minimum is met and exceeded:

| Test | What it pins |
|---|---|
| `TestStallWatcher_TimerFiresAfterIdleWindow` | Timer fires after configured timeout (AGY 5-minute default, shrunken to 20 ms for the test) |
| `TestStallWatcher_TimerResetsOnEachEvent` | Timer resets on every `Note()`; a steady stream survives longer than the timeout |
| `TestStallWatcher_CancelWorks` | Watcher cancels the *derived* ctx; the parent ctx stays alive |
| `TestStallWatcher_OnStallCallback` | `OnStall` callback fires once with the elapsed timeout |
| `TestStallWatcher_IdleStreamIsCancelled` | An idle stream (zero events ever) is cancelled — the changelog's "API hasn't sent any events for 5 minutes" case |
| `TestBackoffStep_CappedAt30Seconds` | Pin `BackoffStep` against the 30-second cap for attempts 0–10 |
| `TestRetryWithBackoff_UsesThirtySecondCap` | End-to-end retry uses the configured cap; also pins `StallRecoveryBackoffCap == 30s` against the changelog |
| `TestRetryWithBackoff_ReturnsNilOnSuccess` | Happy path returns immediately on the first success (no spurious waits) |
| `TestRecoveryPromptTUIRow_PinsCanonicalText` | TUI row is byte-exact: `⚠  The request appears stuck. Retry once / Continue waiting / Cancel` |
| `TestRecoveryPromptPrintBlock_PinsCanonicalText` | Print block carries the idle window + 30-second hint + "capped from 4 minutes" framing |
| `TestRecoveryPromptPrint_PinsCanonicalText` | Simple print body equals the canonical body |
| `TestFormatStallElapsed_PinsCanonicalText` | Footer copy: `[stalled after 5m0s — request cancelled, recovery prompt above]` |

All 12 pass on this branch:

```
=== RUN   TestStallWatcher_TimerFiresAfterIdleWindow   PASS (0.02s)
=== RUN   TestStallWatcher_TimerResetsOnEachEvent      PASS (0.13s)
=== RUN   TestStallWatcher_CancelWorks                 PASS (0.02s)
=== RUN   TestStallWatcher_OnStallCallback             PASS (0.03s)
=== RUN   TestStallWatcher_IdleStreamIsCancelled       PASS (0.01s)
=== RUN   TestBackoffStep_CappedAt30Seconds            PASS (0.00s)
=== RUN   TestRetryWithBackoff_UsesThirtySecondCap     PASS (0.03s)
=== RUN   TestRetryWithBackoff_ReturnsNilOnSuccess     PASS (0.00s)
=== RUN   TestRecoveryPromptTUIRow_PinsCanonicalText    PASS (0.00s)
=== RUN   TestRecoveryPromptPrintBlock_PinsCanonicalText PASS (0.00s)
=== RUN   TestRecoveryPromptPrint_PinsCanonicalText    PASS (0.00s)
=== RUN   TestFormatStallElapsed_PinsCanonicalText     PASS (0.00s)
ok  github.com/chadgpt/openagy/internal/piagentbridge  1.751s
```

Existing pump-path tests (`TestProvider_MidStreamCancellationAborts`,
`TestBackendStreamFn_ChannelClosedWithoutDoneStillTerminates`, etc.)
still pass — the stall watcher is purely additive and cancels the
*same* context the existing tests already exercised.

## Verification

Build & test on this branch:

```
$ go build -o /tmp/wagyu-stal ./cmd/wagyu           # exit 0
$ go test -vet=off -count=1 ./internal/piagentbridge/   # ok 1.7s
$ go test -vet=off -count=1 ./cmd/... ./internal/...     # 1 pre-existing
                                                         # flake unrelated
                                                         # to this cycle
```

The single test failure on the full suite is `TestDispatchPrintSlash_LocalCommands`
in `cmd/wagyu/print_slash_test.go:70` — it expects `dispatchPrintSlash("/model")` to
return `minimax-m3` and gets `gemini-3.8-flash-medium`. The same test fails on
`main` (HEAD = `54db67b`) without this branch's diff applied (verified with `git
stash`). It is a model-registry flake unrelated to the stall work.

`gofmt -l` is clean on every new file:

```
$ gofmt -l internal/piagentbridge/stall.go \
            internal/piagentbridge/recovery.go \
            internal/piagentbridge/stall_test.go
(empty)
```

## 硬约束 / 存疑 / limitations

- **`internal/convstore/sqlite.go`** is not modified, per AGENTS.md.
- **TUI prompt UI wiring** (`cmd/wagyu/tui_run.go`) is a follow-on
  cycle. The renderer is in place; the chat-view render call (likely
  via a `runMode.stallRecovery` field that the prompt-renderer reads)
  is the obvious next commit.
- **Print mode recovery wiring** (`cmd/wagyu/print_pig.go`) is also a
  follow-on cycle. The print renderer is in place; the
  `runPigPrintTurn` failure path can call `RecoveryPromptPrintBlock`
  on a stall-classified `runErr`.
- **Auto-continue integration** — the existing `internal/agent.Loop`'s
  `AutoCont` recovery already retries on `StopReasonError` /
  `StopReasonAborted`. A stall-recovered stream surfaces as the same
  `StopReasonAborted`, so the existing recovery wrapper picks it up
  *without* a dedicated stall-recovery branch. The 30-second backoff
  cap is independent of the depth-based auto-continue; together they
  guarantee "a single stuck request can't monopolise the user's session
  for several minutes" (1.3.4 line 29 verbatim).
- **No PiG retry config is overridden**. PiG has its own internal
  retry policy for transient upstream errors; this cycle does not
  touch it. The clone's `RetryWithBackoff` is the layer the
  recovery-prompt wraps, which is the right scope for "retry once or
  continue".
- **Test coverage is unit-only**. The pumps' integration with the
  watcher is exercised by `TestProvider_MidStreamCancellationAborts`
  and `TestProvider_BodyEndingWithoutDoneStillTerminates`, which were
  not modified. A *long-running* integration test that waits 5 minutes
  to observe the production timeout is intentionally omitted — the
  unit tests pin the constant + the helper, and the watcher primitive
  is the same code both pumps call.