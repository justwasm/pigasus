# Cycle 284 — ctrl+o on the in-flight turn (and the step the turn-end wipe ate)

Reported symptom: **ctrl+o does not expand a tool call that is already on
screen, especially while the turn is streaming/generating.** Confirmed
reproduced, fixed, and re-verified against the real `agy` 1.4.3 binary.

Two defects were found in the same path:

1. `ctrl+o` only re-rendered `m.blocks` — the blocks of **finished** turns.
   The in-flight turn's steps are plain rows in `m.response` until
   `turnDoneMsg` builds a `turnBlock`, so pressing ctrl+o mid-stream flipped
   `m.expanded` and changed nothing visible. The detail appeared only when the
   turn ended (or when a *later* step was painted with the new flag).
2. `turnDoneMsg` finalised the live range in two passes: replace
   `[liveStart, len)` with the block, then replace `[replyStart, len)` with
   the glamour-rendered reply. When the model streamed text between two tool
   calls, `replyStart` pointed *inside* the block, so the second wipe ate every
   step that followed it. The group header said `● 2 commands, 1 tool` while
   only the first command was rendered **at all** — not just unexpandable,
   missing.

## Evidence method

Byte-level pty harness (200×40, `TERM=xterm-256color`, `COLORTERM=truecolor`),
replayed through `pyte`; the same method as cycle 283. Raw bytes, not
`tmux capture-pane -e` (which quantises 24-bit SGR into 256-colour indices).

New this cycle: `/tmp/ptyreact.py` sends the prompt, then presses ctrl+o **the
moment the collapsed hint appears in the byte stream**, and records whether the
status row still said `esc to cancel` / `Generating…` at that instant. Fixed
sleeps were useless here — MiniMax's first-token latency swings between ~2 s
and ~30 s, so a scripted keypress lands either before the step exists or after
the turn ended.

## First-hand evidence

### The reference does expand mid-stream

`/tmp/ref_ctrlo_live.bin` — real `agy` 1.4.3 through `/tmp/agy-wrapper`
(MiniMax M3), two commands with a slow second one:

```
{"t": 16.08, "event": "ctrl+o", "streaming": true}
{"t": 16.09, "event": "collapsed"}
```

The collapsed hint appeared, ctrl+o was sent while `streaming` was still true,
and 10 ms later the frame carried `(ctrl+o to collapse)` plus the detail rows
(`Command exited with code 0.` / `Command output:` / `one`). The reference's
expanded single step keeps the hint **on the head line and again at the end of
the last detail line**:

```
● Bash (echo one) (ctrl+o to collapse)
  ⎿  ...
     Command output:
     one (ctrl+o to collapse)
```

The clone already renders that shape, so no change was needed there.

### The clone before this cycle

`/tmp/rep_ctrlo3.bin` — old build, prompt + Enter at t=7/12, ctrl+o at t=20
(second step `sleep 20` was running):

| offset | frame |
|---|---|
| 244 | `(ctrl+o to expand)` — the first step, painted collapsed |
| 973 | `(ctrl+o to collapse)` — **only when the second step finished (~t=38)** |

Nothing between them: the keypress was consumed, `m.expanded` flipped, and the
step on screen stayed collapsed for the remaining 18 s of the turn.

The same capture's final screen shows defect 2:

```
● 2 commands, 1 tool
▸ Thought for 26.6s, 1 tokens
  ⎿  ...
● Bash (echo one)
  ⎿  Created At: … / Completed At: … / The command exited with code 0.
  Both commands ran successfully — output one, then after a 20s sleep, two.
```

`Bash (sleep 20; echo two)` is gone; its rows were replaced by the reply.
`/tmp/fix2.bin` (same scenario) reproduces it.

### The clone after this cycle

`/tmp/react1.bin` — rebuilt clone, one command:

```
{"t": 15.29, "event": "ctrl+o", "streaming": true}
{"t": 15.30, "event": "collapsed"}
```

Final screen:

```
● Bash (echo one) (ctrl+o to collapse)
  ⎿  Created At: … / Completed At: …
     The command exited with code 0.
     Output:
     one (ctrl+o to collapse)
  DONE
```

`/tmp/new2.bin` — two commands with text between them, ctrl+o at t=12.04
(`streaming: true`). Final block keeps **both** steps and the expanded detail:

```
● 2 commands, 1 tool
▸ Thought for 2.1s, 1 tokens
● Bash (echo one)
  ⎿  … Output:
     one
● Bash (echo two)
  ⎿  … Output:
     two (ctrl+o to collapse)
  I'll run that sequence for you.The output was one.DONE
```

`/tmp/react3.bin` re-checks the `printf "a\r\nb\n"` case (cycle 277's CRLF
shape) with the expanded live step: `a` and `b` render on their own rows.

## Code

| File | Change |
|---|---|
| `cmd/wagyu/tui_tools.go` | new `liveStepRun{start, lines, step}` — transcript coordinates for one in-flight step |
| `cmd/wagyu/tui_run.go` `toolStepMsg` | records the run before appending the rows (same render call as before) |
| `cmd/wagyu/tui_run.go` `rerenderLiveSteps` | walks the runs newest-first, re-renders each with `toolStepLines(step, m.expanded, m.width)`, shifts later runs' `start` and `replyStart` by the row delta; drops a run whose range no longer exists instead of editing the wrong rows |
| `cmd/wagyu/tui_run.go` ctrl+o | calls `rerenderLiveSteps()` after `rerenderBlocks()` |
| `cmd/wagyu/tui_run.go` `turnDoneMsg` | renders the reply **first**, then finalises `[liveStart, len)` in a single replace of `block + reply`; `m.replyStart` is no longer used as a splice point at turn end |
| `cmd/wagyu/tui_run.go` | clears `liveStepRuns` on submit, turn end, and queued-turn flush |

Newest-first iteration is what makes the index arithmetic safe: replacing the
last run cannot move an earlier one, and each replacement shifts only the
runs after it plus `replyStart` (which sits after the block in the normal
step-then-reply order, or between runs when text streamed mid-turn).

## Tests

`cmd/wagyu/ctrl_o_live_test.go` (new):

| Test | Pins |
|---|---|
| `TestCtrlOExpandsLiveStepDuringStreaming` | a painted-but-unfinalised step expands on ctrl+o, collapses on a second press, row count returns to 2 |
| `TestCtrlOTogglesLiveStepsAroundAStreamedChunk` | expanding the first of two runs shifts the second run's start and `replyStart`; collapse restores the original row count exactly |
| `TestTurnDoneKeepsStepsThatFollowAStreamedChunk` | turn end keeps a step painted after the streamed chunk, and the reply lands after the block |

`go build -o /tmp/wagyu ./cmd/wagyu` clean; `go test ./cmd/... ./internal/...`
green (0 FAIL). `gofmt -l` clean for the three touched files.

## 硬约束

- The reference is `agy` 1.4.3, launched through the `cmd/agy` wrapper
  (`/tmp/agy-wrapper`) on **MiniMax M3**; the Google path is still
  location-blocked for this account, so the model backend differs from the
  clone's only in name — the TUI under test is the real binary either way.
- Live captures are 200×40, `TERM=xterm-256color`, `COLORTERM=truecolor`, and
  the clone binary is this commit's `/tmp/wagyu`.
- Timing claims ("mid-stream") are backed by the harness's `streaming` flag,
  computed from the bytes on screen at keypress time (`esc to cancel` /
  `Generating…`), plus the 10 ms hint flip in the same stream.

## 存疑

- **The reference paints a provisional row while a tool runs** —
  `● Running (echo one) (ctrl+o to expand)`, rewritten in place to
  `● Bash (echo one)` on completion. The clone still only names the running
  tool on the spinner line (`… Bash (echo one)`) and appends the transcript row
  when the call finishes. This is the next visible streaming difference; it is
  **not** implemented here. ctrl+o cannot expand a `Running` row (there is no
  detail yet), so this gap does not affect the fix above.
- **Text streamed before a tool call is re-ordered at turn end.** The
  reference keeps it above the block (its own screen shows assistant prose →
  tool block → assistant prose); the clone now always renders block → full
  reply. That is deterministic and loses nothing, but it differs when the
  model speaks before calling a tool. Pre-existing (the old code lost the
  steps in that case); left as-is to keep this cycle small.
- `msg.reply` concatenates text from successive tool iterations without a
  separator (`I'll run that sequence for you.The output was one.DONE`). Not
  touched; it is how the agent accumulates `turnDoneMsg.reply`.
- Expanded-hint duplication (head + last detail line) was **checked against
  the reference and matches** — not a defect.
- `alt+m` / "send immediately" is still the cycle-244 queue-mode toggle, not
  the reference's immediate-send. Unchanged from cycle 283.

## limitations

- `pyte` does not understand the kitty keyboard protocol (`CSI > 1 u` /
  `CSI < u`), so replayed screens show a stray `1u` where the input row is.
  Both binaries emit it; it is a replay artefact, not application output.
- The reference capture `/tmp/ref_ctrlo_live.bin` was still generating at the
  75 s cutoff (a 15 s sleep landed after a long think), so its **final** block
  could not be compared row-for-row; only the mid-stream expansion was.
- A single live run per scenario; model/tool latency is not deterministic.
  The three unit tests pin the invariants independently of the backend.
