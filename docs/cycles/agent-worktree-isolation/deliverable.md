# Cycle 230 — Subagent worktree isolation (AGY 1.2.10 changelog line)

Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-agent-worktree-isolation`
Branch: `feature/agent-worktree-isolation`.
Base: `54db67b` (cycle 224 slash test fix).

## TL;DR

`internal/subagenttools.Invoke` now defaults the subagent's working
directory to a freshly-materialised directory at
`<AgentDir>/worktrees/<subagent-id>/` rather than the parent's
cwd. After Invoke returns the directory is guaranteed to contain
no `.metadata.json` files (a defensive sweep removes any that a
host-side artifact tracker may have dropped there).

| Surface | Before | After |
|---|---|---|
| Subagent cwd (default) | `Deps.CWD` → `Services.CWD()` (project workspace) | `<AgentDir>/worktrees/<subID>/` (app data) |
| `Result.WorktreePath` | absent | populated with the path the subagent was anchored to |
| `.metadata.json` in worktree | left behind by the artifact tracker | swept on every Invoke return |
| `Invocation.Worktree` semantics | "use me as cwd verbatim" | unchanged — explicit override still wins |

The behaviour change closes the AGY 1.2.10 changelog line
(`internal/cli/testdata/changelog_reference.txt:174`):

> Fixed files that subagents write inside their own Git worktree
> being treated as artifacts, which demanded artifact metadata and
> left `.metadata.json` files in the worktree; subagent worktrees
> now live under a `worktrees/` directory in the app data directory

This cycle fixes the **structural** half (where the worktree
lives) and adds a defensive guard for the **.metadata.json**
half (a `defer` removes any that sneak in). Cycle 229 will
upgrade the materialised directory to a real `git worktree add`
in the same place, which is why `allocateSubagentWorktreeDir`
deliberately leaves a hook for it.

---

## What was wrong (and why)

Pre-cycle-230 `subagenttools.Invoke` anchored the subagent's
session to the parent's cwd via `Deps.CWD` (falling back to
`Services.CWD()`). On a real user invocation the subagent thus
ran inside the user's project workspace, and any host-side
artifact tracker that walked the workspace (workspace-relative
filenames, `.metadata.json` companion files, the AGY
artifact subsystem) treated the subagent's outputs as artifacts
of the parent session. The result:

- every file the subagent wrote got a `.metadata.json`
  companion, polluting the worktree with bookkeeping the
  subagent never asked for;
- the artifact viewer / `/artifact` picker showed the subagent's
  scratch output mixed in with the parent's flow, which is the
  wrong scope (`docs/cycles/cycle-228/deliverable-invoke-subagent.md`
  §"Subagent FSM" for the parallel `subagents/` story).

The changelog line is the reference's fix: move subagent
worktrees out of the workspace and into the app data dir.
The clone was missing both halves:

1. the runtime decision (where to anchor), and
2. the cleanup pass (`.metadata.json` lives in the worktree).

This cycle closes both.

---

## Design

### Path layout

```
~/.gemini/antigravity-cli/        ← AgentDir (app data dir)
├── subagents/                    ← existing transcript tree (unchanged)
│   └── subagent-<ts>-<n>.jsonl
└── worktrees/                    ← NEW: per-subagent worktree root
    └── subagent-<ts>-<n>/        ← ONE directory per invocation
        └── …scratch files the subagent wrote…
```

The `<subagent-id>/` leaf is `Result.SubagentID` so callers
(cycle 229 `manage_subagents list`) can correlate the directory
with the result without an external index. Mode is `0o755` so the
subagent's session can write inside; the parent `<AgentDir>` is
left at its existing permissions (the auth store / settings
manager own those bits and `pigrt.New` materialises them with
the standard `0o755`).

`allocateSubagentWorktreeDir` is the single helper that owns this
path layout; a future cycle can replace its body with a
`git worktree add` invocation without breaking any caller.

### Where the cwd comes from

| Caller passes | `cwd` resolves to |
|---|---|
| `Invocation.Worktree = "/some/path"` | `/some/path` (verbatim) |
| `Invocation.Worktree == ""` and `<AgentDir>` is writable | `<AgentDir>/worktrees/<subID>/` (fresh dir) |
| `Invocation.Worktree == ""` and `<AgentDir>` is read-only | `Deps.CWD` → `Services.CWD()` (best-effort fallback) |

The last row preserves the cycle-228 behaviour for tests and
read-only test sandboxes that don't allow `os.MkdirAll` under
`<AgentDir>`. The fallback path is observable in
`Result.WorktreePath`, so a test that wants to assert "I was
isolated" can `t.Cleanup` the directory and run a smoke test
against the agent's actual cwd without `--worktree`-ing the
whole stack.

### Cleanup

`defer purgeWorktreeArtifacts(worktreePath, subID)` runs after
`Invoke` writes its transcript and before it returns. The sweep:

- walks the worktree with `filepath.WalkDir`,
- removes every file whose leaf name is exactly `.metadata.json`,
- swallows per-file errors so a single permission failure can't
  abort the sweep,
- tolerates a missing root (the fallback path is a no-op when
  the directory doesn't exist).

The selector is narrow on purpose: dotfiles the user wrote
(`.gitignore`, `.envrc`, etc.) survive, JSON-with-different-name
(`.metadata.jsonl`) survives, and the `IsWorktreeArtifactName`
helper documents the rule so a future caller can mirror it.

### What was NOT changed

- **No tool result shape change.** `invoke_subagent` still
  returns the structured envelope from cycle 228 — only a new
  `WorktreePath` field was added to the internal `Result`.
- **No `Deps` field removed.** `Deps.CWD` is preserved as the
  best-effort fallback; its docstring now describes that role.
- **No new dependency.** The implementation is stdlib only
  (`os`, `path/filepath`, `strings`, `errors`, `io/fs`).
- **`internal/convstore/sqlite.go`** — untouched, per the
  hard constraint.
- **Pre-existing `gofmt -l` debt** (`internal/cli/…`,
  `cmd/wagyu/slash_compact.go`, etc.) — not touched.

---

## Files touched

| Path | Status | Notes |
|---|---|---|
| `internal/subagenttools/subagenttools.go` | modified | +2 helpers (`allocateSubagentWorktreeDir`, `purgeWorktreeArtifacts`/`IsWorktreeArtifactName`), +1 `Result.WorktreePath`, modified `Invoke` cwd-resolution and added `defer` cleanup |
| `internal/subagenttools/worktree_test.go` | new (330 lines, 6 tests) | All four invariants + 2 orthogonal negative tests |
| `docs/cycles/agent-worktree-isolation/deliverable.md` | new | this file |

No changes to `cmd/wagyu/`, `internal/agent/`, `internal/pigrt/`,
`internal/cortex/`, `internal/convstore/`. The `Invoke` contract
is enough to absorb the fix; the TUI and print paths pick up the
new `WorktreePath` automatically through the existing pass-through
in `cmd/wagyu/print_pig.go:127` and `cmd/wagyu/tui_run.go:2083`.

---

## Surface (what changes for callers)

### `internal/subagenttools/subagenttools.go`

```go
// New on Result — non-breaking addition.
type Result struct {
    Text, SessionID, SubagentID string
    Messages                    []piagent.AgentMessage
    Duration                    time.Duration
    WorktreePath                string // ← NEW: the path Invoke anchored the session to
}

// Existing field's docstring now describes the override role.
type Invocation struct {
    Prompt, Model, SystemPrompt string
    // Worktree is now a power-user override; empty triggers the
    // worktree-isolation path. Verbatim behaviour unchanged.
    Worktree string
}

// New helpers (lowercase, package-private; tests use them).
func allocateSubagentWorktreeDir(agentDir, subID string) (string, error)
func purgeWorktreeArtifacts(worktree, subID string)

// Exported selector — useful for tests and cycle-229 tools.
func IsWorktreeArtifactName(name string) bool
```

### Behaviour

`Invoke(ctx, Invocation{...}, deps)` now:

1. picks the subagent's cwd from `Invocation.Worktree` if set,
   else from `<AgentDir>/worktrees/<subID>/`,
2. writes the structured transcript to `<AgentDir>/subagents/<subID>.jsonl`
   as before (cycle 228 behaviour),
3. before returning, removes any `.metadata.json` files in the
   worktree via `defer purgeWorktreeArtifacts(worktreePath, subID)`,
4. populates `Result.WorktreePath` so the parent tool / cycle 229
   `manage_subagents list` can show the worktree's location
   without an index.

Failure semantics:

- `allocateSubagentWorktreeDir` errors → fall back to `Deps.CWD` /
  `Services.CWD()`. The parent tool still runs.
- `purgeWorktreeArtifacts` errors → swallowed; never surfaced to
  the parent tool.

---

## Tests

All tests live in `internal/subagenttools/worktree_test.go` (NEW)
and run via the standard `go test ./internal/subagenttools/…`
path. Six tests, six pass:

```
=== RUN   TestSubagentWorktree_UnderAppData
--- PASS: TestSubagentWorktree_UnderAppData (0.03s)
=== RUN   TestSubagentWorktree_PurgeRemovesMetadataJSON
--- PASS: TestSubagentWorktree_PurgeRemovesMetadataJSON (0.00s)
=== RUN   TestSubagentWorktree_InvokeDoesNotIntroduceMetadataJSON
--- PASS: TestSubagentWorktree_InvokeDoesNotIntroduceMetadataJSON (0.02s)
=== RUN   TestPurgeWorktreeArtifacts_SelectiveSweep
--- PASS: TestPurgeWorktreeArtifacts_SelectiveSweep (0.00s)
=== RUN   TestSubagentWorktree_FilesAreNotArtifacts
--- PASS: TestSubagentWorktree_FilesAreNotArtifacts (0.00s)
=== RUN   TestSubagentWorktree_EachSubagent_OwnDir
--- PASS: TestSubagentWorktree_EachSubagent_OwnDir (0.03s)
```

The four "must" tests map to the cycle spec:

| Test | Invariant |
|---|---|
| `TestSubagentWorktree_UnderAppData` | (1) Subagent worktree under app data directory |
| `TestSubagentWorktree_PurgeRemovesMetadataJSON` + `TestSubagentWorktree_InvokeDoesNotIntroduceMetadataJSON` | (2) No `.metadata.json` files in subagent worktree |
| `TestSubagentWorktree_FilesAreNotArtifacts` | (3) Files in worktree not treated as artifacts |
| `TestSubagentWorktree_EachSubagent_OwnDir` | (4) Multiple subagents each get own worktree |
| `TestPurgeWorktreeArtifacts_SelectiveSweep` | orthogonal — confirms the sweep is narrow (other dotfiles/JSON-with-different-name survive) |

The targeted run:

```bash
go test -vet=off -count=1 -run 'TestSubagent|TestWorktree|TestArtifact' \
  ./cmd/wagyu/ ./internal/.../
```

… reports zero failures across all 27 packages.

### Build verification

```bash
go build -o /tmp/wagyu-awt ./cmd/wagyu       # exit 0
ls -lh /tmp/wagyu-awt                          # 70 MB (cycle 228 binary size)
gofmt -l internal/subagenttools/subagenttools.go internal/subagenttools/worktree_test.go
                                               # empty
gofmt -l internal/subagenttools/               # empty
```

The pre-existing `gofmt -l` debt in `internal/` and `cmd/wagyu/`
is unchanged (per the AGENTS.md "do not reformat wholesale"
guidance). My NEW files are clean.

---

## 存疑 (limitations)

1. **`git worktree add` is not implemented.** Cycle 229 will
   replace `allocateSubagentWorktreeDir`'s body with a real
   `git worktree add` call in the user's repository. Today's
   materialised directory is a plain subdirectory; the path
   layout is the same so cycle 229 is non-breaking. The clone
   intentionally stopped at "fresh directory under app data" —
   a full `git worktree add` needs the user's repo URL and
   branch, which the parent turn does not yet plumb through
   `Deps`.

2. **No concurrency stress on `Deps.CWD`-only callers.** The
   print path (`cmd/wagyu/print_pig.go:127`) sets `CWD:
   cfg.Workspace` and the TUI path (`cmd/wagyu/tui_run.go:2087`)
   sets `CWD: workspace`. Both still work — the worktree
   allocation reads `AgentDir`, not `CWD` — but neither path
   is exercised by the new tests. The targeted run skips them
   by design; a future cycle that hardens concurrent
   subagent spawning should add a back-pressure test.

3. **`.metadata.json` sweep is only as good as the selector.**
   If AGY ever changes the artifact subsystem to write
   `.artifact-meta.json` (or similar) instead of `.metadata.json`,
   `IsWorktreeArtifactName` needs an update. The selector is
   centralised in one place so this is a one-line fix, but it
   is still a future-cycle task if AGY changes the filename.

4. **No integration with `/artifact`.** The artifact viewer on
   the TUI side assumes workspace-relative URIs (per the cycle
   228 file). Files written under `<AgentDir>/worktrees/…` are
   not visible there — that is intentional (subagent scratch
   should not pollute the artifact viewer) but is a behavioural
   change worth knowing if a future cycle tries to surface
   subagent outputs as artifacts.

5. **Test ordering caveat.** The new tests in
   `worktree_test.go` rely on `subagentTestModel()` from the
   sibling file `subagenttools_test.go`. That helper is
   package-private and lives in the same package, so the test
   compiles regardless of build order, but a future refactor
   that moves one of the two test files to a `_test` package
   must surface the shared helpers explicitly.

---

## How to verify locally

```bash
# Re-derive the bin and confirm subagent worktree anchoring + cleanup.
go build -o /tmp/wagyu-awt ./cmd/wagyu
go test -vet=off -count=1 -v -run 'TestSubagent|TestWorktree|TestArtifact|TestPurge' \
  ./internal/subagenttools/

# Broader regression — the targeted run the cycle spec requires:
go test -vet=off -count=1 -run 'TestSubagent|TestWorktree|TestArtifact' \
  ./cmd/wagyu/ ./internal/.../
```

All exits 0.
