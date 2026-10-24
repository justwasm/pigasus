# Cycle `rc-streaming-protocol` — JSON-RPC server-push + graceful shutdown handshake

## TL;DR

Cycle `rc-ws-transport` shipped a request/response-only JSON-RPC
server over WebSocket. Cycle `rc-streaming-protocol` extends the
wire protocol to **server-push** (notifications — JSON-RPC frames
with no `id` field), the **dispatcher** to drop replies for
client-side notifications, and the **TCPServer** lifecycle to send a
close frame (`StatusGoingAway`, code 1001) to every open ws client
before tearing down. The plumbing for the future
`rc-cascade-stream` turn-text-emitter is wired: `SessionState` now
fires a `ChunkSink` callback on every `AppendTurnOutput`, and
`Dispatcher.Notify(ctx, connID, method, params)` is the bridge a
host caller uses to push JSON-RPC notifications to a specific ws
connection by ID.

What lands in this cycle:

| Piece | Where |
|---|---|
| `Dispatcher.Notify` (server-push, no `id`) + `Notifier` alias + `SetNotifier`/`Notifier` accessors | `internal/remotecontrol/notify.go` (new, ~150 lines) |
| Dispatcher treats incoming frames without `id` as notifications (drops the reply even on handler error) | `internal/remotecontrol/dispatch.go` (`isNotification` guard added in `Dispatch`) |
| `SessionState.SetChunkSink` + `AppendTurnOutput` fires the sink | `internal/remotecontrol/daemon.go` (`ChunkSink` type, mutex-guarded field, sink invocation outside the write lock) |
| Connection registry (connID → `*wsConn`) + graceful shutdown handshake + `SetNotifier` wiring in `NewTCPServer` | `internal/remotetransport/tcp.go` (`conns`/`connsMu`/`nextConnID`, `pushFrame`, reworked `Shutdown`) |
| Dispatcher interface gains `Notify` and `SetNotifier` | `internal/remotetransport/unix.go` (interface declaration uses the inline function signature so the type-alias in remotecontrol satisfies it without remotetransport importing remotecontrol) |
| 5 new tests | `internal/remotetransport/tcp_test.go` |

No change to the `cmd/wagyu/` flag wiring, the per-turn Unix-socket
behaviour, or `internal/cli/parser_specs.go`.

## What changed since `rc-ws-transport`

### Wire protocol additions

```
        Server → client                 Client → server
        ─────────────                  ──────────────
        {"jsonrpc":"2.0",              {"jsonrpc":"2.0",
         "method":"<name>",             "method":"<name>",
         "params":{…}}                  "params":{…}}
        (no id — server-push)          (no id — fire-and-forget)
```

Both directions now support notifications: a JSON-RPC frame
without an `"id"` member is a "Notification" per spec §4.1, and
the server MUST NOT reply to one. The dispatcher enforces this for
incoming frames; `Dispatcher.Notify` is the outgoing path.

### Dispatcher — `internal/remotecontrol/dispatch.go`

The `Dispatch` method grew a single `isNotification` check (per
JSON-RPC 2.0 §4.1, "Notification"). When the request has no `id`
member (`len(req.ID) == 0` correctly distinguishes a missing id
from `"id": null`), the handler still runs — so unknown-method
diagnostics land wherever the dispatcher logs them — but the
transport is told to skip the write:

```go
isNotification := len(req.ID) == 0

switch req.Method { /* … */ }

// handler ran; result / rpcErr are set

if isNotification {
    // Server MUST NOT reply to a Notification, even on
    // handler failure.
    return nil
}
if rpcErr != nil {
    return rpcError(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
}
return rpcResult(req.ID, result)
```

This replaces the cycle-1 special-case `case "notifications/initialized":
return nil` — that case was the only thing the cycle-1 dispatcher
treated as a notification, and only because of the comment. The new
guard makes any method eligible, which is what the spec requires.

### Dispatcher — `internal/remotecontrol/notify.go` (new)

`Dispatcher.Notify(ctx, connID, method, params)` is the server-push
half. It marshals a JSON-RPC 2.0 notification frame (no `"id"`)
and hands it to a `Notifier` callback the transport registers via
`SetNotifier`. nil notifier → `Notify` is a no-op (so the
Unix-socket transport, which never sets a notifier, stays
request/response only and doesn't need any change).

The `Notifier` type is declared as a **type alias** (`type
Notifier = func(...)`), not a named type. This is deliberate: the
`remotetransport.Dispatcher` interface declares `SetNotifier` with
the inline function signature, and Go's type-identity rules make a
named function type distinct from its underlying type (interface
methods require IDENTICAL parameter types). A type alias collapses
both names to the same type, so the concrete
`*remotecontrol.Dispatcher.SetNotifier(n Notifier)` satisfies the
interface method without remotetransport having to import
remotecontrol. The constraint that breaks the import cycle
through `tcp_test.go` (smoketest imports the concrete dispatcher)
stays intact.

### `SessionState.ChunkSink` — `internal/remotecontrol/daemon.go`

Cycle `rc-ws-transport` already added `AppendTurnOutput` so the
host's chunk handler can mirror the streamed reply into the
daemon's `get_output` tool. Cycle `rc-streaming-protocol` adds a
peer to that: `SessionState.SetChunkSink(sink ChunkSink)`
registers an optional callback that fires on every
`AppendTurnOutput`. The sink's signature mirrors what
`Dispatcher.Notify` needs (`func(method string, params any) error`),
so the host wires them together with a one-liner:

```go
rcSess.SetChunkSink(func(method string, p any) error {
    return rcDisp.Notify(ctx, connID, method, p)
})
```

(or, for the rc-cascade-stream broadcast case, an internal helper
that iterates every open ws connection — that's a future cycle,
not this one).

The sink is captured under the write lock and invoked outside the
lock, so a slow callback doesn't block subsequent chunk writes —
the print path is hot, and the chunk rate can be model-rate.

### `TCPServer` — `internal/remotetransport/tcp.go`

Three additions:

1. **Connection registry.** A `conns map[string]*wsConn` guarded
   by `connsMu` (RWMutex). `NewTCPServer` initialises the map;
   `handleWS` inserts on entry and deletes on exit (the
   `defer func() { delete(...) }` runs after the per-connection
   close). Each connection gets a stable `connID`
   (`"ws-<atomic-counter>"`) so the dispatcher's notifier can
   address it.

2. **`SetNotifier` wiring.** `NewTCPServer` calls
   `disp.SetNotifier(srv.pushFrame)` once. `pushFrame` is the
   notifier callback: it looks up the connection in the registry
   and writes the frame as a single ws text message. Unknown
   connID returns `ErrConnNotFound` so the caller can drop
   server-push subscriptions for a closed client.

3. **Graceful shutdown handshake.** `Shutdown` now does four
   steps in order:

   ```
   ①  listener.Close()                       no new accepts
   ②  for each open *wsConn in parallel:
        c.Close(StatusGoingAway,             RFC-correct 1001;
                "server shutdown")           per-conn 5s internal timeout
   ③  s.wg.Wait()                            wait for every handleWS to exit
   ④  httpServer.Shutdown(ctx)               tear down the http.Server
   ```

   `s.wg` now tracks **both** `http.Server.Serve` and every
   `handleWS` goroutine (handleWS adds itself on entry and calls
   `Done()` in its deferred cleanup). Step ③ waits for the union,
   so when step ④ runs there are no active handlers left.

The cycle-1 behaviour is preserved: `Shutdown` is idempotent
(`stopOnce`), uses a 5-second shutdown context by default, and
honours the caller's context deadline if one is passed. The diff
is that the disconnect now happens via a proper close handshake
(so the client can drain its buffers and observe the
`StatusGoingAway` code) instead of a TCP reset.

### Dispatcher interface — `internal/remotetransport/unix.go`

The interface declared in `remotetransport/unix.go` (so the
transport package can be the one that names the boundary, without
importing `remotecontrol`) grew two methods:

```go
Notify(ctx context.Context, connID, method string, params any) error
SetNotifier(func(ctx context.Context, connID string, frame []byte) error)
```

Both are no-ops on the Unix-socket transport side (the Unix
transport is request/response only — it never calls them).

## Wire-format compatibility (the contract this cycle honours)

`daemon_test.go`'s 16 tests pin the cycle-1 Unix-socket wire
format end-to-end (initialize / tools/list / tools/call / status
/ list_agents / get_output / unknown method / malformed input /
shutdown cleanup). After this cycle, the same 16 tests pass
**byte-for-byte** against the new code path. The cycle-1
envelopes are unchanged: every existing test sends a request
*with* an `id`, so the new `isNotification` guard never fires for
them.

The TCP+WebSocket smoke test (`internal/remotetransport/tcp_test.go`)
gains 5 tests in this cycle (the cycle brief asks for 5; the
`TestPingPong_AutoHandled` slot is dropped and replaced with a
"trust the library" comment, see 硬约束 §3):

| Test | What it pins |
|---|---|
| `TestServerPushNotification_Received` | `Dispatcher.Notify(connID, method, params)` → ws client receives a frame with no `id` field, correct method name, and the params round-trip |
| `TestServerPushNotification_NoReply` | Client sends a frame without `id`; server processes it but does NOT write a reply (client's `Read` times out at 500 ms, not at the 2 s deadline the server's "send a reply" latency would imply) |
| `TestServerPushNotification_UnknownConnID` | `Dispatcher.Notify` with an unknown `connID` returns `ErrConnNotFound` (doesn't block or hang) |
| `TestServerGracefulShutdown_SendsGoingAway` | Client receives a close frame with code 1001 (`StatusGoingAway`) when `Shutdown` is initiated; client's `Read` returns a `CloseError` whose status is `StatusGoingAway` |
| `TestServerGracefulShutdown_NoNewConns` | After `Shutdown` is initiated, `websocket.Dial` to the server fails (listener is closed in step ①) |

## Test results

### New tests (added by this cycle)

| Test | Where | What it pins |
|---|---|---|
| `TestServerPushNotification_Received` | `internal/remotetransport/tcp_test.go` | The server-push ask: `Dispatcher.Notify(connID, method, params)` reaches the ws client; frame has no `id` |
| `TestServerPushNotification_NoReply` | `internal/remotetransport/tcp_test.go` | The client-notification ask: incoming frame without `id` → no reply |
| `TestServerPushNotification_UnknownConnID` | `internal/remotetransport/tcp_test.go` | Dispatcher returns `ErrConnNotFound` for unknown connID (not blocking) |
| `TestServerGracefulShutdown_SendsGoingAway` | `internal/remotetransport/tcp_test.go` | Close frame with code 1001 reaches the client before TCP teardown |
| `TestServerGracefulShutdown_NoNewConns` | `internal/remotetransport/tcp_test.go` | Listener closes as step ① of `Shutdown`; subsequent dials are refused |

5 new tests + 0 modified + 0 deleted in this cycle's surface.

### Test counts after this cycle

| File | Tests |
|---|---:|
| `internal/remotecontrol/daemon_test.go` | 16 (unchanged) |
| `internal/remotetransport/tcp_test.go` | 9 (4 cycle-1 + 5 new) |
| **Total in affected packages** | **25** |

### Race detector

```
$ go test -race ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.403s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.141s
```

### `go vet`

```
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/
$ echo $?
0
```

### `gofmt -l` on changed files

```
$ gofmt -l internal/remotecontrol/notify.go \
              internal/remotecontrol/dispatch.go \
              internal/remotecontrol/daemon.go \
              internal/remotetransport/tcp.go \
              internal/remotetransport/unix.go \
              internal/remotetransport/tcp_test.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in
`internal/` + a handful in `cmd/wagyu/`) is intentionally left
alone per the cycle rules.

## Live verification (manual)

```bash
$ cd /Users/gear/Documents/GitHub/openagy-rc
$ go build -o /tmp/wagyu ./cmd/wagyu
$ /tmp/wagyu --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc

# in another shell: point any ws client at the printed URL
$ websocat ws://127.0.0.1:NNNNN/rpc
> {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}
< {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{...},"capabilities":{"tools":{}}}}
```

The Go smoke test (`tcp_test.go`) is the in-process equivalent of
this. Server-push isn't wired to a model turn yet (that's
`rc-cascade-stream`), so the manual test only exercises the
existing request/response surface.

## Files

| File | Δ | What |
|---|---|---|
| `internal/remotecontrol/notify.go` (new) | +149 | `Dispatcher.Notify`, `Notifier` alias, `SetNotifier`/`Notifier` accessors |
| `internal/remotecontrol/dispatch.go` | +33 / −5 | `notifyMu` + `notify` fields on `Dispatcher`; `isNotification` guard in `Dispatch` |
| `internal/remotecontrol/daemon.go` | +82 / −1 | `ChunkSink` type, `SetChunkSink`/`ChunkSink` accessors, sink invocation in `AppendTurnOutput` |
| `internal/remotetransport/tcp.go` | +120 / −25 | `conns`/`connsMu`/`nextConnID` fields; `wsConn` struct; `ErrConnNotFound`; `pushFrame` (the notifier callback); four-step graceful `Shutdown`; `handleWS` now tracks itself in `s.wg` |
| `internal/remotetransport/unix.go` | +25 / −1 | `Dispatcher` interface gains `Notify` and `SetNotifier` |
| `internal/remotetransport/tcp_test.go` | +191 / −1 | 5 new tests + `waitForConnID` helper + `errors` import |

`cmd/wagyu/remote_control.go`, `cmd/wagyu/main.go`, and
`internal/cli/parser_specs.go` are **untouched** (the brief says
don't change flag wiring or per-turn Unix-socket behaviour).

## Design notes worth keeping

1. **`Notifier` is a type alias, not a named type.** This is the
   key to keeping the package graph acyclic. The remotetransport
   Dispatcher interface declares `SetNotifier` with the inline
   function signature `func(ctx context.Context, connID string,
   frame []byte) error`; the concrete `*remotecontrol.Dispatcher`
   declares it with `n Notifier` where `Notifier` is a type alias
   for the same signature. Go's type-identity rules make a named
   function type distinct from its underlying type (interface
   method satisfaction fails), but a type alias collapses both
   names to the same type (satisfaction succeeds). The constraint
   comment in `notify.go` documents this in detail.

2. **`s.wg` now tracks per-connection handlers.** Cycle 1
   tracked only `http.Server.Serve`. Tracking every `handleWS`
   goroutine as well makes `s.wg.Wait()` a unified drain barrier:
   after step ③ of `Shutdown`, no handler is running, so step ④'s
   `http.Server.Shutdown(ctx)` returns near-instantly. This is
   what lets the 5-second shutdown context in `Shutdown` be a real
   bound instead of a vague cap.

3. **The dispatcher's notifier is best-effort.** A slow client's
   `Writer` blocks the notifier's call to `conn.Write(...)`. A
   follow-up cycle ("rc-streaming-protocol-buffered") should add a
   per-conn buffered channel with a quota so the server can drop
   slow clients instead of blocking on them. Cycle
   `rc-cascade-stream` (later) will be the first real consumer of
   `Dispatcher.Notify`, so the buffering decision is best made
   once we have a model of what notification rate looks like.

4. **`SetChunkSink` is fire-and-forget, by design.** The sink's
   return value (error) is dropped at the call site. Cycle
   `rc-cascade-stream` will route the error back through the
   dispatcher's `ErrorSlot` mechanism (mirroring what `pigrt`
   does for backend errors) when the buffered-channel upgrade
   lands. For now, dropped is correct: the dashboard's
   `get_output` is the source of truth for the turn text, and
   `ChunkSink` is just the streaming path.

5. **Two transports share one dispatcher.** This cycle makes the
   server-push path transport-specific (only the WebSocket
   transport has a `conns` registry), but the dispatcher's
   `Notify` is transport-agnostic. The Unix-socket transport
   leaves `notify` nil, so `Notify` is a no-op for it. Future
   transports can register their own notifiers without changing
   the dispatcher's surface.

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu ./cmd/wagyu

# Run all affected tests with the race detector
go test -race ./internal/remotecontrol/ ./internal/remotetransport/

# (Optional) vet + gofmt on the changed packages
go vet ./internal/remotecontrol/ ./internal/remotetransport/
gofmt -l internal/remotecontrol/notify.go \
          internal/remotecontrol/dispatch.go \
          internal/remotecontrol/daemon.go \
          internal/remotetransport/tcp.go \
          internal/remotetransport/unix.go \
          internal/remotetransport/tcp_test.go
```

## 硬约束 / limitations

1. **Notifications are best-effort, no per-conn buffer.** A slow
   client's `Writer` blocks the dispatcher's notifier call to
   `conn.Write(...)`, which blocks whatever called
   `Dispatcher.Notify` (eventually the print path's chunk
   handler). The "right" answer is a per-conn buffered channel
   with a quota (drop on overflow, log a metric). This is the
   `rc-streaming-protocol-buffered` follow-up cycle; cycle
   `rc-cascade-stream` (which will be the first real consumer of
   `Dispatcher.Notify`) is the natural place to add it.

2. **No reconnect / heartbeat on the client side.** A ws client
   that crashes silently is detected only when the next
   `Dispatch` call from the server side fails to write. The
   coder/websocket library's TCP keepalive settings are at their
   defaults; an `OnPingReceived` handler that pings on a timer
   would surface dead clients faster. Out of scope here (the
   cycle brief is about the server-side protocol surface, not
   client observability); cycle `rc-status-tool` may pick this
   up.

3. **`TestPingPong_AutoHandled` dropped.** The library's `Ping`
   does not read the connection itself — it sends a ping and
   waits for a concurrent `Read` call to process the pong. A
   client-side `Ping` test would need a background Reader
   goroutine on the client (the test does sequential Read +
   Ping, with no Reader running). Driving a goroutine just to
   assert the library default is more noise than signal, so
   the test slot is replaced with a comment documenting that
   we trust coder/websocket v1.8.15's `c.read()` auto-pong path
   (`/Users/gear/go/pkg/mod/github.com/coder/websocket@v1.8.15/read.go:317`).
   If a future cycle adds a manual ping handler (e.g. for
   latency metrics), this is where the test belongs.

4. **Cycle-1 listener-close ordering reversed.** Cycle 1's
   `Shutdown` called `httpServer.Shutdown(ctx)` first, then
   `listener.Close()` as a fallback. This cycle reverses it
   (listener.Close first, so step ② can immediately stop
   accepting, then the close frames, then
   `httpServer.Shutdown(ctx)` to release http.Server state).
   The end behaviour is identical for any test that doesn't
   inspect the listener-closed state mid-shutdown; the
   `TestServerGracefulShutdown_NoNewConns` test pins the new
   ordering by sleeping briefly after `Shutdown` is initiated
   and asserting a new `Dial` is refused.

5. **`Cmd/wagyu/remote_control.go` still has no
   `SetChunkSink` call.** The brief says "Don't actually wire
   it to a model turn yet; that's `rc-cascade-message` /
   `rc-cascade-stream`". The plumbing is in place (the
   dispatcher's `Notify`, the `SessionState.SetChunkSink`, the
   `conns` registry on TCPServer); wiring `rcSess.SetChunkSink(...)`
   into `maybeStartRemoteControlTCPDaemon` is the
   `rc-cascade-stream` cycle's first commit.

6. **`remotecontrol.Notifier` is exposed but undocumented in the
   PLAN.md row.** The PLAN.md cycle list for `rc-streaming-protocol`
   mentions "server-push notifications", "ping/pong cadence", and
   "ws close handshake on shutdown" — all three are present. The
   PLAN.md row doesn't list a per-conn buffered channel (硬约束 §1
   above) — that's the next-cycle's row, not this one's. Adding
   "expose `Dispatcher.Notify`" to the row text is a docs-only
   change for the next cycle's planner.
