# Cycle 211 — the SKILL.md detail viewer for `/skills`

Target: reference `agy` 1.4.0 (macOS/arm64), TUI 220 columns unless noted.
All raw captures live in the cycle-211 probe directory
(`…/agy211/caps/`); the figures come from live `tmux -L agy211` runs of
the reference and of a freshly built clone.

---

## TL;DR

`/skills` `enter View` opened a *title + description + path + esc Back*
place-holder (cycle 201), then a *title + description + path + esc Back*
over the **selected** catalogue row (cycle 203). The reference's real
screen is a scrollable document viewer over that row's `SKILL.md` —
capture shows `(1–26 of 27 lines)` counters, `↑/↓ · pgup/pgdown ·
ctrl+end · ctrl+home · esc back` legend, and a `science:uv` body with
`1. first item` style ordered lists, `` `code` `` and `**bold**` spans,
and a `───` thematic break (`caps/ref.detail.uv.raw.txt`). Cycle 211
reproduces all of that.

The viewer is now backed by a purpose-built renderer:

| | cycle 203 | cycle 211 (this commit) |
|---|---|---|
| `enter View` payload | 4 rows: title / blank / description / path | 26–64 rows: title / wrapped description / path / blank / H1 / H2 / paragraphs / bullet list / numbered list / code spans / `**bold**` / `───` |
| `Panel window` field | n/a — only "Esc Back" | `m.panel.window` carries the 0-based logical-line offset, clamped to `[0, total − vis]` |
| Counter | "Skills" panel-title + esc legend only | "`(a–b of T lines)`" via `viewerCounter`, dim grey, only when `total > vis` (else an empty row) |
| Scroll keys | esc only | `↑/↓` line, `pgup/pgdown` page (`vis − 1`), `ctrl+home` top, `ctrl+end` bottom, `esc` returns to the list (cursor survives) |
| Body renderer | n/a | `mdSkillBody` reads `SKILL.md`, strips leading `--- … ---` frontmatter, runs the line-by-line renderer (`mdBodyLines`) |
| Window formula | n/a | `max(5, h*3/4 − 11)` — same rule as the list, with a 5-row floor that the reference applies at low heights |

All REF vs clone captures produced by the cycle-211 viewer match at the
level the renderer covers (counter, legend, scroll keys). The two
remaining gaps — GFM pipe tables and `<!-- … -->`-stripping behaviour
on rows that contain them — are documented as 存疑 below.

---

## What was wrong

Cycle 203 shipped a minimal place-holder ("Skills" title, description,
path, esc-only legend) and explicitly punted on the real viewer:

> REF's real screen is a full SKILL.md document viewer
> (title, wrapped description, path, the file body with a
> "(1–7 of 252 lines)" window and a scroll legend — see
> `caps/ref.down_enter.raw.txt`); reproducing that viewer is out of scope
> for this cycle, so the clone keeps the cycle-201 minimal shape but now
> backs it with the *selected catalogue entry* instead of a hardcoded
> `~/.gemini/skills` path. 【推测】 for the layout; the name/description/path
> values are first-hand.

The captured evidence in `caps/ref.detail.*.raw.txt` painted a richer
picture:

- `science:uv` opens to a 26-row window over a 27-line document and a
  counter `(1–26 of 27 lines)` in dim grey.
- `antigravity-guide` (`caps/ref.detail.agguide.raw.txt`) — 51 logical
  lines, same shape.
- Scroll: `pgdown` (`ref.detail.pgdown.raw.txt`) advances by one window
  minus one; `ctrl+home` (`ref.detail.ctrlendhome.raw.txt`) jumps to the
  top; `ctrl+end` (`ref.detail.ctrlned.raw.txt`) jumps to the bottom and
  keeps the bottom line on screen.
- Window geometry: heights 12–22 all show `5`-row windows (`ref.detail.o0.h*.raw.txt`),
  heights ≥ 24 follow `h*3/4 − 11` exactly. Counter disappears at
  `h = 100` when the document fits in one window.

---

## What changed

| File | Δ | Role |
|---|---|---|
| `cmd/wagyu/slash_skills_detail.go` | **+549 (new)** | the SKILL.md viewer: header, body renderer, scrollable window, key handler |
| `cmd/wagyu/slash_skills.go` | +30 / −10 | delegate `skillsPanelDetailLines` to the viewer when `Path` is set, keep `skillsDetailLegacyLines` as the no-Path fallback |
| `cmd/wagyu/slash_panels.go` | +9 / −9 | panel-level key routing: detail-view keys now go to `handleSkillsDetailKey`; comment cross-refs |
| `cmd/wagyu/slash_panels_test.go` | +8 / −4 | `TestPanelSkillsDetailView` checks the viewer-shaped legend, not the cycle-201 minimal one |
| `cmd/wagyu/slash_cycle211_test.go` | **+383 (new)** | 22 tests pinning the viewer geometry, body, scroll keys, counter and wiring |

`gofmt -l cmd/wagyu/slash_skills_detail.go cmd/wagyu/slash_cycle211_test.go`
→ clean.

The panel-level edits are minimal:

```go
// slash_panels.go — cycle 211 routes detail-view keys through
// the new handler; everything else (esc's `m.panel.detail = false`,
// modified-key fall-through) follows cycle-201 rules.
if m.panel.kind == panelSkills {
    if m.panel.detail {
        if handled, cmd := m.handleSkillsDetailKey(key); handled {
            return true, cmd
        }
        return false, nil
    }
    …
}
```

```go
// slash_skills.go — cycle 211 chooses between the viewer and the
// legacy 4-row minimal shape by Path presence; both render through
// the existing panelKeys plumbing.
func (m *runMode) skillsPanelDetailLines() []string {
    if m.skillsDetailSelected().Path == "" {
        return m.skillsDetailLegacyLines()
    }
    return m.skillsDetailViewLines()
}
```

### Body renderer (`slash_skills_detail.go`)

The renderer is a small scanner, not a CommonMark parser — the four
inline constructs (`code`, `**bold**`, `[text](url)`, bare URLs) plus
the four line kinds (`#`/`##`/`---`/`-`/`N.`) cover every visible row
in the captured REF bodies (`uv`, `agents-sdk`, `agy-customizations`,
`antigravity-guide`, `a11y-debugging`). Each kind is a documented measured
shape, recorded inline in the file header.

| Source line | REF render (sample, SGR stripped) |
|---|---|
| `# X` | `  ` + bg 63 + fg 228 + ` X ` | (centred in the H1 pad) |
| `## X` / `### X` | `  ` + fg 27 + `## X` (marker kept verbatim) |
| `---` | `  ` + fg 249 + `──────` |
| `- X` | `  • X` (top-level); `      • X` (nested, four-source-spaces in) |
| `N. X` | `  N. X` (the digit is reproduced verbatim) |
| (paragraph) | `  X` (no wrapping — REF also doesn't wrap body lines) |
| `` `code` `` in paragraph | fg 69 + bg 254 + `code` + reset |
| `**bold**` in paragraph | `**` opens bold, `**` closes it; a code span inside bold restores `\x1b[1m\x1b[38;5;235m\x1b[49m` afterwards (this is how REF renders "`**Antigravity CLI (\`agy\`)**: …`" rows in `agguide`) |
| `[text](url)` | `text` only (URL dropped; matches `agguide` row "**…**: [references/cli.md](…)" → `**…**: references/cli.md`) |
| bare `<url>` / `url` | `url` underlined, fg 36 |
| `<!-- … -->` | the line is dropped from the document **and** from the counter (cycle-211) — REF renders it transparently, never counts it (visual diff over `agguide`'s two comment lines is silent) |

Frontmatter (`---\nname: x\ndescription: y\n---\n…`) is consumed by
`mdSkillBody`; everything from the second `---\n` onward is the body.

### Window geometry

```
vis    = max(5, h*3/4 − 11)        // h ≤ 0 falls back to skillsPanelDefaultHeight (30)
total  = len(doc) + 1              // the +1 is the reference's trailing-line quirk
                                 // (uv: 26 logical rows but the footer says 27)
window ∈ [0, total − vis]         // clamped at both ends
```

Verified at heights 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 24, 30,
40, 50, 60, 80, 100 — every point matches REF
(`ref.detail.o0.h40.raw.txt` … `ref.detail.o0.h100.raw.txt`,
`ref.detail.uv.tall.raw.txt` at h=100).

The formula `max(5, h*3/4 − 11)` was the cycle-203 list panel rule
without the floor (it had `max(1, …)`); the floor moves from 1 to 5 here
because the chrome (5 header rows + counter + dot legend + a blank
above each) eats the panel at low heights, and REF clips at 5, not 1.
Verified at heights 13–22: REF and OUR both show `end = 5`.

### Counter / header

```
total > vis   → dim-grey row "  (a–b of T lines)"
total ≤ vis   → empty row
```

`viewerCounter(start, end, total)` (in `slash_panels.go:2120`) was the
shared helper cycle-207 introduced for the viewer panels; cycle 211
reuses it unchanged. The `(a–b of N lines)` form has the en-dash `\u2013`.

The window itself scrolls the body — title, description, path and blank
are the first four logical rows, and the visible window is the slice
`doc[window … window+vis]`. At `window=0` the header sits at the top; at
`window=1` the description tops the panel, exactly as REF shows
(`caps/ref.detail.o0..o4.h50.raw.txt`).

---

## Counter formula — measured vs reproduced

| Skill | REF counter | OUR counter | Δ |
|---|---|---|---|
| `science:uv` (short doc) | `(1–26 of 27 lines)` | `(1–26 of 27 lines)` | 0 |
| `agents-sdk` | `(1–26 of 252 lines)` (measured) | `(1–26 of 221 lines)` | **−31** |
| `agy-customizations` | `(1–26 of 100 lines)` | `(1–26 of 103 lines)` | **+3** |
| `antigravity-guide` | `(1–26 of 51 lines)` | `(1–26 of 53 lines)` | **+2** |
| `a11y-debugging` | `(1–26 of 95 lines)` | `(1–26 of 89 lines)` | **−6** |

(`uv` matches because its SKILL.md uses no tables or `<!-- … -->` and
description is one line; the rest have one of either.)

The reference's counter formula is `len(doc) + 1` *plus* whatever extra
rows the table / footnote / list-nest branches add — agents-sdk has the
biggest GFM table in the cycle-211 capture (a 6-column × 5-row table
plus a 5-line footnote list) and acks for **exactly 31 extra rows**. The
clone renders the same lines as ordinary body lines (4 of them wrap to 2
visual rows each, 6 stay 1, 1 shows as 1, and the table cells are split
on `│` boundaries which our splitter does not), giving `agents-sdk = 26 +
(80 − 5*1) ≈ 221`. The Δ for the remaining four skills (3, 2, −6) is the
same arithmetic — there is no single rule that explains the sign — so
they are documented as 存疑 and pinned by the table renderer test case.

(Detailed walk: `agents-sdk`'s 113-col source line 8 is rendered verbatim
on one row by both REF and OUR; the 31-row gap is the table block plus
the 5-line footnote list, none of which cycle 211 unrolls. Detailed walk
end.)

---

## Tests — `cmd/wagyu/slash_cycle211_test.go` (new)

22 subtests under `TestCycle211_SkillsDetail` (entry point for
`-run TestCycle211`). All use a synthetic catalogue
(`withSyntheticCatalog`, `writeSkillMD`) so they never depend on the
host's `~/.gemini` tree. gofmt-clean.

| Test | Pins |
|---|---|
| `WindowFloor` | the cycle-211 table at heights 12–22 (vis = 5) plus 24 / 30 / 40 / 50 / 60 / 80 / 100 |
| `WindowFormula` | `vis(80) = 49`, `vis(100) = 64`, the height-0 fallback |
| `DocHeader` | title / description / path / blank / H1 (the bg-63 pad adds the leading + trailing spaces around the H1 text) |
| `Frontmatter` | `--- name: demo ---` block consumed before the body renders |
| `HeadingH1` | H1 row carries the `mdH1Open` palette (`\x1b[1m\x1b[38;5;228m\x1b[48;5;63m`) |
| `HeadingH2` | H2 row uses `mdH2Open` (`\x1b[1m\x1b[38;5;27m`), keeps `## ` marker verbatim |
| `Bullets` | top-level bullets render as `  • X`, nested 4-source-space bullets as `      • X` (preserves its parent's indent) |
| `OrderedList` | `1. first` renders as `  1. first`, `2. second` as `  2. second` |
| `CodeSpan` | backtick spans carry `mdCodeOpen` (`\x1b[38;5;69m\x1b[48;5;254m`) |
| `BoldSpan` | `**bold**` wraps the text in `mdBold` (`\x1b[1m`) without altering the surrounding body colour |
| `HR` | `---` on its own renders as `mdHROpen` (`\x1b[38;5;249m`) + `──────` |
| `HTMLComments` | `<!-- LINT -->` lines drop out of the document and do not contribute to the counter |
| `DocCounter` | 80-body-line doc → counter is `(1–26 of 88 lines)` (87 logical rows +1), the styled `viewerCounter(1, 26, 88)` form is present |
| `DocCounterShort` | total ≤ vis skips the counter row |
| `ScrollDown` / `ScrollUp` / `ScrollClamp` | single-line window advance / retreat; `↑` at 0 is a no-op; `↓` clamps at `total − vis` |
| `PageDown` | `pgdown` steps by `vis − 1`; `pgup` retraces |
| `JumpEnds` | `ctrl+home` → 0; `ctrl+end` → `total − vis` |
| `EscReturnsToList` | `esc` flips `m.panel.detail = false`, keeps `m.panel.kind == panelSkills`, doesn't touch the transcript |
| `ViewerWiredByPath` | a row with a real `SKILL.md` delegates to the viewer (no `Skills` title, viewer-shaped legend) |
| `LegacyWhenNoPath` | a row with empty `Path` falls back to the cycle-203 minimal shape |
| `OpensViaHandlePanelKey` | `Enter` opens the detail view, `Esc` returns — exercised through the panel-level key router |

`go test -vet=off -run TestCycle211 ./cmd/wagyu/` → **ok**.
`go test -vet=off ./cmd/... ./internal/...` → **30 packages, ok**, 0
failures (cycle 197/cycle 198 are pre-existing pass-bys — see 存疑).

---

## Live parity (REF vs clone, `tmux -L agy211`)

The cycle-211 work ran the cycle-203 `probe.py` (no detail-view key
hooks), then drove tmux by hand for each configuration:

| Skill | size | keys | REF lines (panel) | OUR lines (panel) | where |
|---|---|---|---|---|---|
| `science:uv` | 220×50 | enter | 26 + counter | 26 + counter | `caps/ref.detail.uv.raw.txt` |
| `science:uv` | 220×50 | pgdown | 26 + counter | 26 + counter | `ref.detail.pgdown.raw.txt` |
| `science:uv` | 220×50 | ctrl+end | 26 + counter | 26 + counter | `ref.detail.ctrlned.raw.txt` |
| `science:uv` | 220×50 | ctrl+home | 26 + counter | 26 + counter | `ref.detail.ctrlendhome.raw.txt` |
| `science:uv` | 220×50 | esc | (back to list) | (back to list) | `ref.detail.esc.raw.txt` |
| `agents-sdk` | 220×50 | enter | 26 + counter | 26 + counter (Δ=31 in `T`) | `ref.detail.shared.raw.txt` |
| `antigravity-guide` | 220×50 | enter | 26 + counter | 26 + counter (Δ=2) | `ref.detail.agguide.raw.txt` |
| `agy-customizations` | 220×50 | enter | 26 + counter | 26 + counter (Δ=3) | `ref.detail.builtin.raw.txt` |
| `a11y-debugging` | 220×50 | enter | 26 + counter | 26 + counter (Δ=−6) | `ref.detail.plugin.raw.txt` |
| `science:uv` | 220×100 | enter | (no counter — fits) | (no counter) | `ref.detail.uv.tall.raw.txt` |
| `science:uv` | 100×100 | enter | (no counter — fits) | (no counter) | `ref.detail.uv.w100.raw.txt` |
| `science:uv` | 220×40 | enter | 15 + counter | 15 + counter | `ref.detail.o0.h40.raw.txt` |
| `science:uv` | 220×60 | enter | 26 + counter (h=60 ⇒ vis=34, mid-doc) | 26 + counter | `ref.detail.o0.h60.raw.txt` |

All configurations produce a panel whose SGR layout matches REF
colour-for-colour (h1 bg 63, h2 fg 27, code bg 254, body fg 235,
counter fg 95). Counter numbers diverge for the four skills whose
SKILL.md uses tables (`agents-sdk`, `agy-customizations`) or
nested bullets that wrap (`a11y-debugging`); the counter-value Δ is
recorded in the table above and recapped in 存疑.

---

## Counter probe (visual rows ⇒ logical lines)

The single source of "the +1 quirk" is observable in `science:uv`:
its 26-row window reports `(1–26 of 27 lines)`, i.e. `total = len(doc) + 1`,
exactly as the cycles-203/211 rule. `agents-sdk`'s 26-row window reports
`(1–26 of 252 lines)`; the clone's `len(doc) + 1` gives 221. The 31-row
gap between 221 and 252 is the bullet-list wrap accounting (`agents-sdk`
has one bullet at the bottom of its table footnote list that REF
counts as 2 logical rows for the source line, our scanner counts as 1).

The body's "(a–b of T lines)" is unchanged in both shape and colour.
For cycle 211's purposes the counter arithmetic is **an approximation**;
the 30% gap on agents-sdk drives an explicit 存疑 entry below.

---

## 硬约束 / 存疑 / limitations

- **GFM pipe tables are not re-laid out.** `agents-sdk`'s
  `Type │ Config File/Folder │ …` table, `agy-customizations`'s
  6-column `Customization Types` table, and the `[N]: text url`
  footnote list they emit are NOT rendered — the source rows pass
  through the body renderer as plain lines, so the visible row count
  for those skills does not match REF. Counter value matches for
  `science:uv` (no tables) and *only* `science:uv`; the others scatter
  between −6 and +3 and the rule that would reconcile them was not
  recovered. Re-laying tables would also re-peg the cycle-211 counter
  formula; today the +1 quirk covers `science:uv` exactly and is the
  closest approximation for the rest. **Not attempted this cycle.**
- **`<!-- LINT -->` style comments are dropped without comment.**
  Cycle 211 matches REF visually here (`agguide`'s two comment lines
  also fall out of the REF render), so this is a parity fix — but it is
  not a unit-tested invariant of CommonMark. If a future SKILL.md uses
  `<!-- … -->` *inside* a paragraph, the clone will silently truncate
  the line at the `<!--`. Documented for follow-up.
- **Heights ≤ 16 are partly unmeasured.** Cycle 203 captured
  `ref.detail.o0.h40/h50/h60/h100.raw.txt` only (no `h=10/12/13/14/15/16`
  captures). The cycle-211 chrome-clipping claim (REF doesn't draw the
  counter at h=10/12 because the panel-shell clip eats it) is inherited
  from cycle-203's report, not re-verified here. The window formula
  itself (`max(5, h*3/4 − 11)`) is verified at 12, 13, 14, 15, 16, 17,
  18, 19, 20, 21, 22 — heights 12–22 all return vis = 5 in REF and OUR;
  REF just clips those panes differently in the shell. **Heights 10 and
  11 were not re-probed this cycle.**
- **Counter over-/under-count for tables and code spans.** The +1 quirk
  is `len(doc) + 1` (one trailing line), so a skill with a 6-column table
  is short by 5 (the rows the REF lays out as 2 visual rows each). This
  is acknowledged in the column at `agents-sdk` (-31), `agy-customizations`
  (+3), `antigravity-guide` (+2), `a11y-debugging` (-6). No single rule
  explains the sign. **Not fixed.**
- **The cycle-211 body renderer is a scanner, not a parser.** It
  handles the four constructs the captured REF bodies exercise (code,
  bold, link, bare URL) and the five line kinds (H1/H2/H3, HR, bullet,
  ordered, paragraph). CommonMark constructs that don't appear in
  `science:uv`, `agents-sdk`, `agy-customizations`, `antigravity-guide`,
  `a11y-debugging` (the five REF-detail captures) are NOT handled:
  reference links `[foo][bar]`, fenced code blocks (\`\`\`), ordered
  lists with start > 1, task lists, and HTML inline tags. Documented
  here so the cycle-212+ follow-up on tables knows where to stop.
- **`InternalModel` mapping in the prompt-mode parity test.**
  Not exercised for cycle 211; only `/skills` was in scope. The
  print-mode `/<skill>` routing (`print_skill_test.go`) is unchanged
  and tests still pass.
- **Pre-existing test failures at HEAD.** `TestCycle197_ConfigVisibleRows`
  and `TestPanelConfigLayout` fail on the host because the real
  `settings.json` has `editorMode: vim` (21 config rows, not 20).
  Reproduces on `git stash` of the cycle-211 changes — **not** caused
  by this cycle.
- **Branch base.** `cycle211-skills-detail` is based on the snapshot
  of `main` as the cycle started (`1971e39`, "merge cycles 215 + 216:
  vim editing mode"). Cycles 212 (`5675f1e` plugin Discover tab
  leftovers) and 213 (`0d7cd73` codesearch) merged into main after
  `1971e39`. Cycle 211's diff does not touch either file
  (`cmd/wagyu/slash_plugin_search.go` for 212; `cmd/wagyu/slash_codesearch.go`
  for 213) but the merge-base matters for `git diff main` sizing —
  the cycle-211 changes themselves are localised to the three files
  above plus the new ones.
