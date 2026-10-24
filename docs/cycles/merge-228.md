# Merge record — cycle 228 (invoke_subagent tool + subagent stub toolset)

**Date:** 2026-10-18
**Cycle:** 228 (single-cycle merge)
**Branch:** `cycle228-invoke-subagent` → `main` (merge commit `8630c18`)
**Cycle-228 commit (post-rebase):** `757d8aa`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w228` (cleaned up)

## TL;DR

Wagyu now ships the AGY 1.4.0 subagent toolset (3 of 5 tools — the others
are scoped to future cycles). `invoke_subagent` is **fully implemented**:
spawns a fresh PiG `coding.Session` (not a `Worker`) on a separate
`coding.Services` triple, sends the prompt as the user message, streams
the response, persists the subagent transcript to
`<AgentDir>/subagents/<id>.jsonl` for a future `/agents` panel. The
`send_message` and `manage_subagents` tools are **documented stubs**
(cycle-228 stub marker literal in every description) — they intentionally
return `{ok: false, error: "not yet implemented (cycle 228 stub)"}` so the
model can advertise the surface without invoking broken tools.

## What landed

| File | Lines | Role |
|---|---|---|
| `internal/subagenttools/subagenttools.go` (new) | +747 | `invoke_subagent` (real delegation), `send_message` / `manage_subagents` (stubs); `Tools(deps)` factory |
| `internal/subagenttools/subagenttools_test.go` (new) | +472 | 8 tests (declaration, scripted end-to-end via `TestFauxProvider`, error paths, all 3 stubs) |
| `internal/subagenttools/fake_provider_test.go` (new) | +238 | scripted PiG provider for `TestFauxProvider` (network-free) |
| `internal/agent/loop.go` | +16 / −X | `Options.ExtraAgentTools []piagent.AgentTool` field; `Loop.pigTools()` merges after the runner-backed tools |
| `internal/agent/pig.go` | +25 / −X | `runMode.subAgentExtras` builds the deps on demand |
| `internal/pigrt/runtime.go` | +7 / −X | `Runtime.Services()` accessor for future cycles preferring single-services |
| `cmd/wagyu/print_pig.go` | +26 / −X | print path: parallel `coding.Services` + `subagenttools.Tools(deps)` appended to `pigrt.Options.ExtraTools` |
| `cmd/wagyu/tui_run.go` | +78 / −0 | wires `extras` into the TUI `agent.Options` struct (combined with cycle-227's `OnCompactionStart`/`OnCompactionEnd`) |
| `cmd/wagyu/tui_tools_test.go` | +24 / −X | cycle-99 `TestTUITools_TurnAdvertisesAndRunsTool` updated from "exactly one tool" to "run_command is one of the tools" — only the surface assertion needed re-pointing (cycle-99 behaviour pinned) |
| `cmd/agy228probe/main.go` (new) | +102 | standalone wire-surface probe (network-free) — the cycle-228 deliverable quotes its output verbatim |
| `docs/cycles/cycle-228/deliverable-invoke-subagent.md` (new) | +377 | TL;DR, scope, PiG API chain, gates, probe-vs-clone, 存疑, hard constraints |

**Total:** 11 files changed, **+2099 / −13**.

## Rebase conflict resolution

Cycle 228 and cycle 227 both touched `cmd/wagyu/tui_run.go` and
`internal/agent/loop.go`. Two conflicts:

### Conflict 1: `internal/agent/loop.go` import alias

Cycle 228 introduced `piagent "github.com/MichaelKinsy/PiG/agent"`; cycle 227
had `"github.com/MichaelKinsy/PiG/agent"` (no alias). Resolution: keep
cycle 228's `piagent` alias, rename cycle 227's 3 code references in
`recordCompact` to use `piagent`:

- `agent.AgentEvent` → `piagent.AgentEvent` (line 298)
- `agent.CompactionStartEvent` → `piagent.CompactionStartEvent` (line 300)
- `agent.CompactionEndEvent` → `piagent.CompactionEndEvent` (line 312)

### Conflict 2: `cmd/wagyu/tui_run.go` `agent.Options` struct literal

Cycle 227 added `OnCompactionStart` / `OnCompactionEnd` callbacks; cycle 228
added `ExtraAgentTools: extras`. Resolution: keep both — different fields
in the same struct literal.

The cycle-228 commit was amended post-rebase to include both resolutions
(final hash `757d8aa`).

## PiG API chain (verbatim from deliverable §3)

```
user invokes invoke_subagent (in main session)
        │
        ▼
subagenttools.invoke_subagent handler
        │
        ├─ create fresh coding.Runtime + coding.Services (independent)
        ├─ create coding.Session (NOT Worker — own transcript)
        ├─ write prompt to Session
        │
        ▼
coding.Session.Send (or Continue)
        │
        ├─ event drain (goroutine) → write to <AgentDir>/subagents/<id>.jsonl
        ├─ final response → Card.Answer
        │
        ▼
return as tool result to parent session
        │
        ▼
Parent transcript gains invoke_subagent tool_call + tool_result pair
        │
        ▼
Subagent's intermediate tool calls NOT visible to parent (synchronous delegation)
```

## Gates verified

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-w228 ./cmd/wagyu` (post-rebase, pre-amend) | exit 0 |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (post-rebase) | one pre-existing flake (`TestCycle225QuotaCountdown`) — verified not cycle-228's fault (passes 2/3 runs on clean main too) |
| `gofmt -l` on all touched `.go` files | empty |
| `go vet ./cmd/wagyu/ ./internal/agent/ ./internal/subagenttools/ ./internal/pigrt/` | only pre-existing cycle-224 vet warnings |
| Stash-dance verify on clean main before merge | build exit 0, test exit 0 (modulo the known flake) |
| Post-merge build + test | build exit 0, test exit 0 (modulo the known flake) |

## Hard constraints honoured

- Did not touch `internal/convstore/sqlite.go` (concurrent session owns it).
- Did not touch `internal/subagent/` (bash exec wrapper, separate concern).
- Pre-existing tests preserved (only the cycle-99 surface assertion was
  re-pointed).
- `~/.gemini/**` SHA byte-identical before/after.
- Worktree-only commit; no `git push`.
- `gofmt -l` clean for all new/touched files (the cycle-228 deliverable
  notes the 50-file pre-existing gofmt-untidy set stays untouched per
  AGENTS.md).

## 存疑 carried into cycle 230+

1. **`send_message` "pipe to running session" PiG-SDK shape** — needs
   `Session.Send` against an existing Session ID; PiG exposes the Session
   handle but not a "route stdin to handle" helper. Future cycle.
2. **`manage_subagents` "list" via JSONL vs index** — could re-scan the
   `<AgentDir>/subagents/` directory on every call, or maintain an index
   in the registry. The TaskRegistry (cycle tasks-panel) suggests index
   path; future cycle 232 picks.
3. **Subagent progress wire-up to parent TUI** — subagent tool calls
   currently invisible to parent transcript. Future cycle 233 adds the
   `<SUBAGENT>` XML wrapper emission (per strings research §4 line 690).
4. **`Deps.PermissionGate` declared but not yet plumbed** — both print
   and TUI paths pass a `PermissionGate: nil`; cycle 235 (define_subagent)
   and cycle 236 (nested subagent depth) may need it.
5. **AGY runtime model confirmation needs decompiled `invoke_subagent`** —
   the strings research hint "Pure orchestrator. Dispatches tasks to
   specialists…" suggests fresh `Agent` instance per subagent (which
   matches cycle-228's design). Future decompile cycle.
6. **Parallel-services load test not done in 228** — spawning N subagents
   in parallel creates N `coding.Services` instances; not load-tested.
   Future cycle.

## Cleanup

After merge to main:
- `git worktree remove --force /tmp/.../openagy-w228` — released ~754 MB
- `git branch -D cycle228-invoke-subagent` — branch removed

Main is now at `8630c18` (merge commit); the cycle-228 commit `757d8aa`
remains reachable through the merge commit's second parent.

## Cycle numbering reconciliation

Cycle 228 was dispatched before the user's "语义化的命名方式而不是数字"
instruction. Branch is `cycle228-invoke-subagent`. New cycles from cycle
230 onward use `feature/<name>`. The semantic-namesake for this work
would be `feature/subagent-delegation`; the existing branch name is
preserved in git history for traceability.

## Next

- 5 subagents have just completed (`feature/codesearch-panel`,
  `feature/tasks-panel`, `feature/effort-picker`,
  `feature/side-questions`, plus cycle 228). Each needs the same
  5-step flow: rebase onto `8630c18`, review, merge, cleanup.
- 2 subagents still in flight (`feature/resume-continuation`,
  `feature/auto-continue-interrupt`).
- Slot strategy: each merge frees a slot; the next completed subagent
  takes the slot.
