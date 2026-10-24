# cycle 274 — `cmd/agy`: a wrapper that gives the official `agy` custom models

## TL;DR

`agy` 1.4.2 honours `CLOUD_CODE_URL`. That single fact removes the need to
patch the vendor binary, which is what the TypeScript reference
(`kelvinzer0/antigravity-cli`) did. This cycle ships a Go launcher that

1. starts a local CloudCode proxy on an ephemeral loopback port,
2. exports `CLOUD_CODE_URL=http://127.0.0.1:<port>` and execs the **real**
   `agy` (never itself — resolved-path *and* inode guard),
3. merges registry models into `fetchAvailableModels` and translates their
   `generateContent` / `streamGenerateContent` calls to whatever provider
   `internal/models` speaks,
4. captures every request/response pair as JSON for reverse engineering.

Measured end to end on this machine: `agy models` lists
`custom-minimax-m3  MiniMax M3`, and

```
$ agy -p "reply with the single word: ping" --model custom-minimax-m3
<think>The user is asking me to reply with the single word "ping". …</think>

ping
```

The turn is served by `https://agw.k0s.io/v1/chat/completions`
(`MiniMax-M3`), not by Google. `~/.local/bin/agy` is byte-identical before and
after (SHA-256 `df2d4fb5…`).

**One contract deviation, forced by measurement:** the real
`fetchAvailableModels` returns `models` as an **object keyed by model id**, not
as an array, and each value must carry a `model` placeholder for `--model` to
resolve (§3.2).

## 1. What was measured first

`agy models` through a proxy bound to an unused port, with no registry and no
translation (§7 reproduces it verbatim):

```
agyproxy: POST /v1internal:loadCodeAssist -> 200
agyproxy: POST /v1internal:fetchAdminControls -> 200
agyproxy: POST /v1internal:fetchUserInfo -> 200
agyproxy: POST /v1internal:retrieveUserQuotaSummary -> 403
agyproxy: POST /v1internal:fetchAvailableModels -> 200
agyproxy: POST /v1internal:retrieveUserQuotaSummary -> 403
agyproxy: POST /v1internal:listExperiments -> 200
agyproxy: POST /v1internal:loadCodeAssist -> 200
```

【一手】Every RPC arrives as `POST <base>/v1internal:<method>`; the body is the
CloudCode envelope `{project, requestId, model, request:{…}, userAgent,
requestType}` and the reply is `{response:{…}, traceId, metadata}`. The proxy
therefore routes on the path suffix and only parses the body for the RPCs that
carry a model.

The account on this machine is not eligible for Gemini Code Assist in its
region, so every **Google** turn answers 403:

```
error: Your current account is not eligible for Gemini Code Assist for individuals because it is not currently available in your location.
```

【一手】That is reproduced faithfully (the proxy is transparent), and it is why
the interesting acceptance case is the *custom* model: it never reaches Google
for inference.

## 2. Layout

All new, stdlib only; `devutils/agyproxy` imports `internal/models`.

| Path | Contents |
|---|---|
| `devutils/agybin/` | resolve the real `agy` (self-exec guard), `--version` |
| `devutils/geminiwire/` | Gemini CloudCode JSON types (request, response, chunk) |
| `devutils/agyproxy/` | server: passthrough, injection, translation, capture |
| `cmd/agy/` | launcher + `agy wrapper …` admin subcommands |
| `internal/models/{provider,tools}.go` | function-calling added to the provider layer |

```
agy ──CLOUD_CODE_URL──▶ agyproxy ──▶ https://cloudcode-pa.googleapis.com
                          │
       fetchAvailableModels  → merge registry models
       generateContent       → translate → models.Provider.Chat   → JSON
       streamGenerateContent → translate → models.Provider.Stream → SSE
       everything else       → verbatim passthrough
```

## 3. Model injection

### 3.1 Placeholder and slug

`Placeholder` = `"MODEL_PLACEHOLDER_M" + (400 + djb2(lower(displayName || name)) % 200)`,
the djb2 in `uint32` so it matches the reference bit for bit.
`Slug` = `custom-` + lowercased external model name with `models/` stripped and
non-alphanumerics collapsed to `-`. For the default entry:
`MODEL_PLACEHOLDER_M488`, `custom-minimax-m3`.

【一手】`agy models` prints the **slug**, and the RPCs carry the slug too —
not the placeholder:

```
"model":"custom-minimax-m3"   (captured request body, §6)
```

so `MatchModel` accepts placeholder, `models/<placeholder>`, slug, registry
name, display name and external model name, case-insensitively. Substring
matching is limited to ids of ≥ 4 characters so a short name cannot swallow the
whole list.

### 3.2 The `models` field is a map, not an array — contract deviation

The contract's entry shape is the **array** element:

```json
{"name":"models/MODEL_PLACEHOLDER_M4xx","version":"1.0","displayName":"…",
 "description":"…","inputTokenLimit":1048576,"outputTokenLimit":8192,
 "supportedGenerationMethods":["generateContent","countTokens"],
 "temperature":0.7,"topP":0.9,"topK":40}
```

【一手】agy 1.4.2's `fetchAvailableModels` reply uses an **object**:

```
top keys: agentModelSorts, audioTranscriptionModelIds, commandModelIds,
          commitMessageModelIds, defaultAgentModelId, deprecatedModelIds,
          experimentIds, imageGenerationModelIds, models, mqueryModelIds,
          tabModelIds, tieredModelIds, webSearchModelIds
models: object with 35 entries, e.g.
  "gemini-3.8-flash-high": {"apiProvider":"API_PROVIDER_GOOGLE_GEMINI",
    "displayName":"Gemini 3.8 Flash (High)","maxOutputTokens":65536,
    "maxTokens":1048576,"model":"MODEL_PLACEHOLDER_M318", …}
```

and each value carries `model` = a placeholder id in the 1..399 range. Inserting
the contract's array-shaped entry as a map *value* produces a model that
`agy models` happily lists and `agy --model <slug>` rejects:

```
error: failed to construct executor: plan model not specified; you must specify a valid model
```

after which `agy --model MODEL_PLACEHOLDER_M488` said, even more explicitly:

```
error: invalid model selection (--model "MODEL_PLACEHOLDER_M488" --effort ""):
       model MODEL_PLACEHOLDER_M488 is not recognized as a known model or custom model in settings
```

So `MergeAvailableModels` picks the shape from the payload:

| upstream `models` | emitted custom entry |
|---|---|
| array | the contract's array element, verbatim (`modelEntry`) |
| object | map value with `model` + capability flags (`capabilityEntry`) |
| absent | object form — a degraded Google still allows a custom turn |

`capabilityEntry` is the reference implementation's shape plus the contract's
numeric fields:

```json
{"displayName":"MiniMax M3","description":"…","version":"1.0","recommended":true,
 "maxTokens":1000000,"maxOutputTokens":8192,
 "inputTokenLimit":1000000,"outputTokenLimit":8192,
 "model":"MODEL_PLACEHOLDER_M488",
 "apiProvider":"API_PROVIDER_GOOGLE_GEMINI","modelProvider":"MODEL_PROVIDER_GOOGLE",
 "tokenizerType":"LLAMA_WITH_SPECIAL","supportsImages":false,"supportsThinking":false,
 "supportedGenerationMethods":["generateContent","countTokens"],
 "temperature":0.7,"topP":0.9,"topK":40}
```

`agentModelSorts` handling is exactly as contracted: append the slug to every
`groups[].modelIds`, creating `[{"groups":[{"modelIds":[…]}]}]` when absent.
Upstream models are never dropped, and merging is idempotent — a test asserts
byte equality after a second merge.

【一手】After the fix `agy models` lists the model and the turn runs. 【推测】
Whether the array shape is still used by some other CloudCode surface is not
known; it is kept because the contract specifies it and it costs nothing.

### 3.3 `countTokens`

answered locally for custom models (`chars/4`), because forwarding a placeholder
to Google 404s and aborts the turn before the translator runs.

## 4. Translation

`ToChatRequest` maps

| Gemini | `models.ChatRequest` |
|---|---|
| `systemInstruction.parts[].text` | `SystemPrompt` |
| `contents[].role` `user`/`model` | `user`/`assistant` |
| `parts[].text` (non-thought) | message `Content` |
| `parts[].functionCall` | assistant `.ToolCalls` (`id`, `name`, `arguments` JSON string) |
| `parts[].functionResponse` | one `tool` message per response, `ToolCallID` paired |
| `tools[].functionDeclarations[]` | `Tools` with JSON-Schema `parameters` |
| `generationConfig.maxOutputTokens/temperature` | `MaxTokens`/`Temperature` |

Schema types are lower-cased (`STRING` → `string`, `OBJECT` → `object`) because
Gemini spells them in upper case and OpenAI rejects that. Thought parts are
dropped (no provider field yet). Empty call ids are synthesised as
`call_<name>` so the response pairs.

【一手】The real agent request carries **13 tool declarations** and a
**34,350-character** system prompt (see §6), so the tool path is exercised by
the money shot, not only by unit tests.

The response side emits Gemini wire format, always `finishReason:"STOP"` and
`functionCall` parts with `id`:

```
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"ping"}]},"index":0}]},"traceId":"","metadata":{}}

data: {"response":{"candidates":[{"content":{"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{…}},"traceId":"","metadata":{}}
```

Tool-call arguments that are not valid JSON (bare strings, fenced markdown) are
kept as `{"_raw":"…"}` rather than losing the call. Non-streaming calls return
a single JSON body; `wrapped` reproduces whichever envelope agy sent.

### Provider layer

`internal/models` gained `ToolSpec`/`ToolCall` and `ChatRequest.Tools`,
`ChatMessage.ToolCalls`/`ToolCallID`, `ChatResponse.ToolCalls`,
`StreamEvent.ToolCalls`. The OpenAI-shaped provider both sends declarations and
accumulates streamed `tool_calls` fragments (keyed by stream `index`), flushing
one complete event before `Done`. Everything is `omitempty`, so a pre-274
caller is byte-identical on the wire. Anthropic/Google providers do not carry
tools yet (recorded as a follow-up).

## 5. Passthrough

`Authorization`, `x-goog-*`, `User-Agent`, method, path, query and body are
copied; `Host`, `Content-Length` and `Accept-Encoding` are recomputed so a
rewritten body cannot lie about its length. The response status and all
non-hop-by-hop headers are copied and the body is streamed through a 32 KiB
buffer with a flush after each read — no buffering of the model's SSE.
Upstream transport failure is a 502 with a Gemini-shaped error envelope.

【一手】Covered by tests: a 418 + custom header + body round-trip, header
assertions on the upstream side, and a 3-frame SSE body arriving complete
through the proxy.

## 6. Capture

One JSON file per record, `~/.openagy/captures/<unixmilli>-<seq>-<method>.json`,
raw bodies (`json.RawMessage` when valid JSON, a string otherwise — which is
exactly what an SSE response is).

【一手】A real captured custom-model request (file names from the acceptance
run):

```
/tmp/cap-minimax/1792429582609-026-streamGenerateContent.json   (request)
/tmp/cap-minimax/1792429582609-038-streamGenerateContent.json   (response)
```

request record:

```
top keys      : ['ts', 'kind', 'method', 'httpMethod', 'path', 'model',
                 'custom', 'requestHeaders', 'requestBody']
httpMethod    : POST  path: /v1internal:streamGenerateContent
model         : custom-minimax-m3   custom: True
body keys     : ['project', 'requestId', 'request', 'model', 'userAgent', 'requestType']
inner keys    : ['contents', 'systemInstruction', 'tools', 'labels',
                 'generationConfig', 'sessionId']
system prompt : 1 part(s), 34350 chars, starts '<identity>\nYou are Antigravity, a powerful agentic AI coding'
contents      : 1 roles ['user']
tools         : 13 declarations: 1
tool names    : ['view_file']
genConfig     : {"maxOutputTokens": 8192, "thinkingConfig": {"includeThoughts": false, "thinkingBudget": 0}}
requestHeaders: ['Accept-Encoding', 'Authorization', 'Content-Type', 'User-Agent']
```

response record (raw SSE text, truncated here):

```
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"\u003cthink\u003eThe user is asking me to reply…"}]},"index":0}]},"traceId":"","metadata":{}}
…
data: {"response":{"candidates":[{"content":{"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":16199,"candidatesTokenCount":31,"totalTokenCount":16230}},"traceId":"","metadata":{}}
```

Capture is a bounded channel (256) drained by its own goroutine: a slow disk
drops records and logs, it never blocks or fails a request. Streaming
responses are buffered up to 16 MiB so the SSE text can be stored; past that
the capture is truncated and the request continues.

Capture files **exist even when the turn fails** — the plain (non-custom) run
in §7 wrote 36 files (18 request/response pairs) before `agy` exited 3 on the
location 403.

## 7. Acceptance, verbatim

### 7.1 build

```
$ go build -o /tmp/agy-wrapper ./cmd/agy
$ ls -l /tmp/agy-wrapper
-rwxr-xr-x  1 gear  staff  10484402 Oct 20 01:00 /tmp/agy-wrapper
```

### 7.2 `wrapper doctor`

```
$ /tmp/agy-wrapper wrapper doctor
wrapper     0.1.0 (cycle 274)
agy         /Users/gear/.local/bin/agy (1.4.2)
registry    /Users/gear/.config/openagy/models.json (1 model)
            minimax-m3       MiniMax M3           agw        → MiniMax-M3           MODEL_PLACEHOLDER_M488
config      /Users/gear/.openagy/wrapper.json
upstream    https://cloudcode-pa.googleapis.com
port        127.0.0.1:0 (ephemeral; CLOUD_CODE_URL=http://127.0.0.1:62406)
capture     /Users/gear/.openagy/captures (on)
            52 files, 968.5 KiB
probe       https://cloudcode-pa.googleapis.com: HTTP 404 in 344ms
```

The `created …` line seen on the very first run went to stderr:

```
agy wrapper: created /Users/gear/.config/openagy/models.json with the default MiniMax M3 entry
```

An existing registry is never rewritten (`ensureRegistry` stats the path first).

### 7.3 money shot — `agy models` through the proxy

stdout (the tool's own output):

```
$ /tmp/agy-wrapper models
gemini-3.8-flash-high	Gemini 3.8 Flash (High)
gemini-3.8-flash-medium	Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low	Gemini 3.8 Flash (Low)
gemini-3.7-flash-high	Gemini 3.7 Flash (High)
gemini-3.7-flash-medium	Gemini 3.7 Flash (Medium)
gemini-3.7-flash-low	Gemini 3.7 Flash (Low)
gemini-3.6-flash-high	Gemini 3.6 Flash (High)
gemini-3.6-flash-medium	Gemini 3.6 Flash (Medium)
gemini-3.6-flash-low	Gemini 3.6 Flash (Low)
gemini-3.1-pro-high	Gemini 3.1 Pro (High)
gemini-3.1-pro-low	Gemini 3.1 Pro (Low)
claude-sonnet-4-6	Claude Sonnet 4.6 (Thinking)
claude-opus-4-6-thinking	Claude Opus 4.6 (Thinking)
gpt-oss-120b-medium	GPT-OSS 120B (Medium)
custom-minimax-m3	MiniMax M3
```

stderr:

```
agyproxy: listening on http://127.0.0.1:62371 (upstream https://cloudcode-pa.googleapis.com, 1 custom model(s), capture /Users/gear/.openagy/captures)
Fetching available models...
agyproxy: POST /v1internal:loadCodeAssist -> 200 (542ms)
agyproxy: POST /v1internal:fetchAdminControls -> 200 (255ms)
agyproxy: POST /v1internal:fetchUserInfo -> 200 (255ms)
agyproxy: POST /v1internal:retrieveUserQuotaSummary -> 403 (255ms)
agyproxy: POST /v1internal:fetchAvailableModels -> 200 (2.2s)
agyproxy: POST /v1internal:retrieveUserQuotaSummary -> 403 (124ms)
agyproxy: POST /v1internal:listExperiments -> 200 (820ms)
agyproxy: POST /v1internal:loadCodeAssist -> 200 (1.114s)
```

Eligibility passed (`loadCodeAssist` 200), and the list contains
`custom-minimax-m3  MiniMax M3`. Note the list is 15 rows while upstream
advertises 35 models: that filter is agy's own (plan/quota), not the wrapper's —
merging never removes anything.

### 7.4 one real turn, capture on

Plain turn (agy's default model) in a throwaway directory — **fails upstream,
captures anyway**:

```
$ cd $(mktemp -d) && /tmp/agy-wrapper -p "reply with the single word: ping"
EXIT=3
stdout bytes: 0
proxy RPCs: 18
capture files: 36
error: Your current account is not eligible for Gemini Code Assist for individuals because it is not currently available in your location.
```

Custom-model turn — **succeeds**:

```
$ cd $(mktemp -d) && OPENAGY_CAPTURE_DIR=/tmp/cap-minimax \
    /tmp/agy-wrapper -p "reply with the single word: ping" --model custom-minimax-m3
EXIT=0
<think>The user is asking me to reply with the single word "ping". This is a simple, direct request that I should comply with.</think>

ping
```

stderr, filtered to the interesting lines:

```
agyproxy: listening on http://127.0.0.1:62257 (upstream https://cloudcode-pa.googleapis.com, 1 custom model(s), capture /tmp/cap-minimax)
agyproxy: minimax-m3 → MiniMax-M3 (stream, 1 msg, 13 tool)
agyproxy: POST /v1internal:streamGenerateContent?alt=sse -> 200 (3.593s)
```

The two `streamGenerateContent … -> 403` lines in the same run are `agy`
trying its default Google model for a side call; the final
`-> 502 (context canceled)` is `agy` aborting a follow-up request after the
turn completed. Both are agy's behaviour, passed through.

Capture evidence for that turn is quoted in §6 (`…-026-…` request,
`…-038-…` response, `model: custom-minimax-m3`, `custom: true`, status 200).

### 7.5 the vendor binary is untouched

```
$ shasum -a 256 ~/.local/bin/agy      # before
df2d4fb571d9be0557c32a5ce899fc3773f43b2ee3b18bee0d0340d14dbf9590
$ shasum -a 256 ~/.local/bin/agy      # after all of the above
df2d4fb571d9be0557c32a5ce899fc3773f43b2ee3b18bee0d0340d14dbf9590
```

`~/.gemini/antigravity-cli/history.jsonl` was not edited by hand; no
interactive `agy` session was run.

### 7.6 a real tool call, both directions

Added after the first write-up: follow-up 7 from the previous revision, run on
the same machine. MiniMax M3 was asked to list the directory *with a tool*, so a
full Gemini ⇄ OpenAI function-call round trip had to happen on the wire.

```
$ D=$(mktemp -d); cd "$D"; printf 'alpha\nbeta\n' > a.txt; printf 'x\n' > b.txt
$ OPENAGY_CAPTURE_DIR=/tmp/cap-tool /tmp/agy-wrapper -p \
    "List the files in the current directory using a tool, then say DONE." \
    --model custom-minimax-m3
EXIT=0
agyproxy: POST /v1internal:streamGenerateContent?alt=sse -> 403 (128ms)   # agy's own default model
agyproxy: POST /v1internal:streamGenerateContent?alt=sse -> 200 (5.713s)  # custom turn 1: tool call
agyproxy: POST /v1internal:streamGenerateContent?alt=sse -> 200 (2.183s)  # custom turn 2: final text
```

Model → agy (SSE frame, our translation; `args` intact):

```
data: {"response":{"candidates":[{"content":{"role":"model","parts":[
  {"functionCall":{"name":"run_command","args":{"CommandLine":"ls -la","Cwd":"…"}}}]}
```

agy → model on the next turn (we rebuild these as OpenAI `tool_calls`):

```
functionCall     {"id":"call_FY65VdFmTRXh0CMPZdZGr9nT","name":"run_command",
                  "args":{"CommandLine":"ls -la", …}}
functionResponse {"id":"call_FY65VdFmTRXh0CMPZdZGr9nT","name":"run_command",
                  "response":{"output":"Created At: … The command …"}}
```

Counts over `/tmp/cap-tool`: 2 `functionCall`, 2 `functionResponse`, all with the
matching `id`. So the id-mapped round trip is now 【一手】 for both directions,
not stub-only. The first attempt of this run failed with agy's own
`Eligibility check failed: … userinfo: EOF` (this account's googleapis
reachability is intermittent) and succeeded on retry — a vendor-side transient,
relayed untouched.

## 8. Files

| File | Change |
|---|---|
| `devutils/agybin/agybin.go` | new — path resolution + self-exec guard + version |
| `devutils/agybin/agybin_test.go` | new — self guard (path and symlink), explicit/env/precedence, non-executable, directory, version |
| `devutils/geminiwire/geminiwire.go` | new — Gemini/CloudCode request, response, chunk types |
| `devutils/agyproxy/server.go` | new — routing, passthrough, capture-aware writer, defaults |
| `devutils/agyproxy/inject.go` | new — slug, placeholder, merge (array + map), matching, countTokens |
| `devutils/agyproxy/translate.go` | new — Gemini ⇄ `models.ChatRequest`, custom turn handler, SSE |
| `devutils/agyproxy/capture.go` | new — asynchronous JSON capture |
| `devutils/agyproxy/inject_test.go` | new — slug/placeholder/detection, idempotent merge, map shape |
| `devutils/agyproxy/server_test.go` | new — translation, stream round-trip, non-stream, passthrough, unreachable upstream, capture |
| `cmd/agy/main.go` | new — dispatch (`wrapper` only as literal argv[1]) |
| `cmd/agy/launcher.go` | new — proxy start, `CLOUD_CODE_URL`, exec, signal + exit-code forwarding |
| `cmd/agy/config.go` | new — `~/.openagy/wrapper.json`, env precedence, default registry |
| `cmd/agy/wrapper.go` | new — `models|capture|doctor|version|help` |
| `cmd/agy/main_test.go` | new — dispatch, help, capture toggle, add/remove round-trip |
| `cmd/agy/README.md` | new — usage, internals, limits |
| `internal/models/tools.go` | new — `ToolSpec`, `ToolCall` |
| `internal/models/provider.go` | modified — tools in request/messages/response/stream; streamed `tool_calls` accumulation |

## 9. Verification

```
$ go test -count=1 ./devutils/... ./cmd/agy/... ./internal/models/...
ok  	github.com/chadgpt/openagy/devutils/agybin	1.505s
ok  	github.com/chadgpt/openagy/devutils/agyproxy	0.928s
?   	github.com/chadgpt/openagy/devutils/geminiwire	[no test files]
ok  	github.com/chadgpt/openagy/cmd/agy	0.913s
ok  	github.com/chadgpt/openagy/internal/models	1.089s
RC=0
```

`go vet ./devutils/... ./cmd/agy/...` clean; `gofmt -l cmd/agy devutils` empty
(new files only — `internal/models` was already gofmt-dirty and was not
reformatted wholesale). No network in any test: upstreams are `httptest`
servers, and the one "unreachable" test points at `127.0.0.1:1`.

Tests found two real defects before the acceptance run did:

1. **Case-sensitive RPC sniffing.** `inspect` tested
   `strings.Contains(path, "GenerateContent")`, but agy spells
   `:generateContent` with a lower-case `g`. Every non-streaming custom call
   was silently passed through to Google (401/403). The streaming path worked,
   which is exactly the kind of asymmetry a single happy-path test misses;
   both are now case-insensitive and routed explicitly.
2. **The map/array shape of `models`** (§3.2): `agy models` looked fine while
   `--model` could not resolve a plan model.

## 硬约束 / 存疑 / limitations

- **The Google path could not be exercised positively.** This account is
  location-blocked for Gemini Code Assist, so all Gemini traffic is a 403
  passthrough. Passthrough fidelity is therefore proved against `httptest`
  upstreams (status, headers, body, SSE framing), not against a successful
  Google turn. 【一手】only for "403 is relayed unchanged"; a 200 Google turn is
  【推测】.
- **The map-shaped entry is a superset guess.** It matches the reference
  implementation and the observed upstream entries, and it demonstrably makes
  `--model` resolve. Whether agy proto-decodes `fetchAvailableModels`
  strictly (rejecting unknown fields) was not tested by removing fields one at
  a time; `capabilityEntry` therefore contains no field that is absent from
  both the reference and the upstream payload. `inputTokenLimit` /
  `outputTokenLimit` / `supportedGenerationMethods` / `temperature` / `topP` /
  `topK` are in the map entry because the contract requires them, but no
  evidence shows agy reads them there; `maxTokens`/`maxOutputTokens` are what
  it displays.
- **`--model` accepts the slug, and agy sends the slug.** `MODEL_PLACEHOLDER_M488`
  is what makes resolution work, but it never appears as `model` on the wire in
  our captures. Matching both is deliberate; only the slug path is
  【一手】.
- **Non-streaming custom calls were proved against a stub, not agy.** agy
  always used `streamGenerateContent?alt=sse` for the custom model in the
  acceptance run. `generateContent` translation is unit-tested only.
- **Tool *execution* is now verified for one tool (cycle-274 §7.6), not for all
  13.** A real MiniMax M3 turn emitted `run_command`, agy executed it, and the
  `functionResponse` came back with the matching `id`; that path is 【一手】. The
  other declarations (`view_file`, `invoke_subagent`, the write/edit family, …)
  were not individually exercised, and streamed *parallel* tool calls were
  stub-tested only.
- **Images, thoughts and context compaction are not forwarded.**
  `inlineData`/`fileData` parts are dropped by `ToChatRequest`, `thought`
  parts are dropped (no provider field), and there is no context injection or
  compaction. A vision prompt through a custom model therefore loses its
  attachments silently. `generationConfig.thinkingConfig` is forwarded to no
  provider.
- **`internal/models` Anthropic/Google providers ignore `Tools`.** Only the
  OpenAI-shaped providers (openai, agw, openrouter, ollama, custom) send
  declarations and parse calls. A registry entry on `anthropic` will get a
  tool-less prompt.
- **Capture is best-effort by design.** Queue overflow drops records
  (logged), bodies are capped at 16 MiB, and `close()` waits at most 5 s.
  A record is written after the handler returns, so a hard kill mid-stream
  loses that pair. Prompt text is stored verbatim — the capture directory is
  `0700`, the files `0600`, but they are unredacted by intent.
- **`doctor`'s upstream probe is a `GET /`.** A 404 proves DNS and TLS, not
  that `fetchAvailableModels` will succeed; no auth is sent.
- **No binary-patch fallback.** The contract allows `agy wrapper patch` behind
  a subcommand; it is **not implemented** (no need — `CLOUD_CODE_URL` works).
  Patching the vendor binary is also exactly what the constraints forbid.
- **Windows is untested.** `syscall.WaitStatus` handling for signal exit codes
  is Unix-specific; `agybin`'s inode comparison compiles everywhere but the
  self-guard was only exercised on darwin.

## Follow-ups

1. Forward `inlineData`/`fileData` as OpenAI `image_url` content parts.
2. Carry `thought`/`thoughtSignature` through a provider field so replaying a
   Gemini-style transcript round-trips.
3. Add tools to the Anthropic and Google providers in `internal/models`.
4. `agy wrapper patch` (patch-based fallback), off by default.
5. Per-model reasoning / `thinkingConfig` mapping, and context compaction
   thresholds driven by `context_window`.
6. Redaction mode for capture (`OPENAGY_CAPTURE_REDACT=1`) before sharing
   captures outside the machine.
7. ~~Exercise a real tool call~~ — **done, see §7.6**. Remaining: exercise the
   other 12 declarations, and streamed *parallel* tool calls from a real model.
