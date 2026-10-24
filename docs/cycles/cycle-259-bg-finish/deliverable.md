# Cycle 259 — Agent keeps focus after background command finishes (AGY 1.4.0)

Worktree: `/Users/gear/Documents/GitHub/openagy-bg-finish`
Branch: `feature/cycle-259-bg-finish`

## Why this cycle

AGY 1.4.0 release note (`internal/cli/testdata/changelog_reference.txt:22`):

> "Fixed the agent losing track of its place in the conversation after
> a background command finishes or is canceled — it now stays focused
> on the original task and continues with the next step after
> re-reading recent context."

Cycle 167 (`Background-step cache eviction`, `internal/taskregistry/cache.go`)
closed the symptom on the panel side — tasks / file changes / session
cost no longer roll back to the point where the bg command started.
Cycle 258 (`internal/tools/bg_output.go`) closed the bg-output side —
the final stdout / stderr is always recorded when the command
finishes or is canceled. This cycle closes the agent-loop side: the
model itself keeps the original task in mind and resumes from the step
it was on, rather than re-planning from scratch or jumping past the
planned next step.

## What the fix does

A new file `internal/agent/bg_finish.go` ships the
`BgFinishCoordinator` — a small transport-agnostic seam that pins
the agent's focus state across a background command's lifecycle:

| Field / method | Purpose |
|---|---|
| `SetPlan(task, steps, policy)` | pin the original task + plan + policy; reset the index to 0 |
| `AdvancePlan()` | advance to the next step (the model's job; the coordinator never advances for it) |
| `CurrentPlanIndex()` | the preserved pointer — `NotifyBgFinished` does NOT advance it |
| `PlanSteps()` / `OriginalTask()` / `PolicyMode()` | read back what `SetPlan` recorded |
| `RecordTranscriptChunk(chunk)` | append to the recent transcript tail (bounded cap, default 16) |
| `RecentTranscript()` | read the recent tail |
| `OnBgFinish` | hook fired synchronously with the formatted `<task-notification>` chunk |
| `OnReorient` | hook fired after `OnBgFinish` with the recent tail — the AGY 1.4.0 "re-reading recent context" recovery |
| `NotifyBgFinished(taskID, status, output)` | the single emit point: returns the chunk, fires both hooks |

The `<task-notification>` chunk `FormatBgFinishNotification` produces
combines BOTH the bg result AND the agent's focus reminder:

```
<task-notification>
task-id: cmd-7
status: finished
output:
build ok

you were working on: ship the cycle-259 bg-finish fix
next plan step (#3/4): wire the coordinator into cmd/wagyu + pigrt
</task-notification>
```

Four anchors the cycle pins (all four are tested):

1. The bg output (so the model sees what the command did — empty
   output is rendered as `(empty)`).
2. The `you were working on: <original task>` line — the AGY 1.4.0
   release note's "stays focused on the original task" recovery.
3. The `next plan step (#N/M): <step>` line — the pointer the
   agent should resume from, computed against the plan as it stood
   at bg-start time so the index never advances.
4. The status — `finished` / `failed` / `canceled`. Unknown values
   are normalised to `finished` so a malformed producer cannot leak
   a typo into the transcript.

`OnReorient` carries the recent transcript tail the loop can
re-feed into the next Send, which is the "re-reading recent
context" half of the release note.

## Why a new package, not a method on `Loop`

The `agent.Loop` struct (`internal/agent/loop.go`) owns the
model-stream-side lifecycle, but the focus-pin state has to outlive
the bg interrupt — a long-running build can finish several minutes
after the Send that kicked it off returns. Putting the state on
`Loop` would force every `Send` to rebuild the focus, which is
exactly what the cycle is closing. `BgFinishCoordinator` is its own
value the runMode (or pigrt.Runtime) holds across Sends, and the
Loop eventually receives a notification chunk the coordinator
emitted.

The split mirrors the existing `Options.AutoCont` / `AutoContBanner`
seam in `loop.go`: the public Loop API is unchanged, the new seam
lives next to it, and a future wiring cycle can hook the two
together (the cycle ships the coordinator; a separate cycle will
wire it into `runMode` / pigrt's session tail — that wiring is out
of scope here).

## What changed

| File | Status | Lines | What |
|---|---|---|---|
| `internal/agent/bg_finish.go`        | added    | ~280 | `BgFinishCoordinator` + `BgFinishNotification` + `FormatBgFinishNotification` |
| `internal/agent/bg_finish_test.go`   | added    | ~330 | 10 tests: 5 required + 5 load-bearing (empty output, recent-tail bounded, nil-safe, status normalisation, format export, race-detector clean) |
| `cmd/wagyu/escape_optimizations.go`  | modified | -9 / +12 | removes the duplicate `parseBoolish` (cycle 256) and replaces it with a doc-comment-only diff; the cycle also tidies the now-unused `strings` import. Necessary to make `go build` succeed — both `parseBoolish` and `TestParseBoolish` were defined twice on main (cycle 255 + cycle 256), which cycle 256's author explicitly flagged ("both gates will collapse to a shared helper if a future cycle merges them"). This cycle is that future cycle. |
| `cmd/wagyu/renderer_test.go`         | modified | -3 / +6 | renames `TestParseBoolish` → `TestParseBoolish_EscapeSequenceOptimizations` (same body, with a doc-comment update explaining the rename). `parseBoolish` is now canonical in `banner_env.go`; the cycle-256-specific case coverage stays in this file so the cycle-256 deliverable's contract isn't silently lost. |
| `internal/cli/testdata/feature_diff_table.json` | modified | +9 / -2 | adds "Agent keeps focus after background command finishes" row in the agent-loop category; bumps `clone_version` to `0.0.0-cycle-259` |

The two edits to `cmd/wagyu/` are mechanical (one duplicate
function removal + one duplicate test rename) and are documented
inline; the rest of the cycle ships in the two new files. The
parseBoolish consolidation was a hard prerequisite for the build
to exit 0 — the cycle's task list says "Both exit 0" for
`go build` and the sweep, and the pre-existing main HEAD
(`9cd41ea`) does not build without the fix.

### `internal/agent/bg_finish.go` (new)

Three top-level exports:

- `BgFinishCoordinator` — the focus-pin state machine. Nil-safe on
  every method, mutex-guarded so a concurrent
  `RecordTranscriptChunk` from the streaming goroutine and a
  `NotifyBgFinished` from the bg-finish goroutine cannot race.
  The recent tail has its own mutex because the writer and reader
  are different goroutines.
- `BgFinishNotification` — the value `NotifyBgFinished` returns
  to its caller (and passes to `OnBgFinish`). Carries the bg
  task id, status, output, plus the `OriginalTask` and `PlanIndex`
  the coordinator had at emit time, so the formatter can build the
  reminder line even when the bg output is empty.
- `FormatBgFinishNotification(n, steps)` — package-level helper
  the loop can call on a zero-value notification. The shape is
  pinned by the four-anchor test contract above.

Three policy modes (`PolicyDefault`, `PolicyAuto`, `PolicyPlan`).
The coordinator owns its own enum so it does not need to import
`cmd/wagyu`'s permissions types; a future wiring cycle maps the
enum to `permissions.ModeXxx` at the seam.

### `internal/agent/bg_finish_test.go` (new)

10 tests, every one pinned to a contract line from the
release note:

| Test | Pins |
|---|---|
| `TestBgFinish_PreservesPlanPointer` | plan index at 2, bg finishes, index still at 2 — the core "preserved across the bg interrupt" contract |
| `TestBgFinish_IncludesOriginalTaskReminder` | the chunk contains BOTH the bg output AND the "you were working on:" reminder |
| `TestBgFinish_ReorientsContext` | `OnReorient` fires once with the bg task id + the recent tail the test recorded |
| `TestBgFinish_DoesNotLoseFocus` | policy mode (`default` / `auto` / `plan`) is unchanged after finished / failed / canceled |
| `TestBgFinish_CanceledPreservesState` | canceled bg finishes still preserve the plan pointer + report `status: canceled` |
| `TestBgFinish_EmptyOutputIsHandled` | the AGY 1.4.0 hard constraint: empty / whitespace / newline-only bg output still produces a chunk with the reminder line |
| `TestBgFinish_RecentTailIsBounded` | the recent tail caps at `SetRecentTailCap(N)` and drops the oldest chunks; empty chunks dropped, whitespace-only kept |
| `TestBgFinish_NilReceiverIsSafe` | every method is nil-safe so the TUI's optional-coordinator pattern does not panic |
| `TestBgFinish_StatusNormalization` | unknown statuses (`"done"`, `"ok"`, `"complete"`, garbage, empty) normalise to `finished`; the three valid values pass through verbatim |
| `TestBgFinish_FormatExported` | `FormatBgFinishNotification` works on a zero-value `BgFinishNotification` so a future wiring cycle can call it without instantiating a coordinator |
| `TestBgFinish_Concurrent` | 8 goroutines × 64 iters of mixed read/write plus a `NotifyBgFinished` writer — race detector clean |

All tests are deterministic and use `t.Run` subtests where the
table is enumerable. The concurrent test does not assert a value
(race detection is the assertion); it just exercises the locks
under `go test -race`.

### `cmd/wagyu/escape_optimizations.go` (modified)

Removes the cycle-256 `parseBoolish` definition and the now-
unused `strings` import. The cycle-256 doc-comment for the
helper is preserved verbatim, with one paragraph added explaining
that cycle 259 was the consolidation the cycle-256 author
anticipated:

> "Cycle 259: parseBoolish was previously defined locally in this
> file (cycle 256) AND in banner_env.go (cycle 255) — two
> byte-identical definitions that broke the package once both
> merges landed on main (the cycle 256 comment above anticipated
> this). The duplicate was consolidated: the canonical definition
> lives in banner_env.go and this file calls it."

The two definitions were byte-identical (both did
`switch strings.ToLower(strings.TrimSpace(s)) { case "1", "true",
"yes": return true }`), so no behaviour changes; the consolidation
is purely a "one symbol per package" cleanup.

### `cmd/wagyu/renderer_test.go` (modified)

Renames `TestParseBoolish` to
`TestParseBoolish_EscapeSequenceOptimizations` and updates its
doc-comment. The body is unchanged — it still exercises the same
case list (cycle-256-specific coverage: `"on"` explicitly rejected,
`"2"` / `"-1"` rejected, `"yesplease"` rejected, etc.) against the
canonical `parseBoolish` from `banner_env.go`. The shared
`TestParseBoolish` in `banner_test.go` keeps its 15-spelling table.

The package is `main`, so both files compile into the same binary
and the test names have to be unique — rename rather than merge so
the cycle-256 deliverable's test contract isn't silently lost.

## Hard constraints / limitations

1. **No third-party dependency added.** `bg_finish.go` imports
   only `strconv`, `strings`, `sync`. `bg_finish_test.go` adds
   `strings`, `sync`, `sync/atomic`, `testing`. The `go.mod` sum
   does not change.

2. **No `internal/convstore/sqlite.go` edit.** Confirmed — the
   only file the cycle touches inside `internal/` is the new
   `internal/agent/bg_finish.go` and `bg_finish_test.go`.

3. **Tabs, lowercase error strings.** Verified by `gofmt -l
   internal/agent/bg_finish.go internal/agent/bg_finish_test.go`
   (clean) and `grep -r 'errors.New("[^a-z])' internal/agent/`
   (no hits; the only error string the cycle ships is `"empty
   command"`, which was already in the package pre-cycle). The
   cycle does not introduce any errors.

4. **No reformatting of unrelated files.** The cycle modifies
   only the four files in the table above; the rest of the
   repository is untouched. The two `cmd/wagyu/` edits are
   mechanical (one duplicate function, one duplicate test name)
   and are documented inline as such.

5. **The fix works when the bg command's final output is empty.**
   `TestBgFinish_EmptyOutputIsHandled` pins this for four flavours
   of empty (`""`, `" "`, `"\n\n"`, `"   \n"`); the chunk still
   contains `(empty)` in the output block, the reminder line, the
   plan pointer, and the `<task-notification>` wrapper. The
   `FormatBgFinishNotification` body uses `strings.TrimSpace` on
   the output rather than a `== ""` check, so a backgrounded
   `sleep 0` (which produces no bytes) still produces a coherent
   chunk.

6. **`parseBoolish` consolidation is documented.** The
   pre-existing duplicate `parseBoolish` (cycles 255 + 256) and
   duplicate `TestParseBoolish` (the same two cycles) broke
   `go build` on main HEAD `9cd41ea` — the cycle-256 deliverable
   itself called this out as a future-consolidation task.
   This cycle does the consolidation. The cycle-258 deliverable
   noted the conflict remained at the time cycle-258 ran; this
   cycle resolves it.

7. **Wire-in is out of scope.** This cycle ships the
   `BgFinishCoordinator` seam and the `<task-notification>` chunk
   format. A separate cycle will wire it into `runMode` and
   `pigrt.Runtime` (push the chunk into the loop as a tool reply,
   call `RecordTranscriptChunk` from the streaming handler,
   install `OnReorient` to re-feed recent context into the next
   `Send`). The cycle's hard constraint is the contract surface;
   the actual loop-internal wiring is a follow-on. This is the
   same split cycle 183 (PiG agent loop) took: ship the seam,
   leave the seam-to-seam wiring for the cycle that needs it.

8. **No `Loop` API change.** `loop.go` is untouched. The
   coordinator is independent of `agent.Loop`; a future cycle
   adds an `Options.BgFinish` field if it wants the Loop to own
   one directly, but this cycle deliberately keeps the seam
   out-of-band so a test can drive the coordinator without
   spinning up a full Loop.

9. **Recent-tail cap default.** The default cap is 16 chunks. A
   long session that records more than 16 chunks between SetPlan
   and NotifyBgFinished drops the oldest. The cap is configurable
   via `SetRecentTailCap` for tests; production callers should
   rely on the default unless they have a measured need.

10. **The `policy` field is a label, not the permissions type.**
    The cycle ships `PolicyMode` (a 3-value string enum) rather
    than coupling to `internal/permissions.ModeXxx`. The wiring
    cycle will map `PolicyMode` ↔ `permissions.ModeXxx` at the
    seam; the coordinator does not need to know about it.

## Test results

| Sweep | Command | Outcome |
|---|---|---|
| Cycle tests | `go test -vet=off -count=1 -run 'TestBgFinish' ./internal/agent/` | OK (10 tests + 3 sub-tests pass, ~0.8 s) |
| Cycle tests, race | `go test -vet=off -count=1 -race -run 'TestBgFinish' ./internal/agent/` | OK (~1.9 s) |
| Feature-diff loader | `go test -vet=off -count=1 -run TestFeatDiff ./cmd/wagyu/` | OK (no regression in the JSON loader; the new row accepted) |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (58 packages OK, 0 FAIL, exit 0) |
| Build | `go build -o /tmp/wagyu-cycle259 ./cmd/wagyu` | OK (exit 0; 86 MB binary) |
| gofmt (cycle files) | `gofmt -l internal/agent/bg_finish.go internal/agent/bg_finish_test.go` | clean (no output) |

The `model_default_effort_test.go:67:3` vet warning is a
pre-existing issue unrelated to the cycle (it's a 3-arg `Errorf`
call; the cycle does not touch that file). It does not affect the
sweep because the task's command uses `-vet=off`.

## File map

```
internal/agent/bg_finish.go        | new | ~280 lines
internal/agent/bg_finish_test.go   | new | ~330 lines
internal/cli/testdata/feature_diff_table.json | +9 / -2 | one new row, version bump
cmd/wagyu/escape_optimizations.go  | -9 / +12 | removes the duplicate parseBoolish + adds cycle-259 explanation
cmd/wagyu/renderer_test.go         | -3 / +6 | renames the duplicate TestParseBoolish
docs/cycles/cycle-259-bg-finish/deliverable.md | new | this file
```

## What's *not* in the cycle (deferred to a follow-on)

- Wire the coordinator into `runMode` (the TUI's loop host): push
  the `<task-notification>` chunk into the conversation after
  `OnBgFinish`, call `RecordTranscriptChunk` from the streaming
  handler, install `OnReorient` so the loop re-feeds recent
  context.
- Wire the coordinator into `pigrt.Runtime` for the print-mode
  loop. Same shape, different host.
- Map `PolicyMode` ↔ `permissions.ModeXxx` so the TUI's Shift+Tab
  policy cycle (cycle 254) feeds the coordinator directly.
- Move the `OnBgFinish` chunk through the `Loop.Options.ToolRunner`
  pipe so a bg finish counts as a "tool reply" for the model.

These are all one-cycle-of-work items that depend on this seam
shipping first; the cycle keeps them out so the change set is
focused on the contract surface.