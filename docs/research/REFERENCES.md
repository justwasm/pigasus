# External references

Third-party material this repository reads or compares against. Nothing here
is authoritative over the binary: where a reference and a capture disagree, the
capture wins (see `docs/research/agy-feature-strings.md` for the evidence
grading used across these docs).

## System prompts

| Reference | What it is | How this repo uses it |
|---|---|---|
| [Jackwio — `antigravity-cli.md`](https://gist.github.com/Jackwio/fd918f4d5d3e530cf95c6b8b74e10fde) | A 1,400-line zh-Hant *feature and command guide* for Antigravity CLI: install, auth, the 15 core features, the full slash-command list, keybindings, and execution modes. It is documentation, not a prompt — it describes the surfaces the prompt talks about. | Cross-check for the `/goal`, `/compact`, `/schedule`, `/browser`, `/plan` slash commands the CLI prompt lists as recommendable, and for the tool/feature vocabulary the prompt assumes. |
| [asgeirtj/system_prompts_leaks PR #233](https://github.com/asgeirtj/system_prompts_leaks/pull/233) → [`Google/antigravity-cli.md`](https://github.com/asgeirtj/system_prompts_leaks/blob/X-Nemesis-main/Google/antigravity-cli.md) | The **Antigravity IDE (desktop)** system prompt, ~50 KB, with MCP-server docs, the Knowledge-Items system, the planning/walkthrough workflow and a JSON tool-schema appendix. Open, unmerged PR; the text lives on the `X-Nemesis` fork. | Independent corroboration for the CLI prompt's `<identity>` opening sentence and its `<user_information>` field layout (`The USER's OS version is …`, `Code relating to the user's requests …`, `App Data Directory:`, `Conversation ID:`). It is **not** the CLI prompt: its app-data directory is `~/.gemini/antigravity-ide`, it documents IDE-only sections, and it is prose rather than the CLI's XML tags. |
| `internal/agyprompt/testdata/capture_1.4.2.txt` (this repo) | The **Antigravity CLI 1.4.2** system prompt, 34,350 bytes, recorded verbatim from a real request by the cycle-274 wrapper. | The authoritative text. `internal/agyprompt` reproduces it byte for byte. |

The captured CLI prompt and the leaked IDE prompt share an ancestry — the
identity paragraph and the `<user_information>` block are near-identical — but
they are different products' prompts and must not be substituted for one
another.

## Proxying and custom models

| Reference | What it is | How this repo uses it |
|---|---|---|
| [`kelvinzer0/antigravity-cli`](https://github.com/kelvinzer0/antigravity-cli) | A TypeScript launcher that **patches** the vendor binary and fronts it with a proxy so non-Google models can be used. | Design reference for the proxy (cycle 274, now `internal/agyproxy`). This port keeps the proxy but drops the patching: `agy` honours `CLOUD_CODE_URL`, so no binary edit is needed. |
| A local gateway checkout (a Go proxy exposing an OpenAI-shaped `/v1/chat/completions` **and** an Anthropic-shaped `/anthropic/v1/messages` over upstream Gemini/Anthropic) | The gateway the clone's provider configuration points at. | The upstream proxy proxies custom-model calls to it; the clone reaches it through `~/.config/openagy/providers.json`. |
| [`charmbracelet/crush`](https://github.com/charmbracelet/crush) | Another Charm-based coding agent, with a `providers` map in its config, a read-only `crush models` command, and a `/models` dialog that groups by provider and edits the credential (`ctrl+e` → an API-key dialog that *verifies* the key with the provider before saving). | Design reference for the provider-configuration editor (cycle `provider-model-editor`): the shape of the file, the write cycle (`sjson.Set` → atomic → `flock` → reload, `internal/config/store.go:301`) and the panel's grouping. The *bytes* are the clone's own — the reference binary has no such panel, so there is nothing to match byte for byte. |
