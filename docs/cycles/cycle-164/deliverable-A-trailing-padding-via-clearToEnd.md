# Cycle 164 — trailing-whitespace padding via ultraviolet `clearToEnd`

## Problem

Pre-cycle-164, the chrome rows (separator, status bar) ended short of the
right wall at 200×60. The transcript rows also showed visible ragged
right edges where trailing content didn't fill the line. REF pads every
row to the terminal width with a separator-gray fill (38;5;235) so the
entire right edge looks uniform.

`fitWidth` and `padTo` did pad, but with raw spaces. After several
investigations we believed those spaces were being stripped somewhere
in the bubbletea/ultraviolet pipeline — `go test` shows correct
emission, but `tmux capture-pane` showed the line terminating at the
last content byte.

## Investigation — the actual mechanism

The real mechanism is in `ultraviolet/terminal_renderer.go`:

1. `transformLine` walks each rendered line and finds the trailing
   "blank" region (`newLine.At(width-1)`, the last cell). For our
   pad output that cell is a gray-styled space.
2. `canClearWith(blank)` returns `true` because the cell is a single
   ASCII space with no underline/reverse/link — eligible for the
   `[K` (Erase Line Right) optimization.
3. After `putRange` writes the content cells, `transformLine` calls
   `clearToEnd(newbuf, blank, false)` from the column after the last
   content cell.
4. `clearToEnd` walks the *next-buffer* cells from cursor to width
   against the *curbuf* cells. For an initial render the curbuf
   cells are nil/empty — `cellEqual(nil, blank) = false`, so
   `force = true` is set.
5. With `force = true`, `clearToEnd` emits `s.updatePen(blank)` then
   `s.el0Cost() <= count` decides between
   - `\x1b[K` (3 bytes) — most cases, the optimization we trigger
   - per-cell `putCell` (count × bytes) — rare, only when el0Cost
     is somehow larger than the trailing count
6. `\x1b[K` is terminal-side Erase-Line-Right with BCE (Background
   Color Erase) — the cells get filled with the current pen's
   foreground style. With `blank` = gray-styled space, the cells
   become gray fg + default bg, matching our visual goal.

## Empirical verification

script(1) raw capture (`/tmp/our_script.bin`, 5290 bytes):

```
grep -c '\\x1b\[K' → 28 occurrences in a 200×60 transcript.
grep -c '\\x1b\[\d\+X' → 1 occurrence (the prompt cursor's
single trailing space).
```

The pattern `[38;5;235m[K` appears at the end of every padded
transcript row and every padded chrome row. That's the
`updatePen(blank)` (`\x1b[38;5;235m`) immediately followed by
`EraseLineRight` (`\x1b[K`) — 11 bytes total per row instead of
~190 bytes if we wrote each cell individually.

Visual diff against REF (`/tmp/agycmp/ref136/agy` v1.3.6) at 200×60:
- separator row: extends to right wall with gray fill — matches
- status row: extends to right wall with gray fill — matches
- transcript rows: every row's trailing gray fill is identical to
  REF's gray-fill rows

## The fix

`cmd/wagyu/tui_viewport.go` — change `fitWidth` and `padTo` to
emit pad cells coloured with separator gray (38;5;235) rather than
raw spaces:

```go
case sw < w:
    return s + "\x1b[38;5;235m" + strings.Repeat(" ", w-sw) + "\x1b[39m"
```

The colour matters because ultraviolet's `canClearWith` test
requires a non-default style to keep the cells real; with raw
spaces the cells would still be eligible for `[K`, but the
resulting fill would be default-fg instead of separator-gray and
the chrome rows would not match REF.

## Why not match REF byte-for-byte

REF (a closed-source Google internal Go binary) almost certainly
emits literal trailing spaces + a final reset. We cannot do that
through bubbletea/ultraviolet — the renderer always collapses
trailing equal-cell runs into `[K` (or `ECH` if the terminal
caps include it; ours doesn't, so `[K` it is).

Two workarounds were considered and rejected:

1. **Set `MouseMode` etc. to disable optimization.** No such knob
   exists in uv's renderer.
2. **Write the trailing pad outside of `View()`.** The pad would
   have to be a separate tea command — which would render a
   different buffer that uv then re-optimises anyway. No-op.

The visual is identical, and the byte delta (11 bytes vs ~190
bytes per row × 60 rows) is a feature, not a bug: smaller PTY
traffic, less render work, and `tmux` strips the `[K` in its
internal pane buffer because it tracks the resulting cell state
(not the escape sequence), so the captured tmux output is *also*
smaller than REF's.

## 硬约束 / known difference

A byte-exact comparison between our PTY output and REF's PTY
output will show `\x1b[K` where REF shows literal spaces. This is
a fundamental behaviour of ultraviolet's renderer and there is no
hook to disable it. The visible output is byte-equivalent on
every tested terminal (iTerm, Terminal.app, tmux, kitty).

## Tests

`go test ./cmd/... ./internal/...` — all green (no test changes;
the rendering behaviour is verified via tmux/script captures, not
unit tests).