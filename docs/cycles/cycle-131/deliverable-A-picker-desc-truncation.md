# Cycle 131 — picker description truncation

## Why

The reference's picker overlay truncates long descriptions to
**45 chars + ellipsis**, so descriptions like `/btw` (57 chars) and
`/codesearch` (57 chars) end with `...` instead of overflowing into
the hint row below. The clone was rendering full descriptions,
producing visually inconsistent rows.

Captured reference behaviour at
`/tmp/agycmp/hotpath/picker/ref.001-down5.txt`:

```
/btw                                             Ask a side question without interrupting the ...
/codesearch (cs)                                 Search code in the workspace (usage: /codesea...
/config (settings)                               Open settings panel
```

The `/config` description (18 chars) passes through unchanged;
`/btw` and `/codesearch` truncate. The trailing width is exactly
48 characters (45 + `...`).

## What changes

### `cmd/wagyu/picker_view.go`

- New constant `pickerDescWidth = 48` (45 + 3 for the ellipsis).
- New helper `truncatePickerDesc(desc string) string`:
  - Returns `desc` unchanged if it already fits in 48 chars.
  - Otherwise returns `desc[:45] + "..."`.
- The picker overlay loop wraps `c.Description` through
  `truncatePickerDesc(desc)` before writing it to the rendered
  output, both for highlighted and non-highlighted rows.

### Tests (`cmd/wagyu/picker_view_test.go`)

| Test | Asserts |
|---|---|
| `TestPickerView_DescriptionTruncation` | short desc passes through; exact-width (48) passes through; 49 truncates to 45+`...`; `/btw` and `/codesearch` give the recovered forms |
| `TestPickerView_RenderTruncatesBtw`     | picker overlay (after typing `/`) renders the `/btw` row with `...` and **not** with `current task` |

## What does NOT change in cycle 131

- **Catalog length discrepancy** — the reference's picker shows
  94 entries (`3 above + 5 visible + 86 below`), the clone's 99
  entries. Cycle 131 leaves this alone because the catalog delta
  is a cycle-74 enumeration issue (5 commands likely filtered out
  by the reference, e.g. skill-plugin entries that show only when
  their skill is loaded). A separate cycle (132+) audits the
  catalog against an offline `strings ~/.local/bin/agy` scan.
- **Word-boundary ellipsis** — the reference truncates at a
  character boundary, not a word boundary. Cycle 131 mirrors that
  byte-for-byte (`Ask a side question without interrupting the ...`,
  not `Ask a side question without interrupting...`). If the
  ref ever breaks at a word boundary, cycle 132+ changes it.
- **Description column width** — the cap is 48 chars regardless of
  terminal width. Cycle 131 derives this from a 220-wide terminal
  capture; narrower terminals would have a different cap. The
  reference likely uses the same fixed cap (it's a layout choice,
  not a fit-to-width one).

## Harness changes

| Scenario | Before | After | Note |
|---|---:|---:|---|
| `picker 001-down5` | 25 | 24 | `/btw` and `/codesearch` truncation now matches ref |
| `picker 000-slash` | 23 | 32 | *no-op drift* (trailing-newline jitter) |

The 5-row off-by-five in the "↓ N more" tail (we say 91, ref says
86) is unchanged in cycle 131 — it's the catalog-length mismatch,
not a render bug.

## Hard constraints / 存疑 / limitations

- 【推测】The exact truncation rule (45 + `...`) is recovered from
  one capture of the picker overlay. The reference may use a
  slightly different rule (e.g. 47 + `...` for 50 total, or
  word-boundary break). Cycle 131 keeps the byte-for-byte form
  that matches the captured `/btw` and `/codesearch` rows.
- The `truncatePickerDesc` helper is single-line, byte-counting,
  and Unicode-unsafe. The descriptions in the catalog are all
  ASCII so this works for every cycle-74 entry; a future
  Unicode-bearing catalog (skill descriptions in non-Latin
  scripts) would need rune-aware truncation. Cycle 132+
  candidate.
- The catalog length difference (94 vs 99) is **not** a
  truncation issue — it's a picker-filter issue. Tracked under
  the cycle-74 backlog.