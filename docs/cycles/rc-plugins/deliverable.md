# Cycle `rc-plugins` — two JSON-RPC plugin methods

## TL;DR / 摘要

Cycles 1–19 of `exp/rc-server-mode` landed the transport + JSON-RPC envelope
(rc-ws-transport), the cascade lifecycle surface (rc-cascade-start / -message /
-stream), the bi-directional user-interaction RPC (rc-handle-user-interaction),
the four read-only catalog probes (rc-list-readonly), the two read-only
filesystem probes (rc-file-ops), the three project CRUD methods (rc-projects),
the cancel-cascade RPC (rc-cancel-cascade), the deep-link surface
(rc-deep-link), and the four permission-mutation methods (rc-permissions-grants).
This cycle wires the **two plugin methods** that close the "plugins cluster"
line of the original re-investigation:

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `ListRemoteAgentPlugins` | read | `{}` (params ignored) | `{ok:true, plugins:[{plugin_id, name, version, connected_at, capabilities:[…], origin?: "…"}, …]}` | `-32603` (no store) |
| `DisconnectRemoteAgentPlugin` | write | `{plugin_id: "<id>"}` | `{ok:true, plugin_id:"…", disconnected_at:"<RFC3339Nano>"}` | `-32602` (empty / non-string / whitespace id), `-32015 Plugin not found`, `-32603` (no store) |

Every successful `DisconnectRemoteAgentPlugin` emits a **`plugins/changed`**
notification (the same broadcast-to-all shape `rc-permissions-grants` uses for
`permissions/changed`) to **every** open ws connection, so a dashboard that
subscribes to the channel re-renders without polling and dashboards on other
conns stay in sync with each other.

The cycle ships a **`StubPluginRegistry`** that:

- Reads entries from `$ANTIGRAVITY_HOME/plugins.json` if it exists (and is
  well-formed).
- Else seeds three fixture entries: `claude-code-bridge@1.0.0`,
  `mcp-bridge@1.0.0`, `github-copilot-bridge@1.0.0` with `connected_at = today`
  and a small capabilities list per bridge.
- `Disconnect` removes the entry from the JSON file (atomic temp + rename +
  0o600 perms) and broadcasts `plugins/changed`.

This is a **"fixture that reads from disk" pattern** — production deployments
will plug in real plugin lifecycle later (the gap closes in cycle
`rc-plugins-real-bridge`, not yet scheduled). Documented in the
硬约束 section.

| Piece | Where |
|---|---|
| `Plugin` / `PluginState` / `PluginStore` interface + `ErrPluginNotFound` sentinel + `PluginStateVersion = 1` + `DefaultPluginFixtures` (claude-code-bridge / mcp-bridge / github-copilot-bridge) + `normalisePluginID` helper | `internal/remotecontrol/plugins.go` (new, ~210 lines) |
| `FSPluginStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/plugins.json`) + `MemPluginStore` (in-memory test fixture) + `NewMemPluginStoreWith` (custom-snapshot constructor) + `onDiskPluginState` struct + `load` / `atomicWrite` (atomic temp + rename + dir fsync) + `ensureParentDir` (sync.Once-guarded) + `fixtureSeedState` projection + `clonePluginState` defensive copy + `sortPluginsByID` (sort + secondary ConnectedAt tiebreak) | `internal/remotecontrol/plugins_store.go` (new, ~500 lines) |
| `disconnectRemoteAgentPluginParams` envelope type + `handleListRemoteAgentPlugins` / `handleDisconnectRemoteAgentPlugin` handlers + `pluginStateToWire` projection (brief's `{ok:true, plugins:[…]}` shape) + `mapPluginsError` (sentinel → JSON-RPC) + `broadcastPluginsChanged` (delegates to `d.BroadcastNotify`); `PluginsChangedMethod = "plugins/changed"`; `PluginStateWireFormat = 1` | `internal/remotecontrol/plugins_dispatcher.go` (new, ~290 lines) |
| `case "ListRemoteAgentPlugins", "DisconnectRemoteAgentPlugin"` switch arm with inner method-name switch + `NewDispatcher` nil-fabrication (fabricates a fresh `MemPluginStore` seeded with `DefaultPluginFixtures` when `cfg.PluginsStore == nil`) | `internal/remotecontrol/dispatch.go` (+27 / -0) |
| `Config.PluginsStore PluginStore` + `Config.PluginsPath string` (default `auth.AntigravityHome()/plugins.json`); `HostConfig.PluginsStore` / `HostConfig.PluginsPath` mirror; propagation through `Start` | `internal/remotecontrol/daemon.go` (+27 / -0), `internal/remotecontrol/host.go` (+17 / -0) |
| `buildHostPluginStore()` helper at the bottom of `cmd/wagyu/remote_control.go` (mirrors `buildHostPermissionStore`); wired into both `maybeStartRemoteControl{Daemon,TCPDaemon}` paths as `PluginsStore: buildHostPluginStore()` | `cmd/wagyu/remote_control.go` (+50 / -0) |
| In-process tests (`TestNormalisePluginID` × 6 sub-tests + `TestDefaultPluginFixtures` + `TestPluginStore_DefaultState_WithFixtures` + `TestMemPluginStore_DefaultsFixturesOrigins` + `TestPluginStore_Disconnect` + `TestPluginStore_Disconnect_Missing` + `TestPluginStore_Disconnect_Empty` + `TestNewMemPluginStoreWith` + `TestMemPluginStore_SortStableOnDuplicateIDs` + `TestMemPluginStore_ClonePluginState` + 9 `TestFSPluginStore_*` + `TestListRemoteAgentPlugins_Ok` + `TestListRemoteAgentPlugins_OkWithParams` + `TestDisconnectRemoteAgentPlugin_Ok` + `TestDisconnectRemoteAgentPlugin_NotFound` + `TestDisconnectRemoteAgentPlugin_BadParams` × 5 sub-tests + `TestDisconnectRemoteAgentPlugin_NoStore` + `TestListRemoteAgentPlugins_NoStore` + `TestPluginState_SortPinning` + `TestPluginState_DefaultRoundTrip` = **29 tests**) | `internal/remotecontrol/plugins_test.go` (new, ~930 lines) |
| Wire-level WS round-trip tests + `newTestTCPServerWithPlugins` / `assertPluginListEnvelope` / `assertPluginsChangedNotification` helpers (5 tests: `TestListRemoteAgentPlugins_WS_Ok` + `TestListRemoteAgentPlugins_WS_OkNoParams` + `TestDisconnectRemoteAgentPlugin_WS_Ok` + `TestDisconnectRemoteAgentPlugin_WS_NotFound` + `TestDisconnectRemoteAgentPlugin_WS_BadParams` × 5 sub-tests + `TestPluginsChanged_Broadcast`) | `internal/remotetransport/tcp_test.go` (+482 / -0) |

What lands for forward consumers:

- A `PluginStore` seam the dispatcher carries, ready for the future
  `ConnectRemoteAgentPlugin` / `UpdateRemoteAgentPlugin` /
  `ListRemoteAgentPluginCapabilities` (out of scope here) to be added without
  re-touching the dispatcher;
- An `FSPluginStore` JSON-file layout that matches the cycle-9 `FSProjectStore`
  and the cycle-19 `FSPermissionStore` style (atomic temp + rename + dir fsync,
  0600 perms, 0700 dir);
- A `MemPluginStore` in-memory fixture tests inject without a tempdir;
- A `MemPluginStore.With(custom snapshot)` constructor that sorts the input
  by `plugin_id` ascending and deep-copies each plugin's `Capabilities` slice
  so a test that mutates the returned state's Plugins doesn't race the
  store's internal state;
- A new sentinel error `ErrPluginNotFound` → `-32015 "Plugin not found"`;
- Reuse of the `BroadcastNotify` / `SetBroadcaster` callback seam from
  `rc-permissions-grants` — the future `rc-triggers`,
  `rc-conversation-config`, and any other cycle that emits "X changed"
  notifications will share it.

---

## JSON-RPC surface (precise)

### `ListRemoteAgentPlugins`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "ListRemoteAgentPlugins", "params": {} }
```

(`params` is optional; the dispatcher ignores its contents.)

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "plugins": [
      {
        "plugin_id":    "claude-code-bridge",
        "name":         "Claude Code Bridge",
        "version":      "1.0.0",
        "connected_at": "2026-10-19T00:00:00.000000000Z",
        "capabilities": ["conversation.send", "conversation.read", "tool.execute"],
        "origin":       "config"
      },
      ...
    ]
  }
}
```

- `plugins` is sorted by `plugin_id` ascending so dashboards see a stable
  sequence (`claude-code-bridge < github-copilot-bridge < mcp-bridge` —
  ASCII on `c < g < m`).
- `plugins: []` (never `null`) on the wire so dashboards never have to
  nil-check the array.
- `origin` is omitted (`omitempty`) when the plugin has no provenance note;
  the three `DefaultPluginFixtures` carry `origin: "config"` so a dashboard
  can distinguish fixture-seeded plugins from operator-supplied ones.
- `capabilities`: non-empty array of strings; never `null`.
- Errors: `-32603 "Internal error"` if no `PluginsStore` is installed
  (the dispatcher's nil-fabrication installs a fresh `MemPluginStore`,
  so this case only trips a dispatcher that explicitly cleared the field
  post-construction — the negative test).

### `DisconnectRemoteAgentPlugin`

Request:

```json
{ "jsonrpc": "2.0", "id": 2, "method": "DisconnectRemoteAgentPlugin", "params": { "plugin_id": "mcp-bridge" } }
```

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "ok":              true,
    "plugin_id":       "mcp-bridge",
    "disconnected_at": "2026-10-19T01:23:45.678901234Z"
  }
}
```

- Response envelope is the brief's lean `{ok, plugin_id, disconnected_at}`
  shape. The full post-state (with the disconnected plugin removed) is
  available via the `plugins/changed` broadcast that fires on every open
  ws connection immediately before this response is enqueued.
- Validation:
  - Empty / whitespace / non-string `plugin_id` → `-32602 "Invalid params"`
    (the "dashboard bug" envelope).
  - Unknown `plugin_id` → `-32015 "Plugin not found"`.
- Side effects:
  - Atomic disk write (temp + rename + dir fsync + 0o600 perms) on the
    production store.
  - Broadcasts `plugins/changed` on every open ws connection.

---

## `plugins.json` schema (`version: 1`)

Single JSON file at `$ANTIGRAVITY_HOME/plugins.json`:

```json
{
  "version": 1,
  "plugins": [
    {
      "plugin_id":    "claude-code-bridge",
      "name":         "Claude Code Bridge",
      "version":      "1.0.0",
      "connected_at": "2026-10-19T00:00:00.000000000Z",
      "capabilities": ["conversation.send", "conversation.read", "tool.execute"],
      "origin":       "config"
    },
    {
      "plugin_id":    "mcp-bridge",
      "name":         "MCP Bridge",
      "version":      "1.0.0",
      "connected_at": "2026-10-19T00:00:00.000000000Z",
      "capabilities": ["mcp.connect", "mcp.list_tools", "mcp.invoke_tool"],
      "origin":       "config"
    },
    {
      "plugin_id":    "github-copilot-bridge",
      "name":         "GitHub Copilot Bridge",
      "version":      "1.0.0",
      "connected_at": "2026-10-19T00:00:00.000000000Z",
      "capabilities": ["conversation.send", "completion.generate", "pr.review"],
      "origin":       "config"
    }
  ],
  "updated_at": "2026-10-19T00:00:00.000000000Z"
}
```

Field semantics:

- `version` — on-disk format version. Bumped on breaking changes (renamed
  keys, removed fields); non-breaking additions (new optional fields with
  `omitempty`) leave the version alone. `v1` is the cycle-rc-plugins
  initial layout. A reader that finds `version > 1` refuses to load the
  file with a "newer binary required" error (pinned by
  `TestFSPluginStore_VersionCheck`).
- `plugins` — the live plugin slice. Sorted by `plugin_id` ascending on
  read (the store applies the sort before handing back to the dispatcher
  so dashboards see a stable sequence).
- `updated_at` — RFC3339Nano UTC stamp the store bumps at the most-recent
  mutation. The store's `Snapshot` does NOT bump `updated_at` (read-only
  path); only `Disconnect` does.

Atomic writes (cycle convstore / antigravity-token / permissions style):

1. `os.WriteFile(tmp, data, 0o600)` (perms enforced at `WriteFile` time —
   Go's `OpenFile` honours the mode bits only when the file is created,
   and the temp file is always created).
2. `os.Rename(tmp, final)` — POSIX-atomic on the same filesystem.
   Crash-consistent; a stale `*.json.tmp` after a crash is the only
   cleanup needed.
3. Best-effort dir fsync (matches antigravity-token's style; tolerates
   the failure silently when the platform doesn't expose a directory
   fsync primitive).

---

## `plugins/changed` notification broadcast semantics

The notification fires on **every** successful `DisconnectRemoteAgentPlugin`
invocation (no broadcast on `ListRemoteAgentPlugins` — it's a pure read).
The frame shape is byte-for-byte identical to the `ListRemoteAgentPlugins`
response envelope so a dashboard listening on the channel sees the same
shape it would have polled for.

Frame shape:

```json
{
  "jsonrpc": "2.0",
  "method":  "plugins/changed",
  "params":  {
    "ok":      true,
    "plugins": [ { "plugin_id": "...", "name": "...", "version": "...",
                   "connected_at": "...", "capabilities": [...], "origin": "..." },
                 ... ]
  }
}
```

(no `id` field — JSON-RPC 2.0 §4.1 notification semantics.)

Three guarantees:

1. **Broadcast-to-all**: the `plugins/changed` notification fans out to
   **every** open ws connection, including the originating one. The
   dispatcher invokes `BroadcastNotify` (the seam `rc-permissions-grants`
   added) before marshalling the response, so the originating conn's
   outbound channel holds `[notification, response]`. Pinned by
   `TestPluginsChanged_Broadcast`: two conns (`c1` originating the call,
   `c2` passive observer), both observe the same notification frame
   carrying the post-state with two entries (and the disconnected
   `github-copilot-bridge` is absent).

2. **Sort-pinned**: the `plugins` slice inside the frame is sorted by
   `plugin_id` ascending so dashboard subscribers see a stable sequence
   (same ordering rule as `ListRemoteAgentPlugins`).

3. **No broadcast on read**: `ListRemoteAgentPlugins` is a pure read —
   no `plugins/changed` fires. Subscribers must call `List` for the
   initial fill; subsequent `Disconnect` calls then deliver the
   notifications.

Failure modes:

- A slow / dead subscriber cannot pin the server: the per-conn
  `enqueueFrame` writes through the same buffered channel the response
  goes through (the `rc-streaming-protocol` seam); a permanent failure
  surfaces the conn-down state and the dashboard's disconnect handler
  drops the subscription out-of-band.
- The `plugins/changed` notification is best-effort: errors from the
  broadcaster are returned to the dispatcher but do NOT cause a retry.
  This matches the `rc-permissions-grants` `permissions/changed`
  semantics.
- In-process dispatchers (Unix-socket transport, in-process callers)
  have no broadcaster registered — `BroadcastNotify` is a no-op there.
  The mutation still lands in the store; a follow-up
  `ListRemoteAgentPlugins` poll always reflects the post-state.

---

## Storage shape

| Layer | Path | Format | Permissions |
|---|---|---|---|
| `FSPluginStore` | `$ANTIGRAVITY_HOME/plugins.json` | JSON, versioned (`{"version":1, "plugins":[...], "updated_at":"…"}`) | `0o600` file + `0o700` dir |
| `MemPluginStore` | (none — in-memory) | same shape | n/a |
| `MemPluginStoreWith(custom)` | (none — in-memory) | same shape, sorted ascending by `plugin_id` on read | n/a |

Atomic-write template (mirrors `FSPermissionStore`):

```go
tmp := s.Path + ".tmp"
os.WriteFile(tmp, data, 0o600)
os.Rename(tmp, s.Path)
// best-effort dir fsync
```

Future cycles that need `ConnectRemoteAgentPlugin` /
`UpdateRemoteAgentPlugin` extend the `PluginStore` interface only — the
dispatcher loop already routes on `d.cfg.PluginsStore`, so the seam is
the only edit site.

---

## Sentinel-error map

| Sentinel | JSON-RPC | Reason envelope | Where handled |
|---|---|---|---|
| `ErrPluginNotFound` | `-32015` | `"Plugin not found"` | `mapPluginsError` in `plugins_dispatcher.go` |
| (empty / non-string `plugin_id`) | `-32602` | `"Invalid params"` (verbatim validator message) | `handleDisconnectRemoteAgentPlugin` |
| (no store installed) | `-32603` | `"Internal error"` (verbatim message) | `handle{List,Disconnect}RemoteAgentPlugin` |
| (other I/O — corrupt JSON, malformed state) | `-32603` | `"Internal error"` (with the underlying error in `data`) | `mapPluginsError` in `plugins_dispatcher.go` |

---

## Wiring changes

### `cmd/wagyu/remote_control.go`

Both `maybeStartRemoteControlDaemon` (the Unix-socket path) and
`maybeStartRemoteControlTCPDaemon` (the TCP+WS path) gain a
`PluginsStore: buildHostPluginStore()` field. `buildHostPluginStore()` is
the production-side constructor that resolves
`$ANTIGRAVITY_HOME/plugins.json` and returns an `FSPluginStore` rooted
there. The Unix-socket daemon and the WS daemon share the same backing
file — both surfaces see identical plugin state, and (when run side-by-
side in the same process) the file's `0666` lock avoids concurrent-write
races (the `FSPluginStore`'s mutex is the in-process arbiter; inter-
process races are a `rc-plugins-real-bridge` problem).

### `internal/remotecontrol/daemon.go`

`Config` gains two new fields: `PluginsStore PluginStore` (the seam the
two plugin handlers call into) and `PluginsPath string` (the absolute
path the production `FSPluginStore` loads from / saves to; surfaced so a
startup check can read it without fabricating a store).

### `internal/remotecontrol/host.go`

`HostConfig` mirrors the new fields. The propagation through
`StartHostSession → Start → NewDispatcher` is direct — no host-side
gating logic is needed because the dispatcher's nil-fabrication installs
a fresh `MemPluginStore` when the host didn't wire one (tests benefit
here: a dispatcher built without a `PluginsStore` still has a coherent
answer to the two methods).

### `internal/remotecontrol/dispatch.go`

The dispatcher-level switch gains a `case "ListRemoteAgentPlugins",
"DisconnectRemoteAgentPlugin":` arm with an inner method-name switch
(dispatching to the per-method handlers in `plugins_dispatcher.go`).
`NewDispatcher`'s `nil`-guard for `cfg.PluginsStore` fabricates a fresh
`MemPluginStore` seeded with `DefaultPluginFixtures` when the host
didn't install one.

### `internal/remotetransport/tcp.go`

Already wires `disp.SetBroadcaster(s.broadcastFrame)` at
`NewTCPServer` time (the seam `rc-permissions-grants` added). The
`plugins/changed` notification rides the same broadcaster with no
additional wiring.

### `internal/remotetransport/unix.go`

Already declares `SetBroadcaster` on the `Dispatcher` interface (the
interface contract added in `rc-permissions-grants`). No edit needed
for the cycle but the seam is now documented for the reader.

---

## Tests

### In-process (`internal/remotecontrol/plugins_test.go`, 29 tests)

- `TestNormalisePluginID` × 6 sub-tests (claude / mcp / github / empty /
  whitespace / idempotent)
- `TestDefaultPluginFixtures` (3 entries, all well-known ids, every
  fixture carries ≥1 capability)
- `TestPluginStore_DefaultState_WithFixtures` (fresh MemPluginStore
  returns the 3 fixture ids, sorted ascending)
- `TestMemPluginStore_DefaultsFixturesOrigins` (every fixture carries
  `origin: "config"`)
- `TestPluginStore_Disconnect` (disconnect mcp-bridge → 2 entries
  remain, sorted)
- `TestPluginStore_Disconnect_Missing` (unknown id → ErrPluginNotFound)
- `TestPluginStore_Disconnect_Empty` (`""` / `"   "` →
  ErrPluginNotFound; store defence-in-depth)
- `TestNewMemPluginStoreWith` (custom snapshot constructor: sorts
  ascending, deep-copies Capabilities)
- `TestMemPluginStore_SortStableOnDuplicateIDs` (secondary sort by
  ConnectedAt when ids collide)
- `TestMemPluginStore_ClonePluginState` (Snapshot's defensive copy
  blocks caller → store leaks)
- `TestFSPluginStore_Roundtrip` (fresh + Disconnect + reload → post-
  state survives)
- `TestFSPluginStore_LoadExisting` (manual JSON write → store reloads
  without fixture-re-seeding)
- `TestFSPluginStore_FreshFile` (no file → fixture-seeded state)
- `TestFSPluginStore_Disconnect` (on-disk Disconnect survives reload)
- `TestFSPluginStore_DisconnectMissing` (unknown id → ErrPluginNotFound
  + no .tmp left behind)
- `TestFSPluginStore_AtomicWriteSentinel` (no .tmp after successful
  write)
- `TestFSPluginStore_FilePerms` (0o600 on disk)
- `TestFSPluginStore_MalformedFile` (corrupt JSON surfaces
  `rc-plugins: parse` to caller)
- `TestFSPluginStore_VersionCheck` (future-version file → "newer than
  supported" envelope)
- `TestListRemoteAgentPlugins_Ok` (3 plugins sorted, every plugin
  carries connected_at + capabilities)
- `TestListRemoteAgentPlugins_OkWithParams` (dispatcher ignores
  params; envelope is `ok / plugins`)
- `TestDisconnectRemoteAgentPlugin_Ok` (lean `{ok, plugin_id,
  disconnected_at}` envelope + follow-up List confirms the plugin is
  gone)
- `TestDisconnectRemoteAgentPlugin_NotFound` (unknown id → -32015
  "Plugin not found")
- `TestDisconnectRemoteAgentPlugin_BadParams` × 5 sub-tests (empty
  string / whitespace / missing / non-string number / non-string bool →
  -32602)
- `TestDisconnectRemoteAgentPlugin_NoStore` (nil store → -32603)
- `TestListRemoteAgentPlugins_NoStore` (nil store → -32603 on the read
  path)
- `TestPluginState_SortPinning` (insertion-order independence:
  regardless of input order, Snapshot hands back plugins sorted by
  plugin_id ascending)
- `TestPluginState_DefaultRoundTrip` (default state has version=1 +
  3 plugins + every plugin carries id / version / connected_at)

### Wire-level (`internal/remotetransport/tcp_test.go`, 5 tests)

- `TestListRemoteAgentPlugins_WS_Ok` (List happy path through ws
  round-trip; the response frame carries `ok / plugins`, sorted
  ascending, every plugin row has connected_at + capabilities)
- `TestListRemoteAgentPlugins_WS_OkNoParams` (params-absent call →
  same envelope)
- `TestDisconnectRemoteAgentPlugin_WS_Ok` (lean response envelope +
  `plugins/changed` broadcast drained on c1 + follow-up List confirms
  the plugin is gone)
- `TestDisconnectRemoteAgentPlugin_WS_NotFound` (unknown id →
  -32015 "Plugin not found")
- `TestDisconnectRemoteAgentPlugin_WS_BadParams` × 5 sub-tests (empty
  / whitespace / missing / non-string number / non-string bool → -32602
  "Invalid params")
- `TestPluginsChanged_Broadcast` (broadcast-to-all: two ws conns, c1
  issues a Disconnect, both observe `plugins/changed` carrying the
  post-state — pinned as the cycle's headline ask)

### Helper additions

- `newTestTCPServerWithPlugins(t, ps)` — mirror of
  `newTestTCPServerWithPermissions`; brings up a TCPServer whose
  dispatcher's `PluginsStore` seam is the supplied `PluginStore` (one
  per test, so state doesn't leak).
- `assertPluginListEnvelope(t, label, result, wantCount)` — asserts the
  `{ok, plugins}` shape, the ascending sort, and the per-plugin field
  set (id / name / version / connected_at / non-empty capabilities).
- `assertPluginsChangedNotification(t, label, frame, wantCount)` —
  asserts a `plugins/changed` frame has no `id` field, the right
  method name, and a params that satisfies `assertPluginListEnvelope`.

---

## Style / vetting / formatting

- `gofmt -l` clean on all new + modified files
  (`internal/remotecontrol/plugins.go`,
  `internal/remotecontrol/plugins_store.go`,
  `internal/remotecontrol/plugins_dispatcher.go`,
  `internal/remotecontrol/plugins_test.go`,
  `internal/remotecontrol/dispatch.go`,
  `internal/remotecontrol/daemon.go`,
  `internal/remotecontrol/host.go`,
  `cmd/wagyu/remote_control.go`,
  `internal/remotetransport/tcp_test.go`).
- `go vet ./internal/remotecontrol/ ./internal/remotetransport/
  ./cmd/wagyu/` exit 0 (pre-existing
  `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf` vet warning
  stays as-is — out of scope per the brief).
- `go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/` PASS.
- `go build -o /tmp/wagyu ./cmd/wagyu` exit 0.

---

## 硬约束 / explicit defers

- **Real plugin lifecycle is deferred to `rc-plugins-real-bridge`**.
  This cycle ships a `StubPluginRegistry` that reads entries from a
  manual JSON file, or seeds three fixtures (claude-code-bridge,
  mcp-bridge, github-copilot-bridge) when the file is missing. There is
  no actual Claude Code bridge, MCP bridge, or GitHub Copilot bridge in
  the openagy binary today; the fixtures describe what `agy`'s
  `RemoteAgentPlugin` records *would* look like in production. A
  future cycle that wants real bridges must: (a) replace
  `DefaultPluginFixtures` with the real per-bridge registry, (b) wire
  `ConnectRemoteAgentPlugin` / `UpdateRemoteAgentPlugin` /
  `ListRemoteAgentPluginCapabilities` to the `PluginStore` interface,
  (c) decide whether the bridges run in-process or out-of-process
  (the decompiled agy binary uses an external binary; see
  `remoteAgentEndpoint` etc.). Documented in `PLAN.md`'s gap-closure
  table as a future cycle.
- **`origin` field** is a `omitempty` string today
  (`DefaultPluginFixtures` carry `"config"`). A future cycle that
  introduces per-bridge provenance (`"auto-discovered"`,
  `"sso-imported"`, etc.) extends the wire without bumping
  `version`.
- **No `version` / `updated_at` on the wire envelope** — the
  `ListRemoteAgentPlugins` response shape is byte-for-byte the
  brief's pinned `{ok:true, plugins:[…]}` shape, no extra fields. A
  future cycle that wants versioning on the wire branches on
  `PluginStateWireFormat = 1` and revs the envelope (cycle
  document required).
- **`ListRemoteAgentPlugins` does not broadcast**. A subscriber that
  wants the initial fill must call `List`; subsequent `Disconnect`
  calls deliver `plugins/changed`.
- **`MemPluginStore.With(custom)` is read-only on the input slice**.
  The constructor deep-copies each `Capabilities` slice so a test
  that mutates the input slice never races the store's internal
  state. This is intentional; if you want the store to *see* your
  mutation, take a Snapshot first and rebuild via `NewMemPluginStoreWith`.
- **File-vs-store consistency**: `FSPluginStore.Disconnect` writes
  the post-state to disk atomically (temp + rename + dir fsync +
  0o600 perms). A second daemon running on a different host (or a
  second user) reading the same file will see the post-state but
  only via the file — concurrent inter-process writes are not
  gated by the `FSPluginStore` mutex (the mutex is per-process).
  This is the same scope rule as `FSPermissionStore`; the
  rc-plugins-real-bridge cycle that introduces real bridges will
  pick up the inter-process locking discipline (file locks,
  flock, etc.) as part of the bridge-port wiring.
- **Pre-existing project hygiene**: pre-existing files were not
  reformatted (`gofmt -l` was clean only on the new + directly-
  modified files). The pre-existing `model_default_effort_test.go`
  vet warning is out of scope.

---

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# 1. Build
go build -o /tmp/wagyu ./cmd/wagyu

# 2. Vet (the pre-existing model_default_effort_test.go:67
#    3-arg Errorf warning stays; everything else clean)
go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/

# 3. gofmt (clean on new + modified files)
gofmt -l \
  internal/remotecontrol/plugins.go \
  internal/remotecontrol/plugins_store.go \
  internal/remotecontrol/plugins_dispatcher.go \
  internal/remotecontrol/plugins_test.go \
  internal/remotecontrol/dispatch.go \
  internal/remotecontrol/daemon.go \
  internal/remotecontrol/host.go \
  cmd/wagyu/remote_control.go \
  internal/remotetransport/tcp_test.go

# 4. In-process + wire-level tests (race detector)
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# 5. Run just the plugin tests for a tight loop
go test -race -count=1 ./internal/remotecontrol/ -run "Plugin"
go test -race -count=1 ./internal/remotetransport/ \
  -run "TestListRemoteAgentPlugins|TestDisconnectRemoteAgentPlugin|TestPluginsChanged"

# 6. Smoke the binary
/tmp/wagyu --version
```
