# Cycle 198 — `/plugin`, both tabs

## TL;DR

Cycle 193 shipped `/plugin` as a *picture*: four hardcoded rows, all badged
`[enabled]`, a hardcoded `Discover (17)` label, and a legend advertising six
keys that did nothing. This cycle drove the reference key-by-key and rebuilt
the panel around what it actually reads and writes:

- the `[enabled]`/`[disabled]` badge comes from
  `~/.gemini/config/config.json` → `plugins.<name>.enabled`, and **space flips
  it on disk** (it survives a restart);
- **enter** appends the five-line detail block (Description / Skills /
  Marketplace / Path / Version) under the row;
- **tab** swaps to the Discover tab — `+ Install from local directory...` plus
  every `cache/marketplaces/<mkt>/marketplace.json` entry, with a green `●` when
  the plugin is installed and grey 253 otherwise, and its own legend.

A live REF-vs-clone byte comparison of the panel block (title → legend) is now
**0 mismatches** on all three states: Installed, Discover, and
Installed-with-details-expanded.

## What was wrong

| Symptom | Reality |
|---|---|
| every row badged `[enabled]` | the badge is read from `config.json`; a plugin can be installed and disabled |
| `Discover (17)` hardcoded | the count is the marketplace cache's entry count (0 when the cache is empty) |
| Installed descriptions from a static table | the reference prefers the *marketplace* entry's description, then `plugin.json`; for `modern-web-guidance-plugin` the two disagree and the static table matched neither |
| `tab`/`space`/`enter`/`ctrl+s`/`←/→` inert | `tab`, `space` and `enter` all do real work; the legend was decorative |
| Discover legend read `enter / ctrl+s Enter Local Path  esc Exit` | it also carries `←/→ Page` and `tab Cycle Tabs` |
| non-cursor plugin names rendered bold **blue** | they are bold *default-fg*; only the cursor row is blue |
| title rendered as `\x1b[39m \x1b[1mPlugins\x1b[0m` (one leading space) | `\x1b[1m\x1b[39m  Plugins\x1b[0m` — two leading spaces, bold from the start |
| the tab row had no leading indent | it starts with two plain spaces |
| `[enabled]`'s trailing space sat outside the green span | it is inside (`\x1b[38;5;29m[enabled] \x1b[39m`), which is what keeps both badges ten columns wide |

## What changed

### `internal/auth/compat.go`

- `GeminiConfigHome()` → `~/.gemini/config`, derived as
  `filepath.Dir(AntigravityConfigHome())/config` so `$ANTIGRAVITY_HOME`/`$HOME`
  move both trees together.
- `PluginEnabled(name) (bool, error)` — reads the flag; a missing file or a
  missing entry reads as `(false, nil)`, because that is what the reference
  shows for a plugin directory it has no row for.
- `SetPluginEnabled(name, enabled) error` — rewrites `config.json` preserving
  `userSettings` and every other plugin row.

### `cmd/wagyu/plugin_data.go` (new)

The two on-disk sources, each with a documented fallback:

- `installedPluginRows()` walks `config/plugins/*` (directory name = plugin
  name, description from `plugin.json`) and falls back to the four stock rows
  when the tree is absent — so a machine without plugins still renders a
  truthful panel instead of an empty list;
- `pluginSkills(name)` — sorted `skills/*` directory names;
- `pluginMarketplaces()` / `marketplaceItems()` / `marketplaceOf()` /
  `marketplaceDesc()` — the Discover source and the `Marketplace:` field;
- `pluginDescFor(name, mkts)` — marketplace first, then `plugin.json`, then the
  stock table. This is the fix for `modern-web-guidance-plugin`, whose shipped
  manifest says *"Curated collection of agent skills for modern web
  development."* while the panel shows *"Keep your coding agent up to date with
  the latest web best practices."*

### `cmd/wagyu/slash_panels.go`

`panelPluginLines` now renders both tabs; the shared pieces are
`pluginTabHeader` / `pluginTabLabel` / `pluginBadge` / `pluginInstalledLine` /
`pluginDiscoverLine` / `pluginLocalRow` / `pluginInstalledDetails` /
`pluginDetailLine` / `truncateTo`. `slashPanel` gained `pluginTab` and
`pluginExpanded map[string]bool`; `handlePanelKey` gained a `panelPlugin`
branch.

Two behaviours are worth calling out because they are not obvious:

- **Truncation is width-driven.** Every row is cut to `terminal_width − 2`
  columns with a mid-word `...` (measured at width 220 → 218 and width 100 →
  98). The whole line — badge, name, description — participates, so the
  available description width depends on the plugin name's length.
- **Expansion is keyed by plugin name, not by cursor row.** The reference
  leaves a block open when the cursor moves away, so `pluginExpanded` is a set
  and the block is emitted for every open plugin while its row is rendered.

## Evidence

| Probe | What it settled |
|---|---|
| `/tmp/agycmp/probe_ref_plugin.py` | first pass — Enter opens a detail block; Discover is a second tab |
| `/tmp/agycmp/probe_ref_plugin2.py` | frame-clean key walk: space flips `config.json`, enter toggles the block, tab swaps tabs |
| `/tmp/agycmp/probe_ref_plugin3.py` | Discover cursor styling, `space` = focus the search box, enter = expand, esc = leave search mode |
| `/tmp/agycmp/probe_ref_plugin4.py` (bullet probe) | the `●` is green for *installed*, even when the plugin is disabled |
| `/tmp/agycmp/cmp_plugin_live.py` | live REF vs live clone, three states |
| `/tmp/agycmp/smoke_cycle198.py` | clone TUI end to end → `config.json` on disk |

### The enabled state (`probe_ref_plugin2.py`)

```
----- as opened  (chrome-devtools enabled=True) -----
   '> [enabled]  chrome-devtools-plugin Reliable automation, …'
----- after Space  (chrome-devtools enabled=False) -----
   '> [disabled] chrome-devtools-plugin Reliable automation, …'
----- after Space  (chrome-devtools enabled=True) -----
   '> [enabled]  chrome-devtools-plugin …'
```

The flag lives in `~/.gemini/config/config.json`:

```json
{ "plugins": { "chrome-devtools-plugin": { "enabled": true }, … },
  "userSettings": { "themeMode": "THEME_MODE_INHERIT" } }
```

`settings.json` is *not* written by this panel — an earlier probe diffed it and
saw only a key reordering.

### The detail block (`probe_ref_plugin2.py`, Installed)

```
'     \x1b[38;5;253m Description: Reliable automation, …\x1b[39m'
'     \x1b[38;5;253m Skills:      a11y-debugging, chrome-devtools, debug-optimize-lcp, memory-leak-debugging, troubleshooting\x1b[39m'
'     \x1b[38;5;253m Marketplace: antigravity-plugins-official\x1b[39m'
'     \x1b[38;5;253m Path:        /Users/gear/.gemini/config/plugins/chrome-devtools-plugin\x1b[39m'
'     \x1b[38;5;253m Version:     0.21.0\x1b[39m'
```

Five plain spaces, then the whole line in grey 253, labels left-justified to 13
columns. `Skills:` is the sorted skill directory list (the reference sorts;
`os.ReadDir` does not).

### The Discover tab (`probe_ref_plugin3.py`)

```
'  \x1b[38;5;253mInstalled (4)\x1b[39m    \x1b[1;4m\x1b[38;5;69mDiscover\x1b[0;4m\x1b[38;5;69m \x1b[1m(17)\x1b[0m \x1b[38;5;253m  (tab to cycle)\x1b[39m'
'\x1b[38;5;238m  Type to search plugins...\x1b[39m'
'  \x1b[1m+ Install from local directory...\x1b[0m'
'\x1b[38;5;69m> \x1b[38;5;253m●\x1b[39m \x1b[1m\x1b[38;5;69mdata-agent-kit-plugin\x1b[0m \x1b[38;5;241mThis plugin provides …natural languag...\x1b[39m'
'  \x1b[38;5;253m●\x1b[39m \x1b[1mgcalendar\x1b[0m \x1b[38;5;241mCheck availability, schedule events, and RSVP.\x1b[39m'
```

The local row is bold with no colour; a plugin row is `●` + bold name + grey-241
description; the cursor row prefixes `\x1b[38;5;69m> ` and colours the name
blue. The bullet is green 29 for an installed plugin (probed separately with
that plugin disabled — it stays green, so the marker tracks *installed*, not
*enabled*).

### Live parity

```
cmp_plugin_live.py
  installed:  mismatches 0
  discover:   mismatches 0
  expanded:   mismatches 0
```

### Live end-to-end (`smoke_cycle198.py`)

```
chrome-devtools enabled @open : True
chrome-devtools enabled @space: False
chrome-devtools enabled @space: True
clone Discover tab: '  Installed (4)    Discover (17)   (tab to cycle)'
                    '> + Install from local directory...'
                    '  ● data-agent-kit-plugin This plugin provides …natural languag...'
                    'Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter / ctrl+s Enter Local Path  esc Exit'
```

## Tests

`cmd/wagyu/slash_cycle198_test.go` (new) pins the two tab-header byte strings,
the badge strings, the `config.json` accessor round-trip (including
`userSettings` preservation and creating a new entry), space toggling through
`handlePanelKey` with the rendered badge following, the five detail lines with
their exact padding, expansion surviving cursor movement and collapsing on a
second Enter, tab switching + cursor reset + `pluginRowCount`, the green/grey
bullet rule, the `truncateTo` table, row widths at width 100, the
marketplace-preferred description, and the no-plugin-tree fallback.

`TestPanelPluginLayout` now seeds an isolated plugin tree (four plugins, one
17-entry marketplace) instead of reading whatever the machine has — the panel
became environment-dependent this cycle, and the old test would have passed or
failed based on the developer's `~/.gemini/config`.

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu   # exit 0
go test -vet=off ./cmd/... ./internal/...     # exit 0
```

## Limitations / 存疑

- **The search box is not implemented.** On the Discover tab `space` moves focus
  into it (`  Search: ▏` — the placeholder is replaced and a block cursor
  appears) and typing filters; on the Installed tab `space` means "toggle
  enable". The clone implements the Installed meaning and makes Discover's
  `space` a no-op, which is deliberately *not* the reference behaviour but does
  not corrupt state. Filtering, and the `Escape`-leaves-search-mode transition,
  are a follow-up.
- **Discover `enter` is inert in the clone.** The reference expands a details
  block with different fields (`Components:  loading...` instead of `Skills:`)
  and a *word-wrapped* `Description:` whose continuation lines are indented 18
  columns. The wrap width was not derived, and the `Components:` value is a
  transient network-fetch state, so the block is left unimplemented rather than
  approximated.
- **`←/→ Page`, `ctrl+s` and `+ Install from local directory...` are inert.**
  All three are advertised by the legends; none was exercised. `←/→` presumably
  pages the list and the local row presumably opens a path prompt.
- **The multi-marketplace merge order is unverified.** Only one marketplace
  (`antigravity-plugins-official`) exists on the probe machine, so
  `marketplaceItems` flattens in directory order and nothing pins what the
  reference does with two.
- **An installed plugin with no `skills/` directory is unverified.** The clone
  emits `Skills:      ` with an empty value; the reference may omit the line.
- **`Version:` with no manifest** renders `-` (invented); the reference's
  fallback for a plugin directory without `plugin.json` was not probed.
- **`[disabled]`'s true colour is unknown.** The capture shows index 253;
  whether the reference emits `\x1b[38;2;…m` that rounds to 253 there is not
  recoverable from a tmux capture (the colour caveat at the top of
  `slash_panels.go`). The same applies to the `●` bullets.
- **The `Description`/`Skills`/… detail block is emitted for every open plugin,
  in row order** — consistent with the one measurement (a block left open while
  the cursor moved), but "several blocks open at once" was never observed.
- **`Esc` semantics on the Discover tab are unresolved.** The probe's `Esc`
  (pressed while the search box was focused) returned the placeholder text and
  reset the cursor to row 0 without closing the panel, while the legend says
  `esc Exit`. The clone keeps its existing `esc → close` for both tabs.

## Diff stat

```
 cmd/wagyu/plugin_data.go              | new
 cmd/wagyu/slash_cycle198_test.go      | new
 cmd/wagyu/slash_panels.go             | plugin panel + legend/key rewrite
 cmd/wagyu/slash_panels_test.go        | TestPanelPluginLayout isolation
 internal/auth/compat.go               | plugin state helpers
 docs/cycles/cycle-198/…               | this file
```
