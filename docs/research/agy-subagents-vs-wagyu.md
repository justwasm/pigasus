# Research note — agy subagents vs wagyu

**Date:** 2026-10-18
**Source:** `internal/cli/testdata/changelog_reference.txt` (915 lines, full
AGY 1.4.0 changelog text extracted from `agy --help`-adjacent string table)
+ `cmd/wagyu/testdata/print_changelog.md` (the same text, --print output format).

## TL;DR

AGY has a **first-class subagent system** with:

1. A dedicated `/agents` panel (running subagents, create new agents,
   custom agents in Markdown format)
2. Five agent tools on the default toolset: `invoke_subagent`,
   `send_message`, `manage_subagents`, `define_subagent`, `generate_image`
3. A prompt syntax `@<subagent> <message>` to route messages to a subagent
4. Real-time subagent + background-task status indicator below the input box
5. Per-subagent isolated git worktrees
6. Nested subagent depth (grandchild + deeper) with recursive subtrajectory
   relay
7. Custom agents declared via `agents.json` in plugins or `agent.md` with
   YAML frontmatter (`mainAgent`, `subagent`, `hidden`, `inheritMcp`,
   `commandExecutionPolicy`)

Wagyu has the **shell of a subagent system but no actual delegation**:

- `internal/subagent/` is a **child-process spawn manager** (`exec.Cmd`
  wrapper, 5 hard-coded bash convenience constructors: `ExploreSubagent`,
  `CodeReviewSubagent`, `TestSubagent`, `BuildSubagent`, `DeploySubagent`).
  None of them start an LLM-driven agent — they all run `bash -c "..."`.
- `/agents` panel renders the "Create New Agents" help page but Enter
  echoes "Already using default agent." The `k` "Kill Active Subagent"
  binding is **inert** (`cmd/wagyu/slash_action_panels.go:741`).
- No `@<subagent>` syntax in `cmd/wagyu/tui_run.go` prompt parser.
- No `invoke_subagent`, `send_message`, `manage_subagents`,
  `define_subagent`, `generate_image` tool declarations in
  `internal/tools/` or `internal/piagentbridge/`.

## AGY changelog entries (chronological by line number)

The changelog is reverse-chronological; **higher line numbers = older**.
Key entries by topic:

### Subagent foundations

| Line | Summary |
|---|---|
| 685 | `Added the --agent flag and agent/agents subcommand, allowing users to select a custom agent at launch and list available agents.` |
| 614 | `Custom Agents (Markdown Format). Added support for defining custom agents using Markdown files (agent.md) with YAML frontmatter and H1-delimited system prompts.` |
| 621 | `Fixed switching from a custom agent back to the default agent via /agents, which previously failed silently.` |
| 356 | `Fixed duplicate agent entries appearing in the /agents picker when an agent with the same name is discovered across multiple configuration sources or plugins.` |
| 161 | `Fixed project custom agents in .agents/agents/ not being found or selectable in workspaces created in the Desktop App, in already-trusted workspaces, under execution with --agent, in headless (-p / --prompt) runs, and in the /agents panel.` |

### Subagent toolset

| Line | Summary |
|---|---|
| 513 | `Fixed invoke_subagent, send_message, manage_subagents, define_subagent, and generate_image rendering with no tool call line at all in collapsed and expanded tool groups.` |
| 234 | `Added automatic subagent guidance for custom agents defined in Markdown: agents that list invoke_subagent in their tools now receive the roster of available subagents and usage instructions in their system prompt.` |
| 213 | `Improved the default agent and subagent toolset by retiring the legacy find_by_name, grep_search, and list_dir tools from the default baseline while keeping them available to custom agents.` |

### Subagent runtime

| Line | Summary |
|---|---|
| 690 | `Added support for displaying nested subagents (grandchild and deeper) and handling tool confirmation requests across all subagent depths by recursively relaying nested subtrajectory updates to the root conversation.` |
| 682 | `Fixed a data race and copy-on-write violation when updating subagent states by cloning their stats before in-place mutation, preventing corrupted step counts and status for parallel subagents.` |
| 539 | `Fixed a subagent going silent on its parent when its own message failed to send, and fixed the parent being handed an empty notice when a subagent went idle without a final response.` |
| 573 | `Fixed stopping a subagent tree stopping only the conversation it was invoked from, while every descendant subagent and the background tasks they owned kept running.` |
| 574 | `Fixed a forced-continuation deadlock where a coordinator waiting on active subagents or background tasks would loop injecting empty continue steps until it hit the invocation limit.` |
| 252 | `Fixed subagent status and termination tracking under pubsub batch coalescing so killed subagents are not reset to active by late step updates.` |

### Subagent UI

| Line | Summary |
|---|---|
| 723 | `Introduced a new interactive status indicator below the input box that displays active subagents and background tasks in real-time, making it easy to monitor and navigate parallel workflows at a glance.` |
| 177 | `Added @<subagent> <message> prompt syntax to send a message directly to a subagent conversation, with autocomplete listing running and completed subagents.` |
| 730 | `Fixed the subagent approval TUI to dynamically render user-defined custom keybindings (such as alternative approval keys) instead of showing hardcoded defaults.` |
| 736 | `Enabled "always proceeds" mode for subagents to auto approve artifacts, preventing them from hanging when the parent is blocked.` |

### Subagent worktrees

| Line | Summary |
|---|---|
| 174 | `Fixed files that subagents write inside their own Git worktree being treated as artifacts, which demanded artifact metadata and left .metadata.json files in the worktree; subagent worktrees now live under a worktrees/ directory in the app data directory.` |
| 51 | `Fixed subagents started with a branch or share workspace in a multi-repository workspace silently working in your real checkout; each repository now gets its own isolated copy.` |

### Subagent questions / approvals

| Line | Summary |
|---|---|
| 6 | `Fixed questions asked by subagents and workflow agents being lost, which left the subagent waiting forever: the alert now reads <agent> has a question and shows it, and Ctrl+K or a in the subagent's view opens a question picker that sends your answer to that agent. Headless -p runs skip these questions so the subagent can continue.` |
| 121 | `Improved how the agent handles a permission request that is not approved, such as one raised by a subagent that cannot ask you: it now respects the denial and no longer tries to work around it.` |
| 890 | `Fixed timeout overrides: restricted the default 60-second interaction timeout specifically to subagents, preventing the main agent from being unconditionally capped.` |

### Subagent via plugins

| Line | Summary |
|---|---|
| 43 | `Added support for a root-level agents.json in plugins, so a plugin can declare its subagents the same way it declares skills and rules with skills.json and rules.json.` |
| 67 | `Improved plugin compatibility: plugins load every skill in their skills/ folder, remote MCP servers start correctly once their settings are filled in, and a plugin's subagents can use the plugin's own MCP servers.` |
| 13 | `Fixed agy plugin validate, the summary agy plugin install prints, and marketplace previews in the /plugin Discover tab leaving out subagents a plugin declares in its root agents.json.` |
| 35 | `Fixed the Agents: row in /plugin plugin details leaving out subagents a plugin declares in its root agents.json; it now lists every loaded agent by name instead of by its agents/ folder name.` |

### Boost mode

| Line | Summary |
|---|---|
| 350 | `Fixed /boost failing at runtime by improving worker tool configurations.` |

(`/boost` is real but **only one changelog entry** — implies it was a stub
or recently introduced, not a fully-featured orchestrator.)

### Image generator subagent

| Line | Summary |
|---|---|
| 107 | `Changed how the agent generates images: it now hands image requests to a built-in image-generator subagent, which writes the prompt, checks each result with up to three attempts, and saves the images to the conversation's artifacts.` |

### Background tasks (sibling system to subagents)

| Line | Summary |
|---|---|
| 496 | `Improved custom agent support by making manage_task available to declarative agents that have not opted out of fundamental components, so a custom agent can list and kill its own background tasks.` |
| 184 | `Fixed headless (-p / --prompt) runs cancelling still-running background tasks about 5 seconds after the agent went idle; runs now wait for background tasks until the --print-timeout deadline, up to a 30-minute cap.` |
| 235 | `Improved background task naming so a task keeps the name it was given when sent to the background.` |
| 253 | `Fixed conversations with active turns or running daemon background tasks failing to auto-resume after a restart.` |
| 301 | `Improved headless (-p) runs to exit promptly once the final answer is delivered: the CLI waits for running background tasks and scheduled timers to finish.` |

## Where wagyu's `internal/subagent` actually comes from

The package was added in **cycle 22** (early cycles, before cycles were
numbered meaningfully). It's a thin `exec.Cmd` wrapper with five
hard-coded bash convenience constructors. The Subagent type carries
`ID`, `Name`, `Cmd`, `State`, `Result`, `Err` — there is **no model
context, no system prompt, no transcript**. The Manager just keeps a
map of `id → *Subagent` and provides `Add / Get / List`.

This is a *tool-result backend*, not an agent runtime. It predates PiG
integration (cycle 183) and has not been revisited since.

## Future-cycle map

| Cycle | Topic | Key changelog lines |
|---|---|---|
| 228 | `invoke_subagent` + `send_message` + `manage_subagents` tool declarations on the default toolset | 513, 234, 213 |
| 229 | `@<subagent> <message>` prompt syntax + autocomplete | 177, 437 |
| 230 | `/agents` panel real subagent list (currently placeholder) | 685, 621, 723 |
| 231 | Real-time subagent + background-task status indicator below input box | 723, 705, 456 |
| 232 | Per-subagent isolated git worktree (under app data dir) | 174, 51 |
| 233 | Subagent questions + approval picker | 6, 121, 730, 736 |
| 234 | `agents.json` plugin declaration | 43, 67, 13, 35 |
| 235 | `define_subagent` Markdown format (replaces JSON defs) | 614, 717, 709, 710 |
| 236 | Nested subagent depth (grandchild + recursive subtrajectory relay) | 690 |
| 237 | Background-task waiting + headless mode | 184, 235, 253, 301, 496 |

These can be batched. A reasonable 4-cycle wave:

- **228**: `invoke_subagent` + `send_message` + `manage_subagents` tools
  (real LLM-driven delegation via PiG `coding.Agent` as sub-runtime)
- **229**: `@<subagent>` syntax + autocomplete + `/agents` real list
- **230**: per-subagent git worktree + background-task status row
- **231**: subagent questions picker + `define_subagent` Markdown format

Cycle 227 (auto-compact) and these four would close the major functional
gap between wagyu and agy.

## 存疑

1. **AGY's `invoke_subagent` runtime model** is not in the changelog. The
   string evidence (`cycle-217/deliverable` notes "Pure orchestrator.
   Dispatches tasks to specialists…") suggests each subagent runs a
   fresh `Agent` instance with a tailored system prompt. Cycle 228 needs
   to verify this against a decompiled `invoke_subagent` invocation
   before designing the runtime.
2. **No `--agent` flag** in wagyu's flag parser; cycle 229 should add it
   before any `--agent` plumbing lands.
3. **No `subagent_info` structured stream payload** in wagyu. PiG has
   `agent.CompactionEndEvent` (cycle 227 wires that); a parallel
   `agent.SubagentStartEvent` / `SubagentEndEvent` may exist upstream
   but needs verification before cycle 229.
4. **`generate_image` is a subagent**, not a separate tool. Wagyu's
   image-generation support (cycle 30-era stub) likely needs replacing
   with a subagent invocation. Out of scope for cycles 228-231.