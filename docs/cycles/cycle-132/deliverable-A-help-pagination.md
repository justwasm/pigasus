# Cycle 132 — /help COMMANDS tab pagination

## Why

The reference's /help COMMANDS tab paginates the 99-row catalog
into 18-row pages with a `[N-N of N items]` page indicator and
arrow-key navigation (Up/Down move within the page; Left/Right
cycle pages). The cycle-83 clone was rendering all 99 rows
uncondensed; the harness capture (`001-second.txt`) showed the
clone's pane scrolled past the header to land mid-catalog while
the reference pinned page 1 to the top with `> /add-dir` as the
first row.

## What changes

### `cmd/wagyu/tui_v135.go`

- New struct fields `helpPage int` and `helpCursor int`.
- `renderV135Help(tab, ..., m)` now takes the run-mode so the
  COMMANDS panel can read pagination state. `renderV135HelpCommands(b, m)`
  slices the catalog with `start = page * 18`, writes up to
  `helpCommandsVisibleRows(page)` rows, marks `start+i == helpCursor`
  with the `> ` prefix, and emits
  `[<from>-<to> of <total> items]` between the rows and the
  keyboard hint.

### `cmd/wagyu/history.go`

- Three new helpers on `*v135RunMode`:
  - `helpCommandsPageNext()` — advance to the next page; wrap at end.
  - `helpCommandsPagePrev()` — retreat one page; wrap at start.
  - `helpCommandsCursorDown()` — within-page cursor; rolls to the
    next page if the user holds Down past the last row of the
    last page.
- Two package-level helpers:
  - `helpCommandsPages(total)` — page count given the page size.
  - `helpCommandsVisibleRows(page)` — row count on a page (last
    page is short when the catalog doesn't divide evenly).

### Tab/keyboard wiring

The `m.helpMode` switch (cycle 83) now branches on `m.helpTab`:

- `Tab` — cycles 0→1→2→0 and resets page/cursor.
- `Right` — on tab 1 (commands), cycles pages; otherwise cycles tabs.
- `Left`  — on tab 1, retreats pages; otherwise cycles tabs.
- `Up`    — on tab 1, decrements `helpCursor` (clamped at 0).
- `Down`  — on tab 1, calls `helpCommandsCursorDown()`.

### `cmd/wagyu/tui_v135.go::buildV135CommandRow`

`buildV135CommandRow(c, highlighted)` replaces the cycle-83g
hardcoded `c.Name == "/add-dir"` check with a real `highlighted`
flag passed from the paginated renderer.

## What does NOT change in cycle 132

- **Page size** — fixed at 18 (matching the reference capture).
  Terminal-height-aware sizing is a cycle 133+ candidate.
- **Catalog length discrepancy** — the reference shows 99 entries
  but the harness `↓ N more` count is 86 (= 94 visible catalog).
  The catalog itself is unchanged in cycle 132; the 5-row delta
  is the chrome-devtools-plugin filter (cycle 131 deliverable
  flagged it for a later audit).
- **Help tabs `general` and `shortcuts`** still render their full
  content; pagination only kicks in for `commands`.

## Harness changes

| Scenario | Before | After |
|---|---:|---:|
| `help_tabs 001-second` | 61 | (pending re-run) |

The `000-general` and `004-closed` rows are unchanged — this cycle
only affects the COMMANDS tab.

## Hard constraints / 存疑 / limitations

- 【推测】The 18-row page size is fixed by the reference capture.
  Narrower terminals (where 18 rows + chrome won't fit) might
  trigger a smaller page size in the reference; cycle 133+ can
  derive this from `m.height`.
- The `[N-N of N items]` format string is recovered from one
  capture. The reference may use em-dash separators or a
  different bracket style. Cycle 132 mirrors the capture
  byte-for-byte.
- Page-cycling via Left/Right only fires on the commands tab; the
  other two tabs (general, shortcuts) don't have enough content
  to need it. The Tab key still cycles all three tabs.