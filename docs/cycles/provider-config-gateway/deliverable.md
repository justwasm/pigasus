# Cycle `provider-config-gateway` — the catalogue is configuration, the vendor's name is gone

**Status**: implemented, `cmd/wagyu` + `cmd/agy` + `internal/*` green
**Reference**: `~/.local/bin/agy` 1.4.5, live 2026-10-23
**Evidence**: `captures.md` in this directory — the live endpoint probes and the
byte-level streams they produced

Three problems, one change.

| problem | before | after |
|---|---|---|
| the extra models were hardcoded | `internal/modelcatalog/agw.go` carried the id table; the transport carried the endpoint and the credential | both live in `~/.config/openagy/providers.json` |
| the vendor's name was everywhere | `AgwK0sClient`, `AgwConfig`, `OPENAGY_AGW_URL`, the `"agw"` provider, the `"agw"` backend label, the `agw:` error prefix | `GatewayClient`, `GatewayConfig`, `OPENAGY_GATEWAY_URL`, the `"gateway"` backend label, the `gateway:` error prefix |
| `agy models` was not aligned like the reference | two fixed-width columns, always | the reference's tabwriter, aligned under a pty and tab-separated into a pipe |

The repository contains **no `Agw` and no `K0s`** any more — not in a Go
identifier, not in a config key, not in a user-visible string. The wrapper
CLI, the agent-context templates, the remote-control protocol's backend
label and the error prefixes all moved with it.

---

## 1. The configuration

`~/.config/openagy/providers.json` is the canonical file. One entry holds the
endpoint, the credential, the wire protocol and the models that provider
serves:

```json
{
  "providers": {
    "gateway": {
      "name": "Gateway",
      "type": "openai",
      "base_url": "https://gateway.example/v1",
      "api_key": "$OPENAGY_GATEWAY_KEY",
      "timeout_seconds": 60,
      "extra_headers": { "x-tenant": "acme" },
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3",
          "context_window": 1000000,
          "default_max_tokens": 8192,
          "reasoning_effort": "default"
        }
      ]
    }
  }
}
```

The path is `$XDG_CONFIG_HOME/openagy/providers.json`, else
`$HOME/.config/openagy/providers.json`; `OPENAGY_PROVIDERS_CONFIG` overrides
it. The legacy flat `~/.config/openagy/models.json` (a list of
self-contained model entries) is **still read**, so an install that has one
keeps working. Both are merged into one registry, and **the provider section
wins on a name collision** (`Registry.GetByName` consults the flattened
provider models before the flat list).

### Two ids per model

A model entry carries two names:

| field | who uses it | example |
|---|---|---|
| `id` | the catalogue: what `--model` takes, what `agy models` prints | `minimax-m3` |
| `model` | the wire: what the provider is asked for | `MiniMax-M3` |

An empty `model` means "the catalogue id is the upstream id". This split was
already the shape of the flat registry; the provider form just made it
explicit per model instead of per entry.

### The credential

`api_key` may be a literal or an environment reference: `$NAME` and
`${NAME}` are expanded (`models.ExpandEnvRef`), anything else is returned
unchanged. So the file can be committed without the secret in it.

### The endpoint

The endpoint is derived from `base_url` **by protocol**, because the two
dialects disagree about where the version segment lives:

| `type` | endpoint | base example |
|---|---|---|
| `openai` (default) | `base_url` + `/chat/completions` | `https://host/v1` |
| `anthropic` | `base_url` + `/v1/messages` | `https://host/anthropic` |

`api_url` overrides both and is used verbatim — the escape hatch for a proxy
that does not follow the convention.

### The CLI

The wrapper's registry editor is now provider-centric:

```
$ agy wrapper providers add gateway --type openai \
      --base-url https://gateway.example/v1 --api-key '$OPENAGY_GATEWAY_KEY'
$ agy wrapper models add minimax-m3 --provider gateway \
      --external-model MiniMax-M3 --display-name 'MiniMax M3' \
      --context-window 1000000
$ agy wrapper providers list
$ agy wrapper models list
```

`models add` **requires** `--provider` and refuses an unknown one (the error
names the `providers add` command). The wrapper no longer invents models on
first run: a wrapper that wrote an endpoint the user never configured would
advertise a model that cannot work, and the first run would have to be undone
by hand.

`cmd/openagy-models` still manages the **legacy flat file** and lost its
`agw` provider token (the wire protocols it accepts are now exactly the ones
`models.NewProvider` dispatches on: `openai`, `anthropic`, `ollama`,
`openrouter`, `google`, `custom`).

---

## 2. The dispatch

`pickBackendWithLog` (`cmd/wagyu/main_backend.go`) now decides by the model,
not by a hardcoded provider:

| the model is… | the transport is |
|---|---|
| in the reference's own catalogue (`cli.ReferenceModels()`) | **CloudCode** — the reference's transport |
| configured by the user | **the configured endpoint**, with the protocol from the provider entry |
| neither | the **shim**, plus a warning that names `providers.json` |

An explicit `--backend gateway` for a model nobody configured also lands on
the shim with the same warning: the configured transport has no endpoint for
a model that is not configured, and answering from the shim without saying
so would look like a working model.

The backend names are `gateway`, `cloudcode` and `shim`. `agw` and `k0s` are
no longer accepted.

---

## 3. The Anthropic dialect

【一手】 The endpoint that answers the Anthropic Messages API is
**`<base>/v1/messages`** — the version segment belongs to the *dialect*, not
to the host:

```
POST https://gateway.example/anthropic/v1/messages   → 200
POST https://gateway.example/anthropic/messages       → 404
POST https://gateway.example/v1/messages              → 404
```

The credential travels in `x-api-key` (not `Authorization`), and
`anthropic-version: 2023-06-01` is required. The same endpoint serves the
same models as the OpenAI-shaped one, with the same credential.

The dialect is implemented in `internal/backend/gateway_anthropic.go`, inside
the transport seam, so the agent loop does not know which wire it is on. The
two places where a mechanical translation from the OpenAI shape would be
wrong are handled explicitly:

- **the system prompt is a top-level field**, not a message;
- **a tool result is a *user* turn** holding a `tool_result` block — the
  Messages API has no `tool` role.

The response is the Anthropic event stream: `message_start`,
`content_block_start` / `_delta` / `_stop`, `message_delta`, `message_stop`
(the endpoint also sends a `ping` frame, which the parser skips). Text
arrives as `text_delta`, a tool call's arguments as `input_json_delta`
fragments that have to be concatenated, and a thinking block as
`thinking_delta` — which is **dropped**, because the transport seam carries
text only and the clone's sanitiser turns reasoning into a separate surface.

`max_tokens` is **required** by the Messages API, so the Anthropic request
always carries one: the caller's value, else the model's configured
`default_max_tokens`, else a conservative 4096.

**One implementation, not two.** `piagentbridge.NativeProvider` derives the
native PiG provider only for the OpenAI dialect; an Anthropic provider goes
through the generic `BackendProvider`/`StreamFn` path. The print path
(`internal/printmode`) and the TUI path (`internal/agent/pig.go`) therefore
move together without either of them growing a second transport.

---

## 4. The catalogue

`cli.ReferenceModels()` is now the **pure reference catalogue** — the six ids
the reference itself accepts, and nothing else. The MiniMax rows that used
to live there are gone: they are configured models, not reference models.

`cli.ModelCatalogue()` merges the two: the reference rows first, then the
configured rows sorted by id, with the reference winning an id/label
collision. It is what `agy models` prints and what the `/model` picker
shows. `internal/modelctx` resolves a display label to a catalogue id
through it, so a configured model's context window comes from the
configuration and not from a hardcoded table.

The effort tiers are a property of the reference catalogue: `hasEffortTiers`
is false only for a configured (non-reference) model. An unknown or empty
name keeps the tiers, which preserves the previous behaviour.

---

## 5. `agy models` alignment

【一手】 The reference renders the model list two ways, and the difference is
only visible in a terminal:

```
stdout is a terminal  ->  space aligned columns
  gemini-3.8-flash-medium   Gemini 3.8 Flash (Medium)
  claude-opus-4-6-thinking  Claude Opus 4.6 (Thinking)

stdout is a pipe      ->  tab separated rows
  gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)
```

The aligned form is a `text/tabwriter` with minwidth 0, tabwidth 8 and
padding 2: the widest catalogue id is `claude-opus-4-6-thinking` (24
characters), so every label starts at column 26. `Fetching available
models...` goes to **stderr**, not stdout, so a piped list is clean.

`internal/cli/model_table.go` renders it; both the parity path
(`runModelsParity`) and the legacy path (`runModels`) use it.

---

## What changed where

| file | what |
|---|---|
| `internal/models/providers.go` | `ProviderConfig`, `ProviderModel`, `ProviderTypeOpenAI`/`ProviderTypeAnthropic`, `WireType`, `Endpoint`, `ResolvedAPIKey`, `ExpandEnvRef`, `Flatten`, `ProvidersPath`, `LoadProviders`, `ParseProviders`, `SaveProviders`, `LoadAll`, `CachedRegistry` |
| `internal/models/registry.go` | `Model` gained `ProviderID`, `Type`, `ExtraHeaders`, `DefaultMaxTokens`; the lookups are provider-aware |
| `internal/models/provider.go` | the `"agw"` case is gone; the remaining protocols are the ones the config can name |
| `internal/backend/gateway.go` | `GatewayConfig` (`URL`, `APIKey`, `ExternalModel`, `Type`, `Headers`, `TimeoutSeconds`, `DefaultMaxTokens`); **no built-in endpoint, credential or model**; protocol dispatch; `GatewaySystemFingerprint` derives the host from the configured endpoint |
| `internal/backend/gateway_anthropic.go` | the Messages dialect: request build and event-stream read |
| `internal/piagentbridge/native_provider.go` | `NativeProvider` returns nil for a non-OpenAI protocol, an empty URL or an empty model |
| `internal/cli/model_catalogue.go` | `ModelCatalogue()` — the reference rows plus the configured rows |
| `internal/modelcatalog/wire.go` | `WireModel` / `WireLabel` — the catalogue-id ↔ upstream-id mapping, now table-free |
| `internal/modelctx/modelctx.go` | the context windows come from the configuration |
| `internal/configtest/configtest.go` | `Isolate(t)` / `WriteProviders(t, body)` — the tests get their own configuration |
| `cmd/wagyu/main_backend.go` | the dispatch above |
| `cmd/wagyu/slash_panel_model.go` | the picker shows the reference rows and the configured rows |
| `cmd/agy/wrapper.go` | `providers add\|remove\|list`, `models add\|remove\|list` |
| `cmd/agy/config.go` | `loadRegistry()` replaces `ensureRegistry()`; `defaultModels()` is deleted |

---

## 硬约束

- The gateway transport has **no default endpoint, credential or model**. An
  unset endpoint is an error, not a fallback. The only environment overrides
  are `OPENAGY_GATEWAY_URL` and `OPENAGY_GATEWAY_KEY`, and they apply only
  when the configuration leaves the field empty.
- The Anthropic request always carries `max_tokens`; the OpenAI one does not
  (the field is optional there and the reference omits it).
- The provider section wins over the flat section on a name collision, and
  the flat section is **read-only** now — nothing in the tree writes it
  except `cmd/openagy-models`.
- The remote-control protocol's backend label changed from `"agw"` to
  `"gateway"`. It is a wire value: a dashboard that expects the old string
  has to be told.

## 存疑

- **The Anthropic dialect was exercised against one endpoint.** The request
  shape follows the Messages API, and the *response* shape was transcribed
  from that endpoint's stream. An endpoint that emits the standard frames in
  a different order (a `content_block_delta` before its
  `content_block_start`, say) would not be handled.
- **The thinking block is dropped, not surfaced.** The clone's sanitiser
  turns reasoning into a separate surface for the OpenAI dialect; the
  Anthropic dialect has no equivalent yet, so a model that reasons under
  the Messages API shows no thought row.
- **The `deepseek` catalogue the gateway advertises is not used.**
  `GET /v1/models` lists two ids (`deepseek-flash`, `deepseek-v4-pro`) with
  `api_capabilities.anthropic_messages`, and neither is in the clone's
  catalogue. Whether the reference would surface them is unknown.
- **The wrapper's proxy merges the configured models into
  `fetchAvailableModels`.** With the provider section, a model whose
  catalogue id contains a dot still has to survive the slug mapping
  (`custom-minimax-m3.1-flash`). The exact-match-first lookup is pinned by
  a test, but the merge is only exercised by the wrapper's own tests.

## limitations

- One gateway, one day. The endpoint probes in `captures.md` are evidence
  for *this* endpoint on 2026-10-23; the configuration is the durable part.
- No `internal/referencedrift` artifact was added: the provider
  configuration is the user's file, not a version-scoped fixture, and it
  changes whenever the user adds a model.
- The clone's `agy models` lists the reference's six rows **plus** the
  configured ones. The reference lists six, or seven under the wrapper. The
  extra rows are the documented deviation of a configurable catalogue.
- `cmd/openagy-models` still writes the legacy flat file. Two editors for
  two shapes is more than ideal; the provider one is the canonical one.
- The Anthropic path is exercised through `backend.Stream` and through the
  generic PiG provider. The **native** PiG provider is OpenAI-only, so an
  Anthropic provider pays the generic path's translation cost.
