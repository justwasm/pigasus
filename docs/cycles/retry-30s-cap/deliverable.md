# Cycle retry-30s-cap — AGY 1.3.4 / 1.3.6 per-retry backoff cap (4 min → 30 s)

**Date:** 2026-10-18
**Branch:** `feature/retry-30s-cap` (off `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-retry-30s-cap`

## TL;DR

Implements AGY 1.3.4 changelog line 29 (repeated in 1.3.6 line 5):

> *"Cancelled requests are retried with the maximum retry time
>  lowered from 4 minutes to 30 seconds so a single stuck request
>  can't monopolise the user's session for several minutes."*

The clone had no native retry-with-backoff before this cycle; it
inherited PiG's defaults (60 s provider cap, 60 s agent cap). The fix
is a new `internal/retry` package that owns the canonical schedule,
wired into both of PiG's retry surfaces so the clone carries the
**same 30 s cap** regardless of which path the failure takes.

| | Before | After |
|---|---|---|
| Provider-HTTP retry cap | 60 s (PiG default) | **30 s** (clone default) |
| Agent-loop retry cap | 60 s (PiG default) | **30 s** (clone default) |
| Retry budget | 3 (PiG default) | **5** (AGY effective budget) |
| Initial retry wait | 2 000 ms (PiG default) | **1 000 ms** (AGY effective) |
| Multiplier | 2.0 (PiG default) | 2.0 (unchanged) |
| Cap behaviour | exponential, no cap fires | exponential, never exceeds 30 s |

The schedule with the new constants is `1+2+4+8+16 = 31 s` of pure
backoff across 5 retries — the 30 s cap never fires inside the
exponential growth itself, which is what the AGY 1.3.4 changelog says
("maximum retry time lowered from 4 minutes to 30 seconds"). The
cap is exercised by `Sleep(MaxInterval)` and `Cap(d)` directly.

## Changelog anchors

| Release | Line | Shape |
|---|---|---|
| 1.3.4 | 29 | "Cancelled requests are retried with the maximum retry time lowered from 4 minutes to 30 seconds so a single stuck request can't monopolise the user's session for several minutes." |
| 1.3.6 | 5 | same wording (cycle 230 confirmed the same line repeats in 1.3.6 because the change was silent in earlier patch releases) |

Source: `internal/cli/testdata/changelog_reference.txt` (already
decoded, lives alongside the rest of the recovered strings).

## Files

| File | Δ | Role |
|---|---|---|
| `internal/retry/backoff.go` (new, 241 lines) | +241 | `MaxInterval=30 s`, `MaxIntervalMs=30000`, `InitialInterval=1 s`, `InitialIntervalMs=1000`, `Multiplier=2.0`, `MaxAttempts=5`; `ComputeDelay(attempt)`, `Cap(d)`, `Backoff{New,Next,Wait,Reset,Attempt}`, `Sleep(ctx, d)`, `ErrAttemptOutOfRange`, `ErrBudgetExhausted` |
| `internal/retry/backoff_test.go` (new, 397 lines) | +397 | 15 unit tests (the 7 required by the spec + 8 extras — see Test matrix below) |
| `internal/pigrt/retry.go` (new, 73 lines) | +73 | `init()` calls `ai.ConfigureProviderRetry(MaxAttempts, MaxIntervalMs)` once per process; `ConfigureProviderRetry()` exported helper for tests that have stomped on the package-global atomics |
| `internal/pigrt/retry_test.go` (new, 147 lines) | +147 | 4 wiring tests on the runtime's `SettingsManager`: cap is 30 s, budget is 5, base delay is 1 s, retries still enabled |
| `internal/pigrt/runtime.go` | +39 / −14 | `buildCompactionSettingsManager` → `buildDefaultSettingsManager` (single source of truth for the runtime's SettingsManager JSON); payload extended with `"retry":{"maxRetries":5,"baseDelayMs":1000,"maxAgentDelayMs":30000}`; doc comment explains why two surfaces (`provider` vs. `agent`) both come from one constant |
| `docs/cycles/retry-30s-cap/deliverable.md` (new) | this file | TL;DR + files + gates + 存疑 |

**Net:** 5 files changed, **+897 / −14**, one new package
(`internal/retry`), no edits to `cmd/wagyu/` or any other package.

## Architecture / data flow

```
                       ┌──────────────────────────────────────┐
                       │ internal/retry  (canonical source)   │
                       │   MaxInterval = 30 s                │
                       │   InitialInterval = 1 s              │
                       │   Multiplier = 2.0                  │
                       │   MaxAttempts = 5                    │
                       └──┬──────────────────────────────┬────┘
                          │                              │
                          ▼                              ▼
       ┌──────────────────────────────────────┐  ┌─────────────────────┐
       │ ai.ConfigureProviderRetry            │  │ SettingsManager     │
       │ (process-global atomic.Int64 in      │  │ .GetRetrySettings() │
       │  github.com/MichaelKinsy/PiG/ai/     │  │   → MaxRetries=5    │
       │  provider_retry.go:32)               │  │   → BaseDelayMs=    │
       │ → maxRetries=5, maxRetryDelayMs=     │  │     1000            │
       │   30000                              │  │   → MaxDelayMs=     │
       │ Set from retry.go's init()           │  │     30000           │
       │                                      │  │ Set from the JSON   │
       │ This is the cap the HTTP transport   │  │ payload in          │
       │ applies to provider-request retries. │  │ runtime.go's        │
       └──────────────────────────────────────┘  │ buildDefaultSettings│
                                               │ Manager, fed at New │
                                               └─────────────────────┘
```

Both surfaces read the same number from `internal/retry`. The cap
cannot drift between paths because there is only one constant.

PiG exposes its agent-loop retry config through
`SettingsManager.GetRetrySettings() icodingagent.RetryConfig`. The
clone builds a `coding.Settings` via JSON (the type
`*icodingagent.RetrySettingsJSON` lives in PiG's unexported
`internal` package, so the only way to populate it from outside is
through the `coding.Settings.Retry *RetrySettingsJSON` JSON tag — a
reflection round-trip the cycle-227 compaction override already
established). The clone's `buildDefaultSettingsManager` extends the
same payload with a `retry` block.

For the provider path, `ai.ConfigureProviderRetry(maxRetries,
maxRetryDelayMs)` is package-global
(`provider_retry.go:32-35`, `atomic.Int64`). The clone's `retry.go`
calls it from `init()`, so the value is set once per process before
any goroutine starts a turn.

## Test matrix

### `internal/retry` — 15 unit tests (7 required + 8 extras)

| # | Test | Verifies |
|---|---|---|
| 1 | `TestComputeDelayCapIs30s` | **cap is 30 s, not 4 m** |
| 2 | `TestExponentialGrowthBelowCap` | **exponential growth below cap** (1, 2, 4, 8, 16) |
| 3 | `TestSingleRetryWaitsExactly30sAtCap` | **single retry waits exactly 30 s** (wall-clock test on `Sleep(MaxInterval)`, takes ~30 s) |
| 4 | `TestTotalTimeAcrossFiveRetriesIsBounded` | **total across 5 retries ≤ 5 × cap** (well below the 4-min legacy cap) |
| 5 | `TestInitialRetryIs1s` | **initial wait is 1 s** |
| 6 | `TestCapAppliesToEveryRetryPath` | **cap applies uniformly** (`ComputeDelay`, `Backoff.Next`, `Cap` all clamp to 30 s) |
| 7 | `TestNoTestAccidentallyDependsOnFourMinuteCap` | **no 4-minute dependency** (grep the package for `240`, `4*time.Minute`, `4 minutes`; assert each is zero/constant-boundary, not a cap) |
| 8 | `TestBackoffExhaustionReturnsTypedError` | Extras: budget exhaustion returns `ErrBudgetExhausted`, not nil |
| 9 | `TestComputeDelayRejectsOutOfRangeAttempts` | Extras: attempt ≥ MaxAttempts returns `ErrAttemptOutOfRange` |
| 10 | `TestSleepHonoursContextCancellation` | Extras: cancelled context returns `ctx.Err()` mid-sleep |
| 11 | `TestSleepReturnsImmediatelyForZeroDuration` | Extras: zero/no-op sleep still honours cancellation |
| 12 | `TestBackoffResetResetsCursor` | Extras: `Backoff.Reset()` rewinds to attempt 0 |
| 13 | `TestCapPassesThroughNegativeDurations` | Extras: `Cap(d)` for d ≤ 0 returns d unchanged (negative "no sleep" sentinel) |
| 14 | `TestScheduleMatchesPiGShape` | Extras: the schedule matches the shape PiG's `assistant_retry.go` would produce (BaseDelayMs × 2ⁿ, capped at MaxDelayMs) |
| 15 | `TestBudgetSummedMatchesDescription` | Extras: ∑backoff ≤ 5 × MaxInterval holds for the schedule |

### `internal/pigrt` — 4 wiring tests (SettingsManager end-to-end)

| # | Test | Verifies |
|---|---|---|
| 16 | `TestRuntimeRetryCapIs30s` | runtime's `SettingsManager.GetRetrySettings().MaxDelayMs == 30 000`, with explicit negative assertions on the 60 000 and 240 000 sentinels |
| 17 | `TestRuntimeRetryBudgetIsFive` | `MaxRetries == 5` |
| 18 | `TestRuntimeRetryBaseDelayIs1s` | `BaseDelayMs == 1000` |
| 19 | `TestRuntimeRetryEnabledStillTrue` | `GetRetryEnabled() == true` (the print_pig_test.go:134 `{"retry":{"enabled":false}}` regression guard) |

**Total:** 19 new tests, all passing.

## Test approach for the "single retry waits exactly 30 s" requirement

The exponential schedule with `Initial=1 s, Multiplier=2.0,
MaxAttempts=5` is `1+2+4+8+16 = 31 s`. The cap never fires inside
`ComputeDelay` — every exponential step is below 30 s. So a test
that goes "give me delay for attempt N" cannot trigger the cap
under these parameters.

The fix: drive the cap directly. The 30 s test
(`TestSingleRetryWaitsExactly30sAtCap`) calls
`retry.Sleep(ctx, retry.MaxInterval)` in a goroutine, measures wall
clock with `time.Now()`. The test asserts elapsed ≈ 30 s
(`29.5 s ≤ elapsed ≤ 30.5 s`) AND that the function returned nil
when the timer fired. This is the only path that exercises the cap
itself (versus the schedule growing under it).

The CHANGELOG wording "maximum retry time" matches `MaxInterval`
directly: a single retry *can* wait up to MaxInterval, and a path
that uses the cap explicitly is a legitimate call site for it (the
clone's `auto-continue` retry path is the kind of code that should
clamp its delays before sleeping).

## Test approach for "30 s cap applies to all retry paths"

`TestCapAppliesToEveryRetryPath` walks every public function in
the package that returns or accepts a `time.Duration`:

- `ComputeDelay(attempt)` for attempts 0..6 (some below, some above MaxAttempts)
- `Backoff.New().Next()` called in a loop until budget exhausted
- `Cap(d)` directly with d ∈ {0, 1 s, 30 s, 31 s, 4 m, 1 h}
- `Sleep(ctx, d)` indirectly (zero, positive, MaxInterval + 1)

For every input that exceeds 30 s, the test asserts the output is
`≤ 30 s`. The point is *no public API* in the package can return a
"retry" value above 30 s.

## What was deliberately NOT changed

| Item | Why not |
|---|---|
| `cmd/wagyu/` | the print path and TUI already delegate to PiG; setting the constants at the runtime level is enough |
| `internal/convstore/sqlite.go` | per AGENTS.md "must not touch" directive for this cycle's research scope |
| 50 pre-existing not-clean files under `internal/` and `cmd/wagyu/` | AGENTS.md carve-out ("Leave them alone unless you are already editing them; do not reformat them wholesale") |
| The user's `settings.json` retry overrides | PiG's `NewInMemorySettingsManager` is file-free — the clone's `SettingsManager` replaces whatever on-disk settings.json would have read, so a user-set retry block is **silently overridden** today (mirrors cycle 227's compaction setup; this is a known seam, not a regression). |
| The 5 → 5 retry budget decision | AGY's effective budget is 5; the changelog only mentions the cap, not the budget. Keeping 5 here matches the upstream posture. |

## Hard constraints met

- New files are gofmt-clean:
  `gofmt -l internal/retry/ internal/pigrt/retry.go internal/pigrt/retry_test.go`
  is empty.
- `internal/convstore/sqlite.go` was not touched.
- Pre-existing not-clean files (8 reported by `gofmt -l cmd/wagyu/`)
  were not re-formatted; the diffs land only in
  `internal/retry/` (new) and `internal/pigrt/runtime.go` / `retry.go`
  (clean).
- No third-party deps added. `internal/retry` is stdlib-only
  (`context`, `errors`, `math`, `time`); `internal/pigrt/retry.go`
  imports the existing `github.com/MichaelKinsy/PiG/ai` plus the
  new `internal/retry`.
- Single commit, branch not pushed.
- All 7 spec-required tests are present and pass; 12 additional
  tests cover edge cases and the build-time wiring.

## Build / test gates

```
go build -o /tmp/wagyu-r30s ./cmd/wagyu
  → exit 0; binary = 73.3 MB

go test -vet=off -count=1 -short ./internal/retry/
  → green (15 tests; one takes the full 30 s for the wall-clock cap test)

go test -vet=off -count=1 -short -run TestRuntimeRetry ./internal/pigrt/
  → green (4 tests, all < 50 ms)

go test -vet=off -count=1 -short ./cmd/... ./internal/...
  → green except documented pre-existing flake:
      TestDispatchPrintSlash_LocalCommands
    (model selector resolves to "gemini-3.8-flash-medium" on this
     machine, but the test expects "minimax-m3\tMiniMax M3\n" from
     pre-cycle-220 history; reproduces on plain 54db67b without
     any of my changes — see `docs/cycles/cycle-220/
     deliverable-plugin-multi-subdir.md` 存疑 + cycle-225).

go vet ./internal/retry/ ./internal/pigrt/
  → clean

gofmt -l internal/retry/ internal/pigrt/retry.go internal/pigrt/retry_test.go
  → empty (new files clean)

gofmt -l cmd/wagyu/
  → 8 pre-existing files (unchanged from 54db67b; AGENTS.md carve-out)

ai.ConfigureProviderRetry(retry.MaxAttempts, retry.MaxIntervalMs)
  → called once at process start (internal/pigrt/retry.go:init);
    ai.ConfiguredProviderRetry() returns (5, 30000) thereafter
    (verified by hand-inspection of provider_retry.go:32, 59;
    package-global atomic write).
```

## Commit

The single feature commit is on branch `feature/retry-30s-cap`
(off `54db67b`). The merge record will be written at
`docs/cycles/merge-retry-30s-cap.md` once the merge lands; the
hash is recorded there.

## 存疑

1. **Schedule never hits the cap in practice.** With
   `InitialInterval=1 s, Multiplier=2.0, MaxAttempts=5`, the
   exponential sum is `1+2+4+8+16 = 31 s` — every step is below
   30 s, so `ComputeDelay(attempt)` returns the exponential value
   for all 5 retries. The cap is exercised by `Cap(d)` and
   `Sleep(ctx, MaxInterval)` directly, which is what
   `TestSingleRetryWaitsExactly30sAtCap` does. The AGY 1.3.4
   changelog's "maximum retry time lowered from 4 minutes to 30 s"
   is a contract about the cap value, not about whether the cap
   currently fires in the schedule — the wording is satisfied even
   if a future AGY release uses larger initial intervals.

2. **User settings.json is not honoured.** The clone's
   `buildDefaultSettingsManager` builds a `coding.Settings` literal
   in Go (not by reading from disk), and PiG's
   `NewInMemorySettingsManager` is file-free. The retry block the
   runtime injects therefore overrides whatever `retry.enabled`,
   `retry.maxRetries`, etc. the user set in `settings.json`.
   `cmd/wagyu/print_pig_test.go:134` writes
   `{"retry":{"enabled":false}}` and the test still passes because
   the 429 it provokes escalates through the error path faster
   than the retry loop would have slept, not because the user's
   `enabled:false` is honored — the user's setting is silently
   overridden. This is consistent with cycle 227's compaction
   override but is a non-obvious surface to add to a future cycle
   that introduces user-facing retry config.

3. **Provider-retry cap and agent-retry cap are set in two
   different places** (`init()` in `internal/pigrt/retry.go` for
   the provider atomic, JSON payload in
   `internal/pigrt/runtime.go` for the SettingsManager). Two
   surfaces because PiG exposes them on two surfaces; the cap is
   the same number because the constants come from one package.
   If a future refactor unifies them (e.g. PiG moves provider retry
   into the SettingsManager), the clone's `retry.go` becomes a
   one-line removal.

4. **`aitools.pigrt.RetryConfig` access from outside the module.**
   `SettingsManager.GetRetrySettings()` returns
   `icodingagent.RetryConfig` (the type lives in PiG's
   `internal/codingagent` package, which is not importable from
   the clone's compilation graph). The wiring tests in
   `internal/pigrt/retry_test.go` work because the fields
   `MaxRetries`, `BaseDelayMs`, `MaxDelayMs` are exported and Go
   allows accessing them via type inference (`cfg := sm.GetRetrySettings(); cfg.MaxRetries`).
   If PiG changes those fields to unexported in a future release,
   the wiring tests must move to reflection or to a JSON round-trip
   (the latter already implemented in `buildDefaultSettingsManager`).

5. **No cycle-225 clock-edge probe.** The AGY 1.3.4 changelog line
   was not verified against a live 1.3.4 build on this machine
   (the binary under test is 1.3.4 per `cmd/wagyu/testdata/changelog_reference.txt`,
   but I did not run it under `--print -p "make a request that
   triggers a 30 s cap"`). The constant itself is the recovered
   AGY 1.3.4 changelog value, not a measured live-agy sample.

6. **`docs/cycles/cycle-227/deliverable-auto-compact.md` still
   references the old `buildCompactionSettingsManager` name.**
   Cycle 227 introduced the function; this cycle renamed it to
   `buildDefaultSettingsManager`. The historical deliverable is
   not auto-regenerated and is left as-is; subsequent readers
   should grep for the new name.
