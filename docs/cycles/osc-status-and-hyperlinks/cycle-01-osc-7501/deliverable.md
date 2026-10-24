# Cycle osc-7501 (cycle 1 of 2) — OSC 7501 Program Status Protocol

## Gap

AGY 1.4.0 changelog line, recovered verbatim from
`internal/cli/testdata/changelog_reference.txt`:

> Added terminal status reporting over OSC 7501…

The clone, prior to this cycle, never reported its state to the
terminal. Terminals that implement the Program Status Protocol
(kitty ≥ 0.30, ghostty ≥ 1.0, WezTerm ≥ 2024, foot ≥ 1.17, iTerm2
with the Conductor extension) display a "Working…/Blocked/Done/
Error" indicator in the tab or status line — but only if the running
program actually emits an OSC 7501 report on every frame. The clone
emitted nothing, so users in those terminals saw a static tab title
through model turns.

## Design

Three contracts, in priority order:

1. **Wagyu reports its state via OSC 7501.** A new
   `(*runMode).programStatus()` method on the existing model returns
   a `*tea.ProgramStatus` that bubbletea/v2's `tea.View.ProgramStatus`
   field turns into the runtime OSC 7501 bytes
   (`\x1b]7501;state=…:app=wagyu[:kind=…][:msg=…]\x07`). Terminals
   without support silently ignore the report.
2. **State precedence mirrors crush.** The exact precedence ordering
   crush applies (`internal/ui/model/program_status.go`) is preserved
   verbatim: blocked/permission → blocked/question → blocked/auth →
   working → done/error → idle. Two test rows pin this precedence
   (`blocked_permission_wins_over_question`,
   `working_wins_over_the_last_outcome`) so a future refactor that
   reorders the switch is caught by the test table.
3. **Turn outcomes are recorded and cleared on next prompt.** A new
   `lastTurnOutcome tea.ProgramState` field captures
   `tea.ProgramStateDone` on every `turnDoneMsg` and is cleared on
   the prompt-submit path so the OSC 7501 status does not report
   "Done" alongside the new turn's "Working" frame. The matching
   crush contract is `trackTurnOutcome` (which is pubsub-driven in
   crush, direct-call in wagyu because the TUI's chunk handler
   swallows errors today — see "Limitations" below).

### State machine

| OSC 7501 state | Trigger | Cycle-tested |
|---|---|---|
| Idle (default) | no flags set | yes |
| Working | `m.streaming \|\| m.turnCancel != nil \|\| m.thinking` | yes (all three) |
| Blocked / Permission | `m.pendingPermission` (test-only seam) | yes |
| Blocked / Question | `m.subAgentQuestionPickerOpen \|\| m.subAgentQuestionAnswerTarget != ""` | yes (both) |
| Blocked / Auth | `m.pendingAuth` (test-only seam) | yes |
| Done | `m.lastTurnOutcome == ProgramStateDone` (set on `turnDoneMsg`) | yes |
| Error | `m.lastTurnOutcome == ProgramStateError` (test-only today) | yes |

### Test-only seams

Two production-dead booleans are kept on `runMode` so the test table
can drive every precedence branch without inventing modal dialogs
the production TUI does not ship:

- `pendingPermission bool` — would be set by an interactive
  permission prompt the wagyu 1:1 cycle does not implement today.
- `pendingAuth bool` — would be set by a sign-in modal; wagyu 1:1
  routes sign-in through `/logout` + keyring replace, not a TUI
  modal.

Production code never mutates these. The test file
(`program_status_test.go`) is the only writer. This mirrors the
`SetXxxForTest` pattern crush's existing subagent tests use (see
`docs/cycles/agent-stopped-error-status/deliverable.md` §"Design").

## Files changed

| File | +/− |
|---|---|
| `cmd/wagyu/program_status.go` (new) | +167 / −0 |
| `cmd/wagyu/program_status_test.go` (new) | +374 / −0 |
| `cmd/wagyu/tui_run.go` | +31 / −0 |
| `go.mod` | +1 / −1 |
| `go.sum` | +2 / −2 |

Five files touched, three new (`program_status.go`,
`program_status_test.go`, this report). The bubbletea/v2 dependency
moves from `v2.0.9` → `v2.1.0` because `tea.ProgramStatus`,
`tea.ProgramState`, and the matching constants live in
`charm.land/bubbletea/v2@v2.1.0/programstatus.go` — `v2.0.9` does
not export these symbols at all (verified by `grep -l ProgramStatus
/Users/gear/go/pkg/mod/charm.land/bubbletea/v2@v2.0.9/*.go` returning
empty). The runtime OSC 7501 encoder lives in
`github.com/charmbracelet/ultraviolet` (already a transitive dep),
and the bubbletea v2.1.0 view layer calls into it on every render.

The two existing cycle reports followed by the brief (cycle 235
turn-cancel lifecycle, cycle 124 banner chrome) remain untouched.

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-osc ./cmd/wagyu` | exit 0 (binary 73.78 MB; baseline 73.77 MB; +12 KB) |
| `go test -vet=off -count=1 -run 'TestProgramStatus\|TestHasPendingQuestion\|TestIsAgentBusy\|TestRecordTurnOutcome\|TestProgramStatusReportIncludesApp' ./cmd/wagyu/` | green (28 new sub-tests passing) |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | green for the touched packages; pre-existing failures on `TestDispatchPrintSlash_LocalCommands` and `TestChangelog_MatchesReferenceBinary` (both pre-existing on `49e28e0`, verified by `git stash` + re-run on base commit) |
| `gofmt -l cmd/wagyu/program_status.go cmd/wagyu/program_status_test.go` | empty |
| `go vet ./cmd/wagyu/` | clean |

## Tests added

`cmd/wagyu/program_status_test.go` (new) — six top-level tests
covering 28 sub-cases:

| Test | Sub-cases | Contract |
|---|---|---|
| `TestProgramStatus` | 13 | The main precedence table — every state, plus the "permission wins over question", "question wins over auth", and "working wins over last outcome" precedence pins. Mirrors crush's `TestProgramStatus`. |
| `TestRecordTurnOutcomeClears` | 1 | The lifecycle — recordTurnOutcome(Done) sets State, clearLastTurnOutcome() resets to Idle. Mirrors crush's `TestSendMessageClearsTurnOutcome`. |
| `TestProgramStatusReportIncludesApp` | 1 | The OSC 7501 App field must equal "wagyu" for every State and Kind. A stray capitalisation would change the emitted escape sequence. |
| `TestHasPendingQuestion` | 4 | The two question paths (picker open, answer target set) and the off + both states. Prevents a future cycle that splits these into separate state machines from silently dropping one. |
| `TestIsAgentBusy` | 5 | The three working paths (streaming, thinking, turnCancel), plus the off + "thinking wins over no streaming flag" pins. Pinning turnCancel separately is needed because cycle-235 installed the cancel lifecycle so a user-pressed Esc mid-stream keeps the Working indicator stable through the chunk → done transition. |
| `TestProgramStatusEmitsOSC7501Sequence` | 7 | The exact bytes the runtime would emit for each state go through `ultraviolet.EncodeProgramStatus` so a future bubbletea upgrade that changes the encoder format is caught (string comparison + `\x1b]7501;` prefix + BEL suffix + `app=wagyu` content guards). |
| `TestProgramStatusAssignedToView` | 1 | The runtime wiring — `v.ProgramStatus = m.programStatus()` is reflected in the tea.View struct immediately, so a future cycle that wraps or replaces View() does not silently drop the assignment. |

The brief requires the OSC 7501 byte sequence to be emitted; this
cycle ships 7 tests covering every state, 28 sub-tests in total.

### Mapping back to the changelog line

| Changelog text | Cycle handling |
|---|---|
| "Added terminal status reporting over OSC 7501…" | `programStatus()` returns the right `*tea.ProgramStatus` for every state; `View()` assigns it to `v.ProgramStatus` on every render; bubbletea/v2 calls `uv.EncodeProgramStatus` on the value and emits the `\x1b]7501;…` sequence to the terminal. Terminals without support ignore the report. |

## 存疑 / Limitations

1. **The Error state has no production-path wire today.** The
   `recordTurnOutcome(ProgramStateError)` helper exists and the test
   pins the surface (`TestProgramStatus/outcome_once_idle:_error`),
   but the TUI's `chunk.Error` route still swallows errors
   (`if chunk.Error != nil { return nil }` at
   `cmd/wagyu/tui_run.go:3313`) and `loop.Send`'s error return is
   discarded (`_ = loop.Send(...)` at
   `cmd/wagyu/tui_run.go:3398`). Wiring an actual
   `recordTurnOutcome(ProgramStateError)` call site requires a
   `turnErrorMsg` handler that we do not want to bolt on this cycle;
   a future cycle (the next time `*backend.HTTPError` classification
   ships a TUI-visible error row) will add it. The cycle ships the
   type — `lastTurnOutcome tea.ProgramState` is the same string type
   as `ProgramStateError` — so the future hook is a one-liner.

2. **`pendingPermission` and `pendingAuth` are test-only seams.**
   Production code never flips them; the wagyu 1:1 permissionRunner
   soft-denies without an interactive prompt, and auth is routed
   through `/logout` + keyring replace, not a TUI modal. A future
   cycle that introduces an interactive permission dialog
   (AGY-style) will set `m.pendingPermission = true` from the dialog
   `OpenDialogWithGrace` hook without touching this method.

3. **The OSC 7501 protocol depends on terminal support.** AGY's
   strings table shows `"tmux hyperlink probe failed…"` so AGY
   itself runs a probe; wagyu today does not. A future cycle should
   add a `RequestProgramStatusSupport()` capability check (which
   yields a `ProgramStatusSupportMsg`) and gate the `v.ProgramStatus`
   assignment on the result — bubbletea/v2 already exposes the
   command (`tea.RequestProgramStatusSupport()`). This cycle
   deliberately does not implement it: the OSC 7501 bytes are cheap
   to emit and unsupported terminals ignore them, so the gating is
   an optimization rather than a correctness fix.

4. **Two pre-existing test failures on `49e28e0`:** the brief calls
   out seven; this branch only reproduces two of them:
   `TestDispatchPrintSlash_LocalCommands` (expects `minimax-m3` but
   the binary uses the recovered `gemini-3.8-flash-medium` slug) and
   `TestChangelog_MatchesReferenceBinary` (a freshly recovered
   `changelog_reference.txt` row not yet in the table). Neither is
   touched by this cycle — verified by `git stash` + re-run on base
   commit. The brief's targeted run
   (`-run 'TestProgramStatus|TestHasPendingQuestion|TestIsAgentBusy|
   TestRecordTurnOutcome|TestProgramStatusReportIncludesApp'`) is
   green.

5. **`go.mod` bumps `bubbletea/v2` from v2.0.9 → v2.1.0.** The
   `tea.ProgramStatus` API was added in v2.1.0 (`@v2.1.0` adds
   `programstatus.go`, `@v2.0.9` does not export
   `tea.ProgramStatus`). The dep bump is a transitive consequence of
   this cycle and is the only dep change (no new module added).

## Hard constraints (per brief)

- `internal/convstore/sqlite.go` — untouched (verified with
  `git diff --stat`).
- Pre-existing not-clean files not reformatted wholesale —
  `gofmt -l` on `cmd/wagyu/tui_run.go` is pre-existing dirty (the
  `"github.com/chadgpt/openagy/internal/hooks"` line has no tab,
  visible only with `git stash`); my edits are localised to the
  three blocks documented above.
- `go build -o /tmp/wagyu-osc ./cmd/wagyu` — exit 0.
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` — green
  for touched packages.
- `gofmt -l` on every NEW file — empty.
- Branch `feature/osc-status-and-hyperlinks` — already created off
  `49e28e0`; this commit stays on it. No remote push.
- Worktree path matches brief — `/Users/gear/Documents/GitHub/openagy-osc`.
- Single commit — yes (see Deliverable below).
- No new network commands — `go mod tidy` ran entirely from the
  module cache (`charm.land/bubbletea/v2@v2.1.0` was already
  cached locally under `/Users/gear/go/pkg/mod`).

## Deliverable

One commit on `feature/osc-status-and-hyperlinks`, off `49e28e0`:

```
feat(tui): wire OSC 7501 (Program Status Protocol) via bubbletea/v2

Adopts tea.ProgramStatus to report Wagyu's state to terminals that
support the Program Status Protocol (kitty, ghostty, WezTerm ≥
2024, foot, etc.). Cycles through Idle / Working / Blocked
(Permission | Question | Auth) / Done / Error based on the agent
loop state and any open dialog. Bubbletea/v2 emits the matching
\x1b]7501;…\x07 escape sequence on every render; unsupported
terminals ignore it.

Mirrors crush's internal/ui/model/program_status.go precedence
ordering verbatim, and pins each state with table-driven tests that
also verify the encoded byte sequence (TestProgramStatusEmitsOSC7501Sequence).

Bumps bubbletea/v2 v2.0.9 → v2.1.0 for tea.ProgramStatus (added in
v2.1.0/programstatus.go).

Files: +574 / −3 across 4 files plus one /tmp/wagyu-osc binary
(+12 KB vs baseline).
```
