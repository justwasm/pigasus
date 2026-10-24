# Cycle `legacy-config-retirement` — the flat configuration file is gone

**Date:** 2026-10-23
**Scope:** `internal/models`, `internal/cli`, `internal/configtest`,
`internal/modelcatalog`, `cmd/agy`, `cmd/openagy-models`, `AGENTS.md`,
`README.md`.

## 0. What this cycle is

The provider configuration went through two shapes:

| shape | file | written by | status |
|---|---|---|---|
| flat | `~/.config/openagy/models.json` — a list of self-contained entries, each with its own endpoint, credential and upstream id | cycle 33 (`internal/models/registry.go`) | **retired here** |
| provider-centric | `~/.config/openagy/providers.json` — one provider entry holds the endpoint, the credential and the protocol; the models it serves are listed under it | cycle `provider-config-gateway` | the only shape |

Both were read, and the provider section won on a name collision. The flat
file had not been *written* since the provider-centric form landed — the only
code that still touched it was `models.RemoveFlatModel`, i.e. the fallback
branch of "remove this model" when the provider section did not hold it.

The user asked for the compatibility to be dropped and the code removed. This
cycle does that: the flat file's read path, its write path, its fallback
branches and the provider-type alias that only existed to serve it are gone.

## 1. What was removed

### 1.1 `internal/models/registry.go`

The `Registry` had two halves — a `models []Model` slice (the flat section)
and a `providers map[string]ProviderConfig` map. The flat half is gone; the
registry is now the provider section and nothing else.

| removed | replaced by |
|---|---|
| `Registry.models` field | — (the flat section) |
| `LoadFromFile(path)` — read a flat file *and* a provider section | `LoadRegistry(path)` — read the provider section of `path` |
| `SaveToFile(path, r)` — write the flat file | — (the `Editor` is the only writer) |
| `Registry.Add(Model)` | `Registry.AddProviderModel(providerID, ProviderModel)` |
| `Registry.Remove(name)` — flat first, then the provider section | `Editor.DeleteModel(id)` / `Registry.RemoveProviderModel` |
| `Registry.List()`'s flat merge | `Registry.List()` returns the provider models |
| `Registry.Len()`'s flat count | provider models only |
| `clone()` | — (only `SaveToFile` used it) |
| `DefaultConfigPath()` — the path of `models.json` | `ProvidersPath()` |

The `Model` struct itself stays: it is the *merged* shape the rest of the
code base works with, and `ProviderConfig.Flatten()` produces it.

### 1.2 `internal/models/providers.go`

`LoadAll()` read two files and merged them. There is one file now, so
`LoadAll()` reads the provider file and `CachedRegistry()`'s cache stamp
covers that file alone.

### 1.3 `internal/models/edit.go`

`RemoveFlatModel(path, name)` — the only remaining writer of the flat file —
is deleted. `agy model remove` and `agy wrapper models remove` now have one
place to look.

### 1.4 `internal/models/provider.go` — the `agw` alias

`legacyProviderTypes` mapped `"agw"` → `"custom"` so a configuration the
clone had written itself would still resolve. With the flat file gone, the
provider's `type` field can only be a wire dialect, so the alias is dead
code — and so is the `case "agw":` branch of `openAIShapeProvider.doPost`,
which sent the bearer header even for an empty credential.

`NewProvider`'s switch now accepts exactly: `openai`, `anthropic`, `ollama`,
`openrouter`, `google`, `custom`. Anything else is
`models: unknown provider "<name>"`.

### 1.5 The front-ends

| file | change |
|---|---|
| `internal/cli/provider_cmds.go` | `agy model remove`'s flat fallback deleted |
| `cmd/agy/wrapper.go` | `wrapper models remove`'s flat fallback deleted; `wrapper models list` no longer prints the flat path |
| `cmd/agy/config.go` | the "merged registry" comment |
| `cmd/openagy-models/main.go` | `loadOrEmpty` uses `models.LoadRegistry` |
| `internal/models/agent_context.go` | the two context bodies describe one file; the default install directory is the provider file's directory |
| `internal/configtest/configtest.go` | `OPENAGY_MODELS_CONFIG` is no longer set (the variable does not exist any more) |
| `internal/cli/dispatch.go`, `internal/backend/gateway.go`, `internal/cli/parser_specs.go` | comments |

## 2. What was *not* removed, and why

- **`Model.Provider` / `Model.Type`** still exist. `Provider` is the wire
  dialect the transport speaks; `Type` is the same thing spelled out. They
  are the merged shape, not the flat file.
- **`ProviderConfig.Type` still accepts any string.** `WireType()` lowercases
  it and defaults it to `openai`; a file that says `"type": "ollama"` still
  gets Ollama's no-auth-header behaviour, because that is a *dialect*, not a
  tenant name.
- **`SaveProviders`** stays. It is provider-centric and not part of the flat
  file's life cycle (it is, however, unused outside the package — the
  `Editor` is the writer the front-ends use).
- **`internal/models/render.go`, `verify.go`, `errors.go`** are untouched:
  they never knew about the flat file.

## 3. A machine-dependent test that this exposed

`internal/modelcatalog/model_select_test.go` pinned the "Available models:"
block of the unknown-model error **including the two MiniMax models the
developer's own machine had configured** — the expectation was the user's
`models.json`, not the reference's catalogue. It passed on this machine and
would have failed anywhere else.

The test now isolates the configuration (`configtest.Isolate`) and pins the
reference's own six rows, and a new test
(`TestAvailableModelsBlockIncludesConfiguredModels`) covers the configured
rows with an explicit fixture. Same for `internal/modelcatalog`'s other
machine-dependent expectations.

**This is the second time** a test in this tree depended on the operator's
own configuration. `internal/modelcatalog/model_resolve_test.go` already used
`configtest`; `model_select_test.go` was simply missed.

## 4. The migration

The flat form cannot be expressed one-to-one in the provider form, and the
losses are worth naming:

| flat field | provider form | loss |
|---|---|---|
| `provider` (a *type*) | `type` | a tenant name in the field (`"agw"`) is not a dialect any more — it becomes the provider's `name` |
| `api_url` | `api_url` (verbatim) or `base_url` (protocol-appended) | none: `Endpoint()` uses `api_url` verbatim |
| `api_key` | `api_key` | none |
| `external_model_name` | the model's `model` | none |
| `display_name` | the model's `name` | none |
| `context_window` | the model's `context_window` | none |
| `timeout_seconds` / `max_retries` / `extra_headers` | the provider's | per-model → per-provider |
| *nothing* | the provider's `id` | a new key: the flat entry had no provider to hang under, so the models of one endpoint have to be grouped under one provider |

The migration for this machine (the user's own configuration) was:

```json
{
  "providers": {
    "agw": {
      "id": "agw",
      "name": "AGW",
      "type": "openai",
      "api_url": "https://agw.k0s.io/v1/chat/completions",
      "api_key": "-",
      "models": [
        {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3", "context_window": 1000000},
        {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash", "context_window": 1048576}
      ]
    }
  }
}
```

Written to `~/.config/openagy/providers.json` (`0600`). The old file was kept
as `~/.config/openagy/models.json.bak` — nothing reads it.

## 5. Verification

`repro/wg01_config_retirement.sh` runs the built binaries against two
fixtures and records what they say (the transcript is in `captures.md`):

- fixture 1 — a flat `models.json` alone: the catalogue is the reference's
  six rows, the wrapper says `no models configured`. The flat file is inert.
- fixture 2 — the same model in `providers.json`: it appears in both.

Live, against the user's own endpoint (`https://agw.k0s.io`):

| path | command | result |
|---|---|---|
| clone, print | `wagyu --model "MiniMax M3.1 Flash" --print "reply with the single word: pong"` | `pong` |
| clone, print | `wagyu --model "MiniMax M3" --print …` | `pong` |
| wrapper (the real `agy` 1.4.5 + the local proxy) | `agy-wrapper --model "MiniMax M3.1 Flash" -p "reply with the single word: pong"` | `pong`, exit 0 |
| clone, catalogue | `wagyu models` | the reference's six rows + `minimax-m3` + `minimax-m3.1-flash` |
| wrapper, catalogue | `agy-wrapper wrapper models list` | `registry: ~/.config/openagy/providers.json`, two models |
| wrapper, doctor | `agy-wrapper wrapper doctor` | `registry ~/.config/openagy/providers.json (2 models)` |

The unit suite: `go test ./cmd/... ./internal/...` and
`go test ./devutils/...` are green; `go vet ./cmd/... ./internal/...` is
clean.

## 6. 硬约束 / 存疑 / limitations

**硬约束**

- The flat file is **not** read any more. An install that only has
  `models.json` has **no configured models** — the catalogue falls back to
  the reference's six and the transport falls back to the shim. There is no
  migration tool and no automatic conversion; the file has to be rewritten by
  hand (the shape is in §4).
- `OPENAGY_MODELS_CONFIG` no longer exists. A script that sets it is
  silently ignored; the environment variable that matters is
  `OPENAGY_PROVIDERS_CONFIG`.
- `NewProvider` rejects `"agw"` (and every other non-dialect) with
  `models: unknown provider "<name>"`. A provider that wants a tenant label
  uses the provider's `name`.
- `~/.config/openagy/models.json.bak` on this machine is a **backup**, not a
  configuration. Nothing in the tree reads it. It can be deleted.

**存疑**

- The flat file's `extra_headers` were per model; the provider form's are
  per provider. A configuration that had two models on one endpoint with
  *different* extra headers cannot be expressed exactly. This machine's
  configuration had none.
- The flat file's `timeout_seconds` / `max_retries` were per model. The
  provider form's are per provider, so two models on one endpoint with
  different timeouts cannot be expressed exactly.
- The `agw` provider-type alias is gone. If some *other* tool in the
  ecosystem writes `models.json` with `"provider": "agw"`, that tool has to
  be updated too. Nothing in this tree does any more (checked: the flat
  writer was `RemoveFlatModel`, and that is deleted).

**limitations**

- The repro script only exercises the *listing* path. The live `pong`
  checks were run by hand against the user's endpoint; they are not part of
  the script (the script stays offline).
- The migration of this machine's configuration was done by hand. A general
  migration command (`agy wrapper providers migrate`) was considered and
  **not** implemented: with the flat file no longer read, it would have to
  read a file the code otherwise ignores, which is the opposite of what this
  cycle is for.
- The reference binary (`~/.local/bin/agy` 1.4.5) has no notion of any of
  this — it never read `models.json`. It is unaffected.
