# Cycle 289 — slash commands during a live turn, streaming `/context`, MiniMax M3 restored

## TL;DR

| What | Where |
|---|---|
| Slash dispatch before the streaming-queue branch | `cmd/wagyu/tui_run.go` (`submitPrompt`, `slashQueuesWhileStreaming`) |
| Shared panel/command dispatch used by the queue flush | `cmd/wagyu/tui_run.go` (`dispatchSlashLine`, `turnDoneMsg`) |
| In-flight `/context` layout (`Estimated usage (awaiting generation)`) | `cmd/wagyu/slash_panels.go` (`panelContextStreamingLines`) |
| `1.0M` free-space label instead of `1000.0k` | `cmd/wagyu/slash_panels.go` (`contextTokenLabel`) |
| MiniMax M3 restored in `/model`, with the agw registry note | `cmd/wagyu/slash_panels.go` (`modelRows`, `modelRowType.note`) |
| Regression tests | `cmd/wagyu/cycle289_streaming_slash_test.go` |

Reference: `~/.local/bin/agy` 1.4.3, unpatched, 120x40 tmux, driven through a
local CloudCode mock (`CLOUD_CODE_URL`) that streams slowly enough to keep a
turn in flight while keys are sent. All reference facts are 【一手】 unless
marked otherwise.

---

## Slash commands while a turn is streaming

### Reference

Live 1.4.3 behaviour, captured with the spinner still running:

- `/context` opens the panel **immediately**; the stream keeps writing behind it.
- `/usage` opens the quota panel immediately.
- `/model` opens the picker immediately. The picker in this run contained the
  registry-supplied `MiniMax M3` row (see below).
- `/plan` is **not** a local command: it is queued exactly like a prompt,
  rendered above the input as

  ```text
  ▸ /plan (alt+m to send immediately)
  ```

  and the status row switches to `Press up to edit queued messages`. When the
  in-flight turn ends, 1.4.3 echoes `> /plan` into the transcript and starts a
  new model turn (a second `streamGenerateContent` request appears in the mock
  log only after the first finishes).

So the rule is not "all slash commands run immediately". Local/panel commands
run immediately; commands that start a model turn are queued and then
dispatched as commands, not replayed as literal prompt text.

### Clone

`submitPrompt` now checks the streaming branch first but only takes the queue
path for:

- ordinary (non-`/`) text, and
- slash commands in `slashQueuesWhileStreaming`.

Everything else falls through to the normal slash dispatch. The queue set is:

| Reason | Commands |
|---|---|
| Starts another model turn | `plan`, `goal`, `learn`, `teamwork`, `boost`, `grill-me`, every `/skills` skill |
| Rewrites the live transcript/session | `clear`, `new`, `continue`, `resume`, `rewind`, `fork`, `branch`, `share`, `compact`, `quote` |

The mutating list is deliberately conservative: the reference executes slash
commands generically, but the clone's live anchors (`liveStart`, `replyStart`,
`liveReplyIdx`, `liveSteps`) are only safe if the transcript is not rewritten
while chunks are still arriving. The behaviour is therefore "execute now" for
read-only/panel commands and "execute when the turn finishes" for state
mutations. `/plan` matches the measured reference behaviour exactly.

Previously a queued slash command was handed to `streamPrompt`, which sent the
literal `/plan` or `/clear` to the model. `dispatchSlashLine` is now shared by
`submitPrompt` and the `turnDoneMsg` queue flush, so a queued command runs as a
command at flush time. Verified live: queueing `/clear` during a stream left
the transcript untouched until the turn ended, then cleared it without a second
model request.

Relevant capture shape:

```text
  The lighthouse stood at the edge
⡿  Generating
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
▸ /clear (alt+m to send immediately)
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  Press up to edit queued messages                                                        auto · Gemini 3.8 Flash · high
```

After the turn completed: banner + empty input, no `> /clear` row, no second
stream.

---

## `/context` while streaming

1.4.3 does not render the completed-session category breakdown during a turn.
It keeps the `Estimated usage (awaiting generation)` title and shows only the
user / agent / tool rows:

```text
└ Context Usage
◉ ◉ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □     Gemini 3.8 Flash (High) · 13/1.0M tokens
□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □      (0.0%)
□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □     Estimated usage (awaiting generation)
□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □     ◉ User messages: 5 tokens (0.0%)
□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □     ◉ Agent responses: 8 tokens (0.0%)
□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □     ◉ Tool calls: 0 tokens (0.0%)
□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □     □ Free space: 1.0M (100.0%)
```

Each non-empty category owns **one** grid cell (user, agent, tool, in that
order); empty categories emit no cell. The clone now has a dedicated
`panelContextStreamingLines` branch keyed on `m.streaming`, and reuses the same
`sessionTokenCounts` estimator as the completed layout. A live clone capture at
the same geometry matched the reference rows; the only difference was the token
count at the moment of capture (12 vs 13), which moves with the stream.

`contextTokenLabel` also promotes values that would round to `1000.0k`. The
reference prints `Free space: 1.0M`, not `1000.0k`; the clone previously printed
the latter when `contextMax - used` was 999,987.

---

## MiniMax M3 back in `/model`

Stock 1.4.3 does not advertise MiniMax M3 in `fetchAvailableModels`, which is
why cycle 1.4.3 alignment removed the row. The wrapper registry
(`~/.config/openagy/models.json` → `fetchAvailableModels` merge) does advertise
it, and 1.4.3 then renders it between GPT-OSS and Argon with the registry
description in 256-colour 60:

```text
> Gemini 3.8 Flash             (current)
  Claude Sonnet 4.6 (Thinking)
  Claude Opus 4.6 (Thinking)
  GPT-OSS 120B (Medium)
  MiniMax M3                    MiniMax M3 via agw (openagy wrapper)
  Gemini 4 Argon
```

The clone now keeps the same row and note at the same column, and the picker
diff against the live reference is empty. Selecting it sets the banner/status
to `MiniMax M3` and routes the next turn through `backend.AgwK0sClient` via the
existing `isMiniMax` split. This is a deliberate user-requested deviation from
the stock catalogue: without the wrapper registry, `--model minimax-m3` still
works, and the picker keeps it visible either way.

Index-based tests were updated for the extra row: Argon moved from index 4 to 5
and the non-Gemini effort-inert loop now covers MiniMax as well.

---

## Tests

- `cmd/wagyu/cycle289_streaming_slash_test.go`
  - `/context` opens while `streaming=true` and does not clear the flag
  - ordinary text still queues
  - `/plan` queues; `/clear`, `/new`, `/compact`, `/rewind` queue
  - a queued `/context` dispatches on `turnDoneMsg` without entering `m.session`
  - streaming `/context` has the awaiting-generation row, no category breakdown,
    estimated user tokens and `Free space: 1.0M (100.0%)`
  - MiniMax M3 is present with the registry note
- Existing model tests updated for the restored row.

`go test ./cmd/... ./internal/...` was run. `TestSlashCodesearchStreaming`
(2-second settle window) failed once under full-suite load and passed three
consecutive standalone runs; it is timing-sensitive, not a regression from this
cycle.

---

## 硬约束

- The reference binary was not patched. All reference observations come from
  the installed 1.4.3 TUI with `CLOUD_CODE_URL` pointing at a local mock.
- The mock server lives in `/tmp` and is not committed; its only role is to keep
  one SSE turn open long enough to type slash commands.
- The clone still estimates `/context` token counts from prompt/tool/session
  sizes; the provider's usage metadata is not surfaced per category.
- Transcript-mutating slash commands are queued rather than executed while
  chunks are in flight. This is a safety choice, not a measured reference
  difference.
- No browser/TUI full-screen byte diff was taken; comparisons are tmux
  `capture-pane` rows at 120x40 plus targeted `-e` spans.

## 存疑

- Whether 1.4.3's `/clear`, `/new`, `/compact` etc. run immediately or queue
  when typed mid-turn was not measured. The clone queues them; `/plan` was
  measured and queues.
- `Alt+M` "send immediately" on a queued slash command is still not modelled;
  alt+m continues to toggle Queue mode in the clone.
- The exact slash-skill set that starts a model turn is derived from
  `slashSkillLookup`; a future skill that behaves differently would need the
  same queue treatment.

## limitations

- `@` picker candidate order remains an acknowledged approximation
  (language-server index order is not deterministic).
- The agent-status row tab byte is still expanded by Bubble Tea even though the
  visible column matches.
- `/context` system-prompt/tools/skills/subagents values remain estimates; only
  the streaming/completed shape is byte-aligned.
- The reference `/plan` prompt body is still a clone-side reconstruction
  (`planPrompt`), not recovered text.
