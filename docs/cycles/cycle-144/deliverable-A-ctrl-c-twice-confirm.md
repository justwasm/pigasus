# Cycle 144 — Ctrl+C twice to exit

## Why

The v1.3.6 reference binary requires two Ctrl+C presses to exit. The
first press shows "press ctrl+c again to exit" in the status row's
left slot (replacing the "? for shortcuts" / "esc to cancel"
label). The second Ctrl+C actually terminates the program. Any
other keypress cancels the pending exit and returns to the normal
status row. Captured at /tmp/refscroll.txt:17 — the line is
byte-exact `press ctrl+c again to exit` rendered in dim gray.

The cycle-1..143 clone's Update handler at tui_v135.go:1245
returned `m, tea.Quit` on the first Ctrl+C, so a single
accidental press would terminate the program. This violates the
v1.3.6 spec and is the kind of one-keystroke data loss users complain
about in chat agents.

## What changes

### `cmd/wagyu/tui_v135.go::Update`

The Ctrl+C branch now goes through a two-step confirmation:

```
if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
    if m.exitConfirm {
        m.quitting = true
        return m, tea.Quit
    }
    m.exitConfirm = true
    m.exitConfirmAt = time.Now()
    return m, nil
}
```

Any other keypress clears `m.exitConfirm`. Ctrl+D still exits
unconditionally (matches the reference).

### `cmd/wagyu/tui_v135.go::v135StatusRow`

When `m.exitConfirm` is set, the left label swaps from
`"? for shortcuts"` to `"press ctrl+c again to exit"`. The right
model label, dim-gray colour and column-width math are unchanged.

### `cmd/wagyu/tui_v135.go::v135RunMode`

Two new fields on the run mode:

```
exitConfirm   bool
exitConfirmAt time.Time
```

`exitConfirmAt` is recorded but not consumed yet — the v1.3.6
reference doesn't auto-expire the confirmation (the user has to
either press Ctrl+C again or any other key). The field is kept so
cycle 145+ can add a 3-second auto-clear if a future capture
shows the reference doing so.

## What does NOT change

- The picker overlay's "esc to cancel" label is unchanged.
- The "? for shortcuts" idle label is unchanged.
- The "press ctrl+c again to exit" label uses the same dim-gray
  colour as "? for shortcuts" / "esc to cancel" — no new colour
  constants needed.
- The model label and " · high" suffix on the right are unchanged.

## Hard constraints / 存疑 / limitations

- 【推测】Auto-expire timeout. The reference doesn't auto-clear
  the exit hint after a fixed delay (the captured
  /tmp/refscroll.txt shows the hint persisting until the next
  keypress). If a future capture contradicts it, add a Tick
  handler that clears `m.exitConfirm` after 3 s.
- 【推测】Cascade. The confirmation state lives on the run mode
  and is cleared by any other keypress, including the picker
  arrow keys and the ctrl+o / ctrl+g shortcuts. The reference
  presumably behaves the same way — no test capture verified
  this.
- 【推测】Print mode. The `--print` path bypasses the TUI
  entirely so Ctrl+C doesn't apply there; it still exits on
  the first Ctrl+C as before.