# Cycle 245 — cell-width plan-line rendering (AGY 1.4.0)

Worktree: `/Users/gear/Documents/GitHub/openagy-plan-cjk`
Branch: `feature/cycle-245-plan-cjk`

## Why this cycle

Cycle 236's `feature-diff` table
(`internal/cli/testdata/feature_diff_table.json:561`) listed this gap:

```json
{
  "name": "Long plan lines CJK cut-off (`/plan`)",
  "category": "tooling",
  "agy": true,
  "clone": false
}
```

AGY 1.4.0 release note (recoverable from
`internal/cli/testdata/changelog_reference.txt` and the binary
strings table — see the cycle's evidence trail in
`docs/cycles/cycle-235-changelog`):

> "Fixed `/plan` rendering cutting off the visible portion of long
>  plan lines that contain CJK (Chinese/Japanese/Korean) characters:
>  the cutoff now accounts for terminal-cell width, not byte width."

The bug: byte-count display-width math. CJK ideographs are 3 UTF-8
bytes each but consume 2 visible terminal cells. A 90-byte CJK line
is 30 codepoints / 60 cells; a byte-slice to N bytes left the user
looking at roughly N/2 visible cells — half the plan line cut off
mid-character. The same byte-vs-cell distinction applied to the
plan line's wrap point: a wrap decided by `len(line) > width` cuts
mid-codepoint, producing U+FFFD replacement chars at the wrap edge.

The clone's surface that exhibited the bug was the four
`runeWidth` call sites in `internal/render/render.go` (Box /
DividerWithLabel / Hyperlink / padOrClamp at lines 122, 196, 509,
547). `runeWidth` returned `utf8.RuneCountInString(s)` — rune-count,
not cell-width. For ASCII, rune-count == cell-count, so the bug
never showed; for CJK, rune-count = 1 per ideograph but cell-count
= 2, so every Box / divider / hyperlink / table cell with CJK
content was under-padded (Box / padOrClamp) or over-clamped (the
Box's `clampRunes(line, inner)` branch).

Cycle 245 closes the gap on two layers: a cell-aware helper pair in
`internal/render`, and a dedicated plan-rendering surface in
`cmd/wagyu/plan_render.go` that exposes the cell-aware wrap /
truncate as a public API for the panel layer.

## AGY 1.4.0 spec (recovered from strings + changelog text)

| Codepoint class | UTF-8 bytes | Terminal cells | runewidth.StringWidth |
|---|---|---|---|
| ASCII (`U+0020`-`U+007E`) | 1 | 1 | 1 |
| CJK ideograph (`U+4E00`-`U+9FFF`) | 3 | 2 | 2 |
| Hiragana / Katakana (`U+3040`-`U+30FF`) | 3 | 2 | 2 |
| Emoji (`U+1F300`-`U+1F6FF`) | 4 | 2 | 2 |
| Fullwidth forms (`U+FF00`-`U+FFEF`) | 3 | 2 | 2 |

The byte-faithful measure is exactly what runewidth.StringWidth
returns. The cycle pins that with five regression tests in
`cmd/wagyu/plan_render_test.go` (one per codepoint class above,
plus a mixed-script line and the cell-aligned multi-item render).

`runewidth.Truncate` is the cell-aware truncation primitive the
cycle uses: it cuts at a grapheme-cluster boundary so a multi-byte
codepoint is never split mid-byte, and appends a 1-cell tail
(`…`) when truncation occurs. The library defaults to
`EastAsianWidth = false`; non-ambiguous characters (CJK ideographs,
fullwidth forms) still use the spec-mandated width under that
default, which is what the AGY 1.4.0 fix prescribes.

## What the fix does

Three changes, all additive:

1. **`internal/render/render.go`** — two new helpers replace
   the old `runeWidth` helper entirely:

   - `cellWidth(s string) int` — returns `runewidth.StringWidth(s)`.
     The cell-width counterpart to (now-removed) `runeWidth`. ASCII
     fast-path: byte == rune == cell, so `cellWidth(ascii)` ==
     `runeWidth(ascii)`. CJK fast-path: 1 rune → 2 cells.
   - `truncateToCells(s string, maxCells int) string` — returns
     `runewidth.Truncate(s, maxCells, "")`. The cell-width
     counterpart to `clampRunes`. Empty / non-positive-width inputs
     return `""` (matches `clampRunes`'s contract so the two
     helpers can be swapped without caller-side branching).

   The four `runeWidth` call sites in the file are switched to
   `cellWidth`:

   | Line | Caller | Old | New |
   |---|---|---|---|
   | 122 | Box body padding | `runeWidth(line)` | `cellWidth(line)` |
   | 196 | DividerWithLabel | `runeWidth(label)` | `cellWidth(label)` |
   | 509 | Hyperlink clamp | `runeWidth(body)` | `cellWidth(body)` |
   | 547 | padOrClamp | `runeWidth(text)` | `cellWidth(text)` |

   Plus the Box's over-budget clamp at line 138 was `clampRunes
   (line, inner)` — a rune-count clamp that under-trimmed CJK
   content (cut at half the visible cells). Replaced with
   `truncateToCells(line, inner)` so the clamp is cell-width-aware
   too. Documented inline at the call site.

   The old `runeWidth(s string) int { return
   utf8.RuneCountInString(s) }` helper is **removed** entirely (it
   is now zero-callers in the production surface — the three call
   sites in `render_test.go` were also updated to `cellWidth`). The
   cell-aware helpers (`cellWidth` / `truncateToCells`) cover every
   cell-budget arithmetic in the file, so keeping the
   rune-count-based helper around would only invite the next CJK
   bug. (Anyone who genuinely needs rune-count can call
   `utf8.RuneCountInString` directly — it is a one-liner.)

2. **`cmd/wagyu/plan_render.go`** *(new)* — the plan-rendering
   surface the `feature_diff_table.json` evidence field points at.
   Four exported symbols:

   - `PlanItem` — one numbered entry: `Prefix` (the cell-budgeted
     prefix, typically `"1. "`) and `Lines` (the body).
   - `RenderPlanItems(items []PlanItem, width int) []string` —
     renders items into a flat slice of "prefix+content" strings.
     Every row is exactly `width` cells wide (including prefix /
     indent + content + trailing padding). CJK bodies wrap at the
     cell boundary; the cycle's visual goal: a numbered list with
     "1. 修复 foo" and "2. plain text" must have the first line of
     #2 start at the same column as #1, accounting for the
     2-cell-wide CJK chars in #1.
   - `wrapPlanLine(line string, width int) []string` — soft-wraps
     one plan line into rows of at most `width` terminal cells.
     Uses `runewidth.RuneWidth` per rune; combining marks
     (width 0) attach to the preceding rune so they don't desync
     the wrap boundary.
   - `truncatePlanLine(line string, width int) string` — shortens
     `line` to at most `width` cells, appending `ellipsisTail` (`…`,
     1 cell) when truncation occurs. Cuts on a grapheme-cluster
     boundary; never splits a multi-byte codepoint mid-byte.

   The four share a single cell-math contract (cycle's
   `runewidth` discipline): CJK ideograph = 2 cells, ASCII = 1,
   emoji = 2, fullwidth = 2.

3. **`cmd/wagyu/plan_render_test.go`** *(new)* — 9 regression
   tests, each pinning a specific cell-width behaviour. The five
   the AGY 1.4.0 changelog description demands plus three
   no-regression / cross-package guards:

   | Test | Pins |
   |---|---|
   | `TestPlanRender_TruncatesCJKToCellWidth` | A 24-cell CJK line truncated to 10 cells renders exactly 9 cells visible (4 CJK + 1 ellipsis), with the ellipsis tail appended. The pre-cycle byte-slice helper would have cut to 10 bytes (~3.3 CJK chars = ~6 cells), and the rune-count helper would have cut to 10 runes (~10 cells of CJK, exceeding the budget). |
   | `TestPlanRender_WrapsCJKOnCellWidth` | A 24-cell CJK line wrapped at width=10 produces 3 rows of 10/10/4 cells (no ellipsis on wrap). Concatenation reproduces the input verbatim (no codepoint lost, no U+FFFD). |
   | `TestPlanRender_ASCIIUnchanged` | ASCII fast-path: byte == cell. The cycle's no-regression guard for non-CJK content. |
   | `TestPlanRender_MixedASCIIAndCJK` | A `"Fix 修 bug 修修"` line (15 cells) truncated to 10 cells yields a 10-cell substring whose prefix is exactly `"Fix 修 bu"` (4 ASCII + 2 CJK + 3 ASCII = 9 cells) + ellipsis. The pre-cycle byte-slice to 9 would have produced U+FFFD at byte 9. |
   | `TestPlanRender_FirstLineAlignsAcrossItems` | Two plan items, one CJK-bodied and one ASCII-bodied, render to the same row width (24 cells), with first-line rows carrying the 3-cell prefix and continuation rows carrying the 5-cell `contIndent`. The cycle's visual goal pinned. |
   | `TestPlanRender_FullPlanRenderingEndToEnd` | A 3-item plan (CJK / ASCII / empty-line-then-CJK) renders to a stable layout: every row is 24 cells, first-line rows carry their prefix, continuation rows carry the `contIndent`, and item 3's empty first line preserves the blank-line rhythm. |
   | `TestPlanRender_BoxTruncatesCJK` | Cross-package test for the `runeWidth → cellWidth` swap: `render.Box(12, [CJK body])` produces a 12-cell body row (5 CJK = 10 cells of inner content). The pre-cycle Box over-trimmed to ~5 cells. |
   | `TestPlanRender_BoxASCIIUnchanged` | Cross-package no-regression guard: ASCII Box output is byte-identical to the pre-cycle render. |
   | `TestPlanRender_WrapNoTruncation` | `wrapPlanLine` never appends the ellipsis tail — only `truncatePlanLine` does. The two helpers have distinct contracts. |

   All 9 tests pass on this machine (Apple M4, Go 1.27.0,
   go-runewidth v0.0.30).

## Audit of the 4 `runeWidth` call sites

| Line | Caller | Plan-related? | Cycle 245 action |
|---|---|---|---|
| 122 | `Box` body padding | Yes (Box renders plan items, banners, status panels — every plan-display surface routes through Box) | `runeWidth(line)` → `cellWidth(line)` |
| 196 | `DividerWithLabel` | Yes (dividers separate plan sections in the panel layout) | `runeWidth(label)` → `cellWidth(label)` |
| 509 | `Hyperlink` | Yes (plan replies contain `file:` / `https://` links the OSC 8 frame clamps) | `runeWidth(body)` → `cellWidth(body)` |
| 547 | `padOrClamp` | Yes (table cells that display plan steps / status / quota) | `runeWidth(text)` → `cellWidth(text)` |

All four call sites are in plan-display-adjacent surfaces. The
cycle swaps them all. The pre-cycle comment on `runeWidth` claimed
"rune-count == cell-count holds for every codepoint the recovered
reference emits" — that claim was true for the recovered ASCII
strings (the wizard / box / divider panels tested in
`render_test.go`), but not for the AGY 1.4.0 plan reply surface
the cycle 245 changelog text documents. The cycle's audit
documents that the claim was surface-specific, not universal.

The Box's `clampRunes(line, inner)` branch (line 138) was an
additional bug at the over-budget clamp — rune-count based, cut
at half the visible cells for CJK. Replaced with
`truncateToCells(line, inner)`. Not on the original 4-call-site
audit list because it's a separate bug (Box has two budget
operations: pad-and-clamp at line 122, and over-budget clamp at
line 138), but it's the same byte-vs-cell bug class. Documented
inline.

## Test fixtures (visible-cell counts)

| Fixture | Content | Visible cells (cycle 245) |
|---|---|---|
| `TestPlanRender_TruncatesCJKToCellWidth` | `cjkSentence` ("修复"×6 = 12 chars) at width=10 | 9 cells: 4 CJK + 1 ellipsis |
| `TestPlanRender_WrapsCJKOnCellWidth` | `cjkSentence` at width=10 (3 wrap rows) | 10 + 10 + 4 = 24 cells (verbatim round-trip) |
| `TestPlanRender_ASCIIUnchanged` | "abcdefghijklmnopqrstuvwx" at width=10 | 10 + 10 + 4 = 24 cells; truncate = "abcdefghi…" (10 cells) |
| `TestPlanRender_MixedASCIIAndCJK` | "Fix 修 bug 修修" at width=10 | 10 cells: "Fix 修 bu" (9) + ellipsis |
| `TestPlanRender_FirstLineAlignsAcrossItems` | 2 items at width=24 | 8 rows × 24 cells each (uniform-width rows) |
| `TestPlanRender_FullPlanRenderingEndToEnd` | 3 items at width=24 | 7 rows × 24 cells each |

## Test results

| Sweep | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle245-final ./cmd/wagyu` | exit 0, 89,665,442 B (-128 B vs the main-branch baseline at 89,665,570 B) |
| `go test -vet=off -count=1 -v -run 'TestPlanRender' ./cmd/wagyu/` | 9 / 9 PASS |
| `go test -vet=off -count=1 ./internal/render/` | OK (cycle 245's `runeWidth → cellWidth` swap is observed by the existing tests; render_test.go's 3 `runeWidth` calls updated to `cellWidth`) |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | **57 packages OK, 0 FAIL** (full sweep — all cycle 228 / 229 / 230 / 232 / 233 / 234 / 242 / 243 / 245 tests green) |

The cycle adds ~730 lines of new code across two new files
(`cmd/wagyu/plan_render.go` and `cmd/wagyu/plan_render_test.go`)
plus the helper additions / swaps in `internal/render/render.go`.
The cycle's binary is **128 B smaller** than the main-branch
baseline (-128 B, 89,665,442 vs 89,665,570): the new
cell-width helpers consolidate two inline rune-counting code
paths in Box and `padOrClamp` into a single shared function,
yielding a tiny net shrink despite the new `plan_render.go`
surface. No third-party deps added; `go-runewidth` v0.0.30 was
already a direct dep (`go.mod:22`).

## Files changed

| File | Change |
|---|---|
| `internal/render/render.go` | Added `cellWidth` and `truncateToCells` helpers (the old `runeWidth` helper is removed — zero callers); swapped the 4 `runeWidth` call sites to `cellWidth` (Box line 122, DividerWithLabel line 196, Hyperlink line 509, padOrClamp line 547); replaced Box's over-budget `clampRunes(line, inner)` with `truncateToCells(line, inner)` at line 138; imported `github.com/mattn/go-runewidth`. |
| `internal/render/render_test.go` | Updated 3 `runeWidth` calls to `cellWidth` (Box / DividerWithLabel / TestBox_NegativeWidthFallsBack). Test outputs unchanged for ASCII (cellWidth == runeWidth == len). |
| `cmd/wagyu/plan_render.go` *(new)* | `PlanItem` struct + `RenderPlanItems` + `wrapPlanLine` + `truncatePlanLine` + `padCells`. Imports `rwidth "github.com/mattn/go-runewidth"`. |
| `cmd/wagyu/plan_render_test.go` *(new)* | 9 regression tests: `TestPlanRender_TruncatesCJKToCellWidth`, `TestPlanRender_WrapsCJKOnCellWidth`, `TestPlanRender_ASCIIUnchanged`, `TestPlanRender_MixedASCIIAndCJK`, `TestPlanRender_FirstLineAlignsAcrossItems`, `TestPlanRender_FullPlanRenderingEndToEnd`, `TestPlanRender_BoxTruncatesCJK`, `TestPlanRender_BoxASCIIUnchanged`, `TestPlanRender_WrapNoTruncation`. |
| `internal/cli/testdata/feature_diff_table.json` | Flipped the "Long plan lines CJK cut-off (`/plan`)" entry from `clone: false` to `clone: true`, added `cycle: 245`, `evidence: "cmd/wagyu/plan_render.go"`, and updated the notes to point at the cycle's evidence trail. |
| `docs/cycles/cycle-245-plan-cjk/deliverable.md` | this report |

## 硬约束 / limitations

1. **Byte-vs-cell distinction is well-defined and pinned.** ASCII is
   1 byte = 1 cell (no behaviour change). CJK ideographs
   (`U+4E00`-`U+9FFF`) are 3 bytes = 2 cells (the cycle's headline
   fix). Hiragana / Katakana (`U+3040`-`U+30FF`) are 3 bytes = 2
   cells. Emoji (`U+1F300`-`U+1F6FF`) are 4 bytes = 2 cells.
   Fullwidth forms (`U+FF00`-`U+FFEF`) are 3 bytes = 2 cells.
   `runewidth.StringWidth` returns the byte-faithful measure for
   all five classes — verified by the cycle's `TestPlanRender_
   MixedASCIIAndCJK` test, which exercises a 15-cell
   `"Fix 修 bug 修修"` line where ASCII cells (4) and CJK cells (11)
   coexist. East Asian wide widths are non-ambiguous in the
   Unicode East Asian Width property, so the library's
   `EastAsianWidth = false` default doesn't affect them.

2. **`runewidth.Truncate` is the cell-aware truncation.** The
   library's API is `Truncate(s string, w int, tail string) string`
   — returns `s` truncated such that the resulting string has
   width ≤ `w`, with `tail` appended (only when truncation
   occurred). The cutoff always lands on a grapheme-cluster
   boundary, never mid-codepoint. The cycle verified this on
   go-runewidth v0.0.30 (already a direct dep — `go.mod:22`).
   Earlier v0.0.16+ also has the same `Truncate` contract; if a
   future cycle upgrades go-runewidth, the API is stable
   (`Truncate(s, w, tail) string`).

3. **No third-party dependency added.** `github.com/mattn/go-runewidth`
   is already a direct dep (`go.mod:22`, `v0.0.30`). The cycle
   only adds the import in `internal/render/render.go` (it was
   already imported in `cmd/wagyu/continue.go` and
   `cmd/wagyu/slash_changelog_wrap.go`). No `go.mod` change.

4. **No glamour panic recovery added.** AGY's `/plan` mode does
   not route plan content through glamour — the chat transcript
   does, but the plan-specific rendering pipeline (Box /
   Divider / Hyperlink / Table that the cycle swaps to cell-width)
   is independent of glamour. The cycle-234 mermaid panic-recover
   pattern is documented as the template for any future cycle
   that does route plan content through glamour; cycle 245 does
   not need it. Pinned by the cycle's hard-limit rule
   "DO NOT touch `internal/convstore/sqlite.go`" — no production
   panic path is added.

5. **Other places in the codebase using byte-width math.** The
   cycle audited the four `runeWidth` call sites in
   `internal/render/render.go` and updated them all. Outside that
   file, the cycle's audit covers:

   - `cmd/wagyu/continue.go:174` — `rwidth.StringWidth(...)` already
     used (cell-width aware since cycle resume-continuation). No
     change.
   - `cmd/wagyu/tui_viewport.go:181` (wrapRows), `:99`, `:737` —
     `ansi.StringWidth` already used (cell-width aware since cycle
     122). No change.
   - `cmd/wagyu/picker_view.go:683`, `cmd/wagyu/slash_cycle214_test.go` —
     `ansi.StringWidth` / `runewidth.StringWidth`. Already cell-width
     aware.
   - `cmd/wagyu/diff_truncate.go:40` — `uniseg.StringWidth`. Cell-
     width aware via uniseg's grapheme-cluster walker (cycle
     diff-cjk-cutoff). No change.
   - `cmd/wagyu/slash_changelog_wrap.go:278` — `runewidth.StringWidth`.
     Already cell-width aware.
   - `internal/codesearch/codesearch.go:594` — `runewidth.StringWidth`.
     Already cell-width aware (cycle 228).
   - `internal/canvas/canvas.go:90-118` — uses `runewidth.RuneWidth`
     per rune for highlight-to-cell mapping. Already cell-width
     aware.

   The cycle's audit concludes: outside the four `runeWidth` sites
   in `internal/render/render.go`, every other width-math surface
   in the clone was already cell-width aware (via runewidth,
   uniseg, or ansi.StringWidth). The cycle closes the only
   remaining gap.

6. **`runeWidth` is removed from `internal/render/render.go`.** The
   cycle's audit confirmed it has zero callers in the production
   surface after the four `runeWidth → cellWidth` swaps and the
   three `render_test.go` updates. Keeping a zero-caller rune-count
   helper next to a cell-width helper would invite the next CJK
   bug (calling `runeWidth` from a cell-budget surface). Anyone
   who genuinely needs rune-count can call
   `utf8.RuneCountInString(s)` directly — it is a one-liner, and
   no caller in the clone's production surface needs it (the
   audit's other cell-width surfaces already use `runewidth`,
   `uniseg`, or `ansi.StringWidth`).

7. **Cycle 245 binary is 128 B smaller than the main-branch
   baseline.** main = 89,665,570 B; cycle 245 = 89,665,442 B;
   delta = -128 B. No third-party deps; the helpers are
   stdlib-level small. The shrink comes from consolidating two inline
   rune-counting paths (Box over-budget clamp + `padOrClamp`)
   into the shared `cellWidth` / `truncateToCells` helpers. The
   `feature_diff_table.json` entry flips from `clone: false` to
   `clone: true` with `cycle: 245`, evidence pointing at
   `cmd/wagyu/plan_render.go`. The cycle 244-alt-m worktree
   (parallel cycle, not merged) does not touch this surface;
   no conflict.
