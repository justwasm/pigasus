# Cycle `model-catalogue-argon-gate` — 24 advertised, 6 selectable, 1 gated

**Status**: implemented, `cmd/wagyu` + `internal/cli` green
**Reference**: `~/.local/bin/agy` 1.4.4, live 2026-10-22
**Evidence**: `fetchAvailableModels_1.4.4.json` in this directory — the full
`v1internal:fetchAvailableModels` response body, captured by `cmd/agy`

## What the server advertises

The response is a **map keyed by model id** (24 entries) plus `agentModelSorts`, which
defines what the CLI is allowed to show:

```json
"defaultAgentModelId": "gemini-3.8-flash-high",
"agentModelSorts": [{"displayName": "Recommended", "groups": [{"modelIds": [
  "gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low",
  "claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium",
  "gemini-4-argon-disabled", "custom-minimax-m3"]}]}]
```

(`custom-minimax-m3` is `cmd/agy`'s own registry injection, not Google's — the bare
binary's response has 23 entries and a 7-id group.)

The 24 ids split into three classes, and the split only becomes visible by probing
`--model <id> -p ok` one by one:

| class | ids | `--model` outcome |
|---|---|---|
| **selectable** (6) | `gemini-3.8-flash-{high,medium,low}`, `claude-sonnet-4-6`, `claude-opus-4-6-thinking`, `gpt-oss-120b-medium` | runs the turn; printed by `agy models` and in `Available models:` |
| **gated** (1) | `gemini-4-argon-disabled` ("Gemini 4 Argon") | known, refused with a plan reason (below) |
| **not recognized** (17) | `gemini-3.8-flash-tiered`, `gemini-3.5-flash-lite`, `gemini-3.5-flash-{low,extra-low}`, `gemini-3-flash`, `gemini-3-flash-agent`, `gemini-3.1-flash-lite`, `gemini-3.1-flash-image`, `gemini-2.5-pro`, `gemini-2.5-flash`, `gemini-2.5-flash-lite`, `gemini-2.5-flash-thinking`, `tab_flash_lite_preview`, `tab_jump_flash_lite_preview`, `chat_20706`, `chat_23310`, `custom-minimax-m3`\* | `model…is not recognized as a known model or custom model in settings` |

\* under the wrapper only; the bare binary does not have that id at all.

So the server advertising an id does **not** mean the CLI accepts it: `gemini-3.5-flash-lite`
is the helper model the reference itself calls (`tabModelIds`, `tieredModelIds.flashLite`) and
is still not user-selectable.

## The gate (一手)

```
$ agy -p ok --model gemini-4-argon-disabled
error: invalid model selection (--model "gemini-4-argon-disabled" --effort ""): "Gemini 4 Argon" is unavailable: Gemini 4 Argon is currently available to Ultra and paid Pro plan subscribers
Available models:
  Gemini 3.8 Flash (High)
  Gemini 3.8 Flash (Medium)
  Gemini 3.8 Flash (Low)
  Claude Sonnet 4.6 (Thinking)
  Claude Opus 4.6 (Thinking)
  GPT-OSS 120B (Medium)
```

Four probes pin the branches:

| probe | outcome |
|---|---|
| `--model "Gemini 4 Argon"` (label) | same gate message |
| `--model gemini-4-argon` (no `-disabled`) | `not recognized` — the gate is keyed on the exact id |
| `--model gemini-4-argon-disabled --effort high` | gate message, **not** the `conflicts with --effort` error |
| `--model gemini-4-argon-disabled --effort bogus` | `invalid --effort "bogus"` — the effort vocabulary is checked **before** the gate |

Argon is absent from `agy models`, from `Available models:` and from the `/model`
table: it is known only in order to explain why it cannot be chosen.

## What changed

| File | Change |
|---|---|
| `internal/cli/model_gates.go` (new) | `GatedModel{ID, Label, Reason}`, `ReferenceGatedModels()` — one entry, the captured reason verbatim |
| `cmd/wagyu/model_select.go` | `gatedModel()` lookup, applied after the `--effort` vocabulary check and before every other `--effort` branch |
| `cmd/wagyu/model_select_test.go` | five new table rows: id, label, `--effort high`, bare `gemini-4-argon` (unknown), `--effort bogus` (precedence) |

`ReferenceModels()` is unchanged — the selectable set already matched the capture
(`gemini-3.8-flash-{high,medium,low}`, the two Claude 4.6 entries, `gpt-oss-120b-medium`).
The clone's `agy models` prints those six plus `minimax-m3` (its configured-only model), which
is the documented deviation; under the wrapper the reference prints six plus
`custom-minimax-m3`.

## 硬约束

- The gate message, the available block and the precedence between the gate and `--effort`
  are captured; the tests spell them out.
- `Available models:` in the clone lists seven labels (the six plus MiniMax M3). The
  reference prints six when run bare and seven under the wrapper; the clone's list is its
  configured-model deviation, unchanged by this cycle.

## 存疑

- **Whether Argon is actually selectable on an Ultra account** is untested — the message
  says "currently available to Ultra and paid Pro plan subscribers", and this account is
  neither. The gate is modelled as unconditional; a plan-aware gate would add a field, not a
  new code path.
- **`--model gemini-4-argon-disabled` in the TUI** is untested. The print-mode validator runs
  before the session exists; the TUI may render a disabled row instead of exiting 1.
- The 17 "not recognized" verdicts are inferred from the group filter plus spot probes
  (`gemini-2.5-pro`, `gemini-3-flash`, `gemini-3.5-flash-lite`, `gemini-3.8-flash-tiered`).
  The remaining ids share the same shape (`modelProvider` present but no `displayName` in
  the sort group) and were not each probed.

## limitations

- One account, one day. `fetchAvailableModels` is account- and experiment-scoped: the
  committed snapshot is evidence for *this* profile, and the drift gate should treat a new
  map as a new capture rather than as a code regression.
- No `internal/referencedrift` artifact was added for the catalogue. Its `artifacts[]` list
  is version-scoped fixtures; a per-account model map does not fit the "digest must not
  change" rule, so the snapshot lives with the cycle report instead.
