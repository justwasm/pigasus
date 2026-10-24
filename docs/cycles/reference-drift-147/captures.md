# Reference drift 1.4.6 → 1.4.7 — captures

Raw evidence for `docs/cycles/reference-drift-147/deliverable.md`. Every number
here comes from a command run on this machine against the installed `agy`
**1.4.7** (`~/.local/bin/agy`).

```
$ ~/.local/bin/agy --version
1.4.7

$ shasum -a 256 ~/.local/bin/agy
b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32  /Users/gear/.local/bin/agy
```

Capture directories are `0700` and live **outside** the repository
(`/tmp/agy-cap-147`, `/tmp/agy-cap-sb`, `/tmp/agy-cap-agent*`); no capture file
is committed.

---

## 1. The capture harness

The reference is captured through the clone's own proxy
(`internal/agyproxy`), which launches the *real* binary with a local CloudCode
proxy in front — nothing in the vendor binary is patched:

```sh
cd /tmp/agy-agent-ws
OPENAGY_CAPTURE_DIR=/tmp/agy-cap-147 ~/.local/bin/agy -p 'x' --output-format stream-json
```

The capture directory holds one JSON file per upstream request. The main turn's
request body is
`/tmp/agy-cap-147/1792857207929-026-streamGenerateContent.json`; the
`streamGenerateContent` bodies with no `tools` are the title generator.

| capture dir | what it ran |
|---|---|
| `/tmp/agy-cap-147` | the main agent, no sandbox |
| `/tmp/agy-cap-sb` | the main agent with `--sandbox` |
| `/tmp/agy-cap-agent` | `--agent test`, Gemini model |
| `/tmp/agy-cap-agent2` | `--agent test`, Gemini model, second agent body |
| `/tmp/agy-cap-agent3` | `--agent test`, Claude model |

## 2. The version and the changelog

```sh
$ ~/.local/bin/agy changelog | wc -c -l
 174284  1007
```

| measurement | value |
|---|---|
| bytes | 174,284 |
| lines | 1,007 |
| version sections | 77 |
| top-level bullets | 854 |
| sha256 | `b1b3cd19434ba3f80aa72eef672e97a6bbf5119ec9301b38afb73c29f3a9942f` |

The fixture is byte-identical to the live output:

```sh
$ ~/.local/bin/agy changelog | cmp - internal/cli/testdata/changelog_reference.txt && echo IDENTICAL
IDENTICAL
```

The **newest section is `1.4.6:`** — the 1.4.7 binary's feed has no `1.4.7:`
section yet. The diff against the 1.4.6 capture is +13 lines: the section
header, a blank line and **eleven** bullets.

The lead bullet of the new section:

```
1.4.6:
· Added a `Configure MCP servers...` row for installed plugins in `/plugin`, so you can set or change a plugin's MCP server variables at any time without disabling and re-enabling it; plugins with missing values show `⚠ needs setup`, each server shows `✓ configured` or the variables it still needs, and saving restarts an enabled plugin's servers so the new values take effect.
```

## 3. The `--help` surface

The golden is `internal/cli/testdata/help_1.4.6.txt` — 30 commands, stdout and
stderr captured separately, with a throwaway `HOME`.

```sh
$ diff internal/cli/testdata/help_1.4.6.txt /tmp/help_1.4.7.txt && echo IDENTICAL
IDENTICAL
```

**Byte-identical.** The flag table, the column widths and every per-command
usage block are unchanged.

## 4. The advertised surface (`init.tools`)

```sh
$ diff /tmp/advertised_147.txt internal/tools/testdata/advertised_tools_1.4.3.txt && echo IDENTICAL
IDENTICAL
```

Sixty names, byte-identical to the 1.4.3 / 1.4.5 / 1.4.6 capture
(sha256 `46dcfca2805e6efa56a3fa408cfb423636dbb2892b6072c5ee4b28094c300436`).

## 5. The model-facing declarations (the main surface)

### 5.1 Without `--sandbox`

```sh
$ python3 - <<'PY'
import json
cap = json.load(open('/tmp/agy-cap-147/1792857207929-026-streamGenerateContent.json'))
r = cap['requestBody']['request']
decls = [fd for t in r.get('tools', []) for fd in t.get('functionDeclarations', [])]
print(len(decls), [x['name'] for x in decls])
PY
12 ['read_file', 'run_command', 'manage_task', 'send_message', 'schedule', 'invoke_subagent', 'define_subagent', 'manage_subagents', 'write_file', 'edit_file', 'search_web', 'ask_question']
```

Compared field by field against `internal/tools/model_tools_1.4.6.json`:

| declaration | 1.4.7 vs 1.4.6 |
|---|---|
| `read_file` | same |
| `run_command` | **DIFF** — only `BypassSandbox` (see §5.2) |
| `manage_task` | same |
| `send_message` | same |
| `schedule` | same |
| `invoke_subagent` | same |
| `define_subagent` | same |
| `manage_subagents` | same |
| `write_file` | same |
| `edit_file` | same |
| `search_web` | same |
| `ask_question` | same |

```
live props  : ['CommandLine', 'IsDaemon', 'WaitMsBeforeAsync', 'toolSummary']
1.4.6 props : ['BypassSandbox', 'CommandLine', 'IsDaemon', 'WaitMsBeforeAsync', 'toolSummary']
live required: ['CommandLine', 'toolSummary', 'WaitMsBeforeAsync']
1.4.6 required: ['CommandLine', 'toolSummary', 'WaitMsBeforeAsync']
```

### 5.2 With `--sandbox`

```sh
$ python3 - <<'PY'
import json
cap = json.load(open('/tmp/agy-cap-sb/1792857313720-024-streamGenerateContent.json'))
decls = [fd for t in cap['requestBody']['request'].get('tools', []) for fd in t.get('functionDeclarations', [])]
old = json.load(open('internal/tools/model_tools_1.4.6.json'))
print(decls == old)
PY
True
```

**Byte-identical to the 1.4.6 capture.** The `BypassSandbox` parameter is
conditional on `--sandbox`, not on the version — the same finding the 1.4.6
cycle recorded.

## 6. The system prompt

The prompt is the `systemInstruction` of the captured request body. The
**eight static sections** of the 1.4.6 capture are byte-identical at 1.4.7:

| section | identical |
|---|---|
| `identity` | ✓ |
| `terminal_sandbox` | ✓ (only under `--sandbox`) |
| `artifacts` | ✓ |
| `conversation_transcript` | ✓ |
| `guidelines` | ✓ |
| `communication_style` | ✓ |
| `slash_commands` | ✓ |
| `messaging` | ✓ |

The environment-driven sections (`subagents`, `user_rules`, `skills`,
`user_information`) differ because this machine's skill set and this
repository's `AGENTS.md` changed — not because the reference changed.

The full 1.4.7 prompt (sandboxed) is **58,601 B**; the 1.4.6 capture is
70,853 B. The difference is the environment, not the reference.

## 7. The custom-agent surface — the one real change

```sh
$ cd /tmp/agy-agent-ws
$ OPENAGY_CAPTURE_DIR=/tmp/agy-cap-agent ~/.local/bin/agy --agent test -p 'x' --output-format stream-json
```

The agent file was deliberately written to *demand* the old parameters:

```markdown
---
name: test
description: A test agent used to capture the reference's agent tool declarations.
---

When you call a tool, you must include a `toolSummary` (a brief 2-5 word noun
phrase describing the specific task) and a `toolAction` (a brief 2-5 word
phrase in -ing form describing the specific action).
```

The reference **ignored the demand**. The seven declarations it sent:

| # | name | 1.4.4 capture | 1.4.7 capture |
|---|---|---|---|
| 1 | `send_message` | old wording, `toolSummary`+`toolAction` required | new wording, `Recipient`+`Message` required |
| 2 | `view_file` | same | same |
| 3 | `read_url_content` | same | same |
| 4 | `search_web` | same | same |
| 5 | `schedule` | old wording | new wording, `ExactTiming` added |
| 6 | `generate_image` | old wording | new wording, `enum` on `AspectRatio` |
| 7 | `manage_task` | old wording | new wording |

Field by field, against the 1.4.4 capture:

```
--- send_message ---
   description CHANGED
     1.4.4: Send a message to another agent. This tool can be used to communicate with subagents, peer agents, etc. Do not
     1.4.7: Send a message to another agent, such as a subagent or a peer. Not a channel to the user.
   required: ['Recipient', 'Message', 'toolSummary', 'toolAction'] -> ['Recipient', 'Message']
   props removed: ['toolAction', 'toolSummary'] added: []
   prop Message: {'minLength': (None, '1'), 'description': ('The message content.', 'Content of the message.')}
   prop Recipient: {'minLength': (None, '1'), 'description': ('The recipient ID to send the message to, e.g. a subagent conversation ID.', 'Conversation ID of the agent to message.')}
--- view_file ---   description same
   required: ['AbsolutePath', 'toolSummary', 'toolAction'] -> ['AbsolutePath']
   props removed: ['toolAction', 'toolSummary'] added: []
--- read_url_content ---   description same
   required: ['Url', 'toolSummary', 'toolAction'] -> ['Url']
   props removed: ['toolAction', 'toolSummary'] added: []
--- search_web ---   description same
   required: ['query', 'toolSummary', 'toolAction'] -> ['query']
   props removed: ['toolAction', 'toolSummary'] added: []
--- schedule ---   description CHANGED
   required: ['Prompt', 'toolSummary', 'toolAction'] -> ['Prompt']
   props removed: ['toolAction', 'toolSummary'] added: ['ExactTiming']
   prop CronExpression: {'description': (...)}   # reworded
   prop DurationSeconds: {'description': (...)}  # reworded
   prop IsDaemon: {'description': (...)}         # reworded
   prop MaxIterations: {'description': (...)}    # reworded
   prop Prompt: {'description': (...)}           # reworded
   prop TimerCondition: {'description': (...)}   # reworded
--- generate_image ---   description CHANGED
   required: ['Prompt', 'ImageName', 'toolSummary', 'toolAction'] -> ['Prompt', 'ImageName']
   props removed: ['toolAction', 'toolSummary'] added: []
   prop AspectRatio: {'enum': (None, ['1:1', '2:3', '3:2', '3:4', '4:3', '9:16', '16:9'])}
   prop ImageName: {'minLength': (None, '1')}
   prop ImagePaths: {'maxItems': (None, '3')}
   prop Prompt: {'minLength': (None, '1')}
--- manage_task ---   description CHANGED
   required: ['Action', 'toolSummary', 'toolAction'] -> ['Action']
   props removed: ['toolAction', 'toolSummary'] added: []
```

### 7.1 The model does not cause the difference

The same agent, the same workspace, two different models:

| capture | `model_enum` | `used_claude` | `view_file` mentions `pdf` |
|---|---|---|---|
| `/tmp/agy-cap-agent` | `MODEL_PLACEHOLDER_M318` | false | yes |
| `/tmp/agy-cap-agent3` | `MODEL_PLACEHOLDER_M35` | **true** | **no** |

Only `view_file`'s *description* differs, and it differs by the **binary
types** the model can read (`image, pdf, video, audio` vs `image, video`) —
a per-model capability list, not a surface change. The seven names, their
order, their `required` lists and the absence of `toolSummary`/`toolAction`
are identical in both.

### 7.2 The old wording is still in the binary

Both the old and the new `send_message` descriptions are in the 1.4.7 binary
(`strings` hits). The old one belongs to the reference's **subagent** surface,
not to the custom-agent surface — a `strings` hit proves nothing about which
surface a declaration is on. Only a captured request body does.

### 7.3 The clone's wire matches

The clone's own `--agent test` request body, captured through the same proxy:

```sh
$ diff <(python3 /tmp/agent_decls.py /tmp/agy-cap-clone /dev/stdout) \
       <(python3 /tmp/agent_decls.py /tmp/agy-cap-agent /dev/stdout) && echo IDENTICAL
IDENTICAL
```

A direct JSON comparison of the two declaration sets: `True` — byte for byte.

## 8. The remote-control descriptors

Extracted with `descriptor_pb2` from the raw binary (`out/local-macos/extract_descriptors.py`,
`/tmp/extract_proto.py`):

| file | offset | descriptor bytes | methods | messages | enums |
|---|---|---:|---:|---:|---:|
| `google/gca/aicode/v1main/remote_agent_proxy.proto` | `0x3b2124f` | 46,301 | 79 | 174 | 5 |
| `third_party/jetski/remote_agent_proxy_pb/remote_agent_proxy.proto` | `0x37ab385` | 4,361 | 0 | 10 | 3 |

The two RPCs appended at 1.4.7:

```
78  ListAgents               ListAgentsRequest               ListAgentsResponse
79  GetChangelistStatus      GetChangelistStatusRequest      GetChangelistStatusResponse
```

Their message shapes:

```proto
message ListAgentsRequest {
}

message ListAgentsResponse {
  repeated PersistentAgent agents = 1;
}

message GetChangelistStatusRequest {
  repeated int64 cl_numbers = 1;
  bool include_content = 2;
}

message GetChangelistStatusResponse {
  repeated exa.language_server_pb.ChangelistStatus statuses = 1;
}
```

The internal descriptor changed too — two fields on `ConfigOverrides`:

```
> field {
>   name: "is_cel_only"
>   number: 8
>   label: LABEL_OPTIONAL
>   type: TYPE_BOOL
> }
> field {
>   name: "cel_type"
>   number: 9
>   label: LABEL_OPTIONAL
>   type: TYPE_STRING
> }
```

`is_cel_only` / `cel_type` are a CEL (Common Expression Language) evaluation
hook on the environment's config overrides.

The committed `.proto` rendering was produced with the repository's own
`out/local-macos/render_proto.py`, which reproduces the committed 1.4.6
rendering byte for byte:

```sh
$ python3 out/local-macos/render_proto.py out/local-macos/protos/remote_agent_proxy_1.4.6.txt 1.4.6 /tmp/r146.proto
$ diff out/local-macos/protos/remote_agent_proxy_1.4.6.proto /tmp/r146.proto && echo IDENTICAL
IDENTICAL
```

## 9. Fixture digests

| file | sha256 |
|---|---|
| `internal/cli/testdata/changelog_reference.txt` | `b1b3cd19434ba3f80aa72eef672e97a6bbf5119ec9301b38afb73c29f3a9942f` |
| `internal/agenttools/tools_1.4.7.json` | `4a5d7db676e72164712bdff02d4b8465c0f2954b0764852ce83889dc98a14a71` |
| `internal/agenttools/testdata/agent_decls_1.4.7.json` | `4a5d7db676e72164712bdff02d4b8465c0f2954b0764852ce83889dc98a14a71` |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.7.txt` | `b9349945add96fb63ccece5e332e81f5b029588aef4194024f341fab7da985e6` |
| `out/local-macos/protos/remote_agent_proxy_1.4.7.txt` | `c45294d544119a8d16ddbf59ab6e269569d0934da33dfbda4a6570201a5901ef` |
| `out/local-macos/protos/remote_agent_proxy_pb_1.4.7.txt` | `e18ac4ea487ddeb3267c162e03fcc55e1d4f79a39a15b52859a87069206ff217` |
| `internal/tools/model_tools_1.4.6.json` (unchanged) | `8a7173c6579ee076f131b2f9d8866731c3c22fba62fd704957a33a156cc49ed9` |
| `internal/cli/testdata/help_1.4.6.txt` (unchanged) | `3192e2970d18f99fb426f793e69e96a5ae3f63563a800bea38dbf6989c52bd77` |
| `internal/tools/testdata/advertised_tools_1.4.3.txt` (unchanged) | `46dcfca2805e6efa56a3fa408cfb423636dbb2892b6072c5ee4b28094c300436` |

## 10. What was *not* re-captured

* `internal/tools/testdata/result_texts_1.4.4.txt` — the 32 tool result texts
  need a tool-using turn answered by a mock, under a pty harness. Not redone.
* `out/local-macos/seeds/` and everything derived from it — the
  reverse-engineering pipeline is a separate cycle. Every address moves on
  every release.
* `internal/agyprompt/testdata/capture_1.4.6.json` — the prompt's *inputs*.
  The skill list has to be re-discovered from the filesystem, not parsed back
  out of the rendered prompt, so the pair stays at the 1.4.6 inputs.
