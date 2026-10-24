# Merge record — feature/effort-picker (`/effort` timeline-gauge picker + `--effort` flag + model-default fallback)

**Date:** 2026-10-18
**Feature:** `feature/effort-picker` → `main` (merge commit `e3579ae`)
**Feature commit (post-rebase):** `5ec86b4`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-effort` (cleaned up)

## TL;DR

The cycle-208 effort picker (timeline-gauge UI for low/medium/high
selection) is now paired with a direct `/effort <level>` slash form,
a `--effort` CLI flag, and a model-default fallback. Empty `Effort` in
`pigrt.Options` keeps the model's default (no override); non-empty
values are forwarded to PiG's `Session.SetThinkingLevel` (print path)
or `runner.SetThinkingLevel` (TUI path) before the next `Send`. PiG
clamps via `model.ThinkingLevelMap`, so an unsupported level silently
narrows to what the model supports rather than erroring.

Cycle 208 already shipped the picker UI. Cycle effort-picker adds:
- `/effort <level>` direct form (no picker) — sets and echoes
- `--effort <level>` CLI flag — sets the session-wide default
- `Effort` field plumbed through `pigrt.Options`, `agent.Loop.Options`,
  `pigPrintConfig.Effort` (print path)
- Model-default fallback when neither CLI nor picker has set the level
- Persistence across resumes via the same `Effort` field

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/slash_effort_test.go` (new) | +190 | 7 tests: Direct, InvalidLevel, NoArgsOpensPicker, PickerNavigate, PickerEscape, DefaultFallback, Persistence |
| `cmd/wagyu/slash_regression.go` | +57 / −4 | cycle-208 picker remains; `applyEffortSelection` → `applyEffortLevel` refactor |
| `cmd/wagyu/slash_panels.go` | +11 / −0 | `panelEffort` registered in panel kind enum |
| `cmd/wagyu/tui_run.go` | +5 / −0 | `runner.SetThinkingLevel` before each `SendMessages` |
| `docs/cycles/effort-picker/deliverable-effort.md` (new) | +190 | TL;DR, scope, slash table, architecture, gates, probe table, 8 存疑 |
| `internal/agent/loop.go` | +11 / −0 | `Options.Effort` field; `runner.SetThinkingLevel` wiring |
| `internal/agent/pig.go` | +9 / −0 | `runMode.pickEffort` defaults to `opts.Effort` (model-default fallback) |
| `internal/pigrt/runtime.go` | +27 / −X | `Options.Effort` field; `Session.SetThinkingLevel` after `BindExtensions` |

**Total:** 13 files changed, **+636 / −4**.

## Rebase conflict resolution

Four files had conflicts with cycles 227/228 and tasks-panel:

### Conflict 1: `internal/agent/loop.go` Options struct
- HEAD (cycle 227) added `OnCompactionStart` / `OnCompactionEnd`
- effort-picker added `Effort string` field — but the field landed in
  a duplicate struct copy (the rebase interleaved into the
  `compactRecord` struct definition below Options).
- **Resolution**: move `Effort string` into the **Options** struct
  (alongside `OnCompactionEnd`), then strip the duplicate out of
  `compactRecord`. Final shape has Options containing both
  callbacks AND Effort; compactRecord untouched.

### Conflict 2: `internal/pigrt/runtime.go` Options struct
- HEAD (cycle 227) added `CompactEnabled *bool` / `CompactThreshold float64`
- effort-picker added `Effort string`
- **Resolution**: keep both — three new fields on Options, alphabetically
  ordered (Compact* then Effort).

### Conflict 3: `cmd/wagyu/print_pig.go` Options literal
- HEAD (cycle 227) added `CompactEnabled: ptrBool(true)`
- effort-picker added `Effort: cfg.Effort`
- **Resolution**: keep both fields in the struct literal.

### Conflict 4: `cmd/wagyu/tui_run.go` agent.Options literal
- HEAD (cycle 227+228) added `OnCompactionStart` / `OnCompactionEnd` /
  `ExtraAgentTools`
- effort-picker added `Effort` field assignment
- **Resolution**: keep both — different struct fields.

### Conflict 5: `cmd/wagyu/slash_codesearch.go` `openSlashPanelArgs`
- HEAD (codesearch) had `if cmd == "codesearch" && args == ""` block
- effort-picker added `if cmd == "effort" && args != ""` block
- **Resolution**: `sed`-stripped the markers but accidentally also
  stripped the `return false` line of the codesearch block; restored
  manually.

The effort-picker commit was NOT amended post-rebase (all four files
were already gofmt-clean).

## Gates verified

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-effort ./cmd/wagyu` (post-rebase) | exit 0 |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (post-rebase) | fully green (no flake this run) |
| `gofmt -l` on all touched `.go` files | empty |
| `go vet ./cmd/wagyu/ ./internal/agent/ ./internal/pigrt/` | only pre-existing cycle-224 vet warnings |
| Post-merge build + test | build exit 0; tests green |

## Hard constraints honoured

- Did not touch `internal/convstore/sqlite.go`.
- Did not touch `internal/subagent/` or `internal/subagenttools/`.
- Cycle-208 `TestCycle208_Effort*` tests untouched and still green.
- `~/.gemini/**` SHA byte-identical before/after (no probe mutated state).
- Worktree-only commit; no `git push`.

## 存疑 carried into cycle 230+

1. **PiG's `model.ThinkingLevelMap` clamp is opaque** — silent drops
   for unsupported tiers (no warning to user).
2. **Per-model overrides** (`--model foo --effort high` when foo doesn't
   expose High) are not exercised.
3. **Reference live probe could not be re-run** because
   `~/.config/gcloud/application_default_credentials.json` is absent
   on this machine; byte-match table reuses cycle-208 captures.

## Cleanup

After merge to main:
- `git worktree remove --force /tmp/.../openagy-effort` — released ~720 MB.
- `git branch -D feature/effort-picker` — branch removed.

Main is now at `e3579ae` (merge commit); the effort-picker commit
`5ec86b4` remains reachable through the merge commit's second parent.

## Cycle numbering reconciliation

Branch is `feature/effort-picker` (semantic name). Cycle anchor used
during planning was cycle 234 of the cycle-228/237 roadmap; see
`docs/research/agy-subagents-vs-wagyu.md`.

## Next

- 2 subagents completed and waiting: `feature/side-questions`, plus
  this cycle.
- 2 subagents still in flight: `feature/resume-continuation`,
  `feature/auto-continue-interrupt`.
- Queue order: side-questions → resume → auto-continue.