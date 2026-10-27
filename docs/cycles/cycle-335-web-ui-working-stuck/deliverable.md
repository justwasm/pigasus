# The Web UI stuck on "Working" in reference mode — deliverable

**Cycle:** `cycle-335-web-ui-working-stuck`
**Subject:** the operator reported that sending a message in the Web UI of
the reference mode hangs: *"启动后在 web ui 发送消息会卡住，一直 working
却没有输出"* — the page sits on **Working** and no output ever appears.
The cause is not the turn: the reference's own bundle runs the page on the
**fetch transport**, where the app opens six long-lived server streams that
take every connection Chrome keeps per host, so the turn's own calls queue
behind them and never run.
**Evidence:** 【一手】 the reference's own bundle and its transport
selector (`out/web-ui-working-stuck/bundle_transport.txt`); 【一手】 Chrome's
network panel, both transports on the same rig
(`out/web-ui-working-stuck/network_census.txt`); 【一手】 the page's own
console, the rendered UI and the rig's log
(`out/web-ui-working-stuck/console_and_turn.txt`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the transport the reference's page runs on** | fetch — the bundle's own default | WebSocket — chosen through the URL, the reference's bytes untouched |
| **the HTTP requests the page makes** | 628, a large share of them `(pending)` forever | **0** — every RPC goes over one WebSocket |
| **the send** | the page sat on **Working**; the answer appeared about two minutes later | the answer is rendered in the same second the model finishes |
| **the clone's own Web UI** | already on the WebSocket transport (the bundle is patched) | unchanged |
| **the operator's lever** | the URL: adding `?useWebSocket=true` by hand already worked (§2.5) | the same transport, reached without touching the URL |

---

## 1. What the operator saw

The operator ran the reference mode — `pigasus web --reference-backend=…`
— opened the Web UI and sent a message. The page went to **Working** and
stayed there: no answer, no error, nothing in the log.

【一手】 reproduced on the same rig, in Chrome, with the reference's own
account (`out/web-ui-working-stuck/console_and_turn.txt`):

```
Conversation Log
reply with the word quince
1:38 AM
Working
```

45 seconds after the send. The rig's log says the model itself was fast:

```
2026/10/27 01:38:25 refbackend: minimax-m3.1-flash → MiniMax-M3.1-Flash (stream, 1 msg, 12 tool)
2026/10/27 01:38:27 refbackend: POST /v1internal:streamGenerateContent?alt=sse -> 200 (1.263s)
2026/10/27 01:38:29 refbackend: POST /v1internal:streamGenerateContent?alt=sse -> 200 (3.271s)
```

The turn ran in four seconds. The page was still waiting. So the defect is
on the browser's side of the wire, not in the agent.

---

## 2. The transport the reference's page runs on

### 2.1 The bundle decides from the URL

【一手】 the reference's own frontend, byte for byte
(`out/ide-language-server/frontend/main.js`, md5
`9f96dc058705b85c1191c6c5ce7bf1a9` — the same file as
`internal/rcfrontend/assets/main.js`):

```js
function AHc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");
return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="3"?"hub":"off"}
```

| query | transport |
|---|---|
| `useWebSocket=true` | `explicit` — WebSocket |
| `useWebSocket=false` (or any other value) | `off` — fetch |
| `wsTransport=3` | `hub` — WebSocket, with a fetch fallback after a 4 s dial timeout |
| **nothing** | `off` — **fetch** |

So a page served without a query parameter runs on the fetch transport. The
reference's own server serves it that way.

### 2.2 Why the fetch transport hangs

【一手】 Chrome's network panel, the reference mode, the fetch transport
(`out/web-ui-working-stuck/network_census.txt`): 628 requests, and the
requests that never finished are the interesting ones.

```
  JetboxSubscribeToState         done: 8    pending: 20
  JetboxSubscribeToSummaries     done: 8    pending: 20
  ProjectUpdatesStream           done: 8    pending: 20
  SubscribeToMcpServerStates     done: 8    pending: 20
  SubscribeToPlugins             done: 8    pending: 20
  StreamAgentStateUpdates        done: 6    pending:  9
  RequestAgentStatePageUpdate    done: 0    pending: 15
  RecordAnalyticsEvent           done: 87   pending: 22
  GetTurnDiff                    done: 0    pending:  4
  GetAllSkills                   done: 12   pending:  6
  GetAllWorkflows                done: 13   pending:  4
```

Six names, six server streams. Over HTTP/1.1 each one is a response that
never ends, and each one holds one of Chrome's six connections per host.
The pool is exactly full. Everything after it — including the calls the
*turn* needs — waits in the queue.

That is also why the symptom is "stuck" rather than "wrong": the queue is
FIFO and the long streams never finish, so a queued request waits for a
slot that only frees when the app gives up a stream. The app's own logs
show the same shape: `Cold load completed: 25154ms`, and the answer that
the model produced in four seconds was rendered about two minutes later.

### 2.3 The clone's own page never had the problem

The clone's Web UI serves the *same* bundle, but it patches it:
`internal/rcfrontend` runs `patches.Apply` over `main.js` on the way out,
and the `websocket-transport-default` patch
(`internal/rctunnel/patches/registry.go`) makes `AHc()` default to
`explicit`, keeping `?useWebSocket=false` as the per-page escape hatch.

【一手】 the clone's own mode, measured: the page loads, the conversation
list shows 34 conversations, and the **only** POST is the clone's own
`/__agy/api/accounts` — no RPC POST at all.

The patch's own comment already says why `hub` is not the choice: hub falls
back to fetch after a four-second dial timeout, which is the very path this
cycle has to avoid.

### 2.4 The reference mode has no lever but the URL

Reference mode serves the reference's **own** bytes: its own document, its
own bundle, its own ETag and SPA fallback
(`internal/refbackend/assets.go`). Patching the bundle would break the one
property the mode exists for — the page the browser gets is the page the
reference would have given it — and it would break the ETag contract with
the reference as well.

The transport, though, is the clone's own wire decision: the clone's server
is what the browser talks to. And the bundle reads the transport out of the
URL, so the URL is the lever.

### 2.5 The lever was already there — the fix makes it the default

This is worth stating plainly, because it is easy to overstate what the
cycle fixed: the stall was **not** an impossibility, it was a default.

【一手】 the binary from **before** this cycle's first commit
(`05b302c1`, built in a scratch worktree) was run in reference mode and
opened with the parameter added by hand:

```
http://127.0.0.1:8897/?token=…&useWebSocket=true
```

The pre-fix proxy does not redirect, and it passes the query through as it
is — so the bundle read `explicit` and ran on the WebSocket. Everything
worked (`out/web-ui-working-stuck/prefix_manual_workaround.txt`):

* the page booted fully — conversation history, project tree, model picker;
* a one-word prompt was answered in seconds
  (`02:06:33 refbackend: … streamGenerateContent?alt=sse -> 200 (2.299s)`);
* the resource timeline held **4 entries, all static** — no fetch, no XHR,
  so every RPC went over the one WebSocket;
* the console held no error and no warning.

So the reference mode's WebSocket path was functional before this cycle,
and the manual URL was already an escape hatch from the stall. What the
cycle changes is the **default**: the operator opens the page the way the
rig prints the URL and it works, instead of having to know that the
reference's bundle reads its transport out of the query string.

---

## 3. The fix

The fix is one redirect: the document is served with a query that names the
transport the reference's bundle already knows how to read (§2.1). It does
not add a capability — it changes the default (§2.5).

### 3.1 The document is redirected to the WebSocket transport

`internal/refbackend/transport.go` (new) and the hook in
`internal/refbackend/assets.go`: when the proxy answers the reference's HTML
document and the request names no transport, it answers **302** with the
same path and query plus `useWebSocket=true`.

```
$ curl -s -D - -o /dev/null "http://127.0.0.1:8893/?token=UTOJ…"
HTTP/1.1 302 Found
Location: /?token=UTOJ…&useWebSocket=true
Cache-Control: no-store
```

The bundle then reads `explicit` and builds its WebSocket client against
`ws://<host>/connect-websocket` — the endpoint the clone's transport
already serves (`internal/remotetransport/tcp_server.go`).

Why a redirect and not a patched bundle: the reference's bytes stay the
reference's bytes (md5 unchanged, the ETag and the SPA fallback are
untouched), the change is visible to the operator in the address bar, and
it is reversible without a rebuild.

The path and the query string are carried over as they are: the document may
be requested under any path (the reference's SPA fallback answers with it),
and the secret the operator shares with the LAN clients travels in the query
— dropping it would lock them out.

### 3.2 What the operator can still do

* `?useWebSocket=false` — the bundle reads it as `off`, and the proxy does
  not redirect. The old behaviour, including the stall, is still reachable.
* `?wsTransport=…` — any value of the bundle's own parameter is honoured.

### 3.3 Two clone-side hardenings found on the way

Neither of them is the cause of the stall — the stall is the connection
pool, and the `skills_fetch_failed deadline_exceeded` in the log is that
same pool (the panel's own POST is one of the pending requests). They were
found while measuring, they are correct, and they are kept:

* **A unary method that arrives as a stream gets its end frame.**
  (`internal/remotetransport/tcp_connectrpc.go`, commit `64eeb5b1`.) The
  Connect protocol allows a unary call to travel as a stream, and then
  still wants a "data" frame followed by an "end" frame; without the end
  frame the caller's promise never settles. The transport now asks the
  service descriptor whether the method is a server stream, and a unary
  method that arrived with `stream:true` is answered with the data frame
  *and* the end frame.

  What the reference's own bundle sends is a separate measurement, and it
  is worth stating plainly because the first draft of this report had it
  wrong: 【一手】 the bundle sends `stream:false` for **every** unary
  call and `stream:true` only for the six long-lived subscriptions
  (`out/web-ui-working-stuck/websocket_frames.txt`, 42 start frames of one
  page boot, counted with a temporary probe in the transport). The
  reference's unary path was therefore never broken — the fix is
  hardening for a frame the bundle does not itself produce.

* **`StartNarration` is relayed as a stream.** The reference mode's
  dispatcher keeps the list of the reference's server-streaming methods,
  and `StartNarration` was missing from it: a narration call fell back to
  the unary path and came back as a single message with the stream closed,
  where the client expected the next event. The list is now pinned to the
  descriptor (`internal/refbackend/connect.go`,
  `internal/refbackend/streaming_methods_test.go`, commit `9fab2d45`),
  so a method the schema gains fails a test instead of silently getting
  the wrong answer.

### 3.4 The measurement after the fix

【一手】 the same rig, the same browser, the same prompt
(`out/web-ui-working-stuck/console_and_turn.txt`):

* the URL after the load is `…/?token=…&useWebSocket=true`, and the app's
  own routing keeps the parameter;
* the page makes **5 GETs** (the document and its four assets) and **no
  POST at all** — every RPC, the six long streams included, travels over
  one WebSocket;
* the console holds two info lines and **no error and no warning**;
* the turn is answered in the same second the model finishes (the rig's
  log: 01:31:27 → 01:31:30), and the answer is rendered;
* the Customizations panel loads — no more `plugins_fetch_failed`,
  `skills_fetch_failed` or `workflows_fetch_failed`.

---

## 4. What was *not* changed

* **The reference's own bytes.** The document, the bundle, the CSS and the
  ETag are still the reference's. The proxy only chooses the transport.
* **The clone's own Web UI.** It was already on the WebSocket transport
  (the `websocket-transport-default` patch) and is untouched.
* **`internal/rchost/remote_control_backends.go`** and the other agent's
  files: untouched.
* **The reference binary.** The reference's own page has the same stall in
  a browser — this cycle fixes the clone's reference mode, not the
  reference.

## 硬约束

1. **The reference mode's page runs on the WebSocket transport.** On the
   fetch transport the six long streams take Chrome's six connections per
   host and the turn's own calls never leave the queue.
2. **The reference's bytes are not touched.** The proxy redirects the
   document; it does not rewrite it. The bundle stays the reference's own
   (md5 `9f96dc058705b85c1191c6c5ce7bf1a9`).
3. **The operator's explicit choice wins.** A request that names
   `useWebSocket` or `wsTransport` is served as it is.
4. **The path and the query survive the redirect.** The secret travels in
   the query; a redirect that dropped it would lock the LAN clients out.
5. **`hub` is not used.** `wsTransport=3` falls back to fetch after a 4 s
   dial timeout — the path this cycle exists to avoid.
6. **A unary call over the WebSocket transport gets its end frame.** The
   Connect stream protocol requires it, and the client's promise never
   settles without it.
7. **Whether a call streams is decided by the descriptor.** The
   reference mode's list is pinned to the service's own server-streaming
   methods, so a method the schema gains fails a test instead of silently
   getting the wrong answer.

## 存疑

- **"Stuck" or "slow" depends on the timing.** In the reproduction the
  turn itself ran in four seconds and the page rendered the answer about
  two minutes later; in an earlier reproduction `StartCascade` was itself
  `(pending)` and the model was never called at all. Both have the same
  cause (the connection pool), but the "never appears at all" shape was not
  reproduced stably on this machine.
- **The reference has the same stall in a browser.** Its own server serves
  the page without the parameter, so it is on the fetch transport too.
  Nothing here changes the reference binary.
- **The redirect is visible.** The operator's address bar changes. It is
  the price of not touching the reference's bytes; the alternative — a
  patched bundle — would break the mode's own contract and the ETag.
- **The first draft of this report blamed the missing end frame for the
  `skills_fetch_failed deadline_exceeded` in the log.** It did not: the
  measurement in `out/web-ui-working-stuck/websocket_frames.txt` shows the
  reference's bundle never sends a unary call as a stream, and the network
  panel shows the panel's own POST among the pending requests — the
  deadline is the connection pool's. The two clone-side changes are kept
  as hardening, and the report now says so.
- **The first draft of this report also implied that the reference mode's
  Web UI could not work at all before the cycle.** It could: adding
  `?useWebSocket=true` to the URL by hand worked on the pre-fix binary
  (§2.5, `out/web-ui-working-stuck/prefix_manual_workaround.txt`). The
  cycle changes the default, not the capability. The operator's report —
  "stuck, always working" — was about the default the rig printed.
- **The LAN path was reasoned about, not measured end to end.** The
  WebSocket handshake cannot carry a custom header, so the secret travels
  in the `csrfToken` cookie: the clone's `serveWebUI` hands that cookie to
  a peer that presented the secret, and the reference's own page sets the
  same cookie. The measurement itself was made from loopback.

## Limitations

- One machine, one Chrome, one account, one rig.
- The browser MCP's network and console buffers are cumulative per tab and
  keep the *oldest* entries; the two sessions were separated by the last
  document GET, not by a native boundary in the tool.
- The "clean console" was read from a freshly opened tab, because the
  buffer of an older tab still holds the previous session's errors.
- The turn in the reproduction was a one-word prompt with the operator's
  own custom model; a longer turn was not measured.

## Files

| | |
|---|---|
| `internal/refbackend/transport.go` | the transport switch: the redirect, and why the URL is the lever |
| `internal/refbackend/transport_test.go` | the redirect, the secret, the path, the escape hatch, the bundle |
| `internal/refbackend/assets.go` | the hook: the document is redirected, everything else is served verbatim |
| `internal/refbackend/assets_test.go` | the document is served the way the browser asks for it after the redirect |
| `internal/refbackend/connect.go` | `StartNarration` is a server stream |
| `internal/refbackend/streaming_methods_test.go` | the streaming list is pinned to the descriptor |
| `internal/lsproto/lsproto.go` | `Codec.ServerStreamingMethods` |
| `internal/remotetransport/tcp_connectrpc.go` | a unary call sent as a stream gets its end frame |
| `internal/remotetransport/tcp_connectrpc_unary_test.go` | the same, as a regression test |
| `internal/rchost/web_reference_assets_test.go` | the reference mode's wiring, and the redirect |
| `out/web-ui-working-stuck/README.md` | the evidence, in short |
| `out/web-ui-working-stuck/bundle_transport.txt` | the bundle's own transport selector, verbatim |
| `out/web-ui-working-stuck/network_census.txt` | the network panel: the pending requests, and the zero POSTs |
| `out/web-ui-working-stuck/console_and_turn.txt` | the console, the UI, the rig's log, both ways |
| `out/web-ui-working-stuck/websocket_frames.txt` | the frames the reference's own bundle sends: `stream:false` for every unary call, `stream:true` only for the six subscriptions |
| `out/web-ui-working-stuck/prefix_manual_workaround.txt` | the pre-fix binary with `?useWebSocket=true` by hand: the page worked, 0 fetch/XHR, the answer in seconds |
