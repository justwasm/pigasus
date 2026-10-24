# Cycle 137 — /help SHORTCUTS tab pagination

## Why

The v1.3.6 reference's /help SHORTCUTS tab paginates its 33 keyboard
binding rows into 18-row pages with the same `[N-N of N items]`
indicator as the COMMANDS tab. Captured at
`/tmp/agycmp/hotpath/help_tabs/ref.002-third.txt`:

```
Keyboard Shortcuts
  /keybindings to customize

> /                               Open slash commands
  \ + enter                       Insert newline fallback
  ...16 more rows...
  ctrl+z                          Suspend CLI
  [1-18 of 33 items]

Keyboard: ↑/↓ Navigate  ←/→ Switch View  esc Close
```

The cycle-85 clone dumped all 33 rows in one shot, scrolling the
page indicator and the bottom keyboard hint past the terminal height
on the harness's 30-row pane. The "page indicator missing" diff
(`002-third` 44 chars) and the general layout drift in `003-wrapped`
and `004-closed` came from the same root cause.

## What changes

### `cmd/wagyu/tui_v135.go`

- `renderV135HelpShortcuts(b)` → `renderV135HelpShortcuts(b, m)`.
  Reads `m.helpPage` and `m.helpCursor`, slices `v135HelpShortcuts`
  with the same math as the COMMANDS tab, and emits the same
  `[<from>-<to> of <total> items]` indicator between the rows and
  the keyboard hint. Cursor row is marked with the `> ` pattern
  already used by the shortcuts rows.
- `renderV135Help` now passes `m` to `renderV135HelpShortcuts`.

### `cmd/wagyu/tui_v135.go` (key dispatch)

The `m.helpMode` block now treats `helpTab == 2` (SHORTCUTS) the
same as `helpTab == 1` (COMMANDS) for Left / Right (cycle pages)
and Up / Down (within-page cursor). GENERAL (tab 0) still uses
Tab / Left / Right to switch tabs since it has no cursor row.

### `cmd/wagyu/history.go`

- New helper `helpCommandsVisibleRowsOf(total, page)` does the
  page-row-count math given any catalog size. The cycle-132
  `helpCommandsVisibleRows(page)` is now a thin shim around it
  that passes `len(DefaultSlashCommands)`. Cycle 137 unifies the
  math under one helper so a future tab (e.g. /help AGENTS,
  /help SKILLS) can reuse it.

## What does NOT change in cycle 137

- `/help COMMANDS tab` behaviour is unchanged — it already used the
  cycle-132 helpers.
- `/help GENERAL tab` content and tab-switching rules unchanged.
- The `> /`, `  \ + enter`, `  alt+enter` row formatting (cycle 85
  hardcoded strings) is unchanged — cycle 137 only adds pagination
  and the cursor highlight on the active row.
- `v135HelpShortcuts` (the 33-row list) is unchanged.

## Hard constraints / 存疑 / limitations

- 【推测】The page size (18) is fixed. The reference's capture is
  at 100×30 and 18 rows fit naturally. A narrower terminal
  (e.g. 80 cols × 24 rows) might trigger a smaller page size;
  cycle 138+ can derive it from `m.height`.
- 【推测】The cursor highlight in SHORTCUTS tab reuses the `> `
  prefix that's already in the row strings (cycle 85 had `> /` for
  the first row as part of the formatting). Cycle 137 turns that
  static highlight into a movable one driven by `m.helpCursor`.
  This matches the COMMANDS tab semantics but the reference
  capture doesn't directly exercise cursor movement on the
  SHORTCUTS tab; cycle 138+ can verify against a fresh capture.