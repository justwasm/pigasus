# The IDE language server — the second reference binary

> The Antigravity **IDE** ships its own Go binary, `language_server`, built from
> the **same monorepo commit** as the `agy` CLI this repo clones. It is not the
> CLI — but it is a first-hand cross-reference for it, and it opens a surface
> the CLI cannot show (the IDE's own tool-safety layer, its prompt templates,
> its MCP manager, its model catalogue).
>
> Evidence and the full write-up: `out/ide-language-server/README.md` (the
> `out/` tree is not part of the published snapshot, so it is referenced by
> path, not by link).

## What it is

| | |
|---|---|
| Path | `/Applications/Antigravity.app/Contents/Resources/bin/language_server` |
| Size / SHA-256 | 161,803,376 B · `6f7e5004629070ac0c4651a1dc29030eaadcd5dc68fa9edf170047d7f8424f4d` |
| Go build info | `go1.28-20261015-RC01 cl/1000353293 +18a2e1f59d X:fieldtrack,boringcrypto,simd` |
| The CLI for comparison | `go1.28-20261015-RC02 cl/1000353293 +18a2e1f59d` — **same CL, same commit** |
| `.text` base | `0x100000f80` (the CLI's is `0x1000011c0` — **addresses are not comparable**) |
| pclntab | `__lrodata_gopcln`, 48.7 MB — the seed tool takes it as-is |
| App version | Antigravity 2.26.0 (`com.google.antigravity`) |

**Never mix its addresses with the CLI's.** Only the *names* are comparable —
and the names are the point.

## The headline result

Same function name, decompiled in **both** binaries, **the pseudocode comes out
line for line identical**:

| comparison | compared | identical | ≥95% similar | differs |
|---|---|---|---|---|
| same name | 10,285 | **5,309** | 3,390 | 1,338 |
| same function, two import paths | 100 | **42** | 37 | 21 |

Split by whether the function is hand-written or generated protobuf, the
difference is stark:

| | compared | identical | ≥95% | differs |
|---|---|---|---|---|
| **hand-written code** | 8,206 | **4,329** (53%) | 3,131 | **660 (8%)** |
| generated protobuf | 2,079 | 980 | 259 | **678 (33%)** |

So the *code* is 92% the same; the 33% on the proto side is the IDE's **schemas
growing** (new fields), not its logic changing.

**Where** they differ is also not scattered — it is whole packages:

| package | compared | differs |
|---|---|---|
| `cortex/policyguardian/vetting` | 201 | **47** |
| `enterprise` | 132 | **43** |
| `cortex/shared` | 115 | **42** |
| `cortex/cortex` | 373 | **41** |
| `cortex/command/sandboxproxy` | 182 | **41** |
| `language_server/code_assist_client` | 480 | **37** |

and 27 hand-written packages (≥8 functions each) are **identical throughout** —
`cortex/battleworkspace` (70), `cortex/messages` (58),
`cortex/snapshot_recorder` (45), `snapshot_recorder/gitclient` (39),
`gitignore` (31), `cortex/traj` (22).

**So: the two products share one agent kernel, and the IDE rewrote exactly the
product-facing blocks — the vetting gate, the permission model, the sandbox
proxy, the enterprise features, the code-assist client.**

The second row of the first table is a monorepo migration caught in the act: the
CLI still ships a second copy of the tool implementations under
`oneharness/internal/tools/tools` while the IDE uses
`jetski/cortex/core/tools/tools`. 79 of 100 pairs are the same code; the 21
that differ are the copies having *evolved* — the IDE's package `init` is
1,773 lines against the CLI's 223, and the IDE's `WriteFileTool.Execute`
stamps a `timestamp.Now()` into the tool result while the CLI's does not.

Report: `out/ide-language-server/cross-check.md` · script:
`out/ide-language-server/compare_dec.py`.

## What the CLI cannot show

| Surface | What it is | Evidence |
|---|---|---|
| `cortex/policyguardian` | the **tool-call safety gate**: deterministic pre-checks per write tool, dangerous-command regex profiles, sandbox path rules, an LLM vetting loop, the "Remy" policy judge — plus **outbound checks against Google SafeBrowsing, GAVS, antivirus, deps.dev and the GitHub API**, and `RewriteToolCall`, which **rewrites the tool arguments** (expands `$VAR`, resolves symlinks) from its tracked env state | `strings-resolved.txt` + `dec/` |
| `prompt/template_provider` | the system prompt is a **`.tmpl` template** (`system_prompts`, `ephemeral_prompts`, `step_strings`, `helpers/`) and can be fetched from a **feature provider** | `strings-resolved.txt` |
| `cortex/utils/mcp` | the MCP manager: `mcpServers` config, admin allow-lists, MCP OAuth, elicitation, `list_changed` | `strings-resolved.txt` |
| `models/model_config` | the model catalogue (`globalCatalog` / `staticCatalog`, `InferAPIProviderFromModelName`) | `name-diff/` + `strings-resolved.txt` |
| `product_api/productapi` | the IDE's conversation API and its bidi interaction stream | `strings-resolved.txt` |
| `cortex/chatconverters` | the IDE's diff-review UX (`RenderReviewComments`) | `name-diff/` |
| `cortex/command/sandboxproxy` | the IDE puts **all egress through a local `127.0.0.1` proxy** that does an OAuth2 flow, stores credentials in a `.env` file and enforces **CEL** policies | `strings-resolved.txt` |

The `policyguardian` profiles name the exact commands the reference refuses:
`killall|pkill`, `git commit --amend`, `sshpass|ssh …@|paramiko|plink`,
`gcloud config set`, `cluster-admin|system:masters`, `kubectl apply|patch|edit`,
`.env` in `git add`, force-push, `mkfs|dd of=/dev/`, `adb root|remount|wipe`,
`deploy --only hosting:prod`.

## The one that is **not** IDE-only: rehydration

`cortex/rehydration` is a **shared** package (171 names in the CLI, 169 in the
language server, **130 of them byte-identical**). It is worth calling out
because of what it says about the design:

- The reference **stores the trajectory and nothing else**. Every piece of
  live agent state — subagent definitions, scheduled tasks, the task list,
  pending messages — is a **function of the stored steps**, recomputed on
  load: `rehydrateDefineSubagent` (`tool_names` / `agent_path` / `definition` /
  `SystemPrompt` / `EnableMCPTools` / `EnableWriteTools`), `rehydrateSchedule`
  (`mode` / `outcome` / `iterations` / `cancelled_by`), `rehydrateManageTask`
  (`action` / `input` / `task_id`), `rehydrateManageSubagents`
  (`conversation_ids` / `killed_conversation_ids`), `rehydrateSendMessage`
  (`recipient`).
- `(*CascadeManager)` owns it: `LoadTrajectory` / `SaveTrajectory` /
  `processLoadedTrajectory` / **`ResumeConversationsFromRestart`** ("server
  restart") / `RevertToStep` (rewind) and `rehydratingTrajectory.Branch` (fork
  from a step).
- It has a **kill switch** (`cascade-disable-rehydration`) and a **latency
  metric** (`rehydration_enabled`, measured inside the first-send latency
  streamer) — so even the reference treats it as expensive and off the
  critical path when it can.
- `MigrateLegacyPayload` / `__original_cortex_step` pack the original step
  inside the new payload so **old records still replay**.

The clone now **does the replay**. `internal/rehydration` is the hub: a
`Dispatcher` routes a stored tool call to the rehydrator registered under
that tool's name and skips the steps that did not run to completion; the
per-tool rehydrators live in the package that owns the tool
(`internal/subagenttools/rehydrate.go`, `internal/tools/rehydrate.go`) and
parse the stored arguments with **the live handler's parser**, so a
rehydrated state and a live state agree by construction. The clone's
kill switch is the environment variable `OPENAGY_DISABLE_REHYDRATION`.

Two things had to be true for the replay to have anything to read:

- The **status filter is "not 4/6/7"**, not "equals 3": the reference's
  status enum changed, and "completed" is 3 **or** 5 (4,072 vs 10,709 tool
  calls across 540 real conversations) — see
  `out/ide-language-server/README.md` §12.2.
- The **trajectory has to carry the tool calls**. The clone used to write
  only the user and assistant steps, so its own conversations had no
  trajectory at all, and resuming a reference conversation replaced the
  reference's stored trajectory with a thinner one. `internal/cliapp/trajectory.go`
  keeps the adopted steps and appends the new turns, and a tool call is
  stored in the reference's own step shape (132: `5 -> 4 -> {2: tool name,
  3: JSON args}`, the result in `140`).

The state the clone restores: the subagent definitions (a
conversation-scoped store — the TUI rebuilds the tool set every turn), the
schedule table with its per-conversation task-id counter, the task registry's
finished records, and the killed-subagent set. The pass runs where a
conversation is adopted: the TUI's startup resume, `/continue` and `/resume`
(all through `seedConversation`), and the print mode's continued
conversation.

## What it buys the clone

| Finding | Evidence |
|---|---|
| The agent core is **shared product code**: `jetski/cortex`, `language_server_pb`, `codeium_common_pb`, `api_server_pb`, `seat_management_pb` are near-identical in both binaries | `name-diff/by-package.tsv`, `cross-check.tsv` |
| The hand-written divergence rate is **8%**, and it is concentrated in the vetting gate, the permission model, the sandbox proxy, the enterprise features and the code-assist client | `cross-check.md` §C |
| **The CLI's tool surface is a half-finished migration**: 11 tool types live in `jetski/cortex/core/tools/tools` and 6 more (the file / task / message tools) are still in the old `oneharness/internal/tools/tools`. The IDE consolidated all 15 into the new path and **dropped** `AskQuestionTool` and `FinishTool` | `name-diff/`, `seeds/` |
| **The CLI has an "execute URL" grant policy the IDE deleted** — `GetExecuteURLPolicy`, `ExecuteURLPolicyFromGrants`, `RegistrableDomain`, `normalizeExecuteURLTarget`, `mergeScopedGrants` exist only in the CLI; `ExecuteURL` occurs **0** times in the language server. The clone's `internal/permissions` has no counterpart for it | `name-diff/`, `out/local-macos/seeds/seeds.txt` |
| The IDE's **tool arguments are rewritten before execution** (`policyguardian.RewriteToolCall`: `Resolved $%s → %s from env_state`, `Resolved symlink %s → %s from env_state`) — the reference resolves env vars and symlinks *in the safety layer*, not in the tool | `dec/101c1b240_RewriteToolCall.c` |
| The **system prompt is the same template**: 83 of the capture's long lines appear in both binaries, 2 are CLI-only, 0 are IDE-only | `prompt/` |
| The **tool surface is identical**: all 60 advertised names appear in both | `tools/matrix.tsv` |
| The CloudCode endpoint has a **second, independent source**: the IDE's own JS passes `--cloud_code_endpoint https://daily-cloudcode-pa.googleapis.com` | `ide-js/dist/languageServer.js` |
| The reference's **client identity vocabulary**: `--subclient_type` is `sdk` / `cli` / `hub` — the IDE passes `hub`, the CLI `cli` | `help.txt` |
| The CloudCode client is **internally called `ccpa`**: `--model_api_client_type=ccpa`, and the MCP manager logs *"mcp disabled by CCPA admin controls"* | `help.txt`, `strings-resolved.txt` |
| The reference's **config roots**: `--gemini_dir=.gemini`, `--config_dir=config`, `--app_data_dir=antigravity-ide` | `help.txt` |
| **Real skill samples**: 7 `SKILL.md` files shipped by the IDE, in exactly the format `internal/agyprompt/skills.go` parses | `skills/` |

## How to work with it

```bash
# rebuild the whole evidence set (3.6 s for the seeds)
python3 out/ide-language-server/build_evidence.py

# pick the priority list
#   default (2,845): cross-check + new surface + renamed paths
#   --wide (14,555): + the shared surface to cross-check on both sides
#                    + the big IDE-only packages
python3 out/ide-language-server/build_priority.py \
  out/ide-language-server/seeds/seeds.txt out/local-macos/dec --wide \
  > out/ide-language-server/priority-funcs-wide.txt

# feed it to Ghidra like the CLI (import + seed, then decompile)
#   see out/ide-language-server/README.md §8 for the exact commands
#   the CLI side takes the same list -> out/ide-language-server/dec-cli/

# compare the two binaries (the CLI side may be several dirs, comma separated)
python3 out/ide-language-server/compare_dec.py \
  out/local-macos/dec,out/ide-language-server/dec-cli out/ide-language-server/dec \
  --renamed-cli-dec out/ide-language-server/dec-cli \
  --renamed-pairs out/ide-language-server/renamed-pairs.tsv

# slice the resolved strings by package
python3 out/ide-language-server/package_strings.py \
  out/ide-language-server/strings-resolved.txt cortex/policyguardian

# first-hand: its flag surface and build stamp
/Applications/Antigravity.app/Contents/Resources/bin/language_server --help
/Applications/Antigravity.app/Contents/Resources/bin/language_server --stamp
```

## What is *not* in the repo

The 161 MB binary itself (like the CLI's, it stays on disk), the Ghidra
project (680 MB), the decompiled `.c` files (93 MB + 65 MB — regenerable in
about six minutes), the Electron framework, and the vendored `node_modules` —
only the IDE's **own** 35 JS files and the bundled skills were copied.
`ide-js/` is minified product code, so treat it as string-level evidence, not
specification.
