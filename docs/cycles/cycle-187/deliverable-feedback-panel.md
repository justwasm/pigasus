# Cycle 187 — /feedback stage-1 category picker

## Why

Cycle 184's inventory put `/feedback` in class A: REF renders a full-screen
picker, OUR printed a single text line. Cycle 185 added the panel framework
but only the nine statically-rendered ones; cycle 187 closes `/feedback`
stage 1 (category picker) on top of that framework.

REF's `/feedback` is a two-stage interaction: stage 1 lets the user pick a
category (six numbered options), then Enter transitions to stage 2
("Review Submission Data" — a free-form textarea + `ctrl+s Submit`).
Cycle 187 ships stage 1; selecting a category and pressing Enter simply
closes the panel silently because there is no server upload endpoint in
`openagy` and stage 2 would require a textarea we don't have.

## 1. Evidence

Captured at 220×30 on REF v1.4.0
(`/tmp/agycmp/panels/ppref.feedback.raw.txt`,
`/tmp/agycmp/panels/ppfb_open.raw.txt`,
`/tmp/agycmp/panels/ppfb_wrap.raw.txt`,
`/tmp/agycmp/panels/ppfb_afteresc.raw.txt`):

```
Submit Feedback

What went wrong? Select a category to assist your report:

> 1. Code doesn't work (e.g., syntax errors, bad logic)
  2. Repeated itself (e.g., stuck running the same tool over again)
  3. Took too many steps (e.g., progress was much too slow)
  4. Ignored instructions (e.g., missed specific constraints)
  5. Got stuck (e.g., unable to recover from an error)
  6. Other

  esc Cancel · 1-6 Select & Continue · enter Continue
```

Three behaviours not shared by the cycle-185 panels:

| Behaviour | Where | Notes |
|---|---|---|
| ↑/↓ wrap at both ends | Down 1→6, 6→1, Up 1→6 | every other panel clamps |
| Esc closes silently | `ppfb_afteresc.raw.txt` | same `exitNotice()==false` set as `/mcp` `/permissions` `/codesearch` |
| Dot hint (no "Keyboard:" prefix) | last row | like `/hooks`, unlike the "Keyboard:" rows on `/mcp` etc. |

## 2. Change

`cmd/wagyu/slash_panels.go`

- New `panelFeedback` constant in `panelKind`.
- New `feedbackCategories` table (six static rows).
- New `panelFeedbackLines()` method (12 rows: title + blank + prompt + blank
  + 6 categories + blank + dot hint).
- New `panelKind.wraps()` accessor — true only for `panelFeedback`.
- `panelForCommand` learns `"feedback"` → `panelFeedback`.
- `panelRowCount` learns `panelFeedback` → 6.
- `panelLines` switch dispatches to `panelFeedbackLines`.
- `exitNotice()` adds `panelFeedback` to the silent-close set.
- `handlePanelKey`'s ↑/↓ case uses `wraps()` to wrap instead of clamp.
- Header comment bumped from "cycle 185" to "cycles 185 + 187".

`cmd/wagyu/slash_panels_test.go`

- `TestPanelForCommand` extended with `"feedback"` → `panelFeedback`.
- `TestPanelFeedbackLayout` pins the 12 rows as plain text and checks
  that the selected row carries `cBlueOpen`/`cBlueClose` while every
  other category row does not, and that the dot-hint's key glyphs use
  `panelBoldBlueOpen`.
- `TestHandlePanelKeyFeedbackWraps` drives Up/Down through all six rows
  and checks the wrap at both ends.
- `TestHandlePanelKeyHooksClamps` regresses the cycle-185 clamping
  behaviour on `/hooks` so the new `wraps()` accessor doesn't
  accidentally widen the clamp-vs-wrap rule.
- `TestFeedbackExitNotice` pins `panelFeedback.exitNotice() == false`.

`TODO.md`

- §2.1 row for `/feedback` now struck through and marked ✅ cycle 187.
- §2.1 top note adds `✅ Cycle 187 已关闭: /feedback stage 1`.
- §4 已关闭 table adds the cycle-187 row.
- §5 P1 list removes `/feedback`.

## 3. Verification

```
go test ./cmd/wagyu/ -run 'TestPanelFeedback|TestHandlePanelKeyFeedback|
                            TestHandlePanelKeyHooks|TestFeedbackExit|
                            TestPanelForCommand|TestPanelExit' -count=1
# 6 tests pass

python3 /tmp/agycmp/cmp_panels.py \
  mcp tasks effort hooks permissions agents credits codesearch artifact feedback
# 10/10 IDENTICAL (SGR-normalised)

python3 /tmp/agycmp/ppfb_our.py   # captures OUR /feedback
python3 /tmp/agycmp/cmp_panels.py feedback   # IDENTICAL

# ppour.feedback.esc.raw.txt shows status-row → ? for shortcuts,
# no "⎿  Exited /feedback command" line
```

The two unrelated failures
(`TestSlashToggles_Persist`, `TestResume_PopulatesFromStoreDir`) are
the concurrent agent's uncommitted `SetSettingsString` → `SetSettingsBool`
rewrite and `/resume` picker work, not from this cycle. Confirmed by
`/tmp/headcheck/` (clean HEAD tree) passing all 27 packages green.

## 4. Not done

- **Stage 2 of `/feedback`** (the "Review Submission Data" textarea with
  `ctrl+s Submit`). Selecting a category and pressing Enter currently
  closes the panel silently because we have no upload endpoint; the
  stage-2 transition (textarea + Submit key + Esc Back) is a separate
  cycle.
- **`/feedback` Enter → "Submitted" echo.** Without stage 2 and without a
  backend, this would just be a lie. If the user wants stage 1 to echo a
  fake "Feedback saved" line, that can be a one-line follow-up.
- **The remaining 9 P1 panels** — `/model`, `/config`, `/plugin`,
  `/skills`, `/usage`, `/context`, `/changelog`, `/diff`, `/rewind`.
  Each needs its own catalogue (model list, settings list, disk-loaded
  skills, quota, etc.) and is bigger than `/feedback`'s static list.
- **C text gaps** (`/artifact`, `/fork`, `/compact`, `/voice`,
  `/remote-control`, `/btw` indent) and **B echo format** still need
  `cmd/wagyu/slash_commands.go`, which the concurrent agent is editing
  this cycle.
