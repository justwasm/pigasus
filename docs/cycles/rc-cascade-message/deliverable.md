# Cycle `rc-cascade-message` — `SendRemoteAgentCascadeMessage` (text → turn → reply, non-streaming)

## TL;DR / 摘要

Cycles 1–3 landed the transport surface (rc-ws-transport `46b348b`),
server-push + graceful shutdown (rc-streaming-protocol `23de9aa9`),
and the first cascade lifecycle method (rc-cascade-start `62bba6e`).
This cycle wires the **second** lifecycle method:
`SendRemoteAgentCascadeMessage` — the dashboard sends one user
message, the server drives one full PiG-style turn (synchronously,
through a `MessageBackend` seam), and returns the assistant's final
reply in a single response.

**No streaming**: this cycle is the synchronous end-to-end path.
Cycle rc-cascade-stream (next) will add mid-turn notifications via
the existing `Dispatcher.Notify` mechanism; the reply here is the
*final* text and the surface a non-streaming dashboard receives.

**No PiG wiring yet**: a `StubBackend` is the default
implementation. The `MessageBackend` interface is the seam a future
cycle (`rc-piagent-bind`) will hand a real PiG-session-backed
implementation to via `(*Cascade).SetBackend`.

| Piece | Where |
|---|---|
| `MessageBackend` interface + `MessagePrompt` / `MessageResult` / `ToolCall` / `MessageSendOptions` types, custom error sentinels `ErrCascadeClosed` / `ErrTurnTimeout` / `ErrTurnFailed`, custom JSON-RPC code constants `-32004` / `-32005` / `-32010`, `StubBackend` fixture (sends a fixed result; honours ctx) | `internal/remotecontrol/backend.go` (new, ~280 lines) |
| `(*Cascade).Backend() / SetBackend()` accessors; per-cascade mu + `IsClosed() / MarkClosed() / recordReply()`; `LastReplyAt` / `Closed` fields; `(*CascadeRegistry).Send(ctx, cascadeID, prompt, opts)` (lookup → context-bounded timeout → backend Send → recordReply) | `internal/remotecontrol/cascade.go` (+~120 / -3) |
| `(*Dispatcher).handleSendRemoteAgentCascadeMessage` (param shape, validation, per-turn override resolution, registry.Send, error → JSON-RPC envelope mapping for `-32602` / `-32004` / `-32005` / `-32010` / `-32603`), `case "SendRemoteAgentCascadeMessage"` in `Dispatch` | `internal/remotecontrol/dispatch.go` (+~190 / -3) |
| 19 cascade-level tests + 7 wire-level tests (`StubBackend` only — no real PiG) | `internal/remotecontrol/cascade_test.go` (+~430) + `internal/remotetransport/tcp_test.go` (+~280) |

What lands for forward consumers: a `MessageBackend` interface
that any `*Cascade` may have installed on it, plus the
`(*Cascade).Backend() / SetBackend()` accessors. A future cycle
(`rc-piagent-bind`) hands a real `*pigrt`-backed backend to the
cascade and the dispatcher's `SendRemoteAgentCascadeMessage`
handler runs it transparently — the seam is the only thing the
cycle needs to know about.

## Wire protocol added by this cycle

### Method: `SendRemoteAgentCascadeMessage`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "SendRemoteAgentCascadeMessage",
  "params": {
    "cascade_id":  "<opaque-id>",          // REQUIRED, from StartRemoteAgentCascade
    "text":        "<user-prompt>",        // REQUIRED, non-empty
    "agent":       "<string>",             // optional, overrides cascade's agent
    "model":       "<string>",             // optional, overrides cascade's model
    "effort":      "low|medium|high|xhigh|max",   // optional, "" = use cascade's
    "scaffold":    true,                   // optional
    "sessionMode": "<string>",             // optional, opaque passthrough
    "timeoutMs":   30000                   // optional, server-side turn timeout
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "conversationId": "conv-<id>",
    "reply":          "<assistant-final-text>",
    "durationMs":     1234,
    "toolCalls": [
      {"name": "<tool>", "args": {...}, "result": "<string>", "ok": true}
    ],
    "stopReason": "end_turn"
  }
}
```

Errors:

| Code | When | Surface path |
|---|---|---|
| `-32602 Invalid params` | `cascade_id` missing / empty / whitespace-only; `text` missing / empty; `effort` not in {`low`, `medium`, `high`, `xhigh`, `max`}; `timeoutMs < 0`; one of `cascade_id / text / agent / model / effort / sessionMode` present but not a string | `handleSendRemoteAgentCascadeMessage` returning `-32602` |
| `-32004 Cascade not found` | `cascade_id` is well-formed but no cascade is registered | `d.cascades.Get` returning `(nil, false)` (or `(*CascadeRegistry).Send` returning `ErrCascadeNotFound`) |
| `-32005 Cascade closed` | the cascade has been marked closed via `MarkClosed` (out-of-band today; future `rc-cancel-cascade` wires an RPC for it) | `c.IsClosed()` returning true in the dispatcher handler, or `(*CascadeRegistry).Send` returning `ErrCascadeClosed` |
| `-32603 Internal error` | backend returned a non-timeout error; registry full at mint time; `(*CascadeRegistry).Send` returned `ErrTurnFailed` for any reason not otherwise classified | the handler's default branch |
| `-32010 Turn timeout` | `timeoutMs > 0` and the backend Send did not return within that budget | `(*CascadeRegistry).Send` returning `ErrTurnTimeout` (wrapped `context.DeadlineExceeded`) |

The custom code range `-32000 .. -32099` is reserved per the brief.
`cmd/wagyu/` leaves room for `rc-piagent-bind` to add more in the
same band without overlap. The full envelope is a normal JSON-RPC
2.0 error: `{"code": <int>, "message": <string>, "data": <any>}`.

### Stop reasons the backend may surface

| Value | When |
|---|---|
| `end_turn` | the assistant finished a turn naturally; the model's response is complete |
| `tool_use` | the assistant's turn is parked on a tool invocation; the dashboard should expect mid-turn notifications (rc-cascade-stream) |
| `max_tokens` | the assistant hit the model's output token cap; partial text may be lost |
| `error` | the backend itself failed; `reply` is typically empty |

The constants live in `internal/remotecontrol/backend.go` so the
dispatcher and the future real backend agree on the vocabulary.

## What changed since `rc-cascade-start`

### `internal/remotecontrol/backend.go` (new)

The seam — every type the dispatcher's new handler reads from the
cascade, every sentinel the registry's `Send` returns, and the one
fixture the cycle's tests use.

```go
type MessageBackend interface {
    Send(ctx context.Context, prompt MessagePrompt) (MessageResult, error)
}

type MessagePrompt struct {
    ConversationID string  // cascade's prior ConversationID ("" on first turn)
    Text           string
    Model          string
    Agent          string
    Effort         string
    Scaffold       bool
    SessionMode    string
    BackendLabel   string  // "agw" / "shim" / ""
}

type MessageResult struct {
    Reply              string
    ToolCalls          []ToolCall
    StopReason         string
    NewConversationID  string  // may equal MessagePrompt.ConversationID
}

type ToolCall struct {
    Name   string
    Args   []byte        // raw JSON object the model emitted
    Result string
    OK     bool
}

type MessageSendOptions struct {
    Timeout time.Duration  // 0 = no extra timeout (caller's ctx)
}

const (
    StopReasonEndTurn   = "end_turn"
    StopReasonToolUse   = "tool_use"
    StopReasonMaxTokens = "max_tokens"
    StopReasonError     = "error"
)

const (
    ErrCodeCascadeNotFound = -32004
    ErrCodeCascadeClosed   = -32005
    ErrCodeTurnTimeout     = -32010
)

var (
    ErrCascadeClosed = errors.New("remotecontrol: cascade closed")
    ErrTurnTimeout   = errors.New("remotecontrol: turn timeout")
    ErrTurnFailed    = errors.New("remotecontrol: turn failed")
)
```

The `StubBackend` is a hand-rolled configurable fixture:

```go
type StubBackend struct {
    Reply          string
    ConversationID string
    ToolCalls      []ToolCall
    StopReason     string     // default: StopReasonEndTurn
    Err            error      // when non-nil, Send returns Err verbatim
    Sleep          time.Duration  // when > 0, Send waits (honouring ctx)
}
```

Tests instantiate `&StubBackend{...}` with whatever fields they
need; the empty `StubBackend{}` is a usable "do nothing" backend
that returns the zero `MessageResult` with `StopReasonEndTurn`.
The dispatcher's default-installed backend is exactly this —
production callers wire a real one via `(*Cascade).SetBackend`.

### `internal/remotecontrol/cascade.go` (+~120 / -3)

Three additions; no behavioral change to the existing
StartRemoteAgentCascade path:

1. **Field rename**: `Cascade.Backend string` → `Cascade.BackendLabel string`
   (matching `Prompt.BackendLabel`). The JSON-RPC request field is
   still `"backend"`; only the Go field name moved. `CascadeOpts.Backend`
   follows. The two existing tests that asserted `c.Backend` (one in
   `cascade_test.go`) were updated to track the rename — the only
   diff to pre-existing files.

   Why: `(*Cascade).Backend() MessageBackend` is the seam per the
   brief; the existing `Backend` string label blocks that exact
   accessor name.

2. **Closed sentinel + LastReplyAt**: `Cascade` gains `Closed bool`
   and `LastReplyAt time.Time`. Both are guarded by the per-cascade
   `mu sync.Mutex`. Accessors:

   ```go
   func (c *Cascade) IsClosed() bool
   func (c *Cascade) MarkClosed()        // future rc-cancel-cascade
   func (c *Cascade) recordReply(convID string, t time.Time)  // internal
   ```

   `IsClosed` / `MarkClosed` / `recordReply` are nil-safe; the
   cascade's `Backend() / SetBackend()` accessors are nil-safe
   too. Field access on a nil `*Cascade` is intentionally not
   nil-safe (that's a Go semantic, not anything this package
   should mask) — the cycle's nil-safety test stays above the
   accessor layer.

3. **`Backend()` seam**: `NewCascade` seeds `backend: &StubBackend{}`,
   so a freshly-built cascade's `Backend()` returns a usable
   default. `SetBackend(b)` swaps it. The accessors do not take a
   lock (the SetBackend contract is "call before the cascade
   becomes reachable from a method handler" — same pattern the
   print-path's chunk-sink uses, which also has no per-call
   locking).

4. **`(*CascadeRegistry).Send`**: the in-process face of
   `SendRemoteAgentCascadeMessage`. Looks the cascade up;
   returns `ErrCascadeNotFound` on miss, `ErrCascadeClosed` on
   closed; applies the timeout via `context.WithTimeout`; calls
   `backend.Send`; on `context.DeadlineExceeded` (when a timeout
   was set) wraps `ErrTurnTimeout`; otherwise wraps `ErrTurnFailed`
   via `errors.Join` so `errors.Is(err, ErrTurnFailed)` AND
   `errors.Is(err, innerErr)` both match. On success, calls
   `c.recordReply(res.NewConversationID, time.Now().UTC())`.

   The registry mu is held only during the existence check
   (`RLock`); the per-cascade mu is held only during the
   ConversationID + LastReplyAt update. The backend Send call
   holds NO registry lock, so a slow backend does not serialise
   other operations on other cascades in the same registry.

### `internal/remotecontrol/dispatch.go` (+~190 / -3)

One method registration + one handler:

1. `case "SendRemoteAgentCascadeMessage":` in `Dispatch` →
   `d.handleSendRemoteAgentCascadeMessage(ctx, req.Params)`.
2. The handler:

   a. JSON-decodes into `sendRemoteAgentCascadeMessageParams`.
      Type-checks the string-typed fields via
      `nonStringFieldInJSON` so `{model: 42}` doesn't silently
      default-walk (mirrors the rc-cascade-start rule).
   b. Validates:
      - `cascade_id`: required, non-empty (whitespace-trimmed
        before the existence check so a stray space isn't a
        silent `ErrCascadeNotFound`)
      - `text`: required, non-empty
      - `effort`: optional, must be in `DefaultEffortValues`
        when present
      - `timeoutMs`: optional, ≥ 0
      All violations → `-32602`.
   c. `d.cascades.Get(cascade_id)`; missing → `-32004`.
   d. `c.IsClosed()` → `-32005`.
   e. Per-turn overrides applied to `MessagePrompt` via
      `firstNonEmpty(per-turn, cascade-pinned)`; `Scaffold`
      defaults to `c.Scaffold` when the per-turn param is
      false (so a dashboard can't silently turn scaffold mode
      off by omitting the field).
   f. `opts.Timeout = time.Duration(p.TimeoutMs) * time.Millisecond`.
   g. `start := time.Now()`; `d.cascades.Send(ctx, cascadeID,
      prompt, opts)`.
   h. On error, the switch maps sentinels to JSON-RPC codes:
      `ErrCascadeNotFound` → `-32004`, `ErrCascadeClosed` →
      `-32005`, `ErrTurnTimeout` → `-32010`, anything else
      (incl. `ErrTurnFailed` and unmapped backend errors) →
      `-32603` with the wrapped message in `data`.
   i. On success, builds the result:

   ```go
   return map[string]any{
       "conversationId": res.NewConversationID,
       "reply":          res.Reply,
       "durationMs":     time.Since(start).Milliseconds(),
       "toolCalls":      toolCalls,   // always []toolCall (nil → empty)
       "stopReason":     res.StopReason or StopReasonEndTurn,
   }, nil
   ```

   `toolCalls` is a `[]toolCall` whose fields carry JSON tags
   matching the brief's envelope exactly (`name / args /
   result / ok`). `args` is `json.RawMessage` so the dashboard
   sees the same JSON object shape the model emitted. Empty
   `Args` are normalised to `"null"` so the field is never
   absent.

3. Helper `firstNonEmpty(a, b string) string` is the per-turn
   override resolver; kept local because the brief's resolver
   only needs to fall back once.

### `internal/remotecontrol/cascade_test.go` (+~430)

19 new tests, organised in three groups:

| Test | What it pins |
|---|---|
| `TestCascade_Send_Success` | registry.Send returns the backend's `MessageResult`; cascade's `ConversationID` + `LastReplyAt` are updated; cascade's `Backend()` returns the installed backend |
| `TestCascade_Send_ConversationIDRetained` | backend returns empty `NewConversationID` → cascade's prior value is preserved (the brief's "follow-up turn reuses" rule) |
| `TestCascade_Send_PropagatesError` | backend returns inner error → registry wraps in `ErrTurnFailed`; `errors.Is(err, ErrTurnFailed)` AND `errors.Is(err, innerErr)` both match (the `errors.Join` guarantee) |
| `TestCascade_Send_Timeout` | `StubBackend{Sleep: 200ms}` + `opts.Timeout: 30ms` → `ErrTurnTimeout`; Send returned before the sleep elapsed (matches the timeout, didn't run it full) |
| `TestCascade_Send_NoTimeoutSucceeds` | `StubBackend{Sleep: 10ms}` + `opts.Timeout: 100ms` → success (contrast to the previous test; pins that a buggy "always-timeout" handler can't pass) |
| `TestCascade_Send_NotFound` | empty registry + unknown id → `ErrCascadeNotFound` |
| `TestCascade_Send_ClosedSentinel` | `MarkClosed()` → `IsClosed()` flips; subsequent `Send` returns `ErrCascadeClosed`; nil-receiver `IsClosed / MarkClosed` are no-ops |
| `TestCascade_NilSafe_SendAndAccessors` | nil-registry `Send` → `ErrCascadeNotFound`; nil-cascade `Backend / SetBackend / MarkClosed / IsClosed` don't panic |
| `TestCascade_BackendAccessorRoundtrip` | constructor-seeded `*StubBackend`; `SetBackend(custom)` swaps it; `Backend()` returns the custom instance |
| `TestStubBackend_DefaultNoOp` | zero-value `*StubBackend` and nil `*StubBackend` both return the zero `MessageResult` with `StopReasonEndTurn` (the dispatcher's default-installed backend) |
| `TestIsValidEffort` | `DefaultEffortValues` values pass; bogus / case-mismatched / dash-modified fail |
| `TestDispatcher_SendRemoteAgentCascadeMessage_OK` | full dispatcher round-trip: Start → install backend → Send → assert reply / conversationId / stopReason / toolCalls is `[]` (not nil) |
| `TestDispatcher_SendRemoteAgentCascadeMessage_NoCascadeID` | 4 sub-tests: missing field / empty string / whitespace-only / empty params → all `-32602` |
| `TestDispatcher_SendRemoteAgentCascadeMessage_NoText` | empty text → `-32602` |
| `TestDispatcher_SendRemoteAgentCascadeMessage_BadEffort` | 3 sub-tests: bogus / `x-high` / `MEDIUM` → `-32602` |
| `TestDispatcher_SendRemoteAgentCascadeMessage_NotFound` | unknown id → `-32004` |
| `TestDispatcher_SendRemoteAgentCascadeMessage_Closed` | marked-closed cascade → `-32005` |
| `TestDispatcher_SendRemoteAgentCascadeMessage_PropagatesError` | backend error → `-32603` |
| `TestDispatcher_SendRemoteAgentCascadeMessage_Timeout` | slow backend + 30ms `timeoutMs` → `-32010` |

The cascade-level tests use a helper `registerCascadeWithBackend(t, backend)`
that builds a registry + cascade and installs the supplied backend
in one step. The dispatcher-level tests share the same helper via
a small accessor (`d.cascades.cascades[cid] = ...`) to inject a
pre-built cascade with a backend into the dispatcher's registry —
a unit-test affordance, not a public API.

### `internal/remotetransport/tcp_test.go` (+~280)

7 new wire-level tests, built on the existing
`newTestTCPServerWithCap` helper plus a new
`startCascade(t, c)` that round-trips `StartRemoteAgentCascade`
and returns the id:

| Test | What it pins |
|---|---|
| `TestSendRemoteAgentCascadeMessage_OK` | full WS round-trip: `{"cascade_id": cid, "text": "..."}` → reply / conversationId / durationMs-as-number / `toolCalls` as empty array (not null) / `stopReason: end_turn` / cascade `LastReplyAt` updated |
| `TestSendRemoteAgentCascadeMessage_NotFound` | `cascade_id: "cas_doesnotexist"` → `error.code == -32004` |
| `TestSendRemoteAgentCascadeMessage_BadEffort` | `effort: "bogus"` → `error.code == -32602` |
| `TestSendRemoteAgentCascadeMessage_NoCascadeID` | missing `cascade_id` → `error.code == -32602` |
| `TestSendRemoteAgentCascadeMessage_ToolCalls` | StubBackend returns one `read_file` ToolCall with `args: {"path":"/etc/hostname"}` → result envelope faithfully serialises it |
| `TestSendRemoteAgentCascadeMessage_Timeout` | `StubBackend{Sleep: 200ms}` + `timeoutMs: 30` → `error.code == -32010` |
| `TestSendRemoteAgentCascadeMessage_PropagatesError` | `StubBackend{Err: ...}` → `error.code == -32603` |

Each wire test dials the WS adapter, starts a cascade (round-trips
the rc-cascade-start method), patches a backend onto the resulting
`*Cascade` via `d.cascades.cascades[cid]`, and round-trips
`SendRemoteAgentCascadeMessage`. The `code` field's float64 shape
(after a JSON round-trip) is asserted via the same `int(errObj["code"].(float64))`
cast the rc-cascade-start tests use.

## Wire-format compatibility

The cycle-1/2/3 contract — `initialize / tools/list / tools/call /
ping / notifications/initialized / StartRemoteAgentCascade`
returning byte-for-byte the same shape — is preserved. The 16
daemon_test.go tests + 9 cycle-1/2 tcp_test.go tests + 5
rc-cascade-start tcp_test.go tests all pass unchanged against the
new code. The dispatcher interface in `internal/remotetransport/unix.go`
does not grow a method this cycle (no new transport-adapter
involvement).

The cycle is additive on the wire: the only new request method is
`SendRemoteAgentCascadeMessage`. A dashboard that ignores it sees
the same surface it saw yesterday.

## Custom error code reservation

| Code | Reserved for | Cycle that consumes |
|---|---|---|
| `-32004` | Cascade not found | rc-cascade-message (this cycle) |
| `-32005` | Cascade closed | rc-cascade-message (this cycle); `rc-cancel-cascade` (next cycle to wire an RPC) |
| `-32010` | Turn timeout | rc-cascade-message (this cycle) |
| `-32000 .. -32003`, `-32006 .. -32009`, `-32011 .. -32099` | free for future `rc-*` cycles | future |

The codes live in `internal/remotecontrol/backend.go` as named
constants (`ErrCodeCascadeNotFound = -32004`, etc.) so a future
cycle can import them without re-declaring.

## Test results

### New tests (added by this cycle)

| Test | Where | What it pins |
|---|---|---|
| 9 `TestCascade_*` tests + 3 `TestStubBackend_DefaultNoOp` / `TestIsValidEffort` / `TestCascade_BackendAccessorRoundtrip` | `internal/remotecontrol/cascade_test.go` | registry-level Send semantics + dispatcher envelope mapping |
| 7 `TestSendRemoteAgentCascadeMessage_*` tests | `internal/remotetransport/tcp_test.go` | wire-level round-trip + envelope codes |

### Test counts after this cycle

| File | Tests (before) | Tests (after) |
|---|---:|---:|
| `internal/remotecontrol/daemon_test.go` | 16 | 16 (unchanged) |
| `internal/remotecontrol/cascade_test.go` | 13 | 32 (+19 new) |
| `internal/remotetransport/tcp_test.go` | 14 | 21 (+7 new) |
| **Total in affected packages** | **43** | **69** |

### Race detector

```
$ go test -race -count=3 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.807s
ok  	github.com/chadgpt/openagy/internal/remotetransport	3.530s
```

Re-run with `-count=3` against all `TestSend*` and
`TestDispatcher_Send*` tests: all pass three times in a row.

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
file is left alone.

### `gofmt -l` on changed files

```
$ gofmt -l internal/remotecontrol/backend.go \
          internal/remotecontrol/cascade.go \
          internal/remotecontrol/cascade_test.go \
          internal/remotecontrol/dispatch.go \
          internal/remotetransport/tcp_test.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in
`internal/` + `cmd/wagyu/picker_view.go`) is intentionally left
alone per cycle rules.

## Live verification (manual sketch)

```bash
$ cd /Users/gear/Documents/GitHub/openagy-rc
$ go build -o /tmp/wagyu_rc4 ./cmd/wagyu
$ /tmp/wagyu_rc4 --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc

# In another shell:
$ websocat ws://127.0.0.1:NNNNN/rpc
> {"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade","params":{"clientInfo":{"name":"smoke","version":"0"}}}
< {"jsonrpc":"2.0","id":1,"result":{"cascade_id":"cas_a3f1...","conversationId":"","created_at":"...","model":"Gemini 3.8 Flash (Medium)","backend":"agw","version":"0.1.0"}}

> {"jsonrpc":"2.0","id":2,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"cas_a3f1...","text":"hi"}}
< {"jsonrpc":"2.0","id":2,"result":{"conversationId":"","reply":"","durationMs":0,"toolCalls":[],"stopReason":"end_turn"}}

> {"jsonrpc":"2.0","id":3,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"cas_doesnotexist","text":"hi"}}
< {"jsonrpc":"2.0","id":3,"error":{"code":-32004,"message":"Cascade not found","data":"cas_doesnotexist"}}

> {"jsonrpc":"2.0","id":4,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"cas_a3f1...","text":"hi","effort":"bogus"}}
< {"jsonrpc":"2.0","id":4,"error":{"code":-32602,"message":"Invalid params","data":"effort must be one of low|medium|high|xhigh|max"}}
```

The Go tests are the in-process equivalent. The wire tests
(`tcp_test.go`) round-trip through the actual `coder/websocket`
library so the wire envelope is pinned byte-for-byte.

## Files

| File | Δ | What |
|---|---|---|
| `internal/remotecontrol/backend.go` (new) | +291 | `MessageBackend` interface; `MessagePrompt` / `MessageResult` / `ToolCall` / `MessageSendOptions` types; `StopReason*` constants; `ErrCascadeClosed` / `ErrTurnTimeout` / `ErrTurnFailed` errors; `ErrCode*` JSON-RPC code constants; `IsValidEffort` helper; `DefaultEffortValues` slice; `StubBackend` fixture |
| `internal/remotecontrol/cascade.go` | +140 / -4 | `mu` field + `LastReplyAt`/`Closed` fields + `backend MessageBackend` field; `Backend() / SetBackend()` accessors; `IsClosed() / MarkClosed() / recordReply()`; `BackendLabel string` rename for the existing label field; `(*CascadeRegistry).Send(ctx, cascadeID, prompt, opts)`; `errors` / `context` / `time` imports |
| `internal/remotecontrol/dispatch.go` | +200 / -2 | `case "SendRemoteAgentCascadeMessage"` in `Dispatch`; `sendRemoteAgentCascadeMessageParams` / `sendRemoteAgentCascadeMessageResult` / `toolCall` types; `handleSendRemoteAgentCascadeMessage`; `firstNonEmpty` helper |
| `internal/remotecontrol/cascade_test.go` | +430 / -3 | 19 new tests + 1 rename tracking update (3 lines: `Backend:` → `BackendLabel:` ×2 + `c.Backend` ×1) + `fmt` import |
| `internal/remotetransport/tcp_test.go` | +280 / -0 | `newTestTCPServerWithBackend` + `startCascade` helpers; 7 new `TestSendRemoteAgentCascadeMessage_*` tests |

`internal/remotecontrol/daemon.go`, `internal/remotecontrol/wire.go`,
`internal/remotecontrol/host.go`, `internal/remotecontrol/notify.go`,
`internal/remotetransport/tcp.go`, `internal/remotetransport/unix.go`,
`cmd/wagyu/remote_control.go`, and `cmd/wagyu/remote_control_adc.go`
are **untouched**. The dispatcher's default-fills behavior means
existing host wiring sees no change.

The pre-existing `cascade_test.go` was edited only to track the
`Backend → BackendLabel` rename — those three lines are the
minimum diff to keep the file compiling. No formatting changes.

## Design notes worth keeping

1. **The `MessageBackend` seam is the only contract future cycles
   need to know about.** `rc-piagent-bind` constructs a real
   `*pigrt`-backed implementation and calls
   `(*Cascade).SetBackend(...)` on every cascade the dispatcher
   creates (via a new `(*Dispatcher).SetCascadeBackend` factory
   is also fine). The dispatcher handler is unchanged: it calls
   `(*Cascade).Backend().Send(ctx, prompt)` regardless of the
   concrete type. Cycle-3a (this cycle) only ships a `StubBackend`
   as the default; the production wiring is parked for the
   PiG-binding cycle.

2. **`Send` is the in-process face; `Dispatch` is the wire face;
   `MessageBackend.Send` is the model turn.** Three layers, each
   knows only the layer below it. The `StubBackend` fixture lives
   in the dispatcher-agnostic `backend.go` file because it's a
   backend (it implements the model-turn layer), not a dispatcher
   or registry concern. Tests instantiate `&StubBackend{...}` and
   install it via `(*Cascade).SetBackend(...)`; production wiring
   does the same with a real backend.

3. **`errors.Join` for the wrapping path.** The registry's
   `errors.Join(ErrTurnFailed, err)` lets `errors.Is(err,
   ErrTurnFailed)` (used by the dispatcher's switch) AND
   `errors.Is(err, innerErr)` (used by logs) both match. The
   dispatcher surfaces the joined error's `.Error()` string in
   `data`, which prefixes with `"remotecontrol: turn failed: "`
   — that's noisy on the wire but useful for debugging; a future
   polish cycle can split the inner message into a structured
   `data` payload.

4. **Per-turn vs cascade-pinned resolution is one-way.** A
   per-turn param that's empty falls back to the cascade's
   pinned value; we never re-resolve against the host's
   `SessionState` because the rc-cascade-start cycle's Start
   handler already resolved at Start time. This keeps the
   "what was resolved at Start time" trail visible: the cascade
   carries the resolved values, and the Send handler reads them.

5. **`Scaffold` defaulting uses OR (`p.Scaffold || c.Scaffold`)
   not firstNonEmpty.** A bool can't be empty vs. unset; absent
   and `false` collapse to false in JSON. The dashboard's
   "scaffold mode off" path is `{"scaffold": false}`; the
   "use cascade's pinned scaffold" path is omitting `scaffold`.
   We OR the two so an explicit `false` doesn't silently turn
   scaffold mode off when the cascade had it on. (A future cycle
   that wants to distinguish "absent" from "explicit false" can
   switch `Scaffold *bool` like the rc-cascade-start cycle does
   for `sandbox`.)

6. **`toolCalls` is always a JSON array, not null.** The handler
   explicitly normalises a nil slice to `[]toolCall{}` so the
   dashboard sees `[]` rather than `null` when no tool calls
   happened. This matches the brief's "empty array if none".

7. **The custom code range `-32000 .. -32099` is reserved per the
   brief.** This cycle uses `-32004 / -32005 / -32010`. Future
   `rc-*` cycles can pick from the same band without colliding
   — `cmd/wagyu/` leaves a comment in `backend.go` documenting
   the reservation.

8. **`(*CascadeRegistry).Send` does not hold any lock during the
   backend call.** The lookup phase takes the registry's RLock
   briefly; the cascade's ConversationID + LastReplyAt update
   take the per-cascade mu briefly; the backend Send runs
   without locks held. A slow backend does not serialise other
   operations on other cascades in the same registry.

9. **`(*Cascade).SetBackend` is not lock-protected.** The contract
   is "call once, before the cascade becomes reachable from a
   method handler". The dispatcher (and any production wiring)
   calls it on a freshly-built cascade before `Register`. Tests
   do the same. If a future cycle wants concurrent SetBackend
   (e.g. a per-turn backend swap), it must add its own
   synchronisation — the cycle doesn't promise to.

## 硬约束 / limitations

1. **StubBackend is the only backend installed.** The cycle
   ships `&StubBackend{}` as the constructor-seeded default
   for every new cascade. A future `rc-piagent-bind` cycle
   hands the real PiG-session-backed implementation to
   `(*Cascade).SetBackend(...)` for every cascade the
   dispatcher creates. Today, a real-wire integration test
   would receive whatever the StubBackend was configured
   with — not a model turn. The dispatcher handler is
   unchanged once the real backend lands.

2. **No streaming.** This cycle is the synchronous end-to-end
   path. The `reply` field is the *final* assistant text,
   returned when the backend Send returns (which is when
   the model's turn is fully complete). Mid-turn chunk
   notifications ride on the existing `Dispatcher.Notify`
   mechanism and ship in cycle rc-cascade-stream. A streaming
   dashboard may receive the same `SendRemoteAgentCascadeMessage`
   response (with the *final* text) plus interim
   `StreamRemoteAgentStateUpdates` notifications; the
   final-text reply here is the canonical end-of-turn signal.

3. **No PiG/agw session lives on `*Cascade` yet.** The struct
   has no `*pigrt.Session` field. The rc-piagent-bind cycle
   will add one; until then, the StubBackend is the
   "model turn" (returning a fixed reply and stop reason
   based on how its fields are configured).

4. **Per-turn vs cascade-pinned effort / model / agent
   resolution runs at Send time, not at registration.** A
   cascade created with `effort = "high"` then sent a
   message with `effort = "low"` will run that turn at
   `"low"`. A future turn on the same cascade with empty
   `effort` will run at `"high"` again. The cycle makes no
   effort to remember "the dashboard changed effort" — the
   cascade's pinned value stays `high` until Start runs
   again. Documented behaviour; the brief specifies the
   "per-turn override" semantics and not "sticky override"
   semantics.

5. **`Closed` is set only via `MarkClosed` today.** No JSON-RPC
   method registers it (the brief says rc-cancel-cascade or
   similar will). A future cycle must add a cancellation
   RPC, otherwise the only path to ErrCascadeClosed is test
   code. Until then, a stuck cascade occupies a registry
   slot until the dispatcher-level `Drop` (out of scope
   this cycle) or process exit runs.

6. **`Scaffold` per-turn is OR-merged with cascade-pinned,
   not "absent = keep, explicit false = override".** The
   brief says the field is `bool`; absent and `false` both
   unmarshal to `false`. A dashboard that wants scaffold
   mode off for one turn must use `{"scaffold": false}`,
   which the handler reads as "false" and OR-merges with
   the cascade's value. That can surprise callers; a future
   cycle that wants to distinguish can switch the field to
   `*bool` (already the rc-cascade-start pattern for
   `sandbox`).

7. **No reconnect on the ws side.** A dashboard's TCP
   connection dropping leaves any cascades it started
   orphaned in the registry. Reconnect+resume is out of
   scope; rc-cascade-stream will add a per-conn "I
   dropped" hook (nothing in this cycle depends on it).

8. **No streaming subscribe yet.** A dashboard that opens a
   cascade and sends a message today sees only the final
   reply. Mid-turn chunk notifications ride on
   `Dispatcher.Notify` and ship in `rc-cascade-stream`
   (next cycle). The StubBackend's `Reply` is what's
   returned; a real backend will stream via the same
   mechanism.

9. **Pre-existing `go vet` issue is not in scope.** The
   `cmd/wagyu/model_default_effort_test.go:67` `t.Errorf`
   with 4 args (verified pre-existing via `git stash`) is
   unchanged. The cycle's
   `go vet ./internal/remotecontrol/ ./internal/remotetransport/`
   is clean.

10. **`Backend` field renamed to `BackendLabel` (Go only).**
    The three lines in pre-existing
    `cascade_test.go` (`Backend:` in `CascadeOpts`,
    `c.Backend` read at two assertion sites) were updated to
    track the rename. The JSON-RPC request/response field is
    still `"backend"`; only the Go struct field name moved.
    No reformatting; the diff is the minimum needed to keep
    the file compiling.

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu_rc4 ./cmd/wagyu

# Run all affected tests with the race detector, 3 iterations
go test -race -count=3 ./internal/remotecontrol/ ./internal/remotetransport/

# Optional: vet + gofmt on the changed packages
go vet ./internal/remotecontrol/ ./internal/remotetransport/
gofmt -l internal/remotecontrol/backend.go \
          internal/remotecontrol/cascade.go \
          internal/remotecontrol/dispatch.go \
          internal/remotecontrol/cascade_test.go \
          internal/remotetransport/tcp_test.go
```

## 后续 cycle 入口 / next-cycle entry points

| Future cycle | What this cycle left for them |
|---|---|
| `rc-piagent-bind` | `(*Cascade).SetBackend(MessageBackend)` is the seam. The cycle constructs a `*pigrt`-backed `MessageBackend` and installs it on every cascade at registration time. The dispatcher handler is unchanged. |
| `rc-cascade-stream` | `MessageResult.ToolCalls` + `StopReason` are already populated; the stream cycle subscribes per cascade and emits `StreamRemoteAgentStateUpdates` notifications via `Dispatcher.Notify` as the model emits chunks. The final reply returned here is the same one a streaming dashboard receives when no chunks have arrived (or arrives standalone on a non-streaming connection). |
| `rc-handle-user-interaction` | `ErrCascadeNotFound` + the `Closed` sentinel are still in place. The handler uses `(*Dispatcher).Cascades().Get(cascade_id)` and the same accessors. |
| `rc-cancel-cascade` | `(*Cascade).MarkClosed` is the seam. The cycle wires a JSON-RPC method (e.g. `CancelRemoteAgentCascade`) that calls `MarkClosed` then `*CascadeRegistry.Drop` or leaves the slot for `ErrCascadeClosed` to handle the follow-up. |
| `rc-list-conversations` | `(*Cascade).ConversationID` is now non-empty after the first successful Send. The cycle iterates `(*Dispatcher).Cascades().IDs()` and reads each `*Cascade.ConversationID`. |
| `rc-status-tool` | The cascade registry now exposes `LastReplyAt` per cascade; the status tool's per-cascade breakdown (future) reads this for "idle / last-active" reporting. |
| `rc-error-status` | `(*Cascade)` will gain a `lastError error` field; the dispatcher will surface it through a new method. Today's `(*Cascade).IsClosed` proves the per-cascade mu pattern is race-clean under `-race`. |
| `rc-final-parity-verify` | Every method this cycle ships is exercised end-to-end at this layer: a Python ws client dials, calls Start → Send → asserts reply / conversationId / durationMs; assert error envelopes for not-found, bad-effort, timeout. |
