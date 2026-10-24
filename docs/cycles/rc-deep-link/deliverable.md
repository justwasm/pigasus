# rc-deep-link — `remoteControlDeepLink` JSON-RPC method (G9)

Cycle ships the one JSON-RPC method the fresh-eyes
re-investigation at `/Users/gear/Documents/GitHub/openagy/.scratch/rc-reinvestigation.md`
called out as **G9** — `remoteControlDeepLink` was previously
`-32601 Method not found`. The brief:

> *"G9 — `remoteControlDeepLink` handler that accepts
> `agy://...` events (currently `-32601`)"* —
> `rc-reinvestigation.md §G9`

The cycle is the bounded goal from `PLAN.md:173`:

> *"`remoteControlDeepLink` handler that accepts `agy://...`
> events (currently `-32601`)"*

One method, three verbs (`agy://open`, `agy://focus-cascade`,
`agy://paste-and-send`), full URL parser, `Focused` flag on
`*Cascade`, `LastDeepLink` timestamp holder on `Config`. No
streaming protocol changes. No new transport. **No push/merge.**
All envelopes from cycle-15 stay byte-compatible.

---

## TL;DR

| What | Where | LOC |
|---|---|---:|
| `DeepLinkAction` constants + `DeepLink` struct + `ParseDeepLink` parser + 4 sentinels + `Echo()` helper | `internal/remotecontrol/deep_link.go` | +280 |
| `Focused atomic.Bool` + `Focus()` / `IsFocused()` on `*Cascade` | `internal/remotecontrol/cascade.go` | +63 |
| `Config.LastDeepLink *DeepLinkState` + `NewDispatcher` nil-guard (fabricates a fresh state when nil) | `internal/remotecontrol/daemon.go` | +11 |
| `DeepLinkState` type (mirrors `AuthState`: internal RWMutex + `RecordOpen` + `Snapshot`) | `internal/remotecontrol/deep_link.go` | (above) |
| `remoteControlDeepLink` switch case + `handleRemoteControlDeepLink` handler (3 verbs, 3 reply shapes) | `internal/remotecontrol/dispatch.go` | +292 |
| 9 new in-process tests in `deep_link_test.go` (ParseDeepLink × 5 shapes + Echo + DeepLinkState nil-safe + RecordOpen + Cascade.Focus + Dispatcher round-trip × 3 verbs / 2 errors) | `internal/remotecontrol/deep_link_test.go` | +523 |
| 5 new wire-level tests in `tcp_test.go` (Open / FocusCascade NotFound / FocusCascade Ok / PasteAndSend FiresTurn / InvalidURL over WebSocket) | `internal/remotetransport/tcp_test.go` | +389 |

- **Tests:** 14 new total (9 in-process + 5 wire-level), all
  pass `-race -count=1`. Total runtime for the scoped packages:
  ~4.9 s.
- **`go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/`** PASS (2.0 s + 2.9 s).
- **`go vet ./internal/remotecontrol/ ./internal/remotetransport/
  ./cmd/wagyu/`** exit 0. The pre-existing
  `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf`
  warning stays as-is (out of scope).
- **`gofmt -l`** empty on the seven touched files. Pre-existing
  `cmd/wagyu` gofmt dirt untouched.
- **No third-party deps added.** Stdlib + `coder/websocket` +
  the project's own packages.
- **No `go test ./...`.** Scope matches the brief.

---

## Method shape

```
req:
  { "jsonrpc":"2.0", "id":<any>, "method":"remoteControlDeepLink",
    "params": { "url": "agy://<verb>?<query…>" } }

res (verb="open"):
  { "jsonrpc":"2.0", "id":<…>, "result": {
      "ok":          true,
      "action":      "open",
      "echo":        {"host":"open", "query":{…}, "raw":"<url>"},
      "received_at": "<RFC3339Nano>"
  }}

res (verb="focus-cascade", cascade resolves):
  { "jsonrpc":"2.0", "id":<…>, "result": {
      "ok":          true,
      "action":      "focus-cascade",
      "cascade_id":  "<cas_…>",
      "echo":        {…},
      "received_at": "<RFC3339Nano>"
  }}
  + server-push notification on the originating ws:
    { "jsonrpc":"2.0", "method":"cascade/state", "params": {
        "cascade_id":  "<cas_…>",
        "phase":       "focused",
        "focused_at":  "<RFC3339Nano>",
        "ts":          "<RFC3339Nano>"
    }}

res (verb="paste-and-send", cascade resolves, not closed):
  { "jsonrpc":"2.0", "id":<…>, "result": {
      "ok":          true,
      "action":      "paste-and-send",
      "cascade_id":  "<cas_…>",
      "echo":        {…},
      "accepted_at": "<RFC3339Nano>"   ← synchronous, does not wait
  }}
  + the dispatcher spawns a goroutine that drives
    (*CascadeRegistry).SendStream against the pinned
    cascade's backend. The goroutine emits the normal
    cascade/state and cascade/text_delta notifications on
    the originating ws (via ConnNotifierSink); it does NOT
    write a second JSON-RPC envelope on completion — the
    dashboard subscribes to the streaming notifications for
    turn progress.
```

### Error envelope vocabulary

| Code | When |
|---:|---|
| `-32602` | `params` missing or unparseable (`json.Unmarshal` failure) |
| `-32602` | `params.url` absent / empty / not a string |
| `-32602` | `params.cascade_id` present but not a string (matches the cycle-15 typed-field rule) |
| `-32602` | `ParseDeepLink` returned any of `ErrDeepLinkBadScheme` / `ErrDeepLinkMissingVerb` / `ErrDeepLinkUnknownVerb` / `ErrDeepLinkMalformed` (verbatim verb in `data` so a dashboard sees the rejected input) |
| `-32602` | `focus-cascade` / `paste-and-send`: `cascade_id` empty (top-level + URL-embedded both missing) |
| `-32602` | `paste-and-send`: `text` query param empty |
| `-32004` | `focus-cascade` / `paste-and-send`: `cascade_id` not registered (parity with `SendRemoteAgentCascadeMessage`) |
| `-32005` | `paste-and-send`: cascade is `MarkClosed`'d at the moment the deep-link arrives (parity with `SendRemoteAgentCascadeMessage`) |
| `-32603` | `dispatcher not initialised` (defensive — never in production) |

The `data` field carries the verbatim rejection reason ("unsupported
scheme: \"http\"" / "unknown verb: \"nuke\"" / "cascade_id is
required" / the resolved cascade_id). JSON-RPC 2.0 §5.1 puts
`data` on the error object as opaque, and the dashboards read
it via `error.data` / `error.message` so the verb itself
propagates through.

---

## `ParseDeepLink` (`internal/remotecontrol/deep_link.go`)

```go
type DeepLinkAction string
const (
    DeepLinkOpen         DeepLinkAction = "open"
    DeepLinkFocusCascade DeepLinkAction = "focus-cascade"
    DeepLinkPasteAndSend DeepLinkAction = "paste-and-send"
)

type DeepLink struct {
    Action    DeepLinkAction
    CascadeID string            // "" when URL doesn't carry one
    Text      string            // "" when URL doesn't carry one
    URL       *url.URL          // parsed URL — kept for Echo()
    Raw       string            // trimmed input
}

func ParseDeepLink(raw string) (DeepLink, error)
```

`ParseDeepLink` is a thin wrapper over `net/url.Parse` plus a
handful of structured errors. The four sentinels:

| Sentinel | When |
|---|---|
| `ErrDeepLinkBadScheme` | scheme is not `agy` |
| `ErrDeepLinkMissingVerb` | host (verb) is empty (e.g. `agy://?cascade_id=…`) |
| `ErrDeepLinkUnknownVerb` | verb is non-empty but not in the three supported constants |
| `ErrDeepLinkMalformed` | `net/url.Parse` itself failed (e.g. control characters in path) |

All four are wrapped into `-32602` envelopes with the verbatim
error message in `data` so a dashboard sees the rejected input.

### `Echo()` helper

The brief specifies the response shape:

```
"echo": {"host": "<verb>", "query": {"k":"v", …}, "raw": "<url>"}
```

`(*DeepLink).Echo()` returns this map. `query` is a
`map[string]string` — repeated keys are taken **first value
wins** (matching the dash-companion's URL-contract). `raw` is
the trimmed input, not the post-`Parse` representation (so
the dashboard sees what the user clicked on, not the parser's
canon form).

---

## `DeepLinkState` (`internal/remotecontrol/deep_link.go`)

Mirrors the project's `AuthState` pattern: pointer field on
`Config`, internal `sync.RWMutex`, `RecordOpen(time.Time)`
writer, `Snapshot() time.Time` reader.

```go
type DeepLinkState struct {
    mu          sync.RWMutex
    lastOpenAt  time.Time
}

func (s *DeepLinkState) Snapshot() time.Time { … }   // returns zero time when never opened
func (s *DeepLinkState) RecordOpen(at time.Time) { … }
```

`(*Dispatcher).Config.LastDeepLink *DeepLinkState` is the
config-level handle. `NewDispatcher` fabricates a fresh state
when the field is nil — defence-in-depth, but in practice
the field is always populated by the host wiring path.

`open` is the only verb that stamps `LastDeepLink` today; a
future cycle that wants to track focus / paste-and-send
events separately would extend `DeepLinkState` with
`LastFocusAt` / `LastPasteSendAt` slots and stamp from the
other branches — out of scope here.

---

## `*Cascade.Focused` (`internal/remotecontrol/cascade.go`)

```go
type Cascade struct {
    …
    focused atomic.Bool   // cycle rc-deep-link
}

func (c *Cascade) Focus()       // nil-safe; flips the atomic
func (c *Cascade) IsFocused() bool  // nil-safe; current value
```

The flag is read by future dashboard cycles (e.g. a status
tool that surfaces "this cascade is the one the user is
looking at"). For cycle rc-deep-link, the focus branch sets
the flag and emits the notification; nothing else reads it.
`atomic.Bool` matches the project's per-cascade concurrency
discipline (the rest of `*Cascade` uses `sync.Mutex` for
compound-state reads, and `atomic.Bool` for single-bit
state).

`Focus()` is idempotent: a second call leaves the flag set
without emitting a second notification (the dispatcher's
focus branch returns synchronously, so the only side-effects
are `c.Focus()` + the notification emit; the notification
emit is outside any lock and uses `NotifCascadeState` even
when the flag was already set).

---

## Concurrency model

- **Synchronous.** The focus-cascade + open verbs are entirely
  synchronous — handler runs, sets state, returns the reply.
  The dispatcher's `Notify` runs outside any lock
  (`context.Background` for the Notify call so a cancelled
  dispatcher ctx doesn't short-circuit the write — matches
  the rc-cancel-cascade pattern at `dispatch.go:1862`).

- **Asynchronous.** The paste-and-send verb returns
  `accepted_at` synchronously and spawns a goroutine that
  calls `(*CascadeRegistry).SendStream` independently of the
  RPC reply. The goroutine:
    - captures the connID from `connIDFromContext(ctx)`
      BEFORE `go func()` so its notifications land on the
      originating ws even after the request ctx is gone;
    - uses `context.Background()` for `SendStream`, since the
      request ctx may be cancelled the moment the transport
      writes the response (rc-cancel-cascade pattern);
    - snapshots the cascade's `Model` / `Agent` / `Effort` /
      `Scaffold` / `SessionMode` / `BackendLabel` so a
      future cycle's mid-turn override path doesn't race
      the goroutine;
    - reports goroutine failures via the sink
      (`cascade/state{phase:"error"}`) and via the cascade's
      bounded error ring — NOT as a second JSON-RPC envelope
      (the response has already landed with `accepted:true`).

---

## Tests

### In-process (`internal/remotecontrol/deep_link_test.go`, +523 LOC)

| Test | Pins |
|---|---|
| `TestParseDeepLink_Open` | `agy://open` → `DeepLinkOpen`, query empty, no error |
| `TestParseDeepLink_FocusCascade` | `agy://focus-cascade?cascade_id=cas_1` → `CascadeID = "cas_1"`, `Action = DeepLinkFocusCascade` |
| `TestParseDeepLink_PasteAndSend` | URL-encoded `text` query is `QueryUnescape`d to "hello world" |
| `TestParseDeepLink_BadURL` | `agy://` (missing host) returns `ErrDeepLinkMissingVerb` wrapped |
| `TestParseDeepLink_UnknownVerb` | `agy://nuke` returns `ErrDeepLinkUnknownVerb` with `data` echoing "nuke" |
| `TestParseDeepLink_BadScheme` | `http://open` returns `ErrDeepLinkBadScheme` |
| `TestDeepLink_Echo` | Echo shape: `{host, query, raw}` map with first-value-wins on repeated keys |
| `TestDeepLinkState_NilSafe` | nil receiver → `Snapshot()` returns zero time, `RecordOpen` is a no-op (defensive — production never sees nil) |
| `TestDeepLinkState_RecordOpen` | sequential `RecordOpen(now1); RecordOpen(now2)` → `Snapshot() == now2` |
| `TestCascade_Focus` | fresh `IsFocused() == false`; `Focus()` then `IsFocused() == true`; idempotent |
| `TestDispatcher_RemoteControlDeepLink_Open_OK` | dispatch round-trip: open verb stamps `LastDeepLink` (Snapshot is non-zero), returns `{ok, action, echo, received_at}` |
| `TestDispatcher_RemoteControlDeepLink_FocusCascade_NotFound` | unknown `cascade_id` → `-32004` |
| `TestDispatcher_RemoteControlDeepLink_PasteAndSend_Closed` | closed cascade → `-32005 ErrCodeCascadeClosed` (parity with `SendMessage`) |
| `TestDispatcher_RemoteControlDeepLink_BadURL` | handler-level round-trip: `agy://nuke` → `-32602` with `data` carrying the rejected verb |

### Wire-level (`internal/remotetransport/tcp_test.go`, +389 LOC)

| Test | Pins |
|---|---|
| `TestRemoteControlDeepLink_Open_WS` | full ws round-trip; response shape; no notification expected |
| `TestRemoteControlDeepLink_FocusCascade_NotFound_WS` | full ws round-trip; error envelope `-32004` |
| `TestRemoteControlDeepLink_FocusCascade_Ok_WS` | full ws round-trip; both response AND `cascade/state{phase:"focused"}` notification observed on the originating ws; `c.IsFocused() == true` post-flight |
| `TestRemoteControlDeepLink_PasteAndSend_FiresTurn_WS` | full ws round-trip; sync `accepted_at` response AND goroutine-emitted `cascade/state{phase:"done"}` notification; `c.LastReplySnapshot() == "paste-and-send reply"` post-flight |
| `TestRemoteControlDeepLink_InvalidURL_WS` | three sub-shapes: missing host → `-32602`; bad scheme → `-32602`; unknown verb → `-32602` |

The wire-level tests run against the live `TCPServer` with
the ws adapter (`/rpc` path, ws upgrade), so they catch any
transport-level regression in the connID-threading /
cancellable-ctx / enqueueFrame path that the in-process tests
would miss.

---

## Sanity checks (run on this commit)

```bash
$ go build -o /tmp/wagyu ./cmd/wagyu
$ echo "build: $?"
build: 0

$ go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.974s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.911s

$ go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/
$ echo "vet: $?"
vet: 0
# Pre-existing cmd/wagyu/model_default_effort_test.go:67
# 3-arg Errorf warning is out of scope.

$ gofmt -l \
    internal/remotecontrol/deep_link.go \
    internal/remotecontrol/deep_link_test.go \
    internal/remotecontrol/cascade.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/dispatch.go \
    internal/remotetransport/tcp.go \
    internal/remotetransport/tcp_test.go
$ echo "gofmt: $?"
gofmt: 0
```

The wire-level `PasteAndSend_FiresTurn_WS` test reads the
response AND the cascading notifications in a single drain
loop because the wire order between the synchronous response
(id=2) and the goroutine's first notification
(`cascade/state{phase:"starting"}`) is non-deterministic —
the dispatch goroutine's marshal+enqueue and the goroutine's
first `sink.OnState` race. The loop records both and breaks
when both have been observed.

---

## 硬约束 (hard constraints honoured)

1. **One method, one bounded goal.** Only
   `remoteControlDeepLink` was added. The envelope shapes of
   every cycle-15 method stay byte-compatible. No transport
   changes. No streaming-protocol changes (no new
   notification channels, no `phase` enum extensions).
2. **Three verbs only.** `open`, `focus-cascade`,
   `paste-and-send`. Any other verb → `-32602` with the verb
   echoed in `data`. A future cycle that wants more verbs
   (e.g. `agy://cancel-cascade`) extends `DeepLinkAction`
   and the handler's switch — out of scope today.
3. **Top-level `cascade_id` wins.** Both top-level
   `params.cascade_id` and URL-embedded `cascade_id=…` are
   honoured; top-level wins per the brief's "dashboard's
   explicit choice" rule. Empty top-level + empty
   URL-embedded → `-32602 "cascade_id is required"` (parity
   with `CancelRemoteAgentCascade`).
4. **Paste-and-send closed-cascade envelope is `-32005`.**
   Matches the `SendRemoteAgentCascadeMessage` envelope on
   the same condition so a dashboard's retry / fallback
   logic treats them uniformly.
5. **`Config.LastDeepLink` is a `*DeepLinkState`, mirroring
   `AuthState`.** Internal RWMutex; `RecordOpen` /
   `Snapshot`. `NewDispatcher` fabricates a fresh state when
   nil — defence-in-depth, but production always wires one.
6. **`Focused` is `atomic.Bool`.** Matches the project's
   single-bit-state discipline for `*Cascade`. Future
   dashboard cycles read it via `IsFocused()`.
7. **`gofmt -l` clean on every touched file.** 5 files
   touched, 5 files formatted (two new). The 50-file
   pre-existing `cmd/wagyu` dirt and any other untouched
   gofmt dirt stays put.
8. **`go vet ./internal/remotecontrol/ ./internal/remotetransport/
   ./cmd/wagyu/` exit 0.** Pre-existing
   `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf`
   warning stays as-is (out of scope).
9. **`go test -race -count=1`** PASSes on the scoped
   packages. Full `internal/remotecontrol` and
   `internal/remotetransport` packages green.
10. **No third-party deps added.** Stdlib + `coder/websocket`
    + the project's own packages.
11. **`go test ./...` not run.** Scope matches the brief.
12. **No push/merge.** Stays on `exp/rc-server-mode` at the
    same tip. Commit message form: `<verb>(<scope>): <short>
    (<id>)`.
13. **Subagent race respected.** No edits to `openagy/`,
    `openagy-mermaid/`, `openagy-osc8-semicolon/`,
    `openagy-stream-glamour/`. No edits to untracked leftovers
    in `out/`, `.scratch/`, `cmd/wagyu-loop-repro/`.
14. **Re-investigation file (`openagy/.scratch/rc-reinvestigation.md`)
    is read-only reference.** No edits made; this cycle
    closes G9 from that file's checklist.

---

## 存疑 (limitations / open questions)

1. **`LastDeepLink` only tracks `open`.** A future cycle that
   wants a per-verb timestamp would extend `DeepLinkState`
   with `LastFocusAt` / `LastPasteSendAt` slots and stamp
   from the other branches. The cycle keeps the field
   minimal: a single `lastOpenAt time.Time` because G9 is
   the `open` verb's "did we see a dashboard open?" stamp.
2. **`Focus()` is not stale-aware.** Calling `Focus()` on a
   cascade that was already focused produces no
   notification or response difference. A dashboard that
   wants "this is a NEW focus event, not a re-focus" should
   read `IsFocused()` itself before sending the deep-link —
   the cycle intentionally keeps the handler idempotent
   (matches `Cancel()`'s pattern from `rc-cancel-cascade`).
3. **Paste-and-send goroutine errors do not produce a second
   JSON-RPC envelope.** The response has already landed
   with `accepted:true`. The goroutine's failure is surfaced
   via the sink (`cascade/state{phase:"error"}`) and via
   the cascade's bounded error ring. A dashboard that wants
   "did the turn actually succeed?" must look at the
   subsequent `cascade/state{phase:"done"}` notification's
   `stopReason` / `error` fields. This matches
   `cycle-13`'s "no second envelope" convention.
4. **No newline-trim convenience on `text`.** The handler
   whitespace-trims the top-level `text` query param before
   validation (matches the cycle-5 trim-norm in
   `SendMessage`), but a `text` that is `"\nhello\n"` becomes
   `"hello"` rather than `""`. A dashboard that wants to
   reject all-whitespace prompts should pass `text=""`
   instead, which surfaces `-32602 "text is required"`.
5. **The handler does NOT route `agy://…` URLs back through
   the recursive parser.** A `raw` like
   `agy://paste-and-send?url=agy://open` is parsed once; the
   inner `url=…` query value is just a string. The cycle
   keeps the parser non-recursive — re-parsing nested
   deep-links is something a future cycle would add if the
   dashboard ever needs URL-of-URL composition.
6. **Wire-level test 4 reads are race-tolerant.** The
   `TestRemoteControlDeepLink_PasteAndSend_FiresTurn_WS`
   test drains the response AND notifications in a single
   loop because the wire order between the synchronous
   response and the goroutine's first notification is
   non-deterministic. The loop records both and breaks when
   both have been observed — see the test's comment at the
   drain loop for the rationale. `readJSONRPCResponse`
   (which only consumes the response) and
   `readJSONRPCNotifications` (which collects notifications
   before a response) are not individually sufficient
   because each leaves a window where the other might
   consume the missing frame on a different ordering.
7. **`ErrCodeCascadeClosed` is `-32005`.** Matches
   `SendRemoteAgentCascadeMessage`'s envelope so a
   dashboard's retry / fallback logic treats both RPC
   surfaces the same way. If a future cycle wants
   paste-and-send to surface a different code (e.g.
   `-32603`), it would diverge from SendMessage's envelope
   and break the parity — out of scope here.
8. **The `echo.query` map drops repeated keys (first wins).**
   `(*DeepLink).Echo()` runs the URL's parsed query through
   `url.Values.Get` (which returns the first value of a
   repeated key) and serializes to `map[string]string`. A
   dashboard that needs the full multi-value shape should
   read `echo.raw` and re-parse, or extend `Echo()` to emit
   `map[string][]string`. Today no dashboard does.

---

## Why this isn't a `Notify` (notification) method

A reader might ask: "why is `remoteControlDeepLink` a JSON-RPC
Request and not a JSON-RPC Notification?". Three reasons:

1. **The dashboard wants backpressure + an envelope.** An
   `agy://paste-and-send?…` deep-link is fire-and-forget at
   the browser-companion level, but the dispatcher's
   paste-and-send path needs to surface
   `accepted:true`/`err:Invalid_params`/etc. so the
   dashboard's UI can render a "we got your deep-link"
   indicator or a validation error.
2. **Cycle-15's notification-only surface is for
   server-push.** The cascade-state, text_delta, and
   tool_call notifications are dashboard-bound server
   events. Deep-links are the inverse: dashboard-bound
   request bodies that may produce server-side state
   changes plus (optional) notifications. The
   request/response shape is what lets the dashboard
   observe validation errors before triggering the
   goroutine.
3. **Agility for future verbs.** A future cycle that adds
   a verb with side-effects the dashboard might want to
   observe (e.g. `agy://cancel-cascade` returning the
   cascade's `cancelled_at`) gets the response envelope
   for free with this shape.

---

## Wire-level diff vs. `-32601` baseline

Before this cycle, `remoteControlDeepLink` was
`-32601 Method not found` because no case was wired in the
dispatcher's switch. After this cycle, three verbs
(`agy://open`, `agy://focus-cascade`, `agy://paste-and-send`)
are accepted and routed; everything else is `-32602` with
the verb echoed in `data`.

The cycle-15 `STUB_METHODS` / "method not found" baseline
stays intact for the other `RemoteAgent*` methods that the
re-investigation called out as out-of-scope (G6 OAuth, G7
fastpush, G8 ACE, etc.) — those would close in subsequent
cycles. G9 is the bounded goal of this cycle and ships alone.

---

Cycle done. Commit hash: see `PLAN.md:173` after this commit
lands.
