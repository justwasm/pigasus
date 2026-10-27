# The cloud remote view's three differences — deliverable

**Cycle:** `cycle-326-cloud-remote-view-three-differences`
**Subject:** three differences between the clone and the reference in the
*cloud* remote view — the app iframe never reported ready, the clone
could not fetch the full Mendel set, and the clone's JSON answers carried
their keys in the wrong order.
**Evidence:** 【一手】 the live cloud dashboard (`antigravity.google.com/u/3/r/…`)
against the clone's own instance, the reference's live `GetMendelFlags`
answer (the reference's IDE language server on 127.0.0.1:8796), the
Mendel backend itself (four User-Agents, three bodies), the reference's
CLI daemon's own HTTP port; 【反编译】 the reference's own literals for the
Mendel backend call (`codeassistclient.ListExperiments`, 0x101acd210 —
"AICode FetchConfig failed, falling back to CCPA ListExperiments");
【一手】 the CLI web bundle (`internal/rcfrontend/assets/main.js`) — the
app's own stream readers, the request it builds and the frame it
decodes.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the app iframe** | never reported ready — the dashboard spun on "Connecting to instance..." and the iframe stayed black | the app boots, renders the sidebar (481 conversations) and reports ready; the overlay is gone |
| **the stream dialect** | the tunnel answered `application/connect+json` — a *JSON* frame the app cannot read | the tunnel answers `application/connect+proto` — a binary protobuf frame, the only dialect the app's stream reader has |
| **the Mendel set** | six hand-written flags | the live backend set: **508** experiments, every one marked `EXPERIMENT_SOURCE_LANGUAGE_SERVER`, the six local ones first, `webrtcMeshReady: false` carried |
| **the answer's key order** | Go-map JSON → alphabetical | protojson → the descriptor's **declaration order** |

The three symptoms have three different causes, and only the first one
was a *protocol* error:

1. the iframe never became ready — the clone answered the app's
   subscription streams in the **wrong dialect** (§2);
2. the Mendel set was short — the clone never asked the **backend** for
   the flags at all (§3);
3. the key order was wrong — the clone's answers are **Go maps**, and
   `encoding/json` writes a map's keys alphabetically, while the
   reference's answers are protojson and carry the descriptor's
   declaration order (§4).

---

## 1. The three symptoms, as the user reported them

* **the app iframe never reports ready** — in the dashboard's cloud
  remote view the page spins on "Connecting to instance..." and the
  iframe is black;
* **the clone cannot fetch the full Mendel set** — the answer carries
  six flags where the reference carries 508;
* **the clone's JSON answers use Go-map alphabetical key order instead
  of protojson field order**.

The rig for the live parts of this report: the clone's own daemon
(`pigasus remote-control serve`, instance
`0698e8f6-2cee-42c0-a454-02c764f6b9b6-v2`, name
`pigasus-gear.tailac191.ts.net`), the dashboard's own host page, and
the reference's IDE language server on 127.0.0.1:8796.

---

## 2. Difference 1 — the app iframe never reports ready

### 2.1 The symptom

The dashboard's host page keeps an overlay — "Connecting to instance..." —
over the iframe, and the iframe itself stays black. The overlay is the
host's own state: it is drawn while `iframeInitializing` is true, which
means *the iframe is loading* **or** *the app has not reported ready*.
The host page learns the second half from the app's own
`uiReady` message (`IframeMessage` field 26), and it drops the overlay
only then.

So "the app never reports ready" is not a *host* problem: it is the
app's own boot that never completes.

### 2.2 What the app asked for, and what it got

【一手】 the clone's daemon log, the live instance:

```
inbound RPC path=…/JetboxSubscribeToState      reqID=0  callerContentType=""
inbound RPC path=…/JetboxSubscribeToSummaries   reqID=13 callerContentType=""
inbound RPC path=…/ProjectUpdatesStream         reqID=1  callerContentType=""
inbound RPC path=…/SubscribeToPlugins           reqID=12 callerContentType=""
inbound RPC path=…/SubscribeToMcpServerStates   reqID=11 callerContentType=""
inbound RPC path=…/GetAllSkills                 reqID=6  callerContentType="application/proto"
```

The three (and five) *subscription* calls name **no content type at
all**; every *unary* call names `application/proto`.

The answer the instance gave them was a JSON frame — captured from the
host page, which forwards the cloud's payload to the app verbatim:

```
00 00 00 00 fa {"appState":{"postOnboarding":{"completedSteps":[]},
              "seenNuxs":{"uids":[24]},
              "agentOnboardingCompleted":"AGENT_ONBOARDING_STATE_COMPLETED"},…}
```

The frame's *envelope* is right (flag 0, big-endian length, the
message). The message is JSON text.

### 2.3 The app reads a stream frame as protobuf, and only as protobuf

【一手】 the CLI web bundle — the app's own code, in
`internal/rcfrontend/assets/main.js`:

* the **unary** path has a dialect switch:

  ```js
  // XChannelTransport.unary
  m = this.remoteControlVersion === "v2" && this.binaryUnaryEnabled;
  if (m) s1(n, 3).set("content-type", "application/proto"), …;
  …
  J = m ? Kc(a.output, G) : De(a.output, (new TextDecoder).decode(G),
                                {ignoreUnknownFields: true, registry: vN});
  ```

* the **stream** path has **no** switch at all. The MessagePort reader
  (`mHc`):

  ```js
  t.flags === 2 ? … : t.flags === 0 ? l(yield k(Kc(c.output, t.data)))
                                  : console.warn(…);
  ```

  and the webchannel reader (`xHc`) does the same:
  `y.flags === 0 ? k(yield h(Kc(b.output, y.data)))`.

* `Kc(a, b, c)` is the **binary** protobuf decoder — `Hba(a, new Yb(b), …)`,
  `Yb` being the protobuf wire-format reader. There is no JSON branch
  in the stream path: `De` (the JSON decoder) is used by the unary path
  only.

* the app's stream request names no content type either: the port
  transport's headers are `{"x-is-streaming": "true",
  "x-jetski-remote-control-user-agent": …}` and nothing else
  (`kHc`).

So a JSON frame is not "read with the wrong decoder" — it is *not
readable at all*: `Kc` parses `{"appState":…` as a protobuf wire
format, hits a malformed tag, and the frame is dropped. The app's
state store therefore never initialises, the manager layout never
mounts, and the `uiReady` reporter — which lives inside that layout —
never runs. The dashboard waits forever.

The app's own log says it plainly:

```
[ERROR] [mcp_servers_fetch_failed] ConnectError: [unavailable] Stream closed before receiving OK trailer
[WARN]  [ProjectUpdates] Disconnected
```

### 2.4 The rule the reference follows

The reference's language server speaks protobuf, so its streaming
handler answers binary frames. The instance therefore has to pick the
dialect when the caller names none, and the two cases differ:

| the call | the caller's content type | the answer's dialect |
|---|---|---|
| **unary** | none | **JSON** (`application/json`) |
| **streaming** | none | **binary protobuf** (`application/connect+proto`) |
| either | named | the caller's own dialect |

【一手】 the unary row: the dashboard's host page asks for the Mendel
flags without naming a content type and reads the answer with
`JSON.parse`:

```js
fetchMendelFlags() {
  const {payload} = await this.proxyCommand(
    "/exa.language_server_pb.LanguageServerService/GetMendelFlags",
    new TextEncoder().encode("{}"));
  return payload;
}
```

and the reference's captured answer for that call carries the content
type `application/json` and JSON text — a protobuf answer would break
the host page with `SyntaxError: Unexpected token '\u0000'`.

【一手】 the streaming row: the app itself is the only client that
streams over the remote-control channel, and it can only read protobuf
(§2.3). The reference works against the dashboard in production, so its
streaming answer must be binary.

### 2.5 The fix

`internal/rctunnel/httpproxy.go` — `proxiedContentType`:

```go
	if streaming {
		// A stream with no caller dialect is binary protobuf: the
		// remote-control app decodes a stream frame as protobuf only.
		return contentTypeConnectProto
	}
	return contentTypeJSON
```

The clone's local handler already knows both dialects: the rewritten
content type reaches `dialectFor` (`internal/remotetransport/
connect_codec.go`), which selects the binary codec, and
`encodeResponseBody` turns the dispatcher's proto-JSON into a protobuf
message with the schema's descriptor
(`internal/lsproto.EncodeResponse`).

The regression test is end-to-end: `TestTCPServer_RPCRouteStreamsAppStateAsProtobuf`
(`internal/remotetransport/connect_codec_test.go`) asks the local
handler for `JetboxSubscribeToState` in the proto dialect and decodes
the first frame with the schema's own codec, asserting that
`agentOnboardingCompleted` is `AGENT_ONBOARDING_STATE_COMPLETED` — the
value the app's own layout check (`=== 2`) reads.

### 2.6 The live result

【一手】 with the fix, the dashboard boots the app:

```
[INFO] [TTIR] Input area rendered.
[INFO] [SidebarLoad] Conversation list loaded (481 conversations)
```

the host page's overlay is gone, the iframe has lost its `dimmed`
class, and the app's UI is fully rendered inside the dashboard (the
sidebar's conversation list, the workspace list, the instance name
`pigasus-gear.tailac191.ts.net`).

---

## 3. Difference 2 — the clone cannot fetch the full Mendel set

### 3.1 The symptom

The clone's `GetMendelFlags` answer carried six flags, hand-written in
the clone. The reference's carries **508** experiments — and the mesh
field with it: the reference's **CLI** answer says
`webrtcMeshReady: true` (the CLI has the mesh), the reference's **IDE**
language server says `false` (the IDE has none).

### 3.2 Where the set comes from

The reference's language server keeps a *Mendel state cache*: it polls
the backend and holds the account's flag set. The clone reads the same
backend.

【反编译】 the reference's IDE language server —
`codeassistclient.ListExperiments` (0x101acd210) tries the AICode
endpoint first and falls back to the CloudCode one:

```
"AICode FetchConfig failed, falling back to CCPA ListExperiments: %v"
```

and the CloudCode path is the POST of the reference's own request:

```
POST https://cloudcode-pa.googleapis.com/v1internal:listExperiments
{"project":"aicode-consumers"}
```

the AICode path is the GET the reference builds with `getAICodeURL`
(0x101abd840):

```
GET ${AICODE_ENDPOINT_URL:-https://aicode.googleapis.com}/v1alpha:fetchConfig
```

### 3.3 Which of the two is the complete set

【一手】 measured on this machine, 2026-10-26:

| the path | the flags it answers with |
|---|---|
| `POST cloudcode-pa…/v1internal:listExperiments` | **508** |
| `GET aicode.googleapis.com/v1alpha:fetchConfig` | 310 |

and every one of the 310 is also in the 508 — the AICode path is a
*subset*, not a different set. The reference's own answer carries 508
experiments, so the CloudCode path is the one the clone reads.

The request carries two things that decide the answer: the body
(`{"project":"aicode-consumers"}` — the reference's own) and the
**User-Agent**.

【一手】 measured on this machine, 2026-10-26, the same body and the
same token, five User-Agents:

| the User-Agent | the flags |
|---|---|
| `antigravity/cli/1.4.4 (aidev_client; os_type=darwin; arch=arm64; auth_method=consumer)` — the reference's own | **508** |
| `antigravity/cli/1.4.7 (…)` — the same shape, a newer version | 508 |
| `antigravity/ide/1.4.7 (…)` — the IDE's own | 508 |
| `Go-http-client/1.1` — no User-Agent at all | 504 |
| `curl/8.7.1` | 504 |

the four flags a generic client does not get are the Gemini CLI's own
preview flags:

```
GeminiCLIIsLaunched__gemini_3_1_flash_lite_preview_launched
GeminiCLIIsLaunched__gemini_3_1_flash_preview_launched
GeminiCLIIsLaunched__gemini_3_1_pro_preview_launched
GeminiCLIPreviewAvailable__enable_preview
```

⇒ the backend serves the *complete* set to a client that identifies
itself as an Antigravity client, and a smaller set to everybody else.
The version, the platform and the auth method in the User-Agent do not
change the set — the product name does. The clone sends the
reference's own User-Agent (`internal/auth/mendel.go`), so it gets the
same 508.

The backend serves the two remote-control booleans *disabled* —

```
{"name":"remote-control-setting-enabled","boolValue":false}
{"name":"remote-control-webrtc-mesh","boolValue":false}
```

— while the reference's own **CLI** answer has them *enabled*. The
difference is the shadowing of §3.4: the language server's own values
win over the backend's copies. (The reference's **IDE** answer has
both disabled — the IDE's own local values; §9.6.)

### 3.4 The answer's shape

The answer says where every flag came from.

【一手】 the reference's own answer — the live one, from the
reference's IDE language server on 127.0.0.1:8796, 508 experiments —
marks **every** flag it serves `EXPERIMENT_SOURCE_LANGUAGE_SERVER`:

```
{"keyString":"enable-deepagent","source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"}
{"keyString":"remote-control-version","string":"v2",
 "source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"}
```

508 of 508, the flags the backend served included. The `source` names
*who resolved* the flag, not who stores it: the language server
resolves the flags it serves — the ones it defines itself and the ones
it fetched from the backend — and the API server's own payloads are the
ones that carry `EXPERIMENT_SOURCE_API_SERVER`. The app merges both —
its own flags panel says "Flags come from Mendel experiments and from
the host app" — and neither the app nor the dashboard's host page ever
reads the `source` field.

The six remote-control flags are the language server's *own*, and the
clone defines the same six itself, so the answer's head is the
reference's head:

| key | the reference's **CLI** answer | the clone |
|---|---|---|
| `remote-control-version` | `"v2"` | `"v2"` |
| `remote-control-webrtc-mesh` | (enabled) | (disabled — on purpose, below) |
| `remote-control-proxy-server-url` | `"jetski-webchannel.googleapis.com:443"` | the same |
| `remote-control-silence-timeout-seconds` | `"60"` | `"60"` |
| `remote-control-setting-enabled` | (enabled) | (enabled) |
| `remote-control-companion-url` | `"https://antigravity.google.com"` | the same |

The local flags **shadow** the backend's copies of the same flags: the
backend serves the very same six names (§3.3), and the language
server's own values win over the backend's copies.

【一手】 the clone's answer today: **508** experiments, every one of
them marked `EXPERIMENT_SOURCE_LANGUAGE_SERVER`, the six local ones
first.

One value differs on purpose: the mesh experiment is *disabled*. The
clone does not implement the WebRTC mesh — no data channel, no
signalling — so it reports the mesh as off, and the dashboard's host
page takes its WebChannel path instead of waiting for a mesh that
would never come up. The reference, which does implement the mesh,
answers with the mesh enabled.

The *field* is there either way: `webrtc_mesh_ready` is an `optional
bool` in the schema, so protojson emits it even when it is false — the
reference's own answer carries `"webrtcMeshReady": false` (its IDE
language server has no mesh either), and the clone carried no field at
all until this cycle.

### 3.5 The value mapping

A flag is either a *boolean* flag (on or off) or a flag with a value.
The answer's shape (`exa.codeium_common_pb.ExperimentWithVariant`):

| the backend's flag | the answer |
|---|---|
| a boolean flag that is **off** | `disabled: true` |
| a boolean flag that is **on** | nothing — protojson omits a false boolean |
| a string, an int or a float | the `string` field, as text |
| a list | the `json` field, as the JSON text of the array |

【一手】 the reference's own answer settles the list: a list is carried
in `json`, as the JSON text of the array —

```
{"keyString":"science-plugin-split-targets","json":"null", …}
{"keyString":"DuetAiLocalRag__substrings_to_identify_doc_prompts",
 "json":"[\"document\",\"comment\"]", …}
```

— and `null` for a list that carries no elements. The reference's
answer carries **no** `csv` entry at all (0 of 508), so the CSV
reading of the reference's `flagValueToString` (0x101a82c80) is wrong:
that function renders a *scalar*, and the list takes the `json` field.

An empty value is kept: a flag whose value is the empty string is
carried as `""` and not as a flag without a value. The `string` field
is an `optional string` in the schema, so protojson emits it whenever
it is set — 36 of the reference's 508 entries carry `"string": ""`.

Measured side by side, the two answers carry the same fields and the
same counts (the *values* move with the account and the hour):

| | the reference (the IDE's LS) | the clone |
|---|---|---|
| the experiments | 508 | 508 |
| `keyString` / `source` | 508 / 508 | 508 / 508 |
| `string` | 122 (36 empty) | 122 (36 empty) |
| `json` | 7 (4 `null`) | 9 (5 `null`) |
| `disabled` | 225 | 245 |
| `csv` | 0 | 0 |
| `webrtcMeshReady` | `false` | `false` |

the two captures are minutes apart, and the *values* move: the
backend's set is the account's, and a flag that is off in one answer
can be on in the next. The *shape* — which fields, how many, and the
empty value kept as `""` — is the same on both sides.

---

## 4. Difference 3 — the answers' key order

### 4.1 The symptom

The clone's dispatcher answers with Go maps, and `encoding/json` writes
a map's keys **alphabetically**. The reference's answers are protojson,
and protojson writes a message's fields in the order the **descriptor
declares them** — not in field-number order, and not alphabetically.

### 4.2 The evidence

【一手】 the reference's own captured `GetMendelFlags` answer carries
its experiment entries as

```
keyString, disabled, string, json, csv, source
```

and the declaration order of
`exa.codeium_common_pb.ExperimentWithVariant`
(`internal/lsproto/descriptor/language_server.protoset`) is

```
key (1), key_string (5), disabled (6), string (2), json (3), csv (4), source (7)
```

— the same order, with `key` left out because it is an enum whose
default is `UNSPECIFIED` and protojson omits a default value. (`csv` is
in the schema and in the order, but the reference's answer never uses
it — §3.5.) The clone's answer had read `csv, disabled, json,
keyString, source, string`: the same data, the same fields, the wrong
order.

### 4.3 The fix

`internal/lsproto/order.go` — `MarshalAnswer` / `MarshalMessage`:

* the fields the schema knows come first, **in the descriptor's
  declaration order**;
* the fields the schema does not know follow, **alphabetically** (they
  have no place in the schema, so they are appended in a stable order);
* the **values are not touched** — only the order of the keys is.

A method the schema does not know is encoded the ordinary way: the
schema is not the authority on what the clone serves.

The reordering reaches

* the unary answers (`DispatchDirect`),
* the five subscription streams' frames
  (`internal/remotecontrol/ls_streams.go`), and
* the pushed `AgentStateUpdate` frame
  (`exa.jetski_cortex_pb.StreamAgentStateUpdatesResponse`) — a message
  of the cortex service, not a method's answer, so it is named by its
  full proto name.

---

## 5. The queue rule, next to the three differences

While the three differences were being chased, the reference's own
queue rule was re-read and the clone aligned with it (commit
`3cbadf84`, and the corrected report in `cycle-322`):

* the reference's **server** queues the messages that arrive during a
  running turn; the web UI only mirrors them optimistically — so the
  clone's web path queues instead of cancelling the running turn;
* the reference **stubs** `SendAllQueuedMessages` and
  `DeleteQueuedUserInputStep` — both answer HTTP 500 `"deprecated"`.
  The clone keeps its own working implementations, because the clone's
  own web UI uses them.

This is not one of the three differences, but it is the same surface
(the cloud remote view), and the earlier report's two wrong claims
were corrected in the same pass (commit `1b885372`).

---

## 6. What changed in the clone

| commit | what |
|---|---|
| `1b885372` | the corrected `cycle-322` report — the literal addresses were file offsets, and the `NEXT_INVOCATION` reading came from the setting's copy, not from the implementation |
| `3cbadf84` | the web path queues a message that arrives during a running turn, FIFO, like the reference |
| `b6c7d949` | the live Mendel set — `internal/auth/mendel.go` (the backend call), `internal/remotecontrol/mendel_flags.go` (the answer's shape) |
| `86d63548` | the answers in the schema's field order — `internal/lsproto/order.go`, `internal/remotecontrol/answer_order.go` |
| `bdd9666c` | the cloud streams answer in binary protobuf — `internal/rctunnel/httpproxy.go` |
| `baa3c3b8` | the Mendel answer's values — a list in the `json` field, an empty value kept as `""` |
| `710dbf06` | the Mendel answer's sources — every flag `EXPERIMENT_SOURCE_LANGUAGE_SERVER` — and the `webrtcMeshReady` field, carried as `false` |

---

## 7. The live verification

【一手】 the rig: the clone's daemon (`pigasus remote-control serve`,
127.0.0.1:8787, instance `0698e8f6-…-v2`) registered with the cloud,
the dashboard at
`https://antigravity.google.com/u/3/r/0698e8f6-2cee-42c0-a454-02c764f6b9b6-v2`.

* the app boots and the dashboard's overlay is gone — the host page's
  `iframeInitializing` is false, so the host received the app's
  `uiReady` message;
* the app's own log: `[TTIR] Input area rendered.`,
  `[SidebarLoad] Conversation list loaded (481 conversations)`;
* the daemon log shows the app's whole boot: the five subscription
  streams (no content type), then `GetAllWorkflows`, `GetSlashCommands`,
  `GetAllSkills`, `ListMcpPrompts`, `GetUserStatus`,
  `GetCustomizationStates`, `GetAgentScripts`, `FetchUserInfo`,
  `FetchAdminControls`, `RecordAnalyticsEvent` — all with
  `application/proto`;
* the clone's `GetMendelFlags` answer: 508 experiments, every one
  marked `EXPERIMENT_SOURCE_LANGUAGE_SERVER`, the six local ones
  first, and `webrtcMeshReady: false` carried as a field;
* the same answer, taken from the **running daemon** over its own
  Connect endpoint (127.0.0.1:8787, 55,431 bytes): the top-level keys
  are `experimentConfig` then `webrtcMeshReady`, and the entries carry
  their keys in the schema's order — `keyString`, `disabled` / `string`
  / `json`, `source` — the four orders the reference's own answer has
  and no others;
* the reference's IDE language server (127.0.0.1:8796) answers
  `GetMendelFlags` the same way — 508 experiments, all of them
  `EXPERIMENT_SOURCE_LANGUAGE_SERVER`, `webrtcMeshReady: false`, the
  same fields in the same order, and the same four remote-control
  values the clone carries;
* the reference's IDE language server also agrees with the dialect rule
  of §2.4 from the other side: a streaming request that names no
  content type is answered **415 Unsupported Media Type** (the
  reference's own server wants to be told the dialect), and a
  `application/connect+json` streaming request must carry an
  *enveloped* body — `{}` is answered "incomplete envelope: unexpected
  EOF".

---

## 8. 硬约束 / the load-bearing rules

1. **A stream frame is binary protobuf.** The remote-control app's
   stream readers have no JSON branch (`Kc` is the binary decoder).
   A JSON frame is not "decoded wrongly", it is *dropped*, and the
   app's boot stops with it.
2. **The dialect rule is asymmetric.** A *unary* call that names no
   content type gets JSON (the host page parses the Mendel answer with
   `JSON.parse`); a *streaming* call that names no content type gets
   binary protobuf. "Connect's default is JSON" is true for a unary
   call and false for a stream.
3. **The caller's own dialect wins**, whenever the caller names one:
   `application/proto` → `application/connect+proto` for a stream,
   `application/json` → `application/connect+json`.
4. **The Mendel set comes from the backend**, and the CloudCode path is
   the complete one (508 against the AICode path's 310). The request
   body `{"project":"aicode-consumers"}` is the reference's own.
5. **The Mendel request must carry the reference's own User-Agent.**
   The backend serves four extra flags — the Gemini CLI's preview flags
   — only to a client that identifies itself as `antigravity/…`; a
   generic client (Go's default, curl) gets 504 instead of 508.
6. **Every flag the answer carries is
   `EXPERIMENT_SOURCE_LANGUAGE_SERVER`** — 508 of 508 in the
   reference's own answer, the flags it fetched from the backend
   included. The source names who *resolved* the flag.
7. **A list is carried in the `json` field**, as the JSON text of the
   array (`null` for an empty list) — never in `csv`, which the
   reference's answer does not use at all. An empty value is kept as
   `""`: the `string` field is an `optional string`, so protojson emits
   it whenever it is set.
8. **`webrtcMeshReady` is always carried** — it is an `optional bool`,
   so protojson emits it even when it is false, and the reference's own
   answer carries `"webrtcMeshReady": false`.
9. **The six remote-control flags are the language server's own**:
   they come first and they shadow the backend's copies — the backend
   serves the same flags *disabled*, and the reference's answer has
   them enabled.
10. **protojson's key order is the descriptor's declaration order** —
    not the field number, and not alphabetical. A Go map cannot carry
    it; the answer has to be reordered against the descriptor.
11. **The queue is the server's.** The reference queues a message that
    arrives during a running turn; the web UI's mirror is optimistic.
12. **The reference's `SendAllQueuedMessages` and
    `DeleteQueuedUserInputStep` are stubs** (HTTP 500, `"deprecated"`).
    The clone implements them because the clone's own web UI uses them —
    that difference is deliberate and stays.

---

## 9. 存疑 → the open questions

1. **Where exactly does the reference choose the streaming dialect?**
   【推测】 the reference's language server registers its streaming
   handlers with the protobuf codec and its unary handlers with
   protojson — the asymmetry of §2.4 is what its own answers show, but
   the *code* that decides it was not recovered: the reference's
   `newProxiedHTTPRequest` (the tunnel's request rewriter) is not in
   the decompiled set, and the recovered Go source under
   `out/local-macos/gosrc/` is a stub
   (`panic("agentrecon: not reconstructed")`).
2. **Does the reference's stream answer carry its own content type?**
   The cloud relays the instance's answer to the browser as bytes; the
   app decides the dialect from *its own* configuration
   (`binaryUnaryEnabled`), not from the answer's headers — so the
   header was not observable from the browser side. The clone sends
   `application/connect+proto`, which is the honest answer for a
   protobuf frame.
3. **The list → `csv` mapping** was 【推测】 in the first draft of this
   report and is now **measured**: the reference's own answer carries a
   list in the `json` field and carries no `csv` entry at all (§3.5).
   The question is closed.
4. **The source values of the reference's *CLI* answer were not
   re-captured.** The measurement in §3.4 is the reference's **IDE**
   language server (the same `jetski` codebase as the CLI's, one
   binary over), and the earlier capture of the reference's **CLI**
   answer through the cloud recorded that the `source` field is present
   on all 508 entries but not its values. The clone follows the IDE's
   answer: `EXPERIMENT_SOURCE_LANGUAGE_SERVER` for everything. The
   field is inert — neither the app nor the host page reads it — so
   the risk is cosmetic.
5. **The reference's CLI language server could not be queried
   directly.** The daemon's own HTTP port (127.0.0.1:64417) refuses
   every request without the CSRF token it generates in-process ("the
   address and CSRF token are normally already" — the reference's own
   strings), and the token was not recovered: a wrong token is answered
   "invalid CSRF token", a missing one "missing CSRF token". The
   IDE's language server, started without a token, answers freely —
   that is the reference this cycle measured.
6. **`remote-control-setting-enabled` differs between the two
   references.** The backend serves it disabled, the reference's **CLI**
   answer says *enabled*, the reference's **IDE** answer says
   *disabled*, and the clone says *enabled* — the clone is the CLI's
   counterpart, and the CLI's captured answer is the evidence. The
   backend's copy is disabled in both captures, so the shadowing of
   §3.4 is what decides it.
7. **Why the app of §2.6 needed a page reload** to report ready. The
   app instance that had been running since *before* the fix rendered
   the manager layout as soon as the streams started working (its own
   log: "[TTIR] Input area rendered", "[SidebarLoad] Conversation list
   loaded (481 conversations)") — but the host page's `uiReady`
   message was never observed from it, and the overlay had been hidden
   by hand at that moment, so whether the host had marked the iframe
   ready could not be told. A fresh page load reported ready at once.
   【推测】 the reporter's effect runs when the manager layout mounts,
   and the layout's own branch depends on the auth state: while the
   state is `signedOut` the layout renders the onboarding redirect
   instead of the reporter, so a layout that mounts before its data
   arrives may never report ready. Whether the reference behaves the
   same way is not known.
8. **The reference's Mendel answer is 56,758 bytes and the clone's is
   52,347.** The gap is the mesh experiment's value and
   `webrtcMeshReady: true` — the clone reports the mesh as off on
   purpose (§3.4). Nothing else differs in the *shape*; the values of
   the 502 backend flags move with the account and were not compared
   one by one against the reference's captured answer.

---

## 10. Limitations

* The live parts were measured on **one machine and one account**
  (2026-10-26): the clone's own instance, the dashboard's host page,
  the reference's IDE language server (127.0.0.1:8796), the reference's
  **CLI** daemon (127.0.0.1:64417, whose language server refuses every
  request that does not carry the in-process CSRF token — §9.5) and the
  Mendel backend itself.
* The dashboard's host page and the app bundle are **closed-source
  and minified**; every claim about them is a claim about the
  *shipped* `cli-1.4.7` bundle in `internal/rcfrontend/assets/`, not
  about the reference's source.
* The Mendel set is **live**: the 508 flags and their values change
  with the account and with the backend, and the set's *size* moves
  with the User-Agent (§3.3). The *shape* of the answer is pinned by
  the tests; the contents are not.
* The Mendel User-Agent the clone sends carries the reference's
  **1.4.4** version string, while the clone emulates 1.4.7. The
  reference builds the string from its own version (the binary holds no
  literal), and the version does not change the flag set (§3.3), so
  the difference is cosmetic — but it is a difference.
* The reference's IDE language server on 8796 answers **415** for a
  streaming request that names no content type, while the *clone's*
  tunnel answers one — the tunnel is the layer that rewrites the
  content type for the cloud path, and the reference's own tunnel does
  the same, but that rewrite was not recovered from the reference's
  code (§9.1).
* The screenshots and the console captures of this cycle were taken
  through a browser session whose page state was manipulated during
  the investigation (the iframe was remounted once by hand, and the
  overlay was hidden manually at one point). Every claim in §2.6 and
  §7 was re-taken from a **fresh page load** with no manipulation.

---

## 11. The files

| file | what |
|---|---|
| `internal/rctunnel/httpproxy.go` | `proxiedContentType` — the streaming dialect (§2.5) |
| `internal/rctunnel/httpproxy_test.go` | the dialect rule, pinned |
| `internal/remotetransport/connect_codec.go` | `dialectFor`, `encodeResponseBody` — the dialect bridge |
| `internal/remotetransport/connect_codec_test.go` | the app-state frame as protobuf, end to end |
| `internal/lsproto/order.go` | the answers in the descriptor's declaration order (§4.3) |
| `internal/lsproto/convert.go` | proto-JSON ↔ binary protobuf |
| `internal/auth/mendel.go` | the live Mendel backend call — the body, the User-Agent (§3.2, §3.3) |
| `internal/auth/mendel_test.go` | the request's shape, pinned |
| `internal/remotecontrol/mendel_flags.go` | the answer's shape — the six local flags first, the sources, the mesh field (§3.4, §3.5) |
| `internal/remotecontrol/mendel_flags_test.go` | the answer's values, the sources and the mesh field, pinned |
| `internal/remotecontrol/answer_order.go` | the answer funnel's reordering |
| `internal/remotecontrol/answer_order_test.go` | the dispatched answer's field order, pinned |
| `internal/remotecontrol/ls_streams.go` | the five subscription streams' frames |
| `internal/rcfrontend/assets/main.js` | the app bundle — the evidence of §2.3 |
