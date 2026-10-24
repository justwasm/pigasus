# Cycle custom-agents — `--agent` markdown agents (1.4.3)

## TL;DR

| What | Where |
|---|---|
| Custom-agent discovery (workspace, user, plugin; enablement; exclusions) | `internal/agent/list.go` |
| Exact reference errors, `default` handling, case-sensitive matching | same |
| Custom-agent prompt (`<System Prompt>` + six sections) | `internal/agyprompt/agyprompt.go` (`BuildAgent`), `sections/agent_messaging.txt` |
| Restricted seven-tool surface, declarations captured byte-for-byte | `internal/agenttools/` (`tools_1.4.3.json`) |
| Print and TUI wiring, restricted tool catalogue, no subagent tools | `cmd/wagyu/print_pig.go`, `cmd/wagyu/tui_run.go`, `cmd/wagyu/tui_system_prompt.go`, `internal/pigrt/runtime.go` |
| `⚠ Agent Not Found` TUI card (replaces the stderr line on a real TTY) | `cmd/wagyu/tui_run.go`, `cmd/wagyu/tui_viewport.go` |
| Default-prompt fixes found along the way: skill roots, builtin allowlist, global sort, custom agents in `<subagents>` | `internal/agyprompt/skills.go`, `internal/pigrt/skills.go`, `cmd/wagyu/print_slash.go` |

The 1.3.4 changelog bullet

> Fixed `--agent` silently running the default agent when the name matches no
> agent: headless `-p` runs now exit with code 1 and list the available agents,
> and interactive sessions show an `Agent Not Found` warning before falling
> back to the default agent.

was implemented in cycle `agent-not-found-exit-1` from the prose alone. It
assumed a built-in `default` catalogue and invented the error text. Live 1.4.3
traffic shows that **`--agent` selects markdown custom agents**, each with its
own system prompt and a read-only tool set. This cycle implements that, and the
custom-agent request is now byte-identical to the reference (modulo UUIDs) in
both prompt and tools.

---

## Evidence

All reference facts are 【一手】 unless marked otherwise. The reference is
`~/.local/bin/agy` 1.4.3 (SHA-256 checked before/after). Captures were taken
through the cycle-274 wrapper:

```sh
OPENAGY_UPSTREAM=https://daily-cloudcode-pa.googleapis.com \
OPENAGY_CAPTURE_DIR=/tmp/cap \
/tmp/agy-wrapper --agent foo -p=hi
```

with `/tmp/agtest/.agents/agents/foo.md`:

```markdown
---
name: foo
description: A test agent
---
You are foo.
```

The wrapper does not rewrite `systemInstruction` or `tools`; the captured
request is what the vendor binary sent.

### Discovery

The reference scans four sources, flat one level deep:

| Scope | Root |
|---|---|
| Workspace custom | `<workspace>/.agents/agents/*.md` |
| User custom | `~/.gemini/config/agents/*.md` |
| Workspace plugins | `<workspace>/.agents/plugins/<plugin>/agents/*.md` |
| User plugins | `~/.gemini/config/plugins/<plugin>/agents/*.md` |

Recovered rules, each probed by changing one input:

* A file is an agent only when its YAML frontmatter has a non-empty `name` and
  `description`. No frontmatter, missing either field, invalid YAML, or a
  non-`.md` file is skipped silently.
* `--agent` matches the **frontmatter name**, case-sensitively. The file/folder
  name is irrelevant (`agents/bar.md` with `name: baz` is selected by `baz`,
  and `bar` errors).
* The `available:` list is sorted by name, deduplicated, comma-joined. The
  built-in `default` is accepted but never listed.
* An `agents.json` next to the `agents/` directory drops entries by **Markdown
  file stem**, not frontmatter name: `bar.md` + `name: baz` is excluded by
  `"bar"`, not `"baz"`. This matches the `plugin validate` exclusion rule.
* Plugin folders load only when they contain a parseable `plugin.json`. A
  folder without one is ignored even if `agents/` is present.
* User plugins respect `~/.gemini/config/config.json`:
  `{"plugins":{"p":{"enabled":false}}}` hides that plugin's agents. A missing
  entry (or `enabled` absent) means enabled.
* Workspace plugins were **not** disabled by `<workspace>/.agents/config.json`
  in the probe; the clone ignores that file for plugin enablement.
* Exact errors (exit 1 in `-p`):

```text
error: unknown agent "nosuch" (no custom agents are available)
error: unknown agent "nosuch" (available: alpha, zebra)
```

`--agent default` and `--agent=` run normally. Interactive mode renders

```text
⚠ Agent Not Found
  ⎿  unknown agent "nosuch" (no custom agents are available). Using the default agent instead.
```

captured under a pty from the reference.

### Prompt

A custom agent does **not** get the default ten-section prompt with the
identity swapped. It gets a different six-section prompt, in this order:

```text
<System Prompt>
{body}
</System Prompt>
<user_information>…</user_information>
<skills>…</skills>
<messaging>…</messaging>
<artifacts>…</artifacts>
<guidelines>…</guidelines>
<communication_style>…</communication_style>
```

* `<System Prompt>` is a literal tag with a space, not XML. The body is
  `strings.TrimSpace`d (verified with leading/trailing whitespace).
* `<identity>`, `<conversation_transcript>`, `<slash_commands>`, and
  `<subagents>` are absent.
* `<skills>` is the same global alphabetical list as the default prompt, minus
  the two builtin skills (everything under `<agentHome>/builtin/skills`).
  Workspace skills (`<cwd>/.agents/skills`), workspace-plugin skills
  (`<cwd>/.agents/plugins/<p>/skills`), and user-plugin skills remain. A plugin
  contributes only with a parseable `plugin.json`; user plugins also obey
  `config.json` enablement.
* `<messaging>` is the default messaging section minus the subagent/peer-agent
  clauses: the agent can receive background-task and user-queued messages only.
  The exact body is `internal/agyprompt/sections/agent_messaging.txt`.
* `<user_information>`, `<artifacts>`, `<guidelines>`, and
  `<communication_style>` are byte-identical to the default prompt's sections.

Verification: the clone request's system prompt, after replacing conversation
UUIDs, has SHA-256

```text
2557ba0449ba1b4a9bc94d244cc2ab157532c8aa4856e2a38e99de9e2db4f733
```

for both reference and clone (27,279 bytes normalized; 27,343 raw). A second
capture with an extra workspace skill and workspace-plugin skill is also
byte-identical on both sides (27,482 bytes), which is what pins the workspace
roots and the global sort.

### Tools

A custom agent advertises exactly seven declarations, in request order:

```text
send_message, view_file, read_url_content, search_web, schedule,
generate_image, manage_task
```

This is the read-only/research surface: no `run_command`, no file writes, no
subagent management. The declarations' descriptions and parameter schemas are
stored verbatim in `internal/agenttools/tools_1.4.3.json`; the clone's captured
declarations compare **JSON-equal** to the reference and have SHA-256

```text
c10adf15f145c684bc1acb839af48c92556220b99a0e5864fef54d4dbe0a4c79
```

on both sides. Executions route to the existing `internal/tools` executors of
the same name; `view_file` is covered by a test that reads a real file through
the captured schema.

### Wiring

* `pigrt.Options` gained `AgentBody` and `CustomSubagents`. An empty
  `SystemPrompt` + non-empty `AgentBody` selects `agyprompt.BuildAgent`.
* Print mode sets `ExtraTools` to `agenttools.Tools(catalog)` and skips the
  subagent services entirely; the default agent keeps the full catalogue plus
  subagent tools.
* The TUI sets `runMode.agentBody`, builds `BuildAgent` in `systemPrompt()`,
  filters the catalogue with `agenttools.Registry`, and returns no subagent
  extras. `customSubagents()` is appended only for the default agent.
* A real TTY gets the in-TUI `⚠ Agent Not Found` card; a non-TTY interactive
  run keeps the stderr line. The card's body color was also corrected to the
  reference's `#586E75` (the old `#073642` was visibly darker).
* `isTerminalInteractive` now uses `term.IsTerminal` instead of a char-device
  test, because `/dev/null` is a char device: automated `--agent` runs were
  trying bubbletea, failing on `/dev/tty`, and losing the fallback warning.
* The user customizations tree follows `$ANTIGRAVITY_HOME` (its sibling
  `config/`), matching `auth.GeminiConfigHome`; `DiscoverOptions.Home` still
  pins it for tests.

### Default-prompt fixes found while verifying

The live default-agent capture exposed three pre-existing divergences, fixed
here because the custom prompt shares the same machinery:

1. **Shared skills are not in the prompt.** The reference's `<skills>` list
   contains the builtin bundle, workspace skills, and plugins;
   `~/.gemini/skills` entries (`agents-sdk`, `wrangler`, …) are **absent** (the
   `/skills` command does list them, so `printSlashSkillInfos` keeps that
   root). `AgySkillPaths` no longer returns it, and now does return
   `<cwd>/.agents/skills` and workspace-plugin `skills/` (both verified live).
2. **Builtin skills are allowlisted.** Only `agy-customizations` and
   `antigravity_guide` appear; the other seven bundled directories are
   internal. The prompt now shares `agyprompt.BuiltinSkillDirs` with `/skills`.
3. **Skills sort globally by name.** Concatenating per-root `ReadDir` order
   grouped builtins, then plugins, and diverged from the reference's
   alphabetical list. `DiscoverSkills` now sorts the merged list.
4. **Custom agents are subagents.** The default prompt lists discovered
   markdown agents after `image-generator` (live: `- foo: A test agent`), so
   `pigrt.Options.CustomSubagents` feeds `customSubagents()` into the default
   `<subagents>` section.

After these fixes the default prompt differs from the same 1.4.3 capture only
by the two missing slash-command lines below.

---

## Known gaps

* The default prompt still lacks `/teamwork` and `/boost`; the clone has no
  implementation of either command. The custom-agent prompt is unaffected
  (it has no `<slash_commands>` section).
* The default agent's advertised tool surface is the clone's 60-name catalogue;
  the same 1.4.3 request advertises 12 (`read_file`, `run_command`,
  `manage_task`, `send_message`, `schedule`, `invoke_subagent`,
  `define_subagent`, `manage_subagents`, `write_file`, `edit_file`,
  `search_web`, `ask_question`). This is a separate, pre-existing surface
  drift; custom agents are exact.
* MCP tools are not included in the captured comparison (no MCP server was
  configured for the turn). The reference presumably adds them to the seven;
  the clone's PiG session does too, but that ordering was not measured.
* Precedence between a workspace/user markdown agent and a plugin agent with
  the same name was not measured (no model turn can run under a fake `HOME`,
  and the error list dedupes the name). The clone applies workspace custom >
  user custom > workspace plugin > user plugin.
* Workspace-plugin enablement is not understood: `.agents/config.json` did not
  disable one, so the clone always loads workspace plugins.

## 硬约束

* The only authoritative source used here is the live 1.4.3 binary and the
  requests it emitted. The committed Ghidra project is 1.3.4 and was not used
  for this cycle.
* The captured declarations are tied to 1.4.3's Gemini 3.8 Flash surface. A
  model with a different tool surface (or a later release) may advertise a
  different set; `internal/referencedrift` will not detect this fixture because
  it is not in `reference.lock.json`.
* The custom prompt's byte-identity depends on the host's skill inventory.
  The comparison was made with the same machine and the same plugin set; the
  stable claim is the section structure/order and the body/allowlist rules, not
  the 27 KB literal.

## 存疑

* Whether plugin agents are listed in the default `<subagents>` section was
  inferred from the shared discovery path, not captured with a valid plugin
  agent and a live turn.
* Whether the reference applies `config.json` enablement to workspace plugins
  at all (the probe says no for `.agents/config.json`).
* Duplicate-name precedence across scopes (see above).
* Whether `--agent` accepts an agent whose body is empty but frontmatter is
  valid; the fixture always had a body. The loader's frontmatter check is
  exact, the body check was not probed.

## Limitations

* No live custom-agent **tool call** was executed against the reference in this
  cycle; only the request was compared. The clone's executor routing is tested
  with `view_file`; the other six names reuse the same wrappers.
* The TUI's custom-agent path was verified by unit tests and code inspection,
  not by a full interactive turn; the `Agent Not Found` card was captured under
  a pty and compared textually.
* The report does not claim parity for the default agent's tool surface,
  `/teamwork`, `/boost`, or model-specific tool aliases.
