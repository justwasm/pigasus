# Cycle 347 deliverable: the agent ↔ browser bridge, reproduced — one broker, three RPCs, one tool

**Cycle:** `cycle-347-agent-browser-bridge`
**Subject:** the reference's IDE turn and its built-in browser. The
reference keeps the two in one process and lets them talk over three
`LanguageServerService` RPCs; the clone had the IDE half (its frontend
is the reference's own bundle, byte for byte) and no LS half at all.
**Method:** the reference's own descriptor set read first-hand, its
frontend bundle read for the consumer side, and its Go symbols
decompiled for the broker's policies.

---

## 0. The short answer

The clone now speaks the reference's browser bridge: a broker, the
three RPCs, and the reference's **one** browser tool on the IDE turn's
wire.

| | before | now |
|---|---|---|
| `SubscribeToAgentBrowserRequests` | the default instance `{}` | a real server-streaming subscription; one frame per action |
| `UpdateAgentBrowserSubscription` | the default instance `{}` | records the window's displayed conversation |
| `FulfillAgentBrowserRequest` | the default instance `{}` | hands the IDE half's answer to the waiting agent half |
| the IDE turn's tools | the reference's 1.4.6 **CLI** twelve | those twelve plus **`webview_browser`** (20 actions) |

The load-bearing part is that there is **one broker per daemon**. The
agent half blocks on an answer; the IDE half delivers it. Two brokers
would be two conversations.

---

## 1. What the evidence said

### 1.1 The wire shapes — 【一手】

`out/ide-language-server/protos/language_server.protoset`, the
reference's own descriptor set:

```
exa.language_server_pb.AgentBrowserWireRequest
  request_id, cascade_id, root_conversation_id,
  action, params_json, target_window_id

exa.language_server_pb.AgentBrowserWireResponse
  request_id, ok, result_json, error, error_code,
  window_id, screenshot_png

exa.language_server_pb.SubscribeToAgentBrowserRequestsRequest
  window_id, surface, open_conversation_ids,
  supports_isolated_world, displayed_root_conversation_id,
  backend_instructions
exa.language_server_pb.SubscribeToAgentBrowserRequestsResponse
  request  (AgentBrowserWireRequest)

exa.language_server_pb.UpdateAgentBrowserSubscriptionRequest
  window_id, displayed_root_conversation_id, supports_isolated_world

exa.language_server_pb.FulfillAgentBrowserRequestRequest
  response  (AgentBrowserWireResponse)
```

The member names on the wire are proto-JSON's lowerCamelCase
spellings, and the reference's own frontend reads exactly those:
`J.paramsJson`, `J.requestId`, `J.targetWindowId`, `windowId`,
`openConversationIds`, `displayedRootConversationId`,
`supportsIsolatedWorld`, `backendInstructions` 【一手 — the frontend
bundle】.

Both response messages are empty, so both answer `{}`.

### 1.2 The reference's own refusals — 【一手】

`out/langserver/coverage-351.json`, measured against the reference:

```
grpc-status 3: invalid_argument: UpdateAgentBrowserSubscription requires window_id
grpc-status 3: invalid_argument: FulfillAgentBrowserRequest requires a non-nil response payload
```

Those two sentences are a contract. The clone reproduces them
verbatim, and adds the third the reference implies — a response
without a request id — with the same shape.

### 1.3 The broker's policies — 【反编译】

`agentbrowser.(*StreamBroker)` carries `Subscribe`,
`SubscribeWithInfo`, `Request`, `Fulfill`, `Store`, `Take`, `Resolve`,
`SupportsIsolatedWorld`, `WaitForSupportsIsolatedWorld`,
`DeleteRootConversation`, `RecordUserMessageWindow`,
`UpdateWindowDisplayedConversation`, `SubscriberCount`, plus
`(*OwnerLivenessFunc).IsOwnerActive`.

From those, and from the strings around them:

- **window selection** (`selectWindowForRootLocked`): the request's
  `target_window_id` wins; then the window that sent the user's last
  message (`sender_window_id`, with a grace window); then the window
  that *displays* the conversation (`displayed_conversation`); then a
  window that has it open; then the only window; otherwise
  `no active AGY Hub webview browser windows connected to Language Server`.
- **timeout** (`requestTimeoutForAction`): 30 s base; a batch gets
  20 s per step clamped to [20 s, 120 s], plus 10 s of slack, never
  below the base. 2 steps → 50 s; 10 steps → 130 s.
- **one owner per conversation**: `browser is currently in use by
  another browser subagent in this conversation`.
- **four distinct fulfil failures**: `agentbrowser.Response cannot be
  nil`, `agentbrowser.Response.RequestID is required`, `no pending
  agent browser request found for request_id=%q (may have timed out or
  already been fulfilled)`, `window_id mismatch for request_id=%q: got
  %q, want %q`.
- **the timeout message**: `timed out after %s waiting for AUIT webview
  to fulfill browser action %q (request_id=%s)`.

---

## 2. The tool: one name, twenty actions — 【反编译】

The reference's IDE turn advertises **one** browser tool:
`webview_browser`, with a twenty-value `action` enum:

```
open  navigate  snapshot  click  fill  type  press  hover  scroll
select_option  get_state  wait  back  forward  reload  list-tabs
new-tab  activate-tab  close-tab  batch
```

A batch's steps take the same enum **minus `batch` itself** (19
values), so a batch cannot nest.

The 23 names the binary carries (`open_browser_url`,
`read_browser_page`, `execute_browser_javascript`,
`capture_browser_screenshot`, `click_browser_pixel`, `browser_get_dom`,
`list_browser_pages`, `browser_subagent` …) are a **catalogue**, not
declarations — this was the one thing the earlier reading got wrong,
and it is the reason the earlier evidence report said "the IDE turn
has no browser tool at all".

The declaration is a proto-JSON `learning.genai.beyond.Schema`:
upper-case type names, the member order type → description → enum →
items → minItems → minimum → maximum → minLength, `int64` bounds
written as **strings**. The clone reproduces it byte for byte,
including the 611-byte description.

> PiG's vendored `third_party/jsonschema` fork ignores a string-valued
> `minimum` / `maxItems` silently (`numVal` returns nil for a string),
> so the reference's string bounds are harmless at runtime — the
> constraint is simply not enforced.

---

## 3. The parameters — 【反编译】

`tools.buildStepParams` builds every request's parameters. Six keys
are always there:

| key | value |
|---|---|
| `tabId` | `agent-tab-1` by default; `__new_tab__` for `new-tab` |
| `includePerception` | `true` |
| `captureScreenshot` | `true` |
| `captureSnapshot` | `true` |
| `coordinateSystem` | **`normalized_1000`** — the static string at `0x1068aff30` |
| `normalizedMaxDimension` | `1000` |

Then each action adds its own:

| action | its keys |
|---|---|
| `click`, `hover` | `uid`, or `x` + `y` (both in [0, 1000]) |
| `fill` | `uid`, `text`, `value` |
| `type` | `text`, `value` |
| `press` | `key` |
| `select_option` | `uid`, `value`, `text` |
| `scroll` | `direction` (default `down`), `amount` (default `400`), `deltaX`, `deltaY` |
| `wait` | `ms` (default `1000`, max `30000`), optional `text` |
| `open`, `navigate` | `url` |
| `new-tab` | `url` |
| `snapshot`, `back`, `forward`, `reload`, `get_state`, `list-tabs`, `activate-tab`, `close-tab` | — |

The reference's own frontend reads **none** of `coordinateSystem`,
`normalizedMaxDimension` or a `normalized` flag. It reads `x` / `y`
and decides by itself: in [0, 1000] they are normalised and get
converted to pixels (`WS(a, b, c)` divides by 1000 and multiplies by
the captured size); outside, they are pixels already 【一手 — the
frontend bundle】.

URLs are fenced: `http://`, `https://` and `about:blank` only; a host
is required; `169.254.169.254` is cloud metadata; `169.254.0.0/16` and
`fe80::/10` are link-local. The reference's own wording is used
verbatim (`disallowed URL scheme %q in %q: only http://, https://, and
about:blank URLs are permitted`, …).

---

## 4. The code

### 4.1 `internal/agentbrowser` — the bridge, without the clone

| file | what it holds |
|---|---|
| `wire.go` | the five wire shapes, in proto-JSON's lowerCamelCase |
| `broker.go` | the broker: subscribe, request, fulfil, the owner fence, the timeout |
| `broker_window.go` | the window selection order and the timeout policy |
| `params.go` | the argument struct, the parameter builder, the URL rules |
| `declaration.go` | the tool's declaration, byte for byte |
| `tool.go` | the executor and the summary the model reads back |

The package imports **nothing of the clone's** — no PiG, no
`internal/tools`. That is what makes the import direction safe:
`internal/piagentbridge` → `internal/agentbrowser` → (nothing).

### 4.2 `internal/remotecontrol/dispatch_agentbrowser.go` — the three RPCs

| RPC | where | answers |
|---|---|---|
| `SubscribeToAgentBrowserRequests` | `DispatchStream` | one `{"request": {…}}` frame per action |
| `UpdateAgentBrowserSubscription` | `routeAgentBrowserMethods` | `{}` |
| `FulfillAgentBrowserRequest` | `routeAgentBrowserMethods` | `{}` |

The refusals:

| failure | code | message |
|---|---|---|
| no `window_id` | `-32602` | `UpdateAgentBrowserSubscription requires window_id` |
| no `response` | `-32602` | `FulfillAgentBrowserRequest requires a non-nil response payload` |
| no `response.request_id` | `-32602` | `FulfillAgentBrowserRequest requires response.request_id` |
| the request is not pending | `-32018` | `not_found` |

`-32602` is the Connect dialect's `invalid_argument` (HTTP 400,
`grpc-status 3`); `-32018` is its `not_found` (HTTP 404).

### 4.3 `internal/piagentbridge/browser_tool.go` — the adapter

`BrowserTool(broker, opts)` returns a `piagent.AgentTool`; the
declaration is the reference's own, and the executor runs the action
in the broker. `BrowserDeclaration()` hands the same declaration to a
caller that wants to advertise it beside the reference's twelve.

### 4.4 The wiring — `internal/rchost`

One broker per daemon, handed to both halves:

```
remotecontrol.Config.Browser          ← the three RPCs
piagentbridge.RemoteCascadeOptions.Browser  ← the turn's tool
```

The tool is advertised **only when a broker is present** and only on
the **IDE turn** (`remoteCascadeTools`); the CLI and print paths do
not carry it, exactly as the reference does not.

---

## 5. What is still open

- **No end-to-end run yet.** The unit tests cover the broker, the
  parameter builder, the three RPCs and the adapter, but no real IDE
  window has been in the room: the IDE half (the reference's frontend,
  byte for byte) has to subscribe, answer, and be answered back.
- **`deltaX` / `deltaY` are the clone's own arithmetic.** The
  reference's argument struct has no such fields, so either the LS
  computed them or the webview side did. The clone derives them from
  `direction` + `amount`, which lands on the same number either way.
- **`backend_instructions` is carried but not consumed.** The IDE
  hands it over with the subscription; the reference turns it into the
  browser agent's operating manual. The clone stores it on the
  subscription and does not yet use it as a prompt.
- **The Linux shape is untested.** Every measurement here is macOS
  arm64.

---

## Hard constraints

1. **`SubscribeToAgentBrowserRequests` is server-streaming.** It cannot
   be answered as a unary.
2. **The reference's two refusal sentences are a contract**, and so is
   the third one the clone derives from them.
3. **One broker per daemon.** The two halves must land in the same
   instance.
4. **The IDE turn has exactly one browser tool**, `webview_browser`,
   with twenty actions. The 23 catalogue names are not declarations.
5. **`coordinateSystem` is `normalized_1000`**, not `normalized`.
6. **One owner per conversation**, and the fence's wording is part of
   the contract.

## Open questions

- Who really executes the action: the IDE window's own `<webview>`
  (this cycle's reading) or the LS's own Chrome with the IDE only
  displaying it? The frontend's code reads as the former, but the LS
  side was not decompiled far enough to confirm it 【推测】.
- What `supports_isolated_world` isolates. The IDE reports true in
  Electron and false in a browser; what that means for the page's own
  JavaScript is not measured 【推测】.

## Limitations

- The reference's stream emits **no frame** in standalone + mock mode
  (it closes the connection), so "what the stream pushes" has no
  first-hand measurement: the frame's shape comes from the descriptor
  and the field names from the IDE's own frontend calls.
- The reference's measured answers above are from
  `out/langserver/coverage-351.json`; the clone's own answers were
  measured against a running Pigasus.app, whose port and CSRF token
  change every start.
