# The browser subagent — deliverable

**Cycle:** `cycle-311-browser-subagent`
**Subject:** the reference's `browser_subagent` tool — the tool that hands a
browsing task to a browser agent — and the browser agent behind it.
**Evidence:** 【一手】 the 1.4.7 binary's own advertised-tool list and its
production variants; 【反编译】 the step payload, the built-in agents package
and the launch path, read out of the binary's symbol names and literals;
【strings】 the reference's own description text and the browser agent's
prompt lead.

---

## 0. Summary

| | before | after |
|---|---|---|
| **`browser_subagent`** | advertised, no executor — a call failed with `ErrNotImplemented` | a real tool: the reference's description, a `task` parameter, and a browser agent behind it |
| **the description** | the clone's own paraphrase ("Delegate a browsing task to a browser subagent.") | the reference's own text, 458 bytes, recovered from the binary |
| **the browser agent** | none | `internal/browsersubagent`: a subagent whose tool set is the browser-control surface |
| **the delegation machinery** | `invoke_subagent` could only start a *text* subagent | `Invocation.Tools` lets a subagent carry a tool set of its own |

## 1. The reference's shape

### 1.1 It is a tool, not an agent type

【一手】 `browser_subagent` is one of the **sixty-three** names the 1.4.7
binary knows (`internal/tools/testdata/tool_names_1.4.7.txt`).

【反编译】 The reference's production variants carry the literal next to the
other subagent tools. `cortex/core/contrib/variants/prodvariants` holds
seven functions; `ProdMinimal` (0x101d74a90) references, in order:

```
invoke_subagent  define_subagent  manage_subagents  browser_subagent  send_message
```

Those are tool names, so `browser_subagent` is a tool — not an agent type
name that `invoke_subagent` would target.

### 1.2 What the call carries

【反编译】 The reference records the call as a
`CortexStepBrowserSubagent` (`out/local-macos/protos/cortex_1.4.6.proto`):

```
task, result, task_name, task_summary,
recording_name, recording_path, recording_generation_status,
subagent_id, reused_subagent_id, skipped, scratchpad_path, media
```

The first field is `task` and the second is `result`: the tool takes a task
and answers with the agent's result. The `recording_*` fields are how the
reference's *recording* variant reports the WebM it writes.

### 1.3 The description — four variants

【strings】 The tool's description is the reference's **browser agent's**
description, and four variants of it live in the binary, all in
`cortex/customizations/builtin/agents`:

| vmaddr | bytes | shape | referenced by |
|---|---|---|---|
| `0x103691b96` | 458 | Chrome, no recording note | `agents.adaptBrowserAgentForEval` (0x1014f2874) |
| `0x1036a1b41` | 683 | Chrome, with the recording note | `agents.productBrowserAgent` (0x1014efca0) |
| `0x1036a30e7` | 709 | webview | `agents.BrowserWebviewAgentWithAutoInvoke` (0x1014f12e0) |
| `0x1036a41b7` | 724 | webview | `agents.ScriptHarnessText` (0x1014f1250) |

The 458-byte one — the one the clone ships — reads:

> Browser agent that can perform actions in user's Google Chrome browser with
> the given task description. The agent has access to tools for interacting
> with web page content (clicking, typing, navigating, etc) and controlling
> the browser window itself (resizing window and emulating). Please make
> sure to define a clear condition to return on.
>
> The chrome_devtools MCP server is started automatically when this agent is
> invoked — no manual setup is required.

The lengths are pinned by the reference's own code: the length immediate
sits next to the `adrp`/`add` pair that materialises the string header
(`mov x6, #0x1ca` at 0x1014f2850 for the 458-byte one, `mov x2, #0x2ab` at
0x1014efc90 for the 683-byte one).

The clone ships the **Chrome, no recording note** variant, for two
reasons: the clone drives Chrome over the DevTools Protocol
(`internal/browsercdp`), and the clone records nothing — the recording
sentence would be a lie.

### 1.4 The browser agent behind it

【反编译】 The reference's browser agent is a real agent, in
`cortex/customizations/builtin/agents`:

```
BrowserAgent                    0x1014ef7e0
productBrowserAgent             0x1014ef840
BrowserAgentWithConfig          0x1014f0f70
ScriptHarnessText               0x1014f1140
BrowserWebviewAgentWithAutoInvoke 0x1014f12c0
browserWebviewAgent             0x1014f13a0
WithBrowserBackendInstructions  0x1014f1ee0
adaptBrowserAgentForEval        0x1014f25a0
BrowserOnMainAgentRequested     0x1014f2a10
AddBrowserToMainAgent           0x1014f3860
defaultBuiltinSubagentNames     0x1014f5330
AllAgentsWithConfig             0x1014f54a0
UseWebviewBrowserScript         0x1014f58f0
UseWebviewBrowserUx             0x1014f5a20
UseBrowserSubagentAutoInvoke    0x1014f5b50
ByNameWithConfig                0x1014f5c10
```

The launch path is `cortex/core/tools/tools.resolveBrowserSubagentBackend`
(0x101ddacc0), called from `(*InvokeSubagentTool).launchSubagent`
(0x101dd9a70) — so the reference reaches its browser agent through the
subagent machinery, exactly where the clone puts its own.

【strings】 The agent's prompt opens with the reference's own sentence:

```
You are a browser agent.
```

(the literal at `0x10352408d`, 24 bytes). The prompt is assembled from named
sections — `Identity`, `Tools`, `Recording`,
`MCP Connection Troubleshooting` — and a fifth block, the *Chrome Connection
& Remote Debugging Guidelines* (0x1036a448d), which tells the agent never to
launch Chrome itself: the reference attaches to the **user's** Chrome over
remote debugging.

【strings】 The agent's tool set is the chrome-devtools MCP surface
(`mcp(chrome_devtools/*)`), plus `view_file` and the recording tools
(`mcp_browser_recording_start` / `_stop` / `_stitch`,
`mcp_browser_intercept_snapshot`, `click_at`,
`mcp_browser_scale_coordinates`).

【strings】 The reference starts Chrome itself with `--headless`,
`--isolatedautomatio…`, `--viewport=1280x720`,
`--chromeArg=--no-sandbox` and `--chromeArg=--disable-dev-shm-usage`, and
runs the `chrome-devtools-mcp@latest` server through `npx`.

【反编译】 The settings that select a variant are
`enable-browser-subagent-v2`, `enable-browser-subagent-webview`,
`enable-browser-subagent-auto-invoke`,
`enable-browser-subagent-webview-script` and
`enable-browser-subagent-webview-ux`; the protos are
`BrowserSubagentToolConfig` (mode, `browser_subagent_model`) and
`AntigravityBrowserToolConfig`.

## 2. The clone's shape

### 2.1 The tool

`internal/tools/browser_subagent.go` holds the executor:

- `Name()` → `browser_subagent`;
- `Description()` → the reference's own text, read from
  `testdata/tool_descriptions_1.4.7.json` under the new layer
  `"agent catalogue (builtin/agents)"`;
- `Schema()` → one string parameter, `task`, required;
- `Run()` → reads `task`, hands it to the browser agent, and returns the
  agent's answer.

The executor calls through a `BrowserSubagentProvider` seam
(`SetBrowserSubagentProvider`), exactly like the three MCP tools' seam — so
`internal/tools` carries no dependency on the delegation machinery.

The catalogue registers it with a real executor
(`internal/tools/catalog.go`), so `browser_subagent` left the
"advertised but unimplemented" set.

### 2.2 The browser agent

`internal/browsersubagent` is the agent:

- `PromptLead` — the reference's own sentence, `You are a browser agent.`;
- `Prompt(inventory)` — the prompt: the reference's lead, then the clone's
  own inventory of the tools the agent actually has;
- `BrowserToolNames` / `BrowserTools(reg)` — the browser tools out of a
  registry, adapted to PiG's tool interface;
- `Runner.RunBrowserTask(ctx, task)` — the delegation.

The runner reuses `internal/subagenttools.Invoke`, the same machinery
`invoke_subagent` uses, with the browser tools as the subagent session's own
tool set. To make that possible, `subagenttools.Invocation` gained a
`Tools []piagent.AgentTool` field, which
`createSubagentSession` hands to PiG's `SessionStartOptions.ExtraTools`
(`internal/subagenttools/session.go`).

### 2.3 The wiring

The provider needs the session's model and the browser tools, so it is
installed where the model is known:

| surface | where |
|---|---|
| the print path | `internal/printmode/print_pig.go` → `installBrowserSubagent` |
| the TUI | `internal/cliapp/tui_runmode_agent.go` → `subAgentExtras` |

Both build the same runner: `subagenttools.Deps` (services, model, cwd,
agent dir) plus `browsersubagent.BrowserTools(catalogue)`. The TUI keeps
the catalogue on the run mode (`m.catalog`) so the per-turn rebuild can
reach it.

### 2.4 The flow

```
the model calls browser_subagent {"task": "…"}
  → internal/tools.BrowserSubagent.Run
     → the task is read
     → internal/browsersubagent.Runner.RunBrowserTask
        → subagenttools.Invoke
           → a PiG session whose tool set is the browser tools
           → the browser agent drives Chrome over CDP
        → the agent's final text
     → the tool's result
```

## 3. 硬约束 / the load-bearing rules

1. **The parameter is `task`.** The reference's step's first field is
   `task`, and its description says the agent acts "with the given task
   description". A missing task is refused before a session is created.
2. **The tool's answer is the agent's final text** — the reference's step
   carries a `result` field, and the reference's `invoke_subagent` returns
   the subagent's final answer the same way.
3. **The browser agent's tool set is the browser surface, and nothing else.**
   The clone gives it the twenty-three browser tools and *not*
   `browser_subagent` itself.
4. **`Invocation.Tools` is a seam, not a default.** A subagent with no
   tools of its own still has none: `createSubagentSession` passes `nil`
   through unchanged.
5. **The description the clone ships is the Chrome variant without the
   recording note** — the clone records nothing.
6. **The reference's "Do NOT Attempt to Launch Chrome" rule is deliberately
   not carried over.** The reference's browser agent attaches to the user's
   Chrome over remote debugging; the clone's browser client launches its own
   headless Chrome (`internal/browsercdp`). Carrying the rule over would
   tell the agent to do something the clone cannot do.

## 4. 存疑 / open questions

- **【推测】** The reference's *schema JSON* for `browser_subagent` is not
  recoverable. The reference builds its reflected schemas through
  `utils.ToJsonSchemaString`, and the 1.4.7 binary's instantiations carry
  no browser-subagent struct; no JSON-schema literal names `browser_subagent`
  either. The clone's schema is hand-built in the house style: the
  reference's parameter name, the reference's required set.
- **【推测】** The reference's browser-agent prompt is assembled from the
  sections `Identity`, `Tools`, `Recording` and
  `MCP Connection Troubleshooting`, but only the section *titles* are
  recoverable — the bodies live in a part of the binary the string table
  does not expose. The clone's prompt body is therefore its own.
- **【反编译】** The reference's browser tool *wrapper* — the thing that
  turns the browser agent's answer into the tool result — was not located:
  the 1.4.7 pclntab carries no `BrowserSubagentTool` or
  `BrowserSubagentToolConverter`, and the browser tools have no dedicated
  converter symbol in `cortex/tools/tools` either. The only code reference
  to the literal `browser_subagent` in the whole binary is
  `prodvariants.ProdMinimal`.
- **【strings】** The *Chrome Connection & Remote Debugging Guidelines* block
  (0x1036a448d) ends at "…debugging enabled.\n"; the boundary of the
  trailing newline is inferred from the next literal in the blob, not
  pinned by a length immediate.

## 5. Limitations

- **The clone's browser agent drives a different browser stack.** The
  reference's agent calls the chrome-devtools MCP server; the clone's calls
  its own twenty-three CDP tools (`internal/tools/browser.go`,
  `internal/browsercdp`). The *names* match the reference's advertised
  browser surface, the *mechanics* do not.
- **No recording.** The reference's recording variant writes a WebM per
  session and reports it through the step's `recording_*` fields. The clone
  ships the non-recording description and does not record.
- **`browser_subagent` is advertised, not declared.** The clone's
  model-facing surface is the twelve-name shipped declaration
  (`internal/tools/model_tools_1.4.6.json`), which does not include
  `browser_subagent` — the same as the reference, where the tool only
  appears in a variant. The executor is live whenever a surface carries the
  name.
- **One browser agent per call.** A call starts a fresh session and a fresh
  worktree (`<AgentDir>/worktrees/<id>`); the reference's `reused_subagent_id`
  and `skipped` fields — the paths that resume or skip an agent — are not
  implemented.

## 6. The evidence

| artefact | what it is |
|---|---|
| `internal/browsersubagent/browser_subagent.go` | the browser agent: prompt, tool inventory, the runner |
| `internal/browsersubagent/browser_subagent_test.go` | the prompt, the tool set, the delegation end to end |
| `internal/tools/browser_subagent.go` | the executor, the provider seam, the schema |
| `internal/tools/browser_subagent_test.go` | the schema, the missing-task path, the delegation |
| `internal/subagenttools/session_tools_test.go` | the subagent session carries the tools it was handed |
| `internal/tools/testdata/tool_descriptions_1.4.7.json` | the reference's own description text for `browser_subagent` |
| `internal/tools/catalog.go` | the catalogue entry, now backed |
| `internal/printmode/print_pig.go` | the print path's wiring |
| `internal/cliapp/tui_runmode_agent.go` | the TUI's wiring |
| `out/local-macos/protos/cortex_1.4.6.proto` | `CortexStepBrowserSubagent` — the step payload |
