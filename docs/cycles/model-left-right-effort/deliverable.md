# Cycle `feature/model-left-right-effort` — /model ←/→ wraps effort and persists immediately

**AGY 1.3.6 changelog line 14 (TODO.md P4):**
> "/model ←/→ switches effort level (low / medium / high) and persists immediately."

**Branch:** `feature/model-left-right-effort` (forked from main at `10a4d31`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-model-left-right-effort/`

## TL;DR

`runMode.cycleModelEffort` (cmd/wagyu/slash_panels.go:3633) now:

1. Wraps the effort cursor across both boundaries — pressing → at high
   lands on low, and ← at low lands on high — instead of clamping.
2. Persists the new `(model, effort)` slug to `settings.json` on
   **every** ←/→, exactly the same key/value pair `selectModelEffort`
   would write on `Enter`.
3. Updates `m.model` and `m.statusModel` from the catalogue entry so
   the status row reflects the new tier on the very next frame.

The picker slider redraws without any TUI change because
`panelModelLines` reads `m.panel.effortCursor` on every render through
the same `rebuildEffortSlider` helper `/effort` already uses; the
catalogue `ID` is the canonical `<base>-<tier>` form (`agy -p /model`
emits it, `modelAvailable()` matches against it).

The pre-existing `TestCycle197_ModelEffortCyclesGeminiOnly` was
updated to reflect the new boundary behaviour: "clamp at high" no
longer applies; the test now confirms high → low wrap on the
`+1`-on-high edge and low → high wrap on the `-1`-on-low edge. Non-
Gemini rows remain inert.

## Why

TODO.md §P4 names this as one of the lingering small fixes — "/model
←/→ (切换 effort) still inert". Inert is too strong a word (the
slider did move), but the persistence was an Enter-only side effect,
so a user that previewed the effort with ←/→, hit Esc or moved away
from `/model`, and then re-opened the panel saw their preview wiped:

| Action | Pre-cycle-197 | Cycle 197 (prior to this change) | This cycle |
|---|---|---|---|
| Open `/model`, cursor on Gemini | slider at persisted effort | slider at persisted effort | slider at persisted effort |
| Press → on Gemini (medium → high) | slider at high, settings.json unchanged | slider at high, settings.json unchanged | slider at high, **settings.json** = `gemini-3.8-flash-high`, status row = `Gemini 3.8 Flash (High)` |
| Press Esc, then re-open `/model` | slider back at saved effort | slider back at saved effort | slider stays at high (just persisted) |
| Press → at high | slider clamps at high (no-op) | slider clamps at high (no-op) | slider **wraps** to low |
| Cursor on Claude Sonnet | ←/→ inert | ←/→ inert (still no model write) | ←/→ inert (still no model write) |

The reference appears to wrap rather than clamp — `/effort`'s slider
behaviour, `/config`'s left/right cycling, and the user expectation
from the inline slider agree — so the boundary case became "always
move".

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/slash_panels.go` | +33 / −9 | `cycleModelEffort` now wraps and persists; doc-comment refresh |
| `cmd/wagyu/slash_cycle197_test.go` | +11 / −5 | `TestCycle197_ModelEffortCyclesGeminiOnly` re-pinned to wrap behaviour |
| `cmd/wagyu/slash_cycle_model_left_right_effort_test.go` (new) | +387 / −0 | 8 tests covering advancement, persistence, label update, slider rerender, no-dropdown, boundary wrap, non-Gemini inertness, key-handler integration |
| `docs/cycles/model-left-right-effort/deliverable.md` (this file) | — | TL;DR, scope, helpers, gates, 存疑 |

**Total:** 2 source files changed + 1 new test file + this deliverable.

## How the new cycle decides what to do

```
cycleModelEffort(dir) — dir ∈ {-1, +1}
            │
            ▼
   row = modelRows[m.panel.cursor]
            │
   row.slug != "gemini-3.8-flash"? ─yes─► return (cursor on Claude/Argon/etc.)
            │ no
            ▼
   c = m.panel.effortCursor + dir
            │
   c < 0? ─yes─► c = 2 (wrap low → high)
            │ no
   c > 2? ─yes─► c = 0 (wrap high → low)
            │ no
            ▼
   m.panel.effortCursor = c
   slug = row.catalogueID(c)          ◄── gemini-3.8-flash-{low|medium|high}
            │
            ▼
   auth.SetSettingsField("model", slug)        ◄── persists immediately
            │
            ▼
   entry = catalogueEntry(slug)
            │
   ok? ─yes─► m.model      = entry.Label         ◄── "Gemini 3.8 Flash (Medium)"
            │              m.statusModel = stripTier(entry.Label)
            │                                       ◄── "Gemini 3.8 Flash"
            ▼ no
   m.model      = slug                   ◄── defensive fallback (unreachable
   m.statusModel = stripTier(slug)            in practice; the three Gemini
                                              IDs are always in the catalogue)
```

The triple-ID guarantee comes from `cli.ReferenceModels` which lists
all three by ID; the catalogue lookup `catalogueEntry` is the
same helper `selectModelEffort` uses for the `Enter` path, so the
display label and the persisted slug can't disagree.

## Settings persistence

| Step | settings.json key | Value |
|---|---|---|
| `/model` open, cursor on Gemini (medium) | `model` | `gemini-3.8-flash-medium` (unchanged from open) |
| Press → | `model` | `gemini-3.8-flash-high` (rewrite) |
| Press → again (wrap) | `model` | `gemini-3.8-flash-low` (rewrite) |
| Press Esc | `model` | stays `gemini-3.8-flash-low` |

The persistence is write-through: every ←/→ is a `SetSettingsField`
call against the same `"model"` key that `selectModelEffort` writes
on `Enter`. The reference lists "delete on default" as the convention
(`agentMode: "default"` makes 1.4.0 reject settings); the model
selector has no equivalent default-removal — there is always an
active model — so a literal-write path is correct here.

## Boundary behaviour (the wrap)

| At | `dir` | lands on | persisted slug | Note |
|---|---|---|---|---|
| low (0) | +1 | medium (1) | `…-medium` | standard forward |
| medium (1) | +1 | high (2) | `…-high` | standard forward |
| high (2) | +1 | **low (0)** | `…-low` | **wrap** (cycle's defining behaviour) |
| low (0) | -1 | **high (2)** | `…-high` | **wrap** |
| medium (1) | -1 | low (0) | `…-low` | standard reverse |
| high (2) | -1 | medium (1) | `…-medium` | standard reverse |

The non-Gemini rows (Claude Sonnet, Claude Opus, GPT-OSS 120B,
MiniMax M3, Gemini 4 Argon) remain inert even when ←/→ is pressed;
the early return when `row.slug != "gemini-3.8-flash"` happens
**before** any persistence call, so a wandering cursor cannot
silently re-pin a different model.

## Test pins (where in the suite)

| Test | What it pins |
|---|---|
| `TestModelLeftRight_AdvancesEffort` | The 6-step forward/reverse wrap table from a known medium seed |
| `TestModelLeftRight_PersistsToDisk` | Forward (5 +1's) and reverse (4 -1's) walks both rewrite `settings.json` to the matching slug at every step |
| `TestModelLeftRight_PersistsAndUpdatesLabel` | `m.model` reads "Gemini 3.8 Flash (Medium/High/Low)" matching the slug; `m.statusModel` tier-strips to "Gemini 3.8 Flash" |
| `TestModelLeftRight_RerendersSlider` | The `  Effort` line's `●` count before the active `◉` matches the cursor (0/1/2) — the slider literally moves |
| `TestModelLeftRight_NoDropdownOpen` | After ←/→: `panel.kind`, `panel.cmd`, `panel.cursor`, `panel.detail` are all unchanged |
| `TestModelLeftRight_HomeKeyIgnored` | Leftmost + ← wraps to high; rightmost + → wraps to low (boundary is symmetric, never silent) |
| `TestModelLeftRight_NonGeminiInert` | `cursor` ∈ {0, 2, 3, 4, 5} + ←/→ ⇒ `effortCursor` unchanged AND `settings.json` byte-equal |
| `TestModelLeftRight_KeyHandlerIntegration` | Full end-to-end: feed a real `tea.Key{Left, Right}` through `handlePanelKey` ⇒ settings.json updated, `m.model` updated, slider updated |

## Why no TUI plumbing change was needed

The cycle touches only the back-end (`cycleModelEffort`'s logic); the
existing key dispatch in `handlePanelKey` (cmd/wagyu/slash_panels.go:3000-3033)
already routes ←/→ to `cycleModelEffort(±1)` on the model panel, and
`panelModelLines` already reads `m.panel.effortCursor` on every
render through `rebuildEffortSlider` (cmd/wagyu/slash_panels.go:1533).
The status row already reads `m.model` and `m.statusModel` on every
render via `statusEffortSuffix`. The entire user-visible behaviour
change collapses to the one function body.

## 硬约束 / 存疑

- 【实测】The wrap boundary was *inferred* from the reference's
  `/effort` and `/config` patterns rather than measured against
  AGY 1.3.6 itself — the reference was not run on this cycle (no
  network access in the worktree's effective environment). A
  probe to drive AGY's `/model` panel left/right and observe the
  cursor's terminal position would harden the claim, but the wrap
  choice is consistent with the recovery's two other cycle-pickers
  and is unlikely to drift in a patch release.
- 【推测】The persistence-on-preview is *also* inferred from a
  status-row observation: REF's status row reflects the panel's
  current cursor between Esc and re-open. The exact key/value
  written is not measurable from the panel alone, so the test pins
  the assumption that REF writes the same `model: <slug>` form on
  ←/→ that it writes on Enter — same value, same key.
- 【实测】Non-Gemini inertness is asserted via a side effect (no
  settings.json change) rather than a probe. A directory walk
  through `ppref.model.raw.txt`'s Claude row would be needed to
  confirm REF does the same; the recovery's `/model` panel test
  only exercises the Gemini row in the live captures.
- 【实测】The `countConsecutiveBefore` helper relies on the bar's
  structural invariant — `●━━●━━◉ ──○ ──…` at level 1,
  `●━━●━━●━━◉ ──…` at level 2 — after SGR stripping. A future
  visual change that moved `◂` or `▸` arrows or added intermediate
  marks would silently invalidate the count; the helper would
  return `-1` and the test would fail loudly, so the assumption
  is at least self-checking.
- 【实测】The `cycleModelEffort` "no-op on non-Gemini rows"
  branch is an explicit early-return *before* `SetSettingsField`.
  If a future catalogue change adds a second multi-effort row,
  the function will need a `row.slug in <multi-effort bases>`
  check there; today the only multi-effort row is
  `gemini-3.8-flash`.
