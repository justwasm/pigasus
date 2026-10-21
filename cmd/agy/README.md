# cmd/agy — the wrapper

A transparent launcher for the official Antigravity CLI that

1. serves a local CloudCode proxy and points `agy` at it with
   `CLOUD_CODE_URL`,
2. injects models from `~/.config/openagy/models.json` into
   `fetchAvailableModels`, so `agy --model <slug>` works for models Google does
   not ship (MiniMax M3 via `agw.k0s.io` out of the box),
3. captures every request/response pair as JSON.

The vendor binary is never patched. `agy` 1.4.2 reads `CLOUD_CODE_URL`; the
proxy is the only mechanism. See `docs/cycles/cycle-274/`.

## Build and install

```sh
go build -o /tmp/agy-wrapper ./cmd/agy
```

Put it earlier in `$PATH` than the real binary (e.g. as `agy`), or call it
directly. The wrapper finds the real binary at `$AGY_REAL_BIN`,
`~/.local/bin/agy`, `/usr/local/bin/agy`, `/opt/homebrew/bin/agy`, then `$PATH`
— and refuses to exec itself (resolved-path **and** inode comparison), so a
mistaken `agy -> agy-wrapper` symlink cannot fork-bomb the machine.

## Usage

```
agy [agy args …]                 # proxy on, exec the real agy
agy wrapper models               # list registry models
agy wrapper models add|remove    # edit ~/.config/openagy/models.json
agy wrapper capture on|off|dir   # capture toggle, persisted in ~/.openagy/wrapper.json
agy wrapper doctor               # agy path/version, registry, port, capture, upstream probe
agy wrapper version
agy wrapper help
```

Only the literal **first** argument `wrapper` selects the subcommands.
`agy models`, `agy --version`, `agy -p "…"` and even `agy -p wrapper` go to the
vendor binary untouched.

### `wrapper models add`

```sh
agy wrapper models add local-qwen \
  -provider custom \
  -api-url http://127.0.0.1:11434/v1/chat/completions \
  -external-model qwen3:8b \
  -display-name "Qwen 3 8B" \
  -context-window 32768
```

Flags: `-provider` (openai/anthropic/ollama/google/openrouter/agw/custom),
`-api-url`, `-api-key`, `-external-model`, `-display-name`,
`-context-window`, `-reasoning-effort`. Underscore spellings are accepted too.
The name may appear before or after the flags. `add` replaces an existing entry
with the same name.

## First run

With no `~/.config/openagy/models.json`, the wrapper writes one entry and
leaves every existing file alone:

```json
{"models":[{"name":"minimax-m3","display_name":"MiniMax M3","provider":"agw",
 "api_key":"-","api_url":"https://agw.k0s.io/v1/chat/completions",
 "external_model_name":"MiniMax-M3","context_window":1000000}]}
```

`agy models` then lists `custom-minimax-m3  MiniMax M3`, and

```sh
agy -p "reply with the single word: ping" --model custom-minimax-m3
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
agy wrapper capture off
agy wrapper capture dir /tmp/agy-capture
OPENAGY_CAPTURE_DIR=/tmp/x agy -p "…"   # one-off override
```

## Logging

The proxy logs about one line per RPC. Those lines go to a **file**, not to
stderr: fd 2 is the terminal agy's TUI owns, and writing to it mid-session
smears the screen.

```
~/.openagy/wrapper.log           # default, append, truncated past 4 MiB
```

```sh
agy wrapper log                  # last 40 lines
agy wrapper log -n 200
OPENAGY_WRAPPER_LOG=off agy      # silence the file entirely
OPENAGY_WRAPPER_LOG=stderr agy   # debugging escape hatch — expect a smeared TUI
```

If the file cannot be opened the wrapper discards instead of falling back to
stderr: losing log lines must never break the TUI. `agy wrapper doctor` prints
the active sink on its `log` line.

## Environment

| Variable | Meaning |
|---|---|
| `AGY_REAL_BIN` | path to the real `agy` |
| `OPENAGY_WRAPPER_ADDR` | proxy listen address (default `127.0.0.1:0`) |
| `OPENAGY_CAPTURE_DIR` | capture directory (empty string disables) |
| `OPENAGY_UPSTREAM` | upstream base URL (default `https://daily-cloudcode-pa.googleapis.com`) |
| `OPENAGY_WRAPPER_CONFIG` | wrapper config file (default `~/.openagy/wrapper.json`) |
| `OPENAGY_WRAPPER_LOG` | proxy log sink: a file path (default `~/.openagy/wrapper.log`), `stderr`, or `off`. `stderr` smears agy's TUI — debugging only. |

Precedence is environment > `wrapper.json` > default.

## Limits

- Translation covers text, `functionCall`, `functionResponse` and
  `tools[].functionDeclarations`. Images (`inlineData`/`fileData`), thought
  parts and context compaction are not forwarded — see the cycle-274
  deliverable's follow-ups.
- The Anthropic and Google providers in `internal/models` do not carry tool
  declarations yet; OpenAI-shaped endpoints (`openai`, `agw`, `openrouter`,
  `ollama`, `custom`) do.
- `agy wrapper doctor` probes the upstream with a plain `GET /`; a 401/403/404
  still proves DNS+TLS work.
