# Cycle 174 — In-app text selection (drop the cycle-166 mouse toggle)

## Goal

User feedback: "原版的 agy 根本就没有 /mouse 这个命令，而且它同时支持 wheel
scroll 和 mouse select + 自动 copy 你要 1:1 复刻". The reference `agy` binary
supports both wheel-scroll AND mouse select-and-copy simultaneously with no
runtime toggle. Cycle 166's `--mouse`/`--no-mouse`/`/mouse` infrastructure was a
cycle-166-only compromise (a way to opt out of in-app selection when the
terminal-native select was wanted instead); cycle 174 closes the gap by adding
the in-app selection path itself, after which the toggle is no longer needed
and the user can simply drag to copy.

The single deliverable: drop the cycle-166 toggle and implement the in-app
mouse-down → drag → release-to-clipboard flow, matching the reference binary.

## What was removed (the cycle-166 toggle)

| Where | What |
|---|---|
| `cmd/wagyu/slash_commands.go` | `case "mouse":` block that toggled `m.mouseEnabled` at runtime and emitted "Mouse on / Mouse off" feedback. |
| `internal/cli/parser_specs.go` | `--mouse` and `--no-mouse` flag definitions. |
| `cmd/wagyu/tui_run.go` (struct) | `mouseEnabled bool` field on `runMode`. |
| `cmd/wagyu/tui_run.go` (`NewRunMode`) | the trailing `mouseEnabled bool` parameter. |
| `cmd/wagyu/tui_run.go` (`View()`) | the `if m.altScreen && m.mouseEnabled { v.MouseMode = … }` guard — replaced with unconditional `if m.altScreen { v.MouseMode = tea.MouseModeCellMotion }`. |
| `cmd/wagyu/tui_run.go` (`main`) | the `--no-mouse overrides --mouse` flag-resolution block. |
| 11 × `*_test.go` under `cmd/wagyu/` | every `NewRunMode(..., true, true)` call shortened to `NewRunMode(..., true)`. |
| `cmd/wagyu/tui_run_test.go` | the cycle-166 `Test_MouseMode` (which asserted `mouseEnabled` toggled MouseMode) replaced with `Test_MouseModeConstant` (asserts the now-constant behaviour). |
| `cmd/wagyu/tui_tools_test.go` | one stray `toolBlockLines(steps,)` (trailing comma from the cycle-166-era arglist surgery) replaced with `toolBlockLines(steps, true)`. |

Two pre-existing signature changes (from cycles 96/165) that surfaced as
not-enough-arguments errors during the NewRunMode surgery were also repaired:
`warnAgentMode(fs)` (× 2 in `cmd/wagyu/main.go`) → `warnAgentMode(fs, false)`,
and `add(...)` in `cmd/wagyu/print_slash.go:627` → `add(..., true)`. These
were latent in main since before cycle 174 but only became build errors when
this cycle moved `NewRunMode` to one fewer arg.

## What was added (the in-app selection)

| Where | What |
|---|---|
| `cmd/wagyu/tui_run.go` (struct) | `selDragging bool`, `selStartRow`, `selStartCol`, `selEndRow`, `selEndCol int` on `runMode`. |
| `cmd/wagyu/tui_run.go` (`Update()`) | three new cases: `tea.MouseClickMsg`, `tea.MouseMotionMsg`, `tea.MouseReleaseMsg` — placed BEFORE the existing `tea.MouseMsg` umbrella so the concrete types get the events first. |
| `cmd/wagyu/tui_run.go` (`View()`) | one line: `transcript = m.selectionPass(transcript)` between `transcriptWindow()` and `viewBody()`. |
| `cmd/wagyu/tui_selection.go` (NEW, 174 lines) | the selection logic: `transcriptCellAt`, `selectionRange`, `selectionText`, `copySelectionCmd`, `applySelectionHighlight`, `selectionPass`, plus the `\x1b[48;5;240m…\x1b[49m` highlight escape constants. |
| `cmd/wagyu/tui_selection_test.go` (NEW, 199 lines) | six new tests covering drag start/motion/release, copy-text, reverse-drag normalization, highlight rendering, click-without-drag, and the `/mouse` command's removal. |

### The flow

```
MouseClickMsg (left button only, on a cell inside the viewport)
  └→ selDragging = true
  └→ selStartRow/Col = selEndRow/Col = (Y, X)

MouseMotionMsg (only consumed while selDragging)
  └→ selEndRow/Col = (Y, X)

MouseReleaseMsg
  └→ selDragging = false
  └→ text := selectionText()  // joins [start..end] rows with \n, ANSI-stripped
  └→ selStartRow/Col/EndRow/EndCol = 0
  └→ if text != "" → return m, tea.SetClipboard(text)  // OSC 52
```

### The highlight

`selectionPass` walks each visible transcript row (already produced by
`transcriptWindow()` and therefore already padded to `m.width` cells) and
applies `applySelectionHighlight` row-by-row. The implementation uses
`ansi.Cut(s, from, to)` to slice by display cell (which understands
embedded SGR sequences and skips them in the cell count), wraps the selected
slice in `\x1b[48;5;240m` (grey30 background) / `\x1b[49m` (bg reset), and
concatenates with the original prefix/suffix. The original SGR state
(foreground colour, intensity, etc.) on either side of the highlight is
preserved because the prefix/suffix come straight from the source row.

`selectionText()` uses `ansi.Strip` rather than `ansi.Cut` to produce the
plain-text payload: ANSI escapes don't belong in clipboard text. Newlines
join rows so a multi-row drag yields a multi-line value.

### The clipboard wire format

`tea.SetClipboard(text)` returns a `tea.Cmd` that on the next program tick
emits the OSC 52 sequence `\x1b]52;c;<base64>\x07`. Modern terminals
(xterm, iTerm, kitty, Alacritty) decode the base64 and write it to the
system clipboard. This is the same mechanism `crush`'s
`internal/ui/common/common.go:CopyToClipboardWithCallback` uses — except
crush additionally calls `clipboard.WriteText(text)` first for native
clipboard access (X11/Wayland/macOS CGO path). The clone doesn't pull in
`atotto/clipboard`; OSC 52 is sufficient on every terminal that supports
mouse tracking in the first place, and the cycle-167/168 work deliberately
stays within the existing dependency surface.

## Why drop the toggle entirely (not keep /mouse off as a fallback)

The cycle-166 toggle was added because "shift/alt+click terminal-native
select" was the only documented way to copy text under the old
DECSET-1002-only-in-alt-screen setup. With in-app selection working, the
toggle becomes a footgun: `/mouse off` removes wheel scroll AND in-app
selection simultaneously, leaving the user with **no** way to copy text
from the transcript at all. The user explicitly asked for the toggle to
go away, not for it to be preserved as an alternative path.

Terminals that don't honour shift/alt+click under DECSET 1006 (tmux on
older xterms, some embedded terminals) get the same behaviour the
reference binary users get there: only in-app selection. Users on those
terminals would have to `Ctrl+L` to drop out of alt-screen to use
terminal-native selection. This matches the documented reference
behaviour.

## Tests added

| Test | What it pins |
|---|---|
| `Test_MouseModeConstant` | `MouseMode = CellMotion` whenever alt-screen is on, regardless of any toggle (there isn't one). |
| `Test_SelectionMouseDownMotionRelease` | Click → state `(2,3)..(2,3)` dragging; Motion → `(2,3)..(2,10)`; Release → OSC 52 cmd + state cleared; subsequent View has no highlight. |
| `Test_SelectionCopyText` | Single-row drag yields the row's plain-text slice; multi-row drag contains `\n`. |
| `Test_SelectionReverseDrag` | A drag from (20,3) to (5,1) is normalized to (1,5)..(3,20) so the highlight paints the right rectangle. |
| `Test_SelectionHighlightInView` | With `selDragging=true` and a (2,5)..(4,15) range, View contains ≥3 `selHighlightOpen` substrings (one per row). |
| `Test_SelectionClickWithoutDrag` | Click → Release at the same cell returns nil cmd (no OSC 52 for a zero-byte copy) and leaves no highlight. |
| `Test_MouseSlashCommandRemoved` | `/mouse off`, `/mouse on`, `/mouse toggle` all hit the cycle-99 default branch (no "Mouse on/off" feedback leaks into `m.response`); `MouseMode` is unaffected. |

All 7 new tests pass. The pre-existing `TestChangelog_MatchesReferenceBinary`
failure (3232-byte drift on main, not caused by this cycle) is unchanged.

## Files changed in this cycle

```
M  cmd/wagyu/tui_run.go            - 119 / -59 lines (struct, View, Update)
A  cmd/wagyu/tui_selection.go      - 174 lines (new)
A  cmd/wagyu/tui_selection_test.go - 199 lines (new)
M  cmd/wagyu/tui_run_test.go       - 33 / -42 lines (Test_MouseMode → Test_MouseModeConstant)
M  cmd/wagyu/slash_commands.go     - 25 lines (mouse case removed)
M  cmd/wagyu/tui_tools_test.go     - 1 line (trailing-comma fix)
M  internal/cli/parser_specs.go    - 7 lines (mouse/no-mouse flags removed)
M  cmd/wagyu/main.go               - 5 lines (warnAgentMode callsite fixes)
M  cmd/wagyu/print_slash.go        - 1 line (add() callsite fix)
M  11 × cmd/wagyu/*_test.go        - arglist surgery (each call shortened by one bool)
```

## Open / Limitations

- **Native clipboard path not wired.** The OSC 52 path covers every terminal
  that supports mouse tracking (the only terminals anyone uses for `agy`
  today). Adding `atotto/clipboard` for a native CGO path is doable but
  contradicts the cycle-167/168 dependency surface. Skip until asked.
- **Selection is row-wise, not column-wise through the wrapped transcript.**
  `transcriptWindow()` returns rows already padded to `m.width` cells, but
  a long wrapped response line can occupy multiple physical screen rows.
  The clone treats each physical row independently — a drag from row 5
  col 0 to row 8 col 40 selects all three intermediate rows in full,
  which means the user's selected text may include cells from the
  *previous* wrapped logical line on row 5 if that line wraps.
  Reference behaviour is not recovered (the reference binary's source is
  not available); visual inspection in iTerm against `~/.local/bin/agy`
  1.4.0 suggests it does NOT include the wrap-leftover cells, but pinning
  that would require either testing on a synthetic wide terminal or
  reading the reference binary's selection math, neither of which is
  in-scope this cycle. Mark as 存疑.
- **No multi-byte grapheme awareness.** A drag from inside a CJK
  character to outside it could split the rune. `ansi.Cut` operates on
  grapheme clusters when possible, so the highlight is correct, but the
  copied text may still split a CJK pair across rows. Same caveat as
  the reference; mark as 存疑.
- **Pasting the highlighted ANSI escapes.** `selectionText()` calls
  `ansi.Strip` so the clipboard payload is plain text. The visual
  highlight on screen still carries SGR, but it never reaches the
  clipboard. This is the desired behaviour for copy-on-select and
  matches the reference binary.
- **No shift+click / alt+click modifier handling.** The reference binary
  may distinguish modifiers (e.g. shift+click = terminal-native select
  through to the host). The clone does not; any left-click starts an
  in-app selection. With cycle-166's toggle gone there's no path back
  to terminal-native selection. If the user reports this as a
  regression we'll add the modifier dispatch next cycle.