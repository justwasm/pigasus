# The cascade's model catalogue, its validator and its session-summary instruction — deliverable

**Cycle:** `cycle-303-cascade-model-profiles`
**Subject:** the reference's *cascade* layer is the reference's own
auto-compaction subsystem — `CascadeConfig.checkpoint_config`. At **agy
1.4.7** the binary ships that subsystem's configuration **inside itself**:
a 57 635-byte `model_profiles.json` embedded in the rodata, carrying one
checkpoint payload per model family, the alias map that folds the model ids
onto those families, the base block's `checkpoint-variant` and the
per-profile variant payloads. The clone had been reading the *server's*
1.4.4 catalogue instead and was carrying the **1.4.4 revision** of the
reference's continuation-summary instruction — six sections, 2 289 bytes —
whereas 1.4.7 ships **seven** sections, 3 937 bytes. This cycle swaps the
catalogue, ports the validator and the clamps, pins the 1.4.7 overflow
message, and replaces the instruction with the bytes the reference now
sends.
**Evidence:** 【一手】 the binary's own bytes — `~/.local/bin/agy`
(agy **1.4.7**, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`) — and the
three artefacts extracted from it at known file offsets; 【反编译】 the
conversion limit and the validator, from the binary's own decompiled output;
【strings】 the literal message set.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the per-model catalogue** | the *server's* 1.4.4 probe (`fetchAvailableModels_1.4.4.json`) | the **binary's own** `model_profiles.json` (1.4.7), with the 1.4.4 capture as the fallback for the models it does not carry |
| **the model → profile mapping** | none — the clone knew a window per slug | the reference's own alias map (34 model enums → 7 profiles) |
| **the checkpoint strategy** | unknown | `SAME_MODEL` / `SINGLE_PROMPT` / `UNSPECIFIED`, per profile |
| **the checkpoint variant** | unknown | `claude-1m_840k` — an 840 000-token window for `MODEL_PLACEHOLDER_M400`, plus `use_model_reported_token_count` |
| **the validator** | none | the reference's four messages, byte for byte |
| **the clamps** | none | `max_token_limit ≤ 256000`, `max_output_tokens ≤ 16384` (and `< 1` → 16 384), plus model 323's 1 000 000-token window |
| **the overflow message** | the 1.4.5 wording (a comment) | the **1.4.7** wording, 438 bytes, pinned by a fixture |
| **the continuation-summary instruction** | the 1.4.4 revision — 2 289 bytes, six sections | the **1.4.7** revision — **3 937 bytes, seven sections** |

Two of these are behaviour-changing today:

1. **The instruction the clone sends changed.** The compaction request now
   carries the reference's 1.4.7 text. This is the only byte-level change
   the clone *emits*.
2. **The catalogue is now the binary's.** The windows do **not** move —
   the two catalogues agree on every number the clone uses (see §4) — but
   the source of truth is now the artefact the reference ships, and the
   fields the capture never carried (strategy, snapshot switches, retry
   policy, session-summary override, variant) are now known.

## 1. The embedded document

【一手】 agy 1.4.7 embeds a JSON document at **file offset 0x9a0f20a** of the
Mach-O — 57 635 bytes, extracted byte for byte and committed at
`internal/modelctx/testdata/model_profiles_1.4.7.json`. Its top level is:

```
base           — the global settings (checkpoint-variant, the compaction
                 switches, the auto-command allowlist, …)
subclients     — per-client overrides (CLI / IDE / SDK); the CLI's own
                 block leaves every knob null
os             — per-OS overrides
model_profiles — one profile per model family, each with a
                 CASCADE_USE_EXPERIMENT_CHECKPOINTER payload
models         — the alias map: model enum → profile
```

The `models` map folds **34** model enums onto **7** profiles:

| profile | the model enums that fold onto it |
|---|---|
| `MODEL_PLACEHOLDER_M124` | M317, M318, M319, M320, M322 |
| `MODEL_PLACEHOLDER_M26` | M35, M26 |
| `MODEL_GOOGLE_GEMINI_2_5_FLASH` | M50, M198, `…_FLASH_LITE`, `…_FLASH_THINKING` |
| `MODEL_GOOGLE_GEMINI_2_5_PRO` | M148, M18 |
| `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` | — (the profile's own name) |
| `MODEL_PLACEHOLDER_M400` | M400 … M405 |
| `MODEL_PLACEHOLDER_M408` | M409 … M413, M417 … M421 |

(M414, M415 and M416 are absent from the map — the fold skips them.)

### 1.1 The payloads

Each profile's payload is a JSON *string* whose numbers are quoted
(`"max_token_limit": "256000"`), which is the reference's own encoding:

| profile | strategy | `max_token_limit` | `max_output_tokens` | `token_threshold` | `max_overhead_ratio` | `retry.max_retries` | session-summary override |
|---|---|---|---|---|---|---|---|
| `MODEL_GOOGLE_GEMINI_2_5_FLASH` | `SINGLE_PROMPT` | 128 000 | 16 384 | 50 000 | 0.15 | 0 | — |
| `MODEL_GOOGLE_GEMINI_2_5_PRO` | `SINGLE_PROMPT` | 128 000 | 16 384 | 50 000 | 0.15 | 0 | — |
| `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` | `UNSPECIFIED` | 80 000 | 8 192 | 50 000 | 0.15 | 0 | — |
| `MODEL_PLACEHOLDER_M26` | `UNSPECIFIED` | 160 000 | 16 384 | 50 000 | 0.15 | 0 | — |
| `MODEL_PLACEHOLDER_M124` | `SAME_MODEL` | 256 000 | 16 384 | 50 000 | 0.15 | 4 | **yes** (3 931 chars) |
| `MODEL_PLACEHOLDER_M400` | `SAME_MODEL` | 256 000 | 16 384 | 50 000 | 0.15 | 4 | — |
| `MODEL_PLACEHOLDER_M408` | `SAME_MODEL` | 256 000 | 16 384 | 50 000 | 0.15 | 4 | **yes** (3 931 chars) |

### 1.2 The variant

【一手】 the base block carries `"checkpoint-variant": "claude-1m_840k"`, and
exactly one profile ships a payload under that name —
`MODEL_PLACEHOLDER_M400`'s
`CASCADE_USE_EXPERIMENT_CHECKPOINTER/claude-1m_840k`:

```
strategy                     CHECKPOINT_STRATEGY_SAME_MODEL
max_token_limit              840000
use_model_reported_token_count true
include_last_user_message    true
include_conversation_log     false
is_sync                      true
```

【反编译】 `config.selectCheckpointExperimentKey` builds the lookup key
`"<experiment>/<variant>"` from the variant name and the experiment key, so
the variant is a **per-profile** payload: the reference asks the profile for
`CASCADE_USE_EXPERIMENT_CHECKPOINTER/claude-1m_840k` and falls back to the
plain payload when the profile has none. The IDE and SDK sub-clients set
`"checkpoint-variant": null`, so the **CLI inherits the base block's
value** — the variant is a CLI-wide setting, not a per-model one.

The clone therefore applies the base variant automatically: a model whose
profile carries a variant payload is budgeted from that payload.

### 1.3 The session-summary override

The two profiles that override it (`M124`, `M408`) carry the **same 3 931
characters** as the binary's own built-in default — the string literal at
file offset **0x3785081** (3 937 bytes; the body carries three em-dashes,
hence 3 931 characters). See §3.

## 2. The validator and the clamps

【反编译】 `cortex/utils.ValidateCheckpointConfigForModelInfo` (0x101318120)
checks a checkpoint configuration against the models it names and reports
four errors, all of them literals in the binary's string table. The clone
keeps them byte for byte:

| # | message | when |
|---|---|---|
| 1 | `plannerModelInfo cannot be nil` | the planner's model info is missing |
| 2 | `config.MaxTokenLimit (%d) cannot exceed the planner model's context window limit (%d) minus the planner model's MaxOutputTokens (%d)` | the checkpoint's limit is above the planner's window minus the planner's output cap |
| 3 | `config.MaxOutputTokens %d is greater than checkpointModelInfo.MaxOutputTokens %d` | the checkpoint's output cap is above the checkpoint model's own |
| 4 | `overheadRatio %f is not between 0 and %f` | the ratio is outside the band — `0 < ratio < 0.5`, **both bounds exclusive** |

【反编译】 the companion `cortex/utils.GetCheckpointConfig` (0x101317a20)
applies the clamps before the validator ever sees the configuration:

- `max_token_limit` is capped at **256 000**;
- `max_output_tokens` is capped at **16 384**, and a value below one becomes
  **16 384**;
- model id **323** (`0x143`) gets `max_token_limit = 1 000 000` and
  `enabled = true`, whatever the payload said.

## 3. The continuation-summary instruction — the byte-level win

【一手】 the reference's instruction is a **string literal** in the 1.4.7
binary (file offset **0x3785081**, **3 937 bytes**), and it is *the same
bytes* as the `session_summary_prompt_override` of the two profiles that
override it.

The clone was carrying the **1.4.4** revision: 2 289 bytes, six sections.
1.4.7 asks for **seven** — it splits `Errors & Fixes` out of `Key Findings`
and renumbers the rest — and adds the sentences that keep a resumed agent
from repeating work:

- "The user's core request, success criteria, and **requested
  deliverables**. Record any target IDs, URLs, or reporting recipients from
  the request.";
- "test or **verification commands run and their outcomes**";
- "Skills, documentation, or reference files that were read or inspected";
- "Distinguish completed edits from files that were only read or partially
  edited, and **do not mark unverified test commands as completed or
  passed**";
- "Preserve specific uncompleted items rather than collapsing checklists
  into high-level summaries."

The clone now sends the 1.4.7 bytes. The on-demand tail
(`ON-DEMAND COMPACTION:\nThe user triggered this compaction. …`) is
unchanged — it is the reference's own literal at 0x036619c8 — so the user
turn the clone sends is now **4 057 bytes** (was 2 409).

The old revision is kept as evidence:
`internal/compaction/testdata/session_summary_prompt_override_1.4.4.txt`.

## 4. What did **not** move

The 1.4.7 catalogue and the 1.4.4 capture agree on every number the clone
uses. The 1.4.4 probe already carried the same checkpoint payloads for the
models it advertised, and the effective window is

```
min(model input limit, checkpoint max_token_limit)
```

with the model's own input limit larger in every case:

| catalogue id | the reference's own limit | the checkpoint's | effective | reserve |
|---|---|---|---|---|
| `gemini-3.8-flash-{high,medium,low}` (`M318/M319/M320`) | 1 048 576 | 256 000 | 256 000 | 16 384 |
| `gemini-3.8-flash-tiered` (`M322`) | 1 048 576 | 256 000 | 256 000 | 16 384 |
| `claude-sonnet-4-6` (`M35`) | 250 000 | 160 000 | 160 000 | 16 384 |
| `claude-opus-4-6-thinking` (`M26`) | 250 000 | 160 000 | 160 000 | 16 384 |
| `gpt-oss-120b-medium` | 131 072 | 80 000 | 80 000 | 8 192 |
| `gemini-2.5-{flash,flash-lite,flash-thinking}` | 1 048 576 | 128 000 | 128 000 | 16 384 |
| `gemini-2.5-pro` (`M18`) | 1 048 576 | 128 000 | 128 000 | 16 384 |
| `gemini-3.1-flash-lite` (`M50`) | 1 048 576 | 128 000 | 128 000 | 16 384 |
| `gemini-4-argon-disabled` (`M421` → `M408`) | 1 048 576 | 256 000 | 256 000 | 16 384 |

So the catalogue swap changes **no** window and **no** reserve. What it
buys is the mechanism (the alias map, the variant, the strategy) and the
fields the capture never had.

## 5. What changed in the clone

| file | change |
|---|---|
| `internal/modelctx/profiles_1.4.7.go` | **new** — the profile table, the alias map, the variant payloads, `ProfileFor` |
| `internal/modelctx/validate.go` | **new** — the validator, the clamps, the model's own exception |
| `internal/modelctx/overflow.go` | **new** — the 1.4.7 overflow message and its renderer |
| `internal/modelctx/modelctx.go` | the `limits` table becomes `catalogue` (the raw numbers), `For()` consults the 1.4.7 catalogue first and the 1.4.4 capture second, the package comment is rewritten |
| `internal/modelctx/testdata/model_profiles_1.4.7.json` | **new** — the extracted document (57 635 bytes) |
| `internal/modelctx/testdata/overflow_1.4.7.txt` | **new** — the overflow message (438 bytes) |
| `internal/compaction/prompt.go` | the instruction is now 1.4.7's (3 937 bytes) |
| `internal/compaction/testdata/session_summary_prompt_1.4.7.txt` | **new** — the instruction (3 937 bytes) |
| `internal/compaction/prompt_test.go` | the byte counts, the section list and the catalogue comparison are re-pinned |
| `internal/referencedrift/reference.lock.json` | the three new artefacts are registered |

The tests derive the expected numbers **from the extracted document**, not
from a hand-copy: `TestProfilesMatchTheEmbeddedDocument` parses the fixture
and compares every field of every profile, `TestProfileAliasesMatchTheEmbeddedDocument`
compares the alias map, and `TestThe840kVariant` pins the variant. A table
edit that drifts from the binary fails there.

## 6. 硬约束 (hard constraints)

- The catalogue is keyed by the **reference's model enum names**, and the
  clone's slug → enum mapping comes from the 1.4.4 capture plus the clone's
  own table. **Never** invent a slug → enum mapping: the binary's
  `models` map folds *enums* onto profiles, not slugs.
- The effective window is `min(model input limit, checkpoint
  max_token_limit)`, and the checkpoint's limit is ignored when the
  checkpoint is **disabled** — never swap the two operands.
- The reserve is the checkpoint's `max_output_tokens` — never the model's
  own output cap for a model that carries a checkpoint experiment.
- `max_overhead_ratio`'s band is **exclusive on both ends** (`0 < r < 0.5`).
- The validator's four messages are reproduced **byte for byte**; they are
  the reference's own strings, not the clone's wording.
- The clamps live in `Clamp`/`ClampForModel`, and the model-323 exception
  must stay out of `Clamp` — it is per model, not per configuration.
- The overflow message's leading `%w` is the **cause**: the reference
  builds it with `fmt.Errorf`, so the cause's text leads the sentence. Do
  not render it with `Sprintf` and a `%w` (the `go vet` printf check
  rejects that).
- The 1.4.4 instruction stays in the tree as evidence; it must not be
  edited, and the 1.4.7 fixture must not be retyped — both are extracted
  bytes.

## 7. 存疑 (open questions)

- **The 1.4.7-only model ids are not mapped.** The binary's string table
  carries a newer catalogue than the clone's static one (`gemini-4-argon-
  {high,medium,low}`, `claude-opus-5-5-{max,high,medium,low}`,
  `claude-sonnet-5-5-{max,high,medium,low}`, `gemini-3.1-pro-*`, …), but
  the mapping from those slugs to the binary's model enums is **not
  resolved**. Until it is, the `claude-1m_840k` variant is unreachable
  through `For()`: no clone-visible slug folds onto
  `MODEL_PLACEHOLDER_M400`. The variant is reachable through the profile
  table (`profileVariants`), which is where the test pins it.
- **Which profile the 840 000-token window belongs to** is therefore also
  unresolved — the variant payload is *under* `MODEL_PLACEHOLDER_M400`, but
  which of the CLI's model ids is `M400`…`M405` is not known.
- **The order of the validator's four checks** is the order the strings sit
  in the binary's string table; the decompilation does not pin it. The
  clone's order is that one.
- **The automatic trigger's tail is still unknown.** The reference has no
  `AUTO COMPACTION` literal (0 hits at 1.4.7), so the on-demand tail is
  the only one the clone can reproduce.
- **`shouldCheckpoint` is not ported.** The reference's predicate decides
  *when* the cascade checkpoints; the clone's compaction is PiG's, and
  PiG's own trigger is what fires.
- **The fourth resume section (`# Conversation Logs`)** is still omitted:
  the clone writes no trajectory files, so there is nothing to include.

## 8. Limitations

- The document was extracted from the **Mach-O** of this machine's agy
  1.4.7. A different build (or the IDE's own `language_server`) may embed a
  different catalogue; the artefact's digest is what pins *this* one.
- The profile table carries the fields the cascade *reads*, not the whole
  31-field `CheckpointConfig`: the media-token budget, the user-request byte
  cap, the artifact-section cap and the retry timings are not modelled.
- The validator is a pure function over the clone's own types; nothing in
  the clone calls it yet, because the clone has no cascade configuration to
  validate. It is here so the messages and the clamps stay pinned while the
  cascade grows.
- The overflow error is likewise unraised: the clone's conversion is PiG's
  and does not produce that message.
- The clone's own estimate (3 bytes per token) is unchanged; the reference's
  own numbers in the overflow message are the ones it *reports*, which the
  clone does not reproduce.
