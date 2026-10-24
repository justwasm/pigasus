# Cycle 147 — Restore cycle 135 chrome-devtools-plugin:* picker filter + /help COMMANDS tab matches catalog

## Why

Cycle 146 un-filtered the picker based on a misread of
`picker.000-slash.txt`'s "↓ 94 more". The capture actually
means: empty-prefix picker shows 5 visible + 89 hidden = 94
entries; the catalog has 99. The ref's picker filters out 5
chrome-devtools-plugin:* plugin commands (cycle 135 was right;
cycle 146's revert was wrong).

Also: the /help COMMANDS tab footer reads "[1-18 of 99 items]"
in the ref, NOT 94 — the /help surface keeps all 99 catalog
entries (no filter). Picker filters to 94, /help keeps 99.
The picker filter is one-sided.

Captured at `/tmp/agycmp/hotpath/picker/ref.001-down5.txt`:

```
   /clear (new)                                     Clear conversation and start a new one
   /codesearch (cs)                                 Search code in the workspace (usage: /codesea...
   /config (settings)                               Open settings panel
    ↓ 86 more
```

86 + 3 (offset) + 5 (visible) = 94 total entries — five
fewer than the 99-entry catalog.

Captured at `/tmp/agycmp/hotpath/help_tabs/ref.001-second.txt`:

```
   /help                                            Show available commands and keybindings
   /hooks                                           Manage hook configurations for tool events
  [1-18 of 99 items]
```

The /help tab footer is "[1-18 of 99 items]" — the catalog
count, not the picker count.

## What changes

### `internal/tui_v135/commands.go::FilterSlashCommands`

- Restored the `if strings.Contains(c.Name, "chrome-devtools-plugin:") { continue }`
  filter (cycle 135). Cycle 146's revert was wrong.
- Restored the `import "strings"` line at the top of the file.

### `cmd/wagyu/tui_v135.go::renderV135HelpCommands`

- Keeps `cmds := tui135.DefaultSlashCommands` (all 99 entries).
  A first-pass cycle-147 attempt changed this to
  `FilterSlashCommands("/")` to match the picker (94 entries)
  but the ref actually shows 99 — same as the catalog. Reverted
  to `DefaultSlashCommands`. The picker is the only surface
  that filters out the 5 plugin entries.

### `cmd/wagyu/tui_v135_test.go::TestV135_PickerCatalogMatchesRef`

- Now expects `len(FilterSlashCommands("/")) == 94` (was 99).

### `cmd/wagyu/picker_view_test.go::TestPickerView_MoreTailTracksWindow`

- Now expects the tail `↓ 89 more` at offset 0 (was `↓ 94 more`).

## What does NOT change in cycle 147

- The catalog (`DefaultSlashCommands`) still has 99 entries.
  /help COMMANDS tab still shows all of them with the footer
  `[1-18 of 99 items]`.
- The cycle 146 banner model row + email tier fix is preserved
  — those are independent of the picker/help filter and remain
  correct.
- The picker's substring match (cycle 135's other half) is
  unchanged: `containsFold` still applies for typed prefixes.

## Hard constraints / 存疑 / limitations

- 【推测】Why picker filters but /help doesn't. The picker is
  for "user-facing" commands; the 5 chrome-devtools-plugin:*
  entries are namespaced plugin commands aimed at the model
  (advertised via the system prompt), not the user. /help is
  a documentation surface and shows everything so the user can
  see what plugins exist. Cycle 147 mirrors this divergence.
- 【推测】Why cycle 146 was wrong (twice). I first read
  "↓ 94 more" as "94 entries visible at offset 0" (off by the
  5 visible row count), then read `[1-18 of 99 items]` as
  "[1-18 of 94 items]". Both misreads pushed me toward
  un-filtering the picker. Cycle 147 corrects both by carefully
  matching offset arithmetic and reading the footer strings
  verbatim.
