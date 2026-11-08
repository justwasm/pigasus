# Auto-compaction evidence — agy 1.5.3

> The live binary on this machine is **1.5.3** (`go1.28-20261102-RC00`), not
> 1.4.8. This note records what changed and how the new functions were read.

## Extracting the embedded profile document

The binary embeds the same `model_profiles` JSON document as 1.4.8. It was
extracted by locating the `"model_profiles"` key and walking the enclosing
balanced JSON object; the result is committed at
`internal/features/models/ctx/testdata/model_profiles_1.5.3.json` (28 174
bytes, keys `base`, `subclients`, `os`, `model_profiles`, `models`).

The only payload field that differs from the 1.4.8 document is
`use_model_reported_token_count`: **every** 1.5.3 payload sets it to `true`
(1.4.8 had `false`). `moving_window_size` is **absent** from both the 1.4.8
and 1.5.3 payloads; the 1.4.4 server capture had `1`.

## Recovering symbols from the Go 1.28 binary

`GoReSym` and `redress` cannot parse this Go 1.28 pclntab. The `__TEXT`
section starts at `0x1000011c0`, while the pclntab's text base is the segment
base `0x100000000`; a `debug/gosym` table built with the segment base reports
every function `0x11c0` lower than its real address. Add `0x11c0` to the
parsed entry/end before disassembling.

This recovered, among others:

| function | address |
|---|---|
| `checkpoint.shouldCheckpoint` | `0x101e926a0` |
| `checkpoint.MaybeCreateCheckpoint` | `0x101e92280` |
| `checkpoint.(*CheckpointHandler).generateCheckpoint` | `0x101e8fef0` |
| `chatconverters.(*TrajectoryChatConverter).TrajectoryToChatMessages` | `0x10162ff00` |
| `utils.GetDefaultCheckpointConfigForModel` | `0x1013dba30` |
| `cortex_go_proto.(*CheckpointConfig).GetTokenThreshold` | `0x101e93a30` |
| `...GetMovingWindowSize` | `0x101e93890` |
| `...GetMaxOverheadRatio` | `0x101e935f0` |

## `shouldCheckpoint` (1.5.3)

The actual function at `0x101e926a0` gates in this order:

1. `config != nil` and `config.Enabled` (`[config+0xa8]` is a `*bool`).
2. A last **checkpoint** step exists (`[trajectory+0x80]` method call).
3. A step/status gate (`[trajectory+0x38]` bool and a method call).
4. `TrajectoryToChatMessages` is called. If its error matches the
   **"agent max token limit error"** sentinel, return **true** (hard-limit
   checkpoint).
5. Otherwise, if the conversion result is nil or `result[+0x10] == 0`, return
   false.
6. Return `!isAnchorCheckpointAt(...)`.

So 1.5.3's trigger is: **hard-limit conversion failure**, or a conversion
result that asks for a checkpoint and is not already anchored at that step.
The 1.4.5 decompilation's sentinel comparison was an earlier shape; the
anchor check is the 1.5.3 addition.

The `token_threshold` and `max_overhead_ratio` values are read inside the
conversion path, not in `shouldCheckpoint` itself. The conversion result's
field at `+0x10` is the soft-trigger signal; the exact arithmetic is still
inside `TrajectoryToChatMessagesToTokenLimitOrStepIndex`.

## Default config (`GetDefaultCheckpointConfigForModel`)

`0x1013dba30` computes:

```text
limit = max(0, model.input_limit - max(40000, model.max_output_tokens))
max_output_tokens = clamp(model.max_output_tokens, 16384)
```

and builds a `CheckpointConfig` with `enabled=true`, `max_user_requests=10`,
`max_subagent_snapshots=20`, retry `1000ms`/`2x`, and the model's checkpoint
model. `moving_window_size` at `[config+0xd8]` is **not set** by this
constructor; an experiment payload that omits it leaves the field at zero.

Field offsets read from the generated getters:

| field | offset |
|---|---|
| `max_overhead_ratio` (float32) | `+0x8` |
| `token_threshold` (uint32) | `+0xc0` |
| `moving_window_size` (uint32) | `+0xd8` |

## What this changes in the clone

- `ProfileFor` overlays `use_model_reported_token_count = true`, matching the
  1.5.3 payloads while the structural table stays the 1.4.8 evidence.
- The hard-limit trigger is already implemented as the conversion/estimate
  failure path; the anchor check is represented by the checkpoint boundary
  (`IncludedThroughStep`) and the `moving window`.
- **Open:** what `moving_window_size = 0` means in 1.5.3. The clone keeps the
  1.4.4 value (1) until a live 1.5.3 checkpoint or a deeper decompilation of
  the conversion path settles it.

## Reproduction

```sh
# symbols
go run /tmp/dumpfuncs.go ~/.local/bin/agy > /tmp/agy_funcs.json   # +0x11c0
# disassemble
llvm-objdump -d --start-address=0x101e926a0 --stop-address=0x101e92820 ~/.local/bin/agy
# profile document
python3 - <<'PY'
data = open('/Users/gear/.local/bin/agy','rb').read()
# find b'"model_profiles"', walk the enclosing balanced object
PY
```

## Resume sections (`serializers`)

The resume block is assembled in `oneharness/internal/core/serializers`. The
Go 1.28 pclntab yields these addresses (after the `+0x11c0` correction):

| function | address | role |
|---|---|---|
| `DefaultSerializer.checkpointToMessage` | `0x101e2c680` | assembles every section |
| `DefaultSerializer.appendArtifactSnapshots` | `0x101e2c010` | artifact snapshot section |
| `DefaultSerializer.formatCheckpointWrapper` | `0x101e2c420` | `<CONTEXT_SUMMARY>` wrapper |
| `serializers.subagentSnapshotForPrompt` | `0x101e2dd60` | per-subagent snapshot |
| `serializers.stateSnapshotToContext` | `0x101e30b00` | state snapshots |

The section strings are literal constants; reading the exact bytes (including
newlines inside the format string) recovers:

```text
# Conversation Logs
Start with the compact transcript; if a step has `truncated_fields`, read only that specific line in `transcript_full.jsonl`:
- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`
- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript_full.jsonl`
```

```text
You have the %d following artifacts written to the artifacts directory:
```

```text
[ARTIFACT: %s]
Path: %s
Last Edited: %s
```

```text
# Running Background Tasks
The following background tasks were running at the time this checkpoint was created.
Use the manage_task tool to interact with them (e.g. to kill them or check their status).

%s
```

The `%s` is built by `protojson.MarshalOptions.Format` over the checkpoint's
`running_task_snapshots` and `strings.Join(..., "\n")`; the section separator
is `"\n\n"`. The artifact header constant is followed by a blank line, then
the entries (also one blank line between entries).

Subagent headers (constants recovered from the binary):

```text
# Subagents (%d total)
# Subagents (%d total, showing %d most recent)
The following subagents were spawned during this conversation.
The following are the most recent subagents spawned during this conversation.
```

A live checkpoint supplied the concrete section. Conversation
`43966145-9669-4114-ac5c-91e08c7cd05b`, checkpoint at step 7414:

```text
# Subagents
The following subagents were spawned during this conversation (up to 20).

{
  "spec":  {
    "typeName":  "DeepCoder",
    "role":  "Subagent & Task TUI Alignment Engineer"
  },
  "result":  {
    "conversationId":  "b1923c8b-d44e-4b8a-a368-29b54434bee4",
    "logAbsoluteUri":  "file:///Users/gear/.gemini/antigravity-cli/brain/b1923c8b-…/.system_generated/logs/transcript.jsonl",
    "workspaceUris":  [
      "file:///Users/gear/GitHub/openagy"
    ]
  },
  "spawnStepIndex":  7363
}
```

The same conversation's checkpoints from step 23467 onward carry the
running-task section:

```text
# Running Background Tasks
The following background tasks were running at the time this checkpoint was created.
Use the manage_task tool to interact with them (e.g. to kill them or check their status).

{
  "taskId":  "43966145-…/task-23437",
  "toolName":  "run_command",
  "toolSummary":  "Start proxy on port 19998",
  "description":  "/Users/gear/go/bin/pigasus proxy serve -addr 127.0.0.1:19998",
  "startTime":  "2026-11-03T04:05:44.024948Z",
  "stepIndex":  23437,
  "logUri":  "file:///Users/gear/.gemini/antigravity-cli/brain/43966145-…/.system_generated/tasks/task-23437.log",
  "isDaemon":  true
}
```

Both sections use protojson's multiline form: two spaces after the colon,
two-space indentation, zero values omitted, fields in proto field order, and
entries joined by a single newline. `# Conversation Logs` is always the final
section. The clone renders both from host-supplied refs; wiring the local
engine's task/subagent registries is the remaining host-side work.

## Subagent checkpoints: `# Parent Agent Messages`

A subagent's own checkpoints (a child conversation under
`brain/<child-id>/`) do not carry `# User Requests`. They carry
`# Parent Agent Messages` instead: the task the parent gave the child and the
messages the parent sent since, rendered in the reference's one-line agent
message form.

Live capture — conversation `a9756e99-fbff-4c39-b38b-6382a8451ea5`,
checkpoint at step 350:

```text
# Resuming from a compaction

You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:

# Parent Agent Messages
The following were messages from your parent agent in the truncated conversation in chronological order:
1. [Message] timestamp=2026-11-03T21:30:22Z sender=53dc2d4d-166f-4082-9dce-7ca95818e00d priority=MESSAGE_PRIORITY_HIGH content=<original_task>
Task: 好啊，那你去全面 Actor 化并且去 Pig 化，请你继续推进直到项目不含 pig 依赖
…
```

The two preamble constants are in the binary:

```text
# Parent Agent Messages
The following were messages from your parent agent in the truncated conversation in chronological order:
```

```text
# Parent Agent Messages
The following were the most recent messages from your parent agent in chronological order:
```

`cortex/utils.FormatCheckpointParentMessages` (`0x1013d9b80`) and
`FormatCheckpointUserRequests` (`0x1013d99b0`) both tail-call the shared
formatter at `0x1013d9c50`; the only difference is the section/preamble
strings and the captured-message slice.
`cortex/utils.CaptureParentMessages` (`0x1013d9560`) collects the parent's
messages the same way `CaptureUserRequests` collects the user's.

The clone keeps the same data in
`internal/app/subagenttools/parentcontext.go` (`ParentContext`, capped at ten).
`ParentContext.Lines` now renders it for the child's resume block, and
`compaction.ParentBlock` replaces `# User Requests` with it. The task is the
first entry; later parent messages follow; a repeated task is not listed
twice. The cap keeps the ten most recent entries and selects the "most
recent messages" preamble.

## Manual `/compact` on Gemini 3.8 Flash High

Captured with `agy 1.5.3` on a fresh conversation
(`e9bd3e8a-3d4a-4618-b9e2-6a4fd4860408`):

```sh
agy --model gemini-3.8-flash-high --output-format json -p "Reply with exactly: ok"
agy --model gemini-3.8-flash-high --conversation <id> --output-format json -p "/compact"
```

The CHECKPOINT step at index 4 is the manual compaction's output. It carries
the same four tails as an automatic checkpoint:

```text
# User Requests
The following were user requests from the truncated conversation in chronological order:
1. Reply with exactly: ok
2. /compact

<summary>
…
</summary>

You have the 1 following artifacts written to the artifacts directory:

[ARTIFACT: manual_artifact_probe]
Path: file:///Users/gear/.gemini/antigravity-cli/brain/e9bd3e8a-…/manual_artifact_probe.md
Last Edited: 2026-11-08T08:52:45Z

# Conversation Logs
…
```

Three facts this pins:

1. A manual checkpoint includes `# Conversation Logs` even though
   `gemini-3.8-flash-high` resolves to `MODEL_PLACEHOLDER_M124`, whose
   `include_conversation_log` is `false`. The manual path does not consult the
   profile flag for that section.
2. `/compact` is recorded as a user request. A second manual compaction on the
   same conversation listed the first `/compact` too (`… 2. /compact …
   4. /compact`), so the list is persisted across the session replacement.
3. An artifact written with `write_file` under
   `<appDataDir>/brain/<conversation-id>/` is recognized by the artifact layer
   and gets the sidecar; the section renders it. The sidecar may omit
   `userFacing` — the artifact is still listed (the probe's sidecar had only
   `summary` and `updatedAt`).

The manual summary itself used the seven-section shape (Task Overview,
Progress, Key Findings, Errors & Fixes, Active Context, Next Steps,
Commitments & Constraints), the same as the automatic summaries in the other
captures.

## Stored checkpoint proto and the `/resume` round trip

Reading the conversation database behind the live manual capture
(`conversations/e9bd3e8a-….db`) gives the field numbers the transcript JSONL
cannot show. The checkpoint step (type 23) stores summary, boundary, artifacts
and requests in `step_payload` field 30:

```text
30.1   checkpoint index (omitted when 0)
30.5   summary bytes, the whole <summary>…</summary> block
30.11  first included step
30.12  first step after the checkpoint
30.14  artifact snapshot: 1 name, 3 file URI, 4 updatedAt Timestamp, 6 metadata
30.30  one entry per user request, each a nested message {1: text}
```

The second manual checkpoint lists all four requests in order
(`Reply with exactly: ok`, `/compact`, the `write_file` prompt, `/compact`),
so the list survives the session replacement in the reference's own store.

The clone's writer previously had no `StepCheckpoint` case, so it wrote an
empty payload. `Conversation.Checkpoint()` deliberately ignores a payload-less
type-23 step, which meant `/resume` replayed the full pre-compaction history.
The writer now emits the shape above and the reader reads 30.30 (the earlier
guess, field 19, remains only as a fallback).

Two more observations from the same live run against real CloudCode:

- the summary stream can arrive as `<think>…</think><summary>…</summary>`;
  the reference stores only the `<summary>` span, so the clone normalizes it
  before storing;
- a manual `/compact` on a two-step conversation reports `context 11.9k →
  ~20.7k`, i.e. the carried block can be larger than the summary itself on a
  short conversation, matching the reference's accounting.

A live pigasus TUI run on Gemini 3.8 Flash High (conversation `d2d640b0-…`)
then produced a persisted checkpoint with `included=[0,3)`,
`requests=["Reply with exactly: ok","/compact"]` and a clean `<summary>` body.
