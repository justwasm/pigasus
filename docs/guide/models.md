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
provider serves. That file is the **only** configuration shape — the legacy flat
`~/.config/openagy/models.json` has been retired (read path, write path and the
`agw` provider-type alias with it; report:
`docs/cycles/legacy-config-retirement/`). `cli.ReferenceModels()` is
the *pure* reference catalogue; `cli.ModelCatalogue()` merges the configured
models in (reference rows first, configured rows sorted by id). The transport
has **no** built-in endpoint, credential or model — an unset endpoint is an
error, not a fallback. Report: `docs/cycles/provider-config-gateway/`.

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

