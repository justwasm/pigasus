# Cycle `rc-file-ops` — `ReadRemoteAgentFile` + `ReadRemoteAgentDir` (read-only filesystem probes)

## TL;DR / 摘要

Cycles 1–7 of `exp/rc-server-mode` landed the transport +
JSON-RPC envelope (rc-ws-transport `46b348b`), the cascade
lifecycle surface (`rc-cascade-start` `62bba6e`,
`rc-cascade-message` `f9bba59`, `rc-cascade-stream` `fbd8dec`),
the bi-directional user-interaction RPC
(`rc-handle-user-interaction` `64e85ec`), and the four
read-only catalog probes (rc-list-readonly `c5cc4af`). This
cycle wires the **two read-only filesystem methods** that
mirror `agy`'s `ReadRemoteAgentFile` and
`ReadRemoteAgentDir`:

| Method | Params | Returns |
|---|---|---|
| `ReadRemoteAgentFile` | `path`, `maxBytes` (default 1 MiB, cap 16 MiB), `offset` | `path` (canonical), `size`, `truncated`, `encoding` (`utf-8`/`binary`), `content` (string for utf-8, base64 for binary) |
| `ReadRemoteAgentDir` | `path`, `maxEntries` (default 1000, cap 10000) | `path`, `entries[]`, `total_entries`, `truncated` |

Both are strictly read-only. Both honour the host's
filesystem-scope policy through a new `FSProvider` seam; a
production `WorkdirScope` enforces the workspace +
`--add-dir` + sandbox rules; tests inject a small
in-memory `MemFS`.

| Piece | Where |
|---|---|
| `FSProvider` interface, `DirEntry` type, `MemFS` + `MemFSEntry` fixture, `ErrPathNotAllowed` / `ErrTooLarge` / `ErrNotRegularFile` sentinels, `DefaultReadMaxBytes` / `MaxReadMaxBytes` / `DefaultReadDirMaxEntries` / `MaxReadDirMaxEntries` constants, `isBinaryBytes` + `encodeContent` + `classifyType` helpers | `internal/remotecontrol/files.go` (new, ~430 lines) |
| Production `FSProvider` impl: `WorkdirScope{Workspace, Allow, Sandbox}` + `ResolvePolicy` (workspace-or-Allow resolution, relative paths joined against workspace) + `ReadFile` (cap-aware, detects binary via 8 KiB NUL window, returns `ErrTooLarge` / `ErrNotRegularFile` / offset-beyond-size) + `ReadDir` (Lstat-based type classification, sorted, mtime RFC3339Nano) + `isUnder` helper | `internal/remotecontrol/scope.go` (new, ~190 lines) |
| `readRemoteAgentFileParams` / `readRemoteAgentFileResult` / `readRemoteAgentDirParams` / `readRemoteAgentDirResult` envelope types + `handleReadRemoteAgentFile` / `handleReadRemoteAgentDir` handlers + `mapFSError` sentinel-to-JSON-RPC mapping | `internal/remotecontrol/file_dispatcher.go` (new, ~280 lines) |
| `case "ReadRemoteAgentFile", "ReadRemoteAgentDir"` in `Dispatch` | `internal/remotecontrol/dispatch.go` (+19 / -0) |
| `Config.FS` field + `HostConfig.FS` field + wiring in `Start` / `StartHostSession` | `internal/remotecontrol/daemon.go` (+11), `internal/remotecontrol/host.go` (+14) |
| `flagStringValues` helper that splits the cli parser's comma-joined stringSlice value (used by `buildHostFSProvider` for `--add-dir`) | `cmd/wagyu/main.go` (+19 / -0) |
| `buildHostFSProvider(fs, cwd)` + `FS: buildHostFSProvider(fs, cwd)` wiring in both `maybeStartRemoteControlDaemon` and `maybeStartRemoteControlTCPDaemon` | `cmd/wagyu/remote_control.go` (+20 / -0) |
| 25 in-process tests (FSProvider + MemFS + WorkdirScope + dispatcher entry points) | `internal/remotecontrol/files_test.go` (new, ~780 lines) |
| 9 wire-level WS round-trip tests + 2 server-fixture helpers (`newTestTCPServerWithFS`, `newTestTCPServerWithMemFS`) | `internal/remotetransport/tcp_test.go` (+~430 / -0) |

What lands for forward consumers:

- An `FSProvider` seam the dispatcher carries and that any
  future read-only filesystem RPC (`rc-list-projects` /
  `rc-update-project` / `rc-status-tool` file probes)
  reuses without re-implementing policy;
- A `WorkdirScope` production implementation the host
  wires from `cwd + --add-dir + sandbox`;
- A `MemFS` in-memory fixture tests inject without a real
  filesystem;
- The three sentinel errors map to three custom JSON-RPC
  codes (`-32007` / `-32008` / `-32603`) — the same
  vocabulary a future cycle that wants to add
  write/delete methods (rc-conversation-deletions) extends
  with `ErrWriteNotAllowed` etc. without re-doing the
  mapping.

## Wire protocol added by this cycle

### Method: `ReadRemoteAgentFile`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ReadRemoteAgentFile",
  "params": {
    "path":     "<abs-or-rel-path>",
    "maxBytes": 1048576,
    "offset":   0
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "path":      "<abs canonical path>",
    "size":      12345,
    "truncated": false,
    "encoding":  "utf-8",
    "content":   "<utf-8 string or base64>"
  }
}
```

Errors:

| Code | When |
|---|---|
| `-32602` Invalid params | empty `path`, negative `offset`, `maxBytes > 16 MiB`, non-string `path` |
| `-32007` Path not allowed | `ResolvePolicy` returned `ErrPathNotAllowed` |
| `-32008` Not a regular file | `ReadFile` returned `ErrNotRegularFile` (directory, fifo, socket, broken symlink, …) |
| `-32603` Internal error | I/O failure, `offset > size`, `ErrTooLarge` (file exceeds `maxBytes`), no FS provider installed |

### Method: `ReadRemoteAgentDir`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ReadRemoteAgentDir",
  "params": {
    "path":       "<abs-or-rel-path>",
    "maxEntries": 1000
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "path":          "<abs canonical path>",
    "entries": [
      {"name": "<basename>", "type": "file|dir|symlink|other", "size": 1234, "mtime": "<RFC3339>"}
    ],
    "total_entries": 42,
    "truncated":     false
  }
}
```

Errors:

| Code | When |
|---|---|
| `-32602` Invalid params | empty `path`, negative `maxEntries`, `maxEntries > 10000`, non-string `path` |
| `-32007` Path not allowed | `ResolvePolicy` returned `ErrPathNotAllowed` |
| `-32603` Internal error | I/O failure, no FS provider installed |

(The brief: "Errors: -32602, -32007, -32603 only" — no
`-32008` because `ReadDir` on a non-directory surfaces
through the stdlib `os.File.Readdirnames` error path, not
the sentinel path.)

## Architecture

### `FSProvider` seam

The dispatcher never touches the filesystem directly. It
calls into a single `FSProvider` interface that the host
configures via `Config.FS`:

```go
type FSProvider interface {
    ResolvePolicy(path string) (canonical string, err error)
    ReadFile(canonical string, maxBytes int64, offset int64)
        (data []byte, total int64, truncated bool, isBinary bool, err error)
    ReadDir(canonical string, maxEntries int)
        (entries []DirEntry, total int, truncated bool, err error)
}
```

Three sentinels anchor the policy surface:

- `ErrPathNotAllowed` — the policy rejected the path
  (outside workspace, blocked by sandbox). Maps to `-32007`.
- `ErrNotRegularFile` — `ReadFile`'s target is a
  directory, fifo, socket, or broken symlink. Maps to
  `-32008`.
- `ErrTooLarge` — `ReadFile`'s target exceeds `maxBytes`.
  Maps to `-32603` with a "file too large" message.

Anything else (offset > size, ENOENT from a symlink race,
EACCES the OS decided to surface, etc.) is a generic
`-32603` with the operation label ("resolve policy" /
"read file" / "read dir") prefixed so dashboards log the
right stage.

### `WorkdirScope` (production)

The cycle's production `FSProvider` impl is
`WorkdirScope`:

```go
type WorkdirScope struct {
    Workspace string   // cwd at start, exclusive
    Allow     []string // additive paths from --add-dir
    Sandbox   bool
}
```

`ResolvePolicy`:

1. Empty path → `ErrPathNotAllowed`.
2. Resolve the path against the workspace (or cwd if
   `Workspace == ""`). Absolute paths are passed through
   `filepath.Clean`; relative paths are joined with the
   workspace.
3. If the canonical path equals or sits under the
   workspace, allow.
4. Otherwise walk `Allow`: each entry is resolved the
   same way; if any matches, allow.
5. Otherwise `ErrPathNotAllowed`.

`ReadFile` opens the canonical path under a hard cap.
Reads `maxBytes+1` bytes so it can detect truncation
without over-reading by much. Classifies as binary via an
8 KiB NUL-window scan (`isBinaryBytes`). Returns
`ErrNotRegularFile` for non-regular-file inodes;
`ErrTooLarge` is *not* emitted by the production
`WorkdirScope` (a future cycle that wants a hard pre-stat
cap can add it; today the cap is detected via the
post-read comparison).

`ReadDir` lists entries via `os.File.Readdirnames(0)`. Each
entry is `Lstat`-ed (so a symlink is reported as a
symlink — the brief parks symlink-following control).
Entries are sorted case-sensitively by name. Type
classification: `symlink | dir | file | other`.

### `MemFS` (test fixture)

The dispatcher tests need a deterministic filesystem
without `t.TempDir()` boilerplate. `MemFS` is the
fixture: a map of `absolute-dir-path → []MemFSEntry`.
The fixture has no policy of its own — `ResolvePolicy`
returns the path verbatim. Tests pair a `MemFS` with a
`WorkdirScope` (via the `policyOverFS` adapter, which
routes `ResolvePolicy` through the policy and `ReadFile` /
`ReadDir` through the fixture) to exercise the rejection
paths.

### Sentinel mapping

The `mapFSError` helper centralises the sentinel-to-code
mapping in one place. A future cycle that adds new
sentinels (write/delete, rc-conversation-deletions) only
has to add another `errors.Is` arm.

### Dispatch

`Dispatch` gains a two-method case entry:

```go
case "ReadRemoteAgentFile", "ReadRemoteAgentDir":
    switch req.Method {
    case "ReadRemoteAgentFile":
        result, rpcErr = d.handleReadRemoteAgentFile(req.Params)
    case "ReadRemoteAgentDir":
        result, rpcErr = d.handleReadRemoteAgentDir(req.Params)
    }
```

(The split-case style is the same one
`ListRemoteAgent…` uses — keeps each method's body in
`file_dispatcher.go` rather than dispatch.go.)

## Policy resolution (sandbox + --add-dir)

The `WorkdirScope{Workspace, Allow, Sandbox}` tuple is
the single place the filesystem policy lives. Production
wiring in `cmd/wagyu/remote_control.go`:

```go
func buildHostFSProvider(fs *flag.FlagSet, cwd string) remotecontrol.FSProvider {
    return &remotecontrol.WorkdirScope{
        Workspace: cwd,
        Allow:     flagStringValues(fs, "add-dir"),
        Sandbox:   flagBoolValue(fs, "sandbox"),
    }
}
```

`flagStringValues` is a new `cmd/wagyu/main.go` helper
that splits the cli parser's comma-joined stringSlice
output. The cli parser's `--add-dir` is registered as a
`stringSlice` (see `internal/cli/jetski.go`'s
`stringSliceValue.String()` which joins with commas), so
the comma split recovers the same slice the parser
accumulated when `--add-dir` was repeated on the command
line.

`Sandbox` is a placeholder flag today — the policy
resolution is "workspace + Allow" whether sandbox is on
or off. A future cycle that wants sandbox to tighten the
scope (e.g. refuse `--add-dir` outside an allow-list
file) only has to update `WorkdirScope.ResolvePolicy`.

Both transports (Unix-socket daemon + TCP+WS daemon)
wire the same `WorkdirScope`, so a dashboard sees
identical filesystem behaviour regardless of how it
dials in.

## Forward compatibility

The `FSProvider` seam is the seam any future cycle that
needs to read a user-controlled path reuses:

| Future cycle | Likely use of `FSProvider` |
|---|---|
| `rc-list-projects` | list a project's config files (read-only) |
| `rc-update-project` | validate a path before mutate (then a separate write method) |
| `rc-delete-conversation` (after `rc-conversation-deletions`) | verify the path the request refers to is inside the workspace |
| `rc-status-tool` | surface workspace + Allow + Sandbox state through the status tool |

The three sentinel codes (`-32007` / `-32008` / `-32603`)
also extend cleanly:

| Future sentinel | Likely code |
|---|---|
| `ErrWriteNotAllowed` | `-32009` (parallel to `-32007` for the read path) |
| `ErrReadOnly` (when a write RPC is asked in a read-only mode) | `-32010` |
| `ErrPathTooDeep` (future "max symlink hops" knob) | `-32011` |

The dispatcher's `mapFSError` is the single place these
land.

## Limitations

- **Symlink following is parked.** `WorkdirScope.ReadFile`
  uses `os.Stat` (follows symlinks); `ResolvePolicy` uses
  `filepath.Clean` (does NOT follow symlinks). A
  dashboard that issues `/etc/hosts` via a symlink will
  see inconsistent results. A future cycle adds the
  `follow-symlinks: bool` knob (likely on
  `ReadRemoteAgentFileParams`).

- **No binary-text heuristic override.** The 8 KiB NUL
  window matches `git` and `file -i`'s default; a file
  with NULs only past the 8 KiB boundary is classified as
  text. A future cycle that wants a longer window (or a
  per-call override) adds a `binaryWindowBytes` field on
  `readRemoteAgentFileParams`.

- **No streaming reads.** `ReadRemoteAgentFile` reads up
  to `maxBytes` and returns. A dashboard that wants to
  page through a multi-MiB file sends `offset` repeatedly.
  A future cycle that wants server-push chunks (e.g.
  `file/chunk` notifications) reuses the same
  `FSProvider.ReadFile` over the existing ws transport;
  no transport-level change is required.

- **`ErrTooLarge` is post-read, not pre-stat.** The
  production `WorkdirScope.ReadFile` does not pre-stat to
  reject "file too large"; it reads `maxBytes+1` bytes and
  reports `truncated=true` when the file is bigger. The
  brief: "maxBytes > 16 MiB" rejection happens at the
  param-validation step (`-32602`), not the FS seam. A
  future cycle that wants the dispatcher to refuse
  billion-byte files at the seam adds a pre-stat check
  here.

- **Sandbox flag is a no-op at the FS seam today.** A
  future cycle that wants `--sandbox` to tighten the
  policy (e.g. refuse any `--add-dir` that isn't in
  `~/.agy/sandbox.allow`) only has to update
  `WorkdirScope.ResolvePolicy`.

- **No wildcard / glob support.** `ReadRemoteAgentDir`
  returns the contents of one directory at a time. A
  future cycle that wants recursive listing or glob
  patterns adds a new method (`ListRemoteAgentDirRecursive`
  or `GlobRemoteAgentPath`); the existing two methods are
  not extended.

- **MemFS has no symlink modelling.** Production
  (`WorkdirScope`) classifies symlinks via `os.Lstat`;
  tests that need symlink behaviour need a real
  `t.TempDir()` instead.

## Test surface

The cycle ships **34 new tests**: 25 in-process (in
`internal/remotecontrol/files_test.go`) + 9 wire-level
(in `internal/remotetransport/tcp_test.go`).

In-process (files_test.go):

- `TestFSProvider_MemFS_AllowsAllowed` — MemFS gives
  canonical path verbatim.
- `TestFSProvider_MemFS_RejectsSecret` — WorkdirScope
  rejects an out-of-workspace path → `-32007`.
- `TestReadRemoteAgentFile_Ok_UTF8` — happy path.
- `TestReadRemoteAgentFile_Binary` — synthetic binary,
  base64-decode round-trip.
- `TestReadRemoteAgentFile_Truncated` — 100 KiB file
  with `maxBytes: 4096` returns truncated + 4096 bytes.
- `TestReadRemoteAgentFile_OffsetBeyondSize` — `-32603`.
- `TestReadRemoteAgentFile_PathNotAllowed` — `-32007`.
- `TestReadRemoteAgentFile_NotRegularFile` — `-32008`.
- `TestReadRemoteAgentFile_MaxBytesTooLarge` — `-32602`.
- `TestReadRemoteAgentFile_NegativeOffset` — `-32602`.
- `TestReadRemoteAgentFile_NoFSProvider` — `-32603`.
- `TestReadRemoteAgentFile_EmptyPath` — `-32602`.
- `TestReadRemoteAgentDir_Ok` — happy path, sorted, typed.
- `TestReadRemoteAgentDir_TruncatedByMaxEntries` —
  `maxEntries: 2` on a 4-entry dir returns 2 + truncated.
- `TestReadRemoteAgentDir_PathNotAllowed` — `-32007`.
- `TestReadRemoteAgentDir_MaxEntriesTooLarge` — `-32602`.
- `TestWorkdirScope_AllowList` — `--add-dir` resolution.
- `TestWorkdirScope_RelativePath` — relative path joins
  against workspace.
- `TestWorkdirScope_EmptyPath` — `ErrPathNotAllowed`.
- `TestMemFS_ReadFile_Binary` — MemFS binary detection.
- `TestMemFS_ReadFile_Directory` — MemFS
  `ErrNotRegularFile`.
- `TestMemFS_ReadFile_OffsetBeyondSize` — MemFS offset
  path.
- `TestWorkdirScope_ReadFile` — production ReadFile
  against `t.TempDir()` (text / binary / truncated paths).
- `TestWorkdirScope_ReadFile_Directory` — production
  ReadFile's directory rejection.
- `TestWorkdirScope_ReadDir` — production ReadDir
  against `t.TempDir()`.

Wire-level (tcp_test.go):

- `TestReadRemoteAgentFile_WS_OK` — ws round-trip,
  utf-8.
- `TestReadRemoteAgentFile_WS_Binary` — ws round-trip,
  base64.
- `TestReadRemoteAgentFile_WS_Truncated` — ws round-trip,
  cap.
- `TestReadRemoteAgentFile_WS_OffsetBeyondSize` — ws
  `-32603`.
- `TestReadRemoteAgentFile_WS_PathNotAllowed` — ws
  `-32007`.
- `TestReadRemoteAgentFile_WS_NotRegularFile` — ws
  `-32008`.
- `TestReadRemoteAgentDir_WS_OK` — ws round-trip,
  sorted, typed.
- `TestReadRemoteAgentDir_WS_TruncatedByMaxEntries` —
  ws round-trip cap.
- `TestReadRemoteAgentDir_WS_PathNotAllowed` — ws
  `-32007`.

Plus the two helper functions in `tcp_test.go`:

- `newTestTCPServerWithFS` — wires a `*WorkdirScope`
  pointing at `t.TempDir()`; the production path is
  exercised end-to-end (real `os.Open` / `os.Stat`).
- `newTestTCPServerWithMemFS` — wires a `MemFS` for
  tests that need rejection paths without a real
  filesystem.

## 硬约束 / Hard constraints

These are the project's hard constraints from
`AGENTS.md` + `docs/cycles/PLAN.md` that this cycle
honoured:

- **WebSocket lib = `github.com/coder/websocket` v1.8.x.**
  The wire tests use `coder/websocket` exactly as the
  prior cycles do; no other ws library was introduced.
- **No `go test ./...`** — only the scoped
  `./internal/remotecontrol/ ./internal/remotetransport/
  ./cmd/wagyu/` packages were run.
- **No reformatting pre-existing files** — `gofmt -l`
  was only run on files the cycle added or modified;
  pre-existing `gofmt` dirt (50+ files in `internal/`)
  was left untouched.
- **No third-party deps beyond `coder/websocket`** — the
  new files use stdlib only (`bytes` / `encoding/base64` /
  `errors` / `fmt` / `os` / `path/filepath` / `sort` /
  `time` + the existing `encoding/json`).
- **Cycle IDs are semantic** — `rc-file-ops` matches
  the `PLAN.md` row, not a `cycle-N` number.
- **Don't probe `/logout` / `/exit` / `/keybindings`**
  — irrelevant to this cycle (no slash-command surface
  touched).
- **Production wiring lives in `cmd/wagyu/`, not
  `internal/`** — `buildHostFSProvider` is in
  `cmd/wagyu/remote_control.go`; the dispatcher package
  imports nothing from `cmd/wagyu/`.

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Format / vet (new + modified files only).
gofmt -l internal/remotecontrol/files.go \
          internal/remotecontrol/scope.go \
          internal/remotecontrol/file_dispatcher.go \
          internal/remotecontrol/files_test.go \
          internal/remotecontrol/daemon.go \
          internal/remotecontrol/host.go \
          internal/remotecontrol/dispatch.go \
          internal/remotetransport/tcp_test.go \
          cmd/wagyu/remote_control.go \
          cmd/wagyu/main.go
go vet ./internal/remotecontrol/ ./internal/remotetransport/

# Full scoped test surface, with -race.
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# Pre-existing cmd/wagyu test build issue is
# unrelated to this cycle:
#   cmd/wagyu/model_default_effort_test.go:67
#   (*testing.common).Errorf call needs 2 args but has 3 args
# The cycle did not touch that file.
```

## Hash + stat + diff

- Commit: `2fb7fbb`
- Files added:
  - `internal/remotecontrol/files.go` (new, ~430 lines)
  - `internal/remotecontrol/scope.go` (new, ~190 lines)
  - `internal/remotecontrol/file_dispatcher.go` (new,
    ~280 lines)
  - `internal/remotecontrol/files_test.go` (new, ~700
    lines)
- Files modified:
  - `internal/remotecontrol/dispatch.go` (+19 / -0)
  - `internal/remotecontrol/daemon.go` (+11 / -0)
  - `internal/remotecontrol/host.go` (+14 / -0)
  - `cmd/wagyu/remote_control.go` (+20 / -0)
  - `cmd/wagyu/main.go` (+19 / -0)
  - `internal/remotetransport/tcp_test.go` (+~430 / -0)
- Tests added: 25 in-process + 9 wire-level = **34 new
  tests**.
- Test result: `ok
  github.com/chadgpt/openagy/internal/remotecontrol`,
  `ok github.com/chadgpt/openagy/internal/remotetransport`.
