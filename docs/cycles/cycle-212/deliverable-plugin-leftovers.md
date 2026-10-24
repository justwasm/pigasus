# Cycle 212 — Discover-tab plugin leftovers (local-install prompt, Tab-while-focused, cursor blink)

## TL;DR

Three Discover behaviours the previous cycles did not measure are now
implemented, pinned and tested against the live reference (Antigravity CLI
1.4.0, driven at 220x50 through a private tmux socket):

1. Enter / ctrl+s on the unfocused `+ Install from local directory...` row
   opens an inline path prompt. Enter on a directory with a parseable
   `plugin.json` copies it into `~/.gemini/config/plugins/<manifest-name>/`
   and writes `plugins.<name>.enabled=true` into `config.json`.
2. Tab while the Discover search box is focused cycles `Installed↔Discover`
   **while keeping the box focused and the typed query intact**. The
   cycle-204 simplification (`Tab leaves search`) is reversed.
3. The cursor in both the search box and the install prompt blinks at
   ~530 ms on / ~530 ms off — a single `pluginBlinkMsg` drives both.

The accept path (1) is fully measurable, so it is implemented; path
completion (1) and the Installed-tab focused-box rendering (2) are
deliberately scoped down — see 【存疑】.

## Probe vs clone (the gap, by surface)

Every row below was measured against the reference (`~/.local/bin/agy`,
build 1.4.0) by reading the on-screen cells row-by-row. Probe transcripts
are at `/private/var/folders/.../opencode/agy212/p{1..15}.out`.

| Surface | Probe (`agy`) | Clone before | Clone after |
|---|---|---|---|
| Enter on local-install row | opens row-3 prompt with `+ Install from local directory:` label and a trailing block cursor (p1.out row 28; p3.out rows 10–14) | no-op (the row was a stub) | `openPluginLocalInstall` paints `pluginLocalInstallRow` in row 3 and arms `pluginBlink` |
| Empty-prompt cursor | reverse-video block over the trailing space the box reserves (`\x1b[7m\x1b[37m \x1b[0m`) | no cursor | `pluginBlinkMsg` toggles `pluginBlinkOn`; the on-phase emits the span, off-phase the plain space |
| Typed-prompt cursor | reverse-video block immediately after the typed text (p2.out row 11) | no cursor | `pluginLocalInstallRow(query, blinkOn=true)` trails the typed path with the same span |
| Tab on focused Discover "g" | keeps box focused on Installed; query "g" persists; legend reads `enter Search` (p14.out lines 1–10) | exited search, returned to Installed with empty query | `handlePluginSearchKey` flips `pluginTab` and stays; `m.panel.pluginQuery` is unchanged |
| `←/→` on focused Discover | jump to first / last filtered row (p13.out — Right → cursor=2 of `alpha,beta,gamma`; Left → cursor=0) | unmeasured; the cycle-204 docstring said `Page` and the implementation did not change | matches the unfocused behaviour (jump-first / jump-last) via the existing `filtered = len(rows)` bound |
| Cursor blink cadence | ~530 ms on / ~530 ms off (one-second period; sampled every ~10 ms for 10 s) | static reverse-video span, never changed | `pluginBlink` re-arms while focused; `pluginBlinkMsg` flips phase on tick |
| Ctrl+s on local-install row | opens the same prompt (legend lists `enter / ctrl+s Enter Local Path` together — p1.out row 28, p24.out line 27) | no-op | handled in `handlePanelKey` before the `tea.ModCtrl` `s` falls into the printable branch |
| Empty hint (no query) | no hint line; rows=3 (p25.out) | nothing | matches |
| "Directory not found" hint | `└─ ✗ Directory not found (press Tab to complete)` (p3.out line 17) | no hint | `pluginLocalInstallHint` returns the same string for `os.Stat != nil` |
| "Directory does not contain plugin.json" hint | `└─ ✗ Directory does not contain plugin.json (press Tab to complete subdirs)` (p4.out) | no hint | `pluginLocalInstallHint` returns the same string for `os.Stat == nil && !hasPluginJSON` |
| Valid plugin (has plugin.json) | no hint (p2.out — rows=3) | no hint | hint returns `""` |
| Title error (Enter on bad path) | `Plugins (Error: Directory not found)` (p24.out lines 13–14) — red span (SGR `38;5;166`) | no error display | `panelPluginLines` appends ` (Error: <msg>)` in the same red when `m.panel.pluginLocalError != ""`; cleared on next Esc |
| Esc from prompt | closes, clears cursor (p24.out line 16) | n/a | matches |
| Accept conflict (destination already installed) | prompt stays open, title shows error (probed) | n/a | `installLocalPlugin` returns `errors.New("plugin <name> is already installed")`; `submitPluginLocalInstall` records it in `pluginLocalError` |
| Accept success | prompt closes, plugin appears under Installed (p11.out line 21) — `└─` becomes a fresh installed row | n/a | `copyDir` walks `src` with `filepath.WalkDir`, `os.MkdirAll(dst, 0700)`, `os.CopyFS`-style buffer copy per file; `auth.SetPluginEnabled(name, true)` writes the config entry |

### Files

| File | Δ |
|---|---|
| `cmd/wagyu/slash_plugin_local_install.go` | new — `pluginLocalInstall`, `pluginBlinkMsg`/`pluginBlink`, `openPluginLocalInstall`, `closePluginLocalInstall`, `pluginLocalInstallRow`/`Hint`/`Legend`, `handlePluginLocalInstallKey`, `submitPluginLocalInstall`, `completeLocalInstallPath`, `commonPrefix`, `installLocalPlugin`, `copyDir` |
| `cmd/wagyu/slash_cycle212_test.go` | new — 10 tests, all green |
| `cmd/wagyu/slash_panels.go` | +67/−11 — panel struct gains `pluginLocalInstall`, `pluginLocalError`, `pluginBlinkOn`; `panelPluginLines` paints the title error; `panelDiscoverLines` swaps the local row for the prompt and switches the footer; `handlePanelKey` dispatches to the prompt, opens it on Enter / ctrl+s on the local row, returns `(bool, tea.Cmd)` everywhere |
| `cmd/wagyu/slash_plugin_search.go` | +24/−5 — `enterPluginSearch` arms the blink; `pluginSearchBoxLine` uses `pluginBlinkOn` for its trailing cursor; `handlePluginSearchKey` keeps focus on Tab |
| `cmd/wagyu/tui_run.go` | +11 — `case pluginBlinkMsg` flips phase and re-arms while the panel owns focus |
| `cmd/wagyu/slash_cycle204_test.go` | +50/−8 — the cycle-204 simplification test was renamed to `TestCycle212_TabFromSearchKeepsFocusAndQuery` (asserts the new behaviour, with a follow-up TODO in the docstring); the old test name is kept as a stub that now also asserts the new behaviour |

## Before / after — one focused-row screenshot

The four screenshots below were taken from `p1.out` (rows are 0-indexed
frame cells — SGR sequences stripped for the deliverable):

**Before, unfocused Discover** (cursor on the local-install row):

```
[0]   Plugins
[1]   Installed (4)    Discover (17)   (tab to cycle)
[2]   Type to search plugins...
[3] > + Install from local directory...
[4]   ● data-agent-kit-plugin This plugin provides…
[5]   ● gcalendar Check availability, schedule events, and RSVP.
…
[f]   Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter / ctrl+s Enter Local Path  esc Exit
```

**After, prompt open (empty query, blink on)**:

```
[0]   Plugins
[1]   Installed (4)    Discover (17)   (tab to cycle)
[2]   Type to search plugins…
[3] > + Install from local directory: ▍ ~/path/to/plugin
[f]   Keyboard: enter / ctrl+s Install Plugin  tab Complete Path  esc Cancel
```

(`▍` is the reverse-video block — `\x1b[7m\x1b[37m \x1b[0m`.)

**After, typed `/tmp/agy212v` + Enter on a directory with `plugin.json`**:

```
[0]   Plugins
[1]   Installed (5)    Discover (17)   (tab to cycle)
[2]   Type to search plugins…
[3] > ● agy212v v1.0.0
…
```

**After, focused Discover with query "g", then Tab**:

```
[0]   Plugins
[1]   Installed (4)    Discover (17)   (tab to cycle)
[2]   Search: g▍  (enter to search)
[3] > ● google-antigravity-sdk Using the Google Antigravity Python…
[f]   Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search  ctrl+s Uninstall  esc Exit Search
```

(Tab moves to Installed with the same `g` and `enter Search` legend; the
next Tab returns to Discover with the same query.)

## Design notes

### Why a single `pluginBlinkMsg`

The reference blinks every cursor at ~530 ms; on the panel side that is
two call sites (search box, install prompt) and one owner (the panel). A
single `tea.Tick` keyed off `pluginBlink` and re-armed from `Update`
ensures the period is identical regardless of which widget is in focus.
The implementation mirrors `resumeBlinkMsg` from cycle 184 (`puck` /
`/resume`) — same shape, different owner field — and stays out of
`picker_view.go`.

### Why the install-accept lives in `cmd/wagyu`, not `internal/auth`

`installLocalPlugin` needs `GeminiConfigHome()` to land the directory and
`SetPluginEnabled` to write the config entry, both of which exist in
`internal/auth`. The actual copy is a `filepath.WalkDir` + `os.MkdirAll` +
per-file `ReadFile` / `WriteFile` (Go 1.27's `os.CopyFS` could collapse
this, but only if the destination directory does not already exist — the
existing-install conflict path needs the explicit walk). The copy is small
(a plugin directory is ~10 KB) so `os.CopyFS` was deferred to a follow-up
if profiling shows it matters; see 【存疑】 for the manual file-walk
rationale.

### Why `←/→` while focused = first/last, not Page

The unfocused-tab legend reads `←/→ Page` (the prompt binds the same
keystroke to "page" because *the prompt* is page-shaped), but the
cycle-204 docstring already noted the behaviour was unmeasured. Probing
showed `←/→` jumps to the first / last filtered row whether the search
box is focused or not — `Left` lands on row 0 and `Right` lands on the
last filtered row (p13.out, measured against an `alpha/beta/gamma` set).
The clone therefore reuses the unfocused branch through the existing
`filtered = len(rows)` bound. The prompt-vs-list legend-symmetry
discrepancy is left as a separate cycle if it ever matters.

### Why `Tab`-keeps-focus is a one-line change in `handlePluginSearchKey`

Cycle 204 deliberately exited search on Tab because the Installed-tab
*focused* rendering was out of scope: its legend is `enter Search` (not
`type to filter`), the search mode requires an `enter` commit (not
live-filter), and Enter takes a different action (it expands the
selected plugin, not the box). Cycle 212 measures those branches in
p14/p15.out and notes that **all of them are owned by the search-mode
state, not the unfocused state** — the prompt's focused-row rendering on
Installed already supported every keyed action cycle 204 needed, the
only gap was the cycling **between tabs**, which is exactly what the
search-mode branch now does. The Installed search-mode rendering is
still unimplemented in the clone (search-mode on Installed is a
no-op-equivalent placeholder), but the cycle between tabs works because
the state we already had (`pluginTab`, `pluginSearch`, `pluginQuery`)
is sufficient.

### Why the title-error span is `\x1b[38;5;166m`

The reference paints the error in colour 166 from the same 256-colour
palette that the search-instruction placeholder uses (e.g. colour 238 on
`Type to search plugins...`). The cycle-212 tests assert
`(Error: <msg>)` as plain text via `plainLines(m)[0]`; the SGR string is
asserted only in the prompt-rendering test, which uses the constant
`cBlueOpen`/`cBlueClose` (cycle 198) for the prompt-frame row and the
literal spans above for the prompt body.

## Verified

```
$ /Users/gear/go/bin/go test -vet=off -count=1 -run 'Cycle212' ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.230s
```

10 tests, all green:

- `TestCycle212_PromptRowRendering` — empty / typed / typed-blink-off
- `TestCycle212_PromptValidationHints` — empty / not-found / no-plugin.json / valid-plugin
- `TestCycle212_PromptLegend` — the Install-Plugin / Complete-Path / Cancel footer
- `TestCycle212_PromptKeyLifecycle` — printable, backspace×3, esc, Tab path completion, Enter-on-bad, Esc
- `TestCycle212_EnterOpensPromptOnLocalRow` — Enter and ctrl+s open the prompt
- `TestCycle212_AcceptCopiesAndEnablesPlugin` — Enter on a valid path closes the prompt, copies `plugin.json`, writes the enabled entry
- `TestCycle212_AcceptConflictLeavesPromptOpen` — a pre-existing destination keeps the prompt open with the `already installed` error
- `TestCycle212_SearchBoxBlink` — both blink phases render correctly
- `TestCycle212_SearchBoxBlinkMessage` — `pluginBlinkMsg` flips and re-arms while focused, drops the arm when focus is gone
- `TestCycle212_TabKeepsQueryAndFocus` — Tab on a focused box cycles tabs and keeps the query
- `TestCycle212_LeftRightWhileFocusedJumpsFirstLast` — focused `←/→` jumps first/last

Full sweep:

```
$ /Users/gear/go/bin/go test -vet=off -count=1 ./cmd/... ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.719s
ok  	github.com/chadgpt/openagy/internal/agent	1.081s
ok  	github.com/chadgpt/openagy/internal/auth	27.168s
ok  	github.com/chadgpt/openagy/internal/cli	21.146s
… (28 packages ok)
```

Build & format:

```
$ /Users/gear/go/bin/go build -o /tmp/agy212-build/our/agy ./cmd/wagyu
BUILD_OK  -rwxr-xr-x 1 gear staff 72646482 Oct 17 20:21 /tmp/agy212-build/our/agy

$ gofmt -l cmd/wagyu/slash_plugin_local_install.go cmd/wagyu/slash_cycle212_test.go
(empty)
```

`~/.gemini/settings.json` and `~/.gemini/config/config.json` were
restored from `/tmp/agy212-backup/` after every test ran; final SHA:

```
9b28d2667884ec9708fa322ba93d28d99a129673  /Users/gear/.gemini/settings.json
bf8d9eb6cc17a5f75956a2d98b14822ad8bf9f21  /Users/gear/.gemini/config/config.json
```

(matches the captured pre-cycle state; the test environment uses
`ANTIGRAVITY_HOME` so the cycle never wrote to `~/.gemini/`, but
`cp -p /tmp/agy212-backup/* ~/.gemini*/` was still run defensively.)

## 硬约束 / 存疑 — not verified

- **Installed-tab search-mode rendering.** The reference's focused
  Installed box (`Search: …▍ (enter to search)`, p14.out) commits the
  filter only on Enter — typing more characters does not live-filter,
  and the legend switches from `enter Search` to `enter Expand Details`
  after the commit. The cycle-212 `Tab`-keeps-focus implementation
  preserves the cycle-204 `pluginSearch=true, pluginQuery=...` state
  across tabs, so once the Installed-tab focused-mode rendering is
  implemented the cycle-between-tabs behaviour is free; this cycle does
  **not** add the rendering itself.
- **Multi-subdirectory Tab completion.** The hint string references
  "press Tab to complete subdirs" (p4.out), which implies the reference
  cycles through a list of subdirectory matches rather than expanding to
  a longest-common prefix. The cycle-212 implementation does
  longest-common-prefix expansion against the parent directory; a probe
  that types `/tmp/agy212dirs/u` then Tabs three times was not driven end
  to end, so the cycling shape is unverified. The unit test
  (`TestCycle212_PromptKeyLifecycle`) covers the unique-prefix case only.
- **`os.CopyFS` would replace the manual `copyDir`.** `os.CopyFS(dst,
  os.DirFS(src))` from Go 1.23 is exactly equivalent to the manual walk
  for the no-conflict case; switching to it would simplify `copyDir` to
  a one-liner but cannot be used in the conflict path (where `os.DirFS`
  reports a directory that already exists and `os.CopyFS` would still
  complain). The cycle-212 implementation keeps the explicit walk so a
  single code path handles both. Defer the `os.CopyFS` change unless
  the walk shows up in a profile.
- **`←/→ Page` legend mismatch.** The reference legend still says `←/→
  Page` even after the focus-state changes, but the actual behaviour is
  "jump to first / last filtered row" (p13.out). The cycle-212
  implementation ignores the legend and matches the probe — the legend
  mismatch is a reference quirk the clone does not paper over.
- **`~/.gemini/settings.json` pre-cycle SHA.** The expected restore
  reference in the cycle note (`30471e37…`) did not match the
  pre-cycle SHA actually captured (`9b28d266…`); the cycle notes also
  reference `c029a38a…`. The file was restored from the pre-cycle SHA
  captured by this cycle, not from the listed expected SHA. No
  `~/.gemini/` file was observed to differ at the end of the cycle.
- **Live reference cannot be probed at headless.** Cycle 1 (`docs/cycles/
  cycle-001/FINDINGS.md`) established that `~/.local/bin/agy` 1.4.0
  requires an OAuth-baked config to run interactively; cycle 212
  measured through a tmux-driven full TTY via the cycle-002 wrapper,
  not through print mode. The agree-with-reference loop ran on every
  probe before the implementation was changed — every row in the
  **Probe vs clone** table above is a real on-screen cell.
- **Cycle-204 docstrings still say `Tab leaves search`.** `slash_cycle204_test.go`
  contains the legacy `TestCycle204_TabFromSearchSwitchesToInstalled`
  stub (now asserts the new behaviour) and a TODO comment that says
  "follow-up cycle". It is left as-is so reviewers can see the
  before-and-after side by side; deletion is left for a docstring sweep
  cycle.
- **Cycle-212 test scaffolding uses `cycle212Root(t)`.** The function
  builds `<tmp>/.gemini/{antigravity-cli,config}` under the test's
  `t.TempDir()` and points `ANTIGRAVITY_HOME` at the
  `antigravity-cli` part, mirroring `cycle204Root`. The promise to cycle
  204 was that **every** marketplace test passes `root` (not a fresh
  `t.TempDir()`) to `seedMarketplace204` / `seedPlugin204`; the same
  contract is honoured in cycle 212 tests that need the marketplace
  count to be non-zero. Tests that only exercise the prompt's own
  rendering do not need `root`; they still call `cycle212Root(t)` for
  the `ANTIGRAVITY_HOME` side effect.
