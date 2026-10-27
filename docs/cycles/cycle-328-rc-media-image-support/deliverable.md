# The Web UI can attach a picture, and the picture reaches the model — deliverable

**Cycle:** `cycle-328-rc-media-image-support`
**Subject:** the media surface of the Antigravity web UI — the model
catalogue's `supportedMimeTypes`, the two media RPCs
(`SaveMediaAsArtifact`, `DeleteMediaArtifact`), and the path a picture
takes from the composer's attachment to the model's request.
**Evidence:** 【一手】 the reference's own frontend bundle
(`internal/rcfrontend/assets/main.js`): the attachment check that
produced the user's error, the upload's three-attempt loop, the
`remote-artifact://` scheme, and the `FileDescriptorProto` of
`codeium_common.proto` the bundle embeds; 【反编译】 the language
server's own service descriptor
(`out/ide-language-server/protos/language_server.protoset`): the two
media methods, their four messages, and
`SendUserCascadeMessageRequest.media`; 【一手】 the reference's own live
catalogue (`out/ide-language-server/live/reference/GetUserStatus.json`):
the mime types the model that is *enabled* declares; 【一手】 the
reference's own JSON wire types (`internal/geminiwire`) and PiG's
provider dialects: the spelling of an inline image on the wire.

The user reported, from the web UI and from a locally started
language server:

> Invalid media. Media upload failed after 3 attempts. Selected model
> does not support this media type

Two of those three strings are the reference's own, and both are now
answered.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the catalogue's `supportedMimeTypes`** | absent — and an absent map reads as an *empty* one, so the Web UI refused every picture | the mime types a model accepts, on the model that accepts them |
| **`SaveMediaAsArtifact`** | no route at all — the upload failed, three times over | the picture is stored, and the answer carries a `remote-artifact://` uri |
| **`DeleteMediaArtifact`** | no route at all | the artifact is dropped, and the answer carries the success flag |
| **`SendUserCascadeMessageRequest.media`** | read as nothing | the picture's bytes ride the turn |
| **the model request** | text only — "image blocks have no representation in the clone's Message shape" | the picture rides the request as the transport's own image part |
| **the transports** | CloudCode, OpenAI, Anthropic, Gemini: text only | each spells the picture its own way |

---

## 1. The root cause: an absent map is an *empty* map

The message the user saw is the reference's own string, and it comes
from the *frontend*:

【一手】 `internal/rcfrontend/assets/main.js`:

```js
function ZNa(a,b){return b&&a?a.some(c=>{c=c.mimeType;
    return!(RA(c)||b.get(c)===!0)}):!1}

function aOa(a,b,c){if(b&&a.length!==0){
    if(ZNa(a,b.supportedMimeTypes))
        return"Selected model does not support this media type";…}}
```

and the composer calls it with the *current model config*:

```js
ce=(0,A.useMemo)(()=>aOa(Ja,x,Pd),[Ja,x,Pd]);
(0,A.useEffect)(()=>{ce&&Im(`Attachment error: ${ce}`)},[ce]);
```

`x` is the model config the frontend's own mapper built:

```js
var Rk=a=>a.modelOrAlias?{label:a.label,…,
    supportedMimeTypes:new Map(Object.entries(a.supportedMimeTypes)),…}
```

So the map the check reads is `new Map(Object.entries(config.supportedMimeTypes))`
— and **a proto3 map field that the server omits decodes as an empty
map, not as an absent one**. `new Map()` is truthy, so `ZNa` takes its
"some" branch, and for a picture:

- `RA(mime)` is `NNa(mime) || QNa(mime)` — the office-document types
  and the zip types. It does **not** know `image/png`;
- `map.get("image/png")` is `undefined`, and `undefined === true` is
  false.

So `!(false || false)` is true, `some` is true, and the check returns
"Selected model does not support this media type" — for *every*
picture, whatever the model.

The field itself is the reference's own:

【反编译】 `exa.codeium_common_pb.ClientModelConfig.supported_mime_types`
— **field 18, a `map<string, bool>`** — parsed out of the
`FileDescriptorProto` the reference's frontend bundle embeds
(`out/rc-media/extract_codeium_common.py` →
`out/rc-media/codeium_common_media.json`):

```json
"supported_mime_types": {
  "number": 18, "repeated": true, "type": "TYPE_MESSAGE",
  "type_name": ".exa.codeium_common_pb.ClientModelConfig.SupportedMimeTypesEntry"
},
"map_entries": {"SupportedMimeTypesEntry": {
  "key": {"number": 1, "type": "TYPE_STRING"},
  "value": {"number": 2, "type": "TYPE_BOOL"}
}}
```

The same message also carries `supports_images` (field 5, a bool) —
which the reference's own catalogue does not set, because the map
already says it.

And the reference's own catalogue says what the map holds:

【一手】 `out/ide-language-server/live/reference/GetUserStatus.json` —
the entry the reference *enables*:

```json
{"label":"gemini-3.1-pro", …,
 "supportedMimeTypes":{"image/jpeg":true,"image/png":true}}
```

The entries the reference disables carry no map at all.

**So the map is not optional in practice.** The clone now emits it for
every model that takes pictures, and omits it for the ones that do
not — an empty map is a map's default, and proto-JSON omits a default.

---

## 2. The upload: two RPCs the clone never routed

【一手】 the frontend's upload hook retries three times and then gives
up:

```js
for(let B=1;B<=3;B++)try{let D=v(await u(c({media:t})));
    if(D.uri){w(D.uri);return}}
catch(D){v(),x=D,console.error(`Failed to save media artifact (attempt ${B}/3)`,D),
    B<3&&v(await u(new Promise(C=>setTimeout(C,Math.min(1E3*Math.pow(2,B-1),5E3)))))}
m.current.add(t);console.error("Media upload failed after 3 attempts.",x);
b?.("Media upload failed after 3 attempts.")
```

"Media upload failed after 3 attempts." is the second string the user
saw — and it is the *frontend's* toast, so the upload call itself
failed. The clone had no route for the call.

【反编译】 the two methods are the reference's own language-server
service (`out/ide-language-server/protos/language_server.protoset`):

```
LanguageServerService.SaveMediaAsArtifact
    exa.language_server_pb.SaveMediaAsArtifactRequest
    → exa.language_server_pb.SaveMediaAsArtifactResponse
LanguageServerService.DeleteMediaArtifact
    exa.language_server_pb.DeleteMediaArtifactRequest
    → exa.language_server_pb.DeleteMediaArtifactResponse

SaveMediaAsArtifactRequest  { media = 1 : exa.codeium_common_pb.Media }
SaveMediaAsArtifactResponse { uri = 1 : string }
DeleteMediaArtifactRequest  { uri = 1 : string }
DeleteMediaArtifactResponse { success = 1 : bool, error_message = 2 : string }
```

The reference's own handlers are at 0x101bc4c80
(`SaveMediaAsArtifact`) and 0x101bc4ed0 (`DeleteMediaArtifact`) in the
IDE's language server
(`out/local-macos/gosrc/language_server_pb/language_server_go_grpc/api_generated.go`).

### The uri's scheme matters

【一手】 the frontend recognises the scheme of the uri the upload
answers with:

```js
function ZA(a){return!!a?.startsWith("remote-artifact://")}
```

and the composer keeps the picture's bytes inline *for that scheme*:

```js
…&&t.uri&&!ZA(t?.uri)&&k.current.has(t)
    ?{…t,payload:{case:"inlineData",value:new Uint8Array}}:t
```

— the payload is emptied only for a uri that points at a **local
file** (the Electron case, where the server can read the path). A
`remote-artifact://` uri keeps the bytes, and the bytes are what the
turn carries.

The clone therefore answers `remote-artifact://<id>`, and keeps the
picture in a small process-wide store (64 artifacts, 64 MiB). The
store is what `DeleteMediaArtifact` drops.

---

## 3. The message: the picture rides the turn

【反编译】 the request that carries the message:

```
exa.language_server_pb.SendUserCascadeMessageRequest {
  cascade_id = 1, items = 2, …,
  media = 14 : repeated exa.codeium_common_pb.Media
}
```

and the media's own shape:

【反编译】 `exa.codeium_common_pb.Media` (from the same descriptor
closure the frontend bundle embeds):

```json
"mime_type": {"number": 1, "type": "TYPE_STRING"},
"inline_data": {"number": 2, "type": "TYPE_BYTES"},
"oneofs": {"payload": ["inline_data", "blobref"]}
```

The payload is a oneof, so proto-JSON spells it by its member:
`{"mimeType":"image/png","inlineData":"<base64>"}`.

The clone's chain, end to end:

```
the composer's attachment
  → SaveMediaAsArtifact            (the artifact's uri)
  → SendUserCascadeMessage.media    (the picture's bytes)
  → the queued message              (lsQueuedMessage.Images)
  → the prompt                      (MessagePrompt.Images)
  → PiG's user content blocks       (coding.BuildUserContent)
  → the neutral message             (backend.Message.Images)
  → the transport's own image part
```

Two seams are worth naming:

- **The queue.** A message that arrives while a turn is in flight
  waits for it — and it waits *with* its picture. The pictures are a
  field of the queued message, not of the request.
- **PiG's own shape.** The picture becomes an `ai.ImageContent` — the
  base64 payload and the media type — and the turn sends it with
  `Session.SendContent`, the same call `Session.Send` makes for a text
  prompt (`coding.BuildUserContent`, `coding/session.go`). PiG's own
  image policy then decodes and re-encodes it, exactly as it does for
  a picture attached to a CLI prompt.

---

## 4. The transports: four spellings of one picture

The neutral shape is the mime type and the bytes
(`backend.ImagePart`). Each transport spells it its own way, and each
spelling is taken from a source that is not the clone's own invention:

| transport | the part on the wire | source |
|---|---|---|
| CloudCode | `{"inlineData":{"mimeType":"image/png","data":"<base64>"}}` | 【一手】 the reference's own JSON wire types (`internal/geminiwire`) |
| Gemini API | `{"inlineData":{"mimeType":"image/png","data":"<base64>"}}` | 【一手】 the same |
| OpenAI-shaped | `{"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}` | 【一手】 PiG's own OpenAI dialect (`ai/openai.go`) |
| Anthropic | `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"…"}}` | 【一手】 PiG's own Anthropic dialect (`ai/anthropic.go`) |

The OpenAI dialect's `content` is a **string** for a text-only message
and an **array** for a message with a picture. The clone keeps the
string in that case: the key is the same, only the JSON type changes,
and every other consumer of the shape (the transcripts, the
persistence) sees what it saw before.

---

## 5. What the clone changed

| file | what |
|---|---|
| `internal/remotecontrol/model_mime_types.go` | **new** — the mime types a model accepts, and the rule that decides it |
| `internal/remotecontrol/dispatch_userstatus.go` | the catalogue emits `supportedMimeTypes` |
| `internal/remotecontrol/media_store.go` | **new** — the `remote-artifact://` store |
| `internal/remotecontrol/dispatch_media.go` | **new** — the two media RPCs |
| `internal/remotecontrol/message_image.go` | **new** — `MessageImage`, and the media → images conversion |
| `internal/remotecontrol/dispatch_languageserver.go` | the two routes |
| `internal/remotecontrol/dispatch_ls.go` | the request's `media` reaches the turn |
| `internal/remotecontrol/ls_queue.go` | the queued message carries its pictures |
| `internal/remotecontrol/backend.go` | `MessagePrompt.Images` |
| `internal/backend/client.go` | `backend.ImagePart`, `Message.Images` |
| `internal/backend/cloudcode_request.go` | the CloudCode inline part |
| `internal/backend/gateway_request.go` | the OpenAI content array |
| `internal/backend/gateway_anthropic.go` | the Anthropic image block |
| `internal/backend/gemini.go` | the Gemini inline part |
| `internal/piagentbridge/stream.go` | PiG's image blocks → `Message.Images` |
| `internal/piagentbridge/piagent_remote.go` | the remote session sends the content blocks |
| `internal/pigrt/session.go` | `RunWithContent` |

---

## 6. 硬约束 / the load-bearing rules

- **An absent `supportedMimeTypes` is not "no restriction" — it is an
  empty map, and an empty map refuses every picture.** The frontend's
  `ZNa` takes its "some" branch on a truthy map, and a truthy empty
  map fails every mime type that `RA` does not know. A model that
  takes pictures *has* to declare them.
- **The uri's scheme is load-bearing.** The frontend empties the
  picture's payload for any uri that is **not** `remote-artifact://`.
  Answering with a `file://` uri would make the turn carry a picture
  with no bytes in it.
- **The media RPCs answer in the schema's own dialect.** The Web UI
  speaks the v2 "binary unary" Connect protocol: the request body is
  a binary protobuf and the answer has to be one too. The dispatcher
  speaks JSON, and `internal/lsproto` bridges the two — a request's
  `Media` message decodes to `{"mimeType":…,"inlineData":…}`, and the
  answer's `uri` encodes back into `SaveMediaAsArtifactResponse`.
- **A text-only message keeps its shape.** The OpenAI dialect's
  `content` stays a string, the CloudCode one keeps its single empty
  text part, the Gemini one keeps dropping a message with neither text
  nor picture. The transports' existing tests are the proof.
- **The picture belongs to the message, not to the conversation.** The
  history the clone replays into a fresh session is text-only; a
  picture from an earlier turn is not replayed.
- **`internal/backend/gemini.go` and the files under
  `internal/remotecontrol/` are the other agent's.** The edits in this
  cycle are additive and were staged by explicit path, one hunk each.

---

## 7. 存疑 / open questions

- **【推测】 Which models take pictures.** The reference's catalogue
  says it for exactly one model — the Gemini one it enables — and says
  nothing for the Anthropic entry it *disables*. The clone's rule is
  read off the model's own name, the way `userStatusModelProvider`
  reads the vendor off it: Gemini and Claude take pictures, the
  open-weight GPT-OSS models do not, and a model whose name says none
  of these is treated as text-only. A wrong "yes" shows an attachment
  button the model cannot answer; a wrong "no" hides a capability it
  has. Neither is recoverable from the reference.
- **【存疑】 Whether the reference's server validates the mime type on
  upload.** The user's message put "Media upload failed after 3
  attempts." next to "Selected model does not support this media
  type", and the frontend's own toast for the second is
  `Attachment error: …` — a *different* toast. It is therefore
  possible that the reference's `SaveMediaAsArtifact` refuses a media
  type its model does not support, and that the clone's should refuse
  it too. The clone answers the upload for any picture.
- **【存疑】 "Invalid media."** The string is in neither the clone, nor
  the bundle, nor the captures this cycle read. It is probably a toast
  title or an upstream error the clone never sees.
- **【存疑】 Where the reference keeps the artifact.** The clone keeps
  it in memory, for the life of the daemon. The reference may write it
  to disk — the uri scheme, `remote-artifact://`, says nothing about
  where it lives.

---

## 8. Limitations

- **The CLI's own provider path still drops a picture.**
  `internal/models` — the path cycle 274 documented as "a vision
  prompt through a custom model therefore loses its attachments
  silently" — is untouched. This cycle is the remote-control and web
  surface; the registry path is a separate piece of work.
- **No live run against antigravity.google.com.** The frontend's own
  code and the reference's own catalogue are the evidence; the clone's
  answers are pinned by tests, not by a browser session.
- **A picture larger than the model's limit is PiG's business.** PiG
  resizes the attachment and drops it (with its own
  "[Image omitted: …]" note) when it cannot be resized — the clone
  passes the bytes on unchanged.
- **The `internal/remotecontrol/` edits touch the other agent's
  files.** They are additive, one hunk each, and were staged by
  explicit path.

---

## 9. The files

### The evidence

| file | what |
|---|---|
| `internal/rcfrontend/assets/main.js` | the reference's own frontend bundle — `ZNa`, `aOa`, `Rk`, `ZA`, the upload's retry loop, and the embedded `codeium_common.proto` descriptor |
| `out/rc-media/extract_codeium_common.py` | the extractor: bundle → base64 → `FileDescriptorProto` → the two messages |
| `out/rc-media/codeium_common_media.json` | the parsed `Media` and `ClientModelConfig` |
| `out/ide-language-server/protos/language_server.protoset` | the language server's service descriptor — the two media methods, their messages, and `SendUserCascadeMessageRequest.media` |
| `out/ide-language-server/live/reference/GetUserStatus.json` | the reference's own catalogue, with its `supportedMimeTypes` |
| `out/local-macos/gosrc/language_server_pb/language_server_go_grpc/api_generated.go` | the reference's own handler addresses |
| `internal/geminiwire/geminiwire.go` | the reference's own wire types — the `inlineData` spelling |

### The tests

| file | what |
|---|---|
| `internal/remotecontrol/media_test.go` | **new** — the catalogue's map, the two RPCs, the media → images conversion, the store's bounds, and the picture riding the turn |
| `internal/remotecontrol/media_wire_test.go` | **new** — the media RPCs on the wire: the binary-protobuf request the frontend sends, and the answer the schema takes |
| `internal/backend/media_test.go` | **new** — the four transports' image parts, and the text-only shapes they keep |
| `internal/piagentbridge/piagent_remote_test.go` | the picture reaching the model's request |
| `internal/piagentbridge/bridge_test.go` | PiG's image block → `Message.Images` |
| `internal/remotecontrol/modelenum_test.go` | the catalogue's map, beside the defaults it omits |
