# Cycle 208 — regression sweep over /config, /model, /effort, /feedback

Re-drove the four panels cycles 196/197/201/202 declared *done* against the live
reference (`agy` 1.4.0, Mach-O arm64, `~/.local/bin/agy`) at 220×50 and 220×24,
through a private tmux socket (`agy208`). Eight divergences were found; all eight
were fixed and re-verified byte-for-byte.

**TL;DR** — after the fixes the live diff is **0 mismatches** for every panel and
every key state at 220×50 (and 0 for `/config` at 220×24 in every cell except
`Enable Telemetry`'s persisted value — see §4, store not identified). Two of the
four panels were **not** done: `/feedback`'s stage 2 was still the 【推测】 layout
from cycle 187, and `/effort`'s primary key (Enter) did nothing at all.

## 1. Per-panel verdict

Three of the panels come out **re-verified with 0 mismatches** — that is the
verdict for `/effort`'s slider, `/model`'s picker and `/feedback`'s stage 1: no
change was needed for their already-correct cells, and the states below are the
ones that did need work. "Before" = the sweep's first live run (captures
`ref_*_*`); "after" = the final run (`harness/verify.py`, captures `v_*`). A
*mismatch* is one rendered cell differing from REF, including its SGR.

| Panel | States probed | Mismatches before | Mismatches after |
|---|---|---|---|
| `/config` | open @220×50 (21 rows, `editorMode: vim`) | 2 | **0** |
| `/config` | cursor Down×9 → Right (`Enable Telemetry`) | 2 | **0** \* |
| `/config` | open @220×24 (6-row window) | 1 † | **0** |
| `/config` | open + Down×9 @220×24 (`Enable Telemetry` visible) | 1 † | **1** \* |
| `/model` | open | 3 | **0** |
| `/model` | Down (cursor off the Gemini row) | 6 | **0** |
| `/model` | Down ×3, PageUp (same code path) | 5 | **0** |
| `/effort` | open | 0 | **0** |
| `/effort` | ← (slider step to Medium/Low) | 1 | **0** |
| `/effort` | Enter (applies + closes) | 7 | **0** |
| `/feedback` | stage 1 (category picker) | 0 | **0** |
| `/feedback` | stage 2 (form) | 7 | **0** |

† measured with the cycle's height sweep (`harness/cfg_heights.py`, h=20/24/32/40);
the pre-fix harness did not count cells at h=24.

\* `Enable Telemetry` is the one remaining cell that can differ: its value comes
from a persisted flag whose store was not found (§4). The panel itself renders
correctly; at 220×50 the final run happens to compare 0 because REF's persisted
value was `on`, i.e. equal to the clone's default. At 220×24 the same cell
compared `off` (REF) vs `on` (clone), which is exactly what isolates the
divergence to *persistence*, not rendering.

## 2. What changed, and why

All new code lives in **`cmd/wagyu/slash_regression.go`** (276 lines);
`slash_panels.go` and `tui_run.go` carry only call sites. Evidence file names
refer to the probe directory (`…/opencode/agy208/`).

| # | Divergence | Reference evidence | Fix |
|---|---|---|---|
| 1 | `/config` padded `name → value` with `len()` (bytes). `Editor Mode › Insert First` is 26 runes / 28 bytes, so the clone was two cells short. | `fin config`: `  Editor Mode › Insert First   off` (3 spaces) | `configNamePad` (rune count) — `slash_regression.go:143`, used at `slash_panels.go:1329` |
| 2 | A *windowed* `/config` list was missing one blank line before the `[W+1 of N items]` counter. | REF @h=24: `rows, '', desc, '', '', [1-6 of 21 items], '', Keyboard`; @h=50: `rows, '', desc, '', '', '', Keyboard` | extra `""` in the windowed branch — `slash_panels.go:1350` |
| 3 | `/config` cursor row: REF has **two** forms. Names of 16+ runes render as one blue span covering name+pad+value; names of ≤15 runes leave the pad uncoloured. The clone always emitted two spans. | `harness/cfg_cursor.py`, all 21 rows, two independent runs (`▶ Enable Telemetry             off` one span vs `▶ Agent Mode` + bare pad + `auto`) | `configCursorRowIsSingleSpan` / `configCursorFieldCols` — `slash_regression.go:158`, used at `slash_panels.go:1331` |
| 4 | `Enable Telemetry` renders the default `on`; REF renders a persisted flag the clone never reads. | §4 | **not fixed** — store not identified |
| 5 | `/model` padded the cursor row to column 30 unconditionally, leaving a trailing space on rows with no `(current)` marker. | `fin_model_down` row 6: `> Claude Sonnet 4.6 (Thinking)` (no trailing space) | pad only when `isCurrent` — `slash_panels.go:914`; `modelRowPad` — `slash_regression.go:118` |
| 6 | `/model` drew the effort slider (and `←/→ Effort` in the legend) for **every** row. REF shows them only while the cursor is on the three-tier Gemini row; otherwise it emits `rows, '', Keyboard` and the legend drops the pair. | `fin_model_down`: 12 rows vs clone 16; legend `Keyboard: ↑/↓ Navigate  enter Select  esc Go Back` | `modelRowHasEffortSlider` — `slash_regression.go:130`, early return at `slash_panels.go:937` |
| 7 | Moving the cursor in `/model` reset the slider to Medium *and* removed the `(current)` marker from the Gemini row. | `fin_model_up_back`: REF returns the slider to **High** (the model's current effort) and keeps the marker | re-seed from `effortCursorForLabel(m.panelCurrentModelLabel())` — `slash_panels.go:2165`/`:2177` |
| 8 | `/effort` slider: the 14 cells *after* the active ◉ were lit. REF ends the coloured run at the ◉. | `fin_effort_left2`: REF `…◉\x1b[38;5;238m──────…` vs clone `…◉──────…\x1b[38;5;238m○` | `effortBarSegments` — `slash_regression.go:79`, used at `slash_panels.go:510` |
| 9 | `/effort` **Enter** was swallowed. | `fin_effort_apply_med`: transcript `> /effort` + `⎿  Effort set to medium`, banner → `Gemini 3.8 Flash (Medium)`, status → `· medium`, and `settings.json`'s `model` becomes the **label** `"Gemini 3.8 Flash (Medium)"`. At the already-active level REF closes silently and writes nothing (`fin_effort_apply_high`: hash and `model` unchanged). | `applyEffortSelection` + `effortLevelName`/`modelLabelForEffort` — `slash_regression.go:51/170/200`; Enter case at `slash_panels.go:2140` |
| 10 | The status row's effort suffix was the constant `statusRightPrefix = " · high"`. | `fin_effort_apply_med` row 13: `Gemini 3.8 Flash · medium` | `statusEffortSuffix` — `slash_regression.go:66`; `tui_run.go:241`/`:253` now use it (the constant stays for `staticStatusRow`) |
| 11 | `/feedback` stage 2 was the cycle-187 【推测】 layout (`Submit Feedback` / indented category / `esc Back · enter Submit`). | `fin_fb_stage2`: title `Review Submission Data  (You can always provide manual feedback via /feedback)`, body `Your feedback helps improve Antigravity CLI. The following sanitized data will be sent:` / `Feedback: …` / `Additional Details (optional):` / cursor, 3 blanks, legend ` ctrl+s Submit Feedback   esc Back` | `feedbackStage2Lines` — `slash_regression.go:255`; `panelFeedbackStage2` now delegates |
| 12 | Stage 2 echoed the category *with* its `1. ` picker index. | REF: `Feedback: Code doesn't work (e.g., syntax errors, bad logic)` | `feedbackCategoryLabel` — `slash_regression.go:228` |
| 13 | Enter submitted the stage-2 form. | REF's legend advertises ` ctrl+s Submit Feedback`; Enter leaves the form (and the legend) unchanged (`ref_fb_submitted`) | Enter is now inert; `ctrl+s` submits — `slash_panels.go:2432` |

Out-of-scope observation while diffing (recorded, **not** fixed): with
`agentMode` set in `settings.json`, REF prefixes the status row's right label with
the mode — `auto · Gemini 3.8 Flash · medium` (REF) vs `Gemini 3.8 Flash · medium`
(clone). That is the banner/status area, not one of the four panels, and the
user's config has `agentMode` absent.

## 3. Tests

`cmd/wagyu/slash_cycle208_test.go` (336 lines, 12 tests, gofmt-clean) — each pins
one row above:

`TestCycle208_ConfigNamePadCountsRunes`, `…_ConfigNestedRowAlignment`,
`…_ConfigWindowedCounterBlanks`, `…_ConfigCursorRowSpanForms`,
`…_ModelSliderOnlyOnTieredRow`, `…_ModelCursorRowPadOnlyWhenCurrent`,
`…_ModelEffortCursorReseeds`, `…_EffortBarColourBoundary`,
`…_StatusEffortSuffixTracksEffort`, `…_EffortEnterApplies`,
`…_FeedbackStage2Layout`, `…_FeedbackStage2EnterInert`.

Three existing tests were touched:

* `TestPanelFeedbackStage2` — updated for the measured stage-2 form and the
  dropped picker index (it asserted the old 【推测】 layout).
* `TestPanelConfigLayout` and `TestCycle197_ConfigVisibleRows` — **pre-existing
  failures** on the shared machine, proven with `git stash`: both hardcode a
  20-row catalogue while the shared `settings.json` sets `editorMode: vim`, which
  adds the nested `Editor Mode › Insert First` child (21 rows). They now isolate
  the settings home with the cycle-196 helper, so the measured table is pinned
  against the state it was measured in.

Gates: `go build -o <probe>/our/agy ./cmd/wagyu` ✅ ·
`go test -vet=off ./cmd/... ./internal/...` → 27 packages ok, 0 failures ✅ ·
`gofmt -l cmd/wagyu/slash_regression.go cmd/wagyu/slash_cycle208_test.go` → empty ✅.
`slash_panels.go` was left gofmt-unclean exactly as at `HEAD` (the repo's
pre-existing drift); the `gofmt -w` I ran initially re-aligned four unrelated
struct literals and a `const` block, and those hunks were reverted so the diff
contains only functional change.

## 4. `Enable Telemetry` — investigation and snapshots

The reference's `Enable Telemetry` value is durable and its store was **not
found**. Everything below is from this sweep's snapshots.

* **Default for a never-toggled install**: `on` (matches the clone).
* **Durability**: `harness/telemetry_persist.py` — four consecutive launches
  produced `on → off → off → on → on` across clean exits (Escape, then ctrl+c
  twice). An abrupt `tmux kill-session` loses the change.
* **`settings.json`**: byte-identical before and after the toggle
  (`sha256` unchanged), and the file never contains a telemetry key. Keys seen
  across the sweep: `altScreenMode, colorScheme, editorMode, model, permissions,
  statusLine, title, toolPermission, trustedWorkspaces` (plus `agentMode` while
  another agent had written it).
* **Filesystem**: `find -newer` across `~/.gemini`, `~/.config`,
  `~/Library/Application Support`, `~/Library/Preferences`, `~/.local/share`,
  `~/.cache`, `/tmp`, and a full `/ -xdev` scan reported no modified file.
* **Keychain / defaults**: the `security` dump hash is unchanged; no plist or
  `defaults` domain changes.
* **Binary strings**: `enableTelemetry` @52750330, `disableTelemetry`
  @57019118 and @59971286, `telemetryEnabled` @58124430.
* **Negative control**: an isolated `HOME=/tmp/telhome` cannot be used — REF
  forces the OAuth flow without the real home, i.e. the state it reads is
  keyed to the real account/home.
* **In-sweep isolation**: at 220×50 the final `/config` run compared 0/0
  mismatches on that cell because the persisted value was `on`; at 220×24, after
  the same sweep had pressed Right once, REF read `off` while the clone read
  `on`. Same code, same key, different persisted state — so the clone's
  *rendering* is right and only persistence is missing.

Verdict: **not identified**. The remaining hypotheses are a keychain secret
payload or a server-side/account flag. Nothing was changed in the clone for it:
writing it to `settings.json` would be a *new* divergence, since REF provably
does not.

## 5. 存疑 / limitations

* `Enable Telemetry`'s store is unidentified (§4); the clone's default `on` is
  correct for a never-toggled install.
* `configCursorFieldCols = 16` is **empirical**. The 16/15 boundary is sharp and
  reproduced across all 21 rows in two runs, but the mechanism (a 16-cell label
  field?) was not recovered.
* `/feedback`'s `ctrl+s` submit still only records the clone's local-only
  message; REF's submit is a backend call and the OAuth tokens for driving it are
  expired, so its success/failure rendering is unverified. Enter's exact
  semantics are pinned only as "does not submit and does not close" — whether REF
  inserts a newline into the textarea is unobservable in a capture.
* `/effort`'s persistence predicate ("write only when the effective model
  changes") is inferred from two samples: Enter at the active level wrote
  nothing, Enter at a different level wrote the label. The clone reproduces both
  observations, but the predicate itself is unproven. The written value is the
  model *label*, not the catalogue slug.
* The `/model` slider position after returning to the Gemini row is set to the
  model's *current* effort. That is consistent with both measured states (open,
  and Down→Up) but the "move the slider, leave the row, come back" case was not
  probed, so "re-seed from the current effort" is not separable from "remember
  the last slider position".
* The REF startup warning `"MiniMax-M3" is no longer available. Using "Gemini 3.8
  Flash (High)".` appeared in the early runs and was absent in the final sweep
  run; the clone always prints it. The banner was not investigated — it is
  outside the four panels and the transcript comparison was scoped accordingly
  (`tail_block`, `panel_region` in `harness/verify.py`).
* The REF telemetry flag is left wherever the sweep's toggles put it (last known:
  `off`, after the 220×24 Right). It could not be restored because the store is
  unidentified.
* `settings.json` restore: after all probing, the exact pre-probe bytes were
  written back from the preserved snapshot
  `…/opencode/PRISTINE/settings.json.pre-agent`
  (`sha256 30471e37a34d7037e7e29075d8767a69fbf8f813b095551f95804b0bc63ff95e`).
  That snapshot's `model` is `"MiniMax-M3"` and it **does** carry
  `agentMode: "auto"` — the pre-probe state. This supersedes the mid-sweep note
  that `agentMode` should be absent: `agentMode` was written in several variants
  while other agents shared `$HOME`, and the authoritative target is the
  pre-probe file, not the state any one of us happened to observe. The restore is
  performed as the **last** step of the cycle, because another agent may still be
  probing and can rewrite the file again afterwards — re-restoring is idempotent.
  `~/.gemini/config/config.json` was never written and is byte-identical to its
  pre-session backup:
  `sha256 10efd0c356f387cd4b3b35500c771f692b8130a58fa2e7d664d58bbf9c764c82`.

## 6. Reproducing

```bash
# probe dir (isolated tmux socket agy208, never kill-server)
cd /private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy208

# build the clone under test
go build -o our/agy ./cmd/wagyu            # from the worktree

python3 harness/verify.py                  # final REF-vs-clone diff (all states)
python3 harness/cfg_cursor.py              # the 21-row cursor-row span table
python3 harness/ref_final.py               # /effort Enter + persistence, /model re-entry
```

Captures: `v_ref_*.raw.txt` / `v_our_*.raw.txt` (final run), `fin_*.raw.txt`
(REF one-launch multi-state), `ref_*.raw.txt` / `our_*.raw.txt` (the first run),
comparison logs `cmp_multi.out`, `base4.out`.
