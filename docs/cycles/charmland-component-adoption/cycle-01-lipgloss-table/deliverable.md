# Cycle 01 — adopt `charm.land/lipgloss/v2/table`

**Branch:** `feature/charmland-component-adoption`
**Package:** `internal/render`
**Upstream:** `charm.land/lipgloss/v2@v2.0.4/table`
**Status:** ✅ shipped

## Goal

Replace the hand-rolled `internal/render.NewTable` with a thin wrapper
around `charm.land/lipgloss/v2/table.Table` so wagyu's table-rendering
surfaces match AGY 1.4.2 (which uses the same package — `strings(1) |
grep -c lipgloss/v2/table` on `~/.local/bin/agy` returns 82 hits per the
task brief).

## API mapping

The wrapper preserves the public surface of the hand-rolled renderer so
existing callers don't need to change. **No external Go file in the
clone calls `render.NewTable`, `render.Table`, `render.Row`, or
`render.Cell`** — verified via `grep -rn "render\.\(NewTable\|Table\|Row\|Cell\)" --include='*.go'`
(only matches in `internal/render/` itself). The wrapper is therefore
the only consumer of its own types today.

| Old API | New API | Notes |
|---|---|---|
| `NewTable(width, ncols int) *Table` | same | width budget unchanged |
| `SetColumnWidths([]int)` | same | Width() preserved exactly when sum ≤ budget (matches v1.2.10 semantics) |
| `Widths() []int` | same | |
| `TotalWidth() int` | same | |
| `Render([]Row) string` | same | output separator changed from " │ " → "│" (1 cell instead of 3) |
| `Row` type | same struct (`{Cells []Cell}`) | |
| `Cell` type | same struct (`{Text, Width}`) | |
| — | **`WithHeaders(...string) *Table`** 🆕 | chainable; default headers off |
| — | **`WithStyles(cell, header lipgloss.Style) *Table`** 🆕 | chainable; per-cell and per-header styling; default = bare style + bold header |

## Style alignment

The wrapper uses `lipgloss.NewStyle()` (no padding, no margin, no
colour) by default — equivalent to the previous hand-rolled output
modulo the separator. The `StyleFunc(row, col)` on the underlying
table is the natural seam for per-cell styling: callers can build a
`lipgloss.NewStyle().Bold(true).Foreground(lipgloss.ANSIColor(244))`
and pass it through `WithStyles`, and it will be applied to the
corresponding cell. **The StyleFunc deliberately does not set
`Width(N)`** — the per-column width is owned by the wrapper (cells are
pre-padded / pre-truncated before being handed to `lipgloss/table`,
and the table's own resizer measures content width via `lipgloss.Width`
which ignores ANSI escapes; the inner `Width(t.widths[c])` then
re-confirms the layout).

## Callers migrated

There were no callers to migrate — `render.Table` was never used
outside the package. Internal `internal/render/render_test.go` was
regenerated for the new output format (see the table-rendering tests
section below).

## Behaviour changes (tested)

| Scenario | Old behaviour | New behaviour |
|---|---|---|
| Default widths `NewTable(20, 3)` | `[3, 3, 14]` | `[3, 3, 14]` (unchanged) |
| `SetColumnWidths([6,6,6])` | preserves exactly | preserves exactly (unchanged) |
| Row with `Cell.Width=0` | uses column width | uses column width (unchanged) |
| Output separator | " │ " (3 cells) | "│" (1 cell) |
| Empty table (no headers, no rows) | empty string | empty string (unchanged) |
| `ncols=0` (pathological) | collapsed to 1 column | collapsed to 1 column (unchanged) |
| `width=0` (bbt-v2 quirk) | falls back to `DefaultBoxWidth` | falls back to `DefaultBoxWidth` (unchanged) |
| **Headers** 🆕 | not supported | rendered above data with bold style by default |
| **Per-cell styles** 🆕 | not supported | via `WithStyles(cell, header)` |
| **Total visible line width** | `width` (cells sum to width, separator is extra) | `sum(colW) + (ncols-1)` (same arithmetic: separator is 1 cell) |

The `width` semantics changed from "the visible line width, excluding
the 3-cell separator" to "the cell budget, the visible line width is
`width + (ncols-1)` when columns are not explicitly shrunk". This is
the same model AGY 1.4.2 uses (lipgloss/table's Width takes total
visible width including borders).

## Test results

```
$ go test -vet=off -count=1 -run 'Test' ./internal/render/
ok  	github.com/chadgpt/openagy/internal/render	0.301s
```

The package tests now cover:

| Test | What it pins |
|---|---|
| `TestBox_Borders` | top/right/bottom edge glyphs; `Box` width arithmetic |
| `TestBox_NegativeWidthFallsBack` | `width=0/-1/-3` → `DefaultBoxWidth` |
| `TestDivider_FallbackWidth` | `width=0/-7` → `DefaultBoxWidth` (bbt-v2 quirk) |
| `TestDividerWithLabel_CentredLabel` | "─── Preview ───" pattern |
| `TestHyperlink_Underflow` | OSC 8 `body-clamp` semantics |
| `TestTable_ColumnWidths` | 3-column widths; cell padding/clamping; "│" separator count |
| `TestTable_MissingColumnWidthFallback` | 2-column widths; row shorter than ncols; missing-cell pad |
| `TestTable_WithHeaders` | header row with `Bold` style; 2-column layout |
| `TestTable_FiveColumnMixedWidths` | 5 columns, mixed widths, content clamping |
| `TestTable_Empty` | no-rows, headers-only, ncols=0, width=0 paths |
| `TestTable_PadOrClamp` | `padOrClamp` helper directly |

11 tests, all passing. New tests: 4 (`TestTable_WithHeaders`,
`TestTable_FiveColumnMixedWidths`, `TestTable_Empty`,
`TestTable_PadOrClamp`). Existing tests: 7, regenerated for the new
separator (`│` instead of ` │ `).

## Full test suite

```
$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/... 2>&1 | grep -E 'FAIL|^ok ' | grep -E 'FAIL' 
--- FAIL: TestDispatchPrintSlash_LocalCommands (0.00s)
--- FAIL: TestChangelog_MatchesReferenceBinary (1.09s)
```

Two failures — both are in the 7 pre-existing test flakes list from
the task brief (`TestDispatchPrintSlash_LocalCommands` and
`TestChangelog_MatchesReferenceBinary`). **No new failures.**

## Build / binary size

```
$ go build -o /tmp/wagyu-charmland ./cmd/wagyu
$ ls -la /tmp/wagyu /tmp/wagyu-charmland
-rwxr-xr-x  1 gear  staff  73767010  /tmp/wagyu
-rwxr-xr-x  1 gear  staff  73767010  /tmp/wagyu-charmland
```

**0 byte delta.** `charm.land/lipgloss/v2/table` was already a transitive
dep via `charm.land/lipgloss/v2@v2.0.4` (verified via `go list -deps
./cmd/wagyu | grep lipgloss/v2/table`).

## Files

```
 internal/render/render.go      | 316 ++++++++++++++++++++++++--------
 internal/render/render_test.go | 197 +++++++++++++++++++++-
 2 files changed, 448 insertions(+), 65 deletions(-)
```

## Hard constraints / limitations

- `Render` returns a string with no trailing newline. `lipgloss/table`
  normally appends a `\n` after the bottom border (which we disabled),
  so this matches the previous behaviour. Callers that need a trailing
  newline (e.g. when concatenating into a multi-line panel) should add
  one explicitly.
- `SetColumnWidths` does not aggregate: when the caller's widths sum
  to less than `width`, the table underfills (matches the v1.2.10
  contract). Callers who want auto-grow should compute widths
  explicitly via `NewTable + SetColumnWidths`.
- The wrapper does not expose `YOffset` / `Height` / `Wrap` from the
  underlying `lipgloss/table.Table`. Cycle 01 has no caller for those
  features; a follow-up cycle can add `WithHeight(int)`, `WithYOffset(int)`,
  etc. when a panel needs scrollable tables.
- `WithStyles` styles are applied via `StyleFunc` — only the style
  *attributes* propagate (colours, bold, italic). Padding/margin on
  the style are intentionally ignored because the wrapper owns the
  cell-width math. Callers who want extra padding should pad the cell
  text explicitly (the wrapper will preserve their padding).

## Follow-up cycles

Cycle 02 (`bubbles/v2/textinput`) is the next deliverable. The
`lipgloss/table` wrapper is the last helper used only by its own
tests; cycles 03 / 04 / 05 touch hot-path components
(viewport / key / cursor) that have wider blast radius.
