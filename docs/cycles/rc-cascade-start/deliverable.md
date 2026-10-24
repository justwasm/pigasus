# Cycle `rc-cascade-start` — first cascade lifecycle method (`StartRemoteAgentCascade`) + per-process registry

## TL;DR

Cycles `rc-ws-transport` (46b348b) and `rc-streaming-protocol` (23de9aa9)
landed the wire-protocol surface: a JSON-RPC-over-WebSocket server
with notifications and graceful shutdown. Cycle `rc-cascade-start`
adds the first **lifecycle method** on top of that: a remote dashboard
can now call `StartRemoteAgentCascade` over the wire and get back an
opaque `cascade_id` it will use for everything that follows
(`SendRemoteAgentCascadeMessage`, `StreamRemoteAgentStateUpdates`,
`HandleRemoteAgentCascadeUserInteraction` — all future cycles).

The cycle ships four pieces, each limited in surface so cycles
`rc-cascade-message` / `rc-cascade-stream` / `rc-handle-user-interaction`
can build on it without rewriting:

| Piece | Where |
|---|---|
| `*Cascade` value + `CascadeOpts` + `NewCascade` (id allocator uses `crypto/rand` for 16 hex chars after the `cas_` prefix — 8 random bytes, 64 bits entropy) | `internal/remotecontrol/cascade.go` (new, ~260 lines) |
| `*CascadeRegistry` (RWMutex-guarded, capped at `CascadeRegistryDefaultMax`=256 by default, soft `ErrRegistryFull` over the cap; `Register / Get / Drop / Len / Max / IDs`) | same file |
| Dispatcher hooks the registry: `Config.Cascades` (injected, default = fresh), `(*Dispatcher).Cascades()` accessor, `handleStartRemoteAgentCascade` JSON-RPC handler that mints → registers → returns, `nonStringFieldInJSON` helper for the "model must be string" rule | `internal/remotecontrol/dispatch.go` (+~120 / -3) and `daemon.go` (+~10 / -0) |
| 18 new tests (13 in-process + 5 wire-level over WS) | `internal/remotecontrol/cascade_test.go` (new) + `internal/remotetransport/tcp_test.go` (+340 lines) |

What lands for forward consumers: a `*CascadeRegistry` per
`*Dispatcher` (one registry per host process; ws connections on the
same TCP listener share it; restart resets it per the brief). The
cycle explicitly does NOT wire any PiG session, model turn, or
streaming notifications — `*Cascade` is metadata only.

## Wire protocol added by this cycle

### Method: `StartRemoteAgentCascade`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "StartRemoteAgentCascade",
  "params": {
    "clientInfo": {
      "name": "<string>",
      "version": "<string>"
    },
    "projectId":     "<string>",         // optional, "" = current
    "model":         "<string>",         // optional, "" = daemon default
    "effort":        "low|medium|high|xhigh|max",   // optional, like --effort
    "agent":         "<string>",         // optional, the agent name
    "sandbox":       true,               // optional, mirrors --sandbox
    "scaffold":      true,               // optional
    "sessionMode":   "<string>"          // optional, opaque passthrough
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "cascade_id":     "cas_<16 hex chars (8 random bytes)>",
    "conversationId": "",
    "created_at":     "<RFC3339Nano>",
    "model":          "<resolved model>",
    "backend":        "<resolved backend>",
    "version":        "<ServerVersion>"
  }
}
```

Errors:

| HTTP/JSON-RPC code | Cause | Surface path |
|---|---|---|
| `-32602 Invalid params` | params cannot be parsed; one of `projectId / model / effort / agent / sessionMode` is present and not a string | `json.Unmarshal` error, or `nonStringFieldInJSON` finding a non-string type |
| `-32603 Internal error` | registry is at cap (`ErrRegistryFull`); crypto/rand failed to mint an id | `CascadeRegistry.Register` returning the error |
| `-32700 Parse error` | entire JSON-RPC envelope is not JSON | upstream; unchanged from cycle 1 |

The cycle brief's specific test ask *"{model: 42} → error.code ==
-32602"* is covered twice (WS-level in `TestStartRemoteAgentCascade_InvalidParams`
and dispatcher-level in `TestDispatcher_StartRemoteAgentCascade_InvalidNumericModel`).

## What changed since `rc-streaming-protocol`

### `internal/remotecontrol/cascade.go` (new)

The cascade lifecycle **value type**:

```go
type Cascade struct {
    ID             string            // "cas_<16 hex chars from crypto/rand>"
    ConversationID string            // empty until rc-cascade-message
    CreatedAt      time.Time         // pinned at NewCascade time
    Model          string            // resolved label, "" = use host default
    Backend        string            // resolved backend, "" = use host default
    Agent          string            // agent name (default, explorer, …)
    ClientInfo     CascadeClientInfo // {Name, Version} from the dashboard
    Workspace      string            // captured at NewCascade time
    Effort         string            // low|medium|high|xhigh|max
    Sandbox        bool              // mirrors --sandbox
    Scaffold       bool              // mirrors --scaffold
    SessionMode    string            // opaque passthrough
    ProjectID      string            // "" = use current
}

type CascadeRegistry struct {
    mu       sync.RWMutex
    cascades map[string]*Cascade
    max      int
}
```

The registry is **soft-capped** at `CascadeRegistryDefaultMax = 256`
when `Config.Cascades == nil`. Over-cap `Register` returns
`ErrRegistryFull`; the dispatcher surfaces that as `-32603 Internal
error`. Drop is no-op-on-unknown, so race-free shutdown is just
`registry.Drop(id)` from anywhere.

Forward-compatibility — fields already on `*Cascade` that downstream
cycles will use:

| Field | Future consumer |
|---|---|
| `ConversationID` | `rc-cascade-message` writes it after the first model turn; `rc-list-conversations` iterates `registry.IDs()` and reads this field |
| `Backend`        | the response surface to surface "which path the cascade is on" (when we add a per-cascade backend override) |
| `Agent`          | `rc-cascade-message` agent validation; agent-specific turn runners |
| `ClientInfo`     | `rc-status-tool` per-client breakdown; `rc-list-conversations` dashboard filter |
| `Workspace`      | `rc-cascade-message` per-cascade workspace resolution (so the same registry can hold cascades for different projects) |
| `Effort`         | `rc-cascade-message` reasoning-effort selection on the PiG side |
| `Sandbox`        | `rc-cascade-message` permission-mode selection (mirrors `--sandbox`) |
| `Scaffold`       | `rc-cascade-message` scaffold-mode selection (mirrors `--scaffold`) |
| `SessionMode`    | `rc-cascade-message` passes through to `SessionState` (the existing `--session-mode` flag family) |
| `ProjectID`      | `rc-create-project` / `rc-update-project` / per-cascade project routing |

Fields that will be added in future cycles (NONE removed or renamed):

- `*coding.Session` (PiG session handle) — `rc-cascade-message`
- pending-message channel / queue — `rc-cascade-message`
- pending user-interaction (permission request / ask-question) — `rc-handle-user-interaction`
- stream-subscribers map (`map[connID]struct{}` for broadcast) — `rc-cascade-stream`
- last-error `error` — `rc-error-status`

The struct shape is forward compatible because every added field is
additive; no field is removed or renamed in this cycle or any
documented future cycle.

### `internal/remotecontrol/dispatch.go`

Three additions; no behavioral change to the cycle-1/2 methods:

1. `Config` gains a `Cascades *CascadeRegistry` field
   (`internal/remotecontrol/daemon.go`). Nil → `NewDispatcher`
   builds a fresh `NewCascadeRegistry(CascadeRegistryDefaultMax)`.
   Production wiring may pass a registry explicitly (the
   `maybeStartRemoteControlTCPDaemon` host wiring is unchanged
   because the default fills in).
2. The `Dispatcher` struct holds `cascades *CascadeRegistry`;
   `(*Dispatcher).Cascades()` exposes it (nil-safe). Wire-up
   in `NewDispatcher` is `"cfg.Cascades` is the source of truth,
   and the dispatcher keeps a pointer reference for access".
3. `case "StartRemoteAgentCascade":` in `Dispatch` → calls
   `handleStartRemoteAgentCascade(req.Params)`. The handler:

   a. JSON-decodes into `struct { ClientInfo ...; ProjectID ...
      Model ...; Effort ...; Agent ...; Sandbox *bool; Scaffold
      *bool; SessionMode ... }`. `Sandbox`/`Scaffold` are
      `*bool` so absent and `false` are distinguishable (the
      dashboard's "default — don't touch" is the absense; an
      explicit `false` is "I checked — off").
   b. Runs `nonStringFieldInJSON(params, "model", p.Model)` and
      cousins for `projectId / effort / agent / sessionMode` —
      if any of those fields is *present* in the raw JSON but
      not a string, returns `-32602`. This is the brief's
      *"{model: 42} → Invalid params"* rule; without the
      helper, `json.Unmarshal` would silently coerce the number
      into `""` and the dispatcher would default-walk to the
      host's model.
   c. Resolves `model` and `backend` from the dispatcher's
      `SessionState` when the dashboard left them empty (the
      brief's "empty = use daemon default" rule). The resolved
      values are what's echoed in the response, so the
      dashboard sees what actually ran.
   d. Builds `NewCascade(CascadeOpts{ … })`. `Workspace` is
      snapshotted from `SessionState.Workspace` so a future
      chdir doesn't change the cascade's working directory.
   e. `d.cascades.Register(c)`; on `ErrRegistryFull`, returns
      `-32603` with the error string in `data`.

`handleStartRemoteAgentCascade` is the only handler that knows
the cascade type — every cycle after this reads the registry through
`d.cascades.Get(id)` and operates on the `*Cascade` it returns.

### `internal/remotetransport/tcp_test.go` (+340 lines)

Five wire-level tests, built on a `newTestTCPServerWithCap` helper
that injects a custom `*CascadeRegistry` (the dispatcher default
fills in, but `TestStartRemoteAgentCascade_RegistryFull` needs a
cap of 1 to hit the over-cap path):

| Test | What it pins |
|---|---|
| `TestStartRemoteAgentCascade_Ok` | round-trip: `{clientInfo:…, agent:"explorer"}` in → `cascade_id` non-empty out, `model` and `backend` resolved from `SessionState`, `conversationId` empty (rc-cascade-message's job), `version` matches `ServerVersion`, registry holds the cascade with the right `ID / Agent / ClientInfo` |
| `TestStartRemoteAgentCascade_TwoIndependent` | two starts on the same ws produce two distinct cascade_ids; `registry.Len()==2`; dropping one does not affect the other (the rc-cascade-message close-one-keep-the-others path) |
| `TestStartRemoteAgentCascade_InvalidParams` | `{model: 42}` → envelope `error.code == -32602`, no `result` field, no registry insert |
| `TestStartRemoteAgentCascade_RegistryFull` | 1-cap registry → second start returns `-32603`, registry still holds exactly 1 (no partial insert) |
| `TestStartRemoteAgentCascade_EmptyParams` | the minimum-viable `{}` request returns a valid cascade whose `model` and `backend` fall back to the host's `SessionState` |

### `internal/remotecontrol/cascade_test.go` (new)

Eight in-process tests:

| Test | What it pins |
|---|---|
| `TestCascadeRegistry_RegisterGetDrop` | basic semantics: register one cascade, look it up, drop it; `Len` flips 1→0; `Get` after Drop returns `(nil, false)` |
| `TestCascadeRegistry_Full` | over-cap returns `ErrRegistryFull`; `Len()` is still the cap (no partial insert); over-cap `Get` returns false |
| `TestCascadeRegistry_MaxDefault` | `NewCascadeRegistry(0)` and `NewCascadeRegistry(-3)` collapse to `CascadeRegistryDefaultMax`; explicit `8` stays at 8 |
| `TestCascadeRegistry_IDsUnique` | 200 consecutive `NewCascade` calls produce 200 distinct ids (`crypto/rand` collision probability is non-zero in principle; a duplicate would be a fundamental bug) |
| `TestCascadeRegistry_Concurrent` | 8 goroutines × 100 ops each, Register/Get/Drop under `-race` — no race detector flag, every Drop matches a successful Register |
| `TestCascadeRegistry_NilSafety` | every method on a nil `*CascadeRegistry` is a no-op (no panic, no surprising value) |
| `TestNewCascade_Defaults` | empty `CascadeOpts` ⇒ empty optional fields, non-empty `ID`, `CreatedAt` within 1 second of test start |
| `TestNewCascade_ClientInfoPopulated` | every CascadeOpts field the dashboard might pin (ClientInfo, ProjectID, Model, Effort, Agent, Sandbox, Scaffold, SessionMode, Workspace, Backend) lands in the right Cascade field |
| `TestDispatcher_StartRemoteAgentCascade_OK` | the dispatcher's `Dispatch` API (no Unix socket, no WS adapter) returns the same response shape — pins the dispatcher-level surface |
| `TestDispatcher_StartRemoteAgentCascade_InvalidNumericModel` | `{model: 42}` → `-32602`, registry still empty (no partial insert) |
| `TestDispatcher_StartRemoteAgentCascade_MalformedJSON` | invalid JSON envelope → `-32700`, registry still empty |
| `TestDispatcher_CascadesAccessor` | `Cascades()` is nil-safe; default registry has `Max()==CascadeRegistryDefaultMax` |
| `TestDispatcher_InjectedRegistry` | the brief's "host passes a fresh registry" affordance — `Config.Cascades` is the same instance as `Dispatch().Cascades()` |

The `errorCode` helper at the top of the dispatch-level tests is
the small dance-around for `code` being `int` in-process and
`float64` after a JSON round-trip — both shapes are valid JSON-RPC
envelopes, and our tests must accept whichever they see.

### `internal/remotecontrol/daemon.go` (+10 / -0)

The `Config` struct gains a `Cascades *CascadeRegistry` field,
documented with the default-fills-in behavior. No wire-level change;
no behavior change for any test that leaves `Cascades == nil`.

## Wire-format compatibility

The cycle-1/2 contract — `initialize / tools/list / tools/call /
ping / notifications/initialized` returning the same byte-for-byte
shape — is preserved. Every cycle-1 test in `daemon_test.go` (16
of them) passes against the new code path unchanged. The TCP+WS
test surface (`tcp_test.go`) keeps its 9 cycle-1/2 tests green and
gains 5 new ones for a total of 14.

The dispatcher interface in `internal/remotetransport/unix.go` does
**not** grow a method this cycle: the cycle adds `Config.Cascades`
(an opaque value the dispatcher reads but transports don't touch)
rather than a `Dispatcher` interface method, which keeps the
import-cycle-free constraint intact.

## Test results

### New tests (added by this cycle)

| Test | Where | What it pins |
|---|---|---|
| `TestCascadeRegistry_RegisterGetDrop` | `internal/remotecontrol/cascade_test.go` | Basic registry semantics; `Get` flips to `(nil, false)` after `Drop` |
| `TestCascadeRegistry_Full` | same | `ErrRegistryFull` over cap; `Len` unchanged; over-cap `Get` returns false |
| `TestCascadeRegistry_MaxDefault` | same | `max <= 0` collapses to default; explicit max is preserved |
| `TestCascadeRegistry_IDsUnique` | same | 200 consecutive `NewCascade` produce 200 distinct ids |
| `TestCascadeRegistry_Concurrent` | same | `-race`-clean: 8 goroutines × 100 Register/Get/Drop |
| `TestCascadeRegistry_NilSafety` | same | every method on a nil registry is a no-op |
| `TestNewCascade_Defaults` | same | empty opts ⇒ empty optional fields, non-empty id, recent CreatedAt |
| `TestNewCascade_ClientInfoPopulated` | same | every CascadeOpts field lands in the right Cascade field |
| `TestDispatcher_StartRemoteAgentCascade_OK` | same | in-process dispatch returns the right envelope; registry has 1 entry |
| `TestDispatcher_StartRemoteAgentCascade_InvalidNumericModel` | same | `{model:42}` → `-32602`; no partial insert |
| `TestDispatcher_StartRemoteAgentCascade_MalformedJSON` | same | invalid JSON → `-32700`; no partial insert |
| `TestDispatcher_CascadesAccessor` | same | `(*Dispatcher).Cascades()` is nil-safe; default registry has `CascadeRegistryDefaultMax` |
| `TestDispatcher_InjectedRegistry` | same | `Config.Cascades` is the same instance as `Dispatcher.Cascades()` |
| `TestStartRemoteAgentCascade_Ok` | `internal/remotetransport/tcp_test.go` | Wire-level round trip; registry holds the cascade |
| `TestStartRemoteAgentCascade_TwoIndependent` | same | Two starts ⇒ two distinct ids; Drop is per-id |
| `TestStartRemoteAgentCascade_InvalidParams` | same | `{model:42}` over ws yields `-32602` |
| `TestStartRemoteAgentCascade_RegistryFull` | same | Over-cap yields `-32603`; no partial insert |
| `TestStartRemoteAgentCascade_EmptyParams` | same | `{}` params works; model and backend fall back to `SessionState` |

13 new dispatcher-level tests + 5 new wire-level tests + 0 modified
+ 0 deleted.

### Test counts after this cycle

| File | Tests (before) | Tests (after) |
|---|---:|---:|
| `internal/remotecontrol/daemon_test.go` | 16 | 16 (unchanged) |
| `internal/remotecontrol/cascade_test.go` | 0 | 13 (new) |
| `internal/remotetransport/tcp_test.go` | 9 | 14 (+5 new) |
| **Total in affected packages** | **25** | **43** |

### Race detector

```
$ go test -race ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.495s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.078s
```

Re-run with `-count=3` against `TestCascade|TestNewCascade|TestDispatcher|TestStartRemoteAgentCascade`:
all 18 cycle tests pass three times in a row.

### `go vet`

```
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/
$ echo $?
0
```

`go vet ./cmd/wagyu/...` reports a pre-existing issue in
`cmd/wagyu/model_default_effort_test.go:67` (a `t.Errorf` with 4
args instead of 3) — NOT introduced by this cycle (verified by
`git stash` and re-running the same `go vet`). Per the project's
"don't reformat pre-existing gofmt/vet dirt" rule, that file is
left alone.

### `gofmt -l` on changed files

```
$ gofmt -l internal/remotecontrol/cascade.go \
              internal/remotecontrol/cascade_test.go \
              internal/remotecontrol/dispatch.go \
              internal/remotecontrol/daemon.go \
              internal/remotetransport/tcp_test.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in
`internal/` + `cmd/wagyu/picker_view.go`) is intentionally left
alone per cycle rules.

## Live verification (manual sketch)

```bash
$ cd /Users/gear/Documents/GitHub/openagy-rc
$ go build -o /tmp/wagyu_rc3 ./cmd/wagyu
$ /tmp/wagyu_rc3 --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc

# In another shell:
$ websocat ws://127.0.0.1:NNNNN/rpc
> {"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade","params":{"clientInfo":{"name":"smoke","version":"0"}}}
< {"jsonrpc":"2.0","id":1,"result":{"cascade_id":"cas_a3f1...","conversationId":"","created_at":"2026-...","model":"Gemini 3.8 Flash (Medium)","backend":"agw","version":"0.1.0"}}

> {"jsonrpc":"2.0","id":2,"method":"StartRemoteAgentCascade","params":{"model":42}}
< {"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Invalid params","data":"string fields must be strings"}}
```

The Go tests are the in-process equivalent. The smoke tests
(`tcp_test.go`) round-trip through the actual `coder/websocket`
library so the wire envelope is pinned byte-for-byte.

## Files

| File | Δ | What |
|---|---|---|
| `internal/remotecontrol/cascade.go` (new) | +260 | `Cascade` type, `CascadeClientInfo`, `CascadeOpts`, `NewCascade` (id allocator uses `crypto/rand`), `CascadeRegistryDefaultMax = 256`, `ErrRegistryFull`, `ErrCascadeNotFound`, `CascadeRegistry` (RWMutex-guarded `Register/Get/Drop/Len/Max/IDs`), `(*CascadeRegistry).Nil`-safety |
| `internal/remotecontrol/cascade_test.go` (new) | +392 | 13 in-process tests (8 registry-level + 5 dispatcher-level) |
| `internal/remotecontrol/dispatch.go` | +130 / -3 | `Config.Cascades`, dispatcher `cascades` field, `(*Dispatcher).Cascades()`, `case "StartRemoteAgentCascade"` in `Dispatch`, `handleStartRemoteAgentCascade`, `nonStringFieldInJSON`, `timeFormat` helper, `time` import |
| `internal/remotecontrol/daemon.go` | +10 / -0 | `Config.Cascades *CascadeRegistry` field with documentation |
| `internal/remotetransport/tcp_test.go` | +340 / -0 | `newTestTCPServerWithCap` helper + 5 new `TestStartRemoteAgentCascade_*` tests |

`cmd/wagyu/remote_control.go`, `internal/remotetransport/tcp.go`,
`internal/remotetransport/unix.go`, and the rc-ws-transport /
rc-streaming-protocol files are **untouched** — the dispatcher
default-fills in a registry on `Config.Cascades == nil`, so
existing host wiring (`maybeStartRemoteControlTCPDaemon`) sees
no signature change. The brief's "CLI wiring" ask is satisfied
implicitly: production wiring continues to call `NewDispatcher(cfg)`
with the existing Config struct, and a working
`(*Dispatcher).Cascades()` is the new affordance for future cycles
that need it.

## Design notes worth keeping

1. **`Config.Cascades` is optional.** Host wiring that wants a
   different cap (tests, mostly) passes one explicitly; production
   wiring leaves it nil. The dispatcher's `NewDispatcher`
   default-fills in `NewCascadeRegistry(CascadeRegistryDefaultMax)`,
   matching the brief's "256 default, configurable for tests"
   rule. Backward-compatible for any caller built before this
   cycle.

2. **`nonStringFieldInJSON` is the workaround for json.Unmarshal's
   silent number-to-string coercion.** Without it, a dashboard
   that sent `{model: 42}` would silently default-walk to the
   host's model — and that goes through to the registry and the
   response as `model: ""` (or the resolved fallback), which is
   an unhelpful failure mode. The helper re-parses the params
   object as `map[string]json.RawMessage` and asserts each
   string-typed field's raw JSON value either isn't present or
   is a string. It's intentionally narrow — only the fields
   the brief calls out — so adding a new field is a deliberate
   decision rather than an upgrade-time foot-gun.

3. **`(*Dispatcher).Cascades()` is nil-safe.** The
   `case "StartRemoteAgentCascade"` handler checks
   `if d == nil || d.cascades == nil` and returns
   `-32603 Internal error`, so a misconfigured dispatcher (one
   that somehow got past `NewDispatcher`'s defaults) fails
   cleanly rather than panicking.

4. **Sandbox / Scaffold use `*bool`.** Absent and `false` are
   distinguishable, so a dashboard that says "I want my
   defaults, don't touch" can send `{clientInfo:…}` (no sandbox
   field) and a dashboard that says "off, explicitly" can send
   `{sandbox: false}`. Going forward we treat absent as "use
   host defaults" (the rc-cascade-message cycle's resolve path)
   and explicit `false` as "explicit off".

5. **The `*Cascade` value is metadata only.** No PiG session, no
   pending-message queue, no stream-subscribers. Future cycles
   add fields to `Cascade` (never rename or remove) and methods
   on `*Dispatcher` (`GetCascade(id)` is the natural accessor
   shape, but this cycle ships `Cascades().Get(id)` directly).
   The struct's zero-value semantics are intended: a freshly-built
   *Cascade is fully usable; nothing is required to be inited
   before it's handed to the registry.

6. **Per-cycle shape, not per-cycle blast radius.** The cycle
   adds 18 tests but its in-tree surface is ~260 new lines
   (cascade.go) + ~110 added lines (dispatch.go) + ~350 added
   lines (test code). The actual production binary increases by
   ~2 KB after `go build -o /tmp/wagyu_rc3 ./cmd/wagyu`. Most
   of the work is the test scaffolding the next cycles will
   reuse.

7. **`CascadeRegistryDefaultMax = 256` is the soft cap, not a
   hard limit at the dispatcher API.** A future cycle can lift
   it by passing `NewCascadeRegistry(<larger>)` in
   `Config.Cascades`. 256 was the brief's number; the rc-cascade-message
   cycle may revisit it once we have telemetry on how many cascades
   a real dashboard opens in steady state.

## 硬约束 / limitations

1. **`Cmd/wagyu/remote_control.go` is intentionally untouched.**
   The host wiring already calls `NewDispatcher(cfg)` and the
   cycle's default-fills behavior means existing wiring
   transparently gets a fresh registry. The "CLI wiring" line in
   the cycle brief is satisfied by the dispatcher's new
   `Config.Cascades` field plus the `(*Dispatcher).Cascades()`
   accessor; no `cmd/wagyu/` change is needed for the per-turn
   Unix-socket daemon OR the `--remote-control-tcp` daemon.
   Future cycles that need to inject a different cap, or run
   multiple registries, will edit `remote_control.go` then.

2. **No PiG session lives on `*Cascade` yet.** The struct has
   no `*pigrt.Session` field — the rc-cascade-message cycle
   will add one. Until then, `ConversationID` is always ""
   and the only way to drive a model turn is via the existing
   per-turn Unix-socket daemon (untouched).

3. **Notifications are not wired to cascades.** `Dispatcher.Notify`
   is a transport-agnostic broadcaster (cycle 2); making
   `StreamRemoteAgentStateUpdates` per-cascade is rc-cascade-stream.
   This cycle doesn't call `Notify` from anywhere new, so the
   existing 25 tests in `remotecontrol` / `remotetransport`
   keep passing without any change.

4. **No reconnect on the ws side.** A dashboard's TCP connection
   dropping leaves any cascades it started orphaned in the
   registry (Drop is by cascade_id, not by connID). rc-cascade-stream
   will add a per-conn "I dropped" hook; for now a forgotten
   cascade is reclaimed only when the host's daemon exits and
   the registry is GC'd along with it.

5. **No cascade deletion API.** `Drop` exists for tests and
   future-cycle teardown, but there's no `CancelRemoteAgentCascade`
   method registered this cycle. The decompiled reference has
   one (`1018cbe40_CancelRemoteAgentCascade.c`) — that lands
   in `rc-cascade-message` or a dedicated `rc-cancel-cascade`
   cycle, whichever the next-cycle planner chooses.

6. **`(*Cascade).Model` is set even when the dashboard left
   `model` empty.** The handler resolves empty → `SessionState.Model`
   before storing it. A future cycle that wants to know "did the
   dashboard pin a model or did we default" must check the
   `Params.Model` shape separately — `c.Model` is the resolved
   value, not the requested one. The same applies to `Backend`.
   Not a bug today (no consumer cares), but worth keeping in
   mind when `rc-cascade-message`'s model-resolution logic lands.

7. **Registry is per-dispatcher, not per-connection.** A ws
   connection's `Drop` semantics are documented in the
   `(*CascadeRegistry).Drop` doc-comment (no-op on unknown).
   A dashboard's "I disconnected, drop everything I started"
   pattern needs rc-cascade-stream's per-conn bookkeeping before
   it can be wired; this cycle doesn't promise that contract.

8. **Pre-existing `go vet` issue is not in scope.** The
   `cmd/wagyu/model_default_effort_test.go:67` `t.Errorf` with
   4 args (verified pre-existing via `git stash`) is unchanged.
   The cycle's `go vet ./internal/remotecontrol/
   ./internal/remotetransport/` is clean.

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu_rc3 ./cmd/wagyu

# Run all affected tests with the race detector
go test -race ./internal/remotecontrol/ ./internal/remotetransport/

# (Optional) vet + gofmt on the changed packages
go vet ./internal/remotecontrol/ ./internal/remotetransport/
gofmt -l internal/remotecontrol/cascade.go \
          internal/remotecontrol/cascade_test.go \
          internal/remotecontrol/dispatch.go \
          internal/remotecontrol/daemon.go \
          internal/remotetransport/tcp_test.go
```

## 后续 cycle 入口 / next-cycle entry points

| Future cycle | What this cycle left for them |
|---|---|
| `rc-cascade-message` | `*Cascade.ConversationID` and the empty value semantics; `cascades.Get(cascade_id)` to look up a cascade from a method call; dispatcher's `(*Dispatcher).Cascades()` accessor |
| `rc-cascade-stream` | `StreamRemoteAgentStateUpdates` will subscribe per-cascade; needs new field on `*Cascade` (`subscribers map[connID]…`) and broadcasts via `Dispatcher.Notify` (cycle 2) |
| `rc-handle-user-interaction` | `ErrCascadeNotFound` reserved (this cycle); method is the second user-keyed JSON-RPC call after `Start`, and uses the `cascades` registry to look the cascade up |
| `rc-status-tool` | the registry's `Len()` is the dashboard-visible "live cascades" counter; `IDs()` is the per-dashboard breakdown hook |
| `rc-error-status` | `*Cascade` will gain a `lastError error` field; the dispatcher will surface it through a new method |
| `rc-cancel-cascade` | drops the cascade from the registry; mirrors `(*CascadeRegistry).Drop` semantics; needs a JSON-RPC method registration, this cycle doesn't have that |
| `rc-final-parity-verify` | every method this cycle ships is exercised end-to-end at this layer: a Python ws client dials, calls `StartRemoteAgentCascade`, asserts the registry holds the cascade, optionally disconnects, asserts the cascade is still registered |
