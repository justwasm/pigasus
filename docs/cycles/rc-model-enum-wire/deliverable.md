# Cycle `rc-model-enum-wire` — the reference speaks the Model enum by **name**, and the clone now does too

**Status**: done
**Reference**: Antigravity IDE's `language_server`, LS version `1.11.0`,
sha256 `6f7e5004629070ac0c4651a1dc29030eaadcd5dc68fa9edf170047d7f8424f4d`
**Scope**: the Web UI's model picker, the Settings panel, and the whole
`Model` / app-state wire format — aligned against the reference binary
running live, not against a reading of it.

## TL;DR

1. The reference binary **runs standalone and answers offline**:
   `-use_mocked_data=true` gives it a catalogue without `loadCodeAssist`.
   Connect-RPC over plain HTTP POST on `-http_server_port` returns its
   **own** proto-JSON. That turns "what does the reference put on the
   wire" from a guess into a first-hand capture
   (`out/ide-language-server/live/`).
2. **Enums go on the wire by name.** The reference's own `GetUserStatus`
   answers `{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}` — not
   `1008`. That is Go's `protojson` default (`UseEnumNumbers: false`).
3. The `MODEL_PLACEHOLDER_M<n>` slots (`1000+n`, `n` = 0 … 1650) are the
   reference's **own mechanism for a model it cannot name**. The clone's
   catalogue (the user's configured models) is exactly that case, so every
   entry now takes a placeholder slot and is served as the enum's name.
4. The oneof's JSON form is the **member field** (`{"model": …}`), never a
   `{case, value}` envelope — the Web UI's protobuf-es decoder matches
   JSON keys against field names and drops the rest.
5. `JetboxReadAppStateResponse` and `JetboxReadUserConfigResponse` have
   **no fields at all**; the state arrives on the `JetboxSubscribeToState`
   stream. The clone now answers `{}` on the two reads.
6. The clone's state store keeps the **number** internally and marshals
   the **name** back out — the reference's own shape (its state is a proto
   message).

Verified in Chrome against the running clone: the picker's write is
`{"appState":{"lastSelectedAgentModel":"MODEL_PLACEHOLDER_M1"}}`, the
cascade's is `{"requestedModel":"MODEL_PLACEHOLDER_M1"}`, and the picked
model (MiniMax M3.1 Flash) is the one that answers.

## 1. The reference, live 【一手】

```
/Applications/Antigravity.app/Contents/Resources/bin/language_server \
    -http_server_port=8791 -gemini_dir=<tmp> \
    -standalone=true -use_mocked_data=true -persistent_mode=true
```

```
I1026 02:17:04  server.go:1766] Language server version: 1.11.0
I1026 02:17:04  server.go:687] Language server listening on random port at 49265 for HTTPS (gRPC)
I1026 02:17:04  server.go:695] Language server listening on random port at 8791 for HTTP
I1026 02:17:04  server.go:2965] Wrote discovery file: <gemini_dir>/antigravity-ide/daemon/ls_<hash>.json
```

`-use_mocked_data=true` is the load-bearing flag: the real catalogue needs
`loadCodeAssist` (network + auth), so without it the server has nothing to
serve. With it, the HTTP port speaks plain Connect-RPC:

```bash
curl -s -X POST http://127.0.0.1:8791/exa.language_server_pb.LanguageServerService/GetUserStatus \
     -H 'Content-Type: application/json' -d '{}'
```

Seven read RPCs were captured verbatim into
`out/ide-language-server/live/reference/`. `ref_ls_query.py` (next to them)
is the query tool.

The catalogue the reference answers with:

```json
{"userStatus":{"pro":true,"name":"Mock User","email":"mock-user@google.com",
 "cascadeModelConfigData":{
  "clientModelConfigs":[
   {"label":"gemini-3.1-pro","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"},
    "provider":"MODEL_PROVIDER_GOOGLE",
    "supportedMimeTypes":{"image/jpeg":true,"image/png":true}},
   {"label":"gemini-3.1-ultra","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M80"},
    "disabled":true,"provider":"MODEL_PROVIDER_GOOGLE",
    "tagTitle":"Unavailable","tagDescription":"Temporarily disabled due to capacity constraints"},
   {"label":"claude-4.5-opus","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M81"},
    "disabled":true,"provider":"MODEL_PROVIDER_ANTHROPIC",
    "quotaInfo":{"resetTime":"2026-10-25T22:33:00.530076Z"}}],
  "clientModelSorts":[{"name":"Recommended","groups":[{"modelLabels":[
    "gemini-3.1-pro","gemini-3.1-ultra","claude-4.5-opus"]}]}],
  "defaultOverrideModelConfig":{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}}},
 "planInfo":{"planName":"Mock Plan"}}
```

Four things to read off it:

| observation | consequence |
|---|---|
| `"model":"MODEL_PLACEHOLDER_M8"` — a **name**, not `1008` | every enum the clone emits must be a name |
| `MODEL_PLACEHOLDER_M<n>` for models the enum cannot name | the clone's placeholder slots are the reference's own mechanism, not an invention |
| `"provider":"MODEL_PROVIDER_GOOGLE"` | provider is an enum too, and is spelled by name |
| `defaultOverrideModelConfig` is **present** | serving it would let the frontend override the user's pick (§4) |

The two state reads answer, in this standalone mode:

```
JetboxReadAppState   → {"code":"unknown","message":"jetbox state store not initialized (error ID: …)"}
JetboxReadUserConfig → {"code":"unknown","message":"jetbox state store not initialized (error ID: …)"}
GetUserSettings      → {"code":"unimplemented","message":"unimplemented: unimplemented (error ID: …)"}
GetCascadeModelConfigs → {}
```

## 2. The `Model` enum, from the bytes the UI parses 【一手】

The IDE's own Web UI bundle embeds the `FileDescriptorProto`s it decodes
with (`internal/rcfrontend/assets/main.js`). Extracting them yields the
authoritative schema:

* `exa.codeium_common_pb.Model` — **1691 values**: 40 real ones
  (`MODEL_UNSPECIFIED` = 0, `MODEL_GOOGLE_GEMINI_2_5_PRO` = 246,
  `MODEL_CLAUDE_4_5_SONNET` = 333,
  `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` = 342,
  `MODEL_GOOGLE_GEMINI_INTERNAL_BYOM` = 326, …) and **1651 placeholders**,
  `MODEL_PLACEHOLDER_M0` … `MODEL_PLACEHOLDER_M1650`, numbered **1000 …
  2650**.
* `exa.codeium_common_pb.ModelProvider` — `MODEL_PROVIDER_UNSPECIFIED` 0,
  `ANTIGRAVITY` 1, `OPENAI` 2, `ANTHROPIC` 3, `GOOGLE` 4, `XAI` 5,
  `DEEPSEEK` 6, `GLM` 7, `KIMI` 8.
* `exa.codeium_common_pb.ModelOrAlias` — a oneof:
  `choice { Model model = 1; ModelAlias alias = 2; }`.
* `exa.codeium_common_pb.ClientModelConfig` — `label`, `model_or_alias`,
  `disabled`, `supported_mime_types` (map<string,bool>), `provider`,
  `tag_title`, `tag_description`, `model_url`, `model_id`, `quota_info`, …
* `CascadeModelConfigData` — `client_model_configs`,
  `client_model_sorts`, `default_override_model_config`, …
* `ClientModelSort{ name, groups[ ClientModelGroup{ group_name,
  model_labels } ] }`.
* `jetbox_state_pb.JetboxAppState` — `last_selected_agent_model` is field
  **10**, type `Model`.
* `jetbox_state_pb.State` — `last_selected_agent_model` is field **4**.
* `exa.config_pb.UserConfig{ sidecars, user_settings, plugins,
  conversation_groups, skills }`.
* `jetbox_state_pb.UserSettings` — ~50 members, **most of them in oneofs**
  (`auto_execution_policy`, `artifact_review_mode`, `planning_mode`,
  `theme_mode` are **not**).
* `language_server_pb.StartCascadeRequest.requested_model` — field **14**,
  type `Model` (a plain enum, not a `ModelOrAlias`).
* `JetboxReadAppStateResponse` and `JetboxReadUserConfigResponse` — **no
  fields**.
* `JetboxWriteStateRequest{ state, app_state, user_config }`,
  `JetboxSubscribeToStateResponse{ state, app_state, user_config }`.

The extractor lives in `out/ide-language-server/extract_ls_descriptor.py`.

## 3. The frontend's codec, read out of the bundle 【一手】

The bundle's protobuf-es v2 codec is two small functions, and they settle
every remaining question:

```js
// decode: Ie(enumSchema, jsonValue, ignoreUnknownFields, …)
case "number": if (Number.isInteger(b)) return b; break;
case "string": e = a.values.find(f => f.name === b);
                if (e !== void 0) return e.number;
                if (c) return Je;                       // unknown name → sentinel
throw Error(`cannot decode ${a} from JSON: ${cc(b)}`);

// encode: Re(enumSchema, number, useEnumNumber)
return a.typeName == "google.protobuf.NullValue" ? null : c ? b : a.value[b]?.name ?? b;
```

* **decode** takes a number *or* a name; an unknown **name** yields the
  sentinel `Je` (a `Symbol`) — so a name the UI's enum does not know is
  worse than a number it does not know.
* **encode** emits the **name** whenever the enum knows the number, and
  the raw number only when it does not.

So the round trip is: the server sends a name → the UI decodes it to the
number → the UI re-encodes it as the same name. A server that sends a
number the enum does not know gets that number back, unadorned.

The picker's own mapping confirms the oneof form:

```js
var Rk = a => a.modelOrAlias ? {
  label: a.label,
  value: a.modelOrAlias.choice.case === "model" ? a.modelOrAlias.choice.value : I.Model.UNSPECIFIED,
  modelAlias: a.modelOrAlias.choice.case === "alias" ? a.modelOrAlias.choice.value : void 0,
  … } : null;
```

`choice.case === "model"` — the **member field** must be the JSON key.
`{"choice":{"case":"model","value":1000}}` is dropped wholesale: the
decoder matches JSON keys against field names, `choice` is not one, and
the response is decoded with `ignoreUnknownFields`.

`Sk(value, configs)` matches the selection by that number, and
`Ls({visibleModelConfigs, customModels, modelSorts}, {model, customLabel})`
resolves the current model: `Sk(model, visibleModelConfigs)` for a normal
model, the BYOM branch (`MODEL_GOOGLE_GEMINI_INTERNAL_BYOM` = 326) for a
custom one. The selection itself comes from the app state's
`lastSelectedAgentModel` — hydrated by the `modelSelection/startPersistence`
middleware, which reads it off the **subscribe stream**.

Effort grouping (why three catalogue entries render as one row) is the
label regex, with an empty static table:

```js
var X$b = ["low","medium","high","xhigh","max"];
const Y$b = new Map;                                   // empty in this build
const Z$b = /^(.*?)\s*\((low|medium|high|xhigh|max)(?:\s+thinking)?\)\s*$/i;
```

So `Gemini 3.8 Flash (High|Medium|Low)` collapse into one row with an
effort submenu, while `Claude Sonnet 4.6 (Thinking)` stays standalone —
the regex does not match `thinking`. The clone's labels already behave
that way, unchanged by this cycle.

## 4. What changed in the clone

| file | change |
|---|---|
| `internal/remotecontrol/modelenum.go` | **new** — the `Model` enum table (40 real values), the placeholder range (`M0 … M1650` → 1000 … 2650), and `modelWireJSON` (protojson's rule: the name when known, the number when not) |
| `internal/remotecontrol/modelvalue.go` | the placeholder **slot** replaces the id-derived synthetic number; `modelValueFromJSON` accepts a number, a numeric string or an enum name |
| `internal/remotecontrol/dispatch_userstatus.go` | the catalogue serves `"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M<n>"}` |
| `internal/remotecontrol/state.go` | the app state keeps the **number** and marshals the **name** back out; the write path accepts the name |
| `internal/remotecontrol/dispatch_ls.go` | `modelValue.UnmarshalJSON` resolves an enum **name** |
| `internal/remotecontrol/dispatch_languageserver.go` | `JetboxReadAppState` / `JetboxReadUserConfig` answer `{}`; the subscribe stream keeps the snapshot |

`defaultOverrideModelConfig` is **deliberately not served**. The
frontend's `QCa`/`RCa` middleware turns it into a `defaultOverrideApplied`
action that overwrites `modelSelection.default` *and* persists it — the
user's pick would be replaced by the server's default on every catalogue
refresh. The reference's mock does serve one; the clone must not, or the
picker is not the user's any more.

### proto-JSON omits the defaults

The reference's catalogue is thinner than the clone's was: it carries
neither `disabled: false` nor an empty `supportedMimeTypes` map, and its
sort groups carry `modelLabels` and nothing else. That is `protojson`'s
rule — a field holding its default is not emitted. The clone now follows
it, and names the vendor in `provider`:

| | reference | clone (now) |
|---|---|---|
| `disabled` | only on the entries it disables | omitted (the clone disables none) |
| `supportedMimeTypes` | only when non-empty | omitted (the clone's models are text-only) |
| `provider` | `MODEL_PROVIDER_GOOGLE` / `MODEL_PROVIDER_ANTHROPIC` — the vendor | the same, plus `MODEL_PROVIDER_OPENAI` for a GPT-OSS model and `MODEL_PROVIDER_ANTIGRAVITY` for the user's own |
| `clientModelSorts[].groups[].groupName` | absent | absent |

The vendor rule is the reference's own: its catalogue maps
`gemini-3.1-pro` to `MODEL_PROVIDER_GOOGLE` and `claude-4.5-opus` to
`MODEL_PROVIDER_ANTHROPIC`, and the `Model` enum agrees — it calls a
GPT-OSS model `MODEL_OPENAI_GPT_OSS_120B_MEDIUM`. A model of the user's
own belongs to no vendor in the enum, so the product's own provider is
the honest answer. The frontend does not read `provider` at all (no
`.provider` access on a `ClientModelConfig` in the bundle), so this is
shape, not behaviour.

### The `UserStatus` shape

`exa.codeium_common_pb.UserStatus` (from the IDE bundle's descriptor)
has 21 fields; the clone carries five of them, and every key it sends
is a real field:

| field | number | clone | reference (mock) | read by the frontend |
|---|---|---|---|---|
| `pro` | 1 | omitted (`false` → proto-JSON omits it) | `true` | **no** (no `.pro` access in the bundle) |
| `disable_telemetry` | 2 | `true` | absent | **yes** — `telemetryEnabled: !c.disableTelemetry` |
| `name` | 3 | the local user | `"Mock User"` | account chip |
| `email` | 7 | the signed-in account | `"mock-user@google.com"` | account chip |
| `user_tier` | 36 | `google.internal.cloud.code.v1internal.UserTier` | absent | **yes** — `userStatus.userTier?.availableCredits` |
| `cascade_model_config_data` | 33 | the catalogue | the catalogue | the picker |

`plan_info` (field 12 of `UserStatus`, and the response's own
`plan_info`) is **not read** by the frontend — zero occurrences of
`planInfo` in the bundle — so the clone leaves it unset, as it already
did.

`disable_telemetry: true` is load-bearing, not decoration: it is what
makes the Settings panel's telemetry switch render **off**, because the
frontend computes `telemetryEnabled: !c.disableTelemetry`.

One measurement note, because it cost a wrong conclusion: `json.Marshal`
sorts a map's keys, so `cascadeModelConfigData` sorts **before**
`disableTelemetry`, `email`, `name` and `userTier`. A truncated look at
the response made those four look missing; they were there all along.

## 5. The round trip, in the browser 【一手】

Chrome, against the running clone (`http://127.0.0.1:8899`), with
`WebSocket.prototype.send` patched to record the frames:

**Pick "MiniMax M3.1 Flash"** — the write carries the **name**:

```json
{"streamId":"e5165b90-…","type":"start",
 "procedure":"/exa.language_server_pb.LanguageServerService/JetboxWriteState",
 "stream":false,
 "headers":{"Content-Type":["application/json"],"x-codeium-csrf-token":["pigasus-rc-csrf-token"]},
 "payload":{"appState":{"lastSelectedAgentModel":"MODEL_PLACEHOLDER_M1"}}}
```

**Send a message** — the cascade carries the **name** too:

```json
{"procedure":"/exa.language_server_pb.LanguageServerService/StartCascade",
 "payload":{"source":"CORTEX_TRAJECTORY_SOURCE_CASCADE_CLIENT",
            "cascadeId":"616d2e93-…",
            "requestedModel":"MODEL_PLACEHOLDER_M1",
            "projectEnvConfig":{"projectId":"outside-of-project","defaultProjectEnvironment":{}}}}
```

**The server's own state**, after the pick:

```json
{"appState":{"agentOnboardingCompleted":2,"lastSelectedAgentModel":"MODEL_PLACEHOLDER_M1",
              "postOnboarding":{"completedSteps":[]},"seenNuxs":{"uids":[]}},
 "userConfig":{"userSettings":{"enableNotificationsForSpecialEvents":true,
                                "enableSoundsForSpecialEvents":false}}}
```

**Reload** → the button reads `Select model, current: MiniMax M3.1 Flash`
(the selection survives), and the turn answers `pong` **from MiniMax M3.1
Flash** — the picked model is the one that runs.

**Settings** → *Execution → Queued Messages → Send Immediately* lands in
the store as the enum's **name**:

```json
"queuedMessageDeliveryStrategy": "MESSAGE_DELIVERY_STRATEGY_NEXT_INVOCATION"
```

and flipping it back writes `"MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE"`. After
a reload the panel shows *Queue* selected — the setting persisted.

The frames are captured in
`out/ide-language-server/live/clone/browser_frames.json`.

## 6. Reproduction

```bash
# 1. the reference, live
/Applications/Antigravity.app/Contents/Resources/bin/language_server \
    -http_server_port=8791 -gemini_dir=$(mktemp -d) \
    -standalone=true -use_mocked_data=true -persistent_mode=true &
curl -s -X POST http://127.0.0.1:8791/exa.language_server_pb.LanguageServerService/GetUserStatus \
     -H 'Content-Type: application/json' -d '{}' | python3 -m json.tool

# 2. the same against the clone
go build -o /tmp/pigasus ./cmd/pigasus
/tmp/pigasus web --port 8899 --host 127.0.0.1 &
curl -s -X POST http://127.0.0.1:8899/exa.language_server_pb.LanguageServerService/GetUserStatus \
     -H 'Content-Type: application/json' -d '{}' | python3 -m json.tool

# 3. the regressions
go test ./internal/remotecontrol/ -run 'TestClientModelConfigs|TestModelEnum|TestStateStore|TestStartCascadeResolves|TestJetboxRead' -v
```

## 硬约束

* The `Model` enum on the wire is **the name**. A number is only correct
  for a value the enum does not know — and the frontend's decoder turns an
  unknown **name** into a `Symbol` sentinel, which is worse than an
  unknown number.
* The `ModelOrAlias` JSON form is the **member field** (`{"model": …}`).
  The `{case, value}` envelope is silently dropped by the decoder, and
  every entry then reads as UNSPECIFIED — the picker snaps back to the
  first model and clicking does nothing.
* `JetboxReadAppStateResponse` / `JetboxReadUserConfigResponse` have **no
  fields**. The state arrives on `JetboxSubscribeToState`.
* `defaultOverrideModelConfig` must not be served by the clone.
* The placeholder range is `M0 … M1650` (1000 … 2650). A slot outside it
  is not a name the frontend's enum knows.

## 存疑

* The reference's mock catalogue assigns `M8` / `M80` / `M81` — **not**
  catalogue positions. The slots come from a registry the mock does not
  expose. The clone assigns them by catalogue order instead (stable for
  one catalogue, which is what the round trip needs); the reference's own
  assignment rule is unknown.
* The reference's websocket endpoint (`/connect-websocket`) completes the
  upgrade and then closes the connection on the first frame from a raw
  Python client. The frontend's frames (captured from the clone, whose
  server is a re-implementation) use a JSON **object** payload; the
  reference may want a JSON **string**. Unresolved — the HTTP POST path
  was used for the reference's captures instead.
* The settings write reached the clone over the **HTTP** Connect-RPC
  path, not the websocket, and was therefore not caught by the
  `WebSocket.prototype.send` patch. The clone serves both, so this is a
  measurement artefact, not a defect — but it means the *reference's*
  transport split between the picker and the settings panel is unverified.

## limitations

* The reference's **real** (non-mocked) catalogue was never observed:
  it needs `loadCodeAssist`, i.e. network plus auth. So the reference's
  real `Model` enum names for Gemini 3.1 / Claude 4.5 are not first-hand
  here — the placeholders are the reference's own answer for a model it
  cannot name.
* The reference's `JetboxRead*` cannot be observed *answering* in this
  environment: standalone, it has no state store and errors. The `{}`
  the clone returns is aligned to the **descriptor** (zero fields), not
  to the reference's runtime behaviour.
* The write path (`JetboxWriteState`) was **not** captured from the
  reference — the same missing state store. The clone's write path is
  aligned to the frontend's frames and to the descriptor.
* `quotaInfo.resetTime` is the server's current time and changes on every
  call, so byte comparisons must exclude it.
