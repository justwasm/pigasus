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
| Size / SHA-256 | 166,394,176 B · `7e25e79c819ab9b2e27ad5cfbc5822e5d47885a6c5c033e8d9b7b2e79b8565b0` |
| Go build info | `go1.28-20261015-RC02 cl/1000353293 +18a2e1f59d X:fieldtrack,boringcrypto,simd,nogolistexportnewformat` |
| The CLI for comparison | `go1.28-20261015-RC02 cl/1000353293 +18a2e1f59d` — **same CL, same commit** |
| `.text` base | `0x100000f80` (the CLI's is `0x1000011c0` — **addresses are not comparable**) |
| pclntab | `__lrodata_gopcln`, 50.1 MB — the seed tool takes it as-is |
| App version | Antigravity 2.28.0 (`com.google.antigravity`), stamp CL 1008300195 |

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

## What the binary carries inside itself

The Go binary holds **two kinds of static payload**, and both are now in the repo:

| | what | where |
|---|---|---|
| `//go:embed` file systems | the **prompt templates** (`system_prompts/*.tmpl` etc., 17 files), the **built-in skills** (9 `SKILL.md`), the JSON-Schema metaschemas (35) | `out/ide-language-server/embed-fs/` |
| an embedded **ZIP** | the IDE's own **"Jetski Web" frontend** — `index.html`, a 10 MB Closure-compiled `main.js`, the Tailwind and JetBox stylesheets, ~250 file-type icons, the sounds | `out/ide-language-server/frontend/` |

`devutils/embedextract` (in the repo, standard library only) takes both apart:
it scans the image for `embed.file` records — 48 bytes each, name + content +
truncated hash — and **accepts a record only when the content's hash matches**,
then locates embedded ZIPs through their end-of-central-directory record and
opens each candidate with `archive/zip`.

Two things this settles that strings alone could not:

- The prompt is a **template file**, not a string blob. `internal/agyprompt`
  now renders the prompt from the reference's own `.tmpl` files — the CLI's
  copies are in `internal/agyprompt/templates/` — so the prompt can be diffed
  against `system_prompts/*.tmpl` itself rather than against one long
  capture. Note that the CLI's and the IDE's copies are **not** the same
  bytes: the CLI's `guidelines.tmpl` carries one bullet the IDE's does not,
  and the captured CLI request contains it.
- The CLI and the IDE ship the **same frontend** — 301 of 304 entries are
  byte-identical; the three that differ (`compiled_tailwind.css`,
  `index.html`, `main.js`) are **build-version** differences, not packaging
  ones. The CLI additionally embeds **280 TextMate grammar definitions**
  (`embedded/*.xml`) that the IDE's server does not have at all.
- The **builtin skill bundle** the CLI unpacks into
  `<AppDataDir>/builtin/skills` is nine skills — two `assets/external/skills/`
  and seven `assets/shared/skills/`. The clone ships the same bundle
  (`internal/builtinskills/`).

Full write-up: `out/ide-language-server/embedded-assets.md`.

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

## It runs standalone — and answers

The binary is not a passive artefact. Started with a mocked catalogue it
serves the **Web UI's own RPCs** offline, which turns "what does the
reference put on the wire" into a first-hand capture:

```bash
/Applications/Antigravity.app/Contents/Resources/bin/language_server \
    -http_server_port=8791 -gemini_dir=$(mktemp -d) \
    -standalone=true -use_mocked_data=true -persistent_mode=true &

curl -s -X POST http://127.0.0.1:8791/exa.language_server_pb.LanguageServerService/GetUserStatus \
     -H 'Content-Type: application/json' -d '{}'
```

`-use_mocked_data=true` is the load-bearing flag: the **real** catalogue
needs `loadCodeAssist` (network + auth), the mocked one does not. The
captures and the query tool live in `out/ide-language-server/live/`.

What that answered, for the Web UI's model and app-state surface:

| finding | where it bites |
|---|---|
| **Enums go on the wire by name** — `{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}`, not `1008` | Go's `protojson` default; `internal/remotecontrol/modelenum.go` |
| `MODEL_PLACEHOLDER_M<n>` (`1000+n`, `n` = 0 … 1650) is the reference's **own slot for a model the enum cannot name** | the clone's catalogue is exactly that case |
| the `ModelOrAlias` oneof's JSON form is the **member field** (`{"model": …}`), never `{case, value}` | the UI's decoder drops the envelope; every entry reads UNSPECIFIED |
| `JetboxReadAppStateResponse` / `JetboxReadUserConfigResponse` have **no fields** — the state arrives on `JetboxSubscribeToState` | `internal/remotecontrol/dispatch_languageserver.go` |
| `defaultOverrideModelConfig` **overrides the user's pick** (the UI's `RCa` middleware) | the clone must not serve it |

The schema itself came out of the `FileDescriptorProto`s the IDE's own
Web UI bundle embeds — the bytes the shipped UI decodes with:
`exa.codeium_common_pb.Model` has 1691 values (40 real + 1651
placeholders), `ModelProvider` 9, `ModelOrAlias` is a oneof
(`model = 1`, `alias = 2`). Cycle:
[`rc-model-enum-wire`](../cycles/rc-model-enum-wire/deliverable.md).

## The clone: `cmd/langserver`

The reference is a **second product**: the IDE's own agent backend. Until
cycle `cycle-341-langserver-replica` the repo only *documented* it. The
clone now **is** it — `cmd/langserver`, built from the repository's existing
packages, speaking the same `exa.language_server_pb.LanguageServerService`
the IDE's bundle speaks.

```bash
go build -o /tmp/langserver ./cmd/langserver

# the IDE's own shape
/tmp/langserver --standalone --override_ide_name antigravity \
  --subclient_type hub --override_ide_version 2.26.0 \
  --override_user_agent_name antigravity \
  --https_server_port 8799 --http_server_port 8798 \
  --csrf_token tok123 --app_data_dir antigravity-ide \
  --api_server_url https://generativelanguage.googleapis.com \
  --cloud_code_endpoint https://daily-cloudcode-pa.googleapis.com \
  --enable_sidecars
```

What it reproduces, measured side by side against the binary:

| | reference | clone |
|---|---|---|
| `--help` | 68 flags | 68 flags, byte-identical |
| argument parsing (27 cases) | — | **27/27 identical**, including the exit code 2 |
| `--stamp` | 4 lines | identical |
| RPC answers, standalone (15 methods) | — | **15/15 byte-identical** |
| RPC answers, hub (15 methods) | — | 10/15 identical; 5 differ because the reference is backed by a live account |
| web UI page | 3866 B | byte-identical modulo the CSRF token |
| the 401 body and its channels | — | identical |
| the on-disk layout | — | identical modulo `builtin/.checksum` |

Three things are worth knowing before reading the code:

- **the reference's parser is not Go's `flag` package.** `--help` exits
  **2**, an unknown flag is reported in the plural, a value error wins over
  the collected unknown flags, and `--help <pattern>` prints the flags whose
  name contains the pattern. `internal/lsserver/args.go` implements the scan;
  `internal/lsserver/help.go` prints the list.
- **the answers are hand-built JSON**, because the **key order** is part of
  the contract (`internal/lsserver/answers.go`). Three of them follow a
  flag: the `antigravityHub` marker is switched by
  `--override_ide_name antigravity` (with the `hub` subclient type — the
  user agent name is *not* the switch), the sidecar block has three
  shapes, and the instance id is the process's own only for the `cli`
  and the `hub` subclients.
- **the secret has exactly one channel**: the `x-codeium-csrf-token` header.
  The reference does not read the cookie, the bearer header or the query
  parameter, and the page and `/healthz` are **not** gated at all — only the
  service calls are, loopback included.
- **three flags are hidden**: `--enable_sidecars`, `--host_bridge_url` and
  `--host_bridge_token` are accepted by the reference but **not listed** by
  its `--help`. The IDE passes all three on every spawn — the live IDE on
  this machine was captured doing exactly that
  (`out/langserver/spawn-args.txt`). The clone strips them from the
  argument vector before its own flag set parses it; without that the IDE
  would get `flags provided but not defined` and exit code 2.

The reference reads its initial metadata from **stdin** when it is *not* in
standalone mode, and dies on EOF (`main.go:326`) or blocks while stdin is
open. The IDE always passes `--standalone` and closes stdin immediately, so
the clone does not read stdin — an accepted difference, recorded in
`out/langserver/README.md`.

Evidence: `out/langserver/` (zh-CN) · cycle:
[`cycle-341-langserver-replica`](../cycles/cycle-341-langserver-replica/deliverable.md).

## The 351 methods, measured

The service has **351 methods**. The table above covers the 15 whose
answers were compared byte for byte — that number is a *comparison
subset*, not an implementation count. The clone answers **all 351**:
nothing comes back as `-32601` "Method not found".

Two facts make the measurement possible.

**The workbench speaks gRPC-Web, not Connect.** It posts
`application/grpc-web+json` with the five-byte frame envelope and reads
the trailers envelope at the end. The plain-JSON dialect masks a failure
as `200 {}` — under it, "answered" and "not implemented" are the same
bytes. Only the gRPC-Web dialect puts the failure in the `Grpc-Status`
header, where it can be told apart.

**A stream that says nothing is an answer.** A subscription that holds
the connection open and sends no frame is recorded as an *empty stream*,
not as a failure: the reference holds six of them open forever.

`scripts/probe_ls_methods.py` asks all 351 methods of a running instance
(the request body is each request message's default instance, `{}`),
normalises the uuids, the tracing ids and the timestamps, and
`scripts/build_ls_coverage.py` merges the two captures into
`out/langserver/coverage-351.json`.

| | reference | clone |
|---|---|---|
| answered | 153 | **310** |
| refused (an error) | 186 | 33 (the clone's own parameter validation) |
| empty stream (held open) | 7 | 8 |
| closes the connection | 5 | 0 |
| **"Method not found"** | — | **0** |

The reference's 186 refusals break down as 52 *not implemented*, 44
*empty request* (`unknown uri scheme`), 37 *missing argument*, 22
*deprecated*, 19 *not found* and 9 *not initialised*. Five methods make
the reference **close the connection without answering**:
`CheckoutCommit`, `ProvideCompletionFeedback`,
`SubscribeToAgentBrowserRequests`, `UpdateCustomizationPathsFile` and
`WatchDirectory`.

Of the reference's 153 answers, **111 are exactly `{}`** — the default
instance — and the clone's answer is byte-identical to the reference's
for **110** of the 153; the 43 that differ are the ones the clone answers
from its own state (the skill catalogue, the conversation list, the
model names).

The clone answers a method it does not implement with the **default
instance of that method's response message** — the schema knows what each
answer looks like, and the default instance is what "nothing to report"
means (`lsproto`/default.go`, `internal/lsserver/coverage.go`).
That is a deliberate difference from the reference, which refuses: a
workbench that gets `-32601` logs an error for every call it makes while
the page loads (`skills_fetch_failed`, `Failed to fetch admin controls`).

### The conversation batch

The batch that *reads a conversation back* is answered from the clone's
own state (`internal/remotecontrol/dispatch_ls_trajectory.go`,
`internal/remotecontrol/dispatch_ls_conversation.go`,
`internal/remotecontrol/ls_turns.go`):

| method | answer |
|---|---|
| `GetCascadeTrajectory` | the conversation's trajectory (its step list) and the run status |
| `GetCascadeTrajectorySteps` | the step list, from a `stepOffset` |
| `SearchConversations` | the clone's own conversation list (title, workspace, time) |
| `GetConversationMetadata` | the conversation's metadata (`trajectoryId`, `numSteps`, `metadata{createdAt, workspaceUris}`) |
| `GetTrajectoryTurns` | the conversation's *turns* — one user message and the reply that followed it |
| `GetAllCascadeTrajectories` | a summary per conversation (`summary`, `stepCount`, `trajectoryId`, `status`, `createdTime`, …) |
| `DeleteCascadeTrajectory` | drops the conversation's trajectory |
| `GetUserTrajectory` | one trajectory, by trajectory id |
| `GetUserTrajectoryDebug` | the mainline trajectory the debug panel reads |
| `GetUserTrajectoryDescriptions` | the trajectory list |

A **turn** starts at a user-input step and runs until the next one; the
summary is the reference's own `exa.jetski_cortex_pb.TurnSummary`
(`startStepIndex`, `firstStep`, `responsePreview`).

Measured in standalone mode with the mocked catalogue:

```json
{"trajectory":{"trajectoryId":"traj-c-test-1","cascadeId":"c-test-1",
  "steps":[{"type":14,"userInput":{"items":[{"text":"hello there"}]}},
           {"type":15,"plannerResponse":{"response":"mock reply …"}}],
  "metadata":{}},
 "status":"CASCADE_RUN_STATUS_IDLE","numTotalSteps":2}
```

### The trajectory id is `traj-` + the conversation id

【一手】 2026-10-27, measured against the reference binary in its mocked
standalone mode (`out/langserver/conversation.json`).

The workbench **never sends a trajectory id**: it mints one uuid per
conversation and sends it as the `cascadeId`, and it uses that uuid as
the `conversationId` afterwards. The trajectory id is the *server's*:

```
StartCascade {"cascadeId":"XYZ-9"}
GetCascadeTrajectory
  → {"trajectory":{"trajectoryId":"traj-XYZ-9","cascadeId":"XYZ-9","metadata":{}},
     "status":"CASCADE_RUN_STATUS_IDLE"}
```

Another id gives the same shape (`ref-probe-1` → `traj-ref-probe-1`): the
rule is the prefix **`traj-`** plus the conversation id. The clone mints
the same shape (`lsTrajectoryID`), and `GetUserTrajectory` — which reads
by *trajectory* id — looks the trajectory up by that id, not by the
conversation id (`lsTrajectoryStore.LookupByTrajectory`).

### The failure format

【一手】 2026-10-27, measured against the reference binary.

A refused call carries its code and its text in the `Grpc-Message`
header, in the reference's own format:

```
grpc-status 5
grpc-message: not_found: failed to get trajectory: trajectory not found for id <id> (error ID: <32 hex>)
```

The `error ID` is the reference's own tracing id — a fresh 32 hex digits
per call — so two runs of the same call differ only there. The clone
writes the same shape (`grpcWebErrorMessage` in
`remotetransport/grpcweb_codec.go`), which is what makes five
methods of the batch **byte-identical** after the normalisation:
`GetUserTrajectory`, `GetConversationMetadata`, `GetTrajectoryTurns`,
`EndAudioSession` and `SendAudioChunk`.

### Two streams hold the connection open

【一手】 2026-10-27, measured against the reference binary.

A `StreamAgentStateUpdates` or `StreamTerminalOutput` call that carries
no id is **not** a failure in the reference — it answers the status line
and the content type and then says nothing at all, holding the
connection open:

```
StreamAgentStateUpdates {}
  → HTTP 200  Content-Type: application/grpc-web+json
    (the connection stays open, no frame)
```

The clone used to answer `grpc-status 13 / stream failed` for both. It
now holds the stream open the same way, and the transport flushes the
response headers *before* the first frame — the reference sends them at
once, and a client that waits for them would otherwise see nothing at
all.

### The debug panel's `mainline` is a list

【一手】 2026-10-27, measured against the reference binary.

`GetUserTrajectoryDebug` answers the mainline entry as a **list**, and
the trajectory in it carries its type and its metadata:

```json
{"mainline":[{"trajectory":{"trajectoryId":"<uuid>","cascadeId":"<uuid>",
  "trajectoryType":"CORTEX_TRAJECTORY_TYPE_USER_MAINLINE",
  "metadata":{"createdAt":"<timestamp>","initializationStateId":"<uuid>"}},
  "trajectoryScope":{}}]}
```

`exa.language_server_pb.GetUserTrajectoryDebugResponse.mainline` is a
repeated `ImplicitTrajectory` — the clone answers a list too
(`lsUserTrajectoryJSON`).

### In the shell

With the fresh binary installed into `/Applications/Pigasus.app` and the
workbench reloaded, the page asks **23 methods** while it loads —
`GetAllSkills`, `FetchAdminControls`, `GetAgentScripts`, `GetAllWorkflows`,
`GetCustomizationStates`, `ListMcpPrompts`, `GetCascadeNuxes`,
`GetMendelFlags`, `GetUserStatus`, `ListProjects`, `ReadProjects`, the
`SubscribeTo*` / `JetboxSubscribeTo*` families, `ProjectUpdatesStream`,
`RecordAnalyticsEvent` — and the console holds **no error at all**.
Sending a message renders the answer and the conversation appears in the
sidebar.

Evidence: `out/langserver/coverage-351.json` (zh-CN keys) ·
`out/langserver/conversation.json` (the conversation batch, side by side) ·
`out/langserver/README.md` §9–§10.

## The IDE shell, running on the clone

The clone can be installed **into a copy of the IDE's own Electron shell**,
so the real IDE UI runs on the clone's backend. The copy is fully isolated:
a different bundle identifier, a different product name, therefore a
different `~/.gemini/<name>` and a different Electron `userData`. The
original app is only read — its signature still verifies and its own
language server binary is byte-for-byte the one that shipped.

```bash
scripts/install-ide-shell.sh          # copy, rename, swap the binary, re-sign
open -a /Applications/Pigasus.app     # the IDE shell on the clone
```

Four macOS rules make this more than a file copy; each one killed a launch
until it was found:

| # | the rule | the symptom it produces |
|---|---|---|
| 1 | the bundle's **resource seal** is enforced at exec time | `amfid: The signature on the file is invalid (Code=-420)` → SIGKILL. The bundle must be re-signed after the surgery |
| 2 | the **hardened runtime flag** must be dropped | `dyld: Library not loaded: @rpath/Electron Framework.framework/…` — `mapping process and mapped file (non-platform) have different Team IDs`. Keep the entitlements (JIT, automation, audio, camera), drop the flags |
| 3 | Electron finds the helper **by name** | `FATAL: Unable to find helper app` — Electron 44 builds `<productName> Helper.app/Contents/MacOS/<productName> Helper`, so all four helper bundles and their executables must be renamed |
| 4 | the language server's data dir is passed **by the IDE** | `--app_data_dir <app.getName().toLowerCase()>` — so isolation means repacking `app.asar`'s `package.json` (`productName`), not just renaming the bundle |

What the shell does with the clone, measured: it spawns it with the **same
argument vector** it uses for the reference (`out/langserver/spawn-args.txt`),
reads the port out of the clone's log with its own regex
(`/listening on \w+ port at (\d+) for HTTP(S)?\b/i`), and loads
`https://127.0.0.1:<port>/` — **the entire workbench bundle (10,461,461
bytes of `main.js`) is served by the language server**, not by the app.

What works: startup, the port handshake, the page, the bundle, the CSRF
gate, the project list, the sidebar, the model picker — and the **agent**:
a message typed in the workbench reaches a model and the answer comes back
in the conversation.

### The turn reaches a real model

`rchost.BackendPicker` — the hook that picks the transport a turn rides on —
**defaults to the deterministic stand-in**. The CLI overrides it
(`internal/cliapp`), so the standalone language server overrides it too
(`cmd/langserver/backend.go`), with the clone's own rule
(`internal/backendselect`): a model the user configured goes to its own
endpoint, a model of the reference's catalogue goes to CloudCode, and a
model nobody configured gets the stand-in **with a warning** — an answer
that silently came from the stand-in would look like a working model.

The mocked mode keeps the stand-in: `--use_mocked_data=true` is the
reference's own offline switch, and the coverage probe runs against it.

A live run on the copy: a reload issues 23 RPC calls with **zero console
errors** (one IDE warning about a trajectory-summary timeout), and a message
queued behind a busy turn was answered by the model — *"Time to first token:
3213ms"* — not by the stand-in's `mock reply from model=…`.

Evidence: `out/langserver/pigasus-app.txt` (zh-CN README: `out/langserver/README.md` §8).

## Run the reference as the web backend

`pigasus web --reference-backend` swaps the clone's own
`LanguageServerService` implementation for a **proxy** to a running
reference. The browser still talks to the clone's web server; every RPC
is answered by the reference, so the UI shows the reference's own
catalogue, its own conversation titles and its own answers — which is
what makes a side-by-side comparison possible.

```bash
# a reference that is already running
pigasus web --port 8902 --reference-backend=127.0.0.1:8791

# or the binary: the web server starts it and stops it on exit
pigasus web --port 8902 \
    --reference-backend=/Applications/Antigravity.app/Contents/Resources/bin/language_server
```

The value is an **endpoint** (`host:port`, `http://host:port`,
`https://host:port`) or the **path of a binary**. A binary is started
with

```
-http_server_port=<free port> -gemini_dir=<tmp> -standalone=true
-persistent_mode=true -use_mocked_data=true
```

and `OPENAGY_REFERENCE_BACKEND_ARGS` adds or overrides arguments.

Two wire details the proxy has to get right (`internal/refbackend`):

- a **unary** call is `POST <base>/exa.language_server_pb.LanguageServerService/<Method>`
  with `Content-Type: application/json`; the answer is the proto-JSON
  message;
- a **server-streaming** call needs the Connect *streaming* codec:
  `Content-Type: application/connect+json`, the request body is one
  length-prefixed envelope (flags byte + big-endian uint32 length +
  payload) and the response is a sequence of them. The reference answers
  `application/json` on a streaming method with **HTTP 415**.

The reference's 24 server-streaming methods were recovered from the
IDE's own service descriptor (`out/ide-language-server/extract_ls_descriptor.py`)
and are hard-coded in `internal/refbackend/connect.go`. A method outside
that set is a unary call.

Cycle: [`rc-web-parity`](../cycles/rc-web-parity/deliverable.md).

## The `LanguageServerService`'s wire form

The clone's own `LanguageServerService` answers the Web UI, and the Web
UI decodes those answers with protobuf-es against the IDE's own
descriptor. Six rules decide whether an answer survives that decoder:

| rule | why |
|---|---|
| a **oneof** is spelled by its **member** (`{"projectList":{}}`) | the `{case, value}` envelope is the *decoded* form; on the wire "update" is an unknown key and is dropped |
| a message field is a **message** | a string there makes the decoder throw, and the bad frame kills the stream |
| a field name is the descriptor's | an unknown key is dropped — a wrong name is a silent feature failure |
| an **enum** is spelled by its **name** | `AGENT_ONBOARDING_STATE_COMPLETED`, not `2` |
| a `Timestamp` / `Duration` is a **string** | "2026-10-25T19:31:11.993337Z", "1.500s" |
| an unset message is **omitted** | not `null` |

The frontend's client always decodes with `ignoreUnknownFields: true`,
so a wrong key does not throw — it disappears. The IDE's descriptor is
the oracle for the names:

```bash
python3 out/ide-language-server/extract_ls_descriptor.py msg UpdateStatus
```

and the clone's answers can be compared against a live reference method
by method:

```bash
python3 out/ide-language-server/ls_shape_diff.py \
    --clone http://127.0.0.1:8899 --reference http://127.0.0.1:8791
```

The one deliberate deviation: the clone keeps the **empty lists** in
`ListProjects` / `ReadProjects`, because the Web UI spreads them without
a null guard (`[...a.localProjectIds]`).

Cycle: [`rc-web-parity`](../cycles/rc-web-parity/deliverable.md).

## The trajectory step

The Web UI renders the conversation from the trajectory the
`StreamAgentStateUpdates` server stream carries. The step the assistant's
answer lands on is a planner response, and the IDE's own message
(`exa.cortex_pb.CortexStepPlannerResponse`) carries two more fields the
clone now fills:

| field | what the frontend does with it |
|---|---|
| `response` | the answer, rendered as markdown (`data-testid="planner-response-text"`) |
| `thinking` | the reasoning, rendered in the collapsible panel **above** the answer |
| `thinkingDuration` | the panel's label: `Thought for {max(1, seconds)}s`, or "Thought Process" when it is absent |

Two rules the wire makes load-bearing:

- **the panel needs a non-empty `thinking`** — the frontend's component
  returns null for an empty thought, so the panel simply is not there;
- **`thinkingDuration` is a proto-JSON *string*** ("1.500s"), because a
  `google.protobuf.Duration` is encoded as a string. The object form
  makes the frontend's protobuf-es decoder throw, and one bad frame
  kills the **whole** trajectory stream.

The clone's thinking comes from the `<think>…</think>` markers the
CloudCode transport wraps around a model's reasoning
(`internal/backend/cloudcode_stream.go`). `internal/remotecontrol/ls_thought.go`
splits them out of the answer, keeps the reasoning and times the block;
`internal/remotecontrol/ls_trajectory.go` puts the result on the step.

The reference's own mocked step carries `thinking` but **no**
`thinkingDuration` — that is why the reference's panel reads "Thought
Process", and the duration only appears with a real model. Captured
first-hand with `out/ide-language-server/ref_ls_turn.py`; cycle:
[`rc-web-parity`](../cycles/rc-web-parity/deliverable.md).

## The built-in browser ↔ the agent

The IDE's built-in browser is **an aux pane of the workbench**, not a
separate application: the `browserAuxPane` setting (engine `webview`,
plus a `useIwaControlledFrame` variant) together with `"browser"` in
`customAuxPanes` makes it appear, and the pane holds Electron
`<webview>` tags in a `builtin-browser-webview-pool` container, driven
with `executeJavaScript` and `domReady`.

The agent reaches it **through the language server**. The reference
carries a broker —

```
google3/third_party/jetski/language_server/agentbrowser/agentbrowser
```

— a `(*StreamBroker)` with `Subscribe`, `SubscribeWithInfo`, `Request`,
`Fulfill`, `Store`, `Take`, `Resolve`, `SupportsIsolatedWorld`,
`WaitForSupportsIsolatedWorld`, `DeleteRootConversation`,
`RecordUserMessageWindow`, `UpdateWindowDisplayedConversation` and
`SubscriberCount` 【strings】. It reaches the wire through three
methods of the service:

| RPC | shape | carries |
|---|---|---|
| `SubscribeToAgentBrowserRequests` | **server-streaming** | the IDE window subscribes with `window_id`, `surface`, `open_conversation_ids`, `displayed_root_conversation_id`, `supports_isolated_world`, `backend_instructions`; each frame is an `AgentBrowserWireRequest` |
| `UpdateAgentBrowserSubscription` | unary | `window_id`, `displayed_root_conversation_id`, `supports_isolated_world` |
| `FulfillAgentBrowserRequest` | unary | `response: AgentBrowserWireResponse` |

`AgentBrowserWireRequest` is `{request_id, cascade_id,
root_conversation_id, action, params_json, target_window_id}`;
`AgentBrowserWireResponse` is `{request_id, ok, result_json, error,
error_code, window_id, screenshot_png}`.

**The direction is the interesting part**: the IDE window *executes*
the action, the agent *asks* for it. The reference's own frontend
subscribes with `surface:"hub"`, checks that the request's
`root_conversation_id` is open in that window (else it answers
`WRONG_WINDOW`), runs the action in its `<webview>`, and answers with
`fulfillAgentBrowserRequest` — the screenshot included. So a model
call to `open_browser_url` goes: model → LS broker → the subscription
stream → IDE window → fulfil → broker resolves → tool returns.

The `backend_instructions` the IDE hands over with the subscription is
the browser agent's operating manual, and it says so in the first
line: *"You operate the built-in browser panel in Antigravity Hub."*
It also pins the fences — `USER_TAKEOVER`, `WRONG_WINDOW`,
`WINDOW_RELOADED_ACTION_STATE_UNKNOWN`, `WINDOW_CLOSED_OR_RELOADED`,
and *"browser is currently in use by another browser subagent"* (one
owner per conversation).

The reference's LS binary carries all **23** browser tool names and
their `jsonschema_description:` strings (one default is `Target agent
browser tab ID (defaults to 'agent-tab-1')`), and
`exa.cortex_pb.CascadeToolConfig.antigravity_browser` is the knob
that puts them on the model's tool list: `enabled`, `tool_set_mode`
(`ALL_TOOLS` / `PIXEL_ONLY` / `ALL_INPUT_PIXEL_OUTPUT`),
`browser_subagent.mode` (`MAIN_AGENT_ONLY` / `SUBAGENT_PRIMARILY` /
`BOTH_AGENTS` / `SUBAGENT_ONLY`), `disable_open_url`,
`disable_read_browser_page`, `enable_browser_subagent_webview` …

Those 23 names are a **catalogue**, not the model's declaration. The
reference's IDE turn advertises **one** browser tool: `webview_browser`,
with a twenty-value `action` enum (`open`, `navigate`, `snapshot`,
`click`, `fill`, `type`, `press`, `hover`, `scroll`, `select_option`,
`get_state`, `wait`, `back`, `forward`, `reload`, `list-tabs`,
`new-tab`, `activate-tab`, `close-tab`, `batch`) and thirteen
parameters. A batch's steps take the same enum minus `batch` itself
(19 values) 【decompile】.

**Where the clone stands — the LS half is now there.** The IDE half
was *already* there: the clone serves the reference's own frontend
byte-for-byte (`internal/rcfrontend/assets/main.js` ==
`out/ide-language-server/frontend/main.js`), so the
`AgentBrowserListener`, the webview pool and the aux pane's
`"browser"` custom view all ship — the clone's own `app_storage.json`
already holds a `browser__…` tab with `initialUrl
https://www.google.com/`.

Path A landed on 2026-10-27 and filled the LS half:

| piece | where |
|---|---|
| the broker, the wire shapes, the tool's declaration, the parameter builder | `agentbrowser` — no PiG, no `internal/tools`, nothing of the clone's |
| the three RPCs | `internal/remotecontrol/dispatch_agentbrowser.go` |
| the tool on PiG's wire | `internal/piagentbridge/browser_tool.go` |

The daemon builds **one** broker per process and hands the same
instance to both halves — `remotecontrol.Config.Browser` (the three
RPCs) and `piagentbridge.RemoteCascadeOptions.Browser` (the turn's
tool). That is not tidiness: the agent half blocks on the answer the
IDE half delivers, so the two must land in the same place. The tool is
advertised **only when a broker is present**, and only on the IDE
turn.

The reference's strings are verbatim: the action errors
(`webview_browser action 'fill' requires 'uid'`), the batch errors
(`batch step 0 has invalid action "fly"`), the broker's four failures
(`agentbrowser.Response cannot be nil`, …), the timeout message. The
timeout policy is the reference's: 30 s, and a batch gets 20 s per
step clamped to [20 s, 120 s] plus 10 s of slack — 2 steps = 50 s,
10 steps = 130 s 【decompile】. Every action carries the same six
keys (`tabId` defaulting to `agent-tab-1`, `includePerception`,
`captureScreenshot`, `captureSnapshot`, `coordinateSystem` =
`normalized_1000`, `normalizedMaxDimension` = 1000) plus its own
【decompile】.

The reference's own answers for the three methods are measured in
`out/langserver/coverage-351.json`: it closes the connection for
`SubscribeToAgentBrowserRequests`, and answers the other two with

```
grpc-status 3: invalid_argument: UpdateAgentBrowserSubscription requires window_id (error ID: <tracingId>)
grpc-status 3: invalid_argument: FulfillAgentBrowserRequest requires a non-nil response payload (error ID: <tracingId>)
```

Full record, with the measurements and the two ways forward (implement
the broker, or attach to the IDE's `<webview>` over CDP):
[`out/langserver/agent-browser-bridge.md`](../../out/langserver/agent-browser-bridge.md).

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
only the IDE's **own** 35 JS files, the bundled skills, and the assets
**embedded in the Go binary** (`frontend/`, `embed-fs/`) were copied.
`ide-js/` is minified product code, so treat it as string-level evidence, not
specification.
