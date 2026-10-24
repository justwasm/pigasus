# Cycle 182 — `/help` overlay: scrolling window model + pixel-perfect v1.4.0 panels

## Why

Cycle 181 opened the help overlay on `?` and moved the target to agy 1.4.0,
but the *contents* were still the cycle-137 shape: three tabs paginated in
fixed 18-row pages, a page indicator that only ever printed `[1-18]`-style
page numbers, an uncoloured COMMANDS list, and a status row that vanished
whenever the overlay was open.

Driving the real binary at 220×30 and diffing cell-by-cell (raw SGR) showed
four independent divergences:

1. **The list model is a window, not pages.** REF keeps a cursor over the
   *whole* catalog and scrolls one row at a time; `Right`/`Left` cycle tabs
   instead of paging, and `Ctrl+Home`/`Ctrl+End` jump to the ends.
2. **The window is height-adaptive.** 18 is only correct at 30 rows.
3. **The emitted view is capped at `height + 8` rows.** REF lets the overlay
   run past the fold and scrolls; the status row is simply truncated away
   when the panel is taller than that.
4. **The panels were missing their colour**, and the COMMANDS description
   column truncates *relative to the terminal width*.

## What changed

### 1. Window model replaces pagination (`cmd/wagyu/history.go`)

`helpCommandsPageNext/Prev/CursorDown`, `helpCommandsPages` and
`helpCommandsVisibleRowsOf` are gone. In their place, shared by both list
tabs (`total` is passed in, so COMMANDS=99 and SHORTCUTS=34 share one
implementation):

| helper | behaviour |
|---|---|
| `helpListLen(tab)` | catalog length for tab 1/2, 0 for GENERAL |
| `helpListWindowFor(height)` | window size (see §2) |
| `helpListVisible(total, window, size)` | rows actually drawn (short last window) |
| `helpListClampWindow(total)` | safety net after a resize |
| `helpListDown/Up(total)` | move the cursor; scroll at an edge; wrap at both ends |
| `helpListHome()` / `helpListEnd(total)` | `Ctrl+Home` / `Ctrl+End` |

`Right`/`Left`/`Tab` now cycle tabs and **reset window + cursor to 0** —
REF does not preserve per-tab scroll. Cycle 137 had `Right` = page-forward,
which diverges from REF; this replaces that decision.

Verified against REF at 220×30 (COMMANDS):

```
entry          [1-18 of 99 items]   cursor at row 0
Down ×17       [1-18 of 99 items]   cursor at row 17 (window edge)
Down ×18       [2-19 of 99 items]   window scrolled one row
Ctrl+End       [82-99 of 99 items]  cursor at row 98
Down at 98     [1-18 of 99 items]   wraps to row 0
Up at 0        [82-99 of 99 items]  wraps to row 98
```

`PageDown`/`PageUp`/`End`/`Home` have **no effect** in REF's overlay.

### 2. Height-adaptive window size

Recovered by scanning REF v1.4.0 at heights 13…60:

```
h :  13 14 15 16 17 18 20 22 24 26 28 30 32 33 34 36 38 40 50 60
n :   4  5  6  7  8  9 11 12 14 15 17 18 20 20 21 23 24 26 33 41
```

Every sample satisfies

```
n = min(3h/4 - 4, h - 9)      // integer division, clamped to >= 1
```

`h - 9` dominates below h≈17. `height <= 0` (the terminal has not reported
its size yet) falls back to 18 — the pre-182 value the byte-exact snapshots
and the unit tests assert.

### 3. `height + 8` cap in `View()` (`viewOverflowRows`)

REF emits `<transcript> <sep> <prompt> <sep> <panel> <status>` and never
more than `height + 8` rows; past that it drops whole rows from the
**bottom**. The status row therefore disappears when the panel is tall:

| tab | panel rows | status row visible? |
|---|---|---|
| general | 15 | `h >= 21` |
| commands | 7 + n | `h >= 33` (n = 20 at h = 32..33) |
| shortcuts | 8 + n | `h >= 38` (n = 24 at h = 38) |

At 220×30 the COMMANDS panel is 39 rows and SHORTCUTS 40, so both clamp to
38 and lose the model label; GENERAL keeps it. Confirmed by REF captures at
h=14 and h=20, where the *panel* is cut and the banner stays put.

**Consequence for the transcript budget.** `viewportHeight()` charged the
help panel against the transcript, which clipped the banner to a single row
at 220×30 — REF never scrolls the banner off. Cycle 182 subtracts the panel
back out while `helpMode` is on and adds the same `+8` allowance:

```go
extra := viewRows(viewBody(nil))
budget := m.height
if m.helpMode {
    extra -= viewRows(renderHelp(...))   // the panel may overflow
    budget += viewOverflowRows
}
```

### 4. Panel styling + truncation

* COMMANDS: header bold `38;2;60;64;67`; cursor row name brand blue
  `38;2;66;133;244`; every description `38;2;60;64;67`; indicator
  `38;2;95;99;104`.
* SHORTCUTS: normal row `  ` + blue(key) + pad + `38;2;95;99;104`(desc);
  cursor row blue(`> ` + key) + pad + `38;2;60;64;67`(desc); the
  `  /keybindings  to customize` sub-label.
* `buildShortcutRow` now splits by **column position** (marker 2 + key
  field 32 → key = `runes[2:34]`, desc = `runes[34:]`), not by the first
  space run.
* COMMANDS description truncation is width-relative: description column 51
  (0-based), cap = `width - 52`, overflow becomes `cap-3` chars + `...`;
  `width <= 0` disables it. Measured REF: w=60→8, 80→28, 100→48, 120/220
  untruncated.
* GENERAL email row carries the plan tier suffix
  (`navigaid@gmail.com (Antigravity Starter Quota)`), matching the banner —
  the cycle-145 comment that claimed otherwise was reading a stale capture.

### 5. Three smaller fixes

* `renderWarning` emitted an extra blank row; REF has exactly one blank
  between the warning body and the top separator. Removed.
* The cycle-94 prompt post-process matched only the 256-colour
  `\x1b[38;5;69m> \x1b[m`; bubbles v2 now emits the 24-bit brand blue, so
  the patch had silently stopped firing and the prompt space stayed blue.
  It now matches the 24-bit form first and falls back to the old one.
* `statusRow()`'s right label reused `leftOpen`/`leftClose`, which the
  blanking branches (typing / picker / help) set to `""` — so the model
  label lost its colour under the overlay. It now uses
  `cDimGrayOpen`/`cDimGrayClose` directly.

## Verification

`/tmp/agycmp/full_screen_diff.py` (220×30, raw SGR, tmux `capture-pane -p -e`):

| state | result |
|---|---|
| IDLE screen | **pixel-perfect** |
| GENERAL | **pixel-perfect** |
| COMMANDS | **pixel-perfect** |
| COMMANDS Down ×20 | **pixel-perfect** |
| SHORTCUTS | 2 divergences, both invisible (below) |
| SHORTCUTS / COMMANDS text (`text_diff_help.py`) | **text-perfect** in every state |

### Accepted, invisible divergences

1. **`ctrl+_, ctrl+shift+-` pad cells.** REF writes 10 padding spaces
   *inside* the blue wrap; the clone erases them (`\x1b[10X`). Spaces have no
   glyph, so the visible screen is identical. REF's own byte stream is
   frame-history dependent here (it is a cell-diff renderer), so a strict
   raw-SGR match is not achievable even for REF against itself.
2. **`ctrl+k` row reset placement.** The clone's line ends in
   `\x1b[38;5;235m\x1b[K\x1b[m`; tmux's `-e` capture attributes the
   trailing reset to the next capture line. The stripped text grid is
   identical (31 lines both sides), so this is a capture artefact, not a
   screen difference.

### Tracked, not fixed here

Scrolling to `Ctrl+End` on the COMMANDS tab exposes **stale plugin
descriptions**: the clone's `internal/tuilib/commands.go` is a hard-coded
snapshot, while REF reads each plugin's `SKILL.md` frontmatter at startup.
Example — `/science:interpro-database`:

```
REF: Identify domains, families, and sites in proteins; find all proteins …
OUR: Query InterPro for protein families; find all proteins …
```

57 of the 99 rows diverge past ~131 chars. This is a **data** gap, not a
rendering one, and is carried to the plugin-catalog cycle.

## Tests

`cmd/wagyu/help_cycle182_test.go` (new):

* `Test_HelpListWindowFor` — the 20 measured heights + the `<= 0` fallback.
* `Test_HelpListVisible` — short last window, past-the-end, tiny catalog.
* `Test_HelpListNavigation` / `…ShortCatalog` — edge scroll, both wraps,
  `Ctrl+Home`/`Ctrl+End`, tab reset.
* `Test_HelpListLenMatchesCatalogs` — 99 commands / 34 shortcuts.
* `Test_BuildCommandRowTruncation` — w=220/60/0 and the untouched short case.
* `Test_BuildCommandRowCursorStyling` — cursor blue vs normal plain.
* `Test_RenderHelpGeneralTierSuffix` — suffix present; empty tier leaves no
  dangling ` ()`.
* `Test_StatusRowRightLabelColouredInHelpMode` — overlay keeps the wrap,
  idle row unchanged.

## 硬约束 / 存疑

* **硬约束**: REF uses a cell-diff renderer (`\x1b[<n>X` ECH + CUP + `\x1b[m`),
  so its raw byte stream is not frame-deterministic. Raw-SGR equality is a
  *sufficient* but not *necessary* condition; the stripped text grid is the
  authoritative visible comparison.
* **存疑**: the `height + 8` cap was measured on the three help panels only —
  it is the only view tall enough to overflow. Whether REF applies the same
  constant to a long transcript + overlay is unverified; the clone's
  transcript budget keeps the banner put and lets the panel absorb the
  truncation, which matches every measured REF state.
* **存疑**: `helpListWindowFor` is fitted to 20 integer samples between
  h=13 and h=60. The `h - 9` term is inferred from h<=18; a taller scan
  could not distinguish it from another line with the same samples.
* The `ctrl+_` / `ctrl+k` divergences are declared cosmetic above; a
  stricter harness (per-cell colour extraction rather than `capture-pane -e`)
  could in principle show a real difference. None is visible.
* Not fixed: the stale plugin catalog (see above) and REF's odd habit of
  restoring a previous `/help` scroll position on a fresh run
  (`[58-67 of 152 items]` in one capture, `[1-10 …]` in the next).
