# Cycle 148 — loadedToolStep maps status=4/6/7 → err:true

## Why

The convstore records `Step.Status` for every stored tool call.
A live `agy` session distribution (queried via
`sqlite3 ~/.gemini/antigravity-cli/conversations/4638a7b0-…db`):

```
status=3 → 3129 rows  (success)
status=4 →  390 rows  (failed at the tool runner level)
status=5 → 3907 rows  (completed-skipped / alternate success)
status=6 →    4 rows  (rare — probably permission-denied)
status=7 →   21 rows  (rare — probably cancelled)
```

The cycle-120 `loadedToolStep` ignored `Step.Status` and never
set `err: true`. The v1.3.6 reference, on resume, marks every
previously-failed tool call with the `(Failed)` suffix — the
same suffix the live tool runner emits. Cycle 148 restores that
so the loaded conversation matches the live conversation
visually for the failed-tool subset.

Captured at `/tmp/conv2.txt` after scrolling 15 PageUp presses
in the loaded 4638a7b0 conversation:

```
⏺ Searched v135StatusRow\|StatusRow\|esc to cancel (Failed)
⏺ Searched esc to cancel (Failed)
⏺ Searched esc to cancel (Failed)
⏺ Ran find /Users/gear/GitHub/openagy -name "*.go" | xargs grep -l… (Failed)
⏺ Read /Users/gear/.gemini/antigravity-cli/brain/4638a7b0-… (Failed)
```

8 visible (Failed) entries on the scrolled-up view. Before
cycle 148 these rows had no suffix.

## What changes

### `cmd/wagyu/conversation.go::loadedToolStep`

- Adds `err: loadedStepErr(s.Status)` so a loaded tool step
  inherits the convstore status's failed-state when present.

### `cmd/wagyu/conversation.go::loadedStepErr`

- New helper. Maps status → `err`:
  - `4` → true (errored at runtime)
  - `6` → true (rare; permission-denied / similar)
  - `7` → true (rare; cancelled)
  - everything else → false (success / completed-skipped /
    unknown status)

## What does NOT change in cycle 148

- The `toolStep` struct, `stepTitle`, and the
  `stepKindTool → "(Failed)"` rendering are unchanged.
- The live tool call path (`streamPrompt` → `tuiToolRunner.onEnd`)
  is unchanged — `err: err != nil` is still how live calls are
  marked.
- The convstore read path is unchanged; only the consumer
  (`loadedToolStep`) is updated.

## Hard constraints / 存疑 / limitations

- 【推测】Why 6 and 7 also map to err:true. The cycle-148
  status codes come from a one-machine probe; the v1.3.6
  reference's exact mapping is unrecovered. Most plausibly:
  status=6 = the tool runner rejected the call before
  execution (e.g. permission denied at the gate level) and
  status=7 = the user cancelled mid-tool. Both should display
  the same "(Failed)" affordance. Cycle 148 includes both
  as a defensive mapping; if a future capture shows status=6
  not rendering as (Failed), drop it from the switch.
- 【推测】Why status=5 doesn't map to err. Status=5 is the
  largest single bucket (3907 rows in the live conversation)
  and is "completed-skipped or alternate success" — the tool
  ran but the result was inconclusive or the agent decided
  not to use it. Showing "(Failed)" on those would clutter
  the transcript; the ref doesn't either. Cycle 148 leaves
  status=5 alone.
- 【推测】Why status=3 maps to err:false. Status=3 is "the
  tool ran and returned a result". That's the canonical
  success path. The "(Failed)" suffix is for runtime errors,
  not for empty-result / no-tool-needed cases.
