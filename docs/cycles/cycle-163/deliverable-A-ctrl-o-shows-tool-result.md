# Cycle 163 — Ctrl+o on a resumed conversation now shows the captured tool result

## Symptom

The user observed that pressing `ctrl+o` on a resumed conversation had
no visible effect — ref's binary clearly expands and collapses a tool
group's detail view, but the clone just toggled the hint text with
nothing actually appearing or disappearing.

The root cause is documented in `seedConversation`:

> Tool *results* are not restored: the reference keeps them in a step
> shape the clone does not decode yet, so an expanded loaded step
> shows its title only.

The convstore's `parseStepPayload` walked `step_payload → field 5 →
field 4 → {field 2: tool name, field 3: tool args}` and stopped. The
tool *output* lives at `step_payload → field 140 → field 2 → field 1`
(a different sub-tree entirely) and was being thrown away.

## Fix

`internal/convstore/agy.go`:

- New field `ToolResult string` on the `Step` struct. Holds the
  tool-call output text recovered from the proto. Pre-cycle-163 the
  clone ignored this entirely, so `stepDetailLines` always returned
  `nil` for a resumed step (its `s.detail` was always empty).
- `parseStepPayload` for `StepToolCall` now extracts
  `protoBytes(top, 140)` (the tool-result wrapper), walks its fields,
  and reads `protoText(f140, 2)` (the result-content wrapper), then
  drills one more level into `protoText(..., 1)` for the actual
  result text. A defensive fallback uses `p2` directly if the inner
  wrapper is absent (older or partial records).

`cmd/wagyu/conversation.go`:

- `loadedToolStep` now copies `s.ToolResult` into `toolStep.detail`.
  Pre-cycle-163 the detail field was always empty.

`cmd/wagyu/conversation_test.go`:

- `convstoreStep` test helper updated to include the new
  `ToolResult` field so the test's anonymous type matches `convstore.Step`.

## After

```
=== ours, before ctrl+o ===
● 2 commands  (ctrl+o to expand)
● Ran (rm rea-go-seeds/XxxX.java ...)
● Ran (git status echo "---" git log --oneline -1)

=== ours, after ctrl+o ===
● 2 commands  (ctrl+o to collapse)
● Ran (rm rea-go-seeds/XxxX.java ...)
  ⎿ The command exited with code 0.
      Output:
      [empty]
      [empty]
      [empty]
      [empty]
      [empty]
      6084230 (HEAD -> main) Cycle 95: respect settings.json model …
● Ran (git status echo "---" git log --oneline -1)
  ⎿ The command exited with code 0.
      Output:
      …
```

The `⎿` (U+23BF) detail bullet, the captured command output, and the
hint toggle all behave as ref does.

The detail line count is capped at `maxStepDetailLines` (=25) by
`stepDetailLines`; an over-cap step gets the recovered
`"<output +%d lines>"` marker — matches ref.

## Files touched

- `internal/convstore/agy.go` — new `ToolResult` field on `Step`;
  `parseStepPayload` walks `field 140 → field 2 → field 1`.
- `cmd/wagyu/conversation.go` — `loadedToolStep` copies the new
  `s.ToolResult` into `toolStep.detail`.
- `cmd/wagyu/conversation_test.go` — `convstoreStep` helper updated.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
tmux new-session -d -s our -x 200 -y 60 \
  "AGY_CLI_DISABLE_AUTO_UPDATE=1 OPENAGY_BACKEND=agw /tmp/agycmp/our/agy --conversation=$CONV"
tmux send-keys -t our $'\x0f'                      # ctrl+o
tmux capture-pane -p -t our -e > /tmp/cmp_after_ctrl_o.txt
```

The captured file shows the tool result expanding inline. Pre-cycle-163
the same capture was byte-identical between collapsed and expanded
states; post-cycle-163 the hint flips from "to expand" to "to
collapse" *and* the captured command output appears under each step.

## Limitations

- The detail line cap (`maxStepDetailLines = 25`) is conservative;
  ref's recovered limit is higher for `Read` / `write_to_file`. The
  output shape matches; only the line count is slightly lower. Cycle
  164+ work.
- The detail's first column (`+` / `-` diff marker) is still coloured
  via the cycle-127 `styleDiffLine`; the colour is monochrome on the
  recovery test conversation. Cycle 164+ work for richer styling.
- Trailing-whitespace padding (the 197-column transcript area) is
  still missing — that's cycle 164's primary target.
- Status=5 tool calls (no `field 140` at all) still render as
  collapsed-only; that's the pre-cycle-163 behaviour preserved for
  in-flight steps.