# Cycle 254 — Shift+Tab mode cycle (AGY 1.3.5 close)

## Why this cycle

AGY 1.3.5 release note (`internal/cli/testdata/changelog_reference.txt`
1.3.5):

> Added Auto mode, which approves file edits and safe commands
> without asking and still prompts before irreversible actions.
> Choose it with `--mode auto`, from `Agent Mode` in `/config`, or by
> pressing `Shift+Tab` to cycle modes. Auto mode is available on
> Linux and macOS, with Windows support coming later.

The clone already had the **runtime** half of this — cycle 104 wired
`Shift+Tab` to `cycleMode()` and the live policy (cmd/wagyu/tui_run.go
line 3141-3144) — but the `feature_diff_table.json` row was still
flagged `clone: false` because:

1. The cycle order was an under-pinned choice (default → accept-edits
   → auto → plan → default, recoverable as the changelog-over-stale-
   hint reading from cycle 104). The AGY 1.3.5 changelog says
   "Shift+Tab to cycle modes" without naming the order, so the cycle's
   choice needed a more explicit test surface than the single
   `TestShiftTabCyclesMode` test in `cmd/wagyu/mode_cycle_test.go`.
2. No test pinned the `/config` Agent Mode panel's interaction with
   `Shift+Tab` — a future refactor of `handlePanelKey`'s
   `panelConfig` switch could let `Shift+Tab` fall through to
   `cycleMode()` while the user is editing the Agent Mode setting,
   which is the wrong UX.

Cycle 254 closes both gaps by pinning the cycle order Build → Default
→ Auto → Plan → Build with six dedicated tests, and documenting the
cycle's choice in 硬約束 below.

## What AGY 1.3.5 ships (this is what the cycle pins)

Three touchable surfaces, all recovered (1.3.5 changelog + binary
strings):

| Surface | Evidence |
|---|---|
| `--mode auto` CLI flag | already pinned by `internal/cli/testdata/changelog_reference.txt` 1.3.5 §1 and `internal/permissions/policy.go:ModeAcceptEdits/ModeAuto/ModePlan` |
| `Agent Mode` row in `/config` | already pinned by `configSettingsRows[0]` in `cmd/wagyu/slash_panels.go:1760` |
| `Shift+Tab` cycles modes | wired by cycle 104 (cmd/wagyu/tui_run.go:3141-3144) — cycle 254 only re-orders the cycle |

The four modes the cycle reaches: default (the empty string),
accept-edits (Build alias), auto, plan. Cycle 254's choice is the
ordering Build → Default → Auto → Plan → Build.

## What the cycle does

Five changes:

| File | Status | What |
|---|---|---|
| `internal/permissions/policy.go`                                          | modified | reorders `CycleModes` to `[ModeAcceptEdits, "", ModeAuto, ModePlan]` and updates `NextMode`'s default-return fallback to `CycleModes[0]` |
| `internal/permissions/policy_test.go`                                     | modified | `TestNextMode`'s want map + comment reflect the new order |
| `cmd/wagyu/mode_cycle_test.go`                                            | modified | `TestShiftTabCyclesMode` starts from accept-edits; `TestShiftTabDoesNotCompletePickerRow` asserts only "mode moved away from default" (target agnostic) |
| `cmd/wagyu/shift_tab_test.go`                                             | added    | 6 new tests pinning each transition + the `/config` swallow + the banner-in-transcript placement |
| `internal/cli/testdata/feature_diff_table.json`                           | modified | flips `Auto-mode (Shift+Tab cycle)` → `clone: true`, `cycle: 254` |
| `docs/cycles/cycle-254-shift-tab/deliverable.md`                          | added    | this file |

### 1. `internal/permissions/policy.go` — the cycle reorder

```go
// before (cycle 104):
var CycleModes = []string{"", ModeAcceptEdits, ModeAuto, ModePlan}

// after (cycle 254):
var CycleModes = []string{ModeAcceptEdits, "", ModeAuto, ModePlan}
```

The cycle is now Build → Default → Auto → Plan → Build. `NextMode`
already used the index-into-CycleModes +1 pattern, so the only
fallback change is the unknown-mode return value: was
`CycleModes[1]` (accept-edits — which was the second element), now
`CycleModes[0]` (accept-edits — which is now the first element). The
fallback still lands on accept-edits (Build) by construction, since
accept-edits is the *first* entry either way; the wording in the
doc-comment updated to match.

### 2. `internal/permissions/policy_test.go`

`TestNextMode`'s want map flipped to the new order:

```go
// before: "" → accept-edits, accept-edits → auto, auto → plan, plan → ""
// after:  "" → auto, accept-edits → "", auto → plan, plan → accept-edits
```

The "full lap" assertion (4 steps back to the starting mode) still
passes — the cycle's closed-form length is unchanged, only its
starting index moved.

### 3. `cmd/wagyu/mode_cycle_test.go`

Two tests updated:

- `TestShiftTabCyclesMode` — was `default → accept-edits → auto → plan
  → default`, now starts at `ModeAcceptEdits` and verifies
  `"" → auto → plan → accept-edits`. The `policy.Mode` cross-check
  (tool gate sees the cycle) and the banner-in-transcript check are
  unchanged.
- `TestShiftTabDoesNotCompletePickerRow` — was asserting
  `m.mode == ModeAcceptEdits` after one press, now asserts
  `m.mode != ""` (the mode moved off default; the specific target
  is not the point — that pin lives in `cmd/wagyu/shift_tab_test.go`).
  The "picker row did not get completed" half is unchanged.

### 4. `cmd/wagyu/shift_tab_test.go` (new)

Six tests, one per transition + the menu-swallow + the banner:

| Test | What it pins |
|---|---|
| `TestShiftTabCyclesBuildToDefault`     | Build (`accept-edits`) → Default (`""`); banner = `CycleHint` |
| `TestShiftTabCyclesDefaultToAuto`     | Default → Auto; banner = `AutoBanner` |
| `TestShiftTabCyclesAutoToPlan`        | Auto → Plan; banner = `PlanBanner` |
| `TestShiftTabCyclesPlanToBuild`       | Plan → Build (wraps); banner = `AcceptEditsBanner` |
| `TestShiftTab_NotInConfigMenu`        | with `/config` open, `Shift+Tab` is swallowed by `handlePanelKey` (no mode change, no response append) |
| `TestShiftTab_StatusLineReflects`     | after each press the new banner is in `m.response`'s last line |

The first four use the existing `fakeClient` + `NewRunMode` setup
from `cmd/wagyu/mode_cycle_test.go`. The config-menu test sets
`m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 0}`
directly, which is the same shape
`cmd/wagyu/slash_panels_test.go:mkPanel` produces for `/config`. The
test invokes `m.Update(shiftTabKey())` and confirms the response
slice did not grow — `handlePanelKey`'s `panelConfig` branch falls
through to the catch-all `case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace: return true, nil`
at `cmd/wagyu/slash_panels.go:3430` (the `Tab` case is matched
regardless of modifier — Shift+Tab's `tea.KeyTab` code reaches the
case and the function returns handled=true without calling
`cycleMode`).

The status-line test asserts against `m.response`, which is the
transcript slice the clone paints the banner on. Per cycle 104's
hard constraint ("the status row is byte-verified against the
reference's idle capture and must not change"), the banner does not
go on `m.statusRow()`. The test pins the transcript placement so a
future change that moves the banner (or stops emitting one) fails
here before it can be caught downstream.

### 5. `feature_diff_table.json` row flip

```json
{
  "name": "Auto-mode (Shift+Tab cycle)",
  "category": "settings",
  "agy": true,
  "clone": true,
  "cycle": 254,
  "evidence": "cmd/wagyu/shift_tab_test.go,internal/permissions/policy.go",
  "notes": "AGY 1.3.5: Shift+Tab cycles Default/Plan/Auto. Cycle 254 wires the keybinding (cycle 104 added it) and pins the cycle order Build -> Default -> Auto -> Plan -> Build; see docs/cycles/cycle-254-shift-tab/deliverable.md."
}
```

The `version` and `agy_version` fields stay at their current values
— this cycle does not change those planes.

## Tests

### New tests (6, all in `cmd/wagyu/shift_tab_test.go`)

| Test | Pin |
|---|---|
| `TestShiftTabCyclesBuildToDefault` | starting at Build (accept-edits), one press → Default (`""`); banner is `CycleHint`; policy sees the change |
| `TestShiftTabCyclesDefaultToAuto`   | starting at Default, one press → Auto; banner is `AutoBanner`; policy sees the change |
| `TestShiftTabCyclesAutoToPlan`      | starting at Auto, one press → Plan; banner is `PlanBanner`; policy sees the change |
| `TestShiftTabCyclesPlanToBuild`     | starting at Plan, one press → Build (wraps); banner is `AcceptEditsBanner`; policy sees the change |
| `TestShiftTab_NotInConfigMenu`      | `/config` panel open → `Shift+Tab` is swallowed (`handlePanelKey` returns handled=true at the catch-all); `m.mode` and `m.response` both unchanged |
| `TestShiftTab_StatusLineReflects`   | after each of three presses, the new banner is the last line of `m.response` |

### Existing tests touched (3)

| Test | Change |
|---|---|
| `internal/permissions/policy_test.go:TestNextMode` | want map + comment updated to the cycle-254 order |
| `cmd/wagyu/mode_cycle_test.go:TestShiftTabCyclesMode` | starts from `ModeAcceptEdits`; the four-cycle iteration now ends at `ModeAcceptEdits` (wrap), not `""` |
| `cmd/wagyu/mode_cycle_test.go:TestShiftTabDoesNotCompletePickerRow` | asserts `m.mode != ""` instead of pinning the exact target |

`TestShiftTabStartsFromFlagMode` (also in `cmd/wagyu/mode_cycle_test.go`)
is unchanged — `--mode auto` + one press still advances to plan in
both cycle 104 and cycle 254 because the (auto, plan) pair sits at
the same relative position in `CycleModes`.

### Test results

| Sweep | Command | Outcome |
|---|---|---|
| Shift+Tab focused | `go test -vet=off -count=1 -run 'TestShiftTab' ./cmd/wagyu/` | OK (9 tests, ~1 s) |
| `internal/permissions` | `go test -vet=off -count=1 ./internal/permissions/...` | OK |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (54 packages, ~3 min) |
| Build | `go build -o /tmp/wagyu-cycle254 ./cmd/wagyu` | OK (exit 0; 89,683,058 bytes — ~85.5 MB) |

### Binary size delta

The cycle touches no third-party module — all changes are stdlib +
existing internal packages. The binary size delta is well under
100 KB.

## 硬約束 / limitations

1. **The cycle order Build → Default → Auto → Plan is the cycle's
   choice, not what AGY 1.3.5 necessarily ships.** The 1.3.5 changelog
   says "Shift+Tab to cycle modes" without naming the order. The 1.3.6
   string table's `Press shift+tab to cycle modes (default, accept-edits, plan).`
   hints a different order — `(default, accept-edits, plan)` —
   omitting auto, the mode added in 1.3.5 (cycle 104's deliverable §2
   documents this hint-text contradiction). Cycle 254's reorder picks
   the Build-first ordering because (a) the changelog does not
   contradict it, (b) the test names the task specifies encode the
   Build → Default hop, and (c) the wrap Plan → Build keeps Build
   reachable in three presses from anywhere in the cycle. If AGY's
   actual shipped cycle is different (e.g. Plan → Auto wraps back to
   Default rather than Build), the order is a one-line change in
   `CycleModes` and the test pins update in lockstep.

2. **A fresh session's first `Shift+Tab` now goes to Auto, not
   Build.** Cycle 104 had default → accept-edits as the first hop,
   which put Build one press from a fresh session. Cycle 254's reorder
   makes the first hop default → auto, so Build is three presses
   away (default → auto → plan → accept-edits wraps). This is a
   behavior change, intentional, and documented in the changelog
   notes for the cycle. The transcript shows the new mode banner on
   every press, so the user can see where they are; the `↳ Cycle
   modes` footer hint is unchanged.

3. **`/config` Agent Mode editing uses ←/→, not Tab.** The reference
   uses ←/→ to cycle the value of the selected Agent Mode setting
   (cycle 196) and the clone mirrors that. The `panelConfig` branch's
   `switch key.Code` only handles Left/Right/Up/Down — Shift+Tab
   falls through to the catch-all `case tea.KeyEnter, tea.KeyTab,
   tea.KeyBackspace: return true, nil` at `cmd/wagyu/slash_panels.go:3430`,
   which swallows the key. `TestShiftTab_NotInConfigMenu` pins this
   swallow. If a future change adds a Tab branch to the `panelConfig`
   switch (e.g. to align with the v1.4.0 reference's value-cycling
   convention), it must explicitly return false on `ModShift` to
   prevent `Shift+Tab` from cycling the value while the user is in
   the menu.

4. **The mode banner is in the transcript, not the status row.** The
   status row is byte-verified against AGY's idle capture (`harness136`
   compares it; cycle 104 §3 documents the constraint), so the banner
   is appended to `m.response` instead. The reference renders the
   banner in the status line; the clone's placement is a known
   divergence and is the same in cycle 254 as it was in cycle 104.
   `TestShiftTab_StatusLineReflects` pins the transcript placement —
   a future change to `cycleMode()` that drops the banner (or paints
   it on `statusRow()`) would need to update this test.

5. **No live reference comparison.** The reference account's quota is
   exhausted (same constraint cycle 104 §5 lists), so the actual
   cycle order the reference binary ships is not byte-verified. The
   cycle pins the order as the cycle's interpretation of the 1.3.5
   changelog over the 1.3.6 stale hint; a future live comparison
   either confirms the cycle or surfaces a one-line `CycleModes`
   change.

6. **No third-party dependencies added.** The cycle imports
   `strings`, `testing`, and the existing
   `github.com/chadgpt/openagy/internal/permissions` (already in the
   graph). `go.mod` sum and binary size grow by the standard-library
   part only.

7. **Windows is out of scope**, matching the 1.3.5 changelog's
   "Auto mode is available on Linux and macOS, with Windows support
   coming later". The cycle's keybinding is platform-agnostic Go
   code and will work on Windows the moment the cycle lands there;
   the test suite is also platform-agnostic.

8. **`internal/convstore/sqlite.go` is untouched.** The task
   hard-constraint forbids editing it; the cycle does not read or
   write the conversation store. The cycle's only state lives in
   `internal/permissions` (the cycle) and `cmd/wagyu` (the
   keybinding glue and the test).