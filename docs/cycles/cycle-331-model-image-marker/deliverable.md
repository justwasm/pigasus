# A picture reaches the model — deliverable

**Cycle:** `cycle-331-model-image-marker`
**Subject:** the Web UI's attachment check refuses every picture for a model
that does not say it takes pictures, and the transport that carries a
user's **own** endpoint dropped the picture blocks. Both halves are fixed,
and the marker follows crush's shape: a per-model field in the provider
configuration.
**Evidence:** 【一手】 the live `GetUserStatus` against the operator's own
configuration; 【一手】 the request bytes a Web UI turn sent to a logging
endpoint; 【一手】 two live requests to the configured endpoint that read a
red/blue PNG; 【一手】 the reference's own frontend check
(`internal/rcfrontend/assets/main.js`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the marker** | there was none — "does this model take pictures" was guessed from the model's **name** | a per-model field in the provider configuration (`supports_images`), the way crush's `supports_attachments` works; a model that says nothing falls back to the family table |
| **MiniMax M3 / M3.1 Flash in the Web UI** | the attachment was refused: "Selected model does not support this media type" | the model config carries `supportedMimeTypes`, the attachment is accepted, and the picture is read by the model |
| **the picture on the wire** | the PiG-native gateway provider's request builder **dropped** the image blocks — `chatMessage.Content` was a `string` | the user message carries the OpenAI content array: the text part, then the `image_url` part with a `data:` URL |
| **a text-only turn** | `{"role":"user","content":"…"}` | **unchanged** — the same bytes |

---

## 1. The gap, exactly

### 1.1 The gate

【一手】 the Web UI's own check (from the bundle it ships):

```js
function ZNa(a,b){return b&&a?a.some(c=>{c=c.mimeType;
    return!(RA(c)||b.get(c)===!0)}):!1}

function aOa(a,b,c){if(b&&a.length!==0){
    if(ZNa(a,b.supportedMimeTypes))
        return"Selected model does not support this media type";…}}
```

The frontend hands the check `new Map(Object.entries(a.supportedMimeTypes))` —
an **empty map** when the server omits the field, and an empty map is
truthy while knowing no mime type at all. So the field is not optional in
practice: a model that takes pictures has to say so.

The clone's only decision point was a name heuristic
(`internal/remotecontrol/model_mime_types.go`): `gemini` / `claude` → yes,
`gpt-4o` / `gpt-5` → yes, `gpt-oss` → no, **everything else → no**.
`minimax` fell into "everything else", so the MiniMax models carried no
`supportedMimeTypes` and every attachment was refused.

### 1.2 The transport

The upload half already worked (cycle 328: `SaveMediaAsArtifact` answers
`remote-artifact://…`, the turn's request carries the media). The picture
also reached the session — `remoteCascadeSession.Send` builds the user
message with `coding.BuildUserContent(prompt, s.images)`, so the message
holds `ai.ImageContent` blocks.

The request builder dropped them:

```go
// internal/piagentbridge/gatewayprovider_request.go, before
case pi.UserMessage:
    request.Messages = append(request.Messages, chatMessage{Role: "user", Content: userText(m)})
```

`chatMessage.Content` was a **string**, so a user message could only be
text. And this is exactly the path a **user's own** endpoint takes: a
model from the reference's catalogue goes to CloudCode, a model from
`providers.json` goes to `NativeProvider` → this builder. So "Gemini can
attach a picture, MiniMax cannot" had two causes, and the second one was
this builder.

---

## 2. The marker

crush's own model list carries the capability per model
(`catwalk.Model.SupportsImages bool json:"supports_attachments"`, in
`charm.land/catwalk/pkg/catwalk/provider.go`), and crush's UI reads it off
the **selected model** (`m.currentModelSupportsImages()`). The clone
mirrors that shape:

| file | field |
|---|---|
| `internal/models/providers.go` | `ProviderModel.SupportsImages *bool` — json `supports_images,omitempty` |
| `internal/models/registry.go` | `Model.SupportsImages *bool` — json `supports_images,omitempty` |
| `internal/remotecontrol/lists.go` | `ModelInfo.SupportsImages *bool` — json `supports_images,omitempty` |

It is a **pointer**, because "not stated" is an answer of its own:

- **not stated** → the family table decides (`internal/remotecontrol/
  model_mime_types.go`);
- **stated** → the user's word wins, in both directions. A stated `false`
  turns the capability off for a family the table would have enabled; a
  stated `true` turns it on for a family the table knows nothing about.

The chain, end to end:

```
providers.json
  → models.ProviderModel.SupportsImages
  → models.Model.SupportsImages                 (ProviderConfig.Flatten)
  → remotecontrol.ModelInfo.SupportsImages      (rchost.hostListModels)
  → GetUserStatus's clientModelConfigs → supportedMimeTypes
```

Three front-ends write it: the file itself, `agy model add <id> --provider
<p> --supports-images` (and `--supports-images=false` for the opposite; a
flag that is not passed keeps the file's value), and the `/provider` panel's
generic JSON editor.

### The family table learns the MiniMax M3 generation

【一手】 2026-10-26, two live requests to the operator's configured
endpoint (`https://agw.k0s.io/v1/chat/completions`), each carrying a
64×64 PNG split red/blue down the middle:

| model | answer |
|---|---|
| `MiniMax-M3` | `Red, blue` |
| `MiniMax-M3.1-Flash` | `Red, blue` |

Both models read the picture, so the M3 generation takes pictures and the
table says so (`minimax m3` / `minimax-m3`).

---

## 3. The wire

`chatMessage.Content` became `any`: a message with no picture still
serializes as a **string** (the bytes the gateway sees today are the bytes
it saw yesterday — pinned by a test), and a message with a picture carries
the OpenAI content array:

```json
{"role":"user","content":[
  {"type":"text","text":"What color is the left half of this picture, and the right half?"},
  {"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo…"}}]}
```

【一手】 the shape is the OpenAI Chat Completions one, and it is the shape
PiG's own OpenAI dialect writes (`ai/openai.go`); the clone's **own**
transport already wrote the same array
(`internal/backend/gateway_request.go`, `formatOpenAIContentParts`). Both
transports now carry a picture the same way.

---

## 4. The evidence

### 4.1 The model configs the Web UI reads

【一手】 `out/web-image-support/client-model-configs.json` — the live
`GetUserStatus` against the operator's own configuration:

```json
{"label":"MiniMax M3","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M0"},
 "provider":"MODEL_PROVIDER_ANTIGRAVITY",
 "supportedMimeTypes":{"image/jpeg":true,"image/png":true}}
```

`MiniMax M3.1 Flash` carries the same map; `GPT-OSS 120B (Medium)` carries
none (it is text-only).

### 4.2 The request bytes

【一手】 `out/web-image-support/gateway-request-user-message.json` — the
provider pointed at a local endpoint that logs the body it receives, one
Web UI turn, one attached PNG:

```json
{"role":"user","content":[
  {"type":"text","text":"What color is the left half of this picture, and the right half?"},
  {"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgoAAAANSU…(1182 bytes)"}}]}
```

Before the change, that user message was `{"role":"user","content":"What
color is …"}` — the picture was not in it.

### 4.3 The browser

【一手】 the clone's own web UI (port 8919), the model set to `MiniMax M3`,
the red/blue PNG attached: the attachment was **accepted** (before: "Selected
model does not support this media type"), and the model's turn says
`attached.png (128x128, left=pure red, right=pure blue)` — it read the
picture.

### 4.4 The operator's own file

【一手】 `~/.config/openagy/providers.json`, written by the clone's own
command:

```json
{"id":"minimax-m3","name":"MiniMax M3","model":"MiniMax-M3",
 "context_window":1000000,"supports_images":true}
```

The rest of the file is byte-identical (the diff is two lines).

---

## 5. Hard constraints

1. **The marker is a pointer.** "Not stated" and "stated false" are
   different answers, and the author has to be able to tell them apart.
2. **The stated value beats the family table**, in both directions.
3. **A text-only turn's bytes do not change.** `chatMessage.Content` is
   `any` now, but a message with no picture still serializes as a string.
4. **The picture rides the OpenAI content array**, the url a
   `data:<mime>;base64,<payload>` URI — the same shape as the clone's own
   transport and PiG's own OpenAI dialect.
5. **`supportedMimeTypes` names exactly two types** (`image/jpeg`,
   `image/png`) — the reference's own catalogue names exactly those two.
6. **Two files that belong to the other agent's cycle were edited**:
   `internal/remotecontrol/media_test.go` and
   `internal/remotecontrol/modelenum_test.go` both asserted that MiniMax
   declares no mime types. The assertions had to change with the behaviour.

## 6. Open questions

- 【推测】 The family's MiniMax entry covers the **M3 generation** only
  (`minimax m3` / `minimax-m3`). M2, M1, Text-01 and abab have no
  first-hand evidence either way and are not in the table; a model outside
  the table states the marker itself.
- 【推测】 The family table still guesses from the **name** (gemini, claude,
  gpt-4o, gpt-5, gpt-oss). A model whose name carries one of those words
  and does not take pictures is misjudged; `supports_images: false` is the
  correction.
- 【推测】 Image blocks inside a **tool result** are still dropped
  (`buildChatAssistantMessage` takes the text only). Not touched here.
- 【推测】 Whether the Anthropic dialect's transport
  (`internal/backend/gateway_anthropic.go`) sends a picture as an image
  block was not examined.

## 7. Limitations

- The reference's binary is not part of this evidence: what is measured is
  the **clone's** behaviour (configuration → catalogue → `GetUserStatus` →
  request body). The reference side contributes only the frontend's check
  function and the shape of the `supportedMimeTypes` field 【一手】, both
  already recorded in `internal/remotecontrol/model_mime_types.go`.
- The logging endpoint answers with a minimal SSE frame: it witnesses the
  **request body**, not the model's answer. "The model read the picture" is
  the live endpoint's answer (§2, §4.3).
- Only the Web UI path was exercised. `--print` and the TUI run the same
  native provider, so they carry pictures too, but they were **not**
  verified separately.

## 8. The files

### The evidence

| file | what |
|---|---|
| `out/web-image-support/README.md` | the zh-CN walk-through |
| `out/web-image-support/client-model-configs.json` | 【一手】 the live `GetUserStatus`'s model configs |
| `out/web-image-support/gateway-request-user-message.json` | 【一手】 the request body a Web UI turn sent |
| `out/web-image-support/red-blue.png` | the picture both live probes carried |

### The code

| file | what |
|---|---|
| `internal/models/providers.go` | the config marker and its carrier |
| `internal/models/registry.go` | the registry's carrier |
| `internal/models/edit.go` | the writer: `setOrDeleteBool` |
| `internal/cli/model_actions.go` | the `--supports-images` flag |
| `internal/cli/provider_cmds.go` | the flag's usage text |
| `internal/remotecontrol/lists.go` | the catalogue row's marker |
| `internal/remotecontrol/model_mime_types.go` | the decision point: stated beats the table |
| `internal/remotecontrol/dispatch_userstatus.go` | the Web UI's model configs |
| `internal/rchost/remote_control_backends.go` | the catalogue's builder |
| `internal/piagentbridge/gatewayprovider_request.go` | the picture on the wire |
