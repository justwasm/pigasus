# Cycle 229 — manual `/compact` slash command wired to PiG `Session.Compact()`

## Goal

Cycle 167 shipped `/compact` as a placeholder that trimmed the older half of
`m.session` in-memory and printed `"Compacted N older message(s)"` /
`"Nothing to compact yet."`. REF 1.4.0's `/compact` actually drives PiG's
full summarisation pipeline (`Session.Compact(ctx, customInstructions)` →
summarisation call → `CompactionEndEvent{TokensBefore, EstimatedTokensAfter,
Aborted, Reason, ErrorMessage}`). Cycle 229 closes that gap: the manual
`/compact` slash command fires PiG's real compact, listens for the matching
end event, and surfaces the upstream token counts in the transcript.

This is the user-driven companion to cycle 227's auto-compact plumbing.
Cycle 227 owns `internal/pigrt/runtime.go`'s `lastCompact` / `compactActive`
record (the bookkeeping PiG's own events feed) and the TUI's
`recordCompact` hook in `internal/agent/pig.go`. Cycle 229 ships only the
slash-command seam so a later cycle can wire the compactor from the print
path or the TUI without revisiting this file.

## Scope (what cycle 229 changes)

1. **New seam `manualCompactor`** — a three-method interface
   (`Compact`, `IsCompacting`, `SubscribeManual`) plus a `pigrtCompactor`
   adapter over `*pigrt.Runtime.Session()`. Production callers
   (cycle-227 TUI, future print-path wiring) populate `runMode.compactor`
   with a `pigrtCompactor`; tests use a `fakeCompactor` that records calls
   and replays synthetic `CompactionEndEvent`s.
2. **`startManualCompact` method on `runMode`** — three branches:
   - `m.compactor == nil` → cycle-167 stub (`compactSession` helper,
     `"Nothing to compact yet."` for empty sessions, `"Compacted N older
     message(s) from the wire history"` otherwise). This is the path
     live in the `w229` worktree today because the TUI plumbing from
     cycle 227 lives on `openagy-w227`, unmerged.
   - `m.compactor.IsCompacting() == true` → short-circuit with
     `"⎿  Already compacting."` (the cycle-229 spec calls out the race
     where a user types `/compact` while cycle 227's auto-compact is
     mid-flight).
   - happy path → emit `"⎿  Compacting context…"`, subscribe a one-shot
     listener on the compactor, fire `Compact(ctx, customInstructions)`
     in a goroutine with a 60 s timeout, post a `compactDoneMsg` back
     to the bubbletea program when the matching end event arrives.
3. **`/compact <instr>` forwards `<instr>` as `customInstructions`** —
   the trailing text after `/compact ` is `trimSpace`'d and passed
   straight through to `Compact`. Empty string for argument-less
   `/compact`.
4. **`compactDoneMsg` tea.Msg + `appendCompactResult` helper** — the
   end-event payload becomes two transcript rows: the running
   `"Compacting context…"` (already in `m.response` when the goroutine
   fires) and a styled `"⎿  Compacted: <before> → <after> tokens
   (<X.X%> reduction)"` row. The reduction percentage is computed
   `1 − TokensAfter/TokensBefore`; if both are zero (an aborted compact
   or one that never recorded counts) the suffix is omitted and the row
   reads `"⎿  Compacted: 0 → 0 tokens"`.
5. **Error path** — `Compact` returning a non-nil error posts
   `compactDoneMsg{Err: err}`; `appendCompactResult` renders
   `"⎿  Compact failed: <err>"`. A `CompactionEndEvent{ErrorMessage:
   "..."}` from PiG becomes `Err = errors.New(ErrorMessage)` before the
   goroutine posts it.

## Files changed

| File | +/− | Why |
|---|---|---|
| `cmd/wagyu/slash_compact.go` | **new, 288** | `manualCompactor` interface; `pigrtCompactor` adapter over `*pigrt.Runtime`; `startManualCompact` method on `runMode` (three branches + goroutine + listener drain). |
| `cmd/wagyu/tui_run.go` | +132 / −0 | `compactor manualCompactor` field on `runMode`; `compactDoneMsg` tea.Msg type; `Update` handler for `compactDoneMsg`; `appendCompactResult` helper. |
| `cmd/wagyu/slash_commands.go` | +18 / −19 | `/compact` branch now calls `m.startManualCompact(trimSpace(args))` instead of the inline cycle-167 trim. |
| `cmd/wagyu/slash_cycle229_test.go` | **new, 464** | 8 tests (5 spec-required + 3 bonus for full coverage). |

No new third-party dependencies. The seam reuses the existing
`github.com/MichaelKinsy/PiG/coding` import already in `tui_run.go`.

## PiG API chain

```
startManualCompact(customInstructions string)
  ├─ nil-branch:                       cycle-167 stub
  │     └─ compactSession(m.session)   slash_helpers.go (unchanged)
  │
  ├─ IsCompacting-branch:              short-circuit
  │     └─ transcript append:          "⎿ Already compacting."
  │
  └─ happy-path:
        ├─ transcript append:          "⎿ Compacting context…"
        ├─ SubscribeManual(listener)   ← coding/session.go:1957
        ├─ go func() {
        │     ctx, cancel = WithTimeout(60s)
        │     defer cancel()
        │     Compact(ctx, customInstructions)   ← coding/session.go:2288
        │     ← CompactionStartEvent  /agent/agent.go:521
        │     ← CompactionEndEvent    /agent/agent.go:531
        │            ├─ TokensBefore
        │            ├─ EstimatedTokensAfter
        │            ├─ Aborted
        │            ├─ Reason         ("manual" | "auto" | ...)
        │            └─ ErrorMessage
        │     drain one-shot channel
        │     prog.Send(compactDoneMsg{...})
        │   }()
        ↓
   Update(msg tea.Msg) → case compactDoneMsg → appendCompactResult
        ├─ TokensBefore > 0 AND TokensAfter > 0
        │     "⎿ Compacted: <before> → <after> tokens (<X.X%> reduction)"
        ├─ TokensBefore == 0 AND TokensAfter == 0
        │     "⎿ Compacted: 0 → 0 tokens"   (no % reduction suffix)
        └─ Err != nil
              "⎿ Compact failed: <err>"
```

PiG surface used:

| Method | Path | Lines |
|---|---|---|
| `Session.Compact(ctx, customInstructions)` | `coding/session.go` | 2288 |
| `Session.IsCompacting()` | `coding/session.go` | 2463 |
| `Session.Subscribe(listener)` | `coding/session.go` | 1957 |
| `agent.CompactionStartEvent{Reason}` | `agent/agent.go` | 521–538 |
| `agent.CompactionEndEvent{Reason, Aborted, TokensBefore, EstimatedTokensAfter, ErrorMessage}` | `agent/agent.go` | 521–538 |

## Live parity (probe-vs-clone, `agy229/`)

REF 1.4.0 (`~/.local/bin/agy`) is gated behind Google OAuth on this
machine and `navigaid@gmail.com` has no quota — REF probes are not
capturable here. Recorded as 存疑 below.

The clone (`/tmp/wagyu229` built from the `w229` worktree at HEAD
`cc0d8ad`) runs both slash forms on a fresh session in the
`agy229` tmux socket (`-L agy229`, 220×50 and 220×30):

| Probe | Geometry | Visible transcript |
|---|---|---|
| `agy229/our/compact-empty.220x50.raw.txt` | 220×50 | banner + warning + `Nothing to compact yet.` |
| `agy229/our/compact-empty.220x30.raw.txt` | 220×30 | banner + warning + `Nothing to compact yet.` |
| `agy229/our/compact-with-instr.220x50.raw.txt` | 220×50 | banner + warning + `Nothing to compact yet.` × 2 (cycle-167 stub, both invocations hit empty session) |
| `agy229/our/compact-with-instr.220x30.raw.txt` | 220×30 | banner + warning + `Nothing to compact yet.` × 2 (same as above) |

All four show the cycle-167 stub branch because `m.compactor == nil` in
the `w229` worktree (cycle 227's TUI plumbing lives in `openagy-w227`,
unmerged). The test suite exercises the compactor-armed path; the live
probes are pinned to this seam so a future merge of cycle 227 will
re-probe and see the new transcript rows.

```
> /compact
⎿  Nothing to compact yet.
```

vs the compactor-armed path the tests assert:

```
> /compact
⎿  Compacting context…
⎿  Compacted: 184000 → 42000 tokens (77.2% reduction)
```

(`77.2%` derives from `1 − 42000/184000 = 0.7717`.)

## Tests

`cmd/wagyu/slash_cycle229_test.go` — 8 tests, all passing:

| Test | Spec role | Asserts |
|---|---|---|
| `TestSlashCompactNoArgs` | required | `handleSlashCommand("/compact")` returns handled, transcript contains `"Compacting context…"`, `fake.CompactCalls == [""]` (1 call, empty customInstructions). |
| `TestSlashCompactWithInstructions` | required | `/compact focus on auth code` forwards `"focus on auth code"` to `Compact`. |
| `TestSlashCompactAlreadyRunning` | required | fake `IsCompacting()=true`; transcript contains `"Already compacting."`, **does NOT** contain `"Compacting context"`, `CompactCalls` is empty. |
| `TestSlashCompactError` | required | fake returns `context.DeadlineExceeded`; transcript contains `"Compact failed"` and `"context deadline exceeded"`. |
| `TestSlashCompactTokensReduction` | required | `appendCompactResult({184000, 42000})` produces `"184000 → 42000 tokens"` and `"77.2% reduction"`. |
| `TestSlashCompactZeroTokens` | bonus | `appendCompactResult({})` produces `"0 → 0 tokens"`, no `% reduction` suffix. |
| `TestSlashCompactFullEventFlow` | bonus | full event loop: slash → goroutine → fake fires end event → transcript has both rows. |
| `TestSlashCompactCycle167StubPath` | bonus | `m.compactor == nil` + empty session → `"Nothing to compact yet."` (pins the stub branch so a future "always wire compactor" change surfaces as a regression). |

Plus a compile-time guard (`var _ manualCompactor = (*fakeCompactor)(nil)`)
that fails the build if `fakeCompactor` ever drifts from the interface
signature.

### Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu229 ./cmd/wagyu` | exit 0 |
| `go test -vet=off -count=1 -run TestSlashCompact ./cmd/wagyu/` | green (8/8 pass, ~1.4 s) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` | fully green (27 packages) |
| `gofmt -l cmd/wagyu/slash_compact.go cmd/wagyu/slash_cycle229_test.go cmd/wagyu/slash_commands.go cmd/wagyu/tui_run.go` | empty (all four files clean) |
| `sha256sum ~/.gemini/{settings.json,google_accounts.json,installation_id}` vs `agy229/*.bak` SHAs | unchanged (byte-identical to start-of-cycle backups) |

## Notable design choices

- **60 s `context.WithTimeout`** matches the existing `streamPrompt`
  deadline in `tui_run.go` (the wire-stream timeout for the TUI prompt
  path). Long compactions abort at the goroutine level; PiG's own
  `compactCancel` path (coding/session.go's manual-compaction cancel
  hook) takes over from there. We do not surface the timeout as a
  distinct transcript row — the user only sees `"Compact failed:
  context deadline exceeded"` if PiG's compact goroutine errors out
  before its own deadline.
- **One-shot listener unsubscribe** — the goroutine subscribes a
  listener that translates the next `CompactionEndEvent` into a
  `compactDoneMsg`, then unsubscribes itself. A long-lived TUI does not
  accumulate listeners every time the user types `/compact`.
- **`m.prog == nil` goroutine branch** — when `m.prog` is nil (in
  tests, or in the print path), the goroutine skips the `prog.Send`
  call and the result is delivered via `appendCompactResult` directly.
  This is the same shape cycle 227's `compactStartMsg` / `compactEndMsg`
  handlers use (see `openagy-w227/internal/pigrt/runtime.go:466` —
  `InjectForTest` follows the same "deliver out of band when no
  program" pattern).
- **Goroutine test discipline** — the slash command's background
  goroutine is bypassed in tests because `m.prog == nil`; tests assert
  `m.response` immediately after `handleSlashCommand` returns, then
  use a 20 ms `time.Sleep` to let the goroutine's `Compact()` call
  land in the fake's recorder. Where the goroutine's outcome matters,
  tests call `m.appendCompactResult(compactDoneMsg{...})` directly to
  mirror what `Update()` would do with a real prog.

## 存疑 / limitations

1. **REF probes not capturable on this machine.** `~/.local/bin/agy`
   1.4.0 is gated behind Google OAuth and `navigaid@gmail.com` has no
   quota. The `agy229/ref/` directory is empty. The clone's cycle-167
   stub transcript (`"Nothing to compact yet."`) is therefore the only
   observable output in the live probes; the cycle-229 styled rows
   (`"⎿  Compacting context…"`, `"⎿  Compacted: <before> → <after>
   tokens (<X.X%> reduction)"`) are visible only via the test suite
   until a future OAuth-capable machine can re-probe. The test output
   is the source of truth for the compactor-armed path.
2. **Sync vs async goroutine model.** The slash command fires the
   `Compact` call on a goroutine and posts the result via `prog.Send`.
   In production this means the user can keep typing while the compact
   runs (the input loop is not blocked). PiG's `Session.Compact` itself
   is synchronous — the goroutine is purely there to keep the bubbletea
   runtime's Update loop responsive. The downside: a fast follow-up
   `/compact` from the user races the in-flight compact; the
   `IsCompacting()` gate covers that race for cycle 227's auto-compact
   path, but a manual `/compact` press *after* the gate-check but
   *before* the goroutine's `Compact()` call lands will fall through to
   PiG, which aborts the in-flight compact and starts a new one
   (coding/session.go:beginManualCompaction loops until the active one
   finishes). Held.
3. **`/compact <instr>` trailing-text forwarding.** Cycle 229 forwards
   the trailing text verbatim — no quoting, no shell parsing, no
   multi-line. The cycle-151 picker substitution intercepts `/compact`
   as the user types, so trailing text needs `Escape` first (see
   the 220×30 capture session: typing `/compact focus on cycle 229
   work` shows the picker overlay on the matched prefix; pressing
   `Escape` dismisses the picker so `Enter` submits the full
   command). The slash command itself does not strip the prefix —
   that is the picker's job.
4. **`Already compacting.` short-circuit is best-effort UX.** PiG's
   `Session.Compact` is happy to abort an in-flight compact and start
   a new one (the loop in `beginManualCompaction`); the gate is a UX
   nicety that prevents the user from queuing two compactions when
   they meant to press it twice. The transcript echoes
   `"Already compacting."` instead of silently queuing.
5. **Mid-stream `/compact` during a tool call.** Cycle 229 does not
   cancel an in-flight tool call before compacting. PiG's `Compact`
   takes care of that (coding/session.go:compactCancel — it cancels
   the active prompt stream before summarising). The slash command
   is therefore safe to invoke mid-stream, but the user will see the
   active tool's transcript row truncated by the compact's
   `CompactionStartEvent` mark. Held — PiG's own behaviour, not a
   cycle-229 decision.
6. **Zero-token fallback is implicit.** When PiG emits a
   `CompactionEndEvent` with both counts zero (an aborted compact,
   or one that never recorded counts), the transcript row reads
   `"⎿  Compacted: 0 → 0 tokens"` with no percentage. This is the
   cycle-229 spec's documented fallback; cycle 195's `slashRespond`
   helper does not have an explicit test for zero-count compactions
   so we added `TestSlashCompactZeroTokens` to pin the behaviour.
7. **`sessionForTest` exposes the underlying `coding.Session` for
   the `pigrtCompactor` adapter.** The helper is unexported and only
   reachable from `cmd/wagyu`'s `package main` tests; nothing outside
   the package can reach into the session directly. Cycle 227's
   `pigrt_compactor_test.go` uses the same surface (see
   `openagy-w227/internal/pigrt/runtime.go:466` `InjectForTest`).
   Future cycles that need a real-session test should use this
   helper, not re-implement the adapter.

## Hard constraints

- Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w229`, branch `cycle229-manual-compact`. Did not touch `main`; did not `git push`.
- Probe dir: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy229`. TMUX socket: `agy229` (separate from default; `kill-server` not invoked).
- Did not modify `internal/pigrt/runtime.go` or `internal/agent/pig.go` — those belong to cycle 227 (`openagy-w227` worktree). Cycle 229 only *reads* the cycle-227 worktree to confirm field naming (`lastCompact` / `compactActive` / `compactMu`) and event surface, never edits.
- Did not modify `internal/convstore/sqlite.go` (per cycle-205 onwards standing rule).
- No destructive `/rewind` probe; nothing under `$HOME/.gemini/**` was modified (SHAs byte-identical before/after).
- The clone's agw client and the native provider stay behaviourally identical (cycle-229 does not touch any transport code).
- New code confined to `cmd/wagyu/slash_compact.go`, `cmd/wagyu/slash_cycle229_test.go`, and the localised edits to `cmd/wagyu/slash_commands.go` / `cmd/wagyu/tui_run.go`. No wholesale reformatting of pre-existing files.
- No third-party dependencies added.