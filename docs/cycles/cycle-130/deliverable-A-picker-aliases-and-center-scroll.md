# Cycle 130 — picker alias rendering + centre-scroll

## Why

Continuing the visual fidelity push, two small picker-overlay details
were left over from cycles 60/93/129:

1. **The reference renders the first alias of each command in the
   picker overlay** (e.g. `/clear (new)`, `/codesearch (cs)`,
   `/config (settings)`). The clone's catalog already had aliases
   (`tui135.SlashCommand.Aliases []string` since cycle 83), but the
   picker overlay never read them, so the rows read like
   `/clear                  Clear conversation...` — visually
   inconsistent with the reference.

2. **After scrolling, the reference centres the highlighted row in
   the visible window**, not pinning it to the bottom (cycle-129's
   pager behaviour). After 5× Down's, the highlight lands on
   `/clear` (idx 5) and the reference's window shows rows [3..7]:
   `/btw`, `/changelog`, `> /clear (new)`, `/codesearch`,
   `/config` — with `↑ 3 more` above and `↓ 91 more` below.

## What changes

### `cmd/wagyu/picker_view.go`

- The picker loop's name string is built as
  `"> " + c.Name + aliasSuffix` (or `"  " + c.Name + aliasSuffix`
  for the non-highlighted path). `aliasSuffix = " (" + c.Aliases[0] + ")"`
  when the catalog has any aliases; otherwise the empty string.
- `pickerOffset` now advances to `hi - visible/2` (= 2) once the
  highlight reaches `visible-1` (= 4), so the highlight lands at
  row index 2 of the 5-row window. This matches the reference's
  centre-scroll behaviour captured at
  `/tmp/agycmp/hotpath/picker/ref.001-down5.txt`.
- A new `↑ N more` header is emitted above the visible rows when
  `pickerOffset > 0`, mirroring the `↓ N more` tail that's been
  there since cycle 91. Same dim 95,99,104 grey as the tail.

### Tests (`cmd/wagyu/picker_view_test.go`)

| Test | Asserts |
|---|---|
| `TestPickerView_FirstAliasInline` | After 5× Down's, `> /clear (new)`, `/codesearch (cs)`, `/config (settings)` are all present in the rendered picker; the multi-alias join form `/codesearch (cs, search)` is **not** present (only first alias) |

The cycle-129 tests (`TestPickerView_ScrollOffsetKeepsHighlightVisible`,
`TestPickerView_ScrollOffsetRetreatsOnUp`, `TestPickerView_MoreTailTracksWindow`)
were already passing and are re-run with the new offset formula.

## What does NOT change in cycle 130

- **Multi-alias rendering** (commands with 2+ aliases) — the picker
  still shows only the first. The reference's `/help COMMANDS` tab
  is the right place to render the full alias list (cycle 83 already
  does this); the picker overlay is space-constrained.
- **Description wrapping / truncation** — the reference truncates
  descriptions that overflow column ~120 with a trailing `…`. The
  cycle-130 clone still emits the full description; the harness
  diff shows ~3 trailing-character differences on `/btw` and
  `/codesearch` that are pure-wrap behaviour. Cycle 131+ candidate.
- **Picker PageUp/PageDown** — still single-row Up/Down only; the
  reference may support PageUp/PageDown (unverified offline).

## Harness changes

| Scenario | Before | After | Note |
|---|---:|---:|---|
| `cold_start` | 14 | 23 | *trailing-blank-row drift* (whitespace at end of capture) |
| `picker 000-slash` | 31 | 23 | alias renders immediately on `/` (cycle 130 wins on top of cycle 129) |
| `picker 001-down5` | 34 | 25 | centre-scroll + `↑ 3 more` + alias all align |
| `picker 002-closed` | 22 | 21 | trailing-blank-row drift |

The trailing-blank-row drift in `cold_start` (14 → 23) is unrelated
to this cycle: the script capture's exact trailing-newline count
varies across runs depending on tmux timing. The *content* of the
diff is unchanged (the warning-block blank row from cycle 129 is
still there).

## Hard constraints / 存疑 / limitations

- 【推测】The reference's `↑ N more` uses the dim 95,99,104 colour
  directly without bold — this matches the `↓ N more` style cycle 91
  set up. Without direct ANSI capture of an actual scrolled picker
  row, the colour is the conservative choice (same as the tail row).
- 【推测】The visible window count (5 rows) is hard-coded; the
  reference is most likely also 5 (the captured `/tmp/agy_slash.txt`
  shows 5 visible rows + tail). If the reference ever supports a
  resizable window the constant should become a derived field.
- **Alias first-only** is the conventional choice — most CLI
  pickers (vscode, claude-cli, aider) show only the primary alias.
  If the ref renders all aliases comma-separated for some commands,
  cycle 131+ adjusts.