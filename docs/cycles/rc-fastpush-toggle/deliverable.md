# Cycle `rc-fastpush-toggle` — `SetRemoteAgentFastpushEnabled` JSON-RPC toggle (G7 partial)

## TL;DR / 摘要

Cycles 1–22 of `exp/rc-server-mode` shipped 22 JSON-RPC methods. The
[re-investigation matrix](https://github.com/chadgpt/openagy/blob/main/.scratch/rc-reinvestigation.md#method-parity-matrix)
still flags **G7 (Fastpush server-push mechanism)** — agy ships a separate
`SetRemoteAgentFastpushEnabled` JSON-RPC method backed by a
`remoteAgentFastpushClient` server-push transport, with no clean stand-in on
the clone's surface. The full transport (a new push channel that bypasses the
existing `cascade/state` notifications) is a multi-cycle effort; this cycle
ships the **toggle seam** that future cycles will plug the transport into:

- A single `SetRemoteAgentFastpushEnabled` JSON-RPC method that lets a
  dashboard flip a per-cascade fast-push flag on/off and persists the row
  to `$ANTIGRAVITY_HOME/fastpush.json`;
- A `fastpush/setting_changed` server-push notification broadcast (via the
  existing `(*Dispatcher).BroadcastNotify` seam added in
  `rc-permissions-grants` and shared with `rc-plugins` / `rc-triggers`)
  fires on every successful toggle, so a dashboard that subscribes to
  the channel re-renders without polling and dashboards on other conns
  stay in sync with each other;
- An explicit `push_delivery:"deferred"` sentinel in the wire envelope so
  dashboards do not assume an actual delivery path is wired — the future
  `rc-fastpush-channel` cycle flips the sentinel to `"live"` once a real
  transport lands.

The cycle is **persistence only** — the dispatcher records the per-cascade
toggle, flips the cascade's in-memory `(*Cascade).SetFastpush(...)` flag,
and emits the broadcast notification. The `fastpush/*` channel plumbing
itself (per-cascade routing, mid-turn delivery hooks, the
`remoteAgentFastpushClient` analogue) is parked for `rc-fastpush-channel`.
Documented in the **硬约束** section.

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `SetRemoteAgentFastpushEnabled` | write | `{cascade_id, enabled}` | `{ok:true, cascade_id, enabled, channel:"fastpush/cascade", push_delivery:"deferred", updated_at}` | `-32602` (empty / non-string `cascade_id`; wrong-type `enabled`), `-32004` "Cascade not found", `-32603` (no store / I/O) |

Every successful toggle emits a **`fastpush/setting_changed`** notification
to **every** open ws connection (the same broadcast-to-all shape
`rc-permissions-grants` introduced for `permissions/changed`,
`rc-plugins` reused for `plugins/changed`, and `rc-triggers` reused for
`triggers/changed`).

| Piece | Where |
|---|---|
| `FastpushSetting` (`cascade_id` / `enabled` / `channel` / `push_delivery` / `updated_at`) + `FastpushState` (`version:1` / `settings:[…]` / `updated_at`) + `FastpushStore` interface (`Snapshot` / `SetEnabled` / `Get`) + `ErrFastpushCascadeNotFound` sentinel (dispatcher maps to `-32004 "Cascade not found"` via the existing `ErrCodeCascadeNotFound` constant) + `FastpushStateVersion = 1` + `FastpushChannelName = "fastpush/cascade"` + `FastpushPushDeliveryDeferred = "deferred"` + `NotifFastpushSettingChangedMethod = "fastpush/setting_changed"` + `normaliseCascadeID` helper + `fastpushSettingToWire` (per-row wire projection) + `mapFastpushError` (sentinel → JSON-RPC envelope) | `internal/remotecontrol/fastpush.go` (new, ~290 lines) |
| `FSFastpushStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/fastpush.json`; atomic temp + rename + dir fsync + 0o600 perms + 0o700 dir; injectable `now` for tests; optional `checkCascade` closure that the production `buildHostFastpushStore` doesn't wire today — the dispatcher's `(*CascadeRegistry).Get` is the source of truth on cascade existence) + `MemFastpushStore` (in-memory test fixture) + `NewMemFastpushStore` (empty + now + optional checkCascade) + `NewMemFastpushStoreWith` (custom-snapshot constructor with sort-by-CascadeID + deep clone) + `NewFSFastpushStore` / `NewFSFastpushStoreWithCheck` (two-constructor split: production wires the optional cascade-existence check via `WithCheck`; tests use the simpler `NewFSFastpushStore` when they don't bother with a registry) + `onDiskFastpushState` struct + `load` / `atomicWrite` / `ensureParentDir` + `emptyState` / `cloneFastpushState` / `cloneFastpushSetting` defensive copies + `sortSettingsByCascadeID` | `internal/remotecontrol/fastpush_store.go` (new, ~440 lines) |
| `setRemoteAgentFastpushEnabledParams` envelope type + `handleSetRemoteAgentFastpushEnabled` handler + `broadcastFastpushSettingChanged` helper (delegates to `d.BroadcastNotify`); `NotifFastpushSettingChangedMethod = "fastpush/setting_changed"` | `internal/remotecontrol/fastpush_dispatcher.go` (new, ~270 lines) |
| `case "SetRemoteAgentFastpushEnabled":` switch arm + `NewDispatcher` nil-fabrication (fabricates a fresh empty `MemFastpushStore` when `cfg.FastpushStore == nil` — mirrors the TriggersStore nil-guard); package-level comment on the `Dispatch` switch updated to document the rc-fastpush-toggle cycle | `internal/remotecontrol/dispatch.go` (+~60 / -0) |
| `Config.FastpushStore FastpushStore` + `Config.FastpushPath string` (default `auth.AntigravityHome()/fastpush.json`); `HostConfig.FastpushStore` / `HostConfig.FastpushPath` mirror; propagation through `StartHostSession` | `internal/remotecontrol/daemon.go` (+~50 / -0), `internal/remotecontrol/host.go` (+~30 / -0) |
| `buildHostFastpushStore()` helper at the bottom of `cmd/wagyu/remote_control.go` (mirrors `buildHostTriggerStore`); wired into both Unix-socket (`maybeStartRemoteControlDaemon`) and TCP (`maybeStartRemoteControlTCPDaemon`) paths as `FastpushStore: buildHostFastpushStore()` | `cmd/wagyu/remote_control.go` (+~60 / -0) |
| `Fastpush atomic.Bool` field on `*Cascade` + `(*Cascade).SetFastpush(bool)` + `(*Cascade).IsFastpushEnabled() bool` nil-safe accessors; package doc updated to mention rc-fastpush-toggle; mirrors the `Focused atomic.Bool` precedent set in `rc-deep-link` | `internal/remotecontrol/cascade.go` (+~80 / -0) |
| In-process tests: `TestNormaliseCascadeID` × 6 sub-tests + `TestFastpushChannelName` + `TestFastpushPushDeliveryDeferred` + `TestFastpushStateVersion` + 9 store tests (`TestFastpushStore_DefaultState_Empty`, `TestFastpushStore_SetEnabled_New`, `TestFastpushStore_SetEnabled_Upsert`, `TestFastpushStore_SetEnabled_CascadeNotFound`, `TestFastpushStore_SetEnabled_EmptyCascadeID`, `TestFastpushStore_Get_Unknown`, `TestFastpushStore_Get_MissingReturnsFalse`, `TestFastpushStore_Get_AfterSet`, `TestFastpushStore_SetEnabled_SortPinning`) + `TestNewMemFastpushStoreWith` (custom-snapshot + deep-copy) + 6 `TestFSFastpushStore_*` (round-trip, atomic-save, malformed, newer-version, post-write reload, perms) + `TestSortSettingsByCascadeID` + 6 dispatcher round-trips (`TestSetRemoteAgentFastpushEnabled_Ok`, `_ToggleOff`, `_BadParams` × 7 sub-cases, `_CascadeNotFound`, `_NoStore`, `_PushDeliverySentinel`) = **~30 tests** | `internal/remotecontrol/fastpush_test.go` (new, ~880 lines) |
| Wire-level WS round-trip tests + `newTestTCPServerWithFastpush` / `assertFastpushSettingEnvelope` / `assertFastpushSettingChangedNotification` helpers (5 tests: `TestSetRemoteAgentFastpushEnabled_WS_Ok` (asserts `push_delivery == "deferred"`), `_WS_BadParams_EmptyCascadeID`, `_WS_BadParams_MissingEnabled`, `_WS_CascadeNotFound`, `_Broadcasts_Setting_Changed` (two ws conns using `waitForTwoConnIDs`)) | `internal/remotetransport/tcp_test.go` (+~480 / -0) |

What lands for forward consumers:

- A `FastpushStore` seam the dispatcher carries, ready for the future
  `rc-fastpush-channel` to add `ListByChannel` / `Subscribe` /
  `DeliverMidTurn` etc. without re-touching the dispatcher;
- An `FSFastpushStore` JSON-file layout that matches the cycle-9
  `FSProjectStore` / cycle-19 `FSPermissionStore` / cycle-20
  `FSPluginStore` / cycle-21 `FSTriggerStore` style (atomic temp + rename
  + dir fsync, 0600 perms, 0700 dir);
- A `MemFastpushStore` in-memory fixture tests inject without a tempdir;
- A `MemFastpushStore.With(custom snapshot)` constructor that sorts the
  input by `CascadeID` ascending and deep-clones each `Setting` row so a
  test that mutates the returned state's Settings doesn't race the
  store's internal state;
- A `(*Cascade).Fastpush atomic.Bool` flag (mirror of
  `(*Cascade).Focused`) flipped in lockstep with the on-disk row, so
  the future `rc-fastpush-channel` cycle's delivery path can read it
  via `IsFastpushEnabled()` without re-reading the store;
- Reuse of the `BroadcastNotify` / `SetBroadcaster` callback seam from
  `rc-permissions-grants` (shared with `rc-plugins` and `rc-triggers`) —
  future cycles that fan their own "X changed" notifications
  (`rc-conversation-config`, `rc-resources`, `rc-terminals`) share the
  same plumbing.

---

## JSON-RPC surface (precise)

### `SetRemoteAgentFastpushEnabled`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "SetRemoteAgentFastpushEnabled", "params": { "cascade_id": "cas_abcdef0123456789", "enabled": true } }
```

`cascade_id` is **required**, non-empty. Whitespace-only values are
trimmed and rejected as `-32602` (matches the
`SendRemoteAgentCascadeMessage` / `CancelRemoteAgentCascade` /
`HandleRemoteAgentCascadeUserInteraction` validation rule).

`enabled` is **required**, bool. Missing or wrong-type values
(`null`, `"yes"`, `42`) are rejected as `-32602`; explicit `false` is a
legitimate "toggle off" mutation, not a silent default-walk. The
pointer-typed field distinguishes "absent" from "explicit false" so a
dashboard that sends `{"enabled": false}` lands as a legitimate
"toggle off".

Response (success, post-mutation state):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok":            true,
    "cascade_id":    "cas_abcdef0123456789",
    "enabled":       true,
    "channel":       "fastpush/cascade",
    "push_delivery": "deferred",
    "updated_at":    "2026-10-19T12:34:56.789012345Z"
  }
}
```

The response always carries `channel:"fastpush/cascade"` (the
`FastpushChannelName` constant) and `push_delivery:"deferred"` (the
`FastpushPushDeliveryDeferred` sentinel) so dashboards can render a
clear "no live delivery" pill without confusing the toggle with a
transport feature.

Error envelopes:

| Code | Meaning | Trigger |
|---|---|---|
| `-32602` "Invalid params" | `cascade_id` missing / empty / whitespace / non-string; `enabled` missing / wrong type | dashboard sent bad JSON |
| `-32004` "Cascade not found" | `cascade_id` not in the dispatcher's `(*CascadeRegistry).Get` | stale id from a closed/restarted session |
| `-32603` "Internal error" | store I/O error; `cfg.FastpushStore == nil` (post-construction clear) | transient resource problem |

---

## `fastpush.json` schema (the on-disk file `$ANTIGRAVITY_HOME/fastpush.json`)

```json
{
  "version": 1,
  "settings": [
    {
      "cascade_id":    "cas_abcdef0123456789",
      "enabled":       true,
      "channel":       "fastpush/cascade",
      "push_delivery": "deferred",
      "updated_at":    "2026-10-19T12:34:56.789012345Z"
    }
  ],
  "updated_at": "2026-10-19T12:34:56.789012345Z"
}
```

A fresh host with no `fastpush.json` returns the empty state
(`version:1, settings:[]`). The file is written atomically (temp +
rename + dir fsync) with `0o600` perms under the `0o700` parent dir
(`$ANTIGRAVITY_HOME/`), matching the cycle-9 `FSProjectStore` /
cycle-19 `FSPermissionStore` / cycle-20 `FSPluginStore` / cycle-21
`FSTriggerStore` style.

The `version` field is bumped only on breaking changes (renamed keys,
removed fields); non-breaking additions (new optional fields with
`omitempty`) leave it alone so older readers still parse the file. The
brief pins `v1`; the reader refuses any file with `version > 1` (returns
a "newer binary required" envelope that the dispatcher maps to `-32603`).

---

## Broadcast semantics

A successful `SetRemoteAgentFastpushEnabled` emits a
`fastpush/setting_changed` notification to **every** open ws
connection via `(*Dispatcher).BroadcastNotify(method, params)`:

```json
{
  "jsonrpc": "2.0",
  "method":  "fastpush/setting_changed",
  "params":  {
    "ok":            true,
    "cascade_id":    "cas_abcdef0123456789",
    "enabled":       true,
    "channel":       "fastpush/cascade",
    "push_delivery": "deferred",
    "updated_at":    "2026-10-19T12:34:56.789012345Z"
  }
}
```

The notification's params shape is byte-for-byte identical to the
response envelope the originating dash-caller receives — subscribers
that watch the channel and the originator that polls
`SetRemoteAgentFastpushEnabled` see the same shape.

The broadcaster is `nil` for in-process dispatchers and the Unix-socket
transport (request/response only); the broadcast becomes a no-op there.
The mutation still lands in the store; the response envelope echoes the
post-state so polling is always possible. The `(*TCPServer)` registered
broadcaster (set at `NewTCPServer` time) fans the frame to every open
ws connection.

The originating conn's outbound channel holds
`[notification, response]` — `broadcastFastpushSettingChanged` is
called BEFORE `dispatchOne` marshals and enqueues the response. The
notification is the same byte-for-byte shape as the response, but
delivered without an `"id"` field (per JSON-RPC 2.0 §4.1 notification
rules).

---

## `push_delivery:"deferred"` sentinel — what it means for dashboards

The brief calls out an explicit sentinel: dashboards must not assume
actual delivery is wired. This is the cycle's central scope decision.

| Sentinel | Wire value | Meaning | Cycle |
|---|---|---|---|
| `FastpushPushDeliveryDeferred` | `"deferred"` | "The toggle is persisted; no actual delivery path is wired." | rc-fastpush-toggle (today) |
| `FastpushPushDeliveryLive` | `"live"` (future) | "A real `remoteAgentFastpushClient`-style transport is delivering mid-turn updates." | rc-fastpush-channel (future) |

Dashboards that want to render a clear "fast-push is on but no live
delivery yet" pill can read `result.push_delivery == "deferred"` and
apply a `data-deferred` attribute on the toggle. Dashboards that want to
filter for "fast-push is *actually* delivering" should treat the
deferred sentinel as "no".

This is **forward compat**, not a wire-format break: the future
`rc-fastpush-channel` cycle will introduce a parallel
`FastpushPushDeliveryLive` constant and flip the wire value on rows
the new transport serves. Existing dashboards that read the literal
string `"deferred"` continue to work; dashboards that want the
"live" surface branch on the new value when it lands.

---

## Forward compatibility with `rc-fastpush-channel`

The cycle's persistence + toggle surface is the foundation the
`rc-fastpush-channel` cycle will plug a real transport into. The
seam is deliberately narrow today (one method, one store, one
notification) so the future cycle can extend it without breaking
existing dashboards.

What the future cycle is expected to add (out of scope here):

- A `ListByChannel` method on `FastpushStore` (consumers that want
  to iterate the on-disk state without going through
  `SetRemoteAgentFastpushEnabled`'s per-cascade flip);
- A `Subscribe` / `Unsubscribe` pair so dashboards pick which cascades
  they want delivered (today the cycle broadcasts the same envelope
  to every conn);
- A `DeliverMidTurn` hook the dispatcher's cascade-message path calls
  between `OnTextDelta` events so the future transport can stream
  chunks through the fast-push channel;
- The `FastpushPushDeliveryLive` sentinel and the wire envelope's
  `push_delivery` value flip from `"deferred"` to `"live"` on rows
  the new transport serves.

The cycle ships:

- `(*Cascade).Fastpush atomic.Bool` — flipped via
  `(*Cascade).SetFastpush(bool)` after every successful `SetEnabled`
  so the future delivery path's hot read (per-tick check on every
  cascade the model turn drives) is lock-free;
- `FastpushChannelName = "fastpush/cascade"` constant — the future
  per-cascade channel suffixes (e.g.
  `fastpush/cascade/<cas_...>`) only have to extend the
  `FastpushState` on-disk format and bump `FastpushStateVersion`;
- `NotifFastpushSettingChangedMethod = "fastpush/setting_changed"`
  constant — the future mid-turn notif
  (`fastpush/delivered` or similar) is a fresh constant.

The cycle's doc-comments on `fastpush.go` / `fastpush_dispatcher.go`
flag every extension point the future cycle will touch, so the
auditor can grep the cycle boundary.

---

## Hard constraints / 硬約束

What the cycle **did not** ship, by deliberate scope decision:

1. **No actual fast-push delivery path.** The wire envelope's
   `push_delivery:"deferred"` sentinel is the explicit contract:
   dashboards that want a real `remoteAgentFastpushClient`-style
   transport must wait for the future `rc-fastpush-channel` cycle.
   The toggle is *persistence only* — recording the per-cascade bit,
   flipping `(*Cascade).Fastpush`, and emitting the broadcast
   notification. The dispatcher's cascade-message handler does not
   read `IsFastpushEnabled()` today.
2. **No per-cascade channel routing.** Every successful toggle
   broadcasts the same `fastpush/setting_changed` notification to
   every open ws connection; consumers that want "only cascades I
   care about" must filter on the `params.cascade_id` field. A
   future cycle that adds a per-cascade channel suffix
   (`fastpush/cascade/<cas_...>`) only has to extend
   `FastpushChannelName` and bump `FastpushStateVersion`.
3. **No `ListFastpushSettings` / `GetFastpushSetting` JSON-RPC
   methods.** The cycle ships `SetEnabled` only; `Snapshot` and
   `Get` on the `FastpushStore` interface are there for the future
   `rc-fastpush-channel` cycle's `ListByChannel` / `Subscribe`
   paths. Today's brief is silent on the read surface.
4. **No `GetRemoteAgentCascadeFastpushStatus` (or equivalent)
   read-side RPC.** The cycle's `(*Cascade).IsFastpushEnabled()`
   is in-memory only — dashboards that want to read the persisted
   toggle for an arbitrary cascade must wait for the future
   `rc-fastpush-channel` cycle's read RPC. The current state is
   observable through `SetRemoteAgentFastpushEnabled` itself
   (the response envelope echoes the post-state).
5. **The Unix-socket transport's broadcast is a no-op.** Only the
   `(*TCPServer)` registers a broadcaster; the Unix-socket daemon
   doesn't install one. Mutations still land in the store, and
   `SetRemoteAgentFastpushEnabled` echoes the post-state in the
   response envelope so polling is always possible. This matches
   the `rc-plugins` / `rc-triggers` precedent: a Unix-socket-only
   deployment that wants the broadcast must either switch to the
   WS transport or accept the polling-only surface.
6. **`(*Cascade).Fastpush` is in-memory only.** A restart of the
   daemon zeroes the bit; the on-disk row in `fastpush.json` is
   the source of truth. The future `rc-fastpush-channel` cycle is
   expected to seed the in-memory bit from the on-disk row at daemon
   start (mirroring the cycle-21 `FSTriggerStore` /
   `(*Cascade).focused` re-hydration pattern).
7. **The cascade-existence check is wired at the dispatcher layer
   (`(*CascadeRegistry).Get`), not at the store layer.** The
   `FSFastpushStore`'s `checkCascade` field exists for future
   defence-in-depth (e.g. a future cycle that wants to run the
   store without a dispatcher-bound registry), but
   `cmd/wagyu/remote_control.go`'s `buildHostFastpushStore` does
   NOT wire it today. The dispatcher's handler runs the same check
   via `(*CascadeRegistry).Get(cascadeID)` so the brief's `-32004`
   envelope is preserved; `checkCascade` is only a backstop for
   non-dispatcher callers.

The cycle's `docs/cycles/PLAN.md` row now reads
**DONE `<this-commit>`** for `rc-fastpush-toggle` (G7 partial);
the G7 line in the re-investigation matrix reduces from "🟡 **no
fast-push server-push channel**" to "🟡 **toggle seam shipped; full
channel plumbing in rc-fastpush-channel**" once the verifier's next
matrix run reflects the change.

---

## Reviewer commands

```bash
# Build the cycle's binary (must exit 0).
go build -o /tmp/wagyu ./cmd/wagyu

# Run the in-process + wire-level tests (must PASS).
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# Vet the cycle's touched packages (pre-existing
# model_default_effort_test.go:67 3-arg Errorf is out of scope; the
# cycle does not touch it).
go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/

# Format-check the cycle's touched files (must emit no
# filenames; pre-existing dirt across internal/ is out of scope).
gofmt -l \
    internal/remotecontrol/fastpush.go \
    internal/remotecontrol/fastpush_store.go \
    internal/remotecontrol/fastpush_dispatcher.go \
    internal/remotecontrol/fastpush_test.go \
    internal/remotecontrol/cascade.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotecontrol/dispatch.go \
    internal/remotetransport/tcp_test.go \
    cmd/wagyu/remote_control.go

# Run only the cycle's in-process tests.
go test -race -count=1 ./internal/remotecontrol/ \
    -run 'TestFastpush|TestSetRemoteAgentFastpush|TestNormaliseCascadeID|TestNewMemFastpushStoreWith|TestSortSettingsByCascadeID|TestFSFastpushStore'

# Run only the cycle's wire-level tests.
go test -race -count=1 ./internal/remotetransport/ \
    -run 'TestSetRemoteAgentFastpushEnabled'
```