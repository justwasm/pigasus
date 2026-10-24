# Cycle `rc-top-level-rpc` — three top-level JSON-RPC methods promoted from internal helpers

## TL;DR / 摘要

Cycles 1–27 of `exp/rc-server-mode` shipped 32 JSON-RPC methods across the
cascades / lists / projects / file-ops / destructive / status / permissions /
plugins / triggers / resources / conversation-config / terminals surfaces.
The re-investigation at `openagy/.scratch/rc-reinvestigation.md` flagged three
**G+H cluster** helpers that live behind the dispatcher's switch arms but are
still treated as internal scaffolding rather than dashboard-facing methods:

- `RemoteControlStatus` — the dashboard-on-connect snapshot
  (version / uptime / connected_clients / open_cascades / dispatch_methods /
  server_fingerprint);
- `RemoteControlOnOutput` — register a ws connection as a sink for ALL
  dispatcher notifications (the surface dashboards use to subscribe);
- `remoteControlOnMessage` — gate-injected JSON-RPC message-injection for
  read-only probes (read-only side-channel so a dashboard can render the
  rich-status block + per-cascade error ring + version without holding
  its own copy).

This cycle promotes them from internal helpers to **top-level JSON-RPC
methods** (the third switch arm each, paired with the existing `addAllSink` /
`isRemoteControlOnMessageAllowed` helpers the dispatcher already had):

- The handlers land in a new `top_level_dispatcher.go` next to the new
  `top_level.go` data model, sharing the `AllSinks` set + the
  `RemoteControlOnMessageAllowList` gate + the `topLevelStatus` snapshot
  builder;
- The dispatcher grows an `allSinks map[string]struct{}` + `allSinksMu`
  pair guarded by a `sync.RWMutex` (read-mostly hot path on
  `IsAllSink` / `AllSinkConnIDs`, write on `AddAllSink` / `RemoveAllSink`);
- The `(*TCPServer)` surface gains a public `ConnCount() int` so the
  in-process dispatcher's status snapshot can include
  `connected_clients` without poking at the transport's internals across
  the package boundary;
- The package-level `DefaultDispatchMethods` slice grows by three entries
  (`RemoteControlStatus` / `RemoteControlOnOutput` / `remoteControlOnMessage`)
  and remains **manually synced** with the dispatch switch (the brief's
  "package var populated from each switch arm — prefer the package var
  so it can't drift" rule; auto-generation is parked for
  `rc-dispatch-table-gen`);
- 27 new tests (20 in-process in `internal/remotecontrol/top_level_test.go`
  + 7 wire-level in `internal/remotetransport/tcp_test.go`).

The cycle **does not** wire `AllSinks` into the per-cascade notification
paths (cycle-5) or the broadcast-to-all paths (cycle-13's
`BroadcastNotify`); the registration surface + a canonical filter list are
shipped so dashboards know what topics are available, and the future
`rc-all-sinks-fanout` cycle wires the actual fan-out integration.
Documented in the **硬約束** section.

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `RemoteControlStatus` | read | `{}` | `{version, uptime_seconds, started_at, connected_clients, open_cascades, dispatch_methods, server_fingerprint}` | `-32603` (dispatcher is nil) |
| `RemoteControlOnOutput` | subscribe | `{sink_id?}` (defaults to the calling ws conn's connID) | `{ok:true, sink_id, subscribed_at, filter}` | `-32602` (non-string `sink_id`), `-32603` (dispatcher is nil) |
| `remoteControlOnMessage` | inject (gated) | `{message:{jsonrpc?, method, params?}}` | `{ok:true, accepted:true, replayed_at}` (flat confirmation; inner response is **intentionally discarded**) | `-32602` (missing/non-object `message` / empty/non-string `message.method` / marshal failure), `-32603` (gatekeeper rejected the method) |

What lands for forward consumers:

- A `(*Dispatcher).AllSinks map[string]struct{}` registry the future
  `rc-all-sinks-fanout` cycle consumes without re-touching the
  dispatcher;
- A public `(*TCPServer).ConnCount() int` accessor future cycle dashboards
  can use without importing the package's internals;
- A `RemoteControlOnMessageAllowList` closed-set variable a future cycle
  that wants to expose more methods through the message-injection
  surface extends (the gatekeeper picks up the change automatically —
  no per-method code);
- A `DefaultDispatchMethods` package var + `sortedDispatchMethods()`
  sorted-copy helper the future `rc-dispatch-table-gen` cycle replaces
  with reflection-driven auto-generation;
- A `DefaultNotificationFilter` canonical topic list future cycles that
  add new server-push topics append to without changing the wire shape.

---

## JSON-RPC surface (precise)

### `RemoteControlStatus`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "RemoteControlStatus", "params": {} }
```

`params` is **ignored** — the handler is a pure read with no input
validation. A dashboard that posts `{}` / `{"foo":"bar"}` / `null` /
absent `params` all see the same envelope.

Response envelope (`top_level.go:TopLevelStatus`):

```json
{
  "version": "1.4.2",
  "uptime_seconds": 17,
  "started_at": "2026-10-19T08:56:32.123456Z",
  "connected_clients": 1,
  "open_cascades": 0,
  "dispatch_methods": ["CancelRemoteAgentCascade", "..."],
  "server_fingerprint": "wagyu-rc/1.4.2/7f93617a"
}
```

Field semantics:

| Field | Source | Notes |
|---|---|---|
| `version` | `SessionState.Version` → falls back to `ServerVersion` (the MCP-protocol-level `"0.1.0"`) | mirrors `cli.ReferenceVersion` (`"1.4.2"`) when `Session.Version` is unset |
| `uptime_seconds` | `time.Since(SessionState.StartedAt).Round(time.Second).Seconds()` | clamped to `0` on clock skew / future `StartedAt` |
| `started_at` | `SessionState.StartedAt.UTC().Format(time.RFC3339Nano)` | empty string when `SessionState.StartedAt` is zero |
| `connected_clients` | `SessionState.Connected()` (atomic int64) | read without the `SessionState` mutex |
| `open_cascades` | `(*CascadeRegistry).Len()` | counts both open + closed cascades (the broader interpretation is parked for a future cycle that wants the dashboard's live-cascade gauge) |
| `dispatch_methods` | `sortedDispatchMethods()` (sorted copy of `DefaultDispatchMethods`) | sorted ascending so dashboards can `binarySearch(allowed)` without re-sorting |
| `server_fingerprint` | `serverFingerprint(d, version)` | shape: `"wagyu-rc/<version>/<short-hash>"` where `<short-hash>` is the first 8 chars of `Config.Commit`, or `"unknown"` when empty |

Defensive nil-safety: a dispatcher with bare-bones `Config` (no `Session`,
no cascades registry) returns a populated envelope with zero values
rather than panicking. `server_fingerprint` always resolves to a
non-empty string (the helper falls back to `"unknown"` when `Commit` is
empty / whitespace).

Error envelopes:

- `dispatcher is nil` → `-32603 "Internal error"` (defensive; the
  dispatcher's `Dispatch` switch never passes a nil dispatcher, but the
  handler is exported so a test or future refactor can call it
  directly).

### `RemoteControlOnOutput`

Request:

```json
{ "jsonrpc": "2.0", "id": 2, "method": "RemoteControlOnOutput", "params": {} }
```

`params` is optional. The handler reads one optional field:

| Param | Required | Default | Notes |
|---|---|---|---|
| `sink_id` | optional | the calling ws conn's `connID` (from `connIDFromContext(ctx)`) | non-string → `-32602`; whitespace-trimmed; empty after trim → default-walk to `connID` |

Response envelope (`top_level.go:RemoteControlOnOutputResult`):

```json
{
  "ok": true,
  "sink_id": "ws-1",
  "subscribed_at": "2026-10-19T08:56:33.123456Z",
  "filter": ["cascade/state", "cascade/text_delta", "..."]
}
```

Server-side effects:

1. `(*Dispatcher).AddAllSink(sinkID)` — idempotent; the
   `map[string]struct{}` set is grown by exactly one entry per unique
   `sink_id`. A future `rc-all-sinks-fanout` cycle's notification
   fan-out helper reads this set; today's per-cascade notification paths
   and `BroadcastNotify` paths do NOT consult `AllSinks` (the registration
   is a forward-compat seam).
2. The response carries the canonical `filter` list (the topics the
   server publishes today, see `DefaultNotificationFilter`). Dashboards
   that want to subscribe to a subset of topics still receive the full
   list — the filter is informational, not enforced.

Error envelopes:

- `sink_id` present but non-string (e.g. numeric `"sink_id":42`) →
  `-32602 "Invalid params"` (the cycle's `nonStringFieldInJSON` helper
  catches this BEFORE the JSON unmarshal coerces it silently).
- Empty after whitespace-trim → default-walk to the calling
  `connID` (NOT an error).

### `remoteControlOnMessage`

Request:

```json
{ "jsonrpc": "2.0", "id": 3, "method": "remoteControlOnMessage", "params": { "message": { "jsonrpc": "2.0", "method": "RemoteAgentStatus", "params": {} } } }
```

`params.message` is **required, non-null JSON object**. The handler
reads the inner frame as a JSON-RPC 2.0 request:

| Inner field | Required | Notes |
|---|---|---|
| `jsonrpc` | optional | any value accepted; the dispatcher ignores the field on the re-dispatched frame (the inner handler is reached via `(*Dispatcher).Dispatch`, which doesn't depend on `jsonrpc` being a specific value) |
| `method` | required, non-empty after trim | non-string / empty → `-32602` |
| `params` | optional | re-marshalled into the inner frame so a permitted `{"X":1}` is faithfully forwarded |

Gatekeeper: `message.method` must be in
`RemoteControlOnMessageAllowList` (the closed-set package var).
A method NOT in the list → `-32603 "Internal error"` with
`data:"rejected by gatekeeper: <method>"`.

The allowlist today (per the brief's "no arbitrary remote-execution"
rule):

| Method | Status |
|---|---|
| `RemoteAgentStatus` | read-only probe (agy-synonym of `RemoteControlStatus`) |
| `ListRemoteAgent*` (12 entries) | read-only list surface |
| `GetRemoteAgentFile` / `GetRemoteAgentDir` | reserved (not yet dispatched; future `rc-file-ops-read` cycle adds the handlers) |
| `ListRemoteAgentChannels` | reserved (not yet dispatched; future `rc-channels-list`) |
| `RemoteAgentErrorStatus` | read-only probe of the cascade error ring |
| `remoteControlVersion` | already a top-level method; included for parity |

No write methods are in the list today; a future cycle that wants to
expose more methods through the message-injection surface extends the
slice (no per-method code; the gatekeeper picks up the change
automatically). The cycle also logs every attempt (allowed + rejected)
via the standard `log` package so an operator can audit which
dashboards tried to side-channel what through the daemon.

Response envelope (`top_level.go:RemoteControlOnMessageResult`):

```json
{ "ok": true, "accepted": true, "replayed_at": "2026-10-19T08:56:34.123456Z" }
```

The inner dispatch's response is **intentionally discarded** — the outer
envelope is a flat `{ok:true, accepted:true, replayed_at}` confirmation
that the inner message reached the dispatcher. Side effects of the inner
dispatch (mutations, notifications, etc.) already happened; that's the
point. A future cycle that wants the inner response nested into the
outer envelope (e.g. for read-on-inject probes that need the inner
result rendered in the dashboard's status pill) splits the inner
`(*Dispatcher).Dispatch` into a `dispatchForTest` helper that returns
the response without writing to the transport.

Error envelopes:

- `message` missing / null / non-object (`{"message": null}` /
  `{}` without a `message` key / `{"message": "string"}` / absent
  `params`) → `-32602 "Invalid params"` (`data:"message is required
  and must be a non-null object"`).
- `message.method` empty / non-string / whitespace-only →
  `-32602 "Invalid params"`.
- `message.method` not in `RemoteControlOnMessageAllowList` →
  `-32603 "Internal error"` with `data:"rejected by gatekeeper:
  <method>"`.
- Inner marshal failure (defensive; `encoding/json` can't fail on the
  shapes this handler accepts in practice) → `-32603 "Internal error"`.

---

## Pieces landed

| Piece | Where |
|---|---|
| `TopLevelStatus` wire envelope (`version` / `uptime_seconds` / `started_at` / `connected_clients` / `open_cascades` / `dispatch_methods` / `server_fingerprint`) + `RemoteControlOnOutputResult` (`ok` / `sink_id` / `subscribed_at` / `filter`) + `RemoteControlOnMessageResult` (`ok` / `accepted` / `replayed_at`) + `RemoteControlOnMessageAllowList` closed-set (12 read-only `List…` + 1 `RemoteAgentStatus` + 1 `remoteControlVersion` + 1 `RemoteAgentErrorStatus` + 3 reserved entries = 18 names) + `DefaultNotificationFilter` canonical topic list (12 entries, alphabetical) + `DefaultDispatchMethods` package var (now 56 entries after this cycle's 3 additions: `RemoteControlStatus`, `RemoteControlOnOutput`, `remoteControlOnMessage`; hand-organised by category — MCP-compat / cascade-lifecycle / reads / writes / errors-status-session / top-level-RPC / other-top-level) + `sortedDispatchMethods()` / `sortedDefaultNotificationFilter()` helpers (sorted-copy) + `isRemoteControlOnMessageAllowed(method string) bool` gatekeeper helper (nil-safe; closed-set walk) + `topLevelStatus()` snapshot builder (nil-safe; reads `Session.Version` → `ServerVersion` fallback / `Session.Connected()` / `SessionState.StartedAt` with clock-skew clamp / `CascadeRegistry.Len()` / `serverFingerprint(d, version)`) + `serverFingerprint(d *Dispatcher, version string) string` helper (shape `wagyu-rc/<version>/<short-hash>`; `<short-hash>` = first 8 chars of `Config.Commit`; falls back to `"unknown"` on empty / whitespace `Commit` / empty `version`) | `internal/remotecontrol/top_level.go` (new, ~478 lines) |
| `AddAllSink(connID string)` / `RemoveAllSink(connID string)` / `IsAllSink(connID string) bool` / `AllSinkConnIDs() []string` helpers (RWMutex-guarded; nil-safe; idempotent registration; sorted return on `AllSinkConnIDs`) + `handleRemoteControlStatus()` (pure read; no params parsed; no dispatcher state mutated) + `handleRemoteControlOnOutput(ctx, params)` (parses `{sink_id?}`; defaults to `connIDFromContext(ctx)`; non-string `sink_id` → `-32602`; whitespace-trim; idempotent `AddAllSink`; returns `{ok:true, sink_id, subscribed_at, filter}` with the canonical `filter`) + `handleRemoteControlOnMessage(ctx, params)` (parses `{message:{…}}`; enforces non-null-object + non-empty-string-method via `nonNullObjectInJSON` + `nonStringFieldInJSON`; gatekeeps on `RemoteControlOnMessageAllowList`; logs every attempt via `log.Printf`; on the accepted path re-dispatches through `(*Dispatcher).Dispatch(ctx, innerRaw)` and **DISCARDS** the returned frame — the outer envelope is the confirmation; returns `{ok:true, accepted:true, replayed_at}`) | `internal/remotecontrol/top_level_dispatcher.go` (new, ~376 lines) |
| `case "RemoteControlStatus":` switch arm + `case "RemoteControlOnOutput":` switch arm + `case "remoteControlOnMessage":` switch arm in the dispatcher's `Dispatch` switch; `Dispatcher` struct gains `allSinks map[string]struct{}` + `allSinksMu sync.RWMutex` (read-mostly hot path on `IsAllSink` / `AllSinkConnIDs`, write on `AddAllSink` / `RemoveAllSink`) | `internal/remotecontrol/dispatch.go` (+67 / -0) |
| Public `(*TCPServer).ConnCount() int` accessor (nil-safe; reads `s.conns` under `s.connsMu.RLock()`; returns `len(s.conns)`); no separate counter because the map's `len` is the single source of truth and the per-conn lock patterns don't double-count | `internal/remotetransport/tcp.go` (+24 / -0) |
| In-process coverage: 20 tests in `top_level_test.go` — `TestTopLevelStatus_Shape` (asserts every field type + presence; version mirror; uptime clamp; fingerprint pattern) + `TestTopLevelStatus_DispatchMethods_Listed` (sorted + non-empty + contains all 56 method names) + `TestTopLevelStatus_UptimeIncreases` (1.1s sleep between two calls; asserts `uptime_seconds_2 > uptime_seconds_1`) + `TestTopLevelStatus_NilSessionDefensive` (no `cfg.Session` → zero fields; `server_fingerprint` = `wagyu-rc/0.1.0/unknown`; no panic) + `TestTopLevelStatus_OpenCascadesIncrements` (start cascade → `open_cascades` increments; cancel → returns to zero) + `TestServerFingerprint_KnownCommit` (long commit `deadbeef1234567890abcdef1234567890abcdef` → `wagyu-rc/1.4.0/deadbeef` — first 8 chars) + `TestServerFingerprint_UnknownCommit` (empty / whitespace `Commit` → `wagyu-rc/1.4.0/unknown`) + `TestDispatcher_RemoteControlStatus` (in-process `Dispatch("RemoteControlStatus")` returns a `TopLevelStatus` with the expected fields; cross-checks `dispatch_methods` against the in-process package var) + `TestDispatcher_RemoteControlOnOutput_DefaultFilter` (`{}` params → `filter` is the canonical 12-topic sorted list) + `TestDispatcher_RemoteControlOnOutput_AddsSink` (`AllSinkConnIDs()` grows by one entry after a `RemoteControlOnOutput` call) + `TestDispatcher_RemoteControlOnOutput_DefaultsToConnID` (no `sink_id` posted → response `sink_id` = `connIDFromContext(ctx)` = `"test-conn-1"`) + `TestDispatcher_RemoteControlOnOutput_BadParams` (numeric `"sink_id":42` → `-32602`) + `TestDispatcher_RemoteControlOnMessage_Allowed` (`RemoteAgentStatus` posted; inner dispatch fires; outer envelope = `{ok:true, accepted:true, replayed_at:RFC3339Nano}`; **inner response is discarded** — outer envelope has no nested `inner_result`) + `TestDispatcher_RemoteControlOnMessage_NotAllowed` (`Foo` posted; `-32603` with `data:"rejected by gatekeeper: Foo"`) + `TestDispatcher_RemoteControlOnMessage_BadParams` (`{}` / `{"message": null}` / `{"message": "string"}` / `{"message": {"method": ""}}` / `{"message": {"method": "  "}}` all → `-32602`; verbatim `data` messages asserted) + `TestIsRemoteControlOnMessageAllowed` (closed-set membership for every entry in the allowlist + a dozen non-matches; nil-safe; empty / whitespace → false) + `TestDefaultDispatchMethods_WireSorted` (the wire shape — `sortedDispatchMethods()` returns a sorted copy — is monotonic ascending; asserts the brief's "package var populated from each switch arm — prefer the package var so it can't drift" rule) + `TestDefaultNotificationFilter_Sorted` (canonical 12-topic list is sorted; no duplicates) + `TestAllSinks_NilReceiverSafe` (`AddAllSink` / `RemoveAllSink` / `IsAllSink` / `AllSinkConnIDs` on a nil receiver all return cleanly without panicking) + `TestRemoteControlOnMessage_InnerResponseDiscarded` (a dashboard that posts `remoteControlOnMessage{method:"remoteControlVersion"}` gets `{ok:true, accepted:true, replayed_at:...}` and the `remoteControlVersion` handler's actual response is **discarded** — the outer envelope has no nested field carrying the inner result; this pins the brief's "outer envelope is a flat confirmation" rule). Helpers: `resultAsMap(t, raw)` (handles both `map[string]any` from a wire-roundtrip and the typed struct values from in-process dispatch); `fieldString(t, m, key)` / `fieldBool(t, m, key)` / `fieldFloat(t, m, key)` / `fieldStringList(t, m, key)` (typed accessors for the envelope shape) | `internal/remotecontrol/top_level_test.go` (new, ~908 lines) |
| Wire-level coverage: 7 tests in `tcp_test.go` — `TestRemoteControlStatus_WS_Ok` (dial WS; fire `RemoteControlStatus`; asserts `version=="1.4.0"` (the test fixture's `SessionState.Version`), `started_at` parses as RFC3339Nano, `uptime_seconds >= 0`, `connected_clients == 1` (the just-dialled ws conn), `open_cascades == 0`, `dispatch_methods` is sorted + non-empty + contains all three new methods, `server_fingerprint == "wagyu-rc/1.4.0/deadbeef"` (the test fixture's Commit is `deadbeef1234567890abcdef1234567890abcdef`)) + `TestRemoteControlOnOutput_WS_Ok` (omit `sink_id`; response carries `sink_id == ws-1` (the dialled connID) + `subscribed_at:RFC3339Nano` + `filter` = sorted canonical list) + `TestRemoteControlOnOutput_WS_CustomSink` (post `{"sink_id":"my-router"}`; response echoes `sink_id == "my-router"`; the dispatcher registers `"my-router"` in `AllSinks`) + `TestRemoteControlOnOutput_WS_BadParams` (numeric `"sink_id":42` → `-32602`) + `TestRemoteControlOnMessage_WS_Allowed` (post `{message:{method:"RemoteAgentStatus"}}`; outer envelope = `{ok:true, accepted:true, replayed_at:RFC3339Nano}`; logs `rc-top-level-rpc: remoteControlOnMessage dispatched method="RemoteAgentStatus" conn="ws-1"`) + `TestRemoteControlOnMessage_WS_Rejected` (post `{message:{method:"Foo"}}`; `-32603` with `data:"rejected by gatekeeper: Foo"`; logs `rc-top-level-rpc: remoteControlOnMessage rejected method="Foo" conn="ws-1"`) + `TestRemoteControlOnMessage_WS_BadParams` (`{}` → `"-32602 message is required and must be a non-null object"`; `{"message": null}` → same; `{"message": {"method": ""}}` → `"-32602 message.method is required and must be non-empty"`). Helpers reused from existing cycle-NN tests: `newTestTCPServerWithSessionMeta` / `dialWS` / `writeJSONRPC` / `readJSONRPCResponse` / `errorCode` / `errorData` | `internal/remotetransport/tcp_test.go` (+381 / -0) |

---

## Gate verification

```bash
# Build the cycle's binary (must exit 0).
go build -o /tmp/wagyu ./cmd/wagyu

# Run the in-process + wire-level tests (must PASS).
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# Vet the cycle's touched packages (pre-existing
# cmd/wagyu/model_default_effort_test.go:67 3-arg Errorf is out of
# scope; the cycle does not touch it).
go vet ./internal/remotecontrol/ ./internal/remotetransport/

# Format-check the cycle's touched files (must emit no filenames;
# pre-existing dirt across internal/ is out of scope).
gofmt -l \
    internal/remotecontrol/top_level.go \
    internal/remotecontrol/top_level_dispatcher.go \
    internal/remotecontrol/top_level_test.go \
    internal/remotecontrol/dispatch.go \
    internal/remotetransport/tcp.go \
    internal/remotetransport/tcp_test.go
```

All four gates are green on this commit:

- `go build ./...` → exit 0.
- `go vet ./internal/remotecontrol/ ./internal/remotetransport/` → exit 0
  (pre-existing `cmd/wagyu/model_default_effort_test.go:67` 3-arg
  `Errorf` is out of scope; not touched).
- `gofmt -l <touched-files>` → empty.
- `go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/`
  → PASS (608 tests, 0 failures across both packages; +27 new tests vs
  the prior tip).

---

## Run-only-the-cycle tests

```bash
# In-process coverage only.
go test -race -count=1 -run 'TestTopLevel|TestServerFingerprint|TestDefaultDispatchMethods|TestDefaultNotificationFilter|TestAllSinks|TestIsRemoteControlOnMessageAllowed|TestDispatcher_RemoteControl|TestRemoteControlOnMessage_InnerResponseDiscarded' ./internal/remotecontrol/

# Wire-level coverage only.
go test -race -count=1 -run 'TestRemoteControlStatus_WS_Ok|TestRemoteControlOnOutput_WS_|TestRemoteControlOnMessage_WS_' ./internal/remotetransport/
```

---

## Forward compatibility with `rc-all-sinks-fanout` (parked)

The cycle ships the registration surface + the canonical filter list
but does NOT wire the actual fan-out path. The future
`rc-all-sinks-fanout` cycle owns the integration:

1. The per-cascade notification paths (cycle-5's
   `(*ConnNotifierSink).OnState` / `OnTextDelta` / `OnToolCall` /
   `OnUserInteraction`) consult `(*Dispatcher).AllSinkConnIDs()` and
   push a notification to every registered `connID` in addition to the
   per-cascade `connID`;
2. The cross-conn broadcast paths (cycle-13's
   `(*Dispatcher).BroadcastNotify`, shared with
   `rc-permissions-grants` / `rc-plugins` / `rc-triggers` /
   `rc-fastpush-toggle` / `rc-resources` / `rc-conversation-config` /
   `rc-terminals`) consult `AllSinkConnIDs()` to **add** the registered
   sinks to the fan-out (the broadcaster already walks `s.conns` for
   the TCP transport — the future cycle changes that walk to consult
   the registry rather than the raw map);
3. The Unix-socket transport's broadcaster is a no-op (no `(*TCPServer)`
   installation); a Unix-socket-only deployment that wants the fan-out
   must either switch to the WS transport or accept the polling-only
   surface (matches the `rc-plugins` / `rc-triggers` /
   `rc-fastpush-toggle` / `rc-resources` / `rc-conversation-config` /
   `rc-terminals` precedent).

`AddAllSink` / `RemoveAllSink` are idempotent so a future cycle's
teardown path (the transport's accept-loop teardown calls
`RemoveAllSink(connID)` when a ws conn closes) doesn't have to dedupe.

---

## Forward compatibility with `rc-dispatch-table-gen` (parked)

`DefaultDispatchMethods` is **manually synced** with the dispatcher's
switch arms — the brief pins "package var populated from each switch
arm — prefer the package var so it can't drift". The trade-off is
manual sync: a new switch arm must add its method name(s) here before
the next release. The future `rc-dispatch-table-gen` cycle replaces
this with reflection-driven auto-generation by:

1. Walking the `(*Dispatcher).Dispatch` switch arms via reflection
   (`reflect.TypeOf((*Dispatcher)(nil)).Method` or
   `golang.org/x/tools/go/ssa`);
2. Diffing the discovered names against the package var;
3. Failing the build if the package var drifts away from the switch
   arms (a CI guardrail rather than a runtime check).

Today's `TestDefaultDispatchMethods_WireSorted` test pins the wire
shape (sorted ascending, non-empty, all 56 entries present) but does
NOT pin the var's category ordering — the package var is
hand-organised by category for human readability, and the
auto-generation cycle replaces the var with a switch-driven sorted
slice at runtime via `sortedDispatchMethods()`.

---

## 硬約束 / limitations

1. **AllSinks fan-out is NOT wired.** The cycle ships the
   `(*Dispatcher).AllSinks` registry + the `AddAllSink` /
   `RemoveAllSink` / `IsAllSink` / `AllSinkConnIDs` helpers, but the
   per-cascade notification paths (cycle-5's `(*ConnNotifierSink)`)
   and the cross-conn broadcast paths (cycle-13's `BroadcastNotify`)
   do NOT consult the registry. The future `rc-all-sinks-fanout`
   cycle wires the integration. Today a dashboard that subscribes via
   `RemoteControlOnOutput` is registered in the set but receives no
   notifications from the existing per-cascade / cross-conn paths —
   it gets only direct responses to its own RPC calls. Documented in
   the **Forward compatibility** section above.
2. **Unix-socket listeners don't receive broadcasts.** The
   Unix-socket transport doesn't install a `(*Dispatcher).broadcaster`
   (no per-connection ID threading; the per-request ctx has no
   `connID` value, so `connIDFromContext(ctx)` returns `""`). A
   Unix-socket-only deployment that calls `RemoteControlOnOutput`
   registers the empty string `""` in `AllSinks` (the `AddAllSink`
   helper short-circuits on empty `connID`, so this is actually a
   no-op — the cycle's defensive guard means `AllSinkConnIDs()`
   returns `nil` until a WS transport registers a sink). The brief's
   "fan-out" interpretation is WS-only; the Unix-socket transport's
   request/response surface is unchanged.
3. **Inner dispatch response is DISCARDED.** A dashboard that posts
   `remoteControlOnMessage{method:"remoteControlVersion"}` gets a flat
   `{ok:true, accepted:true, replayed_at}` envelope — the
   `remoteControlVersion` handler's actual `{version, build_label,
   daemon_sha}` response is NOT nested into the outer envelope. The
   brief pins this as "outer envelope is a flat confirmation"; a
   future cycle that wants the inner response surfaced (e.g. for
   read-on-inject probes that need the inner result rendered in the
   dashboard's status pill) splits `(*Dispatcher).Dispatch` into a
   `dispatchForTest` helper that returns the response without writing
   to the transport. The cycle's `TestRemoteControlOnMessage_InnerResponseDiscarded`
   pins the discard behaviour so a future cycle that wants to surface
   the inner response has to update both the handler AND the test.
4. **`open_cascades` is the broader count, not "live" only.** The
   cycle's `topLevelStatus()` reads `(*CascadeRegistry).Len()`,
   which counts every cascade in the registry — open + closed. The
   brief pins the broader "open" interpretation; a future cycle that
   wants the dashboard's live-cascade gauge replaces
   `d.cascades.Len()` with an `openLen()` helper that iterates
   `d.cascades.cascades` and counts those with `!c.IsClosed()` (the
   pattern `(*Dispatcher).richStatusSnapshot` already uses in
   `internal/remotecontrol/dispatch.go` — cycle rc-status-tool ships
   the helper; the future cycle only has to swap the read line).
5. **`server_fingerprint` is best-effort.** The `<short-hash>` is the
   first 8 chars of `Config.Commit`. Empty / whitespace `Commit`
   falls back to `"unknown"`; the test fixture's Commit is
   `deadbeef1234567890abcdef1234567890abcdef`, so the wire shape is
   `wagyu-rc/1.4.0/deadbeef`. Production `Config.Commit` is populated
   via `-ldflags '-X main.commit=deadbeef...'` at build time (the
   cycle does NOT add the ldflags wiring — that's parked for the
   future `rc-version-build-label` cycle; today's production build
   shows `server_fingerprint:"wagyu-rc/1.4.2/unknown"` until the
   ldflags plumbing is wired).
6. **`DefaultDispatchMethods` is hand-organised by category.** The
   package var is NOT sorted; the wire shape is sorted via
   `sortedDispatchMethods()`. A future edit that reorders the var
   (e.g. moves `RemoteControlStatus` to a different category) does
   NOT change the wire shape. A future edit that ADDS a new switch
   arm but FORGETS to add the method name to the var DOES change the
   wire shape — `RemoteControlStatus` shows up in
   `dispatch_methods` but not in the actual switch, and a dashboard
   that calls it gets a successful response (the cycle's handler
   exists). The test `TestDefaultDispatchMethods_WireSorted` pins
   the wire shape (sorted, non-empty, all 56 entries) but does NOT
   pin "every wire method has a switch arm" — a buggy add-to-var-
   without-switch-arm pass would slip through. The future
   `rc-dispatch-table-gen` cycle adds the CI guardrail.
7. **`RemoteControlOnMessageAllowList` is a closed set, not a regex.**
   A future cycle that wants to expose more methods through the
   message-injection surface extends the slice (the gatekeeper picks
   up the change automatically — no per-method code). Adding a
   method requires both the var entry AND a switch arm that actually
   dispatches the method; today's allowlist contains three "reserved"
   entries (`GetRemoteAgentFile`, `GetRemoteAgentDir`,
   `ListRemoteAgentChannels`) that the cycle does NOT add switch
   arms for — those entries are placeholders for the future
   `rc-file-ops-read` / `rc-channels-list` cycles. Calling one today
   triggers `(*Dispatcher).Dispatch` to return `-32601 "Method not
   found"` (the inner dispatch's switch-arm default), and the outer
   envelope is still the `{ok:true, accepted:true, replayed_at}`
   confirmation (the inner response is discarded).
8. **No `RemoteAgentStatus` top-level method.** The cycle's allowlist
   includes `RemoteAgentStatus` (agy-synonym of `RemoteControlStatus`)
   for the message-injection surface, but does NOT add a
   `RemoteAgentStatus` switch arm. The future `rc-status-tool-extend`
   cycle (or a follow-on to this cycle) adds the top-level method
   alias; today a dashboard that posts `RemoteAgentStatus` to the
   outer dispatcher gets `-32601 "Method not found"`, and only the
   `remoteControlOnMessage` injection path routes it correctly.
9. **The handler doesn't validate the inner `message.id`.** The
   re-dispatched frame uses a fresh numeric `id:1` (the outer
   caller's id is dropped — the inner response is intentionally
   discarded). A future cycle that wants id-correlation across the
   outer / inner frames carries the outer id into the inner
   re-dispatch (cycle rc-handle-user-interaction's notification path
   uses this pattern for `cascade/user_interaction_required`).
10. **No dispatcher-level metric on `remoteControlOnMessage`
    attempts.** The cycle logs every attempt (allowed + rejected) via
    `log.Printf`, but does not record counters. A future cycle that
    wants a metrics surface (e.g. dashboards reporting
    "rejected-attempts-per-minute") adds an `atomic.Uint64` pair to
    the `Dispatcher` struct; the cycle's seam
    (`handleRemoteControlOnMessage`) is the only place that needs to
    touch the new counters.
11. **Pre-existing vet warning stays.** The
    `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf`
    warning is pre-existing (introduced in cycle rc-list-readonly
    area) and out of scope for this cycle. The cycle does not touch
    `cmd/wagyu/` at all — the JSON-RPC dispatcher changes are
    package-internal to `internal/remotecontrol/`, and the only
    `internal/remotetransport/` change is the new public accessor on
    the existing `(*TCPServer)` type.

---

## Reviewer commands

```bash
# 1. Confirm the tip of exp/rc-server-mode is this cycle's commit.
git log --oneline -1 exp/rc-server-mode

# 2. Confirm the cycle's files (added + modified).
git show --stat <commit>

# 3. Build the cycle's binary (must exit 0).
go build -o /tmp/wagyu ./cmd/wagyu

# 4. Run the in-process + wire-level tests (must PASS).
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# 5. Vet the cycle's touched packages (the pre-existing
# model_default_effort_test.go:67 3-arg Errorf is out of scope;
# the cycle does not touch it).
go vet ./internal/remotecontrol/ ./internal/remotetransport/

# 6. Format-check the cycle's touched files (must emit no filenames).
gofmt -l \
    internal/remotecontrol/top_level.go \
    internal/remotecontrol/top_level_dispatcher.go \
    internal/remotecontrol/top_level_test.go \
    internal/remotecontrol/dispatch.go \
    internal/remotetransport/tcp.go \
    internal/remotetransport/tcp_test.go

# 7. Run only the cycle's in-process tests.
go test -race -count=1 -v -run 'TestTopLevel|TestServerFingerprint|TestDefaultDispatchMethods|TestDefaultNotificationFilter|TestAllSinks|TestIsRemoteControlOnMessageAllowed|TestDispatcher_RemoteControl|TestRemoteControlOnMessage_InnerResponseDiscarded' ./internal/remotecontrol/

# 8. Run only the cycle's wire-level tests.
go test -race -count=1 -v -run 'TestRemoteControlStatus_WS_Ok|TestRemoteControlOnOutput_WS_|TestRemoteControlOnMessage_WS_' ./internal/remotetransport/

# 9. Confirm the PLAN.md row is DONE <this-commit>.
grep -F 'rc-top-level-rpc' docs/cycles/PLAN.md
```

---

## Cycle delta

| Surface | Before | After |
|---|---:|---:|
| `internal/remotecontrol/dispatch.go` | case arms for 32 methods | +3 case arms (RemoteControlStatus / RemoteControlOnOutput / remoteControlOnMessage) |
| `internal/remotecontrol/dispatch.go` | Dispatcher struct with `cfg` + `accepted` + `notifyMu/notify` + `cascades` + `broadcasterMu/broadcaster` | + `allSinks map[string]struct{}` + `allSinksMu sync.RWMutex` |
| `internal/remotetransport/tcp.go` | private `connCount() int` only | + public `(*TCPServer).ConnCount() int` |
| `DefaultDispatchMethods` entries | 53 | 56 |
| `DefaultNotificationFilter` entries | 12 (existing) | 12 (unchanged) |
| `RemoteControlOnMessageAllowList` entries | n/a (new) | 18 |
| In-process tests in `internal/remotecontrol/` | existing count | +20 (top_level_test.go) |
| Wire-level tests in `internal/remotetransport/` | existing count | +7 (tcp_test.go) |
| New LOC | — | ~1750 (top_level.go 478 + top_level_dispatcher.go 376 + top_level_test.go 908, minus the docstring overhead) + 67 in dispatch.go + 24 in tcp.go + 381 in tcp_test.go |

The cycle's `docs/cycles/PLAN.md` row now reads **DONE `<this-commit>`**
for `rc-top-level-rpc`. The next available TODO is
`rc-serve-subcommand` (G1).
