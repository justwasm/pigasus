# The reference's own page, and a Stop button that stops — deliverable

**Cycle:** `cycle-330-web-reference-mode-cancel`
**Subject:** `pigasus web --reference-backend` — the mode that is supposed to
look like the reference's own web UI, and the one button that was supposed to
stop the turn. Both were measured against the reference's **own** bytes: a
real `language_server` serving its own page, captured live, and the clone's
own web UI run beside it.
**Evidence:** 【一手】 the reference's own index document and its assets,
served through the clone (headers, byte counts, sha256);
【一手】 the gRPC-Web framing, the reference's answer and the clone's
forwarded answer, byte for byte; 【一手】 the cancel round trip at four
landings, with the trajectory's last frame; 【一手】 the IDE web bundle that
carries the button (`internal/rcfrontend/assets/main.js`); 【一手】 the
authoritative protobuf descriptor
(`internal/lsproto/descriptor/language_server.protoset`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the page in reference mode** | the clone's **own** embedded index document and bundle, served next to a proxy | the **reference's own** index document, bundle and CSS, forwarded byte for byte |
| **the reference's own secret** | the clone minted its own; the reference's RPCs were called without the token they demand | the clone learns the reference's CSRF token from the reference's own page and **adopts it as its own transport secret** |
| **the dialect the reference's page speaks** | the clone answered Connect; the reference's page speaks **gRPC-Web** and could not read a single answer | the transport grew a **gRPC-Web dialect** — the reference's own page boots through the clone with **no console error** |
| **the Stop button** | the button sent `CancelCascadeInvocation`, the clone did not implement it: **nothing happened** | the IDE dialect's **three** cancel methods answer, and the turn's context dies |
| **the turn after a cancel** | the answer step stayed `DONE` | the answer step ends **`INTERRUPTED` (12)** and the executor is back to **`IDLE` (1)** |

The clone's **own** web server is untouched by all of this: it still serves its
own page, still speaks Connect-JSON, and still shows its 477 conversations.

---

## 1. The gap, exactly

`--reference-backend` was meant to be the comparison rig: point the clone at
a reference `language_server` and look at the same surface from both sides.
Two things kept it from being that.

**The page was the clone's.** `--reference-backend` proxied the *RPCs*; the
browser still got the clone's own embedded `index.html` and the clone's own
copy of the bundle. So the comparison was never side by side — it was the
clone's UI in front of the reference's answers, and the reference's own UI
was never in the picture at all.

**The transport spoke the wrong dialect.** The reference's own page does not
speak Connect. It announces `application/grpc-web+json` and reads the answer
in the gRPC-Web framing. The clone answered Connect, so the reference's page
read **every** answer as a protocol error. That is not a cosmetic difference:
the gRPC-Web client's reader throws *"protocol error: incomplete envelope"*
on a body it did not frame, and the page's own bootstrap — the server
configuration, the Mendel flags, the slash commands — never landed.

**And the Stop button did nothing.** The chat area's stop button is
`button[aria-label="Cancel (⌃C)"]`, and the bundle sends
`CancelCascadeInvocation({cascadeId, killBackgroundTasks:true})`
(【一手】 `internal/rcfrontend/assets/main.js`). The clone implemented
`CancelRemoteAgentCascade` — the **dashboard's** method — and none of the
IDE dialect's three. The button was a decoration.

---

## 2. The reference's own page

### 2.1 The assets

`internal/refbackend/assets.go` is the proxy: GET and HEAD only (405 for
anything else), the reference's own headers passed through, `If-None-Match`
answered with a 304, the non-HTML assets cached on the clone's side and the
HTML fetched live (so the reference's `window.__APP_CONFIG__` is never stale).

Measured through the clone (8917 → 8802):

| path | bytes | ETag | sha256 |
|---|---|---|---|
| `/` | 3,870 | — | the reference's index document, with `window.__APP_CONFIG__` |
| `/main.js` | 10,461,461 | `W/"9fa115-bb169a04402c03c6"` | `94abc09ac34dcae3a98f5e0fb74b66c87f8a8ad6ad214136544f924da2099e5e` |
| `/jetbox.css` | 95,487 | `W/"174ff-937ee821f1b5139e"` | `82e93aba1ce9799de626ab06fa174c37d74259eea43a5f0160fd1ab0ead13359` |
| `/compiled_tailwind.css` | 249,475 | `W/"3ce83-41bae253b12a9636"` | `5ba613f8e8f082f7fa8f96d5125a6602460ef607ce7f6333c0900e89fd07e16f` |
| `/prism_bundle.js` | 141,963 | `W/"22a8b-ba6d1fdd13cc9888"` | `94c46cdf8a570db44a8a01b6a49b0c033cbb2788c174f70c4782d939bae8f2e2` |
| `/diff_worker.js` | 9,952 | `W/"26e0-d9044aa953b81898"` | `edd2a509494f8cb6d13ed2f1af792bf0c5f5850a3b4696a20b7a76abbad294e4` |

An unknown path answers the reference's **index document** — 200, 3,870
bytes, byte-identical to `/`, with `Cache-Control: no-cache, no-store,
must-revalidate`. That is the reference's own single-page fallback, passed
through.

### 2.2 The seam

`internal/rchost/web_reference_assets.go` composes the two: `/__agy/*` is
the clone's own (its own endpoints belong to the process the browser talks
to), **everything else is the reference's**. The clone's own page is not
served at all in reference mode.

### 2.3 The secret

The reference's RPCs demand a CSRF token, and the reference's own page
carries it in `window.__APP_CONFIG__`:

```html
<script>window.__APP_CONFIG__ = {"productName":"antigravity","csrfToken":"ref-csrf-token","appVersion":"2.26.0","releaseChannel":"stable","devMode":false};</script>
```

Without it the reference answers (【一手】):

```
HTTP/1.1 200 OK
Grpc-Status: 16
Grpc-Message: unauthenticated: missing CSRF token
Content-Length: 0
```

So the clone learns the token from the reference's own index document
(`internal/refbackend/csrf.go`), presents it on every call it forwards, and
**adopts it as its own transport secret** — the byte-identical page and the
clone's transport have to agree about the secret, and the reference's token
is the only one both of them know. A 401 means the reference was restarted
with another token: the clone forgets what it learned and tries once more.

---

## 3. The dialect — gRPC-Web

`internal/remotetransport/grpcweb_codec.go` is the framing. The two
protocols disagree on four points, and all four were measured on the
reference's own wire:

| | Connect | gRPC-Web |
|---|---|---|
| the unary request | bare JSON | **enveloped** |
| the unary answer | bare JSON | a **message envelope** (`0x00`) **and** a **trailers envelope** (`0x80`, the body is the header block `grpc-status: 0\r\n`) |
| the failure | an error envelope | **no body** — `Grpc-Status` / `Grpc-Message` in the HTTP headers (the "trailers-only" answer) |
| the end of a stream | the end-of-stream envelope (`0x02`) | the **trailers envelope** (`0x80`) |

The reference's own unary answer, and the clone's forwarded answer, are
byte-identical:

```
### 参考实现自己:一元调用成功
    响应体: 12001 字节
      信封 flags=0x00 len=11975 payload=b'{"userStatus":{"disableTelemetry":true,"name":"Naiv gaid",…'
      信封 flags=0x80 len=16 payload=b'grpc-status: 0\r\n'

### 克隆的参考模式:同一个调用,经由克隆转发
    响应体: 12001 字节
      信封 flags=0x00 len=11975 payload=b'{"userStatus":{"disableTelemetry":true,"name":"Naiv gaid",…'
      信封 flags=0x80 len=16 payload=b'grpc-status: 0\r\n'
```

**Whether a call streams is decided by the method descriptor**, not by the
content type: gRPC-Web's content type is the same for a unary and a
server-streaming call, so `lsproto.Codec.IsServerStreaming` answers it
(23 of the 351 methods are server-streaming). The reference's own page
relies on this — it calls a server-streaming method with the unary content
type.

The dialect is picked by the request's content type, so the clone's **own**
page is untouched: it announces `application/json`, gets Connect's plain
JSON, and works exactly as before (477 conversations, the composer, no
console error).

---

## 4. The Stop button

`internal/remotecontrol/ls_cancel.go` implements the IDE dialect's three
cancel methods (【一手】 the descriptor and the bundle agree):

| method | request | answer |
|---|---|---|
| `CancelCascadeInvocation` | `{cascadeId, killBackgroundTasks, notifyParent}` | `{}` |
| `ForceStopCascadeTree` | `{conversationId}` | `{"stoppedConversationIds":[…]}` |
| `CancelCascadeSteps` | `{cascadeId, stepIndices}` | `{}` |

They are routed in `dispatch.go`'s `routeMethod`, **before** the alias table
— the IDE's own names win over the clone's legacy ones, and the legacy
`CancelRemoteAgentCascade` keeps its own envelope.

`killBackgroundTasks` kills the background tasks through
`internal/tools/tasks_kill.go`.

### 4.1 The cancel is a real cancel

The turn's context dies; the trajectory's last frame says so. Measured at
four landings (the turn's end is the moment `SendUserCascadeMessage`
returns — that call runs the turn inside it):

| the cancel landed | the turn ended |
|---|---|
| 0.2 s into the turn | +0.24 s |
| 3 s | +3.02 s |
| 5 s | +5.04 s |
| 60 s (the model was writing an artifact) | +60.37 s |

and the trajectory's last frame for the 5 s landing:

```
executor=2 steps=[(14, 3), (15, 8)]                     ← the answer step, GENERATING
executor=2 steps=[(14, 3), (15, 3), (132, 3), (132, 3)]
executor=1 steps=[(14, 3), (15, 3), (132, 3), (132, 3), (15, 12)]
                                            ↑ the answer step: INTERRUPTED (12)
                                              the executor: IDLE (1)
```

(Step type 14 is the user input, 15 the answer, 132 the artifact; status 3 is
DONE, 8 GENERATING, 12 INTERRUPTED.)

In the browser: clicking the button removes it, clears the sidebar's spinner
and freezes the answer mid-sentence — the same event, read by a human.

### 4.2 The two supporting changes

`ls_turn_sink.go`'s `finishStatus()` ends an interrupted turn's last step
with `INTERRUPTED` instead of `DONE`, so the page shows the badge instead of
a finished answer that stops mid-sentence.

`cascade_lifecycle.go`'s `StartCancel` clears the cascade's cancelled flag
when a turn starts. The flag decides how a **failed** turn is reported, and
a turn that failed after an earlier turn was cancelled has to report its own
failure.

---

## 5. Side by side in the browser

| | the reference's own page through the clone (8917) | the clone's own page (8918) |
|---|---|---|
| console | **no message at all** | no error |
| the sidebar | the reference's 4 conversations | the clone's 477 conversations |
| the composer | present | present |
| the model picker | "Gemini 3.8 Flash High" (the reference's) | the clone's own |

The reference's page boots through the clone: the index document, the bundle,
the CSS and the RPCs are all the reference's or the reference's own answers.

---

## 6. What the clone changed

| file | what |
|---|---|
| `internal/refbackend/assets.go` | the reference's own assets, forwarded |
| `internal/refbackend/csrf.go` | the reference's CSRF token: learned from its index document, refreshed on a 401 |
| `internal/refbackend/refbackend.go` | the token is presented on the unary calls; the 401 → re-learn → retry |
| `internal/refbackend/connect.go` | the token is presented on the streaming calls too |
| `internal/rchost/web_reference_assets.go` | the seam: `/__agy/*` is the clone's, the rest is the reference's |
| `internal/rchost/web.go` | the reference mode adopts the reference's secret; the banner says so |
| `internal/rchost/web_reference.go` | the dispatcher builder hands the reference's base URL out |
| `internal/remotetransport/grpcweb_codec.go` | the gRPC-Web framing |
| `internal/remotetransport/connect_codec.go` | the dialects, and the envelope stripping |
| `internal/remotetransport/tcp_server.go` | the unary path: the gRPC-Web answer, the trailers-only failure |
| `internal/remotetransport/tcp_connectstream.go` | the stream's end: Connect's end-of-stream or gRPC-Web's trailers |
| `internal/lsproto/lsproto.go` | `IsServerStreaming` |
| `internal/remotecontrol/ls_cancel.go` | the three cancel methods and their helpers |
| `internal/remotecontrol/dispatch.go` | the cancel methods are routed before the alias table |
| `internal/remotecontrol/ls_turn_sink.go` | an interrupted turn ends `INTERRUPTED` |
| `internal/remotecontrol/cascade_lifecycle.go` | the cancelled flag is per turn |
| `internal/tools/tasks_kill.go` | the background tasks a cancel kills |
| `internal/cli/subcommand_web.go` | the usage text says the frontend is the reference's |

---

## 7. 硬约束 / the load-bearing rules

- **Reference mode serves the reference's own bytes.** The page, the bundle
  and the CSS come from the reference; the clone only keeps `/__agy/*`.
- **Reference mode adopts the reference's secret.** The CSRF token is read
  out of the reference's own index document and becomes the clone's transport
  secret. It is never guessed, and a 401 re-learns it once.
- **The dialect follows the request's content type; the stream follows the
  descriptor.** gRPC-Web's content type does not mark a stream.
- **gRPC-Web's unary answer is two envelopes and its failure has no body.**
  The status of a failed call is in the HTTP headers.
- **A cancel kills the turn's context.** The trajectory's last frame says
  `INTERRUPTED` and the executor says `IDLE`; the button is not a decoration.
- **The clone's own web server is a separate front.** It serves its own page
  and speaks Connect; the reference mode is the other side of the comparison.

## 8. 存疑 / open questions

- 【推测】The status the **reference** puts on a stopped turn is unobserved:
  it would need a live turn in the reference itself. The clone uses
  `INTERRUPTED` (12) for an interrupted turn and `CANCELED` (6) for a step
  that was cancelled by name.
- 【推测】The reference's asset cache policy was not compared item by item.
  The clone **passes the reference's headers through** and adds one thing:
  `If-None-Match` → 304 for the non-HTML assets it caches.
- 【推测】Whether the reference's own page would talk to the clone through a
  *spawned* reference (rather than an already running one) is untested — the
  reference serves its page only in the hub subclient mode, which is the
  other agent's change to `internal/refbackend/spawn.go`.

## 9. Limitations

- The clone's transport masks a **failed dispatch** as HTTP 200 `{}` on the
  **Connect** dialect, so a failed `CancelCascadeInvocation` looks exactly
  like a successful one. The gRPC-Web dialect does not have that shape — it
  carries the status in the headers. Not changed here (it is another
  surface's business).
- The reference's trajectory frames reach the subscriber at the **end** of
  the turn, so a cancel has to be measured by the turn's end time; a live
  frame is not a witness.
- The reference's own standalone `language_server` does not resume a turn
  after an answer (the unary `HandleCascadeUserInteraction` never returns) —
  a reference-side limit already recorded in cycle 327.
- The reference mode's proxy covers the assets and the RPCs; it does not
  cover the reference's **websocket** surface.

## 10. The files

### The evidence

| file | what |
|---|---|
| `out/web-reference-mode/README.md` | the zh-CN walk-through of the four captures |
| `out/web-reference-mode/reference-index.html` | 【一手】 the reference's own index document, through the clone |
| `out/web-reference-mode/asset-headers.txt` | 【一手】 the assets' headers, byte counts and hashes |
| `out/web-reference-mode/grpcweb-frames.txt` | 【一手】 the gRPC-Web framing, the reference's answer and the clone's |
| `out/web-reference-mode/clone-cancel.txt` | 【一手】 the cancel round trip: four landings and the last frame |

### The code

| file | what |
|---|---|
| `internal/refbackend/assets.go` | the assets proxy |
| `internal/refbackend/csrf.go` | the token's discovery and refresh |
| `internal/rchost/web_reference_assets.go` | the seam |
| `internal/remotetransport/grpcweb_codec.go` | the gRPC-Web framing |
| `internal/remotecontrol/ls_cancel.go` | the three cancel methods |
| `internal/tools/tasks_kill.go` | the background tasks a cancel kills |
| `internal/rcfrontend/assets/main.js` | the IDE web bundle — the button's contract |
| `internal/lsproto/descriptor/language_server.protoset` | the authoritative proto shapes |

### The reference

- `language_server --standalone --subclient_type hub --http_server_port=8802
  --csrf_token=ref-csrf-token --gemini_dir=…`
- its own web UI at `http://127.0.0.1:8802/`
- the clone's reference mode at `http://127.0.0.1:8917/`
- the clone's own web UI at `http://127.0.0.1:8918/`
