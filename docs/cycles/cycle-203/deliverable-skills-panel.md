# Cycle 203 — the `/skills` picker rebuilt on the real catalogue

Target: reference `agy` 1.4.0 (macOS/arm64), TUI, 220 columns unless stated.
All raw captures referenced below live in the cycle-203 probe directory
(`…/agy203/caps/`); the figures come from live `tmux -L agy203` runs of the
reference and of a freshly built clone.

---

## TL;DR

`panelSkillsLines()` (`cmd/wagyu/slash_panels.go`) was a hardcoded stand-in:
the string `"  63 skills"`, one invented group header, eleven invented rows
with a fabricated description, a `> ` cursor on row 0, and a static
`[1-11 of 63 items]`. Cycle 203 rebuilds the panel on the same catalogue the
`-p /skills` print path already byte-matches (`slashSkillCatalog()` =
`printSlashSkillInfos()`), adds the reference's per-root grouping (Shared,
Built-in, one group per plugin), the ` · built-in` suffix, the
`plugin:skill` display names, the height-derived window and the slide-with-
cursor counter.

Live REF-vs-clone capture now agrees **byte-for-byte at every terminal size
≥ 17 rows** tested (18 configurations, 0 mismatching lines), and across the
`↓`/`↑` sequences that move the cursor. The only open deviations are tiny
panes (`height ≤ 16`), where the reference's shell clips the panel
differently — see **存疑**.

---

## What was wrong

| Symptom (pre-203) | Reference behaviour (measured) |
|---|---|
| subtitle `"  63 skills"` (two-space indent) | `"<N> skills"`, no indent, `N` = catalogue size (**57** on this host) |
| a single hardcoded header `Shared skills · From ~/.gemini/skills` | one group per catalogue root: Shared, the builtin bundle, then each plugin, in plugin-name order |
| rows from `skillsFromDisk()` (shared root only), fabricated desc | the full catalogue, real descriptions, ` · built-in` suffix on builtin rows, `plugin:skill` names on plugin rows |
| cursor `"> "` **outside** the colour span, pinned to row 0 | `"› "` **inside** the blue name span, panel opens with **no** row selected |
| fixed 11-row window, static `"[1-11 of 63 items]"` | window derived from terminal height, slides with the cursor |
| no "Create new skills" block | three-line block: `Create new skills` + `Workspace:`/`Global:`/`Shared:` paths |

The `-1` "no selection" state matters for the `enter View` gate too: the old
code forced `cursor = 0`, so `Enter` on the freshly opened panel opened a
detail view. The reference does nothing (`caps/ref.enter.raw.txt`). This cycle
makes `Enter` with no selection a no-op while keeping the cycle-201 detail
view for a selected row.

---

## What changed

| File | Δ | Role |
|---|---|---|
| `cmd/wagyu/slash_skills.go` | **+375** | the whole cycle-203 picker: model, grouping, renderer, cursor, window, counter, detail |
| `cmd/wagyu/slash_panels.go` | +31 / −100 | four localised hooks (below); the old hardcoded body is deleted |
| `cmd/wagyu/slash_panels_test.go` | +5 / −31 | `TestPanelSkillsLayout` reduced to a one-line delegation |
| `cmd/wagyu/slash_cycle203_test.go` | **+486 (new)** | 12 tests pinning the recovered layout and rules |

The four `slash_panels.go` hooks (kept deliberately small):
1. `panelSkillsLines()` → `m.skillsPanelLines()`;
   `panelSkillDetailLines()` → `m.skillsPanelDetailLines()`.
2. `panelRowCount(panelSkills)` → `len(skillsPanelCatalog())` (was `11`).
3. `handlePanelKey`: an `↑`/`↓` branch calling `m.moveSkillsCursor(down)`.
4. `openSlashPanel("skills")` sets `m.panel.cursor = -1`;
   `openSkillDetail()` no-ops when the cursor is `< 0`.

New file contents: `fmt`/`os`/`path/filepath`/`sort`/`strings` imports and the
types `skillsPanelGroup` / `skillsPanelItem` / `skillsPanelModel`, the funcs
`newSkillsPanelModel`, `skillsPanelGroups`, `skillsPanelCatalog`,
`skillsGroupDir`, `tildeHome`, `skillsVisibleItemsFor`,
`skillsPanelWorkspacePath`, `skillsPanelRow`, `skillsPanelLines`,
`moveSkillsCursor`, `skillsPanelDetailLines`.

---

## Evidence — probe → raw line

Every line below is the literal `capture-pane -p -e` text (SGR intact).

**Title / summary / create block** (`caps/ref.h50.raw.txt`)
```
\x1b[1m\x1b[39mSkills\x1b[0m
\x1b[38;5;241m57 skills\x1b[39m
<blank>
\x1b[1mCreate new skills\x1b[0m
  \x1b[38;5;241mWorkspace: /private/var/…/agy203/.agents/skills/{skill_name}/SKILL.md\x1b[39m
  \x1b[38;5;241mGlobal:    ~/.gemini/antigravity-cli/skills/{skill_name}/SKILL.md\x1b[39m
  \x1b[38;5;241mShared:    ~/.gemini/skills/{skill_name}/SKILL.md\x1b[39m
```
Note the two-space indent sits **outside** the grey span (found and fixed
mid-cycle), and the subtitle has **no** indent.

**Group headers**
```
\x1b[1mShared skills · \x1b[38;5;238mFrom ~/.gemini/skills\x1b[0m
\x1b[1m/Users/gear/.gemini/antigravity-cli/builtin/skills · \x1b[38;5;238mBuilt-in\x1b[0m
\x1b[1m/Users/gear/.gemini/config/plugins/chrome-devtools-plugin/skills · \x1b[38;5;238mFrom ~/.gemini/config/plugins/chrome-devtools-plugin\x1b[0m
```
i.e. `bold "<left>" · <238>"<label>" <reset>`; `left` is the literal
`"Shared skills"` for the shared root but the **absolute** skills directory
for builtin/plugin, and `label` is `Built-in` or `From <dir>` with `$HOME`
shortened to `~`. A blank row precedes every header except the first rendered
one.

**Skill rows** — plain then cursor (`caps/ref.down1.raw.txt`)
```
  \x1b[38;5;69magents-sdk\x1b[39m: \x1b[38;5;238mBuild AI agents …voice agents...\x1b[39m
\x1b[38;5;69m› agents-sdk\x1b[39m: \x1b[38;5;238mBuild AI agents …real-ti...\x1b[39m
```
Name in palette 69, description in 238, `"<name>: <desc>"`. The cursor row
moves the `"  "` prefix **into** the blue span as `"› "`. Descriptions are cut
**mid-word** (`real-ti` + `...`) so the whole rendered line is ≤ `width-1`
columns; builtin rows reserve a trailing
`" · \x1b[3m\x1b[38;5;241mbuilt-in\x1b[0m"` before the cut. (Careful when
measuring: `·` is 2 bytes / 1 column — a byte count over-reports by one.)

**Counter** (`caps/ref.h50.raw.txt`)
```
\x1b[38;5;241m  [1-26 of 63 items]\x1b[39m
```
Spans are `241`; the two-space indent is **inside** the span. Shown only while
the window is shorter than the item list; **N = skills + headers = 57 + 6 =
63**. No blank between the last visible item and the counter.

**Legend** (`caps/ref.h50.raw.txt`, via `panelKeyboardHint`)
```
\x1b[1mKeyboard: \x1b[0m\x1b[38;5;69m↑/↓\x1b[39m \x1b[38;5;241mNavigate\x1b[39m  \x1b[38;5;69menter\x1b[39m \x1b[38;5;241mView\x1b[39m  \x1b[38;5;69mesc\x1b[39m \x1b[38;5;241mCancel\x1b[39m
```

**Cursor semantics** (`caps/ref.down*.raw.txt`, `caps/ref.up*.raw.txt`)
The panel opens with **no** cursor. `↓` from `-1` selects skill 0; both keys
then clamp (eight `Down,Up` trials across 140/220 × 24/50 all left the cursor
on `agents-sdk`, so `↑` on the first skill stays put — it does not return to
`-1`). Repeated `↑` from rest leaves the panel unselected. `Enter` with no
selection is a no-op.

**Workspace path** (`caps/ref.cwd-tmp.raw.txt`) — the reference prints the
*logical* cwd (`/tmp`, not `/private/tmp`); Go's `os.Getwd()` honours `$PWD`
the same way, so the clone needed no special casing.

---

## The fitted height → window rule

`n = height*3/4 − 11` (integer division), floored at 1:

| height | 16 | 17 | 18 | 20 | 24 | 28 | 30 | 32 | 36 | 40 | 44 | 46 | 50 | 60 | 70 | 80 | 98 | ≥99 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| items | 1 | 1 | 2 | 4 | 7 | 10 | 11 | 13 | 16 | 19 | 22 | 23 | 26 | 34 | 41 | 49 | 62 | 63 (=all) |

Every sample satisfies the formula. The window start is
`max(0, cursorItem − (n−1))`, so the cursor's row is always the last visible
one while scrolling; the counter then reads `[start+1 − end of N items]`.

For `height == 0` (no `tea.WindowSizeMsg` yet) the function falls back to the
220×30 class-A default → `n = 11`, reproducing the `ppref.skills` snapshot the
byte-exact tests assert.

---

## Live parity (REF vs clone, `tmux -L agy203`)

`OPENAGY_BACKEND=agw` (reference) vs `OPENAGY_BACKEND=mock` (clone), same cwd,
same key sequence, raw SGR compared line-for-line. Each configuration is the
best of up to three attempts (the harness occasionally drops a key — see 存疑).

| size | keys | REF lines | OUR lines | mismatches |
|---|---|---|---|---|
| 220×50 | — | 42 | 42 | **0** |
| 220×50 | ↓ | 42 | 42 | **0** |
| 220×50 | ↓×9 | 42 | 42 | **0** |
| 220×50 | ↓×12 | 42 | 42 | **0** |
| 220×50 | ↓,↑ | 42 | 42 | **0** |
| 220×50 | ↑×3 | 42 | 42 | **0** |
| 220×50 | ↓×57 (clamp at end) | 37 | 37 | **0** |
| 220×24 | — | 18 | 18 | **0** |
| 220×24 | ↓×9 | 18 | 18 | **0** |
| 220×20 | — | 15 | 15 | **0** |
| 220×18 | — | 13 | 13 | **0** |
| 220×17 | — | 12 | 12 | **0** |
| 220×32 | — | 25 | 25 | **0** |
| 220×36 | — | 29 | 29 | **0** |
| 220×40 | — | 32 | 32 | **0** |
| 220×70 | — | 57 | 57 | **0** |
| 220×98 | — | 78 | 78 | **0** |
| 220×100 | — | 78 | 78 | **0** |
| 100×50 | — | 42 | 42 | **0** |
| 100×50 | ↓ | 42 | 42 | **0** |

*(An earlier `100×50 ↓` run also matched; `220×80` was not run.)*

---

## Tests

`cmd/wagyu/slash_cycle203_test.go` (new, gofmt-clean). All use a synthetic
catalogue (`withSyntheticCatalog`) so they never depend on the host's
`~/.gemini` tree, except the one smoke test which skips when the plugin bundle
is absent.

| Test | Pins |
|---|---|
| `TestCycle203_SkillsPanel` | entry point running the whole family under `-run TestCycle203` |
| `TestSkillsPanelGroups` | the group split and each header's `left · label` strings |
| `TestSkillsPanelLayout_ByteExact` | the **entire** panel, every SGR byte, at a height where everything fits |
| `TestSkillsPanelCounts` | `N = len(skills)`; items `= skills + groups`; skill→item index map |
| `TestSkillsVisibleItemsFor` | the height→window table incl. the floor and the height-0 default |
| `TestSkillsPanelCounterAndWindow` | `[1-N of M items]` text, its `241` span, top and scrolled placements, and its absence when everything fits |
| `TestSkillsPanelRow_Truncation` | mid-word `...` cut, `width−1` budget, builtin-suffix reservation, rune-vs-byte width |
| `TestSkillsPanelRow_CursorSpan` | `› ` inside the blue span vs the plain prefix outside it |
| `TestSkillsPanelCursorKeys` | `-1` initial; `↓` from `-1` → 0; clamps at both ends; `↑` on the first skill stays |
| `TestSkillsPanelEnterGate` | `Enter` no-ops at `-1`, opens on a selection, `esc` returns; `panelRowCount` |
| `TestSkillsPanelOpensUnselected` | `openSlashPanel("skills")` leaves the cursor at `-1` |
| `TestSkillsPanelLayout_HostCatalogue` | live catalogue smoke test: 57 skills / 63 items / 6 groups, 220×50 counter, no over-wide rows |
| `assertPanelSkillsLayout` | backs the one-line `TestPanelSkillsLayout` delegation in `slash_panels_test.go` |

`go test -vet=off -run TestCycle203 ./cmd/wagyu/` → **ok**.

---

## 硬约束 / 存疑 / limitations

- **Heights ≤ 16 are not reproduced.** At 220×15 and 220×16 the reference's
  shell clips the legend (the clone's panel starts one row higher, so the
  legend fits): 1 mismatching line each. At 220×13/14 the reference capture is
  unreliable at this pane size (it returned the startup banner / a partial
  panel — the harness's `Enter` or the panel did not land), so those numbers
  are not evidence either way. The *content model* still follows `n = h*3/4−11`
  floored at 1; only the shell's viewport/clipping at ≤ 16 rows is unmodelled.
  Parity is verified for **height ≥ 17**.
- **The full `SKILL.md` detail viewer is not reproduced.** `Enter` on a
  selection shows the reference's real document viewer (title, wrapped
  description, path, the file body with a `(1–7 of 252 lines)` window and the
  legend `↑/↓ scroll · pgup/pgdown page · ctrl+end bottom · ctrl+home top ·
  esc back`; `caps/ref.down_enter.raw.txt`). Cycle 203 keeps the cycle-201
  minimal shape, now backed by the **selected catalogue entry**. Its layout is
  【推测】; the name/description/path values are 【一手】.
- **The reference's catalogue is not perfectly stable.** In 2 of ~10 runs
  `/skills` reported **60** skills, having surfaced the workflow builtins
  (`automation`, `migrate-workflows`, `run-workflow`) that the steady state
  hides. The clone follows `printSlashSkillInfos()`' steady-state set (**57**),
  which byte-matches the reference at rest and the `-p /skills` output.
  Modelling the 60-skill transient was out of scope.
- **Harness key drops.** The tmux capture occasionally loses a `↓` (the
  reference then shows no cursor, or a stale panel). Every parity figure above
  is the best of up to three attempts; the raw first-attempt failures were all
  attributable to this.
- **`skillsFromDisk()` is now dead in production** (only the pre-existing
  `TestPanelSkillsDetailView` still calls it). Left in place to keep the diff
  to `slash_panels.go` minimal.
- **Unrelated pre-existing test failures.** `TestCycle197_ConfigVisibleRows`
  and `TestPanelConfigLayout` fail at `HEAD` on this machine because the real
  `settings.json` has `editorMode: vim` (21 config rows, not 20). Verified by
  stashing the cycle-203 changes; **not** caused by this cycle and deliberately
  not "fixed".
- **Not verified:** heights between the sampled points (14, 19, 21–23, 25–27,
  29, 31, 33–35, 37–39, 41–43, 45, 47–49, 51–59, 61–69, 71–79, 81–97) are
  covered only by the arithmetic; no live capture was taken for them.
