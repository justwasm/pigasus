# Cycle 226 — `/diff` non-empty viewer content (TODO §4 cycle-207 follow-up)

Subagent workstream: **/diff viewer content** (`panelDiffLinesVCS`,
`handleDiffPanelKey`, `diffMoveCursor`).

Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w226`
Branch: `cycle226-diff-content`.

---

## TL;DR

Three TODO §4 cycle-207 存疑 items are now closed (in the form the user
specified — *conservative*, not regressing byte parity):

1. **All Changes `enter` handler** — was inert. Cycle 226 wires it to a
   per-file inline-diff subview (`git diff --no-color -- <path>`), styled
   line-by-line (+ green, − red-orange). Esc returns to the file list
   (does NOT close the panel).
2. **Per Turn tab** — was a hardcoded `"No file changes in conversation"`
   placeholder regardless of state. Cycle 226 keeps the placeholder when
   the session has no user turns OR the working copy is clean, and
   renders a one-row-per-turn list (`Turn N · <file changed>`) when
   session has turns AND changes exist.
3. **Commit Tree `enter` handler** — was inert. Cycle 226 wires it to a
   per-commit inline-diff subview (`git show --no-color <hash>`) for the
   commit under the cursor; cursor on the working-copy row is a no-op
   (working copy has no commit hash to drill into).

A side-effect fix: the cycle-207 Commit Tree emitted a `|` tree
connector before *every* commit; a fresh multi-commit probe
(`agy226/diff50-CT.ref.raw.txt`) shows the connector is only between the
working-copy row and the **first** commit, after which commits stack
directly. Cycle 226 corrects the connector logic and unifies the cursor
model across all three tabs (cycle 207 had the cursor inert on Per Turn
and Commit Tree).

REF 1.4.0's side-by-side diff viewer (the `n/N f c d esc` keyboard
surface; `agy226/diffA-enter1.ref.raw.txt`) is out of scope for this
cycle; the cycle-226 substitute is a plain inline diff body. The
list-view shapes are byte-identical to cycle 207.

---

## What was wrong (and why)

### 1. `enter` on All Changes was inert  【一手】

The cycle-207 deliverable noted:

> `/diff` enter-view-diff (opening a file's diff from the All-Changes list)
> was not probed; the key path is inert.

A fresh probe of REF 1.4.0 (`agy226/diffA-enter1.ref.raw.txt`) shows that
REF opens a side-by-side diff viewer with a `change 1/1 · file 1/N`
footer and the `n/N next/prev diff · ←/→ cycle file · f full file · c
comment · d delete comment · esc back` legend. The full side-by-side
viewer is too large for this cycle (hunk header parsing, per-line
numbering, hunk-by-hunk navigation, `f`/`c`/`d` handlers) — the
cycle-226 substitute is a plain inline-diff subview that reads
`git diff --no-color -- <path>` and styles the +/− lines.

Wiring: `handleDiffPanelKey`'s new `tea.KeyEnter` branch calls
`m.diffEnter()`, which sets `m.panel.diffView = 1` and
`m.panel.diffViewPath = <file>`; `panelDiffLinesVCS` then dispatches to
`panelDiffInlineLines` instead of `diffAllChangesLines`.

### 2. Per Turn tab was a constant placeholder  【推测】

The cycle-207 deliverable noted:

> Per Turn has no captured per-turn model (the clone does not track
> per-turn changes)

Per Turn was a hardcoded `"No file changes in conversation"` row
regardless of state. Cycle 226 keeps that placeholder when the session
has no user turns OR `git diff` has no changes, and renders one row
per user turn with a `Turn N · <file>` snippet when both conditions hold.
The cycle-197 / cycle-194 convention is that user-role messages are
"turns" (`diffCountTurns` walks `m.session` for `Role == "user"`); the
clone does not have a per-turn diff model, so each row is paired with
one of the changed files (`tracked[i] + untracked[i]` indexed by `i %
len`) — see the 推测 marker.

### 3. Commit Tree `enter` was inert  【一手】

The cycle-207 deliverable noted:

> the scroll/navigation of those two tabs could not be captured and
> stays inert.

A fresh probe of REF 1.4.0 (`agy226/diffCT-D-E.ref.raw.txt`) shows that
on a commit row, Enter opens the *commit's* All Changes view (titled
`Diff (git) @ <hash>`), and Enter *again* on a file row opens the
side-by-side diff viewer. The clone substitutes a single-level
`git show <hash>` body, gated through `m.panel.diffView = 2`.

Wiring: the cursor now maps differently on Commit Tree (cursor==0 is
the working-copy row, cursor==1..N are the N commits, see
`diffMoveCursor`'s case 2). Enter on the working-copy row is a no-op
(no hash to drill into); Enter on a commit row calls
`m.diffOpenInline(2, commits[cursor-1][0])`.

### 4. `|` tree connector regression (cycle 207 → 226)  【一手】

`diffCommitTreeLines` emitted a `|` connector before **every** commit;
the cycle-207 implementation extrapolated this from a single-commit
capture (the cycle-207 存疑 note flagged it as 推测). A fresh
multi-commit probe shows the connector is only between the working-copy
row and the first commit; subsequent commits stack directly
(`agy226/diff50-CT.ref.raw.txt`):

```
[38;5;69m[48;5;255m▌[39m [1m*[0m[48;5;255m  [1m[38;5;172mWorking Copy (Uncommitted Changes)[0m[48;5;255m
[49m   |
   * [38;5;69m1e46caa[39m second commit: ...
   * [38;5;69m74ab8bf[39m initial commit
```

Cycle 226's `if i == 0` guard in `diffCommitTreeLines` matches this;
the single-commit cycle-207 test still passes (the connector is still
emitted between the working-copy row and the first — and only — commit).

---

## Files changed

| File | Lines | Why |
|---|---|---|
| `cmd/wagyu/slash_panels.go` | +31 / −0 | `slashPanel.diffView`/`diffViewPath` fields; `handleDiffPanelKey` Enter + subview Esc branches; tab switch resets cursor. |
| `cmd/wagyu/slash_viewer_panels.go` | +260 / −67 | `panelDiffLinesVCS` dispatch to subview; `diffPerTurnLines`; `panelDiffInlineLines`; `gitDiffBody`; `diffInlineStyle`; `diffInlineLegend`; `diffOpenInline` / `diffCloseInline` / `diffEnter`; `diffMoveCursor` extended to Per Turn + Commit Tree; `diffCommitTreeLines` connector fix. |
| `cmd/wagyu/slash_cycle226_test.go` | **new, ~400** | 10 tests (see below). |

No new third-party dependencies. The cycle-195 `slashRespond` is *not*
used (the cycle-226 subview is a panel sub-state, not a transcript
echo).

---

## Evidence

Probe harness (`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy226/`):
`probe.py` (copy of the cycle-207 driver with the cwd reset to the
agy226 scratch dir) drives REF (`OPENAGY_BACKEND=agw`) and the clone
(`mock`) in tmux socket `-L agy226`, at a given size, sends the slash
keys, and `capture-pane -e`'s the result. `PROBE_SETTLE=3.0` is used
to capture REF's steady state.

Fixtures:
- `repo` — modified `tracked.txt` + `staged.txt`, untracked `new.txt`,
  deleted `tracked.txt` (clean enough to drive all three tabs with real
  data).
- `repoclean` — clean single-commit git workspace (drives the All
  Changes empty-state).

`compare.py` reuses cycle 207's `slice_panel` (anchors: `Diff` /
`switch view`), normalises the clone's two truecolour constants to the
256-colour index tmux renders them as.

Raw frames kept per case: `{name}.ref.raw.txt`, `{name}.our.raw.txt`.

---

## Live parity (final run, 220×50 + 220×24)

| Panel | Case | Geometry | Mismatched lines | Nature of mismatch |
|---|---|---|---|---|
| `/diff` All Changes | `diff50` (dirty) | 220×50 | 1 / 9 | line 0 SGR pen only (cycle 207 存疑) |
| `/diff` All Changes | `diffclean2` (clean) | 220×50 | 1 / 5 | line 0 SGR pen only |
| `/diff` Commit Tree | `diff50-CT` | 220×50 | 1 / 9 | line 0 SGR pen only |
| `/diff` Per Turn | `diff50-PT` | 220×50 | 1 / 5 | line 0 SGR pen only |
| `/diff` All Changes | `diff24` | 220×24 | 1 / 7 | line 0 SGR pen only |
| `/diff` Commit Tree | `diff24-CT` | 220×24 | 1 / 7 | line 0 SGR pen only |
| `/diff` Per Turn | `diff24-PT` | 220×24 | 1 / 5 | line 0 SGR pen only |

All seven cases carry the same one-token residual the cycle-207 report
documented as a tmux-pen-state artefact, not a cell difference. The
panels' rows are otherwise byte-identical.

The list-view shape is preserved byte-exact (the `TestCycle207Diff*`
suite still passes; see Tests below), so a future change to /diff that
regresses the cycle-207 rendering will fail these tests first.

---

## New behaviour, live-probed

The cycle-226 subview is **new behaviour**, not byte-equivalent to REF.
Probes that show the new surface working:

- `agy226/diffA-enter.our.raw.txt` — All Changes Enter opens the
  inline-diff subview for `staged.txt`; `+modified line` is green
  (`38;5;29m`), `−staged second` is red-orange (`38;5;166m`); title row
  is `staged.txt` (bold); legend is `esc back`.
- `agy226/diffCT-D-E.our.raw.txt` — Commit Tree Down + Enter on the
  first commit opens the inline-diff subview for `1e46caa`; the body
  is `git show`'s output (commit metadata + diff hunks).
- `agy226/diffA-enter1.ref.raw.txt` — REF's side-by-side viewer, kept
  as the 存疑 reference (the cycle-226 subview is a structural
  substitute, not a reproduction).

---

## Tests

`cmd/wagyu/slash_cycle226_test.go` — 10 tests, all passing:

| Test | Covers |
|---|---|
| `TestCycle226PerTurnEmpty` | Per Turn keeps the cycle-207 placeholder when session has no turns. |
| `TestCycle226PerTurnRows` | Per Turn renders `<N> turn(s) · <M> file(s) changed` summary + N rows with `Turn N · <file>` snippets; cursor row carries the brand-blue `> ` marker. |
| `TestCycle226PerTurnNoChanges` | Per Turn keeps the placeholder when turns exist but the working copy is clean. |
| `TestCycle226AllChangesEnter` | Enter on All Changes sets `diffView=1` + `diffViewPath=<file>`; the body is `git diff` output with +/− lines styled; Esc returns to the list (panel stays open). |
| `TestCycle226CommitTreeEnter` | Enter on a commit row sets `diffView=2` + `diffViewPath=<hash>`; the body is `git show` output including commit metadata; the title is `commit <hash>`. |
| `TestCycle226WorkingCopyEnter` | Enter on the Commit Tree working-copy row is a no-op (no hash to drill into). |
| `TestCycle226DiffCursorCommitTree` | Cycle 226 unifies the cursor model: Commit Tree cursor moves from 0 (working-copy) to 1 (first commit) and clamps. |
| `TestCycle226DiffCursorPerTurn` | Per Turn cursor moves over the turns list and clamps. |
| `TestCycle226CommitTreeMultiCommitShape` | The cycle-207 `|` connector regression is fixed: a 2-commit repo renders `\|` only before the first commit; subsequent commits stack directly. |
| `TestCycle226PanelKeyDefaults` | Tab switch resets the cursor to 0; Esc on the list view closes the panel (cycle-207 behaviour preserved). |

Plus the cycle-207 regression tests (`TestCycle207DiffAllChanges`,
`TestCycle207DiffEmptyStates`, `TestCycle207DiffMoveCursor`,
`TestCycle207DiffGitChanges`, `TestCycle207DiffCommitTree`) all still
pass byte-exact.

### Gates

- `go build -o <PROBEDIR>/our/agy ./cmd/wagyu` — OK.
- `go test -vet=off ./cmd/... ./internal/...` — fully green
  (count=1, no cache).
- `gofmt -l cmd/wagyu/slash_cycle226_test.go cmd/wagyu/slash_viewer_panels.go`
  — empty (both new/touched files clean). `slash_panels.go` was
  gofmt-unclean at HEAD and is left as-is apart from the localised
  edits (per the AGENTS.md convention).
- `go vet ./cmd/wagyu/` — clean.

---

## 存疑 / limitations

- **`/diff` All Changes enter-view-diff in REF is a side-by-side
  viewer.** The clone substitutes a plain inline diff body (`git diff
  --no-color -- <path>` with line-by-line `+`/`-` colouring). The
  full side-by-side rendering (hunk headers as table cells, line
  numbers in two columns, `n/N next/prev diff` / `←/→ cycle file` / `f
  full file` / `c comment` / `d delete comment` bindings) is out of
  scope; `agy226/diffA-enter1.ref.raw.txt` is the reference, the
  cycle-226 subview is the structural substitute.
- **`/diff` Commit Tree enter in REF is two-level drill-down**: Enter
  on a commit opens the commit's All Changes view (`Diff (git) @
  <hash>`), and Enter again on a file opens the side-by-side diff
  viewer. The clone collapses this into one `git show <hash>` body.
- **Per Turn rows are paired with files heuristically** — each turn
  gets `all[i % len(all)].path`, because the clone has no per-turn
  diff model. REF (which has the model) probably pairs each turn with
  the file that turn actually touched. Held.
- **REF's commit-tree working-copy row** carries a ▌ tree connector on
  its own (the row is highlighted by default). Cycle 226 leaves the
  cycle-207 default cursor at row 0 (working-copy) but does not
  reproduce the highlighted-▌ visual. Held.
- **`/diff` line-0 SGR token** — the cycle-207 存疑. One redundant
  `\x1b[39m` before the bold title, a pen-state/capture artefact, not a
  cell difference. The clean fix would be to stop the separator
  padding from resetting the foreground, which is shared chrome outside
  this workstream's scope.

---

## Hard constraints

- Did not touch `main`; did not `git push`.
- Never edited `internal/convstore/sqlite.go`.
- New code confined to `slash_viewer_panels.go` and the new
  `slash_cycle226_test.go`; edits to `slash_panels.go` are localised,
  no wholesale reformatting.
- No destructive `/rewind` probe; nothing under `$HOME/.gemini/**` was
  modified.
- tmux used socket `-L agy226` exclusively; no `kill-server`, no
  default socket.
- `~/.local/bin/agy` (1.4.0, ~180 MB) was used only as the live-probe
  reference; the probe dir is `/private/var/folders/.../agy226/` and
  every fixture lives there too. `cycle226Root` env was not needed
  because the probe dir was passed positionally to `probe.py`.
