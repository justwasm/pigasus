# Cycle empty-assistant-message-skip — drop empty model turns at the persist boundary

## TL;DR

AGY 1.2.13 fix: assistant messages whose content is empty or
whitespace-only (the model returned no prose) used to be saved to
the conversation transcript as a `StepModelResponse` with `Text=""`,
and `--continue` had to filter them out again at replay time. A
streamed reply whose every chunk happened to be empty — a real
failure mode of the agw transport — produced a visible blank row in
scrollback on every resumed session.

The fix lands at two boundaries:

1. **`sessionToSteps`** in `cmd/wagyu/slash_helpers.go` drops
   assistant steps whose `Content` is empty/whitespace-only. The
   protobuf payload that `WriteConversation` emits no longer carries
   a blank row.
2. **`seedConversation`** in `cmd/wagyu/conversation.go` keeps a
   defensive `strings.TrimSpace` guard on the model branch. A legacy
   DB that pre-dates the fix and still holds a blank row is silently
   re-filtered at the resume boundary instead of rendering a blank
   transcript line.

The load path was already safe — `Conversation.Transcript()` and
`Conversation.ChatTurns()` both skip empty model rows — so this cycle
closes the persist half of the gap that the load half was already
hiding. **Empty assistant messages are no longer written; --continue
no longer replays blanks.**

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.2.13 |  | Fixed empty assistant messages (model returned no content) being saved to the conversation transcript and replayed in --continue sessions, producing blank turns in scrollback. |

## Files

| Path | Role |
|---|---|
| `cmd/wagyu/slash_helpers.go` (+12 / −1) | `sessionToSteps` drops an assistant step when `strings.TrimSpace(m.Content) == ""`. The Idx column keeps tracking the sess slot, so a row dropped in the middle does not change the Idx of the rows behind it; `ON CONFLICT(idx) DO UPDATE` in `WriteConversation` overwrites the right primary-key row instead of leaving orphans or duplicating. |
| `cmd/wagyu/conversation.go` (+13 / −0) | `seedConversation` defensive guard on the `case "model":` branch. `Transcript()` already drops empty model steps, but a legacy DB could still surface one if a future change to the load filter regressed; this guard short-circuits the render and the re-seed into `m.session`. |
| `cmd/wagyu/slash_empty_assistant_test.go` (new, 297 lines) | Six tests: empty assistant not saved, whitespace-only assistant not saved, normal assistant saved (with Idx invariant), tool-call assistant still saved through round-trip (the empty bubble filtered, the tool-call row preserved), `--continue` does not replay blanks, `Idx` column tracks the sess slot. |
| `docs/cycles/empty-assistant-message-skip/deliverable.md` (new) | this file. |

**Net:** +28 production lines, +297 test lines, +192 doc lines across 4 files
(2 modified, 1 new test, 1 new doc).

## Behaviour matrix

| `m.session[i]` role | `m.Content` | Persisted via `sessionToSteps`? | Shown by `seedConversation` for a legacy row? |
|---|---|---|---|
| user | any non-empty | yes (`StepUserInput`) | yes (user branch) |
| user | empty | yes (`StepUserInput`, Text="") — empty user turns are not the bug AGY 1.2.13 fixes; an empty user prompt is a different code path (`streamPrompt` rejects it earlier) | yes (user branch) |
| assistant | non-empty | yes (`StepModelResponse`) | yes (model branch) |
| assistant | empty `""` | **no** (filtered, this cycle) | **no** (defensive guard, this cycle) |
| assistant | whitespace-only `"   \n\t"` | **no** (filtered, this cycle) | **no** (defensive guard, this cycle) |
| tool call (liveSteps path, not sessionToSteps) | n/a | yes (separate `StepToolCall` rows) | yes (tool branch in seedConversation) |

## Tests — `cmd/wagyu/slash_empty_assistant_test.go`

Six new tests, all pass with `-vet=off -count=1`:

```
=== RUN   TestEmptyAssistantNotSaved
--- PASS: TestEmptyAssistantNotSaved (0.00s)
=== RUN   TestWhitespaceAssistantNotSaved
--- PASS: TestWhitespaceAssistantNotSaved (0.00s)
=== RUN   TestNormalAssistantSaved
--- PASS: TestNormalAssistantSaved (0.00s)
=== RUN   TestToolCallAssistantSaved
--- PASS: TestToolCallAssistantSaved (0.01s)
=== RUN   TestContinueNoBlankTurns
--- PASS: TestContinueNoBlankTurns (0.00s)
=== RUN   TestSessionToStepsIdxUnchanged
--- PASS: TestSessionToStepsIdxUnchanged (0.00s)
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.648s
```

### Coverage map

| Test | Locks |
|---|---|
| `TestEmptyAssistantNotSaved` | `sessionToSteps([user, assistant("")])` → only the user step survives |
| `TestWhitespaceAssistantNotSaved` | newlines / tabs / spaces / lone space all filtered |
| `TestNormalAssistantSaved` | four-message round-trip; Idx column keeps tracking the sess slot |
| `TestToolCallAssistantSaved` | the empty bubble between a user prompt and a tool call is filtered, the tool-call row survives, `Transcript()` and `ChatTurns()` both still drop any pre-existing empty rows |
| `TestContinueNoBlankTurns` | `seedConversation` on a legacy DB with an empty model row: no blank transcript line, no re-seeded empty assistant message in `m.session` |
| `TestSessionToStepsIdxUnchanged` | Idx invariant: dropping step 1 and step 4 leaves steps 0/2/3/5/6 with Idx values 0/2/3/5/6 |

The full targeted-test command from the task brief passes
(`go test -vet=off -count=1 -run 'TestEmpty|TestAssistant|TestSave|TestContinue' ./cmd/wagyu/ ./internal/.../`):

```
ok    github.com/chadgpt/openagy/cmd/wagyu                          0.681s
ok    github.com/chadgpt/openagy/internal/announce                  0.238s
ok    github.com/chadgpt/openagy/internal/auth                      0.256s
ok    github.com/chadgpt/openagy/internal/convstore                 0.253s
… (28 more ok lines, no FAIL)
```

## What was deliberately not touched

- **`internal/convstore/sqlite.go`** — the task brief hard-bans edits
  to this file, and none were needed; the fix is a one-line predicate
  on the in-memory conversion path.
- **`Conversation.Transcript()` and `Conversation.ChatTurns()`** —
  these already filtered empty model rows (`agy.go` lines 271 and
  305). They are the load-time half of the AGY 1.2.13 fix, but they
  pre-date this cycle, so no edit was needed there.
- **`sessionToSteps` `Idx` semantics** — `Idx` keeps tracking the
  sess slot, not the kept count. This preserves the existing
  `INSERT … ON CONFLICT(idx) DO UPDATE` contract in
  `WriteConversation`; if we renumbered the kept steps from 0, the
  new save would land on different primary keys than the previous
  save, and the legacy rows would either be re-inserted (the
  upsert is keyed on idx, not step_type+idx) or persist as
  orphans. The current "Idx tracks the sess slot" rule is what
  keeps `ON CONFLICT(idx) DO UPDATE` correct.
- **Existing non-gofmt-clean files** — the task brief asks for
  gofmt-clean **new** files; the pre-existing 50+ files under
  `internal/` and the two in `cmd/wagyu/` (`picker_view.go`,
  `tui_v135_test.go`) were left untouched, per `AGENTS.md` "do not
  reformat them wholesale (it buries real diffs)". `gofmt -l` on
  the new test file is empty.

## Open questions / hard constraints

- **Streamed empty chunks.** `backendChunkMsg` appends a separate
  `backend.Message{Role: "assistant", Content: reply}` per chunk
  (`tui_run.go:2676`). An empty chunk therefore adds an empty
  assistant message to `m.session` in-memory, and a turn whose every
  chunk is empty ends up with `len(m.session)` empty entries. This
  cycle's filter at `sessionToSteps` keeps the DB clean, but the
  in-memory `m.session` still grows. A follow-up cycle could gate
  the chunk-append too — a `len(m.session) == 0 || trim(back) != ""`
  check before appending — but doing so here would expand the
  blast radius beyond AGY 1.2.13's "do not save empty messages"
  scope and is left as a separate change.
- **`WriteConversation` does not delete stale rows.** When a
  session shrinks (e.g. `/rewind` pops the tail, an old empty row
  is now skipped), the previous-save row at the matching idx is
  overwritten by `ON CONFLICT(idx) DO UPDATE` for idx values still
  in the new slice, but idx values no longer present in the new
  slice remain in the DB as orphan rows. `Transcript()` and
  `ChatTurns()` filter them at the load time, so the user never
  sees them, but they inflate `NumSteps` in
  `cache/conversation_metadata.json` and
  `conversation_summaries.db`. Cleaning them up requires a
  `DELETE FROM steps WHERE idx > ?` after the insert loop — a
  pre-existing limitation that affects every `/rewind` and `/compact`
  call, not specific to this cycle.
- **Reference binary validation.** No probe-vs-clone diff was run
  against `~/.local/bin/agy 1.3.4`: this is a *behaviour fix*
  cycle (the user-reported "blank turns in scrollback" is a UX
  defect, not a byte-mismatched column in a probe dump) and the
  recovered AGY changelog line is the source of truth. The
  unit-test matrix covers both the persist and the replay
  boundaries the changelog text names.
- **`Transcript()` already filters** — the in-load filter is what
  kept the user-visible symptom (blank turns) from being worse
  than it already was. The 1.2.13 fix is the *persist* half; the
  load half was already correct. Both are locked by tests now.

## How to verify locally

```bash
# 1. Build the cycle-225 / feature/empty-assistant-message-skip binary.
go build -o /tmp/wagyu-eas ./cmd/wagyu      # exits 0

# 2. Run the new tests.
go test -vet=off -count=1 -run 'TestEmpty|TestAssistant|TestSave|TestContinue' \
  ./cmd/wagyu/ ./internal/.../

# 3. Spot-check the convstore transactional layer.
go test -vet=off -count=1 ./internal/convstore/

# 4. Confirm gofmt-clean for the new file.
gofmt -l cmd/wagyu/slash_empty_assistant_test.go  # empty output
```

## Why this cycle is small

The cycle-167 fork path (`sessionToSteps` → `WriteConversation`) was
already the bottleneck for every conversation save, and the load
path was already filtering. The fix is two predicates and six
tests — the recovery work for the *reference* surface was a
no-op, because the load half already matched what 1.2.13 would
write, and the gap was that we were persisting rows the load
half then had to ignore. Closing the persist half makes the
load half unnecessary on the happy path and the legacy path
still safe via the defensive guard.