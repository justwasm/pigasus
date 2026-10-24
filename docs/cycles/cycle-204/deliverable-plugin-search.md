# Cycle 204 — `/plugin` Discover: the search box, the details block, the footer

## TL;DR

Cycle 198 left three of the Discover tab's behaviours inert: cycle 193's picture
advertised a search box and an `enter Expand Details` legend on the Discover
tab, but `space` did nothing, `enter` did nothing, and the box was a static
`Type to search plugins...` placeholder. This cycle drove the live reference
(Antigravity CLI 1.4.0, 220×50 and 100×50, tmux) key by key and implemented what
it actually does:

- **the search box** — `space` (or *any* printable key) focuses it on the
  Discover tab, the placeholder is replaced by `  Search: <query>` plus a
  reverse-video block cursor, and the list is **filtered live**;
- **the filter rule** — a case-insensitive substring over
  `{marketplace name} + " " + {plugin name} + " " + {plugin description}`; the
  marketplace's *description* is **not** searched;
- **Discover `enter`** — opens a detail block that differs from the Installed
  one: `Description` (word-wrapped) / `Components` / `Marketplace` / `Version`,
  with **no** `Path:` and **no** `Skills:`;
- **the contextual Discover footer** — `enter / ctrl+s Enter Local Path` on the
  `+ Install from local directory...` row, otherwise `enter Expand Details
  ctrl+s Install`, and `esc Exit Search` while the box has focus.

A live REF-vs-clone byte comparison of the panel block (from the last ` Plugins`
title down to the `Keyboard:` legend) is **0 mismatches in all six states**:
Discover unfiltered, Discover with a query typed, Discover with no match, and
Discover with a detail block expanded (both from the unfocused list and from
inside the search box).

## What was wrong

| Symptom (cycle 193/198) | Reality (measured this cycle) |
|---|---|
| `Type to search plugins...` is a static placeholder | on the Discover tab `space`/any printable key focuses the box; the reference then draws `  Search: ` + query + a **blinking** block cursor and filters the list |
| the Discover list never filters | it filters live, case-insensitively, over the marketplace *name* and the plugin's name and description |
| Discover `enter` inert | it expands a detail block whose fields are **not** the Installed tab's |
| the footer is one fixed line | it is contextual: the local-install action appears only while that row is selected, and `esc` reads `Exit Search` while the box has focus |
| `←/→` advertised as `Page` | on the unfocused Discover tab they park the cursor on the first / last row |

## What changed

| File | Δ | What |
|---|---|---|
| `cmd/wagyu/slash_plugin_search.go` | **new, 343 lines** | search state (`enterPluginSearch` / `exitPluginSearch`), the box line (`pluginSearchBoxLine`), the filter (`pluginDiscoverFilter`), the Discover row list + cursor bound (`pluginDiscoverRows`, `pluginDiscoverCount`), the `enter` toggle for Discover rows (`togglePluginDiscoverExpanded`), the key handler (`handlePluginSearchKey`), the contextual footer (`pluginDiscoverLegend`), the detail block (`pluginDiscoverDetails`, `pluginWrappedDetail`) and its two helpers (`wrapWords`, `pluginComponents`) |
| `cmd/wagyu/slash_cycle204_test.go` | **new, 565 lines** | 13 tests (below) |
| `cmd/wagyu/slash_panels.go` | +91 / −17 | `slashPanel` gains `pluginSearch bool` + `pluginQuery string`; the box line is routed through `pluginSearchBoxLine()`; the Discover branch of `panelPluginLines` delegates to the new `panelDiscoverLines(mkts, max)` (filtered rows, hidden local row while focused, details under the expanded row, the no-match message + its extra blank line, contextual legend); `pluginRowCount()` → `pluginDiscoverCount()`; `panelPlugin`'s key branch gains the search-mode guard, `←/→`, `space → enterPluginSearch`, the printable-key focus+seed path, and Discover `enter → togglePluginDiscoverExpanded` |
| `cmd/wagyu/plugin_data.go` | +26 / −7 | `pluginRow` gains `version` (the Discover `Version:` value; Installed rows leave it empty and read `plugin.json`), `pluginMarketplaces` parses the per-plugin `version` from `marketplace.json`, and `marketplaceVersion(name, mkts)` is added |

No new dependency; `internal/convstore/sqlite.go` untouched. Total: 2 new files
(908 lines) + 117/−24 across 2 existing files.

## Evidence

Everything below is a raw terminal capture (`tmux capture-pane -p -e`) from the
live reference at 220×50; the probe scripts are in the cycle's probe dir
(`p1.py`–`p19.py`, `sum.py`, `cmp204.py`).

**The box, byte for byte.** The reference emits the grey label, a reset, one
plain space, the query, then the reverse-video cursor:

```
empty, focused  \x1b[38;5;238m  Search: \x1b[39m \x1b[7m\x1b[37m \x1b[0m
query "gc"      \x1b[38;5;238m  Search: \x1b[39m gc\x1b[7m\x1b[37m \x1b[0m
unfocused       \x1b[38;5;238m  Type to search plugins...\x1b[39m
```

Both lines are reproduced byte-for-byte by `pluginSearchBoxLine()`
(`TestCycle204_SearchBoxBytes`).

**The filter is name + description only, not the marketplace description.** The
marketplace here is `antigravity-plugins-official`, whose `marketplace.json`
description contains the word "catalog":

- `official` / `gravity` / `plugins` / `antigravity` → all **17** rows (they
  appear only in the marketplace *directory name*);
- `catalog` → **0** rows (it appears only in the marketplace *description*, so
  the description is not in the searched text);
- `gc` → **3** rows: `data-agent-kit-plugin` (…"GCP's data ecosystem"),
  `gcalendar` (name), `google-cloud-developer` (…"gcloud CLI commands") — which
  pins the rule to a plain substring over the concatenation above.

**The detail block** (Discover `enter`, captured under `data-agent-kit-plugin`
and `gcalendar`):

```
     \x1b[38;5;253m Description: This plugin provides a specialized suite of skills for data engineers and database practitioners working on Google Cloud. It acts as an expert assistant, allowing you to use natural language prompts\x1b[39m
                  \x1b[38;5;253m in your preferred coding agent to architect complex data pipelines, transform data with dbt, write Spark and BigQuery SQL notebooks, and orchestrate end-to-end workflows across GCP's data ecosystem.\x1b[39m
     \x1b[38;5;253m Components:  loading...\x1b[39m
     \x1b[38;5;253m Marketplace: antigravity-plugins-official\x1b[39m
     \x1b[38;5;253m Version:     1.1.0\x1b[39m
```

The value column is 19 (`5-space indent + 13-wide label + 1`), so the Description
wraps at `max−19` where `max = terminal_width − 2`: at 220 columns that is
**199**, and at 100 columns it is **79** (captured; width 78 does *not* reproduce
the reference's line breaks — `TestCycle204_DetailWrap`). Continuation lines are
18 spaces + grey + one space (also column 19). `Components:` reads the sorted
directory names under
`<antigravity home>/cache/marketplaces/<mkt>/plugins/<name>/<version>/`, and falls
back to the literal `loading...` until the plugin is unpacked — measured:
`gcalendar` → `skills`, `data-agent-kit-plugin` → `loading...`
(`TestCycle204_ComponentsSortedAndJoined`, `TestCycle204_DiscoverDetails`).

**The no-match screen** (query `zzq`) is box / message / blank / blank / legend —
one more blank line than a screen with rows:

```
  \x1b[38;5;238m  Search:  zzq\x1b[39m\x1b[7m\x1b[37m \x1b[0m
  \x1b[38;5;238m  No marketplace plugins match your search.\x1b[39m

Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  ctrl+s Install  esc Exit Search
```

**The blink.** While idle the reference alternates the cursor block with
*nothing* (`\x1b[7m\x1b[37m \x1b[0m` on, nothing off) at roughly 2 Hz — ten
captures 0.4 s apart show both phases. The clone renders the block steadily,
which is one of the two phases the reference itself emits. The parity harness
therefore retries a capture until the reference is in the cursor-on phase and
normalises the block away on both sides (below); with the block present the raw
box lines are **identical**.

## Live parity

Harness `agy204/cmp204.py`: both binaries in tmux (`-L agy204`, 220×50), open
`/plugin` + `enter`, send the key sequence, slice the pane from the **last**
` Plugins` title to the `Keyboard:` legend, and compare line by line after
removing the blink cursor block from both sides. Reference
`/Users/gear/.local/bin/agy` with `OPENAGY_BACKEND=agw` (≈24 s sign-in) vs clone
`agy204/our/agy` with `OPENAGY_BACKEND=mock` (≈8 s).

| Case | keys sent | rows compared | mismatches |
|---|---|---|---|
| `discover-unfiltered` | `tab` | 23 | **0** |
| `discover-query-g` | `tab`, `space`, `g` | 22 | **0** |
| `discover-query-gc` | `tab`, `space`, `gc` | 8 | **0** |
| `discover-nomatch` | `tab`, `space`, `zzq` | 7 | **0** |
| `discover-details` | `tab`, `↓`, `enter` | 28 | **0** |
| `discover-details-focused` | `tab`, `space`, `↓`, `enter` | 26 | **0** |
| **total** | | **114** | **0** |

Raw box-line cross-check (cursor-on phase, no normalisation):

```
REF empty-focused box= '\x1b[38;5;238m  Search: \x1b[39m \x1b[7m\x1b[37m \x1b[0m'
OUR empty-focused box= '\x1b[38;5;238m  Search: \x1b[39m \x1b[7m\x1b[37m \x1b[0m'
REF gc box= '\x1b[38;5;238m  Search: \x1b[39m gc\x1b[7m\x1b[37m \x1b[0m'
OUR gc box= '\x1b[38;5;238m  Search: \x1b[39m gc\x1b[7m\x1b[37m \x1b[0m'
```

Two notes on getting to zero:

- the first harness run reported **1 mismatch** on `discover-details-focused`
  (empty query). It was purely the blink: the reference was captured in the
  *off* phase, where it paints nothing after `  Search:`; the clone — always on —
  left the space that precedes the cursor block once the block was stripped.
  Re-capturing in the on phase (the harness now does this) gives the byte-identical
  raw line above.
- the same first run also lost two reference captures (`discover-query-g`,
  `discover-query-gc`) to an incomplete sign-in at 24 s; both re-ran clean.

## Tests

`cmd/wagyu/slash_cycle204_test.go` — 13 tests, all passing:

| Test | Pins |
|---|---|
| `TestCycle204_SearchBoxBytes` | the box line's raw bytes, placeholder and `  Search:  gc` |
| `TestCycle204_FilterRule` | the filter rule, incl. marketplace-name positives and the marketplace-description negative (`catalog` → 0) |
| `TestCycle204_WrapWords` | the greedy word-wrap primitive (a word longer than the width is emitted whole) |
| `TestCycle204_DetailWrap` | the `max−19` width rule and the w100 6-line Description wrap |
| `TestCycle204_DiscoverDetails` | the 5-line Discover detail block and its field set/order |
| `TestCycle204_ComponentsSortedAndJoined` | `Components:` from the cache dir, sorted, `", "`-joined, `loading...` fallback |
| `TestCycle204_NoMatchScreen` | the no-match screen's extra blank line |
| `TestCycle204_PrintableKeyFocusesAndFilters` | a printable key focuses + seeds + parks the cursor on the first result |
| `TestCycle204_SearchKeyLifecycle` | `esc` / `ctrl+u` / `backspace`-to-empty exit; `backspace` edits |
| `TestCycle204_TabFromSearchSwitchesToInstalled` | the deliberate Tab simplification (below) |
| `TestCycle204_LegendVariants` | the four footer variants |
| `TestCycle204_FirstLastJumps` | `←/→` first/last on Discover, inert on Installed |
| `TestCycle204_EnterExpandsUnderFilteredRow` | `enter` toggles under the filtered row; `esc` keeps the expansion |

Gates: `go build -o agy204/our/agy ./cmd/wagyu` OK; `gofmt -l
cmd/wagyu/slash_plugin_search.go cmd/wagyu/slash_cycle204_test.go` empty;
`go test -vet=off -run TestCycle204 ./cmd/wagyu/` PASS. The full
`go test -vet=off ./cmd/... ./internal/...` fails only
`TestCycle197_ConfigVisibleRows` and `TestPanelConfigLayout`, both of which fail
**identically on base `4f9945e`** (verified in a throwaway worktree at that
commit): this machine's `settings.json` has `editorMode: vim`, which adds a
21st config row — unrelated to `/plugin`.

## 存疑 / not implemented

Anything below was either unmeasurable or deliberately out of this cycle's
scope; such behaviour is left **inert**.

- **`tab` while the search box is focused.** The reference keeps the box focused
  and switches to the Installed tab, where the box and footer differ (measured:
  `  Search: <q>  (enter to search)` / `(type to continue searching)`, legend
  `enter Search  esc Exit Search`, no-match row `  No installed plugins match
  your search.`). The clone instead exits search mode and shows the plain
  Installed tab. This is a **deliberate simplification**, pinned by
  `TestCycle204_TabFromSearchSwitchesToInstalled`; the Installed-tab search is a
  separate task.
- **`←/→` while the box is focused.** The clone jumps to the first / last
  filtered row (what the keys do unfocused). Whether the reference *pages* instead
  while focused was not measured.
- **The blink.** The clone paints the cursor steadily; the reference blinks it off
  on a timer. A byte-diff of a single capture can therefore land on the reference's
  off phase. Recorded, not emulated.
- **The `+ Install from local directory...` path prompt.** `enter` / `ctrl+s` on
  that row opens an inline path prompt in the reference; the clone leaves it
  inert (the row is still selectable and keeps its own footer line).
- **`ctrl+s Install` on a Discover row.** Inert in the clone (it would install
  from the marketplace — a network operation).
- **`Components:` freshness.** The value is read from the local cache; the
  reference fills it in when it downloads/unpacks, so for an uncached plugin the
  clone can only ever say `loading...`. Only `gcalendar` is cached on this machine.
- **The w220==199 wrap width** was confirmed against the live reference; the
  earlier proposal of "220 → 199" for a *different* terminal width is only
  asserted through the measured `max−19` rule, not a saved 220-column capture of
  every plugin.
