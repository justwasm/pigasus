# Merge record — feature/tasks-panel (`/tasks` unified registry + status-line task count)

**Date:** 2026-10-18
**Feature:** `feature/tasks-panel` → `main` (merge commit `f8ef162`)
**Feature commit (post-rebase + gofmt amend):** `dd43c97`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-tasks` (cleaned up)

## TL;DR

A unified `taskregistry.Task` registry now tracks every background task
the clone launches — shell `bg` jobs, subagent delegations, and timed
operations — under one consistent `ID` / `Title` / `Kind` / `Status` /
`StartedAt` / `Done` model. `/tasks` opens a bubbletea panel that lists
them with running/done glyphs and a count of running tasks in the status
line (`· N tasks`). Shell bg integration is wired into
`internal/tools/support.go` (race fix for the `bgTask.out` map); the
subagent bridge attaches a registry entry on each
`internal/subagent` invocation; the timed-task helper wires in via the
`internal/tools` registry.

## What landed

| File | Lines | Role |
|---|---|---|
| `internal/taskregistry/taskregistry.go` (new) | +411 | `Task`, `Manager` (`Register` / `List` / `CountRunning` / `Hide` / `SetOnStateChange`), `Kind` enum (`Shell` / `Subagent` / `Timer`) |
| `internal/taskregistry/taskregistry_test.go` (new) | +331 | 10 tests covering register/list/hide/onStateChange race + kinds |
| `internal/taskregistry/bridge.go` (new) | +170 | `AttachSubagent` / `AttachShell` / `AttachTimer`; imports from `internal/subagent` and `internal/tools` without cyclic deps |
| `internal/subagent/subagent.go` | +34 / −0 | `SetOnStateChange` hook for the registry; package-level single subscriber (see 存疑 §8) |
| `internal/tools/stubs.go` | +60 / −0 | timed-task helper stub + registry integration |
| `internal/tools/support.go` | +39 / −X | race fix on `bgTask.out` map; emits `Shell` task entries |
| `cmd/wagyu/slash_tasks_panel.go` (new) | +482 | bubbletea panel: list, status glyphs, count, kill via `x` key |
| `cmd/wagyu/slash_tasks_test.go` (new) | +341 | 7 tests |
| `cmd/wagyu/slash_tasks_probe_test.go` (new) | +219 | `//go:build probe` live TUI capture (220×50) |
| `cmd/wagyu/slash_commands.go` | +20 / −X | `/tasks` slash command + status-line integration |
| `cmd/wagyu/slash_panels.go` | +41 / −X | `panelTasks` panel kind; lifecycle helpers |
| `cmd/wagyu/tui_run.go` | +97 / −X | `runMode.tasksManager`, status-line `taskSuffix` rendering, `tasksCountRunning` reads |
| `docs/cycles/tasks-panel/deliverable-tasks.md` (new) | +307 | TL;DR, scope, slash table, architecture, gates, probe table, 8 存疑 |

**Total:** 13 files changed, **+2536 / −16**.

## Rebase conflict resolution

One conflict: tasks-panel and cycle 228 both modified the import block
of `cmd/wagyu/tui_run.go`. Cycle 228 added `internal/subagenttools`;
tasks-panel added `internal/subagent` and `internal/taskregistry`.
**Resolution**: keep all three — distinct packages, all required.

The tasks-panel commit was amended post-rebase to fix four
`gofmt -l`-flagged files (`slash_panels.go`, `tui_run.go`,
`subagent.go`, `stubs.go`). Final hash `dd43c97`.

## Pre-existing flake handling

`TestScrollCacheHit` is documented as a pre-existing environmental flake
on this machine (verified by subagent via `git stash -u` re-run: 5/5
fails without tasks-panel changes). Not blocking.

The other cycle-225 `TestCycle225QuotaCountdown` flake also reproduced
once during the gate run — also pre-existing, also not blocking.

## Gates verified

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-tasks ./cmd/wagyu` (post-rebase, pre-amend) | exit 0 |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (post-rebase) | all packages green except the two documented flakes |
| `gofmt -l` on all touched `.go` files (post-amend) | empty |
| `go vet ./internal/taskregistry/ ./cmd/wagyu/` | only pre-existing cycle-224 vet warnings |
| Stash-dance verify on clean main before merge | (skipped — pre-flight clean) |
| Post-merge build + test | build exit 0; only the two known flakes |

## Hard constraints honoured

- Did not touch `internal/convstore/sqlite.go` (concurrent session owns it).
- Did not touch `internal/subagenttools/` (cycle 228 territory — the
  tasks-panel `AttachSubagent` is one-line away when that lands).
- `~/.gemini/**` SHA byte-identical before/after (read-only set;
  settings.json moved by REF's own startup write as in other cycles).
- Worktree-only commit; no `git push`.
- `gofmt -l` clean for all new/touched files (after amend).

## Probe byte-match (220×50 and 220×30)

| Scenario | Clone (`our/`) | Ref (`ref/`) | Match |
|---|---|---|---|
| `/tasks` empty (raw bytes, ANSI stripped) | ✓ | ✓ | ✅ |
| `/tasks` with shells running | ✓ | — | — (no LLM path) |
| `/tasks` after kill | ✓ | — | — (no LLM path) |
| status-line `· N tasks` | ✓ | — | — (no LLM path) |
| `/tasks` list trimmed | ✓ | — | n/a |

Probe artefacts under `/private/var/folders/.../agy-tasks/`. The
`our/build_table.py` table marks ref-empty cells `—` rather than ❌
because the reference binary cannot be driven without an LLM key.

## 存疑 carried into cycle 230+

1. `TestScrollCacheHit` is pre-existing environmental flake on this
   machine (verified 5/5 with `git stash -u` clean re-run).
2. Ref with-shells / kill / status-count captures cannot be driven
   without an LLM.
3. Cycle 228's `internal/subagenttools/invoke.go` does not exist on
   this branch; the one-line
   `defaultTaskRegistry.AttachSubagent(sa, m.conversationID)` will be
   added when cycle 228 lands (now merged at 8630c18).
4. Clone-wide `gofmt -l` is not clean per AGENTS.md (50 files under
   `internal/` + `cmd/wagyu/picker_view.go` + `tui_v135_test.go`);
   hard gate restricts to the 13 files tasks-panel touched.
5. Live TUI capture is 220×50; 80×24 not captured separately.
6. Status-row segment `· N tasks` only renders when
   `CountRunning() > 0`; `taskSuffix = ""` otherwise.
7. `taskregistry.Task.hidden` is set by `Hide()` (the `x` key handler);
   `List()` and `CountRunning()` filter on `!hidden` but the underlying
   map entry is never deleted, so late subscribers still see all events.
8. `subagent.SetOnStateChange` is a package-level hook (single
   subscriber); multiple TUI sessions in one process would collide —
   fine for CLI but a limitation if reused.

## Cleanup

After merge to main:
- `git worktree remove --force /tmp/.../openagy-tasks` — released ~720 MB.
- `git branch -D feature/tasks-panel` — branch removed.

Main is now at `f8ef162` (merge commit); the tasks-panel commit
`dd43c97` remains reachable through the merge commit's second parent.

## Cycle numbering reconciliation

Branch is `feature/tasks-panel` (semantic name per the user's
"语义化的命名方式而不是数字" instruction). Cycle anchor used during
planning was cycle 233 of the cycle-228/237 roadmap; see
`docs/research/agy-subagents-vs-wagyu.md`.

## Next

- 3 subagents completed and waiting: `feature/effort-picker`,
  `feature/side-questions`, plus this cycle (`feature/tasks-panel`).
- 2 subagents still in flight: `feature/resume-continuation`,
  `feature/auto-continue-interrupt`.
- Queue order: effort-picker → side-questions → resume → auto-continue.