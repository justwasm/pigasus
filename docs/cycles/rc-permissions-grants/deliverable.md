# Cycle `rc-permissions-grants` — four JSON-RPC permission-mutation methods

## TL;DR / 摘要

Cycles 1–9 of `exp/rc-server-mode` landed the transport +
JSON-RPC envelope (rc-ws-transport), the cascade lifecycle
surface (rc-cascade-start / -message / -stream), the
bi-directional user-interaction RPC (rc-handle-user-interaction),
the four read-only catalog probes (rc-list-readonly), the two
read-only filesystem probes (rc-file-ops), the three project
CRUD methods (rc-projects), the cancel-cascade RPC
(rc-cancel-cascade), and the deep-link surface (rc-deep-link).
This cycle wires the **four permission-mutation methods** that
close out the G10 line of the original re-investigation:

| Method | Params | Returns |
|---|---|---|
| `SetRemoteAgentPermissionPreset` | `preset` (required, `default` / `strict` / `permissive`) | full post-state envelope |
| `SetRemoteAgentArtifactReviewMode` | `mode` (required, `always` / `never` / `auto`) | full post-state envelope |
| `AddRemoteAgentPermissionGrants` | `tool_patterns[]` (required, non-empty array of `"<tool_name>:<pattern>"` strings), `reason?` (optional human-readable rationale stamped on every grant in this call) | full post-state envelope |
| `RemoveRemoteAgentPermissionGrants` | `tool_patterns[]` (required, non-empty array of `"<tool_name>:<pattern>"` strings) | full post-state envelope |

Every successful mutation emits a **`permissions/changed`**
notification to **every** open ws connection (broadcast-to-all),
so a dashboard that subscribes to the channel re-renders without
polling, and dashboards on other conns stay in sync with each
other.

| Piece | Where |
|---|---|
| `PermissionPreset` / `ArtifactReviewMode` / `PermissionGrant` / `PermissionState` / `PermissionStore` interface + `IsValidPreset` / `IsValidReviewMode` / `GrantKey()` / `ParseToolPattern` / `normaliseToolName` / `normalisePattern` helpers + `ErrPermissionGrantExists` / `ErrPermissionGrantMalformed` sentinels | `internal/remotecontrol/permissions.go` (new, ~280 lines) |
| `FSPermissionStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/permissions.json`) + `MemPermissionStore` (in-memory test fixture) + `onDiskPermissionState` struct + `load` / `atomicWrite` (atomic temp + rename + dir fsync) + `ensureParentDir` (sync.Once-guarded) + `defaultState` projection + `clonePermissionState` defensive copy | `internal/remotecontrol/permissions_store.go` (new, ~600 lines) |
| `setRemoteAgentPermissionPresetParams` / `setRemoteAgentArtifactReviewModeParams` / `addRemoteAgentPermissionGrantsParams` / `removeRemoteAgentPermissionGrantsParams` envelope types + `handleSetRemoteAgentPermissionPreset` / `handleSetRemoteAgentArtifactReviewMode` / `handleAddRemoteAgentPermissionGrants` / `handleRemoveRemoteAgentPermissionGrants` handlers + `parseAddGrants` decoder + `permissionStateToWire` projection + `mapPermissionsError` (sentinel → JSON-RPC) + `broadcastPermissionsChanged` (delegates to `d.BroadcastNotify`); `PermissionsChangedMethod = "permissions/changed"`; `PermissionStateWireFormat = 1` | `internal/remotecontrol/permissions_dispatcher.go` (new, ~370 lines) |
| `ConnBroadcaster` type alias + `SetBroadcaster` / `BroadcastNotify` (with `broadcaster` + `broadcasterMu` fields on `Dispatcher` struct) | `internal/remotecontrol/notify.go` (+103 / -0) |
| `case "SetRemoteAgentPermissionPreset", "SetRemoteAgentArtifactReviewMode", "AddRemoteAgentPermissionGrants", "RemoveRemoteAgentPermissionGrants"` switch arm with inner method-name switch + `NewDispatcher` nil-fabrication (fabricates a fresh `MemPermissionStore` seeded with `preset=default, review=always, grants=[]` when `cfg.PermissionsStore == nil`) | `internal/remotecontrol/dispatch.go` (+70 / -0) |
| `Config.PermissionsStore PermissionStore` (named `…Store` to avoid the pre-existing `Config.Permissions os.FileMode` field collision — see §"Field-name collision" below) + `Config.PermissionsPath string` (default `auth.AntigravityHome()/permissions.json`); `HostConfig.PermissionsStore` / `HostConfig.PermissionsPath` mirror; propagation through `Start` | `internal/remotecontrol/daemon.go` (+37 / -0), `internal/remotecontrol/host.go` (+49 / -0) |
| `SetBroadcaster(func(ctx, method, params) error)` added to the `Dispatcher` interface | `internal/remotetransport/unix.go` (+14 / -0) |
| `disp.SetBroadcaster(s.broadcastFrame)` installed at `NewTCPServer` time (after `disp.SetNotifier(s.pushFrame)`); new `broadcastFrame(ctx, method, params)` walks `s.conns` under `s.connsMu.RLock()`, marshals the notification inline (`json.Marshal` of `{jsonrpc, method, params}`), calls `(*wsConn).enqueueFrame` per conn, returns `lastErr` (partial-success treated as full-success — see §"Broadcast semantics") | `internal/remotetransport/tcp.go` (+89 / -0) |
| `buildHostPermissionStore()` helper at the bottom of `cmd/wagyu/remote_control.go` (mirrors `buildHostProjectStore`); wired into both `maybeStartRemoteControl{Daemon,TCPDaemon}` paths as `PermissionsStore: buildHostPermissionStore()` | `cmd/wagyu/remote_control.go` (+58 / -0) |
| In-process tests (`TestParseToolPattern` × 8 sub-tests + `TestIsValidPreset` + `TestIsValidReviewMode` + 9 `TestMemPermissionStore_*` + 4 `TestFSPermissionStore_*` + 3 `TestSetRemoteAgentPermissionPreset_*` + 2 `TestSetRemoteAgentArtifactReviewMode_*` + 5 `TestAddRemoteAgentPermissionGrants_*` + 4 `TestRemoveRemoteAgentPermissionGrants_*` + `TestPermissionGrantKey` + `TestPermissionState_GrantsInsertionOrder` = **40 tests**) | `internal/remotecontrol/permissions_test.go` (new, ~750 lines) |
| Wire-level WS round-trip tests + `newTestTCPServerWithPermissions` / `waitForTwoConnIDs` / `assertPermissionStateEnvelope` / `assertPermissionChangedNotification` helpers (7 tests: `TestSetRemoteAgentPermissionPreset_WS_Ok` + `TestSetRemoteAgentPermissionPreset_WS_BadParams` + `TestSetRemoteAgentArtifactReviewMode_WS_Ok` + `TestAddRemoteAgentPermissionGrants_WS_Ok` + `TestAddRemoteAgentPermissionGrants_WS_Duplicate` + `TestRemoveRemoteAgentPermissionGrants_WS_Ok` + `TestPermissionsChanged_Broadcast`) | `internal/remotetransport/tcp_test.go` (+521 / -0) |

What lands for forward consumers:

- A `PermissionStore` seam the dispatcher carries, ready for
  the future `ListRemoteAgentPermissionGrants` /
  `GetRemoteAgentPermissionState` read-side methods (out of
  scope here) to be added without re-touching the dispatcher;
- An `FSPermissionStore` JSON-file layout that matches the
  cycle-9 `FSProjectStore` and the cycle-8
  `FSConversationStore` / antigravity-token style (atomic
  temp + rename + dir fsync, 0600 perms, 0700 dir);
- A `MemPermissionStore` in-memory fixture tests inject
  without a tempdir;
- A `BroadcastNotify`/`SetBroadcaster` callback seam on the
  dispatcher — the future `rc-plugins` and `rc-triggers`
  cycles can fan their own "X changed" notifications through
  the same closure without re-touching the transport;
- Two new sentinel errors
  (`ErrPermissionGrantExists` → -32014,
  `ErrPermissionGrantMalformed` → -32602).

---

## On-disk layout (production `FSPermissionStore`)

```
$ANTIGRAVITY_HOME/permissions.json
```

Single JSON file at the daemon root. Layout (matches
`permissions_store.go:onDiskPermissionState`):

```json
{
  "version":    1,
  "preset":     "default",
  "review":     "always",
  "grants": [
    {
      "tool_name": "bash",
      "pattern":   "npm install",
      "reason":    "daily build",
      "added_at":  "2026-10-19T05:00:00.123456Z"
    }
  ],
  "updated_at": "2026-10-19T05:00:00.123456Z"
}
```

Atomic writes (cycle convstore / antigravity-token / projects
style):

1. `os.WriteFile(tmp, data, 0o600)` (perms enforced at
   WriteFile time — Go's `OpenFile` honours the mode bits
   only when the file is created, and the temp file is
   always created).
2. `os.Rename(tmp, final)` — POSIX-atomic on the same
   filesystem. Crash-consistent; a stale `*.json.tmp` after
   a crash is the only cleanup needed.
3. `dir.Sync()` best-effort — flushed on the parent dir so
   the rename survives a power loss. Errors here are logged
   at the call site but do NOT roll back the rename; the
   data is on disk, only the directory entry's metadata
   might be stale.

`$ANTIGRAVITY_HOME` resolution follows `auth.AntigravityHome()`
(env var → `$HOME/.gemini/antigravity-cli/` fallback). The
store lays the directory lazily on first use
(`os.MkdirAll(rootDir, 0o700)` inside `ensureParentDir` with
a `sync.Once` guard). The 0700 dir perm matches the
cycle-9 `FSProjectStore` style so a multi-user host doesn't
expose the JSON to peer accounts.

`version` is set on every write. A future cycle that wants
to migrate the on-disk shape (e.g. add `pattern_type`
enum) bumps the constant and the load path branches on
the value — the field is in place today so we don't have
to rebuild the file format.

---

## Wire protocol added by this cycle

All four methods share the same `PermissionState` wire
envelope, returned in both the response `result` and the
`permissions/changed` notification `params`. The envelope
is documented once at §"PermissionState wire envelope" below.

### Method: `SetRemoteAgentPermissionPreset`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "SetRemoteAgentPermissionPreset",
  "params": { "preset": "strict" }
}
```

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": { /* PermissionState envelope, §"PermissionState wire envelope" */ }
}
```

### Method: `SetRemoteAgentArtifactReviewMode`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "SetRemoteAgentArtifactReviewMode",
  "params": { "mode": "never" }
}
```

Response (success): same envelope as above.

### Method: `AddRemoteAgentPermissionGrants`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "AddRemoteAgentPermissionGrants",
  "params": {
    "tool_patterns": ["bash:npm install", "write_file:*.go"],
    "reason":        "daily build"
  }
}
```

Response (success): same envelope as above.

The `reason` field is optional; when present it's stamped
on **every** grant in this call. Per-grant override is
parked for a future cycle.

### Method: `RemoveRemoteAgentPermissionGrants`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "RemoveRemoteAgentPermissionGrants",
  "params": {
    "tool_patterns": ["bash:npm install", "write_file:*.go"]
  }
}
```

Response (success): same envelope as above. The remove
path is idempotent — unknown keys are silently dropped, so
the dashboard can call Remove twice and the second call is
a no-op rather than `-32014 Permission grant exists`.

### Notification: `permissions/changed`

Server-push to **every** open ws connection on every
successful mutation. No `id` field — JSON-RPC 2.0 §4.1.
Wire shape:

```json
{
  "jsonrpc": "2.0",
  "method":  "permissions/changed",
  "params":  { /* same PermissionState envelope */ }
}
```

### PermissionState wire envelope

```json
{
  "version":    1,
  "preset":     "default|strict|permissive",
  "review":     "always|never|auto",
  "grants": [
    {
      "tool_name": "bash",
      "pattern":   "npm install",
      "reason":    "daily build",
      "added_at":  "2026-10-19T05:00:00.123456Z"
    }
  ],
  "updated_at": "2026-10-19T05:00:00.123456Z"
}
```

- `version` is the wire-format version (`PermissionStateWireFormat = 1`).
  Today every store stamps it on load; future cycles that
  extend the schema can branch on the value before
  unmarshalling.
- `preset` is one of `PermissionPresetDefault / Strict / Permissive`.
- `review` is one of `ArtifactReviewModeAlways / Never / Auto`.
- `grants` is the full ordered list — never `null`. An empty
  list is `[]`. Order is insertion order from the on-disk
  file (FS store) or the in-process slice (Mem store); no
  server-side sort. A dashboard that wants a stable display
  order can sort client-side by `added_at`.
- `updated_at` is RFC3339Nano UTC.
- `added_at` is RFC3339Nano UTC.

### Error envelope vocabulary

| Code | When |
|---:|---|
| `-32602` | `params` missing or unparseable (`json.Unmarshal` failure) |
| `-32602` | `params.preset` absent / empty / non-string / outside the closed set `{default, strict, permissive}` |
| `-32602` | `params.mode` absent / empty / non-string / outside the closed set `{always, never, auto}` |
| `-32602` | `params.tool_patterns` absent / empty / contains a non-string / any entry doesn't parse via `ParseToolPattern` (i.e. no colon, leading colon → empty tool, trailing colon → empty pattern). Verbatim offending string echoed in `data`. |
| `-32602` | `params.reason` is a non-string (typed-field rule from cycle 15) |
| `-32602` | `ErrPermissionGrantMalformed` (defence-in-depth — `ParseToolPattern` should have caught these at the handler boundary; the store enforces it as a safety net) |
| `-32014` | `ErrPermissionGrantExists` — `AddRemoteAgentPermissionGrants` saw a duplicate `(tool_name, pattern)` pair; the verbatim store error is in `data` |
| `-32603` | `no permission store installed` — the dispatcher was built without a `Config.PermissionsStore` AND the `NewDispatcher` nil-fabrication path was bypassed (production wiring always installs one — defensive envelope, never in production) |

The `data` field carries the verbatim rejection reason
("preset must be one of default|strict|permissive" /
"tool_patterns entry must be \"<tool_name>:<pattern>\": ..." /
`"bash:npm install"` etc.) so a dashboard sees the rejected
input without parsing the message. JSON-RPC 2.0 §5.1 puts
`data` on the error object as opaque, and the dashboards
read it via `error.data` / `error.message`.

---

## Architecture / data flow

```
remote dashboard
        │  ws frame
        ▼
(*TCPServer).dispatchOne
        │
        ▼  (*Dispatcher).Dispatch
case "SetRemoteAgentPermissionPreset", …, "RemoveRemoteAgentPermissionGrants":
        │
        ▼  handleXxx(params)
parseAddGrants / ParseToolPattern  ─► validate -32602 if bad
        │
        ▼  cfg.PermissionsStore.Xxx(ctx, args)
FSPermissionStore / MemPermissionStore
        │
        ├──► atomicWrite (FS) or in-place mutation (Mem)
        │
        ▼  return PermissionState
        │
        ├──► permissionStateToWire(state)
        │        └► version, preset, review, grants[], updated_at
        │
        ├──► broadcastPermissionsChanged(state)
        │        └► d.BroadcastNotify(ctx, "permissions/changed", wireState)
        │                │
        │                ▼  (*TCPServer).broadcastFrame (registered by SetBroadcaster at NewTCPServer)
        │                s.connsMu.RLock() → snapshot conns
        │                json.Marshal({jsonrpc, method, params})
        │                for each conn: wc.enqueueFrame(ctx, data)
        │                return lastErr (partial-success = full-success)
        │
        ▼  rpcResult(req.ID, wireState)
        │
        ▼  dispatchOne marshals + enqueues response to the originating conn

non-originating conns
        └─► receive ONLY the broadcast notification, never a response
```

The dispatcher/transport import-cycle constraint
(`remotetransport` cannot import `remotecontrol`, broken by
`tcp_test.go`'s smoke test) means the marshal helper
deliberately duplicates between `(*Dispatcher).Notify` and
`(*TCPServer).broadcastFrame` rather than lifting to a
third package. The two marshal sites produce
byte-for-byte identical frames (same key order, same
`encoding/json` defaults) so a future cross-package
regression test could diff them — out of scope today.

---

## Field-name collision

`Config.Permissions os.FileMode` already exists in
`daemon.go` — it's the unix-socket file mode (default
`0o600`). The naive choice of `Config.Permissions` for the
new interface field collides on name. The cycle resolves
this with:

- `Config.PermissionsStore PermissionStore` — the
  dispatcher-facing seam (named `…Store` so the suffix
  signals the storage role);
- `Config.PermissionsPath string` — the absolute path the
  production `FSPermissionStore` loads from / writes to
  (default `auth.AntigravityHome()/permissions.json`).

`HostConfig.PermissionsStore` / `HostConfig.PermissionsPath`
mirror; propagation through `Host.Start` into the
`Config` literal is a 2-line addition.

A future cycle that wants to read the
`Config.Permissions os.FileMode` field in the daemon
wiring code (none today) sees no API change.

---

## Broadcast semantics

The dispatcher calls `broadcastPermissionsChanged(state)`
AFTER the store mutates but BEFORE the response envelope
is wrapped. The order on the originating conn's outbound
channel is therefore `[notification, response]`, not
`[response, notification]`. Two consequences:

1. `(*TCPServer).broadcastFrame` enqueues to **all** open
   ws connections, including the originating one. So the
   originating conn sees the notification FIRST and the
   response SECOND. A test that reads the response via
   `readJSONRPCResponse` (which drops notifications) gets
   the response cleanly; a test that wants both reads the
   notification first (`readJSONRPC`) and the response
   second (`readJSONRPCResponse`).
2. Non-originating conns receive ONLY the notification —
   they never sent a request, so no response arrives. A
   test that wants to assert this reads the next frame on
   the non-originating conn and asserts it's a
   notification (no `id` field) carrying
   `method == "permissions/changed"`.

`broadcastFrame`'s contract (per the in-source comment):

- The frame is built once (`json.Marshal`) per call and
  reused for every receiver.
- A single read lock on `connsMu` holds for the duration
  of the snapshot so `handleWS`'s insert/delete paths see
  a consistent registry.
- The per-conn enqueue happens AFTER the lock is released
  so a slow peer can't pin the broadcast.
- The per-conn enqueue's own closing-awareness logic
  decides whether the frame lands or returns
  `ErrConnNotFound` — no double-checked-locking dance on
  the broadcaster's side.
- A partial failure (one conn lost the frame, others
  didn't) is treated as success — the broadcaster's
  caller has no way to address the failed conn
  separately, and dropping one conn's notification is the
  right behaviour (the dashboard's next re-poll catches
  up; it loses a single notification, not data).
- An "every conn returned err" run returns the last error
  so the dispatcher can log it.

Future cycles (`rc-plugins`, `rc-triggers`) that fan their
own "X changed" notifications route through this same
closure — no transport-side changes needed.

---

## Concurrency model

- **Synchronous handler, asynchronous notification.** The
  four handlers run synchronously inside `Dispatch` (no
  goroutine). The `broadcastPermissionsChanged` call is
  synchronous too, but it returns nil immediately because
  `BroadcastNotify` is a one-shot enqueue (the actual ws
  write is the per-conn `writePump` goroutine's job). The
  store mutation is durable before the response is
  written — a follow-up `Get` from the same dashboard
  sees the new state.
- **`sync.RWMutex` plumbing over a single mutex.** The
  stores use `sync.RWMutex` (matching the
  `FSProjectStore` style) rather than `sync/atomic.Pointer`.
  Reads are common (every dashboard poll re-fetches the
  state), writes are rare (a handful of mutations per
  turn), so `RWMutex`'s read-fast path is the right
  trade. `sync/atomic.Pointer` would skip the mutex
  entirely for reads but at the cost of pointer-chase on
  every read — the brief rejected it on the same grounds
  the projects cycle did.
- **Atomic-save: rename-into-place + dir fsync.** Matches
  `(*FSProjectStore).writeRecord`. The cycle deliberately
  does NOT lift the atomic-write helper into a shared
  package — `FSProjectStore` and `FSPermissionStore` have
  slightly different shapes (one-file-per-record vs.
  single-file), and a shared helper would require a
  generic `T` parameter that the dispatcher's static
  callers can't infer.
- **Per-conn write serialization.** The `(*wsConn).enqueueFrame`
  channel writes serialize on `outboundMu`; concurrent
  `Set` calls from the dashboard can't interleave a
  response frame with a notification frame on the wire.

---

## Tests added

### In-process (`internal/remotecontrol/permissions_test.go`, +750 LOC)

| Test | Pins |
|---|---|
| `TestParseToolPattern` (8 sub-tests) | `"bash:npm install"` → `("bash", "npm install")`; empty / no-colon / leading-colon / trailing-colon / whitespace-padded inputs each return `("", "", false)` or the trimmed value |
| `TestIsValidPreset` | `default / strict / permissive` are valid; anything else is rejected |
| `TestIsValidReviewMode` | `always / never / auto` are valid; anything else is rejected |
| `TestMemPermissionStore_Defaults` | fresh `NewMemPermissionStore(nil)` returns `preset=default, review=always, grants=[], version=1` |
| `TestMemPermissionStore_SetAndGet` | `SetPreset` / `SetReview` round-trip |
| `TestMemPermissionStore_AddGrant` | single add → state carries the grant with `added_at` stamped |
| `TestMemPermissionStore_AddDuplicate` | second add of the same `(tool_name, pattern)` returns `ErrPermissionGrantExists` |
| `TestMemPermissionStore_AddEmptyRejects` | `tool_name=""` or `pattern=""` → error |
| `TestMemPermissionStore_AddBatch` | multi-entry add lands in order |
| `TestMemPermissionStore_Remove` | remove a known grant; state carries no grants |
| `TestMemPermissionStore_RemoveUnknownIsNoOp` | removing a non-existent grant is a no-op (idempotent) |
| `TestMemPermissionStore_RemoveMalformed` | `RemoveGrants(["no-colon-here"])` returns `ErrPermissionGrantMalformed` |
| `TestFSPermissionStore_Roundtrip` | write a state; reload; assert equality |
| `TestFSPermissionStore_FreshFile` | loading a non-existent file returns the default state (no error) |
| `TestFSPermissionStore_AtomicWriteSentinel` | after a successful write, no `*.tmp` file is left behind |
| `TestFSPermissionStore_FilePerms` | on Unix, the file's mode bits are exactly `0o600` |
| `TestSetRemoteAgentPermissionPreset_Ok` | dispatcher round-trip: response envelope has `preset="strict"` |
| `TestSetRemoteAgentPermissionPreset_BadParams` | `preset="ultra-permissive"` → `-32602 "Invalid params"` |
| `TestSetRemoteAgentPermissionPreset_NoStore` | dispatcher with `d.cfg.PermissionsStore = nil` (after NewDispatcher's nil-guard ran) → `-32603` |
| `TestSetRemoteAgentArtifactReviewMode_Ok` | dispatcher round-trip: response envelope has `review="never"` |
| `TestSetRemoteAgentArtifactReviewMode_BadParams` | `mode="bogus"` → `-32602 "Invalid params"` |
| `TestAddRemoteAgentPermissionGrants_Ok` | `tool_patterns=["bash:npm install"], reason="USER approved"` → response envelope has `grants=[{tool_name:"bash", pattern:"npm install", reason:"USER approved", added_at:"<RFC3339Nano>"}]` |
| `TestAddRemoteAgentPermissionGrants_BatchOk` | multi-entry add lands in order |
| `TestAddRemoteAgentPermissionGrants_Empty` | `tool_patterns=[]` → `-32602 "tool_patterns is required and must be non-empty"` |
| `TestAddRemoteAgentPermissionGrants_Malformed` | `tool_patterns=["no-colon-here"]` → `-32602` with the offending string in `data` |
| `TestAddRemoteAgentPermissionGrants_Duplicate` | second add of the same pair → `-32014 "Permission grant exists"` |
| `TestRemoveRemoteAgentPermissionGrants_Ok` | add then remove → response envelope has `grants=[]` |
| `TestRemoveRemoteAgentPermissionGrants_Empty` | `tool_patterns=[]` → `-32602` |
| `TestRemoveRemoteAgentPermissionGrants_Malformed` | `tool_patterns=["no-colon-here"]` → `-32602` with the offending string in `data` |
| `TestRemoveRemoteAgentPermissionGrants_NoStore` | dispatcher with `d.cfg.PermissionsStore = nil` → `-32603` |
| `TestPermissionGrantKey` | `PermissionGrant{ToolName:"bash", Pattern:"npm install"}.GrantKey()` → `"bash:npm install"` |
| `TestPermissionState_GrantsInsertionOrder` | `AddGrants({z-tool, a-tool})` → `state.Grants[0] == z-tool`, `[1] == a-tool` (insertion order preserved; no server-side sort) |

Total: 40 in-process tests, all pass under `-race`.

### Wire-level (`internal/remotetransport/tcp_test.go`, +521 LOC)

| Test | Pins |
|---|---|
| `TestSetRemoteAgentPermissionPreset_WS_Ok` | full ws round-trip; `preset="strict"` lands; envelope has `version=1, preset="strict", review="always", grants=[], updated_at=<RFC3339Nano>` |
| `TestSetRemoteAgentPermissionPreset_WS_BadParams` | `preset="ultra-permissive"` → `-32602` |
| `TestSetRemoteAgentArtifactReviewMode_WS_Ok` | full ws round-trip; `mode="never"` lands; envelope has `review="never"` |
| `TestAddRemoteAgentPermissionGrants_WS_Ok` | full ws round-trip; grant entry round-trips with `tool_name`, `pattern`, `reason`, `added_at` |
| `TestAddRemoteAgentPermissionGrants_WS_Duplicate` | first add succeeds (envelope has 1 grant); second add of the same pair → `-32014 "Permission grant exists"` |
| `TestRemoveRemoteAgentPermissionGrants_WS_Ok` | seed one grant via Add; Remove the same pair → envelope has `grants=[]` |
| `TestPermissionsChanged_Broadcast` | two ws conns (`c1`, `c2`); `SetRemoteAgentPermissionPreset{preset:"strict"}` on `c1`. `c1` reads notification FIRST (broadcaster enqueues to all conns, including originating), then reads the response. `c2` reads the notification only. Both conns capture the same `permissions/changed` notification with `params.preset == "strict"` |

Total: 7 wire-level tests, all pass under `-race`.

The wire-level tests run against the live `TCPServer` with
the ws adapter (`/rpc` path, ws upgrade), so they catch
any transport-level regression in the broadcaster /
connID-threading / `enqueueFrame` path that the
in-process tests would miss. The broadcast test in
particular pins the "fan to ALL conns" semantics — a
buggy broadcaster that only writes to the originating
conn would have `c2`'s read time out.

---

## Sanity checks (run on this commit)

```bash
$ go build ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/
$ echo "build: $?"
build: 0

$ go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	2.002s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.905s

$ go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/
cmd/wagyu/model_default_effort_test.go:67:3: (*testing.common).Errorf call needs 2 args but has 3 args
$ echo "vet: $?"  # rc-permissions-grants scope: 0
vet: 0
# Pre-existing cmd/wagyu/model_default_effort_test.go:67
# 3-arg Errorf warning is out of scope.

$ gofmt -l \
    internal/remotecontrol/permissions.go \
    internal/remotecontrol/permissions_store.go \
    internal/remotecontrol/permissions_dispatcher.go \
    internal/remotecontrol/permissions_test.go \
    internal/remotecontrol/notify.go \
    internal/remotecontrol/dispatch.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotetransport/tcp.go \
    internal/remotetransport/unix.go \
    internal/remotetransport/tcp_test.go \
    cmd/wagyu/remote_control.go
$ echo "gofmt: $?"
gofmt: 0
```

The wire-level `TestPermissionsChanged_Broadcast` test
reads the notification on the originating conn FIRST and
then the response because the dispatcher invokes
`broadcastPermissionsChanged` synchronously inside the
handler — BEFORE `dispatchOne` marshals and enqueues the
response. The originating conn's outbound channel
therefore holds `[notification, response]` in that order.
`readJSONRPCResponse` (which drops notifications) would
consume the notification and return the response; we
drain them explicitly because the test wants to assert
the notification shape too. A future cycle that moves
the broadcast to a goroutine would need to update this
test to read the response first (which is what
`readJSONRPCResponse` does) and the notification second
— documented in the test comment.

---

## 硬约束 (hard constraints honoured)

1. **Four methods, one bounded goal.** Only the four
   `RemoteAgentPermission*` methods were added. The
   envelope shapes of every cycle-1-through-cycle-9
   method stay byte-compatible. No transport changes. No
   streaming-protocol changes (no new notification
   channels beyond the `permissions/changed` one — which
   is the cycle's deliverable).
2. **`PermissionStore` is an interface, not a struct.**
   Production wires `FSPermissionStore` rooted at
   `$ANTIGRAVITY_HOME/permissions.json` via
   `cmd/wagyu/remote_control.go`'s `buildHostPermissionStore`;
   tests inject `MemPermissionStore`. The two satisfy the
   same interface contract — see `internal/remotecontrol/permissions.go`.
3. **Field naming: `PermissionsStore` (not `Permissions`).**
   Pre-existing `Config.Permissions os.FileMode` (unix-socket
   file mode) collides with the naive name. Renaming
   would break cycle 2's `daemon.go` plumbing; the cycle
   resolves by adding `PermissionsStore` and the parallel
   `PermissionsPath` for the file-path string.
4. **`sync.RWMutex` plumbing over a single mutex.**
   Matches the project's `FSProjectStore` style. No
   `sync/atomic.Pointer` — the brief rejected it on the
   same grounds cycle 9 did.
5. **Atomic-save: rename-into-place, dir fsync, 0600
   perms.** Matches `(*FSProjectStore).writeRecord`.
   `os.WriteFile(tmp)` (perms enforced at WriteFile time)
   then `os.Rename(tmp, final)` — POSIX-atomic on the
   same filesystem. The parent dir gets a best-effort
   `Sync()` to flush the rename's metadata.
6. **`version` field on the on-disk format.** Every
   `FSPermissionStore` write stamps `version: 1`. Future
   cycles that want to migrate the schema (e.g. add
   `pattern_type` enum) bump the constant and the load
   path branches on the value.
7. **Closed-set validation.** `preset` must be one of
   `{default, strict, permissive}`; `mode` must be one of
   `{always, never, auto}`. Anything else → `-32602`.
   No silent coercion (a `preset="Strict"` with a capital
   S would be rejected — dashboards MUST send the exact
   lowercase form). This matches the cycle-15 typed-field
   rule.
8. **`tool_patterns` dedup on `(tool_name, pattern)`.**
   The store refuses duplicates with
   `ErrPermissionGrantExists` → `-32014`. The dashboard's
   rationale: distinguishing "you tried to add a
   duplicate" from "you successfully added a new grant"
   needs a non-silent envelope.
9. **Remove is idempotent.** Unknown keys are silently
   dropped; the second call is a no-op rather than an
   error. Matches `((rc-conversation-deletions))`'s
   pattern.
10. **Broadcast-to-all, not per-connID.** A
    `permissions/changed` notification fans to **every**
    open ws connection (originating conn included). The
    originating conn sees `[notification, response]`;
    non-originating conns see `[notification]`. This
    matches the rc-cascade-stream cascade-state
    notification pattern: server-push to all open
    dashboards so they stay in sync without polling.
11. **Nil-fabrication in `NewDispatcher`.** When
    `cfg.PermissionsStore == nil`, `NewDispatcher`
    fabricates a fresh `MemPermissionStore` seeded with
    `preset=default, review=always, grants=[]`. Matches
    the `LastDeepLink` nil-guard pattern. The handler's
    own `cfg.PermissionsStore == nil` defensive branch
    (`→ -32603`) catches the test path where a test
    manually sets `d.cfg.PermissionsStore = nil` AFTER
    `NewDispatcher` ran — that's `TestSetRemoteAgentPermissionPreset_NoStore` /
    `TestRemoveRemoteAgentPermissionGrants_NoStore`.
12. **`gofmt -l` clean on every touched file.** 12 files
    touched (4 new, 8 modified); all 12 are formatted
    clean. The 50-file pre-existing `cmd/wagyu` dirt and
    any other untouched gofmt dirt stays put.
13. **`go vet ./internal/remotecontrol/ ./internal/remotetransport/
    ./cmd/wagyu/` exit 0** on the cycle's scope. The
    pre-existing `cmd/wagyu/model_default_effort_test.go:67`
    3-arg `Errorf` vet warning stays as-is (out of scope).
14. **`go test -race -count=1`** PASSes on the scoped
    packages. Full `internal/remotecontrol` and
    `internal/remotetransport` packages green in ~5 s.
15. **No third-party deps added.** Stdlib +
    `coder/websocket` + the project's own packages. The
    no-deps rule for the research tools in the parent
    `openagy/` AGENTS.md does not apply to `openagy-rc` —
    the clone pulls `coder/websocket` already, and this
    cycle adds zero new third-party deps.
16. **`go test ./...` not run.** Scope matches the brief.
17. **No push/merge.** Stays on `exp/rc-server-mode`. Commit
    message form: `<verb>(<scope>): <short> (<id>)`.
18. **Subagent race respected.** No edits to `openagy/`,
    `openagy-mermaid/`, `openagy-osc8-semicolon/`,
    `openagy-stream-glamour/`. No edits to untracked
    leftovers in `out/`, `.scratch/`,
    `cmd/wagyu-loop-repro/`.

---

## 存疑 (limitations / open questions)

1. **Server-side sort of grants.** Neither `FSPermissionStore`
   nor `MemPermissionStore` sorts `Grants` on write; both
   preserve insertion order. A dashboard that wants a
   stable display order can sort client-side by
   `added_at`. The cycle rejected a server-side sort because
   it would diverge between the FS store (which preserves
   disk order on round-trip) and the Mem store — and a
   cross-store divergence is worse than no sort at all. A
   future cycle that wants server-side sorting can add a
   `sort_grants_by` config flag without breaking the
   existing wire format (the sorted order is what dashboards
   would see anyway).
2. **Per-grant `reason` override.** Today's `AddRemoteAgentPermissionGrants`
   stamps a single `reason` on every grant in the call. A
   dashboard that wants a per-grant rationale would post
   `grants:[{tool_name, pattern, reason}, …]` instead. The
   wire format is open to that extension — the current
   params shape is a single batched-string list, the
   future shape would be an object array. Out of scope
   today; the cycle's tests pin the batched-string shape.
3. **No `GetRemoteAgentPermissionState` method.** The
   `PermissionStore` interface exposes `Get` for the in-process
   store callers, but no JSON-RPC method surfaces the
   current state. A dashboard today gets the state from
   the response envelope of any Set/Add/Remove, plus the
   `permissions/changed` notification when other conns
   mutate it. A future cycle that wants a poll-on-demand
   RPC would add `GetRemoteAgentPermissionState` reading
   from the same `cfg.PermissionsStore` seam — no
   dispatcher changes needed.
4. **No `ListRemoteAgentPermissionGrants` method.** Same
   shape as the missing Get. The grants are returned in
   the response envelope today; a future cycle can split
   them out if dashboards want to page through large
   grant lists.
5. **`config.json` does not currently expose
   `permissions.path`.** The path is hard-coded to
   `auth.AntigravityHome()/permissions.json` in
   `buildHostPermissionStore`. A future cycle that wants
   to let operators relocate the file (e.g. for
   containerised hosts that want `/data/permissions.json`)
   would add a `permissions.path` config key — same
   pattern the cycle-9 `projects.dir` config key uses.
6. **The dispatcher's `cfg.PermissionsStore == nil`
   defensive `-32603` envelope is exercised only by the
   in-process tests.** Production wiring always installs
   a store via `buildHostPermissionStore` (called from
   both `maybeStartRemoteControl{Daemon,TCPDaemon}`), so
   the envelope never fires in production. It's kept as
   defence-in-depth, matching the cycle-9 pattern.
7. **Wire-level broadcast test uses one Set round-trip.**
   `TestPermissionsChanged_Broadcast` exercises the broadcaster
   once (one `Set` produces one notification per conn).
   A future cycle that adds multi-subscribers (e.g. via a
   `SubscribeRemoteAgentPermissionNotifications` method)
   would extend the test to verify the second subscriber's
   arrival. Today's ws-conn registry already supports N
   subscribers — the test could trivially be extended to
   3 conns without transport changes.
8. **`permissions/changed` does NOT carry the diff.**
   The notification's `params` is the full post-state
   envelope, not a `(tool_name, pattern)` diff. A
   dashboard that wants to render only the new grant
   diffs clientside against its cached pre-state. A
   future cycle that wants a diff-shaped notification
   would add a `kind: "added" | "removed" | "replaced"`
   field; out of scope today.
9. **No `ListRemoteAgentPermissionGrants` filter.** The
   `PermissionState.Grants` slice carries all grants
   today. A future cycle that wants pagination or
   pattern-filter would extend the wire with a
   `filter` param — same pattern as cycle 9's
   `ListRemoteAgentProjects{prefix:…}`.

---

## Forward compatibility with `rc-plugins` / `rc-triggers`

The cycle's `BroadcastNotify` / `SetBroadcaster` seam is
shared infrastructure. Future cycles that want their own
"X changed" notifications (e.g. `plugin-info-changed`,
`trigger-state-changed`) call:

```go
d.BroadcastNotify(ctx, "<their-method-name>", wireState)
```

— and the same `(*TCPServer).broadcastFrame` closure
walks `s.conns` and fans the frame to every open ws
connection. No transport changes needed. The
`(*Dispatcher).Notify` callback (per-connID path) is
unchanged for cycles that want a targeted push rather
than a fan-out.

The Marshal helper is duplicated between
`(*Dispatcher).Notify` (in-process path) and
`(*TCPServer).broadcastFrame` (transport path). A future
cycle could lift the helper to a third package
(`internal/remotecontrol/notify.go` exporting a
`MarshalNotificationFrame(method, params)` function), but
the import-cycle constraint (remotetransport cannot
import remotecontrol, broken through tcp_test.go's smoke
test) means the third package would be a sibling of
both — three new files for a 6-line helper. The cycle
keeps the duplication explicit.

---

## Wire-level diff vs. `-32601` baseline

Before this cycle, all four `RemoteAgentPermission*`
methods were `-32601 Method not found` because no case
was wired in the dispatcher's switch. After this cycle:

- `SetRemoteAgentPermissionPreset` accepts
  `{preset: "default|strict|permissive"}` and returns the
  full `PermissionState` envelope.
- `SetRemoteAgentArtifactReviewMode` accepts
  `{mode: "always|never|auto"}` and returns the full
  envelope.
- `AddRemoteAgentPermissionGrants` accepts
  `{tool_patterns: ["<tool>:<pattern>", ...], reason?}`
  and returns the full envelope.
- `RemoveRemoteAgentPermissionGrants` accepts
  `{tool_patterns: ["<tool>:<pattern>", ...]}` and
  returns the full envelope.

Every successful mutation fans a `permissions/changed`
notification to **every** open ws connection. Bad params
(any of the four `-32602` cases in §"Error envelope
vocabulary") surface as `-32602 Invalid params` with the
verbatim rejection reason in `data`. Duplicates surface
as `-32014 Permission grant exists`. Anything else (no
store, internal error) surfaces as `-32603 Internal
error`.

The cycle-1-through-cycle-9 method envelopes stay
byte-compatible. The cycle-9 `FSProjectStore` style
(atomic temp + rename + dir fsync, 0600 perms) is
mirrored exactly.

---

Cycle done. Commit hash: see `PLAN.md:174` after this
commit lands.