# Cycle 129 — picker scroll offset (and a tiny layout tweak)

## Why

Two small bugs left over from earlier cycles, both surfaced in the
hot-path A/B harness (`/tmp/agycmp/hotpath.sh`):

1. **Picker Down navigation appears stuck** after 5+ Down's. Before
   this cycle, the renderer loop was `for i := 0; i < visible; i++`
   where `visible = 5`. When the user pressed Down more than 5 times,
   the highlight's `picker.Idx()` grew past 4 but the loop never
   iterated over indices >= 5 — so the highlight visually disappeared
   even though `Idx()` was advanced. The user complaint was
   "5× Down after / does not move selection".
2. **Warning block butted against the last banner row** instead of
   being separated by a blank line. The reference's cold-start layout
   has banner-row-5, blank, ⚠ Warning, ⎿ body, blank, top separator.
   The clone had banner-row-5, ⚠ Warning directly — the warning
   visually crowded the banner.

## What changes

### Picker scroll offset (the main fix)

| File | Change |
|---|---|
| `cmd/wagyu/tui_v135.go` | new `pickerOffset int` field on `v135RunMode` |
| `cmd/wagyu/picker_view.go` | `pickerView` now keeps the highlight in the visible window via `pickerOffset`, the loop walks `[offset, offset+visible)`, the "↓ N more" tail reports rows below the window (not just rows below row 0) |

The offset is updated **lazily inside `pickerView`**: every call to
the renderer computes `pickerOffset` to keep the highlight in
[0, total). This means MoveDown/MoveUp keep their simple
increment/decrement semantics (no scroll bookkeeping in the key
handler), and the renderer is the single source of truth for what
the user sees.

### Warning blank row

| File | Change |
|---|---|
| `cmd/wagyu/tui_viewport.go` | `scrollHeaderRows` emits `"\n"` before `renderV135Warning` so the banner-to-warning gap is one blank row, matching the reference |

### Tests

| Test                                          | Asserts |
|---|---|
| `TestPickerView_ScrollOffsetKeepsHighlightVisible` | After 8 Down's, the highlighted row is in the rendered output (not lost past the window); `pickerOffset >= 1` |
| `TestPickerView_ScrollOffsetRetreatsOnUp`     | After 10 Down's then 10 Up's, offset does not advance past where it was |
| `TestPickerView_MoreTailTracksWindow`         | Default picker shows "↓ 94 more" (full set minus visible window); after 50 Down's the count has dropped (rows below the new window) |

## Harness changes

| Scenario | Before | After | Note |
|---|---:|---:|---|
| `cold_start` | 31 | 23 | removed extra blank row before `⚠` |
| `picker 001-down5` | 34 | 25 | highlight scrolls past visible window now |

`help_tabs`, `ctrl_o_expand`, `resume_scroll` numbers are unchanged
(this cycle touches only the picker).

## What does NOT change in cycle 129

- **`/help` overlay scroll behaviour** — it has its own helper that
  was a regression-free cycle-86 implementation. Picker and /help
  have different state machines (one is anchored on a buffer
  prefix, the other is a finite 3-tab state) and don't share
  scroll logic.
- **Picker **↕️ PageUp/PageDown scrolling** — the reference may or
  may not support it (no capture available offline). The current
  implementation only accepts Up/Down, which is the simpler
  and almost certainly correct behaviour for this UI. Cycle 130+
  can add PageUp/PageDown if the ref proves to support it.
- **Banner layout rework** — still cycle-124 work; cycle 129 only
  adds the single blank row.

## Hard constraints / 存疑 / limitations

- 【推测】The "↓ N more" tail wording (`%d more`) matches the
  ref capture when `total - 5 - offset > 1` and shows `"1 more"`
  when exactly one row is below. The reference *might* say
  `"1 more row"` (with "row" suffixed); without a recovery of a
  full tail table this is the conventional form and is what the
  cycle-74 parser expected.
- The `pickerOffset` field on `v135RunMode` rather than on
  `PickerState` keeps the picker package pure (no cross-package
  state-mutation). If `PickerState` ever needs to be reused in
  non-v135 contexts (e.g. an internal picker for `agy models`),
  copy the offset field to that struct as well.
- The cold_start diff dropped 31→23 thanks to the blank-row fix,
  but the residual 23 chars are unrelated layout choices (warning
  block visibility, status-row model label, `(Antigravity Starter
  Quota)` text vs not) — those are cycle-124 leftovers outside
  this cycle's scope.
