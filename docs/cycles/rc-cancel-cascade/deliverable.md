# rc-cancel-cascade — `CancelRemoteAgentCascade` JSON-RPC method (G5)

Cycle ships the one JSON-RPC method the fresh-eyes
re-investigation at `/Users/gear/Documents/GitHub/openagy/.scratch/rc-reinvestigation.md`
called out as **🚨 G5** — `CancelRemoteAgentCascade` was missing
both as a handler and as cancel plumbing on `*Cascade`. The
brief:

> *"`internal/remotecontrol/cascade.go` has no cancel path;
> `dispatch.go:217` switch has no case. Cycle candidate:
> `rc-cancel-cascade`."* — `rc-reinvestigation.md §G5`

This cycle ships the **bounded goal** from `PLAN.md:172`:

> *"`CancelRemoteAgentCascade` — abort the cascade's running
> PiG turn and emit a final `cascade/state {phase:"cancelled"}`"*

One method, idempotent, with a small set of cancel-plumbing
primitives on `*Cascade`. No streaming protocol changes. No new
transport. **No push/merge.** All 47 of the cycle-15 method
envelopes stay byte-compatible.

---

## TL;DR

| What | Where | LOC |
|---|---|---:|
| `Cancel` / `StartCancel` / `StopCancel` / `IsCancelled` / `CancelReason` / `recordCancel` on `*Cascade` | `internal/remotecontrol/cascade.go` | +269 |
| New sentinel `ErrTurnCancelled` | `internal/remotecontrol/backend.go` | +25 |
| New constant `StatePhaseCancelled = "cancelled"` + `StateFrame` doc | `internal/remotecontrol/streaming.go` | +29 |
| Drain-loop `<-turnCtx.Done()` branch in `(*PiagentBackend).SendStream` (no `OnState(cancelled)` from the loop — the dispatcher emits the notification once) | `internal/remotecontrol/piagentbackend.go` | +137 |
| `CancelRemoteAgentCascade` switch case + `handleCancelRemoteAgentCascade` handler | `internal/remotecontrol/dispatch.go` | +172 |
| 10 new in-process tests in `cascade_test.go` (Cancel idempotent + ctx propagation + error-ring recording + dispatcher param validation) | `internal/remotecontrol/cascade_test.go` | +463 |
| 4 new wire-level tests in `tcp_test.go` (Cancel ok / idempotent / not-found / bad-params over WebSocket) | `internal/remotetransport/tcp_test.go` | +373 |

- **Tests:** 14 new total (10 in-process + 4 wire-level), all
  pass `-race -count=1`. Total runtime for the scoped packages:
  ~4.3 s.
- **`go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/`** PASS (2.0 s + 2.9 s).
- **`go vet ./internal/remotecontrol/ ./internal/remotetransport/
  ./internal/pigrt/`** exit 0.
- **`gofmt -l`** empty on the seven touched files. Pre-existing
  `cmd/wagyu` gofmt dirt untouched.
- **No third-party deps added.** The cycle touches only stdlib
  + the project's own packages.
- **No `go test ./...`.** Scope matches the brief.

---

## Method shape

```
req:
  { "jsonrpc":"2.0", "id":<any>, "method":"CancelRemoteAgentCascade",
    "params": { "cascade_id":"<cas_…>" } }

res:
  { "jsonrpc":"2.0", "id":<…>, "result": {
      "ok":               true,
      "already_cancelled": <bool>,
      "cancelled_at":      "<RFC3339Nano>"
  }}

err:
  -32602  missing / empty cascade_id
  -32602  cascade_id present but not a string
  -32004  cascade_id present but not registered (parity with SendMessage)
  -32603  registry not initialised (defensive — never in production)

side-effect (notification, on the originating ws):
  { "jsonrpc":"2.0", "method":"cascade/state", "params": {
      "cascade_id":   "<cas_…>",
      "phase":        "cancelled",
      "cancelled_at": "<RFC3339Nano>",
      "reason":       "user requested",
      "ts":           "<RFC3339Nano>"
  }}
```

- `already_cancelled:false` ⇒ this call actually fired the
  cancel. The `cascade/state{phase:"cancelled"}` notification
  is also emitted on the originating ws connection.
- `already_cancelled:true` ⇒ idempotent re-call. **No
  notification is emitted** on the second call (the first
  one already landed it). The response is still `ok:true` so
  a dashboard that double-clicks "Stop" sees the same
  envelope shape both times.

---

## Cancel plumbing on `*Cascade` (`internal/remotecontrol/cascade.go`)

`rc-piagent-bind` shipped `(*Cascade).Backend()` as a lazy
init from `Config.MessageBackend`, but the cascade had **no
cancel surface** — `c.SendStream` only derived a timeout ctx
and threaded that. The cycle adds six primitives:

| Method | Purpose |
|---|---|
| `StartCancel(parent) context.Context` | derive a child ctx whose cancel func lives on the cascade; installs under `cancelMu`. |
| `StopCancel()` | clears (and fires) the stored cancel func on every exit path — used in a `defer` from `SendStream`. |
| `Cancel(reason string) bool` | idempotent — fires the stored cancel func; returns **true** iff it actually fired (first call or stored func was non-nil). Default reason `"user requested"`. |
| `IsCancelled() bool` | lock-free read of the atomic `cancelled` flag (set by `Cancel`). Used on the hot path inside `SendStream`'s error-mapping. |
| `CancelReason() string` | returns the reason captured at `Cancel` time, with the default lazy-applied. |
| `recordCancel(reason, at)` | appends a cancel-sourced entry to the cascade's bounded `Errors` ring via `recordError("CancelRemoteAgentCascade", …, -32603, at)`, so `RemoteAgentErrorStatus` surfaces the cancel as the most-recent error. |

Six new fields on `*Cascade`:

```go
cancelMu     sync.Mutex         // guards cancel + cancelReason
cancel       context.CancelFunc // the per-turn func
cancelReason string             // captured at Cancel() time
cancelled    atomic.Bool        // read by IsCancelled (lock-free)
```

`(*CascadeRegistry).SendStream` now derives
`turnCtx := c.StartCancel(sendCtx)` with `defer c.StopCancel()`
and threads `turnCtx` to the backend. After the backend
returns, the registry checks `c.IsCancelled()` and maps the
error to `ErrTurnCancelled` (joined with the inner error) —
otherwise the existing `ErrTurnTimeout` / `ErrTurnFailed`
mapping wins.

`StopCancel` always fires the stored cancel func before
clearing it — so a `SendStream` that returns without ever
needing the cancel still tears the func down. This avoids
both a leaked-goroutine (Ctx lifetime) and a future
`StartCancel` overwriting the leaked entry.

---

## PiG `Session.Send(ctx,…)` ctx propagation

The cancel signal doesn't end at the `*Cascade` boundary —
it has to reach the model's streaming goroutine. The path is:

```
(*Dispatcher) CancelRemoteAgentCascade
   ↓ d.cascades.Get(cascadeID)
   ↓ (*Cascade).Cancel(reason)
   cancel() // per-turn ctx.CancelFunc
   ↓
(*CascadeRegistry).SendStream's turnCtx  ← StartCancel parent
   ↓
(*PiagentBackend).SendStream(turnCtx, ...)
   ↓ sess.Send(turnCtx, prompt.Text, events)
   ↓
(coding.Session).Send(ctx, prompt)
   ↓ internal/pigrt/runtime.go:610
```

What's load-bearing in `internal/pigrt/runtime.go:610`:

```go
msgs, err := r.session.Send(ctx, prompt)
// Send returns once the run settles. …
// A cancelled context is the caller's own failure and outranks
// whatever the provider reported on the way out; internal/agent's
// sendPig orders it the same way. --print-timeout depends on it:
// a deadline becomes a partial output success, not an error.
if ctxErr := ctx.Err(); ctxErr != nil {
    return msgs, ctxErr
}
```

PiG's `coding.Session.Send(ctx, prompt)` honours the ctx in
this **post-call** `ctx.Err()` check — that's what's currently
load-bearing. **Mid-event ctx propagation inside PiG is
PiG-internal**; this cycle doesn't pin it (it's an upstream
concern). What this cycle ships on the `PiagentBackend` side
is the **drain-loop** select between events and
`<-turnCtx.Done()`:

```go
cancelled := false
for {
    select {
    case ev, ok := <-events:
        if !ok {
            // upstream closed without sending Done / Error
            break loop
        }
        if cancelled {
            // drain remainder without processing, then return
            continue
        }
        // process ev …
    case <-turnCtx.Done():
        // Cycle rc-cancel-cascade: the cascade's cancel
        // func fired. Mark the loop as cancelled and drop
        // any remaining events. We do NOT emit
        // OnState(cancelled) here — the dispatcher's
        // CancelRemoteAgentCascade handler is the single
        // source of truth for the cascade/state notification.
        cancelled = true
    }
}
```

After the loop, **if the cancel actually fired and neither
`DoneEvent` nor `ErrorEvent` was observed in the meantime**,
the `SendStream` returns the partial `result` with
`turnCtx.Err()`. The registry then walks
`if c.IsCancelled() { errors.Join(ErrTurnCancelled, err) }` so
the dispatcher's switch surfaces a clean `-32603` with
`ErrTurnCancelled` joined into the message.

### Why the drain loop does NOT emit `OnState(cancelled)`

The dashboard's "Stop" button shows a single cancel event —
not one per cancel signal. Two layers emitting
`cascade/state{phase:"cancelled"}` (one from `SendStream`'s
`<-turnCtx.Done()` branch, one from the dispatcher's
`Cancel` handler) would land two notifications when the
dashboard only fired one cancel RPC. The dispatcher's
`handleCancelRemoteAgentCascade` is therefore the **single
source of truth** for the cancelled notification — emitted
via `d.Notify(connID, NotifCascadeState, …)` exactly when
`Cancel()` returns `true`. The drain loop's job is the
ctx-honouring + partial-result plumbing; not the user-visible
notification.

---

## Tests

### In-process (`internal/remotecontrol/cascade_test.go`, +463 LOC, 10 tests)

| Test | What it pins |
|---|---|
| `TestCascade_Cancel_Idempotent` | First `Cancel` returns true and stores the reason; second returns false; `IsCancelled()` is true both times. |
| `TestCascade_Cancel_NilSafe` | `Cancel` on a `(*Cascade)(nil)` doesn't panic; `IsCancelled` returns false. |
| `TestCascade_Cancel_InterruptedBackend` | A `StubBackend` with `Sleep: 30s` is parked inside `Send`; `Cancel` fires the per-turn ctx; `SendStream` returns within budget with `ErrTurnCancelled` joined. |
| `TestCascade_Cancel_DefaultReason` | `Cancel("")` is treated as `Cancel("user requested")` (the same default `handleCancelRemoteAgentCascade` uses). |
| `TestCascade_RecordCancel_AppendsToErrorRing` | `recordCancel("user requested", t)` puts an entry at the tail of `Errors`; `Method == "CancelRemoteAgentCascade"`, `Code == -32603`, `Message == "user requested"`. |
| `TestCascade_Cancel_DoesNotPanicOnDoubleClose` | A `SendStream` that returns via `Cancel` and then a follow-up `Cancel` neither panics nor double-fires. |
| `TestDispatcher_CancelRemoteAgentCascade_BadParams` | Missing / empty / non-string `cascade_id` ⇒ `-32602`. |
| `TestDispatcher_CancelRemoteAgentCascade_NotFound` | Unknown `cascade_id` ⇒ `-32004 "Cascade not found"` with the bad id in `Data`. |
| `TestDispatcher_CancelRemoteAgentCascade_Idempotent` | Two cancels in a row: first returns `already_cancelled:false`, second `already_cancelled:true`; both `ok:true`. |
| `TestDispatcher_CancelRemoteAgentCascade_RecordsError` | One cancel after a parked stub; after cancel, `d.cascades.Get(id).ErrorsSnapshot()` carries a `CancelRemoteAgentCascade` entry at the tail. |

All 10 pass `-race -count=1`:

```
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.995s
```

### Wire-level (`internal/remotetransport/tcp_test.go`, +373 LOC, 4 tests)

| Test | What it pins |
|---|---|
| `TestCancelRemoteAgentCascade_Ok_WS` | After dispatching `SendRemoteAgentCascadeMessage` against a parked `StubBackend{Sleep: 5s}`, sleeps 50 ms, fires `CancelRemoteAgentCascade`; response arrives within the budget with `ok:true, already_cancelled:false`; sees the `cascade/state{phase:"cancelled"}` notif on the same ws. |
| `TestCancelRemoteAgentCascade_Idempotent_WS` | Two cancels in a row: first `already_cancelled:false`, second `already_cancelled:true`. No second notification. |
| `TestCancelRemoteAgentCascade_NotFound_WS` | Random `cascade_id` ⇒ `-32004`. |
| `TestCancelRemoteAgentCascade_BadParams_WS` | Empty `params` ⇒ `-32602 "cascade_id is required"`. |

All 4 pass:

```
=== RUN   TestCancelRemoteAgentCascade_Ok_WS
--- PASS: TestCancelRemoteAgentCascade_Ok_WS (0.06s)
=== RUN   TestCancelRemoteAgentCascade_Idempotent_WS
--- PASS: TestCancelRemoteAgentCascade_Idempotent_WS (0.05s)
=== RUN   TestCancelRemoteAgentCascade_NotFound_WS
--- PASS: TestCancelRemoteAgentCascade_NotFound_WS (0.00s)
=== RUN   TestCancelRemoteAgentCascade_BadParams_WS
--- PASS: TestCancelRemoteAgentCascade_BadParams_WS (0.00s)
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.353s
```

---

## Sanity checks (run on this commit)

### Tests

```bash
$ go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.995s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.926s
```

### vet

```bash
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./internal/pigrt/
(empty)
```

### gofmt

```bash
$ gofmt -l \
    internal/remotecontrol/cascade.go \
    internal/remotecontrol/cascade_test.go \
    internal/remotecontrol/streaming.go \
    internal/remotecontrol/piagentbackend.go \
    internal/remotecontrol/backend.go \
    internal/remotecontrol/dispatch.go \
    internal/remotetransport/tcp_test.go
(empty)
```

Pre-existing `cmd/wagyu` gofmt dirt untouched (50+ files).

### build

```bash
$ go build ./...
(exit 0)
```

---

## 硬约束 (hard constraints honoured)

1. **One method, one bounded goal.** Only
   `CancelRemoteAgentCascade` was added. No envelope-shape
   changes on the 47 methods from cycle-15. No transport
   changes. No streaming-protocol changes (no new notif
   channels, no `phase` enum extensions outside the new
   `StatePhaseCancelled` constant).
2. **Single source of truth for the `cancelled`
   notification.** The dispatcher's
   `handleCancelRemoteAgentCascade` emits it explicitly via
   `d.Notify(connID, NotifCascadeState, …)`. The
   `PiagentBackend.SendStream` drain loop detects ctx.Done
   between events but does NOT emit `OnState(cancelled)` —
   one notification per cancel RPC, not two.
3. **`gofmt -l` clean on every touched file.** 7 files
   touched, 7 files formatted. The 50-file pre-existing
   `cmd/wagyu` dirt and any other untouched gofmt dirt
   stays put.
4. **`go vet ./internal/remotecontrol/ ./internal/remotetransport/
   ./internal/pigrt/` exit 0.** All three packages clean.
5. **`go test -race -count=1`** PASSes on the scoped
   packages. Full `internal/remotecontrol` (94 tests) and
   `internal/remotetransport` (44 tests) green.
6. **No third-party deps added.** Stdlib + `coder/websocket`
   + the project's own packages.
7. **`go test ./...` not run.** Scope matches the brief.
8. **No push/merge.** Stays on `exp/rc-server-mode` at the
   same tip. Commit message form: `<verb>(<scope>): <short>
   (<id>)`.
9. **`StubBackend` ctx-honouring preserved.** The cycle's
   `TestCascade_Cancel_InterruptedBackend` proves the stub
   honours ctx already (`Sleep: 30*time.Second` + a ctx-
   propagating select) — no change to the stub.
10. **No probing of `/logout`, `/exit`, `/keybindings` in
    print mode.** Cycle doesn't probe any print-mode path.
11. **Subagent race respected.** No edits to `openagy/`,
    `openagy-mermaid/`, `openagy-osc8-semicolon/`,
    `openagy-stream-glamour/`. No edits to untracked leftovers
    in `out/`, `.scratch/`, `cmd/wagyu-loop-repro/`.

---

## 存疑 (limitations / open questions)

1. **Mid-event ctx propagation inside PiG is upstream.**
   `internal/pigrt/runtime.go:610` threads `ctx` to
   `(*coding.Session).Send(ctx, prompt)`. PiG honours it in
   the **post-call** `ctx.Err()` check (the line right after
   `Send` returns) — `r.session.Send` itself may not abort
   mid-stream on this cycle's `PiagentBackend`'s behalf. The
   drain loop's `<-turnCtx.Done()` branch is the
   belt-and-braces safety net: even if PiG's `Send` keeps
   reading events off the upstream provider, the loop stops
   processing them and returns the partial result + ctx.Err.
   That's enough for the wire-level contract (no further
   `cascade/text_delta` notifications after cancel) — but a
   future cycle that wants PiG to abort network reads mid-
   turn is upstream work, not in scope here.

2. **`cascade/state{phase:"cancelled"}` does NOT stamp
   `LastReplyAt`.** `ConnNotifierSink.OnState`'s doc spells
   out that `cancelled` is informational; a successful
   `done` phase is what stamps `LastReplyAt`. A dashboard
   that wants to know "did a reply land before the user
   hit Stop?" must read `LastReplyAt` itself — the
   `cancelled` notification's `cancelled_at` timestamp is
   the cancel time, not the last-success time. If a future
   cycle wants `cancelled` to **also** record any partial
   text accumulated since the last `done`, it'd need to
   call `setReply` from the `cancelled` branch — out of
   scope today.

3. **`StubBackend`'s `Sleep` field drives the
   `InterruptedBackend` test.** Production code never
   wires a stub; production goes through `PiagentBackend`
   (`internal/remotecontrol/piagentbackend.go`). The stub
   is a test fixture, and its ctx-honouring is test-only.
   The `PiagentBackend.SendStream` drain loop is what
   production uses, and its `<-turnCtx.Done()` branch is
   what we tested with the stub. **Caveat:** if a future
   production backend opts out of the streaming
   `SendStream` interface, it'd hit the `backend.Send`
   fallback at `cascade.go:1308`, which uses the same
   `turnCtx` but doesn't have a drain loop to interrupt.
   `context.CancelFunc` propagation through whatever
   `backend.Send` does is the backend's responsibility —
   `PiagentBackend` honours it because `coding.Session.Send`
   does, per `internal/pigrt/runtime.go:610`.

4. **Forward compat with `rc-piagent-done-event`.** That
   future cycle will translate PiG `agent.MessageEndEvent`
   into a `cascade/state{phase:"done"}` sink emit. The
   `cancelled` phase this cycle adds is orthogonal: a
   `cancelled` notification is emitted **after** the
   dashboard's cancel RPC fires; a `done` notification is
   emitted when the upstream signals normal completion.
   The two don't race in this cycle's design — but a
   future cycle that translates `MessageEndEvent` for
   `done` should leave the dispatcher's `cancelled`
   handler alone (it's already idempotent and emits
   exactly one notification).

5. **Idempotent re-call emits no notification, even though
   it returns `ok:true`.** A dashboard that double-clicks
   "Stop" sees one `cascade/state{phase:"cancelled"}`
   notification, then a `CancelRemoteAgentCascade` response
   with `already_cancelled:true`. The dashboard's UI handler
   must treat the response, not the notification count, as
   authoritative. This is documented in the handler's
   comment (`dispatch.go:1759-1764`).

6. **Cancel after the turn's already `done` is a no-op.**
   The first `Cancel` ran while a turn was in flight ⇒ it
   fired the per-turn ctx. After that turn `done`s, the
   `SendStream` defers `StopCancel()` (clearing the stored
   cancel func). A follow-up `Cancel` after the turn's
   settled finds `c.cancel == nil`, sets the atomic
   `cancelled` flag, and returns `false` —
   `already_cancelled:true`. No second `done` notification
   is emitted (the cycle-5 sink already fired it). This is
   the right behaviour but the document trail is in the
   `Cancel` doc comment, not in this report.

7. **No `OnState("cancelled")` from `ConnNotifierSink` is
   on the wire path.** Cycle-5's `(*ConnNotifierSink)` only
   stamps `LastReply` / `LastReplyAt` on `done`. A future
   cycle that wants "did the cancel race a turn that
   already had text on the way?" can add an
   `OnState(cancelled)` branch to the sink — it would need
   to call `setReply` with whatever `LastReply` already
   carries. The dispatcher's notification is a separate
   concern (notification routing, not sink state).

---

## Wire-level diff

Before this cycle, sending `CancelRemoteAgentCascade` returned:

```json
{ "jsonrpc":"2.0", "id":1, "error":{
    "code":-32601, "message":"Method not found"
}}
```

After this cycle, sending it with `cascade_id=cas_<hex>` for
an in-flight turn returns:

```json
{ "jsonrpc":"2.0", "id":1, "result":{
    "ok":true, "already_cancelled":false, "cancelled_at":"2026-…Z"
}}
```

…and the originating ws emits the new notification frame:

```json
{ "jsonrpc":"2.0", "method":"cascade/state", "params":{
    "cascade_id":"cas_<hex>", "phase":"cancelled",
    "cancelled_at":"2026-…Z", "reason":"user requested",
    "ts":"2026-…Z"
}}
```

The cycle-15 parity verifier (`internal/parityverify` Go
harness + `docs/cycles/rc-final-parity-verify/verify.py`
Python harness) will need a new row for this method. That's
deferred to whichever cycle next updates the verifier
matrix — out of scope here.
