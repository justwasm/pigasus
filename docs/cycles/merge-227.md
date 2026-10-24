# Merge record — cycle 227 (PiG auto-compact + TUI status row events)

**Date:** 2026-10-18
**Cycle:** 227 (single-cycle merge)
**Branch:** `cycle227-auto-compact` → `main` (merge commit `3c3ead6`)
**Cycle-227 commit (post-rebase + gofmt amend):** `c61cabd`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w227` (cleaned up)

## TL;DR

PiG's `Session.CheckPromptCompaction` (auto-compact at threshold) is now
**wired** in wagyu via `pigrt.Options.CompactEnabled *bool` /
`pigrt.Options.CompactThreshold float64`. When a compaction fires, the
runtime surfaces `agent.CompactionStartEvent` / `agent.CompactionEndEvent`
through the event drain to a new bubbletea status-row overlay (`Compacting
context… 184 K → 42 K (3 s)`), with a 100 ms `compactSpinnerTickMsg` cadence
matching the cycle-225 quota spinner. The cycle-99 `TestTUITools` test had
its one-line surface assertion updated (the *only* diff to a pre-existing
test) because adding tool declarations no longer leaves "exactly one tool"
in scope.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/print_pig.go` | +79 / −0 | print-path observer: prints `⎿  Compacting context… 184 K → 42 K tokens` to stdout |
| `cmd/wagyu/slash_cycle227_test.go` (new) | +154 | TUI integration tests |
| `cmd/wagyu/slash_viewer_panels.go` | +10 / −0 | `compactSpinnerTick` tea.Cmd |
| `cmd/wagyu/tui_run.go` | +202 / −1 | `compactStartMsg` / `compactEndMsg` / `compactSpinnerTickMsg` handlers; status-row override rendering |
| `docs/cycles/cycle-227/deliverable-auto-compact.md` (new) | +193 | TL;DR, scope, PiG API chain, 存疑, hard constraints |
| `internal/agent/loop.go` | +108 / −0 | `Options.OnCompactionStart` / `OnCompactionEnd`; `Loop.lastCompact`; `recordCompact` helper |
| `internal/agent/pig.go` | +21 / −X | `chunkSink.observe` routes `CompactionStart` / `CompactionEnd` to Loop's record |
| `internal/pigrt/cycle227_test.go` (new) | +305 | runtime tests |
| `internal/pigrt/runtime.go` | +253 / −6 | `Options.CompactEnabled` / `CompactThreshold`; `Runtime.lastCompact`; `LastCompact()` / `IsCompacting()` / `InjectForTest`; `buildCompactionSettingsManager` (JSON round-trip of `Settings.Compaction` since `*icodingagent.CompactionSettingsJSON` is internal) |

**Total:** 9 files changed, **+1320 / −6** (per the final cycle-227 commit).

## Rebase conflict resolution

Cycle 227 and cycle 229 both touched `cmd/wagyu/tui_run.go`. The conflict
was confined to one block of message-type declarations (around line 2518):
cycle 229 added `compactDoneMsg` for the manual `/compact` path; cycle 227
added `compactStartMsg` / `compactEndMsg` / `compactSpinnerTickMsg` for the
auto-compact path. Both messages are **complementary** (manual fires
`Reason: "manual"`; auto fires `Reason: "threshold"`); the bubbletea `Update`
method had already auto-merged both case clauses (lines 2235, 2287, 2301,
2313 in the rebased file). The conflict was resolved by keeping both type
declaration blocks verbatim. A single missing blank line between
`type compactSpinnerTickMsg struct{}` and `// replaceRange` was caught by
`gofmt -l` during review and fixed with `gofmt -w`; the cycle-227 commit
was amended to include the fix (final hash `c61cabd`).

## PiG API chain (verbatim from deliverable §3)

```
Session.Send(ctx, transcript, opts)
        │
        ▼
coding.Session.CheckPromptCompaction
        │
        ├─ settings.Compaction.Enabled == false → no-op
        ├─ threshold not yet hit → no-op
        │
        ▼ (threshold hit)
        ├─ emit agent.CompactionStartEvent{StartedAt}
        ├─ run summarization
        ├─ emit agent.CompactionEndEvent{
        │     TokensBefore, EstimatedTokensAfter, Aborted, Summary, Reason: "threshold"}
        │
        ▼
pigrt runtime event drain (channel, 128-buffer)
        │
        ▼
Runtime.recordCompactionEvent (writes lastCompact)
        │
        ▼
pigrt prints ⎿  Compacting context… 184 K → 42 K tokens (print path)
or
agent.Loop → runMode.compactStartMsg → status-row overlay + spinner tick (TUI path)
        │
        ▼
Transcript / status row renders the compaction result
```

## Gates verified

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-w227 ./cmd/wagyu` (post-rebase, pre-amend) | exit 0 |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (post-rebase) | fully green |
| `gofmt -l` on all touched files (post-amend) | empty |
| `go vet ./cmd/wagyu/ ./internal/agent/ ./internal/pigrt/` | only pre-existing cycle-224 vet warnings |
| Stash-dance verify on clean main before merge | build exit 0, test exit 0 |
| Post-merge build + test | build exit 0, test exit 0 |
| 27 packages × ~750 tests | no regressions |

## Hard constraints honoured

- Did not touch `internal/convstore/sqlite.go` (concurrent session owns it).
- Did not modify pre-existing tests except the one-line surface assertion
  update in cycle 99's `TestTUITools_TurnAdvertisesAndRunsTool` (pinned
  cycle-99 behaviour; only the assertion changed because adding tool
  declarations no longer leaves "exactly one tool" in scope).
- `~/.gemini/**` SHA byte-identical before/after (no probe mutated state).
- Worktree-only commit; no `git push`.
- `gofmt -l` clean for all new/touched files (after amend).

## 存疑 carried into cycle 228+

1. **`icodingagent.CompactionSettingsJSON` is internal to PiG** — cycle 227
   round-trips through `json.Unmarshal` into `coding.Settings.Compaction`.
   Future cycle can revisit if PiG exposes a public hook.
2. **PiG `Subscribe` is consumer-only** — cycle 227 added
   `Runtime.InjectForTest` for unit tests; production path is purely
   consumer. Drain-to-channel wiring is proven by `TestRuntimeRunsFirstTurn`.
3. **Two pre-existing timing flakes** (`TestWrapRowsQuadratic`,
   `TestCycle225QuotaCountdown`) — pass in isolation, occasionally fail in
   full suite. Neither references cycle-227 changes; flagged for a future
   hardening cycle (not blocking).
4. **Status-row geometry borrows the left-label slot** — terminals narrower
   than ~30 cells would clip the message. The byte-exact 220-column pad
   from cycle 87 still floors the row.
5. **REF probe-vs-clone table is empty** — the spec's
   `tmux -L agy227 send-keys '/' 'compact' Enter` triggers `/compact` (the
   manual path, cycle 229), not the auto-compact path (which fires on
   context exhaustion, not slash command). A forced-compact probe needs a
   long resumed conversation outside the cycle's scope. The cycle-225
   regression suite keeps passing byte-for-byte as the proxy
   REF-compatibility anchor.

## Cleanup

After merge to main:
- `git worktree remove --force /tmp/.../openagy-w227` — released ~685 MB
- `git branch -D cycle227-auto-compact` — branch removed

Main is now at `3c3ead6` (merge commit); the cycle-227 commit `c61cabd`
remains reachable through the merge commit's second parent.

## Cycle numbering reconciliation

Cycle 227 was dispatched before the user's "语义化的命名方式而不是数字"
instruction, so it used the numeric branch name `cycle227-auto-compact`. The
git history is preserved as-is. From cycle 230 onwards, **new cycles use
semantic branch names** (`feature/<name>`).

## Next

- Cycle 228 (`invoke_subagent`) just completed; needs the same 5-step
  flow (rebase onto `3c3ead6`, review, merge, cleanup).
- 7 other subagents still in flight (side-questions, codesearch-panel,
  resume-continuation, tasks-panel, effort-picker, auto-continue-interrupt,
  cycle 228 if not yet merged).
- Slot strategy: when a slot opens, dispatch from the queue (next candidate
  is `agents.json-in-plugins` after cycle 228 merges).
