# The operator's own models, offered to the reference — deliverable

**Cycle:** `cycle-332-reference-backend-custom-models`
**Subject:** `pigasus web --reference-backend=<language_server>` — the
operator's models from `~/.config/openagy/providers.json`, injected into
the reference the clone *starts*: the catalogue the reference reads, the
entries its model picker shows, and the traffic of a turn that picks one.
**Evidence:** 【一手】 the reference's own CloudCode traffic through a
loopback endpoint (`out/refbackend-custom-models/ccpa_traffic.txt`); 【一手】
the reference's own `GetAvailableModels` and `GetUserStatus` answers
(`…/available_models.json`, `…/user_status_models.json`); 【一手】 the
reference's own web app in a browser, its picker and a finished turn
(`…/picker_and_turn.txt`); 【一手】 the rig's own log
(`…/rig_log.txt`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the reference's model catalogue** | Google's own — 17 entries | Google's own **plus the operator's** — 19 entries |
| **the reference's model picker** | *Claude Sonnet 4.6*, *Claude Opus 4.6*, *GPT-OSS 120B*, *Gemini 4 Argon* | the same four **plus** *MiniMax M3*, *MiniMax M3.1 Flash* |
| **a turn on a local model** | impossible — the model was not in the list | the operator's own provider answers it (`minimax-m3 → MiniMax-M3 (stream, 1 msg, 12 tool)`) |
| **the CloudCode endpoint** | `https://daily-cloudcode-pa.googleapis.com` | the clone's own proxy (`http://127.0.0.1:<port>`), which forwards everything else unchanged |
| **no configured model** | — | no proxy at all, the reference talks to the cloud exactly as before |

---

## 1. The question

`pigasus agy` already offers the operator's own models to the vendor
binary: the clone's CloudCode proxy merges them into the catalogue the
binary reads and translates their traffic. The reference — the IDE's
`language_server`, started by `--reference-backend` — is a different
binary with a different frontend, and it read its catalogue from the
cloud, so the operator's models were nowhere to be seen.

The question this cycle answers: **can the same injection be made to
work for the reference?**

**Yes.** And the mechanism turned out to be the reference's own: it
promotes the catalogue it is given into the entries its picker reads, so
the merge alone puts the models into the picker. Nothing is patched, no
file in the operator's state is written, and the reference's own code
does the rest.

---

## 2. Where the injection point is

### 2.1 The reference honours `-cloud_code_endpoint`

【一手】 The reference was started with a loopback address instead of the
cloud:

    language_server … -cloud_code_endpoint=http://127.0.0.1:9410

and **all** of its CloudCode traffic arrived there — five endpoints, and
nothing went anywhere else:

    POST /v1internal:loadCodeAssist
    POST /v1internal:fetchUserInfo
    POST /v1internal:listExperiments
    POST /v1internal:fetchAvailableModels
    POST /v1internal:fetchAdminControls

A turn's inference arrives at the same place
(`POST /v1internal:streamGenerateContent?alt=sse`). The reference does
not refuse a plain `http://` endpoint and does not check the host.

### 2.2 The catalogue the endpoint returns *is* the reference's catalogue

【一手】 `GetAvailableModels` answered with exactly the map the endpoint
returned — the reference is not assembling a catalogue of its own. The
proxy merged two entries into the answer and the reference served 19
entries instead of 17.

The reference parses that answer as a proto (`FetchAvailableModelsResponse`,
whose `models` is a `map<string, ModelDetails>`), and re-serialises it.
Fields the proto does not have are **dropped** — the proxy's entry
carries `version`, `inputTokenLimit`, `outputTokenLimit`,
`supportedGenerationMethods`, `temperature`, `topP` and `topK`, and none
of them survives the round trip. The parse is lenient: an unknown field
does not fail the answer.

### 2.3 The reference promotes the catalogue into its picker

This is the part that makes the feature work with **no** interception of
the reference's RPCs. 【一手】 `GetUserStatus` answered with the
catalogue's entries promoted into
`cascadeModelConfigData.clientModelConfigs` — the very list the Web UI's
model picker reads:

    labels: ['MiniMax M3', 'MiniMax M3.1 Flash',
             'Claude Sonnet 4.6 (Thinking)', 'Claude Opus 4.6 (Thinking)',
             'GPT-OSS 120B (Medium)', 'Gemini 4 Argon']

    {"label": "MiniMax M3",
     "modelOrAlias": {"model": "MODEL_PLACEHOLDER_M488"},
     "modelId": "custom-minimax-m3",
     "description": "MiniMax M3 via openai (openagy wrapper)",
     "isRecommended": true, …}

The sort group ("Recommended") carries the new labels too, so the picker
renders them in place.

### 2.4 The placeholder is the reference's own slot

`MODEL_PLACEHOLDER_M488` is not a name the clone invented: the
reference's `Model` enum has 1,651 placeholders, `MODEL_PLACEHOLDER_M0` …
`M1650` (numbers 1000 … 2650), and it uses them for models its enum
cannot name — its own mock catalogue answers
`{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}` for one. The proxy's
placeholder is the same shape, so the reference accepts it, keeps it in
the picker, and sends it back on the wire — where the proxy recognises
it again and translates the request.

### 2.5 The paths that were *not* used, and why

The IDE's Web app has its own "custom model" machinery, and it was
measured before the catalogue route was chosen:

- the app's picker keeps a separate `customModels` list, hydrated from
  `appState.customModelsConfig.customModels` and rendered by a component
  that is only mounted when `showCustomModelHeader` is true — which is
  `isGoogleInternal`, i.e. an internal Google build;
- the feature is gated behind a `customModelsFeature` flag that the
  hub app's bootstrap does not define at all;
- the app's own "Add custom model" dialog writes
  `modelId: GOOGLE_GEMINI_INTERNAL_BYOM` with a model URL of the form
  `gemini-api:/models/<name>` — a *Gemini API* model, not an arbitrary
  OpenAI-shaped endpoint.

So the IDE's native path can carry a model the Gemini API serves; it
cannot carry the operator's own provider. The catalogue route can, and it
is the route the `agy` path already uses.

---

## 3. What was built

`internal/refbackend/custommodels.go` (new) and
`internal/refbackend/spawn.go`:

- the spawn asks the operator's registry for its models
  (`models.CachedRegistry().List()`);
- **no model, no proxy** — with an empty registry the reference is
  started with the real endpoint, exactly as before;
- with models, the clone's own CloudCode proxy (`internal/agyproxy`, the
  one `pigasus agy` uses) is started on a loopback port and the
  reference's `-cloud_code_endpoint` points at it;
- the proxy's upstream is the same cloud host the reference would have
  used, so every other request is forwarded unchanged;
- the proxy dies with the reference (`Process.Stop`).

Two deliberate choices:

- **The capture stays off** unless the operator asks for it with the
  proxy's own `OPENAGY_CAPTURE_DIR`. The reference polls the catalogue,
  so an always-on capture is a directory full of noise.
- **The operator's own endpoint wins.** An explicit
  `-cloud_code_endpoint` in `OPENAGY_REFERENCE_BACKEND_ARGS` suppresses
  the proxy entirely — the reference takes the last occurrence of a flag,
  and a proxy nothing talks to is just a listener.

---

## 4. The end-to-end measurement

【一手】 The rig, with the reference started by the clone:

    2026/10/26 23:52:08 refbackend: listening on http://127.0.0.1:59758 (upstream https://daily-cloudcode-pa.googleapis.com, 2 custom model(s), capture off)
    2026/10/26 23:52:08 refbackend: 2 custom model(s) offered to the reference: [minimax-m3 minimax-m3.1-flash]

The reference's own Web UI (served by the rig from the reference's own
assets) then showed the operator's models in its own picker:

    Gemini 3.8 FlashHigh
    Claude Sonnet 4.6 (Thinking)Notice
    Claude Opus 4.6 (Thinking)Notice
    GPT-OSS 120B (Medium)Notice
    Gemini 4 ArgonUnavailable
    MiniMax M3
    MiniMax M3.1 Flash

With *MiniMax M3* selected and the prompt "Reply with exactly one word:
ok", the reference created its conversation and the answer arrived:

    2026/10/26 23:53:38 refbackend: minimax-m3 → MiniMax-M3 (stream, 1 msg, 12 tool)
    2026/10/26 23:53:40 refbackend: POST /v1internal:streamGenerateContent?alt=sse -> 200 (2.447s)

and the conversation reads:

    Reply with exactly one word: ok
    ok

The operator's own provider answered the reference's turn, with the
reference's twelve tool declarations in the request.

---

## 5. Hard constraints

### 硬约束

1. **The injection point is the CloudCode endpoint.** The reference
   takes its catalogue from `fetchAvailableModels` and answers
   `GetAvailableModels` with that catalogue verbatim. Anything else
   (the app state, `clientModelConfigs`) is the reference's own
   derivation of it.
2. **The reference promotes the catalogue into its picker.** The merge
   alone puts the models into `clientModelConfigs` and into the sort
   group — no interception of `GetUserStatus` is needed, and none is
   done.
3. **The placeholder must be a name the reference's enum has.**
   `MODEL_PLACEHOLDER_M0` … `M1650` are the reference's own slots; the
   proxy's hash lands inside that range (`M400` … `M599`), and the
   reference re-sends it unchanged, which is how the proxy recognises
   the model on the way back.
4. **Unknown fields in the catalogue entry are harmless.** The reference
   parses the answer leniently and drops what its proto does not have.
5. **A non-zero exit is not a failure here either**, but the proxy
   *is* a failure point: if it cannot start, the spawn fails rather than
   silently handing the operator a reference that only knows Google's
   models.
6. **No proxy without models.** An empty registry means the reference
   talks to the cloud directly, exactly as it did before this cycle.
7. **The operator's own endpoint wins.** `-cloud_code_endpoint` in
   `OPENAGY_REFERENCE_BACKEND_ARGS` replaces the proxy's address (the
   reference takes the last occurrence of a flag) and suppresses the
   proxy.

## 存疑

- **The IDE's native custom-model path was measured from the bundle, not
  exercised.** The code that renders `appState.customModelsConfig` is
  gated behind `isGoogleInternal` and a `customModelsFeature` flag the
  hub app's bootstrap does not define, and its dialog only accepts a
  `gemini-api:` model URL. What is *measured* is the gate and the
  dialog's fields; that the native path could not have carried an
  OpenAI-shaped endpoint is an inference from them.
- **The reference's catalogue promotion was measured on one build.** The
  reference under `/Applications/Antigravity.app` turned the merged map
  into `clientModelConfigs` entries. Another build might serve the
  catalogue to the picker by another route.
- **The turn's request was translated by the proxy's own matcher.** The
  reference sent the placeholder back and the proxy mapped it to the
  operator's model id. Whether a *different* reference build would
  resolve the placeholder to a real model before sending it was not
  tested — the IDE's enum has 1,651 slots and the reference might
  resolve them.
- **The provider's own answer was not checked against the provider's
  log.** The rig's line `minimax-m3 → MiniMax-M3 (stream, 1 msg, 12
  tool)` and the answer `ok` are the evidence that the operator's
  endpoint served the turn.
- **The verification turn wrote into the operator's real history.** The
  reference created a conversation ("Single Word Reply Test") in the
  operator's own CLI Project, and it is still there — the reference owns
  that directory, and deleting it by hand was judged riskier than
  leaving it.

## Limitations

- The measurements are from **one** machine, one IDE build (the binary
  under `/Applications/Antigravity.app`, 2.26.0) and one date.
- **The operator's registry is the operator's.** The measurement used
  one OpenAI-shaped provider with two models; another dialect (Anthropic)
  goes through the same proxy and the same merge, but was not measured
  here.
- **The reference's traffic now passes through a listener.** Every
  CloudCode request the reference makes goes through the clone's proxy
  first. The proxy forwards the upstream answer unchanged apart from the
  catalogue merge, but it is an extra hop with the operator's session
  token in it.
- **Only the catalogue route was implemented.** The reference's own
  app-state custom models remain unused; the clone writes nothing into
  the operator's state directory.
- The proxy's **capture** stays off by default, so the reference's
  CloudPA traffic is not recorded unless the operator asks for it with
  `OPENAGY_CAPTURE_DIR`.

## Files

| | |
|---|---|
| `internal/refbackend/custommodels.go` | the proxy the reference is pointed at, the operator's models, the caller's override |
| `internal/refbackend/custommodels_test.go` | the endpoint shape, the caller's override, the operator's models |
| `internal/refbackend/spawn.go` | the spawn recipe: the endpoint is now the proxy's address |
| `internal/refbackend/spawn_test.go` | the flags pinned, the endpoint pinned |
| `out/refbackend-custom-models/ccpa_traffic.txt` | the reference's CloudCode traffic through the loopback endpoint |
| `out/refbackend-custom-models/available_models.json` | the reference's own `GetAvailableModels` answer with the merged entries |
| `out/refbackend-custom-models/user_status_models.json` | the reference's own `GetUserStatus` answer, the promoted entries |
| `out/refbackend-custom-models/picker_and_turn.txt` | the picker in the browser, the turn, the answer |
| `out/refbackend-custom-models/rig_log.txt` | the rig's log: the proxy's startup and the translated turn |
