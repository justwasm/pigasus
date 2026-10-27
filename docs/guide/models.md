# Model routing and the provider configuration

> Which transport a model gets, the shape of `providers.json`, and the one writer that edits it.

## Model routing (`internal/cliapp/run.go`, cycle `provider-config-gateway`)

There are **two real transports**, and the selected model decides which one runs:

| The model is… | Transport | Endpoint |
|---|---|---|
| in the reference's catalogue (Gemini / Claude / GPT-OSS) | `backend.CloudCodeClient` | `https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse` |
| configured by the user | `backend.GatewayClient` | the configured endpoint: `<base>/chat/completions` (OpenAI) or `<base>/v1/messages` (Anthropic) |

`pickBackendWithLog` (`internal/cliapp/run.go`) does the split at startup; the TUI
does it again on every `/model` change through `runMode.backendFor`
(`syncBackendForModel`). An explicit `--backend gateway` /
`OPENAGY_BACKEND=gateway` still forces the configured endpoint. `--backend cloudcode`
(`google`, `gemini`, `cc`) forces the official path. A model nobody configured
lands on the shim **with a warning that names `providers.json`** — answering
from the shim without saying so would look like a working model.

**The catalogue is configuration.** `~/.config/openagy/providers.json` holds one
entry per provider: the endpoint, the credential (`$NAME` is expanded from the
environment), the wire protocol (`openai` | `anthropic`) and the models that
provider serves. That file is the **only** *provider* configuration shape — the
legacy flat `~/.config/openagy/models.json` has been retired (read path, write
path and the `agw` provider-type alias with it; report:
`docs/cycles/legacy-config-retirement/`). `cli.ReferenceModels()` is
the *pure* reference catalogue; `cli.ModelCatalogue()` merges the configured
models in (reference rows first, configured rows sorted by id). The transport
has **no** built-in endpoint, credential or model — an unset endpoint is an
error, not a fallback. Report: `docs/cycles/provider-config-gateway/`.

**The services the clone itself talks to are a *different* file.**
`~/.config/openagy/config.json` carries the named service endpoints — the
CloudCode host, the OAuth2 token endpoint, the companion origin, the browser
Origin allowlist and the rest. `providers.json` answers *"which model talks to
which provider"*; `config.json` answers *"which service does the clone itself
talk to"*. See [`endpoints.md`](endpoints.md).

The CloudCode wire was transcribed from live 1.4.3 traffic (report:
`docs/cycles/cloudcode-model-switching/`). Five things are load-bearing:

- **`daily-` matters.** `cloudcode-pa.googleapis.com` answers 429 for this
  account; `daily-cloudcode-pa` answers 200. The proxy's upstream
  is the prod host, which is why it looked quota-exhausted.
- **`sessionId` is a JSON string**, not a number
  (`400 Invalid value at 'request.session_id' (TYPE_STRING)`).
- **Tool results are model-role contents** with
  `functionResponse{id,name,response:{output}}`, and the model's
  `thoughtSignature` is replayed on the `functionCall` part. Signatures are kept
  in memory per client.
- **Function declarations must be unique.** The clone advertises
  `invoke_subagent` twice (its catalogue + PiG); Gemini rejects the whole
  request. The transport keeps the first per name.
- **macOS token = keychain, not the files.** The reference stores the live token
  in the login keychain (`gemini`/`antigravity`); the on-disk cascade can be a
  stale fixture. The client prefers the keychain and refreshes there.
- **Reasoning is wrapped, not dropped.** CloudCode sends reasoning as
  `thought:true` text parts (Claude does; Gemini Flash's measured turns return
  only signature-only parts). The transport wraps each run in the reference's
  `<think>…</think>` markers, which is what `internal/agent.Sanitizer` already
  consumes for MiniMax — the TUI renders `▸ Thought for …` and ctrl+o expands
  the body, while print mode strips it.

The catalogue is the reference's own selectable set — the six ids
`agy models` prints (`gemini-3.8-flash-{high,medium,low}`, `claude-sonnet-4-6`,
`claude-opus-4-6-thinking`, `gpt-oss-120b-medium`). Configured models (MiniMax
M3 and friends) are **not** in it: they come from the user's configuration.
The server advertises **24** ids in
`fetchAvailableModels`; everything outside the selectable six is rejected by
the reference's own validator (`model X is not recognized…`), so accepting it
would diverge. The one exception is **`gemini-4-argon-disabled`** ("Gemini 4
Argon"): it is *known* and refused with a plan reason — `"Gemini 4 Argon" is
unavailable: Gemini 4 Argon is currently available to Ultra and paid Pro plan
subscribers` — which `internal/cli.ReferenceGatedModels` reproduces. Report:
`docs/cycles/model-catalogue-argon-gate/`.

---

## The per-model budget (`internal/modelctx`, cycle `303-cascade-model-profiles`)

Every model the clone builds a handle for carries a **window** and a
**reserve**. `internal/modelctx.For(modelID)` returns both, and it reads
**two** sources, in this order:

1. **The reference's own catalogue.** agy 1.4.8 embeds a
   `model_profiles.json` (57 984 bytes at file offset `0x9ab160a` of the
   Mach-O) that carries one `CASCADE_USE_EXPERIMENT_CHECKPOINTER` payload
   per model family, the alias map folding the model ids onto those
   families, and the per-profile variant payloads. It is committed at
   `internal/modelctx/testdata/model_profiles_1.4.8.json` and mirrored by
   `internal/modelctx/profiles_1.4.8.go`.
2. **The server's catalogue**, as captured at 1.4.4
   (`docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json`)
   — the fallback for the models the embedded catalogue does not carry.

The two agree on every number the clone uses, so the swap moved no window:

| | the rule |
|---|---|
| **window** | `min(model input limit, checkpoint max_token_limit)` — the checkpoint's limit is ignored when the checkpoint is disabled |
| **reserve** | the checkpoint's `max_output_tokens`; for a model with no checkpoint experiment, the catalogue's own output cap |

`gemini-3.8-flash-*` → 256 000 / 16 384 · the Claude 4.6 family → 160 000 /
16 384 · `gpt-oss-120b-medium` → 80 000 / 8 192 · `gemini-2.5-*` → 128 000 /
16 384. A model the clone cannot name falls back to
`modelctx.DefaultContextWindow`; a **configured** model takes its numbers
from the user's own `context_window` / `default_max_tokens`.

The checkpoint configuration itself is mirrored too — the profile table, the
alias map, the `claude-1m_840k` variant (an 840 000-token window for
`MODEL_PLACEHOLDER_M400`), the validator (the reference's four messages,
byte for byte) and the clamps — `internal/modelctx/validate.go`. The
**cascade runtime** (the planner/executor split, the steps, the trajectory)
is still not implemented; see
[`docs/guide/compaction.md`](compaction.md) and
`docs/cycles/cycle-303-cascade-model-profiles/deliverable.md`.

**Known gap:** the binary's string table carries a newer model catalogue
than the clone's static one (`gemini-4-argon-{high,medium,low}`,
`claude-opus-5-5-*`, `gemini-3.1-pro-*`), and the mapping from those slugs
to the binary's model enums is **not resolved**. Until it is, the 840 000
variant is unreachable through `For()`.

---


## The per-model marker: `supports_images` (`internal/models`, cycle `model-image-marker`)

A model entry may say it takes pictures:

```json
{"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3",
 "context_window": 1000000, "supports_images": true}
```

The marker is a **pointer** in the Go shape (`ProviderModel.SupportsImages
*bool`, `models.Model.SupportsImages *bool`): "not stated" is an answer of
its own. A model that says nothing carries **no key at all**, and the
catalogue's family table decides for it (`internal/remotecontrol/
model_mime_types.go` — gemini / claude / gpt-4o / gpt-5 / the MiniMax M3
generation take pictures, gpt-oss does not). A **stated** value is the user's
answer and nothing overrides it: `false` turns the capability off for a
family the table would have enabled, `true` turns it on for a family the
table knows nothing about.

Three front-ends write it:

| Front-end | How |
|---|---|
| `~/.config/openagy/providers.json` | the `supports_images` key — hand-edited |
| `agy model add <id> --provider <p> --supports-images` | the flag; `--supports-images=false` states the opposite, and **not passing the flag keeps the file's value** |
| the `/provider` TUI panel | the generic JSON editor writes the key like any other |

crush's own model list carries the same marker per model
(`catwalk.Model.SupportsImages`, json `supports_attachments`), and its UI
reads it off the selected model the same way — the clone mirrors that
shape.

**Why the marker exists at all:** the Web UI's attachment check reads one
field — the model config's `supportedMimeTypes` — and an *absent* map reads
as an *empty* one, which refuses every picture ("Selected model does not
support this media type"). A model that takes pictures has to say so.

The chain, end to end:

```
providers.json → models.ProviderModel.SupportsImages
               → models.Model.SupportsImages           (ProviderConfig.Flatten)
               → remotecontrol.ModelInfo.SupportsImages (rchost.hostListModels)
               → GetUserStatus's clientModelConfigs → supportedMimeTypes
```

Report: `docs/cycles/cycle-331-model-image-marker/deliverable.md`.

---

## The per-model attributes (`internal/models`, `internal/remotecontrol`, cycle `model-config-alignment`)

The image marker grew into a **set of per-model attributes**. The names are
the reference's own — the `ClientModelConfig` proto (21 fields, field
numbers in the table below) — so the field the Web UI reads is the field
the reference sends.

| Config key (`providers.json`) | Go shape | Wire field (`ClientModelConfig`) |
|---|---|---|
| `supported_mime_types` | `[]string` | 18 `supported_mime_types` → `supportedMimeTypes` |
| `supports_images` | `*bool` | the shorthand of the above |
| `disabled` | `bool` | 4 `disabled` |
| `quota_info` | `*QuotaInfo` | 15 `quota_info` → `quotaInfo` |
| `quota_info.remaining_fraction` | `*float64` | `quotaInfo.remainingFraction` |
| `quota_info.reset_time` | `string` | `quotaInfo.resetTime` |
| `supports_thought_circulation` | `*bool` | 19 `supports_thought_circulation` |
| `model_url` | `string` | 20 `model_url` |
| `tag_title` | `string` | 16 `tag_title` |
| `tag_description` | `string` | 17 `tag_description` |
| `effort_supported` | `*bool` | — (the clone's own list field; the picker never reads it) |

`model_id` (21) is **derived**, not configured: it is the upstream model
id (the `model` key). The file already has `id` (the catalogue handle)
and `model` (the upstream name) — a third id field would only confuse.

The **decision order** for each attribute:

| Attribute | 1st | 2nd | 3rd |
|---|---|---|---|
| media types | the configured list | `supports_images` | the family table |
| the tag line | the configured words | the id's guess (`flash` → "Fast") | — |
| the reasoning level | `effort_supported` | the clone's default (`true`) | — |
| disabled | the configuration only | — | — |

**proto-JSON's rule on the wire: a field that holds its default is not
emitted.** `disabled: false` is absent, an empty `supportedMimeTypes` is
absent, an empty `modelUrl` is absent — exactly as the reference's own
catalogue answers (`out/ide-language-server/live/reference/GetUserStatus.json`).
The frontend reads an absent field as "not set", so the bytes differ but
the behaviour does not.

`quotaInfo.resetTime` is spelled **protojson's** way
(`"2026-10-25T22:33:00.530076Z"` — 0, 3, 6 or 9 fractional digits), not
the configuration's own string. A string protojson cannot parse is passed
through unchanged.

### The alignment with the picker's mapper

The Web UI maps every model config through **one** function — the `Rk`
mapper in `internal/rcfrontend/assets/main.js`. It reads **11** fields
(the ten it names, plus `provider`):

`label` · `modelOrAlias` · `provider` · `disabled` ·
`supportedMimeTypes` · `quotaInfo` · `tagTitle` · `tagDescription` ·
`supportsThoughtCirculation` · `modelUrl` · `modelId`

The clone used to fill **five** of them. It fills all of them now, and
`internal/remotecontrol/model_config_alignment_test.go` pins both
directions: every field the picker reads has a source, and the clone
emits nothing the picker cannot use.

What the picker *does* with them:

- `disabled` — the entry is filtered out (`filter(b => !b.disabled)`). It
  is the **picker's** field, as in the reference: the clone's own
  `--model` list still offers the model.
- `quotaInfo` — `remainingFraction` of 0 with a `resetTime` in the
  future is the model the picker warns about.
- `tagTitle` — the badge under the model's name; the title `NEW` is what
  the picker looks for when it marks a model as new.
- `supportedMimeTypes` — the attachment check (`b.get(c) === true`), and
  the microphone button (the frontend's audio list is
  `audio/wav … audio/webm;codecs=opus`, so an `audio/webm` entry enables
  voice input).
- `supportsThoughtCirculation` and `modelUrl` — read by the mapper and
  used by nothing yet; the clone sends them for the field set's sake.

The chain, end to end:

```
providers.json → models.ProviderModel.*        (the attributes)
               → models.Model.*                 (ProviderConfig.Flatten)
               → remotecontrol.ModelInfo.*      (rchost.hostListModels)
               → GetUserStatus's clientModelConfigs (model_config_fields.go)
```

The front-ends:

| Front-end | How |
|---|---|
| `~/.config/openagy/providers.json` | the keys above — hand-edited |
| `agy model add <id> --provider <p> --supported-mime-types … ` | the flags; **not passing a flag keeps the file's value**, and `--flag=false` states the opposite |
| the `/provider` TUI panel | the generic JSON editor writes the keys like any other |

The flags: `--supported-mime-types` (comma separated) · `--disabled` ·
`--quota-reset-time` (RFC 3339) · `--quota-remaining-fraction` (a share
of the quota, 0…1) · `--supports-thought-circulation` · `--model-url` ·
`--tag-title` · `--tag-description` · `--effort-supported`. A broken
timestamp or a share above one is rejected at the flag, not at the
picker's screen.

Evidence: `out/model-config-alignment/README.md` (zh-CN).
Report: `docs/cycles/cycle-333-model-config-alignment/deliverable.md`.

---

## The per-model reasoning tiers (`internal/models`, cycle `model-reasoning-tiers`)

A **tier** is the reasoning level a model runs at. The reference has one
vocabulary for it — `low` `medium` `high` `xhigh` `max` — and it spells the
tier into the model's own catalogue id: `gemini-3.8-flash-high` is the
**high** tier of `gemini-3.8-flash`, and the catalogue lists one row per
tier (`Gemini 3.8 Flash (High)`).

A model the operator configured has no such id, so it declares its tiers:

```json
{
  "id": "minimax-m3.1-flash",
  "name": "MiniMax M3.1 Flash",
  "model": "MiniMax-M3.1-Flash",
  "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
  "reasoning_effort": "high"
}
```

| Key | Meaning |
|---|---|
| `reasoning_efforts` | the tiers the model **offers** — the closed set `low, medium, high, xhigh, max` |
| `reasoning_effort` | the tier it runs at when nothing else says — also the OpenAI `reasoning_effort` hint on the wire |

The canonical vocabulary lives in the leaf package `internal/models`
(`EffortLevels`, `IsEffortLevel`, `NormalizeEfforts`, `ValidateEfforts`);
`internal/modelcatalog.ValidEfforts` is that same list.

### The rows

A model that declares tiers is listed **once per tier**, the reference's own
shape:

```
minimax-m3.1-flash-low     MiniMax M3.1 Flash (Low)
minimax-m3.1-flash-medium  MiniMax M3.1 Flash (Medium)
minimax-m3.1-flash-high    MiniMax M3.1 Flash (High)
minimax-m3.1-flash-xhigh   MiniMax M3.1 Flash (Xhigh)
minimax-m3.1-flash-max     MiniMax M3.1 Flash (Max)
```

The tier rides the **id**, so both pickers work without a frontend change:
the Web UI's picker shows the label it is handed and sends the row's id
back; the TUI's `/model` panel does the same.

The expansion happens at the catalogue layer, not in the registry — the
registry keeps **one entry per configured model** (`List()` is unchanged),
and `models.TierRows(...)` expands the list where a catalogue is built
(`cli.ModelCatalogue()` for the CLI and the TUI panel, `rchost.hostListModels`
for the Web UI).

### The resolution

| The user writes | The catalogue id |
|---|---|
| `--model minimax-m3.1-flash --effort xhigh` | `minimax-m3.1-flash-xhigh` |
| `--model minimax-m3.1-flash` | `minimax-m3.1-flash-<the default tier>` |
| `--model minimax-m3.1-flash-max` | `minimax-m3.1-flash-max` |
| `--model minimax-m3.1-flash --effort minimal` | refused — `minimal` is not a tier |
| `--model minimax-m3 --effort high` (no tiers) | refused — the reference's own conflict error |

The **default tier** is the model's own `reasoning_effort` when that names a
tier, else `medium` when the model offers it, else the first tier — the
reference's own rule ("`--model <name>` without `--effort` picks the model's
server default").

The bare id resolves in the **catalogue** (`cli.ConfiguredModelDefaultLabel`,
consulted by `modelcatalog.CanonicalModelLabel`), because a tiered model is
listed once per tier and its bare id is not a row of its own. That lookup is
load-bearing: a configured model that missed the catalogue fell through to
the reference's default model, and the request went to the **CloudCode
account** instead of the configured endpoint — so the quota that ran out was
the Google account's, not the reader's. The reference resolves a bare id the
same way (`gemini-3.8-flash` answers `Gemini 3.8 Flash (High)`).

### The wire

The tier rides the request as the OpenAI dialect's `reasoning_effort` hint.
Three transports carry it:

| Transport | Where |
|---|---|
| the configured transport (`internal/backend/gateway_request.go`) | `backend.GatewayConfig.ReasoningEffort` → the body |
| the PiG-native provider (`internal/piagentbridge/gatewayprovider_request.go`) | `ProviderConfig.ReasoningEffort` → the body |
| the models package's provider (`internal/models/openai.go`) | the model's own `ReasoningEffort` |

The **Anthropic** dialect has no such field, so the hint is dropped there:
the Messages API spells its thinking budget differently.

The operator's own gateway (`https://agw.k0s.io`) **reads** the field: an
unknown word is refused with a 400 whose message names the allowed set —
`low, medium, high, xhigh, max`, the reference's own five words 【一手】
(`out/model-reasoning-tiers/upstream-effort-probe.txt`).

The registry's lookup is tier-aware: `GetByName("minimax-m3.1-flash-xhigh")`
finds the base model and answers with the tier applied, so every existing
caller (the transport, the context window, the catalogue) works unchanged.

### The TUI

The `/model` panel lists the tier rows ✓. The `/effort` **slider** is the
reference's own geometry — three places — so a five-tier model cannot ride
it: the tier is a *row*, and the row's label carries it. The per-turn
runtime level reads the tier from the model's label
(`cliapp.currentEffortName` → `models.TierFromLabel`), and the `--effort`
flag still wins over it.

The `/provider` panel can **declare** the tiers: the "Reasoning tiers" field
(a comma-separated list) writes `reasoning_efforts`, and a tier outside the
closed set is refused before the file is written.

### The flags

`agy model add <id> --provider <p> --reasoning-efforts low,medium,high,xhigh,max`
(also spelled `--reasoning_efforts`) — and `--reasoning-effort <tier>` says
which one the model runs at. `agy model show <id>` prints the tiers.

Evidence: `out/model-reasoning-tiers/README.md` (zh-CN).
Report: `docs/cycles/cycle-335-model-reasoning-tiers/deliverable.md`.

---

## Editing the configuration (`internal/models/edit.go`, cycle `provider-model-editor`)

`~/.config/openagy/providers.json` has **one writer** and **three front-ends**.

| File | What it is |
|---|---|
| `internal/models/edit.go` | `Editor` — `List` / `PutProvider` / `DeleteProvider` / `PutModel` / `DeleteModel`, the lock (`<path>.lock`, `O_CREATE|O_EXCL`, 5 s wait, 30 s stale) and the atomic `0600` write |
| `internal/models/jsonedit.go` | the ordered JSON object: a write replaces the one field that changed, so the key order and the unknown fields survive |
| `internal/models/render.go` | the shared renderers (`SortedProviderIDs`, `MaskAPIKey`, `RenderProviderTable`, `RenderModelTable`, `RenderProviderDetail`, `RenderModelDetail`) |
| `internal/models/verify.go` | the verification probe: one request against the endpoint, classified by the HTTP status |
| `internal/models/errors.go` | `UpstreamError` / `UpstreamStatus` — the typed upstream failure (the message is unchanged: `upstream 401: …`) |
| `internal/cli/provider_cmds.go` | the hidden `agy provider` / `agy model` subcommands — **the only** registry CLI since cycle `wrapper-merge` (`providers` is its plural alias) |
| `internal/cliapp/slash_panel_provider.go` | the `/provider` TUI panel |
| `cmd/openagy-models/main.go` | the legacy tool, repointed at the provider-centric file |

Six things to know:

- **Merge, not replace.** Every `add` keeps the fields the user did not
  pass. Clearing a field is an explicit act.
- **The credential is never rendered.** `MaskAPIKey` shows `$NAME` verbatim
  and masks a literal as `**** (literal, hidden)`. The panel's credential
  field starts *empty* for a literal and an empty field **keeps** the
  stored value.
- **The mutated child must be re-attached to its parent** (`providers.Set(id, obj)`
  *then* `doc.Set("providers", providers)`). A `DeleteModel` that forgets
  the second call reports success and changes nothing — the bug class that
  `internal/models/edit_test.go` exists to catch.
- **`provider` and `model` are hidden subcommands.** `agy --help` lists the
  reference's twelve visible subcommands and nothing else, and `agy models`'
  output is byte-pinned — so the commands that *change* the configuration
  live under their own names. The identifier may come before or after the
  flags (`splitPositionalID` knows which flags take a value).
- **`/provider` is invocable but *not* listed.** `internal/tuilib.DefaultSlashCommands`
  holds the built-in slash-command catalogue (**102** at the time of writing —
  the v1.3.5 capture pinned 99, the 1.4.x catalogue grew), and the reference's
  own "↓ 94 more" hint is byte-pinned, so the panel is routed through
  `commandPanels` / `slashHandlers` only. The clone's own tail count is
  *derived* from its catalogue and the scroll position
  (`internal/cliapp/picker_view.go`), not fixed. Keys: `↑/↓`
  navigate, `enter` edit, `a` add provider, `m` add model, `d` delete
  (twice), `v` verify, `esc` exit; the form's `←/→` cycles a choice field
  like `/config`.
- **The credential is verified, like crush.** `v` probes the endpoint with
  the credential **the user has typed** (the probe takes the credential as
  an argument, so an unsaved key is what is probed); a save starts a probe
  too. The probe is one request — a minimal chat completion when the
  provider has a model, a `GET <base>/models` listing when it does not —
  and its verdict is classified by the status (`the credential was
  rejected (HTTP 401)`, `the endpoint could not be reached: …`). The headless
  twin is `agy provider test <id>` (exit code 1 on a failed probe). The
  probe runs as a `tea.Cmd`, so the modal never blocks on the network.
- **The provider type is the *wire dialect*, and there is no alias for it.**
  `NewProvider`'s switch accepts `openai`, `anthropic`, `ollama`, `openrouter`,
  `google` and `custom`. Earlier versions of this clone wrote a tenant name
  into the field (`"provider": "agw"`), and the rename to the generic names
  dropped that value from the switch — every such configuration then failed
  with `models: unknown provider "agw"`, which the `agy` wrapper answers with
  a `502` (and the TUI hangs at *generating*). A read-only alias
  (`legacyProviderTypes`) papered over it for one cycle; it is **gone** now
  that the flat configuration file it belonged to is retired. A tenant that
  wants a label of its own puts it in the provider's `name` field and keeps
  `type: openai`. Report: `docs/cycles/legacy-config-retirement/`.

Report: `docs/cycles/provider-model-editor/`.

