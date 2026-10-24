# Cycle `rc-bidi-loop` — bi-directional ws read/write pumps on the same connection

## TL;DR / 摘要

Cycle `rc-handle-user-interaction` (`64e85ec`) shipped the
**dispatcher half** of the prompt flow — the server could park
a turn on `intr.ResultCh`, push a `cascade/user_interaction_required`
notification, and the dashboard could resolve the pending entry
via a follow-up `HandleRemoteAgentCascadeUserInteraction` call.
But the cycle's own wire test
(`TestHandleUserInteraction_NotificationOverWire`) explicitly
deferred the round-trip on the same ws connection because the
single-goroutine `handleWS` loop read one request at a time:
while the stub was parked on `ResultCh`, the reader could not
accept the `Handle…UserInteraction` call back. The cycle's
deliverable named this gap at §6.1 and pointed forward to
**`rc-bidi-loop`** as the pre-requisite for closing it.

This cycle closes the gap. The transport (`internal/remotetransport/tcp.go`)
is refactored from the read+write single-goroutine loop into
three pieces:

1. **Reader goroutine** (`handleWS`) — reads one JSON-RPC
   request per ws message and spawns one dispatcher goroutine
   per request (`dispatchOne`), so the reader can immediately
   pick up the next frame.
2. **Writer goroutine** (`writePump`) — drains a bounded
   per-conn channel (`outbound chan []byte`, cap=64) and writes
   one ws text message per frame. It is the **sole** caller of
   `(*websocket.Conn).Writer`; coder/websocket does not allow
   concurrent writers, so this serialisation is what keeps the
   ws layer happy when the dispatch goroutine (responses) and
   `pushFrame` (notifications) both publish frames.
3. **Close coordination** (handleWS's deferred teardown) —
   signals senders via a `closing` channel, cancels the
   writer's per-conn ctx, closes the ws (idempotently via
   `closeOnce`), closes the outbound channel (under `outboundMu`
   so a concurrent sender cannot be mid-send), and cancels the
   reader's per-conn ctx.

Wire protocol is unchanged: every existing JSON-RPC envelope
the previous cycles shipped still parses, and `pushFrame` +
`dispatchOne` both go through the same `enqueueFrame` helper
so notifications and responses are guaranteed to interleave in
the order they were produced.

The headline wire test
(`TestHandleRemoteAgentCascadeUserInteraction_BiDirectional_WS`)
flips the cycle-6 deferred test to **end-to-end on the same
ws connection**: a parked stub waits on `intr.ResultCh`; the
client reads the `cascade/user_interaction_required`
notification and answers `HandleRemoteAgentCascadeUserInteraction`
on the same conn; the answer resolves the pending entry; the
stub unblocks; `SendRemoteAgentCascadeMessage` completes; both
envelopes come back on the same connection.

| Piece | Where |
|---|---|
| `outboundChCap = 64` const (per-conn channel capacity); `ErrBackpressure` sentinel (returned by `enqueueFrame` on overflow, increments the conn's `dropped` counter) | `internal/remotetransport/tcp.go` |
| `wsConn` redesign: gains `outbound chan []byte`, `closing chan struct{}`, `closeOnce sync.Once`, `writeCancel context.CancelFunc`, `outboundMu sync.Mutex`, `dropped uint64`; field-level sync contract documented inline | `internal/remotetransport/tcp.go` (+~80 lines of comments) |
| `(*wsConn).enqueueFrame(ctx, frame) error` — shared sender-side helper. Cheap pre-check on `closing`, lock `outboundMu`, re-check `closing`, non-blocking send, count overflow on `ErrBackpressure` | `internal/remotetransport/tcp.go` |
| `(*TCPServer).pushFrame` — replaces inline `wc.conn.Write(...)` with `wc.enqueueFrame(...)`; returns `ErrConnNotFound` / `ErrBackpressure` / `ctx.Err()`; same dispatcher-facing surface as before | `internal/remotetransport/tcp.go` |
| `(*TCPServer).DroppedFrames(connID) uint64` — accessor for the per-conn backpressure counter (test-only today, future status-tool field) | `internal/remotetransport/tcp.go` |
| `(*TCPServer).handleWS` — refactored: spawns `writePump`; per-conn read / write contexts; deferred teardown runs the close coordination in the order the brief pins | `internal/remotetransport/tcp.go` |
| `(*TCPServer).dispatchOne(wc, reqCtx, raw)` — runs one `Dispatcher.Dispatch` on its own goroutine, marshals the response (or skips on notification), enqueues via `enqueueFrame` | `internal/remotetransport/tcp.go` |
| `(*TCPServer).writePump(wc, ctx)` — drains `wc.outbound` to `wc.conn.Writer(...)`; tracked by `s.wg` so `Shutdown` waits for it | `internal/remotetransport/tcp.go` |
| `(*TCPServer).Shutdown` — per-conn close wrapped in `wc.closeOnce.Do(...)` so it composes safely with handleWS's deferred teardown (both call `c.Close`; only one wins) | `internal/remotetransport/tcp.go` |
| 6 new wire-level tests (the headline bi-directional round-trip; stream-notifications-during-other-RPC; write-pump-exits-on-EOF; backpressure-bounded; concurrent-writes-no-corruption; reader-exit-stops-writer) + 2 small helpers (`connCount`, `firstConnID`, `pushFrameForTest` for tests only) | `internal/remotetransport/tcp_test.go` (+~614 lines) |

## Architecture / 数据流

### Before (cycle rc-handle-user-interaction)

```
┌──────── ws client ────────┐         ┌──────── ws server (handleWS goroutine) ────────┐
│                           │         │                                                 │
│  SendMessage (id=2)  ────►│─read──►│  raw = c.Read(ctx)                              │
│                           │         │  resp = Dispatcher.Dispatch(reqCtx, raw)  ◄─┐  │
│                           │         │  wsjson.Write(ctx, c, resp)              │  │  │
│  (sees user_interaction   │         │  (blocked in Dispatch until stub unparks)│  │  │
│   _required notification) │◄──ws────│                                       │  │  │
│                           │         │                                       │  │  │
│  Handle…UserInteraction   │         │  (reader can't pick up the next frame│  │  │
│  (id=3)               ───►│─read──►│   while Dispatch is running)         │  │  │
│                           │         │                                       ▼  │  │
│  (NEVER arrives: reader    │         │  Eventually Dispatch returns; resp   │  │  │
│   is blocked)              │         │  written; loop reads next frame.     │  │  │
│                           │         │                                                 │
│  (timed out; -32603)       │◄──ws────│  SendMessage response (err envelope)            │
└───────────────────────────┘         └─────────────────────────────────────────────────┘
                                       dispatch is synchronous in the reader goroutine,
                                       so any notification sent by the dispatcher during
                                       dispatch (via pushFrame → wc.conn.Write) interleaves
                                       with the response on a single ws Write call — but
                                       the reader CANNOT read another request frame while
                                       dispatch is parked. Round-trip deadlocks.
```

### After (cycle rc-bidi-loop)

```
┌──────── ws client ────────┐    ┌──────── ws server ──────────────────────────────────────┐
│                           │    │                                                            │
│  SendMessage (id=2) ─────►│    │  handleWS goroutine                                       │
│                           │    │   ┌─────────────────────────────────────────────────────┐ │
│                           │    │   │  for { raw = c.Read(readCtx); go dispatchOne(...) } │ │
│                           │    │   └─────────────────────────────────────────────────────┘ │
│                           │    │        │                                                    │
│  (sees user_interaction   │    │        ▼ spawn                                              │
│   _required notification) │    │  ┌───────────────────────────────────────────┐              │
│                           │    │  │ dispatchOne goroutine #1 (SendMessage)     │              │
│                           │◄───│  │  resp = Dispatch(reqCtx, raw)              │              │
│                           │    │  │  enqueueFrame(wc, reqCtx, data) ─────────────┐│          │
│                           │    │  │  (parked on intr.ResultCh via stub)        ││          │
│                           │    │  └───────────────────────────────────────────┘│          │
│  Handle…UserInteraction   │    │        │                                                │     │
│  (id=3)               ───►│    │        │ read continues while #1 is parked       │     │
│                           │    │        ▼                                                │     │
│                           │    │  ┌───────────────────────────────────────────┐│     │
│                           │    │  │ dispatchOne goroutine #2 (Handle…)        ││     │
│                           │    │  │  resp = Dispatch(reqCtx, raw)              ││     │
│                           │    │  │  enqueueFrame(wc, reqCtx, data) ──────────┐ ││     │
│                           │    │  │  (Resolve → ResultCh → stub unblocks)    │ ││     │
│                           │    │  └─────────────────────────────────────────┘ ││     │
│                           │    │        │                                  │ │     │
│  (sees Handle… response   │    │        │  both #1 and #2 eventually        │ │     │
│   id=3: {ok,decided_at,   │◄───│        │  enqueue; writer drains in        │ │     │
│   applied_to:[]})         │    │        │  order. Reader keeps reading      │ │     │
│                           │    │        ▼                                  ▼ ▼     │
│  (sees SendMessage        │    │  ┌─────────────────────────────────────────────────┐ │
│   response id=2: {reply,  │◄───│  │ writePump goroutine (one per conn)              │ │
│   stopReason, ...})       │    │  │  for frame := range wc.outbound {               │ │
│                           │    │  │    c.Writer(ctx, MessageText).Write(frame).Close│ │
│                           │    │  │  }                                             │ │
│                           │    │  └─────────────────────────────────────────────────┘ │
└───────────────────────────┘    │                                                            │
                                 │  Close coordination (handleWS deferred teardown):      │
                                 │   1. close(wc.closing)  ─────► senders bail             │
                                 │   2. wc.writeCancel()     ─────► writer bails           │
                                 │   3. wc.closeOnce.Do(c.Close) ─► idempotent ws close    │
                                 │   4. close(wc.outbound)   ─────► writer range exits      │
                                 │   5. readCancel()         ─────► reader returns          │
                                 │   6. deregister from s.conns                              │
                                 └────────────────────────────────────────────────────────────┘
```

The key shape: the **reader stays free** (it spawns a goroutine
per dispatch and immediately goes back to `c.Read`), and the
**writer is the sole caller** of `(*websocket.Conn).Writer`. Two
dispatch goroutines can publish frames concurrently; their
enqueues serialise on `outboundMu` and the writer drains them
in order.

## Why this matters — the cycle-6 round-trip

`docs/cycles/rc-handle-user-interaction/deliverable.md` §6.1
documents the gap:

> 1. **Wire-level round-trip is not exercised end-to-end.** The
>    `handleWS` single-request-at-a-time loop in
>    `internal/remotetransport/tcp.go` lines 404–436 reads one
>    message at a time, so a parked stub cannot receive the
>    `HandleRemoteAgentCascadeUserInteraction` call back from the
>    same connection. The cycle's wire tests assert the
>    notification envelope (server-push half is fine) and the
>    error-path codes only. The bi-directional close
>    (dashboards answering a parked prompt) is exercised
>    in-process at `TestHandleRemoteAgentCascadeUserInteraction_Ok`
>    in `pending_test.go`, which calls `d.Dispatch(...)` directly
>    from a goroutine. A future cycle patches `handleWS` to
>    split the read + dispatch goroutines; today this is a
>    transport-level gap, not a cycle bug.

This cycle is exactly that future cycle. The deliverable's
forward entry at §13 was named `rc-bidi-loop`:

> | `rc-bidi-loop` | A pre-requisite for any round-trip beyond the notification envelope: split `handleWS` into separate read + dispatch goroutines, and have the dispatcher's request handler release its read on the connection while waiting for `HandleRemoteAgentCascadeUserInteraction` to resolve a parked turn. Without this patch the wire-level round-trip stays gated. |

The new
`TestHandleRemoteAgentCascadeUserInteraction_BiDirectional_WS`
is the wire-level happy path the cycle-6 deferred. The stub
parks on `intr.ResultCh` (with a 5-second `InteractionWait`);
the client reads the `cascade/user_interaction_required`
notification; the client answers
`HandleRemoteAgentCascadeUserInteraction` on the same ws; the
dispatcher's handler calls `pending.Resolve(interaction_id, …)`
which sends on `intr.ResultCh`; the stub unblocks and continues
the turn; the dispatch goroutine marshals the
`SendRemoteAgentCascadeMessage` response and enqueues it; both
responses arrive on the same connection.

## Wire protocol — unchanged

This cycle does not add or change any JSON-RPC envelope. Every
method cycle `rc-ws-transport` through `rc-conversation-deletions`
shipped still parses; every notification name still serialises
verbatim. The change is purely in how frames are *delivered*
(synchronously via the reader's inline write vs. asynchronously
via the shared writer + outbound channel). A future cycle that
wants to add a new wire envelope will see no difference.

## Backpressure policy

`wc.outbound` is a buffered channel with capacity `outboundChCap = 64`
(pinned by the brief). When a sender finds the channel full,
`enqueueFrame` returns `ErrBackpressure` and increments
`wc.dropped` atomically. The frame is dropped — not blocked
on. The choice: **drop on overflow, count drops, signal
backpressure**.

Why drop instead of block:

- A blocked dispatch goroutine holds the per-conn `outboundMu`
  and prevents any other sender from publishing on this conn
  until the writer drains enough room. With a slow client, that
  cascade would freeze every `pushFrame` call (and every
  dispatch response) on the conn.
- The dispatcher emits notifications as side-effects of
  `SendStream`. Blocking the dispatcher means blocking the
  model's turn — the worst outcome.
- A drop counter is observable: a future status-tool field can
  surface `dropped` per cascade / per conn so a dashboard sees
  "this conn is slow" rather than mysteriously missing frames.

The counter is read via `(*TCPServer).DroppedFrames(connID) uint64`.
The cycle's `TestBackpressure_Bounded` pins that the counter
equals the number of `ErrBackpressure` returns the caller
observed across a 1000-frame push (16 KiB each) — the channel
fills before the writer can drain and the `dropped` counter
tracks it.

`enqueueFrame` also honours the request ctx: if the buffer is
full AND ctx is done AND closing is not closed, the function
returns `ctx.Err()` rather than counting the frame as dropped
(because the request that triggered the push was already
cancelled by the dashboard — there's no point in counting a
"drop" for a frame the caller no longer wants to send).

## Concurrent-write safety

coder/websocket declares "All methods may be called concurrently
except for Reader and Read" on `*websocket.Conn`; concurrent
`Writer` / `Write` calls are not safe. The cycle enforces
single-writer by routing **every** outbound frame through
`wc.outbound` and having `writePump` be the sole consumer. The
two producer paths (dispatch responses, pushFrame notifications)
serialise on `outboundMu` for the channel send; the writer
serialises on the ws library's internal Writer / Close
discipline.

`TestConcurrentWrites_NoCorruption` fires 50 `tools/list`
requests rapidly; the dispatch goroutines complete in arbitrary
order (the dispatcher is goroutine-safe); the writer drains
their enqueues in the order they were produced; the client
sees 50 well-formed responses with 50 distinct ids (no
corruption, no id collision, no overwrites).

## Concurrent-close safety

The lifecycle close has to coordinate four things without
deadlocking, panicking, or leaking:

1. `close(wc.closing)` — signal senders (pushFrame + dispatch
   goroutines) that the conn is dying.
2. `wc.writeCancel()` — cancel the writer's per-conn ctx so a
   pending `c.Writer(ctx, ...)` bails with `ctx.Err()` instead
   of blocking on a half-closed peer.
3. `wc.closeOnce.Do(func() { _ = wc.conn.Close(StatusNormalClosure, "") })`
   — kill the ws. Idempotent: both Shutdown's per-conn
   goroutine AND handleWS's deferred teardown try this; only
   one wins. The other is a no-op.
4. `wc.outboundMu.Lock(); close(wc.outbound); wc.outboundMu.Unlock()`
   — let `writePump`'s range exit. The lock prevents the
   "send on closed channel" panic: any sender that already
   passed the cheap `closing` pre-check is either holding the
   lock (and finishes its send before close runs) or waiting
   for the lock (and re-checks `closing` after acquiring it,
   bailing if closed).

The dispatch goroutines (per-request, not tracked by `s.wg`)
do not need explicit cancellation handling: they hold the
request's ctx, which is derived from `readCtx` via
`dispatcher.WithConnID`. When handleWS's deferred teardown
calls `readCancel()`, every in-flight `reqCtx` is cancelled
too; the goroutines see `ctx.Done()` in the dispatcher's
internal selects and bail. If a goroutine is mid-`enqueueFrame`
when teardown runs, the `closing` pre-check makes it return
`ErrConnNotFound` instead of trying to send on a closed
outbound channel.

`TestReaderExit_StopsWriter` exercises this: the client closes
the ws while a stub `SendMessage` is parked in `Sleep`; the
server's reader sees EOF; deferred teardown runs; Shutdown
returns within its 2-second budget. A leaked writer would
hold `s.wg` above zero and the budget would elapse — the test
catches that as a non-nil `Shutdown` error.

## Test coverage

### `internal/remotetransport/tcp_test.go` (+~614 lines / 6 new tests)

| Test | What it pins |
|---|---|
| `TestHandleRemoteAgentCascadeUserInteraction_BiDirectional_WS` | **Headline of the cycle.** Parked stub with one `Interaction`, 5s `InteractionWait`. Client reads `cascade/user_interaction_required` notification, answers `HandleRemoteAgentCascadeUserInteraction` on the same ws. Both responses (id=2 SendMessage + id=3 HandleInteraction) arrive on the same connection. The round-trip the cycle-6 deliverable deferred at §6.1. |
| `TestStreamNotifications_ReachClient_DuringOtherRPC` | Reader doesn't block on dispatch: a slow `SendMessage` (100ms Sleep backend) is in flight; the client sends a sync `tools/list`; the `tools/list` response (id=3) arrives BEFORE the `SendMessage` response (id=2). With the old single-goroutine loop the second request couldn't be dispatched until the first returned — id=3 would arrive last. |
| `TestWritePump_ClosesOnEOF` | Writer exits on client EOF. After client close + 50ms wait, `Shutdown` returns within its 2s budget. A leaked writer would hold `s.wg` and the budget would elapse. |
| `TestBackpressure_Bounded` | 16 KiB × 1000 frames through `pushFrameForTest`. The 64-cap channel fills; the `dropped` counter equals the number of `ErrBackpressure` returns the caller observed. The test does NOT pin a minimum backpressure count (writer may be faster than the push loop on a clean machine) — the invariant is `counter == backpressureCount`, not a designer-chosen threshold. |
| `TestConcurrentWrites_NoCorruption` | 50 concurrent `tools/list` requests. 50 unique-id responses, each with a valid `result.tools` catalogue. Proves the writer serialisation holds under load. |
| `TestReaderExit_StopsWriter` | Reader-exit triggers writer-exit. Client closes while stub `SendMessage` is parked in Sleep. Shutdown returns within budget. |

Plus 3 test-only accessors on `*TCPServer`:

- `(*TCPServer).connCount() int` — for tests to assert exactly one open conn
- `(*TCPServer).firstConnID() string` — for tests to address the conn without poking at `s.conns` directly
- `(*TCPServer).pushFrameForTest(ctx, connID, frame) error` — direct enqueue helper so the backpressure test can fill the channel without dispatching a real JSON-RPC request

The pre-existing tests stay green: every `rc-*` cycle's wire
tests still pass under `-race -count=1` (verified below). The
cycle does not fire `pushFrame` from any new code path — only
the cycle-5 `ConnNotifierSink.OnState/OnTextDelta/OnToolCall/OnUserInteraction/OnError`
methods and the cycle-6 `ErrCascadeInteractionTimeout` path
publish through `pushFrame`, exactly as before, except now via
`enqueueFrame` instead of inline `wc.conn.Write(...)`.

### Verification

```
$ go build ./...                                              # exit 0
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/ # exit 0
$ gofmt -l internal/remotetransport/tcp.go internal/remotetransport/tcp_test.go  # empty
$ go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.683s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.706s
$ go test -race -count=3 ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotetransport	4.570s
```

## Forward compatibility

### `rc-piagent-bind`

The cycle-6 deliverable names `rc-piagent-bind` as the next
cycle after `rc-handle-user-interaction`: a PiG-backed
`StreamingMessageBackend` implementation that calls
`sink.OnUserInteraction(cascade, intr)` on PiG's
`permission_required` event. The shape this cycle ships
(`enqueueFrame` as the shared sender-side helper) is what
`rc-piagent-bind`'s notifications will use. The cycle-6
forward entry stays valid; this cycle just closes the
"transport-level gap" the entry identified.

A future `rc-piagent-bind` test will fire PiG's permission
prompt on one goroutine; the dashboard's
`HandleRemoteAgentCascadeUserInteraction` will arrive on the
same ws; the dispatcher's `Resolve` will unblock the parked
turn; the turn will continue. The transport's bi-directional
shape is what makes that possible. No additional transport
work is required.

### `rc-backpressure`

The cycle-5 deliverable named a future `rc-backpressure` cycle
for replacing the `ConnNotifierSink`'s inline write with a
buffered variant. This cycle does **not** replace the sink
(it can't — the sink is in `internal/remotecontrol`, and
changing its surface would ripple through every test that
constructs one). What this cycle DOES do is move the
**transport-level** backpressure seam to the right place: the
bounded per-conn `outbound` channel with a `dropped` counter
that future cycles can surface as a status field.

A future `rc-backpressure` cycle can now drop in a more
sophisticated policy (e.g., priority lanes — drop `state`
notifications before `user_interaction_required` notifications;
or per-cascade quotas) by replacing `enqueueFrame`'s overflow
branch. The `outboundMu` + `closing` + `outbound` shape stays.

### `rc-cancel-on-ws-close`

The cycle-5 deliverable named `rc-cancel-on-ws-close` as a
future cycle that hooks handleWS's per-conn ctx cancellation
into a cascade-level cancel that aborts the in-flight backend
turn. With this cycle's refactor, the per-conn `readCtx` is
cancelled in the deferred teardown; an `rc-cancel-on-ws-close`
cycle can wire that to `(*Cascade).MarkClosed` +
`(*PendingInteractions).Expire(now)` and the cycle-6 future
entry point for `rc-cancel-cascade` falls into place
automatically. No additional transport work is required.

### `rc-final-parity-verify`

The cycle-6 forward entry at line 993 was:

> | `rc-final-parity-verify` | Every method this cycle ships is exercised end-to-end at this layer: a Python ws client dials, calls Start → install-stub-with-Interactions → Send → asserts notification envelope → calls HandleRemoteAgentCascadeUserInteraction → asserts `{ok, decided_at, applied_to: []}` result. The unresolved wire round-trip stays in the brief's "split read + dispatch goroutines" follow-up. |

This cycle closes the "unresolved wire round-trip" gap. The
new `TestHandleRemoteAgentCascadeUserInteraction_BiDirectional_WS`
is exactly the `rc-final-parity-verify` happy-path shape — a
client dials, calls Start, installs a parked stub, sends a
message, reads the notification, answers the interaction,
asserts both envelopes — now at the `tcp_test.go` level
rather than deferred to a future cycle.

## 硬约束 / limitations

1. **The bounded-channel overflow is a hard drop, not a back-off.**
   A sender that finds the buffer full gets `ErrBackpressure`
   and the frame is lost. The current dispatcher and stub
   backend don't check the return value of `enqueueFrame` (the
   dispatch goroutine uses `_ = wc.enqueueFrame(...)` and the
   cycle-5 `ConnNotifierSink` ignores notification errors).
   This is fine for a single-dashboard deployment (a healthy
   client never fills the 64-frame buffer), but a future
   `rc-multi-dashboard` cycle that fans out notifications to
   many conns simultaneously may need either (a) a larger cap,
   (b) a per-cascade quota, or (c) explicit sender-side
   back-off. The `dropped` counter is the metric a status tool
   would surface to detect this case in production.

2. **`StubBackend.Answers` is still declared but not consulted.**
   The cycle-6 deliverable's forward entry `rc-stub-config`
   (line 989) still applies: a future cycle can drive the stub
   with declarative `interaction_id → response` tables. The
   cycle intentionally does NOT touch `emitInteractions` — the
   bi-directional test drives the parked stub via a follow-up
   `Handle…UserInteraction` RPC, which is what production
   dashboards will do, so the stub's `Answers` consumption is
   not on the critical path.

3. **One dispatch goroutine per request — no upper bound.**
   The reader spawns a goroutine for every incoming frame with
   no pool. A pathological client that sends 10,000 requests
   without reading responses spawns 10,000 goroutines. The
   `outbound` channel backpressure (frames that can't be
   enqueued are dropped) prevents the writer from
   memory-bloating, but the dispatch goroutines themselves
   are unbounded. A future `rc-concurrency-budget` cycle can
   add a semaphore to bound them; for the single-dashboard
   case the brief assumes, this is not a real concern.

4. **The writer's `c.Writer(ctx, ...)` writes one frame per
   ws message without batching.** Two frames enqueued back-to-
   back become two ws messages with no coalescing. The
   `coder/websocket` library does not expose a batch-write
   API; a future cycle that wants to coalesce frames would
   need to switch to a different ws library or implement
   batching at the application layer (the dispatcher would
   batch `ConnNotifierSink` events into a single notification).
   The current shape matches what the agy reference does.

5. **The close coordination depends on `outboundMu` for
   liveness under load.** A sender that already holds
   `outboundMu` and is mid-send will block the close
   `close(outbound)` step. With dispatch goroutines that
   complete quickly (the typical case — most dispatch returns
   within microseconds), this is not an issue. A pathological
   slow backend could hold `outboundMu` indefinitely; the
   cycle pins `enqueueFrame`'s lock-acquisition time as
   bounded by the non-blocking send + the closing pre-check
   (both sub-microsecond), so the realistic worst case is
   the close path waiting for one in-flight dispatch to
   enqueue its frame, then proceeding.

6. **`internal/remotetransport/tcp_test.go` now ships 67
   `Test*` functions (+6 from this cycle).** The package's
   `-race` test runtime is ~2.7s; cycle-6 was ~2.5s. The
   ~10% runtime bump is from the 6 new tests, none of which
   are individually slow (the longest is `TestReaderExit_StopsWriter`
   at 50ms — the stub's Sleep backend is set but not waited on;
   Shutdown returns as soon as the writer exits, which is
   immediate after the client close).
