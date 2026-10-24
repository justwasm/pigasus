# Cycle 207 — viewer panels: live re-diff against `agy` 1.4.0

Subagent workstream: **viewer panels** (`/usage`, `/changelog`, `/diff`, `/rewind`,
`/credits`) in `cmd/wagyu`, re-diffed cell-by-cell against the closed-source
reference `~/.local/bin/agy` (Antigravity CLI 1.4.0) at 220×50 and a short height.

Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w207`
Branch: `cycle207-viewers`.

---

## TL;DR

Five panels were re-measured live. Four real structural divergences were fixed and
one non-structural one was added (the `/diff` git All-Changes body); two panels
(`/rewind`, `/credits`) were confirmed **byte-identical** and only got regression
tests.

The headline bug was an **off-by-one row**: every overflowing view emitted one row
more than the terminal, and the terminal's scroll dropped the real top row — at
h=24 the `/changelog` viewer lost its title. Root cause: `fitLines` padded the
phantom trailing empty line (from the body's trailing `\n`) into a real
space-filled row. Fixed in two independent places for belt and braces.

Two secondary fixes: `/changelog` re-created its title/heading layout (2-space
indent + `48;5;63` background, an added blank row) and the cycle-200 markdown
refresh had **duplicated the whole 1.3.6 section**; `/diff`'s title margin and
working-copy row text were corrected and the live git file list was wired in.

Remaining divergence is dominated by one thing the clone deliberately does not do:
**REF wraps long changelog lines across rows, the clone truncates them** — declared
存疑, not attempted (see below).

---

## What was wrong (and why)

### 1. `fitLines` padded a phantom row → the top row scrolled away  【一手】

`viewBody` always ends the rendered body with `"\n"`. `strings.Split(body,"\n")`
therefore yields one final **empty** entry that is *not* a row. `fitLines`
(`tui_viewport.go`) ran `fitWidth` over every entry, so that empty entry became a
space-filled, SGR-padded row; the body was then one row taller than the terminal
and `View()`'s top-drop branch scrolled the real first row off. Concretely, at
h=24 the `/changelog` viewer's title (REF row 1) was pushed off-screen in the clone.

Witness — `compare.py` before the fix reported the clone's panel starting one row
below REF's; after the fix `changelog24`/`changelog26`/`changelog33`/`changelog50`
all slice to the same window as REF (`compare` now finds the title at index 0 in
both). Evidence frames: `changelog24.ref.raw.txt` vs `changelog24.our.raw.txt`.

Fix, two places:
- `tui_viewport.go` `fitLines`: `if i == last && line == "" { continue }`.
- `tui_run.go` `View()` drop branch: trim a trailing `""` from `lines` before
  slicing, so a body that already carries the phantom entry cannot re-introduce it.

### 2. `/changelog` title/heading layout  【一手】

REF 1.4.0 (settled) shows the title as `  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Antigravity CLI Changelog \x1b[0m`
— a **2-space left indent**, yellow-on-blue (`48;5;63`) — followed by a **blank
row**, then the tagline, then the 1.3.6 block. The clone had no indent, no
background and no blank. Fixed in `changelogExcerptRows()`.

The added blank shifted the window's opening row, so the counter is now
`(1–18 of 1238 lines)` at the unit-test geometry (was `(1–17 …)`);
`slash_panels_test.go` was updated for that one string.

### 3. `/changelog` duplicated the 1.3.6 section  【一手】

`changelogBody` already ships the transcribed 1.3.6 block. The cycle-200 markdown
refresh re-added a `## 1.3.6` section to `printSlashChangelog`
(`testdata/print_changelog.md` line 7), and `buildChangelogRestRows` started
emitting at the first `## ` header — so the whole 1.3.6 section appeared **twice**
in the panel. Fix: skip the first `## ` header; the rest-rows stream now starts at
`## 1.3.5`.

### 4. `/changelog` window height is `height-4`, not `height-3`  【一手】

Settled re-measurement at four heights:

| size | REF counter | window rows | ⇒ `height-4` |
|---|---|---|---|
| h=24 | `(1–20 …)` | 20 | ✔ |
| h=26 | `(1–22 …)` | 22 | ✔ |
| h=33 | `(1–29 …)` | 29 | ✔ |
| h=50 | `(1–46 …)` | 46 | ✔ |

The earlier `height-3` reading was taken **before the panel settled** (the probe
captured a transient frame one window row taller) and is retracted here. The
changelog panel is full-screen: title always at row 1, counter at h−3, blank
h−2, legend h−1, blank last row h — and it draws **no status row**.

### 5. `/usage` window geometry is `height-13`  【一手】

REF appends the usage panel after 13 rows of chrome; with a real terminal the
panel's window is `height-13` document rows (h=24→11, h=28→15, h=33→20), the
counter counts the scrollable document rows (title + 3 chrome rows excluded), and
the status row **is** drawn. The clone's height-aware path was added as
`panelUsageLinesREF()` / `usageViewerSpecREF()`; the cycle-193 renderer (and its
`usageViewerLines()` string `...models in the group.`) is untouched and remains the
fallback while `m.height == 0` (the unit tests).

### 6. `/diff` title margin and working-copy row  【一手】

- Title margin: **dirty** All Changes is flush-left; **clean** All Changes and the
  Per Turn / Commit Tree tabs are indented one space. Implemented via
  `diffTabRow(vcs, active, leading)`.
  Witness: `diff50` (dirty) title `\x1b[1mDiff (git)…`; `diffclean2` (clean) title
  ` \x1b[1mDiff (git)…`.
- Commit-Tree working-copy row plain text is `" ▌ *  Working Copy (Uncommitted Changes)"`
  (a space between `▌` and `*`). Witness: `diffE.ref.raw.txt`.
- A git workspace (`m.cwd` detected as git) now renders the real working-copy file
  list (`panelDiffLinesVCS`), with the ↑/↓/j/k row cursor. The no-VCS warning
  remains the fallback (and is what the unit tests exercise, since `mkPanel`
  leaves `m.cwd` empty).

---

## Files changed

| File | Lines | Why |
|---|---|---|
| `cmd/wagyu/slash_viewer_panels.go` | **new, 530** | Cycle-207 renderers: `usageDoc`, `usageBar`, `usagePage`, `panelUsageLinesREF`, `usageViewerSpecREF`, `gitDiffChanges`, `diffAllChangesLines`, `diffTabRow`, `diffCommitTreeLines`, `diffMoveCursor`, `diffTextKey`, `wrapWords`, `usageNoteText`, `usageNoteWrap`. |
| `cmd/wagyu/slash_cycle207_test.go` | **new, 444** | 13 tests (see below). |
| `cmd/wagyu/slash_panels.go` | +87 / −21 | `viewerSpecFor`: changelog `vis=height-4`, usage REF spec; `panelUsageLines` dispatch; `panelChangelogLines` counter placement; `changelogExcerptRows` title/blank; `buildChangelogRestRows` skip-first-header; `panelDiffLines` git dispatch + plain inactive tabs; `handleDiffPanelKey` cursor keys; doc comments. |
| `cmd/wagyu/tui_run.go` | +15 / −1 | `View()` drop-branch trailing-`""` trim; `viewBody` suppress the status row for a height-aware `/changelog` panel. |
| `cmd/wagyu/tui_viewport.go` | +8 | `fitLines` skip the phantom trailing empty entry. |
| `cmd/wagyu/slash_panels_test.go` | +5 / −5 | One counter string `1–17`→`1–18`, comments. |

### Why `tui_run.go` and `tui_viewport.go` changed

Both are on the **shared** render path, so the off-by-one could equally be fixed
in either; both were touched because the two are complementary and neither alone
is a proof against re-introduction:

- `fitLines` is the *cause*: it materialises the phantom row. Skipping it there
  means no view ever gains the extra row.
- `View()`'s drop branch is the *last line of defence*: it strips a trailing `""`
  before the top-drop slice, so any *future* body that ends in `\n` (or a caller
  that bypasses `fitLines`) still cannot scroll its real top row away.

`/changelog` additionally needed the status row suppressed (`tui_run.go`
`viewBody`), because REF's changelog panel is full-screen and ends on its own
trailing blank rather than a status row. All three edits are guarded so the
cycle-201/202 legacy paths (`m.height == 0`) and their byte-exact tests are
unchanged.

---

## Evidence

Probe harness (`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy207/`):
`probe.py` drives REF (`OPENAGY_BACKEND=agw`) and the clone (`mock`) in tmux
socket `-L agy207`, at a given size, sends the slash keys, and
`capture-pane -e`'s the result. `PROBE_SETTLE=4.5` is required to capture REF's
**steady state** — frames taken immediately after opening are one window row
taller. `slice.py` isolates the panel; `compare.py` normalises the clone's two
truecolour constants to the 256-colour index tmux renders them as
(`cBlueOpen 38;2;66;133;244`→`38;5;69`, `cDimGrayOpen 38;2;95;99;104`→`38;5;241`)
and diffs line-by-line.

Raw frames kept per case: `{name}.ref.raw.txt`, `{name}.our.raw.txt`.

Fixtures:
- `repo` — modified `tracked.txt`, untracked `scratch.txt` (dirty git workspace).
- `repoclean` — clean git workspace.
- `repo3` — deleted `del.txt`, modified `mod.txt`, untracked `sub/deep.txt` and
  `untracked.txt`.

---

## Live parity (final run, 220×50 + 220×24)

| Panel | Case | Geometry | Mismatched lines | Nature of mismatch |
|---|---|---|---|---|
| `/rewind` | `rewind50` | 220×50 | **0 / 5** | byte-identical |
| `/credits` | `credits50` | 220×50 | **0 / 11** | byte-identical |
| `/diff` | `diff50` (dirty, All Changes) | 220×50 | 1 / 8 | line 0 SGR pen only (§存疑) |
| `/diff` | `diffclean2` (clean) | 220×50 | 1 / 5 | line 0 SGR pen only |
| `/diff` | `diff3` (repo3) | 220×50 | 1 / 10 | line 0 SGR pen only |
| `/usage` | `usage50` | 220×50 | 3 / 26 | **values only** (bar %, refresh clock) |
| `/usage` | `usage24` | 220×24 | 2 / 18 | **values only** |
| `/changelog` | `changelog24` | 220×24 | 16 / 23 | long-line wrapping (§存疑) |
| `/changelog` | `changelog50` | 220×50 | 39 / 49 | long-line wrapping (§存疑) |

`/usage` value rows (structure, colours, bar width, brackets and `%` all match):

```
L8 ref: '    [\x1b[32m██████████████████████████████████\x1b[38;5;241m░░░░░░░░░░░░░░░░\x1b[39m]\x1b[38;5;241m 67.71%\x1b[39m'
   our: '    [\x1b[32m██████████████████████████████████████████████████\x1b[39m]\x1b[38;5;241m 100.00%\x1b[39m'
L9 ref: '    \x1b[32mRefreshes in 156h 29m\x1b[39m'
   our: '    \x1b[32mRefreshes in 158h 18m\x1b[39m'
```

REF's bar percentage and refresh countdown are **live**; the clone renders its
frozen mock (100.00% / 0.00%) with the refresh clock frozen to a captured value.
The countdown also drifts between REF captures (`156h 29m` vs `156h 34m` in the
two usage cases), i.e. it is a clock, not a layout constant.

The `/diff` line-0 residual is exactly one SGR token: the clone emits
`\x1b[1m\x1b[39mDiff (git)…` where REF emits `\x1b[1mDiff (git)…`. REF's title
row inherits the separator's foreground (`38;5;253`) through the terminal pen;
the clone's separator padding resets to default first, so tmux emits a redundant
`\x1b[39m` when it captures the title cell. The **cells are identical** (same
glyphs, bold); only the capture's pen-state bookkeeping differs. Left as-is —
see 存疑.

### Re-measured and already correct (no code change)

This is the coverage record: each of these was re-probed live this cycle and found
to already match REF, so nothing was changed for them.

| Surface | What was checked | Result |
|---|---|---|
| `/rewind` | full panel, 220×50 | byte-identical, 0/5 |
| `/credits` | full panel, 220×50 | byte-identical, 0/11 |
| `/usage` | 13-row chrome, title row, panel order, counter placement, status row, bar glyph/colour/brackets, note indentation | structure identical; only the live *values* differ |
| `/usage` | page-scroll keys (`pgup`/`pgdn`/`ctrl+u`/`ctrl+d`) | window moves, counter follows, offset-0 rows unchanged |
| `/diff` | inactive-tab colour | REF shows **no SGR** on inactive tabs in both the 1.3.x and 1.4.0 captures; clone's `cDimGray` was replaced with plain (this *is* a change, listed here because it was a re-measurement) |
| `/diff` | `esc` dismiss, `←/→` tab switch | unchanged from cycle 201; still correct |
| `/changelog` | counter text, `1238` denominator, legend row | identical |
| `/changelog` | no status row drawn | confirmed; the cycle-207 suppression matches REF |

---

## Tests

`cmd/wagyu/slash_cycle207_test.go` — 13 tests, all passing:

| Test | Covers |
|---|---|
| `TestCycle207UsageDocGeometry` | 20-row document, blank rows, note row |
| `TestCycle207UsageBar` | bar width/colour from a percentage |
| `TestCycle207UsagePage` | `height-13` page sizing |
| `TestCycle207UsageViewerHeight` | REF spec selection at a real height |
| `TestCycle207WrapWords` | word wrap helper (bounded) |
| `TestCycle207DiffAllChanges` | git file-list rows |
| `TestCycle207DiffEmptyStates` | no-VCS fallback |
| `TestCycle207DiffMoveCursor` | ↑/↓/j/k cursor movement + clamping |
| `TestCycle207DiffGitChanges` | `gitDiffChanges` parse |
| `TestCycle207DiffCommitTree` | working-copy row `" ▌ *  …"` |
| `TestCycle207ChangelogTitle` | title indent + `48;5;63` + trailing blank |
| `TestCycle207ChangelogHeight` | `height-4` window |
| `TestCycle207RewindCreditsRegression` | `/rewind`, `/credits` unchanged |

Gates:

- `go build -o <PROBEDIR>/our/agy ./cmd/wagyu` — OK.
- `go test -vet=off -run 'TestCycle207|TestCycle201|TestCycle202' ./cmd/wagyu/` — ok
  (cycle 201/202 not regressed).
- `go test -vet=off ./cmd/... ./internal/...` — all packages ok except
  `cmd/wagyu`, which fails only `TestCycle197_ConfigVisibleRows` and
  `TestPanelConfigLayout`. Both are **pre-existing at HEAD** on this machine,
  confirmed by `git stash -u`; they fail because the real `settings.json` has
  `editorMode=vim` (21 config items). Not caused by this cycle; not fixed.
- `gofmt -l cmd/wagyu/slash_viewer_panels.go cmd/wagyu/slash_cycle207_test.go`
  — empty (both new files clean). `slash_panels.go` was already gofmt-unclean at
  HEAD and is left as-is apart from the localised edits.

---

## 存疑 / limitations

- **`/changelog` long-line wrapping is not implemented.** REF wraps a long bullet
  across several rows (continuation lines like `of asking about them, and it
  blocks commands that would upload sensitive data.` appear as their own rows);
  the clone truncates the bullet with `fitWidth` to one row. This is the entire
  `changelog50` 39/49 and most of `changelog24` 16/23 residual. Implementing it
  needs REF's exact wrap column and the per-row continuation style, which the
  captures so far pin only loosely. Not attempted.
- **`/changelog` inline-code / inline-styling** inside bullets is
  approximated by `panelPlainInline`; REF's exact inline SGR was not re-derived.
- **`/usage` values are live in REF** (bar %, refresh countdown). The clone's mock
  is frozen; the value-level divergence is expected and cannot be closed without a
  live backend. The *structure* matches.
- **`/usage` page keys** (`pgup`/`pgdn`/`ctrl+u`/`ctrl+d`) are wired but REF's
  binding could only be partially confirmed; treated as inert-but-present.
- **`/diff` line-0 SGR token** — one redundant `\x1b[39m` before the bold title,
  a pen-state/capture artefact, not a cell difference. The clean fix would be to
  stop the separator padding from resetting the foreground, which is shared chrome
  outside this workstream's scope.
- **`/diff` enter-view-diff** (opening a file's diff from the All-Changes list) was
  not probed; the key path is inert.
- **`/diff` Commit-Tree / Per-Turn bodies** were captured only structurally; the
  scroll/navigation of those two tabs could not be captured and stays inert.
- **`/usage` note wrap cap** — the group-note wrap was fit within an empirically
  chosen cap in the range [76, 79]; the exact REF column is not pinned.

---

## Hard constraints

- Did not touch `main`; did not `git push`.
- Never edited `internal/convstore/sqlite.go`.
- New code confined to `slash_viewer_panels.go` and `slash_cycle207_test.go`; edits
  to `slash_panels.go` / `slash_panels_test.go` are localised, no wholesale
  reformatting.
- No destructive `/rewind` probe; nothing under `$HOME/.gemini/**` was modified.
- tmux used socket `-L agy207` exclusively; no `kill-server`, no default socket.
