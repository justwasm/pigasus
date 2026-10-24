# Compaction

> Two compaction implementations, the reference's numbers, and how the context window is derived.

## Compaction (cycle `auto-compact-parity`)

There are **two** compaction implementations in the clone, and the one the
reference's UI is matched against is the smaller one:

| path | where | live? |
|---|---|---|
| generic summariser (cycle 237) | `internal/compaction` + `internal/cliapp/slash_compact.go` | yes — the TUI's `/compact` |
| PiG compaction | `internal/pigrt` → `coding.Session` | print mode only |

`m.compactor` — the hook that would let the TUI drive PiG's compaction — is
**never assigned** (`internal/cliapp/slash_commands.go:867`), so the TUI always takes
the generic path and `appendCompactResult` (`internal/cliapp/tui_turn_blocks.go`) is
unreachable today. It was aligned to the reference's shape anyway.

The reference's `/compact` completion (agy 1.4.5, live pty capture) is **two
centred rows**, not a bullet: a `#586E75` rule of width−1 columns with
` Conversation compacted ` centred inside it (floor left, ceil right), and a
`#657B83` stats line centred on the full width at column
`(width−cells)/2 + 1`:

```
3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s
```

The builders are `internal/compaction/rows.go`
(`compactDividerRow`, `compactStatsRow`, `compactStatsText`,
`compactTokenMagnitude`); the centring is by **display cells** (`·` and `→` are
multi-byte). A negative argument means "not measured" and drops the field,
which is how the PiG path reuses the same shape with only token counts.

What the reference's two context numbers *mean* is resolved (decompiled from
agy 1.4.5's `cli/render/render.compactionStatsParts` + `CompactionTokens`):

| field | the reference | the clone |
|---|---|---|
| `context A` | the compaction request's **model-reported** prompt tokens | the backend's reported `PromptTokens` (fallback: base + conversation) |
| `→ ~B` | the **base context** (instructions + tool declarations, 3 bytes/token) **+ the summary's size** | `compaction.ContextOverheadTokens` + the summary's size |
| `(C summary)` | the reported **completion** tokens | the backend's reported `CompletionTokens` |

The pair is **not** a before/after of one quantity: A counts the model's real
tokens, B counts the reference's *estimate* of the base context, so B is the
larger one in every capture. The reference's divisor is **3.0 bytes/token**
(`EstimateTokenCounter.GetTokenCount` = `int(float64(len)/divisor)`,
`createDefaultTokenCounter` stores `0x4008000000000000`), so
`internal/compaction.EstimateTokens` divides by three, not four.

Three more things the clone now does the reference's way:

- **The summary request.** The reference sends *no* summariser system prompt:
  the normal Antigravity instructions stay in the system prompt and the
  **verbatim** continuation-summary instruction (2409 bytes, captured live) is
  the **final user turn** — `internal/compaction/prompt.go`
  (`SummarizePrompt` + `OnDemandCompactionSuffix`). The **body** of that
  instruction (2289 bytes) is byte-identical to the catalogue's per-model
  `session_summary_prompt_override` — the reference's *automatic* checkpoint
  asks for the same six-section `<summary>` block, so the clone needs no second
  prompt. Pinned by `internal/compaction/prompt_test.go`
  (`TestSummarizePrompt_MatchesCheckpointOverride`).
- **The post-compaction context.** The compacted history is replaced by the
  reference's `# Resuming from a compaction` block — preamble, the **ten** most
  recent user requests (`CheckpointConfig.max_user_requests`), and the
  `<summary>`-wrapped summary — `internal/compaction/resume.go`. The
  reference's fourth section (`# Conversation Logs`) is omitted: the clone
  writes no trajectory files.
- **The PiG context window is the reference's per-model budget.** Every model
  handle the clone builds carries `Capabilities.ContextWindow` from
  `internal/modelctx` — the captured server catalogue
  (`docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json`):
  the effective limit is `min(catalogue maxTokens, the model's checkpoint
  max_token_limit)` — 256 000 for `gemini-3.8-flash-*`, 160 000 for the
  Claude 4.6 family, 80 000 for `gpt-oss-120b-medium`, 1 000 000 for
  `custom-minimax-m3`. `internal/pigrt.ContextWindow` is only the fallback for
  a model the clone cannot name. The *reserve* PiG holds back is the
  checkpoint's `max_output_tokens` (16 384, except gpt-oss's 8 192; minimax has
  no checkpoint experiment, so its reserve is the catalogue's own output cap,
  8 192) — so PiG's rule `contextTokens > contextWindow − ReserveTokens` is the
  reference's own comparison. Without a window PiG's rule degenerated to
  "always" (window 0), so any session past 20 000 tokens would have compacted
  on *every* turn.

The reference's own auto-compaction model (per-model `max_token_limit`
80k…840k, `token_threshold` 50000, `max_overhead_ratio` 0.15, the
`CASCADE_USE_EXPERIMENT_CHECKPOINTER` payload, the checkpoint proto, the
summary cache, the continuation-summary prompt) is documented from the binary
and **not implemented** — the clone has no cascade layer. Report:
`docs/cycles/auto-compact-parity/deliverable.md` (+ `captures.md`).

**"Cascade" = one run of the agent** (Codeium/Windsurf's product name, kept
inside the Antigravity runtime — the binary still carries `You are Cascade, a
powerful agentic AI coding assistant designed by the Codeium engineering
team`). `StartCascade` / `SendUserCascadeMessage` / `RevertCascadeToStep`
are the RPCs; `cortex/cascade_run_state` holds the run's state; a run is a
cascade of `CORTEX_STEP_TYPE_*` steps produced by repeated model
invocations. `CascadeConfig` is the run's behaviour profile
(`PlannerConfig`, `MessageConfig`, `ExecutorConfig`, **`CheckpointConfig`**,
`ConversationHistoryConfig`, `AgentApiConfig`, `BattleModeConfig`,
`TrajectoryConversionConfig`). **Auto-compaction is the
`checkpoint_config` sub-message**, not a separate subsystem — hence the
binary's own log line `Total attempted tokens: ~%d (exceeding
cascade_config.checkpoint_config.max_token_limit (%d))`, and hence the
clone's gap: it has no cascade layer (no config proto, no planner/executor
split, no steps, no trajectory). The `CheckpointConfig` field list and the
evidence grades are in the report's §0.1 and §2.3.
