# Auto-compaction parity table — 1.5.3

> One row per unresolved or partially resolved behaviour. Rank by what a user
> or a script can observe, not by how interesting the internals are. Evidence
> labels: **一手** (live trace), **反编译** (binary), **推断** (inferred).

## How to read the columns

- **Observed surface** — where a difference would show up. If a row has no
  user- or script-visible surface, it should not be worked on.
- **Confidence** — how sure we are of the current clone behaviour, not of the
  reference.
- **Cost** — rough reverse-engineering / implementation cost.
- **Decision** — what the clone does today; the next action only when it buys
  observable parity.

## Table

| # | Item | Observed surface | Evidence | Confidence | Cost | Decision / next |
|---|---|---|---|---|---|---|
| 1 | `# Conversation Logs` tail | resume block sent to the model; `/context` size | 一手 (13/13 checkpoints) | High | Done | Implemented byte-exact in `compaction.ConversationLogs`; gated by the profile's `include_conversation_log`. |
| 2 | Artifact snapshot section | resume block; `/context` size | 一手 (9/13 checkpoints) | High | Done | Renderer and reader implemented. `artifacts.List` reads `<brain>/<conv>/<name>.<ext>` plus its `.metadata.json` sidecar (drops `userFacing:false`, truncates the time to seconds); TUI and agent-server supply it through `agentturn.Env.CheckpointSnapshot`. The clone still has no artifact writer, so a fresh conversation's section is empty. |
| 3 | Subagent snapshot section | resume block after a turn that spawned a subagent | 一手 (checkpoint at step 7414) | High | Done | Renderer byte-exact, including the capped header/preamble. `Registry.SnapshotSection` lists every spawned child (not only running), keeps the newest 20, and the TUI feeds it through `agentturn.Env.CheckpointSnapshot`. |
| 4 | Running-task snapshot section | resume block while a background task runs | 一手 (checkpoints from step 23467) | High | Done | Renderer byte-exact (protojson multiline). `shell.RunningTaskSnapshots` reports the conversation's still-running tasks with their tool, summary, command, start time, spawn step and log URI; TUI and agent-server supply it. |
| 5 | `include_last_user_message` | resume block contents | 一手 (3.8 manual checkpoint; the last request is in `# User Requests`, no separate section) | Medium | Low | The flag is not a section. A live 3.8 (M124, flag `true`) manual checkpoint listed `/compact` in `# User Requests` and ended with the summary, so the flag most likely keeps the pending turn's input outside the summary — which the clone already does through `KeepInput`. The automatic shape remains unverified. |
| 6 | `token_threshold` consumer | when a checkpoint fires (50k vs hard limit) | 反编译 field; live trace shows hard-limit only | Medium (off) | High | Soft trigger is off by default; live 1.5.3 checkpoints fired at ~258k context. Revisit only if a live soft checkpoint is observed. |
| 7 | `max_overhead_ratio` consumer | checkpoint trigger arithmetic / validation | 反编译 field only | Low | Medium | Unused. Do not guess; a live trace that moves the ratio is the cheapest oracle. |
| 8 | `moving_window_size=0` meaning | which steps stay outside the summary | 1.4.4 payload = 1; 1.4.8/1.5.3 omit it; default constructor leaves 0 | Low | High | Clone keeps 1. Behaviour is validated against live checkpoints; field-level semantics remain open. |
| 9 | Automatic summary instruction tail | summary request bytes | 反编译 / live request not captured | Medium | Low | Clone sends the body with no tail; on-demand `/compact` keeps its tail. Revisit only if a captured request shows one. |
| 10 | `full_async` / `enable_fallback` / `fail_on_invalid_output` | async timing / retry behaviour | payload fields; runtime not observed | Low | Medium | Unimplemented or defaulted. Observe one live failure/retry before coding. |
| 11 | Manual `/compact` tails | the session replacement after `/compact` | 一手 (conversation `e9bd3e8a-…`, Gemini 3.8 Flash High) | High | Done | Manual checkpoints carry the artifact section and the fixed `# Conversation Logs` tail; the profile's `include_conversation_log: false` does not apply. `/compact` is itself a request, earlier `/compact`s stay in the list, and the list survives the session replacement (seeded from the persisted checkpoint). |
| 12 | Transient stats row (`Compacting context…`) | TUI only | live TUI parity work | Medium | Low | Existing work; verify the row appears/disappears at the same boundary. |
| 13 | Headless / remote async limitation | print mode, remote hosts | host coverage work | Medium | Medium | Local keeps a session-scoped policy; other hosts are per-turn. Only matters if a summary spans turns there. |
| 14 | Subagent `# Parent Agent Messages` | resume block of a subagent checkpoint | 一手 (subagent conversations, e.g. `a9756e99` step 350) | High | Done | Implemented. `subagenttools.ParentContext.Lines` renders the task and the parent's later messages with `agentmessage.RenderLine`; the child policy receives them through `checkpoint.Snapshot.ParentMessages`, and `compaction.ParentBlock` replaces `# User Requests`, including the capped "most recent messages" preamble. |

| 15 | Checkpoint payload persistence | `/resume`, `/continue`, later manual `/compact` request carry-over | 一手 (reference DB `e9bd3e8a-…`; live pigasus run `d2d640b0-…`) | High | Done | The writer emitted no checkpoint payload, so `Checkpoint()` skipped it as an opaque type-23 placeholder and `/resume` replayed the pre-compaction history. It now writes 30.1/30.3/30.4/30.5/30.11/30.12 and the request list at 30.30 (nested field 1); the reader reads 30.30 instead of the guessed 19. The stored summary is normalized to the `<summary>` span, dropping CloudCode's `<think>` prefix. |

## Already aligned (for context)

- Step-boundary checkpoint inside the turn; never cancels the turn.
- `TurnRecord.Steps` stays model/tool-only; checkpoints are separate records.
- `ResetEpoch` only on history reset; checkpoints bump `Revision`.
- Hard-limit trigger: `projected > contextWindow - reserve`.
- Step gate: no automatic checkpoint after a user-input or model-response step;
  hard limit overrides it.
- 10-request cap, chronological order, verbatim multi-line requests.
- `# Conversation Logs` and the artifact section are rendered byte-exact.
- The running-task and subagent sections are rendered byte-exact from host refs.
- 1.5.3 profiles use the model-reported token count.
- Checkpoint persistence (`StepCheckpoint=23`) and `/resume` context.
- Manual `/compact` writes a checkpoint step and bumps the session epoch.

## Ordering rule

Work top-down. A row moves up only when a live trace or a user-visible
regression makes it observable; otherwise it stays documented and closed.
