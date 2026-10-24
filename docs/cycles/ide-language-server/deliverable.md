# The Antigravity IDE's language server — a second reference binary

## Scope

The repo reverse engineers one binary: the Antigravity **CLI** (`agy`, 1.4.6),
which is what `cmd/` + `internal/` clone. This cycle surveyed
`/Applications/Antigravity.app/` — the **IDE** app bundle — for anything that
belongs in the workspace, and landed the useful half of it.

The IDE ships its own Go binary, `Contents/Resources/bin/language_server`
(161.8 MB). It is a different product, but it is built from the **same Google
internal monorepo commit** as the CLI, which makes it two things at once: a
first-hand cross-reference for everything already in `out/`, and a window onto
code the CLI simply does not contain.

**Nothing in the CLI's evidence was changed.** This cycle adds a new evidence
subtree and a guide entry; it touches no clone code.

The cycle ran in two passes. The first landed the identity, the name diff and
the IDE's own JavaScript. The second — this section — put the binary through
the same Ghidra pipeline the CLI went through, and compared the two
decompilations function by function.

## What was found

### The bundle

| Path | Size | Verdict |
|---|---|---|
| `Contents/Resources/bin/language_server` | 161.8 MB | **the target** — second reference binary |
| `Contents/Resources/bin/webm_encoder` | 12.6 MB | same monorepo (`+18a2e1f59d`), but a screen-recording helper — no agent surface, skipped |
| `Contents/Resources/app.asar` | 4.4 MB | the IDE's Electron app: **35 own JS files (268 KB)** + vendored `node_modules` (16.4 MB) |
| `Contents/Resources/app.asar.unpacked/.../chrome-devtools-mcp/skills/` | 92 KB | **7 real `SKILL.md` files** |
| `Contents/Info.plist`, `app-update.yml` | 5 KB | the IDE's identity (2.26.0, bundle id, update channel) |
| `Contents/Frameworks/`, `MacOS/Antigravity` | ~300 MB | Electron runtime — irrelevant |
| `Contents/Resources/*.lproj` | — | localisation — irrelevant |

### The binary is the CLI's sibling

```
language_server : go1.28-20261015-RC01 cl/1000353293 +18a2e1f59d X:fieldtrack,boringcrypto,simd,nogolistexportnewformat
agy (1.4.6)     : go1.28-20261015-RC02 cl/1000353293 +18a2e1f59d X:fieldtrack,boringcrypto,simd,nogolistexportnewformat
```

Same internal CL (`cl/1000353293`), same commit short hash (`+18a2e1f59d`), one
toolchain release apart. `--stamp` on the language server reports
`Built at CL: 1005016104`, baseline `1002541031`, branch
`antigravity_web_release_branch`.

Its `pclntab` sits in `__lrodata_gopcln` (48.7 MB) — the same section name the
vendored seed tool already handles, and its `.text` base is `0x100000f80`
(the CLI's is `0x1000011c0`).

### Function names: the diff against the CLI

`rea-go-seeds` recovered **164,772** names from it in 3.6 s (the CLI yields
161,286):

| | count |
|---|---|
| shared | **147,120** (91.3% of the CLI's set) |
| CLI-only | 14,049 — of which 6,021 are `jetski/cli/…`, the CLI's own command layer |
| IDE-only | 17,551 |

The package-level split shows the shape of the monorepo: the **agent core is
shared product code** (`jetski/cortex` 8,799 vs 8,556; `language_server_pb`
9,290 vs 9,155; `codeium_common_pb`, `api_server_pb`, `seat_management_pb`
near-identical), while the products differ at the edges — the IDE adds
`google/ai` (+5,484), `google/api` (+3,587), `privacy/data_governance` (+2,427),
`learning/genai` (+1,930).

### The prompt is the same template

Taking the CLI's 70,853 B capture and looking for each of its lines in both
binaries' string tables:

| | lines |
|---|---|
| in both | **83** |
| CLI-only | **2** |
| IDE-only | **0** |

The two CLI-only lines are the access-control rule
(`Never defeat or bypass an access control, permission check, credential
requirement, …`) and the user-defined-rules preamble
(`The following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY
EXCEPTION. …`) — both injected by the CLI side. Both binaries contain the same
opening line verbatim ("You are Antigravity, a powerful agentic AI coding
assistant designed by the Google Deepmind team …") and 22 occurrences of
`BypassSandbox`.

### The tool surface is identical

All 60 names in `internal/tools/testdata/advertised_tools_1.3.6.txt` appear in
both binaries.

### The IDE's half of the contract, in plain JS

`ide-js/dist/languageServer.js` spawns the server with:

```
--standalone --override_ide_name antigravity --subclient_type hub
--override_ide_version <2.26.0> --override_user_agent_name antigravity
--https_server_port <port> --csrf_token <token> --app_data_dir <dir>
--api_server_url         https://generativelanguage.googleapis.com
--cloud_code_endpoint    https://daily-cloudcode-pa.googleapis.com
--enable_sidecars [--host_bridge_url=… --host_bridge_token=…] [--headless]
```

That is a **second, independent source** for the CloudCode endpoint the clone
implements in `internal/backend/cloudcode.go`, and it names the reference's
client-identity vocabulary: `--subclient_type` is `sdk` / `cli` / `hub`.

The server's own `--help` (70 flags, kept verbatim in `help.txt`) adds:

- `--model_api_client_type=ccpa` — "Which model client to use: ccpa or gemini" —
  the CloudCode channel is internally **ccpa**.
- `--cloud_code_endpoint` — "CCPA API URL".
- `--generative_service_addr` — `blade:google.ai.generativelanguage.v1main.generativeservice-prod`.
- `--gemini_dir=.gemini`, `--config_dir=config`, `--app_data_dir=antigravity-ide`.
- `--override_oauth_client_id` / `--override_business_oauth_client_id`.

### Seven real skills

The IDE bundles `chrome-devtools-mcp`, which ships **7 `SKILL.md` files** in the
frontmatter format (`name:` / `description:`) that `internal/agyprompt/skills.go`
parses — the only real reference samples of that format in reach (the repo had
only test fixtures).

---

## Pass 2 — the Ghidra run and the cross-binary comparison

### The pipeline takes it unchanged

The same recipe as the CLI (`docs/guide/reverse-engineering.md`), one binary
swap:

| Stage | Result |
|---|---|
| import + seed | **164,753 / 164,772** functions created (19 pre-existing, **0 failed**) |
| decompile | **2,845 / 2,845**, 0 errors, 48 s → 390,786 lines of pseudocode |
| project | 680 MB on disk (local, git-ignored) |
| strings | 3,031 resolved `&UNK_` references (`strings-resolved.txt`) |

`ReaGhidraApplySeeds.java` applied all 164,772 seeds without a single
failure, so the language server's `pclntab` is as usable as the CLI's.

The priority list (`build_priority.py`) has three parts:

- **A. cross-check** — every function the CLI run already decompiled that also
  exists in the language server (483 of 2,284).
- **B. new surface** — the IDE-only packages, with the generated protobuf
  boilerplate filtered out.
- **C. renamed paths** — the same tool functions under the two import paths
  (100 pairs, mapped in `renamed-pairs.tsv`).

### The headline: same code, two binaries

`compare_dec.py` decompiles the *same function names* in both binaries and
compares the pseudocode. Addresses, address-bearing labels and the
decompiler's local-variable numbering are normalised away (the numbering
alone would otherwise make every large function look different, because one
extra local renumbers all the later ones).

| Comparison | compared | **identical** | ≥95% similar | differs |
|---|---|---|---|---|
| same name | 483 | **204** | 239 | 40 |
| same function, two import paths | 100 | **42** | 37 | 21 |

The 40 that differ are *product entry points*, not library code:

```
main.main                                          0.033  (different programs)
codeassistclient.NewCodeAssistClient              0.647
codeassistclient.(*CodeAssistClient).fetchPeopleInfo             0.376
codeassistclient.(*CodeAssistClient).ListRemoteAgentSlashCommands 0.847
```

while the protocol layer — `GenerateContentFromGateway` (0.915),
`StreamGenerateContentFromVertexAI` (0.934) — is near-identical, and their
`.func1`/`.func2` closures come out byte-identical.

**Conclusion: the two products share one agent kernel and differ at the
edges.** That is the strongest statement the two binaries together support.

### A monorepo migration, caught in the act

The CLI ships a *second copy* of the tool implementations under
`oneharness/internal/tools/tools`; the IDE has them under
`jetski/cortex/core/tools/tools`. The function suffixes are identical, so the
two spellings can be paired and their bodies compared:

- 79 of the 100 pairs are the same code (42 byte-identical, 37 ≥95%).
- The 21 that differ are the copies having **evolved**: the IDE's package
  `init` is 1,773 lines against the CLI's 223; the IDE's
  `WriteFileTool.Execute` is 359 lines against 324.

One concrete behavioural difference, visible in the decompiled source of
`dec/101e17530_Execute.c`:

```
uVar12 = google3_google_protobuf_timestamp_go_proto_Now();
*(undefined8 *)(puVar9 + 0x110) = uVar12;
```

The IDE's write/edit tool stamps a timestamp into the tool result; the CLI's
copy does not. So `jetski/cortex/core/tools` is the **newer** location — the
CLI is carrying the stale copy.

### The surface the CLI cannot show

The IDE-only packages decompiled here (the strings are in
`strings-resolved.txt`, resolved against the binary):

**`cortex/policyguardian` — the tool-call safety gate.** Every tool call
passes through it (`preHook` / `postHook` → `allow` / `ask_user` / `deny`).
It has deterministic pre-checks per write tool
(`[PolicyGuardian] Deterministic pre-check: write_file target=%s` and
friends, for `VetCreateFile`, `VetWriteFile`, `VetWriteToFile`, `VetEditFile`,
`VetReplaceFileContent`, `VetMultiReplaceFileContent`, `VetStrReplaceEditor`,
`VetDeleteFile`, `VetFileDelete`, `VetFileWrite`), dangerous-command regex
profiles (`killall|pkill`, `git commit --amend`, `sshpass|ssh …@|paramiko`,
`gcloud config set`, `cluster-admin|system:masters`, `kubectl apply|patch`,
`.env` in `git add`, force-push, `mkfs|dd of=/dev/`, `adb root|remount|wipe`,
`deploy --only hosting:prod`), the sandbox path rules
(`isSafeSandbox*`, `isForbiddenSandboxWritePath`,
`isAllowedAgentCacheOrConfigPath`), an environment-state ledger
(`env_changes`, `created_files`, `modified_files`, `executable_files`,
`untrusted_downloads`, `symlinks`), an LLM vetting loop (`runVettingLoop`,
fail-closed), and a second "Remy" policy judge (`vetRemyPolicy`,
`askRemyJudge`).

**`prompt/template_provider`** — the system prompt is a `.tmpl` template under
`jetski/prompt/template_provider/templates/`, with `helpers/`, keyed
`system_prompts` / `ephemeral_prompts` / `step_strings`, and
`GetSystemPromptStringWithDataFromFeatureProvider` can fetch the template
from a server-side feature provider.

**`cortex/utils/mcp`** — the MCP manager: the `mcpServers` config key, admin
allow-lists (*"mcp disabled by CCPA admin controls"*), MCP OAuth
(`CompleteMcpOAuth`, `discoverAuthServerMetadata`), elicitation, and
`list_changed` handling.

**`models/model_config`** — the model catalogue: `globalCatalog` /
`staticCatalog`, `GetModelInfoFromName`, `InferAPIProviderFromModelName`
(`/openai/` → OpenAI, `/anthropic/` → Claude), model families `Gemini3`,
`Gemini25Pro`, `Gemini31Pro`, `Claude`, `OpenAI`, `ComputerUse`, …

**`product_api/productapi`** — the IDE's conversation API plus a bidi
interaction stream whose first message must be `create_interaction` or
`reconnect_interaction`.

**`cortex/chatconverters`** — the IDE's diff-review UX
(`RenderReviewComments`, `parseComments`).

**`cortex/core/tools`** — seven tool types the CLI has no copy of
(`ReadFileTool`, `WriteFileTool`, `EditFileTool`, `ManageTaskTool`,
`ScheduleTool`, `SendMessageTool`), each declaring `RequiredPermissions`.

## Pass 3 — the widened run: a divergence map, not a sample

Pass 2's cross-check was **capped by the CLI side, not the language-server
side**: only 483 pairs were compared because the CLI run had decompiled 2,284
names. Widening the selection to the same names on *both* sides, and adding the
large IDE-only packages, produced a comparison that is a **map** rather than a
sample.

### What ran

| Run | Functions | Errors | Time |
|---|---|---|---|
| language server, wide list | 14,555 | 0 | 185 s → 1,555,877 lines |
| CLI, same names (into `dec-cli/`) | 9,902 | 0 | 158 s |

Both runs used `-readOnly`, so neither Ghidra project was re-saved — the
decompile script only reads the program and writes `.c` files.

`build_priority.py --wide` grew the list from three parts to five:

- **D. widened cross-check** — shared names (present in *both* seed files) under
  `jetski/`, `oneharness/`, `google/api/`, `google/ai/`, `codeium_common/`
  (9,802 selected), decompiled on both sides.
- **E. the big IDE-only packages** — `google/ai/generativelanguage`,
  `google/api`, `privacy/data_governance`, `learning/genai`,
  `security/context` (1,908 selected after the boilerplate filter; 15,000 of
  their 17,551 names are generated protobuf).

### The result

| | compared | **identical** | ≥95% | differs | too short |
|---|---|---|---|---|---|
| all | 10,285 | **5,309** | 3,390 | 1,338 | 248 |
| hand-written | 8,206 | **4,329 (53%)** | 3,131 | **660 (8%)** | 86 |
| generated protobuf | 2,079 | 980 | 259 | **678 (33%)** | 162 |

The split matters: the *code* diverges 8%, the *schemas* diverge 33% — the
IDE's protobuf definitions grew new fields. Reading "13% of the two binaries
differ" without that split would be misleading.

### The divergence is per-package, and it is coherent

268 packages were compared; 160 differ somewhere, 108 are identical
throughout. The largest divergence clusters:

| package | compared | differs |
|---|---|---|
| `cortex/policyguardian/vetting` | 201 | 47 |
| `enterprise` | 132 | 43 |
| `cortex/shared` | 115 | 42 |
| `cortex/cortex` | 373 | 41 |
| `cortex/command/sandboxproxy` | 182 | 41 |
| `language_server/code_assist_client` | 480 | 37 |
| `cortex/customizations` | 544 | 29 |

And 27 hand-written packages (≥8 functions) are identical line for line:
`cortex/battleworkspace` (70), `cortex/messages` (58),
`cortex/snapshot_recorder` (45), `snapshot_recorder/gitclient` (39),
`customization/hooks/jsonhook` (35), `gitignore` (31),
`cortex/utils/background` (24), `cortex/proto_saver` (22), `cortex/traj` (22),
`cortex/cascade_run_state` (21).

**The two products share one agent kernel; the IDE rewrote the product-facing
blocks and left the rest alone.**

### Three findings that came out of the widening

**1. The CLI has an "execute URL" grant policy; the IDE deleted it.**
`cortex/permissions` diverges hardest of any hand-written package
(`(*permissionManager).check`: 164 lines vs 256, similarity 0.105). The
CLI-only functions are a coherent feature: `GetExecuteURLPolicy`,
`ExecuteURLPolicyFromGrants`, `ExtractConversationGrants`,
`NewPermissionManagerWithConversationGrants`, `RegistrableDomain`,
`normalizeExecuteURLTarget`, `mergeScopedGrants`, `executeURLPromptTarget`,
`checkLocal`, `hasExplicitUserAllow`, `syncConversationGrants`. The IDE's
counterparts are `checkCompound`, `checkSingleTarget`,
`requiresExactMatch`, `redirectionRequiresExactMatch`,
`(*PermissionGrantStore).LookupEffective`. The string `ExecuteURL` occurs
**0** times in the language server and 5 times in the CLI.

The clone's `internal/permissions` implements allow/ask/deny rule matching and
has **no domain-scoped grant layer** — a candidate gap, pending a check of
whether the CLI surfaces it in its UI.

**2. The IDE's safety layer rewrites the tool call, and calls out to Google's
security services.**
`policyguardian.RewriteToolCall` (717 lines) parses the tool arguments as
JSON (`encoding_json_v2.Unmarshal`), walks the string arguments with two
regexes, and rewrites them from its own env-state ledger:

```
Resolved $%s → %s from env_state          ← expands $VAR
Resolved symlink %s → %s from env_state    ← resolves symlinks
Stripped placeholder tokens from argument %s
```

So in the reference, env-var expansion and symlink resolution happen *in the
safety layer*, before the tool runs.

The wider run also surfaced that the gate is not purely local: it queries
**Google SafeBrowsing** (`Checking URL with SafeBrowsing: %s` → `safe (no
threats detected)` / `threats detected: %v`, with a cache), **GAVS**
(`Checking URL with GAVS: %s` → `clean` / `flagged`), an **antivirus** check
(`Antivirus check failed for %s: %v`, with panic recovery), **deps.dev** for
dependency findings (`Checking dependency findings with deps.dev: %s://%s
(version=%s)`), the **GitHub API** for ref reachability (`Checking GitHub ref
reachability: repo=%s, ref=%s`, skippable via `EnableGitHubUserChecks`), and an
**LLM content scan** (`Scanning content with LLM (destination=%s, type=%s,
persistence=%t)`). The CLI has no equivalent of any of these.

The rest of the gate's lifecycle: `preHook` / `postHook` / `getStateHook` /
`setStateHook`, `Guardian.Verify` / `Guardian.Observe`, the verdict kinds
(`allow` / `ask_user` / `deny`, logged as `[PolicyGuardian] Step %d verdict:
kind=%v, summary=%s`), the *denied-repeat* memory ("denied-repeat pre-check
blocked: %s"), the `canonical_tool` / `rewrite_notes` fields (the verdict can
*rewrite* the call, not only allow or deny), the four auto-approve fast paths
(disabled guardian, empty command, loopback URL, sandbox fast path), and a
special "Sonar context".

**3. The IDE proxies all egress.**
`cortex/command/sandboxproxy` runs a local `127.0.0.1` HTTP proxy that every
outbound request goes through: an OAuth2 flow (`performOAuthFlow`,
`handleCallback`, `OAuth2Config.Inject` adding the `Authorization` header),
credentials in a `.env` file (`envFileAuthStore`, `{access_token}`), multi-
account support (`setDefaultAccount`), and **CEL** policy enforcement
(`celEnforcer.Check`, `translateRuleToCEL`, request bodies dispatched as
`bodyAsJSON` / `bodyAsGraphQL` / `bodyAsURLEncoded`).

One smaller one: the **built-in agent roster is the same in both products**
(`review-agent`, `jetski-guide`, `browser-vision`, `research-google`,
`DeepCoder`, `DeepInvestigator` — the strings are in both binaries) while the
*implementation* forked: the CLI has `BrowserAgentWithConfig` +
`ScriptHarnessText`, the IDE has `BrowserAgentWithBackend` +
`WithBrowserBackendInstructions`.

**A correction the widening forced.** `cortex/rehydration` was first filed
under "the surface the CLI cannot show". It is not: the package is **shared**
(171 names in the CLI, 169 in the language server, **130 byte-identical**).
The same correction applies to the "IDE-only tool types" claim — those six
names are IDE-only *by path*, not by substance: the CLI keeps them in the old
`oneharness/internal/tools/tools` package. The real tool-type counts are 17
(CLI, split across two packages) vs 15 (IDE, one package), with
`AskQuestionTool` and `FinishTool` CLI-only. Both claims are fixed in
`out/ide-language-server/README.md` §10.7 and §10.9.

`rehydration` turned out to be worth its own section anyway, because of the
design it implies: the reference stores the trajectory and **derives every
piece of live state from it** — `rehydrateDefineSubagent` (`tool_names` /
`agent_path` / `definition` / `SystemPrompt` / `EnableMCPTools` /
`EnableWriteTools`), `rehydrateSchedule` (`mode` / `outcome` / `iterations` /
`cancelled_by`), `rehydrateManageTask` (`action` / `input` / `task_id`),
`rehydrateManageSubagents` (`conversation_ids` / `killed_conversation_ids`),
`rehydrateSendMessage` (`recipient`). `(*CascadeManager)` owns the lifecycle
(`LoadTrajectory` / `SaveTrajectory` / `processLoadedTrajectory` /
`ResumeConversationsFromRestart` / `RevertToStep`), with a kill switch
(`cascade-disable-rehydration`) and a first-send latency metric
(`rehydration_enabled`).

The clone's `internal/convstore` already stores the trajectory (SQLite) and
already stores what a replay needs (`Step.ToolName` / `ToolArgs` / `Status`),
but its live state is process-local: `internal/subagenttools.definitionStore`
("conversation-scoped") and `internal/taskregistry.Registry` ("the table of
tasks for one process"). After `--continue` the transcript returns and the
agent's own state does not.

One negative result worth recording: `privacy/data_governance` (2,427 names)
and `google/ai/generativelanguage` (5,484) are **generated protobuf
catalogues** — after the filter, 537 and 845 functions respectively, all
message/enum definitions. Neither is runtime logic, so neither opened a new
behavioural seam.

### Reproducibility

| Check | Result |
|---|---|
| `build_priority.py` (default) | byte-identical to the committed list |
| `build_priority.py --wide` | byte-identical on re-run |
| `compare_dec.py` | `cross-check.md` / `cross-check.tsv` byte-identical on re-run |
| self-check (`dec-cli` vs itself) | 9,654 identical, 0 close, 0 differs |

## Pass 4 — the rehydration: the live state is a function of the trajectory

### The model

The reference keeps exactly one durable record of a conversation -- the
trajectory -- and derives the agent's world from it when the conversation is
loaded:

> Store only the trajectory, never the state. Every "live" thing (the
> subagents it defined, the schedules it created, the tasks it managed, the
> messages it owes) is a function of the stored steps, recomputed on load by
> the `cortex/rehydration` package.

【反编译】 the package has two layers: per-tool rehydrators dispatched by tool
name (`rehydrateDefineSubagent`, `rehydrateSchedule`, `rehydrateManageTask`,
`rehydrateManageSubagents`, `rehydrateSendMessage`, plus one per payload kind
-- `rehydrateReadFile`, `rehydrateFileEdit`, `rehydrateRunCommand`,
`rehydrateSearchWeb`, `rehydrateGenerateImage`, `rehydrateInvokeSubagent`,
`rehydrateViewFile`), and a readable/writable view of the trajectory itself
(`rehydratingTrajectory` / `rehydratingReadOnlyTrajectory`). The consumer is
`(*CascadeManager)`: `processLoadedTrajectory` rehydrates a loaded
trajectory, `shouldDisableRehydration` gates it behind the internal flag
`cascade-disable-rehydration`, and the telemetry name `rehydration_enabled`
hangs off the first-send latency -- so the pass is on the critical path of
the first turn, and the reference ships a switch to turn it off.

### What the stored data actually looks like 【一手】

540 real reference conversations were scanned (19,600 steps):

| step type | meaning | count |
|---|---|---|
| 14 | user input | 509 |
| 15 | model response | 15,316 |
| 17 | error | 73 |
| 23 | subagent | 53 |
| 101 | system notice | 857 |
| **132** | **tool call** | **14,838** |

The tool calls' status distribution is `3` = 4,072, `5` = 10,709, `4` = 2,
`6` = 31, `7` = 24. **3 and 5 both mean "it ran"**: one conversation
(`86add28f-...`) switches cleanly from 5 to 3 at index 22367/22368, so the
reference's status enum changed and the current writer emits 3 while the
older one emitted 5. The failures are 4/6/7 -- exactly the set the clone's
own `loadedStepErr` treats as errors. The rehydration filter is therefore
"not 4/6/7", not "equals 3".

The stored payload (dumped field by field from `86add28f-...`):

```
step_payload
  1: varint 132          (step type)
  4: varint 3|5         (status)
  5: len-delimited       (the tool-call payload)
       4: len-delimited  (the call itself)
            1: str  'call_322216'   (call id)
            2: str  'run_command'   (tool name)
            3: bytes (the model's raw JSON arguments)
  140: len-delimited     (the result)
       2 -> 1: (the structured result)
```

The stored arguments use the **capitalised native-payload names**
(`{"Name","Description","SystemPrompt","EnableWriteTools",...}`), while the
model-facing declarations use the lower-case spelling (`name`,
`system_prompt`, `enable_write_tools`) -- so a rehydrator has to accept
both. The metadata keys the reference writes (checked against
`strings-resolved.txt`) are `name`/`Name`/`description`/`Description`/
`SystemPrompt`/`system_prompt`/`Enable*Tools`/`enable_*_tools` for
`define_subagent`, `action`/`input`/`task_id`/`task_ids` for `manage_task`,
`action`/`conversation_ids`/`killed_conversation_ids` for
`manage_subagents`, `recipient`/`recipient_name` for `send_message`, and
`mode`/`outcome`/`iterations`/`cancelled_by` for `schedule`; lists are
comma-joined and `killed_conversation_ids` is a JSON array. The legacy
`__original_cortex_step` wrapper occurs **zero** times across the 540
conversations -- `MigrateLegacyPayload` is history compatibility now, not a
live path.

### What the clone does with it

`internal/rehydration` is the new hub:

- `Step` / `Metadata` / `Delta` -- one stored step and the
  `map[string]string` delta it produced (the reference's shape);
- `Dispatcher` -- routes a tool call to the rehydrator registered under that
  tool's name, and skips the steps that did not run to completion;
- `Trajectory` -- the read-only view (`NumSteps`, `Step`, `Steps`,
  `StepIndicesByType`, `StepIndicesExcludingStatuses`, `ToolCallSteps`);
- `MigrateLegacyPayload` -- unwraps the legacy `__original_cortex_step`;
- `Enabled()` -- the kill switch, `OPENAGY_DISABLE_REHYDRATION` in the clone,
  `cascade-disable-rehydration` in the reference. The reference's flag is
  internal: the name is in the string table but **not** in the language
  server's `--help`, so an environment variable is not a divergence from a
  user-visible switch.

The per-tool rehydrators live in the package that owns the tool
(`internal/subagenttools/rehydrate.go`, `internal/tools/rehydrate.go`), so
the metadata keys and the live handler that writes the same state sit in one
file -- and the rehydrator parses the stored arguments with **the live
handler's parser**, so a rehydrated state and a live state agree by
construction.

The state the clone can restore: the subagent definitions (now in a
conversation-scoped store -- the TUI rebuilds the tool set every turn, so a
definition used to die between turns), the schedule table with its
per-conversation task-id counter, the task registry's finished records, and
the killed-subagent set. The pass runs where a conversation is adopted: the
TUI's startup resume, `/continue` and `/resume` all funnel through
`seedConversation`, and the print mode has its own entry for a continued
conversation.

### The trajectory itself had to be completed first

The clone wrote only the user and assistant steps -- tool calls were never
written -- so the clone's own conversations had no trajectory to rehydrate,
and (because the clone writes to the same store the reference uses) resuming
a reference conversation replaced its stored trajectory with that thinner
record. `internal/cliapp/trajectory.go` fixes both: the steps a conversation
was adopted with are kept as they were, the turns that run since are appended
to them with the numbering continued, and a tool call is stored in the
reference's own step shape (132: `5 -> 4 -> {2: tool name, 3: JSON args}`,
the result in `140`). A fork now copies the trajectory, tool calls included.

### Four known divergences from the reference

1. **Killed ids**: the reference reads `killed_conversation_ids` from the
   call's *result* metadata, which the clone does not store -- the clone
   derives them from a successful `kill`/`kill_all` action's arguments.
2. **`send_message`**: the clone's delegations are synchronous, so there is
   never a live recipient; the rehydrator restores the recipient and the
   message, and the applier is an intentional no-op. The reference's
   `recipient_name` comes from a field of its native payload that the
   clone's arguments do not carry, so it cannot be recovered.
3. **`manage_task`**: the reference manages logical tasks, the clone manages
   background commands -- the clone restores the reference-shaped logical
   tasks into the task registry as finished records.
4. **Schedules**: the reference's schedules ran, so `outcome` and
   `cancelled_by` are meaningful; the clone's never ran, so they are empty
   and `iterations` is 0. The clone adds the *definition* fields
   (`Prompt`, `DurationSeconds`, `CronExpression`, `TimerCondition`,
   `IsDaemon`, `MaxIterations`) under the names the reference stores them
   under.

### Verification

- `go test ./cmd/... ./internal/...` -- green except three pre-existing
  failures in `internal/auth` (the credential-refresh tests), which are
  outside this change.
- New tests: `internal/rehydration/rehydration_test.go` (dispatch, the status
  filter, the legacy migration, the kill switch, the trajectory helpers),
  `internal/subagenttools/rehydrate_test.go` and
  `internal/tools/rehydrate_test.go` (the five rehydrators and their
  appliers), `internal/cliapp/trajectory_test.go` (the round trip through
  the store, and a resume that rehydrates the definitions).

## What was moved into the repo

`out/ide-language-server/` (59 MB, all regenerable):

| File | What |
|---|---|
| `README.md` | the manifest and the findings (zh-CN, per the `out/` convention) |
| `build_evidence.py` | regenerates every artefact below from the app bundle |
| `build_priority.py` | selects the decompile list (5 parts; `--wide` for the full one) |
| `compare_dec.py` | compares the two binaries' pseudocode (multi-dir CLI side) |
| `package_strings.py` | slices `strings-resolved.txt` by package |
| `build_reports.py` | the per-package decompile reports |
| `identity.txt`, `stamp.txt`, `help.txt` | the binary's identity, `--stamp`, `--help` |
| `seeds/funcs.txt`, `seeds/seeds.txt` | 164,772 recovered names, REA seed format |
| `name-diff/` | the CLI-only and IDE-only name sets + the per-package table |
| `renamed-pairs.tsv` | the 100 `oneharness` -> `jetski` path pairs |
| `prompt/` | the CLI-only / IDE-only prompt lines |
| `tools/matrix.tsv` | the 60 tools x {CLI, IDE} presence matrix |
| `priority-funcs.txt`, `priority-funcs-cli.txt` | the default decompile lists (2,845) |
| `priority-funcs-wide.txt`, `priority-funcs-wide-cli.txt` | the wide lists (14,555 / 9,902) |
| `cross-check.md`, `cross-check.tsv` | the comparison report and the per-function table |
| `strings-resolved.txt` | 11,598 `&UNK_` -> string resolutions with their functions |
| `decompile-summary.md`, `decompile-*.md` | the per-package decompile reports (2,536) |
| `ide-js/` | the IDE's 35 own JS files (minified product code) |
| `skills/` | the 7 bundled skills + the package's LICENSE |
| `ghidra-scripts/DecompilePriority.java` | the decompile script (CLI's copy, paths overridable) |

Plus `docs/guide/ide-language-server.md` (the guide entry) and this report.

**Not** moved: the 161 MB binary (like the CLI's, it stays on disk), the
680 MB Ghidra project, the decompiled `.c` files (93 MB + 65 MB, regenerable
in about six minutes), the Electron runtime, the vendored `node_modules`, and
`webm_encoder`.

## Verification

- `python3 out/ide-language-server/build_evidence.py` -> clean, 8.1 s end to end
  (3.6 s of it is the seed pass).
- `language_server --stamp` and `--help` -> exit 0, captured verbatim.
- The seed file is in REA's format and the tool reports
  `REA_GHIDRA_SEED_FILE=... rea analyze ... --provider ghidra` as the next step.
- The Ghidra pass: 164,753/164,772 seeds applied (0 failed); 14,555/14,555
  language-server functions decompiled (0 errors, 185 s) and 9,902/9,902 on the
  CLI side (0 errors, 158 s).
- `compare_dec.py` is self-consistent: run against the CLI's own `dec-cli/`
  twice it reports 9,654/9,902 identical and **0** differing -- so a
  non-identical verdict is a real difference, not a normalisation artefact.
- Both the priority builder and the comparison are reproducible: re-running
  them yields byte-identical `priority-funcs-wide.txt`, `cross-check.md` and
  `cross-check.tsv`.
- Every relative link in `README.md`, the guide entry and this report resolves.

## 存疑

1. **"Same build" is not proven.** The build infos share the CL and the commit
   short hash, but the toolchain release differs (RC01 vs RC02) and the
   `--stamp` CL (`1005016104`) is a different numbering system from the build
   info's `cl/1000353293`. What is defensible: *same CL family, same commit*.
2. **83/698 is a lower bound on prompt overlap**, not the overlap: the prompt
   is assembled from fragments at runtime, so most lines are not contiguous
   strings in either binary.
3. **The IDE's JS is minified product code.** Only verbatim quotes are 【一手】;
   anything inferred from it is at 【strings】 level.
4. **"Identical pseudocode" is a machine-code statement, not a source
   statement.** Both binaries were decompiled by the same Ghidra (12.1.4); equal
   output means equal *implementation*, not equal *source* -- the compiler
   version, the link layout and the inlining decisions all differ between two
   builds of the same tree.
5. **The comparison normalises three things that could hide a real
   difference**: address-bearing labels (`LAB_`/`DAT_`/`UNK_` -- they *must*
   differ), the decompiler's local-variable numbering (one extra local
   renumbers all the later ones) and, for the renamed-path pairs, the import
   path prefix. Everything still classified as "differs" after that is a real
   difference; everything classified as "identical" could hide a difference
   *inside* those three classes.
6. **10,285 of 147,120 shared functions were compared** (7%). The comparison
   population is the *intersection of what both sides decompiled*, so a
   function the CLI run never decompiled is invisible here. The 8% divergence
   rate is a statement about the **selected** packages, not about every shared
   function.
7. **The "execute URL" gap is a candidate, not a confirmed defect.** The
   clone's `internal/permissions` has no domain-scoped grant layer and the CLI
   binary has one. Whether that layer is user-visible in the CLI (and therefore
   observable in the clone's TUI) was **not** established.
8. **Packages are import-path prefixes, not features.** `cortex/cortex` mixes
   a dozen subsystems, so "41 of 373 differ in `cortex/cortex`" cannot be read
   as "41 features differ" without naming the functions.

## Limitations

- The 17,551 IDE-only names are *catalogued*, not *analysed*: 2,830 of them
  were decompiled (the packages that carry product meaning), the rest are
  catalogued only.
- The IDE's own `--help` was not diffed flag-by-flag against the CLI's; the
  shared/product boundary is inferred from the package table, not proven
  flag-by-flag.
- The decompiled `.c` files (158 MB) are git-ignored -- they are regenerable
  in about six minutes from the committed priority lists and the scripts.
- The `policyguardian` regex profiles were read as *strings*; the rules that
  decide which of them apply to a given call were not traced through the
  decompiled control flow.
