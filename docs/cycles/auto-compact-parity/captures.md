# Auto-compact parity — captures

Every byte sequence below was read out of a **pty capture of the real
`agy` binary** (`~/.local/bin/agy`, **1.4.5**, SHA-256 pinned in
`internal/referencedrift/reference.lock.json`) or out of that binary's
own string table. Nothing here is reconstructed from the clone.

The capture harness is the same one used by the bash-mode cycle: a
`pty` on macOS, the binary run under a fixed terminal size, prompts
typed into the TUI, the raw byte stream of the pty master saved
verbatim.

```
COLS=<width> PROMPTS="<prompt>|<slash command>" WAIT=<s> python3 prompts.py <out>.raw
```

The raw `.raw` files stay out of the repository (they are pty dumps of
a live session); this file records the bytes that matter, with the
offsets needed to find them again.

---

## 1. The manual `/compact` completion (live, agy 1.4.5)

### 1.1 The two rows, at width 120

`live_compact.raw` — a two-turn conversation ("say hi in three words")
followed by `/compact`.

**Row 1 — the rule.** Painted as one styled span, `#586E75`
(RGB 88,110,117), starting at column 1 of the transcript area:

```
\x1b[38;2;88;110;117m ───────… 47 dashes ─────── Conversation compacted ─────── … 48 dashes ─────── \x1b[m
```

Measured, not eyeballed:

| quantity | value |
|---|---|
| rule width (runes, dashes + label + the two spaces) | **119** = width − 1 |
| dashes before the label | **47** (floor) |
| dashes after the label | **48** (ceil) |
| label | `" Conversation compacted "` (22 + 2 spaces) |

**Row 2 — the stats line.** Painted with an explicit cursor move to
row 17, column 27, in `#657B83` (RGB 101,123,131):

```
\x1b[17;27H\x1b[38;2;101;123;131m3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s\x1b[m
```

The text is 67 display cells, and column 27 = `(120 − 67) / 2 + 1` —
i.e. the reference centres the line on the **full** terminal width with
a cursor move, not with padding.

### 1.2 The same completion at three widths

Three captures, identical conversation, different terminal widths:

| width | rule width | dashes before/after | stats cursor column | stats text |
|---|---|---|---|---|
| 100 | 99 | 37 / 38 | 17 | `3 steps summarized · context 13.1k → ~17.9k (253 summary) · took 7s` |
| 120 | 119 | 47 / 48 | 27 | `3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s` |
| 160 | 159 | 67 / 68 | 47 | `3 steps summarized · context 13.1k → ~17.9k (239 summary) · took 8s` |

So the rule is always **width − 1** columns and the stats line always
starts at `(width − cells) / 2 + 1`. The *summary* token count varies
between runs (239 / 253 / 271) — it is the size of the model's own
summary, which is not deterministic.

### 1.3 A longer conversation

`live_compact3.raw` — a seven-step conversation:

```
\x1b[35;27H\x1b[38;2;101;123;131m7 steps summarized · context 16.7k → ~18.1k (467 summary) · took 7s\x1b[m
```

The step count grows with the conversation (3 → 7) and the summary
grows with it (271 → 467 tokens). The context pair grows with it too
(13.1k → 16.7k before; 17.9k → 18.1k after).

**Note the direction:** in every capture the *second* number is
**larger** than the first, so the pair is not a before/after of one
quantity. See the deliverable's 存疑 section.

### 1.4 The progress label

While the compaction runs, the reference paints the label in **two**
places — the status row and the spinner row — with the same text:

```
\x1b[2A \x1b[38;2;101;123;131mCompacting conversation… (0s · esc to\x1b[38;2;38;139;210m canc\x1b[38;2;101;123;131mel)\x1b[m
```

* the label body is `#657B83`;
* the word `cancel` is `#268BD2` (RGB 38,139,210) — the reference
  splits it across a chunk boundary (`canc` blue, `el` grey), which is
  an artefact of how the text is streamed, not of the styling;
* the spinner row paints the same string after the braille spinner:
  `⣾  Compacting conversation… (0s · esc to cancel)`.

### 1.5 The empty-session case

`Nothing to compact yet.` is present in the 1.4.5 string table
(offset 54530878 in the binary) — the reference refuses to compact a
session that has nothing to compact. The clone already prints the same
sentence.

### 1.6 A quirk: the stats row does not survive the final re-render

In every capture the rule row is still on screen at the end of the
turn, but the **stats row is gone** — the reference re-renders the
transcript from its model and the stats line is not part of it. The
clone keeps the row. This is recorded as a deliberate deviation.

---

## 2. The reference's auto-compact model (strings, agy 1.4.5)

The auto-compaction path is not reachable from the CLI without an
experiment assignment, so it is documented here from the binary's own
string table. All offsets are into `~/.local/bin/agy`.

### 2.1 The experiment payload

The cascade config carries a per-model experiment payload under the
key `CASCADE_USE_EXPERIMENT_CHECKPOINTER` (the key itself is in the
binary at offset 54530579 area; the payloads start at 158362343):

```json
"CASCADE_USE_EXPERIMENT_CHECKPOINTER": "{
    \"strategy\": \"CHECKPOINT_STRATEGY_SINGLE_PROMPT\",
    \"max_token_limit\": \"128000\",
    \"token_threshold\": \"50000\",
    \"max_overhead_ratio\": \"0.15\",
    \"moving_window_size\": \"1\",
    \"enabled\": true,
    \"max_output_tokens\": \"16384\",
    \"checkpoint_model\": \"MODEL_PLACEHOLDER_M50\",
    \"use_last_planner_model\": false,
    \"is_sync\": false,
    \"max_user_requests\": 10,
    \"include_last_user_message\": false,
    \"include_conversation_log\": true,
    \"include_running_task_snapshots\": true,
    \"include_subagent_snapshots\": true,
    \"include_artifact_snapshots\": true,
    \"retry_config\": {
        \"max_retries\": 0,
        \"initial_sleep_duration_ms\": 1000,
        \"exponential_multiplier\": 2,
        \"include_error_feedback\": false
    },
    \"session_summary_prompt_override\": \"\",
    \"use_model_reported_token_count\": false
}"
```

Per-model variants (`max_token_limit`, `strategy`):

| model | max_token_limit | strategy |
|---|---|---|
| `MODEL_GOOGLE_GEMINI_2_5_FLASH` | 128000 | SINGLE_PROMPT |
| `MODEL_GOOGLE_GEMINI_2_5_PRO` | 128000 | SINGLE_PROMPT |
| `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` | 80000 | UNSPECIFIED |
| `MODEL_PLACEHOLDER_M26` | 160000 | UNSPECIFIED |
| `MODEL_PLACEHOLDER_M124` | 256000 | SAME_MODEL |
| `MODEL_PLACEHOLDER_M132` | 256000 | SAME_MODEL |
| `MODEL_PLACEHOLDER_M261` / `M269` / `M289` / `M291` / `M400` / `M408` | 256000 | SAME_MODEL |
| `MODEL_PLACEHOLDER_M125` / `M149` | 128000 | SINGLE_PROMPT |
| (the 1M-context Claude entry) | 840000 | — |

`token_threshold` is 50000 and `max_overhead_ratio` 0.15 in every
variant.

### 2.2 The per-model table, from the server's catalogue 【一手】

The binary's `model_profiles` map names the payloads by *internal*
model name (`MODEL_PLACEHOLDER_M<n>`), which no public id maps to. The
server's catalogue resolves it: every entry of `fetchAvailableModels`
carries the model's `maxTokens`, its `maxOutputTokens`, its internal
`model` name **and its own** `CASCADE_USE_EXPERIMENT_CHECKPOINTER`
payload.

Captured live at agy 1.4.4 —
`docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json`:

```
"gemini-3.8-flash-medium": {
  "apiProvider": "API_PROVIDER_GOOGLE_GEMINI",
  "displayName": "Gemini 3.8 Flash (Medium)",
  "maxOutputTokens": 65536,
  "maxTokens": 1048576,
  "minThinkingBudget": 32,
  "model": "MODEL_PLACEHOLDER_M319",
  "modelExperiments": {
    "experiments": {
      "CASCADE_USE_EXPERIMENT_CHECKPOINTER": {
        "stringValue": "{\n    \"strategy\": \"CHECKPOINT_STRATEGY_SAME_MODEL\",\n    \"max_token_limit\": \"256000\",\n    \"token_threshold\": \"50000\",\n    \"max_overhead_ratio\": \"0.15\",\n    \"moving_window_size\": \"1\",\n    \"enabled\": true,\n    \"max_output_tokens\": \"16384\",\n    \"checkpoint_model\": \"MODEL_PLACEHOLDER_M50\",\n    \"use_last_planner_model\": true,\n    \"is_sync\": true,\n    \"max_user_requests\": 10,\n    \"include_last_user_message\": true,\n    \"include_conversation_log\": false,\n    \"include_running_task_snapshots\": true,\n    \"include_subagent_snapshots\": true,\n    \"include_artifact_snapshots\": true,\n    \"retry_config\": {\n        \"max_retries\": 4,\n        \"initial_sleep_duration_ms\": 1000,\n        \"exponential_multiplier\": 2,\n        \"include_error_feedback\": false\n    },\n    \"session_summary_prompt_override\": \"You have been working on …"
```

The whole catalogue, reduced to the two numbers the clone needs:

| catalogue id | internal model | `maxTokens` | `maxOutputTokens` | checkpoint `max_token_limit` | checkpoint `max_output_tokens` | strategy |
|---|---|---|---|---|---|---|
| `chat_20706` | `MODEL_CHAT_20706` | 16 384 | — | — | — | — |
| `chat_23310` | `MODEL_CHAT_23310` | 32 768 | — | — | — | — |
| `claude-opus-4-6-thinking` | `MODEL_PLACEHOLDER_M26` | 250 000 | 64 000 | 160 000 | 16 384 | UNSPECIFIED |
| `claude-sonnet-4-6` | `MODEL_PLACEHOLDER_M35` | 250 000 | 64 000 | 160 000 | 16 384 | UNSPECIFIED |
| `custom-minimax-m3` | `MODEL_PLACEHOLDER_M488` | 1 000 000 | 8 192 | — | — | — |
| `gemini-2.5-flash` | `MODEL_GOOGLE_GEMINI_2_5_FLASH` | 1 048 576 | 65 535 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-2.5-flash-lite` | `MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE` | 1 048 576 | 65 535 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-2.5-flash-thinking` | `MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING` | 1 048 576 | 65 535 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-2.5-pro` | `MODEL_GOOGLE_GEMINI_2_5_PRO` | 1 048 576 | 65 535 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-3-flash` | `MODEL_PLACEHOLDER_M18` | 1 048 576 | 65 536 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-3-flash-agent` | `MODEL_PLACEHOLDER_M84` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-3.1-flash-image` | `MODEL_PLACEHOLDER_M21` | 65 536 | 65 536 | — | — | — |
| `gemini-3.1-flash-lite` | `MODEL_PLACEHOLDER_M50` | 1 048 576 | 65 535 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-3.5-flash-extra-low` | `MODEL_PLACEHOLDER_M187` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-3.5-flash-lite` | `MODEL_PLACEHOLDER_M198` | 1 048 576 | 65 535 | 128 000 | 16 384 | SINGLE_PROMPT |
| `gemini-3.5-flash-low` | `MODEL_PLACEHOLDER_M20` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-3.8-flash-high` | `MODEL_PLACEHOLDER_M318` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-3.8-flash-low` | `MODEL_PLACEHOLDER_M320` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-3.8-flash-medium` | `MODEL_PLACEHOLDER_M319` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-3.8-flash-tiered` | `MODEL_PLACEHOLDER_M322` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gemini-4-argon-disabled` | `MODEL_PLACEHOLDER_M421` | 1 048 576 | 65 536 | 256 000 | 16 384 | SAME_MODEL |
| `gpt-oss-120b-medium` | `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` | 131 072 | 32 768 | **80 000** | **8 192** | UNSPECIFIED |
| `tab_flash_lite_preview` | `MODEL_PLACEHOLDER_M19` | 16 384 | 4 096 | — | — | — |
| `tab_jump_flash_lite_preview` | `MODEL_PLACEHOLDER_M28` | 16 384 | 4 096 | — | — | — |

`token_threshold` is 50000, `max_overhead_ratio` 0.15 and
`moving_window_size` 1 in every payload that has one.

**The effective limit** is the smaller of `maxTokens` and the checkpoint's
`max_token_limit` — see deliverable §2.4 for the decompiled
`min()`.

### 2.3 The proto

The cascade config carries a `checkpoint_config` message. Its
descriptor (offset 62542254 area) names the fields:

```
max_overhead_ratio, moving_window_size, max_token_limit,
max_output_tokens, checkpoint_model, max_user_requests,
max_user_request_bytes, max_subagent_snapshots, retry_config,
enable_fallback, strategy, session_summary_prompt_override,
use_last_planner_model, include_artifact_snapshots,
include_subagent_snapshots, include_running_task_snapshots,
include_last_user_message, include_conversation_log,
token_threshold, is_sync, enabled
```

and the checkpoint proto itself:

```
CortexStepCheckpoint: accumulated_sections, summary_prompt_content_id,
summary_content_id, conversation_title, edited_file_map,
included_step_indices, memory_summary, conversation_log_uris
```

`compacted_at_step_indices` (offset 59227372) is how the reference
records *which* steps a compaction folded away.

### 2.4 The overflow error — the reference's own token accounting

Verbatim from the binary (offset 56068528):

```
the most recent step (%s) could not fit in the available context limit:
  Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))
    - Base context: ~%d tokens (instructions: ~%d, tool definitions: ~%d)
    - Most recent step: ~%d tokens (text: %d, image: %d)

Consider reducing the size of the system instructions or tool definitions, or adjusting tool implementations to return smaller outputs
```

This is the vocabulary the stats row lives in: the reference accounts
for a context as **instructions + tool definitions + the step**. It is
the strongest available hint that the "13.1k" the stats row reports is
the *base context* (the clone's own system prompt is 34 350 bytes =
~8.6k tokens, and the twelve tool declarations are of the same
order — 13.1k is their sum).

### 2.5 The summary prompt

The continuation-summary prompt is in the binary verbatim (offset
56591728):

```
You have been working on the task described above but have not yet completed
it. Write a continuation summary that will allow you (or another instance of
yourself) to resume work efficiently in a future context window where the
full conversation history will NOT be available—only this summary.

This summary is all that will be available to you going forward in the future
context window. Do not call any tools, simply just provide the summary based on
the information available in the current context window.

Your summary must be structured, concise, and actionable. Optimize for enabling immediate resumption with zero redundant work.

Include the following sections:

1. **Task Overview**
   ...
```

(six sections; the full text is reproduced in the deliverable).

### 2.6 The summary cache

A background filler for conversations that were not compacted yet:

```
summaryCache
summaryCacheFillEffort set to %s
summaryCacheFill: filling summary cache for %d deferred conversations
summaryCacheFill: done filling summary cache for %d conversations
summaryCacheFill: canceled after %d/%d conversations
```

---

### 2.7 The trigger, in the instruction stream 【反编译】

Decompiled with Ghidra 12.1.4 from the Mach-O arm64 build of agy 1.4.5
(`-noanalysis` import, so no cross-references — the functions were found
by name in the pclntab seed list and the numbers below are read out of
the disassembly, not trusted from Ghidra's rendering).

**The limit — `chatconverters.(*TrajectoryChatConverter).TrajectoryToChatMessages`
(0x10150abe0).** Ghidra renders it as:

```c
uVar5 = modelConfig[0x20] ? modelConfig[0x20][0x14] : 0;
iVar6 = config[0x10];
if (iVar6 != 0 && config->enabled != nil && *config->enabled) {
    dVar1 = config->max_token_limit;               // +0x10
    if (dVar1 != 0 && dVar1 <= uVar5) uVar5 = dVar1;
}
```

i.e. `limit = min(the model's own limit, checkpoint_config.max_token_limit)`,
with the checkpoint's limit ignored when the checkpoint is disabled or
its limit is zero.

**The clamps — `cortex/utils.GetCheckpointConfig` (0x1013072c0).**

```
1013073c8: ldr  w3, [sp, #0x54]        ; the configured max_token_limit
1013073cc: mov  x4, #0xe800
1013073d0: movk x4, #0x3, lsl #16      ; x4 = 0x3e800 = 256000
1013073d4: cmp  w3, w4
1013073d8: mov  x4, #0xe800
1013073dc: movk x4, #0x3, lsl #16
1013073e0: csel x3, x3, x4, lo         ; x3 = (w3 < 256000) ? w3 : 256000
1013073e4: str  w3, [sp, #0x58]
```

```
1013073e8: ldrsw x4, [sp, #0x64]        ; the configured max_output_tokens
1013073ec: cmp  w4, #0x0
1013073f0: cmp  w4, #0x4, lsl #12      ; = 0x4000 = 16384
1013073f4: orr  x5, xzr, #0x4000
1013073f8: csel x6, x4, x5, lo         ; x6 = (w4 < 16384) ? w4 : 16384
```

So the experiment's numbers are clamped to **≤ 256 000** tokens of
context and **≤ 16 384** tokens of output.

**The headroom.** The same function's model-family branch:

```
1013072ec: ldrsw x3, [x2, #0x30]        ; the model's family
1013072f0: cmp  w3, #0x3
1013072f8: cmp  w3, #0x18
101307300: cmp  w3, #0x1a
101307308: ldrsw x3, [x2, #0x14]        ; the model's input limit
10130730c: ldrsw x4, [x2, #0x70]        ; the model's max output
101307318: mov  x6, #0x9c40             ; = 40000
10130731c: cmp  x5, x6
101307324: csel x5, x5, x6, gt          ; x5 = max(model max output, 40000)
101307328: cmp  x5, x3
10130732c: b.lt  …                      ; else 0
```

⇒ `headroom = max(0, model.input_limit − max(40000, model.max_output))`.
The reference always keeps **at least 40 000 tokens** free.

**The one-model special case.** The same function opens with

```
1013072e4: cmp  w3, #0x143             ; model id 323
1013072e8: b.eq 0x1013076d4
```

and the branch builds a config with `enabled = true` and
`max_token_limit = 1000000`.

**The decision — `checkpoint.shouldCheckpoint` (0x101a81d10).** The tail:

```
101a81ef0: ldr  x0, [sp, #0x48]        ; the last step's token count
101a81ef4: b    0x101a81f04
101a81ef8: ldr  x0, [sp, #0x48]
101a81efc: b    0x101a81f04
101a81f00: mov  x0, xzr                ; no step, or the model reported its own count
101a81f04: ldr  w4, [x3, #0x10]
101a81f08: cmp  x0, x4
101a81f0c: cset x0, ne                 ; ← the boolean
```

The comparison is **`!=`**, not a threshold — so the operand at `+0x10`
is a sentinel, not the limit. `x3` is the fourth return value of
`TrajectoryToChatMessages`, a pointer into the conversion result. The
field was not identified; see deliverable §7.

### 2.8 The checkpoint's instruction 【一手】

The catalogue's per-model checkpoint payload carries a
`session_summary_prompt_override`. For the models whose strategy is
`CHECKPOINT_STRATEGY_SAME_MODEL` it is **2289 bytes**, and it is
**byte-identical** to the instruction the clone sends for the manual
`/compact` (`internal/compaction.SummarizePrompt`).

From the committed live capture
(`docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json`),
`gemini-3.8-flash-medium`:

```
CASCADE_USE_EXPERIMENT_CHECKPOINTER.stringValue =
{
    "strategy": "CHECKPOINT_STRATEGY_SAME_MODEL",
    ...
    "session_summary_prompt_override": "You have been working on the task described above but have not yet completed\nit. Write a continuation summary that will allow you (or another instance of\nyourself) to resume work efficiently in a future context window where the\nfull conversation history will NOT be available—only this summary.\n\nThis summary is all that will be available to you going forward in the future\ncontext window. Do not call any tools, simply just provide the summary based on\nthe information available in the current context window.\n\nYour summary must be structured, concise, and actionable. Optimize for enabling immediate resumption with zero redundant work.\n\nInclude the following sections:\n\n1. **Task Overview**\n ...",
    ...
}
```

Which models ship it:

| catalogue id | strategy | `session_summary_prompt_override` |
|---|---|---|
| `gemini-3.8-flash-{high,medium,low}` | SAME_MODEL | 2289 bytes |
| `gemini-3.5-flash-low`, `gemini-3.5-flash-extra-low`, `gemini-3.8-flash-tiered`, `gemini-3-flash-agent`, `gemini-4-argon-disabled` | SAME_MODEL | 2289 bytes |
| `gemini-2.5-{flash,flash-lite,flash-thinking,pro}`, `gemini-3-flash`, `gemini-3.1-flash-lite`, `gemini-3.5-flash-lite` | SINGLE_PROMPT | empty |
| `claude-sonnet-4-6`, `claude-opus-4-6-thinking`, `gpt-oss-120b-medium` | UNSPECIFIED | empty |

The clone keeps the payload's bytes as
`internal/compaction/testdata/session_summary_prompt_override_1.4.4.txt`
and pins the identity in `internal/compaction/prompt_test.go`
(`TestSummarizePrompt_MatchesCheckpointOverride`, and
`TestSummarizePrompt_MatchesLiveCatalogue` re-derives it from the
catalogue capture).

The on-demand turn is that body plus a 120-byte tail
(`ON-DEMAND COMPACTION: …`), 2409 bytes in total — see §5. The
automatic path's tail (if any) was not observed.

## 3. The clone's rows (live, the same conversation)

`clone_compact_final.raw` — the clone, built from this cycle's tree,
running the same two-turn conversation and `/compact` against the same
model (Gemini 3.8 Flash, medium).

**Row 1 — the rule.** Byte-identical geometry to the reference:

```
\x1b[38;2;88;110;117m ───────… 47 dashes ─────── Conversation compacted ─────── … 48 dashes ─────── \x1b[m
```

119 cells, 95 dashes, 47 before / 48 after — the same as §1.1.

**Row 2 — the stats line.** Painted at row 15, column 30:

```
\x1b[15;30H\x1b[38;2;101;123;131m\x1b[1K 2 steps summarized · context 10 → ~44 (44 summary) · took 4s\x1b[m
```

The clone pads with spaces instead of moving the cursor; the terminal
renderer skips the padding with its own cursor move (`\x1b[1K` + the
cursor jump), so the text lands on the same column the reference's
cursor move would have used: `(120 − 61) / 2 + 1 = 30`.

The numbers differ from the reference's, and the reason is
accounting, not formatting: the clone's "before" is the *session
transcript* (10 tokens for this conversation) and its "after" is the
*summary* (44 tokens). The reference's "before" is the base context
(instructions + tool definitions ≈ 13.1k) — see §2.4.

### 3.1 The clone's progress label

```
\x1b[38;2;38;139;210;1m⣾  \x1b[38;2;88;110;117;22mCompacting conversation…\x1b[38;5;235m\x1b[K\x1b[m
```

The label text matches the reference's, but the clone does not append
the elapsed time and the `esc to cancel` hint to it — the clone's
status row carries `esc to cancel` separately, in `#657B83`.

---

## 4. The stats row's two numbers, read out of the binary 【反编译】

The 1.4.5 binary was imported into Ghidra (160 664 pclntab seeds applied,
160 645 functions created, 0 failures) and the rendering path was
decompiled. The functions that matter are all in the CLI's render package:

| function | address | what it answers |
|---|---|---|
| `cli/render/render.compactionStatsParts` | `0x1027b9a90` | how the stats row is assembled |
| `cli/render/render.CompactionTokens` | `0x1027ba1c0` | the two numbers the row prints |
| `cli/render/render.ContextOverheadTokens` | `0x1027ba250` | the base-context estimate |
| `cli/render/render.fmtTokens` | `0x1027eb6a0` | the `13.1k` formatting |
| `cli/render/render.renderCompactionStats` | `0x1027b9fb0` | the centring (`0.5` = centred) |
| `jetski/tokenizer.(*EstimateTokenCounter).GetTokenCount` | `0x100e48670` | the token estimator |
| `jetski/tokenizer.createDefaultTokenCounter` | `0x100e48920` | the estimator's divisor |

### 4.1 The format string and the number formatting

`compactionStatsParts` ends with

```c
auVar15 = fmt_Sprintf(&UNK_10345e238, 0x1f, puVar2 + 0x24, 3, 3);
```

`0x1f` = 31 bytes — exactly `context %s → ~%s (%s summary)` (the arrow is
three bytes in UTF-8) — with three string arguments.

`fmtTokens` is the formatter:

```c
if (param_1 < 1000000) {
  if (999 < param_1) {
    ... / 1000.0 ... fmt(&UNK_103288c93 /* "%.1fk" */)
  } else {
    ... fmt(&UNK_10327487e /* "%d" */)
  }
} else {
  ... / 1000000.0 ... fmt(&UNK_103288d7e /* "%.1fM" */)
}
```

So `13141` → `13.1k`, `984` → `984`, `2 400 000` → `2.4M`.

### 4.2 The two numbers

`CompactionTokens(step)` walks the step's usage entries, keeps the last
non-nil one and returns two values:

```c
iVar1 = *(int *)(iVar3 + 0x28);
if (*(uint *)(iVar3 + 0x20) < *(uint *)(iVar3 + 0x18)) {
  iVar1 = *(uint *)(iVar3 + 0x18) - *(uint *)(iVar3 + 0x20);
}
auVar4._8_8_ = iVar1;                              /* the summary's size */
auVar4._0_8_ = *(int *)(iVar3 + 0x10) + *(int *)(iVar3 + 0x30);   /* the context */
```

and `compactionStatsParts` prints them like this:

```c
puVar2[0x11] = iVar7;                 /* the summary's size */
puVar2[0x10] = puVar2[0x2e] + iVar7; /* the base context + the summary's size */
... fmtTokens(...) x3 ...
fmt_Sprintf("context %s → ~%s (%s summary)", ...)
```

`puVar2[0x2e]` is the caller's *base context* argument — the value
`render.ContextOverheadTokens` computes from the request's prompt
sections and tool declarations.

So the row is:

| field | meaning | scale |
|---|---|---|
| `context A` | the compaction request's **model-reported** context | the model's real tokens |
| `→ ~B` | the **base context** estimate + the summary's size | the reference's 3-bytes-per-token estimate |
| `(C summary)` | the **summary's own** size (the reported completion tokens) | the model's real tokens |

`A` and `B` are on *different scales*, which is why the second number is
the larger one in every capture: the reference's own
`CheckpointConfig` sets `use_model_reported_token_count`, so the first
number is the model's tokeniser's count while the second is the
reference's byte-based estimate of the base context.

### 4.3 The step count

`compactionStatsParts` reads the compaction step's usage twice:

```c
dVar8  = *(dword *)(iVar9 + 0x10);
dVar11 = *(dword *)(iVar9 + 0x14);
...
*(dword *)((int)register0x00000008 + -0xe4) = dVar11 - dVar8;
```

— the compacted step range's width, printed as `%d steps summarized`
(16-byte format string) with a singular variant for one step.

### 4.4 The token divisor is 3.0 bytes per token

`EstimateTokenCounter.GetTokenCount`:

```c
return ZEXT824((uint)(int)(double)(int)((double)param_3 / *param_1));
```

— `int(float64(len) / divisor)` — and `createDefaultTokenCounter` stores
the divisor:

```c
*puVar1 = 0x4008000000000000;   /* IEEE-754 double 3.0 */
```

So the reference estimates three bytes per token everywhere (its
`/context` panel, its compaction stats, its base-context estimate).

### 4.5 The trigger

`gemini_coder/framework/handlers/checkpoint/checkpoint.shouldCheckpoint`
(`0x101a81d10`) gates on the config's `enabled` flag first, refuses to
checkpoint right after a user-input or model-response step, and then
compares the trajectory's token count against the config's threshold.
The full rule is not decoded in this cycle — see the report's 存疑.

## 5. The reference's summary instruction, captured byte for byte 【一手】

The `cmd/agy` wrapper captured the CloudCode request body of a live
`/compact` on agy 1.4.5. Its final user turn is the reference's
continuation-summary instruction — **2409 bytes** (2403 characters; the
body carries three em-dashes):

```
You have been working on the task described above but have not yet completed
it. Write a continuation summary that will allow you (or another instance of
yourself) to resume work efficiently in a future context window where the
full conversation history will NOT be available—only this summary.
...
6. **Commitments & Constraints**
   - Promises made to the user (e.g., "I said I would do X before Y")
   - User preferences or style requirements
   - Any boundaries the user set on approach, tools, or scope

Be concise but complete—err on the side of including anything that would
prevent duplicate work, repeated mistakes, or broken promises. Do not include
information that is obvious from the task description itself.

Wrap your response in <summary></summary> tags.

ON-DEMAND COMPACTION:
The user triggered this compaction. Follow any instructions in their latest message, if present.
```

Two structural facts the capture settles:

* the reference sends **no summariser system prompt** — the request
  keeps the normal Antigravity instructions as its system prompt and the
  instruction above is the **final user turn**, after the conversation
  and after the user's `/compact` message;
* the tail is trigger-dependent: the captured one is the *on-demand*
  variant. There is no `AUTO COMPACTION` literal in the 1.4.5 binary,
  so the automatic trigger's tail is still unknown.

The clone now sends this instruction byte for byte
(`internal/compaction/prompt.go`, pinned by
`internal/compaction/prompt_test.go`).

## 6. The reference's post-compaction context 【一手】

The reference's own trajectory log
(`~/.gemini/antigravity-cli/brain/<id>/.system_generated/logs/transcript_full.jsonl`)
records the compaction as a `CHECKPOINT` step whose content is the block
the reference installs in place of the compacted history (7403 bytes in
the captured session):

```
# Resuming from a compaction

You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:

# User Requests
The following were user requests from the truncated conversation in chronological order:
1. 加上
你倒置后根本与原来不构成上下镜像翻转啊
2. 你要从方格色块的角度思考
他们才是最小单元
3. continue
...
10. think step by step, you can solve it

<summary>
### 1. Task Overview
...
</summary>

# Conversation Logs
Start with the compact transcript; if a step has `truncated_fields`, read only that specific line in
- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`
- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript_full.jsonl`
```

* the request list holds the **ten** most recent user requests, in
  chronological order, verbatim (multi-line requests keep their
  newlines) — the reference's `CheckpointConfig.max_user_requests` is 10;
* the summary is the model's reply, `<summary>`-wrapped, exactly as the
  instruction asks;
* the fourth section points at the trajectory files the reference wrote.
  The clone keeps its sessions in memory and writes no such files, so it
  omits that section (the first three are reproduced byte for byte in
  `internal/compaction/resume.go`).

## 7. The clone's new row, live 【一手】

The clone, built from this tree, running the same flow as the reference
capture — one turn, then `/compact`, against the same Gemini model
(`--model gemini-3.8-flash-medium --backend cloudcode`, 120 columns):

```
\x1b[38;2;88;110;117m─────────────────────────────────────────────── Conversation compacted ────────────────────────────────────────────────\x1b[38;5;235m\x1b[K\x1b[m
\x1b[15;27H\x1b[38;2;101;123;131m\x1b[1K 2 steps summarized · context 9.3k → ~17.2k (273 summary) · took 5s\x1b[38;5;235m\x1b[K\x1b[m
```

Side by side with the reference (§1.1):

| | reference | clone (now) |
|---|---|---|
| steps summarized | 3 | 2 |
| context (model-reported) | 13.1k | 9.3k |
| → base context + summary | ~17.9k | ~17.2k |
| summary size | 271 | **273** |
| elapsed | 7s | 5s |

The **summary size** is the tell: the clone now sends the reference's
verbatim instruction, so the model produces a summary of the same size
(273 against 271 tokens) — with the cycle-237 reconstruction the two
would have had no reason to agree.

The base context: the clone's instructions in that run were 38 475 bytes
and the twelve declarations 13 125 bytes, so
`(38 475 + 13 125) / 3 = 17 200` — the `~17.2k` on the row. The
reference's `~17.9k` is the same computation over its own (slightly
larger) prompt: `(38 737 + 13 827) / 3 = 17 521` plus the summary.

The first number is the *model-reported* prompt-token count of the
compaction request, so it is not comparable across the two runs (the
reference's conversation was longer).
