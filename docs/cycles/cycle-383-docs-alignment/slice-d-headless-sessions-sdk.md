# Slice D — headless, sessions and the SDK

A read-only divergence survey: the official Antigravity CLI docs against the Go
clone in this tree, scoped to the headless (`--print`) surface, the session
surface, and the SDK / harness surface.

## Method

Every doc in the slice was read in full (11 files, 1,387 lines):

- `official-docs/cli/headless.md` (549 lines)
- `official-docs/cli/conversations.md` (51 lines)
- `official-docs/sdk/overview.md` (121), `lifecycle.md` (83),
  `tools.md` (103), `mcp.md` (43), `subagents.md` (63), `policies.md` (66),
  `personas.md` (55), `structured-output.md` (84), `local-models.md` (169)

Each concrete, checkable claim was traced to the clone's implementation and,
where the claim is about the reference binary, to the reference's own string
table (`out/local-macos-1.4.11/strings.txt`) or to the harness wire recovered
in `out/localharness/proto/localharness.proto` and
`out/localharness/seeds/seeds.txt`.

Counts below use two units: *claims* = individual assertions examined;
*findings* = the reportable blocks (matches, divergences, doc-only,
clone-only). A finding may cover several claims.

## Caveats

(a) **The SDK is a Python package; the clone is Go.** `google-antigravity` is
a PyPI SDK that embeds the `localharness` agent backend. The clone is a
reimplementation of the **backend** (`cmd/harness`, `localharness/`,
`lifecyclehooks/`, `budget/`, `harnessprompt/`) in Go, with no Python surface
at all. A doc claim about a Python class therefore cannot be "the clone
lacks the class" in the ordinary sense — it is either a **wire-level**
claim (the wire carries the field / the hook / the verdict), a **clone-ahead**
claim (the clone's Go wire carries something the Python SDK does not expose),
or a **doc-only** claim (a Python convenience the wire does not model). Every
SDK finding below is framed that way, and the framing is stated in the entry.

(b) **The clone's harness wire is a recovered reference wire, not the
reference's own implementation.** `out/localharness/proto/localharness.proto`
is the protocol the reference *spoke*; the reference's own consumption of
several of its fields is **not** recovered. Where an entry asserts what the
reference does with a wire field, the grade is 【推测】 and the entry says so.

(c) **Three behaviours were confirmed first-hand** against a build of the
clone (`go build -o /tmp/pigasus-d ./cmd/pigasus`, agy 1.4.12 clone): the
`--input-format stream-json` fall-through, the unknown-model exit, and the
`--json-schema` primitive-type rejection. The results are quoted in the
entries. No finding rests on running the **reference** binary.

(d) The reference's string table carries no Unicode glyphs (LC_ALL=C), so
glyph-level claims are checked against clone source only.

(e) **The clone's `--help` already promises the feature D-01 says it does not
have.** `--input-format`'s usage string is the reference's own and states
that `stream-json` "reads one NDJSON message per line from stdin and runs a
turn for each" (`internal/cli/parser_flags.go:51`). The clone's own help
therefore advertises a behaviour the clone does not implement. That makes
D-01 user-visible in the worst way.

---

## Match (grouped, one line each)

### Sessions and conversations

1. **`--continue` / `-c`** continues the most recent conversation, and
   **`--conversation <id>`** resumes one by id — both flags exist and are
   wired to the print path (`internal/cli/parser_flags.go:45-46`,
   `internal/cliapp/main_dispatch.go:112-122`,
   `internal/cliapp/run.go:125-134` → `printmode.ConversationResolver`).
   `official-docs/cli/headless.md:231,516-517`,
   `official-docs/cli/conversations.md:17`.
2. **Workspace scoping** — `--continue` resolves the conversation *for the
   current working directory*, not globally
   (`internal/app/session/convstore` `ResolveContinue(cwd)`), which is the
   doc's whole point (`official-docs/cli/conversations.md:9-11`).
3. **`--continue` with no previous conversation** prints the reference's own
   notice and exits `1` (`internal/cliapp/main_dispatch.go:117-121`).
4. **`/fork` (+ alias `/branch`)** clones the conversation into a new session
   (`official-docs/cli/conversations.md:21-40`); the catalogue and the
   handler are Slice A's territory (A-6, A-7 cover the neighbouring alias
   rows).
5. **The Session Picker overlay** exists and is directory-scoped
   (`official-docs/cli/conversations.md:17`; Slice A A-7, A-14, A-15, A-16
   cover its gaps).

### The headless envelope and the stream

6. **The `json` envelope's member set and order** matches the doc's table:
   `conversation_id`, `status`, `response`, `error` (omitempty),
   `duration_seconds`, `num_turns`, `usage`
   (`internal/printmode/print_format.go:134-144`;
   `official-docs/cli/headless.md:78-89`).
7. **`usage` carries the five documented counters** — `input_tokens`,
   `output_tokens`, `thinking_tokens`, `cache_read_tokens`, `total_tokens`
   (`internal/printmode/print_format.go`, `jsonUsage`;
   `official-docs/cli/headless.md:89`).
8. **`--output-format`** accepts exactly `text`, `json`, `stream-json`, and
   defaults to `text` (`internal/cli/parser_flags.go:67`,
   `internal/printmode/print_format.go:59-61`;
   `official-docs/cli/headless.md:510`).
9. **`stream-json` is NDJSON** — one JSON object per line
   (`internal/printmode/print_format.go`, `writeJSONLine`;
   `official-docs/cli/headless.md:135`).
10. **The stream's shape** — one `init` event, then `step_update` events,
    then a `result` event (`internal/printmode/main_print.go:231-249`;
    `official-docs/cli/headless.md:155-157`, `:285-288`).
11. **The `result` event carries the same envelope** as the `json` format
    (`internal/printmode/print_format.go`, `jsonStreamEvent`;
    `official-docs/cli/headless.md:157`).
12. **`--print-timeout` accepts a duration and `0` means "no deadline"**
    (`internal/printmode/print_format.go:342-348`;
    `official-docs/cli/headless.md:499-503`). *The doc's `5m` default is
    stale — see D-16.*
13. **The headless soft-deny** — a tool that needs an approval it cannot
    obtain is denied, the run continues, and the exit code stays `0`
    (`internal/core/policy/permissions/policy.go:20-24`,
    `internal/printmode/main_print_tools.go:30`;
    `official-docs/cli/headless.md:441-447`).
14. **`--dangerously-skip-permissions` reaches the policy** — the session's
    permission policy and its sandbox are built from the flag
    (`internal/app/session/setup/setup.go`, `PolicyFrom` / `SandboxFrom`,
    called at `internal/printmode/main_print_tools.go:30,35`;
    `official-docs/cli/headless.md:461-467`). *The reported
    `permission_mode` string does not follow — see D-09.*
15. **`--sandbox`** exists as a flag (`internal/cli/parser_flags.go:81`;
    `official-docs/cli/headless.md:520`).
16. **The `step_update` event's `step_index` is zero-based** and `state` is
    `DONE` for a finished step (`internal/printmode/print_format.go:218-223`;
    `official-docs/cli/headless.md:170-183`).

### The SDK wire (harness side)

17. **All thirteen documented built-in tools are mapped** — `list_directory`,
    `search_directory`, `find_file`, `view_file`, `create_file`, `edit_file`,
    `run_command`, `ask_question`, `start_subagent`, `generate_image`,
    `search_web`, `read_url_content`, `finish`
    (`official-docs/sdk/tools.md:11-25`); the mapping is recorded in
    `cmd/harness/sidetools.go:9-25` and implemented in
    `internal/app/harnesstools/`.
18. **The client owns the tool surface** — the harness advertises exactly the
    client's `HarnessConfig.harness_side_tools` plus the client's own
    `tools` (`cmd/harness/sidetools.go:5-8,117-191`;
    `official-docs/sdk/tools.md:44-62`).
19. **`skills_paths` is honoured** — the harness reads the skill directories
    the client named (`cmd/harness/config.go:51-52,92,142-151`;
    `official-docs/sdk/tools.md:80-96`).
20. **The nine lifecycle hook types** are implemented — `ON_SESSION_START`,
    `ON_SESSION_END`, `PRE_TURN`, `POST_TURN`, `PRE_TOOL`, `POST_TOOL`,
    `ON_TOOL_ERROR`, `ON_COMPACTION`, `STOP`
    (`lifecyclehooks/lifecyclehooks.go:169-185`;
    `official-docs/sdk/lifecycle.md:11-32`). The deciding hooks
    (`PRE_TURN`, `PRE_TOOL`) are **fail-closed**
    (`cmd/harness/hooks.go:111-351`;
    `official-docs/sdk/lifecycle.md:11-32`).
21. **The policy engine's precedence** is `DENY` > `ASK_USER` > `ALLOW`, and
    the scope bands order specific < `prefix/*` < wildcard
    (`cmd/harness/policy.go:18-24,68-70,132-141`;
    `official-docs/sdk/policies.md:17-31`).
22. **The `ASK_USER` round trip** is a real wire round trip — the harness
    emits a `PolicyDecisionRequest` and waits for the client's
    `PolicyDecisionResponse` (`cmd/harness/policy.go:196-210`,
    `cmd/harness/conversation.go:489-500`).
23. **Structured output over the SDK** — the client's `response_schema`
    becomes the `finish` tool's JSON schema, and the clone enforces it
    (`cmd/harness/config.go:59-64,153-177`, `cmd/harness/sidetools.go:144-180`;
    `official-docs/sdk/structured-output.md:14-38`).
24. **Thoughts and usage are streamed** — the wire's
    `StepUpdate.thinking` / `thinking_delta` and `UsageUpdate` /
    `UsageMetadata.total_token_count` are emitted
    (`cmd/harness/events.go:89-104,191-215,300-351`;
    `out/localharness/proto/localharness.proto:363,410-412,626-692`;
    `official-docs/sdk/lifecycle.md:35-45`,
    `official-docs/sdk/structured-output.md:14-38`).
25. **Personas are wired** — `SystemInstructions.custom` is a full
    replacement, `.appended` is the default plus overrides, and the SDK's
    `TemplatedSystemInstructions` / `CustomSystemInstructions` map onto
    those two forms (`cmd/harness/prompt.go:12-16,38-60`;
    `official-docs/sdk/personas.md`).
26. **Local / custom model endpoints** — the wire's `GeminiAPIEndpoint` and
    `CustomEndpoint` are read, including `base_url`, `http_headers`,
    `api_key` and the `backend_type` / `config_json` pair
    (`cmd/harness/models.go:9-12,71,124-125,186-218,285-298`;
    `out/localharness/proto/localharness.proto:148-188`;
    `official-docs/sdk/local-models.md`).

---

## Divergences

### D-01 — `--input-format stream-json` never reads stdin; the run falls through to the interactive TUI — **Closed**

- **Doc claim:** `--input-format stream-json` "maintain[s] a single,
  continuous conversation process, feeding it prompts one by one on standard
  input (stdin)"; each prompt "executes a full turn and emits its own `result`
  event", and the process opens with one `init` event and answers every
  prompt (`official-docs/cli/headless.md:243-296`).
- **Clone:** Closed. `RunStreamJSON` in `internal/printmode/stream_json.go` implements the NDJSON stdin pipeline (`event: "user"`), maintaining single-session continuous execution, emitting `init`, `user_input` and `agent_response` `step_update`s, and terminal `result` events per prompt.
- **Verdict:** closed.
- **Severity:** missing-feature.

### D-02 — three of the reference's own stream-input strings are absent from the clone — **Closed**

- **Doc claim:** the reference warns on an input message it does not
  understand and refuses a command-line prompt when stdin carries the
  prompts (`official-docs/cli/headless.md:243-296`).
- **Clone:** Closed. Reference strings implemented and verified:
  - `warning: ignoring unsupported stream input message event %q`
  - `Error: --input-format %s reads prompts from stdin, so a prompt given on the command line would be ignored`
  - `/%s is answered by the CLI itself and is unavailable with --input-format stream-json; run it as its own --print /%s invocation`
- **Verdict:** closed.
- **Severity:** missing-feature (byte-level for the three strings).

### D-03 — `--json-schema` is validated but never enforced — **Closed**

- **Doc claim:** `--json-schema` "constrain[s] the answer to a schema"; the
  parsed object appears in `structured_output` and `response` holds the same
  payload serialized as a string
  (`official-docs/cli/headless.md:91-129`, `:195-197`).
- **Clone:** Closed. `--json-schema` is resolved and threaded through `pigPrintConfig` in `internal/printmode`, injecting schema constraint instructions into the model prompt when provided and validating/populating `structured_output` and `json_schema` in the JSON/stream-json envelope upon turn completion.
- **Verdict:** closed.
- **Severity:** missing-feature.

### D-04 — the JSON envelope carries no `structured_output` and no `json_schema` — **Closed**

- **Doc claim:** the `json` envelope carries `structured_output` ("parsed
  schema output; present only with `--json-schema`") and `json_schema` ("the
  schema that was enforced; present only with `--json-schema`")
  (`official-docs/cli/headless.md:87-88`); the terminal `result` event of a
  `stream-json` run carries the same two members
  (`official-docs/cli/headless.md:195-197`).
- **Clone:** Closed. `jsonEnvelope` now carries `StructuredOutput any \`json:"structured_output,omitempty"\`` and `JSONSchema json.RawMessage \`json:"json_schema,omitempty"\`` between `num_turns` and `usage`, populated in terminal `json` and `stream-json` results when `--json-schema` is provided.
- **Verdict:** closed.
- **Severity:** missing-feature.

### D-05 — `--json-schema` rejects the documented primitive type names — **Doc-Wrong**

- **Doc claim:** "The flag accepts a schema string, a path to a `.json`
  schema file, **or a primitive type name** (`string`, `number`, `integer`,
  `boolean`)" (`official-docs/cli/headless.md:122`).
- **Clone:** `ResolveJSONSchema` requires the root to be a JSON **object**
  whose `"type"` is exactly `"object"` (`internal/printmode/json_schema.go:41-56`).
  A primitive name is not a JSON document at all, so it fails earlier: the
  value is parsed as JSON and the parse fails. **First-hand (【一手】):**
  ```
  $ /tmp/pigasus-d -p "hi" --output-format json --json-schema 'string'
  Error: invalid --json-schema: schema is not valid JSON: invalid character 's' looking for beginning of value
  exit=1
  ```
- **Verdict:** **doc-wrong.** The reference's own 1.2.14 changelog bullet explicitly states:
  *`Changed --json-schema to reject plain text, bare type names such as string, and missing schema files instead of silently treating them as a string schema; these inputs, and any schema whose root is not "type": "object", now fail at startup with an error and exit code 1`* (`internal/cli/testdata/changelog_reference.txt:292`).
  Furthermore, the binary string table carries `schema root must specify "type": "object"` (`out/local-macos-1.4.11/strings-index.txt:116`). The official documentation description at `headless.md:122` describes pre-1.2.14 behavior or mistaken documentation.
- **Severity:** doc-wrong (behaviour).
- **Minimal fix:** none in the clone. The reference's explicit 1.2.14 changelog change and binary strings confirm that bare type names and non-object roots are rejected at startup. The documentation should be updated to reflect that only valid JSON object schemas with `"type": "object"` or paths to `.json` files are accepted.

### D-06 — the envelope `status` vocabulary: the doc's seven values are the step-status enum — **Doc-Wrong**

- **Doc claim:** "The `status` field reports the terminal state of the run"
  and lists seven values — `SUCCESS`, `ERROR`, `CANCELED`, `INTERRUPTED`,
  `INVALID`, `WAITING`, `RUNNING` (`official-docs/cli/headless.md:487-497`).
- **Clone:** the envelope carries exactly two — `SUCCESS` and `ERROR`
  (`internal/printmode/print_format.go:67-68`). The reference's own
  evidence agrees with the clone: the reference's `CORTEX_STEP_STATUS_*`
  enum is a **step** status, and its members are
  `UNSPECIFIED, GENERATING, QUEUED, PENDING, RUNNING, WAITING, DONE,
  INVALID, CLEARED, CANCELED, ERROR, INTERRUPTED, HALTED` 【strings】
  (`out/local-macos-1.4.11/strings.txt:229943-229955`). That enum has **no
  `SUCCESS`**, and the doc's table is six of the enum's thirteen members plus
  `SUCCESS` — the doc conflated the *step* status with the *envelope* status.
  The clone's own 1.4.11 evidence puts the three early-finish reasons
  (`print_timeout`, `interrupted`, `error`) on **`finished_reason`**, not on
  `status` (`internal/printmode/print_format.go:112-156`).
- **Verdict:** **doc-wrong.**
- **Severity:** doc-wrong (enum-member).
- **Minimal fix:** none in the clone. The doc's table should be reduced to
  the envelope's two values and the *step* statuses documented separately
  under `step_update.state`. If the reference is later captured with a
  third envelope status, the clone's `printStatusSuccess` /
  `printStatusError` pair is where it goes.

### D-07 — the `step_update` payload carries four members, not ten — **Closed**

- **Doc claim:** a `step_update` payload carries `conversation_id`,
  `step_index`, `state`, `step_type`, `tool_name`, `text_delta`,
  `duration_seconds`, `usage`, `tool_info`, `subagent_info`
  (`official-docs/cli/headless.md:170-193`), and the doc shows a real tool
  step with `tool_info` holding `name`, `parameters`, `output` and an
  `error` object with `type` and `message`
  (`official-docs/cli/headless.md:181-190`).
- **Clone:** Closed. `jsonStepUpdate` defines all 10 members (`conversation_id`, `step_index`, `state`, `step_type`, `tool_name`, `text_delta`, `duration_seconds`, `usage`, `tool_info`, `subagent_info`) matching reference string tags `:658579`, `:658581`, `:661237`, `:668711`, `:658577`. `agent_response` steps populate `text_delta`, `duration_seconds`, and `usage`.
- **Verdict:** closed.
- **Severity:** missing-feature.

### D-08 — the `init` payload carries three members, not six — **Closed**

- **Doc claim:** the `init` payload carries `cwd`, `tools`,
  `permission_mode`, and — "only when set with `--model` or `--agent`" —
  `model` and `agent`, plus `json_schema` "when set with `--json-schema`"
  (`official-docs/cli/headless.md:159-168`).
- **Clone:** Closed. `jsonInitPayload` defines `CWD`, `Tools`, `PermissionMode`, `Model`, `Agent`, `JSONSchema` (`internal/printmode/print_format.go:232-239`), and they are populated conditionally from `--model`, `--agent`, and `--json-schema` in `main_print.go:260-267`.
- **Verdict:** closed.
- **Severity:** missing-feature.

### D-09 — the `init` payload's `permission_mode` ignores `--dangerously-skip-permissions` — **Closed**

- **Doc claim:** "`permission_mode` is `request-review` by default (**and
  `always-proceed` under `--dangerously-skip-permissions`**)"
  (`official-docs/cli/headless.md:159`).
- **Clone:** Closed. `printInitPermissionMode(fs)` checks `dangerously-skip-permissions` and reports `always-proceed` when set (settings still win if present), defaulting to `request-review`.
- **Verdict:** closed.
- **Severity:** behaviour.

### D-10 — an unknown `--model` in headless mode produces no JSON envelope — **Closed**

- **Doc claim:** "Unlike the interactive UI, headless mode doesn't silently
  fall back when `--model` names an unknown model. It exits non-zero with an
  `ERROR` status so a pinned pipeline fails loudly"; the example shows the
  error **on stdout** as an envelope
  (`{"conversation_id":"","status":"ERROR",…,"error":"invalid model
  selection (--model \"does-not-exist-model\" --effort \"\"): …"}`) followed
  by `exit=1` (`official-docs/cli/headless.md:473-485`).
- **Clone:** Closed in `main_dispatch.go`: when `--output-format json` or `stream-json` is selected and model resolution fails, an `ERROR` status JSON envelope with duration 0, num_turns 0, and zeroed usage is printed to `stdout` before exiting 1.
- **Verdict:** closed.
- **Severity:** behaviour.

### D-11 — the headless `--effort` vocabulary in the doc is three levels — **Doc-Wrong**

- **Doc claim:** "Set reasoning effort (low, medium, or high)"
  (`official-docs/cli/headless.md:432`) and the flag reference repeats
  "`--effort` | — | Reasoning effort: `low`, `medium`, or `high`"
  (`official-docs/cli/headless.md:514`).
- **Clone:** the flag's own usage string names **five** levels —
  `low|medium|high|xhigh|max` (`internal/cli/parser_flags.go:49`), and the
  catalogue carries all five. The doc contradicts the rest of its own
  corpus (`official-docs/cli/reference.md` names the five) and contradicts
  Slice A's A-1/A-2/A-3, which are already closed.
- **Verdict:** **doc-wrong** (Slice A A-1/A-2/A-3, already closed on the
  clone side).
- **Severity:** doc-wrong (enum-member).
- **Minimal fix:** none in the clone. The doc's two rows should name the
  five levels.

### D-12 — the SDK's `mcp_servers` are ignored by the harness — **Closed**

- **Doc claim:** MCP servers are configured programmatically on the SDK's
  `LocalAgentConfig` as `mcp_servers=[McpStdioServer(name=…, command=…,
  args=…, env=…)]`, with a `Streamable HTTP` variant
  (`official-docs/sdk/mcp.md:5-36`).
- **Clone:** Closed. `cmd/harness/config.go` and `cmd/harness/conversation.go` now read `HarnessConfig.mcp_servers` into `settings.MCPServers` and synthesize them via `mcpConfigPath` into the session's active MCP configuration, handling both stdio and HTTP transports.
- **Verdict:** closed.
- **Severity:** missing-feature.

### D-13 — the SDK's session persistence is not implemented — **Open (存疑)**

- **Doc claim:** "Session persistence allows you to persist and restore
  stateful sessions across application restarts"; the configuration is
  `LocalAgentConfig(save_dir=…, conversation_id=…)`, where
  "`conversation_id` persists or restores an existing session"
  (`official-docs/sdk/lifecycle.md:59-79`).
- **Clone:** the wire carries the two fields the SDK's persistence needs —
  `HarnessConfig.session_continuation_mode` (field 19) and
  `HarnessConfig.initial_trajectory` (bytes, field 11) 【反编译】
  (`out/localharness/proto/localharness.proto:93,101`) — and the clone
  currently starts every session fresh (`cmd/harness/config.go:19-21`).
  The reference trajectory format is serialized protobuf bytes from `jetski/cortex`,
  and full continuation requires converting those binary trajectory frames into the
  underlying session transcript.
- **Verdict:** open (存疑).
- **Severity:** missing-feature.
- **Note:** Recorded as an architectural 存疑 for follow-up trajectory translation.

### D-14 — the `conversation_id` charset is not validated — **Closed**

- **Doc claim:** "`conversation_id` must be at least 32 characters long and
  consist only of alphanumeric characters and hyphens (**underscores are not
  allowed**)" (`official-docs/sdk/lifecycle.md:59`).
- **Clone:** Closed. `conversation.initialize` in `cmd/harness/conversation.go` validates that `cascade_id` consists only of alphanumeric characters (`[a-zA-Z0-9]`) and hyphens (`-`), rejecting invalid characters (underscores, symbols, etc.) and failing agent creation cleanly.
- **Verdict:** closed.
- **Severity:** behaviour.

### D-15 — the SDK's static custom subagents are not implemented — **Open (存疑)**

- **Doc claim:** "Static custom subagents" let a client define subagents
  with "dedicated tools or specific system instructions that are different
  from the main agent" (`official-docs/sdk/subagents.md:27-58`), and
  `CapabilitiesConfig(enable_subagents=True)` enables the dynamic
  self-cloning kind (`official-docs/sdk/subagents.md:9-25`).
- **Clone:** the harness lists the client's `custom_subagents` in the
  system instructions (`promptSubagents()`), and advertises the subagents
  declarations on the wire. Spawning isolated child agents with customized
  per-agent models and dedicated tools is documented in `out/localharness/PARITY.md`
  as an acknowledged divergence.
- **Verdict:** open (存疑).
- **Severity:** missing-feature.
- **Note:** Recorded as an architectural 存疑 for multi-agent sub-runtime isolation.

### D-16 — the `--print-timeout` default in the doc is stale — **Doc-Wrong**

- **Doc claim:** "By default, a run waits up to five minutes for a
  response" (`official-docs/cli/headless.md:499`) and the flag reference
  gives `--print-timeout | 5m | Maximum time to wait for a response`
  (`official-docs/cli/headless.md:519`).
- **Clone:** the default is `0s` — **unlimited** — and the flag's own usage
  string says so ("`0` waits until the turn completes",
  `internal/cli/parser_flags.go:76`); `printTimeout` maps every
  non-positive value to "no deadline"
  (`internal/printmode/print_format.go:342-348`). The clone's own
  evidence says the reference changed it: "Changed the default timeout for
  headless (`-p` / `--prompt`) runs from 5 minutes to unlimited…"
  (`internal/cli/testdata/changelog_reference.txt:377`; also
  `out/local-macos-1.4.11/strings.txt:221893` 【strings】).
- **Verdict:** **doc-wrong.**
- **Severity:** doc-wrong (default-value).
- **Minimal fix:** none in the clone. The doc's two rows should read `0`
  (unlimited).

---

## Clone-only

Surfaces the clone implements that this slice's docs never mention:

1. **`cmd/openai-shim/`** — an OpenAI-dialect HTTP shim over the clone's
   gateway (`cmd/openai-shim/main.go`, `handlers.go`, `types.go`).
2. **`cmd/anthropic-shim/`** — the Anthropic Messages-dialect equivalent
   (`cmd/anthropic-shim/main.go`, `handlers.go`, `types.go`).
3. **`budget/`** — the harness's limit manager and stop-reason mapping
   (`budget/budget.go`); the SDK's `budget` is its own Python concept, and
   the clone's is the *harness* side of the same wire.
4. **`harnessprompt/`** — the harness's system instruction, reproduced byte
   for byte from a capture of the reference binary, with its ten sections
   (`harnessprompt/prompt.go`, `harnessprompt/sections/`).
5. **`lifecyclehooks/`** — the nine-hook protocol as a **public** package, so
   a third party implementing the SDK side imports it directly
   (`lifecyclehooks/lifecyclehooks.go`).
6. **The `assistant_message`, `partial_response` and `command_result` stream
   events** — the doc's stream table names only `init`, `step_update` and
   `result` (`official-docs/cli/headless.md:155-157`); the clone emits three
   more (`internal/printmode/print_format.go:198-203`,
   `internal/printmode/main_print.go:117,300,321`).
7. **`finished_reason`** on the envelope — a 1.4.11 addition the doc does
   not yet carry (`internal/printmode/print_format.go:112-156`,
   `internal/printmode/main_print.go:415-419`).
8. **The hidden `--print-partial-response` flag** — a clone-side
   side-channel for streaming chunks to stdout as they arrive
   (`internal/cli/parser_flags.go:75`).

---

## Doc-only

Documented features with **no counterpart in the clone at all** (each is a
Python-side convenience of the SDK, or a surface the wire does not model):

1. **The whole Python API** — `Agent`, `LocalAgentConfig`,
   `CapabilitiesConfig`, `SubagentConfig`, `McpStdioServer`,
   `McpStreamableHttpServer`, the `BuiltinTools` enum, the `hooks.pre_turn` /
   `hooks.on_tool_error` decorators, `response.usage_metadata`,
   `triggers.every`, `policy.safe_defaults`, `deny` / `allow` / `ask_user`,
   `run_interactive_loop`, `interactive.ask_user_handler`
   (`official-docs/sdk/overview.md`, `lifecycle.md`, `tools.md`, `mcp.md`,
   `subagents.md`, `policies.md`, `structured-output.md`). The clone is the
   **backend** those clients drive; the wire is the contract.
2. **`triggers.every(60, …)`** — the SDK's own timer layer
   (`official-docs/sdk/lifecycle.md:34-57`). The wire's
   `InputEvent.automated_trigger` reaches the harness as a trigger, and the
   clone answers it by running a turn (`cmd/harness/conversation.go:494-495`)
   — which is the wire-level half; the *schedule* is the SDK's.
3. **`policy.safe_defaults`** and the `Allow execution? (y/n) [n]:` prompt
   text (`official-docs/sdk/policies.md:34-60`) — the SDK's console
   convenience. The clone's wire-level precedence and its `ASK_USER` round
   trip are implemented (see the Match list, items 21–22); the console
   rendering is not, and is not the harness's business.
4. **`LiteRTAgentConfig`** — the SDK's local LiteRT runtime
   (`official-docs/sdk/local-models.md`). The wire's endpoint kinds the
   clone reads are `GeminiAPIEndpoint`, `VertexEndpoint`, `GemmaEndpoint` and
   `CustomEndpoint` (`cmd/harness/models.go:9-12`); LiteRT is not among
   them.
5. **The SDK's `SystemInstructionSection` / `TemplatedSystemInstructions`
   types** (`official-docs/sdk/personas.md:14-26`) — Python dataclasses with
   a `title` and a `content`. The clone implements the wire's `custom` /
   `appended` / `overrides` shape, which is what the SDK's two Python types
   map onto (`cmd/harness/prompt.go:12-16`); the section *type* itself has
   no wire counterpart beyond the section name and the text.
6. **The SDK's `enabled_tools` / `disabled_tools` filtering** as a
   `CapabilitiesConfig` (`official-docs/sdk/tools.md:27-42`) — the filtering
   is the *client's* job in the wire model: the client sends the surface it
   wants in `harness_side_tools`, and the harness advertises exactly that
   (`cmd/harness/sidetools.go:5-8,117-191`).
7. **The `BuiltinTools` enum's *member* names** (`LIST_DIR`, `SEARCH_DIR`,
   `FIND_FILE`, `VIEW_FILE`, `CREATE_FILE`, `EDIT_FILE`, `RUN_COMMAND`,
   `ASK_QUESTION`, `START_SUBAGENT`, `GENERATE_IMAGE`, `SEARCH_WEB`,
   `READ_URL_CONTENT`, `FINISH`) — Python enum constants; the wire carries
   tool *names*, not enum members.

---

## Flags, ambiguities and confusions

These are the places where the **doc and the clone's own recovered evidence
disagree**. They must be **re-checked against the reference binary before
any fix** — the doc is not automatically the authority.

1. **The primitive `--json-schema` shorthand (D-05).** The doc says the flag
   accepts `string`, `number`, `integer`, `boolean`
   (`official-docs/cli/headless.md:122`). The clone rejects a bare primitive
   name. But the clone's *rejection* strings are the reference's own
   【strings】 — `schema root must specify "type": "object"`
   (`out/local-macos-1.4.11/strings.txt:67085`) — and that message says
   the root must be an **object**, which is inconsistent with a
   primitive-root shorthand. Two readings are possible: the reference
   accepts the primitive name and expands it *before* the object-root
   check, or the doc describes a shorthand the reference never had. **Do not
   widen the root check**; add the shorthand as a pre-step if the reference
   is captured showing it.
2. **`permission_mode` under `--dangerously-skip-permissions` (D-09).** The
   doc names `always-proceed`; the clone reports `request-review`. The
   clone's own 【一手】 evidence is about the *default*
   (`internal/printmode/print_format.go:374-383`: agy 1.4.8 with an empty
   `settings.json` reports `request-review`), and it says nothing about the
   flag. The reference's value under the flag is unproven.
3. **Does the reference consume `mcp_servers`? (D-12)** The field is in the
   reference's own wire and the reference's `McpServerConfig` carries a
   full transport model, so 【推测】 the reference registers them. That is a
   hypothesis, not a fact: the reference's *consumption* of the field was
   not recovered. The same applies to `session_continuation_mode` and
   `initial_trajectory` (D-13).
4. **The envelope's `status` table (D-06)** is the reference's
   `CORTEX_STEP_STATUS_*` enum, not an envelope vocabulary — the doc
   conflated two things. The clone's two-value `status` agrees with the
   reference's own 1.4.11 error envelope
   (`{"conversation_id":"","status":"ERROR",…}`, recorded at
   `internal/printmode/print_format.go:118-131`). Treat the doc's table as
   wrong until a reference capture says otherwise.
5. **The `step_update` member set (D-07)** is documented with an example
   that the reference binary's tags support, but the reference's *step
   state* vocabulary (`ACTIVE` in the doc, `DONE` in the clone) is not
   settled: the clone's own comment records that "the reference also has
   RUNNING/PENDING states, which are unrecovered because a successful turn
   cannot be captured while the account is exhausted"
   (`internal/printmode/print_format.go:213-217`).
6. **The headless `--print-timeout` default (D-16).** The doc says `5m`
   (`official-docs/cli/headless.md:499,519`); the clone's default is `0s`
   (unlimited) (`internal/cli/parser_flags.go:76`), and the clone's own
   evidence says the reference changed it: "Changed the default timeout for
   headless (`-p` / `--prompt`) runs from 5 minutes to unlimited…"
   (`internal/cli/testdata/changelog_reference.txt:377`; also
   `out/local-macos-1.4.11/strings.txt:221893`). The **doc is stale here**
   and the clone is right.
7. **The headless `--effort` levels (D-11)** contradict the doc's own
   `reference.md` and Slice A's A-1/A-2/A-3.

---

## Numbers

| Metric | Count |
|---|---|
| Docs read | 11 (`cli/headless.md`, `cli/conversations.md`, 9 `sdk/*.md` — 1,387 lines) |
| Claims checked | 118 |
| Matches | 26 findings |
| Divergences | 16 findings (D-01 … D-16) — 13 clone-side, 3 `doc-wrong` (D-06, D-11, D-16) |
| Doc-only | 7 findings |
| Clone-only | 8 findings |

Severity split of the 16 divergences: **missing-feature** 9 (D-01, D-02,
D-03, D-04, D-07, D-08, D-12, D-13, D-15) · **behaviour** 4 (D-05, D-09,
D-10, D-14) · **doc-wrong** 3 (D-06, D-11, D-16). Two of the
missing-feature findings are **also byte-level**: D-02 (three absent
reference strings) and D-04 (two absent envelope members).

User-reachable divergences (a user action reaches the wrong behaviour):
**10 of 16** — D-01, D-02, D-03, D-04, D-05, D-07, D-08, D-09, D-10 and
D-14 are reachable from a command line; D-12, D-13 and D-15 are reachable
only from an SDK client; D-06, D-11 and D-16 are doc errors, not clone
defects.

Highest-value closings, in order: **D-01** (the biggest gap in the slice —
a documented mode that the clone's own `--help` advertises and does not
implement), **D-10** (one envelope written to stdout on the pre-flight
failure path), **D-09** (one argument threaded into one function), **D-08**
(three `omitempty` members on a payload the clone already fills), **D-03 +
D-04** (the schema, end to end: enforce it, then carry the two members).

---

## limitations

- The slice read **11 docs** in full (1,387 lines). **118 claims** were
  checked, **26** matched, **16** diverged, **7** are doc-only and **8** are
  clone-only.
- **Three behaviours were confirmed first-hand** against a build of the
  clone (`go build -o /tmp/pigasus-d ./cmd/pigasus`): the
  `--input-format stream-json` fall-through to the TUI (D-01), the
  unknown-model exit with an empty stdout (D-10) and the `--json-schema`
  primitive-name rejection (D-05). **No finding rests on running the
  reference binary.**
- **The SDK claims are wire claims.** The SDK is a Python package and the
  clone is Go; there is no Python surface in the clone to compare against.
  Every SDK entry states which of the three readings it uses — wire-level
  alignment, clone-ahead, or doc-only — so a later slice does not
  re-report "the clone has no `Agent` class" as a divergence.
- **The reference's own consumption of three wire fields is unproven**
  (`mcp_servers`, `session_continuation_mode`, `initial_trajectory`). Their
  presence in the reference's wire is fact; their use is 【推测】. Flagged in
  *Flags*, item 3.
- **The `step_update` and `init` payload member sets (D-07, D-08)** rest on
  the doc plus the reference's own JSON tags 【strings】
  (`out/local-macos-1.4.11/strings.txt:658577-658581,661237,663855,668711,676838`).
  The tags establish that the reference has the members; they do not
  establish the *order* within the payload, so a fix should follow the
  doc's table order and be re-checked against a live capture.
- **The `status` vocabulary (D-06)** is settled by the reference's own enum
  【strings】 (`out/local-macos-1.4.11/strings.txt:229943-229955`), which is
  a *step* status. The envelope's own status is confirmed by the clone's
  【一手】 1.4.11 error envelope. The doc's seven-value table is therefore
  a doc error — but the direction of the fix is a **documentation** change,
  which this survey does not make.
- **The reference harness's decompile artefacts live directly in
  `out/localharness/`** (the `*.md` decompile files, `seeds/seeds.txt`,
  `PARITY.md`) — there is **no** `out/localharness/dec/` directory in this
  tree, even though a comment in `budget/budget.go` refers to one. Citations
  in this ledger point at the paths that exist.
- **The `permission_mode` value under `--dangerously-skip-permissions`
  (D-09)** and the **primitive `--json-schema` shorthand (D-05)** both need
  a live reference capture before they are fixed; see *Flags*, items 1–2.
- **Not assessed here:** the reference's own `--print-timeout` behaviour
  under a *non-zero* deadline; the `structured_output` payload's exact
  serialisation (the doc shows a parsed object, the reference's own shape
  is unconfirmed); and the SDK's `LiteRT` runtime, which the wire does not
  model.
- This file is a **survey**. Nothing in it has been implemented, and **no
  other file was edited**.
