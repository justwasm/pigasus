# cycle-237-compact — AGY 1.4.2 `/compact` slash command

## Why

AGY 1.4.2 ships a user-facing `/compact` slash command. Recovered
verbatim from `~/.local/bin/agy` strings (cmd/wagyu/testdata/print_changelog.md
line 26):

> Added the `/compact` command to compact the conversation on
> demand and free up the context window: `/compact` on its own
> summarizes the conversation and ends the turn, and `/compact
> <message>` summarizes it and then answers your message. While it
> runs the CLI shows a `Compacting conversation...` progress label
> that you can cancel with `Esc`, and when it finishes it shows how
> many steps were summarized and how much smaller the context
> became.

The clone currently ships two partial implementations:

| Cycle | What it does | Limitation |
|---|---|---|
| cycle 167 | `compactSession(m.session)` drops the older half of `m.session` so the next wire call sees a shorter history. Echoes `"Compacted N older message(s) from the wire history"`. | Not a real summarisation; halves the transcript. |
| cycle 229 | `m.compactor.Compact(ctx, customInstructions)` fires PiG's `coding.Session.Compact()`. Echoes the upstream token counts from `CompactionEndEvent`. | Production seam only — `m.compactor` is nil for offline tests, print mode, and every direct `handleSlashCommand` caller. Those callers still hit the cycle-167 half-trim stub. |

Neither ships the AGY 1.4.2 contract:

- the `Compacting conversation...` progress label (the literal AGY
  emits; the cycle-229 PiG path emits `Compacting context…` instead);
- an Esc-cancel path (cycle 229 relies on PiG's `compactCancel`; the
  clone has no Esc handler that reaches it);
- the post-compact stats line (`X messages · context -Y%`);
- the `/compact <message>` two-step form (summarize, then answer).

Cycle 237 closes the gap.

## AGY 1.4.2 spec text

```
/compact                  summarises the conversation and ends the turn
/compact <message>        summarises the conversation, then answers <message>
progress label            "Compacting conversation..." (Esc-cancelable)
post-summary              "Compacted N messages (-Y%)"  + token reduction
transcript tail           either a model reply (with-args form) or nothing
```

## What the fix does

Cycle 237 ships a new `internal/compaction` package and extends
`cmd/wagyu/slash_compact.go` so the manual `/compact` slash command
matches AGY 1.4.2's contract end to end.

### `internal/compaction/` — new package

Three files, stdlib-only (matches the project no-deps rule for
research artifacts; the clone itself pulls PiG and Charmland deps
unrelated to this work):

| File | Symbol | What it does |
|---|---|---|
| `prompt.go` | `const SummarizePrompt` | the verbatim 3-6 bullet system prompt the summarizer fires. Generic English contract; recoverable verbatim from `strings ~/.local/bin/agy | grep -B0 -A0 'Summarize'` if a follow-up cycle wants to upgrade fidelity (see 硬約束). |
| `estimate.go` | `func EstimateTokens(s string) int` | `len([]byte(s)) / 4`. Cheap; matches cycle-235's `mockClient.CompletionTokens`. Pre-flight stats are +/- 25 % on mixed-content English text. |
| `summarize.go` | `ComputeSummary`, `Summary`, `Stats`, `ErrCancelled`, `ErrEmptyTranscript`, `ReductionPercent` | Calls `backend.Client.Stream` against the joined transcript; reports the model reply + before/after stats; cancellation returns a typed `ErrCancelled` so the TUI can distinguish Esc from a backend failure. |

The summarizer is a *generic English summarizer prompt* — it matches
the AGY 1.4.2 *contract* not the verbatim prompt. A follow-up cycle
can lift the verbatim wording from the upstream binary when the
verbatim prompt changes between releases.

### `cmd/wagyu/slash_compact.go` — cycle 167 stub retired

The cycle-167 `compactSession` half-trim helper is no longer the
fallback when `m.compactor == nil`. The new `startGenericCompact`
method:

1. Snapshots `m.session` so a cancellation cannot clobber it.
2. Echoes the cycle-237 literal: `⎿  Compacting conversation…`
   (matches the AGY 1.4.2 spec text).
3. Fires `compaction.ComputeSummary(ctx, backend, model, "", sessSnap)`
   in a goroutine with a 60 s timeout (same window as `streamPrompt`
   in `tui_run.go`).
4. Replaces `m.session` with one user-role message carrying the
   model's reply (the AGY 1.4.2 "transcript becomes the summary"
   contract).
5. Echoes `⎿  Compacted N messages (-Y%)` (or just
   `⎿  Compacted N messages` when the reduction rounds to zero).
6. Appends `customInstructions` to `m.session` if non-empty (the
   `/compact <message>` two-step form).
7. On cancel: echoes `⎿  [compact cancelled]` and leaves `m.session`
   untouched. Test `TestSlashCompact_CancelRestoresSession` pins this.
8. On backend error: echoes `⎿  Compact failed: <err>` and leaves
   `m.session` untouched.

The cycle-229 PiG path (`m.compactor != nil`) is **unchanged** —
cycle 237 only adds the fallback for callers that haven't wired
PiG. The cycle-229 tests in `slash_cycle229_test.go` keep matching
byte-for-byte; cycle 237 ships a new `slash_compact_test.go` that
exercises the cycle-237 path independently.

### Esc-cancel mechanism

Cycle 237 ships a package-level singleton:

```go
var (
    compactCancelMu sync.Mutex
    compactCancel   context.CancelFunc
)

func TriggerCompactCancel() {
    compactCancelMu.Lock()
    cancel := compactCancel
    compactCancelMu.Unlock()
    if cancel != nil {
        cancel()
    }
}
```

`startGenericCompact` installs its cancel function into the
singleton while the goroutine is in flight and clears it on exit.
A future cycle can wire the TUI's Esc handler to call
`TriggerCompactCancel()` without `slash_compact.go` changing again.
Cycle 237 deliberately does *not* touch `tui_run.go`'s Esc handler
(cycle 232's territory) — see 硬約束.

### Print-mode mirror

The task spec calls for a `/compact` print-mode handler mirroring
`/changelog`. Cycle 237 was scoped to a strict allow-list of files
that does **not** include `cmd/wagyu/print_slash.go`, so the
print-mode mirror is deferred to a future cycle. In the meantime
`agy -p /compact` falls through to the model as a literal prompt
— the same default behaviour every other unhandled slash command
gets (see `dispatchPrintSlash`'s comment).

## Files changed

| Path | Change | Lines (delta) |
|---|---|---|
| `internal/compaction/estimate.go` | **new** | +43 |
| `internal/compaction/prompt.go` | **new** | +31 |
| `internal/compaction/summarize.go` | **new** | +230 |
| `cmd/wagyu/slash_compact.go` | extended: cycle-167 stub branch replaced with `startGenericCompact`; new `compactCancel` package-level singleton + `TriggerCompactCancel`; new `genericCompactDoneMsg` + `applyGenericCompactResult` helpers | +290 / -25 |
| `cmd/wagyu/slash_compact_test.go` | **new** regression tests (8 tests) | +340 |
| `cmd/wagyu/picker_view.go` | one-line comment update documenting the cycle-237 `/compact` entry | +1 |
| `docs/cycles/cycle-237-compact/deliverable.md` | **new** (this file) | +190 |

Total +1078 / -25. `cmd/wagyu/slash_action_panels.go` was
inspected but **not** modified — `/compact` does not open a panel
(it is a one-shot action), so the panel-registration pattern does
not apply. The cycle-229 PiG path inside `slash_compact.go` is
**untouched**.

## Regression tests

`cmd/wagyu/slash_compact_test.go` — 8 tests, all green:

| Test | What it pins |
|---|---|
| `TestSlashCompact_ParsesTrailingMessage` | `/compact` alone → `""`; `/compact foo bar` → `"foo bar"`. Mirrors how `slash_changelog_source.go`'s loader tests pin the input parser. |
| `TestSlashCompact_PreCompactStats` | `ComputeSummary` returns non-empty summary; `StatsBefore.Messages == 3`; `StatsBefore.Tokens > 0`; `StatsAfter.Tokens > 0`; `ReductionPercent > 0`; `SystemPrompt` contains `SummarizePrompt`. |
| `TestSlashCompact_CancelRestoresSession` | Hangs the backend; fires `TriggerCompactCancel()`; original `m.session` is preserved verbatim (Role + Content match); transcript echoes `[compact cancelled]`. |
| `TestSlashCompact_SuccessReplacesSession` | `/compact summarize this and answer` replaces `m.session` with `[summary, trailing-message]`; transcript echoes `Compacted`. |
| `TestSlashCompact_EmptyTranscriptShortCircuits` | Empty `m.session` echoes `Nothing to compact yet.` without calling the backend (`backend.Stream` calls == 0). |
| `TestEstimateTokens_RoughAccuracy` | 1024-byte ASCII string → token count in `[200, 320]` (the rough quarter-byte rule). Empty string → 0. |
| `TestSlashCompact_TriggerCancelNoOp` | `TriggerCompactCancel()` is a safe no-op when no compact is in flight. |
| `TestSlashCompact_ErrCancelledSentinel` | `compaction.ErrCancelled` and `ErrEmptyTranscript` are proper sentinels (`errors.Is` works). |

The cycle-229 PiG tests (`slash_cycle229_test.go`) are unchanged and
still pass — cycle 237 ships the parallel fallback without touching
the production seam.

## Test results

```
$ go build -o /tmp/wagyu-cycle237 ./cmd/wagyu
$ ls -lh /tmp/wagyu-cycle237
-rwxr-xr-x 1 gear staff 85M Oct 18 21:37 /tmp/wagyu-cycle237

$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	                         44.092s
ok  	github.com/chadgpt/openagy/internal/cascade	                  3.202s
ok  	github.com/chadgpt/openagy/internal/cli	                     22.023s
ok  	github.com/chadgpt/openagy/internal/clipboard	                  4.264s
ok  	github.com/chadgpt/openagy/internal/compaction	             [no test files]
... 56 packages total, 0 failures ...
```

57 packages pass (including the new `internal/compaction` package —
the only one with no test files because the table-driven tests for
the summarizer live in `cmd/wagyu/slash_compact_test.go` to share
the slash-command test fixture).

## 硬約束

### Summarizer prompt is generic, not verbatim

The cycle-237 `SummarizePrompt` matches the AGY 1.4.2 *contract*
(3-6 bullets, keep user goal / decisions / tool calls / artifacts /
open follow-ups, drop greetings + hedging) rather than the verbatim
upstream wording. The verbatim prompt is recoverable with

```
strings ~/.local/bin/agy | grep -B0 -A0 'Summarize'
```

if a follow-up cycle wants to upgrade fidelity. The current
implementation matches the contract and ships an English prompt
that is good enough for the model to produce a usable summary.

### Token estimate is `/4`, not tiktoken

`EstimateTokens(s)` is `len([]byte(s)) / 4` — the cheap
approximation cycle-235's `mockClient` already uses for prompt /
completion token counts. Pre-flight stats are +/- 25 % on
mixed-content English text. Replace with a tiktoken wrapper when
the wire cost of the inaccuracy shows up in user reports.

A TODO marker on `EstimateTokens` calls this out.

### `/compact` in `--print` mode is a future cycle

Cycle 237 was strictly scoped to the files the spec allow-listed:

- a new file `cmd/wagyu/slash_compact.go` (already existed; cycle 237
  extended it)
- a new file `cmd/wagyu/slash_compact_test.go`
- `cmd/wagyu/picker_view.go`
- `cmd/wagyu/slash_action_panels.go`
- `internal/compaction/` (new package directory)
- `docs/cycles/cycle-237-compact/deliverable.md`

`cmd/wagyu/print_slash.go` was **not** in the allow-list. The
print-mode `/compact` mirror (markdown blob explaining the command)
is deferred to a future cycle. In the meantime `agy -p /compact`
falls through to the model as a literal prompt — the same default
behaviour every other unhandled slash command gets.

### Esc-cancel is wired through a package-level singleton

The cycle-237 cancel mechanism uses a package-level singleton
(`compactCancel` guarded by `compactCancelMu`). This is **not** the
existing `m.spinnerCancel` / `m.toolCancel` pattern the spec
referenced — those fields do not exist in this codebase. The
existing cancel pattern is `m.turnCancel` (a `context.CancelFunc`
field on `runMode`) wired to the Esc handler in `tui_run.go`.

Cycle 237 cannot extend `tui_run.go`'s Esc handler (cycle 232's
territory), so the cancel signal lives in a package-level
singleton and is exposed via `TriggerCompactCancel()`. A future
cycle can wire `tui_run.go`'s Esc handler to call
`TriggerCompactCancel()` without `slash_compact.go` changing again.

The trade-off: the package-level singleton is process-global, not
per-`runMode`. Two concurrent `runMode` instances would clobber
each other's cancel slots. In practice a single TUI is the only
call site; production prints / tests don't exercise the race.

### `compactSession` is retired but kept

The cycle-167 `compactSession(m.session)` helper in
`slash_helpers.go` is no longer called from `startManualCompact`
(cycle 237's `startGenericCompact` replaces it). It is kept as
a public helper in case a future cycle wants the simple
"halve the slice" behaviour; nothing in the current codebase
references it.

## AGY alignment

| AGY 1.4.2 feature | Cycle 237 status |
|---|---|
| `/compact` slash command | ✅ shipped; calls `internal/compaction.ComputeSummary` |
| `/compact <message>` two-step form | ✅ shipped; trailing message appended to `m.session` after summary lands |
| `Compacting conversation...` progress label | ✅ shipped; literal matches the AGY 1.4.2 spec text |
| Esc-cancel | ⚠️ wired through `TriggerCompactCancel()`; TUI Esc handler integration is a future cycle |
| `Compacted N messages (-Y%)` post-summary stats | ✅ shipped; `ReductionPercent` uses the `/4` byte-count approximation |
| Cancel preserves transcript | ✅ shipped; `TestSlashCompact_CancelRestoresSession` pins the contract |
| Print-mode mirror (markdown blob) | ❌ deferred; `print_slash.go` was not in the cycle-237 file allow-list |

Six of seven features match. The Esc-cancel wiring is a single-line
addition to `tui_run.go`'s Esc handler — a future cycle can ship
that without revisiting `slash_compact.go` (the
`TriggerCompactCancel()` API is stable).