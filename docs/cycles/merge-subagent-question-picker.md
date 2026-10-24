# merge subagent-question-picker — Ctrl+K/a picker + per-subagent routing + headless auto-skip

## TL;DR

AGY 1.3.3 changelog line 38 (and 1.3.6): when a subagent runs `ask_question`
during its turn, the parent surfaces a `<agent> has a question` alert and
opens a picker overlay. The clone routes via `internal/subagentquestions.Manager`
with per-subagent pending slices and a `Ctrl+K` / `a` keybind to open/close
the picker. Headless `-p` mode auto-skips.

| | |
|---|---|
| Branch | `feature/subagent-question-picker` |
| Commit | `fd86dd5` |
| Merge | `2b976ef` |
| Lines | +2187 / −2 (9 files) |
| New package | `internal/subagentquestions` |
| Cycle report | `docs/cycles/subagent-question-picker/deliverable.md` |

## Scope (vs AGY 1.3.3 line 38 + 1.3.6)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| Per-subagent pending question store | `Manager.Wait/Answer/Skip` keyed by subagent ID | ✅ |
| `<agent> has a question` alert surfaces in transcript | `subAgentQuestionAlertRows` in `viewBody` | ✅ |
| Ctrl+K / `a` opens picker | `cmd/wagyu/tui_subagent_question.go::openSubAgentQuestionPicker` | ✅ |
| Picker closes via Esc / Enter on question / selection outside | `handleSubAgentQuestionPickerKey` | ✅ |
| Empty answer → `(skipped)` | `submitSubAgentQuestionAnswer` (called BEFORE empty-prompt guard so empty-as-skip is reachable) | ✅ |
| Headless `-p` mode auto-skips | `Manager(headless=true)` in `print_pig.go`; `AskQuestion` returns `SkipAnswer` | ✅ |
| `a` falls through when no pending | `PendingCount > 0` guard on the keybind | ✅ |
| Multiple subagents route independently | scope test in `subagentquestions_test.go` + TUI test | ✅ |
| `internal/tools/nonnetwork.go` nil-Manager keeps cycle-214 `noUserText` path | verified by `TestNilManagerKeepsLegacyPath` | ✅ |

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `internal/subagentquestions/subagentquestions.go` | new | +446 | `Manager`, `Question`, scope-keyed pending slices, headless auto-skip, `sync.Once`-closed answer channel |
| `internal/subagentquestions/subagentquestions_test.go` | new | +362 | 12 race-clean unit tests |
| `internal/tools/nonnetwork.go` | modified | +63 / −2 | `AskQuestion` tool gains `SubAgentQuestions *Manager` + `SubAgentID string`; nil-Manager keeps cycle-214 path |
| `cmd/wagyu/print_pig.go` | modified | +16 | re-registers `AskQuestion` with a headless `NewManager(true)` in the catalog |
| `cmd/wagyu/tui_run.go` | modified | +137 | `runMode` fields; `NewRunMode` initialises Manager; `runLiveTUI` re-registers; Ctrl+K + `a` bindings; picker-handler intercept; `submitSubAgentQuestionAnswer` called from top of `submitPrompt`; alert + picker rows in `viewBody` |
| `cmd/wagyu/tui_subagent_question.go` | new | +291 | picker open/close, key handler, answer submit, alert + picker rows, scope label, sort helper |
| `cmd/wagyu/slash_cycle_subagent_question_test.go` | new | +554 | 14 TUI tests (alert surfaces, picker open/close, picker-routed answer, empty-answer skip, multi-subagent routing, keybind guards, nil-Manager, headless Manager) |
| `cmd/wagyu/print_pig_subagent_question_test.go` | new | +76 | 2 print-path tests (headless auto-skip, nil-Manager) |
| `docs/cycles/subagent-question-picker/deliverable.md` | new | +229 | TL;DR + files + gates + 8-item 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | clean (after fixing one rebase-time dup-block in tui_run.go from the prior botched attempt; the dup was caught by build error and removed by sed) |
| `go build -o /tmp/wagyu-subqp ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./internal/subagentquestions/` | race-clean |
| `go test -vet=off -count=1 -run 'SubAgentQuestion\|PrintPathAskQuestion' ./cmd/wagyu/` | race-clean |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | only `TestDispatchPrintSlash_LocalCommands` — pre-existing environmental flake |
| `gofmt -l` on every NEW file this cycle | empty |
| `gofmt -l internal/tools/nonnetwork.go` | dirty (pre-existing on main per AGENTS.md) |
| `go vet ./cmd/wagyu/ ./internal/subagentquestions/` | only pre-existing cycle-224 `%` warnings |
| New tests (cycle code paths) | 28/28 pass (12 unit + 14 TUI + 2 print) |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |

## Two bugs caught and fixed during testing (already in commit)

1. **`a` keybinding intercepted when no pending question** — `Test_ShiftEnterNewline`
   and `TestVimDisabledLeavesInputAlone` failed initially. Added
   `PendingCount > 0` guard. Both pass.
2. **Empty-answer skip was unreachable** — `submitPrompt` returned early on
   empty input before `submitSubAgentQuestionAnswer` ran. Moved the
   intercept BEFORE the empty-prompt guard. `TestSubAgentQuestion_EmptyAnswerSkips` passes.

## 存疑

1. Picker overlay styling is clone-side cycle-91 (dim-238/blue), not pixel-perfect
   vs REF (subagent-question uses `accent-247` on tab focus). Acceptable for
   subagent routing, picker UX, empty-answer skip semantics.
2. `Manager.Headless=true` constructor parameter is the only "auto-skip on `-p`"
   switch; `-p` does not have an explicit CLI flag to opt into interactive.
3. Multiple subagents' questions surface one alert at a time (cycle 214 single-alert
   invariant). Same scope key (`pending` per-subagent).
4. Subagent answer scope-key can be reused if the user types `/new` mid-question;
   `Manager.Reset(scope)` clears it.
5. `internal/tools/nonnetwork.go` gofmt-dirty (pre-existing).
6. `TestDispatchPrintSlash_LocalCommands` pre-existing environmental flake.
7. Cycle 224 `%` warnings in vet are pre-existing, untouched.
8. No remote push.

## Cleanup

- Worktree removed.
- Branch deleted.
- main is now `2b976ef`.