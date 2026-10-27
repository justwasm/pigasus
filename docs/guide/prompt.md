# The system prompt and custom agents

> The 70 KB instruction block, where it is assembled, and how `--agent` replaces it.

## The system prompt (`internal/agyprompt`, cycle agy-system-prompt)

The reference sends a **70,853-byte** instruction block on every request (at
agy 1.4.6; it was 34,350 bytes at 1.4.4); before this cycle the clone sent
none. `internal/agyprompt` reproduces it byte for byte. Read in this order:

| File | What it is |
|---|---|
| `internal/agyprompt/agyprompt.go` | `Options` + `Build`: twelve XML sections, fixed order, no trailing newline |
| `internal/agyprompt/template.go` | the renderer: the recovered templates, the data model they read, the section framing |
| `internal/agytemplates/` | **the reference's own `.tmpl` files** (17, embedded once) — `templates/PROVENANCE.md` has the digests and the extraction recipe; `internal/agyprompt` and `internal/agystep` are the two consumers |
| `internal/agyprompt/sections/*.txt` | the five bodies that have no template (see below) |
| `internal/agyprompt/testdata/capture_1.4.6.txt` | the ground truth — a real agy 1.4.6 request recorded through the proxy |
| `internal/agyprompt/skills.go` | `DiscoverSkills`: the agy skill roots → the `<skills>` list |
| `internal/agyprompt/rules.go` | `DiscoverUserRules`: the rule files → the `<user_rules>` section |
| `internal/builtinskills/` | the reference's **builtin skill bundle** (9 skills), unpacked into `<AppDataDir>/builtin/skills` |
| `internal/plugins/plugins.go` | which user plugins are loaded (parseable `plugin.json`, `plugins.<name>.enabled`), and which skill directories they contribute |
| `internal/pigrt/runtime.go` | an empty `Options.SystemPrompt` now means **the reference prompt**, not PiG's; `PigPrompt: true` restores PiG's |
| `internal/cliapp/tui_stream_prompt.go` | the TUI's builder, passed as `agent.Options.SystemPrompt` |

### The prompt is rendered from the reference's templates

**Seven of the twelve sections are no longer hardcoded.** The reference
assembles the block from Go `text/template` sources it embeds under
`google3/third_party/jetski/prompt/template_provider/templates/`; those 17
files are now in the repo, byte for byte, under
`internal/agytemplates/templates/`, and `internal/agyprompt/template.go`
renders them with `text/template`:

| Section | Template |
|---|---|
| `<identity>` | `system_prompts/identity.tmpl` |
| `<artifacts>` | `system_prompts/artifacts.tmpl` |
| `<conversation_transcript>` | `system_prompts/conversation_transcript.tmpl` |
| `<guidelines>` | `system_prompts/guidelines.tmpl` |
| `<communication_style>` | `system_prompts/communication_style.tmpl` |
| `<slash_commands>` | `system_prompts/slash_commands.tmpl` |
| `<skills>` | `system_prompts/skills.tmpl` |

The five that remain are the ones the binary carries **without** a template
next to them: `<terminal_sandbox>`, `<messaging>` and `<subagents>` are
literal prompt text in the reference binary, and `<user_rules>` /
`<user_information>` are assembled from the host rather than from a file.
They keep their `sections/*.txt` bodies.

Four things about the templates that cost time to find:

- **A bare `{{ArtifactDirectoryPath}}` is a function, not a field.** In a Go
  template, `{{Foo}}` calls a function; `{{.Foo}}` reads a field. The
  reference registers `ArtifactDirectoryPath`, `SystemGeneratedLogsPath`,
  `KnowledgeDirectoryPath` and `MetadataFileName` as functions, and so does
  `newTemplateSet`.
- **`.ExperimentEnabled` has to be a method.** A Go template can only call a
  *field* of function type without arguments, and `artifacts.tmpl` calls it
  with one (`{{if .ExperimentEnabled "enable-url-artifacts"}}`).
- **The section framing is `"<tag>\n" + body + "\n</tag>"`.** The templates
  render the body without the two newlines, so `framed()` adds them; the
  frozen bodies in `sections/` carry them, which is why they start with one.
- **The paths the templates print are the reference's placeholders.**
  `<appDataDir>/brain/<conversation-id>`, not the real directory — the real
  one is in `<user_information>`. The same literals are in the reference
  binary next to the templates, so this is the reference's own text and not a
  redaction of the capture.

The branches the templates take are inputs now: `Options.Autonomous`,
`Options.SupportsLatex`, `Options.PlanModel`, `Options.Experiments` and
`Options.SlashCommands`. The defaults reproduce the captured 1.4.6 request
byte for byte.

### The builtin skills are the reference's

`internal/builtinskills/` carries the nine skills the CLI embeds — the two
`assets/external/skills/` ones the prompt advertises (`agy-customizations`,
`antigravity_guide`) and the seven `assets/shared/skills/` ones behind
feature flags — and `internal/cliapp/run.go` unpacks them into
`<AppDataDir>/builtin/skills` on startup. It never overwrites a file that is
already there: the reference and the clone share that tree, and the
reference's own install wins. The prompt lists the two, not the nine
(`agyprompt.BuiltinSkillDirs`).

Five things to know:


- **The prompt is a `.tmpl` file, and the templates are now in the repo.** The
  reference assembles the block from templates the binary embeds. The CLI's
  copies were extracted verbatim into `internal/agytemplates/templates/` (17
  files: `system_prompts/`, `step_strings/`, `helpers/`) and the prompt is
  rendered from them. The IDE's copies are in
  `out/ide-language-server/embed-fs/google3/third_party/jetski/prompt/template_provider/templates/`
  and are **not** the same bytes — the CLI's `guidelines.tmpl` has one bullet
  the IDE's does not, and the captured CLI request contains that bullet, so
  the CLI's copy is the one that has to be rendered. The built-in skills the
  prompt advertises came out of the same binary and are now in
  `internal/builtinskills/`. See
  `out/ide-language-server/embedded-assets.md`.
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
  `OPENAGY_CAPTURE_DIR` (see the wrapper section below), then re-extract the
  templates from the new binary into `internal/agytemplates/templates/` (the
  digests in `templates/PROVENANCE.md` and in
  `internal/agytemplates/agytemplates_test.go` have to be updated with them),
  then reseal the lock.

Report: `docs/cycles/agy-system-prompt/deliverable.md`,
`docs/cycles/reference-drift-146/deliverable.md`.

---

## The cumulative prompt (`internal/cumulativeprompt`, cycle `cycle-312-cumulative-prompt`, aligned in `cycle-313-cumulative-prompt-alignment`, config pinned in `cycle-314-cumulative-prompt-config`)

The system prompt is one half of what reaches the model. The other half is
the **cumulative prompt**: the reference's handler builds every prompt out of
a *persistent context* — what it keeps between turns — and the turn's own
messages. One piece of that context is the **implicit trajectory**: the `!`
commands the user ran outside the conversation.

The reference records a `!` command in that trajectory *only*; it never enters
the conversation. `internal/implicit` writes the store byte for byte
(cycle 304), and `internal/cumulativeprompt` is what makes it model-visible.

### What a `!` command contributes

One step becomes **two** chat messages, both in the user-content position,
each carrying a three-line header:

```
Step Id: 0
Created At: 2026-10-26T12:34:56+08:00
Completed At: 2026-10-26T12:34:57+08:00
The USER performed the following action:
Command: ls -la
CWD: /private/var/folders/…/T/opencode
```

```
Step Id: 0
Created At: 2026-10-26T12:34:56+08:00
Completed At: 2026-10-26T12:34:57+08:00

The command exited with code 0.
Output:
total 12
```

The first message is the **run command's input string** — the reference's own
`code.(*RunCommandStringConverter).ToInputString`: the *proposed* command
line (the command line when the proposed one is empty), labelled `Command: `,
and the working directory on a second line when the record carries one. The
second message is the **step string** — `step_strings/run_command.tmpl`'s
rendering — and *not* the CLI's own `Command exited with code N.` formatter.
Both shapes live in the clone (`internal/agystep` reads both); the cumulative
prompt uses the step-string one.

### The step id, the gate and the limit

- **The id** is the step's own index in the trajectory — unless the server's
  config says otherwise. The reference's formula is
  `increment + (start + index − cached)`, where `increment` and `start` are
  the config's `trajectory_index_increment` and `trajectory_start_index` and
  `cached` is the number of messages an earlier prompt carried.
  `Options.StepIndexIncrement`, `Options.TrajectoryStartIndex` and
  `Options.CachedMessages` model the three; the defaults reproduce the
  reference's own default config.
- **The gate** is the config's `allowed_implicit_step_types` — a
  **`map[string, bool]`** from a step type's *name* to whether it may
  contribute. The entry must be present *and* true; a type that is absent
  and a type that is present-and-false are filtered alike. The reference's
  own default gate carries eight step types, all true — the run command
  among them — and that is the clone's default.
- **The limit** is the config's `max_tokens_per_step`, applied to the two
  messages' text. It is zero by default — inert, like the reference's own
  default — and a text over the limit is cut to `limit × 3` bytes and gets
  the reference's own note appended.

### The config itself

The cumulative prompt is driven by `exa.prompt_pb.CumulativePromptConfig` — a
message the reference's binary carries whole, recovered in
`out/local-macos/protos/prompt_1.4.7.proto`. Nineteen fields: the fourteen
multipliers and token reservations that size the persistent, trajectory and
ephemeral context, the gate, the cascade list, `disable_custom_step_id` and
the two step-id knobs.

The reference's own default config (the one it builds when the server-side
flag is absent) is now pinned value by value — the trajectory's context
multiplier is **0.65**, its refresh threshold **0.9**, its truncation
multiplier **0.5**, the intent reserves **512** tokens, and the cascade list
is **empty**. Those are the numbers the *trajectory-level cut* needs; the
formula that combines them with the model's context window is still open.

The per-step token limit and the context-role flag are **not** fields of this
config — they belong to the converter's own `exa.cortex_pb.TrajectoryConversionConfig`.

### Where the messages live

They are **not** in `m.session`: the conversation store holds no `!` record in
the reference, and the clone keeps it that way. The TUI holds them in
`runMode.implicitMsgs` and prepends them to the turn's messages
(`internal/cliapp/tui_cumulative.go`); the print path puts them in front of
the resumed conversation's turns (`pigSeedHistory`).

Only the steps no prompt has carried yet are converted — the reference caches
what it already converted (`SetPersistentCachedContext`), and the clone keeps
a cursor (`runMode.implicitCursor`).

Report: `docs/cycles/cycle-312-cumulative-prompt/deliverable.md`,
`docs/cycles/cycle-313-cumulative-prompt-alignment/deliverable.md`,
`docs/cycles/cycle-314-cumulative-prompt-config/deliverable.md`.

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
