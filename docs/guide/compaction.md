# Compaction

> The actor's step-boundary checkpoint, the reference's per-model policy, and
> what is still unaligned.

## 1. Where it runs

Automatic compaction is a **checkpoint at a tool-step boundary, inside the
turn** — not a post-turn reset. The sequence is:

```text
step N completes tool calls
  -> actor asks TurnConfig.Checkpoint(probe)
  -> host policy decides (hard limit, soft threshold, step gate)
  -> summariser writes the continuation summary
  -> actor records a Checkpoint boundary
  -> the same turn calls the model again with:
       "# Resuming from a compaction" + the moving window
```

The turn is never cancelled by a checkpoint. `FinalOutput` keeps the text the
user already saw, and `TurnRecord.Steps` keeps every model/tool step; the
checkpoint is a separate trajectory record (`actor.Checkpoint`), not a turn
step.

The seam lives in `actor` (`TurnConfig.Checkpoint`); the policy lives in
`internal/features/checkpoint`; the reference's per-model numbers live in
`internal/features/models/ctx`; the prompt/resume/accounting live in
`internal/core/compaction`.

## 2. The reference's policy

The profile is `CascadeConfig.checkpoint_config`. The fields the clone reads:

| field | behaviour |
|---|---|
| `enabled` | master switch |
| `strategy` | `SAME_MODEL` (session model), `SINGLE_PROMPT` (`checkpoint_model`), `UNSPECIFIED` |
| `max_token_limit` | effective window = `min(model input limit, max_token_limit)` |
| `max_output_tokens` | reserve held back for the next step |
| `token_threshold` | soft trigger (50 000 in every shipped payload; consumer not located; the clone keeps it **off by default**) |
| `max_overhead_ratio` | base-context budget (0.15; consumer not located) |
| `moving_window_size` | steps kept outside the summary (1 in every payload) |
| `checkpoint_model` | `MODEL_PLACEHOLDER_M50` = `gemini-3.1-flash-lite` |
| `use_last_planner_model` | overrides `checkpoint_model` |
| `is_sync` | `true` for `SAME_MODEL`, `false` for most `SINGLE_PROMPT`/`UNSPECIFIED` |
| `max_user_requests` | cap on the resume block's request list (10) |
| `include_last_user_message` | keep the last user message outside the summary |
| `include_conversation_log` | add the fixed `# Conversation Logs` tail (written when the profile sets it) |
| `include_*_snapshots` | artifact, running-task and subagent sections render from host-supplied refs |
| `retry_config` | checkpoint retries (`max_retries`, initial sleep, multiplier) |
| `session_summary_prompt_override` | the continuation-summary instruction |
| `use_model_reported_token_count` | 1.5.3 sets it `true` on every shipped profile; the policy prefers the model's reported count and falls back to 3 bytes/token |

The step filter is the reference's: it refuses to checkpoint right after a
user-input or model-response step, i.e. automatic checkpoints happen after
tool steps. The clone adds a **hard-limit override**: when the next call cannot
fit, a checkpoint is taken regardless of step type. Without it a single-step
turn or a pure-chat session could overflow before any tool step.

The live 1.5.3 binary ships the same profile table as 1.4.8 except that
every payload sets `use_model_reported_token_count` to `true`; the extracted
document is `internal/features/models/ctx/testdata/model_profiles_1.5.3.json`.
The 1.5.3 payloads omit `moving_window_size`; the clone keeps the 1.4.4 value
(1) and marks the omission as an open item.

The soft `token_threshold` trigger is **off by default**
(`checkpoint.Config.EnableSoftThreshold`). The 13 automatic checkpoints in the
live 1.5.3 transcript on this machine all fired with `cache_read_tokens` around
252k-255k — hard-limit checkpoints at ~258k context, not 50k soft checkpoints.
Tests that pin the soft path opt in explicitly. Until the threshold's consumer
is located in the binary, firing at 50k would be a guess with visible
consequences (far more frequent summaries).

**Known unknowns** (documented, not guessed):

- the `shouldCheckpoint` sentinel comparison (`+0x10`) was not identified;
- the consumers of `token_threshold` and `max_overhead_ratio` were not located
  (the soft trigger is therefore opt-in, see above);
- the automatic instruction tail was never captured (the clone sends the body
  with **no** on-demand tail);
- the exact runtime semantics of `is_sync` / `full_async` were not observed;
- `include_last_user_message` was only observed on a manual 3.8 checkpoint,
  where it produced no separate section (the last request stayed in
  `# User Requests`); the automatic shape is still unverified;
- `moving_window_size = 0` still has no direct observation (see above).

## 3. What a checkpoint carries

The actor applies `CheckpointDecision`:

```go
type CheckpointDecision struct {
    Apply               bool
    ResumeText          string
    IncludedThroughStep int
    KeepRecent          int
    KeepInput           bool
    ...
}
```

`Trajectory.ToConversationMessages` starts at the latest checkpoint's
`ResumeText` and then includes only steps with a global index greater than
`IncludedThroughStep`. The checkpoint view is grouped by global step so the
policy can cut `moving_window_size` steps out of the summary and keep them in
context. `KeepInput` preserves the current turn's user message when the
checkpoint happened at the start of a turn and the input is still pending.

`ResetEpoch` changes only on `ResetHistory`/`SeedHistory`; a checkpoint bumps
`Revision` but not `ResetEpoch`. `agentturn.Local` uses this to keep its actor
across a checkpoint and rebuild it only when the host replaced the transcript.

## 4. The resume block

`internal/core/compaction` builds the reference's block:

```text
# Resuming from a compaction

<continuation preamble>

# User Requests
1. ...
...

<summary>...</summary>
```

`max_user_requests` is 10; the `<summary>` wrapper comes from the instruction.

When the profile sets `include_conversation_log`, the block ends with the
reference's fixed tail. The paths are the reference's own placeholders — it
does not expand `<appDataDir>` or `<conversation-id>` at checkpoint time:

```text
# Conversation Logs
Start with the compact transcript; if a step has `truncated_fields`, read only that specific line in `transcript_full.jsonl`:
- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`
- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript_full.jsonl`
```

When the host supplies artifacts, the block also carries the captured
snapshot section (header, then one three-line entry per artifact):

```text
You have the N following artifacts written to the artifacts directory:

[ARTIFACT: name]
Path: file:///…
Last Edited: 2026-11-07T16:01:33Z
```

The running-task section (【一手】 the checkpoints from step 23467 of
conversation `43966145-…`) is:

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
  "logUri":  "file:///…/tasks/task-23437.log",
  "isDaemon":  true
}
```

The entries are protojson `TaskSnapshot` objects, pretty-printed with
`Multiline:true` and a two-space indent, joined by newlines. `startTime` is
protojson's timestamp form (0/3/6/9 fractional digits).

The subagent section (【一手】 checkpoint at step 7414 of conversation
`43966145-…`) is:

```text
# Subagents
The following subagents were spawned during this conversation (up to 20).

{
  "spec":  {
    "typeName":  "DeepCoder",
    "role":  "…"
  },
  "result":  {
    "conversationId":  "…",
    "logAbsoluteUri":  "file:///…",
    "workspaceUris":  [
      "file:///…"
    ]
  },
  "spawnStepIndex":  7363
}
```

A subagent's *own* checkpoint is different: its carried half is
`# Parent Agent Messages`, not `# User Requests`. `compaction.ParentBlock`
renders the task the parent gave the child and the parent's later messages,
one `agentmessage.RenderLine` entry each; the child policy receives them
through `checkpoint.Snapshot.ParentMessages`. The capped variant's preamble is
`The following were the most recent messages from your parent agent in
chronological order:`.

When the 20-snapshot cap hides older subagents, the header becomes
`# Subagents (%d total, showing %d most recent)` and the preamble becomes
`The following are the most recent subagents spawned during this conversation.`
## 4.1 Host supply points

The three snapshot sections describe live session state, so the host supplies
them per turn through `agentturn.Env.CheckpointSnapshot`; `Local` copies the
result into the session policy's `checkpoint.Snapshot` before the turn runs.
`checkpoint.Config.Artifacts` / `RunningTasks` / `Subagents` remain the static
form for hosts that do not keep state across turns.

| host | artifacts | running tasks | subagents |
|---|---|---|---|
| TUI (`agentturn.Local`) | `artifacts.List(conversationID)` | `shell.RunningTaskSnapshots` | `subagenttools.Registry.SnapshotSection` |
| agent server / `rchost` | `artifacts.List` | `shell.RunningTaskSnapshots` | none (no registry) |
| `--print`, `cmd/harness` | none | none | none |

`artifacts.List` reads `<appDataDir>/brain/<conversation-id>/<name>.<ext>` and
its `<name>.<ext>.metadata.json` sidecar. A file without the sidecar was not
written through the artifact layer, so it is not listed; `userFacing:false`
is dropped; the timestamp is truncated to whole seconds. The clone has no
artifact writer yet, so in a fresh pigasus conversation the section is
usually empty.

A background task records its creating tool, `toolSummary`, `isDaemon` and
global step when it is launched; `RunningTaskSnapshots` skips tasks that
already completed or were killed. A subagent records its spawn step and log
URI at launch; `SnapshotSection` keeps every child (not only the running
ones) and caps the list at 20, newest first.

The block is sent as the first user message after the checkpoint.

## 5. Sync and async

- `SAME_MODEL` profiles (Gemini 3.8) are synchronous: the turn pauses at the
  step boundary, the status row shows `Compacting context…`, and the next call
  starts from the resume block.
- `SINGLE_PROMPT`/`UNSPECIFIED` profiles with `is_sync=false` are allowed to
  run the summary in the background. The actor keeps using the full context
  until the summary is ready; the next `BeforeStep` applies it. A hard-limit
  call waits for the summary (or fails with `ErrCheckpointRequired`).
- The async job snapshots its cut at plan time; steps generated later are not
  folded into a late-arriving summary.

## 6. Persistence and `/resume`

`convstore` treats `CORTEX_STEP_TYPE_CHECKPOINT = 23` as a checkpoint, not a
subagent. The parser reads `CortexStepCheckpoint` from `step_payload` field
30 (`included_step_index_start/end`, `included_step_indices`,
`session_summary`, `user_requests`, `user_intent`). A checkpoint with no
payload is opaque and is ignored.

【一手】 agy 1.5.3, the database behind the live manual `/compact`
(conversation `e9bd3e8a-…`) pins the field numbers: the checkpoint index is
30.1, the summary 30.5 (plain bytes, the whole `<summary>…</summary>`), the
included range 30.11/30.12, and each user request is its own 30.30 entry — a
nested message whose field 1 is the text. (An older cycle guessed 19; no
reference database uses it. The reader keeps 19 only as a fallback for stores
written by that older clone.) The same payload also carries the artifact
snapshot at 30.14, so `/resume` can rebuild the artifact section.

The writer emits that shape. A checkpoint persisted without it is not just
lossy: `Checkpoint()` treats a payload-less type-23 step as an opaque
placeholder, so a clone restart would replay the whole pre-compaction history
instead of the summary. A live pigasus `/compact` on Gemini 3.8 Flash High
confirmed the round trip after the writer was fixed.

The summary is normalized to the `<summary>…</summary>` span before it is
stored. CloudCode can prepend the model's `<think>…</think>` block to the same
stream; the reference's stored summaries never contain it.

`Conversation.Checkpoint()` returns the latest usable boundary.
`Conversation.ChatTurns()` then starts at the resume block and returns only
the user/model steps with `Idx >= IncludedEnd`. `/resume`, `/continue` and
`--continue` therefore seed the model with the compacted context instead of
the full pre-checkpoint history; the transcript shows `Conversation
compacted` and the post-checkpoint steps.

On save, a turn's checkpoint is placed in `convstore.Trajectory.Calls` at the
number of calls that preceded it. `BuildSteps` assigns the step index and sets
`IncludedStart`/`IncludedEnd` (end exclusive), so the next `/resume` reads the
same boundary. The placement is approximate when a checkpoint happens before
a later tool call in the same turn; it preserves the boundary but may put the
marker after the turn's calls.

## 7. Host coverage

| host | checkpoint path |
|---|---|
| TUI / ACP / AHP (`agentturn.Local`) | full policy, `Compact` frames, host session update |
| `--print` (`printmode`) | policy on each actor turn; no UI notice yet |
| `pigasus agent-server` / `rchost` | policy on each remote cascade turn |
| `cmd/harness` | policy on each turn; emits the compaction step and calls `session_before_compact` |
| subagents (`subagenttools.Launch` / `Execute`) | policy when the parent supplies `Deps.Backend` |

`agentturn.Local` keeps one policy (and one emitter) alive across turns for
the same model, so an asynchronous summary that finishes after a turn is
applied by the next one. It also refreshes the policy's snapshot sections from
the host before each turn, so a task or subagent started by an earlier turn
appears in the next checkpoint. Print, remote and harness create a policy per
turn; their asynchronous summaries are lost if the turn ends first, and only
the local hosts have live artifact/task/subagent registries. The synchronous
`SAME_MODEL` profiles (Gemini 3.8) are complete everywhere.

## 8. Manual `/compact`

`/compact` still uses the generic summariser (`compaction.ComputeSummary`, with
the on-demand tail). The TUI replaces the host session, bumps the session
epoch so the local actor rebuilds from the compacted history, and appends a
`StepCheckpoint` to the durable trajectory. It does not yet run through the
actor seam, so the in-flight turn is not involved. The automatic path uses
`compaction.ComputeSummaryAuto`, which sends the instruction body with no
tail.

The manual checkpoint carries the same four tails as the automatic one. A live
`agy 1.5.3` `/compact` on Gemini 3.8 Flash High (conversation
`e9bd3e8a-…`) ended with the artifact section and the fixed `# Conversation
Logs` tail. The profile (`MODEL_PLACEHOLDER_M124`, 3.8's alias) has
`include_conversation_log: false`, so the manual path does **not** consult that
flag; the clone always includes the tail for `/compact`.

The live list also pins the request bookkeeping: `/compact` is itself a user
request, every earlier `/compact` stays in the list, and the list is carried
across the session replacement. The clone therefore seeds the list from the
last persisted checkpoint, appends the session's new user messages, appends
the current `/compact`, and caps at ten. `compaction.UserRequests` ignores the
resume block itself, so a rebuilt actor cannot mistake it for a request.

## 9. Evidence

- `docs/cycles/auto-compact-parity/captures.md` — live `/compact` captures,
  checkpoint proto, profile payloads, trigger decompilation.
- `docs/cycles/auto-compact-parity/deliverable.md` — the parity table, hard
  constraints and open questions.
- `docs/cycles/cycle-303-cascade-model-profiles/deliverable.md` — the 1.4.8
  catalogue, clamps and model profiles.
- `internal/features/models/ctx/profiles_1.4.8.go` + `testdata/` — the shipped
  per-model checkpoint payloads.
- `docs/cycles/auto-compact-parity/evidence-1.5.3.md` — the 1.5.3 profile
  extraction, the Go 1.28 pclntab offset, `shouldCheckpoint` and the default
  config.
- `docs/cycles/auto-compact-parity/parity-table.md` — every open behaviour,
  its observable surface, confidence, cost and next action.
