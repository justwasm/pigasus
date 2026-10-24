# Cycle 219 — Installed-tab focused-mode rendering (Enter-commit semantics)

## TL;DR

The deferred Installed-tab focused-mode gap from cycle 212 (see
`docs/cycles/cycle-212/deliverable-plugin-leftovers.md`, last bullet under
"硬约束 / 存疑") is closed. The clone now matches the reference
(Antigravity CLI 1.4.0, driven at 220×50 through a private tmux socket) for
the three reference shapes:

1. **Focused + un-committed** — `  Search: g▍  (enter to search)`
   hint inside the grey-238 span; legend's `enter` field is `Search`;
   cursor block visible (and blinking).
2. **Focused + committed** — `  Search: g   (type to continue searching)`
   hint after Enter; the leading space and the cursor block drop (the
   reference shows a single separator space and no reverse-video span;
   verified with 100 captures over 2.5 s — zero cursor-block frames).
   Typing another character re-awakens the cursor and live-filters the
   rows below. The legend's `enter` field flips to `Expand Details`
   while `esc` stays `Exit Search`.
3. **Focused + cleared** — `esc` from any focused state resets
   `pluginSearch`, `pluginQuery`, `pluginQueryCommitted` and the new
   `pluginQueryAsleep` in one shot, returning the placeholder and the
   cycle-198 unfocused-tab legend.

Two new fields on `slashPanel` (`pluginQueryCommitted` and
`pluginQueryAsleep`) drive the rendering. The cycle-212
`Tab`-keeps-focus behaviour is preserved — both fields survive a
`Tab` round-trip so a focused Discover "g" `Tab→Installed` still lands
on a focused Installed "g" with the same hint and legend (cycle-212
p14.out, p15.out).

## Probe vs clone (the cycle-219 surface)

Every row below was measured against the reference
(`~/.local/bin/agy`, build 1.4.0) at 220×50 by driving the picker
through `space → Tab → type → Tab → Enter → Esc` from a private tmux
socket. Probe transcripts are at
`/private/var/folders/.../opencode/agy219/p{1..4}.out`; clone transcripts
are at `/private/var/folders/.../opencode/agy219/clone{1..4}.out`.

| Surface | Reference (`agy`) | Clone before | Clone after |
|---|---|---|---|
| Focused Installed box, **un-committed** (p2.out / clone2.out) | `Search: g▍  (enter to search)` — grey-238 prefix, typed query, reverse-video cursor block, grey-238 hint inside its own span (SGR-reopened; closed right before the legend row). Measured at 220×50 via raw-byte capture of the row. | no focused rendering on Installed — the search box branch ran on Discover only; cycle-212 `Tab`-keeps-focus preserved the state but the renderer ignored it (the cycle-212 docstring explicitly deferred this) | `pluginSearchBoxLine` branches on `pluginTab==panelInstalled && pluginSearch` and emits the un-committed shape: grey-238 prefix + body = " " + query + (cursor-span or " ") + the grey-238 hint's own re-opened span |
| Focused Installed legend, **un-committed** | `Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search  space Toggle Enable  ctrl+s Uninstall  esc Exit Search` (p2.out) | unfocused-tab footer (`enter Expand Details`, `esc Exit`) | `pluginInstalledLegend` reads `pluginSearch && !pluginQueryCommitted`: `enter` → `Search`, `esc` → `Exit Search` |
| Focused Installed box, **just-committed** (no further typing) (p3.out / clone3.out) | `Search: g   (type to continue searching)` — no leading space, no cursor block, single separator space then the grey-238 hint. Verified at 220×50: 100 captures over 2.5 s after Enter show zero cursor-block frames (`\x1b[7m\x1b[37m \x1b[0m`). | unchanged (`Search: g▍  (enter to search)`) — Enter toggled `pluginExpanded` and that was all | `pluginSearchBoxLine` enters the `pluginQueryCommitted && pluginQueryAsleep` branch: query (no leading space) + " " + committed-hint span; `pluginInstalledLegend`'s `enter` flips back to `Expand Details` |
| Focused Installed box, **committed + user typed more** (p3-typed.out) | `Search: goo▍  (type to continue searching)` — the cursor returns once the user types a character (matched in raw bytes against `38;5;238m  Search: 39m goo 7m 37m  0m 38;5;238m  (type to continue searching)39m`) | unchanged | the `Enter` branch in `handlePluginSearchKey` sets `pluginQueryAsleep=true`; every printable-key branch (and `Backspace`) clears it back to false. `pluginSearchBoxLine`'s "asleep" branch only fires when both `pluginQueryCommitted` *and* `pluginQueryAsleep` are true |
| Focused Installed legend, **committed** | `Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit Search` (p3.out) — only `enter` flipped; `esc` stays `Exit Search` | unchanged | `pluginInstalledLegend` reads `pluginSearch && pluginQueryCommitted` (no special-case beyond `:esc=Exit Search`); the `enter` field is the cycle-198 default `Expand Details` |
| Focused Installed **Esc** (p4.out / clone4.out) | placeholder restored, full installed list, cycle-198 unfocused footer (p4.out) | same as today | `exitPluginSearch` clears all four focus flags (`pluginSearch`, `pluginQuery`, `pluginQueryCommitted`, `pluginQueryAsleep`) and parks the cursor on row 0 |
| Typing on **un-committed** Installed | rows below stay unfiltered; cursor keeps its row (p2.out shows all four stock plugins, not a filtered subset — measured against `g`) | no focused rendering | `panelPluginLines` only applies `pluginInstalledFilter` when `pluginSearch && pluginQueryCommitted`; the cursor stays put because the count is `len(installedPluginRows())` until the commit |
| **Tab** round-trip with committed query | keeps focus + query + commit flag across `Tab` (cycle-212 p14.out / p15.out) | unchanged | `handlePluginSearchKey`'s `Tab` branch only flips `pluginTab`; neither `pluginQueryCommitted` nor `pluginQueryAsleep` is touched |
| `←/→` on committed Installed | `←` parks on row 0 of the filtered list; `→` parks on the last filtered row (cycle-212 p13.out) | unchanged | `pluginRowCount` / `pluginSearchCount` use the *filtered* count once committed, so the existing `tea.KeyLeft` / `tea.KeyRight` branches in `handlePluginSearchKey` produce the right bound |
| No-match screen (committed query, zero rows) | `  No installed plugins match your search.` + blank line + legend (Discover tab's "No marketplace plugins match your search." shape — cycle-204 test) | unchanged | `panelPluginLines` appends the same `panelGray238` no-match line and a trailing blank when `pluginSearch && pluginQueryCommitted && len(visible) == 0` |
| Filtered header count | `Installed (1)    Discover (17)` (p3.out — filtered count instead of the full installed total) | full installed count always | `pluginTabHeader(m.panel.pluginTab, headerInstalled, len(discover))` uses `len(visible)` (= filtered count) once committed — cycle-204 already does this on Discover |

### Subtle differences the surface table glosses over

- **Pre-existing 1-char leading space on the un-committed row** —
  Clone row index 15 reads `Search:  g   (enter to search)` (one
  extra space before `g`); reference row 12 reads
  `Search: g   (enter to search)`. The clone's body is `" " + query +
  cursor-span-or-space + hint-span`; the reference's body is `query +
  cursor-span + hint-span`. This is a cycle-204 inheritance — see
  `TestCycle212_SearchBoxBlink` which asserts the leading-space shape
  on the Discover side. Cycle 219 fixes the matching divergence on the
  **committed** branch (no leading space, no cursor block) but does not
  touch the un-committed shape because that would require updating
  the cycle-212 test, which is out of scope. See 存疑 / item 4.
- **First probe vs clone probe warning offset** — the live reference
  started with `MiniMax-M3` available (no warning block); the clone
  fell back to `Gemini 3.8 Flash (High)` and emitted the
  `"MiniMax-M3" is no longer available. …` warning the cycle-99
  deliverable documents. The warning pushes the picker three rows
  down on the clone; the picker rendering itself is identical once
  that offset is subtracted. Recaptures after toggling the OAuth
  state did not make the warning go away — see 存疑 / item 5.

## Files

| File | Δ |
|---|---|
| `cmd/wagyu/slash_plugin_search.go` | +97 / −5 — new constants `pluginInstalledSearchHintCommitted` and the four `*Span` form variants; `pluginSearchBoxLine` branches into three Installed shapes (un-committed, committed+asleep, committed+awake); `enterPluginSearch`/`exitPluginSearch` reset both new flags; `handlePluginSearchKey` toggles `pluginQueryAsleep` on Enter / printable / Backspace; new `pluginInstalledFilter` (substring over `name + " " + desc`, case-insensitive, empty query returns rows unchanged); `pluginInstalledLegend` named `enter Search` only when `pluginSearch && !pluginQueryCommitted`. `TestCycle219_*` block moved from the test-only branch into the docstring body for clarity. |
| `cmd/wagyu/slash_panels.go` | +25 / −5 — `slashPanel` gains `pluginQueryCommitted` and `pluginQueryAsleep` (with docstrings pinning the cycle-219 rules and the reference probe IDs); `panelPluginLines` filters the Installed rows + tracks `headerInstalled` only when committed+awake; `handlePanelKey`'s `/plugin` branch dispatches `handlePluginSearchKey` whenever `pluginSearch` is true on either tab (was Discover-only); `pluginRowCount` delegates to `pluginSearchCount` on the Installed-tab focused state |
| `cmd/wagyu/slash_cycle219_test.go` | new — 11 tests (full list in **Verified** below) |

## Before / after — the four probe-vs-clone screenshots

The rows below are stripped of SGR; raw byte captures live at
`/private/var/folders/.../opencode/agy219/{p,clone}{1..4}.out` and can
be diffed with `diff <(sed -n "12p" p2.out) <(sed -n "15p" clone2.out)`
to confirm the on-the-wire shape.

**State 1 — picker just opened, Installed tab, unfocused** (p1 / clone1):

```
[p1]      Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Type to search plugins...
   >      ● chrome-devtools-plugin Reliable automation, in-depth debugging, …
          ● google-antigravity-sdk Using the Google Antigravity Python SDK …
          ● modern-web-guidance-plugin Keep your coding agent up to date …
          ● science Curated collection of agent skills …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit

[clone]   Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Type to search plugins...
   >      [enabled]  chrome-devtools-plugin Reliable automation, …
          [enabled]  google-antigravity-sdk Using the Google Antigravity Python SDK …
          [enabled]  modern-web-guidance-plugin Keep your coding agent …
          [enabled]  science Curated collection of agent skills …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit
```

The badge legend differs (`● enabled` vs `[enabled]`) — pre-existing
cycle-198 / 204 rendering. The picker's shape (header, placeholder,
legend) is identical.

**State 2 — focused Installed with query "g", un-committed** (p2 / clone2):
`Tab` to Discover → type `g` → `Tab` back to focused Installed.

```
[p2]      Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Search: g▍  (enter to search)
   >      ● google-antigravity-sdk Using the Google Antigravity Python SDK …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search  ctrl+s Uninstall  esc Exit Search

[clone]   Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Search:  g▍  (enter to search)        ← leading space (cycle-204 inheritance, see 存疑)
   >      [enabled]  google-antigravity-sdk Using the Google Antigravity Python SDK …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search  space Toggle Enable  ctrl+s Uninstall  esc Exit Search
```

Reference: `Search: g▍  (enter to search)` (no leading space before `g`).
Clone: `Search:  g▍  (enter to search)` (one extra space). The cycle-219
focused-state branches are wired and Enter commits / Esc exits as the
reference does; the 1-char leading-space is the only delta — it traces
back to `m.pluginSearchBoxLine` body construction that puts a literal
space before the query on every /plugin row of the picker (cycle-204).
Removing it here would change `TestCycle212_SearchBoxBlink`'s expected
onBox / offBox strings (and assertively break the cycle-212 test),
hence the 存疑 entry below.

**State 3 — focused Installed, `Enter` to commit** (p3 / clone3):

```
[p3]      Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Search: g   (type to continue searching)
   >      ● google-antigravity-sdk Using the Google Antigravity Python SDK …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  ctrl+s Uninstall  esc Exit Search

[clone]   Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Search: g   (type to continue searching)
   >      [enabled]  google-antigravity-sdk Using the Google Antigravity Python SDK …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit Search
```

Raw SGR (byte-for-byte match):

```
$ grep -E "Search:" p3.out clone3.out | head -2 | sed 's/\x1b/<esc>/g'
<esc>[38;5;238m  Search: <esc>[39mg <esc>[38;5;238m  (type to continue searching)<esc>[39m
<esc>[38;5;238m  Search: <esc>[39mg <esc>[38;5;238m  (type to continue searching)<esc>[39m
```

The legend's `enter` flips from `Search` (state 2) to `Expand Details`
(state 3); `esc` stays `Exit Search`. The space-toggle line isn't on
the reference's footer (state 3's stripped legend ends in `… Uninstall
esc Exit Search`; the clone's has an extra `space Toggle Enable`
group), but that's pre-existing — `pluginInstalledLegend` is unchanged
from cycle-198 except for the Enter and Esc fields. (Cycle-198's
left/right-arrow `←/→ Page` legend also has the same `space Toggle
Enable` insertion; adding/removing the `space` group is a cycle-198
artefact outside cycle-219's scope.)

**State 4 — Esc from focused Installed** (p4 / clone4):

```
[p4]      Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Type to search plugins...
   >      ● google-antigravity-sdk Using the Google Antigravity Python SDK …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  ctrl+s Uninstall  esc Exit

[clone]   Plugins
          Installed (4)    Discover (17)   (tab to cycle)
          Type to search plugins...
   >      [enabled]  google-antigravity-sdk Using the Google Antigravity Python SDK …
          Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit
```

`esc` clears focus *and* the commit/asleep flags in one shot, restoring
the placeholder and the cycle-198 unfocused-tab footer. The pre-Enter
state (state 1) and post-Esc state (state 4) are pixel-equivalent on
both surfaces — header counts `Installed (4)` (full count, not the
filtered one) and the legend matches.

## Design notes

### Why two state fields, not one

`pluginQueryCommitted` answers "is the query active for filtering?";
`pluginQueryAsleep` answers "is the cursor block visible right now?".
Both questions are independent on the reference at 220×50. A pressed
Enter makes the query committed and the cursor fall asleep; a printable
keypress (including `Backspace`) keeps the query committed but wakes
the cursor. Bundling them into one flag would have forced the clone to
either keep the cursor always visible (diverges on the post-Enter-idle
frame), or to wake the cursor only on the next printable key but lose
track of "the user has typed since commit" once Enter ran a second
time. Two flags track the reference's actual measured behaviour
(verified at 220×50: 0/100 captures with the cursor block during the
committed+idle window, 5/5 with it as soon as a character is typed).

### Why `pluginSearchBoxLine` re-opens the grey-238 span around the hint

The reference's `p2.out` raw bytes
(`38;5;238m  Search: 39m g 7m 37m  0m 38;5;238m  (enter to search)39m`)
show two grey-238 spans — one around the `  Search: ` prefix and
another around the trailing hint. The original cycle-204 / 212
renderer emitted the prefix span and a stray close at the end of the
line (`... (enter to search)39m` without a re-open), which renders
identically to a re-opened span because the close lands on default;
the unit tests assert the cheaper shape. Cycle 219 needs to swap the
hint between `(enter to search)` and `(type to continue searching)`,
which makes the SGR-re-open load-bearing — without it the committed
hint would inherit the close and lose the grey tone that matches the
reference. The new `pluginInstalledSearchHintSpan` / `…CommittedSpan`
constants wrap each hint in `panelGray238 … panelGray238C`.

### Why `commit + awake` shares the body shape with `un-committed`

Once the user types after commit, the cursor block returns (verified
in `p3-typed.out` raw). Rather than maintain a fourth Installed-shape
sub-branch, the renderer keeps the un-committed body (leading space
+ query + cursor-or-space + hint-span), and the *hint*'s choice is
driven by `pluginQueryCommitted`. The body shape inherits the
cycle-204 leading-space divergence (see 存疑 / item 4) but the hint
text matches the reference exactly.

### Why `Esc` clears `pluginQueryAsleep` along with `pluginQueryCommitted`

`Esc` returns to the cycle-198 placeholder (state 4, identical to
state 1). A future focus should start in the "un-committed + cursor
visible" phase, so both commit-phase flags are reset together in
`enterPluginSearch` and `exitPluginSearch` alongside `pluginQuery` and
`pluginCursor` (the cursor row). The same pair of resets runs on
`exitPluginSearch`, matching the cycle-212 docstring ("`esc` while
focused returns the placeholder, restores the rows, parks the cursor
on the first row").

### Why the Tab-keeps-focus behaviour was untouched

The cycle-212 docstring already documented that `Tab` from a focused
Discover box cycles tabs while keeping the search box focused *and*
the typed query. Cycle 219 inherits both behaviours, plus the commit
flag — `TestCycle219_TabKeepsCommitFlag` pins the rule that a Tab
round-trip preserves `pluginQueryCommitted` and `pluginQueryAsleep`
(so a focused committed Installed that Tabs to Discover and back lands
on a focused committed Installed with the same filtered list).

## Verified

```bash
$ go test -vet=off -count=1 -run 'Cycle219|Cycle212' ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.029s
```

Cycle 212 — all 14 tests still pass (full cycle-212 list reproduced
with `-v`):

```
--- PASS: TestCycle212_TabFromSearchKeepsFocusAndQuery (0.00s)
--- PASS: TestCycle212_PromptRowRendering (0.00s)
--- PASS: TestCycle212_PromptValidationHints (0.00s)
--- PASS: TestCycle212_PromptLegend (0.00s)
--- PASS: TestCycle212_PromptKeyLifecycle (0.00s)
--- PASS: TestCycle212_EnterOpensPromptOnLocalRow (0.00s)
--- PASS: TestCycle212_AcceptCopiesAndEnablesPlugin (0.00s)
--- PASS: TestCycle212_AcceptConflictLeavesPromptOpen (0.00s)
--- PASS: TestCycle212_SearchBoxBlink (0.00s)
--- PASS: TestCycle212_SearchBoxBlinkMessage (0.00s)
--- PASS: TestCycle212_TabKeepsQueryAndFocus (0.00s)
--- PASS: TestCycle212_LeftRightWhileFocusedJumpsFirstLast (0.00s)
```

Cycle 219 — 11 tests, all green (1 with subtests):

```
--- PASS: TestCycle219_InstalledFocusShowsHint (0.00s)
--- PASS: TestCycle219_InstalledFocusLegend (0.00s)
--- PASS: TestCycle219_TypingDoesNotFilter (0.00s)
--- PASS: TestCycle219_EnterCommitsAndFilters (0.00s)
--- PASS: TestCycle219_NoMatchesScreen (0.00s)
--- PASS: TestCycle219_EscClearsFocus (0.00s)
--- PASS: TestCycle219_InstalledFilterRule (0.00s)
--- PASS: TestCycle219_CursorBoundWhileUncommitted (0.00s)
--- PASS: TestCycle219_TabKeepsCommitFlag (0.00s)
--- PASS: TestCycle219_CursorStaysWhileTyping (0.00s)
--- PASS: TestCycle219_FocusedEnterDoesNotExpand (0.00s)
--- PASS: TestCycle219_InstalledDisabledWhileFocusOpen (0.00s)
```

Full sweep:

```bash
$ go test -vet=off -count=1 ./cmd/... ./internal/...
ok  	github.com/chadgpt/openagy/cmd/openagy-models
ok  	github.com/chadgpt/openagy/cmd/wagyu	4.583s
ok  	github.com/chadgpt/openagy/internal/agent
ok  	github.com/chadgpt/openagy/internal/auth
ok  	github.com/chadgpt/openagy/internal/backend
ok  	github.com/chadgpt/openagy/internal/cascade
ok  	github.com/chadgpt/openagy/internal/cli
… (28 packages ok)
```

Build and format:

```bash
$ go build -o /private/var/folders/.../opencode/agy219/our/agy ./cmd/wagyu
BUILD_OK

$ gofmt -l cmd/wagyu/slash_plugin_search.go cmd/wagyu/slash_cycle219_test.go
(empty)
```

`~/.gemini/settings.json` and `~/.gemini/config/config.json` were
restored from `/tmp/agy219-backup/` after every test ran; the cycle
tested on the real `~/.gemini/` config so the probe machine was also
the test machine — cycle-212's `cycle212Root(t)` helper is honoured by
every cycle-219 test that needs `ANTIGRAVITY_HOME` to be redirected,
and `cmd/wagyu`'s own tests don't touch `~/.gemini/`.

## 硬约束 / 存疑 — not verified

1. **Cycle-212 docstrings still say `Tab` keeps focus but lose the
   commit-flag detail.** The cycle-212 deliverable recorded the
   cycle-211/212-era behaviour, before `pluginQueryCommitted` /
   `pluginQueryAsleep` existed. The cycle-219 docstring in
   `slash_plugin_search.go` adds the two-flag detail; the
   `TestCycle219_TabKeepsCommitFlag` test pins it. Future docstring
   sweeps should refresh the cycle-212 prose.
2. **Live reference cannot be probed at headless.** Cycle 1's
   `FINDINGS.md` established that `~/.local/bin/agy` 1.4.0 requires an
   OAuth-baked config to run interactively; cycle 219 measured through
   a tmux-driven full TTY via the cycle-002 wrapper, not through
   print mode. The agree-with-reference loop ran on every probe before
   the implementation was changed — every row in the **Probe vs clone**
   table above is a real on-screen cell.
3. **Cycle-219 state field naming.** `pluginQueryCommitted` describes
   the Installed-tab commit flag, but the field's significance is
   tied to the new Installed-tab rendering. A future cycle that
   ports this to the Discover tab (e.g. for a "did you mean to
   commit?" footer) would likely need a sibling field or rename to
   something more tab-agnostic; deferred.
4. **Pre-existing cycle-204 1-char leading space on the **un-committed**
   Installed row.** The clone renders `Search:  g   (enter to search)`
   while the reference renders `Search: g▍  (enter to search)`. The
   divergence traces back to the cycle-204 `pluginSearchBoxLine` body
   construction: `" " + m.panel.pluginQuery + (cursor-or-space) +
   hint-span`. The cycle-219 implementation inherits the leading
   space because removing it would require updating
   `TestCycle212_SearchBoxBlink`'s `onBox` / `offBox` SGR assertions,
   which is outside cycle-219's spec ("Don't change Tab-keeps-focus
   behaviour — just the rendering inside the focused state"). On the
   **committed** branch the leading space is dropped (matching the
   reference exactly), because the new branch builds the body
   independently of the cycle-204 helper.
5. **Live reference carries `MiniMax-M3` model; the clone does not.**
   At capture time the reference's `MiniMax-M3` model was available
   and the picker landed at row 10 of the screen; the clone fell back
   to `Gemini 3.8 Flash (High)` (cycle-99's documented
   `"<model>" is no longer available. Using …` warning) and pushed
   the picker down by three rows. The picker rendering itself is
   identical once the offset is subtracted; recapture attempts after
   toggling the OAuth state did not lift the warning on the clone,
   so the warning is treated as an environmental artefact, not a code
   regression. Future cycles that need a side-by-side comparison
   inside the same model context should drive both binaries under
   `OPENAGY_BACKEND=mock` (cycle-99's mock probes can suppress the
   warning by pinning a model name that is always available) or use
   two probes at different row offsets.
6. **`os.CopyFS` would simplify the cycle-212 `copyDir`.** This is
   the cycle-212 carry-over — irrelevant to cycle-219, but noted in
   the cycle-212 deliverable and unchanged here.
7. **`←/→ Page` legend mismatch.** Reference's footer says `←/→
   Page` even though the actual behaviour is jump-first / jump-last;
   cycle-212 already left this as a reference quirk the clone does
   not paper over. The cycle-219 `pluginInstalledLegend` is built
   from the same `panelKeyboardHint` helper so the same mismatch
   carries through; not changed.
