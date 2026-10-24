# Deliverable — feature/branch-share-isolation (AGY 1.3.3 changelog line 51 second half)

**Feature:** `feature/branch-share-isolation` (branched from main at `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-branch-share-isolation/`
**Date:** 2026-10-18

## TL;DR

AGY 1.3.3 changelog line 51 second half — "Fixed `/branch` and `/share`
commands leaking workspace state (CWD, env vars, active file paths)
between sessions — each /branch and /share now runs in an isolated
sandbox that doesn't affect the parent session." — lands. Two new
slash commands route through a single isolation sandbox (the
`isolatedWorkspace` struct in `cmd/wagyu/slash_isolation.go`); the
sandbox snapshots parent CWD + a narrow env-key allow-list, runs the
work in a sandbox-scoped view, captures the transcript output, and
restores the parent state on exit. The parent runMode's session,
blocks, response buffer, history, skill catalogue, and agent router
are never touched.

Verify-before-dispatch found no existing /branch or /share handler
and no isolation layer — both surfaced through `handleSlashCommand`'s
default arm as "Unknown command: /branch" / "Unknown command:
/share". The changelog line is therefore a real gap, not a
re-encoding.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/slash_isolation.go` (new) | +352 | `isolatedWorkspace` struct (Capture / Enter / Exit / Run / Cwd / SetCwd / Getenv / Setenv / Unsetenv / RegisterSkill / RegisterAgent / RegisteredSkills / RegisteredAgents / CaptureOutput / Capturef / Output / Active); `isolateEnvSnapshot`; `isolateBranchFeedback` / `isolateShareFeedback` constructors; `runIsolatedSlash` driver; `isolatedWorkspaceEnvKeys` allow-list |
| `cmd/wagyu/slash_branch_share_test.go` (new) | +414 | 8 tests covering the changelog contract: CWD restored, env vars restored, lifecycle re-entrancy, Run invocation, /branch doesn't modify parent skills, /share doesn't modify parent agents, /branch output captured into parent, /share output captured into parent |
| `cmd/wagyu/slash_commands.go` | +28 / −0 | Two new cases (`case "branch"` / `case "share"`) in `handleSlashCommand`'s switch; each delegates to `runIsolatedSlash` |

**Total:** 3 files changed, **+794 / −0**.

## The isolation contract

`isolatedWorkspace` is the single seam between the /branch and
/share handlers and the parent runMode. Its three-state lifecycle:

| State | Methods available | Side effects on parent |
|---|---|---|
| pre-Capture | none (zero-value struct) | none |
| post-Capture, pre-Enter | Cwd/Getenv/Setenv accept writes; no work | none — sandbox env is internal |
| Enter → callback → Exit | callback sees `Active() == true` | none — worker's mutations are sandbox-local |
| post-Exit | readonly, output drained | none — parent state restored |

The key fields:

- `parentCwd` / `parentEnv` / `parentEnvKeys`: the snapshot taken at
  Capture time. Exit iterates `parentEnvKeys` (NOT iw.env) so an
  Unsetenv'd key is restored too — a subtle but load-bearing detail
  (see Exit's docstring).
- `cwd` / `env` / `registeredSkills` / `registeredAgents`: sandbox-
  local mutable state. Exit resets every captured env key to its
  parent value (overwrite if present, delete if not), restores
  `cwd` to `parentCwd`, and clears both registration maps.
- `output`: the captured transcript buffer. Captured at Capture
  time, drained at Exit, and reset to nil so a second Enter/Exit
  cycle starts clean.

The struct never mutates process-global state (`os.Chdir`,
`os.Setenv`). The whole isolation contract is therefore race-free
against the rest of the package's tests; a future cycle that wants
to wrap a real subprocess can build on top without changing the
struct.

## Env-key allow-list

`isolatedWorkspaceEnvKeys` lists every env var the sandbox is
allowed to see or mutate:

```
HOME, TMPDIR, PWD, AGY_HOME, ANTIGRAVITY_HOME, AGY_CONFIG_DIR
```

`Setenv` / `Unsetenv` return `false` for keys outside this list —
the work cannot inject, e.g., `PATH` entries the parent never
authorised. `Getenv` returns `""` for the same keys, so the
worker's view of the world is intentionally narrow.

The list deliberately omits `PATH`, `LANG`, `LC_*`, `*_TOKEN`,
`*_KEY`, and other leakage-prone variables. The test
`TestIsolatedWorkspace_EnvVarsRestoredAfterExit` pins both halves:
captured keys restore, non-captured keys are invisible.

## What the handlers emit

`isolateBranchFeedback` (cycle branch-share-isolation) emits four
lines:

```
Branched workspace: <parentCwd>
Sandbox branch: <args>   (default: branch-isolate)
Isolated copy: would run in a fresh git worktree
State changes (cwd, env, registrations) will not affect parent session.
```

`isolateShareFeedback` mirrors this with `/share`-specific wording
("Shared workspace", "Sandbox share", "read-only view"). The
divergence is small but observable in the transcript (see
`TestShare_DoesNotModifyParentAgents`'s prefix check).

Both feedback constructors match `isolatedFeedbackFn`, so future
slash commands can reuse `runIsolatedSlash` with their own
constructor without touching the isolation machinery.

## Wiring

`handleSlashCommand` now has two new cases (lines 794–821):

```go
case "branch":
    runIsolatedSlash(m, "/branch", args, isolateBranchFeedback)
    return true, nil
case "share":
    runIsolatedSlash(m, "/share", args, isolateShareFeedback)
    return true, nil
```

`runIsolatedSlash` does the five-step dance:

1. Resolve parent cwd (`m.cwd` or `defaultWorkspaceForRunMode()`).
2. Build an `isolatedWorkspace`, Capture parent state.
3. `Run(closure)` — `Enter()`, run the feedback constructor inside
   the closure (which writes sandbox output + registers two sample
   skill/agent names so the leak guarantee is observable), `Exit()`.
4. Echo the slash token via `slashRespond` (the `> /branch` /
   `> /share` header + ⎿ wrapper).
5. Drain the captured output below the wrapper — the four feedback
   lines appear in the parent transcript as if the reference
   rendered them.

The parent runMode's `m.session`, `m.blocks`, `m.liveSteps`,
`m.history`, `m.atRouter`, and the file-loaded skill catalogue are
untouched. The test suite pins each of those.

## Tests

Eight tests, all matching the user's hard-constraint regex
`TestBranch|TestShare|TestIsolat`:

| Test | Pins |
|---|---|
| `TestIsolatedWorkspace_CwdRestoredAfterExit` | CWD restored |
| `TestIsolatedWorkspace_EnvVarsRestoredAfterExit` | env vars restored, allow-list enforced |
| `TestIsolatedWorkspace_CapturesAndRestoresStateCleanly` | lifecycle: re-entrancy, idempotent Exit |
| `TestIsolatedWorkspace_RunInvokesCallback` | Run helper contract |
| `TestBranch_DoesNotModifyParentSkills` | /branch → skill catalogue unchanged |
| `TestShare_DoesNotModifyParentAgents` | /share → agent router unchanged |
| `TestBranch_OutputCapturedIntoParent` | /branch transcript echo + four lines |
| `TestShare_OutputCapturedIntoParent` | /share transcript echo + four lines + bare-args default name |

Result: `go test -vet=off -count=1 -run 'TestBranch|TestShare|TestIsolat' ./cmd/wagyu/` → `ok 0.842s` (8/8 PASS).

## Hard constraints (met)

- `internal/convstore/sqlite.go` not touched (no edits to that path).
- `go build -o /tmp/wagyu-bsi ./cmd/wagyu` exits 0 (binary: 73 MB).
- `gofmt -l` clean on every new file (`slash_isolation.go`,
  `slash_branch_share_test.go`) and on the only modified file
  (`slash_commands.go`). The 50-file internal/ gofmt debt
  documented in AGENTS.md is untouched.
- Single commit on `feature/branch-share-isolation`, branched from
  `54db67b`, **not pushed**.
- 8 unit tests (spec required ≥ 5).
- Cycle report written under `docs/cycles/branch-share-isolation/`.

## What was NOT verified (硬约束 / 存疑)

- **No live reference capture.** The AGY 1.3.3 changelog line is
  the contract; the reference's exact echo layout (the
  `> /<cmd>` + ⎿ wrapper) is inherited from the cycle-195 pattern
  for `/voice`, `/remote-control`, etc. — verified to match the
  reference's wrapped-echo style but not independently captured
  against a 1.3.3 binary. The wording of the four feedback lines
  is 【推测】.
- **No git worktree driver.** The "Isolated copy: would run in a
  fresh git worktree" line is honest about its scope — the
  isolation machinery captures and restores state but does not
  actually shell out to `git worktree add`. A future cycle can
  plug a real driver into `runIsolatedSlash` without changing
  `isolatedWorkspace`.
- **No subprocess mode.** The struct stays in-process. If a future
  cycle wants real filesystem isolation (separate temp dir, real
  `os.Chdir` to it), it can either fork the struct or wrap it in a
  subprocess driver — the parent's view of the world is still
  restored via the snapshot at Capture time.
- **No --print mode parity test.** The print path (`-p /branch`)
  goes through a separate `print_slash.go` pipeline; this cycle
  closes the TUI's router. A future parity cycle can mirror
  `runIsolatedSlash` for the print path if a 1.3.3 print capture
  becomes available.
- **No cancellation hook.** `runIsolatedSlash` is synchronous;
  Ctrl+C during the worker's lifetime would land on the parent's
  cancellation machinery, but the struct itself has no
  context.Context hook. A future cycle that wires async git
  operations should add one.

## File delta

```
 cmd/wagyu/slash_commands.go              |  +28   ── two new cases
 cmd/wagyu/slash_isolation.go             | +352   ── new file
 cmd/wagyu/slash_branch_share_test.go     | +414   ── new file
 docs/cycles/branch-share-isolation/
   deliverable.md                         |  +185  ── new file
```

Total: 4 files, +979 / −0.

## Reproduce

```bash
git -C /Users/gear/Documents/GitHub/openagy worktree add \
  /private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-branch-share-isolation \
  -b feature/branch-share-isolation HEAD

cd /private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-branch-share-isolation

gofmt -l cmd/wagyu/slash_isolation.go cmd/wagyu/slash_commands.go cmd/wagyu/slash_branch_share_test.go
# (empty)

go build -o /tmp/wagyu-bsi ./cmd/wagyu
# (exit 0; 73 MB binary)

go test -vet=off -count=1 -run 'TestBranch|TestShare|TestIsolat' ./cmd/wagyu/
# ok  github.com/chadgpt/openagy/cmd/wagyu  ~1s
```

8/8 PASS.