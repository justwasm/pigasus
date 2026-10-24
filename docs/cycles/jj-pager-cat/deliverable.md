# Cycle jj-pager-cat — `JJ_PAGER=cat` for `runGit`

Branch: `feature/jj-pager-cat`
Base: `54db67b` (cycle 224)
Model: agw / MiniMax-M3

## Gap

AGY 1.3.3 changelog lines 50-51:

> "Fixed jj (Jujutsu VCS) command output being broken by the `less`
> pager — `JJ_PAGER=cat` is now set alongside `GIT_PAGER=cat` so commands
> like `jj log` show full output instead of paging through interactive
> less."

## Verify-before-dispatch finding

A `grep` for `JJ_PAGER|GIT_PAGER|PAGER=` over `cmd/` and `internal/`
returned four sites. Three already include `JJ_PAGER=cat`:

| Site | Status pre-cycle |
|---|---|
| `internal/tools/command.go:62` (AgentShellEnvKVs) | ✅ has JJ_PAGER |
| `cmd/wagyu/slash_actions.go:589` (vcsDiff) | ✅ has JJ_PAGER |
| `cmd/wagyu/slash_viewer_panels.go:463` (runGit) | ❌ missing JJ_PAGER |

The gap is the third row: `runGit` sets `PAGER=cat / GIT_PAGER=cat /
GIT_EDITOR=false / LC_ALL=C` but not `JJ_PAGER=cat`. Since `vcsDiff` in
`slash_actions.go` already handles the three-VCS `jj log` case, and the
agent shell tools pick up `AgentShellEnvKVs`, this is a consistency fix
rather than a missing capability — the contract is now "wherever any
pager-disabling var is set, all three are set together".

## Change

`cmd/wagyu/slash_viewer_panels.go`:

- Extracted the inline env slice into a package-level `runGitEnvKVs` var
  (mirrors `internal/tools.AgentShellEnvKVs` in shape: a named slice +
  a `runGitEnv()` helper that prepends `os.Environ()`).
- Added `JJ_PAGER=cat` to the slice.
- Updated the doc comment on `runGit` and the slice to record the 1.3.3
  lines 50-51 rationale.

Final `runGitEnvKVs`:

```
PAGER=cat
GIT_PAGER=cat
JJ_PAGER=cat
GIT_EDITOR=false
LC_ALL=C
```

`runGit` now reads `cmd.Env = runGitEnv()` — single source of truth,
testable without invoking git.

## Why `runGit` (git-only) takes `JJ_PAGER=cat`

Even though `runGit` only ever execs `git`, the JJ_PAGER override travels
alongside GIT_PAGER=cat for three reasons:

1. The 1.3.3 changelog fix is stated as "alongside GIT_PAGER=cat"; keeping
   them in lockstep avoids the regression where a future caller pipes git
   output through a jj-aware step and the pager wedges the agent.
2. The three other VCS-aware env slices (`AgentShellEnvKVs`, `vcsDiff`,
   and now `runGitEnvKVs`) all carry the same trio. Splitting them invites
   drift.
3. The cost is one extra `KEY=VALUE` pair in the child env — measurable
   in nanoseconds, not behaviour.

`HGEDITOR=false` and `JJ_EDITOR=false` are deliberately not added to
`runGitEnvKVs`: `runGit` only execs `git`, so they would be no-ops, and
the multi-VCS trio lives in `vcsDiff` where it's actually meaningful.

## Files

| File | Status | Lines | Purpose |
|---|---|---|---|
| `cmd/wagyu/slash_viewer_panels.go` | modified | +37 / -7 | Add `JJ_PAGER=cat` to `runGit`; extract `runGitEnvKVs` + `runGitEnv()` |
| `cmd/wagyu/run_git_env_test.go` | new | 207 | Six tests pinning the env-var contract |
| `docs/cycles/jj-pager-cat/deliverable.md` | new | this file | Cycle report |

`internal/convstore/sqlite.go` — untouched (hard constraint).

## Tests

Six tests in `cmd/wagyu/run_git_env_test.go`:

| Test | What it pins |
|---|---|
| `TestRunGitEnvKVs_Shape` | exact slice contents + order (catches accidental edits) |
| `TestRunGitEnv_GitPagerSet` | `GIT_PAGER=cat` survives in the env (pre-existing, guarded) |
| `TestRunGitEnv_JjPagerSet` | `JJ_PAGER=cat` survives — the 1.3.3 fix |
| `TestRunGitEnv_BothPagersSet` | both pager overrides present + `PAGER=cat` too (the trio) |
| `TestRunGitEnv_OtherVarsUntouched` | parent-process vars (PATH, sentinel) not stripped |
| `TestRunGitEnv_OverridesWinOnDuplicate` | our value is the LAST occurrence (POSIX duplicate-env rule) |
| `TestRunGitEnv_PrependsParentEnv` | `os.Environ()` entries precede our kvs (the override-wins ordering) |

The headline four requested by the dispatch spec are the
_GitPagerSet / _JjPagerSet / _BothPagersSet / _OtherVarsUntouched cases;
the last two (`_OverridesWinOnDuplicate`, `_PrependsParentEnv`) are
bonus guards on the ordering invariant documented at
`runGitEnv()` line: 116.

## Verification

```text
$ go build -o /tmp/wagyu-jjp ./cmd/wagyu        ; echo exit=$?
exit=0

$ go vet ./cmd/wagyu/                            ; echo exit=$?
exit=0

$ gofmt -l cmd/wagyu/run_git_env_test.go cmd/wagyu/slash_viewer_panels.go
(no output — both files gofmt-clean)

$ go test -vet=off -count=1 -run 'TestRunGitEnv' ./cmd/wagyu/
ok      github.com/chadgpt/openagy/cmd/wagyu    0.892s

$ go test -vet=off -count=1 ./cmd/... ./internal/... 2>&1 | tail -10
--- FAIL: TestDispatchPrintSlash_LocalCommands (0.00s)
    print_slash_test.go:70: ... want "minimax-m3\tMiniMax M3\n"
FAIL
FAIL    github.com/chadgpt/openagy/cmd/wagyu    26.084s
ok      github.com/chadgpt/openagy/internal/tools    3.916s
[... all other packages OK ...]
```

The single failing test (`TestDispatchPrintSlash_LocalCommands`) is the
**documented pre-existing flake**: the test pins `/model` output to
`minimax-m3\tMiniMax M3` but this machine's model catalogue resolves
that to `gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)`. The same
test fails on `main` (54db67b) without any change — confirmed:

```text
$ cd /Users/gear/Documents/GitHub/openagy && \
    go test -vet=off -count=1 -run TestDispatchPrintSlash_LocalCommands ./cmd/wagyu/
--- FAIL: TestDispatchPrintSlash_LocalCommands (0.00s)
    ... want "minimax-m3\tMiniMax M3\n"
```

Not caused by this cycle.

## Hard constraints — checklist

| Constraint | Status |
|---|---|
| `internal/convstore/sqlite.go` not touched | ✅ untouched |
| New files gofmt-clean | ✅ `run_git_env_test.go` gofmt-clean |
| Pre-existing not-clean files not reformatted | ✅ only edited `slash_viewer_panels.go` in place; no wholesale reformat |
| `go build -o /tmp/wagyu-jjp ./cmd/wagyu` exit 0 | ✅ |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` passes except pre-existing flakes | ✅ one pre-existing flake only |
| 4+ unit tests covering the requested cases | ✅ 7 tests (4 headline + 3 ordering guards) |
| Use agw / MiniMax-M3 | ✅ |
| Branch not pushed | ✅ |
| Single commit | ✅ |
| Worktree at `/private/var/folders/.../opencode/openagy-jj-pager-cat` | ✅ |

## 硬约束 / Limitations

- The `JJ_PAGER=cat` change in `runGitEnvKVs` is exercised by the test
  suite but does not change any user-visible behaviour today, because
  `runGit` only execs `git` (git does not consult `JJ_PAGER`). The value
  of the fix is **drift prevention**: when a future caller pipes git
  output through a jj-aware step, or when the helper is generalised to
  call other VCS, the pager override is already in place. 【推测】on
  that future caller — the test pinning is the regression guard.
- No `HGEDITOR=false` / `JJ_EDITOR=false` added to `runGitEnvKVs`. This
  is intentional (the helpers `runGit` execs git only) and noted in the
  slice's doc comment. If `runGit` is later generalised to support
  multi-VCS, those vars must be added at the same time. 【推测】on the
  future shape.
- `TestRunGitEnv_OverridesWinOnDuplicate` verifies the ordering invariant
  but does not exercise an actual `execve` — it asserts the last
  occurrence of each key in the returned slice. POSIX guarantees that
  `execve` takes the last value for duplicate keys, but the test
  documents the ordering rather than measuring execve directly.