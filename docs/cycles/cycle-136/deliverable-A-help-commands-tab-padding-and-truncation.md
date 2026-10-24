# Cycle 136 — /help COMMANDS tab name column padding + description truncation

## Why

The /help COMMANDS tab in the v1.3.6 reference capture
(`/tmp/agycmp/hotpath/help_tabs/ref.001-second.txt`) shows:

```
> /add-dir                                         Add a directory to the workspace
  /btw                                             Ask a side question without interrupting the ...
  /copy                                            Copy the last planner response to the clipboa...
  /fork (branch)                                   Create a branch of the current conversation a...
```

Two layout details the cycle-132 clone got wrong:

1. **Long descriptions were truncated mid-word without an ellipsis.**
   Cycle 132 appended `c.Description` verbatim, so a 100-character
   description got cut at the terminal width with no `...` to
   indicate truncation — looking like a corruption rather than a
   cap. The reference replaces the tail with `...` after 45 chars.
2. **The name column was 50 chars wide; the reference is 51.** A
   byte-position count of `> /add-dir` shows the description
   starting at column 52 in the reference, column 51 in the clone.
   One space of misalignment on every catalog row.

## What changes

### `cmd/wagyu/tui_v135.go::buildV135CommandRow`

- New constant `helpDescWidth = 48` (45 + `...`). When
  `len(c.Description) > helpDescWidth`, the row shows
  `c.Description[:45] + "..."` instead of the raw string.
- The name-column padding cap bumped from 50 → **51**. The
  trailing padding fills `51 - leftRunes` spaces so the
  description flows at column 52, matching the reference byte
  for byte.

`truncatePickerDesc` (cycle 131) uses the same `48` cap and the
same `...` suffix; both surfaces now share the truncation rule.

## What does NOT change in cycle 136

- The page indicator `[1-18 of N items]` — already correct from
  cycle 132.
- The `↑/↓ Navigate` keyboard hint row — unchanged.
- The cycle-83g `> /add-dir` highlight check — replaced in cycle
  132 with `buildV135CommandRow(c, highlighted)` and unchanged
  here.
- The alias rendering `(cs, search)` — the cycle-132 multi-alias
  join stays. Only description truncation changes.
- `/help general` and `/help shortcuts` tabs — neither paginates
  nor truncates; they keep their full content.

## Hard constraints / 存疑 / limitations

- 【推测】The 45-char description cap is recovered from one capture.
  The reference may use a width derived from the terminal width
  (`terminal_width - 52` or similar). At terminal widths narrower
  than 100 cols the cap should shrink; cycle 136 keeps the
  static 45-char figure to match the cycle-83 capture at 100×30.
- 【推测】The 51-char name column was confirmed via a single byte
  position count. If a wider terminal capture exists, the column
  may scale.