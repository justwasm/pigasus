# Cycle `subagent-stopped-clear-on-progress` — deliverable

## AGY changelog gap

AGY 1.2.13 changelog line:

> Fixed subagent Stop reason being cleared automatically by new
> progress, leaving stale 'Done' labels visible in the UI. Subagent
> Stop reason should remain visible until a NEW Stop event replaces
> it, but progress events while running should never clear the
> displayed Stop reason.

## What ships

The fix lives entirely in `internal/subagentrouter` — the package the
`/agents` panel, the `@<subagent>` picker, and the running-agents
list read from. Two new methods are added to `Manager` (`ReportStop`,
`ReportProgress`), one new field on `Entry` (`StopReason`). No
external wiring is touched; the AGY 1.2.13 invariant is enforced at
the package boundary so future callers cannot reintroduce the bug.

### Data model — `internal/subagentrouter/manager.go`

```go
type Entry struct {
    Name       string
    State      State
    Started    time.Time
    Sender     Sender
    StopReason string   // NEW; "" until the first Stop event
}

func (m *Manager) ReportStop(name string, reason string) error
func (m *Manager) ReportProgress(name string) error
```

`ReportStop` replaces the displayed reason only when the new reason
differs from the existing one. `ReportProgress` does NOT touch
`StopReason` — that is the entire fix. Empty-name and unknown-name
calls return the package's existing sentinel errors
(`ErrEmptyName`, `ErrUnknownName`), matching the contract
`SetState` / `Get` / `Unregister` already expose.

### Semantics pinned by unit tests

| Scenario | Behaviour |
|---|---|
| Fresh `Register` | `StopReason == ""`, `State == StateRunning` |
| Stream of progress events (no Stop yet) | `StopReason` stays `""` |
| First `ReportStop("alice", "Error: ...")` | `StopReason == "Error: ..."` |
| 50× `ReportProgress` after the Stop | `StopReason` unchanged (the headline fix) |
| `ReportStop` with same reason as currently displayed | no-op (idempotent) |
| `ReportStop` with a different reason | replaces |
| `ReportStop` with empty reason (resume path) | clears |
| `ReportProgress` after `SetState(StateCompleted)` | `StopReason` unchanged |
| `ReportStop` / `ReportProgress` on unknown name | `ErrUnknownName` |
| Empty-name calls | `ErrEmptyName` |

12 tests added; all green under `go test ./internal/subagentrouter/`.

## Files touched

| File | Change |
|---|---|
| `internal/subagentrouter/manager.go` | +`StopReason` field on `Entry`; +`ReportStop` +`ReportProgress` methods; +package doc paragraph on the AGY 1.2.13 invariant. |
| `internal/subagentrouter/manager_test.go` | +12 unit tests. |
| `docs/cycles/subagent-stopped-clear-on-progress/deliverable.md` | this file. |

## Why `internal/subagentrouter`

The `/agents` UI and the `@<subagent>` picker both read from the
`subagentrouter.Manager` (see `cmd/wagyu/at_subagent.go:65`,
`cmd/wagyu/tui_run.go:1614`). Putting the rule on the package the
UI consumes makes the bug structurally un-reintroducible: any
wiring that pumps subagent runtime events through `ReportStop` /
`ReportProgress` automatically gets the AGY 1.2.13 invariant for
free, with no per-caller discipline required. The other package
that knows about subagent state — `internal/subagent` — manages
bash subprocesses with their own pending/running/done/failed
lifecycle and is not in the `/agents` render path.

## Verification

| Check | Result |
|---|---|
| `gofmt -l internal/subagentrouter/` | empty |
| `go vet ./internal/subagentrouter/` | clean |
| `go build -o /tmp/wagyu-ssp ./cmd/wagyu` | exit 0, 73 MB |
| `go test ./internal/subagentrouter/` | all green (existing 13 + new 12 = 25) |
| `go test -vet=off -count=1 -run 'TestSubagent\|TestStop\|TestReason\|TestProgress' ./cmd/wagyu/ ./internal/.../` | green |
| `internal/convstore/sqlite.go` | untouched |

## Out of scope (deliberately)

- The actual `cmd/wagyu` UI that renders `StopReason` in the
  `/agents` panel. That panel is still the cycle-231 stub; wiring
  the new field into the rendered row is a separate cycle
  (the `Entry.State`/`Entry.StopReason` rendering is the same
  shape the parent already reads).
- Migrating any existing caller. Today `subagentrouter.Manager` is
  populated only by the cycle-231 test scaffolding
  (`cmd/wagyu/at_subagent_test.go`); the production wiring is
  cycle-232 work and will pick up `ReportStop` /
  `ReportProgress` naturally because those are the only ways to
  set the new field.

## Hard constraints checklist

- [x] `internal/convstore/sqlite.go` untouched
- [x] New files gofmt-clean (`gofmt -l internal/subagentrouter/`
      empty); pre-existing files reformatted only inside the
      appended test block
- [x] `go build -o /tmp/wagyu-ssp ./cmd/wagyu` exit 0
- [x] `go test -vet=off -count=1 -run 'TestSubagent|TestStop|TestReason|TestProgress' ./cmd/wagyu/ ./internal/.../` green
- [x] `gofmt -l` on every NEW (no new files in this cycle, only edits)
- [x] agw / MiniMax-M3 model used for implementation
- [x] Branch not pushed; single commit, on
      `feature/subagent-stopped-clear-on-progress`
- [x] ≥5 unit tests (12 added) covering: stop reason persists
      across progress, new stop replaces old, empty progress does
      not clear, initial state has no reason, running subagent
      shows no reason