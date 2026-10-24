# Cycle `rc-terminals` — `RemoteAgentTerminal*` five JSON-RPC methods + `terminal/output` stream notification

## TL;DR / 摘要

Cycles 1–26 of `exp/rc-server-mode` shipped 26 JSON-RPC methods. The
re-investigation matrix still flags `rc-terminals` as **TODO**: a
terminal-style cascade IO abstraction. This cycle ships five JSON-RPC
methods + one stream notification:

- `CreateRemoteAgentTerminal` — mints a fresh `tml_<16 hex>` terminal
  id, optionally bound to a `cascade_id`, with `cols` / `rows`
  defaults of 80 / 24;
- `ListRemoteAgentTerminals` — returns the in-memory terminals list,
  filtered by optional `cascade_id`;
- `SendRemoteAgentTerminalInput` — pushes base64-encoded UTF-8 bytes
  into a terminal; broadcasts `terminal/output` to every open ws conn;
- `ResizeRemoteAgentTerminal` — updates cols / rows on an open
  terminal; no broadcast;
- `CloseRemoteAgentTerminal` — marks a terminal closed; broadcasts
  `terminal/closed` to every open ws conn;
- `terminal/output` — server-push notification carrying
  `{terminal_id, seq:0, data:<base64>, ts}`; emitted on every
  successful Send AND every cascade-output chunk the terminal is
  attached to;
- `terminal/closed` — server-push notification carrying
  `{terminal_id, closed_at, ts}`; emitted on every successful Close.

The cycle is **persistence-only in spirit** — there is no
`$ANTIGRAVITY_HOME/terminals.json`; the registry is in-memory and
survives one daemon lifetime. The brief pins the data model without
an on-disk file. Documented in the **硬約束** section.

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `CreateRemoteAgentTerminal` | write | `{cascade_id?, cols?, rows?, kind:<output\|input>}` | `{ok:true, terminal_id, kind, cols, rows, created_at}` | `-32602` (closed-set `kind`; negative cols/rows; unknown cascade_id; non-string fields), `-32004` (unknown `cascade_id`), `-32603` (no registry) |
| `ListRemoteAgentTerminals` | read | `{cascade_id?}` | `{ok:true, terminals:[{terminal_id, cascade_id?, kind, cols, rows, created_at, closed_at?}, …]}` | `-32603` (no registry) |
| `SendRemoteAgentTerminalInput` | write | `{terminal_id, data:<base64>}` | `{ok:true, terminal_id, bytes_consumed}` + `terminal/output` broadcast | `-32602` (empty / whitespace / non-string id; empty data; not base64), `-32018` (unknown `terminal_id`), `-32019` (terminal closed), `-32602` (kind=output → `ErrTerminalInputRejected`), `-32603` (no registry) |
| `ResizeRemoteAgentTerminal` | write | `{terminal_id, cols, rows}` | `{ok:true, terminal_id, cols, rows}` | `-32602` (missing / non-string id; negative cols / rows), `-32018` (unknown `terminal_id`), `-32019` (terminal closed), `-32603` (no registry) |
| `CloseRemoteAgentTerminal` | write | `{terminal_id}` | `{ok:true, terminal_id, closed_at}` + `terminal/closed` broadcast | `-32602` (missing / non-string id), `-32018` (unknown `terminal_id`), `-32019` (already closed), `-32603` (no registry) |

Every successful `SendRemoteAgentTerminalInput` emits a
**`terminal/output`** notification to **every** open ws connection;
every successful `CloseRemoteAgentTerminal` emits a
**`terminal/closed`** notification. The same broadcast-to-all seam
`rc-permissions-grants` introduced (and `rc-plugins` /
`rc-triggers` / `rc-fastpush-toggle` / `rc-resources` /
`rc-conversation-config` reused).

| Piece | Where |
|---|---|
| `TerminalKind` closed-set (`output` / `input`) + `AllTerminalKinds` + `IsValidTerminalKind` + `DefaultTerminalCols` (=80) / `DefaultTerminalRows` (=24) + `TerminalEventBufferCap` (=64) + `NotifTerminalOutputMethod` (=`"terminal/output"`) + `NotifTerminalClosedMethod` (=`"terminal/closed"`) + `TerminalStateVersion` (=1) + `Terminal` per-row record (`terminal_id`, `cascade_id?`, `kind`, `cols`, `rows`, `created_at`, `closed_at?`) + `TerminalState` on-disk envelope (parked — no persistence today) + `TerminalEvent` per-subscriber delivery shape (`Kind` / `Data` / `Seq`) + `TerminalEventKind` closed-set (`output` / `close`) + sentinels `ErrTerminalNotFound` / `ErrTerminalClosed` / `ErrTerminalInputRejected` + `terminalSeq` per-terminal monotonic counter (atomic, lock-free) | `internal/remotecontrol/terminals.go` (new, ~320 lines) |
| `TerminalsRegistry` (in-memory registry; per-terminal `*terminalState` with its own mutex; per-terminal `subscriber` chans cap 64 non-blocking + drop counter; `terms map` guarded by RWMutex; `clock` injectable via `NewTerminalsRegistryWithNow`); methods `Create` (mints `tml_<hex>`, applies 80/24 defaults when zero) / `Get` / `List` (sorted by CreatedAt asc, terminal_id tiebreaker) / `Send` (returns `len(data)`; rejects output-kind via `ErrTerminalInputRejected`) / `Resize` (positive dims only; zero = no-op; negative rejected by dispatcher) / `Close` (idempotent; emits `TerminalEventClose` + closes subscriber chans) / `Subscribe` (returns chan cap 64 + idempotent unsubscribe func) / `AttachCascade` (idempotent; spawns per-terminal drain goroutine); helpers `drainCascadeOutput` / `forwardCascadeChunk` / `mintTerminalID` (`tml_` + 8 random bytes hex) / `sortTerminalsByCreatedAt` (insertion sort; n is small) / `normalizeTerminalID` / `trimSpaceASCII`; nil-safe at the receiver level (a nil `TerminalsRegistry` returns `ErrTerminalNotFound` for any operation, matching the cycle precedent for "no store installed" sentinels) | `internal/remotecontrol/terminals_registry.go` (new, ~810 lines) |
| `TerminalsRegistry` field on `Config` + `HostConfig` (nil-fabrication in `NewDispatcher` mirrors `ConversationConfigStore` / `PermissionsStore` / `PluginsStore` / `TriggersStore` / `FastpushStore` / `ResourceStore`); propagation through `StartHostSession` | `internal/remotecontrol/daemon.go` (+~25 / -0), `internal/remotecontrol/host.go` (+~25 / -0) |
| `(*Cascade).terminalOutput chan []byte` (lazy via `sync.Once`, capacity 32) + `TerminalOutput() <-chan []byte` accessor + `AppendTerminalOutput(chunk)` write seam (drop-on-full; future `rc-pty-bridge` writes here from a real pty driver); doc-comment on `Cascade` struct updated | `internal/remotecontrol/cascade.go` (+~75 / -0) |
| `case "CreateRemoteAgentTerminal" / "ListRemoteAgentTerminals" / "SendRemoteAgentTerminalInput" / "ResizeRemoteAgentTerminal" / "CloseRemoteAgentTerminal"` switch arm + `NewDispatcher` nil-fabrication; package-level comment on the `Dispatch` switch updated to document the rc-terminals cycle (five methods + cross-conn broadcaster seam + sentinel map) | `internal/remotecontrol/dispatch.go` (+~60 / -0) |
| `createRemoteAgentTerminalParams` / `sendRemoteAgentTerminalInputParams` / `resizeRemoteAgentTerminalParams` / `closeRemoteAgentTerminalParams` envelope types; `handleCreateRemoteAgentTerminal` (cascade-id existence check via `(*Dispatcher).cascades.Get`; kind closed-set; cols/rows non-negative; attach cascade drain on success) / `handleListRemoteAgentTerminals` (optional cascade filter; `terminalToWire` per-row projection) / `handleSendRemoteAgentTerminalInput` (terminal_id required non-empty; data required non-empty base64; bytes_consumed mirrors decoded length; broadcasts `terminal/output` BEFORE response marshal) / `handleResizeRemoteAgentTerminal` (terminal_id required non-empty; cols/rows non-negative; echoes post-state) / `handleCloseRemoteAgentTerminal` (terminal_id required non-empty; broadcasts `terminal/closed` BEFORE response marshal); helpers `broadcastTerminalOutput` / `broadcastTerminalClosed` (routed via `(*Dispatcher).BroadcastNotify` — same seam `rc-permissions-grants` added; nil broadcaster = no-op) / `mapTerminalsError` (sentinel → JSON-RPC envelope) / `terminalToWire` (per-row wire projection; snake_case keys; omits `cascade_id` when empty; omits `closed_at` when zero) / `closedSetList` ("must be one of X\|Y\|Z") | `internal/remotecontrol/terminals_dispatcher.go` (new, ~660 lines) |
| `buildHostTerminalsRegistry()` process-singleton helper (sync.Once + package-level pointer memoised) at the bottom of `cmd/wagyu/remote_control.go`; wired into Unix-socket (`maybeStartRemoteControlDaemon`), TCP+WS per-turn (`maybeStartRemoteControlTCPDaemon`), and foreground TCP+WS (`runRemoteControlForeground`) paths. The singleton means a terminal the dashboard creates against the per-turn `--remote-control` unix socket is visible on the `--remote-control-tcp` LaunchAgent transport without any restart-cycle bookkeeping — the same shape as `buildHostMessageBackend` / `buildHostProjectStore`. | `cmd/wagyu/remote_control.go` (+~50 / -0), `cmd/wagyu/remote_control_foreground.go` (+~10 / -0) |
| In-process tests: `TestAllTerminalKinds` (canonical-order pin) + `TestIsValidTerminalKind` × 7 sub-tests (every closed-set value + every non-match) + `TestNotifTerminalOutputMethod` (exact literal `"terminal/output"`) + `TestNotifTerminalClosedMethod` (exact literal `"terminal/closed"`) + `TestDefaultTerminalDimensions` (=80/24) + `TestTerminalEventBufferCap` (=64) + 11 `TestTerminalsRegistry_*` (`_Create_Ok` / `_Create_DefaultsForZeroDims` / `_List_All` / `_List_FilterByCascade` / `_Send_AfterClose_ReturnsError` / `_Send_UnknownID_ReturnsError` / `_Send_OnOutputKind` / `_Resize_Ok` / `_Close_Ok` / `_Close_AlreadyClosed_ReturnsError` / `_Close_UnknownID_ReturnsError` / `_Subscribe_ReceivesEvents` / `_Unsubscribe_StopsEvents`) + 13 `TestCreateRemoteAgentTerminal_*` / `TestListRemoteAgentTerminals_*` / `TestSendRemoteAgentTerminalInput_*` / `TestResizeRemoteAgentTerminal_*` / `TestCloseRemoteAgentTerminal_*` dispatcher round-trips (happy paths + closed-set / bad-base64 / empty-data / negative-dims / unknown-id / closed-terminal / no-store paths) + `TestTerminalOutput_StreamsFromCascade` (cycle's headline integration — Create → cascade → Spawn goroutine writing `AppendTerminalOutput` chunks → assert terminal subscriber receives `terminal/output` events with the expected data + seq) + `TestMapTerminalsError` (sentinel → code mapping) — 36 in-process tests, all PASS with `-race` | `internal/remotecontrol/terminals_test.go` (new, ~960 lines) |
| Wire-level WS round-trips + `newTestTCPServerWithTerminals` / `assertTerminalOutputNotification` / `assertTerminalClosedNotification` helpers (8 tests: `TestCreateRemoteAgentTerminal_WS_Ok` (flat envelope + tml_<hex> prefix + server-stamped created_at) / `TestListRemoteAgentTerminals_WS_Ok` (Create 3 → List returns 3-element array) / `TestSendRemoteAgentTerminalInput_WS_Ok` (two ws conns use `waitForTwoConnIDs`; Send on c1 broadcasts `terminal/output` to both c1 AND c2; c1 sees `[notification, response]`; c2 sees only `[notification]`; `bytes_consumed` mirrors the decoded length) / `TestSendRemoteAgentTerminalInput_WS_TerminalNotFound` (-32018) / `TestSendRemoteAgentTerminalInput_WS_BadParams_NotBase64` (-32602 with verbatim `message:"Invalid params"`) / `TestResizeRemoteAgentTerminal_WS_Ok` (echoes post-resize dims) / `TestCloseRemoteAgentTerminal_WS_Ok` (Close on c1 broadcasts `terminal/closed` to both c1 AND c2; envelope carries `closed_at`) / `TestCloseRemoteAgentTerminal_WS_NotFound` (-32018)) | `internal/remotetransport/tcp_test.go` (+~480 / -0) |

What lands for forward consumers:

- A `TerminalsRegistry` seam the dispatcher carries, ready for the future
  `rc-terminals-bridge` cycle (real `github.com/creack/pty` driver) to plug
  into without re-touching the dispatcher;
- A `(*Cascade).TerminalOutput()` accessor + `AppendTerminalOutput(chunk)`
  write seam the future `rc-pty-bridge` cycle writes to (today's
  `StubBackend` / `PiG` streaming path doesn't actually write here —
  the per-cycle `TestTerminalOutput_StreamsFromCascade` test wires a
  custom goroutine that exercises the seam);
- A `(*TerminalsRegistry).AttachCascade(terminalID, ch)` method the
  future cycle reuses without re-touching the registry surface;
- A `mapTerminalsError` sentinel-error mapper shared with every
  per-method handler — future cycle methods that add new sentinels
  (`ErrTerminalNotImplemented` etc.) extend the helper without
  re-touching the dispatcher;
- A `terminalToWire` per-row wire projection that the future
  `rc-terminals-persist` cycle's `List` path reuses without re-touching
  the dispatcher;
- A process-singleton `buildHostTerminalsRegistry()` helper (mirrors
  `buildHostMessageBackend` / `buildHostProjectStore`) that the future
  cycle's host wiring reuses so a single shared registry serves every
  transport;
- Reuse of the `BroadcastNotify` / `SetBroadcaster` callback seam from
  `rc-permissions-grants` (shared with `rc-plugins` / `rc-triggers` /
  `rc-fastpush-toggle` / `rc-resources` / `rc-conversation-config`) —
  the cycle's `terminal/output` and `terminal/closed` notifications
  share the same plumbing.

---

## JSON-RPC surface (precise)

### `CreateRemoteAgentTerminal`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "CreateRemoteAgentTerminal", "params": { "kind": "input", "cols": 132, "rows": 50 } }
```

`cascade_id` is **optional**; absent / empty means "no cascade binding"
(the terminal is unattached, a host-level log tail). Non-string
fields are caught by `nonStringFieldInJSON` so a numeric `cascade_id`
doesn't silently default-walk to `""`. Non-empty `cascade_id` triggers
a `(*Dispatcher).cascades.Get(cascadeID)` existence check; an unknown
cascade_id lands as `-32004 "Cascade not found"` (parity with
`SendRemoteAgentCascadeMessage` so dashboards' error filters are
uniform).

`cols` / `rows` are **optional positive integers**. Zero or absent →
registry applies the brief-pinned `80` / `24` default. Negative →
`-32602 "cols must be >= 0"` / `"rows must be >= 0"` so a buggy
dashboard gets a clear error rather than a silent 80 / 24 default.

`kind` is **required, closed-set** `{"output", "input"}`. Empty /
unknown → `-32602` with verbatim
`"kind must be one of output|input"`. The closed-set is pinned by
`AllTerminalKinds` / `IsValidTerminalKind` so a future cycle that
extends the set updates both the validator and the doc-comment in
lockstep.

Response (success):

```json
{
  "ok":          true,
  "terminal_id": "tml_3f7a1b2c4d5e6f70",
  "kind":        "input",
  "cols":        132,
  "rows":        50,
  "created_at":  "2026-10-19T12:34:56.789012345Z"
}
```

`created_at` is server-stamped (UTC, RFC3339Nano). `terminal_id` is
`tml_` + 8 random bytes hex-encoded = 16 hex chars (matches the cycle-5
`cas_<hex>` / `conv_<hex>` / `tml_<hex>` prefix convention).

If `cascade_id` was supplied and the cascade exists, the dispatcher
calls `(*TerminalsRegistry).AttachCascade(t.TerminalID, c.TerminalOutput())`
AFTER the registry's Create succeeds — the drain forwards every
cascade-output chunk to the terminal's subscribers as a
`TerminalEventOutput` (which the per-cycle integration test
`TestTerminalOutput_StreamsFromCascade` exercises end-to-end).

No broadcast on Create — the brief pins Create as the moment a
terminal "appears", and subscribers only learn about it via the
`terminal/output` stream once they explicitly Subscribe, or via a
polling `ListRemoteAgentTerminals`.

### `ListRemoteAgentTerminals`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "ListRemoteAgentTerminals", "params": { "cascade_id": "cas_abc" } }
```

`cascade_id` is **optional**. Empty / absent → every terminal (the
no-filter branch). Non-empty → terminal rows with a matching
`cascade_id`. Unknown `cascade_id` → empty list (the brief's
"filter is best-effort" semantic — matches `rc-triggers` /
`rc-resources` precedent).

Response (success):

```json
{
  "ok": true,
  "terminals": [
    {
      "terminal_id": "tml_3f7a1b2c4d5e6f70",
      "cascade_id":  "cas_abc",
      "kind":        "input",
      "cols":        132,
      "rows":        50,
      "created_at":  "2026-10-19T12:34:56.789012345Z",
      "closed_at":   "2026-10-19T12:35:00.123456789Z"
    }
  ]
}
```

`terminals` is a non-nil array (sorted by `created_at` ascending,
`terminal_id` tiebreaker — matches `rc-fastpush-toggle` /
`rc-triggers` / `rc-resources` precedent). `cascade_id` is omitted
when empty (the brief's "absent means no cascade binding" semantic);
`closed_at` is omitted when zero (the terminal is still open). No
broadcast.

### `SendRemoteAgentTerminalInput`

Request:

```json
{ "jsonrpc": "2.0", "id": 2, "method": "SendRemoteAgentTerminalInput", "params": { "terminal_id": "tml_3f7a1b2c4d5e6f70", "data": "aGVsbG8gdGVybWluYWw=" } }
```

`terminal_id` is **required, non-empty string**. Whitespace-trimmed
before lookup. Empty / non-string → `-32602 "terminal_id is required
and must be non-empty"`.

`data` is **required, non-empty base64-encoded string**. The dispatcher
`base64.StdEncoding.DecodeString`s it; a malformed value lands as
`-32602 "data must be base64"` (clear error rather than silent
`-32603`). Empty data string → `-32602 "data is required and must be
non-empty"` so a dashboard that posts `data: ""` doesn't get a
silent `bytes_consumed: 0` success.

Response (success, post-broadcast):

```json
{ "ok": true, "terminal_id": "tml_3f7a1b2c4d5e6f70", "bytes_consumed": 13 }
```

`bytes_consumed` mirrors `len(base64-decoded data)` (so
`base64("hello terminal") = "aGVsbG8gdGVybWluYWw="` →
`bytes_consumed: 13`).

The originating conn's outbound channel holds
`[notification, response]` — `BroadcastNotify` is called BEFORE the
handler returns the response, so the originating conn sees the
`terminal/output` notification first and the response envelope last;
other ws conns see only the `terminal/output` notification.

Sentinel map:

| Sentinel | Code | Trigger |
|---|---|---|
| `ErrTerminalNotFound` | `-32018` | `terminal_id` is unknown to the registry |
| `ErrTerminalClosed` | `-32019` | `Close` has already fired for this terminal |
| `ErrTerminalInputRejected` | `-32602` | `kind: "output"` (read-only consumer) |

### `ResizeRemoteAgentTerminal`

Request:

```json
{ "jsonrpc": "2.0", "id": 3, "method": "ResizeRemoteAgentTerminal", "params": { "terminal_id": "tml_3f7a1b2c4d5e6f70", "cols": 200, "rows": 60 } }
```

`terminal_id` is **required, non-empty string**. `cols` / `rows` are
**required non-negative integers**. The dispatcher rejects negative
values explicitly with `-32602 "cols must be >= 0"` /
`"rows must be >= 0"`; the registry treats zero as a no-op
("leave the dim alone"). Both zero is also a no-op success (the
registry treats it as "leave both alone" rather than "force 0/0").

Response (success):

```json
{ "ok": true, "terminal_id": "tml_3f7a1b2c4d5e6f70", "cols": 200, "rows": 60 }
```

`cols` / `rows` echo the **post-resize** state (the dims the registry
actually stores, which match what the next `List` will return). A
buggy dispatcher that echoed the input dims would silently desync the
dashboard's view; the in-process `TestResizeRemoteAgentTerminal_Ok`
+ wire `TestResizeRemoteAgentTerminal_WS_Ok` both pin the
post-state verbatim.

No broadcast on Resize — the brief pins Resize as a no-broadcast
mutation; the cascade/terminal state stream already has the
updated dims visible via the next `List` poll.

### `CloseRemoteAgentTerminal`

Request:

```json
{ "jsonrpc": "2.0", "id": 4, "method": "CloseRemoteAgentTerminal", "params": { "terminal_id": "tml_3f7a1b2c4d5e6f70" } }
```

`terminal_id` is **required, non-empty string**. Whitespace-trimmed
before lookup.

Response (success, post-broadcast):

```json
{ "ok": true, "terminal_id": "tml_3f7a1b2c4d5e6f70", "closed_at": "2026-10-19T12:35:00.123456789Z" }
```

`closed_at` is server-stamped at close time (UTC, RFC3339Nano).

Idempotent semantics: a second Close on an already-closed terminal
returns `-32019 "Terminal not found"` is for unknown id; for
already-closed it's `-32019 "Terminal closed"` (matches the Send
path's closed-terminal envelope so dashboards' retry/fallback
treats them uniformly). The originating conn's outbound channel
holds `[notification, response]`; other ws conns see only the
`terminal/closed` notification.

---

## Broadcast semantics

A successful `SendRemoteAgentTerminalInput` emits a `terminal/output`
notification to **every** open ws connection via
`(*Dispatcher).BroadcastNotify(method, params)`:

```json
{
  "jsonrpc": "2.0",
  "method":  "terminal/output",
  "params":  {
    "terminal_id": "tml_3f7a1b2c4d5e6f70",
    "seq":         0,
    "data":        "aGVsbG8gdGVybWluYWw=",
    "ts":          "2026-10-19T12:34:56.789012345Z"
  }
}
```

The notification's params shape carries the base64-encoded bytes
(same encoding as the `data` field the originating dashboard sent via
Send). `ts` is server-stamped at broadcast time. `seq` is the
per-terminal monotonic sequence (the cycle pins `seq: 0` for the
first frame; the future `rc-terminals-backpressure` cycle surfaces a
metrics endpoint with drop counts so subscribers can detect gaps).

A successful `CloseRemoteAgentTerminal` emits a `terminal/closed`
notification to every open ws connection:

```json
{
  "jsonrpc": "2.0",
  "method":  "terminal/closed",
  "params":  {
    "terminal_id": "tml_3f7a1b2c4d5e6f70",
    "closed_at":   "2026-10-19T12:35:00.123456789Z",
    "ts":          "2026-10-19T12:35:00.123456789Z"
  }
}
```

`closed_at` matches the value the response envelope carries; `ts` is
the broadcaster's stamp (server clock).

Both notifications are delivered without an `"id"` field (per
JSON-RPC 2.0 §4.1 notification rules). The broadcaster is `nil` for
in-process dispatchers and the Unix-socket transport (request /
response only); the broadcast becomes a no-op there. The mutation
still lands in the registry; the response envelope echoes the
post-state so polling is always possible. The
`(*TCPServer)` registered broadcaster (set at `NewTCPServer` time)
fans the frame to every open ws connection. This is the same
broadcast-to-all seam `rc-permissions-grants` introduced for
`permissions/changed`, `rc-plugins` reused for `plugins/changed`,
`rc-triggers` reused for `triggers/changed`, `rc-fastpush-toggle`
reused for `fastpush/setting_changed`, `rc-resources` reused for
`resources/changed`, `rc-conversation-config` reused for
`conversation_config/changed`.

---

## Cascade integration (the cycle's headline)

A terminal bound to a cascade (`cascade_id` non-empty on Create)
forwards every cascade-output chunk to the terminal's subscribers
via the per-terminal drain goroutine `(*TerminalsRegistry).AttachCascade`
spawns. The data path:

1. The cascade's streaming backend writes text-delta chunks to
   `(*Cascade).TerminalOutput()` (the lazy-allocated buffered chan
   with capacity 32);
2. `AppendTerminalOutput(chunk)` is the write seam; a future
   `rc-pty-bridge` cycle that wires a real `github.com/creack/pty`
   driver calls this from the pty driver's read goroutine;
3. `(*TerminalsRegistry).AttachCascade(terminalID, ch)` spawns a
   per-(terminal, cascade) drain goroutine that reads `ch` until
   close, forwarding each chunk as a `TerminalEventOutput` to every
   terminal subscriber;
4. The dispatcher's `SendRemoteAgentTerminalInput` path ALSO emits
   a `terminal/output` notification for the bytes a Send caller
   just submitted, so the cross-ws fan-out sees them.

The cycle's headline integration test
`TestTerminalOutput_StreamsFromCascade` exercises this path
end-to-end:

```go
// TestTerminalOutput_StreamsFromCascade pins the
// cascade-output → terminal-subscriber flow.
```

A future `rc-pty-bridge` cycle consumes the same seams without
re-touching this cycle's code — `(*Cascade).TerminalOutput()` is the
public read accessor, `AppendTerminalOutput(chunk)` is the public
write seam, and `(*TerminalsRegistry).AttachCascade(terminalID, ch)`
is the per-terminal drain spawner.

---

## Forward compatibility with `rc-terminals-bridge`

The cycle's terminal surface is the foundation the future
`rc-terminals-bridge` cycle plugs a real pty driver into. The seam
is deliberately narrow today (one accessor + one write seam + one
attach helper) so the future cycle can extend it without breaking
existing dashboards.

What the future cycle is expected to add (out of scope here):

- A `github.com/creack/pty` driver that reads from a real PTY master
  fd and writes via `(*Cascade).AppendTerminalOutput(chunk)` on every
  byte; the cycle's `AppendTerminalOutput` is the seam.
- A `(*Dispatcher).ptyOutputPump` goroutine that wires the pty
  driver's read goroutine to `(*Cascade).AppendTerminalOutput` (the
  cycle's `cascade.go` has the lazy `terminalOutput chan []byte` +
  `terminalOutputOnce sync.Once` already in place — the future cycle
  only has to wire the producer side, not extend the cascade struct).
- A `(*Dispatcher).ptyInputPump` goroutine that consumes the
  terminal/output path's `TerminalEventOutput` frames and forwards
  bytes to the pty driver's write fd (the cycle's
  `Subscribe` / `AttachCascade` paths already deliver the
  TerminalEvent frames to whoever is listening; the future cycle
  only has to subscribe on behalf of the pty driver).

The cycle ships:

- `(*TerminalsRegistry).AttachCascade(terminalID, ch)` — the
  per-terminal drain spawner the future cycle's pty-output path
  reuses without re-touching the registry;
- `(*TerminalsRegistry).Subscribe(terminalID)` — the
  per-subscriber delivery shape the future cycle's pty-input path
  consumes without re-touching the registry;
- `TerminalEvent` (Kind / Data / Seq) — the per-subscriber
  delivery shape the future cycle's pty-input path consumes
  verbatim;
- `NotifTerminalOutputMethod` constant — the cycle's notification
  method name the future cycle's pty-input path emits (no new
  method name is required; the pty-input path reuses the same
  `terminal/output` channel);
- `NotifTerminalClosedMethod` constant — the close notification the
  future cycle's pty teardown path emits (no new method name is
  required);
- `DefaultTerminalCols` / `DefaultTerminalRows` / `TerminalEventBufferCap` —
  the brief-pinned constants the future cycle's pty `Winsize`
  watcher uses to seed the initial PTY size.

The cycle's doc-comments on `terminals.go` / `terminals_registry.go`
flag every extension point the future cycle will touch, so the
auditor can grep the cycle boundary.

---

## Forward compatibility with `rc-terminals-persist`

The cycle's Terminal / TerminalState types are the foundation the
future `rc-terminals-persist` cycle plugs an on-disk file store
into. The cycle doesn't write to disk today (it's
persistence-only-in-spirit), but the types and the
`TerminalState` envelope are deliberately compatible with the
`FSConversationConfigStore` / `FSPermissionStore` / `FSPluginStore`
/ `FSTriggerStore` / `FSFastpushStore` / `FSResourceStore` style:

- `TerminalState` mirrors `ConversationConfigState` /
  `PermissionsState` / `PluginState` / `TriggerState` /
  `FastpushState` / `ResourceState` shape (`version: 1` /
  `terminals: []Terminal` / `updated_at`);
- `TerminalStateVersion` = 1 mirrors the `ConversationConfigStateVersion`
  / `PermissionStateVersion` / `PluginStateVersion` /
  `TriggerStateVersion` / `FastpushStateVersion` /
  `ResourceStateVersion` convention;
- `Terminal.TerminalID` (server-minted `tml_<hex>`) mirrors the
  `Permission.GrantID` / `Plugin.PluginID` / `Trigger.TriggerID`
  server-mint pattern.

The future cycle only has to write an `FSTerminalsStore` rooted at
`$ANTIGRAVITY_HOME/terminals.json` with the same atomic
temp + rename + dir fsync + 0o600 perms + 0o700 dir style — the
registry's existing `Create` / `List` / `Get` / `Send` / `Resize` /
`Close` interface is forward-compatible with persistence.

---

## Hard constraints / 硬約束

What the cycle **did not** ship, by deliberate scope decision:

1. **No on-disk persistence.** The cycle is persistence-only in
   spirit — there is no `$ANTIGRAVITY_HOME/terminals.json`. The
   registry is in-memory and survives one daemon lifetime. A
   future `rc-terminals-persist` cycle adds an `FSTerminalsStore`
   mirroring the `FSConversationConfigStore` / `FSPermissionStore`
   / `FSPluginStore` / `FSTriggerStore` / `FSFastpushStore` /
   `FSResourceStore` style.
2. **No real PTY driver.** The cycle's `AppendTerminalOutput` /
   `TerminalOutput` seams are ready, but no `github.com/creack/pty`
   driver is wired. The future `rc-terminals-bridge` cycle adds the
   pty master-fd reader + writer + Winsize watcher that consumes
   the cycle's seams.
3. **No `GetRemoteAgentTerminal` JSON-RPC method.** The cycle ships
   `List` (returns every terminal or the cascade-filtered subset)
   and a `Terminal` record embedded in the list array — there is no
   dedicated Get-by-id method today. The future `rc-terminals-read`
   cycle adds `GetRemoteAgentTerminal(terminal_id)` so dashboards
   can fetch a single row without iterating. Today's read path:
   the response envelopes echo the post-state (so polling the
   response is always possible); the `terminal/output` +
   `terminal/closed` broadcasts carry the same envelope so
   subscribers see the same shape.
4. **No `DeleteRemoteAgentTerminal` JSON-RPC method.** The cycle
   ships `Close` (marks closed + emits `terminal/closed` + frees
   subscribers) — there is no separate Delete because Close is the
   destructive endpoint. The future `rc-terminals-persist` cycle
   adds a per-row GC step (closed rows expire from the on-disk
   file after N hours) without re-touching the dispatcher.
5. **No `SubscribeRemoteAgentTerminal` JSON-RPC method.** The cycle
   uses the in-process `(*TerminalsRegistry).Subscribe` accessor for
   per-subscriber delivery; there is no JSON-RPC method that
   subscribes because the cross-ws fan-out already happens via the
   `terminal/output` notification. Dashboards that want per-terminal
   events receive them on the broadcast channel. The future
   `rc-terminals-read` cycle adds `SubscribeRemoteAgentTerminal`
   only if a dashboard wants per-conn rather than per-broadcast
   delivery (today the broadcast is sufficient).
6. **No `SendRemoteAgentTerminalOutput` JSON-RPC method.** The cycle
   does not expose a dashboard-driven output emit (only the
   `cascade_id`-bound `AttachCascade` path writes terminal/output
   frames via `AppendTerminalOutput`). The future
   `rc-terminals-bridge` cycle adds a per-pty method (e.g.
   `ReadRemoteAgentTerminal` reading the next N bytes from the pty
   master's read fd).
7. **No terminal-output backpressure.** The cycle's `AppendTerminalOutput`
   drops on a full chan (32-cap) without bumping a counter; the
   per-subscriber `Send` path drops on a full chan (64-cap) with a
   per-subscriber `dropped` counter (read via
   `(*TerminalsRegistry).SubscriberDropped` — parked for the future
   `rc-terminals-backpressure` cycle). The future cycle adds a
   metrics endpoint that exposes the drop counts so dashboards can
   see "I'm too slow" rather than silently missing frames.
8. **The Unix-socket transport's broadcast is a no-op.** Only the
   `(*TCPServer)` registers a broadcaster; the Unix-socket daemon
   doesn't install one. Mutations still land in the registry, and
   the response envelopes echo the post-state so polling is always
   possible. This matches the `rc-plugins` / `rc-triggers` /
   `rc-fastpush-toggle` / `rc-resources` / `rc-conversation-config`
   precedent.
9. **`TerminalEventOutput` Seq is per-terminal, not global.** A
   dashboard subscribing to two terminals sees `seq: 0` on each
   terminal's first frame independently — the cycle does not
   preserve a global seq across terminals. The future
   `rc-terminals-backpressure` cycle adds a global seq counter if
   dashboards need to correlate frames across terminals.
10. **No `cols` / `rows` range validation.** The cycle accepts any
    non-negative value; there is no upper bound (a dashboard that
    posts `cols: 99999` lands as a 99999-wide terminal — the future
    `rc-pty-bridge` cycle that wires a real PTY adds the OS-defined
    row/col bounds).
11. **`Send` on an empty `data` is `-32602` even when the terminal
    is closed.** The dispatcher's "data is required and must be
    non-empty" check fires BEFORE the registry's "closed" check, so a
    dashboard that posts `data: ""` to a closed terminal sees the
    bad-params envelope rather than the closed envelope. The future
    cycle can flip the precedence if dashboards prefer the
    closed-envelope.
12. **No concurrent write coordination on `AppendTerminalOutput`.**
    The helper is goroutine-safe (a single mutex around the
    lazy-init `sync.Once`; the buffered chan itself is
    goroutine-safe). The future `rc-pty-bridge` cycle that has
    multiple goroutines writing chunks (e.g. a Winsize event + a
    read-fd event) can rely on the existing drop-on-full semantic
    without re-touching this cycle's code.

The cycle's `docs/cycles/PLAN.md` row now reads **DONE `<this-commit>`**
for `rc-terminals`; the FutureMatrix row reduces from "TODO" to
"rc-terminals-bridge + rc-terminals-persist" once the verifier's
next matrix run reflects the change.

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
    internal/remotecontrol/terminals.go \
    internal/remotecontrol/terminals_registry.go \
    internal/remotecontrol/terminals_dispatcher.go \
    internal/remotecontrol/terminals_test.go \
    internal/remotecontrol/dispatch.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotecontrol/cascade.go \
    internal/remotetransport/tcp_test.go \
    cmd/wagyu/remote_control.go \
    cmd/wagyu/remote_control_foreground.go

# Run only the cycle's in-process tests.
go test -race -count=1 ./internal/remotecontrol/ \
    -run 'TestAllTerminalKinds|TestIsValidTerminalKind|TestNotifTerminalOutputMethod|TestNotifTerminalClosedMethod|TestDefaultTerminalDimensions|TestTerminalEventBufferCap|TestTerminalsRegistry_|TestCreateRemoteAgentTerminal_|TestListRemoteAgentTerminals_|TestSendRemoteAgentTerminalInput_|TestResizeRemoteAgentTerminal_|TestCloseRemoteAgentTerminal_|TestTerminalOutput_StreamsFromCascade|TestMapTerminalsError'

# Run only the cycle's wire-level tests.
go test -race -count=1 ./internal/remotetransport/ \
    -run 'TestCreateRemoteAgentTerminal_WS|TestListRemoteAgentTerminals_WS|TestSendRemoteAgentTerminalInput_WS|TestResizeRemoteAgentTerminal_WS|TestCloseRemoteAgentTerminal_WS'
```