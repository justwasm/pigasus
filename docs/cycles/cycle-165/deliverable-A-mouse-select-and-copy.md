# Cycle 165 — re-enable terminal-level select-and-copy (drop MouseModeCellMotion)

## Problem

User reports the clone no longer supports "select and copy" — every click is
intercepted by the TUI, so dragging to select text in tmux/iTerm does
nothing. Reference binary takes the documented trade-off:
**"shift/alt+click Select and copy text"**.

The cycle-119 commit enabled `tea.MouseModeCellMotion` (DECSET 1006) so the
viewport could receive wheel events. The side effect was that every click —
including shift/alt/cmd-modified clicks — became an application event, and the
terminal never saw them. Some terminals (xterm, kitty) still pass
shift+click through even with 1006 enabled; most don't. tmux falls in
the "most" bucket, so selection was effectively dead in our usage.

## Fix

`cmd/wagyu/tui_v135.go` — drop the MouseMode assignment entirely. Without
DECSET 1006 the terminal handles selection normally; the trade-off is that
the wheel no longer scrolls the transcript inside the alt-screen buffer.
That matches the reference's documented behaviour and what the user
explicitly asked for.

```go
v := tea.NewView(body)
v.AltScreen = m.altScreen
// Cycle 119 / cycle 165: disable mouse tracking so the user can
// select and copy text in the terminal (shift/alt+click works in
// most terminals, including tmux and iTerm, when DECSET 1006 is
// off). The reference binary takes the same trade-off — wheel
// scroll is lost but selection works. Re-enabling MouseMode here
// would force every click into the application and block
// terminal-level selection entirely.
_ = m.altScreen
```

## Verification

`tmux capture-pane -p -e` of the running binary shows none of the
mouse-tracking escape sequences:
- `[?1000h` — off
- `[?1002h` — off
- `[?1003h` — off
- `[?1006h` — off

User confirmation (manual): shift+click in tmux now selects text and the
terminal copy-paste works. cmd+click in iTerm works.

## Cost

Lost feature: wheel scrolling inside the alt-screen buffer. The
viewport's `AltScreenStrategy.ShouldHandleWheel` will report `false`,
so up/down scroll keys are still the only way to traverse the
transcript in alt-screen mode. That matches the reference.

If wheel scroll is needed in a future cycle, the alternative is to
re-enable `MouseModeCellMotion` AND install a "release-on-shift/alt"
handler — a few terminals honour the modifier even with 1006 enabled,
but most do not, so the test matrix is wide. Not pursued here.

## Tests

`go test ./cmd/... ./internal/...` — all green (no test changes).

## Out of scope

- Cycle 164 (trailing-whitespace padding) is still in progress; the
  `padTo` / `fitWidth` colored-space experiment was reverted in this
  commit so cycle 165 can land clean. Cycle 164 will be picked back up
  next.