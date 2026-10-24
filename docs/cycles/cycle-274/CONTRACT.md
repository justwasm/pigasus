# Cycle 274 — `cmd/agy`: a Go wrapper that gives the official `agy` custom models

Frozen interface contract. Implement exactly these names/signatures; the
integrator depends on them.

## Proven background (do not re-litigate)

`agy` 1.4.2 honours `CLOUD_CODE_URL`. Measured first-hand:

```
$ CLOUD_CODE_URL=http://127.0.0.1:50999 agy models
POST /v1internal:loadCodeAssist      (38 bytes)
POST /v1internal:listExperiments     (2 bytes)
```

So **no binary patching is needed**. The wrapper starts a local HTTP server and
exports `CLOUD_CODE_URL=http://127.0.0.1:<port>`. Every CloudCode RPC arrives at
`<base>/v1internal:<method>`.

The reference implementation (TypeScript, `kelvinzer0/antigravity-cli`) instead
patched the binary — keep a patch fallback only behind `agy wrapper patch`, off by
default.

## Layout (all new, stdlib only)

```
devutils/
  agybin/      locate the *real* agy executable
  geminiwire/  Gemini CloudCode JSON types (request, response, stream chunk)
  agyproxy/    HTTP server: passthrough + model injection + translation + capture
cmd/agy/       the launcher
```

`devutils/agyproxy` MAY import `internal/models` (existing registry + provider
implementations). Nothing else in this cycle imports third-party packages.

## `devutils/agybin`

```go
package agybin

// Path resolves the real agy binary. Order: explicit arg, $AGY_REAL_BIN,
// well-known install paths. It MUST never return the currently running
// executable (self-exec guard) — compare os.Executable() by resolved path and
// by inode.
func Path(explicit string) (string, error)

// Version runs `agy --version` and returns the trimmed first line.
func Version(path string) (string, error)
```

Well-known paths: `~/.local/bin/agy`, `/usr/local/bin/agy`, `/opt/homebrew/bin/agy`.

## `devutils/agyproxy`

```go
package agyproxy

type Config struct {
    Addr       string          // default "127.0.0.1:0"
    Upstream   string          // default "https://cloudcode-pa.googleapis.com"
    Models     []models.Model  // from internal/models
    CaptureDir string          // "" disables capture
    Logf       func(string, ...any)
}

type Server struct { /* ... */ }

func New(cfg Config) (*Server, error)
func (s *Server) Start() (addr string, err error) // "127.0.0.1:PORT"
func (s *Server) Close() error
```

### Routing

| inbound | behaviour |
|---|---|
| `POST /v1internal:fetchAvailableModels` | forward upstream; merge custom models |
| `POST /v1internal:generateContent` | custom model ⇒ translate; else passthrough |
| `POST /v1internal:streamGenerateContent` | custom model ⇒ translate + Gemini SSE; else passthrough |
| anything else | transparent passthrough (method, headers, body, status, body) |

Passthrough must preserve `Authorization`, `x-goog-*`, `User-Agent` and copy the
response status + headers. Upstream failure returns 502 with a JSON error.

### Model injection

Custom model `m` is advertised as a **placeholder slot** so agy treats it as an
agent model:

- `slug`: `custom-` + lowercased `external_model_name` (strip `models/`, collapse
  non-alphanumerics to `-`, trim `-`).
- `placeholder`: `MODEL_PLACEHOLDER_M<N>` where `N = 400 + (djb2(lower(display_name||name)) % 200)`.

`models[]` entry:

```json
{"name":"models/MODEL_PLACEHOLDER_M4xx","version":"1.0","displayName":"…",
 "description":"…","inputTokenLimit":<context_window or 1048576>,
 "outputTokenLimit":8192,
 "supportedGenerationMethods":["generateContent","countTokens"],
 "temperature":0.7,"topP":0.9,"topK":40}
```

and append each `slug` to every `agentModelSorts[].groups[].modelIds` (create
`[{"groups":[{"modelIds":[…]}]}]` when absent). Never drop upstream models.

A custom model is matched when the requested model string contains either the
placeholder id, the slug, the internal `name`, or the external model name.

### Capture (the reverse-engineering payload)

One file per request, `CaptureDir/<unixmilli>-<seq>-<method>.json`, containing:

```json
{"ts":"RFC3339Nano","kind":"request|response","method":"streamGenerateContent",
 "httpMethod":"POST","path":"/v1internal:streamGenerateContent",
 "model":"MODEL_PLACEHOLDER_M4xx","custom":true,"status":200,
 "requestHeaders":{…},"requestBody":<raw JSON>,"responseBody":<raw or SSE text>}
```

Bodies are raw (not re-encoded) so prompts/tool schemas can be diffed verbatim.
Capture must never block or fail a request.

## `cmd/agy`

```
agy [agy args …]                 # default: proxy on, MiniMax M3 available, exec real agy
agy wrapper models               # list registry models
agy wrapper models add|remove    # edit ~/.config/openagy/models.json
agy wrapper capture on|off|dir  # capture toggle, persisted in ~/.openagy/wrapper.json
agy wrapper doctor               # agy path/version, registry, port, capture, upstream probe
agy wrapper version
agy wrapper help
```

Rules:

- Only the literal first argument `wrapper` selects wrapper subcommands. Anything
  else is passed to agy untouched (including `agy --version`, `agy models`).
- Before exec: set `CLOUD_CODE_URL`, keep the rest of the environment, wire
  stdin/stdout/stderr, and forward SIGINT/SIGTERM. Exit with agy's exit code.
- Env: `AGY_REAL_BIN`, `OPENAGY_WRAPPER_ADDR`, `OPENAGY_CAPTURE_DIR`,
  `OPENAGY_UPSTREAM`, `OPENAGY_WRAPPER_CONFIG`.
- Registry path: `internal/models.DefaultConfigPath()`.
- First run with no registry file: create it with **one** default entry so
  MiniMax M3 works out of the box:

```json
{"name":"minimax-m3","display_name":"MiniMax M3","provider":"agw",
 "api_key":"-","api_url":"https://agw.k0s.io/v1/chat/completions",
 "external_model_name":"MiniMax-M3","context_window":1000000}
```

## Non-goals this cycle

- No Anthropic/Google/Ollama translator work beyond what already exists in
  `internal/models` (it covers openai/anthropic/ollama/google/agw/custom).
- No context injection, compaction, or per-model reasoning overrides — record as
  follow-ups in the deliverable.

## Constraints

- `go build ./cmd/agy` and `go test ./devutils/... ./cmd/agy/...` must pass.
- New packages are stdlib-only (plus `internal/models` for the proxy).
- Do **not** run `gofmt -w` across pre-existing dirty files; format only files you
  create.
- Do not push. Commit in the worktree on branch `cycle-274/agy-wrapper`.
