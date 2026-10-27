# Cycle 349 deliverable: the subagent authorization model — the three `Enable*` switches, and a subagent that can open subagents of its own

**Cycle:** `cycle-349-subagent-grants`
**Subject:** the reference's `define_subagent` carries three switches and
the clone threw them away. This cycle gives them back, and with them the
nesting they imply: a subagent that holds `EnableSubagentTools` can
define and invoke subagents of its own.
**Method:** the reference's own captured declaration read first-hand
(`internal/tools/model_tools_1.4.6.json`), the tool descriptions read
first-hand (`internal/tools/testdata/tool_descriptions_1.4.7.json`),
and every change reproduced against the clone's own tool set before it
was made.

---

## 0. The short answer

The clone had the *shape* of the reference's authorization model and
none of its *content*. `define_subagent` parsed the three switches and
dropped them on the floor; the delegated subagent then ran with **no
tools at all**, whatever the definition said.

| | before | now |
|---|---|---|
| **`EnableWriteTools`** | parsed, ignored | the write tools: create and edit files, run commands |
| **`EnableMCPTools`** | parsed, ignored | `call_mcp_tool` `list_resources` `read_resource` |
| **`EnableSubagentTools`** | parsed, ignored | `define_subagent` + `invoke_subagent` — **nesting** |
| **the base set** | nothing | the research tools + the messaging tool, always |
| **`self`** | invocable, tool-less | the parent's whole configuration: catalogue, prompt, model |
| **`research`** | `subagent "research" not found or not allowed to be invoked` | a reachable, read-only type |

---

## 1. The evidence

### The three switches

【一手 — the captured 1.4.6 declaration, `internal/tools/model_tools_1.4.6.json`:

> The capture did not move: at agy 1.4.8 the twelve model-facing
> declarations are byte-for-byte identical to it (【一手】, live 1.4.8
> request — cycle-350). `internal/subagenttools/reference_declaration_test.go`
> pins the clone's parser against the capture's *own* property names, in
> both directions.

```json
"define_subagent": {
  "description": "Define a new subagent type that can then be invoked repeatedly. Subagents always have tools to research the codebase and to communicate with other agents.",
  "properties": {
    "EnableMCPTools":     { "type": "BOOLEAN", "description": "Grant access to MCP tools." },
    "EnableSubagentTools": { "type": "BOOLEAN", "description": "Grant tools to define and invoke its own subagents." },
    "EnableWriteTools":   { "type": "BOOLEAN", "description": "Grant tools to create and edit files, and run commands." }
  },
  "required": ["Name", "Description", "SystemPrompt"]
}
```

Three switches, and one sentence that names the base set: **the
research tools and the messaging tool are always granted.** The grant
model is not an invention — it is the reference's own sentence, read as
a rule.

### The types

【一手 — the same capture, `invoke_subagent`: `required: ["TypeName",
"Role", "Prompt"]`, and the description's own lines: *"The subagent is
identified by the conversationId returned; message it with the
send_message tool"*, *"Multiple subagents may share a type name; each
still gets a unique conversationId"*, *"Default the Model argument to
'inherit' unless the user explicitly requests a different model"*.]

So `TypeName` names a **type**, not an agent: the types are the
conversation's own definitions plus the reference's built-ins. The
clone's built-in list is pinned against the 1.4.4 capture
(`agyprompt.BuiltinSubagents()`): `self`, `research`, `image-generator`.

### The research set

【一手 — the 1.4.7 tool descriptions,
`internal/tools/testdata/tool_descriptions_1.4.7.json`.] The
converter-layer tools that only read are `view_file` (*"View the
contents of a file from the local filesystem"*), `grep_search`
(*"Use ripgrep to find exact pattern matches within files or
directories"*), `list_dir` (*"List the contents of a directory"*),
`search_web` (*"Performs a web search"*) and `read_url_content`
(*"Fetch content from a URL … extracting text from public pages"*).
The ones that write are `write_to_file`, `replace_file_content`,
`multi_replace_file_content`, `notebook_edit`, `sed_file`
(*"Edit files using sed expressions"*), and the command pair
`run_command` + `command_status`. The MCP three are `call_mcp_tool`,
`list_resources`, `read_resource`.

### The messaging tool

【一手 — the captured `send_message`: *"Send a message to another agent,
such as a subagent or a peer. Not a channel to the user."*] The
clone's catalogue has a `send_message` too, but it is the **background
command's stdin** — a different tool that happens to share the name.
The reference's `send_message` is the agent-to-agent one, and that is
what the base set grants: `internal/subagenttools`' own `send_message`.

---

## 2. The model

`internal/subagenttools/grant.go` (**new**) holds the whole thing:

```go
type Grant struct {
	Write    bool
	MCP      bool
	Subagent bool
}
```

and the names each switch adds:

| | tools |
|---|---|
| base (always) | `view_file` `grep_search` `list_dir` `search_web` `read_url_content` + this package's `send_message` |
| `Write` | `write_to_file` `replace_file_content` `multi_replace_file_content` `notebook_edit` `sed_file` `run_command` `command_status` |
| `MCP` | `call_mcp_tool` `list_resources` `read_resource` |
| `Subagent` | `define_subagent` `invoke_subagent` |

The names are the **catalogue** names (`internal/tools`), not the
twelve model-facing declarations. A subagent's tools are drawn from the
parent's catalogue, the same way the parent's own executors are; the
twelve declarations are the *advertisement*, not the pool.

### The type decides

`invoke_subagent` resolves the type and the type carries the
authorization:

| the type names | the tool set | the prompt |
|---|---|---|
| a type the conversation defined | the grant its definition recorded | the definition's own prompt |
| `self` | the parent's whole catalogue | the parent's own prompt, verbatim |
| `research` | the base set (read-only) | the research instruction |
| nothing | the base set | the parent's prompt |

`self` is the reference's own type — *"Subagent that inherits the parent
agent's full configuration including tools, system prompt, and model"* —
so it is the one type that gets **everything**: the parent's catalogue
(with the four subagent tools replaced by this package's own
implementations), the parent's system prompt with **no role prefix**
(`self` *is* the parent), and the parent's model.

`research` is the reference's second built-in type — *"Research subagent
with read-only tools for exploring the codebase, searching the web, and
reading files"*. It used to be unreachable; it is now a type with the
base set and its own instruction.

---

## 3. Nesting

`EnableSubagentTools` is the switch that makes a subagent able to open
subagents of its own. The granted tool set carries **this package's
own** `define_subagent` and `invoke_subagent`, wired to the **same
definition store** and the same deps as the parent's. So:

- a nested definition lands in the same conversation store (the
  reference's scoping — *"for the duration of this conversation"*), and
  the parent can see it too;
- a nested invocation resolves the grandchild's type and the
  grandchild's **own** grant decides the grandchild's tool set — the
  recursion is the same rule applied one level down;
- a resumed conversation gets its definitions back from the trajectory,
  **with the three grants** (`RestoreDefinition`), so a rehydrated
  subagent runs under the authorization it was defined with.

The test that pins it is
`TestSubagentGrantCarriesDefineAndInvoke`: the granted subagent's tool
set carries an `invoke_subagent`, that tool resolves a grandchild, and
the grandchild's tool set carries the grandchild's own write grant.

---

## 4. What the parent hands over

`subagenttools.Deps` grew two fields:

| | |
|---|---|
| `Catalogue []piagent.AgentTool` | the parent's tool catalogue, adapted to PiG's tool interface — the pool every grant draws from |
| `SystemPrompt func() string` | the parent's own system prompt, resolved on demand |

**The catalogue is injected, not imported.** `internal/tools` depends on
`internal/browsersubagent`, which depends on `internal/subagenttools` —
so `internal/subagenttools` **cannot** import `internal/tools` without
closing an import cycle. The caller adapts instead:
`piagentbridge.Tools(registry)`. That is why `Deps.Catalogue` is a
`[]piagent.AgentTool` and not a `*tools.Registry`.

**The prompt is a function.** Building the reference's prompt walks the
skills directories, and the turn's own prompt is built already; a
function value means a turn that spawns no subagent does not pay for a
second build. The TUI hands over `m.systemPrompt` (the method value);
the print mode hands over `pigrt.SystemPromptFor(options)` — the same
text `pigrt.New` computes, so the prompt a `self` delegation inherits
is the parent's own prompt and not a second, slightly different build of
it. The print mode also mints the conversation id **before** the
runtime does, so the prompt's `<user_information>` section names the
same conversation as the parent's own prompt.

---

## 5. What the model sees

The three switches were already **on the wire** — the captured
declaration is what the model reads, in the TUI
(`internal/agent`'s `declOverrideTool`) and in print mode
(`piagentbridge`'s `surfaceTool`). The model could ask for a write
grant; the clone dropped the answer on the floor. Now the answer is
kept.

The clone's own `define_subagent` schema also grew the three fields
(snake_case), so a caller that advertises the package's schema rather
than the captured declaration can ask for the same thing. Both spellings
are parsed: the reference's capitalised names and the clone's
snake_case ones.

---

## 6. 硬约束 (hard constraints)

- **The base set is the reference's sentence, not a choice.**
  *"Subagents always have tools to research the codebase and to
  communicate with other agents"* — the research tools and the
  messaging tool are granted to every subagent, whatever the switches
  say.
- **A grant names tools that already exist.** The three switches select
  from the parent's catalogue; they never add a tool the parent does
  not have. A name the catalogue does not carry is skipped.
- **The four subagent tools are this package's own.** The catalogue's
  `define_subagent` / `invoke_subagent` / `manage_subagents` are
  placeholders that answer "not implemented", and its `send_message` is
  the background command's stdin. The package's own four replace them —
  in the `self` set and in the granted sets alike.
- **The definition store is conversation-scoped.** A nested definition
  is a definition of the conversation, not of the subagent that made
  it. Last writer wins on a name collision.
- **The prompt of a `self` delegation is the parent's, verbatim.** The
  role prefix does not apply there — `self` *is* the parent.
- **A built-in type's name is the reference's.** The 1.4.x plugin
  validator *"errors on … a name reserved for a built-in agent"*
  (【一手 — the changelog's own `agy plugin validate` bullet]), so a
  conversation's own definition of `self` or `research` does not take
  the built-in over — the built-in answers for itself.
- **`internal/subagenttools` must not import `internal/tools`.** The
  cycle goes through `internal/browsersubagent`. The catalogue crosses
  the boundary as `[]piagent.AgentTool`.

## 7. 存疑 (open questions)

- **Does `manage_subagents` belong to the subagent grant?** The
  reference's `EnableSubagentTools` says *"define and invoke"* — and
  `manage_subagents` manages *"the direct subagents of **this
  conversation**"* ([一手] the captured description). The clone grants
  exactly what the switch says, and leaves `manage_subagents` with the
  parent. Since the clone's delegations are synchronous, there is no
  live subagent to list or kill either way.
- **Is `self` the parent's *catalogue* or the parent's *twelve
  declarations*?** The reference says *"full configuration including
  tools"*; the clone's parent advertises twelve declarations and holds
  a sixty-name catalogue. The catalogue is read as "the parent's
  tools", so a `self` delegation can call everything the parent can
  call — and more than the parent is told about. The other reading
  (the twelve) is defensible too.
- **What does a type named `research` or `self` mean?** The built-ins
  shadow the conversation's own definitions of the same name — and the
  reference *reserves* those names (its plugin validator errors on an
  agent file whose name is reserved for a built-in), so the clone's
  leniency is a deviation, not a choice.
- **The reference's delegations are asynchronous.** The captured
  `invoke_subagent` says the subagent *"runs in the background … and
  sends you a message when it completes"*; the clone's delegation is
  synchronous and returns the child's final text as the tool result.
  The grant model is independent of that difference, but the reference's
  *tree* semantics (kill a subagent, kill its descendants) have nothing
  to act on in the clone.
- **The permission policy does not reach the subagent's calls.** The
  seam exists (`Deps.PermissionGate`) and no caller wires it. The
  reference's own permission model is **one per conversation** — the
  1.4.6 changelog: *"allowing an action for the conversation, in the
  main agent or in a subagent, applies across the main conversation and
  all of its subagents"* (【一手 — the changelog's own bullet]), and the
  subagents of a custom agent are reviewed by the same Auto-mode
  review as the main agent's commands. So the gap is a fidelity gap, not
  a design choice. Two things make it more than a one-liner: the
  subagent's tool names are the **catalogue** names (`write_to_file`),
  while the user's allow and deny rules are written against the
  parent's **declaration** names (`write_file`), so a gate needs a name
  mapping; and the terminal sandbox's allowed paths do not yet include
  the subagent's own worktree, so a sandboxed subagent's commands would
  all be denied.

## 8. Limitations

- **The tests are unit tests.** They pin the grant vocabulary, the
  tool sets, the type resolution and the nesting; they do not run a
  real subagent turn end to end (the clone's subagent sessions need a
  model).
- **`self`'s tool set is large.** A `self` delegation advertises the
  parent's whole catalogue, browser tools included. The browser tools
  need a live browser; a `self` delegation that calls one gets the
  browser's own error.
- **The role prefix stays the clone's own.** *"You are a subagent
  acting as: X."* is the clone's reconstruction, not a captured
  sentence, and it is applied to every type except `self`.
- **The catalogue is rebuilt per turn.** The TUI hands over
  `piagentbridge.Tools(m.catalog)` for the turn's catalogue; a
  catalogue that changes mid-turn is not seen by a delegation started
  in the same turn.

## Files

| | |
|---|---|
| `internal/subagenttools/grant.go` | **new** — the grant, the names, and the tool-set assembly |
| `internal/subagenttools/grant_test.go` | **new** — the vocabulary, the types, the nesting |
| `internal/subagenttools/reference_declaration_test.go` | **new** — the contract with the reference's captured declaration |
| `internal/subagenttools/define.go` | the three switches are parsed and stored; the schema grew them |
| `internal/subagenttools/invoke.go` | the type resolution and the tool set per type |
| `internal/subagenttools/subagenttools.go` | `Deps.Catalogue`, `Deps.SystemPrompt`, `Invocation.Grant` |
| `internal/subagenttools/rehydrate.go` | the grants come back with the definition |
| `internal/cliapp/tui_runmode_agent.go` | the TUI hands over its catalogue and its prompt |
| `internal/printmode/print_pig.go` | the print mode hands over its catalogue and its prompt |
| `internal/pigrt/runtime.go` | `SystemPromptFor` — the prompt without a runtime |
| `docs/guide/tools.md` | *The subagent authorization model* |
