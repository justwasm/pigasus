# The model config and the picker — deliverable

**Cycle:** `cycle-333-model-config-alignment`
**Subject:** the Web UI maps every model config through **one** function,
and that function reads **11** fields. The clone filled **5** of them.
The per-model attribute system is now the reference's own vocabulary, and
the clone fills all 11.
**Evidence:** 【一手】 the frontend bundle the clone serves
(`internal/rcfrontend/assets/main.js`) — the mapper, the filters and the
checks; 【一手】 the reference's own live catalogue
(`out/ide-language-server/live/reference/GetUserStatus.json`); 【一手】 the
live `GetUserStatus` of the clone, against the operator's own
configuration and against a copy that states the new attributes.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the per-model attributes** | one marker (`supports_images`) | eleven keys, named after the reference's `ClientModelConfig` proto |
| **the fields the picker's mapper reads** | 5 of 11 (`label`, `modelOrAlias`, `provider`, `supportedMimeTypes`, `tagTitle`/`tagDescription` — the last two guessed from the model's id) | **11 of 11** — every one of them has a source in the configuration |
| **a model the operator took out of service** | impossible | `disabled: true` — the picker filters the entry out |
| **a model that is out of quota** | impossible | `quota_info` — the picker warns, with the reset time in protojson's spelling |
| **the tag line under the model's name** | guessed from the model's id | the configuration's own words, the guess is the fallback |
| **the media types a model accepts** | two (`image/jpeg`, `image/png`), from a marker | a **list** — a model can accept PDF, or audio, and the frontend's attachment check and microphone button follow it |

---

## 1. The gap, exactly

### 1.1 The mapper

【一手】 the Web UI's model picker maps every model config through one
function — the `Rk` mapper in the bundle the clone serves
(`internal/rcfrontend/assets/main.js`, verbatim in
`out/model-config-alignment/frontend-rk-mapper.js`):

```js
var Rk=a=>a.modelOrAlias?{label:a.label,
  value:a.modelOrAlias.choice.case==="model"?a.modelOrAlias.choice.value:I.Model.UNSPECIFIED,
  modelAlias:a.modelOrAlias.choice.case==="alias"?a.modelOrAlias.choice.value:void 0,
  disabled:a.disabled,
  supportedMimeTypes:new Map(Object.entries(a.supportedMimeTypes)),
  quotaInfo:a.quotaInfo,tagTitle:a.tagTitle,tagDescription:a.tagDescription,
  supportsThoughtCirculation:a.supportsThoughtCirculation,modelUrl:a.modelUrl,
  modelId:a.modelId||void 0}:null
```

It reads **ten** fields off the model config — `label`, `modelOrAlias`,
`disabled`, `supportedMimeTypes`, `quotaInfo`, `tagTitle`, `tagDescription`,
`supportsThoughtCirculation`, `modelUrl`, `modelId` — and the clone also
sends `provider`, which the picker reads in a hundred places of its own.
**Eleven.**

The clone filled five of them. The six it did not were `undefined` in the
picker — which is not the same thing as `false` and not the same thing as
an empty string.

### 1.2 The reference's own field set

【反编译】 `exa.codeium_common_pb.ClientModelConfig` has **21** fields.
The eleven the picker reads, with their field numbers:

| # | field | the picker reads it for |
|---|---|---|
| 1 | `label` | the name in the list |
| 2 | `model_or_alias` | the choice, and the value it sends back |
| 4 | `disabled` | `filter(b => !b.disabled)` — the entry leaves the list |
| 10 | `provider` | the vendor badge, and the routing |
| 15 | `quota_info` | the quota warning |
| 16 | `tag_title` | the badge under the name; `NEW` is the "new model" mark |
| 17 | `tag_description` | the badge's tooltip |
| 18 | `supported_mime_types` | the attachment check, and the microphone button |
| 19 | `supports_thought_circulation` | — (read, unused) |
| 20 | `model_url` | — (read, unused) |
| 21 | `model_id` | — (read, unused for ordinary models) |

【一手】 the reference's live catalogue
(`out/ide-language-server/live/reference/GetUserStatus.json`, three
entries) shows the three shapes this cycle has to reproduce:

```json
{"label":"gemini-3.1-pro","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"},
 "provider":"MODEL_PROVIDER_GOOGLE","supportedMimeTypes":{"image/jpeg":true,"image/png":true}}
{"label":"gemini-3.1-ultra","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M80"},"disabled":true,
 "provider":"MODEL_PROVIDER_GOOGLE","tagTitle":"Unavailable",
 "tagDescription":"Temporarily disabled due to capacity constraints"}
{"label":"claude-4.5-opus","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M81"},"disabled":true,
 "provider":"MODEL_PROVIDER_ANTHROPIC","quotaInfo":{"resetTime":"2026-10-25T22:33:00.530076Z"}}
```

An enabled model, a model disabled for capacity, and a model that is out
of quota.

---

## 2. The attributes

The names are the reference's own. The **file** spells them in snake_case
(the file's convention); the **wire** spells them in proto-JSON's
camelCase (the proto's convention). The conversion happens in one place —
the picker's config builder.

| config key | Go shape | wire field |
|---|---|---|
| `supported_mime_types` | `[]string` | `supportedMimeTypes` |
| `supports_images` | `*bool` | the shorthand of the above |
| `disabled` | `bool` | `disabled` |
| `quota_info` | `*QuotaInfo` | `quotaInfo` |
| `quota_info.remaining_fraction` | `*float64` | `quotaInfo.remainingFraction` |
| `quota_info.reset_time` | `string` | `quotaInfo.resetTime` |
| `supports_thought_circulation` | `*bool` | `supportsThoughtCirculation` |
| `model_url` | `string` | `modelUrl` |
| `tag_title` | `string` | `tagTitle` |
| `tag_description` | `string` | `tagDescription` |
| `effort_supported` | `*bool` | — (the clone's own list field) |

`model_id` is **derived**, not configured: it is the upstream model id (the
`model` key). The file already has `id` (the catalogue handle) and `model`
(the upstream name); a third id field would only confuse.

### 2.1 The decision order

| attribute | 1st | 2nd | 3rd |
|---|---|---|---|
| the media types | the configured list | `supports_images` | the family table |
| the tag line | the configured words | the id's guess (`flash` → "Fast") | — |
| the reasoning level | `effort_supported` | the clone's default (`true`) | — |
| disabled | the configuration only | — | — |

The family table (`internal/remotecontrol/model_mime_types.go`) stays the
last word only for a model that says nothing — exactly as before.

### 2.2 proto-JSON's rule on the wire

**A field that holds its default is not emitted.** `disabled: false` is
absent; an empty `supportedMimeTypes` is absent; an empty `modelUrl` is
absent. The reference's own catalogue answers that way (the enabled entry
carries no `disabled`), so the clone's bytes match the reference's shape.

`quotaInfo.resetTime` is spelled **protojson's** way —
`"2026-10-25T22:33:00.530076Z"`, 0, 3, 6 or 9 fractional digits — not the
configuration's own string. A string protojson cannot parse is passed
through unchanged: the operator's own words beat a wrong timestamp.

### 2.3 The chain

```
providers.json
  → models.ProviderModel.*                        (the attributes)
  → models.Model.*                                (ProviderConfig.Flatten)
  → remotecontrol.ModelInfo.*                     (rchost.hostListModels)
  → GetUserStatus's clientModelConfigs            (model_config_fields.go)
```

The builder lives in a file of its own
(`internal/remotecontrol/model_config_fields.go`) so the concern — *the
fields the picker reads* — is separable from the route table.

### 2.4 The flags

`agy model add <id> --provider <p>` gained:

```
--supported-mime-types          --disabled
--quota-reset-time              --quota-remaining-fraction
--supports-thought-circulation  --model-url
--tag-title                     --tag-description
--effort-supported
```

Each keeps the tri-state rule of the image marker: **a flag that is not
passed keeps the file's value**, and `--flag=false` states the opposite.
A broken timestamp and a share of the quota above one are rejected at the
flag, not at the picker's screen.

---

## 3. The evidence

### 3.1 The clone, against the operator's own configuration

【一手】 `out/model-config-alignment/userstatus-real-config.json` — the
live `GetUserStatus`:

```json
{"label":"MiniMax M3","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M0"},
 "provider":"MODEL_PROVIDER_ANTIGRAVITY",
 "supportedMimeTypes":{"image/jpeg":true,"image/png":true},
 "modelId":"MiniMax-M3"}
```

The reference's own catalogue entries are byte-unchanged.

### 3.2 The clone, with the attributes stated

【一手】 `out/model-config-alignment/userstatus-stated-attributes.json` —
the same catalogue, with the configuration stating the attributes:

```json
{"label":"MiniMax M3","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M0"},
 "provider":"MODEL_PROVIDER_ANTIGRAVITY",
 "quotaInfo":{"remainingFraction":0.25,"resetTime":"2026-10-27T00:00:00Z"},
 "tagTitle":"NEW","tagDescription":"The newest MiniMax generation",
 "supportedMimeTypes":{"audio/webm":true,"image/jpeg":true,"image/png":true},
 "supportsThoughtCirculation":true,
 "modelUrl":"https://platform.minimax.io/docs/minimax-m3",
 "modelId":"MiniMax-M3"}
```

and the other model carrying `disabled: true`:

```json
{"label":"MiniMax M3.1 Flash","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M1"},
 "disabled":true,"provider":"MODEL_PROVIDER_ANTIGRAVITY",
 "tagTitle":"Fast","tagDescription":"Recommended for general coding and fast turns",
 "supportedMimeTypes":{"image/jpeg":true,"image/png":true},
 "modelId":"MiniMax-M3.1-Flash"}
```

The **order** is the schema's: `label` (1), `modelOrAlias` (2), `disabled`
(4), `provider` (10), `quotaInfo` (15), `tagTitle` (16), `tagDescription`
(17), `supportedMimeTypes` (18), `supportsThoughtCirculation` (19),
`modelUrl` (20), `modelId` (21). A Go map would have written them
alphabetically; `internal/lsproto`'s marshaller puts them back into the
descriptor's order.

### 3.3 What the picker does with them

【一手】 the frontend bundle:

- **the disabled entry leaves the list**:
  `ima=a=>a.map(Rk).filter(b=>b!==null).filter(b=>!b.disabled)`.
- **the quota**: `lN` reads `f.remainingFraction` and `f.resetTime`;
  `mN` reads `a?.quotaInfo?.remainingFraction===0` (the `LOW_CREDITS` code).
- **the tag line**: `l.tagTitle?.trim().toUpperCase()==="NEW"` is what the
  picker looks for when it marks a model as new; `tagTitle` and
  `tagDescription` together render as a badge and a tooltip.
- **the media types**: the attachment check is `b.get(c)===!0` against the
  map; the microphone button reads
  `for(ta of GNa) if(l.supportedMimeTypes.get(ta))`, and `GNa` is the
  frontend's audio list (`audio/wav … audio/webm;codecs=opus`) — so an
  `audio/webm` entry enables voice input.

### 3.4 The tests

| file | what it pins |
|---|---|
| `internal/remotecontrol/model_config_alignment_test.go` | every field the picker reads has a source; the clone emits nothing the picker cannot use; the defaults are absent; the quota's field names; the timestamp's spelling; the tag line; the mime map's values; the schema's byte order |
| `internal/models/edit_test.go` | the configuration round trip: the attributes survive a write, a model that says nothing carries no key, a cleared quota removes the object |
| `internal/cli/provider_cmds_test.go` | the flags' three states, and the rejection of a broken timestamp and a share above one |
| `internal/rchost/remote_control_models_test.go` | the attributes travel from the file to the catalogue row |

---

## 4. Hard constraints

1. **The configuration is flat, typed and snake_case; the wire is
   proto-JSON's camelCase.** The two are converted in the picker's config
   builder (`internal/remotecontrol/model_config_fields.go`). Do not mix
   the two spellings.
2. **"Not stated" and "stated false" are different answers.** So
   `supports_images`, `supports_thought_circulation` and `effort_supported`
   are pointers, and a model that says nothing carries no key at all.
3. **A default is not emitted.** `disabled: false` does not go on the
   wire — the reference's own catalogue does not send it either.
4. **The timestamp is spelled protojson's way**, not the configuration's
   own string.
5. **`model_id` is derived** — the upstream model id — and is not a
   configuration key.
6. **The list beats the marker beats the family table.** The user's word
   always wins.
7. **The mime decision function changed shape**: `modelSupportedMimeTypes`
   took a fourth (variadic) argument, the list of media types. The old
   three-argument call sites — in `media_test.go` and `modelenum_test.go`,
   which belong to the other agent's cycle — still compile and still mean
   what they meant.

## 5. Open questions

- 【推测】 The meaning of `supports_thought_circulation`. The frontend
  touches it twice, both inside the mapper, and uses it nowhere. "Thought
  circulation" reads like a reference-internal concept. Its **value**
  changes no frontend behaviour today; the clone sends it for the field
  set's sake.
- 【推测】 The meaning of `model_url` — the same: read twice, used
  nowhere.
- 【推测】 The meaning of `model_id`. The reference's live catalogue does
  **not** send it, and the frontend uses the field only for *custom*
  models (a different enum). The clone sends the upstream id, which reads
  sensibly, but no first-hand evidence says the reference does that.
- 【推测】 `effort_supported` does not exist in the reference at all — it
  is the clone's own list-surface field. This cycle only made it
  configurable; the default did not change.
- 【推测】 The family table still guesses from the **name**. This cycle
  neither widened nor narrowed it.

## 6. Limitations

- The reference's binary is not part of this evidence: what is measured is
  the **clone's** behaviour (configuration → catalogue → `GetUserStatus`).
  The reference side contributes the frontend's mapper and its checks
  【一手】 and the live catalogue's three shapes 【一手】.
- **Nothing was clicked in a browser.** "The disabled model leaves the
  picker" and "the quota warning appears" are read out of the frontend's
  code, not seen. The live capture verifies the JSON's shape only.
- The microphone claim (`audio/webm` in the list enables voice input) is
  read out of the frontend's `GNa` list and its check — no recording was
  made.
- The transport was not touched: the picture on the wire is the previous
  cycle's work (`out/web-image-support/`). This cycle is about the **model
  config** side only.
- The operator's own configuration was **not** changed: it still carries
  `supports_images: true` on the two MiniMax models and nothing else.
  The capture that states the attributes ran against a **copy**.

## 7. The files

### The evidence

| file | what |
|---|---|
| `out/model-config-alignment/README.md` | the zh-CN walk-through |
| `out/model-config-alignment/frontend-rk-mapper.js` | 【一手】 the frontend's mapper, verbatim |
| `out/model-config-alignment/userstatus-real-config.json` | 【一手】 the live `GetUserStatus` against the operator's configuration |
| `out/model-config-alignment/userstatus-stated-attributes.json` | 【一手】 the live `GetUserStatus` with the attributes stated |

### The code

| file | what |
|---|---|
| `internal/models/providers.go` | the configuration's attributes, and `QuotaInfo` |
| `internal/models/registry.go` | the registry's carriers |
| `internal/models/edit.go` | the writers: `setOrDeleteStringSlice`, `setOrDeleteQuotaInfo`, `boolPointer` |
| `internal/cli/model_actions.go` | the flags |
| `internal/cli/provider_cmds.go` | the flags' usage text |
| `internal/remotecontrol/lists.go` | the catalogue row's attributes |
| `internal/remotecontrol/model_config_fields.go` | **new** — the fields the picker reads |
| `internal/remotecontrol/model_mime_types.go` | the mime decision: list › marker › family table |
| `internal/remotecontrol/dispatch_userstatus.go` | the picker's model configs |
| `internal/rchost/remote_control_backends.go` | the catalogue's builder |
