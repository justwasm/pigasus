# Cycle 345 deliverable: the type the model names — the browser agent as a built-in subagent

**Cycle:** `cycle-345-browser-subagent-type`
**Subject:** cycle 344 gave the clone the user's own browser, but the
model could not *ask* for it: the model's twelve wire declarations carry
`invoke_subagent` and no browser tool, so a model that only sees the
reference's own declarations had no way in.
**Method:** the reference's built-in agents package read out of the
binary's symbol table and string table, the reference's own browser
agent description read out of the 1.4.7 catalogue fixture, and the
clone's routing measured against the running model.

---

## 0. The short answer

The reference reaches its browser agent through a built-in agent
**type**, not through a browser tool — and the clone now does the same,
under the reference's own name.

| | before | now |
|---|---|---|
| how a model opens a browser | it cannot: no browser tool is in the wire declarations | `invoke_subagent(TypeName: "chrome_devtools")` |
| the type's name | — | `chrome_devtools`, the reference's own 【strings】 |
| where the model reads the name | — | the `invoke_subagent` description's catalogue, and the prompt's "Available subagents" list |

Measured on this machine: the clone's own integration test drives the
whole path — the model's call, the built-in registry, the browser
agent, the answer — with PiG's faux provider, so the run needs no
network and no browser and gives the same answer every time. Against
the live model, the same prompt opened the page and answered
`Example Domain` — through whichever browser route the model picked
that day, since the print mode advertises the direct `browser_*` tools
as well. The subagent route is what the integration test pins.

---

## 1. The gap

The clone advertises sixty tool names, and `browser_subagent` is one of
them. But the **model** never sees that list: it sees the reference's
twelve captured wire declarations
(`internal/tools/model_tools_1.4.6.json`):

```
read_file  run_command  manage_task  send_message  schedule
invoke_subagent  define_subagent  manage_subagents  write_file
edit_file  search_web  ask_question
```

No browser tool. So the browser agent existed in the clone but was
unreachable by the model — the `/browser` slash command could reach it,
the model could not.

That is not a defect of the clone: it is the reference's shape. The
reference's own model reaches its browser agent the same way, and the
reference's own `browser_subagent` *tool* is advertised but likewise
absent from the twelve.

## 2. The name, and where it comes from

The reference's built-in agents package carries a browser agent:
`cortex/customizations/builtin/agents.BrowserAgent`, next to
`agents.ImageGeneratorAgent`, `agents.SelfAgent`,
`agents.defaultBuiltinSubagentNames` and
`agents.CalculateAllowedSubagents`
(out/seeds/funcs.txt — 【反编译】, the symbol table).

The names themselves sit in one string blob of the 1.4.7 binary
(out/local-macos/xref/strings-addrs.tsv, the blob at `0x10336a5b1`):

```
…evaluate_script take_screenshot research-google chrome_devtools
webview_browser image-generator assets/external openai-o-series…
```

`image-generator` is the one the reference's own prompt lists as an
available subagent (internal/agyprompt/testdata/capture_1.4.6.txt, the
`<subagents>` section) — which is what makes that row the agents' name
vocabulary rather than a pile of unrelated literals. So the reference
ships **two** browser agents as types: `chrome_devtools` (the Chrome
one) and `webview_browser` (the IDE's webview one) 【strings】.

The first-hand confirmation is in the reference's own browser agent
description, which the clone already ships as a fixture
(internal/tools/testdata/tool_descriptions_1.4.7.json):

> The **chrome_devtools** MCP server is started automatically when this
> agent is invoked — no manual setup is required.

【一手 — 1.4.7, the `browser_subagent` tool's description】.

The clone ships the Chrome one. The webview variant belongs to the
IDE's built-in `<webview>` browser (the other agent's cycle, see
docs/guide/ide-language-server.md), which the clone does not drive.

## 3. The wiring

Three pieces, and no new machinery:

| | |
|---|---|
| `internal/builtinsubagent/browser.go` | `KindBrowser Kind = "chrome_devtools"` — the name, and the evidence for it |
| `internal/browsersubagent/builtin.go` | `Description`, `Subagent()`, `InstallBuiltin()` |
| the session | installs the type, per turn, and lists it in the prompt |

`InstallBuiltin` registers a `builtinsubagent.Spec` whose handler
builds the browser runner **per call** — so the surface it drives is
the one in force at that moment (the live surface when the Open Claude
in Chrome extension answers, the managed one otherwise). The registry
the session installs is the registry the catalogue is rendered from,
so the name the model reads and the name the router answers to cannot
drift apart.

The two assembly points are the same two that install the
`browser_subagent` provider: the TUI
(`internal/cliapp/tui_runmode_agent.go`, every turn) and the print
path (`internal/printmode/print_pig.go`, every run).

**Why the registry is rebuilt every turn.** The handler closes over the
session's model and services, and the TUI's services are per-turn
(the turn's own subagent tools use them). A registry built once at
session start would carry a closed service set into the next turn.

## 4. Where the model reads the name

Two places, and both must carry it:

- **the tool description** — `invoke_subagent`'s own description is
  spliced with the built-in catalogue (`BuiltinSubagentCatalog`), so
  `chrome_devtools` appears there;
- **the system prompt** — the "Available subagents" list, where the
  browser agent sits with the other built-ins, ahead of the user's own
  agents (the reference's list puts its built-ins first: the 1.4.6
  capture lists `self`, `research` and `image-generator` ahead of the
  user's `alpha` and `zeta`).

The reference's list is *conditional*: `CalculateAllowedSubagents`
decides which built-in types a session may use, which is exactly why
the 1.4.6 capture lists only three — the browser was off. The clone
always lists it, because the clone always has a browser to drive.

The browser agent is deliberately **not** part of
`agyprompt.BuiltinSubagents()`: that list is pinned against the 1.4.4
capture (`TestBuiltinSubagentsMatchCapture`) and stays as it is. The
session adds the browser agent the way it adds the user's own agents.

## 5. The routing

`invoke_subagent` accepts two spellings of the type parameter:

- `TypeName` — the reference's captured schema, and therefore what the
  model actually writes;
- `agent_name` — the clone's own schema.

Both reach the built-in registry, and both skip the parent's session
entirely: the handler answers with the browser agent's final text, in
the envelope the image-generator already uses:

```
{"ok":true,"agent_name":"chrome_devtools","text":"Example Domain", …}
```

**The proof is deterministic.** Three tests carry it:

| | |
|---|---|
| `TestInvokeSubagentBuiltinRouting_TypeNameReachesTheBuiltin` | the model's own parameter (`TypeName`) reaches the built-in, and the answer comes back in the envelope |
| `TestInstallBuiltinHandlerRunsTheBrowserAgent` | the installed type's handler really runs a browser agent — PiG's faux provider, no network, no browser — and its text is the result |
| `TestTheModelCanSeeTheName` | the name is in the `invoke_subagent` catalogue *and* in the prompt's "Available subagents" list |

The second one is the one that matters: it is the whole path, from the
registry to the agent's answer, without a gateway in the way.

## 6. The trap worth knowing about

The built-in registry is seeded lazily: the first read builds the
default. A session that installs its own registry **before** that first
read used to lose it — the lazy seed replaced the installed registry
with the default, and the browser agent was silently never routed to.

The symptom was invisible, and it is the worst kind: the model simply
saw a catalogue without the browser agent, and the turn looked
perfectly normal. The first end-to-end run of this cycle failed for
exactly this reason, and the fix is one line —
`SetBuiltinSubagentRegistry` now marks the lazy seed as done *before*
the assignment — plus a regression test that reproduces the order
(`TestSetBuiltinSubagentRegistrySurvivesTheFirstRead`, which fails
without the fix).

Two smaller guards went in with it:

- `Registry.Spec` and `Registry.Invoke` answer on a nil receiver
  instead of dereferencing it — the registry can be cleared, and the
  routing path runs while a tool call is in flight;
- `builtinSubagentRegistry` now takes the lock the setter already took.
  The session writes the registry every turn; the readers run during a
  turn.

## 7. 硬约束 (hard constraints)

- **The type's name is the reference's own.** `chrome_devtools` is what
  the reference's built-in agents package carries; the clone does not
  rename it to `browser` for taste. The description is the clone's own
  prose — the reference's line for its browser agent is not in the
  captured prompt, because the capture was taken with the browser off.
- **The browser agent stays out of `agyprompt.BuiltinSubagents()`.**
  That list is pinned against the 1.4.4 capture; adding a fourth entry
  would break the reference's bytes.
- **The catalogue keeps its sixty names.** `browser_subagent` was
  already among them; the browser *type* adds no name to the tool
  surface. The bridge tools stay inside the browser agent's own tool
  set.
- **The registry is per session, never process-global by accident.**
  `SetBuiltinSubagentRegistry` is a process-wide setter, and that is
  why the lazy seed had to be disarmed (section 6).

## 8. 存疑 (open questions)

- **Which name would the reference have used for the *live* surface?**
  The reference's two browser agents are the Chrome one and the
  webview one. The clone's live surface is neither: it is the user's
  own browser, reached through the Open Claude in Chrome extension. The
  clone keeps `chrome_devtools` because it is the reference's name for
  the browser agent, and the description says what the browser actually
  is on this surface. If a future cycle finds the reference's own name
  for the extension's surface, that is the string to move to.
- **The reference's gate is unknown.** `CalculateAllowedSubagents` is
  in the symbol table 【反编译】, but its rule was not recovered. The
  clone's rule is "always" — the clone always has a browser to drive.
  A reference that gates the type on a configuration would advertise
  it only sometimes; the clone cannot know when.
- **The webview agent is not implemented.** `webview_browser` is a
  second type in the reference. The clone has no webview browser, so
  it registers one type, not two.

## 9. Limitations

- **The browser agent's cost is the parent's cost.** The browser agent
  runs its own session with the parent's model and the browser tools,
  so a turn that reaches for it pays for a second session's context
  (measured: ~219k input tokens for the parent turn alone, the full
  sixty-name catalogue).
- **The type is advertised even when the browser cannot start.** The
  clone always lists the browser agent; a machine with no Chrome gets
  the failure inside the tool result, not an absent type. The
  reference gates the type instead.
- **The end-to-end measurement is one live run.** The path itself is
  pinned by a deterministic integration test (PiG's faux provider: the
  call reaches the registry, the registry's handler runs a browser
  agent, the agent's text comes back). The live run adds one thing the
  test cannot: that a *real* model, given the prompt, really can open
  a page and answer about it. It does **not** show that the model
  prefers the subagent over the direct `browser_*` tools the print
  mode also advertises — that choice is the model's, and it is not
  pinned by anything here. It is one run on one model; not a
  benchmark.
- **The reference's `webview_browser` type is unimplemented**, and the
  IDE's built-in webview browser is a third surface again (the other
  agent's cycle).

## Files

| | |
|---|---|
| `internal/builtinsubagent/browser.go` | **new** — the type's name and its evidence |
| `internal/builtinsubagent/browser_test.go` | **new** — the name, the nil registry |
| `internal/browsersubagent/builtin.go` | **new** — the catalogue line, the prompt entry, the installer |
| `internal/browsersubagent/builtin_test.go` | **new** — the install, the prompt, the catalogue, and the handler's own run |
| `internal/subagenttools/invoke.go` | the registry is not overwritten any more; the readers take the lock |
| `internal/subagenttools/builtin_routing_test.go` | the `TypeName` route; the lazy-seed regression |
| `internal/cliapp/tui_runmode_agent.go` | the TUI installs the type, every turn |
| `internal/cliapp/tui_stream_prompt.go` | the TUI's prompt lists the type |
| `internal/printmode/print_pig.go` | the print path installs the type and lists it |
| `docs/guide/tools.md` | *How the model opens a browser* |
