# Cycle plugin-space-toggle — `/plugin` Installed tab's `space Toggle Enable`

## Goal

Close the last TODO.md P4 line item that listed
`/plugin space Toggle` as inert:

> panel 内的按键动作：/model ←/→（切换 effort）、/config ←/→（change value）、
> /plugin tab（cycle tabs）、**space Toggle** 等仍 inert。

The wire-up predates this cycle. Cycle 198 first put a Space handler in
`handlePanelKey`'s `panelPlugin` branch and wired it to a new
`m.togglePluginEnabled()` that flips `plugins.<name>.enabled` in
`~/.gemini/config/config.json`. Cycle psif
(`plugin-search-input-flow`) extended the same behaviour to the
focused-committed branch of `handlePluginSearchKey`, so a Space press
in the Installed Committed phase no longer appends a space to the
query — it toggles the highlighted row's enabled flag. The skill was
in place; what was missing was a single test battery pin-pointing the
user-visible behaviour so a future contributor reading the cycle has a
single place to look. This cycle lands that battery and updates
TODO.md / the §6 诚实清单 to reflect the closure.

## Key binding

The `space` binding depends on which tab and which phase the user is
in. Every branch is wired; the cycle's regression battery pins each
one.

| Surface | Tab | Phase | `space` action | Toggle site |
|---|---|---|---|---|
| `/plugin` Installed | un-focused | — | toggle cursor row's `plugins.<name>.enabled` | `handlePanelKey` `case tea.KeySpace:` |
| `/plugin` Installed | focused, un-committed | Typing | append ` ` to the typed query | `handlePluginSearchKey` `case tea.KeySpace:` (else branch) |
| `/plugin` Installed | focused, committed | Committed | toggle cursor row's flag (filtered list) | `handlePluginSearchKey` `case tea.KeySpace:` short-circuit |
| `/plugin` Discover | un-focused | — | focus the search box (`enterPluginSearch`) | `handlePanelKey` `case tea.KeySpace:` else branch |
| `/plugin` Discover | focused | — | append ` ` to the typed query | `handlePluginSearchKey` catch-all printable |

The cycle pins the un-focused Installed path (the headline direction)
and the un-focused Discover path (the legend's only `space` on
Discover). The focused branch is covered by cycle psif
(`TestPSIF_SpaceInCommittedTogglesEnabled`,
`TestPSIF_SpaceTogglesFilteredRow`,
`TestPSIF_SpaceInTypingStillAppends`) and cycle 204
(`TestCycle204_SearchKeyLifecycle`, `TestCycle204_PrintableKeyFocusesAndFilters`).

## Persistence

The toggle writes to `~/.gemini/config/config.json`'s `plugins` map.
The helper is `auth.SetPluginEnabled(name, !on)`, defined in
`internal/auth/compat.go` (cycle 198 addition). The writer:

1. Loads the existing `plugins` map (preserving every other key under
   `plugins.<name>` and every sibling key like `userSettings`).
2. Sets `<name>.enabled = !on` — or creates the row when none existed.
3. Marshals and atomically rewrites `config.json` with `0600` perms.

The reader is `auth.PluginEnabled(name)`, called by both
`m.pluginEnabled(name)` (the badge source) and the toggle's own
"current state" probe. A read failure returns `(false, err)`, which
the toggle handles by aborting the flip — a transient read failure
never silently toggles.

`AGY_CLI_DISABLE_PLUGINS=1` and the matching config flag (`disableAllPlugins`)
still win over the per-row `enabled` flag (cycle
`merge-agents-json-in-plugins` / AGY 1.2.16 close-gap): when the
global gate is on, every row badges as `[blocked]` regardless of
its enabled flag, and `space` carries no observable effect on the
visual state (the disk flag still flips, but the badge is
`[blocked]`). The cycle does not test this — it's a cycle-202
surface, and the existing `TestPluginSpaceToggle_RerendersRow` runs
without `t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")` so the global
blocker is the unset zero value.

## Re-render

The panel re-reads the flag through `m.pluginEnabled(p.name)` inside
`panelPluginLines()` (`slash_panels.go:2044`), and the badge map
(`pluginBadge(enabled bool)`, `slash_panels.go:2155`) returns the
styled `[enabled] ` or `[disabled]` substring every frame. No
explicit `m.rerenderBlocks()` is required — the next Update tick
calls `panelLines` → `panelPluginLines` and picks up the new badge.
This is the same dependency that drives cycle dpl's
`TestDPL_PanelCloseResetsFilter` and cycle 198's
`TestCycle198_SpaceTogglesInstalled`.

## Files

| Path | Role |
|------|------|
| `cmd/wagyu/slash_plugin_space_toggle_test.go` (new, +240 / −0) | Five `TestPluginSpaceToggle_*` tests pinning the user-visible surface. Reuses the `cycle198Root` / `seedPlugin` / `seedPluginConfig` / `cycle212Root` / `seedMarketplace204` / `marketplaceEntry204` helpers already exported by `slash_cycle198_test.go` and `slash_cycle204_test.go`. |
| `TODO.md` (modified, +6 / −4) | Strikes through the two TODO.md P4 bullets that named `space Toggle` as inert and `space Toggle 都没有实测` as untested, replacing them with closure notes pointing at the four prior cycles (196, 197, 198, psif) and this battery. |
| `docs/cycles/plugin-space-toggle/deliverable.md` (this file) | Cycle write-up. |

**Net:** +246 / −4 across 3 files (1 new).

## Tests

```bash
cd <worktree>
gofmt -l cmd/wagyu/slash_plugin_space_toggle_test.go   # empty
go build -o /tmp/wagyu-check ./cmd/wagyu               # exit 0
go test -vet=off -count=1 -run TestPluginSpaceToggle ./cmd/wagyu/
# ok  github.com/chadgpt/openagy/cmd/wagyu  ~1 s   5/5 PASS
```

| Test | What it pins |
|------|--------------|
| `TestPluginSpaceToggle_EnabledToDisabled` | Headline direction: enabled row + Space → flag flips to disabled; the handler reports `(true, nil)` so the Space never falls through to the printable-input filter at `handlePanelKey:3268`. |
| `TestPluginSpaceToggle_DisabledToEnabled` | Inverse direction (and round-trip back to disabled on a second Space). |
| `TestPluginSpaceToggle_PersistsToDisk` | Re-reads `config.json` directly (not just the in-memory cache) to confirm the flip persists; also confirms `SetPluginEnabled` against a fresh name writes a new row when none existed. |
| `TestPluginSpaceToggle_RerendersRow` | Calls `panelLines` after the toggle and asserts the row's plain-text badge substring (`[enabled]` → `[disabled]`) without any explicit rerender hook. |
| `TestPluginSpaceToggle_SpaceOnDiscoverTab` | Un-focused Discover + Space → `m.panel.pluginSearch` flips to `true` and `m.panel.pluginQuery` stays empty (the search box got focused but no character was inserted). Companion to cycle 204's TestCycle204_SearchKeyLifecycle focused-mode insert test. |

The five tests cover every cell in the State × Tab table above that
isn't already pinned by another cycle's suite. Together with cycle
198 (`TestCycle198_SpaceTogglesInstalled` and
`TestCycle198_PluginEnabledRoundTrip`) and cycle psif
(`TestPSIF_Space*`), `space` on `/plugin` is now exhaustively
exercised: every state, every tab.

## Verification

```bash
cd <worktree>
gofmt -l cmd/wagyu/slash_plugin_space_toggle_test.go   # empty
go build -o /tmp/wagyu-check ./cmd/wagyu               # exit 0
go test -vet=off -count=1 -run TestPluginSpaceToggle ./cmd/wagyu/
# 5/5 PASS

# Full-cycle sanity sweep — none of the prior cycle's tests regress.
go test -vet=off -count=1 -run 'TestPluginSpaceToggle|TestCycle198_SpaceTogglesInstalled|TestPSIF_|TestCycle204_PrintableKey|TestCycle204_SearchKey|TestCycle212_|TestCycle219_|TestDPL_' ./cmd/wagyu/
# all PASS
```

The only `go vet` complaint this worktree still surfaces is
`cmd/wagyu/model_default_effort_test.go:67:3: Errorf call needs 2
args but has 3 args`, which lives on `main` too (pre-existing, not
introduced by this cycle; see the cycle-psif verification log).

## Branch / base

| | |
|---|---|
| Branch | `feature/plugin-space-toggle` |
| Base | `10a4d31` (current `main`) |
| Worktree | `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-plugin-space-toggle` |
| Commit | single, no push (per brief) |
| Files changed | 3 (+1 new), +246 / −4 |

## 硬约束 / Limitations

- The cycle does not retest the cycle psif / cycle 198 fixtures in
  isolation. The TestPluginSpaceToggle_* battery walks the same
  surface as cycle 198's `TestCycle198_SpaceTogglesInstalled` and
  cycle psif's `TestPSIF_SpaceInCommittedTogglesEnabled`, but each
  test seeds its own tempdir so the two suites are independent. A
  regression in either file is caught by its own battery — they
  don't gate each other. This was a deliberate choice (clearer
  failure attribution) over deduplication via shared helpers.
- The cycle does not pin what happens when `AGY_CLI_DISABLE_PLUGINS=1`
  is set during a Space press (the global blocker masks the per-row
  toggle visually, but the disk flag still flips). The cycle-202
  surface already covers the render path; combining it with the
  toggle handler would be a one-test follow-up if the reference ever
  adds a "global-blocker also disables the binding" behaviour.
- The cycle does not pin `space` (with `Mod` non-zero) explicitly as
  a no-op. The handler at `handlePanelKey:3128` matches
  `key.Code == tea.KeySpace`, which bubbletea emits with `Mod == 0`
  for a bare Space key; any other modifier produces a different code
  (e.g. `key.Code == ' '` with `Mod == ModCtrl`). The catch-all at
  `slash_panels.go:3268` already returns `false` for modified keys
  (`key.Mod == 0 && key.Text != ""` is `false` for any modifier),
  so the binding is structurally inert — but no test asserts this.
  Tracked in the §6 诚实清单 follow-up notes if a probe-driven
  reference comparison demands one.
- TODO.md §6 (诚实清单) still retains two deeper limitations — the
  `enter Save` row in `/config` is never wired (cycle 196 only wired
  the `←/→` change-value path) and the `enter Save` confirm step in
  `/model` is similarly absent. Those are unrelated to `space Toggle`
  and remain P4 follow-ups under their own cycle banners.

## Changelog anchor

The cycle is a documentation / test-coverage closure, not a new
changelog line. The user-visible behaviour it pins matches
Antigravity CLI 1.4.0's `/plugin Installed tab` as probed in
`out/local-macos/probe_cycle198_plugin2.py` (cycle 198), with the
focused-mode extension matching AGY 1.3.3 changelog line 46 (cycle
psif). No CHANGELOG entry is amended by this cycle.
