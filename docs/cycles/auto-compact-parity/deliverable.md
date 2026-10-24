# Auto-compact parity — deliverable

**Cycle:** `auto-compact-parity`
**Reference:** `agy` **1.4.5** (`~/.local/bin/agy`, auto-updated from the
1.4.4 the clone still emulates)
**Evidence:** `docs/cycles/auto-compact-parity/captures.md`

---

## 0. Summary

Two things happened in this cycle.

1. **The `/compact` completion UX was brought to byte-parity.** The
   reference announces a compaction with a *centred rule* carrying the
   label `Conversation compacted` and a *centred stats line* under it.
   The clone used to print a transcript bullet
   (`⎿  Compacted 12 messages (-87%)`). It now prints the reference's
   two rows, with the reference's geometry (rule = width − 1 columns,
   label centred inside it; stats line centred on the full width).

2. **The reference's auto-compaction model was read out of the binary**
   and compared against the clone's. The two are structurally
   different systems: the reference compacts inside the *cascade* on a
   per-model token limit — the smaller of the server catalogue's
   `maxTokens` and the model's checkpoint `max_token_limit`, which is
   256 000 for the Gemini 3.8 Flash family, 160 000 for the Claude
   4.6 family, 80 000 for gpt-oss-120b-medium — with an
   experiment-gated *checkpoint* strategy (`token_threshold` 50 000,
   `max_overhead_ratio` 0.15); the clone compacts inside *PiG* on a
   `contextWindow − ReserveTokens` rule that, as wired today, can
   never fire correctly.

3. **The reference's compaction semantics were read out of the binary
   and adopted.** The stats row's two numbers are now *resolved*: the
   first is the model-reported context of the compaction request, the
   second is the base-context estimate plus the summary's size — on
   *different scales*, which is why the second is the larger one in
   every reference capture. The clone now sends the reference's
   **verbatim** summary instruction (2409 bytes) as the final user
   turn, reports the model-reported token counts, installs the
   reference's `# Resuming from a compaction` block in place of the
   compacted history, estimates tokens at the reference's three bytes
   per token, and advertises a real context window to PiG so its
   auto-compaction rule stops being degenerate.

4. **The auto-compaction trigger was decoded, and its limit adopted.**
   `checkpoint.shouldCheckpoint` (0x101a81d10) is the reference's
   decision function; the *limit* it works against is computed in
   `chatconverters.(*TrajectoryChatConverter).TrajectoryToChatMessages`
   (0x10150abe0) as `min(the model's own limit, the checkpoint's
   max_token_limit)`, and the reference holds the checkpoint's
   `max_output_tokens` back for the next step. PiG's rule
   (`contextTokens > contextWindow − reserveTokens`) is *the same
   comparison*, so the clone now fills in the reference's per-model
   numbers: `internal/modelctx` holds the captured catalogue, every
   model handle advertises its own window, and the runtime derives its
   reserve from the same table. What is still missing is the trigger
   *function* — its sentinel comparison, its step-type filter and its
   strategy switch (§2.4, §6).

5. **The checkpoint's instruction turned out to be the one the clone
   already sends.** The catalogue's per-model
   `session_summary_prompt_override` is **byte-identical** to
   `internal/compaction.SummarizePrompt` (2289 bytes), so the
   reference's automatic compaction asks for the same six-section
   `<summary>` block as its manual `/compact` — the clone needs no
   second prompt (§2.5). The nineteen cascade knobs and what each one
   does are tabulated in §2.7, and PiG's divergent compaction contract
   in §2.6.

The reference's *auto*-compaction remains unimplemented as a
**mechanism**: it is not reachable from the CLI without an experiment
assignment, and the clone has no cascade layer at all. This cycle's
deliverable is the parity **report** plus the manual `/compact` UX and
semantics alignment, and the per-model context budget PiG compares
against.

### 0.1 Terminology — what a *cascade* is

The word appears throughout this report without ever being defined. It is
**not** "cascading calls", and it has nothing to do with compaction
directly: a *cascade* is **one run of the agent**.

*Naming lineage.* Cascade was Codeium's (Windsurf's) agent product. The
name survived the rename to Antigravity inside the runtime. The binary
still carries the old identity line verbatim 【strings】:

> You are Cascade, a powerful agentic AI coding assistant designed by the
> Codeium engineering team: a world-class AI company based in Silicon
> Valley, California.

*The RPC surface* names it as a run 【strings】: `StartCascade`,
`SendUserCascadeMessage`, `RevertCascadeToStep`,
`HandleCascadeUserInteraction`, `GetCascadeNuxes` — start a run, feed it
a message, roll it back to a step, interact with it.

*The runtime state* is `google3/third_party/jetski/cortex/cascade_run_state`:
`NewCascadeRunState`, `SetExecutorLoopStatusLocked`,
`SetExecutableStatusLocked`, `AddBackgroundTask`, `SyncPendingMessages`,
`UpdateTrajectoryFileDiffs`, `registerWithArtifactsWatcher`,
`closeSubagentForwarder`. The status enum is `CascadeRunStatus`
(`CASCADE_RUN_STATUS_RUNNING`). Every trajectory carries a `cascade_id`
(306 occurrences of the token in the binary).

*A cascade is a cascade of steps.* The executor runs the model repeatedly
("generator invocations") and each turn appends steps to the run:
`CORTEX_STEP_TYPE_VIEW_FILE`, `_SEARCH_WEB`, `_GIT_COMMIT`,
`_CHECKPOINT`, `_SHELL_EXEC`, `_WRITE_BLOB`, `_PLAN_INPUT`,
`_USER_INPUT`, `_RPC_ACTION`, `_BROWSER_*`, … `CascadeExecutorConfig`
bounds the loop 【strings — proto getters, read out of the binary's
pclntab】:

| field | meaning |
|---|---|
| `MaxGeneratorInvocations` | how many model turns a run may take |
| `MaxForcedInvocations` | … including the forced continuation turns |
| `TerminalStepTypes` | the step types that end the run |
| `HoldForValidCheckpoint` / `HoldForValidCheckpointTimeout` | hold the run until a checkpoint is valid |
| `QueueAllSteps` | buffer steps instead of streaming them |
| `EnableTasks` / `RequireFinishTool` | task and finish-tool semantics |
| `UseAggressiveSnapshotting` / `CortexSnapshotRevert` | snapshotting and rollback |
| `DisableEmptyOutputContinuation` / `MaxConsecutiveEmptyOutputRetries` | empty-output handling |

*The behaviour profile is a proto.* `CascadeConfig` (224 occurrences)
carries the sub-messages `PlannerConfig` (`CascadePlannerConfig`),
`MessageConfig`, `ExecutorConfig` (`CascadeExecutorConfig`),
**`CheckpointConfig`**, `ConversationHistoryConfig`, `AgentApiConfig`,
`BattleModeConfig`, `TrajectoryConversionConfig`. `cortex/config`
resolves it:

| function | role |
|---|---|
| `defaultConversationalCascadeConfig` | the default conversational profile |
| `GetCascadeConversationalConfigWithExperiment` | layer the experiment assignment on top |
| `InheritSubagentCascadeConfig` | derive a subagent's profile from its parent's |
| `MergeCascadeConfigsWithOverwrites` / `applyCascadeConfigListOverwrites` | merge |
| `FoldCascadeConfigIntoAgent` / `mergeInheritedCascadeIntoSpec` | fold it into the agent spec |
| `SelfSpecFromCascadeConfig` | derive the agent's own spec |

**Why this report says "the reference compacts inside the cascade".**
Auto-compaction is not a separate subsystem: it is the
`checkpoint_config` **sub-message of the cascade config**. The binary's
own log line joins the two names 【strings】:

```
Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))
```

And `CheckpointConfig`'s fields are exactly the knobs §2.3 recovered by
other means 【strings — proto getters】:

| `CheckpointConfig` field | the knob |
|---|---|
| `max_token_limit` | the per-model limit — the effective one is `min(maxTokens, max_token_limit)`, i.e. 80 000 / 128 000 / 160 000 / 256 000 / 1 000 000 (§2.3) |
| `token_threshold` | 50 000 |
| `max_overhead_ratio` | 0.15 |
| `max_user_requests` | the ten user requests kept in the resuming block |
| `use_model_reported_token_count` | whether the model's own count is used |
| `strategy`, `checkpoint_model`, `checkpoint_prompt_override`, `continuation_prompt_override`, `session_summary_prompt_override` | which model, which instructions |
| `include_conversation_log` | the `# Conversation Logs` section the clone omits |
| `include_last_user_message` | keep the last user message |
| `moving_window_size`, `max_output_tokens`, `max_user_request_bytes`, `max_artifact_section_bytes` | the truncation budgets |
| `include_artifact_snapshots`, `include_subagent_snapshots`, `include_running_task_snapshots` | what the snapshot carries |

The `CASCADE_USE_EXPERIMENT_CHECKPOINTER` payload named in §2.3 is the
experiment that `GetCascadeConversationalConfigWithExperiment` layers
over the default profile — i.e. the checkpoint strategy is switched on by
an experiment assignment, which is why the path is unreachable from the
CLI by default.

**What the clone does not have.** The clone has no cascade layer: no
`CascadeConfig`, no planner/executor split, no steps, no trajectory. Its
`--print` path is PiG's single model↔tool loop, and its `/compact` is the
clone's own summariser in `internal/compaction`. That is why §5 lists
"auto-compact in the TUI" and "auto-compact in print mode" as
deviations, and why the clone has no `# Conversation Logs` section to
emit.

---

## 1. Method

* **Live pty captures** of the real `agy` binary for everything about
  the manual `/compact` UX (progress label, completion rows, geometry
  at three terminal widths, a longer conversation).
* **String-table reads** of the same binary for the auto-compaction
  model (the cascade's experiment payload, the `checkpoint_config`
  proto, the overflow error, the summary prompt, the summary cache) —
  the auto-compaction path is not reachable from the CLI, so the binary
  is the only evidence available.
* **Source reads** of the clone's two compaction implementations and of
  PiG v0.5.1's own compaction rules.
* **Decompilation** of the agy 1.4.5 binary (Ghidra 12.1.4, 160 664
  pclntab seeds applied, 0 failures) for the rendering path
  (`cli/render/render.compactionStatsParts`, `CompactionTokens`,
  `ContextOverheadTokens`, `fmtTokens`) and the token estimator
  (`jetski/tokenizer`'s `EstimateTokenCounter`).

Evidence grades: 【一手】 = measured on the live binary; 【反编译】 =
read out of decompiled code at a confirmed address; 【strings】 =
read out of the binary's string table; 【推测】 = hypothesis.

---

## 2. The reference (agy 1.4.5)

### 2.1 Manual `/compact` 【一手】

The reference's `/compact` has three observable parts.

**The progress label.** One string, painted in two places (the status
row and the spinner row):

```
Compacting conversation… (0s · esc to cancel)
```

The body is `#657B83`; the word `cancel` is `#268BD2`. The elapsed
counter is part of the label.

**The completion rows.** Two centred rows:

```
────────────────────────────────────────────────────── Conversation compacted ──────────────────────────────────────────────────────
                             3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s
```

* the rule is **width − 1** columns wide, `#586E75`, with the label
  centred inside it (floor on the left half, ceil on the right);
* the stats line is centred on the **full** width with a cursor move
  to column `(width − cells) / 2 + 1`, `#657B83`;
* the stats line has four fields: the number of steps summarized, the
  context pair, the summary's token count, and the elapsed time.

**The refusal.** `Nothing to compact yet.` for a session with nothing
to compact (the string is in the 1.4.5 binary; the clone already
prints it).

### 2.2 The stats row's two numbers 【反编译】

`cli/render/render.compactionStatsParts` assembles the row; the format
string in the binary is 31 bytes — `context %s → ~%s (%s summary)` —
with three formatted numbers.

| field | what the binary computes | scale |
|---|---|---|
| `context A` | the compaction request's **model-reported** context (`CompactionTokens`'s first return value, read from the compaction step's usage) | the model's real tokens |
| `→ ~B` | the caller's **base context** argument (`render.ContextOverheadTokens`) **plus** the summary's size | the reference's 3-bytes-per-token estimate |
| `(C summary)` | the summary's own size (`CompactionTokens`'s second return value — the reported completion tokens) | the model's real tokens |

The two numbers are therefore on *different scales*, which resolves the
open question of the previous report: the pair is **not** a before/after
of one quantity, and the second number being larger is expected. The
step count is the compacted step range's width, printed with a singular
variant for one step.

The reference's token estimate is three bytes per token —
`EstimateTokenCounter.GetTokenCount` computes `int(float64(len) /
divisor)` and `createDefaultTokenCounter` stores `0x4008000000000000`,
the IEEE-754 double **3.0**.

The exact decompiled code is in `captures.md` §4.

### 2.3 Auto-compaction 【strings + 一手】

The reference compacts **inside the cascade**, and the cascade's
checkpoint configuration is a per-model *experiment payload* keyed
`CASCADE_USE_EXPERIMENT_CHECKPOINTER`:

```json
{
  "strategy": "CHECKPOINT_STRATEGY_SINGLE_PROMPT",
  "max_token_limit": "128000",
  "token_threshold": "50000",
  "max_overhead_ratio": "0.15",
  "moving_window_size": "1",
  "enabled": true,
  "max_output_tokens": "16384",
  "checkpoint_model": "MODEL_PLACEHOLDER_M50",
  "use_last_planner_model": false,
  "is_sync": false,
  "max_user_requests": 10,
  "include_last_user_message": false,
  "include_conversation_log": true,
  "include_running_task_snapshots": true,
  "include_subagent_snapshots": true,
  "include_artifact_snapshots": true,
  "retry_config": { "max_retries": 0, "initial_sleep_duration_ms": 1000,
                    "exponential_multiplier": 2, "include_error_feedback": false },
  "session_summary_prompt_override": "",
  "use_model_reported_token_count": false
}
```

The binary carries **fifteen** such payloads — one per model profile, the
model named by the `model_profiles` map the payload sits in — but the
decisive source is the **server's own catalogue**: every entry of
`fetchAvailableModels` carries the model's `maxTokens` *and* its own
`CASCADE_USE_EXPERIMENT_CHECKPOINTER` payload. The captured catalogue
(`docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json`,
agy 1.4.4, live probe) pins the numbers per catalogue id — see
`captures.md` §2.2 for the full table. For the models the CLI can select:

| catalogue id | internal model | `maxTokens` | checkpoint `max_token_limit` | checkpoint `max_output_tokens` |
|---|---|---|---|---|
| `gemini-3.8-flash-high` | `MODEL_PLACEHOLDER_M318` | 1 048 576 | 256 000 | 16 384 |
| `gemini-3.8-flash-medium` | `MODEL_PLACEHOLDER_M319` | 1 048 576 | 256 000 | 16 384 |
| `gemini-3.8-flash-low` | `MODEL_PLACEHOLDER_M320` | 1 048 576 | 256 000 | 16 384 |
| `claude-sonnet-4-6` | `MODEL_PLACEHOLDER_M35` | 250 000 | 160 000 | 16 384 |
| `claude-opus-4-6-thinking` | `MODEL_PLACEHOLDER_M26` | 250 000 | 160 000 | 16 384 |
| `gpt-oss-120b-medium` | `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` | 131 072 | **80 000** | **8 192** |
| `custom-minimax-m3` | `MODEL_PLACEHOLDER_M488` | 1 000 000 | — (no experiment) | — |

`token_threshold` is 50 000 and `max_overhead_ratio` 0.15 everywhere.

The surrounding machinery is visible in the binary: a
`checkpoint_config` proto on the cascade config (§0.1 — the cascade is
one run of the agent, and the checkpoint config is one of its
sub-messages), a
`CortexStepCheckpoint` entry type (`accumulated_sections`,
`included_step_indices`, `memory_summary`, `conversation_log_uris`),
`compacted_at_step_indices` bookkeeping, a background `summaryCache`
filler for deferred conversations, and a continuation-summary prompt
with six mandatory sections ("Task Overview", "Progress", "Key
Findings", "Active Context", "Next Steps", "Commitments &
Constraints").

Most useful of all, the reference's own **token accounting** is
printed verbatim in its overflow error:

```
the most recent step (%s) could not fit in the available context limit:
  Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))
    - Base context: ~%d tokens (instructions: ~%d, tool definitions: ~%d)
    - Most recent step: ~%d tokens (text: %d, image: %d)
```

So the reference's context is *instructions + tool definitions + the
step*. The clone's system prompt alone is 34 350 bytes (~8.6k tokens)
and the twelve tool declarations are of the same order — which is why
the reference's "13.1k" is so much larger than the clone's own
numbers.

### 2.4 The trigger, decompiled 【反编译】

The instruction-stream excerpts this section quotes are in
`captures.md` §2.7.

The decision function is
`google3/third_party/gemini_coder/framework/handlers/checkpoint/checkpoint.shouldCheckpoint`
(0x101a81d10 in agy 1.4.5), called from
`checkpoint.MaybeCreateCheckpoint` (0x101a81970). It returns
`(bool, error)`; `MaybeCreateCheckpoint` reads the bool and returns
early when it is false.

**The limit.** The conversion limit is computed one level down, in
`chatconverters.(*TrajectoryChatConverter).TrajectoryToChatMessages`
(0x10150abe0):

```c
uVar5 = modelConfig[0x20] ? modelConfig[0x20][0x14] : 0;   // the model's own limit
iVar6 = config[0x10];                                       // the checkpoint config
if (iVar6 != 0 && config->enabled != nil && *config->enabled) {
    dVar1 = config->max_token_limit;                       // offset +0x10
    if (dVar1 != 0 && dVar1 <= uVar5) uVar5 = dVar1;       // min()
}
```

So the reference's effective context limit is

```
min(the model's own input limit, checkpoint_config.max_token_limit)
```

with the checkpoint's limit ignored when the checkpoint is disabled.
`cortex/utils.GetCheckpointConfig` (0x1013072c0) clamps the
experiment-provided limit to **≤ 256 000** (`if (0x3e7ff < dVar1)
dVar1 = 0x3e800`) and its output cap to **≤ 16 384** — and for one
model id (0x143 = 323) it returns a config with `max_token_limit =
1 000 000` and `enabled = true` instead.

`cortex/utils.GetDefaultCheckpointConfigForModel` (0x101306fb0)
computes the headroom the reference keeps free:

```
max(0, model.input_limit − max(40000, model.max_output_tokens))
```

— i.e. at least **40 000 tokens** are always left for the next step.

**The arithmetic.** The overflow error's format string (438 bytes at
vaddr 0x10357891a) spells the whole comparison out:

```
total attempted = base context + the most recent step
base context    = instructions + tool definitions
```

with the limit being `checkpoint_config.max_token_limit`. The ten
format arguments map to stack slots `+0x94` (total), `+0x5f8` (the
limit), `+0x90` (base), `+0xa8` (instructions), `+0xa4` (tool
definitions), `+0xb0` (step total), `+0xbc` (text), `+0xd0` (image).

**The gates.** `shouldCheckpoint` opens with three gates:

1. **Enabled.** `checkpoint_config.enabled` is a `*bool` at `+0xa0` of
   the config; nil or false ⇒ no checkpoint.
2. **The token source.** `checkpoint_config.use_model_reported_token_count`
   is a `*bool` at `+0x30`. The count then comes either from the step's
   own usage metadata or from the model's own tokenizer
   (`param_3[+0xa8]`, an `EstimateTokenCounter` at 3 bytes/token).
   When the model reported its own count, the step's stored count is
   *not* added again — the instruction stream shows the count zeroed
   for that case (`mov x0, xzr` at 0x101a81ee8).
3. **The step filter.** The reference looks at the last step of type
   23 (`utils.GetLastStepOfType(steps, …, 0x17)`). A step whose type
   is 1 or 2 (a user-input or model-response step) is refused, and one
   of type 7 with the fallback flags set returns the "checkpoint not
   allowed" error.

**The comparison — the one open item.** The final test is in the
instruction stream, not in Ghidra's rendering:

```
101a81f04: ldr  w4, [x3, #0x10]
101a81f08: cmp  x0, x4
101a81f0c: cset x0, ne
```

`x3` is the fourth return value of `TrajectoryToChatMessages` — a
pointer into the conversion result — and `x0` is the last step's token
count (zeroed when the model reported its own). The comparison is
literally **`!=`**, not a threshold, so the operand at `+0x10` is a
*sentinel* rather than a limit: the boolean means "the step's count
differs from the value the conversion recorded", not "the count is
over the limit". The field at `+0x10` was **not** identified. The
*limit* comparison the error message describes lives in the conversion
itself (*The arithmetic*, above), not in `shouldCheckpoint`.

**What the clone does with this.** The clone reproduces the *limit*,
not the trigger function:

* the window PiG compares the context against is the reference's
  effective limit — `min(maxTokens, max_token_limit)` — per model
  (`internal/modelctx`);
* the reserve PiG holds back is the reference's
  `checkpoint_config.max_output_tokens` — 16 384 for every model with
  a checkpoint experiment except `gpt-oss-120b-medium` (8 192).

So the clone's rule

```
contextTokens > contextWindow − reserveTokens
```

is the reference's own comparison — `base + step > max_token_limit`
with the output cap held back — with the per-model numbers filled in.
The reference's *trigger function* (`shouldCheckpoint`'s sentinel
comparison, its step-type filter and its strategy switch) is still not
reproduced; see §6.

---

### 2.5 The checkpoint's instruction is the same one 【一手】

The catalogue's per-model checkpoint payload carries a
`session_summary_prompt_override`. For the models whose strategy is
`CHECKPOINT_STRATEGY_SAME_MODEL` it is **2289 bytes** — and it is
**byte-identical** to `internal/compaction.SummarizePrompt`, the
instruction the clone already sends for the manual `/compact`:

| catalogue id | strategy | `session_summary_prompt_override` |
|---|---|---|
| `gemini-3.8-flash-{high,medium,low}` | SAME_MODEL | 2289 bytes |
| `gemini-3.5-flash-low`, `gemini-3.5-flash-extra-low`, `gemini-3.8-flash-tiered`, `gemini-3-flash-agent`, `gemini-4-argon-disabled` | SAME_MODEL | 2289 bytes |
| `gemini-2.5-{flash,flash-lite,flash-thinking,pro}`, `gemini-3-flash`, `gemini-3.1-flash-lite`, `gemini-3.5-flash-lite` | SINGLE_PROMPT | empty (the binary's built-in prompt) |
| `claude-sonnet-4-6`, `claude-opus-4-6-thinking`, `gpt-oss-120b-medium` | UNSPECIFIED | empty |

So the reference asks for the *same* six-section `<summary>` block in
both paths, and the instruction the clone sends for the manual
`/compact` is also the right one for the automatic path — the clone
does not need a second prompt. The identity is pinned by
`internal/compaction/prompt_test.go`
(`TestSummarizePrompt_MatchesCheckpointOverride`, with
`TestSummarizePrompt_MatchesLiveCatalogue` re-deriving the fixture from
the catalogue capture).

What this does **not** settle: whether the automatic path appends a
tail. The 2289-byte override is the *configured* instruction; the
on-demand path appends a further 120 bytes (`ON-DEMAND COMPACTION: …`,
reproduced as `internal/compaction.OnDemandCompactionSuffix`), making
the on-demand turn 2409 bytes. The automatic trigger's tail is still
unobserved — but it is now **bounded**: it can only be a suffix of the
2289-byte body, and the body itself is known.

### 2.6 PiG's compaction contract, and where it differs

The clone's *print* path's automatic compaction is PiG's, not the
reference's. PiG v0.5.1's `internal/codingagent/compaction` implements
a different contract on six surfaces:

| surface | PiG v0.5.1 | reference 1.4.5 |
|---|---|---|
| summariser system prompt | `SummarizationSystemPrompt` is sent as its own system message (`compaction.go:896`) | none — the normal Antigravity instructions stay as the system prompt |
| the transcript | re-serialized into one user message: `<conversation>\n…\n</conversation>\n\n` + the prompt | the transcript as-is |
| the instruction | `SUMMARIZATION_PROMPT` (or `UPDATE_SUMMARIZATION_PROMPT`) + `"\n\nAdditional focus: " + customInstructions` | the 2289-byte continuation-summary instruction, as the **final user turn** (§2.5) |
| the post-compaction history | one custom message — `"The conversation history before this point was compacted into the following summary:\n\n<summary>\n…\n</summary>"` (`agent/messages.go:235`) — plus the kept recent entries (`KeepRecentTokens` 20000) | the whole history replaced by the `# Resuming from a compaction` block (preamble + ten recent user requests + `<summary>`) |
| the token estimate | `ceilQuarter(chars) = (chars+3)/4` (`estimate.go:38`) | 3 bytes per token |
| the trigger | `contextTokens > contextWindow − reserveTokens` | `base + step > max_token_limit`, with the checkpoint's `max_output_tokens` held back |

The last row is now *equivalent in form* (§2.4, §4.6) — the clone
fills the numbers in from the reference's catalogue — but the reference
adds gates PiG has no notion of: its step-type filter, its strategy
switch and its sentinel comparison.

**The barrier is structural.** PiG's compaction lives in
`internal/codingagent/compaction`, and Go's `internal/` rule makes it
unimportable from the clone (whose module is
`github.com/justwasm/pigasus`). Neither `SUMMARIZATION_PROMPT` nor
`SummarizationSystemPrompt` is re-exported by the public `agent`,
`coding` or `ai` packages, and `Session.Compact`'s `customInstructions`
argument is *appended* to PiG's own prompt (`"Additional focus: …"`) —
it cannot replace it. The divergence therefore cannot be closed from
this side; it needs an upstream change.

**What the clone does instead.** `internal/compaction` implements the
reference's contract for the manual path — the instruction, the
resuming block, the usage accounting, the /3 divisor. The print path's
*automatic* compaction still runs PiG's contract, and that is the
deviation §6 records.

**The upstream change that would close it.** Three things in PiG:

1. a way to *replace* the summarisation instruction and to send none as
   a system message (a `CompactionSettings` field, or an exported
   `SummaryPrompt` override);
2. a way to shape the post-compaction history (the reference's
   resuming block) instead of PiG's one-message + keep-recent shape;
3. a token divisor of 3 rather than 4.

With those, `internal/compaction` could be deleted and both paths
would run PiG's.

### 2.7 The cascade knobs, and what each one does

The checkpoint config carries nineteen knobs. This is what each one
does and what the clone does with it:

| knob | what it does | evidence | clone |
|---|---|---|---|
| `enabled` | the checkpoint's master switch (a `*bool` at `+0xa0`) | 反编译 `shouldCheckpoint` | not implemented — the clone compacts whenever PiG's rule fires |
| `strategy` | which summariser writes the summary: `SINGLE_PROMPT` (a separate checkpoint model), `SAME_MODEL` (the current model), `UNSPECIFIED` | 一手 the payload; 反编译 the enum | not implemented — the clone always summarises with the session's model |
| `max_token_limit` | the context ceiling the checkpoint protects | 反编译 `TrajectoryToChatMessages` | **implemented** — the per-model window (`internal/modelctx`) |
| `max_output_tokens` | the output cap held back for the next step (≤ 16 384) | 反编译 `GetCheckpointConfig` | **implemented** — the reserve PiG holds back |
| `token_threshold` | a soft trigger below the hard limit | strings (50 000 in every payload) | not implemented — no consumer located |
| `max_overhead_ratio` | the allowed overhead of the base context | strings (0.15 in every payload) | not implemented — no consumer located |
| `moving_window_size` | how many steps one checkpoint moves | strings (1 in every payload) | not implemented |
| `checkpoint_model` | the model that writes the summary (`MODEL_PLACEHOLDER_M50` = `gemini-3.1-flash-lite` — a *cheaper* model than the session's) | 一手 the payload | not implemented |
| `use_last_planner_model` | summarise with the last planner's model instead | 一手 the payload | not implemented |
| `is_sync` | whether the checkpoint blocks the cascade | 一手 the payload | not implemented |
| `max_user_requests` | how many recent user requests the resuming block keeps | 一手 (10) | **implemented** — `internal/compaction.MaxUserRequests` |
| `include_last_user_message` | whether the last user message is carried into the summary | 一手 the payload | not implemented |
| `include_conversation_log` | whether the resuming block carries the conversation logs | 一手 the payload | **implemented** (omitted — the clone writes no trajectory) |
| `include_running_task_snapshots` | whether the running task's state is carried | 一手 the payload | not implemented — the clone has no task snapshots |
| `include_subagent_snapshots` | whether the subagents' state is carried | 一手 the payload | not implemented — the clone has no subagent snapshots |
| `include_artifact_snapshots` | whether the artifacts' state is carried | 一手 the payload | not implemented — the clone has no artifact snapshots |
| `retry_config` | the checkpoint's own retry policy (`max_retries`, `initial_sleep_duration_ms`, `exponential_multiplier`, `include_error_feedback`) | 一手 the payload | not implemented — the clone's retry budget is a separate mechanism (`internal/retrybudget`) |
| `session_summary_prompt_override` | the checkpoint's summarisation instruction | 一手 the payload | **the clone's manual instruction is byte-identical** (§2.5); the automatic path still uses PiG's |
| `use_model_reported_token_count` | whether the model's own token count is used instead of the local estimate (a `*bool` at `+0x30`) | 反编译 `shouldCheckpoint` | not implemented — and the *opposite* default: PiG prefers the model-reported count when the response carries usage (`agent.CalculateContextTokens`) and falls back to its own estimate, while the payload sets the flag to false, i.e. the reference always uses its local 3-bytes/token estimate |

Five of the nineteen are implemented, and the two that decide *when*
to compact (`max_token_limit`, `max_output_tokens`) are among them.
The remaining fourteen are the reference's *mechanism*, not its
*contract*: they need the cascade layer the clone does not have.

## 3. The clone

### 3.1 Two implementations, neither of them the reference's

| path | where | trigger | mechanism |
|---|---|---|---|
| generic summariser | `internal/compaction` + `cmd/wagyu/slash_compact.go` | the TUI's `/compact` | one extra model call that returns a summary; `m.session` is replaced by a single user message carrying it |
| PiG compaction | `internal/pigrt` (print path only) | PiG's own rule | `coding.Session` compaction, `agent.CompactionStartEvent` / `CompactionEndEvent` |

`m.compactor` — the hook that would let the TUI drive PiG's
compaction — **is never assigned** (`cmd/wagyu/slash_commands.go:867`
says so), so the TUI always takes the generic path and the PiG path
in the TUI is unreachable today. The PiG path is aligned to the same
two-row shape anyway, so that wiring it up later needs no UI work.

### 3.2 The clone's auto-compaction trigger was mis-wired 【一手】

PiG's rule is

```go
func ShouldCompact(contextTokens, contextWindow int, s CompactionSettings) bool {
    return contextTokens > contextWindow - s.ReserveTokens
}
```

The clone never sets `Capabilities` on the PiG models it builds
(`internal/piagentbridge`, `internal/agent/pig.go`), so
`ContextWindow` is **0**. With the clone's settings (reserve 16 384,
keep-recent 20 000, window constant 200 000 in
`internal/pigrt/runtime.go`), the rule degenerates to
`contextTokens > −16384` — true for **every** turn with a non-empty
context.

What actually happens today is that PiG then refuses the compaction
because the session is too small to cut
(`compaction.PrepareCompaction` → `findProjectedCutPoint(…, 20000)`
returns no cut point → `"Nothing to compact (session too small)"`),
so nothing visible happens. **A session that grows past 20 000 tokens
would compact on every single turn** — the zero context window is a
latent bug, not a benign default.

The reference has no such failure mode: its limit is the model's
declared `max_token_limit` (80 000 … 840 000), which is never zero.

**Fixed in this cycle.** Every model handle the clone builds now carries
`Capabilities.ContextWindow = 200 000` (`internal/pigrt`'s
`ContextWindow`, the number the threshold derivation already assumed).
PiG's rule becomes `contextTokens > 200 000 − 16 384`, so the compactor
fires when the context really is nearly full — and `Session.ContextUsage`
and the usage-based overflow check come back to life. The reference's
per-model limits are not knowable to the clone (its own catalogue's ids
carry no `input_token_limit`), so the canonical 200 K is the honest
stand-in; the report's 硬约束 says so.

### 3.3 What the clone's numbers mean

**Before this cycle** the clone's stats row reported its *session*
tokens: the transcript before, the summary after. The reference reports
a *context* pair (§2.2). The clone's numbers were therefore much smaller
(10 → 44 for a two-turn conversation) and were not comparable with the
reference's 13.1k → ~17.9k.

**Now** the clone computes the same *quantities* the reference does:

| field | the reference | the clone (now) |
|---|---|---|
| `context A` | the compaction request's model-reported prompt tokens | the backend's reported `PromptTokens` for the compaction request; without a report, the base context + the conversation estimate |
| `→ ~B` | the base context (instructions + tool declarations, 3 bytes/token) + the summary's size | the same: `compaction.ContextOverheadTokens` + the summary's size |
| `(C summary)` | the reported completion tokens | the backend's reported `CompletionTokens`; without a report, the summary's estimated size |

The clone's absolute numbers stay smaller than the reference's, because
the clone's system prompt is smaller (34 350 bytes against the
reference's 38 737 in the captured run) — the *shape* and the
*semantics* now match, the scale follows the two prompts.

---

## 4. What this cycle changed

### 4.1 `cmd/wagyu/slash_compact_rows.go` (new)

Two builders, both pure functions over the terminal width:

* `compactDividerRow(width)` — the rule: `#586E75`, width − 1
  columns, the label ` Conversation compacted ` centred inside it
  (floor left, ceil right);
* `compactStatsRow(width, steps, before, after, summary, took)` — the
  stats line: `#657B83`, centred on the full width **by display
  cells** (the line contains `·` and `→`, which are multi-byte), the
  four fields separated by `·`, the summary size hanging off the
  context group with a space.

`compactStatsText` and `compactTokenMagnitude` are the unstyled
cores; a negative argument means "not measured" and drops the field,
which is how the PiG path (whose end event carries only token counts)
reuses the same shape.

### 4.2 The two call sites

* `cmd/wagyu/slash_compact.go` — the generic path now emits
  divider + stats instead of the `Compacted N messages (-Y%)` bullet,
  and carries the elapsed time (`genericCompactDoneMsg.Took`).
* `cmd/wagyu/tui_turn_blocks.go` — `appendCompactResult` (the PiG
  path) emits the same two rows, without the three fields PiG's event
  does not carry.

### 4.3 The compaction request and its instruction 【一手】

`internal/compaction/prompt.go` now carries the reference's
continuation-summary instruction **byte for byte** (2409 bytes, captured
from a live agy 1.4.5 compaction request — `captures.md` §5), and the
request's shape follows the reference's:

* the system prompt stays the *normal* Antigravity instructions (the
  caller passes them; the reference sends no summariser system prompt of
  its own);
* the instruction is the **final user turn**, after the transcript —
  exactly where the reference puts it.

`internal/compaction/summarize.go` also records the model-reported usage
of that request (`Summary.InputTokens` / `Summary.OutputTokens`), which
is what the stats row now prints.

### 4.4 The post-compaction context 【一手】

`internal/compaction/resume.go` builds the reference's
`# Resuming from a compaction` block — the preamble, the ten most recent
user requests (the reference's `max_user_requests`), and the
`<summary>`-wrapped summary — and `cmd/wagyu/slash_compact_generic.go`
installs it in place of the compacted history. The reference's fourth
section (`# Conversation Logs`, pointing at the trajectory files it
wrote) is omitted: the clone keeps its sessions in memory.

The reference's ten-request cap and the byte-exact preamble come from
the reference's own trajectory log (`captures.md` §6).

### 4.5 The token divisor and the base-context estimate

`internal/compaction.EstimateTokens` now divides by **three**, the
divisor decompiled from agy 1.4.5's `EstimateTokenCounter`
(`captures.md` §4.4), instead of the cycle-237 reconstruction's four.
`compaction.ContextOverheadTokens(systemPrompt, declarationsJSON)`
implements the reference's base-context estimate (the instructions plus
the tool declarations at that divisor).

### 4.6 The per-model context window 【一手】

`internal/piagentbridge`'s three model builders (`bridge.Model`,
`BackendProvider.Model`, `Provider.Model`) carry
`Capabilities.ContextWindow`, and the value is now the reference's
**per-model** effective limit (§2.3) instead of a flat 200 K:

| model | window | reserve |
|---|---|---|
| `gemini-3.8-flash-{high,medium,low}` | 256 000 | 16 384 |
| `claude-sonnet-4-6`, `claude-opus-4-6-thinking` | 160 000 | 16 384 |
| `gpt-oss-120b-medium` | 80 000 | 8 192 |
| `custom-minimax-m3` / `minimax-m3` | 1 000 000 | 8 192 |

`internal/modelctx` holds the table (the captured catalogue of §2.3);
`internal/pigrt.ContextWindow` is now only the fallback for a model the
clone cannot name. `buildDefaultSettingsManager` derives PiG's
`ReserveTokens` from the same table — the reference's checkpoint
`max_output_tokens` — and the `CompactThreshold` baseline from the model
handle's window, so the derivation and the comparison PiG makes cannot
drift apart.

`internal/piagentbridge/context_window_test.go` pins the window on all
three builders; `internal/modelctx/modelctx_test.go` pins the table
against the capture.

### 4.7 Tests

* `cmd/wagyu/slash_compact_rows_test.go` (new) — the rule geometry at
  widths 100/120/160 (37/38, 47/48, 67/68 dashes; width − 1 columns
  total), the stats centring (column `(width − cells)/2 + 1`), the
  field layout, and the magnitude format.
* `cmd/wagyu/slash_compact_test.go`,
  `cmd/wagyu/slash_compact_command_test.go` — updated to the new
  rows; the old assertions pinned the reference's *absence* of a
  reduction percentage, which the new shape keeps true.
* `internal/modelctx/modelctx_test.go` (new) — the captured per-model
  window and reserve for every model the CLI can select, the
  case-insensitive lookup (the agw transport labels a model
  `MiniMax-M3`, the catalogue calls it `custom-minimax-m3`, the CLI
  spells it `minimax-m3`), the unknown-model fallback, and the
  invariant that the reserve always leaves room in the window.
* `internal/piagentbridge/context_window_test.go` — the per-model
  window on all three model builders, and the fallback for a model the
  clone cannot name.
* `internal/pigrt/compaction_test.go` — the per-model budget reaching
  PiG's settings: the reserve is the model's checkpoint output cap
  (8192 for `gpt-oss-120b-medium`, 16384 for the rest), and the
  `CompactThreshold` derivation is against the model's own window
  (0.5 of 256 000 is 128 000, not 100 000).
* `internal/compaction/prompt_test.go` — the instruction against the
  catalogue's `session_summary_prompt_override`
  (`TestSummarizePrompt_MatchesCheckpointOverride`), and that fixture
  re-derived from the live catalogue capture
  (`TestSummarizePrompt_MatchesLiveCatalogue`).

### 4.8 Verified live

The clone, built from this tree, running the same two-turn
conversation and `/compact` against the same model as the reference
capture:

```
\x1b[38;2;88;110;117m─── … 47 dashes … Conversation compacted … 48 dashes …───\x1b[m
\x1b[15;30H\x1b[38;2;101;123;131m\x1b[1K 2 steps summarized · context 10 → ~44 (44 summary) · took 4s\x1b[m
```

119 rule cells and 95 dashes — identical to the reference's — and the
stats text on the same column the reference's cursor move would have
used.

This capture predates §4.3–§4.5: its numbers are the old session-token
accounting (`10 → ~44`), not the reference's semantics. The *geometry*
it verified is unchanged by this cycle; the *numbers* now follow the
reference's quantities and would read differently.

---

## 5. Parity table

| behaviour | reference 1.4.5 | clone (before) | clone (after) | verdict |
|---|---|---|---|---|
| `/compact` exists | yes | yes | yes | match |
| `/compact` on an empty session | `Nothing to compact yet.` | same | same | match |
| progress label text | `Compacting conversation…` | `Compacting conversation…` | same | match |
| progress label placement | status row *and* spinner row, with `(0s · esc to cancel)` | spinner row only; `esc to cancel` on the status row | same as before | **deviation** (documented) |
| completion shape | centred rule + centred stats line | `⎿  Compacted N messages (-Y%)` bullet | centred rule + centred stats line | **aligned** |
| rule geometry | width − 1, label centred | — | width − 1, label centred | **aligned** |
| stats centring | `(width − cells)/2 + 1` | — | same | **aligned** |
| stats fields | steps, context pair, summary tokens, elapsed | messages, percentage | steps, context pair, summary tokens, elapsed | **aligned** |
| stats numbers | model-reported context; base context + summary; reported summary size | session transcript; summary | same quantities as the reference | **aligned** (§2.2, §3.3) |
| token estimate | 3 bytes per token | 4 bytes per token (cycle-237 reconstruction) | 3 bytes per token | **aligned** |
| reduction percentage | not printed | printed | not printed | **aligned** |
| stats row survives the final re-render | no (it disappears) | yes | yes | **deviation** (documented) |
| auto-compact trigger | cascade `max_token_limit` (80k…840k), threshold 50 000, overhead 0.15 | PiG `contextWindow − reserve` with a flat 200 K window — a number the reference uses for no model | PiG `contextWindow − reserve` with the reference's **per-model** `contextWindow` (256 000 / 160 000 / 80 000) and the checkpoint's `max_output_tokens` as the reserve | **partly aligned** (the *limit* and the *reserve* are the reference's; the trigger *function* — its sentinel comparison, step filter and strategy switch — is not; §2.4) |
| auto-compact in the TUI | (cascade) | none — the TUI runs `agent.Agent`, which never compacts | unchanged | **deviation** |
| auto-compact in print mode | (cascade) | wired; PiG refuses a session too small to compact ("Nothing to compact (session too small)"), which is what the degenerate window produced | wired; the trigger now fires at the reference's per-model limit | **deviation** (the mechanism is PiG's, not the cascade's; §2.6) |
| what is kept | `moving_window_size: 1`, `max_user_requests: 10` | keep-recent 20 000 tokens | unchanged | **deviation** |
| summary prompt | continuation summary, six sections, `<summary>` framing, sent as the final user turn | cycle-237 reconstruction ("3-6 bullet points") in the system prompt; PiG's own prompt on the PiG path | the reference's instruction, byte for byte, as the final user turn | **aligned** |
| post-compaction context | `# Resuming from a compaction` block: preamble + the ten most recent user requests + the `<summary>` | the bare summary as one user message | the same block (the reference's `# Conversation Logs` section omitted) | **aligned** |
| summary cache | background fill for deferred conversations | none | unchanged | **deviation** |
| compaction bookkeeping | `compacted_at_step_indices`, checkpoint entries | none | unchanged | **deviation** |

---

## 6. 硬约束 (hard constraints — what is *not* implemented)

* The reference's **auto**-compaction **mechanism** is **not** implemented
  in the clone. The clone has no cascade layer, no checkpoint strategy,
  no `token_threshold` / overhead policy, no summary cache and no
  `compacted_at_step_indices` bookkeeping. What *is* implemented is the
  context budget PiG compares against: the reference's per-model
  effective limit and the checkpoint's output reserve (§2.4, §4.6).
  Every row in §5 that says "deviation" on the auto-compact side is
  still open.
* The clone's `/compact` still runs on its **own** generic path (one
  extra model call), not on PiG's compaction — the `m.compactor` hook is
  still unassigned. The *instruction* it sends is now the reference's
  verbatim one, but the surrounding machinery (PiG's cut-point search,
  the keep-recent window, the compaction events) is still the clone's.
* The clone's **absolute** numbers are not the reference's. The
  semantics now match (§2.2), but the clone's base context is smaller
  than the reference's (a 34 350-byte prompt against 38 737 in the
  captured run). The context window it advertises to PiG *is* the
  reference's per-model limit now (§4.6), but the reference's own
  `input_token_limit` is the server's catalogue value, and the clone
  reads it from a capture rather than from the server.
* The PiG path in the TUI (`appendCompactResult`) is **unreachable
  today** (`m.compactor` is never assigned). It was aligned to the
  reference's shape anyway; that is preparation, not a behaviour
  change.
* The reference's **auto**-compaction **trigger function** is not
  reproduced. What is reproduced is its *limit* and its *reserve*
  (§2.4): the clone compacts when the context would exceed
  `min(maxTokens, max_token_limit)` with the checkpoint's
  `max_output_tokens` held back, which is the comparison the reference's
  overflow error describes. What is missing is
  `checkpoint.shouldCheckpoint`'s own machinery — its sentinel
  comparison, its step-type filter (refusing to checkpoint right after
  a user-input or model-response step), its strategy switch
  (`SINGLE_PROMPT` / `SAME_MODEL` / `UNSPECIFIED`) and its
  `use_model_reported_token_count` handling. The clone therefore
  compacts *at the reference's limit* but not *at the reference's
  moments*.
* The reference's `token_threshold` (50 000) and `max_overhead_ratio`
  (0.15) are **not** implemented: the proto fields exist in the binary
  (their descriptor strings are at 0x8e414e2 and 0x8ef1dd1) but their
  consumer was not located, so the clone has no notion of a
  soft threshold below the hard limit.
* The reference's **automatic** compaction's instruction *tail* is
  unknown. The instruction *body* is now known — it is the same
  2289 bytes the clone sends (§2.5) — but the binary has no
  `AUTO COMPACTION` literal, so the on-demand path's 120-byte
  `ON-DEMAND COMPACTION:` suffix has no observed automatic
  counterpart.
* The **print path's automatic compaction** runs PiG's contract, not
  the reference's: PiG sends its own summariser system prompt,
  re-serializes the transcript into a `<conversation>` block, uses its
  own `SUMMARIZATION_PROMPT`, and restores the history as one summary
  message plus the kept recent entries (§2.6). The divergence is
  structural — PiG's compaction is in an `internal/` package the clone
  cannot import, and its `customInstructions` hook *appends* to its own
  prompt rather than replacing it — so closing it needs an upstream
  change. The manual `/compact` path is unaffected: it runs
  `internal/compaction`, which implements the reference's contract.

## 7. 存疑 (open questions)

* **Which usage fields are `A` and `C` exactly?** The reference's
  `CompactionTokens` reads two `int` fields of the usage struct
  (offsets `+0x10` and `+0x30` for the first value, `+0x28` with a
  `+0x20 − +0x18` fallback for the second) and sums the first two. The
  *identification* — the model-reported context and the summary's
  completion tokens — is confirmed by the fit against the captures
  (the third number matches the summary's size in every capture) and by
  the reference's own `use_model_reported_token_count` flag, but the
  proto field names behind those offsets were not mapped.
* **What exactly is `ContextOverheadTokens` summing?** It walks a list
  of per-item token counts and keeps a separate bucket for the items
  whose type is 7, with a fallback that subtracts that bucket from a
  total. The clone approximates it as (system prompt + tool
  declarations) / 3, which fits the captures to within 0.9 % — but the
  reference's item list (prompt sections? tool declarations? both?)
  was not identified.
* **What is the field at `+0x10` of `shouldCheckpoint`'s comparison
  operand?** The final test is `cmp x0, x4; cset x0, ne` — a *sentinel*
  comparison, not a threshold. `x3` is the fourth return value of
  `TrajectoryToChatMessages` (a pointer into the conversion result) and
  `x0` is the last step's token count, zeroed when the model reported
  its own. The operand at `+0x10` was not identified; the conversion
  result's 0x18 pointer holds a struct whose `+0x38` field points at a
  freshly allocated 64-byte object carrying the step's text/image
  counts. Reading the field's meaning needs the type of that struct,
  which the decompilation does not name.
* **Where are `token_threshold` and `max_overhead_ratio` read?** Both
  are in every payload (50 000 and 0.15) and both have proto descriptor
  strings in the binary (0x8e414e2 and 0x8ef1dd1), but their consumer
  was not located. If `token_threshold` is a *soft* trigger below the
  hard limit, the clone's single hard limit is not the whole rule.
* **Is the checkpointer experiment enabled for the CLI?** The
  payload says `"enabled": true` *inside* the experiment, and the
  catalogue ships the payload per model — but the experiment assignment
  itself is server-side (Unleash) and was not observed. The
  reference's auto-compaction may therefore never fire for this
  account.
* **Does the automatic path append a tail to the instruction?** The
  catalogue's `session_summary_prompt_override` is the 2289-byte body
  with no tail (§2.5), while the on-demand turn is the body plus 120
  bytes. If the checkpoint appends a tail of its own, the override is
  the *base* rather than the whole instruction — unobserved.
* **Does the reference's `# Conversation Logs` section belong in the
  clone?** It points at the trajectory files the reference wrote; the
  clone writes none, so the section is omitted. Whether the reference
  would print it against a store the CLI does not own is unknown.
* **What does the reference's aborted / failed compaction look like?**
  Not captured: the compaction succeeded in every run.
* **Where exactly does the reference put the stats row when the
  transcript is re-rendered?** It disappears; whether that is a bug
  in the reference or a deliberate "transient" row is unknown.

## 8. limitations

* The auto-compaction side is **string-table and decompiled** evidence.
  No live auto-compaction was observed (it would need a session of
  50 000+ tokens, or an experiment assignment that could not be
  obtained).
* The decompilation is of a **Mach-O arm64** build imported without
  analyzers (`-noanalysis`), so there are no cross-references: the
  functions were found by name in the pclntab seed list, and the
  strings they reference are read out of the disassembly rather than
  resolved by Ghidra. The Go register ABI makes the render functions
  hard to read (the three `fmtTokens` calls share one stack slot in
  Ghidra's output), so the *order* of the three format arguments was
  settled by the captures, not by the decompilation alone.
* The live captures cover the **TUI** path only. The reference's
  `-p` (print) mode was not exercised for `/compact`.
* The reference's `/compact <message>` form (summarise, then answer
  the message) was not captured; the clone's behaviour for it was
  left as it was.
* The clone's live capture used one model (Gemini 3.8 Flash,
  medium). The numbers depend on the conversation, not on the model,
  but the *shape* was only checked at width 120 on the clone side
  (the geometry tests cover 100/120/160).
* The reference's numbers are re-generated per run (the summary
  token count varies: 239 / 253 / 271 / 467 across captures), so no
  capture pins them.
* The clone's new numbers were verified live **once**
  (`captures.md` §7): the summary size came out at 273 tokens against the
  reference's 271 — the tell that the clone now sends the reference's
  instruction. One run is one run: the row's geometry was checked at
  three widths, but these numbers are not pinned.
* agy is now **1.4.5** while the clone still emulates **1.4.4**
  (`internal/cli.ReferenceVersion`); the drift is being handled by a
  separate change and is not touched here.
