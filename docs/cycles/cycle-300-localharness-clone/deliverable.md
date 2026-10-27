# `cmd/harness` — the clone of the agent backend

**Cycle:** `cycle-300-localharness-clone`
**Subject:** cycle 299 recovered the `localharness` protocol and
reimplemented three of its subsystems. This cycle writes the **other**
half: a `localharness` that speaks the protocol from the *harness* side,
driving the clone's existing agent runtime instead of a reimplementation of
it.
**Evidence:** 【一手】 the recovered descriptor
(`out/localharness/proto/localharness.proto`) and the shipped SDK's own
Python · 【反编译】 decompiled output at confirmed Mach-O addresses ·
【strings】 string-table hits only · 【推测】 hypothesis

---

## 0. Summary

`localharness` is the closed-source agent backend the `agy` CLI and the
`google-antigravity` SDKs spawn. Cycle 299 recovered its wire protocol and
built the *client* half of the transport
(`internal/localharness`), the hook protocol
(`internal/lifecyclehooks`) and the budget manager (`internal/budget`).

This cycle builds the **server** half:

| | |
|---|---|
| `internal/localharness/server.go` | the harness side of the transport: the stdio handshake, the listener, the API-key check, the frame codec |
| `internal/localharness/frame.go` | the protojson frame dialect, shared by both halves so they cannot drift |
| `cmd/harness/` | the clone of the binary: it owns a conversation, runs the turns, fires the hooks, enforces the budget and delegates the client-side tools |
| `docs/guide/localharness.md` | the guide, updated with the server half and the clone |

**The design constraint that shaped everything: reuse, not
reimplementation.** The harness does not implement an agent loop. It drives
`internal/pigrt` — the same PiG-based runtime the CLI's `--print` path
uses — with the same tool catalogue, the same transport dispatch, the same
permission policy and the same budget manager. The only thing this cycle
writes is the *protocol* layer: what goes on the wire, and when.

```
cmd/harness ──┬── internal/localharness  ── the wire (both halves)
               ├── internal/pigrt          ── the turn engine (PiG coding.Session)
               ├── internal/tools          ── the reference's tool names
               ├── internal/piagentbridge  ── the provider
               ├── internal/backendselect  ── the transport dispatch
               ├── internal/permissions    ── the policy in front of every tool
               ├── internal/budget         ── the limits
               └── internal/lifecyclehooks ── the hook model
```

---

## 1. The server half of the transport

`internal/localharness` now carries both halves of the wire.

| symbol | side | role |
|---|---|---|
| `ReadInputConfig` / `WriteOutputConfig` | server | the length-prefixed stdio handshake |
| `Listen` / `Server.Serve` / `Server.Close` | server | the listener |
| `ServerConn.Send` / `ServerConn.Read` | server | the protojson frame codec |
| `Frame` | both | the union of the two frame shapes |
| `MarshalFrame` / `UnmarshalFrame` | both | the protojson dialect, in one place |
| `MintAPIKey` | server | the key the client must present |

Three facts drove the shape of this:

1. **The first frame is not an `InputEvent`.** `InitializeConversationEvent`
   is a standalone message; `InputEvent`'s `oneof` has no member for it.
   `ServerConn.Read` therefore returns a `Frame` — a union — and classifies
   a frame by trying it against both messages.
2. **The API key must be checked before the upgrade.** A request without
   the right key gets a 401 and never becomes a websocket. The check lives
   in the HTTP handler, not in the session.
3. **The frames are not size-limited.** `coder/websocket` defaults to a
   32 KiB read limit; a single `StepUpdate` with a large tool payload
   exceeds it, so `SetReadLimit(-1)` is explicit.

The protojson dialect now lives in one file (`frame.go`), and the client
side's `Send` / `RecvRaw` were refactored to call it. The dialect is
camelCase field names, int64/uint64 as JSON **strings**, int32/uint32 as
JSON **numbers**, enums as their names, a `oneof` flattened into the
parent, and `DiscardUnknown` on the way in.

> **Why `DiscardUnknown` is not optional.** The descriptor was recovered
> from a *shipped SDK*, not from the harness binary's own schema. A field
> the harness adds must not break the connection — but a *wrong type* in a
> field both sides know still surfaces as a wire mismatch.

---

## 2. `cmd/harness` — the clone

### 2.1 The lifecycle

```
stdin  ──[InputConfig frame]──▶  harness
stdout ◀──[OutputConfig frame]──  harness      (port + api key)

websocket ──[InitializeConversationEvent]──▶   harness
websocket ◀──[OutputEvent{initialize_conversation_response}]──
websocket ──[InputEvent{user_input}]──▶          harness
websocket ◀──[OutputEvent{step_update | trajectory_state_update | …}]──
```

One connection carries one conversation. The conversation owns:

- the agent runtime (`internal/pigrt`), one per conversation, because PiG's
  session owns a single turn queue;
- the budget manager (`internal/budget`), seeded from the config's
  `budget_config`;
- the hook dispatcher (the harness side of `internal/lifecyclehooks`);
- the tools the client declared.

### 2.2 Concurrency

The read loop is single-threaded: it decodes a frame and dispatches it. A
turn, however, blocks — the model streams, tools run, and a tool may wait
for the client's answer. So a turn runs on its own goroutine, and the read
loop stays free to deliver the answers that turn is waiting for.

**The correlation slot is reserved before the request is written.** This is
the one race that mattered: the client may answer on the very next frame,
and a slot registered *after* the write drops that answer. `Conversation.await`
takes the send as a callback so the ordering is structural rather than a
convention.

One turn at a time: a second `user_input` while a turn is running is
dropped, because PiG's session is not safe for concurrent sends.

### 2.3 The model and the transport

The model comes from the command line, else from the config's first
`models` entry, else from the reference's default. The transport comes
from the clone's own dispatch (`internal/backendselect`), so a catalogue
model goes to CloudCode and a configured model to its own endpoint — the
same rule the CLI uses. The provider is built with
`printmode.PigPrintModel`, which is the same helper the `--print` path
uses.

### 2.4 The tools

`internal/tools.RuntimeCatalog` supplies the reference's own tool names
(`run_command`, `view_file`, `write_to_file`, `list_dir`, `grep_search`,
…) and `internal/tools.ModelDeclarations` the twelve declarations the
model sees. The permission policy (`internal/permissions`) sits in front of
every call through PiG's `BeforeToolCall` hook, exactly as it does in the
CLI.

The tools the *client* declared (`HarnessConfig.tools`) are the other
half of the reference's split: the harness cannot run them, so it asks the
client over the wire.

```
harness  --[OutputEvent{tool_call}]-->  client
harness  <--[InputEvent{tool_response}]-  client
```

The correlation id is the tool call id PiG handed the tool.

### 2.5 The hooks

The client advertises the hook **types** it has handlers for
(`HarnessConfig.enabled_hooks`); the harness calls each of them at the
right moment and waits for the answer. One request per type — the shipped
SDK's `hook_router` dispatches on `req.type` and fans the request out over
the handlers it registered for that type, so the per-handler loop the
reference runs internally happens on the client's side of the wire.

| hook | fires | can |
|---|---|---|
| `ON_SESSION_START` / `ON_SESSION_END` | session boundaries | observe |
| `PRE_TURN` | before a turn | deny the turn |
| `POST_TURN` | after a turn | observe |
| `PRE_TOOL` | before every tool call | deny the call, or rewrite its arguments |
| `POST_TOOL` | after every tool call | observe |
| `ON_TOOL_ERROR` | after a failed tool call | observe |
| `ON_COMPACTION` | when the context is compacted | observe |
| `STOP` | when the trajectory goes idle | continue the loop |

`PRE_TOOL` maps onto PiG's `BeforeToolCallHook`: `Block` denies, `Args`
replaces the call's arguments.

### 2.6 The budget

`internal/budget` is seeded from the config's `budget_config`. Model calls
are charged on `MessageEndEvent`, tool calls on `ToolExecutionStartEvent`,
and tokens on the assistant message's usage. When a limit is exhausted the
harness reports it as a `TrajectoryStateUpdate` with the matching
`STOP_REASON_MAX_*`.

### 2.7 The permission policy

`internal/permissions`, loaded from the agy `settings.json` the same way
the print path loads it. It gates every tool call, MCP tools included,
through the same `BeforeToolCall` hook the `PRE_TOOL` hook uses.

---

## 3. The one deliberate divergence

The reference's **wire** layer is permissive about a verdict it cannot
interpret.

> 【反编译】 `preToolResultFromProto` (0x101988380) computes
> `denied = (decision == DENY)`, and `preTurnResultFromProto` (0x1019881c0)
> does the same. A `DECISION_UNSPECIFIED` arriving over the wire therefore
> reads as "**not denied**" on the harness side.

The reference's **inner** layer is the strict one.

> 【反编译】 `checkCortexHookResults` (0x101971090) walks the hook results
> and compares the decision against a set of strings — `"deny"`, `"ask"`,
> `"allow"`, … — and refuses to act on one it does not recognise.
>
> 【strings】 `"Unrecognized pre-tool hook decision %q; blocked execution
> for safety."` is a verbatim string-table hit of the reference binary.

`cmd/harness` follows the **strict** reading: anything that is not an
explicit `ALLOW` denies the guarded action, and a hook that errors, times
out or comes back with an `error_message` denies it too. The reasoning is
recorded in `cmd/harness/hooks.go`: a verdict the harness cannot interpret
is exactly the case the reference's own inner layer refuses to guess at.

**The STOP hook is the exception.** An unrecognised STOP verdict means
`ALLOW_STOP`, because the fail-closed alternative — `CONTINUE` — is a loop
that never ends. The continuation count is bounded at 16 as a second line
of defence.

---

## 4. The tests

| test | what it pins |
|---|---|
| `internal/localharness/frame_test.go` | the protojson dialect: camelCase, int64 as a string, int32 as a number, enums as names, unknown fields discarded, garbage rejected |
| `internal/localharness/server_test.go` | the stdio handshake both ways, the API-key rejection, the initialize frame, the input events |
| `cmd/harness/harness_test.go` | the flags, the fail-closed verdicts, the whole protocol end to end (handshake → websocket → initialize → a real turn against the deterministic shim), the client-tool round trip, the hook round trip |

The end-to-end test drives the **real** transport: a pipe stands in for
stdio, a websocket client stands in for the SDK, and the only stub is the
model transport — `backend.NewMock()`, the clone's own deterministic
stand-in.

---

## 5. Limitations

- **The clone has never been driven by the reference binary.** The wire is
  transcribed from the SDK and the recovered descriptor; the tests use the
  clone's own client code and a websocket stub.
- **One connection at a time.** The reference harness may multiplex.
- **A `user_input` that arrives during a turn is dropped**, not queued.
- **The interaction API is not implemented.** `UserQuestionsRequest` and
  `InputEvent.question_response` exist in the descriptor but are not wired
  to the `ask_question` tool.
- **The reference's planner is not reproduced.** The harness runs a
  separate planning model and emits `TaskBoundary` steps; the clone's
  single-loop turn stands in for it.
- **The policy answers are the clone's static policy.** The request and
  response plumbing exists; the harness's dynamic rules,
  `server_name` matching and workspace containment do not.
- **`ActionEditFile` carries no diff block**, and
  `ActionListDirectory` carries no per-entry results: the clone's tools
  return text, not structured listings.
- **`supplemental_media` on a tool response is ignored** — PiG's
  tool-result content has no media slot this bridge can fill.
- **The `ON_TOOL_ERROR` hook's custom message is observed, not applied.**
  PiG has already produced the error result by the time the end event
  fires, so the harness can only log it.
- **The origin check is disabled** (`InsecureSkipVerify`). The listener is
  loopback-only and gated on an API key, and the client is not a browser —
  but a browser on the same machine could reach it if it learned the key.

---

## 6. 硬约束 / 存疑 / limitations

**硬约束 (hard constraints).**

1. The stdio length prefix is 4 bytes, little-endian. A big-endian prefix
   desynchronises the stream permanently.
2. The first websocket frame is a standalone `InitializeConversationEvent`,
   not wrapped in an `InputEvent`.
3. The `x-goog-api-key` header carries the key from the `OutputConfig`; the
   upgrade is refused without it.
4. Unknown fields must be discarded on decode.
5. The websocket read limit must be disabled.
6. On the harness side, stdout carries the `OutputConfig` frame and
   nothing else.
7. A correlation slot must be reserved before its request is written.
8. The handshake timeout bounds the HTTP request, **not** the session: a
   conversation may legitimately run for hours.

**存疑 (open questions).**

- The reference's *inner* hook layer distinguishes `ask` /
  `force_ask` / `auto_approve` / `deny_unapproved_grant`. The wire only
  carries `ALLOW` / `DENY`, so how the harness maps the inner decisions
  onto the wire pair is not established by the evidence. This clone does
  not attempt it.
- `CallHookRequest.name` is used by the client only for logging; the
  harness's spelling of it ("pre_tool", "on_compaction") is the SDK's, but
  nothing depends on it.
- Whether the reference serves more than one connection at a time is not
  established.

**limitations.** See §5.
