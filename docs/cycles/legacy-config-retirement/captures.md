# Captures — cycle `legacy-config-retirement`

All of these are 【一手】: run on this machine (macOS/arm64) against the
binaries built from this tree (`/tmp/wagyu` from `./cmd/wagyu`,
`/tmp/agy-wrapper` from `./cmd/agy`).

## 1. The repro script

```sh
cd docs/cycles/legacy-config-retirement/repro
./wg01_config_retirement.sh /tmp/wagyu /tmp/agy-wrapper ./captures
```

### 1.1 Fixture 1 — the flat file, on its own

`OPENAGY_PROVIDERS_CONFIG` points at a file that does not exist,
`OPENAGY_MODELS_CONFIG` at a flat `models.json` with one entry (the old
shape, `"provider": "agw"`).

```
== fixture 1: flat models.json only ==
Fetching available models...
gemini-3.8-flash-high	Gemini 3.8 Flash (High)
gemini-3.8-flash-medium	Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low	Gemini 3.8 Flash (Low)
claude-opus-4-6	Claude Sonnet 4.6 (Thinking)
claude-opus-4-6-thinking	Claude Opus 4.6 (Thinking)
gpt-oss-120b-medium	GPT-OSS 120B (Medium)
registry: /var/folders/.../wg01-config.6G7dpi/absent-providers.json
no models configured
```

The flat file is inert: the clone prints only the reference's six rows, the
wrapper says `no models configured`.

### 1.2 Fixture 2 — the provider form

The same model, in `providers.json`.

```
== fixture 2: providers.json ==
Fetching available models...
gemini-3.8-flash-high	Gemini 3.8 Flash (High)
gemini-3.8-flash-medium	Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low	Gemini 3.8 Flash (Low)
claude-opus-4-6	Claude Sonnet 4.6 (Thinking)
claude-opus-4-6-thinking	Claude Opus 4.6 (Thinking)
gpt-oss-120b-medium	GPT-OSS 120B (Medium)
minimax-m3	MiniMax M3
registry: /var/folders/.../wg01-config.6G7dpi/providers.json
minimax-m3           MiniMax M3               provider=openai     model=MiniMax-M3           placeholder=MODEL_PLACEHOLDER_M488 slug=custom-minimax-m3
                     context_window=1000000
```

Both front-ends read the same file, and the catalogue row is the same in
both.

## 2. The live machine (this user's own configuration)

`~/.config/openagy/providers.json` — written by the migration, `0600`:

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

The old `models.json` is kept as `models.json.bak`. Nothing reads it.

### 2.1 The clone, print path

```
$ /tmp/wagyu --model "MiniMax M3.1 Flash" --print "reply with the single word: pong"
pong

$ /tmp/wagyu --model "MiniMax M3" --print "reply with the single word: pong"
pong
```

### 2.2 The wrapper (the real `agy` 1.4.5 + the local proxy)

```
$ /tmp/agy-wrapper --model "MiniMax M3.1 Flash" -p "reply with the single word: pong"
pong
exit=0
```

### 2.3 The catalogues

```
$ /tmp/wagyu models
Fetching available models...
gemini-3.8-flash-high	Gemini 3.8 Flash (High)
gemini-3.8-flash-medium	Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low	Gemini 3.8 Flash (Low)
claude-opus-4-6	Claude Sonnet 4.6 (Thinking)
claude-opus-4-6-thinking	Claude Opus 4.6 (Thinking)
gpt-oss-120b-medium	GPT-OSS 120B (Medium)
minimax-m3	MiniMax M3
minimax-m3.1-flash	MiniMax M3.1 Flash

$ /tmp/agy-wrapper wrapper models list
registry: /Users/gear/.config/openagy/providers.json
minimax-m3           MiniMax M3               provider=openai     model=MiniMax-M3           placeholder=MODEL_PLACEHOLDER_M488 slug=custom-minimax-m3
                     context_window=1000000
minimax-m3.1-flash   MiniMax M3.1 Flash       provider=openai     model=MiniMax-M3.1-Flash   placeholder=MODEL_PLACEHOLDER_M573 slug=custom-minimax-m3.1-flash
                     context_window=1048576
```

### 2.4 The wrapper's doctor

```
$ /tmp/agy-wrapper wrapper doctor
wrapper     0.1.0 (cycle 274)
agy         /Users/gear/.local/bin/agy (1.4.5)
note        built against 1.4.2; CLOUD_CODE_URL support is verified there
registry    /Users/gear/.config/openagy/providers.json (2 models)
            minimax-m3       MiniMax M3           openai     → MiniMax-M3           MODEL_PLACEHOLDER_M488
            minimax-m3.1-flash MiniMax M3.1 Flash   openai     → MiniMax-M3.1-Flash   MODEL_PLACEHOLDER_M573
config      /Users/gear/.openagy/wrapper.json
log         /Users/gear/.openagy/wrapper.log
upstream    https://daily-cloudcode-pa.googleapis.com
port        127.0.0.1:0 (ephemeral; CLOUD_CODE_URL=http://127.0.0.1:63504)
capture     /Users/gear/.openagy/captures (on)
            35659 files, 7.8 GiB
probe       https://daily-cloudcode-pa.googleapis.com: HTTP 404 in 1.06s
```

The `registry` line names one file. Before this cycle it named two (the
provider-centric one and the flat one).

## 3. Before the change — the same machine, the flat file only

For the record, what the wrapper answered while the flat file was the only
configuration (the 502 the user reported, reproduced from the cycle
`model-picker-search` report):

```
⚠ Bad Gateway (code 502): provider: models: unknown provider "agw"
```

and the catalogue had the two MiniMax models, because the flat file was
read. Both are gone now: the catalogue comes from `providers.json`, and the
provider type is a dialect (`openai`), never a tenant name.
