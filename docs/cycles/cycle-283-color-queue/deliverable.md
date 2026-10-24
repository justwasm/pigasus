# Cycle 283 — Solarized palette, live mode cue, and prompt queueing mid-turn

The 1.4.x reference runs on a **Solarized** palette selected by
`settings.json`'s `colorScheme` (`"solarized light"` on this machine), while the
clone still shipped the 1.3.x Google-dark RGB set. Measured in tmux the two
diverge everywhere the palette is used (69/241/253 instead of 32/66/254), and the
difference is not a tmux artefact: the reference writes **24-bit RGB** and lets
the terminal quantise, the clone wrote 24-bit RGB of a *different* colour.

This cycle replaces the palette with the reference's, makes the user prompt row
bold blue, moves the mode cue into the input placeholder, and closes two
streaming gaps the A/B exposed: the status row's `esc to cancel`, and Enter
while a turn is in flight (the clone dropped the prompt; the reference queues it
above the input and sends it after the turn).

Everything below was verified against the **real** `agy` 1.4.3 binary through a
pty capture harness, not against a rendered guess. Where the clone deliberately
stops short of the reference, the deviation is listed in **存疑 / limitations**.

## Evidence method

All colour facts come from raw pty bytes captured with `COLORTERM=truecolor`
(`TERM=xterm-256color`), then replayed through `pyte` at 200×40 to recover the
screen. tmux's `capture-pane -e` was **not** used for colour: it rewrites
24-bit SGR into 256-colour indices, which is exactly the mistake that made the
cycle look like a "different palette" rather than "different colour values".

| Capture | What it pins |
|---|---|
| `/tmp/ref_true.bin` | idle screen: banner, separators, `? for shortcuts`, mode + model |
| `/tmp/ref_conv2.bin` | user row, body prose, markdown link label/URL |
| `/tmp/ref_tool_true.bin` | tool step: bullet, verb, args, hint, result line |
| `/tmp/ref_pick_true.bin` | `/resume` picker: placeholder, keyboard row, tabs, footer |
| `/tmp/ref_slash.bin` | `/` picker: selected row, descriptions, `↓ N more`, hint |
| `/tmp/ref_queue.bin` | Enter during a turn: pending line, second separator, status label |
| `/tmp/cl_queue3.bin` | the clone's matching pending-queue frame after this cycle |
| `/tmp/cl_conv.bin` | the clone's link/body/user-row bytes after this cycle |

`/tmp/ptycap.py` (capture + scripted keys) and `/tmp/replay.py` (pyte screen
reconstruction) are throwaway harnesses; the byte spans that matter are quoted
inline below so the report stands on its own.

## Palette (【一手】)

The reference emits **one SGR** per segment, colour and attribute combined
(`\x1b[38;2;38;139;210;1m`), not two sequences. Recovered roles:

| Role | Hex | SGR | Where |
|---|---|---|---|
| blue | `#268BD2` | `38;2;38;139;210` | prompt `>`, picker keys, selected row, active tab, plan mode, user row (bold) |
| base00 | `#657B83` | `38;2;101;123;131` | status row both halves, model, cwd, picker labels, queue line, `esc to cancel` |
| base01 | `#586E75` | `38;2;88;110;117` | picker descriptions, `↓ N more`, tool args + hints, search placeholder |
| base2 | `#EEE8D5` | `38;2;238;232;213` | both separators, inactive help tabs |
| base02 | `#073642` | `38;2;7;54;66` | assistant body prose, list text, table cells |
| orange | `#CB4B16` | `38;2;203;75;22` | `auto` mode |
| green | `#859900` | `38;2;133;153;0` | `accept-edits` mode, success bullet `●`, diff additions |
| yellow | `#B58900` | `38;2;181;137;0` | running bullet `○` + the verb (`Running`, `Ran`) |
| red | `#DC322F` | `38;2;220;50;47` | failure bullet/verb, diff removals |
| link label | index 29 | `38;5;29;1` | **fixed 256 index even on truecolour** |
| link URL | index 36 | `38;5;36;4` | fixed 256 index + underline |

Two facts that are easy to get wrong:

- The reference **mixes** RGB and 256 indices. Chrome and body are RGB; the
  markdown link colours are plain 256 indices (29/36). That is glamour's
  Solarized style, not a terminal limitation — the same capture emits
  `38;2;7;54;66` for the body and `38;5;29;1` for the label two bytes later.
- The mode prefix ends with the dim colour in the *same* run as the separator:
  `\x1b[38;2;203;75;22mauto\x1b[38;2;101;123;131m · MiniMax M3\x1b[m`.
  The clone now emits exactly that (`cmd/wagyu/tui_run.go:statusRow`).

## User row (【一手】)

The submitted prompt is echoed as **bold blue**, not plain text:

```
\x1b[38;2;38;139;210;1m> Output exactly this and nothing else: …\x1b[0m
```

`submitPrompt` now appends `userPromptRow(prompt)` (`cmd/wagyu/tui_run.go`),
which is `cWarningBlueOpen + "> " + text + close`. After the change the clone's
bytes for the row, the body prose, the link label and the URL are identical to
the reference's (`/tmp/cl_conv.bin` vs `/tmp/ref_conv2.bin`):

```
clone    \x1b[38;2;38;139;210;1m> Output exactly this …\x1b[38;5;235;22m…
ref      \x1b[38;2;38;139;210;1m> Output exactly this …\x1b[38;5;235;22m…
body     \x1b[38;2;7;54;66mSee \x1b[38;5;29;1mthe docs\x1b[38;2;7;54;66;22m \x1b[38;5;36;4mhttps://…
```

## Mode cue → placeholder, and it is not permanent (【一手】)

1.4.x does not print a banner row for the mode. The cue is the **empty input's
placeholder** (`Auto mode: edits & safe commands auto-approved (shift+tab to
cycle)`) plus the coloured `auto · …` prefix in the status row. The clone's
cycle-282 banner echo is gone; `cycleMode` sets the placeholder and
`modeHintDismissed` clears it on the first keystroke that reaches the textarea
(clearing the buffer or submitting does not bring it back — only Shift+Tab
does).

Startup reads the mode from the store: `printModePolicy` consults
`auth.SettingsString("agentMode")` when `--mode` is absent, and the TUI uses the
same policy (`m.mode = sessionPolicy.Mode`). On this store that is `auto`, which
is why the clone now starts as `auto · MiniMax M3` like the reference.

## Streaming surfaces

### Status row shows `esc to cancel` (【一手】)

While a turn is in flight and the buffer is empty the left label is
`esc to cancel`, not `? for shortcuts`:

```
\x1b[38;2;101;123;131mesc to cancel\x1b[m
```

The clone previously reserved that label for the picker. `statusRow` now also
enters it when `m.streaming`.

### ctrl+o mid-stream already worked

Verified live with the same harness the reference was captured with
(`/tmp/cl_stream.bin`): the tool block flips to `(ctrl+o to collapse)` with its
detail rows while the turn is still running. No code change was needed —
cycle 278's re-anchor fix covers it.

### Enter during a turn queues, it does not drop (【一手】)

The clone's `submitPrompt` began with `if m.streaming { return nil }` — the
prompt was silently discarded. Replaying `/tmp/ref_queue.bin` through pyte at
the moment the queued row appears gives the reference's frame:

```
> Run the shell command sleep 8 and then reply DONE.
⣷  Generating...
────────────────────────────────────────────────────────────  ← top separator
▸ SECOND message while running (alt+m to send immediately)      ← #657B83
────────────────────────────────────────────────────────────  ← extra border
>
────────────────────────────────────────────────────────────
  Press up to edit queued messages            auto · MiniMax M3
```

and the final transcript (after the first turn finishes):

```
> Run the shell command sleep 8 and then reply DONE.
● Ran (sleep 8) (ctrl+o to expand)
  DONE

> SECOND message while running
  Noted. Ready for your next instruction.
```

So the reference: clears the input, renders one `▸ … (alt+m to send
immediately)` row per pending prompt **above the input box** with an extra
separator under it, swaps the status left label to
`  Press up to edit queued messages` (two leading spaces), and only echoes the
prompt into the transcript — as a normal bold-blue row — when the finished turn
is finalised and the next one starts.

Implementation (`cmd/wagyu/tui_run.go`):

- `streamQueue []string` — FIFO, one entry per Enter.
- `submitPrompt`'s streaming branch appends to the queue, clears the input,
  resets the picker/@-dropdown and returns. It does **not** touch `m.response`:
  echoing there would interleave the row with the live streamed reply (the
  first iteration of this patch did exactly that and the reply bled below the
  queued row).
- `viewBody` renders the pending rows + extra separator between the
  announcement slot and the prompt.
- `statusRow` gives the queue precedence over `esc to cancel`.
- `turnDoneMsg` picks `m.queuedPrompt` (cycle 244's Cmd+Enter slot) or the head
  of `streamQueue` **before** finalising, then flushes at the very end of the
  case: append `userPromptRow(q)`, set `liveStart`, append the user message to
  `m.session`, `saveSession`, `streaming = true`, and return
  `streamPrompt(q) + spinner tick`. Moving the flush to the end matters: the
  finalisation `replaceRange(liveStart, len(m.response), block)` runs over the
  same range and would eat a row appended earlier.
- `Up` on an empty buffer pops the newest queued prompt back into the input
  (the hint the status row advertises).

`/tmp/cl_queue3.bin` is the clone's frame after the change — the pending row,
the extra separator, the status label and the `auto · MiniMax M3` run are
byte-identical to the reference's (`\x1b[38;2;101;123;131m  Press up to edit
queued messages\x1b[m` … `\x1b[38;2;203;75;22mauto\x1b[38;2;101;123;131m · MiniMax M3\x1b[m`).

## Reference version bump (1.4.3)

`internal/cli/version.go` moves `ReferenceVersion` to `1.4.3`. Two fixtures
followed:

- `internal/cli/testdata/changelog_reference.txt` is now `agy changelog`
  verbatim (160,397 bytes, SHA-256 `dc224b98…`). The cycle-235 "release notes
  not yet recovered" stub is gone: 1.4.3's changelog carries the real 1.4.2
  section (`· Fixed conversation history not showing in Remote Control in
  daemon mode.`), so `TestChangelog_MatchesReferenceBinary` compares directly
  with no stub stripping, and `TestChangelogReference_142PlaceholderIsVerbatim`
  pins the recovered bullet instead of the removed placeholder.
- `Test_HelpGeneralPanelStyling` expects `Version 1.4.3`.

## Live A/B in tmux (【一手】)

Two 200×40 tmux sessions (`/tmp/agy-wrapper` and the rebuilt clone) were
captured with `tmux capture-pane -p -e` after seven seconds of idle:

```
diff /tmp/tmuxA.txt /tmp/tmuxB.txt   →   (no output)
```

Byte-identical, including the quantised SGR (`38;5;32`, `38;5;66`, `38;5;254`,
`38;5;166` …). That is the check the palette migration was for: the clone and
the reference now land on the *same* tmux indices because they start from the
same RGB values.

One caveat seen in the pty captures: the reference's banner occasionally omits
the `(Antigravity Starter Quota)` suffix from the email row when its quota
probe fails (visible in `/tmp/ref_idle2.txt` during a service outage); the tmux
run above, and every earlier capture, shows it. The clone always renders the
suffix. Not a palette issue and not changed here.

## Files

| File | Change |
|---|---|
| `cmd/wagyu/colors.go` | Solarized chrome + tool palette, `cSubtleOpen`, `statusModeColor`, tool add/remove red/green |
| `cmd/wagyu/tui_run.go` | mode-coloured status prefix, placeholder style `#657B83`, prompt `#268BD2`, `esc to cancel` while streaming, `streamQueue` + pending rows + flush + Up recall, `userPromptRow` on submit |
| `cmd/wagyu/conversation.go` | `userPromptRow` now resolves to bold blue (constant change) |
| `cmd/wagyu/picker_view.go` | `pickerDescDim` → `#586E75` |
| `cmd/wagyu/slash_panels.go`, `slash_regression.go`, others | 241/253/238/29 literals → Solarized RGB |
| `cmd/wagyu/slash_rename_panel.go` | placeholder colour moved from inline SGR to the textinput style (the 20-rune escape was being counted by `placeholderView`, truncating the text to "type a na") |
| `internal/mdrender/glamour_adapter.go` | `Document.Color` `"23"` → `"#073642"` so a truecolour terminal sees RGB, a 256 terminal sees 23 |
| `internal/cli/version.go` | `1.4.3` |
| `internal/cli/changelog_data_test.go`, `changelog_cycle_235_test.go`, `testdata/changelog_reference.txt` | regenerated for 1.4.3 |
| `cmd/wagyu/queue_stream_test.go` | 4 new tests: queue-on-Enter, pending row + status label, flush ordering, Up recall |

Test suites were updated where they pinned the 1.3.x values. `go test
./cmd/... ./internal/...` is green.

## 存疑 / limitations

- **`alt+m` "send immediately" is not implemented.** The pending row advertises
  it because the reference renders that text, but in the clone alt+m still
  toggles cycle 244's Queue mode. The reference's exact semantics (does it
  interrupt the running turn, or just dispatch the queue at the next boundary?)
  were not measured.
- **OSC 8 is still stripped from the transcript.** The reference frames both
  the link label and the URL with `\x1b]8;id=jk<16 hex>;URL\x07` … `\x1b]8;;\x07`
  on a plain pty. `internal/mdrender` deliberately removes OSC 8 (cycle 271) so
  row-width math and copy/selection stay clean, and this cycle did not reverse
  that. Visible styling is identical (29 bold label, 36 underlined URL), but the
  clone's links are not clickable. Whether the reference keeps OSC 8 under tmux
  was not measured — `capture-pane -e` drops the frames, so the tmux A/B could
  not have shown them either way.
- **The palette is not scheme-aware.** The Solarized values are now the clone's
  only palette. `colorScheme` is still ignored, so a store set to the *dark*
  scheme would render Solarized chrome. The dark-scheme RGB set could not be
  captured on this machine (a second HOME has no valid sign-in). 【推测】 that
  the reference's non-Solarized palette is the 1.3.x Google-dark set the clone
  used to ship.
- **Inline code, headings and fenced code** were not re-captured. The clone
  still emits `38;5;32`/`48;5;254` for inline code and chroma-solarized indices
  for fenced blocks; the one truecolour reply captured had no inline code.
  【推测】 that those fixed indices are correct, because the reference mixes
  fixed indices into markdown output (links prove it).
- **The queue line is truncated to the terminal width by the renderer**, not by
  this code. The reference's own row is not wrapped or ellipsised in the
  200-column capture; behaviour on a narrow terminal is unmeasured.
- `pickerKeyOpen` still emits `\x1b[1m` separately from the blue where the
  reference emits `\x1b[38;2;38;139;210;1m` (slash picker) — visually identical,
  byte order differs.
