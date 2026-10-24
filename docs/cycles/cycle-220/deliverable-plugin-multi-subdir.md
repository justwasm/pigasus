# Cycle 220 — /plugin Discover tab multi-subdirectory Tab completion

## TL;DR

Cycle 212's 2nd 存疑 ("the reference cycles a list of subdirectory
matches rather than expanding to a longest-common prefix") is **retracted**:
the cycle-220 probe (220×50, `~/.local/bin/agy` 1.4.0) shows the
reference does **not** cycle. The first Tab expands to the
alphabetically-first matching subdirectory and appends a trailing `/`;
every Tab after that is a no-op.

| What cycle 212 hypothesised | What the cycle-220 probe measured |
|---|---|
| Tab cycles a list of subdirectories | Tab expands once to `…/ugo/` and stops (four `u*` dirs probed) |
| Last candidate wraps back to the LCP | the LCP `u` never re-appears after the initial expansion |
| A `[N/M]` affordance is painted | the prompt shows no such span |

The clone is therefore corrected from "LCP expansion" to "first-match
expansion" so it matches what is on screen. Five new unit tests pin the
behaviour; the existing `TestCycle212_PromptKeyLifecycle` is updated to
expect the trailing slash.

## Probe vs clone (the gap, by surface)

Every row below was measured against the live reference (`~/.local/bin/agy`,
build 1.4.0) by reading the on-screen cells row-by-row. Probe transcripts
live at `/private/var/folders/.../opencode/agy220/`:

- `probe_multi_tab.py` — primary 8-Tab stress test with four `u*` subdirs
- `probe_lcp.py` — LCP test with three `un_al*` subdirs (5-char prefix)
- `probe_complete.py` — typed an existing complete directory, no trailing `/`
- `probe_single_match.py` — one match (`onlyone`) under the parent
- `probe_existing_dir.py` — typed an unresolvable path
- `probe_keys.py` — Right / Down / Up / Shift+Tab / Ctrl+D / Ctrl+F / Ctrl+B
  tested; none of them cycle
- `probe_diag.py` — three theories (short LCP, mixed files+dirs, full
  first match) all consistent with "no cycling"
- `probe_rapid.py` — 4 Tab presses sent back-to-back, captured every
  100 ms for 2 s; the prompt never deviates from the alphabetically-first match
- `probe_hint.py` — the hint row ("press Tab to complete subdirs") stays
  the same; no `[N/M]` affordance

| Surface | Probe (`agy`) | Clone before (cycle 212) | Clone after (cycle 220) |
|---|---|---|---|
| Multi-match: type `/tmp/agy220dirs/u`, Tab | resolves to `/tmp/agy220dirs/ugo/` (probe p1.out row 12) | LCP expansion → `/tmp/agy220dirs/u` unchanged (no unique path) | first-match + `/` → `/tmp/agy220dirs/ugo/` |
| Multi-match: Tab #2 | same `…/ugo/` (probe p1.out row 12) | no-op (LCP unchanged) | no-op |
| Multi-match: Tab #3 | same `…/ugo/` (probe p1.out row 12) | no-op | no-op |
| Single-match: type `/tmp/agy220single/only`, Tab | resolves to `/tmp/agy220single/onlyone/` | full match → `…/onlyone` (no `/`) | first-match + `/` → `…/onlyone/` |
| Existing directory: type `/tmp/agy220dirs/ugo`, Tab | resolves to `…/ugo/` | unchanged (cycle 212 short-circuits on directories) | q + `/` → `…/ugo/` |
| LCP test: type `/tmp/agy220lcp/un_al`, Tab | resolves to `/tmp/agy220lcp/un_alias/` (probe p_lcp.out row 7) | LCP expansion → `…/un_al` | first-match + `/` → `…/un_alias/` |
| Unresolvable: Tab on `/no/such/path/atall-xyz` | unchanged (probe p_existing_dir.out) | unchanged | unchanged |
| Trailing slash: Tab on `…/ugo/` | unchanged (probe) | unchanged | unchanged (early return — avoids `ugo//`) |
| `[N/M]` affordance after the path | none (probe p_hint.out) | none | none |
| Tab after the path is fully resolved | no-op, same `/tmp/agy220dirs/ugo/` (probe p_rapid.out × 4) | no-op (LCP unchanged) | no-op |

The hint line still toggles: "Directory not found (press Tab to
complete)" stays for an unresolvable prefix, "Directory does not
contain plugin.json (press Tab to complete subdirs)" appears once the
path resolves to a directory that lacks `plugin.json`, and disappears
once the directory holds a valid one (cycle 212's three strings are
unchanged).

### Files

| File | Δ |
|---|---|
| `cmd/wagyu/slash_plugin_local_install.go` | `completeLocalInstallPath` rewritten: LCP → first-alphabetical match, trailing `/`, early return when the path already has one. `commonPrefix` removed (no other callers). |
| `cmd/wagyu/slash_cycle220_test.go` | new — 5 tests, all green. |
| `cmd/wagyu/slash_cycle212_test.go` | `TestCycle212_PromptKeyLifecycle` updated: the unique-prefix Tab completion now expects the trailing `/` (matches reference + new `completeLocalInstallPath`). |
| `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md` | new — this file. |

## Probe transcript — verbatim row-by-row

Captured at 220×50 against `~/.local/bin/agy` 1.4.0. `/tmp/agy220dirs/`
contains `ugo`, `umbra`, `unicorn`, `unknown`. Each `show()` call below
prints only the third panel row (the prompt row) — the rest of the
Discover-tab listing is unchanged.

```
===== after typing '/tmp/agy220dirs/u' =====
  > + Install from local directory: /tmp/agy220dirs/u 
    └─ ✗ Directory not found (press Tab to complete)

===== after Tab #1 =====
  > + Install from local directory: /tmp/agy220dirs/ugo/
    └─ ✗ Directory does not contain plugin.json (press Tab to complete subdirs)

===== after Tab #2 (identical) =====
  > + Install from local directory: /tmp/agy220dirs/ugo/
    └─ ✗ Directory does not contain plugin.json (press Tab to complete subdirs)

===== after Tab #3 (identical) =====
  > + Install from local directory: /tmp/agy220dirs/ugo/
    └─ ✗ Directory does not contain plugin.json (press Tab to complete subdirs)

===== after Tab #4 (identical) =====
  > + Install from local directory: /tmp/agy220dirs/ugo/
    └─ ✗ Directory does not contain plugin.json (press Tab to complete subdirs)
```

The LCP test (probe `probe_lcp.py`) — typing `/tmp/agy220lcp/un_al`
where matches are `un_alias`, `un_aliases`, `un_align`:

```
===== typed '/tmp/agy220lcp/un_al' =====
  > + Install from local directory: /tmp/agy220lcp/un_al 

===== after Tab #1 =====
  > + Install from local directory: /tmp/agy220lcp/un_alias/ 
```

If the reference did LCP it would land on `un_al/` (5 chars, identical
to the typed prefix); if it cycled, Tab #2 would shift to `un_ali` (4
chars); if it cycled-through-siblings it would land on `un_ali`-then
`un_align` etc. The probe shows the reference picks `un_alias/`
unambiguously: **first alphabetical match, with `/`**.

The "typed a complete directory" test (probe `probe_complete.py`):

```
===== PRE-TAB: typed full dir without trailing / =====
  > + Install from local directory: /tmp/agy220dirs/ugo 

===== Tab #1: complete dir → expect / appended =====
  > + Install from local directory: /tmp/agy220dirs/ugo/

===== Tab #2: should stay =====
  > + Install from local directory: /tmp/agy220dirs/ugo/
```

A second `/` would be incorrect (`ugu//`), and the probe confirms the
reference does not produce one.

## Why the cycle-220 implementation differs from cycle 212

### The cycle-212 hypothesis

> The reference cycles a longer list of subdirectories; this is the
> single deliberately-simpler piece.

Cycle 212 chose LCP expansion (the longest common prefix of all
matching siblings) as a friendlier completion when several entries
share a prefix. That hypothesis was untested at the time because the
cycle-212 probe never measured the prompt across more than one Tab.

### What the probe actually measures

`p1.out` (multi-match, 8 Tabs), `p_lcp.out` (LCP, 5 Tabs),
`p_complete.out` (typed complete dir), `p_single.out` (one match),
`p_existing_dir.out` (no matches), and `p_rapid.out` (4-Tab stress +
100 ms cadence capture) all show the same shape:

- **One Tab → one resolution**, to the alphabetically-first match.
- **Subsequent Tabs → no change.**
- **No cycling, no LCP, no `[N/M]` affordance.**

The cycle-220 probe also rules out sibling-target cycling via `Right`,
`Down`, `Up`, `Shift+Tab`, `Ctrl+D`, `Ctrl+F`, `Ctrl+B` — all of them
leave the prompt row untouched (`probe_keys.py`).

### Why the trailing slash matters

A trailing `/` is the reference's only "I'm in cycling state" signal,
and the only one it does paint. The hint row switches from "press Tab
to complete" (the typed path is not yet a directory) to "press Tab to
complete subdirs" (the typed path resolves to a directory that lacks
`plugin.json`) at exactly the moment the slash is appended. The cycle
220 unit tests assert that the *next* Tab after the slash appears is
a no-op, matching the cycle-220 probe (`p_rapid.out`).

### Why the early-return is in the same function, not in the handler

The Tab key owns a single keypress while the prompt is open (the
handler always returns `true`). Removing the `q + "/"` append for an
already-trailing-slashed path avoids creating `ugu//` without needing a
separate handler branch. The handler stays a one-liner — `m.panel.
pluginLocalInstall.query = completeLocalInstallPath(...)` — and the
unit test at `TestCycle220_PromptKeyLifecycleOverTabKeys` checks the
three-Tab no-op shape end-to-end through the public key handler.

## Before / after — one focused-row screenshot

**Before (cycle 212), typed `/tmp/agy220dirs/u` + Tab:**

```
> + Install from local directory: /tmp/agy220dirs/u
  └─ ✗ Directory not found (press Tab to complete)
```

(Tab leaves the path unchanged because the LCP `u` does not extend
beyond what is typed; the "Directory not found" hint stays. The hint
switch only happens once the typed path resolves to a directory.)

**After (cycle 220), typed `/tmp/agy220dirs/u` + Tab:**

```
> + Install from local directory: /tmp/agy220dirs/ugo/
  └─ ✗ Directory does not contain plugin.json (press Tab to complete subdirs)
```

(Tab resolves to the first alphabetical match, `ugo/`, and the hint
flips to "press Tab to complete subdirs". Another Tab leaves the path
unchanged — the prompt does not cycle.)

**After (cycle 220), typed `/tmp/agy220lcp/un_al` + Tab** (LCP test):

```
> + Install from local directory: /tmp/agy220lcp/un_alias/
```

(The reference does not stop at the LCP `un_al/` — it picks the first
alphabetical match with a trailing `/`.)

## Design notes

### What stayed the same as cycle 212

- The hint strings (`Directory not found` / `Directory does not
  contain plugin.json` / `""`) — three discrete shapes, all probed in
  cycle 212 and unchanged.
- The `Enter` and `Ctrl+S` submit paths — cycle 212's `installLocal
  Plugin` / `copyDir` / `auth.SetPluginEnabled` plumbing is
  byte-identical.
- The blink cadence, the placeholder, the row label, the legend, the
  Esc / Backspace keys — all unchanged.
- `TestCycle212_PromptKeyLifecycle` only had its unique-prefix Tab
  expectation updated; every other assertion is preserved.

### Why a single function (`completeLocalInstallPath`)

The handler is one line. Splitting the LCP-vs-first-match decision into
its own helper would let cycle 212's `commonPrefix` survive as a
documentation-only artifact, but every sibling of `completeLocalInstall
Path` in the package already imports nothing and the handler always
returns handled. Inlining the new logic keeps the cycle-220 diff
scoped to one function and removes the now-unused `commonPrefix`.

### Why no `[N/M]` affordance

`probe_hint.py` confirms the reference paints nothing after the typed
path — the prompt row's "cursor" position is a single trailing space,
and the trailing `/` itself is the only state signal. Implementing a
`[N/M]` would be a divergence, not a parity. Cycle 218's reverse-video
block on the empty-placeholder already shows where the cursor ends up;
that span is unchanged.

## Verified

```
$ /Users/gear/go/bin/go test -vet=off -count=1 -run 'Cycle212|Cycle220' ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.854s
```

`TestCycle212_PromptKeyLifecycle` (untouched other than the trailing-
slash expectation), `TestCycle212_PromptRowRendering`,
`TestCycle212_PromptValidationHints`, `TestCycle212_PromptLegend`,
`TestCycle212_EnterOpensPromptOnLocalRow`,
`TestCycle212_AcceptCopiesAndEnablesPlugin`,
`TestCycle212_AcceptConflictLeavesPromptOpen`,
`TestCycle212_SearchBoxBlink`, `TestCycle212_SearchBoxBlinkMessage`,
`TestCycle212_TabKeepsQueryAndFocus`,
`TestCycle212_LeftRightWhileFocusedJumpsFirstLast` — all green.

Cycle 220 tests (5, all green):
- `TestCycle220_MultiSubdirTabFirstMatch` — typing `/u` over
  `[ugo, umbra, unicorn, unknown]` expands to `…/ugo/` and stays
  there across five further Tabs.
- `TestCycle220_LongerPrefixedLeaf` — typing `/un_al` over
  `[un_alias, un_aliases, un_align]` expands to `…/un_alias/`, NOT
  `…/un_al` (LCP).
- `TestCycle220_SingleMatchAlsoAppendsSlash` — typing `/only` over
  `[onlyone]` expands to `…/onlyone/`.
- `TestCycle220_ExistingDirectoryGetsTrailingSlash` — typing
  `/tmp/.../leaf` where `leaf` exists as a directory returns
  `…/leaf/`.
- `TestCycle220_TabNoOpOnUnresolvablePath` — typing
  `/no/such/path/atall-xyz` returns it unchanged.
- `TestCycle220_PromptKeyLifecycleOverTabKeys` — drives Tab through
  `handlePanelKey` four times on a three-candidate parent and
  asserts the query holds `alpha_other/` for every Tab.

Build & format:

```
$ /Users/gear/go/bin/go build -o /tmp/wagyu220-final ./cmd/wagyu
-rwxr-xr-x 1 gear staff 72764338 Oct 17 23:06 /tmp/wagyu220-final

$ gofmt -l cmd/wagyu/slash_plugin_local_install.go cmd/wagyu/slash_cycle220_test.go
(empty)
```

The two pre-existing `TestDispatchPrintSlash_LocalCommands` and
`TestSlashContent_ModelAndEffort` failures in `./cmd/wagyu/` are
unrelated to this cycle — they expect `gemini-3.8-flash-high` but the
configured model selector resolves to `claude-sonnet-4-6` on this
machine; they fail identically at `5ff92af` before the cycle-220 diff
(`git stash` round-trip verifies it). Cycle 220's Gate is "no new
failures" and that holds.

## 硬约束 / 存疑 — not verified

- **Cycling closure retraction.** Cycle 212 documented "the reference
  cycles a list of subdirectories" as a 2nd 存疑. The cycle-220 probe
  (`probe_multi_tab.py` × 8 Tabs, `probe_lcp.py`, `probe_diag.py`,
  `probe_keys.py`, `probe_rapid.py`) shows the reference does **not**
  cycle. The 存疑 is therefore retracted, not closed-with-cycling. A
  follow-up cycle that runs against a newer `agy` build (≥ 1.5.0) is
  not in scope here; should one be set up later, `completeLocalInstall
  Path` needs to learn the cycling shape (e.g. cycle index state in
  `pluginLocalInstall`, the `[N/M]` affordance the cycle-220 probe
  shows is *absent*) and the probe script `probe_rapid.py` already
  records the cadence that any future cycle can re-run.

- **Probe vs. real terminal.** The cycle-220 probe drives a tmux pane
  at 220×50 with `tmux -L agy220` and `tmux capture-pane -p -e`. This
  is the same harness used by cycles 198–219; cycle 1's FINDINGS note
  is the only place we know the harness is sign-in-bound at the head of
  the cycle. The reference login dialog is bypassed by the OAuth
  token already on disk at `~/.config/antigravity/oauth-token`
  (libredot@gmail.com). A different login state on a clean machine
  will need to drive the same trust-folder dialog (Enter) and
  `/plugin Enter Tab Enter` sequence that `probe_multi_tab.py` opens
  with.

- **Behaviour against `plugin.json`-less subdirs.** The cycle-220
  units create subdirs with `os.Mkdir` only — they don't seed
  `plugin.json`. The hint strings ("press Tab to complete" vs.
  "press Tab to complete subdirs") are asserted in cycle 212's
  `TestCycle212_PromptValidationHints` (still green). The cycle-220
  completion function is independent of the hint row, so the probe
  finding carries through to the rendered hint without further
  changes.

- **Backspace-off-trailing-slash behaviour.** Cycle 212 measures
  Backspace as "drops the last rune of `query`", which on a path that
  ends in `/` simply deletes the `/`. The cycle-220 function then sees
  `…/ugo` and the matching-sibling branch re-fires (returning
  `…/ugo/` again). The probe `probe_multi_tab.py` captures a single
  such transition (PRE-TAB `…/ugo` → Tab → `…/ugo/`); the unit test
  `TestCycle220_PromptKeyLifecycleOverTabKeys` exercises the same
  four-Tab shape through the public key handler.
