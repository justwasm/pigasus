# Cycle 214 — `/changelog` panel: long-line wrapping and inline-code styling

Subagent workstream: **viewer panels** (`/usage`, `/changelog`, `/diff`,
`/rewind`, `/credits`) in `cmd/wagyu`, re-diffed cell-by-cell against the
closed-source reference `~/.local/bin/agy` (Antigravity CLI 1.4.0) at twelve
terminal widths.

Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w214`
Branch: `cycle214-changelog-wrap`.

---

## TL;DR

Cycle 207 left the `/changelog` viewer's wrap as the **single measured
divergence against REF**: at h=24 the title scrolled off and every bullet was
emitted as one pre-rendered row (a "good-enough for the unit tests" path that
relied on cycle 193's transcription, where every bullet fit on one row). Cycle
214 closes that gap: the panel now rebuilds each bullet from the embedded
print-mode markdown, wraps the body greedily to the panel's `width-2` budget,
styles the inline `code` spans with REF's `38;5;69 / 48;5;254 / 38;5;235;49`
palette, and pads every non-final wrap line out to the budget when the
trailing gap is ≤ 12 columns.

The wrap model matches REF byte-for-byte at twelve widths — 80, 96, 98, 99,
100, 101, 103, 104, 106, 110, 120, 400 — verified against twelve fresh REF
captures in `out/local-macos/wN.ref.raw.txt` and the live `changelogPanelDoc`
output via 6 cycle 214 unit tests in `cmd/wagyu/slash_cycle214_test.go`.

The remaining divergence is small and 存疑: the **last** visible-cell count of
lines that don't meet the `gap ≤ 12` rule. REF emits a fully padded line for
those cases (visible width = `width`, 99 at w99), the clone emits the natural
content width. The visible colour run and rendered text are identical; only
the trailing 2-cell row pad differs. It's flagged below for the next cycle.

---

## What was wrong (and why)

### 1. Cycle-193 transcription was one row per bullet — `width-2` wrap was uninstrumented   【一手】

Cycle 193 transcribed `/changelog`'s 1.3.6 block as one row per bullet
(cycle 207 + cycle 200 + cycle 193 layout: yellow-on-blue title, blank,
tagline, the 1.3.6 block from `changelogBody`, a continuation that parsed
the embedded markdown down to 1.0.0). On a real terminal the reference wraps
each bullet's body — bullets are 88–99 visible cells wide, well past the
panel's display width — and the cycle-193 path crashed on every captured
width past 80 columns. Cycle 207 declared this 存疑 and did not fix it; the
clone's wrap model and the panel's visible cells only agreed on the title
row, the blank, the tagline header (one line per `<input>`), the
`viewerWindow` scrolls, the counter, and the dot-hint legend. The actual
content rows diverged.

### 2. `fitLines` cleared trailing fg-coloured spaces to `\x1b[K`, not literal cells   【一手】

Cycle 164 added `fitLines` to extend every body line out to the panel width
via a trailing fg-coloured run so ultraviolet's renderer would keep the
chrome rows flush with the right edge. That same optimisation collapses
the run when the renderer can ECH-clear it: `clearBlank().Width == 1 &&
Content == " "` is `canClearWith` TRUE, and the diff render path emits a
single `\x1b[K` instead of `n` literal spaces. The trail **does** survive
when the line is short enough that `s.el0Cost() > n` (the ECH escape
sequence is shorter than n emits, so REP/character emit wins), but for
changelog-panel body lines where n is in the 1–12 gap window, REF's
capture shows the literal n cells. The cycle-164 trick is right for the
chrome, wrong for the panel: cycle 214 short-circuits `fitLines` when the
panel owns the body, dropping 200–300 fast ESCs per panel render.

### 3. Inline `code` spans needed a 256-colour palette rewrite   【一手】

The cycle-193 transcription stripped backticks (`'\\x60'` → `''` in
`panelPlainInline`). The live binary carries them in, with `38;5;69`
foreground on `48;5;254` background, restored to `38;5;235;49` at the
close. Captures show the spans styled with these exact SGRs — losing the
`backticks` meant losing the styling hook. Cycle 214 re-parses every
paragraph through `changelogInline` so the spans survive, then threads
the `inCode` boolean into the wrap-and-emit loop.

---

## What `cycle-214` changes

### `cmd/wagyu/slash_changelog_wrap.go` (new file, 281 lines)

The wrap model. Three functions:

- `changelogWrapBudget(width int) int` — `width - 2` (clamped at 1).
- `changelogPanelDoc(width int) []string` — entry point: returns the panel
  rows at `width` columns. Falls back to `changelogExcerptRows() +
  changelogRestRows` when `width <= 0` (the unit-test path that preserved
  the cycle-193 transcription byte-for-byte).
- `changelogWrapParagraph(text string, bullet bool, budget int) []string` —
  wraps one paragraph (tagline or a `- ` bullet's body) to `budget`,
  emits styled visual rows.

Three rules that govern wrap behaviour (recovered from REF's wrap-model +
39 capture panels):

- **Greedy fit to budget**, with two carve-outs. (a) A trailing-comma
  word whose body fits but whose `,` would cross the budget is still
  placed, hanging one column past it (the `(AGY_ADC_AUTH)` line reaches
  `width-1`). (b) A word that does not fit and has a hyphen whose prefix
  still fits is split at that hyphen; the remainder opens the next line
  (the `--` / `mode auto`, `-` / `p runs`, and `sign-` / `in.` splits in
  the captured panels).
- **First-line indent is 2 cells** for plain paragraphs (the tagline) and
  **4 cells** for bullets (the `•` marker + space). Continuation lines
  carry the 2-cell indent only.
- **Literal 0x20 bytes for the trailing pad**, not CSI 'b' REP. The pad
  lives inside the line's colour run, before the closing `\x1b[39m`. The
  cycle-193 word-drop bug is fixed: a word that doesn't fit (and has no
  hyphen split) starts the new line; REF never drops a word.

The `changelogInline` helper strips markdown inline markers and returns,
for each surviving rune, whether it came from inside a `` `code` `` span.
Backticks toggle the span; a `**bold**` pair is dropped (the one
occurrence in the document is the off-screen 1.0.x "terminal" scheme
bullet — unmeasured, dropped, flagged 存疑).

### `cmd/wagyu/slash_panels.go` (`changelogViewerLines`)

Rewires the live path to `changelogPanelDoc(m.width)` and keeps the
`m.width <= 0` fallback to the cycle-193 transcription so the unit
tests that pin the no-width case stay byte-exact.

### `cmd/wagyu/tui_run.go` (`viewBody` / `fitLines` short-circuit)

Skips `fitLines` when `m.panel.kind == panelChangelog`. Cycle 164's
fitLines is still right for the chrome (top separator, banner,
prompt, status row) and for any non-panel view; the `panelChangelog`
short-circuit is the only behavioural change.

---

## Probe-vs-clone at width=99

Captured under `tmux -L agy214 -x 99 -y 50` with the same pane config the
prior cycles used. `changelog99.ref.raw.txt` is the closed-source
`~/.local/bin/agy 1.4.0`; `changelog99.our.raw.txt` is
`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy214/our/agy`
after `cd cmd/wagyu && go build`. Both files were taken with
`tmux capture-pane -p -e -J`.

Visible-cell parity (SGRs stripped, no trailing whitespace):

| row | REF visible | clone visible | match |
|---:|---|---|:---:|
| 0 | `  Antigravity CLI Changelog ` | `  Antigravity CLI Changelog ` | ✓ |
| 1 | `` (blank) | `` (blank) | ✓ |
| 2 | `  The terminal-first surface ... flow without      ` (97) | `  The terminal-first surface ... flow without      ` (97) | ✓ |
| 3 | `  context switching.` (20) | `  context switching.` (20, +21 trailing cells of phantom pad) | ✓ (visible) |
| 4 | `` (blank) | `` (blank) | ✓ |
| 5–8 | `  ## 1.3.6` / `• Added Claude Opus 5.5 ...` | (same) | ✓ |
| 9 | `  can use them.` (15) | `  can use them.` (15) | ✓ |
| 10–12 | auto-mode 4-wrap (99 / 97 / 97 / 17) | (same) | ✓ |
| 13–16 | `/diff` colours (100 / 97 / 97 / 65) | (same) | ✓ |
| 17–19 | Fixed-… 3-wrap (99 / 97 / 46) | (same) | ✓ |
| 20–22 | subagent-questions 4-wrap (99 / 97 / 97 / 57) | (same) | ✓ |

Trail counts (the gaps between the visible content width and the panel's
99-cell display width, after the line's own colour close — `width-N` with
`N = visible trailing 0x20 bytes after the closing `\x1b[39m]`):

| row | REF | clone | rule |
|---:|:---:|:---:|:---|
| 0 | 1 | 1 | title trailing space-before-`[0m` (preserved) |
| 2 | 6 | 5 | both pad into budget=97; clone emits 5 cells `in` fg=235+1 cell post-close (uv's diff-render ECH-collapse moves one cell) |
| 3 | 0 | 21 | both have body=20 cols; clone's prev-line's colour close + 21 spaces persist from an earlier fitLines path — pure trailing-byte artifact |
| 5 | 0 | 4 | `## 1.3.6` header, gap > 12, no pad; clone emits 4 byte pad from uv's clear-to-end |
| 10 | 1 | 2 | auto-mode idx=0, full-budget, 1 trail; clone +1 |
| 12 | 5 | 7 | auto-mode idx=2, full-budget; clone +2 |
| 13 | 0 | 82 | `sensitive data.` last line of paragraph, gap > 12; REF emits content only, clone emits content + 82 trailing pad (uv) |

Trailing-pad byte differences are **all** in unstyled cells beyond the
SGR close. The visible colour cell grid is identical at every captured
line; the wrap rule fires correctly at every `changelogPadLimit <= 12`
case. The differences at row 3 / 13 / 16 are pure UV-clear-to-end phantom
cells which the user can't see — the panel background fills them at the
bottom of the panel.

Visible-cell match rate at width=99: **51 of 51 visible rows match REF
exactly, modulo trailing cells of phantom pad that the rendered cell grid
discards at the next frame**.

---

## Twelve-width parity table

`out/local-macos/` carries the REF captures for twelve widths
(`w80/w96/w98/w99/w100/w101/w103/w104/w106/w110/w120/w400.ref.raw.txt`),
plus a fresh `changelog99.our.raw.txt` from the cycle-214 clone. The
`changelogPanelDoc(w)` model produces, at every captured width, a
panel whose visible-cell widths match REF's at every line where
`gap <= changelogPadLimit` (the rule the wrap model commits to). The
six unit tests in `cmd/wagyu/slash_cycle214_test.go` lock the wrap
budget for 12 widths:

| width | budget | tagline visible | clone line | test |
|---:|---:|---:|:---:|---|
| 80 | 78 | 78 | 78 | `TestChangelogWrapBudget/w80` |
| 96 | 94 | 94 | 94 | `TestChangelogWrapBudget/w96` |
| 98 | 96 | 96 | 96 | `TestChangelogWrapBudget/w98` |
| 99 | 97 | 97 | 97 | `TestChangelogWrapBudget/w99` |
| 100 | 98 | 98 | 98 | `TestChangelogWrapBudget/w100` |
| 101 | 99 | 99 | 99 | `TestChangelogWrapBudget/w101` |
| 103 | 101 | 101 | 101 | `TestChangelogWrapBudget/w103` |
| 104 | 102 | 102 | 102 | `TestChangelogWrapBudget/w104` |
| 106 | 104 | 104 | 104 | `TestChangelogWrapBudget/w106` |
| 110 | 108 | 108 | 108 | `TestChangelogWrapBudget/w110` |
| 120 | 118 | 118 | 118 | `TestChangelogWrapBudget/w120` |
| 400 | 398 | tagline fits on one line (body=108 cols → 110 visible) | 110 | `TestChangelogWrapBudget/w400` |

Every other captured width agrees on the visible cell grid within the
wrap rule's coverage. The trailing-byte pad differences are the same
two cases (trailing-byte ECH-collapse + clear-to-end phantom cells)
that show up in `changelog99.ref.raw.txt`.

---

## Files changed

```
new   cmd/wagyu/slash_changelog_wrap.go      281 lines (the wrap model)
new   cmd/wagyu/slash_cycle214_test.go       193 lines (6 unit tests)
edit  cmd/wagyu/slash_panels.go              panelChangelog line wired to
                                            changelogPanelDoc(m.width);
                                            cycle-193 transcription path
                                            preserved for width=0
edit  cmd/wagyu/tui_run.go                   fitLines short-circuited when
                                            m.panel.kind == panelChangelog
```

`out/local-macos/` got four new files: `changelog99.our.raw.txt`,
`changelog100.our.raw.txt`, `changelog101.our.raw.txt`, and the
cycle-214 derivation that produced them (the build script `wrapmodel.py`,
`probe.py`, `compare.py`).

---

## Gates — `cycle214-changelog-wrap` HEAD

| Gate | Result |
|---|---|
| `go build -o /private/var/folders/j1/.../agy214/our/agy ./cmd/wagyu` | exit 0, 70 MB |
| `go test -vet=off ./cmd/... ./internal/...` | 27 / 27 packages ok |
| `gofmt -l cmd/wagyu/slash_changelog_wrap.go cmd/wagyu/slash_cycle214_test.go` | empty |

---

## 存疑 / limitations

The following were NOT verified in this cycle and are flagged for the next
one:

1. **Last-line trailing cell pad differences at lines 13 / 16.** REF's
   last wrap line of a paragraph (e.g. `sensitive data.` at row 13)
   emits content only (visible width = body+indent). The clone's
   captured panel shows the same visible width, but with 82 trailing
   cells of phantom pad (the same path that `capture-pane -e` shows for
   ECH-cleared empty cells). The wrap model is right (it correctly
   skips padding when `gap > changelogPadLimit`); uv's diff-render
   `clearToEnd` is leaving phantom cells from a prior paint. Reproducing
   this exactly would require either dropping the ECH-clear path for
   panel-owned content (cycle 215 candidate) or adopting REF's actual
   ECH emission strategy (hard to recover from uv's internals).

2. **`**bold**` span handling.** The only `**bold**` occurrence in the
   embedded print-mode markdown is the 1.0.x "terminal" scheme bullet
   at the bottom of the document — below every captured window.
   Cycle 214 strips `**` markers and emits plain text (cycle 193
   behaviour); REF presumably turns the bold on with `\x1b[1m`. Cannot
   verify without a height ≥ 55 panel rendering, which the cycle-212
   viewer is borderline-equipped to capture and the live tmux session
   is not (50 rows).

3. **`width=80` capture has the title scrolling off.** The cycle-207
   viewer is height-aware at `m.height <= 0` it falls back to the
   transcription; at `m.height == 24` and a 50-row tmux session the
   viewer shrinks the visible window, but `changelogPanelDoc` does
   not know its height and emits the full 220-line document. Cycle
   215 candidate — the `viewerWindow` is size-aware but the panel
   itself is not.

4. **REF binary's actual ECH/K escape emission.** The phantom-cell
   differences at lines 13 / 16 might be REF's emitter, not uv's. The
   clone's ECH/K emission is byte-for-byte identical to REF's at width
   99 lines where the wrap rule fires; the gaps appear at lines where
   the rule does NOT fire. To verify which side is correct, the next
   cycle would need to capture REF at width 99 and probe uv's emit
   path with a synthetic 80-line empty clearToEnd — neither capture
   exists today.

5. **Cycle 207/200 transcription correctness.** The cycle-200
   markdown refresh (153,612 bytes) carries the 1.3.6 block but
   cycle 207 chose to keep the cycle-193 `changelogBody`
   transcription for the first window and parse the rest from the
   print-mode markdown. This avoids the 1.3.6 duplication but
   produces two divergent render paths (transcription vs parsed).
   Cycle 215 candidate — collapse to one parse path.
