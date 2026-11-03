# Cycle 382 — RunMode Model Contract, Panel Registry, and TUI State Extraction

> **Cycle report.** Replaces the monolithic `runMode` anti-pattern with the agy 1.4.12
> reference TUI architecture across six tranches: the `tuimodel` contract package,
> the `panelregistry` dispatcher (eliminating all `switch m.panel.kind` occurrences),
> the `tuistore` state extraction, embedding `tuimodel.Base` on `runMode`, and the
> architecture guard test.

---

## 1. Problem & Architecture

In agy 1.4.12, the TUI is composed of 49 distinct models, a shared store manager,
and a `baseModel` struct that provides 7 inherited default methods across the 10-method
Model interface (`Init`, `Update`, `View`, `Store`, `Layout`, `UIPhase`, `Keybindings`,
`ActionFromMsg`, `HasArtifacts`, `SetLayout`).

In contrast, the clone previously concentrated 219 fields and 527 methods in a single
`runMode` struct, routing panel interactions through four hardcoded `switch m.panel.kind`
statements in `internal/cliapp/`:
- `slash_panel_keymap.go:92`: key routing
- `slash_panel_routing.go:208`: `panelLinesCore`
- `slash_panel_routing.go:250`: `panelLines`
- `slash_viewer_spec.go:47`: `viewerSpecFor`

Cycle 382 eliminates this anti-pattern while strictly preserving Tier invariants
(`core (1) < features (2) < infra (3) < app (4) < ui (5)`):
1. Created `internal/ui/tuimodel` (Tier 5) with `Model` (10 methods) and `Base` (7 inherited defaults).
2. Created `internal/ui/panelregistry` (Tier 5) and routed all panel dispatches through it.
3. Created `internal/app/tuistore` (Tier 4) to hold session state outliving a single frame.
4. Embedded `tuimodel.Base` on `runMode`, satisfying `tuimodel.Model` directly.
5. Implemented `internal/cliapp/architecture_test.go` to guard these invariants permanently.

---

## 2. Key Metrics & Measurements

All numbers verified via `go/ast` parser scripts and test suites.

| Metric | Before Cycle 382 | After Cycle 382 | Delta | Status |
|---|---|---|---|---|
| `switch m.panel.kind` in `cliapp` | 4 sites | **0 sites** | -4 (-100%) | **Eliminated** |
| `runMode` field count | 219 fields | **192 fields** | -27 fields | **Reduced** |
| `runMode` method count | 527 methods | **520 methods** | -7 methods | **Reduced** |
| `tuimodel.Model` compliance | 0 (did not exist) | **100% compliant** | +1 interface | **Enforced** |
| Tier violations / upward imports | 0 | **0** | 0 | **Clean** |
| Extracted `tuistore` state clusters | 0 | **5 clusters** | +5 clusters | **Extracted** |

---

## 3. Tranches Executed

### Tranche 1: The `tuimodel` Contract Package (`cfd0e02d`)
- Package: `internal/ui/tuimodel` (Tier 5).
- Interface: `Model` (10 methods: `Init`, `Update`, `View`, `Store`, `Layout`, `UIPhase`, `Keybindings`, `ActionFromMsg`, `HasArtifacts`, `SetLayout`).
- Base Struct: `Base` implementing default implementations for the 7 inherited methods.
- Layout: Exact 7 height fields (`PromptHeight`, `InputAreaHeight`, `PendingLineHeight`, `HintHeight`, `BtwHeight`, `SubagentAlertHeight`, `StatusLineHeight`) with overhead and available line calculators, matching the agy binary descriptor at `0x1029e59f0`.
- UIPhase: All 26 canonical enum values (0..25: `AuthStart` to `BattleIdle`) matching agy 1.4.12 binary at `0x10869b460`.

### Tranche 2: Panel Registry Kills the Four Switches (`880d44de`)
- Package: `internal/ui/panelregistry` (Tier 5).
- Relocated `Kind` properties (`ExitNotice`, `IsViewer`, `TrailingBlank`, `KeepStatusHint`) to `panelregistry.Kind`.
- Created `internal/cliapp/slash_panel_registry.go` with `buildPanelRegistry()` registering all 24 modal panels.
- Replaced the four `switch m.panel.kind` blocks in `slash_panel_keymap.go`, `slash_panel_routing.go`, and `slash_viewer_spec.go` with registry lookups.

### Tranche 3: Selection and Bash State Extraction (`7bfd7fd9`)
- Package: `internal/app/tuistore` (Tier 4).
- Moved `Selection` (viewport drag/selection coordinates) and `InputSelection` (composer selection range).
- Moved `Bash` mode execution state (`BashMode`, `BashRowIdx`, `BashCancel`, `BashRun`, `BashCmd`, `BashGen`, `BashQueue`).
- Replaced 15 fields on `runMode` with `store tuistore.Store`.

### Tranche 4: Queue, Compaction, and Token State Extraction (`bec7ead7`)
- Moved `Compaction` telemetry and spinner state (`CompactSpinnerFrame`, `CompactStart`, `CompactEnd`, `CompactTokensBefore`, `CompactTokensAfter`, `CompactReason`).
- Moved `Queue` state (`QueueMode`, `QueuedPrompt`, `StreamQueue`).
- Moved `Tokens` session totals and in-flight usage (`SessionPromptTokens`, `SessionCompletionTokens`, `LastTurnCompletionTokens`, `LastTurnPromptTokens`, `StatusLineTickArmed`).
- Replaced 14 additional fields on `runMode` with embedded structs in `tuistore.Store`.

### Tranche 5: Embed `tuimodel.Base` on `runMode` (`1bd1bcb9`)
- `runMode` embeds `tuimodel.Base`.
- Inherits the 7 shared default methods (`Store`, `Layout`, `UIPhase`, `Keybindings`, `ActionFromMsg`, `HasArtifacts`, `SetLayout`).
- Statically verified via `var _ tuimodel.Model = (*runMode)(nil)`.

### Tranche 6: Architecture Guard Test (`b6c1594a`)
- Created `internal/cliapp/architecture_test.go`:
  - `TestRunMode_ArchitectureGuard`: asserts `runMode` fields <= 195, zero `switch m.panel.kind`, zero host imports in `tuimodel` and `tuistore`, and interface satisfaction.
  - `TestRunMode_PanelRegistryCoverage`: verifies all 24 panel kinds query safely against the registry.
  - `TestArchitecture_TiersCompliance`: runs `go vet` over the new packages.

---

## 4. Invariants, Open Questions & Limitations

### 硬约束 (Hard Invariants Maintained)
1. **Tier Discipline Intact**: `internal/ui/tuimodel` and `internal/ui/panelregistry` (Tier 5) import only stdlib and bubbletea; `internal/app/tuistore` (Tier 4) imports only stdlib and `internal/app/tools`. Neither imports any host package (`cliapp`, `agent`, `pigrt`).
2. **Zero `switch m.panel.kind`**: Dispatch is 100% table-driven via `panelregistry.Registry`.
3. **No Behavior Regressions**: All selection, bash mode, queue mode, prompt submit, token accounting, and changelog pagination tests pass.

### 存疑 (Open Questions)
1. **Deeper Field Reduction Target**: The initial exploratory roadmap noted a tentative "80 fields" target. However, as measured in `PLAN.md` §2, the turn loop's immediate working set on `runMode` (backend, session messages, compactor, task inbox, media buffers) accounts for 118 fields. Reaching < 120 fields will require splitting turn-loop orchestration into separate session-engine managers.
2. **Subagent Question State**: `subAgentQuestions*` fields are prime candidates for a future `tuistore/subagent.go` slice once subagent question pickers are decoupled from view rendering. **Answered by cycle 381's 35th tranche**: the picker is now `internal/ui/views/subagentquestionpanel` (tier 5) and owns the scope, the open flag, the two cursors, the staged answer and the rows; the host keeps only the `*subagentquestions.Manager` — the session's store, which the `tasks.AskQuestion` tool seam enqueues into — and the panel that reads it. The manager is a *session* fact rather than a *view* fact, so it belongs to the session's own store, not to `tuistore`'s view slices; the runMode's field count went from 193 to 187.

### Limitations
1. `tuistore` (Tier 4) does not store view render caches (`wrapSlot`, `composedRows`) because those types belong to `internal/ui/foundations/viewport` (Tier 5). To prevent upward tier dependencies, viewport caching remains in Tier 5 / host.
