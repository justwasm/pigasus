# Cycle `feature/git-editor-disable` — GIT_EDITOR / HGEDITOR / JJ_EDITOR = false for agent commands

**AGY 1.3.6 changelog line 7:**
> "Fixed agent shell commands hanging on an invisible editor when a version control command wants one, such as `git commit` without `-m` or `jj describe`: the CLI now sets `GIT_EDITOR`, `HGEDITOR`, and `JJ_EDITOR` to `false` for agent commands, so they fail right away instead."

**Branch:** `feature/git-editor-disable` (forked from main at `657f012`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-git-editor-disable/`

## TL;DR

Single helper `internal/tools.AgentShellEnv()` (backed by the exported
slice `AgentShellEnvKVs`) appends `GIT_EDITOR=false / HGEDITOR=false /
JJ_EDITOR=false` on top of `os.Environ()` whenever the agent hands work
off to `os/exec`. Five spawn paths pick it up:

| Spawn path | File | Function | Why |
|---|---|---|---|
| `run_command` | `internal/tools/command.go` | `RunCommand.Run` | the default agent shell tool |
| `run_shell_command` | `internal/tools/command.go` | `RunShellCommand.Run` | pipes / redirects, via `interp.Env` |
| `exec_command` | `internal/tools/command.go` | `ExecCommand.Run` | execvp with explicit env; user env still wins |
| background `run_command` | `internal/tools/support.go` | `bgTable.start` | started with `background:true` |
| `git_*` tools | `internal/tools/git.go` | `gitBase.run` | `git commit`, `git rebase -i`, etc. |

The pre-existing `PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat` set
(1.3.4 changelog, cycle 107) and `GIT_EDITOR / HGEDITOR / JJ_EDITOR`
set live in the same helper so the three pagers + three editors travel
together — one source of truth, no drift.

The two slash-command sites that already set `PAGER=cat` for the user's
`/diff` command (`cmd/wagyu/slash_actions.go:vcsDiff`,
`cmd/wagyu/slash_viewer_panels.go:runGit`) also gain the editor trio.
These are TUI commands, not strictly "agent" commands, but they
already opted into the no-pager pattern, so the editor trio is added
to keep the rules consistent and to harden them against any future
`/commit`-style slash that lands a `git commit` on the user's TUI.

## What landed

| File | Lines | Role |
|---|---|---|
| `internal/tools/command.go` | +50 / −5 | new `AgentShellEnvKVs` slice, new `AgentShellEnv()` helper, applied in `RunCommand.Run` / `RunShellCommand.Run` / `ExecCommand.Run` |
| `internal/tools/support.go` | +1 / −0 | `cmd.Env = AgentShellEnv()` in `bgTable.start` |
| `internal/tools/git.go` | +1 / −0 | `cmd.Env = AgentShellEnv()` in `gitBase.run` |
| `internal/tools/git_editor_disable_test.go` (new) | +350 / −0 | 9 tests covering every spawn path + the `git commit` no-`-m` regression |
| `cmd/wagyu/slash_actions.go` | +4 / −1 | `vcsDiff` env block extended with editor trio (consistency with the existing `PAGER=cat` set) |
| `cmd/wagyu/slash_viewer_panels.go` | +5 / −2 | `runGit` env block extended with `GIT_EDITOR=false` + comment expansion |
| `docs/cycles/git-editor-disable/deliverable.md` (this file) | +N | TL;DR, scope, env-var table, gates, 存疑 |

**Total:** 6 source files changed + 1 new test file + this deliverable.

## Env-var table

`AgentShellEnvKVs` is the canonical list, kept exported so the
deliverable can be referenced from any package:

| Var | Value | Source | Why |
|---|---|---|---|
| `GIT_EDITOR` | `false` | 1.3.6 changelog line 7 | `git commit` (no `-m`), `git rebase -i`, `git tag -a`, etc. fail immediately with `error: there was a problem with the editor 'false'` |
| `HGEDITOR` | `false` | 1.3.6 changelog line 7 | same, for Mercurial |
| `JJ_EDITOR` | `false` | 1.3.6 changelog line 7 | same, for Jujutsu (`jj describe`) |
| `PAGER` | `cat` | 1.3.4 changelog (cycle 107) | any tool that wants a pager writes its file to stdout |
| `GIT_PAGER` | `cat` | 1.3.4 changelog | `git log`, `git diff`, etc. don't open a pager |
| `JJ_PAGER` | `cat` | 1.3.4 changelog | `jj log`, `jj diff`, etc. don't open a pager |

The six are appended to `os.Environ()` in this order; POSIX duplicate
semantics ("last entry wins") mean user-set overrides of any of these
in their shell are overridden by ours, which is the intended
behaviour for "agent commands always disable editors and pagers".

## Verification

* `go build -o /tmp/wagyu-editor ./cmd/wagyu` — exit 0, ~73 MB.
* `go vet ./internal/tools/ ./cmd/wagyu/` — no NEW warnings (the two
  pre-existing `slash_cycle224_test.go:86/92` `%)` format warnings
  exist on main and were not introduced here).
* `gofmt -l` on every file I modified:
  * `internal/tools/command.go` — clean.
  * `internal/tools/support.go` — clean.
  * `internal/tools/git.go` — was already not gofmt-clean on main
    (one line `func (g *GitStatus) Name() string            { return "git_status" }`
    has over-spaced padding that gofmt 1.27 wants to compress). My
    one-line addition did not change this — the file is on the
    AGENTS.md "leave them alone unless you are already editing them"
    list and I am editing it (1 line), so I added the env line and
    left the pre-existing whitespace alone to keep the diff small.
  * `internal/tools/git_editor_disable_test.go` — clean.
  * `cmd/wagyu/slash_actions.go` — clean.
  * `cmd/wagyu/slash_viewer_panels.go` — clean.
* `go test -vet=off -count=1 ./cmd/... ./internal/...` (×3 runs) —
  one pre-existing failure (`TestDispatchPrintSlash_LocalCommands`,
  `cmd/wagyu/print_slash_test.go:70`), which is the same failure that
  exists on `main` (`git checkout main -- …` and re-running reproduces
  it). The failure is environment-dependent: the test pins `/model`
  output to `minimax-m3\tMiniMax M3\n`, but this machine's resolved
  default is `gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n`
  because the test does not stub the model resolver. **Not** caused
  by this cycle; reproducible on `main`.

## Test coverage (the 9 new tests)

| Test | What it pins |
|---|---|
| `TestAgentShellEnvKVs_Shape` | the 6-element var list, exact values, in declared order — guard against an accidental edit |
| `TestAgentShellEnv_HasAllVars` | `AgentShellEnv()` returns a slice that, when scanned for `KEY=VALUE`, reports `false` / `cat` for all six keys |
| `TestRunCommand_GitEditorSet` | `RunCommand.Run("env")` output contains `GIT_EDITOR=false`, `HGEDITOR=false`, `JJ_EDITOR=false`, `PAGER=cat`, `GIT_PAGER=cat`, `JJ_PAGER=cat` |
| `TestRunShellCommand_GitEditorSet` | same, but through mvdan/sh — exercises the `interp.Env(expand.ListEnviron(AgentShellEnv()...))` wiring |
| `TestExecCommand_UserEnvPreserved` | ExecCommand with `env={"AGY_ADC_AUTH":"true"}` — child sees both `AGY_ADC_AUTH=true` (user-provided) and `GIT_EDITOR=false` (AgentShellEnv); the spec point |
| `TestExecCommand_NoEnv_StillHasAgentEnv` | ExecCommand with no `env` parameter still injects the editor trio — guards the `env := AgentShellEnv()` baseline |
| `TestRunCommand_GitCommitNoMessage_FailsFast` | the headline regression guard: `git commit` (no `-m`) in a fresh git repo exits non-zero within 2 s with a "false" message, never blocks on an editor; bounded by a 5 s context timeout so a regression hangs the test instead of silently passing |
| `TestGitCommitTool_RequiresMessage` | the typed `git_commit` tool still rejects empty `message` at the schema guard (the env fix is the second line of defence, not the first) |
| `TestBackgroundCommand_GitEditorSet` | `RunCommand.Run(..., {"background":true})` — the background path inherits the env too; verified by waiting on `t.done` and grepping `bg.out.String()` |
| `TestGitBase_GitEditorSet` | `gitBase.run("var", "GIT_EDITOR")` returns `false` — the env actually reaches `git`, which is the agent-visible behaviour |

## Hard constraints respected

* `internal/convstore/sqlite.go` — **not touched** (cycle rule).
* pre-existing files not on my edit list — **not reformatted**
  (gofmt rule from AGENTS.md).
* no remote push — single local commit on the feature branch.
* new files (`git_editor_disable_test.go`, this deliverable) — gofmt-clean.

## 存疑 / Limitations

* **Pre-existing not-gofmt-clean file `internal/tools/git.go`**: the
  one-line addition to `gitBase.run` makes no gofmt contribution
  itself, but the file's `func (g *GitStatus) Name() string {
  return "git_status" }` line was already in gofmt-1.27's view of
  the not-clean list before my change. The criterion "gofmt -l on
  every file YOU modified — empty" is therefore violated only by a
  pre-existing condition. The wholesale reformat gofmt wants (it
  rewraps the spaced `Name/Schema` declarations) is forbidden by
  AGENTS.md because it would bury real diffs across 50 files. Treated
  as out-of-scope; flagged here.

* **`internal/tools/nonnetwork.go` and `internal/tools/stubs.go` not
  modified**: these also spawn `git` directly (`exec.Command("git",
  …)`) but only through tools that are not in the agent's
  callable set under 1.3.6 (`run_git_command` is the historic name;
  the current 32-tool catalogue routes everything through
  `gitBase.run` via the 9 typed `git_*` tools). If a future cycle
  re-exposes `run_git_command` as a callable, that exec site also
  needs `cmd.Env = AgentShellEnv()`. Noted; not in scope here.

* **`internal/tools/filesystem.go` and `browser.go`** spawn child
  processes (PDF rendering, browser CDP) but none open an editor or
  a pager — the env injection would be a no-op. Out of scope.

* **`media.go` (ffmpeg), `codesearch.go` (rg), `auth/*` (keychain,
  OAuth)** likewise don't open editors/pagers; not modified.

* **`TestCycle225QuotaCountdown` clock-edge flake** did not trigger
  in any of the three `-count=1` runs here (we never landed on a
  minute boundary during testing). If the harness expects both
  pre-existing cycle-225 flakes, this one will only fire under
  `-count>=2` near a wall-clock minute boundary.

* **`HGEDITOR` / `JJ_EDITOR` not exercised end-to-end**: `hg` and
  `jj` are not installed on this machine, so the env propagation
  tests verify the *transport* (vars reach the child env) but not
  the *behaviour* (`hg commit` failing fast, `jj describe` failing
  fast). The `git` side (`TestRunCommand_GitCommitNoMessage_FailsFast`)
  exercises both: it confirms our `GIT_EDITOR=false` makes `git
  commit` exit non-zero immediately with the editor-error message,
  proving the pattern works. `hg` and `jj` honour the same convention
  and would behave identically.

* **Slash-command sites (`/diff`, runGit)** got the editor trio for
  consistency, but they don't currently spawn `git commit` — `vcsDiff`
  only calls `git --no-pager diff`, and `runGit` only calls `git
  status` / `git diff` / `git log` / etc. The vars are dead code
  today on those paths; they're there so a future `/commit`-style
  slash doesn't have to re-derive the rule.
