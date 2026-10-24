# Reference drift 1.4.5 → 1.4.6 — captures

Raw evidence for `docs/cycles/reference-drift-146/deliverable.md`. Every
number here comes from a command that was run on this machine against the
installed `agy` **1.4.6** (`~/.local/bin/agy`, `agy --version` → `1.4.6`).

Capture directories are `0700` and live **outside** the repository
(`/tmp/cap146*`); no capture file is committed.

---

## 1. The wrapper

The reference is captured through the cycle-274 wrapper, which launches the
*real* binary with a local CloudCode proxy in front (`CLOUD_CODE_URL`), so
nothing in the vendor binary is patched:

```sh
go build -o /tmp/agy-wrapper-146 ./cmd/agy
OPENAGY_CAPTURE_DIR=/tmp/cap146 /tmp/agy-wrapper-146 -p 'x'
```

The main capture produced 52 files in `/tmp/cap146`; the
`streamGenerateContent` request body is
`/tmp/cap146/1792754695779-026-streamGenerateContent.json`.

## 2. The version and the changelog

```sh
$ ~/.local/bin/agy --version
1.4.6

$ ~/.local/bin/agy changelog | wc -c -l
 171493   994 internal/cli/testdata/changelog_reference.txt
```

| measurement | value |
|---|---|
| bytes | 171,493 |
| lines | 994 |
| version sections | 76 |
| top-level bullets | 843 |
| sha256 | `4a2470be55d99589a1c86a6bbbd31a4bb45985e5515bda657c141ab281ce5914` |

The fixture is byte-identical to the live output:

```sh
$ ~/.local/bin/agy changelog | cmp - internal/cli/testdata/changelog_reference.txt && echo IDENTICAL
IDENTICAL
```

The **newest section is `1.4.5:`** — the 1.4.6 binary's feed has no `1.4.6:`
section yet. The diff against the 1.4.5 capture is +14 lines: the section
header, a blank line and the twelve bullets.

## 3. The system prompt

The prompt is the `systemInstruction` of the captured request body:

```sh
$ python3 - <<'PY'
import json
d = json.load(open('/tmp/cap146/1792754695779-026-streamGenerateContent.json'))
r = d['requestBody']['request']
print(len(r['systemInstruction'].get('parts', [{}])[0].get('text', '')))
PY
70853
```

Section boundaries and sizes (the fixture, `internal/agyprompt/testdata/capture_1.4.6.txt`):

| section | bytes |
|---|---:|
| `identity` | 477 |
| `terminal_sandbox` | 3,371 |
| `artifacts` | 3,718 |
| `conversation_transcript` | 1,969 |
| `guidelines` | 715 |
| `communication_style` | 524 |
| `slash_commands` | 1,839 |
| `messaging` | 877 |
| `subagents` | 2,107 |
| `user_rules` | 25,224 |
| `skills` | 29,260 |
| `user_information` | 623 |

### 3.1 The clone reproduces it byte for byte

The clone's builder was run against the capture's own inputs and compared with
`cmp`:

```
$ cmp /tmp/built_146.txt /tmp/prompt_146.txt && echo BYTE_IDENTICAL
BYTE_IDENTICAL
```

`/tmp/built_146.txt` is `agyprompt.Build(...)` with the fixture's
`os_version`, `workspaces`, `cwd`, `app_data_dir`, `conversation_id`,
`artifact_dir`, `subagents`, `skills` and `user_rules`; `/tmp/prompt_146.txt`
is the live section text.

### 3.2 The shared skill library

The nine entries of the shared library in the live `<skills>` section, with
their real paths (excerpt):

```
- agents-sdk (/Users/gear/.gemini/skills/agents-sdk/SKILL.md): …
- cloudflare (/Users/gear/.gemini/skills/cloudflare/SKILL.md): …
- cloudflare-email-service (/Users/gear/.gemini/skills/cloudflare-email-service/SKILL.md): …
- durable-objects (/Users/gear/.gemini/skills/durable-objects/SKILL.md): …
- sandbox-sdk (/Users/gear/.gemini/skills/sandbox-sdk/SKILL.md): …
- terminal-browser (/Users/gear/.gemini/skills/terminal-browser/SKILL.md): …
- web-perf (/Users/gear/.gemini/skills/web-perf/SKILL.md): …
- workers-best-practices (/Users/gear/.gemini/skills/workers-best-practices/SKILL.md): …
- wrangler (/Users/gear/.gemini/skills/wrangler/SKILL.md): …
```

The remaining roots in the same section: the builtin bundle
(`~/.gemini/antigravity-cli/builtin/skills`), the plugin roots
(`~/.gemini/config/plugins/*/skills`).

### 3.3 The `<subagents>` delta

Against the 1.4.4 capture the list grew by exactly 50 bytes — the two
**workspace agents of this repository**:

```
- alpha: Alpha demo agent
- zeta: Zeta demo agent
```

Their source is local:

```
$ cat .agents/agents/alpha.md
---
name: alpha
description: Alpha demo agent
---
body

$ cat .agents/agents/zeta.md
---
name: zeta
description: Zeta demo agent
---
你是 Zeta, 一个擅长讲笑话的 agent
```

The descriptions match the prompt verbatim, so the reference lists every
discovered workspace custom agent in `<subagents>` after the builtin three
(`self`, `research`, `image-generator`). The clone discovers them the same
way (`customSubagents()` → `agent.Discover()`), and the capture is a
cross-check of that discovery: the reference listed exactly the two agents
this workspace has.

## 4. The custom-agent probes

Five agents were written into throwaway workspaces and the reference was run
against each:

| workspace | agent file | body |
|---|---|---|
| `/tmp/agentws` | `.agents/agents/probeagent.md` | `# Probe agent` + text |
| `/tmp/agentws2` | `.agents/agents/zetaagent.md` | `# Totally Different Heading` + text |
| `/tmp/agentws3` | `.agents/agents/nohead.md` | text only |
| `/tmp/agentws4` | `.agents/agents/midhead.md` | preamble + `# Later Heading` + text |
| `/tmp/agentws5` | `.agents/agents/multi.md` | preamble + `# One` + `## Two` + `### Three` + `## Two Again` |

```sh
cd /tmp/agentws && OPENAGY_CAPTURE_DIR=/tmp/cap146agent /tmp/agy-wrapper-146 --agent probeagent -p 'x'
```

The captured `systemInstruction` prefixes:

| probe | the reference's wrapper |
|---|---|
| `probeagent` | `<Probe agent>\nAnswer in one short sentence.\n</Probe agent>\n<user_information>` |
| `zetaagent` | `<Totally Different Heading>\nAnswer with one word.\n</Totally Different Heading>\n<user_information>` |
| `nohead` | `<System Prompt>\nAnswer with one word only.\n</System Prompt>\n<user_information>` |
| `midhead` | `<System Prompt>\nIntro line first.\n</System Prompt>\n<Later Heading>\nAnswer with one word.\n</Later Heading>\n<user_information>` |
| `multi` | `<System Prompt>\nPreamble text.\n</System Prompt>\n<One>\nBody one.\n\n## Two\nBody two.\n\n### Three\nBody three.\n\n## Two Again\nBody four.\n</One>\n<user_information>` |

The 1.4.6 agent prompt carries the same seven sections as the 1.4.4 one
(`user_information`, `user_rules`, `skills`, `messaging`, `artifacts`,
`guidelines`, `communication_style`) — **no** `terminal_sandbox` — and the
same seven tool declarations (`send_message`, `view_file`,
`read_url_content`, `search_web`, `schedule`, `generate_image`,
`manage_task`).

## 5. The model-facing declarations

```sh
$ python3 - <<'PY'
import json
d = json.load(open('/tmp/cap146/1792754695779-026-streamGenerateContent.json'))
r = d['requestBody']['request']
decls = [fd for t in r.get('tools', []) for fd in t.get('functionDeclarations', [])]
print(len(decls), [x['name'] for x in decls])
PY
12 ['read_file', 'run_command', 'manage_task', 'send_message', 'schedule', 'invoke_subagent', 'define_subagent', 'manage_subagents', 'write_file', 'edit_file', 'search_web', 'ask_question']
```

`diff internal/tools/model_tools_1.4.5.json internal/tools/model_tools_1.4.6.json`:

```
35a36,39
>         "BypassSandbox": {
>           "type": "BOOLEAN",
>           "description": "Set to true to run outside the sandbox (requires elevated user permission for auth exchange, network, or port binding)."
>         },
```

The eleven unchanged declarations were confirmed by re-serialising the
capture and comparing the JSON values one by one — all eleven `SAME`, only
`run_command` `DIFF`.

The advertised surface (`init.tools`) is unchanged:

```sh
$ agy -p 'x' --output-format stream-json | head -1   # the init event
```

Sixty names, byte-identical to `internal/tools/testdata/advertised_tools_1.4.3.txt`
(sha256 `46dcfca2805e6efa56a3fa408cfb423636dbb2892b6072c5ee4b28094c300436`).

## 6. The remote-control descriptors

Extracted with `descriptor_pb2` from the raw binary — the descriptors are
uncompressed in `__TEXT,__const`:

| file | offset | descriptor bytes | methods | messages | enums |
|---|---|---:|---:|---:|---:|
| `google/gca/aicode/v1main/remote_agent_proxy.proto` | `0x3a3efd7` | 45,388 | 77 | 170 | 5 |
| `third_party/jetski/remote_agent_proxy_pb/remote_agent_proxy.proto` | `0x36c7311` | 4,284 | 0 | 10 | 3 |

The five RPCs appended at 1.4.6:

```
73  SearchFiles               SearchFilesRequest               SearchFilesResponse
74  StartLocalPluginSignIn    StartLocalPluginSignInRequest    StartLocalPluginSignInResponse
75  CompleteLocalPluginSignIn CompleteLocalPluginSignInRequest CompleteLocalPluginSignInResponse
76  CreateAgent               CreateAgentRequest               CreateAgentResponse
77  GetAgent                  GetAgentRequest                  GetAgentResponse
```

No RPC was removed. The internal descriptor is **byte-identical** to the
1.4.5 capture:

```sh
$ cmp out/local-macos/protos/remote_agent_proxy_pb_1.4.5.txt out/local-macos/protos/remote_agent_proxy_pb_1.4.6.txt && echo IDENTICAL
IDENTICAL
```

(sha256 `883e0633579911e2cce497c6e2d2dca5f4f26ed3351447ae938b895f85840b35`
— the same digest the 1.4.5 lock entry carried.)

## 7. The seed set and the Ghidra rebuild

```sh
$ cd rea-go-seeds && go run . ~/.local/bin/agy ../out/local-macos/seeds
recovered 161286 function names
```

| measurement | 1.4.5 | 1.4.6 |
|---|---:|---:|
| functions | 160,664 | 161,286 |
| seeds.txt sha256 | `3d660aa4…` | `0844c0f1f575ceafdbd6cb7359701982f16908e3a05f907f13f14bb715cdd319` |
| funcs.txt sha256 | `eea2f961…` | `b55db63fd7836624298ba73aed838a4f26809201301ec1faaaa2ce50de0950b6` |

Ghidra 12.1.4, `MAXMEM=8G`, `-noanalysis`, 4 CPUs:

```
INFO  ReaGhidraApplySeeds.java> REA seeds applied: {"entries":161286,"function_created":161267,"function_existing":19,"function_failed":0,…}
INFO  DecompilePriority.java> DecompilePriority DONE: 2290 processed, 2290 ok, 0 err, 0 skip in 31 sec
```

`out/local-macos/build_reports.py` then produced:

```
summary + 102 per-package reports -> /Users/gear/Documents/GitHub/openagy/out/local-macos
2290/2290 decompiled, 335,020 LOC
```

## 8. The registry

After the rewrite and the reseal (`-update` recomputes the digests only):

```sh
$ go test ./internal/referencedrift/
ok  	github.com/justwasm/pigasus/internal/referencedrift
```

The gate's own report:

```
reference-drift: live /Users/gear/.local/bin/agy is 1.4.6; this clone is pinned to 1.4.6 (lag acknowledged through 1.4.6).
  SUSPECT  internal/tools/testdata/result_texts_1.4.4.txt — captured at agy 1.4.4, unverified against 1.4.6
```

## 9. Fixture digests

| file | sha256 |
|---|---|
| `internal/cli/testdata/changelog_reference.txt` | `4a2470be55d99589a1c86a6bbbd31a4bb45985e5515bda657c141ab281ce5914` |
| `internal/agyprompt/testdata/capture_1.4.6.txt` | `4443318b8dad0fb895187a6cb36f1552aad4a35f1c006d438173e2ed1242a7cc` |
| `internal/agyprompt/testdata/capture_1.4.6.json` | `faf60288a629245f9827bbea275a6ff38719b077f2dba16c753a79abdf1c3f0c` |
| `internal/tools/model_tools_1.4.6.json` | `8a7173c6579ee076f131b2f9d8866731c3c22fba62fd704957a33a156cc49ed9` |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.6.txt` | `5b80624aa214e3967eee332a13ee8756ccbb9bfc5ef683b046c61a676368e7b1` |
| `out/local-macos/protos/remote_agent_proxy_1.4.6.txt` | `646c0f7419621cc41c747e54d3dc938088c0ae75d59c62166c9be316579274d6` |
| `out/local-macos/protos/remote_agent_proxy_pb_1.4.6.txt` | `883e0633579911e2cce497c6e2d2dca5f4f26ed3351447ae938b895f85840b35` |
| `out/local-macos/seeds/seeds.txt` | `0844c0f1f575ceafdbd6cb7359701982f16908e3a05f907f13f14bb715cdd319` |
| `out/local-macos/seeds/funcs.txt` | `b55db63fd7836624298ba73aed838a4f26809201301ec1faaaa2ce50de0950b6` |
