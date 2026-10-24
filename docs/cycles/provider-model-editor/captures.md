# Cycle `provider-model-editor` — captures

**Date**: 2026-10-23
**Machine**: macOS/arm64
**Binary**: `go build -o /tmp/wagyu ./cmd/wagyu`
**Fixture**: a two-provider configuration under `$TMPDIR` (see
`repro/wg01_provider_panel.sh`); the verification captures run the same
binary against a local mock endpoint (`repro/wg02_provider_verify.sh`)

Everything below is 【一手】 — a live run of the built binary, not a
reconstruction.

---

## 1. The CLI

```
$ agy provider add gateway --base-url https://gw.example/v1 --api-key $GW_KEY --name Gateway --model minimax-m3=MiniMax-M3
added provider gateway (type=openai endpoint=https://gw.example/v1/chat/completions)
added model minimax-m3 under gateway (→ MiniMax-M3)

$ agy provider list
providers: /tmp/capcli/providers.json
gateway              Gateway                  type=openai     base=https://gw.example/v1            models=1
                       minimax-m3           model=MiniMax-M3           ctx=0

$ agy provider show gateway
provider: gateway
  name:        Gateway
  type:        openai
  base_url:    https://gw.example/v1
  api_url:     (not set)
  endpoint:    https://gw.example/v1/chat/completions
  api_key:     $GW_KEY
  models:
    minimax-m3           minimax-m3               model=MiniMax-M3               ctx=0

$ agy model add minimax-m3.1 --provider gateway --model MiniMax-M3.1 --name "MiniMax M3.1" --context-window 1048576
added minimax-m3.1 under gateway (→ MiniMax-M3.1)

$ agy provider add gateway --base-url https://gw2.example/v1   # merge: the credential survives
added provider gateway (type=openai endpoint=https://gw2.example/v1/chat/completions)

$ cat providers.json
{
  "providers": {
    "gateway": {
      "id": "gateway",
      "name": "Gateway",
      "base_url": "https://gw2.example/v1",
      "api_key": "$GW_KEY",
      "models": [
        {
          "id": "minimax-m3",
          "model": "MiniMax-M3"
        },
        {
          "id": "minimax-m3.1",
          "name": "MiniMax M3.1",
          "model": "MiniMax-M3.1",
          "context_window": 1048576
        }
      ]
    }
  }
}
```

Three things this pins:

1. **the merge** — the second `provider add` changed the endpoint and left
   the credential and both models alone;
2. **the shape** — the key order is the order the fields were written in,
   and the unknown fields (there are none here, but see the unit test in
   `internal/models/edit_test.go`) survive;
3. **the endpoint derivation** — `--base-url https://gw.example/v1` with
   the `openai` protocol gives `…/v1/chat/completions`.

### The error paths

```
$ agy provider show nope
Error: no provider named "nope"
exit=1

$ agy model add x --provider nope
Error: no provider named "nope" (create it with: wagyu provider add nope --base-url …)
exit=1
```

### The credential is never printed

```
$ agy provider add gateway --api-key sk-literal
$ agy provider show gateway
provider: gateway
  …
  api_key:     **** (literal, hidden)
```

An environment reference is shown as it is written (`$GW_KEY`) — it names
a variable, not a secret.

---

## 2. The parity surfaces

`provider` and `model` are hidden: `agy --help` still lists exactly the
reference's twelve visible subcommands.

```
$ agy --help | sed -n '/Available subcommands/,$p'
Available subcommands:
  agent           List available agents
  agents          List available agents
  changelog       Show changelog and release notes
  help            Show help for subcommands
  install         Configure environment paths and shell settings
  mcp             Manage MCP servers (add, remove, list, enable, disable)
  mic-serve       Serve this machine's microphone to a CLI on another host
  models          List available models
  plugin          Manage plugins (install, uninstall, list, enable, disable)
  plugins         Alias for plugin
  remote-control  Manage the remote-control background daemon (start, status, stop)
  update          Update CLI
```

The picker's catalogue is unchanged as well: `internal/tuilib.DefaultSlashCommands`
still holds 99 entries and `/provider` is not among them, so the
"↓ 94 more" hint does not move.

---

## 3. The `/provider` panel

Captured with `repro/wg01_provider_panel.sh`: the built binary runs in a
tmux pane at 120×40 with `--backend shim` (no network), the panel is
opened by typing `/provider`, and the pane is captured with
`tmux capture-pane -p`.

### 3.1 The list

```
Providers

> gateway                 openai     https://gw.example/v1/chat/completions
    minimax-m3              MiniMax M3               model=MiniMax-M3               ctx=1048576
  local                   openai     http://127.0.0.1:11434/v1/chat/completions
  builtin (Google CloudCode)
    gemini-3.8-flash-high   Gemini 3.8 Flash (High)
    gemini-3.8-flash-medium Gemini 3.8 Flash (Medium)
    gemini-3.8-flash-low    Gemini 3.8 Flash (Low)
    claude-sonnet-4-6       Claude Sonnet 4.6 (Thinking)
    claude-opus-4-6-thinking Claude Opus 4.6 (Thinking)
    gpt-oss-120b-medium     GPT-OSS 120B (Medium)

  /tmp/…/providers.json
Keyboard: ↑/↓ Navigate  a Add provider  m Add model  enter Edit  d Delete  esc Exit
```

The configured providers come first, each with its models indented under
it; the reference's own catalogue is the read-only group at the end. The
`>` cursor is on the first row.

### 3.2 The edit form (`enter`)

```
Provider · gateway

> Name                Gateway
  Type                openai
  Base URL            https://gw.example/v1
  API URL
  API Key             $OPENAGY_GATEWAY_KEY
  Timeout (s)         0
  Max retries         0

Keyboard: ↑/↓ Field  ←/→ Change  enter Save  esc Cancel
```

The credential is an environment reference here, so it is shown. A
*literal* is not: the field starts empty, the row reads
`(hidden — leave empty to keep)`, and an empty field keeps the stored
value (pinned by `TestProviderPanel_EditKeepsTheCredential`).

### 3.3 The add-model form (`m`)

```
Add model · gateway

> ID                  the name you pass to --model and /model
  Name
  Upstream id
  Context window      0
  Max output tokens   0
  Reasoning effort    (unset)

Keyboard: ↑/↓ Field  ←/→ Change  enter Save  esc Cancel
```

### 3.4 The delete confirmation (`d`)

The first press arms it:

```
Providers

  gateway                 openai     https://gw.example/v1/chat/completions
    minimax-m3              MiniMax M3               model=MiniMax-M3               ctx=1048576
> local                   openai     http://127.0.0.1:11434/v1/chat/completions
  builtin (Google CloudCode)
    …

  press d again to delete
```

The second press carries it out, and the file loses the provider:

```
Providers

  gateway                 openai     https://gw.example/v1/chat/completions
    minimax-m3              MiniMax M3               model=MiniMax-M3               ctx=1048576
  builtin (Google CloudCode)
    …

  removed provider local
```

### 3.5 A write, end to end

Typing `←/→` on the *Type* field cycles the protocol, and `enter` saves:

```
Provider · gateway

  Name                Gateway
> Type                ‹ anthropic ›
  Base URL            https://gw.example/v1
  …
```

after the save:

```
Providers

> gateway                 anthropic  https://gw.example/v1/v1/messages
    minimax-m3              MiniMax M3               model=MiniMax-M3               ctx=1048576
  …
  saved provider gateway
```

and the file:

```json
{
  "providers": {
    "gateway": {
      "id": "gateway",
      "name": "Gateway",
      "type": "anthropic",
      "base_url": "https://gw.example/v1",
      "api_key": "$OPENAGY_GATEWAY_KEY",
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3",
          "context_window": 1048576
        }
      ]
    },
    "local": { … }
  }
}
```

The other provider, the models and the credential are untouched; only
`type` changed.

---

## 4. The verification

The probe is the panel's answer to *"does this endpoint accept this
credential?"*. The capture runs the built binary against a **local mock
endpoint** (a python http server, `repro/wg02_provider_verify.sh`), so no
request left the machine and the mock can be scripted to answer 401.

### The CLI

```
$ agy provider test gateway
✓ gateway: the model answered (stop)
$ echo $?
0

$ agy provider test wrongkey
✗ wrongkey: the credential was rejected (HTTP 401)
$ echo $?
1

$ agy provider test gateway --model MiniMax-M3
✓ gateway: the model answered (stop)

$ agy provider test nope
Error: no provider named "nope"
$ echo $?
1
```

### The panel — the probe in flight

```
Providers
> gateway                 openai     http://127.0.0.1:8101/v1/chat/completions
    minimax-m3              MiniMax M3               model=MiniMax-M3               ctx=0
  builtin (Google CloudCode)
    gemini-3.8-flash-high   Gemini 3.8 Flash (High)
    ...
  verifying gateway…
  /var/folders/…/wg02-verify.rASviw/providers.json
Keyboard: ↑/↓ Navigate  a Add provider  m Add model  enter Edit  d Delete  v Verify  esc Exit
```

### The panel — the verdict

```
  ✓ gateway: the model answered (stop)
```

### The panel — the form probes the *typed* credential

The credential field is the fourth one; the wrong key is typed into it and
`v` is pressed without saving:

```
  Name                Local gateway
  Type                openai
  Base URL            http://127.0.0.1:8101/v1
  API URL
> API Key             sk-wrong
  Timeout (s)         0
  Max retries         0
  ✗ gateway: the credential was rejected (HTTP 401)
Keyboard: ↑/↓ Field  ←/→ Change  enter Save  v Verify  esc Cancel
```

### The panel — a save starts a probe

```
  saved provider gateway
  verifying gateway…
```

and then:

```
  saved provider gateway
  ✓ gateway: the model answered (stop)
```

### The unit level

`internal/models/verify_test.go` (12 tests) pins the probe against an
`httptest` server:

- a chat probe when a model is named, a listing probe when it is not;
- the Anthropic listing path (`/anthropic/v1/models`) and the `x-api-key`
  header;
- an environment reference is expanded;
- **the caller's credential is used, not the stored one** — the panel
  verifies a key that has not been saved;
- a 401 and a 403 both report the rejected credential;
- an unreachable endpoint and a provider without an endpoint.

---

## 5. The unit level

`internal/models/edit_test.go` (9 tests), `internal/cli/provider_cmds_test.go`
(20 tests) and `cmd/wagyu/slash_panel_provider_test.go` (17 tests) pin the
behaviour that the captures show:

- the provider section is created when the file has none;
- an unknown field survives a write;
- the key order survives a write;
- a model is added and replaced;
- an unknown provider is an error, and the error names it;
- a model and a provider are deleted;
- no lock file is left behind;
- two concurrent writers leave a valid file;
- the panel keeps a literal credential, shows an environment reference,
  cycles a choice field, needs two presses to delete, and refuses to
  edit the built-in group;
- `/provider` is not in the picker's catalogue;
- `provider test` reports a verified provider, reports a rejected
  credential with a non-zero exit, names an unknown provider, and never
  prints the credential;
- the panel's probe starts on `v` and on a save, renders the verdict, and
  carries the credential the user has typed.

```
$ go test ./internal/models/ ./internal/cli/ ./cmd/wagyu/ ./cmd/agy/ ./cmd/openagy-models/
ok  	github.com/justwasm/pigasus/internal/models
ok  	github.com/justwasm/pigasus/internal/cli
ok  	github.com/justwasm/pigasus/cmd/wagyu
ok  	github.com/justwasm/pigasus/cmd/agy
ok  	github.com/justwasm/pigasus/cmd/openagy-models
```
