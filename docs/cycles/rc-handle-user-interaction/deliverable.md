# Cycle `rc-handle-user-interaction` — `HandleRemoteAgentCascadeUserInteraction` + `cascade/user_interaction_required` (bi-directional RPC)

## TL;DR / 摘要

Cycles 1–5 wired the transport, dispatcher, and streaming surface
(rc-ws-transport `46b348b` →
rc-cascade-stream `fbd8dec`). This cycle adds the
**bi-directional half of the prompt flow**: the server can now
hold a turn blocked on a user decision (a permission prompt, a
multiple-choice question, a yes/no confirmation), surface that
prompt to the dashboard as a server-push notification, and
unblock when the dashboard answers over the same WS connection.

Two new wire artefacts:

1. **Server-push notification `cascade/user_interaction_required`**
   — `(*ConnNotifierSink).OnUserInteraction(...)` emits it when a
   streaming backend parks a turn on a permission prompt. Carries
   `cascade_id`, `interaction_id`, `kind`, `title`, `prompt`, plus
   optional `options` / `dangerous` / `expires_at`.
2. **JSON-RPC method `HandleRemoteAgentCascadeUserInteraction`** —
   the dashboard calls it back with `cascade_id` + `interaction_id`
   + `response`; the dispatcher looks up the cascade (-32004) →
   the interaction (-32006) → resolves the pending entry, which
   closes the backend's `ResultCh` and lets the turn continue.

The cycle ships a `StubBackend` extension (a `Interactions []PendingInteractionConfig`
queue and an `InteractionWait` timeout) so the whole path is
exercisable in tests without a real PiG session. The PiG-side
plumbing (wiring `(*pigrt.Runtime)` to call
`sink.OnUserInteraction` on `permission_required`) lands in the
next cycle, `rc-piagent-bind`; this cycle's `StubBackend` makes
the contract exact and pinned.

| Piece | Where |
|---|---|
| `PendingInteraction` / `InteractionOption` / `InteractionResult` / `PendingInteractions` (Add/Get/Resolve/Expire/Len, all nil-safe) + sentinel `ErrInteractionNotFound` + `mintInteractionID` | `internal/remotecontrol/pending.go` (new, ~340 lines) |
| `InteractionKind*` constants (`permission` / `question` / `confirmation` / `choice`) | `internal/remotecontrol/pending.go` |
| `Cascade.pending *PendingInteractions` (lazy-allocated on first `Pending()` call); `(*Cascade).Pending() *PendingInteractions`; `(*Cascade).AddInteraction(kind, title, prompt, opts) *PendingInteraction, error`; package-level cycle-6 doc update in the `cascade.go` preamble | `internal/remotecontrol/cascade.go` (+~104) |
| `EventSink.OnUserInteraction(cascade, intr)`; `NotifUserInteractionRequired = "cascade/user_interaction_required"`; `(*ConnNotifierSink).OnUserInteraction` (does the registry `Add` + notification `Notify` in one shot, after computing the `interaction_id`); `userInteractionRequiredParams` type | `internal/remotecontrol/streaming.go` (+~115) |
| `ErrCascadeInteractionTimeout` sentinel; `ErrCodeInteractionNotFound = -32006`; `StubBackend.Interactions []PendingInteractionConfig` + `Answers map[string]string` + `InteractionWait time.Duration` (default 200 ms); `PendingInteractionConfig` type; `(*StubBackend).emitInteractions` helper (mints an id, registers into the cascade's pending registry, calls the sink); `SendStream` walks `Interactions` before any text/tool emission | `internal/remotecontrol/backend.go` (+~197) |
| `case "HandleRemoteAgentCascadeUserInteraction"` in `Dispatch`; `handleRemoteAgentCascadeUserInteraction` handler (cascade lookup → interaction lookup → Resolve → `{ok, decided_at, applied_to: []}`); `handleRemoteAgentCascadeUserInteractionParams` type | `internal/remotecontrol/dispatch.go` (+~134) |
| 13 new in-process tests (20 with subtests) covering the registry, the cascade accessor, the dispatcher handler, the wire-emitted envelope, expiry, and the stub timeout path | `internal/remotecontrol/pending_test.go` (new, ~640 lines) |
| 3 new wire-level tests (`cascade/user_interaction_required` over the wire envelope; -32004; -32006) — see "Wire-level coverage" below for why the round-trip is not exercised through `handleWS` | `internal/remotetransport/tcp_test.go` (+~206) |
| Existing `captureSink` test fixture now implements `OnUserInteraction` (captures the pointer; doesn't crash on nil); `TestConnNotifierSink_NilSafe` covers all five `OnXxx` methods on a nil receiver + nil cascade | `internal/remotecontrol/cascade_test.go` (+~31 / -3: the existing `captureSink` fixture grows one field + one method; `TestConnNotifierSink_NilSafe` grows one call) |

What lands for forward consumers: a `PendingInteraction` shape
the streaming sink can construct and pass; a `Pending()` accessor
on the cascade that holds the registry; an `EventSink.OnUserInteraction`
hook real backends (`pigrt.Runtime` in `rc-piagent-bind`) call
when they park a turn. The dispatcher's new method is the contract
the dashboard implements; error envelopes `-32602 / -32004 / -32006`
are the same shape every `rc-*` cycle emits.

## Wire protocol added by this cycle

### Server-push notification: `cascade/user_interaction_required`

A JSON-RPC notification (no `id`, no response expected on the wire
itself — the answer rides on a separate `HandleRemoteAgentCascadeUserInteraction`
call). Server emits it from `(*ConnNotifierSink).OnUserInteraction` when
called by the streaming backend:

```json
{
  "jsonrpc": "2.0",
  "method": "cascade/user_interaction_required",
  "params": {
    "cascade_id":     "cas_a3f1...",
    "interaction_id": "int_3f7a92c4b1e0",
    "kind":           "permission",
    "title":          "Allow file edit?",
    "prompt":         "Edit /etc/hosts to add the following entry?",
    "options": [
      {"label": "Approve", "value": "approve", "description": "Allow once"},
      {"label": "Deny",    "value": "deny"}
    ],
    "dangerous":   false,
    "expires_at": "2026-10-19T12:34:56.789Z"
  }
}
```

All top-level `params` fields except `expires_at` are JSON-string
typed (must be present, non-empty). `options` is an array (may be
omitted by encoding as missing — `omitempty` on the `Options`
field of the notification type). `dangerous` is a `bool`.
`expires_at` is an RFC 3339 timestamp; the zero value
(`0001-01-01T00:00:00Z`) is omitted over the wire via
`omitempty`-on-pointer + nil-check (treated as "no deadline").

### Method: `HandleRemoteAgentCascadeUserInteraction`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "HandleRemoteAgentCascadeUserInteraction",
  "params": {
    "cascade_id":     "cas_a3f1...",
    "interaction_id": "int_3f7a92c4b1e0",
    "response":       "approve"
  }
}
```

`response` is `json.RawMessage` over the wire — the dashboard
sends whatever shape it wants (a bare string for `yes / no` /
`approve`, an object for `{choice_id, ...}` or
`{approve, remember}`). The dispatcher does NOT decode it; it
forwards verbatim onto the backend's `ResultCh`.

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": {
    "ok":         true,
    "decided_at": "2026-10-19T12:34:57.012Z",
    "applied_to": []
  }
}
```

`applied_to` is always `[]` (empty array) — a placeholder for
the rc-piagent-bind cycle to populate with the tool identifiers
the decision propagated to (e.g. `["tool:edit_file"]` when a
permission grant cascaded forward to subsequent tool calls on
the same turn). The dispatcher code comment pins the shape as
`["tool:<name>", ...]`; the current cycle returns `[]` because
there is no PiG yet; the field exists so the wire envelope is
stable across cycles and dashboards don't have to defend
against "field sometimes missing". `ok` is always `true` on the
success path; any non-true value is unreachable today.

Errors:

| Code | When | Surface path |
|---|---|---|
| `-32602 Invalid params` | `cascade_id` missing / empty / whitespace-only; `interaction_id` missing / empty; `response` field absent OR `null` | `handleRemoteAgentCascadeUserInteraction` returning `-32602` |
| `-32004 Cascade not found` | `cascade_id` is well-formed but no cascade is registered | `d.cascades.Get` returning `(nil, false)` |
| `-32006 Interaction not found` | the cascade is real, but no interaction is registered under `interaction_id` (expired, already resolved, or never existed) | `c.Pending().Get(intrID)` returning `(nil, false)` OR `Resolve` returning `ErrInteractionNotFound` |
| `-32603 Internal error` | Sink / registry panic recovered; maps to the dispatcher's default branch | the handler's `defer recover()` |

The custom code range `-32000 .. -32099` is reserved per the brief.
This cycle consumes `-32006`. The codes live in
`internal/remotecontrol/backend.go` as named constants so a future
cycle can import them without redeclaring.

## What changed

### `internal/remotecontrol/pending.go` (new, ~340 lines)

The data — every type, sentinel, and helper the rest of the package
needs to drive the interaction flow.

```go
const (
    InteractionKindPermission   = "permission"
    InteractionKindQuestion     = "question"
    InteractionKindConfirmation = "confirmation"
    InteractionKindChoice       = "choice"
)

type InteractionOption struct {
    Label       string `json:"label"`
    Value       string `json:"value"`
    Description string `json:"description,omitempty"`
}

type PendingInteraction struct {
    ID        string              // "int_<12 hex chars>"
    Kind      string
    Title     string
    Prompt    string
    Options   []InteractionOption `json:"options,omitempty"`
    Dangerous bool
    CreatedAt time.Time
    ExpiresAt time.Time

    ResultCh chan InteractionResult // buffered, len 1
    cascade  *Cascade               // back-reference
}

type InteractionResult struct {
    Response   json.RawMessage
    DecidedAt  time.Time
}

type PendingInteractions struct {
    mu sync.Mutex
    m  map[string]*PendingInteraction
}

var ErrInteractionNotFound = errors.New("remotecontrol: interaction not found")

func (p *PendingInteractions) Add(intr *PendingInteraction)
func (p *PendingInteractions) Get(id string) (*PendingInteraction, bool)
func (p *PendingInteractions) Resolve(id string, result InteractionResult) error
func (p *PendingInteractions) Expire(now time.Time) []string
func (p *PendingInteractions) Len() int
```

Every method is nil-safe (no-op on a nil receiver) so the
cascade-side accessor and the dispatcher's handler don't have
to nil-check. `Resolve` sends on the channel **outside** the
registry lock so a slow consumer (the typical state — the
backend is parked on `ResultCh`) doesn't serialise other
resolutions. `Resolve`'s contract is "first wins; subsequent
calls see `ErrInteractionNotFound`" — the brief's
`TestUserInteraction_ConcurrentResolves_NoDoubleAck` pins it.

`Expire` closes the channel and drops the entry; the zero
`InteractionResult` the receiver gets is the "expired"
signal (distinct from "user answered no" because the
backend's decoders can branch on the zero value of the
backend-side "was this user-decided?" flag).

Interaction IDs are minted via `crypto/rand` →
`"int_" + 6 hex bytes` (48 bits, ample for a per-cascade
space at human cadence). `mintInteractionID` is package-private;
callers (`StubBackend.emitInteractions`, `(*Cascade).AddInteraction`)
go through it.

### `internal/remotecontrol/cascade.go` (+~104)

Three additions; no behavioral change to the existing
Start/Send/Stream paths:

1. **Field**: `Cascade.pending *PendingInteractions` (lazy-allocated).

2. **Accessors**:

   ```go
   // Pending returns the per-cascade registry, allocating
   // it on the first call. Nil on a nil receiver.
   func (c *Cascade) Pending() *PendingInteractions

   // AddInteraction mints an interaction_id, constructs a
   // *PendingInteraction (with a buffered `chan InteractionResult,
   // 1` ResultCh + the back-reference `cascade: c`), registers
   // it in c.Pending(), and returns it so the caller can hand
   // it to sink.OnUserInteraction. expiresAt may be zero (no
   // deadline); now may be zero (defaults to time.Now().UTC()).
   func (c *Cascade) AddInteraction(
       kind, title, prompt string,
       opts []InteractionOption,
       dangerous bool,
       expiresAt, now time.Time,
   ) (*PendingInteraction, error)
   ```

   `(*Cascade)(nil).AddInteraction(...)` returns
   `(nil, errors.New("remotecontrol: AddInteraction on nil cascade"))`;
   `(*Cascade)(nil).Pending()` returns nil. The dispatcher
   always passes a non-nil cascade.

3. **Package preamble**: cycle-5 → cycle-6 doc-comment update listing
   the new file in the package overview.

`PendingInteractionConfig` is the typed-input shape the StubBackend's
queue (`StubBackend.Interactions []PendingInteractionConfig`)
consumes — the storage type `PendingInteraction` is what the
registry holds and what `AddInteraction` returns. The split keeps
the `cascade` back-reference and the `ResultCh` allocation
(out of the API a test / future real backend constructs from):
`AddInteraction` is the only path that creates a `PendingInteraction`
with a valid back-reference + result channel.

### `internal/remotecontrol/streaming.go` (+~115)

1. **`EventSink` interface grows `OnUserInteraction`**:

   ```go
   type EventSink interface {
       OnState(c *Cascade, f StateFrame)
       OnTextDelta(c *Cascade, d string)
       OnToolCall(c *Cascade, call ToolCall)
       OnError(c *Cascade, err error)
       OnUserInteraction(c *Cascade, intr *PendingInteraction) // NEW
   }
   ```

   Pre-existing test fixtures (`captureSink`, `silentSink`) gain a
   minimal nil-safe implementation. Adding the method is purely
   additive: a fixture that didn't need it could `panic("not
   implemented")` and prior tests would still pass — but every
   prior test loads `captureSink`, which is why the cycle adds
   the capture method too.

2. **Notification constant**:

   ```go
   const NotifUserInteractionRequired = "cascade/user_interaction_required"
   ```

3. **`(*ConnNotifierSink).OnUserInteraction`**:

   ```go
   func (s *ConnNotifierSink) OnUserInteraction(c *Cascade, intr *PendingInteraction) {
       if s == nil || c == nil || intr == nil {
           return
       }
       // Per brief, the sink does the registry Add (not the
       // backend) — the back-reference on intr.cascade was
       // already set by AddInteraction, so we don't need to
       // re-look-up.
       c.Pending().Add(intr)
       params := userInteractionRequiredParams{
           CascadeID:     c.ID,
           InteractionID: intr.ID,
           Kind:          intr.Kind,
           Title:         intr.Title,
           Prompt:        intr.Prompt,
           Options:       intr.Options,
           Dangerous:     intr.Dangerous,
           ExpiresAt:     intr.ExpiresAt, // RFC 3339; zero omitted
       }
       s.notify("cascade/user_interaction_required", params)
   }
   ```

The `userInteractionRequiredParams` type uses `omitempty` on `ExpiresAt`
via a nil-able `*time.Time` so the zero deadline drops out of the wire
envelope (not encoded as `"0001-01-01T00:00:00Z"`).

### `internal/remotecontrol/backend.go` (+~197)

The StubBackend extension + the new error code + a tiny
`pendingInteractionConfig` helper.

```go
const ErrCodeInteractionNotFound = -32006

var ErrCascadeInteractionTimeout = errors.New("remotecontrol: cascade interaction timeout")

type PendingInteractionConfig struct {
    Kind      string
    Title     string
    Prompt    string
    Options   []InteractionOption
    Dangerous bool
    ExpiresAt time.Time
}

type StubBackend struct {
    // ... existing fields ...
    Interactions    []PendingInteractionConfig // queue of prompts to emit
    Answers         map[string]string          // (declared, not actively read)
    InteractionWait time.Duration              // default 200ms when zero
}

func (s *StubBackend) emitInteractions(ctx context.Context, sink EventSink) (string, error) {
    if len(s.Interactions) == 0 {
        return "", nil
    }
    wait := s.InteractionWait
    if wait <= 0 {
        wait = 200 * time.Millisecond
    }
    for i := range s.Interactions {
        cfg := &s.Interactions[i]
        id, err := mintInteractionID()
        if err != nil {
            return "", err
        }
        intr := &PendingInteraction{
            ID:        id,
            Kind:      cfg.Kind,
            Title:     cfg.Title,
            Prompt:    cfg.Prompt,
            Options:   cfg.Options,
            Dangerous: cfg.Dangerous,
            CreatedAt: time.Now().UTC(),
            ExpiresAt: cfg.ExpiresAt,
            ResultCh:  make(chan InteractionResult, 1),
        }
        // The stub passes nil for cascade on purpose: the
        // sink owns its captured s.cascade (cycle-3/4
        // contract: the cascade passed to the sink's ctor
        // is the one the registry Add targets). The
        // back-reference on intr.cascade is intentionally
        // left zero because the stub path doesn't read it.
        if sink != nil {
            sink.OnUserInteraction(nil, intr)
        }
        t := time.NewTimer(wait)
        select {
        case <-intr.ResultCh:
            t.Stop()
        case <-t.C:
            return intr.ID, ErrCascadeInteractionTimeout
        case <-ctx.Done():
            t.Stop()
            return intr.ID, ctx.Err()
        }
    }
    return "", nil
}
```

The stub constructs the `*PendingInteraction` directly rather than
going through `(*Cascade).AddInteraction(...)` because the sink's
registry `Add` path uses `s.cascade` (the one captured at the sink's
ctor time) — not the cascade parameter passed to
`OnUserInteraction`, not the stub. The back-reference field on
`intr.cascade` is therefore never read by the stub path and can stay
zero. The `(*Cascade).AddInteraction` path is the one tests and
future `rc-piagent-bind` cycle use (it sets `intr.cascade = c`,
which the sink and registry don't need today).

`(*StubBackend).SendStream` now calls `emitInteractions` **before**
text / tool emission so a configured prompt is the first thing the
dashboard sees on a fresh turn:

```go
if _, err := s.emitInteractions(ctx, sink); err != nil {
    if sink != nil {
        sink.OnError(nil, err)
    }
    return MessageResult{}, err // ErrCascadeInteractionTimeout (or ctx.Err())
}
```

This is the cycle's load-bearing test seam: a test wires up a
`StubBackend{Interactions: [...]}`, dials the WS, asserts the
notification envelope appears, then dials the dispatcher handler
out-of-band (via `d.Dispatch(...)` directly — see "Wire-level
coverage" below) to resolve the pending interaction. The
`InteractionWait` field pins the failure path: with a 50 ms
timeout the stub gives up before the dispatcher has time to
respond, which is what the wire-level test asserts.

### `internal/remotecontrol/dispatch.go` (+~134)

1. **`Dispatch` switch grows one case**:

   ```go
   case "HandleRemoteAgentCascadeUserInteraction":
       return d.handleRemoteAgentCascadeUserInteraction(ctx, req.Params)
   ```

2. **The handler**:

   ```go
   func (d *Dispatcher) handleRemoteAgentCascadeUserInteraction(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
       var p struct {
           CascadeID     string          `json:"cascade_id"`
           InteractionID string          `json:"interaction_id"`
           Response      json.RawMessage `json:"response"`
       }
       if len(params) > 0 {
           if err := json.Unmarshal(params, &p); err != nil {
               return nil, &rpcErrorBody{Code: -32602, ...}
           }
       }
       if nonStringFieldInJSON(params, "cascade_id", p.CascadeID) ||
           nonStringFieldInJSON(params, "interaction_id", p.InteractionID) {
           return nil, &rpcErrorBody{Code: -32602, ..., Data: "string fields must be strings"}
       }
       cascadeID := strings.TrimSpace(p.CascadeID)
       interactionID := strings.TrimSpace(p.InteractionID)
       if cascadeID == "" {
           return nil, &rpcErrorBody{Code: -32602, ..., Data: "cascade_id is required"}
       }
       if interactionID == "" {
           return nil, &rpcErrorBody{Code: -32602, ..., Data: "interaction_id is required"}
       }
       if len(p.Response) == 0 || string(p.Response) == "null" {
           return nil, &rpcErrorBody{Code: -32602, ..., Data: "response is required"}
       }

       c, ok := d.cascades.Get(cascadeID)
       if !ok {
           return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, ..., Data: cascadeID}
       }
       pending := c.Pending()
       if _, ok := pending.Get(interactionID); !ok {
           return nil, &rpcErrorBody{Code: ErrCodeInteractionNotFound, ..., Data: interactionID}
       }
       now := time.Now().UTC()
       if err := pending.Resolve(interactionID, InteractionResult{Response: p.Response, DecidedAt: now}); err != nil {
           if errors.Is(err, ErrInteractionNotFound) {
               return nil, &rpcErrorBody{Code: ErrCodeInteractionNotFound, ..., Data: interactionID}
           }
           return nil, &rpcErrorBody{Code: -32603, ..., Data: err.Error()}
       }
       return map[string]any{
           "ok":         true,
           "decided_at": now.Format(time.RFC3339Nano),
           "applied_to": []string{},  // placeholder; rc-piagent-bind will populate
       }, nil
   }
   ```

   Two distinct lookups happen on the happy path: `Get` first
   to surface `-32006` before any state mutation; `Resolve`
   second, which deletes-then-sends. The `Get` result is
   deliberately discarded — the `Resolve` is the authoritative
   step that surfaces the not-found error if a concurrent
   resolver beat us to it (the brief pins this with
   `TestPendingInteractions_ConcurrentResolves_NoDoubleAck`).

The response envelope's `decided_at` uses the same RFC 3339
Nano format the start cascade envelope uses — dashboards
parse both with one helper.

### `internal/remotecontrol/pending_test.go` (new, ~640 / 13 tests)

| Test | What it pins |
|---|---|
| `TestPendingInteractions_AddGetResolve` | Add registers, Get returns the same pointer, Resolve succeeds and drops the entry; second Resolve sees `ErrInteractionNotFound` (one-shot semantics) |
| `TestPendingInteractions_ResolveUnknown` | `Resolve("never-existed", …)` → `ErrInteractionNotFound`; nil result payload allowed |
| `TestPendingInteractions_NilSafe` | `(*PendingInteractions)(nil).Add / Get / Resolve / Expire / Len` all no-op without panicking |
| `TestPendingInteractions_Expire` | Entry with `ExpiresAt` in the past → `Expire(now)` removes + closes ResultCh; entry with zero `ExpiresAt` is kept; entry with future `ExpiresAt` is kept; nil receiver returns nil |
| `TestPendingInteractions_ConcurrentResolves_NoDoubleAck` | 100 goroutines race to `Resolve` the same id; exactly one wins; the other 99 see `ErrInteractionNotFound` (brief: "one-shot") |
| `TestCascade_AddInteractionLazyInit` | First `AddInteraction` allocates `*Pending` (was nil before); second call reuses the same registry; ID format is `int_<12 hex chars>`; both `ResultCh` channels are non-nil |
| `TestCascade_AddInteractionNilSafe` | `(*Cascade)(nil).Pending()` is nil; `(*Cascade)(nil).AddInteraction(...)` returns `(_, non-nil error)` — the back-reference can't be set on a nil cascade, so the method refuses rather than panic |
| `TestHandleRemoteAgentCascadeUserInteraction_Ok` | Full happy path: StubBackend emits one prompt; sink sees it via `captureSink`; out-of-band dispatcher handler resolves; channel close observable; result envelope has `ok: true`, `decided_at: RFC3339Nano`, `applied_to: []string{}` (not nil) |
| `TestHandleRemoteAgentCascadeUserInteraction_UnknownCascade` | `cascade_id` does not exist → `-32004`, data carries the unknown id verbatim |
| `TestHandleRemoteAgentCascadeUserInteraction_UnknownInteraction` | Cascade exists, but no interaction is registered → `-32006`, data carries the unknown `interaction_id` |
| `TestHandleRemoteAgentCascadeUserInteraction_BadParams` | 7 subtests: missing `cascade_id`, empty `cascade_id`, whitespace `cascade_id`, missing `interaction_id`, empty `interaction_id`, missing `response`, `null` response → all `-32602` |
| `TestUserInteraction_Expires` | End-to-end: StubBackend emits an interaction whose `ExpiresAt` is in the past; the registry's `Expire(now)` drops it; the next `Get` returns `(_, false)` |
| `TestUserInteraction_TimeoutInBackend` | StubBackend with `InteractionWait: 30ms` and no answer → `emitInteractions` returns `ErrCascadeInteractionTimeout`; sender observes the sentinel |

The `Ok` test runs the dispatcher's handler out-of-band (no
real WS round-trip — see "Wire-level coverage" below for why)
because the in-process path is more reliable for asserting
the response envelope shape and the `ResultCh` close
behaviour.

### `internal/remotetransport/tcp_test.go` (+~206)

3 new tests, built on the existing `newTestTCPServerWithCap`
helper and a new `startCascade` helper:

| Test | What it pins |
|---|---|
| `TestHandleUserInteraction_NotificationOverWire` | WS round-trip: StubBackend emits one prompt → the WS reader sees a JSON-RPC notification `cascade/user_interaction_required` with the expected params shape (cascade_id, interaction_id, kind, title, prompt, expires_at ISO 8601) — does NOT assert that HandleRemoteAgentCascadeUserInteraction completes the round-trip (see below) |
| `TestHandleUserInteraction_UnknownCascade` | `HandleRemoteAgentCascadeUserInteraction` with a non-existent `cascade_id` → `error.code == -32004` over the wire |
| `TestHandleUserInteraction_UnknownInteraction` | Cascade exists; `interaction_id` is unknown → `error.code == -32006` over the wire |

### `internal/remotecontrol/cascade_test.go` (+~31 / -3)

The pre-existing `captureSink` test fixture (cycle-3 and prior
tests pass it through `Sink()`) gains one field and one method:

```go
type captureSink struct {
    // ... existing fields ...
    interactions []*PendingInteraction // NEW
}

func (s *captureSink) OnUserInteraction(_ *Cascade, intr *PendingInteraction) {
    if s == nil || intr == nil {
        return
    }
    s.mu.Lock()
    s.interactions = append(s.interactions, intr)
    s.mu.Unlock()
}
```

`TestConnNotifierSink_NilSafe` grows one call:

```go
s.OnUserInteraction(&Cascade{ID: "x"}, &PendingInteraction{ID: "x"}) // must not panic
```

Plus the corresponding nil-cascade captured case.

Pre-existing tests are unchanged: the cycle does not fire
`OnUserInteraction` from the StubBackend unless the test
populates `Interactions`, and the existing tests populate
`Interactions` nowhere — so the `captureSink.interactions`
slice stays empty for all of them. The fixture growth is
purely to keep the type satisfying the `EventSink` interface.

## Wire-level coverage — why the WS round-trip is partial

The `handleWS` loop in `internal/remotetransport/tcp.go` lines
404–436 reads **one request at a time** from the connection
and only enters `Dispatcher.Dispatch` after `ReadMessage`
returns. It does not interleave server-push notifications with
incoming requests on the same goroutine. Today, that means:

- A WS connection in the middle of a `SendRemoteAgentCascadeMessage`
  whose StubBackend has parked on `ResultCh` **cannot receive**
  a `HandleRemoteAgentCascadeUserInteraction` call until the
  parked turn completes (because the loop is blocked reading
  the next request from the same connection).
- The cycle's interaction flow needs a turn blocking on
  `ResultCh` in the very same goroutine that should be
  answering it.

This is a known limitation of the existing transport, NOT a
bug introduced by this cycle. Patching it would require either:
(a) splitting the `handleWS` loop into separate read + dispatch
goroutines, or (b) implementing the `InteractionWait` timeout
as a separate dispatcher-side expiry goroutine. Both are out
of scope here (the brief explicitly marks `PendingInteractions.Expire`
as "method only — no long-running goroutine shipped this cycle").

What the tests do instead:

- **`TestHandleUserInteraction_NotificationOverWire`**: dials the
  TCP server, starts a cascade, fires `SendRemoteAgentCascadeMessage`
  with a StubBackend whose `Interactions: [one prompt]` and
  `InteractionWait: 50 ms`. Reads the WS until it sees the
  `cascade/user_interaction_required` notification, asserts the
  envelope fields (cascade_id, interaction_id, kind, title,
  prompt, expires_at ISO 8601). Does not attempt the
  bi-directional close.
- **`TestHandleUserInteraction_UnknownCascade` /
  TestHandleUserInteraction_UnknownInteraction`**: do not park
  the StubBackend — they exercise the dispatcher's error path
  only (which doesn't touch `ResultCh`), so the round-trip
  works fine.

The in-process `TestHandleRemoteAgentCascadeUserInteraction_Ok`
covers the bi-directional close directly by calling
`d.Dispatch(...)` from a goroutine while the stub is parked
on `ResultCh`. This is the dispatcher's correct behaviour; the
transport gap is independent.

A future `rc-cancel-cascade`-style cycle or a `rc-piagent-bind`
follow-up will close the transport gap. The deliverable's
limitations section itemises the work.

## Custom error code reservation

| Code | Reserved for | Cycle that consumes |
|---|---|---|
| `-32004` | Cascade not found | rc-cascade-message |
| `-32005` | Cascade closed | rc-cascade-message |
| `-32006` | Interaction not found | **rc-handle-user-interaction (this cycle)** |
| `-32010` | Turn timeout | rc-cascade-message |
| `-32000 .. -32003`, `-32007 .. -32009`, `-32011 .. -32099` | free for future `rc-*` cycles | future |

The codes live in `internal/remotecontrol/backend.go` as named
constants (`ErrCodeInteractionNotFound = -32006`) so a future
cycle can import them without redeclaring.

## Test results

### New tests (added by this cycle)

| Test file | Tests added | Subtests |
|---|---:|---:|
| `internal/remotecontrol/pending_test.go` (new) | 13 | 7 (`BadParams`) |
| `internal/remotecontrol/cascade_test.go` | 0 new top-level; `captureSink` + `TestConnNotifierSink_NilSafe` grew | – |
| `internal/remotetransport/tcp_test.go` | 3 wire-level | – |

### Test counts after this cycle

| File | Tests (before) | Tests (after) |
|---|---:|---:|
| `internal/remotecontrol/daemon_test.go` | 16 | 16 (unchanged) |
| `internal/remotecontrol/cascade_test.go` | 37 | 37 (unchanged — fixture grew, no new `Test*` funcs) |
| `internal/remotecontrol/pending_test.go` | 0 | 13 (new file) |
| `internal/remotetransport/tcp_test.go` | 25 | 28 (+3) |
| **Total in affected packages** | **78** | **94** |

### Race detector

```
$ /Users/gear/go/bin/go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.793s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.438s
```

The `TestPendingInteractions_ConcurrentResolves_NoDoubleAck`
test exercises 100 goroutines racing on `Resolve` against the
same id; `-race` confirms the per-registry `sync.Mutex` makes
the brief's "exactly one wins" guarantee hold.

### `go vet`

```
$ /Users/gear/go/bin/go vet ./internal/remotecontrol/... ./internal/remotetransport/...
(no output, exit 0)
```

The repository's pre-existing `go vet` dirt
(`cmd/wagyu/model_default_effort_test.go:67` — a 4-arg `t.Errorf`
introduced by a prior cycle; verified pre-existing via
`git stash`) is left alone per cycle rules.

### `gofmt -l` on changed files

```
$ /opt/homebrew/bin/gofmt -l \
    internal/remotecontrol/pending.go \
    internal/remotecontrol/pending_test.go \
    internal/remotecontrol/backend.go \
    internal/remotecontrol/cascade.go \
    internal/remotecontrol/cascade_test.go \
    internal/remotecontrol/dispatch.go \
    internal/remotecontrol/streaming.go \
    internal/remotetransport/tcp_test.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in
`internal/` + `cmd/wagyu/picker_view.go`) is intentionally left
alone per cycle rules.

## Live verification (manual sketch)

```bash
$ cd /Users/gear/Documents/GitHub/openagy-rc
$ go build -o /tmp/wagyu_rc6 ./cmd/wagyu
$ /tmp/wagyu_rc6 --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc

# In another shell (a real dashboard's bi-directional flow):
$ websocat ws://127.0.0.1:NNNNN/rpc
> {"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade",
   "params":{"clientInfo":{"name":"smoke","version":"0"}}}
< {"jsonrpc":"2.0","id":1,"result":{"cascade_id":"cas_a3f1...", ...}}

# (StubBackend isn't yet hot-reloadable from the wire —
#  the smoke-test path today needs a Go-side test that
#  installs a StubBackend with Interactions:[...] on the
#  cascade before SendRemoteAgentCascadeMessage.)
```

The Go tests are the in-process equivalent. The wire tests
(`tcp_test.go`) round-trip through the actual `coder/websocket`
library so the wire envelope is pinned byte-for-byte; the
in-process happy path runs in `pending_test.go`'s
`TestHandleRemoteAgentCascadeUserInteraction_Ok` (which
calls `d.Dispatch(...)` directly).

## Files

| File | Δ | What |
|---|---|---|
| `internal/remotecontrol/pending.go` (new) | +351 | `PendingInteraction` / `InteractionOption` / `InteractionResult` / `PendingInteractions`; `InteractionKind*` constants; `ErrInteractionNotFound`; `Add / Get / Resolve / Expire / Len` (all nil-safe); `mintInteractionID`; package + lifecycle preamble |
| `internal/remotecontrol/pending_test.go` (new) | +637 | 13 tests + 7 subtests; `RegisterCascadeWithSink` test helper |
| `internal/remotecontrol/cascade.go` | +104 / -0 | `pending *PendingInteractions` field; `Pending()` lazy-init accessor; `AddInteraction(...)` method; cycle-6 package preamble |
| `internal/remotecontrol/cascade_test.go` | +39 / -13 | `interactions []*PendingInteraction` field on `captureSink`; `OnUserInteraction` capture method; `TestConnNotifierSink_NilSafe` grows one call on the nil-receiver path and one on the nil-cascade path |
| `internal/remotecontrol/streaming.go` | +115 / -0 | `OnUserInteraction` added to `EventSink`; `NotifUserInteractionRequired = "cascade/user_interaction_required"`; `userInteractionRequiredParams` type; `(*ConnNotifierSink).OnUserInteraction` (registry Add + notify) |
| `internal/remotecontrol/backend.go` | +197 / -0 | `ErrCascadeInteractionTimeout`; `ErrCodeInteractionNotFound = -32006`; `PendingInteractionConfig` type; `StubBackend.Interactions / Answers / InteractionWait`; `(*StubBackend).emitInteractions`; `SendStream` walks `Interactions` before text/tool emission |
| `internal/remotecontrol/dispatch.go` | +134 / -0 | `case "HandleRemoteAgentCascadeUserInteraction"` in `Dispatch`; `handleRemoteAgentCascadeUserInteractionParams` type; `handleRemoteAgentCascadeUserInteraction` handler (cascade lookup → interaction lookup → Resolve → `{ok, decided_at, applied_to: []}`); nil-response / null-response rejection in the per-field string-type check |
| `internal/remotetransport/tcp_test.go` | +206 / -0 | `strings` import; 3 new `TestHandleUserInteraction_*` tests |

`internal/remotecontrol/daemon.go`, `internal/remotecontrol/wire.go`,
`internal/remotecontrol/host.go`, `internal/remotecontrol/notify.go`,
`internal/remotetransport/tcp.go`, `internal/remotetransport/unix.go`,
`cmd/wagyu/remote_control.go`, and `cmd/wagyu/remote_control_adc.go`
are **untouched**.

The pre-existing `cascade_test.go` was edited only to grow the
`captureSink` fixture (one field + one method) and the nil-safety
test (two calls). Those changes are the minimum needed to keep the
file compiling against the new `EventSink` interface.

## Design notes worth keeping

1. **The registry is per-cascade, not per-dispatcher.** "An
   interaction belongs to the cascade that produced it" is the
   natural model: a cascade is the dashboard's session-scoped
   view of "my turn in progress", and a permission prompt is
   part of that turn. A dispatcher-level registry would force
   `interaction_id` to be globally unique (less entropy per
   id); a cascade-level registry scopes uniqueness to the
   places the dashboard can see anyway.

2. **The `Sink` does the registry `Add`, not the backend.**
   Per the brief, the backend mints the id and constructs the
   `*PendingInteraction`; the sink is responsible for registering
   it in the cascade's registry (via `s.cascade.Pending().Add(intr)`)
   and emitting the notification. The split makes it easy for a
   backend that wants to fork (emit two notifications for one
   logical decision) to hand the sink two `*PendingInteraction`
   pointers and have both routed through the same notification
   envelope path. **The sink uses `s.cascade` (the one captured
   at construction) for the registry target, not the `cascade`
   parameter** — the captured cascade is what the dispatcher
   resolves by id on `HandleRemoteAgentCascadeUserInteraction`,
   so the registry entry has to live there regardless of what
   the backend passes.

3. **`ResultCh` is buffered (len 1) and `Resolve` sends outside
   the lock.** Length 1 lets the dispatcher's RPC handler
   complete without blocking on the backend's read (the
   canonical state — backend parked on `ResultCh`). Sending
   outside the lock means a slow consumer doesn't serialise
   other resolutions; the dashboard that answered first gets
   its answer through to the backend, and the dashboard that
   answered second sees `ErrInteractionNotFound`. The brief
   pins the second property with
   `TestPendingInteractions_ConcurrentResolves_NoDoubleAck`.

4. **`Expire` returns the IDs of dropped entries rather than
   closing them in-place.** The caller is responsible for
   closing the channels (Expire does close them — see line 317
   of `pending.go`) and for any audit logging. The returned
   slice is the "what just got expired" log line a future
   cycle's status tool can read.

5. **`applied_to []string{}` is a placeholder, not a bug.**
   The rc-piagent-bind cycle will populate it with the
   cascade / conversation IDs the decision propagated to
   (e.g. "this 'approve' was also applied to cascade X,
   conversation Y"). The current cycle returns `[]` because
   there is no PiG yet; the field exists so the wire envelope
   is stable and dashboards don't have to defend against
   "field sometimes missing". nil slices are normalised to
   `[]string{}` for the same reason `toolCalls` is normalised
   in rc-cascade-message.

6. **`response: null` and missing `response` are both
   `-32602`.** A null payload is semantically equivalent to
   "I have no answer" — there's no useful state a backend
   can derive from it. The bytes-level check
   (`bytes.Equal(p.Response, []byte("null"))`) catches the
   JSON null literal explicitly; the length-0 check catches
   the omitted field. Both paths funnel into the same
   `-32602 Invalid params` envelope.

7. **StubBackend is the only backend installed.** The cycle
   ships `&StubBackend{}` as the constructor-seeded default
   for every new cascade. A future `rc-piagent-bind` cycle
   hands the real PiG-session-backed implementation to
   `(*Cascade).SetBackend(...)` for every cascade the
   dispatcher creates. The dispatcher's new method is
   unchanged once the real backend lands — it just resolves
   whatever `*PendingInteraction` the real backend parked on.

8. **Nil-safe dispatch path.** Every accessor
   (`(*Cascade).Pending()` / `(*PendingInteractions).Add / Get / Resolve / Expire / Len`)
   is nil-safe. A `(*ConnNotifierSink).OnUserInteraction(nil, nil)`
   is a no-op. A `(*Dispatcher).handleRemoteAgentCascadeUserInteraction`
   that receives a closed / missing cascade surfaces the
   documented error code; the dispatcher's `defer recover()`
   is the catch-all for the "`OnUserInteraction` panicked"
   case.

9. **No long-running expiry goroutine shipped this cycle.**
   `(*PendingInteractions).Expire` is a method on the
   registry — the caller (a future cycle's status tool or
   the next rc-piagent-bind) owns when to call it. The
   test `TestUserInteraction_Expires` exercises Expire
   in-process; the production path will wire
   `time.Ticker` → `(*PendingInteractions).Expire(time.Now())`
   once PiG turns exist on the cascade.

## 硬约束 / limitations

1. **Wire-level round-trip is not exercised end-to-end.** The
   `handleWS` single-request-at-a-time loop in
   `internal/remotetransport/tcp.go` lines 404–436 reads
   one message at a time, so a parked stub cannot receive
   the `HandleRemoteAgentCascadeUserInteraction` call back
   from the same connection. The cycle's wire tests assert
   the notification envelope (server-push half is fine) and
   the error-path codes only. The bi-directional close
   (dashboards answering a parked prompt) is exercised
   in-process at `TestHandleRemoteAgentCascadeUserInteraction_Ok`
   in `pending_test.go`, which calls `d.Dispatch(...)` directly
   from a goroutine. A future cycle patches `handleWS` to
   split the read + dispatch goroutines; today this is a
   transport-level gap, not a cycle bug.

2. **StubBackend is the only backend installed.** A future
   `rc-piagent-bind` cycle hands the real PiG-session-backed
   implementation to `(*Cascade).SetBackend(...)`. The cycle's
   StubBackend extension (`Interactions / Answers /
   InteractionWait`) makes the contract exact and pinned —
   a test installs the queue, asserts the notification +
   Resolve path, and `piagent-bind` swaps in the real backend
   without changing the cycle's tests.

3. **`applied_to []string{}` is hard-coded empty.** The
   rc-piagent-bind cycle will populate it with the IDs of any
   downstream cascades / conversations the decision
   propagated to. The current cycle returns `[]` because
   there is no PiG yet; the field exists so the wire envelope
   is stable across cycles.

4. **No long-running expiry goroutine.** `Expire` is a method
   on the registry; today only `TestUserInteraction_Expires`
   calls it. A future cycle (likely `rc-status-tool` or
   `rc-piagent-bind`) wires a `time.Ticker` →
   `(*PendingInteractions).Expire(time.Now())` so stale
   prompts get reaped in production. Until then, an
   interaction with a non-zero `ExpiresAt` lives until the
   dispatcher gets an explicit `HandleRemoteAgentCascadeUserInteraction`
   call (whose `interaction_id` then yields `-32006` because
   Expire already dropped the entry).

5. **StubBackend's `Answers map[string]string` is declared
   but not read by the stub.** The cycle exposes the field
   so a future cycle (likely `rc-stub-config` or
   `rc-piagent-bind`) can drive the stub with a declarative
   "if interaction_id == X, send response == Y" table. The
   field exists; today the stub's only path through
   `emitInteractions` is "wait for the dashboard or time out".

6. **`handleWS` blocks on the next request until a turn
   completes.** A dashboard that fires
   `SendRemoteAgentCascadeMessage` with a stub whose turn
   parks on `ResultCh` cannot send another request on the
   same connection until the park resolves. The cycle's
   interactions are designed so the resolve happens via
   `(*PendingInteractions).Resolve` (which the dispatcher
   handler calls synchronously), but the underlying
   transport-level request-pipeline blocking remains.

7. **Pre-existing `go vet` issue is not in scope.** The
   `cmd/wagyu/model_default_effort_test.go:67` `t.Errorf`
   with 4 args (verified pre-existing via `git stash`) is
   unchanged. The cycle's
   `go vet ./internal/remotecontrol/... ./internal/remotetransport/...`
   is clean.

8. **`PendingInteractionConfig` is the StubBackend's typed-input
   shape, `PendingInteraction` is the storage shape.** The split
   keeps `PendingInteraction.ResultCh` and the
   `cascade` back-reference out of the API a backend / test
   constructs from (the back-reference is set by
   `(*Cascade).AddInteraction`, not by the caller). A future
   `rc-piagent-bind` cycle constructs `PendingInteractionConfig`
   from PiG's permission-prompt shape and passes it via
   `StubBackend.Interactions []PendingInteractionConfig` (or a
   non-stub backend that calls `(*Cascade).AddInteraction(...)`
   directly with the same fields), and never touches the storage
   type's back-reference field.

9. **No reconnect-on-resolve on the dashboard side.** If the
   dashboard's WS connection drops between the
   `cascade/user_interaction_required` notification and the
   `HandleRemoteAgentCascadeUserInteraction` answer, the
   pending interaction is orphaned in the cascade's
   `*PendingInteractions` registry. The brief pins this as
   the next-cycle `rc-status-tool`'s job (it can call
   `Expire(now)` on the orphaned entries). Today, the
   StubBackend's `InteractionWait` is the only
   "abandoned" path; a future wire-level reconnect-resume
   cycle will close the gap properly.

10. **`OnUserInteraction` is called from
    `(*StubBackend).emitInteractions` BEFORE the stub emits
    any text or tool calls.** A real backend may interleave
    prompts with chunks (e.g. "I started reading the file
    before parking on a permission decision"). The stub's
    "all prompts first, then text, then tools" ordering is
    a fixture simplification; the wire protocol allows any
    interleaving (notifications don't carry an `id` and
    don't block the request loop beyond the
    `handleWS` constraint above).

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu_rc6 ./cmd/wagyu

# Run all affected tests with the race detector
/Users/gear/go/bin/go test -race -count=1 \
    ./internal/remotecontrol/ ./internal/remotetransport/

# Optional: vet + gofmt on the changed packages
/Users/gear/go/bin/go vet ./internal/remotecontrol/... ./internal/remotetransport/...
/opt/homebrew/bin/gofmt -l \
    internal/remotecontrol/pending.go \
    internal/remotecontrol/pending_test.go \
    internal/remotecontrol/backend.go \
    internal/remotecontrol/cascade.go \
    internal/remotecontrol/cascade_test.go \
    internal/remotecontrol/dispatch.go \
    internal/remotecontrol/streaming.go \
    internal/remotetransport/tcp_test.go
```

## 后续 cycle 入口 / next-cycle entry points

| Future cycle | What this cycle left for them |
|---|---|
| `rc-piagent-bind` | `EventSink.OnUserInteraction` is the seam. The cycle hooks PiG's permission-prompt flow onto `sink.OnUserInteraction(cascade, intr)` from `internal/pigrt/runtime.go`. The dispatcher handler is unchanged. `applied_to []string{}` becomes populated with the cascade / conversation IDs the decision propagated to. The production expiry goroutine (`time.Ticker` → `(*PendingInteractions).Expire(time.Now())`) lands here as a side-wiring. |
| `rc-stub-config` | `StubBackend.Answers` is the typed-input surface a future cycle can use to drive the stub with declarative "interaction_id → response" tables — useful for end-to-end smoke tests against a real PiG session that doesn't actually need user input. |
| `rc-status-tool` | `(*PendingInteractions).Len()` is exposed; `Expire(now)` is the reaper. The status tool can call `c.Pending().Len()` per cascade and surface "N pending user-interactions" as a status field. |
| `rc-orchestrator-cascade-start` | When a future cycle introduces "orchestrator cascades" (parent / child cascade fan-out), the `applied_to` field is the natural place to record "this decision was forwarded to cascade X, Y". |
| `rc-cancel-cascade` | `(*Cascade).MarkClosed` plus `(*PendingInteractions).Expire(now)` is the cleanup path. A `CancelRemoteAgentCascade` RPC marks the cascade closed and expires every pending interaction so the stub's `emitInteractions` returns next time. |
| `rc-final-parity-verify` | Every method this cycle ships is exercised end-to-end at this layer: a Python ws client dials, calls Start → install-stub-with-Interactions → Send → asserts notification envelope → calls HandleRemoteAgentCascadeUserInteraction → asserts `{ok, decided_at, applied_to: []}` result. The unresolved wire round-trip stays in the brief's "split read + dispatch goroutines" follow-up. |
| `rc-bidi-loop` | A pre-requisite for any round-trip beyond the notification envelope: split `handleWS` into separate read + dispatch goroutines, and have the dispatcher's request handler release its read on the connection while waiting for `HandleRemoteAgentCascadeUserInteraction` to resolve a parked turn. Without this patch the wire-level round-trip stays gated. |
