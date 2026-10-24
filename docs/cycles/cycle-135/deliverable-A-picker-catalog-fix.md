# Cycle 135 — picker catalog off-by-5 (chrome-devtools-plugin:*)

## Why

The v1.3.6 reference picker overlay shows **94 entries** (5 visible +
"↓ 89 more" at empty prefix). The cycle-131 clone showed **99** (5 +
"↓ 94 more") — off by 5.

Investigating which 5 entries were missing from the reference revealed
that they were the namespaced `/chrome-devtools-plugin:*` plugin
commands (`a11y-debugging`, `chrome-devtools`, `debug-optimize-lcp`,
`memory-leak-debugging`, `troubleshooting`). Cycle 131's deliverable
flagged the discrepancy but didn't fix it; cycle 135 cuts the gap.

## What changes

### `internal/tui_v135/commands.go`

`FilterSlashCommands` was a prefix matcher. Cycle 135 changes it to:

1. **Substring match** instead of prefix match. The reference v1.3.6
   picker matches `/s` against `/codesearch`, `/config (settings)`,
   `/resume (switch)` — anything with an "s" anywhere in the name.
   Confirmed by capturing the reference picker with `/s`:
   5 visible + "↓ 61 more" = 66 matches.
2. **Exclude chrome-devtools-plugin:*** from the picker. Five entries.
   The reference shows them in /help COMMANDS tab (footer
   "[1-18 of 99 items]") but not in the picker (header "↓ 89 more").

Two new helpers:

- `containsFold(s, substr)` — strings.Contains with ASCII case-folding.
  Used by the picker's substring filter so `/SKILL` matches `/skills`.
- The filter now uses `strings.Contains(c.Name, "chrome-devtools-plugin:")`
  to skip the five namespaced entries.

The catalog (`DefaultSlashCommands`) keeps all 99 entries unchanged,
because /help COMMANDS tab still shows them all.

### `internal/tui_v135/picker.go`

Unchanged. The picker renders `m.picker.Len()` as `len(p.all)`, which is
`len(FilterSlashCommands(prefix))`, which is now 94 for empty prefix.

### `cmd/wagyu/tui_v135_test.go`

`TestV135_PickerCatalogMatchesRef` is updated to assert both:

- `len(DefaultSlashCommands) == 99` (catalog matches /help footer)
- `len(FilterSlashCommands("/")) == 94` (picker matches "↓ 89 more")

### `cmd/wagyu/picker_view_test.go`

`TestPickerView_MoreTailTracksWindow` now expects `"89 more"`
(was `"94 more"`).

## What does NOT change in cycle 135

- The catalog (`DefaultSlashCommands`) is unchanged — all 5 plugin
  entries remain. `/help COMMANDS tab` still shows them.
- `/help COMMANDS tab` filtering — the cycle-83 helper that powers
  /help has its own filter and continues to produce a 99-row catalog.
- Aliases — the substring filter applies to the command name only,
  not aliases (matching the reference behaviour: `/cs` would match
  the alias, not the command, but `/codesearch` is the form that's
  typically typed).

## Hard constraints / 存疑 / limitations

- 【推测】The substring filter is the simplest rule that fits the
  captures. The reference may apply a more sophisticated scoring
  algorithm (fuzzy match, prefix-boosted, etc.) — but the captures
  show the substring rule produces the same counts in the scenarios
  tested.
- 【推测】Why does the reference suppress `chrome-devtools-plugin:*`
  from the picker but not from /help? Most likely an opt-in flag on
  the plugin entry itself. Cycle 135 mirrors the visible behaviour
  with a substring check; if a later cycle finds the underlying
  flag it can replace this with a per-entry field.
- The picker view of `/help` (which lists all 99 entries) does not
  yet use the new substring filter — it uses the cycle-83 /help
  helper. Cycle 136+ may consolidate if the behaviour diverges.