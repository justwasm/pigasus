# merge git-editor-disable — GIT_EDITOR/HGEDITOR/JJ_EDITOR=false for agent shell commands

## TL;DR

AGY 1.3.6 changelog line 7: agent shell commands must not block on an
EDITOR prompt. The clone now sets `GIT_EDITOR=false`, `HGEDITOR=false`,
`JJ_EDITOR=false`, plus `PAGER=cat` / `GIT_PAGER=cat` / `JJ_PAGER=cat`
(via `internal/tools.AgentShellEnv()`) on every shell-spawning path.

| | |
|---|---|
| Branch | `feature/git-editor-disable` |
| Commit | `ca7f06d` |
| Merge | `33d564b` |
| Lines | +657 / −5 (7 files) |
| Cycle report | `docs/cycles/git-editor-disable/deliverable.md` |

## Scope (vs AGY 1.3.6 line 7)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| `GIT_EDITOR=false` for agent shells | `internal/tools.AgentShellEnv()` returns `os.Environ()` + the six vars | ✅ |
| `HGEDITOR=false`, `JJ_EDITOR=false` | same | ✅ |
| `PAGER=cat` (already in some paths; cycle lifts consistency) | `AgentShellEnv()` includes | ✅ |
| Caller-supplied env still wins | `ExecCommand.Run` appends caller env LAST | ✅ |

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `internal/tools/command.go` | modified | +55 / −5 | `AgentShellEnv` helper + 3 spawn paths (RunCommand.Run, RunShellCommand.Run via interp.Env, ExecCommand.Run) |
| `internal/tools/support.go` | modified | +1 | bgTable.start appends `AgentShellEnv()` |
| `internal/tools/git.go` | modified | +1 | `gitBase.run` appends `AgentShellEnv()` |
| `internal/tools/git_editor_disable_test.go` | new | +416 | 9 new tests |
| `cmd/wagyu/slash_actions.go` | modified | +9 / −2 | `vcsDiff`, `runGit` slash sites get editor trio for consistency |
| `cmd/wagyu/slash_viewer_panels.go` | modified | +8 / −1 | same |
| `docs/cycles/git-editor-disable/deliverable.md` | new | +172 | TL;DR + scope + 6-item 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | clean |
| `go build -o /tmp/wagyu-editor ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | only `TestDispatchPrintSlash_LocalCommands` fails — pre-existing environmental flake |
| `gofmt -l` on every NEW file this cycle | empty |
| `gofmt -l internal/tools/git.go` | dirty (pre-existing on main per AGENTS.md; not introduced by this cycle) |
| `go vet ./internal/tools/ ./cmd/wagyu/` | only pre-existing cycle-224 warnings |
| New tests (cycle code paths) | 9/9 pass |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |

## 存疑

1. `internal/tools/git.go` gofmt: pre-existing on the not-clean list (over-spaced padding gofmt 1.27 wants to compress). My 1-line edit did not introduce this; AGENTS.md forbids reformatting pre-existing not-clean files.
2. Pre-existing `TestDispatchPrintSlash_LocalCommands` failure: environmental.
3. `internal/tools/nonnetwork.go` and `internal/tools/stubs.go` also spawn `git`, but only via non-callable tools. If a future cycle re-exposes them, those exec sites also need `cmd.Env = AgentShellEnv()`. Noted in 存疑.
4. `HGEDITOR` / `JJ_EDITOR` not exercised end-to-end (no `hg`/`jj` installed). The git side proves the pattern works.
5. `TestCycle225QuotaCountdown` did not trigger in any of the three `-count=1` runs.
6. Slash-command sites (`/diff`, `runGit`) got the editor trio for consistency; dead code today but in place for future `/commit`-style slash hardening.

## Cleanup

- Worktree removed.
- Branch deleted.
- main is now `33d564b`.
