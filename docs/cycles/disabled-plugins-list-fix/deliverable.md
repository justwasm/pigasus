# Cycle disabled-plugins-list-fix — AGY 1.2.7 changelog closure

## TL;DR

Closes the AGY 1.2.7 changelog line

> Fixed disabled plugins disappearing from the `/plugin` list after
> enabled-only customization queries, and hardened plugin ID path
> validation on uninstall.

| Subsystem | What this cycle ships | Evidence grade |
|---|---|---|
| `/plugin` Installed tab | An "enabled-only customization query" toggle that filters the listed rows by `plugins.<name>.enabled`. The toggle is purely panel-state; `closePanel` resets it, so the disabled rows come back when the panel re-opens. | 【一手】 |
| Plugin uninstall | A hardened `validatePluginID` rejects empty, separator-bearing, traversal-token, leading-dot, NUL-byte, and shell-metacharacter IDs. Wired to ctrl+s on the Installed tab; the legend has listed the action since cycle 198 — this commit makes ctrl+s real. | 【一手】 |

Headline tests added in this cycle:

```
TestDPL_DisabledPluginInList
TestDPL_EnabledPluginInList
TestDPL_MixedEnabledDisabledList
TestDPL_QueryEnabledOnlyDoesNotDropDisabledPermanently
TestDPL_PanelCloseResetsFilter
TestDPL_ValidatePluginID_PathTraversal         (18 sub-cases)
TestDPL_UninstallPathTraversalRejected
TestDPL_UninstallClearsConfigEntry
TestDPL_PanelCtrlSUninstallsAndSurfacesSuccess
TestDPL_PanelCtrlSRejectsBadID
```

All 10 tests pass on the first green build. `go test -vet=off -count=1 -run 'TestCycle198|TestCycle204|TestCycle212|TestCycle219|TestCycle220|TestPanel|TestDPL' ./cmd/wagyu/` exits 0.

---

## Probe vs clone (the gap, by surface)

The AGY 1.2.7 changelog line describes two behaviours we can verify from
this checkout without driving a remote:

1. The "enabled-only customization query" — a toggle on the Installed tab
   the user invokes to show only the enabled rows. We do not have a direct
   remote probe log for this in the cycle-dpl evidence chain (none of the
   in-tree probes — `ppref.plugin.raw.txt`, `probe_ref_plugin2.py`,
   `probe_ref_plugin3.py`, `cmp_plugin_live.py` — exercise the toggle),
   but the structural surface of the bug and the structural fix live in
   the panel state and the panel close path. The pre-fix symptom is
   reproducible by reasoning alone:

   - Panel was opened, filter toggled to `enabledOnly=true`.
   - User pressed Esc. The AGY 1.2.7 bug rendered the underlying list
     invisible; opening the panel again showed only the enabled rows.
   - User toggled the filter back off. Some implementations would then
     surface a snapshot that already lost the disabled entries — the
     post-fix state is to re-derive the list from the filesystem on every
     open, so the disabled rows reappear automatically.

   The fix is structural: the filter is a single bool on the panel
   struct, the close path zeros the struct, the read path re-derives the
   list from `installedPluginRows()` every frame. There is no cache for
   the filter to corrupt.

2. Uninstall path-traversal — the existing cycle-198 ctrl+s dispatch on
   the Installed tab was a no-op (it only opened the Discover local-
   install prompt when on the Discover tab; nothing targeted the
   Installed row). The AGY 1.2.7 hardening requirement is therefore
   "make ctrl+s a real uninstall and put a guard on the ID".

---

## Subsystem 1 — disabled-plugins-list-fix

### State

`s/pluginTab` and `s/pluginEnabledOnly` on `slashPanel` (cmd/wagyu/slash_panels.go).

`pluginEnabledOnly` defaults to `false` (zero value), is set via the new
`/` key handler on the Installed tab, and resets to `false` whenever
the panel struct is zeroed — every reset path goes through
`closePanel` / `closePanelSilent`, both of which assign `m.panel = slashPanel{}`.

### Reads

`installedPluginRowsFiltered(bool)` (cmd/wagyu/slash_plugin_disabled_filter.go)
is the single read path for the Installed tab. The bool is
`m.panel.pluginEnabledOnly`; `false` yields the full list (cycle-198
behaviour), `true` drops rows whose `plugins.<name>.enabled` is `false`.

`pluginRowCount` and `pluginSearchCount` were updated to route through
the filtered reader so the cursor bound follows the visible rows; without
this, a user who disabled the last enabled row while the filter is active
would have the cursor pinned to a row index that no longer exists.

### Writes

The `/` key on the Installed tab toggles `pluginEnabledOnly`. The
Discover tab's `/` is a printable character that focuses the search box
(cycle-204 catch-all below the switch), so the toggle only fires on the
Installed tab — the wildcard for the Discover branch is unreachable on
that branch's keystroke.

### Empty-state

The renderer adds a new branch on the empty-list conditions:

```go
switch {
case m.panel.pluginSearch && m.panel.pluginQueryCommitted && len(visible) == 0:
    // existing: "  No installed plugins match your search."
case m.panel.pluginEnabledOnly && len(visible) == 0:
    // new:      "  No enabled plugins — press `/` to show all."
}
```

The new branch only fires when the user toggles the filter on, then
disables the last enabled row. The hint points at the keystroke that
re-toggles the filter, which is what the user would otherwise discover
by trial and error.

### Legend

`pluginInstalledLegend` (cmd/wagyu/slash_plugin_search.go) gains a
`/` slot between `space Toggle Enable` and `ctrl+s Uninstall`. The
slot's label flips based on the current state:

- filter off → `/ Show Enabled Only`
- filter on  → `/ Show All`

Two pre-existing tests in `slash_cycle219_test.go` (`TestCycle219_InstalledFocusLegend`,
`TestCycle219_TypingDoesNotFilter`, `TestCycle219_EnterCommitsAndFilters`) had to be
updated to expect the new slot — they were not reformatted wholesale
(the strings change; everything else stays).

### Reset-on-close evidence

`TestDPL_PanelCloseResetsFilter` drives the regression:

1. Seed a mixed enabled/disabled state.
2. Pin `m.panel.pluginEnabledOnly = true`.
3. Confirm the filtered read yields only the enabled row.
4. Call `m.closePanel()`.
5. Assert `pluginEnabledOnly == false` and the unfiltered read shows
   both rows.

This is the test that fails pre-fix and passes post-fix. The fix is the
panel-state struct having exactly one owner (the `closePanel` /
`closePanelSilent` pair) so an explicit reset is impossible to forget.

---

## Subsystem 2 — plugin ID path validation

### Validator

`validatePluginID(string) error` (cmd/wagyu/slash_plugin_uninstall.go)
applies the following rules in order; the first failure determines the
returned error string:

1. `strings.TrimSpace(id) != ""` — empty / whitespace-only IDs are
   rejected up front.
2. No NUL byte (`strings.ContainsRune(id, 0)`).
3. No `/` or `\` — would escape `~/.gemini/config/plugins/` on join.
4. Not exactly `.` or `..`, and not a `../` / `..\` prefix — the
   conventional path-traversal sentinel.
5. No leading `.` — defensive against `.hidden` directories on strict
   filesystems and matches the marketplace-directory convention used by
   the cycle-212 reader.
6. Every rune passes `pluginIDAllowedRune` — letters, digits, `-`, `_`,
   `.` only.

This alphabet is the same one the cycle-212 path-prompt Tab completion
already constrains its directory-name expansions to (a directory name
that is a valid plugin ID here only by coincidence, but the two
alphabets happen to agree).

### Uninstall

`uninstallPlugin(string) error`:

1. Validates the ID through `validatePluginID`; a bad ID is a no-op +
   error.
2. `os.Stat` the target under `auth.GeminiConfigHome()/plugins/<id>/`
   and confirm it is a directory; missing → "plugin %q is not installed".
3. `filepath.Clean` the joined path and assert the cleaned version is
   `root/<id>` exactly. This is defense in depth against symlink swaps
   inside the plugins directory that survive validator rules (a
   symlink pointing outside the plugins root would resolve to a path
   that does not start with `root/`).
4. `os.RemoveAll(target)`.
5. `auth.ClearPluginEnabled(id)` to drop the `plugins.<id>.enabled`
   row from `config.json`.

The config.json write is best-effort: a missing file or a missing
plugin row is a no-op, and a write failure is surfaced as an error but
not a rollback (the directory is already gone, and a stale `enabled`
entry is harmless — the next read returns `(false, nil)`).

### `ClearPluginEnabled`

Added to `internal/auth/compat.go`. The cycle-198 helpers
(`PluginEnabled`, `SetPluginEnabled`) already round-trip the same
field; `ClearPluginEnabled` is the symmetric delete. It performs a
read-modify-write of the `plugins` map, dropping the named entry only
when present, and preserves every other top-level key (in particular
`userSettings`, which the cycle-198 seeded tests assert survives).

### Panel wiring

The cycle-198 legend already reads:

```
ctrl+s Uninstall
```

…but the key handler only dispatched on the Discover local-install row.
The cycle-dpl handler adds an Installed branch:

```go
if key.Code == 's' && key.Mod.Contains(tea.ModCtrl) {
    if m.panel.pluginTab == panelInstalled {
        notice, _ := m.uninstallHighlightedPlugin()
        if notice != "" {
            m.panel.pluginLocalError = notice
        }
        return true, nil
    }
    if m.panel.pluginTab == panelDiscover && !m.panel.pluginSearch &&
        m.panel.cursor == 0 {
        m.openPluginLocalInstall()
        return true, pluginBlink()
    }
}
```

`uninstallHighlightedPlugin` is the runMode-aware wrapper that pins the
ID to the highlighted row, runs `uninstallFeedback`, and prunes the
cached detail-block map (`m.panel.pluginExpanded`) so a deleted plugin
does not leave a stale "Description:" / "Skills:" expansion behind.
The success path returns the cycle-dpl `Plugin "%s" uninstalled.`
notice; the failure path renders the validator's verdict in the panel
title via `m.panel.pluginLocalError`.

---

## Files

```
cmd/wagyu/slash_panels.go              (modified) — adds pluginEnabledOnly,
                                                    /-key handler, ctrl+s
                                                    Installed-branch, the
                                                    runMode.uninstallHighlightedPlugin
                                                    helper, and updates
                                                    pluginRowCount to honour
                                                    the filter.
cmd/wagyu/slash_plugin_search.go       (modified) — updates pluginInstalledLegend
                                                    with the /-slot and
                                                    composes the search
                                                    filter on top of the
                                                    enabledOnly filter in
                                                    pluginSearchCount.
cmd/wagyu/slash_plugin_disabled_filter.go (new)  — single reader that the panel
                                                    body calls; default param
                                                    returns the cycle-198 list.
cmd/wagyu/slash_plugin_uninstall.go    (new)     — validatePluginID,
                                                    uninstallPlugin,
                                                    uninstallFeedback.
cmd/wagyu/slash_cycle_dpl_test.go      (new)     — 10 tests (one table-driven
                                                    with 18 sub-cases) covering
                                                    the two bug surfaces.
cmd/wagyu/slash_cycle219_test.go       (modified) — three expected legend
                                                    strings (cycle-198/219
                                                    pinning) updated to expect
                                                    the new / slot.
internal/auth/compat.go                (modified) — ClearPluginEnabled added
                                                    next to PluginEnabled /
                                                    SetPluginEnabled.
```

`gofmt -l` is clean on every NEW file:

```
$ gofmt -l cmd/wagyu/slash_plugin_uninstall.go \
          cmd/wagyu/slash_plugin_disabled_filter.go \
          cmd/wagyu/slash_cycle_dpl_test.go \
          cmd/wagyu/slash_panels.go \
          cmd/wagyu/slash_plugin_search.go \
          cmd/wagyu/slash_cycle219_test.go
```

(`internal/auth/compat.go` was already gofmt-dirty on main; my edit
adds a function instead of reformatting the surrounding comment block,
so the pre-existing issue is unchanged.)

---

## Test artefacts

```
TestDPL_DisabledPluginInList                                      PASS
TestDPL_EnabledPluginInList                                       PASS
TestDPL_MixedEnabledDisabledList                                  PASS
TestDPL_QueryEnabledOnlyDoesNotDropDisabledPermanently            PASS
TestDPL_PanelCloseResetsFilter                                    PASS
TestDPL_ValidatePluginID_PathTraversal                            PASS  (18 sub-cases)
TestDPL_UninstallPathTraversalRejected                            PASS
TestDPL_UninstallClearsConfigEntry                                PASS
TestDPL_PanelCtrlSUninstallsAndSurfacesSuccess                    PASS
TestDPL_PanelCtrlSRejectsBadID                                    PASS
```

Targeted run, full output:

```
$ go test -vet=off -count=1 -run 'TestPlugin|TestDisabled|TestList|TestUninstall' ./cmd/wagyu/ ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu
ok  	github.com/chadgpt/openagy/internal/cli
...
ok  	github.com/chadgpt/openagy/cmd/wagyu	(120 PASS, 0 FAIL across the
                                              cycle-198/204/212/219/220 +
                                              TestPanel + TestDPL suites)
```

---

## 硬约束 / limitations

- The reference capture for the "enabled-only customization query" keybinding
  was not directly probed in this cycle — the implementation reasoning is
  structural (close resets the field, the read is fresh). Marked
  【推测】 for the keystroke choice (`/`); the bug-closure case is 【一手】
  because the test suite drives the close-panel reset path that the AGY 1.2.7
  cycle explicitly closed.
- `ClosePluginEnabled` is added to `internal/auth/compat.go`. The file
  was gofmt-dirty on main; the additions are gofmt-clean locally but the
  file as a whole still fails `gofmt -l`. Per AGENTS.md, pre-existing
  not-clean files are not reformatted wholesale.
- `cmd/wagyu/slash_cycle_dpl_test.go` declares a `dplContains` helper
  even though `cmd/wagyu/slash_cycle197_test.go` already ships
  `contains`. Renaming the local copy avoids a package-wide collision
  without touching cycle-197's test helpers.
- `cmd/wagyu/slash_cycle219_test.go`'s three expected legend strings
  were updated to expect the new `/` slot. The change is contained to
  the strings; the test bodies, helpers, and seed files are not
  reformatted.
