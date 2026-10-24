# Merge record — feature/side-questions (`/btw` side-question card + `/copy btw`)

**Date:** 2026-10-18
**Feature:** `feature/side-questions` → `main` (merge commit `338188e`)
**Feature commit (post-rebase + gofmt amend):** `cf6bb13`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-side-questions` (cleaned up)

## TL;DR

`/btw <question>` opens an independent PiG `coding.Session` (own
`coding.Runtime` + `coding.Services` + `coding.Session` triple), sends
the question as a one-shot user message, and renders the response as
a *side-question card* in the transcript. The card persists across
resumes (session-scoped), supports `Enter` to expand, `c` / `/copy
btw` to copy the latest answer, and `x` to dismiss. Per-Ask drainage
is per-Session, not the parent's — `internal/sidequestion` owns the
triple lifecycle.

## What landed

| File | Lines | Role |
|---|---|---|
| `internal/sidequestion/sidequestion.go` (new) | +390 | `Card` + `Manager.Ask/List/Get/Latest/LatestDone`; per-Ask Services+Runtime+Session triple; drain goroutine; `StopReasonError`/`StopReasonAborted` recovery |
| `internal/sidequestion/sidequestion_test.go` (new) | +258 | 7 tests |
| `cmd/wagyu/slash_btw.go` (new) | +240 | TUI seam: `runBtwSideQuestion`, `copyBtwCard`, `activeBtwCard`, `BtwCardExpanded`, `ToggleBtwCardExpansion`, `CollapseBtwCard`, `btwManagerFor`, `btwModel`, `newBtwManager` factory hook |
| `cmd/wagyu/slash_btw_test.go` (new) | +336 | 8 tests |
| `cmd/wagyu/slash_commands.go` | +37 / −12 | `case "btw"` → `runBtwSideQuestion`; `case "copy"` dispatch for `/copy btw` |
| `cmd/wagyu/slash_actions.go` | +35 / −17 | `resolveCopy` btw branch removed (now `runMode.copyBtwCard`'s job) |
| `cmd/wagyu/slash_actions_test.go` | +19 / −X | `TestSlashCopy_ArgumentForms` and `TestResolveCopy_Forms` no longer cover the btw subcase |
| `cmd/wagyu/tui_run.go` | +18 / −0 | `runMode` gets `btwManager`, `btwCards`, `activeBtwID`, `btwExpanded`; `sidequestion` import added |
| `docs/cycles/cycle-230/deliverable-btw.md` (new) | +255 | slash command table, architecture diagram, gates, probe-vs-clone, 存疑 |

**Total:** 9 files changed, **+1542 / −46**.

## Rebase conflict resolution

One conflict: side-questions, cycle 228, tasks-panel all touched the
import block of `cmd/wagyu/tui_run.go`. Cycle 228 added
`internal/subagenttools`; tasks-panel added `internal/subagent` +
`internal/taskregistry`; side-questions added `internal/sidequestion`.
**Resolution**: keep all four — distinct packages.

The side-questions commit was amended post-rebase to fix one
`gofmt -l`-flagged file (`tui_run.go` line 45 — missing tab on the
`sidequestion` import line; the manual conflict resolution had
inadvertently dropped the leading `\t`). Final hash `cf6bb13`.

## Gates verified

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle230-final ./cmd/wagyu` (post-rebase) | exit 0, 72 MB |
| `go build -tags nocodemode -o /tmp/wagyu-cycle230-nocodemode ./cmd/wagyu` | exit 0, 67 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (post-rebase) | all 32 packages green |
| `gofmt -l` on all touched `.go` files (post-amend) | empty |
| Post-merge build | exit 0 |
| Cycle-230 specific tests | green |

## Hard constraints honoured

- Did not touch `internal/convstore/sqlite.go` (concurrent session owns it).
- Did not touch `internal/subagent/` (bash exec wrapper, separate concern).
- `~/.gemini/**` SHA byte-identical before/after for the read-only set;
  only `~/.gemini/antigravity-cli/settings.json` moved (REF's own
  startup write).
- Worktree-only commit; no `git push`.

## Probe byte-match (220×50 and 220×30)

| Scenario | Clone (`our/`) | Ref (`ref/`) | Match |
|---|---|---|---|
| `/btw` no-args | `⎿ /btw needs a question.` | `⎿ Usage: /btw <question>` | **diverges** by cycle-230 contract |
| `/btw what is 2+2` (stream) | `btw · what is 2+2` / `2 + 2 = **4**` / `· 209 tok` | `┃ Error: stream timed out after 60s` / `┃ Press Enter/Esc to dismiss` | REF timed out at 60s SLO — not OUR regression |
| `/copy btw` with card | `Copied btw response (209 tok) for "what is 2+2" to clipboard.` | `The /btw question failed: stream timed out after 60s` | diverges (cycle-230 keeps no failed cards) |
| `/copy btw` no card | `No active btw response to copy.` | `No active btw response to copy.` | **matches** |

## 存疑 carried into cycle 230+

1. **PiG's `coding.Runtime` does allow multi-Session** — cycle 230
   deliberately uses one Runtime per Ask (no sharing, no warm-up tax).
2. **`/copy btw` recovery diverges from REF when prior `/btw` failed**
   (clone drops failed cards).
3. **`Card.Tokens` uses `Usage.TotalTokens` with `Usage.Output` fallback**;
   whether agw gateway populates `TotalTokens` is unverified (live
   probe timed out).
4. **Cards are session-scoped, not persisted**; `internal/convstore` is
   read-only by cycle-175 design.
5. **In-flight rendering not implemented**; `Ask` is synchronous from
   TUI's view.
6. **`PIG_TEST_FAUX_SCENARIO=not-a-real-scenario` is the only faux
   error short-circuit**; other failure shapes are not mocked.

## Cleanup

After merge to main:
- `git worktree remove --force /tmp/.../openagy-side-questions` —
  released ~720 MB.
- `git branch -D feature/side-questions` — branch removed.

Main is now at `338188e` (merge commit); the side-questions commit
`cf6bb13` remains reachable through the merge commit's second parent.

## Cycle numbering reconciliation

Branch is `feature/side-questions` (semantic name). Cycle anchor used
during planning was cycle 230 of the cycle-228/237 roadmap; see
`docs/research/agy-subagents-vs-wagyu.md`.

## Subagent quota incident (mid-cycle)

The two still-in-flight subagents (`feature/resume-continuation` and
`feature/auto-continue-interrupt`) **died mid-cycle** with
`已达到 Token Plan 用量上限 (2056)` — both subagents were dispatched
under `agw/MiniMax-M3` and ran into the same Token Plan cap that
ended the previous wave (cycle 211–214). No commits were produced
from either; both worktrees are still at `db8b2ed` / `c60cc26`
(respectively). Resuming them needs the user to lift the Token Plan
cap, then re-dispatch.

## Next

- 2 dead subagents (no commits): `feature/resume-continuation`,
  `feature/auto-continue-interrupt` — pending Token Plan lift.
- 1 unexpected worktree: `feature/custom-statusline` (db8b2ed) —
  status unclear; verify before merge.
- Queue order once quota returns: resume → auto-continue → statusline.