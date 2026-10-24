# Cycle 206 — the four list panels: alias resolution + "own every key"

## TL;DR

The rendered *block* of `/tasks`, `/artifact`, `/codesearch` and `/context` was
already byte-identical to the reference. Two behaviours around those panels were
not:

1. **Aliases.** The reference resolves a slash command's alias before it picks
   the panel, so `/cs` and `/search` open the identical `Code Search` panel. The
   clone skipped resolution in `openSlashPanel`, so `/cs` printed
   `⎿  Unknown command: /cs` into the transcript and rendered no panel at all.
2. **Key ownership.** The four panels have no rows, no search box and no editor
   target, and the reference makes **every** key inert while one is open — only
   `esc` dismisses it. The clone leaked three globals instead: `ctrl+g` opened
   `$EDITOR` over the panel, `ctrl+l` toggled alt-screen underneath it, and
   `pgup`/`pgdn`/`ctrl+home`/`ctrl+end` fell through to the transcript scroller.
   The cycle-202 "panel branch precedes the scroller" hook sat *below* the
   `ctrl+l` / `ctrl+o` / `ctrl+g` globals, so it never saw them.

Both are fixed. Everything else about these four panels was left alone, and
everything I could not observe is listed under **存疑** rather than invented.

## What was wrong

### 1. `/cs` and `/search` never reached the panel table

`internal/tuilib/commands.go` already carries the recovered alias catalogue:

```go
{Name: "/codesearch", Description: "Search code in the workspace …", Aliases: []string{"cs", "search"}},
```

`openSlashPanel(cmd)` called `panelForCommand(cmd)` with the raw token, and
`panelForCommand` is keyed only on canonical names. `/cs` therefore fell through
to `handleSlashCommand`, which has no `cs` case, and the transcript got the
default `Unknown command: /cs` line. Same for `/search`.

The reference opens the panel for both aliases — and, because the same table
also aliases `settings`→config, `quota`→usage and `undo`→rewind, it opens those
panels too (probed below).

### 2. The globals leaked through the open panel

`tui_run.go`'s `Update` handled `ctrl+l` (alt-screen toggle), `ctrl+o`
(tool-step groups) and `ctrl+g` (`$EDITOR`) before it ever looked at
`m.panel`. Cycle 202 moved the *scroller* behind the panel branch but not these
three, so with a list panel open the clone:

| key | clone (before) | reference |
|---|---|---|
| `ctrl+g` | `$EDITOR` replaces the screen | nothing |
| `ctrl+l` | alt-screen toggles, frame reflows | nothing |
| `pgup`/`pgdn`/`ctrl+home`/`ctrl+end` | transcript scrolls | nothing |
| `ctrl+o` | tool-step groups toggle | nothing |

`handlePanelKey` also returned `false` for `left`/`right` and every modified key
that was not `enter`/`tab`/`backspace`, so the panel could not be honoured by
adding cases there alone.

## What changed

| File | Δ | What |
|---|---|---|
| `cmd/wagyu/slash_list_panels.go` | **new**, 93 lines | `listPanelKind`, `canonicalPanelCommand` (catalog-driven alias resolve), `listPanelOwningKey` (Esc closes, `ctrl+c` falls through, everything else swallowed). |
| `cmd/wagyu/slash_cycle206_test.go` | **new**, 201 lines | 6 tests: alias table, alias → panel, per-key swallow for all four panels, `ctrl+c` fall-through, Esc + notice, scope guard. |
| `cmd/wagyu/slash_panels.go` | +15/−1 | `openSlashPanel` resolves the alias; `handlePanelKey` routes the four list panels to `listPanelOwningKey`; one header line. |
| `cmd/wagyu/tui_run.go` | +12 | one hook at the top of `case tea.KeyMsg` so the four list panels are consulted *before* the `ctrl+l`/`ctrl+o`/`ctrl+g` globals. |
| `docs/cycles/cycle-206/deliverable-list-panels.md` | new | this report. |

The change is confined to the panel machinery. `internal/convstore/sqlite.go`
and every unrelated file are untouched; `gofmt -l` is empty for the two new
files (and `slash_panels.go` is *unchanged* in that respect — it is on the
pre-existing 50-file unclean list, verified by `gofmt -l` on the `HEAD` blob).

## Evidence

All probes ran the reference (`OPENAGY_BACKEND=agw`, `~/.local/bin/agy`) and the
clone (`OPENAGY_BACKEND=mock`) side by side on the private tmux socket
**`agy206`** at 220×50 (and 120×40 / 100×24 for the key probes). Scripts live in
`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy206/`.

### Alias — `probe_c.py alias`, `probe_alias.py`

REF `/cs` (final frame, SGR kept):

```
\x1b[1m\x1b[39mCode Search\x1b[0m:
\x1b[38;5;241m  Usage: /codesearch [-F|--literal] <query>\x1b[39m

\x1b[1mKeyboard: \x1b[0m…\x1b[38;5;69mesc\x1b[39m \x1b[38;5;241mClose Search\x1b[39m

\x1b[38;5;241m? for shortcuts\x1b[39m
```

clone `/cs` (before the fix):

```
\x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;5;238mUnknown command: /cs\x1b[39m
```

`/search` produced the same pair. The same probe confirmed the aliases the
catalogue already declares for other *panel* commands open their panels on REF:
`/settings` → the 21-row Settings picker, `/undo` → `Rewind Conversation`,
`/quota` → the quota viewer with
`↑/↓ Scroll · pgup/pgdown Page · ctrl+end Bottom · ctrl+home Top · esc Close`.

### Key ownership — `probe_one.py` (one key per fresh session, 6 s settle)

```
--- ref /tasks [C-g] (alive=True) ---
    (no change)
--- ref /tasks [C-o] (alive=True) ---
    (no change)
--- ref /tasks [C-l] (alive=True) ---
    (no change)
--- ref None [C-c] (alive=True) ---
    (no change)
--- our /tasks [C-g] (alive=True) ---
     +~
     +~
     …
     +"/var/folders/…/agy-prompt-1145822967.md" 0L, 0B      ← $EDITOR took the screen
--- our /tasks [C-l] (alive=True) ---
     +                                        Gemini 3.8 Flash · high   ← alt-screen reflow
     @@ -22 +22,0 @@
```

`probe_keys.py` / `probe_diff.py` added `↓↑ k x p enter tab backspace left
right pgup pgdn ctrl+home ctrl+end` on `/codesearch` and `/tasks`: the reference
was inert for all of them (the only diffs were the app still settling at the
3 s mark; with a 6 s settle `probe_diff.py ref /codesearch PgUp PgUp` printed an
empty diff).

`ctrl+c` is deliberately **not** swallowed. REF does not quit on `ctrl+c` even
with no panel open, and the clone is already inert on it with a panel up, so
letting it fall through keeps the observed behaviour and cannot trap a user.
`TestCycle206ListPanelsCtrlCFallsThrough` pins that.

## Live parity

`cmp206b.py diff` — for each case: fresh session, `/cmd` + Enter, drive the
keys, `capture-pane -e`, slice the final frame from the last title row down to
the legend row, compare raw bytes (tmux has already normalised 24-bit RGB to
256-colour indices on both sides, so "raw" means the normalised form). The
reference intermittently fails to open a panel (a race), so the harness retries
up to 4 times.

| case | keys | h | result |
|---|---|---|---|
| `tasks/open` | — | 50 | IDENTICAL |
| `tasks/kx` | k x | 50 | IDENTICAL |
| `tasks/enter` | enter | 50 | IDENTICAL |
| `tasks/udlr` | ↑ ↓ ← → | 50 | IDENTICAL |
| `tasks/pgup` | pgup pgdn | 50 | IDENTICAL |
| `tasks/cg` | ctrl+g | 50 | IDENTICAL |
| `tasks/cl` | ctrl+l | 50 | IDENTICAL |
| `artifact/open` | — | 50 | IDENTICAL |
| `artifact/p` | p | 50 | IDENTICAL |
| `artifact/enter` | enter | 50 | IDENTICAL |
| `artifact/ud` | ↑ ↓ | 50 | IDENTICAL |
| `artifact/cg` | ctrl+g | 50 | IDENTICAL |
| `codesearch/open` | — | 50 | IDENTICAL |
| `codesearch/type` | h e l l o | 50 | IDENTICAL |
| `codesearch/enter` | enter | 50 | IDENTICAL |
| `codesearch/ud` | ↑ ↓ | 50 | IDENTICAL |
| `codesearch/cg` | ctrl+g ctrl+l | 50 | IDENTICAL |
| `cs/opens` | — | 50 | IDENTICAL |
| `search/opens` | — | 50 | IDENTICAL |
| `context/open` | — | 50 | IDENTICAL |
| `context/keys` | ↑ ↓ pgup pgdn | 50 | IDENTICAL |
| `context/pg` | pgup pgdn | 50 | IDENTICAL |
| `context/open24` | — | 24 | IDENTICAL |
| `context/keys24` | ↑ ↓ pgup pgdn ctrl+home ctrl+end | 24 | IDENTICAL |

**Result: 24/24 cases identical modulo one reference-side race** (`cmp206.run3.log`:
22/24 in the sweep, the two `/context` misses being the transient described
below; the targeted rerun of all four `/context` cases was 4/4 identical twice
in a row). `tasks/cg`, `tasks/cl`, `artifact/cg` and `codesearch/cg` were
4-line (or `<<NO TITLE>>`) mismatches before the `Update` hook and are identical
after — see `cmp206.run2.log` for the pre-hook failures.

`context/*` intermittently came back different because REF renders a transient
pre-load `/context` (`· 0/Unknown tokens`, a 6-row grid, `Free space: 0
(0.0%)`, and once `Gemini 3.8 Flash (Medium)` in the token row) before settling
to the 7-row form the clone renders (`Gemini 3.8 Flash (High) · 0/1.0M tokens`,
`Free space: 1.0M (100.0%)`). Two consecutive reruns of all four `/context`
cases were byte-identical, so this is a reference race, not a clone bug. It is
the same transient cycle 190 already recorded.

## Tests

New: `cmd/wagyu/slash_cycle206_test.go`

| test | asserts |
|---|---|
| `TestCycle206CanonicalPanelCommand` | the alias table incl. `cs`/`search`, `settings`/`quota`/`undo`, canonical and non-alias pass-through |
| `TestCycle206OpenSlashPanelViaAlias` | `openSlashPanel` opens the right `panelKind` for each alias and keeps the typed name in `panel.cmd` for the Esc notice; `clear` still falls through |
| `TestCycle206ListPanelsOwnEveryKey` | 17 keys × 4 panels swallowed, panel stays open, transcript unchanged |
| `TestCycle206ListPanelsCtrlCFallsThrough` | `ctrl+c` returns `handled=false` |
| `TestCycle206ListPanelsEscCloses` | Esc closes; `/tasks` `/artifact` echo `Exited /cmd command`, `/codesearch` `/context` are silent |
| `TestCycle206ScopedToTheFourListPanels` | `/mcp` and `/permissions` are *not* in the list-panel set and keep the old fall-through |

Existing layout tests (`TestPanelTasksLayout`, `TestPanelArtifactLayout`,
`TestPanelCodeSearchLayout`, `TestPanelContextEmptyLayout`,
`TestOpenSlashPanel`, …) still pass unchanged — the fix does not touch the
rendered rows.

Gates:

* `go build -o <PROBEDIR>/our/agy ./cmd/wagyu` — exit 0.
* `go test -vet=off ./cmd/... ./internal/...` — all packages **ok** *after*
  normalising the machine's shared settings (see below); the two `/config`
  tests fail otherwise.
* `gofmt -l cmd/wagyu/slash_list_panels.go cmd/wagyu/slash_cycle206_test.go` —
  empty.

**Test-environment caveat.** `TestPanelConfigLayout` and
`TestCycle197_ConfigVisibleRows` read the *real*
`~/.gemini/antigravity-cli/settings.json` through `auth.SettingsField`. That
file currently has `"editorMode": "vim"`, which makes `configRows()` return the
21-row catalogue instead of the 20 the tests expect. Both tests fail
identically on `HEAD` with cycle 206 stashed (`git stash push -u` → same two
FAILs), so this is pre-existing machine state, not a cycle-206 regression. The
full-suite run above was taken with `editorMode` temporarily set to `default`;
the file was restored byte-for-byte afterwards.

## 存疑 (observed divergence knowingly left, and things not verified)

* **`/codesearch <args>` runs a search on REF; the clone shows the empty
  usage panel.** `probe_c.py args` drove REF's `/codesearch hello` from `/tmp`
  and got the *result list* in the same panel: `Found 1000 results`, an
  underlined-blue file row (`test_pad.txt`), `  L95:` / `> L96:` context rows
  highlighted in blue, a `────────` separator between hits, `  [1-7 of 1000
  items]`, then the unchanged `Keyboard: …` row and `? for shortcuts`. The clone
  opens the usage panel instead, because `openSlashPanel` receives the command
  name only (`splitSlash` splits the args off) and `panelCodeSearch` has no
  result-list renderer. Reproducing it byte-for-byte needs the whole
  result-list UI (per-file grouping, ±1 context window, match highlighting,
  result paging, ellipsis truncation) against the live workspace, and its
  content is workspace-dependent — it is not the empty-state list block this
  cycle owns. Left as-is, recorded here.
* **`Exited …` notice text for aliased panel commands.** `/cs` and `/search`
  are silent (code search's Esc path), so nothing changed there. But the alias
  fix also makes `/settings`, `/quota` and `/undo` open their panels, and
  `closePanel` echoes `Exited /settings command` (the *typed* alias) for the
  config/usage panels. Whether REF quotes the alias or the canonical name on
  Esc was not probed; `rewind` is silent either way.
* **Page-scroll keys against a non-empty transcript.** REF showed no change for
  `pgup`/`pgdn`/`ctrl+home`/`ctrl+end` with a list panel open at 100×24 and
  220×50, but the session's transcript fit the screen, so "ignored" and
  "already at the top" are indistinguishable. The clone now swallows them; if
  REF actually scrolls a long transcript under the panel, that case is still
  wrong.
* **The same `ctrl+g`/`ctrl+l` leak in the other row-less panels.** `/mcp`,
  `/permissions`, `/agents`, `/hooks`, `/rewind` and `/credits` have the same
  fall-through in the clone; they are outside this cycle's four-panel scope and
  were left untouched (`TestCycle206ScopedToTheFourListPanels` pins the scope).
* **`/config` `p`-row and `/context` token counts on a non-empty session.**
  `panelRowCount` still returns 0 for the four kinds (the new router bypasses
  it); any future non-empty list behaviour (populated `/tasks`, `/artifact`
  rows, `/context` real token accounting) is unimplemented and unverified.
* **Prompt-row and status-row divergence visible in every capture** (out of
  scope): REF renders `> Auto mode: edits & safe commands auto-approved
  (shift+tab to cycle)` in the prompt row and `auto · Gemini 3.8 Flash · high`
  in the status row, the clone a bare `>` and `Gemini 3.8 Flash · high`. Owned
  by the permission-banner cycles, not this one.
* **`k` / `x` / `p` / `enter` on empty lists.** Left completely inert, as the
  hard rule requires: the reference shows no observable response on this
  machine, so nothing was added.
