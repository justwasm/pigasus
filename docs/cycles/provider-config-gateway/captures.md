# Cycle `provider-config-gateway` — captures

Everything below was captured on **2026-10-23** against the endpoint the
wrapper used, with the credential the configuration carries. The credential
is the literal dev sentinel `-`, so nothing secret is in this file.

The host is written as `gateway.example` in the prose and as its real name
in the raw transcripts; both are the same endpoint.

---

## 1. The Anthropic endpoint (一手)

The Messages API lives at **`<base>/v1/messages`**. The version segment
belongs to the dialect, not to the host — the two alternatives are 404:

```
$ curl -sS -o /dev/null -w "%{http_code}\n" -X POST https://<host>/anthropic/v1/messages \
    -H 'x-api-key: -' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
    -d '{"model":"MiniMax-M3","max_tokens":32,"messages":[{"role":"user","content":"say hi"}]}'
200

$ ... -X POST https://<host>/anthropic/messages   → 404
$ ... -X POST https://<host>/v1/messages          → 404
```

### The non-streaming response

```
$ curl -sS -X POST https://<host>/anthropic/v1/messages \
    -H 'x-api-key: -' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
    -d '{"model":"MiniMax-M3","max_tokens":32,"messages":[{"role":"user","content":"Reply with the single word: ok"}]}'

{"id":"0729b752ccbeac392bac9857f3d764d1","type":"message","role":"assistant","model":"MiniMax-M3",
 "content":[{"text":"ok","type":"text"}],
 "usage":{"input_tokens":55,"output_tokens":2,"cache_creation_input_tokens":0,
          "cache_read_input_tokens":128,"service_tier":"standard"},
 "stop_reason":"end_turn",
 "base_resp":{"status_code":0,"status_msg":""}}
```

The shape is the standard Messages one. `base_resp` is the gateway's own
addition and is ignored by the transport.

### The streaming response

```
$ curl -sS -N -X POST https://<host>/anthropic/v1/messages \
    -H 'x-api-key: -' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
    -d '{"model":"MiniMax-M3","max_tokens":32,"stream":true,
         "messages":[{"role":"user","content":"Reply with the single word: ok"}]}'

event: message_start
data: {"type":"message_start","message":{"id":"0729b75fbf9f17e7ff258eaae39670c9","type":"message","role":"assistant","content":[],"model":"MiniMax-M3","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0,"service_tier":"standard"},"service_tier":"standard"}}

event: ping
data: {"type":"ping"}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":55,"output_tokens":2,"cache_read_input_tokens":128,"service_tier":"standard"}}

event: message_stop
data: {"type":"message_stop"}
```

Notes that the transport depends on:

- the `ping` frame is present and carries no payload — the parser skips it;
- the token counts arrive in `message_delta`, **not** in `message_start`
  (where they are zero);
- `message_stop` is the terminal frame.

### The headers

```
$ curl -sS -D - -o /dev/null -X POST https://<host>/anthropic/v1/messages \
    -H 'x-api-key: -' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
    -d '{"model":"MiniMax-M3.1-Flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}'

HTTP/2 200
content-type: application/json; charset=utf-8
x-mm-request-id: 2023571767976534329_1792705621zzoeyl
x-session-id: 0fe3fa095fa1e47b5c5e4b08657e9665
trace-id: 0729b755b53310c39c21dc01c4bdedd9
```

The credential is `x-api-key`; there is no `Authorization` header on this
dialect. The clone sends `anthropic-version: 2023-06-01` and no
`Authorization` — the test `TestGatewayAnthropicRequestShape` pins both.

---

## 2. The OpenAI-shaped endpoint (一手)

The same host serves the Chat Completions shape at `<base>/v1/chat/completions`
with `Authorization: Bearer -`.

```
$ curl -sS -X POST https://<host>/v1/chat/completions \
    -H 'Authorization: Bearer -' -H 'content-type: application/json' \
    -d '{"model":"MiniMax-M3.1-Flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}'

{"id":"0729b8433cb48da3e83c5f0505a25d80",
 "choices":[{"finish_reason":"length","index":0,
   "message":{"role":"assistant","reasoning_content":"We need respond greeting. Could be concise, friendly. \"Hi! How"}}],
 "created":1792705859,"model":"MiniMax-M3.1-Flash","object":"chat.completion","usage":{...}}
```

### The model catalogue is not the accepted set

```
$ curl -sS https://<host>/v1/models

{"object":"list","data":[
 {"id":"deepseek-flash","object":"model","owned_by":"deepseek","name":"DeepSeek-V4.1-Flash",
  "context_window":1048576,"max_output_tokens":393216,
  "input_modalities":["text","image"],"output_modalities":["text"],
  "effort":{"supported_levels":["low","high","max"],"default_level":"high"},
  "api_capabilities":{"anthropic_messages":{"system_prompt_update":"in-history","tool_update":"addition-only"}}},
 {"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek","name":"DeepSeek-V4-Pro",
  "context_window":1048576,"max_output_tokens":393216,
  "input_modalities":["text"],"output_modalities":["text"],
  "effort":{"supported_levels":["low","high","max"],"default_level":"high"},
  "api_capabilities":{"anthropic_messages":{"system_prompt_update":"leading-only","tool_update":"leading-only"}}}]}
```

**Only two ids are advertised**, and neither is a MiniMax one — yet the
MiniMax ids are accepted by the chat endpoint. The endpoint's catalogue and
its accepted set are different things, which is why the clone's catalogue is
configuration and not a fetch of `/v1/models`.

Note the `api_capabilities.anthropic_messages` block: the endpoint
advertises that it speaks the Messages dialect, with its own rules for how
the system prompt and the tool definitions may be updated across turns
(`in-history` / `addition-only` / `leading-only`). **The clone does not
implement those update rules** — it sends the system prompt in the top-level
field on every request, which is the `leading-only` behaviour.

### Which ids are accepted

| model id | outcome |
|---|---|
| `MiniMax-M3` | 200 |
| `MiniMax-M3.1-Flash` | 200 |
| `MiniMax-M2.5` | 200 |
| `MiniMax-M2.7` | 200 |
| `MiniMax-M3.1` | **400** — `unknown model 'minimax-m3.1' (2013)` |

`MiniMax-M3.1` is refused: the id is `MiniMax-M3.1-Flash`, and the short
form is not an alias.

### The context window

The limit is 1048576 prompt tokens, exclusive:

| prompt tokens | outcome |
|---|---|
| 1 048 575 | accepted |
| 1 048 576 | **refused** — `invalid params, context window exceeds limit (2013)` |

`max_tokens` is accepted up to 393216 (the same number the catalogue
states as `max_output_tokens`).

---

## 3. The reference's `agy models` (一手)

The reference renders the list two ways, and the difference is only visible
in a terminal.

**Piped** (stdout is not a tty) — tab separated rows, and
`Fetching available models...` on **stderr**:

```
$ agy models | cat -A
gemini-3.8-flash-high^IGemini 3.8 Flash (High)$
gemini-3.8-flash-medium^IGemini 3.8 Flash (Medium)$
...
```

**Under a pty** — space aligned columns:

```
gemini-3.8-flash-high     Gemini 3.8 Flash (High)
gemini-3.8-flash-medium   Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low      Gemini 3.8 Flash (Low)
claude-sonnet-4-6         Claude Sonnet 4.6
claude-opus-4-6-thinking  Claude Opus 4.6 (Thinking)
gpt-oss-120b-medium       GPT-OSS 120B (Medium)
```

The aligned form is a `text/tabwriter` with **minwidth 0, tabwidth 8,
padding 2**. The widest catalogue id is `claude-opus-4-6-thinking` (24
characters), so with padding 2 every label starts at **column 26** — which
is what the clone reproduces.

---

## 4. The configuration as written

The provider section the wrapper's CLI writes:

```json
{
  "providers": {
    "gateway": {
      "name": "Gateway",
      "type": "openai",
      "base_url": "https://<host>/v1",
      "api_key": "$OPENAGY_GATEWAY_KEY",
      "timeout_seconds": 60,
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3",
          "context_window": 1000000,
          "default_max_tokens": 8192
        }
      ]
    },
    "anthropic-gateway": {
      "name": "Anthropic Gateway",
      "type": "anthropic",
      "base_url": "https://<host>/anthropic",
      "api_key": "$OPENAGY_GATEWAY_KEY",
      "models": [
        {
          "id": "minimax-m3-anthropic",
          "name": "MiniMax M3 (Anthropic)",
          "model": "MiniMax-M3",
          "context_window": 1000000
        }
      ]
    }
  }
}
```

The second provider resolves to `https://<host>/anthropic/v1/messages` and
speaks the Messages dialect; the first resolves to
`https://<host>/v1/chat/completions` and speaks Chat Completions. The same
credential serves both.

---

## 5. The clone, driven only by that file (一手)

`/tmp/wagyu` with `OPENAGY_PROVIDERS_CONFIG` pointing at the file above and
nothing else configured:

### The catalogue

```
$ /tmp/wagyu models | cat -A
gemini-3.8-flash-high^IGemini 3.8 Flash (High)$
gemini-3.8-flash-medium^IGemini 3.8 Flash (Medium)$
gemini-3.8-flash-low^IGemini 3.8 Flash (Low)$
claude-sonnet-4-6^IClaude Sonnet 4.6 (Thinking)$
claude-opus-4-6-thinking^IClaude Opus 4.6 (Thinking)$
gpt-oss-120b-medium^IGPT-OSS 120B (Medium)$
minimax-m3^IMiniMax M3$
minimax-m3-anthropic^IMiniMax M3 (Anthropic)$
minimax-m3.1-flash^IMiniMax M3.1 Flash$
```

The reference's six rows first, then the configured rows sorted by id. Piped,
so the rows are tab separated — under a pty they are space aligned, the way
the reference renders them.

### A turn, on each dialect

```
$ /tmp/wagyu -p "Reply with the single word: ok" --model minimax-m3
ok

$ /tmp/wagyu -p "Reply with the single word: ok" --model minimax-m3-anthropic
ok

$ /tmp/wagyu -p "Reply with the single word: ok" --model minimax-m3.1-flash
ok
```

### A tool-using turn, on each dialect

This is the one that exercises the role translation: the Anthropic dialect
sends the tool result back as a **user** turn holding a `tool_result` block,
because the Messages API has no `tool` role.

```
$ echo "hello-from-tool" > /tmp/probe.txt

$ /tmp/wagyu -p "Use the read_file tool to read /tmp/probe.txt, then tell me what it says." \
    --model minimax-m3-anthropic
The file contains a single line: `hello-from-tool`

$ /tmp/wagyu -p "Use the read_file tool to read /tmp/probe.txt, then tell me what it says." \
    --model minimax-m3
hello-from-tool
```

**Summary:**
- **Path:** `/tmp/probe.txt`
- **Size:** 1 line, 15 bytes
- **Contents:** `hello-from-tool`

