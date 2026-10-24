# Cycle diff-folding — deliverable

> AGY 1.3.5 changelog line 4:
>
> *"Added folding to the /diff file view: hunk headers can be collapsed with the f key, which hides the lines inside the hunk and shows just the @@ header with a + count; pressing f again expands the hunk and the cursor stays on the same header."*

## Scope

The reference's side-by-side diff viewer (out of scope for the clone —
see cycle 226's deliverable, §1) carries the f key for full-file
collapse. The clone's plain inline body keeps the same gesture: press
**f** on an `@@` header and the hunk's body collapses into the header
line plus a dim-grey `+N` line-count indicator. Pressing f again
expands.

The cycle is the smallest change that implements the changelog line
verbatim:

| Item | Where |
|---|---|
| Fold state | `slashPanel.diffFolded map[int]bool` (keyed by hunk index in document order). |
| Inline cursor | `slashPanel.diffInlineRow int` (the row cursor for the inline-diff subview, indexes `panelDiffInlineLines`'s body rows; 0..len-1). |
| Key binding | `handleDiffPanelKey`: a new `f` branch fires only when `m.panel.diffView != 0` (inside the inline subview); the pre-existing list-view keys are unchanged. |
| Renderer | `panelDiffInlineLines` calls `renderDiffFromBody`, which applies `diffFolded[i]` per hunk and emits one row per folded `@@` (header + `+N` indicator) or `1+len(body)` rows per expanded hunk. |
| Per-hunk parser | `splitDiffHunks` (in `slash_diff_fold.go`) partitions a git diff body into pre-header lines and a `[]diffHunk` slice. |
| Toggle helper | `toggleDiffHunkFoldAtCursor` looks up the hunk whose @@ header sits at the cursor's row, flips `diffFolded[i]`, and re-anchors the cursor onto the same hunk's header under the new layout. |
| Hunk-fold legend | `diffInlineLegend` advertises the new key as the leading pair: `f fold hunk · esc back`. |
| Reset behaviour | `diffOpenInline` clears `diffInlineRow = 0` and `diffFolded = nil`; `diffCloseInline` (Esc on the subview) does the same. A new diff therefore always starts unfolded at the top row. |

## Key surfaces

### `f` (the fold key)

```
handleDiffPanelKey(key) {
  ...
  if key.Mod == 0 && key.Code == 'f' {
      if m.panel.diffView != 0 {
          m.toggleDiffHunkFoldAtCursor()
      }
      return true, nil    // panel owns the key — never falls through
  }
}
```

The toggle is a **no-op on the list view** (where `diffView == 0`).
On the inline subview, it fires only when the cursor's row is an `@@`
header (`hunkIdxForRow` returns true); f on a +/- or context line is
swallowed by the panel, matching the "f on a non-hunk line is a
no-op" rule.

### Cursor behaviour

The cursor follows the `@@` header across fold → unfold and back.
The exact rule:

| Before toggle | After toggle | Cursor row |
|---|---|---|
| Folded hunk i's header at row R | Hunk i expanded | Still row R (the @@ is at the same row because the toggle moved the body's rows *down* by `len(h.body)` and kept the header where it was). |
| Expanded hunk i's header at row R | Hunk i folded | Now `R'` where `R' = R` if no later hunks were folded, otherwise the new layout's `layout[i].startRow`. |

The implementation re-runs `renderDiffBody` twice (once before, once
after) and pins the cursor to the post-toggle `layout[i].startRow`.

### `+N` indicator format

```
@@ -2,9 +2,9 @@ ctx  +12
                     ↑
            dim grey 95,99,104 (cDimGrayOpen)
```

`+N` where `N = len(h.body)` — the count of body lines that have been
collapsed into the single line. The indicator uses `cDimGrayOpen /
cDimGrayClose` so it stays subordinate to the header. The literal `+`
is followed by the count with no separator; `strconv.Itoa` formats
`N`.

Centralised in `diffHunkFoldHint` so the format is the single source
of truth (the test pins the helper indirectly via `TestDiffFolding_CountIndicator`).

## Render pathway

```
gitDiffBody(cwd, path, "file"|"commit")  →  []string   (raw git diff --no-color split on \n)
                │
                ▼
splitDiffHunks(body)  →  (pre []string, hunks []diffHunk)
                │
                ▼
renderDiffBody(pre, hunks)  →  (rows []string, layout []diffFoldedHunkLayout)
                │
                ├───────────────────────────┐
                ▼                           ▼
panelDiffInlineLines              toggleDiffHunkFoldAtCursor
(uses layout to map               (uses layout + hunkIdxForRow
 rows into the inline cursor's    to identify which hunk's @@
 range, applies fold state)       header is at the cursor row,
                                  flips diffFolded[i], re-anchors
                                  the cursor onto the same header)
```

The renderer and the toggler share `renderDiffBody` so layout and
rendering can never disagree about which row is which.

## Test pins (`go test -vet=off -count=1 -run TestDiffFolding ./cmd/wagyu/`)

The cycle ships 5 tests in `cmd/wagyu/slash_diff_folding_test.go`:

| Test | Pins |
|---|---|
| `TestDiffFolding_Toggle` | The fold toggle's effect on the rendered body — f on first hunk hides the A body's `CHANGED-A*` lines while leaving B's body intact; f again restores both. Baseline check that the diff starts at 2 hunks unfolded. |
| `TestDiffFolding_CountIndicator` | The folded header carries the `+N` suffix with `N` numeric (regex-free; splits on whitespace and asserts the trailing token starts with `+` and has only digits). |
| `TestDiffFolding_CursorStays` | `m.panel.diffInlineRow` is unchanged across a fold-then-unfold cycle and the row it points at is still an `@@` header in the post-toggle render (the "cursor stays on the same header" rule from the changelog). |
| `TestDiffFolding_AllFolded` | After folding every `@@` header, the rendered body has exactly `preHeaderCount + hunkCount` rows (4 pre-header lines + 2 folded headers = 6 rows in the two-hunk test fixture) with no body content remaining. |
| `TestDiffFolding_FOnNonHunkNoOp` | Cursor on a `+`/`-` line; f leaves the body's shape and `m.panel.diffFolded` empty (a no-op fold). |

Tests skip when `git` is unavailable (`exec.LookPath`).

## Files modified / created

| Status | Path | Notes |
|---|---|---|
| ✏️ modified | `cmd/wagyu/slash_panels.go` | Added `diffInlineRow` and `diffFolded` to `slashPanel`; added the `f` binding and inline-row j/k bindings in `handleDiffPanelKey`. |
| ✏️ modified | `cmd/wagyu/slash_viewer_panels.go` | `panelDiffInlineLines` routes through `renderDiffFromBody`; `diffOpenInline` and `diffCloseInline` reset the fold state; `diffInlineLegend` advertises the new key. |
| ➕ created | `cmd/wagyu/slash_diff_fold.go` | All hunk-folding primitives (`splitDiffHunks`, `renderDiffBody`, `toggleDiffHunkFoldAtCursor`, the `+N` indicator, the cursor layout). |
| ➕ created | `cmd/wagyu/slash_diff_folding_test.go` | The 5 regression tests. |

No third-party modules were added (`go.mod` is untouched — the build
emits a transient re-ordering that is reverted before commit; the
cycle ships zero new deps).

## 硬约束 / 存疑 / limitations

- **No side-by-side viewer.** REF 1.4.0's side-by-side diff viewer is
  out of scope for the clone (cycle 226 §1); the cycle ships the
  hunk-fold gesture onto the plain inline body, which is the clone's
  substitute for the reference's "file". The reference's `f` "full
  file" feature (cycle of full-file collapse, not hunk-by-hunk) is
  not implemented here because the clone has no full-file mapping.
- **Static cursor only.** The fold/cursor works one row at a time;
  global shortcuts (gg/G to jump to first/last hunk, f to fold every
  hunk, F to unfold every hunk) are out of scope. Cycle-227 candidates.
- **Count is body-line count.** The `+N` indicator counts body lines
  (everything between an `@@` header and the next `@@` header, or
  EOF), not +/- lines only. The test pins whatever N the renderer
  produces for its fixture; the choice of "body lines" matches the
  prompt's wording ("the lines inside the hunk").
- **Cursor re-mapping under fold-shifted layouts.** The toggle pins
  the cursor to the same hunk's post-toggle `startRow`, which
  preserves "the cursor stays on the same header" but does *not*
  preserve the absolute row index when a later hunk's body shrinks
  or grows above the cursor. The cursor stays "on the same hunk",
  which is what the changelog describes.
- **Reset behaviour.** Esc-then-Enter on the same file is treated as
  "leave this diff": the fold state is cleared. A quick Esc-then-Enter
  does not preserve fold state.
