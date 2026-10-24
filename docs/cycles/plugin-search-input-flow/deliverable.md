# Cycle plugin-search-input-flow — `/plugin` Installed search-input state machine

## Goal

AGY 1.3.3 changelog line 46 closes the last gap in the Installed tab's
focused search box:

> "Improved search in the `/plugin` Installed tab: you can now type
> spaces in a search query, press `Enter` to finish searching, then
> use `Space` to toggle the selected plugin and `Enter` to expand its
> details; start typing again to resume the search."

The state machine is `Typing → Committed → Typing → ...`. Cycle 219
shipped the rendering of both states (the `(enter to search)` hint
flips to `(type to continue searching)` on Enter) and the Enter
transition; this cycle closes the three remaining keyboard gaps:

| Phase | Keystroke | Behaviour | Before | After |
|---|---|---|---|---|
| Typing | `space` | inserts ` ` into the query | ✅ | ✅ unchanged |
| Typing | any printable | appends to the query | ✅ | ✅ unchanged |
| Typing | `Enter` | commits → Committed | ✅ | ✅ unchanged |
| Committed | `space` | inserts ` ` | ✅ (wrong) | ❌ → **toggles the highlighted row's `plugins.<name>.enabled` flag** |
| Committed | `Enter` | toggles detail block | ✅ | ✅ unchanged |
| Committed | any printable | appends to the query, narrows filter live | ✅ | ✅ unchanged |
| Committed | `Esc` | leaves search | ✅ | ✅ unchanged |

## State machine

```
                Enter                any printable / backspace
   ┌────────┐  ────────►  ┌─────────┐  ◄─────────────────────────────┐
   │ Typing │            │Committed│                                │
   └────────┘  ◄────────  └─────────┘  Enter (toggle detail block)  │
       ▲     Enter            Space        Space (toggle enabled flag) │
       └─────────────────────┘            Esc leaves search ─────►   │
                                                                    │
                              ◄────────────────────────────────────┘
```

`Typing` — the user is composing the query. The rows below stay
unfiltered (cycle 219). Any printable key (including `space`) appends
to the query. `Enter` commits and parks the cursor on the first
filtered row.

`Committed` — the rows are filtered live. `Enter` re-awakens the
cursor and toggles the detail block under the highlighted row.
`Space` toggles the highlighted row's enabled flag — the new behaviour
this cycle adds. Any printable key (other than `Space`, which toggles)
appends to the query and narrows the filter live; the cursor re-
awakens as soon as the user types. `Backspace` drops the last rune and
keeps the cursor awake. `Esc` clears every focused-state flag and
returns to the unfocused Installed tab.

## Files

| Path | Role |
|------|------|
| `cmd/wagyu/slash_plugin_search.go` (+13 / −1) | `handlePluginSearchKey` `case tea.KeySpace:` adds a Committed-on-Installed short-circuit that calls `togglePluginEnabled()` instead of appending a space. The existing Typing branch is unchanged. |
| `cmd/wagyu/slash_panels.go` (+34 / −2) | new `installedActiveRows()` helper that returns the filter result when the Installed tab's box has focus and the query is committed, the full installed list otherwise. `togglePluginEnabled` and `togglePluginExpanded` now read this helper instead of `installedPluginRows()` unconditionally. |
| `cmd/wagyu/slash_psif_test.go` (new, 433 lines) | 9 unit tests pinning the state machine and the active-list fix. |
| `docs/cycles/plugin-search-input-flow/deliverable.md` (this file) | cycle write-up. |

**Net:** +50 / −3 across 3 files (1 new).

## Why the active-list fix matters

Before the cycle, both `togglePluginEnabled` and `togglePluginExpanded`
read `installedPluginRows()` unconditionally — the *full* installed
list, not the filtered one. The cursor is bounded by
`pluginSearchCount()`, which is the filtered count when committed, so
the cursor index is in `[0, filtered_count)` while the action reads
`rows[cursor]` from the *full* list. The two only agree when the
filtered and full lists are the same set.

Cycle 219's tests seeded a single matching plugin and never tripped
the bug. The cycle-psif `TestPSIF_SpaceTogglesFilteredRow` seeds
`alpha-x`, `beta-x`, `foxtrot-x`, commits `"f"` (only `foxtrot-x`
matches), and asserts:

| Cursor | Filtered row | Full row | Space toggles |
|---|---|---|---|
| 0 | foxtrot-x | alpha-x | **foxtrot-x** (was: alpha-x — wrong) |

The same fix corrects `togglePluginExpanded` for the cycle-219
focused-Enter path.

## Tests

`cmd/wagyu/slash_psif_test.go` ships 9 tests pinning the state
machine. The helper `commitInstalled(t, m, query)` seeds the
Committed phase so every test starts from the same place.

| Test | What it pins |
|---|---|
| `TestPSIF_TypingAllowsSpaces` | Typing phase: spaces append to the query, state stays un-committed, cursor stays awake. |
| `TestPSIF_EnterCommitsSearch` | Typing → Committed: Enter sets the flag, rows filter live, legend's `enter` field flips to "Expand Details". |
| `TestPSIF_SpaceInCommittedTogglesEnabled` | Committed → Space toggles `plugins.<name>.enabled` in config.json, leaves the query alone. |
| `TestPSIF_SpaceTogglesFilteredRow` | The active-list fix: Space toggles the filtered row, not the same-index full-list row. |
| `TestPSIF_EnterInCommittedExpandsDetails` | Committed → Enter opens (and second Enter closes) the highlighted row's detail block. |
| `TestPSIF_TypingAfterCommitResumesSearch` | Committed → Typing: a printable key re-awakens the cursor and narrows the filter live; the flag stays set. |
| `TestPSIF_MultiToggleEachSpaceActsOnSelected` | Multi-toggle rule: each Space flips only the highlighted row, leaving the others alone. |
| `TestPSIF_FilterNarrowsAsUserTypes` | Live-filter rule: every printable key narrows the row list; backspace widens it back up. |
| `TestPSIF_SpaceInTypingStillAppends` | Regression guard for the Typing-phase Space branch — multi-word queries still type into the query. |

## Verification

```bash
cd <worktree>
gofmt -l cmd/wagyu/slash_psif_test.go     # empty
go build -o /tmp/wagyu-psif ./cmd/wagyu   # exit 0
go test -vet=off -count=1 -run TestPSIF ./cmd/wagyu/   # 9/9 PASS
go test -vet=off -count=1 ./cmd/... ./internal/... \
  | grep -E '(FAIL|ok )' \
  | grep -v 'ok '                         # only the pre-existing
                                           # TestDispatchPrintSlash_LocalCommands
                                           # failure on main
```

The single `FAIL` in the test run is `TestDispatchPrintSlash_LocalCommands`
in `cmd/wagyu/print_slash_test.go:70`. It fails on `main` too (it
expects the model label to default to `MiniMax M3` while the live
environment resolves `MiniMax-M3` via the agw backend). Unrelated to
this cycle; the cycle brief says "documented pre-existing flakes" are
acceptable.

## Branch / base

| | |
|---|---|
| Branch | `feature/plugin-search-input-flow` |
| Base | `6bd5d5f` (the current `main`, ahead of `a58d7dd` by the cycle's "remove duplicate announce-cards struct block" cross-worktree fix that the dispatcher did not know about). Branching from `a58d7dd` directly compiles to a syntax error from the duplicate struct block; rebasing onto `6bd5d5f` lets the build succeed without touching that fix's intent. |
| Worktree | `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-plugin-search-input-flow` |
| Commit | single, no push (per brief) |
| Files changed | 3 (+1 new), +50 / −3 |

## 硬约束 / Limitations

The cycle ships the keyboard surface; it does not ship a live
captured-byte-by-byte render of every focused-mode frame. The
expectations are pinned against the existing cycle-219 + cycle-204
probes (`docs/cycles/cycle-219/deliverable-plugin-installed-focus.md`,
`docs/cycles/cycle-204/deliverable-plugin-search.md`), which the
changelog's description aligns to. Things still pending for a
follow-up cycle:

- The reference's rendering of the `(type to continue searching)`
  hint while the cursor re-awakens mid-typing was not re-measured
  against agy 1.4.0 — the cycle-219 probe recovered the empty-cursor
  shape right after Enter and the re-awake shape on first typing,
  both of which the cycle-219 tests already pin.
- The legend's `enter` field does not re-flip to `Search` when a
  printable key resumes the search. Cycle 219 deliberately kept the
  committed form because the legend's `enter` key still expands the
  highlighted row's detail block, which is what the user is most
  likely to do next; the cycle-psif tests assert the post-resume
  shape. If the reference re-flipping turns out to differ, that is
  a one-line follow-up.
- `Enter` on a committed Installed box re-awakens the cursor block
  (`pluginQueryAsleep = false`). The reference's behaviour on the
  second Enter was not re-measured end-to-end; the cycle-psif tests
  pin the clone's choice (always re-awaken) and a follow-up probe
  can tighten if needed.

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.3.3 | 46 | Improved search in the `/plugin` Installed tab: you can now type spaces in a search query, press `Enter` to finish searching, then use `Space` to toggle the selected plugin and `Enter` to expand its details; start typing again to resume the search. |