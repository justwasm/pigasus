# The CloudCode dialect

> The private Google "Code Assist" surface that `agy` talks to: the request
> envelope, the SSE frames, the tool round trip. **Part A** is the contract a
> local service must implement; **Part B** is how each field was derived.

---

## 0. What this is for

The reference CLI does not use the public Gemini API. It uses Google's private
**Code Assist** surface — the one behind a consumer subscription, with a
Google-private RPC name, a Google-private request envelope, and a handful of
Google-private members (`thoughtSignature`, `labels`, `sessionId`).

Two audiences read this file:

- **A local-service author** — you are standing in for Google. You need the
  exact bytes the clone sends and the exact bytes it expects back. Part A.
- **A clone maintainer** — you are changing the transport. You need to know
  which field is pinned by evidence and which is a guess. Part B.

The executable form of Part A is the **`geminicode` package** at the top of
the tree. It has no dependency on any other package in this repository, so it
can be imported on its own; `internal/backend` adapts between it and the
application's neutral types.

```
geminicode/     the dialect: wire types, envelope, schema, SSE, client
internal/backend/  the adapter + the transport policy (retry, timeout, token)
```

## Part A — the contract

### A.1 The endpoint

One RPC carries the inference:

```
POST {host}/v1internal:streamGenerateContent?alt=sse
```

| | |
|---|---|
| host | `https://daily-cloudcode-pa.googleapis.com` (the `cloudcode` endpoint; see [`endpoints.md`](endpoints.md)) |
| method | `POST` |
| path | `/v1internal:streamGenerateContent` |
| query | `alt=sse` — **required**. Without it the endpoint answers with one JSON document instead of an event stream |
| body | `application/json`, the wrapped envelope (A.3) |
| answer | `text/event-stream` (A.5) |

The host is a **bare host**, not a base URL and not a path prefix. The path is
part of the RPC name.

> `cloudcode-pa.googleapis.com` (the production host) answers **429** for a
> consumer account on every model; `daily-cloudcode-pa.googleapis.com`
> answers 200. Same bytes, same headers — the host is the only difference.

To point the clone at a local service, either set the environment variable
`CLOUD_CODE_URL=http://127.0.0.1:8080` or put it in the endpoints file
(`~/.config/openagy/config.json` → `{"endpoints": {"cloudcode": "…"}}`).

### A.2 The headers

```
Authorization: Bearer ya29.a0AX07…
Content-Type: application/json
User-Agent: antigravity/cli/1.4.4 (aidev_client; os_type=darwin; arch=arm64; cl=1003595037; auth_method=consumer)
```

| header | value | notes |
|---|---|---|
| `Authorization` | `Bearer <oauth access token>` | the same token `loadCodeAssist` and the quota call use |
| `Content-Type` | `application/json` | |
| `User-Agent` | the reference's own string | `os_type` is `runtime.GOOS`, `arch` is `runtime.GOARCH`; the `cl=` build number moves with the reference version |

The `User-Agent` is **not** load-bearing for the server, but it is how the
proxy tells the clone's traffic apart from a browser's, and it is what the
reference's own `Mendel` flag set keys on. Send something that identifies
yourself.

### A.3 The request body — the wrapped envelope

**The body is not a bare `GenerateContentRequest`.** It is an envelope with
the Gemini request nested under `request`:

```json
{
  "project": "aicode-consumers",
  "requestId": "agent/<32-hex>/<unix-millis>/<32-hex>/1",
  "request": {
    "contents": [{"role": "user", "parts": [{"text": "…"}]}],
    "systemInstruction": {"role": "user", "parts": [{"text": "…"}]},
    "tools": [{"functionDeclarations": [{"name": "read_file", "parameters": {"type": "OBJECT", "…": "…"}}]}],
    "labels": {"used_non_gemini_model": "true"},
    "generationConfig": {"maxOutputTokens": 65536, "thinkingConfig": {"includeThoughts": true, "thinkingBudget": 1000}},
    "sessionId": "-3750763034362895579"
  },
  "model": "gemini-3.8-flash-high",
  "userAgent": "antigravity",
  "requestType": "agent"
}
```

**Envelope members**

| member | type | value |
|---|---|---|
| `project` | string | the consumer project — `aicode-consumers`, unless the caller configured another |
| `requestId` | string | `agent/<hex16>/<millis>/<hex16>/1` — see A.4 |
| `request` | object | the inner Gemini request — see below |
| `model` | string | the wire model id, e.g. `gemini-3.8-flash-high`, `claude-sonnet-4-6` |
| `userAgent` | string | the constant `antigravity` |
| `requestType` | string | the constant `agent` |

**Inner request members**

| member | type | when present | notes |
|---|---|---|---|
| `contents` | array | always | the conversation, in order — see A.6 |
| `systemInstruction` | object | when there is a system prompt | `{"role":"user","parts":[{"text":"…"}]}` — **the role is there**; the public Gemini API leaves it out |
| `tools` | array | when the model may call tools | `[{functionDeclarations:[…]}]` |
| `labels` | object | when the model is not a Gemini model | see A.7 |
| `generationConfig` | object | always | see below |
| `sessionId` | string | always | **a JSON string**, not a number — sending the integer gives `400 Invalid value at 'request.session_id' (TYPE_STRING)` |

**`generationConfig`**

| member | value | note |
|---|---|---|
| `maxOutputTokens` | `65536` | `64000` for a Claude model |
| `temperature` | number | **omitted** when the caller has no opinion — the reference omits the member rather than sending `0` |
| `thinkingConfig.includeThoughts` | `true` | always: the reference asks for the reasoning trace |
| `thinkingConfig.thinkingBudget` | `1000` | `1024` for a Claude model |
| `topP`, `topK`, `stopSequences`, `candidateCount` | — | not sent by the reference; accepted so one struct serves both surfaces |

**Schema conversion.** The clone's tool catalogue is provider-neutral JSON
Schema; the wire wants the Gemini `Schema` proto's spelling:

| JSON Schema | Gemini `Schema` on the wire |
|---|---|
| `"type": "object"` | `"type": "OBJECT"` — the type is **upper-cased** |
| `"type": "string"` | `"type": "STRING"` |
| `"minLength": 1` | `"minLength": "1"` — **a JSON string**; the proto's `int64` fields are protojson-encoded as strings |
| `"minItems": 2` | `"minItems": "2"` — same |
| `"additionalProperties"` | **dropped** |
| `"$schema"` | **dropped** |

The accepted keyword set is `type`, `description`, `properties`, `required`,
`items`, `enum`, `nullable`, `format`, `minimum`, `maximum`, `minItems`,
`maxItems`, `minLength`, `maxLength`, `pattern`. Everything else is dropped
rather than risking a 400.

### A.4 The two ids

- **`requestId`** — `agent/<32 hex>/<unix millis>/<32 hex>/1`. Minted **once
  per turn**, so a retry of a dropped connection re-sends the same body. A
  service that de-duplicates by `requestId` will see the retry as the same
  request — that is the point.
- **`sessionId`** — a negative random 64-bit number, **as a string**. It
  groups the turns of one conversation.

### A.5 The answer — the SSE frames

```
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}],
        "usageMetadata":{"promptTokenCount":123,"candidatesTokenCount":1,"totalTokenCount":124},
        "modelVersion":"gemini-3.5-flash-lite"},
       "traceId":"…","metadata":{}}

data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]},
        "finishReason":"STOP"}],
        "usageMetadata":{"promptTokenCount":123,"candidatesTokenCount":2,"totalTokenCount":125}},
       "traceId":"…","metadata":{}}
```

Framing rules:

- every frame is one `data: ` line followed by a blank line;
- the payload is the **wrapped** frame: the response under `response`, the
  trace id beside it;
- a **bare** frame (`data: {"candidates":[…]}`) is also accepted — some
  intermediaries strip the wrapper, and the clone copes;
- `)]}'` (Google's anti-hijacking prefix) and keep-alive lines are ignored;
- the last frame carries `finishReason` on its candidate.

**Response members**

| member | notes |
|---|---|
| `candidates[].content.role` | `model` |
| `candidates[].content.parts[]` | text, thought, functionCall — see A.6 |
| `candidates[].finishReason` | `STOP` at the end of a turn |
| `usageMetadata.promptTokenCount` | |
| `usageMetadata.candidatesTokenCount` | |
| `usageMetadata.totalTokenCount` | |
| `usageMetadata.thoughtsTokenCount` | present when the model spent tokens thinking; the Claude endpoint omits it |
| `traceId` | |
| `metadata` | an empty object in practice |

### A.6 The tool round trip

This is the part that is easiest to get wrong, and it is pinned by three
rules.

**1. A tool result travels back under the `model` role, not `user`.**

```json
{"role":"model","parts":[{"functionResponse":{"id":"call_113205","name":"run_command",
  "response":{"output":"…hello-tool\r\n"}}}]}
```

**2. The envelope under `response` says whether the call failed.**

`agy` 1.4.8 changed this, and the release notes call it out: *"failed tool
calls … were being passed back to the agent as ordinary results; the agent now
sees them as errors and can recover on its next step."*

| the call | the envelope |
|---|---|
| succeeded | `{"response": {"output": "<the result>"}}` |
| failed | `{"response": {"error": "<the failure text>"}}` |

The **body is the same string either way** — the reference's own failure text,
the two stamp lines and then the sentence:

```
Created At: …
Completed At: …
Encountered error in tool execution: file does not exist
```

So the *envelope* is the only signal. Before 1.4.8 both went under `output`.

**3. The `thoughtSignature` is replayed on the `functionCall` part.**

The model's first frame carries the signature next to the call:

```json
{"thoughtSignature":"EmoKaAFpFH0TejWnoITg83m8…",
 "functionCall":{"name":"run_command",
                 "args":{"CommandLine":"echo hello-tool","WaitMsBeforeAsync":5000},
                 "id":"call_113205"}}
```

and the next request replays **both**, on the same part:

```json
{"role":"model","parts":[{"functionCall":{…},"thoughtSignature":"Emo…"}]}
```

Gemini 3 rejects the turn without it. The signature is keyed by the call's
`id`, so a service that keeps the conversation must keep the mapping.

**4. The tool catalogue must be unique.**

Gemini rejects the *whole request* for a duplicate function declaration:

```
Error: The turn stopped before it finished: Duplicate function declaration found: invoke_subagent
```

The clone keeps the first declaration per name.

### A.7 The labels

| the model | the labels |
|---|---|
| a Claude model (`claude…`) | `{"used_claude":"true","used_claude_conservative":"true","used_non_gemini_model":"true"}` |
| any other non-Gemini model | `{"used_non_gemini_model":"true"}` |
| a Gemini model (`gemini…`) | none — the member is omitted |

### A.8 The thinking trace

A part with `"thought": true` carries the model's reasoning. The reference
renders it inside `<think>…</think>` markers, and the clone's transcript
sanitizer consumes exactly that. A service that returns thoughts should mark
the part and let the client render it; a service that does not care may omit
them entirely.

### A.9 A checklist for a local service

1. Accept `POST /v1internal:streamGenerateContent?alt=sse` on **your** host.
2. Read the envelope: the model is `body["model"]`, the conversation is
   `body["request"]["contents"]`, the tool catalogue is
   `body["request"]["tools"][0]["functionDeclarations"]`.
3. Answer `200` with `Content-Type: text/event-stream` and one `data: ` frame
   per chunk; put the response under `response` and the trace id beside it.
4. Put `finishReason` on the last candidate.
5. If the model asks for a tool, emit a `functionCall` part with an `id` and
   the `thoughtSignature` you want replayed.
6. On the next turn, expect the tool's result under `response.output` (or
   `response.error`) in a **`model`-role content, and expect the
   `thoughtSignature` back on the `functionCall` part.
7. Do not reject a duplicate tool name silently — the reference does not send
   one, but if your catalogue has one, drop it before answering.

A worked example lives in the package's own tests: `geminicode/client_test.go`
pins the request bytes and the frame shapes.

### A.10 The other RPCs on the same host

The turn is not the only call the clone makes to this host:

| RPC | what it is for |
|---|---|
| `v1internal:loadCodeAssist` | the account's tier, quota and project — the answer carries `cloudaicompanionProject` |
| `v1internal:retrieveUserQuotaSummary` | the quota counters |
| `v1internal:listExperiments` | the Mendel feature flags |
| `v1internal:fetchAvailableModels` | the model catalogue |
| `v1internal:generateChat` | the unary sibling of the stream (the shims use it) |

`loadCodeAssist` takes a `metadata` block — `ideType`, `platform`,
`pluginType` — and answers with the tier list and the project id
(`aicode-consumers`).

---

## Part B — how the dialect was derived

> This half is for the maintainers. It says which claim rests on what.

### B.1 The evidence

| grade | what | where |
|---|---|---|
| 【一手】 | live traffic from the reference, captured 2026-10-20 by a verbatim local passthrough (`CLOUD_CODE_URL=http://127.0.0.1:51999`) that forwards to `daily-cloudcode-pa.googleapis.com` and writes the raw request/response bytes | the cycle report [`cloudcode-model-switching`](../cycles/cloudcode-model-switching/deliverable.md) |
| 【一手】 | the 1.4.8 tool-error envelope, captured live against the 1.4.8 binary (`devutils/refcapture`, three runs) | `internal/tools/testdata/tool_errors_1.4.8.txt`; commit `5fedb817` |
| 【一手】 | `loadCodeAssist`'s live answer — `cloudaicompanionProject: "aicode-consumers"`, the tier list | `out/web-ui-log-fixes/load_code_assist_live.json` |
| 【一手】 | the reference's whole CloudCode traffic going to a loopback endpoint — the five RPC names, and that the reference does not check the host or refuse `http://` | `out/refbackend-custom-models/ccpa_traffic.txt` |
| 【反编译】 | the reference's own function name for this call path — `codeassistclient.(*CodeAssistClient).streamGenerateContentFromCCPA` | `out/decompile/call-graph.json`, `out/seeds/funcs.txt` |
| 【strings】 | the `cl=1002753436` build number and the `antigravity/cli/1.4.3` header shape | the reference's string table |
| 【推测】 | the `cascade_id` / `model_enum` labels the capture shows on a Gemini request — **the clone does not send them**, because their semantics are unknown | the capture in the cycle report |

### B.2 The package

| file | what it holds |
|---|---|
| `geminicode/wire.go` | the wire types and the envelope helpers (`Inner`, `Wrapped`, `ModelName`, `SystemText`) |
| `geminicode/request.go` | `Turn` and the builder — the request body, the tool declarations, the labels, the ids |
| `geminicode/schema.go` | the JSON Schema → Gemini `Schema` conversion |
| `geminicode/frame.go` | `ParseFrame` — one SSE payload, wrapped or bare |
| `geminicode/stream.go` | `Decoder` — the pure state machine: frames → chunks, tool-call collection, the thought markers |
| `geminicode/client.go` | `Client` — the POST, the token header, the 401 retry, the convenience `Stream` |
| `geminicode/errors.go` | `HTTPError` — the status, the raw body, the URL |
| `geminicode/doc.go` | the package contract |

**One type family.** The transport and the proxy used to keep two near
identical sets of wire types (`internal/geminiwire` and the `cc*` types in
`internal/backend`). They are now one: `geminicode.Part`, `.FunctionCall`,
`.Content`, … The two conflicts were resolved in favour of the wire:

- `FunctionCall.Args` is a `json.RawMessage` — the model's own bytes survive
  the round trip, key order included. The proxy re-encodes them for the
  provider-neutral shape, which is its own business.
- `FunctionResponse.Response` is `any` — the transport always assigns a
  `map[string]any` (`{"output": …}` or `{"error": …}`), the proxy accepts
  whatever it read.

**No `internal/` dependency.** `go list -deps ./geminicode` shows the standard
library only. That is why the package carries its own `Turn`, `TurnMessage`
and `TurnTool` types instead of reusing the application's neutral ones: a
dialect library that depends on its host cannot be reused.

### B.3 What the dialect does *not* own

The transport **policy** is the application's, and it stayed in
`internal/backend`:

| policy | where |
|---|---|
| retry a dropped connection, on the reference's exponential schedule | `internal/backend/cloudcode_stream.go` → `openWithRetry`, `runCloudCodeStreamBodyWithRetry` |
| bound a stalled stream | `internal/backend/cloudcode_stream.go` → `NewIdleTimeoutReader` |
| classify a failure (quota exhausted vs expired sign-in vs HTML proxy page) | `internal/backend/http_error.go`, `gemini_api_key_retry.go` |
| refresh an expired token | `internal/backend/cloudcode.go` → the token source |

`geminicode.Client` performs **one** request and reports what happened. Its
`HTTPError` carries the status, the body and the URL; the adapter maps it to
the application's own `backend.HTTPError` so the headless error reporter and
the TUI keep their classification.

### B.4 Field-by-field

| field | grade | where it comes from |
|---|---|---|
| the envelope members and their order | 【一手】 | the capture |
| `sessionId` as a string | 【一手】 | the capture, plus the server's own `400 Invalid value at 'request.session_id' (TYPE_STRING)` |
| `systemInstruction.role` = `user` | 【一手】 | the capture |
| the upper-cased schema types and the string-encoded counts | 【一手】 | the capture's declarations |
| `used_claude` / `used_claude_conservative` / `used_non_gemini_model` | 【一手】 | the capture |
| `maxOutputTokens` 65536 / 64000, `thinkingBudget` 1000 / 1024 | 【一手】 | the capture, both model families |
| the `thoughtSignature` round trip | 【一手】 | the capture's tool round trip |
| the `model`-role tool result | 【一手】 | the capture's tool round trip |
| `response.output` vs `response.error` | 【一手】 | the 1.4.8 capture |
| the `requestId` shape | 【一手】 | the capture |
| `includeThoughts: true` on every request | 【一手】 | the capture |
| `temperature` omitted when unset | 【一手】 | the capture |
| `cascade_id` / `model_enum` | 【推测】 | the capture shows them on a Gemini request; the clone does not send them and their semantics are unknown |

### B.5 What the tests pin

`geminicode/client_test.go` is the executable form of Part A:

| test | what it pins |
|---|---|
| `TestClient_TextStream` | the envelope members, the `systemInstruction` role, `maxOutputTokens`, `sessionId`, the streamed text, the usage |
| `TestClient_ToolCallAndSignatureReplay` | the tool call's `id` and `name`, the signature on the replayed part, the `model`-role result, the `output` envelope, the schema conversion, the dropped `additionalProperties` |
| `TestClient_HTTPErrorIsSynchronous` | a non-2xx answer is an `*HTTPError` with the raw body, and no channel is returned |
| `TestClient_BareResponseFrame` | a bare frame (no `response` wrapper) is accepted |
| `TestClient_ThinkingIsWrappedForSanitizer` | the `<think>…</think>` markers |
| `TestClient_ThinkingClosesBeforeToolCall` | the thought block closes before the call |
| `TestClient_DuplicateToolNamesDeduped` | the duplicate declaration is dropped |
| `TestClient_ModelFromRequest` | the model travels, and a Claude model gets its labels |
| `TestSchemaProtoIntStrings` | the protojson int-as-string spellings |

`internal/backend/cloudcode_test.go` pins the **adapter** (the neutral turn
reaches the wire; the dialect's chunks come back as the application's chunks;
the HTTP error keeps the application's type).

### B.6 Known differences from the capture

- **The envelope's `contents` member is omitted, not `null`,** when the
  conversation is empty. The reference sends `"contents":null` in that case.
  No real turn is empty.
- **`cascade_id` / `model_enum` are not sent.** Their semantics are unknown
  【推测】; a service that needs them must ask the reference again.
- **The `User-Agent` build number is a constant.** It must move with the
  reference version — it is `cl=1003595037` today, and the 1.4.3 capture
  showed `cl=1002753436`.

### B.7 硬约束 / 存疑 / limitations

- **硬约束**: the live wire did not change in this move. The request bytes,
  the frame handling, the tool round trip and the transport policy are the
  same; only their location moved.
- **硬约束**: the package has no `internal/` dependency, and the tests that
  prove it are in `geminicode/` itself.
- **存疑**: the **tool `thoughtSignature`s live in the client's memory
  only**. A long resumed conversation across a restart is unproven — the
  measured `-c` turn worked without them, but the reference itself keeps them
  somewhere the clone does not model.
- **存疑**: **reasoning display depends on the model.** The Claude endpoint
  streams `thought:true` parts; the measured Gemini turns returned only
  signature-only thought parts (no visible text).
- **limitations**: no live capture was taken on Linux or Windows;
  `os_type` follows `runtime.GOOS`.
- **limitations**: the spec covers the **turn** RPC. The other five RPCs
  (A.10) are described but not specified field by field.

---

Reports: [`cloudcode-model-switching`](../cycles/cloudcode-model-switching/deliverable.md)
(the original capture), [`cycle-353-cloudcode-dialect`](../cycles/cycle-353-cloudcode-dialect/deliverable.md)
(the extraction).
