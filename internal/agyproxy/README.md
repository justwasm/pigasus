# internal/agyproxy — the local CloudCode proxy

A local CloudCode proxy that

1. serves the official Antigravity CLI, pointing it at the proxy with
   `CLOUD_CODE_URL`,
2. injects the models from `~/.config/openagy/providers.json` into
   `fetchAvailableModels`, so `agy --model <slug>` works for models Google does
   not ship (MiniMax M3, once you configure the provider),
3. captures every request/response pair as JSON.

The vendor binary is never patched. `agy` 1.4.2 reads `CLOUD_CODE_URL`; the
proxy is the only mechanism. See `docs/cycles/cycle-274/`.

## Where the code lives

Cycle `wrapper-merge` moved the implementation out of `cmd/agy` and into the
clone; cycle `wrapper-flatten` dissolved the wrapper as a *product*. There is
now **one binary and one command namespace**:

| Command | What it does |
|---|---|
| `pigasus agy [args…]` | runs the vendor `agy` behind the local proxy |
| `pigasus proxy doctor\|log\|capture\|version` | the proxy's admin commands |
| `pigasus provider …`, `pigasus model …` | the provider/model registry (canonical) |
| `pigasus providers …` | the registry under the plural spelling (alias) |

The registry is implemented **once**: `internal/cli` owns the editor, the file
and the renderers. The proxy only *reads* the registry — it never edits it.

`cmd/agy` (the old drop-in `agy` binary) is gone. Use `pigasus agy`, or alias
it in the shell (`alias agy='pigasus agy'`) if you want the vendor CLI to be
reached through the proxy by default.

## Build

```sh
go build -o /tmp/pigasus ./cmd/pigasus
```

The proxy finds the vendor binary at `$AGY_REAL_BIN`, `~/.local/bin/agy`,
`/usr/local/bin/agy`, `/opt/homebrew/bin/agy`, then `$PATH` — and refuses to
exec itself (resolved-path **and** inode comparison), so a mistaken
`agy -> pigasus` symlink cannot fork-bomb the machine.

## Usage

```
pigasus agy [agy args …]              # proxy on, exec the real agy
pigasus proxy capture on|off|dir      # capture toggle, persisted in ~/.openagy/wrapper.json
pigasus proxy doctor                  # agy path/version, registry, port, capture, upstream probe
pigasus proxy log [-n N]              # tail the proxy log
pigasus proxy version
pigasus proxy help
```

Only the literal **first** argument `agy` selects the launcher. `pigasus models`,
`pigasus --version`, `pigasus -p "…"` and even `pigasus -p agy` go to the clone
normally. The launcher is dispatched in `cmd/pigasus/main.go` **before the
parser exists**, so the vendor binary sees its command line untouched
(`pigasus agy --help` prints the *vendor's* help) and never inherits the
clone's startup side effects.

### The registry

```sh
pigasus provider add local \
  -type openai \
  -base-url http://127.0.0.1:11434/v1 \
  -api-key "-"

pigasus model add local-qwen \
  -provider local \
  -external-model qwen3:8b \
  -display-name "Qwen 3 8B" \
  -context-window 32768
```

Provider flags: `-name`, `-type` (`openai` | `anthropic`), `-base-url`,
`-api-url`, `-api-key` (a `$NAME` / `${NAME}` value is read from the
environment), `-timeout-seconds`, `-max-retries`, `-header name=value`
(repeatable). The endpoint is derived from `-base-url` by protocol: `openai`
appends `/chat/completions`, `anthropic` appends `/v1/messages`. `-api-url`
overrides both and is used verbatim.

Model flags: `-provider` (**required** — the provider that serves this model),
`-external-model`, `-display-name`, `-context-window`, `-default-max-tokens`,
`-reasoning-effort`. Underscore spellings are accepted too. The name may
appear before or after the flags. `add` **merges**: a flag that is not passed
keeps the value the file already had.

## First run

**The proxy creates nothing.** There is no registry file until you write one,
because a proxy that invented an endpoint would advertise a model that cannot
work:

```sh
pigasus provider add gateway \
  -type openai \
  -base-url https://gateway.example/v1 \
  -api-key '$OPENAGY_GATEWAY_KEY'
pigasus model add minimax-m3 \
  -provider gateway \
  -external-model MiniMax-M3 \
  -display-name "MiniMax M3" \
  -context-window 1000000
```

`agy models` then lists `custom-minimax-m3  MiniMax M3`, and

```sh
pigasus agy -p "reply with the single word: ping" --model custom-minimax-m3
```

is answered by MiniMax M3. (The default model — the one agy picks with no
`--model` — still goes to Google, so on an account that is not eligible for
Gemini Code Assist that turn fails with the upstream 403. That is agy's own
error, reproduced faithfully.)

## How it works

```
agy ──CLOUD_CODE_URL=http://127.0.0.1:PORT──▶ agyproxy ──▶ cloudcode-pa.googleapis.com
                                               │
                    ┌──────────────────────────┴──────────────────────────┐
                    │ fetchAvailableModels  → merge custom models         │
                    │ generateContent       → translate → provider → JSON │
                    │ streamGenerateContent → translate → provider → SSE  │
                    │ anything else         → byte-for-byte passthrough   │
                    └─────────────────────────────────────────────────────┘
```

The proxy binds `127.0.0.1:0` (ephemeral) unless `OPENAGY_WRAPPER_ADDR` says
otherwise. Pass-through copies the request method, `Authorization`,
`x-goog-*`, `User-Agent` and body, and relays the response status, headers and
body without buffering; only the custom-model calls are translated.

Custom models are advertised as `MODEL_PLACEHOLDER_M<N>` (djb2 of the display
name, `N` in 400..599) and their `custom-…` slug is appended to every
`agentModelSorts[].groups[].modelIds`. Upstream models are never removed and
merging is idempotent.

A request is treated as a custom model when the model string contains the
placeholder, the slug, the registry name or the external model name
(case-insensitive).

## Capture

On by default, into `~/.openagy/captures`. One file per record:

```
~/.openagy/captures/1792429582609-026-streamGenerateContent.json
```

```json
{"ts":"…","kind":"request","method":"streamGenerateContent",
 "httpMethod":"POST","path":"/v1internal:streamGenerateContent",
 "model":"custom-minimax-m3","custom":true,
 "requestHeaders":{"Authorization":["…"],"User-Agent":["antigravity/1.4.2 …"]},
 "requestBody": { …raw JSON exactly as agy sent it… }}
```

The matching `…-038-…json` carries `"kind":"response"`, the status, and the
raw body — for a stream, the concatenated `data: …` SSE text. Capture is
asynchronous and best-effort: a full queue drops records, never a request.

```sh
pigasus proxy capture off
pigasus proxy capture dir /tmp/agy-capture
OPENAGY_CAPTURE_DIR=/tmp/x pigasus agy -p "…"   # one-off override
```

## Logging

The proxy logs about one line per RPC. Those lines go to a **file**, not to
stderr: fd 2 is the terminal agy's TUI owns, and writing to it mid-session
smears the screen.

```
~/.openagy/wrapper.log           # default, append, truncated past 4 MiB
```

```sh
pigasus proxy log                  # last 40 lines
pigasus proxy log -n 200
OPENAGY_WRAPPER_LOG=off pigasus agy     # silence the file entirely
OPENAGY_WRAPPER_LOG=stderr pigasus agy  # debugging escape hatch — expect a smeared TUI
```

If the file cannot be opened the proxy discards instead of falling back to
stderr: losing log lines must never break the TUI. `pigasus proxy doctor` prints
the active sink on its `log` line.

## Environment

| Variable | Meaning |
|---|---|
| `AGY_REAL_BIN` | path to the real `agy` |
| `OPENAGY_WRAPPER_ADDR` | proxy listen address (default `127.0.0.1:0`) |
| `OPENAGY_CAPTURE_DIR` | capture directory (empty string disables) |
| `OPENAGY_UPSTREAM` | upstream base URL (default `https://daily-cloudcode-pa.googleapis.com`) |
| `OPENAGY_WRAPPER_CONFIG` | proxy config file (default `~/.openagy/wrapper.json`) |
| `OPENAGY_WRAPPER_LOG` | proxy log sink: a file path (default `~/.openagy/wrapper.log`), `stderr`, or `off`. `stderr` smears agy's TUI — debugging only. |

Precedence is environment > `wrapper.json` > default.

The `OPENAGY_WRAPPER_*` names and the `wrapper.json` / `wrapper.log` paths are
**kept from the wrapper era on purpose**: they are the on-disk contract of an
installed proxy, and renaming them would silently move an existing capture
directory and reset the persisted settings. The *command surface* was
flattened; the *files* were not.

## Limits

- Translation covers text, `functionCall`, `functionResponse` and
  `tools[].functionDeclarations`. Images (`inlineData`/`fileData`), thought
  parts and context compaction are not forwarded — see the cycle-274
  deliverable's follow-ups.
- The Anthropic and Google providers in `internal/models` do not carry tool
  declarations yet; OpenAI-shaped endpoints (`openai`, `openrouter`, `ollama`,
  `custom`) do.
- `pigasus proxy doctor` probes the upstream with a plain `GET /`; a
  401/403/404 still proves DNS+TLS work.
