# The system prompt and custom agents

> The 70 KB instruction block, where it is assembled, and how `--agent` replaces it.

## The system prompt (`internal/agyprompt`, cycle agy-system-prompt)

The reference sends a **70,853-byte** instruction block on every request (at
agy 1.4.6; it was 34,350 bytes at 1.4.4); before this cycle the clone sent
none. `internal/agyprompt` reproduces it byte for byte. Read in this order:

| File | What it is |
|---|---|
| `internal/agyprompt/agyprompt.go` | `Options` + `Build`: twelve XML sections, fixed order, no trailing newline |
| `internal/agyprompt/sections/*.txt` | the twelve static bodies (embedded) |
| `internal/agyprompt/testdata/capture_1.4.6.txt` | the ground truth — a real agy 1.4.6 request recorded through the proxy |
| `internal/agyprompt/skills.go` | `DiscoverSkills`: the agy skill roots → the `<skills>` list |
| `internal/agyprompt/rules.go` | `DiscoverUserRules`: the rule files → the `<user_rules>` section |
| `internal/plugins/plugins.go` | which user plugins are loaded (parseable `plugin.json`, `plugins.<name>.enabled`), and which skill directories they contribute |
| `internal/pigrt/runtime.go` | an empty `Options.SystemPrompt` now means **the reference prompt**, not PiG's; `PigPrompt: true` restores PiG's |
| `internal/cliapp/tui_stream_prompt.go` | the TUI's builder, passed as `agent.Options.SystemPrompt` |

Five things to know:

- **The system prompt had never reached the wire.** `piagentbridge.systemText`
  read only `SystemMessage.Content`, but a `coding.Session` puts the text in
  `Sections` and leaves `Content` empty — so every prompt was rendered as `""`
  and dropped, on both transports. It now renders content **and** sections the
  way PiG's `ai.GetCurrentSystemPrompt` does. `internal/piagentbridge/system_text_test.go`
  pins it.
- **`<user_rules>` is environment-derived, and 1.4.5 changed it.** The section
  is the user's own rule files, discovered by `DiscoverUserRules`: the four
  home-level files (`~/.gemini/{AGENTS,GEMINI}.md`, `~/.gemini/config/{AGENTS,GEMINI}.md`)
  are printed with the fixed label **`user_global`**, then every loaded
  plugin's `rules/{AGENTS,GEMINI}.md`, then `~/.gemini/config/rules/{AGENTS,GEMINI}.md`,
  then `<dir>/{AGENTS,GEMINI}.md` and `<dir>/.agents/rules/{AGENTS,GEMINI}.md`
  for every ancestor of the workspace. Only those two file names are taken —
  `PROBE.md` beside them is ignored — and `AGENTS.md` precedes `GEMINI.md`
  everywhere. The body is verbatim, trailing newline included, so a rule that
  ends in a newline renders with a blank line before `</RULE[...]>`. The
  preamble says "these rules take precedence over any **other** instructions"
  — the 1.3.x–1.4.3 wording was "any **following** instructions". A custom
  agent's prompt carries the same section, between `<user_information>` and
  `<skills>`. Report: `docs/cycles/reference-drift-145/`.
- **1.4.6 added `<terminal_sandbox>`** (3,371 B, second position, right after
  `<identity>`) and one `<guidelines>` bullet. The section is the prompt half
  of the `run_command` `BypassSandbox` parameter (see *The tool surface*).
  Report: `docs/cycles/reference-drift-146/`.
- **The capture is authoritative.** The leaked
  [system_prompts_leaks PR #233](https://github.com/asgeirtj/system_prompts_leaks/pull/233)
  is the **IDE** prompt, not the CLI's; it corroborates the identity line and
  the `<user_information>` fields and nothing more. Both external references are
  listed in `docs/research/REFERENCES.md`.
- **Releasing a new `agy` invalidates this fixture.** `internal/referencedrift` now
  detects that (see *Reference drift* below): it names this file as suspect and
  prints the re-capture procedure. Note that `TestBuildMatchesCapture` will **not**
  be what fails first — it only compares our `Build()` against this committed
  capture, so both can be stale together and stay green. Re-capture via
  `OPENAGY_CAPTURE_DIR` (see the wrapper section below), regenerate
  `sections/*.txt`, then reseal the lock.

Report: `docs/cycles/agy-system-prompt/deliverable.md`,
`docs/cycles/reference-drift-146/deliverable.md`.

---


## Custom agents (`--agent`, cycle `custom-agents`)

`--agent <name>` selects a Markdown agent, not a built-in catalogue entry.
`internal/agent/list.go` discovers them from four sources and `ValidateAgent`
produces the reference's exact errors. Read it before touching the flag.

| Source | Root |
|---|---|
| Workspace custom | `<cwd>/.agents/agents/*.md` |
| User custom | `~/.gemini/config/agents/*.md` |
| Workspace plugin | `<cwd>/.agents/plugins/<plugin>/agents/*.md` |
| User plugin | `~/.gemini/config/plugins/<plugin>/agents/*.md` |

Frontmatter must carry non-empty `name` and `description`; `--agent` matches
the **name**, case-sensitively, and the body becomes the system prompt. An
`agents.json` beside `agents/` excludes by **file stem**, not name. Plugins
need a parseable `plugin.json`; user plugins also honor `config.json`
`plugins.<name>.enabled=false`. `default`/`""` is always accepted and is never
listed in `(available: …)`.

A custom agent replaces both prompt and tool surface:

- `internal/agyprompt.BuildAgent` emits the body **split at level-1 Markdown
  headings** — each part wrapped in a tag named after its heading, the part
  before the first heading keeping the literal `<System Prompt>` tag — then
  `user_information, user_rules, skills, messaging, artifacts, guidelines,
  communication_style`. No `identity`, `terminal_sandbox`, `subagents`,
  `slash_commands`, or `conversation_transcript`; `sections/agent_messaging.txt`
  is the reduced messaging body; builtin skills are filtered out. The five
  captured shapes are pinned by `internal/agyprompt/agent_blocks_test.go`.
- Skills in both prompts are the builtin allowlist, `<cwd>/.agents/skills`,
  the shared `~/.gemini/skills` library, and plugin `skills/` (a parseable
  `plugin.json` is required; user plugins also honor `config.json`). The
  shared library **is** in the prompt — the earlier "`/skills`-only" claim
  came from 1.4.2/1.4.3 captures taken on a machine whose library was empty
  (【一手】, live 1.4.6 request, `docs/cycles/reference-drift-146/`).
- `internal/agenttools` holds the captured 1.4.7 declarations for exactly
  `send_message, view_file, read_url_content, search_web, schedule,
  generate_image, manage_task`. `Tools` adapts them to PiG; `Registry` is the
  filtered TUI catalogue. Print mode sets `AgentBody`, `ExtraTools`, skips the
  subagent services; the TUI sets `runMode.agentBody` and builds `BuildAgent`
  in `systemPrompt`.

Three measured traps: the seven tool declarations are byte-identical to the
reference, so do not "improve" their descriptions; an unknown name on a
real TTY renders the in-TUI `⚠ Agent Not Found` card (orange header, `#586E75`
dim body), not a stderr line; and only **level-1** headings split the body
(`## Two` stays inside the enclosing block). Report:
`docs/cycles/custom-agents/`, `docs/cycles/reference-drift-146/`.

**1.4.7 rewrote all seven declarations** (【一手】, live 1.4.7 request through
the clone's own proxy, `docs/cycles/reference-drift-147/`). The names and their
order are unchanged, but `toolSummary`/`toolAction` are **gone** from every one
of them (they belonged to the reference's *subagent* surface), the
descriptions were reworded to the current wording, and the schemas gained
`minLength: "1"`, an `enum` on `generate_image.AspectRatio`, `maxItems: "3"`
on `generate_image.ImagePaths` and a new `schedule.ExactTiming` property. The
required lists shrank accordingly — a custom agent's `send_message` no longer
has to carry a summary and an action. The old wording is still in the 1.4.7
binary, so a `strings` hit proves nothing about this surface.
