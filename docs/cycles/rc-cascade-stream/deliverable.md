# Cycle `rc-cascade-stream` — `StreamRemoteAgentStateUpdates` streaming server-side of `SendRemoteAgentCascadeMessage`

## TL;DR / 摘要

Cycle `rc-cascade-message` (`f9bba59`) wired the synchronous end-to-end
turn path: dashboard sends one user message, server drives one full
turn through the `MessageBackend` seam, returns the assistant's final
reply in one response. This cycle (`rc-cascade-stream`) wires the
**streaming counterpart**: while that turn is running, the server
pushes JSON-RPC 2.0 notifications (no `id` field, per spec §4.1) to
the originating ws connection. Dashboards that subscribe see
mid-turn updates; dashboards that ignore them get the same final-reply
envelope cycle-3a already shipped.

**Streaming surface** (cycle-5 adds, no new JSON-RPC method):

| Notification | When | Params |
|---|---|---|
| `cascade/state` (phase=`starting`) | turn begins | `cascade_id`, `phase`, `ts` |
| `cascade/text_delta` | assistant emits text | `cascade_id`, `delta` |
| `cascade/tool_call` | assistant invokes a tool | `cascade_id`, `name`, `args`, `result`, `ok` |
| `cascade/state` (phase=`done`) | turn ends successfully | `cascade_id`, `phase`, `stopReason`, `durationMs`, `ts` |
| `cascade/state` (phase=`error`) | turn fails | `cascade_id`, `phase`, `error`, `ts` |

The agy reference's single-method name `StreamRemoteAgentStateUpdates`
is exposed as a `NotifStreamRemoteAgentStateUpdates` constant for
dashboards that pin it; the dispatcher does **not** emit under that
name (single-emit, not dual-emit).

**No new PiG wiring.** Cycle `rc-piagent-bind` provides the real
`StreamingMessageBackend`; this cycle ships `StubBackend.SendStream`
as the fixture. The dispatcher routes `Send` through the new
`SendStream` path via type assertion — backends that don't implement
`StreamingMessageBackend` silently fall back to the cycle-3a `Send`
and drop the sink (pin: `TestSendStream_BackendNotStreaming_FallsBackToSync`).

| Piece | Where |
|---|---|
| `StateFrame` / `EventSink` / `StreamingMessageBackend` interface, `Notif*` constants (incl. the agy alias), `ConnNotifierSink`, `withConnID` / `connIDFromContext` / `(*Dispatcher).WithConnID` (the per-conn plumb that remotetransport calls per-request) | `internal/remotecontrol/streaming.go` (new, ~497 lines) |
| `StubBackend.SendStream` (mirror of `Send`, but emits through the sink); nil-receiver and nil-sink safe | `internal/remotecontrol/backend.go` (+~86) |
| `LastReply` field on `*Cascade` + `setReply` / `appendReplyText` / `LastReplySnapshot` accessors; `(*CascadeRegistry).SendStream(ctx, cascadeID, prompt, sink, opts)` (the type-assertion dispatch + per-event state updates); `Send(ctx, cascadeID, prompt, opts)` is now a shim that calls `SendStream(..., nil, opts)` so cycle-3a tests stay green | `internal/remotecontrol/cascade.go` (+~148 / -3) |
| `case "SendRemoteAgentCascadeMessage":` in `Dispatch` now calls `d.cascades.SendStream(...)` with a `ConnNotifierSink` that reads `connIDFromContext(ctx)`; `sink.OnError(c, err)` before the error envelope so a streaming dashboard sees `phase=error` before the synchronous `-32603`; no error-mapping changes | `internal/remotecontrol/dispatch.go` (+~43 / -3) |
| `remotetransport.Dispatcher` interface grows `WithConnID(ctx, connID) context.Context` (no import cycle; the seam a future transport uses to thread the connID through the per-request ctx) | `internal/remotetransport/unix.go` (+~10) |
| `handleWS` wraps each per-request ctx via `s.dispatcher.WithConnID(ctx, connID)` so the dispatcher's cascade handler can route notifications back to the originating ws | `internal/remotetransport/tcp.go` (+~12 / -1) |
| 6 in-process tests + 4 wire-level tests (cycle-5 additions) + a `readJSONRPCResponse` helper that drains `cascade/*` notifications before reading a response envelope (used by the cycle-3a tests too — they were tripping on the new `cascade/state` starting frame) | `internal/remotecontrol/cascade_test.go` (+~389) + `internal/remotetransport/tcp_test.go` (+~420 / -1) |

What lands for forward consumers:

- A `StreamingMessageBackend` interface (`MessageBackend + SendStream`)
  the future `rc-piagent-bind` cycle satisfies.
- A `ConnNotifierSink` the dispatcher instantiates per
  `SendRemoteAgentCascadeMessage` call; the sink is the seam a future
  `rc-backpressure` cycle replaces with a buffered + drop-policy
  variant. The interface signature is preserved so the replacement
  is wire-compatible.
- A per-conn `connID` plumbing (transport → dispatcher's
  `WithConnID`) so notifications target the right ws client. Same
  UX cycle-3a Pin #1 promised for `rc-handle-user-interaction`.

## Wire protocol added by this cycle

The cycle adds **zero new JSON-RPC methods**. It adds a `method`
namespace on the notification surface (server-push frames), each
of which is a normal JSON-RPC 2.0 notification (no `id` field):

```json
{
  "jsonrpc": "2.0",
  "method":  "cascade/state",
  "params":  {
    "cascade_id": "cas_<16-hex>",
    "phase":      "starting" | "done" | "error",
    "ts":         "<rfc3339-nano>",
    "stopReason": "end_turn" | "tool_use" | "max_tokens" | "error",
    "durationMs": <int>,         // phase == done only, omitted when ≤ 0
    "error":      "<msg>"        // phase == error only
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "method":  "cascade/text_delta",
  "params":  { "cascade_id": "cas_<16-hex>", "delta": "<chunk>" }
}
```

```json
{
  "jsonrpc": "2.0",
  "method":  "cascade/tool_call",
  "params":  {
    "cascade_id": "cas_<16-hex>",
    "name":       "<tool>",
    "args":       { ... } | null,
    "result":     "<string>",
    "ok":         true
  }
}
```

Sequence on a successful turn with one tool call:

1. `cascade/state` (phase=starting)
2. `cascade/text_delta` (delta = assistant fragment)
3. `cascade/tool_call` (× N, one per invocation)
4. `cascade/state` (phase=done, stopReason, durationMs)
5. `SendRemoteAgentCascadeMessage` synchronous response envelope (carries the final reply / conversationId / durationMs / toolCalls / stopReason)

Sequence on a backend error after some events have been emitted:

1. ... (any preceding starting / text_delta / tool_call / state frames)
2. `cascade/state` (phase=error, error="<inner.Error()>")
3. `SendRemoteAgentCascadeMessage` synchronous error envelope (`-32603`)

Sequence on a timeout (the backend's `Sleep > timeoutMs`):

1. `cascade/state` (phase=error, error="context deadline exceeded")
2. `SendRemoteAgentCascadeMessage` synchronous error envelope (`-32010`)

The `cascade/...` namespace is intentional: the dispatcher emits
**one notification per event kind under its own method name** rather
than the agy reference's single-method `StreamRemoteAgentStateUpdates`
with a discriminator field on `params`. This shape is friendlier for
dashboards that want to subscribe to a subset (e.g. a TUI that
cares only about `cascade/state` and ignores `cascade/text_delta`).
The agy reference's method name is **not** emitted (single-emit,
not dual-emit) — see `NotifStreamRemoteAgentStateUpdates` below.

### `NotifStreamRemoteAgentStateUpdates` exposed as a constant

The agy reference's notification name lives in the cycle's public
constants as `remotecontrol.NotifStreamRemoteAgentStateUpdates`. A
dashboard that wants to subscribe to that name can do so without
re-declaring it; the dispatcher never emits under it. Emitting
under both names would duplicate events on a dashboard that
subscribes to both — the cycle explicitly chose single-emit.

```go
const (
    // Emitted frames use this method name:
    NotifCascadeState    = "cascade/state"
    NotifCascadeTextDelta = "cascade/text_delta"
    NotifCascadeToolCall  = "cascade/tool_call"

    // Exposed as a constant only (not emitted):
    NotifStreamRemoteAgentStateUpdates = "StreamRemoteAgentStateUpdates"
)
```

## What changed

### `internal/remotecontrol/streaming.go` (new, ~497 lines)

Four new surface types and one helper per package-private sink:

```go
// One server-push notification payload — phase / stopReason / totalTokens / error.
type StateFrame struct {
    Phase       string  // "starting" | "working" | "done" | "error"
    StopReason  string  // populated only when Phase == "done"
    TotalTokens int64   // populated only when Phase == "done" (optional, future)
    Error       string  // populated only when Phase == "error"
}

const (
    StatePhaseStarting = "starting"
    StatePhaseWorking  = "working"
    StatePhaseDone     = "done"
    StatePhaseError    = "error"
)

const (
    NotifCascadeState                = "cascade/state"
    NotifCascadeTextDelta            = "cascade/text_delta"
    NotifCascadeToolCall             = "cascade/tool_call"
    NotifStreamRemoteAgentStateUpdates = "StreamRemoteAgentStateUpdates" // alias; not emitted
)

// The seam a streaming backend (StubBackend.SendStream today, a PiG-backed
// backend in rc-piagent-bind) receives from the dispatcher.
type EventSink interface {
    OnState(cascade *Cascade, frame StateFrame)
    OnTextDelta(cascade *Cascade, delta string)
    OnToolCall(cascade *Cascade, call ToolCall)
    OnError(cascade *Cascade, err error)
}

// The optional capability. Implemented by StubBackend; a future PiG-backed
// backend implements it natively; non-streaming backends silently fall back.
type StreamingMessageBackend interface {
    MessageBackend
    SendStream(ctx context.Context, prompt MessagePrompt, sink EventSink, opts MessageSendOptions) (MessageResult, error)
}

// Default sink the dispatcher installs on a SendRemoteAgentCascadeMessage call.
// Routes through dispatcher.Notify(connID, …); captures a *Cascade to update
// LastReply / LastReplyAt as a side effect.
type ConnNotifierSink struct { disp *Dispatcher; connID string; cascade *Cascade; startedAt time.Time }
func NewConnNotifierSink(d *Dispatcher, connID string, cascade *Cascade) *ConnNotifierSink
```

The `cascade` parameter on every `EventSink` method is reserved for
multi-cascade sinks (a future dashboard that aggregates events across
cascades into a single stream); the current `ConnNotifierSink` uses
its captured cascade instead and ignores the per-call argument.
Tests can pass `nil` for `cascade` (the stub backends in
`cascade_test.go` do exactly that).

#### `withConnID` and `(*Dispatcher).WithConnID`

The transport needs to thread the per-connection `connID` into the
dispatcher's per-request `ctx`. The transport cannot import
`internal/remotecontrol` (the constraint that breaks the import
cycle through `tcp_test.go`), so the dispatcher exposes
`WithConnID(ctx, connID) context.Context` as a method on its own
`Dispatcher` and the remotetransport package's interface declares
the same method. The cycle adds:

```go
// On *Dispatcher (visible to remotetransport via the interface):
func (d *Dispatcher) WithConnID(ctx context.Context, connID string) context.Context

// Package-private (used inside internal/remotecontrol):
func withConnID(ctx context.Context, connID string) context.Context
func connIDFromContext(ctx context.Context) string
```

`connIDFromContext` returns `""` when no `connID` is set; the
`ConnNotifierSink` calls `Dispatcher.Notify("" , ...)` in that
case, and `Notify` returns `nil` (no notifier is registered for
in-process dispatcher drivers, e.g. Unix-socket or tests). So an
empty `connID` doesn't crash — it just means "no notifications
land" (the cascade's side effects still update).

### `internal/remotecontrol/backend.go` (+~86)

`StubBackend` gained a `SendStream` method that mirrors `Send` but
emits through the sink before returning. Event sequence:

1. `OnState(phase=starting)`
2. `OnTextDelta(Reply)` — only when `Reply != ""`
3. `OnToolCall(call)` — once per `ToolCalls[i]` (skipping empty names)
4. `OnState(phase=done, stopReason)` — always

Error path:

- Sleep > 0 + ctx cancelled → `OnError(nil, ctx.Err())` then return `ctx.Err()`
- `Err != nil` → `OnError(nil, Err)` then return `Err`

`SendStream` is **nil-receiver safe** (returns the zero
`MessageResult{StopReason: StopReasonEndTurn}` matching `Send`)
and **nil-sink safe** (skips every `OnXxx` call). The cycle's
`TestConnNotifierSink_NilSafe` pins both.

### `internal/remotecontrol/cascade.go` (+~148 / -3)

Three additions:

1. **`LastReply` field on `*Cascade`** (mutex-guarded via the
   existing per-cascade `mu`). The streaming sink fills it
   incrementally via `appendReplyText` on every `OnTextDelta`;
   the synchronous cycle-3a path sets it via `setReply` on a
   successful turn. Both paths converge: the registry's success
   block calls `setReply(res.Reply)` (idempotent against the
   accumulated text) and `recordReply(res.NewConversationID,
   time.Now().UTC())` on the registry side; the sink's
   `OnState(phase=done)` separately calls `recordReply("",
   time.Now().UTC())` so `LastReplyAt` is stamped regardless
   of whether the cascade started the streaming path or not.

   `LastReplySnapshot() string` is the read-only accessor
   (nil-safe; the new `*Cascade.LastReply string` field plus
   the existing `mu`).

2. **`(*CascadeRegistry).SendStream(ctx, cascadeID, prompt, sink, opts)`**:
   the cycle-5 face of `SendRemoteAgentCascadeMessage`. Same
   surface as `Send` plus a sink parameter. Type-asserts the
   cascade's backend to `StreamingMessageBackend`:

   ```go
   if sb, ok := backend.(StreamingMessageBackend); ok {
       res, err = sb.SendStream(sendCtx, prompt, sink, opts)
   } else {
       // Cycle-3a fallback: a non-streaming backend gets
       // called through Send; the sink is dropped.
       res, err = backend.Send(sendCtx, prompt)
   }
   ```

   The error mapping (context-deadline → `ErrTurnTimeout`,
   anything-else → `ErrTurnFailed`) is identical to the
   cycle-3a `Send` path so the dispatcher's switch doesn't
   need to know which path produced the error.

3. **`(*CascadeRegistry).Send(ctx, cascadeID, prompt, opts)` is now
   a shim** that calls `SendStream(..., nil, opts)`. Cycle-3a
   tests (`TestCascade_Send_*`) keep their `Send`-based
   assertions unchanged; they never see a sink so the streaming
   path becomes "run the synchronous stub path verbatim".

### `internal/remotecontrol/dispatch.go` (+~43 / -3)

The `case "SendRemoteAgentCascadeMessage"` branch in `Dispatch`
is updated to build a `ConnNotifierSink` and call
`(*CascadeRegistry).SendStream`:

```go
connID := connIDFromContext(ctx)
sink := NewConnNotifierSink(d, connID, c)
res, err := d.cascades.SendStream(ctx, cascadeID, prompt, sink, opts)
if err != nil {
    // Cycle-5: also surface the error through the sink so
    // a streaming dashboard sees a cascade/state(phase=error)
    // before the synchronous -32603 envelope arrives.
    sink.OnError(c, err)
    // …switch…
}
```

The error envelope mapping (codes -32602 / -32004 / -32005 /
-32010 / -32603) is unchanged. `sink.OnError` is nil-safe and
quiet when no notifier is registered.

### `internal/remotetransport/unix.go` (+~10)

`Dispatcher` interface gains `WithConnID(ctx, connID) context.Context`.
The comment block explains the import-cycle constraint:
remotetransport cannot import remotecontrol (cycle through
tcp_test.go), so the per-conn plumb lives on the interface
directly.

```go
// WithConnID returns a context carrying the given connID.
// Cycle rc-cascade-stream adds this so the transport can
// thread the per-connection connID into the dispatcher's
// per-request context (the cascade handler reads it via
// its private connIDFromContext helper to route mid-turn
// notifications back to the originating ws connection).
WithConnID(ctx context.Context, connID string) context.Context
```

### `internal/remotetransport/tcp.go` (+~12 / -1)

`handleWS` wraps the per-request `ctx` via
`s.dispatcher.WithConnID(ctx, connID)` so the dispatcher's cascade
handler can read the per-connection `connID` for `ConnNotifierSink`.

```go
// Cycle rc-cascade-stream: thread the per-conn connID
// through the request context so the dispatcher's cascade
// handler can route mid-turn notifications back to this ws
// connection. We wrap per-request (not at handleWS entry)
// because the dispatcher internally derives a per-request
// context anyway, and per-request wrapping is what makes a
// slow client's connID stale across requests impossible.
reqCtx := s.dispatcher.WithConnID(ctx, connID)
resp := s.dispatcher.Dispatch(reqCtx, raw)
```

### `internal/remotecontrol/cascade_test.go` (+~389)

6 new tests organised in three groups. The recorder test fixture
is a `recordingBackend` that implements both `SendStream` and
`Send`, captures every event into a `[]sinkEvent`, and lets the
test assert the exact emit order.

| Test | What it pins |
|---|---|
| `TestSendStream_EmitsAllEventKinds` | recordingBackend with one text delta + one tool call → 5 events emitted in order: state(Starting), text, tool, state(Done, EndTurn); cascade `LastReply`, `LastReplyAt`, `ConversationID` all stamped |
| `TestSendStream_BackendNotStreaming_FallsBackToSync` | a backend that only implements `Send` (the `onlySendBackend` fixture) — `SendStream` falls back to `Send`, the sink is silently dropped, `Send` is called exactly once, the cascade's `LastReply` / `LastReplyAt` are still stamped |
| `TestSendStream_ErrorPath_EmitsOnError` | a streaming backend with `Err != nil` — `OnError` fires exactly once on the dispatcher-side sink; the registry wraps in `ErrTurnFailed` AND preserves the inner error in the chain (`errors.Is(err, ErrTurnFailed)` && `errors.Is(err, be.err)` both match) |
| `TestConnNotifierSink_NilSafe` | nil receiver + zero-value sink (`NewConnNotifierSink(nil, "", nil)`) — every `OnXxx` call is a no-op; no panic |
| `TestDispatcher_WithConnID` | `(*Dispatcher).WithConnID` returns a derived ctx with the connID set; `connIDFromContext` reads it; nil dispatcher returns the input ctx; the input ctx is not mutated |

### `internal/remotetransport/tcp_test.go` (+~420 / -1)

Two helpers added and 4 wire tests retrofitted (they used to
break because `readJSONRPC` reads exactly one frame, but now the
first frame after a `Send` is a `cascade/state` starting
notification):

```go
// readJSONRPCResponse reads frames until one has an "id"
// field set. Notification frames (no id) are dropped. Cycle
// rc-cascade-stream introduced mid-turn notifications on
// SendRemoteAgentCascadeMessage — tests that only care about
// the final envelope can use this helper.
func readJSONRPCResponse(t *testing.T, c *websocket.Conn) map[string]any

// readJSONRPCNotifications drains up to max notification
// frames, returning the captured frames. Stops early on the
// first response frame.
type jsonRPCNotification struct { Method string; Params map[string]any; IsResponse bool }
func readJSONRPCNotifications(t *testing.T, c *websocket.Conn, max int) []jsonRPCNotification
```

4 cycle-3a wire tests retrofitted (`_OK`, `_ToolCalls`,
`_Timeout`, `_PropagatesError`) to call `readJSONRPCResponse`
instead of `readJSONRPC` after their `Send` call, draining the
`cascade/state` notifications before reading the synchronous
envelope. Cycle-3a behaviour is unchanged — the tests' assertions
on `result.reply`, `error.code`, `result.toolCalls`, etc. all
still hold.

4 cycle-5 wire tests added:

| Test | What it pins |
|---|---|
| `TestSendStream_FrameOrder` | StubBackend with one `Reply` + two `ToolCall`s → exactly 5 notification frames (starting, text_delta, tool_call × 2, done) before the synchronous envelope; `cascade_id` is on every notification; `phase` values match (`starting` then `done`); `durationMs` is either absent or a `float64` (a fast stub may legitimately skip it); tool-call params carry `name`, `ok`, etc. |
| `TestSendStream_SlowClientDoesNotBlock` | A normal-speed ws client reads everything within a 2-second budget — confirms the dispatcher doesn't block on `Notify`'s writer. The framer test (`TestSendStream_FrameOrder`) pins the normal-speed notification sequence; this test pins "the WS writer doesn't hang the dispatcher when buffer + drop policy kicks in" |
| `TestSendStream_TwoCascadesOnOneConn_DoNotMix` | cascade A and cascade B both turn over the same ws connection in parallel; cascade A's notifications always carry A's id, cascade B's carry B's. A buggy sink that hardcoded a single cascade would scramble the stream and this test would catch it |
| `TestSendStream_MissingConnID_NoPanic` | Dispatch through a `Dispatcher` with no `SetNotifier` (i.e. an in-process driver / Unix-socket transport / future transport that doesn't thread connID). The dispatcher builds a sink, the sink's `OnState` calls `Notify("" , ...)` which returns `nil` (no notifier registered); the synchronous envelope still arrives with the assistant's reply; no panic |

The cycle-3a wire tests pass unchanged in behaviour after the
helper swap. Verified with:

```
$ go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.806s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.432s
```

## Wire-format compatibility

Cycle 1–4 contract is preserved:

- `initialize / tools/list / tools/call / ping /
  notifications/initialized / StartRemoteAgentCascade /
  SendRemoteAgentCascadeMessage` return byte-for-byte the same
  shape they returned in cycle `rc-cascade-message`.

- The cycle **adds** a server-push notification namespace on the
  same ws connection: `cascade/state`, `cascade/text_delta`,
  `cascade/tool_call`. A dashboard that ignores them sees the
  same final envelope it saw in cycle 3a; a dashboard that
  subscribes sees mid-turn updates between the request write
  and the response read.

- `NotifStreamRemoteAgentStateUpdates` is exposed as a constant
  but never emitted. Dashboards that want to subscribe to the
  agy reference's method name can do so without re-declaring
  it; they will simply receive no notifications (the dispatcher
  emits only under `cascade/...`).

The cycle does NOT add a new JSON-RPC method, does NOT change
the error envelope shape, does NOT change the request/response
field types. The 16 daemon_test.go tests + 9 cycle-1/2
tcp_test.go tests + 5 rc-cascade-start tcp_test.go tests + 7
rc-cascade-message tcp_test.go tests all pass unchanged against
the new code (the cycle-3a wire tests were retrofitted to use
`readJSONRPCResponse`, but their assertions are identical).

## Test results

### New tests (added by this cycle)

| Test | Where | What it pins |
|---|---|---|
| 5 cycle-5 tests (`TestSendStream_*` + `TestConnNotifierSink_*` + `TestDispatcher_WithConnID`) | `internal/remotecontrol/cascade_test.go` | registry-level SendStream semantics + EventSink contract + nil-safety + WithConnID seam |
| 4 cycle-5 tests (`TestSendStream_FrameOrder`, `_SlowClientDoesNotBlock`, `_TwoCascadesOnOneConn_DoNotMix`, `_MissingConnID_NoPanic`) | `internal/remotetransport/tcp_test.go` | wire-level notification sequence; back-pressure non-blocking; multi-cascade isolation on one ws; missing-connID no-panic |

### Test counts after this cycle

| File | Tests (before) | Tests (after) |
|---|---:|---:|
| `internal/remotecontrol/daemon_test.go` | 16 | 16 (unchanged) |
| `internal/remotecontrol/cascade_test.go` | 32 | 37 (+5 new) |
| `internal/remotetransport/tcp_test.go` | 21 | 25 (+4 new) |
| **Total in affected packages** | **69** | **78** |

`go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/` PASS.

### Race detector

```
$ go test -race -count=3 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	…
ok  	github.com/chadgpt/openagy/internal/remotetransport	…
```

Re-run with `-count=3`: all 78 tests pass three times in a row.

### `go vet`

```
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/
$ echo $?
0
```

`go vet ./cmd/wagyu/...` reports a pre-existing issue in
`cmd/wagyu/model_default_effort_test.go:67` (a `t.Errorf` with 4
args instead of 3) — NOT introduced by this cycle. Per the
project's "don't reformat pre-existing gofmt/vet dirt" rule, that
file is left alone. Confirmed via `git stash`.

### `gofmt -l` on changed files

```
$ gofmt -l internal/remotecontrol/backend.go \
          internal/remotecontrol/cascade.go \
          internal/remotecontrol/dispatch.go \
          internal/remotecontrol/streaming.go \
          internal/remotecontrol/cascade_test.go \
          internal/remotetransport/tcp.go \
          internal/remotetransport/unix.go \
          internal/remotetransport/tcp_test.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in
`internal/` + `cmd/wagyu/picker_view.go`) is intentionally left
alone per cycle rules.

### `go build`

```
$ go build ./...
(no output)
```

## Live verification (manual sketch)

```bash
$ cd /Users/gear/Documents/GitHub/openagy-rc
$ go build -o /tmp/wagyu_rc5 ./cmd/wagyu
$ /tmp/wagyu_rc5 --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc

# In another shell:
$ websocat ws://127.0.0.1:NNNNN/rpc
> {"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade","params":{"clientInfo":{"name":"smoke","version":"0"}}}
< {"jsonrpc":"2.0","id":1,"result":{"cascade_id":"cas_a3f1...","conversationId":"","created_at":"...","model":"Gemini 3.8 Flash (Medium)","backend":"agw","version":"0.1.0"}}

> {"jsonrpc":"2.0","id":2,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"cas_a3f1...","text":"hi"}}
< {"jsonrpc":"2.0","method":"cascade/state","params":{"cascade_id":"cas_a3f1...","phase":"starting","ts":"…Z"}}
< {"jsonrpc":"2.0","method":"cascade/state","params":{"cascade_id":"cas_a3f1...","phase":"done","stopReason":"end_turn","durationMs":0,"ts":"…Z"}}
< {"jsonrpc":"2.0","id":2,"result":{"conversationId":"","reply":"","durationMs":0,"toolCalls":[],"stopReason":"end_turn"}}

> {"jsonrpc":"2.0","id":3,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"cas_a3f1...","text":"hi","timeoutMs":30}}
< {"jsonrpc":"2.0","method":"cascade/state","params":{"cascade_id":"cas_a3f1...","phase":"error","error":"context deadline exceeded","ts":"…Z"}}
< {"jsonrpc":"2.0","id":3,"error":{"code":-32010,"message":"Turn timeout","data":"remotecontrol: turn timeout: context deadline exceeded"}}
```

The Go wire tests (`tcp_test.go`) round-trip through the actual
`coder/websocket` library, so the wire envelope (incl. the
notification ordering shown above) is pinned byte-for-byte.

## Files

| File | Δ | What |
|---|---|---|
| `internal/remotecontrol/streaming.go` (new) | +497 | `StateFrame` + `EventSink` + `StreamingMessageBackend` + 4 `Notif*` constants + `ConnNotifierSink` + `withConnID` / `connIDFromContext` + `(*Dispatcher).WithConnID` |
| `internal/remotecontrol/backend.go` | +86 | `(*StubBackend).SendStream` (mirror of `Send`, emits through the sink); nil-receiver / nil-sink safe |
| `internal/remotecontrol/cascade.go` | +147 / -3 | `LastReply` field + `setReply` / `appendReplyText` / `LastReplySnapshot`; `(*CascadeRegistry).SendStream(ctx, cascadeID, prompt, sink, opts)`; `(*CascadeRegistry).Send` is a shim that calls `SendStream(..., nil, opts)` |
| `internal/remotecontrol/dispatch.go` | +39 / -2 | `handleSendRemoteAgentCascadeMessage` builds `NewConnNotifierSink(d, connIDFromContext(ctx), c)` + calls `d.cascades.SendStream` + `sink.OnError(c, err)` before the error envelope |
| `internal/remotetransport/unix.go` | +10 | `Dispatcher` interface adds `WithConnID(ctx, connID) context.Context` (transport-facing seam) |
| `internal/remotetransport/tcp.go` | +12 / -1 | `handleWS` wraps each per-request ctx via `s.dispatcher.WithConnID(ctx, connID)` |
| `internal/remotecontrol/cascade_test.go` | +389 | 5 cycle-5 tests + `recordingBackend` / `onlySendBackend` / `captureSink` fixtures + `TestDispatcher_WithConnID` + `fmt` + `sync/atomic` imports |
| `internal/remotetransport/tcp_test.go` | +421 / -1 | `readJSONRPCResponse` + `readJSONRPCNotifications` + `jsonRPCNotification` helpers; 4 cycle-3a wire tests retrofitted to use `readJSONRPCResponse`; 4 cycle-5 wire tests; `fmt` import |

`internal/remotecontrol/daemon.go`, `internal/remotecontrol/wire.go`,
`internal/remotecontrol/host.go`, `internal/remotecontrol/notify.go`,
`cmd/wagyu/remote_control.go`, `cmd/wagyu/remote_control_adc.go`,
and `internal/remotetransport/unix_test.go` are **untouched**.

## Design notes worth keeping

1. **The `StreamingMessageBackend` seam is the only contract
   future cycles need to know about.** `rc-piagent-bind`
   constructs a `*pigrt`-backed implementation that satisfies
   both `Send` (cycle-3a fallback) and `SendStream` (cycle-5
   primary path); the dispatcher's cascade handler is
   unchanged. Backends that only satisfy `Send` keep working
   on the cycle-3a path — `TestSendStream_BackendNotStreaming_FallsBackToSync`
   pins that.

2. **Single-emit on `cascade/...`, no dual-emit with the agy
   alias.** Emitting on both `cascade/state` and
   `StreamRemoteAgentStateUpdates` would duplicate events on a
   dashboard that subscribes to both. The cycle exposes the
   alias as a constant only (`NotifStreamRemoteAgentStateUpdates`)
   so a dashboard can pin the agy reference's name without
   re-declaring, but the dispatcher never emits under it.
   Today this is a documentation choice; a future
   `rc-compat-mode` cycle could add a config flag if dashboards
   in the wild need it.

3. **Best-effort, drop-on-error back-pressure.** The
   `ConnNotifierSink` calls `Dispatcher.Notify(ctx, connID, …)`
   and drops the error (`_ = s.disp.Notify(…)`). A slow client
   whose ws `Write` blocks will time out inside `coder/websocket`
   and start returning errors; subsequent notifications are
   dropped silently. The backend turn continues regardless.
   `TestSendStream_SlowClientDoesNotBlock` and
   `TestSendStream_FrameOrder` pin that the dispatcher doesn't
   block on the writer. A future `rc-backpressure` cycle can
   replace the sink with a buffered + drop-policy variant
   while preserving the `EventSink` interface.

4. **`sink.OnError` is informational; the dispatcher's error
   envelope is canonical.** A streaming dashboard sees the
   `cascade/state(phase=error)` notification before the
   synchronous envelope, but the envelope's `code` is the
   authoritative failure mode. The error notification is for
   the dashboard UI to show "this turn failed" before the
   envelope arrives; the envelope carries the JSON-RPC code
   (`-32603` / `-32010`).

5. **`LastReply` is set via `setReply` from the registry's
   success path AND accumulated via `appendReplyText` from
   the sink's `OnTextDelta`.** Both paths converge: the
   synchronous cycle-3a path sets `LastReply` to the final
   `Reply` at end-of-turn; the streaming path accumulates
   per-delta and the registry's success path then calls
   `setReply(res.Reply)` (idempotent against the accumulated
   text — the accumulated text IS the final reply when the
   backend is a streaming backend). Either path stamps
   `LastReplyAt` via `recordReply`.

6. **`SendStream`'s error path emits `OnError` before
   returning.** Matches `StubBackend.Send`'s contract: a
   real PiG-backed backend that fails mid-turn drives the
   same path, so a streaming dashboard sees `phase=error`
   before the synchronous envelope regardless of whether
   the failure happened before any events fired or after
   half a turn's worth.

7. **Per-request ctx wrapping, not per-connection.** The
   `handleWS` wraps each request's ctx with the per-conn
   connID — not the handler's entry ctx. Wrapping per
   request is what makes a slow client's connID stale across
   requests impossible (the connID is part of every derived
   ctx, and a fresh ctx is built for every read).

8. **The transport-facing interface is the seam; no dispatcher
   import from the transport.** `remotetransport.Dispatcher`
   gains `WithConnID`. The `*remotecontrol.Dispatcher`
   concrete type provides the implementation through the
   same method name (Go's structural interface satisfaction
   does the rest). Adding methods to a public-facing
   interface is a controlled change; the cycle's commit
   message documents that adding `WithConnID` is the first
   such interface growth since `rc-streaming-protocol` (the
   original 7-method surface).

## 硬约束 / limitations

1. **No real PiG/agw wiring.** The cycle ships
   `(*StubBackend).SendStream` as the cycle-5 fixture; a real
   PiG-session-backed `StreamingMessageBackend` lands in
   `rc-piagent-bind`. The dispatcher routes via type
   assertion: a backend that doesn't implement
   `StreamingMessageBackend` silently falls back to the
   `Send` path.

2. **No cancellation on a slow client.** The
   `ConnNotifierSink` is best-effort: a slow client whose ws
   write blocks eventually times out inside `coder/websocket`
   and the dispatcher drops subsequent notifications. The
   backend turn does NOT abort when the ws writer fails — a
   future `rc-cancel-on-ws-close` cycle may add that. Today,
   a dashboard that closes the connection mid-turn sees a
   silent backend completing the turn in the background.

3. **Notifications may arrive out of order with respect to
   the synchronous envelope under load.** The
   `coder/websocket` library serialises writes per
   connection, so within one ws connection the
   `cascade/state(phase=starting)` → `cascade/text_delta`
   → … → `cascade/state(phase=done)` sequence is in order.
   But the final envelope write may be batched behind the
   `done` notification's write, and a slow `Notify` writer
   can cause the envelope to arrive AFTER a frame the
   backend emitted late. Dashboards must treat the
   synchronous envelope as the canonical end-of-turn signal
   (its `toolCalls` / `durationMs` are authoritative) and the
   notifications as informational.

4. **No reconnect / resume on the ws side.** A dashboard's
   connection dropping leaves any cascades it started
   orphaned in the registry. Reconnect+resume is parked for
   the rc-cascade-stream-after-recovery cycle (not yet
   scoped).

5. **`LastReplySnapshot` is mutex-guarded but not
   lock-free.** A multi-model backend that calls
   `OnTextDelta` concurrently from multiple goroutines
   would serialise through the per-cascade `mu`; today
   there's only one appender per turn (the brief's "single
   goroutine drives the turn" rule).

6. **`stream` notifications use `cascade/<kind>` method
   names, not the agy reference's
   `StreamRemoteAgentStateUpdates`.** Dashboards targeting
   the reference have to renamesubscribe, but the cycle
   exposes the reference name as a constant for
   discoverability. A future rc-compat-mode cycle could
   optionally emit under both — today, single-emit.

7. **No persistence of the notification stream.** A
   dashboard that joins an already-running turn does NOT
   get the historical notifications — only the synchronous
   envelope. The brief's "live tail" model means a
   reconnecting dashboard re-fetches `get_output` for the
   historical text. (rc-list-conversations + rc-status-tool
   are the right surfaces for that.)

8. **Empty `durationMs`** is emitted only when
   `time.Since(startedAt).Milliseconds() > 0`. A fast stub
   or a backend that returns inside the same millisecond
   produces a `done` notification with no `durationMs`
   field. Dashboards should treat the absence as "unknown /
   too fast to measure" rather than 0. The cycle's
   `TestSendStream_FrameOrder` pins the lenient acceptance.

9. **Pre-existing `go vet` issue** in
   `cmd/wagyu/model_default_effort_test.go:67` is unchanged
   (verified via `git stash`). Per the project's
   "don't reformat pre-existing vet dirt" rule, the file
   stays as the prior cycle left it.

10. **`Dispatcher.WithConnID` is the first interface
    growth since `rc-streaming-protocol`.** Adding methods
    to the `remotetransport.Dispatcher` interface is a
    controlled change — every concrete implementation in the
    tree (`*remotecontrol.Dispatcher`) must grow the
    method. Today there's exactly one implementation, so
    this is cheap; a future cycle that adds a second
    implementation must remember to implement
    `WithConnID` too.

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu_rc5 ./cmd/wagyu

# Run all affected tests with the race detector, 3 iterations
go test -race -count=3 ./internal/remotecontrol/ ./internal/remotetransport/

# Optional: vet + gofmt on the changed packages
go vet ./internal/remotecontrol/ ./internal/remotetransport/
gofmt -l internal/remotecontrol/backend.go \
          internal/remotecontrol/cascade.go \
          internal/remotecontrol/dispatch.go \
          internal/remotecontrol/streaming.go \
          internal/remotecontrol/cascade_test.go \
          internal/remotetransport/tcp.go \
          internal/remotetransport/unix.go \
          internal/remotetransport/tcp_test.go

# Run the cycle-5-specific tests
go test -race -count=1 -v \
  -run 'TestSendStream_|TestConnNotifierSink_|TestDispatcher_WithConnID' \
  ./internal/remotecontrol/ ./internal/remotetransport/
```

## 后续 cycle 入口 / next-cycle entry points

| Future cycle | What this cycle left for them |
|---|---|
| `rc-piagent-bind` | A `StreamingMessageBackend` interface (`internal/remotecontrol/streaming.go`). The cycle constructs a `*pigrt`-backed implementation and installs it on every cascade at registration time; the dispatcher handler routes through `SendStream` automatically. The pre-existing `Send`-only PiG-session implementation keeps working (cycle-3a fallback). |
| `rc-backpressure` | `ConnNotifierSink` is the per-conn sink the dispatcher installs today; the cycle can replace it with a buffered variant (per-conn channel + quota + drop policy) without touching the `EventSink` interface. |
| `rc-cancel-on-ws-close` | The transport's `handleWS` closes the per-conn ctx on disconnect; a future cycle can hook that into a cascade-level cancel that aborts the in-flight backend turn. Today, the turn runs to completion in the background. |
| `rc-list-conversations` | `(*Cascade).LastReply` + `LastReplyAt` are now stamped; the cycle can surface them through the cascade list method's per-cascade breakdown. |
| `rc-handle-user-interaction` | `(*Cascade).Backend()` returns the `MessageBackend`; the cycle can request `Send`-based permission prompts from the same seam. (The streaming cycle already proved `Sink` parameter is plumbable into the per-request ctx without an interface break.) |
| `rc-cancel-cascade` | `(*Cascade).MarkClosed` is unchanged; the cycle pairs it with `*CascadeRegistry.Drop` to release the slot when the dashboard cancels mid-turn. |
| `rc-status-tool` | `(*Cascade).LastReplyAt` + `LastReply` + `ConversationID` are non-empty after the first turn; the status tool's per-cascade breakdown can read all three from the registry. |
| `rc-final-parity-verify` | Every notification the cycle emits is exercised end-to-end at this layer: a Python ws client dials, calls Start → Send → asserts the notification sequence + final envelope matches the cycle's `TestSendStream_FrameOrder` shape. |
