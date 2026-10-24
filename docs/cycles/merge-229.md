# Merge record — cycle 229 (manual /compact slash command)

**Date:** 2026-10-18
**Cycle:** 229 (single-cycle merge; no wave)
**Branch:** `cycle229-manual-compact` → `main` (merge commit `bad3726`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w229`

## TL;DR

`/compact` slash command now calls PiG's `Session.Compact(ctx, customInstructions)`
on the active session. The previous behaviour (cycle-167 stub `Compacted N older
message(s) from the wire history`) was an echo-only stub; cycle 229 replaces it
with a real compact invocation that respects PiG's `IsCompacting()` guard and
surfaces `CompactionEndEvent{Reason: "manual"}` token counts in the transcript.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/slash_compact.go` (new) | +288 | `manualCompactor` interface, `pigrtCompactor` adapter, `startManualCompact` |
| `cmd/wagyu/tui_run.go` | +132 / −0 | `compactor` field on `runMode`, `compactDoneMsg` tea.Msg, `appendCompactResult` helper |
| `cmd/wagyu/slash_commands.go` | +18 / −19 | `/compact` branch forwards to `startManualCompact(trimSpace(args))` |
| `cmd/wagyu/slash_cycle229_test.go` (new) | +464 | 8 tests (5 spec-required + 3 bonus) |
| `docs/cycles/cycle-229/deliverable-manual-compact.md` (new) | +288 | TL;DR, scope, PiG API chain, probe-vs-clone, 存疑, hard constraints |

**Total:** 5 files changed, **+1193 / −16** (per the merge diff stat).

## PiG API chain (verbatim from deliverable §3)

```
User types `/compact <text>`
        │
        ▼
startManualCompact(instr)  (cmd/wagyu/slash_compact.go)
        │
        ├─ IsCompacting() == true → echo "Already compacting." + return
        │
        ├─ start goroutine:  compactor.Compact(ctx, instr)
        │                    (pigrtCompactor adapts to coding.Session.Compact)
        │
        ▼
PiG Session.Compact (coding/session.go:2288)
        │
        ├─ emit agent.CompactionStartEvent
        ├─ run summarization (LLM-backed or rule-based)
        ├─ emit agent.CompactionEndEvent{TokensBefore, TokensAfter, Reason:"manual"}
        │
        ▼
Event drain (internal/pigrt/runtime.go event channel)
        │
        ▼
pigrtCompactor receives End event, posts compactDoneMsg{before, after, err}
        │
        ▼
tui_run.go's compactDoneMsg handler → appendCompactResult → transcript row
        │
        ▼
Transcript: ⎿  Compacted: 184000 → 42000 tokens (77.2% reduction)
```

## Gates verified

- `go build -o /tmp/wagyu-main ./cmd/wagyu` (clean main, before merge) — exit 0
- `go test -vet=off -count=1 ./cmd/... ./internal/...` (clean main, before merge) — fully green
- `go build -o /tmp/wagyu-post229 ./cmd/wagyu` (post-merge) — exit 0
- `go test -vet=off -count=1 ./cmd/... ./internal/...` (post-merge) — fully green
- 27 packages × ~750 tests — no regressions
- `~/.gemini/{settings.json,google_accounts.json,installation_id}` SHA-256 byte-identical before/after

## REF probes (220×50 + 220×30)

REF probes were attempted but blocked by Google OAuth + zero quota on
`navigaid@gmail.com`; the cycle-229 deliverable documents this as 存疑 §1.
`agy229/ref/` is empty by design.

**Our probes (in `agy229/our/`):**
- `compact-empty.220x50.raw.txt` — bare `/compact` on empty session
- `compact-empty.220x30.raw.txt` — bare `/compact` on empty session, narrow
- `compact-with-instr.220x50.raw.txt` — `/compact focus on cycle 229 work`
- `compact-with-instr.220x30.raw.txt` — same, narrow

All four captured the cycle-167 stub because `m.compactor == nil` in the
unmerged `openagy-w229` worktree (cycle 227's plumbing lives on `openagy-w227`,
which is still in flight). The test suite is the source of truth for the
compactor-armed path; once cycle 227 merges, the probes will re-run and the
styled rows will appear.

## 存疑 (from cycle-229 deliverable §6)

1. **REF probes blocked** — `~/.local/bin/agy` 1.4.0 gated behind Google OAuth.
2. **Sync vs async goroutine model** — manual `/compact` runs on a goroutine;
   fast follow-up presses race the in-flight compact (PiG's
   `beginManualCompaction` loops).
3. **Trailing-text picker** — typing `/compact focus on …` shows the
   cycle-151 picker overlay on the matched prefix; `Escape` first, then `Enter`.
4. **`Already compacting.` is best-effort UX** — short-circuits only the
   user-facing transcript; PiG itself would happily abort-and-restart.
5. **Mid-stream `/compact` during tool call** — PiG's `compactCancel` handles
   cancellation; cycle 229 does not add its own.
6. **Zero-token fallback** — `CompactionEndEvent` with both counts zero renders
   `"⎿  Compacted: 0 → 0 tokens"` with no percentage (pinned by
   `TestSlashCompactZeroTokens`).

## Hard constraints honoured

- Did not touch `internal/pigrt/runtime.go`, `internal/agent/pig.go` (cycle-227
  work), or `internal/convstore/sqlite.go`.
- TMUX socket `-L agy229` only; `kill-server` invoked at end (clean).
- No destructive `/rewind` probe; `~/.gemini/**` byte-identical.
- Worktree-only commit; no `git push`.
- `gofmt -l` clean for all new/touched files.

## Cycle numbering reconciliation

Cycle 229 used the pre-semantic-naming numeric format (it was dispatched
before the user's "语义化的命名方式而不是数字" instruction). From this merge
record forward, **new cycles use semantic branch names** (`feature/<name>`)
and the merge record titles reference the semantic name when applicable.

The strings-research note (`docs/research/agy-feature-strings.md`, commit
`db8b2ed`) uses research-stage cycle anchors (228/229/230 for manual-compact
/ orchestrator / EPHEMERAL) that don't bind to the actual cycle numbers —
those anchors are valid evidence regardless of cycle numbering. Cycle 229
covers the manual-`/compact` anchor from that research note.

## Next

- Cycle 227 (auto-compact wiring) still in flight in `openagy-w227`.
- Cycle 228 (invoke_subagent) still in flight in `openagy-w228`.
- 5 semantic-named cycles in flight: side-questions, codesearch-panel,
  resume-continuation, tasks-panel, effort-picker.
- Slot just opened → dispatching `auto-continue-on-stream-interrupt` next
  (semantic name `feature/auto-continue-interrupt`; addresses the user's
  original "long tool-call chain interruption" pain point — root cause #2:
  stream interrupt with no auto-Continue).
