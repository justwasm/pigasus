# CloudCode model switching — the selected model now reaches the wire

Before this cycle the `/model` picker changed three things: the banner row, the
status row, and `settings.json#model`. It did **not** change the transport.
`pickBackendWithLog` dropped its `modelName` argument on the floor, and
`AgwK0sClient` deliberately ignores `StreamRequest.Model` and sends
`ExternalModel`, whose default is `MiniMax-M3`. So every "switch" to Gemini or
Claude still produced a MiniMax turn.

This cycle adds the official transport the reference uses and routes by model
family, both at startup and on every runtime switch.

## First-hand evidence 【一手】

Captured 2026-10-20. The reference is `agy` 1.4.3 on this machine, with the
user's Google AI Pro account; requests were recorded by a verbatim local
passthrough (`CLOUD_CODE_URL=http://127.0.0.1:51999`) that forwards to
`https://daily-cloudcode-pa.googleapis.com` and writes the raw request/response
bytes.

### Endpoint and headers

```
POST /v1internal:streamGenerateContent?alt=sse
Authorization: Bearer ya29.a0AX07…
Content-Type: application/json
User-Agent: antigravity/cli/1.4.3 (aidev_client; os_type=darwin; arch=arm64;
            cl=1002753436; auth_method=consumer)
```

`cloudcode-pa.googleapis.com` (the wrapper's `DefaultUpstream`) answers **429**
for this account on every model; `daily-cloudcode-pa.googleapis.com` answers
200. The same request bytes, same headers: the host is the only difference.
That is why the `cmd/agy` wrapper appeared "quota exhausted" while the official
binary worked.

### Request envelope (Gemini)

```json
{"project":"aicode-consumers",
 "requestId":"agent/<uuid>/<millis>/<uuid>/1",
 "request":{
   "contents":[{"role":"user","parts":[{"text":"…"}]}],
   "systemInstruction":{"role":"user","parts":[{"text":"…34543 bytes…"}]},
   "tools":[{"functionDeclarations":[{"name":"read_file","parameters":{"type":"OBJECT",…}}]}],
   "labels":{"cascade_id":…,"model_enum":"MODEL_PLACEHOLDER_M35",…},
   "generationConfig":{"maxOutputTokens":65536,
     "thinkingConfig":{"includeThoughts":true,"thinkingBudget":1000}},
   "sessionId":"-3750763034362895579"},
 "model":"gemini-3.8-flash-low",
 "userAgent":"antigravity","requestType":"agent"}
```

Claude differs only in `maxOutputTokens:64000`, `thinkingBudget:1024`, and the
labels (`used_claude`, `used_claude_conservative`, `used_non_gemini_model`).

**`sessionId` is a JSON string**, not a number. Sending the integer produces
`400 Invalid value at 'request.session_id' (TYPE_STRING)`.

### Response frames

```
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"…"}]},
        "finishReason":"STOP"}],
        "usageMetadata":{"promptTokenCount":123,"candidatesTokenCount":1,
                         "totalTokenCount":124},
        "modelVersion":"gemini-3.5-flash-lite"},
       "traceId":"…","metadata":{}}
```

### Tool round trip

First response frame:

```json
{"thoughtSignature":"EmoKaAFpFH0TejWnoITg83m8…",
 "functionCall":{"name":"run_command",
                 "args":{"CommandLine":"echo hello-tool","WaitMsBeforeAsync":5000},
                 "id":"call_113205"}}
```

The follow-up request replays the call and the result like this:

```json
{"role":"model","parts":[{"functionCall":{…},"thoughtSignature":"EmoKa…"}]},
{"role":"model","parts":[{"functionResponse":{"id":"call_113205",
  "name":"run_command","response":{"output":"…hello-tool\r\n"}}}]}
```

Two details matter: the tool result is a **model-role** content (not `user`),
and the signature rides on the same part as the `functionCall`.

### The tool catalogue must be unique

The clone advertises `invoke_subagent` twice (its own catalogue plus PiG's
subagent surface). The agw gateway tolerated it; Gemini rejects the whole
request:

```
Error: The turn stopped before it finished: Duplicate function declaration
found: invoke_subagent
```

The transport now keeps the first declaration per name.

## Implementation

| File | What it does |
|---|---|
| `internal/backend/cloudcode.go` | The transport: envelope, SSE parser, usage, tool calls, signature replay, 401 retry, `CLOUD_CODE_URL`. |
| `internal/backend/cloudcode_schema.go` | JSON Schema → Gemini `Schema`: uppercase `type`, drop `additionalProperties`/`$schema`. |
| `cmd/wagyu/main.go` | `pickBackendWithLog` routes by model; `--backend` gained `cloudcode` (`google`, `gemini`, `cc`); the OAuth gate covers both real families. |
| `cmd/wagyu/tui_run.go` | `runMode.backendFor` factory, installed by `runLiveTUI`. |
| `cmd/wagyu/slash_panels.go` | `syncBackendForModel`, called by `selectModelEffort` and `cycleModelEffort`. |
| `cmd/wagyu/slash_regression.go` | `/effort` rebuilds the transport too. |

Routing rule: catalogue **Gemini / Claude / GPT-OSS** → CloudCode; **MiniMax**
→ agw. An explicit `--backend agw` (or `OPENAGY_BACKEND=agw`) still wins, so the
old gateway stays reachable. Unknown names keep the previous agw behaviour.

The token source prefers the **macOS login keychain** (`gemini`/`antigravity`)
when one exists, because the reference does. This machine's on-disk cascade
(`~/.config/antigravity/oauth-token`, `~/.gemini/antigravity-cli/…`) held a
`ya29.fake-refreshed` fixture while the keychain held the live token; without
the preference every CloudCode turn returned 401.

## Verification 【一手】

Unit tests (all mocked with captured frames):

```
go test ./internal/backend -run TestCloudCode
go test ./cmd/wagyu -run 'TestPickBackend_|TestModelSelectionSwapsBackend'
```

Live, against the real endpoint:

| Path | Command | Result |
|---|---|---|
| print, Gemini | `wagyu -p 'Reply with exactly: PIGASUS-OK' --model gemini-3.8-flash-low` | `PIGASUS-OK` (200 from CloudCode) |
| print, Gemini label | `--model "Gemini 3.8 Flash (Medium)"` | `GEMINI-OK` |
| print, Claude | `--model claude-sonnet-4-6` + `run_command` | tool ran, result reported |
| print, MiniMax | `--model minimax-m3` | `MINIMAX-OK` (agw) |
| continue | turn 1 tool call, then `-c` turn 2 | second tool call succeeds |
| TUI, MiniMax→Gemini | pty: `/model`, ↓, Enter, prompt | banner switches, `CLOUD-OK` |
| TUI, Gemini→Claude | pty: ↓↓ from the MiniMax row | banner `Claude Sonnet 4.6 (Thinking)`, `CLAUDE-OK` |
| TUI, Gemini→MiniMax | pty: Enter on row 0 | banner `MiniMax M3`, `MINI-OK` |
| TUI tool call | Gemini prompt asking for `run_command` | `● Ran (echo tui-tool)` + output |
| TUI thinking | Claude prompt, then ctrl+o | `▸ Thought for 600ms`; expanded body `173 * 29 = …` |
| print thinking | same Claude prompt | body stripped, only `5058` |

The TUI runs used `ANTIGRAVITY_HOME` temp dirs and `OPENAGY_TRUST=1`; captures
are byte-level pty transcripts, replayed with pyte.

## 硬约束 / 存疑 / limitations

- 硬约束: the transport does not touch the system prompt, the display, or the
  catalogue. `resolveEffectiveModel` and `resolveModelSelection` keep the
  reference's 1.4.3 validation: an unknown id still falls back / errors exactly
  as before.
- 硬约束: `internal/referencedrift` and `cli.ReferenceVersion` are untouched;
  the version stays 1.4.3.
- 存疑: **reasoning display depends on the model.** CloudCode streams
  `thought:true` text parts (measured on Claude Sonnet 4.6, one part per token
  run). The transport wraps each run in the reference's `<think>…</think>`
  convention, which is exactly what the TUI's sanitizer already consumes for
  MiniMax, so the transcript renders `▸ Thought for 600ms` and ctrl+o expands
  the body; print mode strips it. The measured Gemini 3.8 Flash turns returned
  only signature-only thought parts (no visible text), so those turns show no
  thought row. `usageMetadata.thoughtsTokenCount` is carried when present; the
  Claude endpoint omits it, so the Claude row has a duration but no token count.
- 存疑: **tool `thoughtSignature`s live in the client's memory only.** History
  replay across a restart (the measured `-c` turn) worked without them, but a
  long resumed conversation is unproven.
- 存疑: Gemini 3.1 Pro / Argon appear in `agy models` but the **reference's own
  `--model` validator rejects them** (`agy -p … --model gemini-3.1-pro-high` →
  "not recognized … Available models: Gemini 3.8 …"). The clone matches that
  behaviour rather than accepting slugs the server 404s. Extending the picker to
  server-advertised models is a separate cycle.
- limitations: the `cl=1002753436` user-agent build number and the
  `antigravity/cli/1.4.3` prefix are constants; they must move with the
  reference version.
- limitations: `HTTPError.Error()` still prefixes `agw:`. That string is only
  used in logs; the user-facing path reads `Message()`.
- limitations: no live capture was taken on Linux or Windows; `os_type` follows
  `runtime.GOOS`.
- limitations: quota/tier rendering is unchanged. `loadCodeAssist` still reports
  `current-tier: free-tier` for this account even though official models work;
  the clone renders whatever the server says.
