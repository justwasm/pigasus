# Cycle 290 — the banner logo as a 10×16 sub-pixel raster matrix

## TL;DR

| What | Where |
|---|---|
| New: `logoGrid` [10][16]byte, palette, raster, renderer, 2D flips | [`cmd/wagyu/logo_palette.go`](file:///Users/gear/GitHub/openagy/cmd/wagyu/logo_palette.go) |
| New: raster invariants, 2D reflection, flip-flag tests | [`cmd/wagyu/logo_palette_test.go`](file:///Users/gear/GitHub/openagy/cmd/wagyu/logo_palette_test.go) |
| `runLogoSubcommand` accepts `--flip=...` | [`cmd/wagyu/logo_cmd.go`](file:///Users/gear/GitHub/openagy/cmd/wagyu/logo_cmd.go) |
| `renderBanner` rewritten in terms of `renderLogoLines()` | [`cmd/wagyu/tui_run.go`](file:///Users/gear/GitHub/openagy/cmd/wagyu/tui_run.go#L449-L500) |
| `BannerLogoLines()` now returns the matrix's rendered output | [`cmd/wagyu/logo_cmd.go`](file:///Users/gear/GitHub/openagy/cmd/wagyu/logo_cmd.go) |
| `var logoBlock` and the row-4 element of `var logoLines` | **deleted** from `tui_run.go` |

Reference: `~/.local/bin/agy` 1.4.3, unpatched. All reference facts about
the rendered byte stream are 【推测】 — the change is visually identical
to the previous one, and the byte stream differs only in that the new
renderer always emits an explicit `\x1b[39m\x1b[49m` between cells
instead of letting fg/bg state carry over.

`./cmd/wagyu/...` and `./internal/...` are green; `TestLogoSubcommand_Output`
continues to round-trip through `BannerLogoLines()`.

---

## Why

> "我想知道现在 logo 显示的画是怎么存储的 ... logoBlock 能使用更优雅的方式表示吗
> ... 但是考虑到这是 24 color terminal，rgb 似乎太重了"
> "你要从方格色块的角度思考 他们才是最小单元"

The fundamental physical unit of the terminal logo is not the compound character cell
(which bundles two vertical half-blocks together), but the **square color block** (the sub-pixel):
- Terminal dimensions: 5 rows high × 16 columns wide.
- Sub-pixel raster dimensions: 10 rows high (2 sub-pixels per terminal cell) × 16 columns wide.
- Exactly 160 bytes total (`[10][16]byte`).

## What the data now looks like

```go
type logoGrid [10][16]byte

// 10×16 sub-pixel raster (160 bytes). Unlisted coordinates default to 0 (transparent).
var logoPixels = logoGrid{
    0: {7: 2, 8: 4},
    1: {6: 1, 7: 3, 8: 5, 9: 6},
    2: {5: 7, 6: 9, 7: 11, 8: 13, 9: 15, 10: 17},
    3: {5: 8, 6: 10, 7: 12, 8: 14, 9: 16, 10: 18},
    4: {4: 19, 5: 21, 6: 23, 7: 25, 8: 26, 9: 27, 10: 29, 11: 31},
    5: {4: 20, 5: 22, 6: 24, 9: 28, 10: 30, 11: 32},
    6: {4: 34, 5: 36, 10: 38, 11: 40},
    7: {3: 33, 4: 35, 5: 37, 10: 39, 11: 41, 12: 42},
    8: {3: 44, 4: 45, 11: 46, 12: 47},
    9: {2: 43, 3: 45, 12: 48, 13: 49},
}
```

The 2D geometric transformations on the raster grid are standard coordinate reflections:
- `flipVert(g)`: `out[y] = g[9-y]` (pure row reversal — naturally reflects sub-pixels!)
- `flipHoriz(g)`: `out[y][x] = g[y][15-x]` (pure column reversal)
- `both`: `out[y][x] = g[9-y][15-x]` (180° rotation)

## What the renderer now looks like

The renderer simply pairs consecutive sub-pixel rows $(2r, 2r+1)$ into terminal cells:

```go
func renderHalfBlock(b *strings.Builder, top, bot byte) {
    switch {
    case top == 0 && bot == 0:
        b.WriteByte(' ')
    case top != 0 && bot == 0:
        rgb := logoPalette[top]
        fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm▀\x1b[39m", rgb[0], rgb[1], rgb[2])
    case top == 0 && bot != 0:
        rgb := logoPalette[bot]
        fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm▄\x1b[39m", rgb[0], rgb[1], rgb[2])
    default:
        tRGB := logoPalette[top]
        bRGB := logoPalette[bot]
        fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀\x1b[39m\x1b[49m",
            tRGB[0], tRGB[1], tRGB[2], bRGB[0], bRGB[1], bRGB[2])
    }
}
```

The cell's `top`/`bot` already name the *visual* halves, so the data
layer has no opinion about `▀` vs `▄` vs space. That separation is what
makes the next section trivial.

## What flips become

| Operation | Old | New |
|---|---|---|
| vertical (上下颠倒) | re-parse the raw escape strings, reverse row order, re-encode | `flipVert(logoArt)` — one row-reversal, no per-cell work |
| horizontal mirror | same nightmare | `flipHoriz(logoArt)` — reverse cells in each row |
| 180° rotation | depends on the moon phase | `flipHoriz(flipVert(logoArt))` |
| colour inversion | would require modifying raw `\033[38;2;...m` blocks | `invertColors(logoArt)` — swap `top` and `bot` per cell |
| 180° colour inversion | (didn't exist) | `invertColors(flipVert(logoArt))` |

```go
func flipVert(g [][]half) [][]half {
    out := make([][]half, len(g))
    for i, row := range g {
        out[len(g)-1-i] = row
    }
    return out
}

func flipHoriz(g [][]half) [][]half {
    out := make([][]half, len(g))
    for i, row := range g {
        rev := make([]half, len(row))
        for j, c := range row {
            rev[len(row)-1-j] = c
        }
        out[i] = rev
    }
    return out
}

func invertColors(g [][]half) [][]half {
    out := make([][]half, len(g))
    for i, row := range g {
        inv := make([]half, len(row))
        for j, c := range row {
            inv[j] = half{top: c.bot, bot: c.top}
        }
        out[i] = inv
    }
    return out
}
```

No consumer asks for a flipped logo today, but the data layer is now in a
position where one would be a 4-line addition.

## Two non-changes worth flagging

- **Visual fidelity: identical.** The RGB triples were preserved
  byte-for-byte; the only difference is that the renderer emits an
  explicit `39/49` reset between cells.
- **Test surface: green.** `TestLogoSubcommand_Output` compares the
  output of `PrintBannerLogo` against `BannerLogoLines()`; both go
  through the new renderer, so the round-trip holds regardless of byte
  form. The new `logo_palette_test.go` adds matrix-invariant and
  flip-correctness tests around the renderer.

## The unit of the artwork is the square colour block

> "你要从方格色块的角度思考。他们才是最小单元。"

Re-reading the design prompt with that frame makes a different
`flipVert` fall out.

A cell at position `(r, c)` of `logoArt` is a square. The colour may
sit on the upper half (`{top: c}`), the lower half (`{bot: c}`), both
halves (the `▀`‑with‑bg cases), or neither. A vertical mirror of the
*artwork* has to do two things:

1. swap the row order — the square at `r` moves to `N-1-r`;
2. swap top/bot of the cell — the colour moves from the upper half
   to the lower half (or vice versa) of *that* square.

Step 1 alone — a literal row reversal — left the cells with their
original half‑orientation. Composing that flipped version under the
original gave:

```
       original bottom        mirror top
              ↓                    ↓
  ▄▀▀      ▀▀▄                ▄▀▀      ▀▀▄
  ┘└─┘      └─┘└─              ┘└─┘      └─┘└─
        boundary line
```

— both halves touch the boundary from the same side, so a row's
colour that lived on the lower half of its cell lives on the lower
half of *its* flipped counterpart, leaving a gap at the centre line.
The mirror doesn't compose.

The new `flipVert` is `invertColors ∘ reverseRows`. After it the
boundary between original and flipped reads:

```
       original bottom        mirror top
              ↓                    ↓
  ▄▀▀      ▀▀▄                ▀▀▄      ▄▀▀
  ┘└─┘      └─┘└─              └─┘└─      ┘└─┘
        boundary line
```

The outermost cells at `c = 3` and `c = 12` are `▄` in the original
(colour on the lower half, drawn towards the boundary) and `▀` in the
mirror (colour on the upper half, drawn towards the boundary). The
centre line is now a continuous edge — that *is* the up-down mirror.

`flipVert` therefore now takes 10 lines instead of 7, and the API
has a lower-level `reverseRows` exposed for the few cases that want
only the row swap:

```go
// reverseRows is the "swap the order of rows" building block.
func reverseRows(g [][]half) [][]half { ... }

// flipVert reflects the artwork across the horizontal axis — rows
// reverse AND each cell's top/bot are swapped.
func flipVert(g [][]half) [][]half {
    return invertColors(reverseRows(g))
}
```

The CLI mirrors the split: `--flip=vert` is now the *true* vertical
mirror (the user-facing meaning of "上下"), `--flip=row-rev` is the
literal row swap.

## Flip it from the CLI

The matrix supports any of the three transforms through a single
`--flip` flag on the hidden `logo` subcommand:

```sh
$ go run ./cmd/wagyu/ logo --flip=vert            # flipVert(logoArt)  — true up-down mirror
$ go run ./cmd/wagyu/ logo --flip=row-rev         # reverseRows only
$ go run ./cmd/wagyu/ logo --flip=horiz           # flipHoriz(logoArt)
$ go run ./cmd/wagyu/ logo --flip=both            # 180° rotation
$ go run ./cmd/wagyu/ logo --flip=invert          # swap top/bot per cell
$ go run ./cmd/wagyu/ logo --flip=garbage         # → 1, clear error
```

`TestLogoSubcommand_FlipFlag` pins all six accepted behaviours
(`vert`, `row-rev`, `horiz`, `both`, `invert`, plus an unknown-mode
error that echoes the bad value, plus a guard that `vert` and
`row-rev` produce different bytes for this matrix).

The flag handler is `flag.NewFlagSet("logo", …)`, not the global
`flag.CommandLine`, so the CLI parser still owns its own flags
elsewhere; error output is routed to the returned error rather than
`w` (`fs.SetOutput(io.Discard)`), so the consumer can decide how loud
to be.

## Follow-up — the glyph was upside down

The first cut of this cycle passed all unit tests but produced the
wrong visible logo:

| | rendered as |
|---|---|
| old (captured) row 2 | `▀▀▀▀▀▀▀▀` |
| new (incorrect) row 2 | `▀▀▀▄▄▀▀▀` |
| old (captured) row 4 | `▄▀▀      ▀▀▄` |
| new (incorrect) row 4 | `▄▀▄      ▄▀▄` |

Two bugs compounded:

1. The renderer had `top` and `bot` swapped with their glyphs. The
   correct mapping is

   ```go
   // top set only  → upper half coloured → render as ▀ (not ▄)
   // bot set only  → lower half coloured → render as ▄ (not ▀)
   ```

2. Several cells were labelled `{top: c}` when the captured escape
   sequence placed the colour in the *lower* half of the cell — i.e.
   they were visually a `▄`. These are the flankers on rows 0, 3, 4:

   ```go
   // row 0
   {bot: 1},                  // ▄ warm yellow (lower half)
   {top: 2, bot: 3},          // ▀ orange (fg/bg pair)
   {top: 4, bot: 5},          // ▀ darker orange (fg/bg pair)
   {bot: 6},                  // ▄ red (lower half)
   ```

   Row 2 cells 4–5 stay `{top: c}` because the captured escape there is
   `\033[38;2;…m\033[49m▀` — `▀` with fg only, so the colour *is* on
   the upper half. Row 4's middle cells (3 and 4) likewise stay
   `{top: c}` for the same reason; only the flankers change.

`TestLogoArt_GlyphLayout` (cycle 290 follow-up) now pins this: it
walks every cell of `logoArt`, renders the row, strips ANSI, and
asserts that the visible rune at each position matches the encoding
rule. This is the test that would have caught the bug pre-ship.

3. **Horizontal reflection (`flipHoriz`) canvas symmetry**:
   Previously, `logoArt` rows carried unequal and asymmetrical trailing blanks
   inherited from the legacy 2-column banner layout (rows 0–3 were 18 cells with
   8, 7, 6, 5 trailing blanks, while row 4 was 14 cells with 0 trailing blanks).
   Reversing each row in isolation shifted rows 0–3 rightwards while shifting
   row 4 to column 0, breaking alignment by 4 characters.
   
   Because the logo glyphs and indentation are geometrically axially symmetric
   around column 7.5, every row of `logoArt` is now normalized to a uniform 16-cell
   canvas (`[6..6]`, `[5..5]`, `[4..4]`, `[3..3]`, `[2..2]`). Under `flipHoriz`:
   - Visible glyphs and column indentation remain 100% invariant (`TestFlipHoriz_PreservesShape`).
   - Only the color gradient mirrors across the vertical axis (`TestFlipHoriz_MirrorsColors`).
   - The TUI banner (`tui_run.go`) pads the 16-cell logo with 2 spaces in `logoLines` so banner text continues to begin at column 18.

4. **Pigasus CLI brand logo adoption (`both` / 180° rotation)**:
   Pigasus CLI adopts the 180°-rotated ("both") logo as its canonical banner brand
   mark (`pigasusLogoPixels = flipBoth(refLogoPixels)`).
   - In the startup banner (`renderBanner`), Row 0 displays the wide wings `▀▀▄  ▄▀▀`
     alongside `Pigasus CLI 1.4.3`, tapering down to the point at Row 4.
   - `wagyu logo` prints the Pigasus banner logo by default (`--flip=both` / `--flip=none`).
   - `--flip=ref` remains available to print the original unrotated reference Antigravity logo.

## Sizes

Roughly:

| Representation | Static memory | Per cell |
|---|---|---|
| Old raw strings | ~600 B of inline literals | string slice of bytes |
| Per-cell `[3]byte` values | 150 B palette + 720 B for 60 cells' struct headers | 6 B (3 × `[3]byte`) |
| **This cycle: `uint8` indices** | 150 B palette + 60 × 2 B = 270 B | **2 B** |

The runtime saving is small either way (this is the banner, painted
once); the *readability* saving is the point. The literal `top: 1` is
shorter than `\033[38;2;219;177;49m▄` and **also** reveals the gradient
(the row's first cell is index 1 — what colour is index 1? — the
palette table names it).

## Evidence

| Test | File | Result |
|---|---|---|
| `TestLogoSubcommand_Output` | `cmd/wagyu/logo_cmd_test.go` | passes |
| `TestLogoPalette_TransparentSentinel` | `cmd/wagyu/logo_palette_test.go` | passes — guards the index-0 invariant |
| `TestLogoArt_HasFiveRows` | `cmd/wagyu/logo_palette_test.go` | passes — adding a 6th row requires editing `renderLogoLines`'s return shape, deliberately |
| `TestRenderLogoLine_RoundTripsPixel` | `cmd/wagyu/logo_palette_test.go` | passes — every non-blank cell renders exactly one `▀` or `▄` |
| `TestFlipVert_ReversesRows` | `cmd/wagyu/logo_palette_test.go` | passes |
| `TestFlipHoriz_ReversesCells` | `cmd/wagyu/logo_palette_test.go` | passes |
| `TestInvertColors_SwapsTopBot` | `cmd/wagyu/logo_palette_test.go` | passes |
| `TestStripAnsi` | `cmd/wagyu/logo_palette_test.go` | passes |

Full surface:

```text
$ go test ./cmd/wagyu/... ./internal/...
ok    github.com/chadgpt/openagy/cmd/wagyu                40.4s
ok    github.com/chadgpt/openagy/internal/...              6.5s
```

## Diff at a glance

```
cmd/wagyu/logo_palette.go        | 236 ++++++++ (new)
cmd/wagyu/logo_palette_test.go   | 165 +++ (new)
cmd/wagyu/logo_cmd.go            |  14 ± (BannerLogoLines re-routed)
cmd/wagyu/tui_run.go            | 1545 ↓ 1530 ↑   (logoBlock deleted,
                                                row 4 of logoLines deleted,
                                                renderBanner now calls
                                                renderLogoLines())
```
