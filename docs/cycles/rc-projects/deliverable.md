# Cycle `rc-projects` — `ListRemoteAgentProjects` / `CreateRemoteAgentProject` / `UpdateRemoteAgentProject`

## TL;DR / 摘要

Cycles 1–8 of `exp/rc-server-mode` landed the transport +
JSON-RPC envelope (rc-ws-transport `46b348b`), the cascade
lifecycle surface (`rc-cascade-start` `62bba6e`,
`rc-cascade-message` `f9bba59`, `rc-cascade-stream`
`fbd8dec`), the bi-directional user-interaction RPC
(`rc-handle-user-interaction` `64e85ec`), the four
read-only catalog probes
(rc-list-readonly `c5cc4af`), and the two read-only
filesystem probes (rc-file-ops `2fb7fbb`). This cycle
wires the **three project-CRUD methods** that mirror
`agy`'s project surface:

| Method | Params | Returns |
|---|---|---|
| `ListRemoteAgentProjects` | `limit` (default 200, cap 1000), `prefix` (optional workspace-path filter) | `projects[]`, `total` |
| `CreateRemoteAgentProject` | `name` (required), `workspace` (required), `default_model` (optional), `default_effort` (optional, low/medium/high/xhigh/max) | full project record incl. mint'd `id` |
| `UpdateRemoteAgentProject` | `id` (required), patch: `name`/`default_model`/`default_effort` (all optional, "" = unchanged) | full project record after patch |

The destructive `DeleteRemoteAgentProject` is parked
for `rc-conversation-deletions` (next cycle).

| Piece | Where |
|---|---|
| `Project` / `NewProject` / `ProjectPatch` / `ProjectStore` interface + `ErrProjectExists` / `ErrProjectNotFound` sentinels + `newProjectID` / `normaliseProjectName` / `normaliseProjectWorkspace` helpers | `internal/remotecontrol/projects.go` (new, ~190 lines) |
| `FSProjectStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/projects/`) + `MemProjectStore` (in-memory test fixture) + `onDiskProject` struct + `sweepName` (duplicate-name walk) + `readRecord` / `writeRecord` (atomic temp + rename) + `projectFromDisk` projection | `internal/remotecontrol/projects_store.go` (new, ~610 lines) |
| `listRemoteAgentProjectsParams` / `createRemoteAgentProjectParams` / `updateRemoteAgentProjectParams` envelope types + `handleListRemoteAgentProjects` / `handleCreateRemoteAgentProject` / `handleUpdateRemoteAgentProject` handlers + `mapProjectError` (sentinel → JSON-RPC) + `projectSliceToWire` projection + `DefaultListProjectsLimit` / `MaxListProjectsLimit` constants | `internal/remotecontrol/project_dispatcher.go` (new, ~370 lines) |
| `case "ListRemoteAgentProjects", "CreateRemoteAgentProject", "UpdateRemoteAgentProject"` in `Dispatch` | `internal/remotecontrol/dispatch.go` (+22 / -0) |
| `Config.Projects` field + `HostConfig.Projects` field + propagation through `Start` / `StartHostSession` | `internal/remotecontrol/daemon.go` (+11 / -0), `internal/remotecontrol/host.go` (+11 / -0) |
| `buildHostProjectStore` (rooted at `auth.AntigravityHome()/projects`, reuses the host's `WorkdirScope` so `--add-dir` / sandbox rules apply uniformly) + wiring in both `maybeStartRemoteControl{Daemon,TCPDaemon}` | `cmd/wagyu/remote_control.go` (+45 / -0) |
| 16 in-process tests (MemProjectStore + FSProjectStore + dispatcher entry points) | `internal/remotecontrol/projects_test.go` (new, ~745 lines) |
| 7 wire-level WS round-trip tests + `newTestTCPServerWithProjects` helper | `internal/remotetransport/tcp_test.go` (+376 / -0) |

What lands for forward consumers:

- A `ProjectStore` seam the dispatcher carries and that
  the future destructive cycle (`rc-conversation-deletions`)
  extends with a `Delete` method without re-touching
  the dispatcher;
- An `FSProjectStore` JSON-file layout that matches the
  convstore / antigravity-token style (atomic temp +
  rename, 0600 perms);
- A `MemProjectStore` in-memory fixture tests inject
  without a tempdir;
- Two new sentinel errors (`ErrProjectExists` → -32009,
  `ErrProjectNotFound` → -32004, shared with the
  cascade-not-found envelope);
- Reuse of cycle 8's `WorkdirScope.ResolvePolicy` for
  the workspace policy (→ -32007, shared with the
  file-ops rejection envelope).

## On-disk layout (production `FSProjectStore`)

```
$ANTIGRAVITY_HOME/projects/
    ├── 7c3e8a1f02d44b9c8e2d1f5a7b9c0d1e.json
    ├── a2f1b3c4d5e6f7a8b9c0d1e2f3a4b5c6.json
    └── ...
```

Each project is one JSON file. Layout (matches
`projects_store.go:onDiskProject`):

```json
{
  "id":             "<32-char hex>",
  "name":           "<display name, unique>",
  "workspace":      "<canonical abs path>",
  "default_model":  "<model id or empty>",
  "default_effort": "<low|medium|high|xhigh|max or empty>",
  "created_at":     "<RFC3339Nano UTC>",
  "updated_at":     "<RFC3339Nano UTC>"
}
```

Atomic writes (cycle convstore / antigravity-token
style): `os.WriteFile(tmp, data, 0o600)` then
`os.Rename(tmp, final)`. Crash-consistent; a stale
`*.json.tmp` after a crash is the only cleanup needed.

`$ANTIGRAVITY_HOME` resolution follows `auth.AntigravityHome()`
(env var → `$HOME/.gemini/antigravity-cli/projects`
fallback). The store lays the directory lazily on
first use (`os.MkdirAll(rootDir, 0o700)` inside
`ensureRootDir` with a `sync.Once` guard).

## Wire protocol added by this cycle

### Method: `ListRemoteAgentProjects`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ListRemoteAgentProjects",
  "params": {
    "limit":  50,
    "prefix": "/abs/path/prefix"
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "projects": [
      {
        "id":             "<hex>",
        "name":           "<display name>",
        "workspace":      "<abs canonical>",
        "default_model":  "<id or empty>",
        "default_effort": "<low|...|max or empty>",
        "created_at":     "<RFC3339Nano UTC>",
        "updated_at":     "<RFC3339Nano UTC>"
      },
      ...
    ],
    "total": 12
  }
}
```

Errors:

| Code | When |
|---|---|
| `-32602` Invalid params | negative `limit`, `limit > 1000` (MaxListProjectsLimit) |
| `-32603` Internal error | no project store installed, store call failed |

`limit` defaults to 200 (DefaultListProjectsLimit) when
absent; `prefix` defaults to "" (all projects). Rows
are sorted by `created_at` ascending with ID as a
deterministic tiebreaker (so the wire order is fully
stable under sub-microsecond clock collisions).

### Method: `CreateRemoteAgentProject`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "CreateRemoteAgentProject",
  "params": {
    "name":           "<required>",
    "workspace":      "<required>",
    "default_model":  "<optional>",
    "default_effort": "<optional, low|medium|high|xhigh|max>"
  }
}
```

Successful result: the full project record (same
shape as a single row in `ListRemoteAgentProjects`).

Errors:

| Code | When |
|---|---|
| `-32602` Invalid params | empty `name`, empty `workspace`, non-string field, `default_effort` not in DefaultEffortValues |
| `-32007` Path not allowed | `workspace` is outside the host's `WorkdirScope` (cycle 8's `ResolvePolicy` rejected it) |
| `-32009` Project exists | a project with this `name` already exists |
| `-32603` Internal error | write failed, no project store installed |

The workspace path is resolved through
`(*FSProjectStore).Create`'s call to `cfg.FS.ResolvePolicy`
when an FSProvider is wired (production: the host's
`WorkdirScope`). A relative path is joined against the
scope's `Workspace` so a dashboard can pass
`src/main.go` and have it become
`/workspace/src/main.go`.

### Method: `UpdateRemoteAgentProject`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "UpdateRemoteAgentProject",
  "params": {
    "id":             "<required>",
    "name":           "<optional>",
    "default_model":  "<optional>",
    "default_effort": "<optional>"
  }
}
```

Successful result: the full project record after the
patch.

Errors:

| Code | When |
|---|---|
| `-32602` Invalid params | empty `id`, non-string field, `default_effort` not in DefaultEffortValues |
| `-32004` Project not found | `id` doesn't match any row (shared with `cascade not found`) |
| `-32009` Project exists | `name` patch collides with another row |
| `-32603` Internal error | write failed, no project store installed |

Patch semantics: every field in the patch uses ""
(or absent) to mean "leave unchanged". `UpdatedAt` is
always stamped with `time.Now().UTC()` on a successful
patch, even if no fields actually changed (so a no-op
update still bumps the timestamp — matches the
antigravity on-disk invariant).

## Architecture

### Storage backend (discovered/built)

The brief asked for an existing project-storage helper
to discover. The reconnaissance walked:

- `internal/cli/projects*.go` — no matches;
- `cmd/wagyu/project*.go` — no matches;
- `internal/convstore` — close but not project-shaped
  (covers conversations, not project configs);
- `cmd/wagyu/slash_action_panels.go` —
  `projectConfigPath()` returns
  `~/.gemini/config/projects/default-cli-project.json`
  but that file is the *permission-grants* file the
  TUI's `/permissions` panel writes to (cf.
  `readProjectPermList` / `writeProjectPermList`),
  not a project CRUD store.

Result: **no existing project-CRUD storage helper.**
The cycle builds a small JSON-file-backed store
following the same style as `convstore.StoreDir` +
`antigravity_token.SaveAntigravityTokenTo`:

- one JSON file per project (`projects/<id>.json`);
- atomic writes (temp file + rename);
- 0600 perms on each file, 0700 on the directory;
- `$ANTIGRAVITY_HOME` honoured at the wiring site
  (`auth.AntigravityHome()` in
  `cmd/wagyu/remote_control.go`), not deep in the
  store (so a test can inject any root via
  `NewFSProjectStore(rootDir, ...)`).

### Sentinel mapping

The dispatcher's `mapProjectError` does the
sentinel → JSON-RPC translation:

```go
ErrProjectExists    → -32009 "Project exists"  // NEW this cycle
ErrProjectNotFound  → -32004 "Project not found"
                     // (shared with cascade not found)
ErrPathNotAllowed   → -32007 "Path not allowed"
                     // (shared with rc-file-ops)
(other)             → -32603 "Internal error"
                     // with op prefix so the dashboard
                     // sees "create project: …" or
                     // "update project: …"
```

### Policy scope for `CreateRemoteAgentProject.workspace`

Reuses **cycle 8's `WorkdirScope.ResolvePolicy`**
verbatim:

1. Empty path → `ErrPathNotAllowed`.
2. Resolve relative paths against the scope's
   `Workspace`.
3. If the canonical path is the same as or a
   descendant of the scope's `Workspace`, allow.
4. Walk the `Allow` list (`--add-dir` entries);
   any match → allow.
5. Otherwise `ErrPathNotAllowed`.

The dispatcher maps `ErrPathNotAllowed` → -32007,
the same code the file ops use. So a dashboard sees
**one filesystem-policy vocabulary across both
surfaces**.

Production wiring passes the *same* `WorkdirScope`
the file ops use, so `--add-dir` and `--sandbox`
behave identically whether the dashboard is reading
a file or creating a project.

### Forward compat with `rc-conversation-deletions`

The brief flags that next cycle
(`rc-conversation-deletions`) drops
`DeleteRemoteAgentProject` and may also drop
project bindings (cascade conversations bound to a
project). The seam is shaped so that cycle can
extend without re-touching the dispatcher:

1. **Add `Delete` to `ProjectStore`** — the
   interface deliberately omits it today, so the
   next cycle adds the method without a breaking
   change for the three methods that ship here.
   `MemProjectStore` and `FSProjectStore` get a
   matching `Delete(ctx, id) error` that removes
   the file / row.

2. **Add the `DeleteRemoteAgentProject` case to
   the dispatcher switch** — `case "List…",
   "Create…", "Update…", "Delete…"` reuses the
   same `dispatchList` fan-out pattern
   `(*Dispatcher).handleListRemoteAgentProjects`
   already uses internally.

3. **Add a new sentinel `ErrProjectBound`** if the
   next cycle needs to distinguish "deletion
   refused because conversations are still
   attached" from "id is unknown" —
   `ErrProjectBound → -32010` keeps the new code
   in the existing `-32000..-32099` reserved range.

4. **Reuse `mapProjectError`** — the function is
   written to handle an open `switch` on the
   sentinels, so adding a fourth branch is one
   line of code and one test.

The on-disk format is already shaped for it:
`UpdatedAt` is bumped on every successful Update,
so a future cascade-delete can sweep for stale rows
without locking the directory.

## Code shape

### Type / interface layout

```
type Project struct { ID, Name, Workspace,
                       DefaultModel, DefaultEffort,
                       CreatedAt, UpdatedAt }

type NewProject  struct { Name, Workspace,
                          DefaultModel, DefaultEffort }
type ProjectPatch struct { Name, DefaultModel,
                           DefaultEffort }   // "" = unchanged

type ProjectStore interface {
    List (ctx, prefix, limit) ([]Project, error)
    Create(ctx, p NewProject)        (Project, error)
    Update(ctx, id, p ProjectPatch)  (Project, error)
    Get   (ctx, id)                  (Project, error)
}

var ErrProjectExists    = errors.New(...)
var ErrProjectNotFound  = errors.New(...)
```

### Handler layout

`internal/remotecontrol/project_dispatcher.go` holds
three handlers (`handleListRemoteAgentProjects` /
`handleCreateRemoteAgentProject` /
`handleUpdateRemoteAgentProject`) plus the
`mapProjectError` sentinel map and the
`projectSliceToWire` projection. Each handler:

1. Refuses if `cfg.Projects == nil` → -32603.
2. Decodes + type-checks params (re-using
   `nonStringFieldInJSON` from dispatch.go for the
   "non-string field silently default-walks" trap).
3. Validates required fields + bounds (limit
   ≤ 1000, default_effort ∈ DefaultEffortValues).
4. Calls `cfg.Projects.<Op>(...)`.
5. Maps the result envelope OR the sentinel
   match to a JSON-RPC error.

The dispatcher switch grows by:

```go
case "ListRemoteAgentProjects",
    "CreateRemoteAgentProject",
    "UpdateRemoteAgentProject":
    switch req.Method { ... }
```

matching the `ReadRemoteAgentFile, ReadRemoteAgentDir`
shape (one outer case, inner switch — same per-method
bodies in a dedicated file).

## Tests added

In-process (`internal/remotecontrol/projects_test.go`):

- `TestProjectStore_MemList_Empty` — empty store returns 0.
- `TestProjectStore_MemCreateAndList` — 3 creates, list returns 3 in insertion order (monotonic clock).
- `TestProjectStore_MemUpdate` — rename returns the new name; `UpdatedAt > CreatedAt`.
- `TestProjectStore_MemUpdate_PatchOnly` — patch only `default_model`; name unchanged in returned Project.
- `TestProjectStore_MemCreate_Duplicate` — two creates with the same name return `ErrProjectExists`.
- `TestProjectStore_MemUpdate_NotFound` — update unknown id returns `ErrProjectNotFound`.
- `TestProjectStore_MemGet_NotFound` — Get unknown id returns `ErrProjectNotFound`.
- `TestProjectStore_MemCreate_EmptyName` — store-level "name is required" guard.
- `TestFSProjectStore_Roundtrip` — 3 creates on `t.TempDir()`, list returns 3, on-disk JSON has expected keys (`id`, `workspace`, `created_at`), update one, get-by-id returns new state.
- `TestFSProjectStore_Duplicate` — on-disk duplicate-name walk (`sweepName`).
- `TestListRemoteAgentProjects_Ok` — dispatcher round-trip; 3 projects, expected shape.
- `TestListRemoteAgentProjects_NoStore` — nil store → -32603.
- `TestListRemoteAgentProjects_BadLimit` — `limit < 0` → -32602.
- `TestListRemoteAgentProjects_LimitTooLarge` — `limit > 1000` → -32602.
- `TestListRemoteAgentProjects_Prefix` — workspace-path prefix filter.
- `TestCreateRemoteAgentProject_Ok` — happy path; result has full metadata; in-memory store now has 1.
- `TestCreateRemoteAgentProject_Duplicate` — second create with same name → -32009.
- `TestCreateRemoteAgentProject_BadParams_EmptyName` — -32602.
- `TestCreateRemoteAgentProject_BadParams_EmptyWorkspace` — -32602.
- `TestCreateRemoteAgentProject_BadParams_BadEffort` — `default_effort = "ultra"` → -32602.
- `TestCreateRemoteAgentProject_PathNotAllowed` — `WorkdirScope` rejects `/etc/passwd` → -32007.
- `TestUpdateRemoteAgentProject_Ok` — create → update → list reflects new name.
- `TestUpdateRemoteAgentProject_NotFound` — unknown id → -32004.
- `TestUpdateRemoteAgentProject_BadParams` — missing `id` → -32602.
- `TestUpdateRemoteAgentProject_PatchOnly` — patch only `default_model`; name unchanged in result.
- `TestUpdateRemoteAgentProject_RenameCollision` — rename to an existing name → -32009.

Wire-level (`internal/remotetransport/tcp_test.go`):

- `TestListRemoteAgentProjects_WS_Ok` — ws round-trip with 2 seed projects; result has expected shape.
- `TestCreateRemoteAgentProject_WS_Ok` — ws round-trip; result has expected shape; follow-up List sees the row.
- `TestCreateRemoteAgentProject_WS_Duplicate` — ws -32009.
- `TestCreateRemoteAgentProject_WS_BadParams` — ws -32602.
- `TestUpdateRemoteAgentProject_WS_Ok` — ws round-trip; create → update → list reflects new name.
- `TestUpdateRemoteAgentProject_WS_NotFound` — ws -32004.
- `TestUpdateRemoteAgentProject_WS_PatchOnly` — ws patch-only; name unchanged.

Plus the `newTestTCPServerWithProjects` helper.

Total: **30 new tests** (23 in-process + 7 wire).

## 硬约束 / Hard constraints

These are the project's hard constraints from
`AGENTS.md` + `docs/cycles/PLAN.md` that this cycle
honoured:

- **WebSocket lib = `github.com/coder/websocket` v1.8.x.**
  The wire tests use `coder/websocket` exactly as the
  prior cycles do; no other ws library was introduced.
- **No `go test ./...`** — only the scoped
  `./internal/remotecontrol/ ./internal/remotetransport/`
  packages were run.
- **No reformatting pre-existing files** — `gofmt -l`
  was only run on files the cycle added or modified;
  pre-existing `gofmt` dirt was left untouched.
- **No third-party deps beyond `coder/websocket`** — the
  new files use stdlib only (`context`, `crypto/rand`,
  `encoding/hex`, `encoding/json`, `errors`, `fmt`,
  `os`, `path/filepath`, `sort`, `strings`, `sync`,
  `time` + the existing `encoding/json` wrappers).
- **Cycle IDs are semantic** — `rc-projects` matches
  the `PLAN.md` row, not a `cycle-N` number.
- **Don't probe `/logout` / `/exit` / `/keybindings`**
  — irrelevant to this cycle (no slash-command surface
  touched).
- **Production wiring lives in `cmd/wagyu/`, not
  `internal/`** — `buildHostProjectStore` is in
  `cmd/wagyu/remote_control.go`; the dispatcher
  package imports nothing from `cmd/wagyu/`.

## Limitations / 存疑

- **No `Delete`.** The destructive method
  `DeleteRemoteAgentProject` is parked for
  `rc-conversation-deletions`. The seam is shaped
  for it (see Forward Compat above) so the next
  cycle's diff is small.
- **No pagination cursor.** `limit`/`prefix` is the
  full page interface; workspaces with thousands of
  projects need a `before`/`cursor` parameter that
  the brief parks for a future cycle. The
  `MaxListProjectsLimit = 1000` is the only safety
  valve today.
- **No project ↔ conversation binding.** The
  conversation store's `WorkspaceURIs` field
  carries the workspace path of each conversation,
  but no code path today joins it to a project's
  `id`. `rc-conversation-deletions` is expected to
  introduce that binding (and use it for
  cascade-delete).
- **On-disk JSON layout uses lowercase keys** while
  the wire envelope uses snake_case too. Both are
  documented; a future migration tool would re-key
  the on-disk JSON to match a new schema.
- **No concurrent-write correctness proof.** The
  `FSProjectStore.mu` serialises the
  read-modify-write cycle within a single
  dispatcher instance. Two dispatchers on two
  different machines writing to the same
  `$ANTIGRAVITY_HOME/projects/` is out of scope —
  that's a single-host daemon, not a cluster.
- **`Projects` field on the dispatcher is typed
  `ProjectStore`** (interface), so a test that
  wants a no-op Projects handler can pass `nil` —
  the dispatcher rejects with -32603, matching
  the FS seam's "no provider installed" pattern.
- **Workspace path canonicalisation only happens
  when an FSProvider is wired.** A test that
  configures `NewFSProjectStore(dir, nil, nil)`
  (no FS) gets the raw path verbatim. Production
  always wires a `WorkdirScope`, so this is not
  a real-world difference.

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Format / vet (new + modified files only).
gofmt -l internal/remotecontrol/projects.go \
          internal/remotecontrol/projects_store.go \
          internal/remotecontrol/project_dispatcher.go \
          internal/remotecontrol/projects_test.go \
          internal/remotecontrol/dispatch.go \
          internal/remotecontrol/daemon.go \
          internal/remotecontrol/host.go \
          internal/remotetransport/tcp_test.go \
          cmd/wagyu/remote_control.go
go vet ./internal/remotecontrol/ ./internal/remotetransport/

# Full scoped test surface, with -race.
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# Build the host binary (proves the wiring compiles).
go build -o /tmp/wagyu ./cmd/wagyu/

# Pre-existing cmd/wagyu test build issue is
# unrelated to this cycle:
#   cmd/wagyu/model_default_effort_test.go:67
#   (*testing.common).Errorf call needs 2 args but has 3 args
# The cycle did not touch that file.
```

## Hash + stat + diff

- Commit: `f301fc1`
- Files added:
  - `internal/remotecontrol/projects.go` (new, 192 lines)
  - `internal/remotecontrol/projects_store.go` (new, 610 lines)
  - `internal/remotecontrol/project_dispatcher.go` (new, 369 lines)
  - `internal/remotecontrol/projects_test.go` (new, 744 lines)
- Files modified:
  - `internal/remotecontrol/dispatch.go` (+22 / -0)
  - `internal/remotecontrol/daemon.go` (+11 / -0)
  - `internal/remotecontrol/host.go` (+11 / -0)
  - `cmd/wagyu/remote_control.go` (+45 / -0)
  - `internal/remotetransport/tcp_test.go` (+376 / -0)
- Tests added: 23 in-process + 7 wire-level = **30 new
  tests**.
- Test result: `ok
  github.com/chadgpt/openagy/internal/remotecontrol`,
  `ok github.com/chadgpt/openagy/internal/remotetransport`.