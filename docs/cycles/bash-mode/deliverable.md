# Cycle `bash-mode` — `!` runs the shell, not the model

**Status**: implemented, `cmd/wagyu` green
**Reference**: `~/.local/bin/agy` 1.4.4, byte-level pty captures on 2026-10-22
**Evidence**: `captures.md` in this directory (raw escape sequences, no tokens)

## The gap

The clone treated every submit as a model prompt, so

```
> !pwd
```

turned into a model turn and the reference-shaped tool block

```
● Ran (pwd) (ctrl+o to expand)

  /Users/gear/GitHub/openagy
```

The reference never calls the model for this. `!` at the start of an empty composer
arms **bash mode**, and Enter runs the buffer through the shell:

```
● ! pwd
  ⎿  /Users/gear/GitHub/openagy
```

## The measured contract

| Behaviour | Evidence |
|---|---|
| `!` on an **empty** composer arms the mode; the key is consumed and the composer paints a bold-blue `!` + space | `\x1b[38;2;38;139;210;1m!\x1b[m ` |
| The hint row becomes ` activated bash mode · esc to cancel` (leading space, `#657B83`, U+00B7) | captures §2 |
| ` !echo hi` (leading space) is an ordinary prompt — the mode only arms on an empty buffer | capture §7 |
| Enter with an empty command leaves the mode and prints nothing | capture §3 |
| Esc leaves the mode and clears the buffer | capture §6 |
| The command runs in the workspace through the platform shell (`zsh -c` on darwin) | `DefaultShellArgv`, same as `run_command` |
| stdout and stderr are merged; trailing newlines dropped | `printf 'x\ny\n'` → two rows |
| Empty output prints **no** `⎿` row | `!true` |
| A failed command **with** output prints only that output — no status line | `sh -c 'echo out; echo err >&2; exit 3'` |
| A failed command **without** output prints the Go error text | `exit status 3` |
| Success: green `●`, body `#586E75`; failure: red `●`, body `#DC322F`; the `  ⎿  ` prefix is bold blue in both | capture §1/§4/§5 |
| It is a local turn: spinner `Generating...`, status `esc to cancel`, Esc cancels it | capture §8 |
| A cancelled command keeps its head row, no body, **green** bullet | capture §8 |
| Submitted mid-turn: queued above the input as `▸ <cmd>`, runs when the turn ends | capture §9 |
| Up-arrow recalls a command back into bash mode | capture §10 |

The full transcript block, byte for byte (success):

```
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mhello\x1b[m
```

Failure:

```
\x1b[38;2;220;50;47m●\x1b[m \x1b[38;2;38;139;210;1m! sh -c '…; exit 3'\x1b[m
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;220;50;47;22mout\x1b[m
\x1b[38;2;220;50;47merr\x1b[m
```

The first body row carries `;22` (normal intensity); continuation rows do not. Only
the first row is indented — continuation lines start at column 0.

## What changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_bash_mode.go` (new) | `bashHeadRow`, `bashOutputRows`, `bashPromptGlyph`, `startBashCommand`, `execBashCommand`, `finishBashCommand`, `cancelBashCommand`, `applyHistoryEntry`, `bashDoneMsg` |
| `cmd/wagyu/tui_run.go` | `bashMode`/`bashRowIdx`/`bashCancel`/`bashQueue` fields; the `!` and Backspace key intercepts; the `submitPrompt` branch; Esc exit/cancel; the status-row label; the composer prompt glyph; queued-command rows; the `bashDoneMsg` handler; the turn-end flush |
| `cmd/wagyu/history.go` | recall goes through `applyHistoryEntry` so a `!…` entry re-arms the mode |
| `cmd/wagyu/tui_bash_mode_test.go` (new) | head/body byte tests, exec shapes (success, cwd, failure with/without output, cancel), key arming, local submit, empty submit, cancel, history recall, streaming queue |

The mode is a composer state, not a prefix in the buffer: typing `!` when the
buffer is empty consumes the key, so `Enter` runs `m.input.Value()` directly and the
prompt glyph is drawn by `SetPromptFunc` (with the same reset-move as the `>` glyph
so only the `!` is bold).

Queued commands live in `m.bashQueue`, not `m.streamQueue`: they render without the
`(alt+m to send immediately)` suffix the reference adds to queued *prompts*, and
`turnDoneMsg` flushes one per turn end.

## 硬约束

- The block rows, the composer glyph, the hint row, the queue row, the status labels
  and the cancel behaviour are copied from captured escape sequences; the tests pin
  them.
- The command never touches `m.session` and never reaches the model. Test
  `TestBashModeSubmitRunsLocally` asserts the session stays empty.
- `!` only arms on an empty buffer; a leading space keeps the old prompt path.

## 存疑

- **Persistence.** The reference registers the command as an implicit trajectory in
  the conversation ("OnShellCommandFinished: updating %d active implicit
  trajectories", 【strings】), so `agy -c` presumably replays it. The clone's
  `m.session` is model-only, so a resumed conversation will not show past shell
  blocks. Not modelled — it would need a new conversation-store row kind.
- **Queued-command ordering** against a queued prompt: the clone runs the prompt
  first (the `streamQueue` head wins) and flushes a shell command only when no
  prompt is queued. The reference's `Auto-flush: dispatching queued shell command`
  log line does not tell us the interleaving.
- **Output encoding.** The block prints the merged output verbatim (no ANSI strip,
  no pty). `run_command` uses a pty on stdin and strips SGR; bash mode's output
  treatment beyond the captured plain-text cases is unmeasured. A command that
  colourises its output will show the escapes.
- **`Start your message with ! to run a shell command.`** exists in the reference's
  string table but does not appear in the `/help` panel (the shortcuts tab lists
  34 items, none of them `!`; the general tab's Quick Reference lists only `/`).
  Where the tip surfaces (announcement card? first-launch hint?) is unmeasured, so
  the clone does not print it.

## limitations

- One terminal size (120×40) and one account. The block layout does not depend on
  width in the captured cases, but wrapping of long output rows was not measured.
- Cancellation is exercised with `sleep`; a command that ignores SIGKILL is
  impossible, but a command that traps SIGTERM is not tested (`exec.CommandContext`
  sends SIGKILL).
- Windows behaviour (`powershell -Command`) is inherited from
  `tools.DefaultShellArgv` and was not run.
- The reference's two-queue "Pending inputs cancelled (%d regular + %d shell
  dropped)" on `/clear` is not modelled: `bashQueue` survives a `/clear` until the
  turn-end flush.
