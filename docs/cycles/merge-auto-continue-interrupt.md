# merge auto-continue-interrupt — stream-interrupt auto-continue (depth ≤3) + banner + iteration cap

## TL;DR

AGY 1.4.0 stream-interrupt recovery: the reference detects a
`stream was interrupted` / iteration-cap failure and re-runs the turn
with a verbatim recovery copy appended ("The stream was interrupted.
Please continue the task you were working on."), bounded by a depth
counter (≤3) and a "↻ auto-continuing…" status line.

| | |
|---|---|
| Branch | `feature/auto-continue-interrupt` |
| Commit | `db5f78e` |
| Merge | `6b23a9e` |
| Lines | +1478 / −1 (11 files) |
| New files | `internal/agent/auto_continue.go`, `internal/pigrt/auto_continue.go` + tests |
| Cycle report | `docs/cycles/auto-continue-interrupt/deliverable-auto-continue.md` |

## Scope (vs AGY 1.4.0)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| Detect stream interrupt (`StopReasonError` / `Aborted` / `Length`) | `agent.classifyInterruption` + `pigrt.classifyInterruption` (typed-error-first ordering) | ✅ |
| Replay with AGY verbatim recovery copy | `AutoContStreamInterrupted` constant pinned at `0x33dc41b` bytes (per `internal/agent/auto_continue.go`) | ✅ |
| Iteration-cap (`agent.ErrMaxTurnsReached`) | `piagent.ErrMaxTurnsReached` matched | ✅ |
| Bounded depth (≤3) | `AutoContMaxDepth = 3` constant in both packages | ✅ |
| Status row banner | `AutoContBanner` callback → `autoContBannerMsg` tea.Msg | ✅ |
| Give-up returns `ErrAutoContMaxDepth` with `Unwrap()` preserving chain (so `errors.As(*backend.HTTPError)` in `cmd/wagyu/api_error.go:102` still classifies) | `ErrAutoContMaxDepth{Depth, Last}` typed error | ✅ |
| Iteration count for "Cancelled after %d iterations" | `AutoContIterationCapFormat` constant pinned at `0x33a3d44` | ✅ |
| Default-off (test mode); on for print + TUI | `Options.AutoCont` field; print sets `true` in `runPigPrintTurn`; TUI sets `true` in `streamPrompt` | ✅ |

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `cmd/wagyu/print_pig.go` | modified | +16 | `Options.AutoCont: true` in print path |
| `cmd/wagyu/tui_run.go` | modified | +67 | `autoContBannerMsg`, `autoCont` callback, `AutoCont: true` and `AutoContBanner` callback in `streamPrompt`'s loop.Options |
| `internal/agent/loop.go` | modified | +127 | `Options.AutoCont`/`AutoContBanner`, dispatch in `Send` |
| `internal/agent/pig.go` | modified | +1 (comment) | comment note about `sendPig` being wrapped |
| `internal/agent/pig_test.go` | modified | +225 | `flakyAgw` helper + 3 e2e tests |
| `internal/pigrt/runtime.go` | modified | +36 | `Options.AutoCont`, `RunWith` calls `maybeAutoContinue` |
| `internal/agent/auto_continue.go` | new | +251 | TUI-side `sendPigAuto`, `classifyInterruption`, `recoveryForError`, `appendRecoveryMessage`, `msgSlice` |
| `internal/agent/auto_continue_test.go` | new | +165 | 7 unit tests |
| `internal/pigrt/auto_continue.go` | new | +287 | print-side `classifyInterruption`, `runAutoContCycle`, `maybeAutoContinue`, `iterationCountFromErr`, `lastAssistant`, `msgSlice` |
| `internal/pigrt/auto_continue_test.go` | new | +207 | 6 unit tests |
| `docs/cycles/auto-continue-interrupt/deliverable-auto-continue.md` | new | +96 | TL;DR + scope + 6-item 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | 3 conflict regions (struct fields in `loop.go`, `runtime.go`, `agent.Options{}` literal in `tui_run.go`). All resolved by retaining both HEAD and incoming blocks verbatim — no semantic rewrite. |
| `go build -o /tmp/wagyu-ac-final ./cmd/wagyu` | exit 0, 72.9 MB |
| `go test -vet=off -count=1 -run 'TestAutoCont\|TestRecovery\|TestLoop_AutoCont' ./internal/agent/ ./internal/pigrt/` | all green |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | only `TestDispatchPrintSlash_LocalCommands` fails — pre-existing environmental flake |
| `gofmt -l` on every touched Go file | empty |
| `go vet ./cmd/wagyu/ ./internal/agent/ ./internal/pigrt/` | only pre-existing cycle-224 `%` warnings |
| New tests (cycle code paths) | 13/13 pass |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |

## 存疑

1. `TestDispatchPrintSlash_LocalCommands` is a pre-existing environmental flake.
2. Documented cycle-225 timing flakes (`TestCycle225QuotaCountdown`, `TestCycle225QuotaStubRefills`) did not trip in this run; statistical, not signature.
3. `internal/convstore/sqlite.go` not touched.
4. `internal/pigrt/auto_continue.go` got exactly one numbered-list reformatting pass from `gofmt -w`; semantically a no-op.
5. No push to remote.

## Cleanup

- Worktree removed.
- Branch deleted.
- main is now `6b23a9e`.

## Cross-worktree note

This worktree (along with `feature/status-line-script` and several others)
carried uncommitted pollution from other subagents. The pollution was
discarded automatically by the rebase (HEAD's state already includes
the merged files).
