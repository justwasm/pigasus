# The `localharness` layer

> The second reference binary: Antigravity's **agent backend**, shipped inside
> the `google-antigravity` PyPI wheel. What it speaks, what the clone built
> from it, and the invariants that keep the two halves apart.

## What `localharness` is

`agy` and the `google-antigravity` SDKs do not talk to a model directly.
They spawn a closed-source Go binary — `localharness`, 120,344,386 B, SHA-256
`991bbea3edc82a9bc276f2965dea564898a494354ebdc4d9f098e30c715c361e` — and
drive it over a two-phase transport. The binary **owns the agent loop**;
the client side owns the hooks, the policy answers, and the model
credentials.

The binary is not a third-party dependency: it is a Google-internal Go
build, and it embeds `google3/third_party/jetski/cortex` — the *same*
agent engine the `agy` CLI uses. That is why this repository's
`internal/tools`, `internal/compaction` and friends line up with the
reference at all.

The evidence for everything below lives in
[`out/localharness/`](../../out/localharness/):
[`FINDINGS.md`](../../out/localharness/FINDINGS.md) is the protocol report,
[`PARITY.md`](../../out/localharness/PARITY.md) is the byte-parity gap
report (zh-CN),
[`README.md`](../../out/localharness/README.md) is the reproduction recipe,
[`decompile-summary.md`](../../out/localharness/decompile-summary.md) is the
run's statistics.

## The transport — two dialects, one pipe

```
phase 1 — stdio handshake
    client  --[4-byte LE length][InputConfig protobuf]-->  harness stdin
    client  <--[4-byte LE length][OutputConfig protobuf]--  harness stdout

phase 2 — websocket
    client  --ws://<host>:<port>/ , header x-goog-api-key: <key]--> harness
    client  <--[protojson OutputEvent]--   harness
    client  --[protojson InputEvent]-->    harness
```

**The stdio phase is binary protobuf; the websocket phase is protojson
text.** Mixing them up is the single easiest mistake to make, so
`localharness` keeps them in separate functions
(`WriteInputConfig` / `ReadOutputConfig` versus `Conn.Send` / `Conn.Recv`).

## The packages

**All four are public** — they live at the top of the tree, not under
`internal/`, because a third party implementing the SDK side has to import
them. None of them depends on `internal/`; see
[`public-packages.md`](public-packages.md) for the rule and the ranking.

| Package | What it is | Depends on |
|---|---|---|
| `localharness` | the transport: spawn, handshake, connect, frame codec — **and its mirror, the server side** | `localharness/pb` |
| `lifecyclehooks` | the hook protocol: nine types, request/response correlation, per-type verdicts, argument rewriting | `localharness/pb` (in `proto.go` only) |
| `budget` | the budget manager: five limits, the scope window, subagent accounting, the stop reason | nothing |
| `harnessprompt` | the reference's own system instruction, rebuilt byte for byte from a capture | nothing |

**`localharness` does not depend on `lifecyclehooks`.**
The transport is a transport; the hook protocol is a protocol. The
dependency runs the other way and only for the pb bridge.

`lifecyclehooks/lifecyclehooks.go` itself has **no** protobuf
import at all — it is a plain typed model plus a dispatcher. The pb coupling
is quarantined in `lifecyclehooks/proto.go`, so the dispatcher can
be unit-tested without a descriptor.

## `cmd/harness` — the clone of the binary

`cmd/harness` is this repository's reimplementation of the reference
binary: it speaks the same two-phase transport, owns the agent loop, and
is driven by the same clients (the `agy` CLI's SDK path, and the
`google-antigravity` PyPI SDKs).

It is **not** a reimplementation of the agent loop. Everything the loop
needs comes from the packages the CLI already uses:

| what | comes from |
|---|---|
| turn engine | `internal/pigrt` (PiG's `coding.Session`) |
| provider / transport | `internal/backend` (the Gemini transport) + `internal/piagentbridge` |
| tool catalogue | `internal/tools` (`RuntimeCatalog` — the reference's own tool names) |
| tool surface to the model | `internal/tools.HarnessDeclarations` (the harness's **eighteen** wire declarations) |
| system instruction | `harnessprompt` (the reference's own prompt, byte for byte) |
| permission policy | `internal/permissions` + the client's own `PolicyConfig` |
| budget manager | `budget` |
| hook model | `lifecyclehooks` |
| wire protocol | `localharness` |

```bash
go build -o /tmp/harness ./cmd/harness
/tmp/harness --model "Gemini 3.8 Flash (High)" --workspace "$PWD"
```

The client writes the `InputConfig` frame to stdin and reads the
`OutputConfig` frame from stdout; everything after that is the websocket.

**stdout belongs to the protocol.** The `OutputConfig` frame is the only
thing this process may write there — a stray newline would be read as the
first byte of a length prefix and desynchronise the client for good. Every
diagnostic goes to stderr.

### The harness's surface is not the CLI's

The CLI's model sees **twelve** declarations
(`model_tools_1.4.6.json`); the harness's model sees **eighteen**
(`harness_tools_1.4.7.json`), and the two name the same operations
differently — the harness reads a file with `view_file`, writes one with
`write_to_file` and edits one with `replace_file_content`.

The surface is also **client-owned**: the SDK says which tools exist
through `HarnessConfig.harness_side_tools`, and the harness advertises
exactly those. `cmd/harness/sidetools.go` maps the protocol's fields onto
the model-facing names.

### Byte parity

`cmd/harness` has been run against the **reference binary** through the
SDK's own protocol, against one fake Gemini endpoint, and the two were
compared frame by frame and byte by byte:

| | result |
|---|---|
| the Gemini request body | identical after normalising the local time and the random `sessionId` |
| a text-only turn | 8 / 8 frames identical |
| a tool-call cycle | the frame order identical, the sampled step frames identical, the usage payloads identical |
| eight tool probes (`run_command`, `write_to_file`, `grep_search`, `list_dir`, `find_by_name`, `view_file`, `replace_file_content`) | **0 frame differences** |
| a call the client's policy refuses | **0 frame differences**, and the model sees the same failed step |

The gap report is [`out/localharness/PARITY.md`](../../out/localharness/PARITY.md).
The rules the comparison pinned — the four-frame tool cycle, the cumulative
usage, the 0-based `view_file` window, the relaxed runtime validation —
are in that report and in
[`docs/cycles/cycle-306-harness-byte-parity/deliverable.md`](../../docs/cycles/cycle-306-harness-byte-parity/deliverable.md).
The tool surface itself — the action vocabulary, the per-tool result
texts, the diff blocks, the refused call — is in
[`docs/cycles/cycle-307-harness-tool-surface/deliverable.md`](../../docs/cycles/cycle-307-harness-tool-surface/deliverable.md).

### The tool surface

The reference gives **every tool its own action** and **its own result
text**, and the clone matches:

| tool | action | result |
|---|---|---|
| `run_command` | `runCommand` | the step string (`The command exited with code N.` + `Output:`), CRLF line endings |
| `write_to_file`, `replace_file_content` | `editFile` + a `diffBlock` | `{"output": …}` |
| `grep_search` | `searchDirectory` | `{"results": [{"path": …}]}` |
| `list_dir` | `listDirectory` | `{"output": …}` |
| `find_by_name` | `findFile` | `{"output": …}` |
| `view_file` | `viewFile` | `{"output": …}` |

`internal/harnesstools` owns that rendering; `cmd/harness/tools.go`
wraps each PiG `AgentTool` and re-renders the result through it. The
structured payload travels out of band in `AgentToolResult.Details` →
`ai.ToolResultMessage.Details` → `backend.Message.Result`, so the Gemini
transport can put the reference's own shape on the wire.

The wrapper has to forward `ArgumentSchema()` and `PrepareArguments`:
without them PiG validates strictly, and every call the reference *runs*
is refused by the clone.

The reference's **write** tool does two things the catalogue's tool does
not, and the harness does them before the call reaches the catalogue
(`cmd/harness/writetool.go`):

* the file **ends with a newline** — the content it is given is written
  with one, and a content that already ends with one does not get a
  second;
* `Append: true` **appends** — the file's own content is left alone and
  the new content goes behind it. A file that is not there is created.

The reference's **edit** tool adds no newline, so the compensation is the
write tool's alone. The catalogue is shared with the CLI, whose own
reference has a different write tool; the harness's reference is this one.

### The `finish` tool

The client may name a JSON schema for the `finish` tool
(`HarnessConfig.finish_tool_schema_json`). The reference then advertises
**nineteen** declarations instead of eighteen, and the new one sorts into
the list by name — between `find_by_name` and `generate_image`.

The schema travels as the client's own bytes with the **keys sorted**: the
reference carries it as a `google.protobuf.Struct`, so the map's keys come
out sorted. A schema that is not JSON is dropped — the reference cannot
put it in a Struct either, and the session then has no `finish`
declaration at all.

The harness declares its tools with a `Struct`, so *every* schema it sends
travels under `parametersJsonSchema` — the client's schema included, whose
shape says nothing about the dialect it belongs to. The transport used to
guess the dialect from the schema's shape
(`backend.GeminiConfig.JSONSchemaTools` now carries the answer).

A `finish` **call** ends the interaction: four frames, all `SOURCE_MODEL`
to `TARGET_USER`, the text `Finished`, and an action carrying the model's
own arguments. The reference does not ask the model again afterwards, so
the clone's tool result asks the runtime to stop after the batch
(`AgentToolResult.Terminate`).

### The tool-output truncation

`HarnessConfig.tool_output_truncation` is a **oneof**: the SDK sends
`truncate`, and the reference's `error` arm bounds the output the same
way. `max_tokens` is a budget of **three bytes per token**.

The reference keeps the **tail** of the output — the SDK's own docstring
says the opposite ("preserves the beginning (prefix) … truncates the
remainder (tail)") and is stale — and cuts it on a line boundary. The
notice is the reference's own text:

```
<truncated 1 lines>
<truncated 22 bytes>
```

The notice in bytes does **not** count the dropped bytes: it counts their
decimal digits and adds nineteen. Ten captures pin that rule; what the
reference means by it is not known.

### A refused call

The client's policy is asked **before** the call is announced, so a call
that is going to be refused is never announced. The reference then writes
**one** frame — `STATE_ERROR`, `TARGET_USER`, the reason in
`errorMessage`, and `denied by pre-tool hook: <reason>` quoted in the
text. The model sees the failed step (`Created At: …` and
`Encountered error in step execution: …`, no completion time).

The verdict is cached by the call's id, so a dynamic rule costs one round
trip and not two.

### A call that failed

A call that ran and failed is reported as a failed **step** — the same
number of frames as a call that succeeded (announcement, start, result,
done), with the done frame in `STATE_ERROR` and a sentence of the tool's
own:

```
Cannot view file file:///…/nope.txt which does not exist.
```

The model reads the step's own text — the two stamp lines and the reason:

```
Created At: 2026-10-26T04:52:19+08:00
Completed At: 2026-10-26T04:52:19+08:00
Encountered error in step execution: path file:///…/nope.txt does not exist
```

The wording is **per tool**: a read says "path", a listing says
"directory", a content search says "search path", a name search says
"search directory". Two of the four print the path bare in the reason and
two print it as a `file://` URI; the frame's sentence always prints the
URI.

A failed step has **no output**: the `find_by_name` action leaves its
names out, and the `run_command` action carries neither an exit code nor
an output.

A command whose working directory is gone **fails the step** — the
reference does not fall back to the workspace the way the CLI's own
runner does, so the harness checks the directory before the call.

The error text is **not** truncated: the client's truncation budget applies
to the tool's output, and a failed step's text reaches the model whole.

### An invalid tool call

The reference validates a call against its **runtime** schema **before** it
dispatches the tool, and a call that fails the check is an *invalid tool
call*: it is never announced, it never runs, and it is reported in **one**
frame — `STATE_ERROR`, `SOURCE_SYSTEM`, `TARGET_USER`, no action:

```
The model produced an invalid tool call. ("model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly.")
```

The model reads a paragraph rather than a tool error — the invalid branch,
the reason, the guidance and the number of retries that are left:

```
Created At: 2026-10-26T04:51:13+08:00
Error invalid tool call: There was a problem parsing the tool call. 
Error Message: model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly. 
Guidance: You are trying to correct your previous tool call error, …
Retries remaining: 4.
```

The retries start at four, fall by one per refusal and are reset by a call
that **ran** — whether it worked or failed: the reference counts the
*refusals* in a row, and a call that was not refused breaks the row.

When the model has **one** retry left, the reference turns the function
calling off: the next request carries
`"toolConfig":{"functionCallingConfig":{"mode":"NONE"}}` — the declarations
stay on the wire, only the mode changes. That is the model's last chance:
it has to answer with text instead of asking for another call. The mode
goes back to `AUTO` as soon as the counter is reset.

When the retries run out the **run fails**: the trajectory carries the
reason and there is no further model request. The **connection is closed**
afterwards, with the normal WebSocket close (`1000 OK`) — the reference's
*process* stays up, so the clone closes the connection and keeps serving.

Two details of that close are not obvious, and both have a wrong-looking
version that fails:

* the read loop cannot be woken by **cancelling its context** — the
  websocket's read context closes the connection when it is cancelled, and
  the client sees a broken pipe where the reference shows a normal close.
  The close frame is written from the failure path instead, and the read
  loop ends when the peer answers it;
* the frame that carries the reason is written with a context of **its
  own** — the halt that ends the run cancels the turn's context, and a
  write with a cancelled context fails silently, so the client would never
  hear why the run failed.

The runtime schema is **not** the advertised one — a `run_command` with no
`CommandLine` runs, a `list_dir` with no `DirectoryPath` runs, a
`find_by_name` with no `Pattern` runs — so only the rules the captures pin
are implemented, and a call the reference runs is never refused here. The
rules live in `internal/harnesstools/invalid.go`; the wording, the order
and the two tools without a path check are in
`out/localharness/PARITY.md` §3.15.

The guard runs **before** the client's own guards: the reference never
announces a call it is going to refuse, and the client is asked about a
call only when it is announced.

The guards run **once per call**. PiG's pre-tool hook can reach the call
before the announcement has finished — the two run in different
goroutines — so the evaluation is done once per call id and the second
caller waits for it. Without that the client would be asked twice for one
call, and the refusal counter would fall twice for one refusal.

### The server half of the transport

`localharness` now carries **both** halves of the wire, so the
two cannot drift:

| symbol | side | role |
|---|---|---|
| `ReadInputConfig` / `WriteOutputConfig` | server | the length-prefixed stdio handshake |
| `Listen` / `Server.Serve` / `Server.Close` | server | the listener |
| `ServerConn.Send` / `ServerConn.Read` | server | the protojson frame codec |
| `Frame` | both | the union of the two frame shapes (`InitializeConversationEvent` or `InputEvent`) |
| `MarshalFrame` / `UnmarshalFrame` | both | the protojson dialect, in one place |
| `MintAPIKey` | server | the key the client must present |

The API key is checked **before** the websocket upgrade: a request
without the right key gets a 401 and never becomes a websocket.

### What the harness does with the protocol

| protocol event | what the harness does |
|---|---|
| `InitializeConversationEvent` | builds the runtime, the budget manager and the hook dispatcher; answers with the cascade id and the sandbox status |
| `InputEvent.user_input` | runs a turn on its own goroutine (the read loop stays free to deliver the answers the turn is waiting for) |
| `InputEvent.tool_response` | resolves the waiting client-side tool call |
| `InputEvent.call_hook_response` | resolves the waiting hook call |
| `InputEvent.halt_request` | cancels the running turn |
| `InputEvent.session_end_request` | fires the session-end hook and stops |
| `OutputEvent.step_update` | the model's text, thinking, and the tool actions (`ActionRunCommand`, `ActionViewFile`, `ActionCustomTool`, …) |
| `OutputEvent.trajectory_state_update` | running / idle, with the budget's stop reason |
| `OutputEvent.tool_call` | asks the client to run one of the tools it declared |
| `OutputEvent.call_hook_request` | fires one of the nine lifecycle hooks |
| `OutputEvent.usage_update` | the turn's token usage |

### The hooks, from the harness side

The client advertises the hook **types** it has handlers for
(`HarnessConfig.enabled_hooks`); the harness calls each of them at the
right moment and waits for the answer. One request per type — the client
fans it out over the handlers it registered for that type.

The verdict mapping is **fail-closed**: anything that is not an explicit
`ALLOW` denies the guarded action, and a hook that errors, times out or
comes back with an `error_message` denies it too. See
[Known divergences](#known-divergences).

## `localharness` — the transport

```go
conn, err := localharness.Dial(ctx, localharness.Options{
    BinaryPath: "/path/to/localharness",
})
defer conn.Close()

resp, err := conn.Initialize(ctx, &pb.HarnessConfig{ /* ... */ })
```

`Dial` does the whole sequence (spawn → handshake → connect → initialize)
with an **empty** `HarnessConfig`; use `Spawn` and then `Conn.Initialize`
when the conversation needs a real configuration.

| Symbol | Role |
|---|---|
| `Options` | binary path, storage directory, env, client info, connect retries, logger |
| `Dial` / `Spawn` | the full sequence / everything but the initialize exchange |
| `Conn.Send` / `Conn.Recv` | one protojson `InputEvent` / `OutputEvent` |
| `Conn.RecvRaw` | the frame bytes *and* the decoded event |
| `Conn.Initialize` | send `InitializeConversationEvent`, await the response |
| `Conn.Addr` / `Conn.Stderr` | the websocket address, the child's stderr |
| `WriteInputConfig` / `ReadOutputConfig` | the length-prefixed stdio codec, on their own |
| `ConnectWebSocket` | the upgrade step, with the retry schedule |

## `lifecyclehooks` — the hook protocol

The hooks are **bidirectional**. The callback lives on the *client* side;
the harness reaches it over the wire:

```
harness --OutputEvent{call_hook_request{request_id,type,name,...}}--> client
client  --InputEvent{call_hook_response{request_id,...}}------------> harness
```

Nine types, in wire-enum order: `ON_SESSION_START`, `ON_SESSION_END`,
`PRE_TURN`, `POST_TURN`, `PRE_TOOL`, `POST_TOOL`, `ON_TOOL_ERROR`,
`ON_COMPACTION`, `STOP`.

> **Do not confuse this with `internal/hooks`.** That package is a
> same-process, fire-and-forget bus for the TUI's `/btw` side-question: two
> events, one `Decision` type, no request/response correlation. This is the
> *protocol* half.

`lifecyclehooks` follows the **shipped Python SDK**
(`hook_runner.py` / `hook_router.py`, both in the wheel) as the authority,
with the binary's decompiled output as corroboration. Where they disagree,
the SDK wins and the divergence is recorded in the package doc.

The three semantics that are easy to get wrong:

- **PRE_TOOL merges cumulatively.** Hook *N* sees the arguments merged by
  hooks *1..N-1*, and the final response carries the **full** merged map —
  not a delta. The SDK uses `dict.update`, so a key cannot be removed, only
  overwritten.
- **STOP: first `CONTINUE` wins.** The SDK docstring says a non-empty
  reason is required; the code only checks the decision.
- **ON_TOOL_ERROR: first non-empty custom message wins.** Inspect-style
  hooks all run, and their errors do not block.

## `budget` — the limits

```proto
message BudgetConfig {
  uint32 max_model_calls  = 1;
  uint32 max_tool_calls   = 2;
  uint64 max_input_tokens = 3;
  uint64 max_output_tokens = 4;
  uint64 max_total_tokens = 5;
  BudgetScope scope       = 6;   // UNSPECIFIED | LIFETIME | FORWARD_LOOKING
}
```

- A limit of **0 means "not set"** — it never triggers a violation, in any
  scope.
- A limit is exhausted when the evaluated usage is **greater than or equal
  to** the limit (`limit <= used`).
- **Subagent spend never counts against the root trajectory's budget.**

The stop reasons the reference reports are `STOP_REASON_MAX_MODEL_CALLS_EXCEEDED`
… `STOP_REASON_MAX_TOTAL_TOKENS_EXCEEDED`, plus
`STOP_REASON_QUOTA_EXHAUSTED` for a provider-side quota error.

## Load-bearing invariants

1. **The stdio length prefix is little-endian and exactly four bytes.** A
   big-endian prefix desynchronises the stream permanently: the harness
   reads a garbage length and the handshake never recovers.
2. **The first websocket frame is a standalone `InitializeConversationEvent`.**
   It is *not* wrapped in an `InputEvent` — `InputEvent`'s `oneof` has no
   member for it.
3. **The `x-goog-api-key` header carries the key the harness minted in its
   `OutputConfig`.** The upgrade is refused without it.
4. **Unknown fields must be discarded.** The descriptor was recovered from a
   *shipped SDK*, not from the binary's own schema, so the harness can send
   fields this build has never heard of. `DiscardUnknown: true` is not
   optional.
5. **Frames are not size-limited.** `coder/websocket` defaults to a 32 KiB
   read limit; a single `StepUpdate` can exceed it, so the limit is
   disabled explicitly.
6. **A hook verdict the harness cannot interpret must never be treated as
   "allow".** The harness's own string — `"Unrecognized pre-tool hook
   decision %q; blocked execution for safety."` — pins this: an unknown
   decision **blocks**.
7. **`request_id` must be echoed verbatim.** The harness correlates
   strictly; a mismatched id is an unmatched response.
8. **The client must never answer a hook it did not enable.** The SDK only
   advertises the hook types it actually has handlers for
   (`HarnessConfig.enabled_hooks`), and the harness only calls those.
9. **A correlation slot must be reserved before the request is written.**
   The client may answer on the very next frame; a slot registered after
   the write drops that answer.
10. **The API key is checked before the websocket upgrade.** A request
    without the right key gets a 401 and never becomes a websocket.
11. **stdout is the protocol channel.** On the harness side, stdout carries
    the `OutputConfig` frame and nothing else — a stray newline is read as
    the first byte of a length prefix and desynchronises the client for
    good.
12. **The reference's runtime schema is not the advertised one.** The
    decision to run a call is made by the runtime check, not by the
    declaration the model sees: a `run_command` with no `CommandLine` runs,
    a `list_dir` with no `DirectoryPath` runs, a `find_by_name` with no
    `Pattern` runs. Refusing a call the reference runs is a parity bug in
    the other direction, and it is not fixable by tightening the clone's
    own schema.
13. **The guards run once per call.** PiG's pre-tool hook can reach a call
    before the announcement has finished — the two run in different
    goroutines — so the evaluation is done once per call id and the second
    caller waits for it. The reference asks the client about a call once,
    and the refusal counter counts refusals, not evaluations.
14. **The write tool ends the file with a newline, and `Append: true`
    appends.** The catalogue's write tool does neither — it writes the
    content as it is and refuses a file that exists — so the harness does
    the reference's part before the call reaches it
    (`cmd/harness/writetool.go`). The reference's *edit* tool adds no
    newline, so the compensation is the write tool's alone.
15. **A failed run closes the connection.** The reference closes the
    websocket with the normal close (`1000 OK`) when the model uses up its
    retries and the run fails; its *process* stays up. The close frame is
    written from the failure path, not by cancelling the read loop's
    context: a cancelled read context *drops* the connection, and the
    client would see a broken pipe instead of a close. The frame that
    carries the reason is written with a context of its own — the halt
    that ends the run cancels the turn's context
    (`cmd/harness/conversation.go`, `cmd/harness/events.go`).
16. **A frame must never be written with the caller's context.**
    `coder/websocket` closes the **whole connection** when the context of
    a write *in flight* is cancelled: the frame is truncated and the client
    sees a dead connection instead of a close frame. A turn's context is
    cancelled when the turn ends and when the run is halted, so
    `ServerConn.Send` drops a frame whose context is already cancelled —
    the caller gave up — and writes every other frame with a context of its
    own (`localharness/server.go`). Without this the failed run's
    close frame never reaches the client, and the reason frame is lost with
    it. The **client** half (`Conn.Send`) has the same trap; it is left
    alone here because on that side the caller's cancellation decides
    whether a frame still goes out.

## Known divergences

Four places where the clone deliberately differs from the binary. All are
recorded in the package docs; none is silent.

| | Binary | Clone | Why |
|---|---|---|---|
| budget limit merge | presence-bitmap **override** (the second config wins field by field) | keeps the **tighter** non-zero limit | nothing in the recovered strings says which the upstream intends |
| budget check order | output tokens before input tokens | input tokens before output tokens | the two differ only when both token limits are exhausted at the same instant |
| `DECISION_UNSPECIFIED` on the wire | reads as "**not denied**" (`denied = decision == DENY`) | never emitted | a decision the harness has to guess at is exactly what `checkCortexHookResults` refuses to act on |
| hook verdict, harness side | `preToolResultFromProto` (0x101988380) and `preTurnResultFromProto` (0x1019881c0) read an unspecified decision as "**not denied**" | an unspecified decision **denies** | the reference's *inner* layer (`checkCortexHookResults`, 0x101971090) is the strict one, and it refuses to act on a decision it does not recognise; `cmd/harness` follows the strict reading. The STOP hook is the exception — an unrecognised verdict means ALLOW_STOP, because CONTINUE is a loop that never ends |

## What the clone still lacks

The gap audit against the harness is in
[`out/localharness/FINDINGS.md`](../../out/localharness/FINDINGS.md) §4; the
byte-parity gap report is in
[`out/localharness/PARITY.md`](../../out/localharness/PARITY.md) §4. The
open items, in rough order of how much they matter:

- **The tool implementations are the CLI's, not cortex's.** The clone runs
  `internal/tools`; the reference runs its own. The clone re-renders every
  result through `internal/harnesstools` so the *text* matches, but the
  code underneath is the CLI's. The `view_file` output is byte-identical
  to the reference's; the difference is in the **diff block**, where the
  clone computes one change region and the reference computes a real
  multi-hunk line diff.
- **`workspace_containment` is parsed but not enforced as its own check.**
  The client's rules are evaluated (scope, `server_name`, decision, dynamic
  round trip, fail-closed), but the containment flag has no separate code
  path — the clone's own permission policy already keeps writes inside the
  workspace. The measurements say the reference does **not** confine the
  tools either: a write outside the workspace succeeds with
  `workspace_containment` enabled and with
  `enforce_workspace_validation` set. The clone is the stricter of the two.
- **The `Ask` verdict is not the reference's full set.** The client's
  `ASK_USER` decision is a real round trip
  (`PolicyDecisionRequest` / `PolicyDecisionResponse`), and an unanswered
  or uninterpretable answer is a denial. The reference's inner hook layer
  distinguishes `allow` / `deny` / `ask` / `force_ask` / `auto_approve` /
  `deny_unapproved_grant`; the clone denies the ones it cannot read.
- **No tool search, no deferred loading, no skill-lookup action.**
- **Custom agents are listed, not spawned.** The prompt names them, but
  they do not get their own model, tools, skills or `agent_behavior` /
  `max_nesting_depth` / `allowed_subagents`.
- **The reference's runtime call validation is reproduced, but only the rules
  the captures pinned.** The clone now rejects an invalid call the way the
  reference does — one `SOURCE_SYSTEM` frame, no announcement, the
  reference's own paragraph with the retries left (§"An invalid tool call"
  above). What is missing: the rules for the tools that were not probed,
  and the **model-facing text of an unknown tool**. PiG's runtime looks a
  tool up *before* the pre-tool hooks run, so a call to a tool that is not
  in the catalogue never reaches the clone's hook and the transcript
  carries PiG's own `Tool <name> not found` where the reference carries
  its paragraph. The frames are byte-identical; only the model's text
  differs.
- **The tool errors are the reference's shape, not its words.** A failed
  call is rendered as a failed step (`Encountered error in step execution:
  …`) with the tool's own sentence on the frame, and the four tools whose
  wording the captures show use the reference's own words. Every other
  failure keeps the Go error's words in the reference's shape: the
  reference has a sentence per tool **and per kind of failure**, and only
  the "path does not exist" kind was measured.
- **Compaction thresholds are not the same quantity.** The harness uses a
  `uint32` token *count*; the clone uses a `float` *ratio*. They are not
  interconvertible — two independent designs, and the harness does not say
  which is right.
- **The reference's planner is not reproduced.** The harness runs a
  separate planning model and emits `TaskBoundary` steps; `cmd/harness`
  runs the clone's single-loop turn in its place.

## Limitations

- **The Go client half has still never been run against the real binary.**
  The differential that pinned the protocol was driven by `drive.py`, a
  Python transcription of the SDK's own client. `localharness`'s
  `Conn` is therefore validated against the reference *protocol* but only
  against a stub for its own framing.
- **`cmd/harness` has now been run against the reference binary**, through
  that Python client, against one fake model endpoint: the request body
  and the frames match. What the differential did **not** cover is the
  real model's behaviour — streaming deltas, streamed tool arguments,
  thinking blocks, retries — and the SDK's `custom_subagents`, resume and
  compaction paths.
- **The descriptor came from an SDK, not from the binary's own schema.**
  Field numbers and types are believed correct, but the recovered `.proto`
  had several scalar types stripped — `BudgetConfig.max_total_tokens` is
  typed `int64` here. A descriptor drift surfaces as a wire mismatch, not a
  compile error, and `DiscardUnknown` hides *extra* fields but not *wrong*
  types.
- **`Conn.Close` kills the child with `SIGKILL`** (mirroring the SDK's
  `process.kill()`). There is no graceful shutdown of the harness.
- **The harness's own JSON-hook host** — a `hooks.json` with `command` /
  `prompt` handlers — is not modelled; only the client-side callback
  surface is.
- **The harness's `Stop` hook has a second, tool-mediated path** ("Call this
  tool to submit your final structured decision for the hook.") that is not
  implemented.
- **`cmd/harness` serves one connection at a time** and drops a
  `user_input` that arrives while a turn is running, rather than queueing
  it.
- **`cmd/harness` does not implement the interaction API**
  (`UserQuestionsRequest` / `InputEvent.question_response`): the
  `ask_question` tool is not wired to the client's question flow.
- **Only the arm64/macOS wheel was sampled.** The Linux wheel was not
  fetched, and its addresses and package names may differ.
- **Every address in the evidence is specific to one SHA-256.** A new
  wheel version needs the whole seeds → priority → Ghidra → reports chain
  re-run; see [`reverse-engineering.md`](reverse-engineering.md).
