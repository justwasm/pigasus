# Slice C — configuration surfaces

A read-only divergence survey: the official Antigravity CLI docs against the Go
clone in this tree, scoped to the **configuration surfaces** — the settings
file and its `/config` panel, the permission engine and its rule grammar, the
MCP server configuration, the hooks configuration, the rules/skills/plugins
discovery roots, the marketplace, the model catalogue, the projects store and
the terminal sandbox.

## Method

Every doc in the slice was read in full (12 files, 3,158 lines):
`official-docs/{settings,permissions,hooks,mcp,agent-settings,rules,sandbox,plugins,marketplace,skills,models,projects}.md`.

Each concrete, checkable claim — a config file path, a JSON key name, a default
value, an enum member set, a precedence order, an on-disk location, a
permission-mode name, a hook event name and its JSON shape, an MCP transport
name, a model id string, a marketplace layout — was traced to the clone's
implementation and cited with a `file:line` anchor. Where the docs and the
clone's own recovered evidence disagree, the disagreement is **flagged rather
than resolved** (see **Flags, ambiguities and confusions**).

The survey was read-only: no file in the tree was modified, no commit was
made, and `git status --porcelain` stayed clean. Every line citation below was
re-pinned against the working tree after the first pass.

Counts use two units: *claims* = individual documented assertions examined;
*findings* = the reportable blocks (matches, divergences, doc-only,
clone-only).

## Caveats

(a) No finding rests on running the reference binary. Every claim rests on
source reading in the clone plus the clone's own recorded 【一手】/【反编译】
evidence, and — for the permission cluster — the reference binary's own string
table at `out/local-macos-1.4.11/strings.txt`.

(b) IDE-vs-CLI descriptions were excluded as *claims*. The Antigravity App
section of `official-docs/permissions.md` (lines 11-189) and the IDE sections
of `rules.md`, `hooks.md` and `plugins.md` were read but not treated as CLI
behaviour — except where the App and CLI sections of the same doc were
compared for **internal consistency** (C-21).

(c) Where the doc describes a surface the clone has no equivalent for, the
finding is marked *missing-feature* rather than guessed at.

(d) `internal/lsserver/*` is the **IDE language-server** surface, not the
CLI's. It is never used as evidence about a CLI path here.

---

## Match (grouped, one line each)

### Settings file

- **settings.json location** — `~/.gemini/antigravity-cli/settings.json` — `official-docs/agent-settings.md:90` vs `internal/core/settings/settings.go:92` `settings.Path()` → `confhome.Antigravity()`.
- **Sparse persistence** — "the CLI saves only the keys whose values differ from the defaults. Setting an option back to its default removes its key" — `official-docs/settings.md:123` vs `internal/core/settings/settings.go:358` `DeleteField`.
- **File mode** — "makes it readable only by your user (`0600`)" — `official-docs/settings.md:123` vs `internal/core/settings/settings.go:214` `os.WriteFile(path, out, 0600)`.
- **Unknown keys kept** — "The CLI ignores `$schema` … and keeps it when it saves the file" — `official-docs/settings.md:110` vs `internal/core/settings/settings.go:183` `modifySettings` (sjson-based, so unknown members survive a save).
- **settings.json key set** — the clone's 36-key catalogue — `internal/printmode/print_slash_config.go:17-45` — is the reference's own key set and is a superset of every key these 12 docs name.
- **`agentMode` enum** — `default`/`accept-edits`/`auto`/`plan` — `official-docs/settings.md:200-203` vs `internal/core/policy/permissions/policy.go:41-42` `Modes`, `:452` `CycleModes` (the Shift+Tab order).
- **`artifactReviewPolicy` enum** — `asks-for-review`/`agent-decides`/`always-proceed` — `official-docs/settings.md:210-212` vs `internal/ui/views/configpanel/configpanel.go:134`.
- **`colorScheme` enum** — `terminal`/`light`/`solarized light`/`colorblind-friendly light`/`dark`/`solarized dark`/`colorblind-friendly dark`/`tokyo night` — `official-docs/settings.md:229` vs `internal/ui/views/configpanel/configpanel.go:138`.
- **`runningLightSpeed` enum** — `fast`/`medium`/`slow`/`off` — `official-docs/settings.md:231` vs `internal/ui/views/configpanel/configpanel.go:127`.
- **`enableTerminalSandbox` key + `--sandbox` flag** — `official-docs/sandbox.md:147`, `:169` vs `internal/ui/views/configpanel/configpanel.go:209`, `internal/cli/parser_flags.go:81`, read at `internal/app/session/setup/setup.go:96`.
- **`allowNonWorkspaceAccess` key** — `official-docs/settings.md:216` vs `internal/ui/views/configpanel/configpanel.go:174`.
- **`statusLine` / `title` object keys** — `command`/`type`/`enabled`/`padding`/`stack_with_default` — `official-docs/settings.md:298-304` vs `internal/core/settings/settings.go:78` (`statusLine`, `title` in the key table).

### Permissions

- **`permissions` object in settings.json** — root key `permissions` with `allow`/`deny`/`ask` sub-keys — `official-docs/settings.md:284-292` vs `internal/core/policy/permissions/panel_rules.go:27` `PermListKeys = [3]string{"allow", "deny", "ask"}`, written at `:115`.
- **Three-scope model** — "the rules in the **Global** scope … The CLI combines them with the rules in the **Project** and **Shared** scopes" — `official-docs/settings.md:294` vs `internal/core/policy/permissions/panel_rules.go:18-23` `PermScopeNames` (three scopes, in the doc's order).
- **Scope labels** — Global / Project / Shared (the clone's middle label is branded, see **Deliberate clone differences**) — `official-docs/settings.md:294` vs `internal/core/policy/permissions/panel_rules.go:23`.
- **Grant rendering shape** — the panel renders `read_file(/path)` / `command(args)` strings — `official-docs/settings.md:317` vs `internal/ui/views/permissionspanel/panel_render.go:126`.

### Rules

- **Standalone global files** — `~/.gemini/AGENTS.md`, `~/.gemini/GEMINI.md`, `~/.gemini/config/AGENTS.md`, `~/.gemini/config/GEMINI.md` — `official-docs/rules.md:27` vs `internal/app/prompt/agyprompt/rules.go:98-109`.
- **Directory-scoped files** — `<dir>/AGENTS.md` / `<dir>/GEMINI.md` — `official-docs/rules.md:19` vs `internal/app/prompt/agyprompt/rules.go:127-131`.
- **Per-file 24,000-byte cap** — "Antigravity truncates any single rule file that exceeds 24,000 bytes" — `official-docs/rules.md:138` vs `internal/app/prompt/agyprompt/rules.go:31` `userRuleByteCap = 24000`, applied at `:161`.
- **Oversized rules demoted to pointers** — `official-docs/rules.md:139` vs `internal/app/prompt/agyprompt/rules.go:279` (the reference's "exceeded the rules token budget" wording).

### Skills, plugins, marketplace

- **Skills: workspace root** — `<workspace-root>/.agents/skills/<skill-folder>/` — `official-docs/skills.md:131` vs `internal/features/tools/skills/discovery.go:91`.
- **Plugin manifest validation** — `plugin.json` name/version/description/author — `official-docs/plugins.md:141` vs `internal/cli/plugin_validate_schema.go`.
- **Plugin hook event names recognised** — the six reference events — `official-docs/hooks.md:107-113` vs `internal/cli/plugin_validate_hooks.go:14` `hookEvents`.
- **Marketplace feed cache** — `~/.gemini/antigravity-cli/cache/marketplaces/<mkt>/marketplace.json` — `official-docs/marketplace.md:120` vs `internal/remotecontrol/plugins_marketplace.go:13`.
- **`agy plugin` subcommand set** — `list`/`import`/`install`/`uninstall`/`enable`/`disable`/`validate` — `official-docs/plugins.md:245-259` vs `internal/cli/subcommand_plugin.go:22-33`.

### MCP

- **`mcp.json` root `servers` + `disableAllMCP`** — `official-docs/mcp.md:65` (root key `mcpServers` in the doc; see C-8) vs `mcp/agyconfig.go:102-111`.
- **Per-server `command`/`args`/`url`/`env`/`headers`/`enabled`** — `official-docs/mcp.md:145-153` vs `mcp/agyconfig.go:120-137`.
- **Transport normalisation** — a URL means HTTP, a command means stdio — `official-docs/mcp.md:141` vs `mcp/agyconfig.go:140` `NormaliseType`.
- **`agy mcp add` write path** — `~/.gemini/config/mcp_config.json`, root `mcpServers` — `official-docs/mcp.md:65` vs `internal/cli/subcommand_mcp.go:83-95`.

### Projects, sandbox

- **Project store** — `~/.gemini/config/projects/<id>.json` — `official-docs/projects.md:62` vs `internal/core/cliflags/projects/projects.go:157` `storeDir`.
- **Projects cache** — `~/.gemini/antigravity-cli/cache/projects.json` — `internal/core/cliflags/projects/projects.go:162` `cachePath`.
- **`--project` / `--new-project` flags** — `official-docs/projects.md:70-81` vs `internal/cli/parser_flags.go:77` / `:66`.
- **`--conversation=<id>` flag** — `official-docs/projects.md:86` vs `internal/cli/parser_flags.go:46`.
- **`/fork <project_id>`** — `official-docs/projects.md:90-93` vs `internal/cliapp/slash_commands_handlers.go:207` `slashFork`.
- **Sandbox shape** — a path allow-list plus a macOS Seatbelt (SBPL) profile generator — `official-docs/sandbox.md:20` vs `internal/core/policy/sandbox/sandbox.go:80` `Resolve`, `:131` `Allowed`, `:197` `CheckCommand`, `internal/core/policy/permissions/sandbox.go:70`.

---

## Divergences

### C-1 — `toolPermission` offers 2 of the 4 documented values, and the default is wrong — **Closed**

- **Doc claim:** "`request-review` (default): Prompts for your approval before running write, bash, web, or MCP tools, unless an allow rule covers the action." / "`proceed-in-sandbox`: Runs terminal commands without prompting when Sandbox Mode is on and the command runs inside the terminal sandbox; otherwise prompts for review." / "`strict`: Prompts before every tool action…" / "`always-proceed`: Runs terminal commands, web requests, and MCP tools without prompting…" — `official-docs/settings.md:205-208`; repeated at `official-docs/agent-settings.md:77-80` and `official-docs/sandbox.md:148`.
- **Clone:** Closed in `fix(permissions)` (commit `699f5636`). `internal/ui/views/configpanel/configpanel.go` offers all four documented values: `request-review` (default, stored as `""` for unset fallback), `proceed-in-sandbox`, `strict`, and `always-proceed`.
- **Verdict:** closed.
- **Severity:** `enum-member` + `default-value`.

### C-2 — `toolPermission` is display-only; it never reaches the policy — **Closed**

- **Doc claim:** "**Tool Permission (`toolPermission`)**: Controls when the agent asks for your approval before it uses a tool, such as running a terminal command." — `official-docs/settings.md:204`; "Deny rules in your permissions block an action in every mode" — `official-docs/settings.md:203`.
- **Clone:** Closed in `fix(permissions)` (commit `699f5636`). `internal/app/session/setup/setup.go` wires `toolPermission` into `permissions.Policy`, and `internal/core/policy/permissions/policy.go` evaluates it against tool calls.
- **Verdict:** closed.
- **Severity:** `behaviour`.

### C-3 — `altScreenMode` has no `default` (hybrid) state — **Open (存疑)**

- **Doc claim:** "`default` (**native terminal (inline)**): Renders the conversation inline in your terminal's scrollback and opens full-screen views, such as the diff viewer, in the alternate screen." — `official-docs/settings.md:225`; and "With Rendering Mode set to `default` … the conversation renders inline, and full-screen views such as the diff viewer, the artifact viewer, and `/help` open in the alternate screen." — `official-docs/settings.md:189`.
- **Clone:** `internal/ui/views/configpanel/configpanel.go:205-207` offers only `V("no flickering (altscreen)", "always")` and `V("native terminal (inline)", "never")`. `internal/cliapp/tui_entry.go:124-127` `altScreenModeValue` maps *every* value other than `"never"` to the alternate screen — so the doc's hybrid mode is not implemented and `"default"` would silently mean "always".
- **Verdict:** divergence, but **ambiguous — flag, do not fix** (see **Flags, ambiguities and confusions** #1). The clone's own comment (`configpanel.go:186-204`) cites a raw pty capture of agy 1.4.7 in which an absent `altScreenMode` emits `?1049h` (alternate screen) and the value `"default"` is rejected by the reference's own settings validation. The docs describe a later behaviour than the clone's evidence.
- **Severity:** `enum-member` (with a `doc-wrong` candidate).
- **Minimal fix:** if a fresh capture of the reference confirms the doc, old `func altScreenModeValue(stored any) bool { s, _ := stored.(string); return s != "never" }` → new `func altScreenModeValue(stored any) bool { s, _ := stored.(string); return s == "always" }` plus a third mode, and `configpanel.go:205-207` gains `V("native terminal (inline)", "default")`. If the binary wins, the doc's `default` row must be corrected upstream instead.

### C-4 — the permission rule grammar is `tool:pattern`, not `action(target)` — **Closed**

- **Doc claim:** "Allow, ask, and deny rules for the agent's tools. Each rule is an `action(target)` string, such as `command(git status)` or `read_url(github.com)`, with no space before the opening parenthesis." — `official-docs/settings.md:282`; the action table lists `read_file`, `write_file`, `read_url`, `execute_url`, `command`, `unsandboxed`, `mcp` — `official-docs/permissions.md:342-348`; "Every sensitive operation the agent performs is represented as a **permission resource** formatted as `action(target)`" — `official-docs/permissions.md:318`.
- **Clone:** Closed in `fix(permissions)` (commit `699f5636`). Implemented `action(target)` grant grammar in `internal/core/policy/permissions` supporting the 6 live actions (`command`, `read_file`, `write_file`, `read_url`, `execute_url`, `mcp`) while retaining backward compatibility for legacy `tool:pattern` rules and rejecting retired `unsandboxed`.
- **Verdict:** closed.
- **Severity:** `behaviour`.

### C-5 — there is no Ask tier; precedence is Deny > Allow > mode > default — **Closed**

- **Doc claim:** "**Precedence rule**: Conflicting rules are strictly evaluated in priority order: **Deny > Ask > Allow**. For example, if you configure `command(*)` in Ask and `command(git)` in Allow, the Ask rule takes precedence and prompts before every command." — `official-docs/permissions.md:21` (repeated at `:198` and `:328`); "A deny rule takes precedence over an ask rule, and an ask rule takes precedence over an allow rule." — `official-docs/settings.md:282`.
- **Clone:** Closed in `fix(permissions)` (commit `699f5636`). Added `Ask []Rule` to `permissions.Policy`, evaluating in strict Deny > Ask > Allow precedence order in `DecideNames`.
- **Verdict:** closed.
- **Severity:** `behaviour`.

### C-6 — the unconfigured default is Allow, not Ask — **Closed**

- **Doc claim:** "**Unconfigured actions default to Ask**: All other unconfigured actions (`command`, `mcp`, `execute_url`, and non-workspace files) default to **Ask**, subject to your active execution mode and `toolPermission` setting." — `official-docs/permissions.md:387`; "Actions for `read_url` and `execute_url` default to **Ask**." — `official-docs/permissions.md:143`.
- **Clone:** Closed in `fix(permissions)` (commit `699f5636`). Default decision evaluation integrates `ToolPermission` (`request-review`, `strict`, `proceed-in-sandbox`) and falls back according to execution mode and sandbox state.
- **Verdict:** closed.
- **Severity:** `default-value` + `behaviour`.

### C-7 — `permissions.ask` is written by the panel but ignored at runtime — **Closed**

- **Doc claim:** the `permissions` object carries `"ask": ["command(git push)"]` — `official-docs/settings.md:288`; the `ask` list is one of the three lists the CLI honours — `official-docs/permissions.md:328`.
- **Clone:** Closed in `fix(permissions)` (commit `699f5636`). `internal/core/policy/permissions/settings.go` decodes `ask` alongside `allow` and `deny`, and `setup.go` assigns `p.Ask = settings.Ask`.
- **Verdict:** closed.
- **Severity:** `behaviour`.

### C-8 — the MCP runtime reads `mcp.json`; `agy mcp add` writes `mcp_config.json` — **Closed**

- **Doc claim:** "**Global configuration**: `~/.gemini/config/mcp_config.json`, which applies to all your projects." — `official-docs/mcp.md:65`; "**MCP servers**: `~/.gemini/config/mcp_config.json` for all workspaces, which you can manage with `/mcp`, or `<workspace>/.agents/mcp_config.json` for one workspace." — `official-docs/settings.md:118`; "Each server entry under `mcpServers` must specify one transport property" — `official-docs/mcp.md:141`.
- **Clone:** Closed in `fix/c10` (deliverable §26) and `99260d9e` (deliverable §2). `mcp/configpaths.go` and `mcp/document.go` unify `mcp_config.json` and `mcp.json` readers across all three levels.
- **Verdict:** closed.
- **Severity:** `behaviour` + `exact-path`.

### C-9 — no workspace-level MCP file — **Closed**

- **Doc claim:** "**Workspace configuration**: `<workspace>/.agents/mcp_config.json`, which applies only to that workspace." — `official-docs/mcp.md:66`; repeated at `official-docs/settings.md:118`.
- **Clone:** Closed in tranche `99260d9e` (deliverable §24) and unified across `InstallMCPProvider` via `mcp.ConfigPaths(agentDir, workspace)`. Workspace `.agents/mcp_config.json` is loaded and prioritized.
- **Verdict:** closed.
- **Severity:** `missing-feature`.

### C-10 — the MCP server property set is missing six documented keys, and the CLI writes `env`/`headers` as arrays — **Closed**

- **Doc claim:** the property table — `cwd` (`official-docs/mcp.md:149`), `authProviderType` (`official-docs/mcp.md:151`), `oauth` (`official-docs/mcp.md:152`), `enabledTools` (`official-docs/mcp.md:154`), `disabledTools` (`official-docs/mcp.md:155`), `timeoutSeconds` (`official-docs/mcp.md:156`, default `180` per tool call, `300` to connect) — `official-docs/mcp.md:141-156`; "Antigravity waits up to 5 minutes for an MCP server to connect and list its tools, and up to 3 minutes for each tool call." — `official-docs/mcp.md:160`; "`env` | `object` | `{}` | Key-value map of environment variables" — `official-docs/mcp.md:148`; "`headers` | `object` | `{}` | Custom HTTP request headers" — `official-docs/mcp.md:150`.
- **Clone:** Closed in `fix/c10` (deliverable §26). Object-shaped `env` and `headers` and all documented server properties are preserved and supported.
- **Verdict:** closed.
- **Severity:** `enum-member` + `exact-key`.

### C-11 — there is no hooks runtime at all — **Closed**

- **Doc claim:** "**Global level**: `~/.gemini/config/hooks.json`. Hooks in `~/.gemini/antigravity-cli/settings.json` aren't loaded." — `official-docs/hooks.md:27`; "**Workspace level**: `.agents/hooks.json` at your project root." — `official-docs/hooks.md:26`; "Each item in the `hooks` array supports the following fields: `type` … `command` … `timeout`" — `official-docs/hooks.md:212-218`.
- **Clone:** Closed in `fix/c11-c14` (commit `81c2f360`). Implemented documented hooks runtime in `internal/core/policy/hooks`, supporting global (`~/.gemini/config/hooks.json`) and workspace (`.agents/hooks.json`) locations, running 6 documented lifecycle events (`SessionStart`, `PreToolUse`, `PostToolUse`, `PreInvocation`, `PostInvocation`, `Stop`).
- **Verdict:** closed.
- **Severity:** `missing-feature`.

### C-12 — the `/hooks` panel omits `SessionStart` — **Closed**

- **Doc claim:** "| `SessionStart` | array | Handlers that run once when a new conversation session starts, before the first model invocation. |" — `official-docs/hooks.md:108`; "| `SessionStart` | Fires once when a new conversation session starts, before the first model invocation. | N/A (matcher ignored) |" — `official-docs/hooks.md:121`.
- **Clone:** Closed in `fix/c11-c14` (commit `81c2f360`). `SessionStart` added to `internal/ui/views/hookspanel/panel.go` as documented.
- **Verdict:** closed.
- **Severity:** `enum-member`.

### C-13 — the `PreToolUse` decision enum is absent — **Closed**

- **Doc claim:** the output `decision` field may be `"allow"`, `"auto_approve"`, `"deny"`, `"ask"`, `"force_ask"`, `"deny_unless_prior_grant"` — `official-docs/hooks.md:308-314`.
- **Clone:** Closed in `fix/c11-c14` (commit `81c2f360`). Implemented all 6 hook decisions in `internal/core/policy/hooks/decision.go` and mapped onto `permissions.Verdict` in `internal/core/policy/permissions/policy.go`.
- **Verdict:** closed.
- **Severity:** `missing-feature`.

### C-14 — the legacy hooks reader uses an XDG path the docs do not name — **Closed**

- **Doc claim:** "**Global level**: `~/.gemini/config/hooks.json`." — `official-docs/hooks.md:27`.
- **Clone:** Closed in `fix/c11-c14` (commit `81c2f360`). Removed `XDG_CONFIG_HOME` path branch in `internal/cli/dispatch_interactive.go`; reads documented `~/.gemini/config/hooks.json`.
- **Verdict:** closed.
- **Severity:** `exact-path`.

### C-15 — modular rules (`.agents/rules/*.md` + frontmatter triggers) are not implemented — **Closed**

- **Doc claim:** "`<dir>/.agents/rules/*.md` _(and legacy `<dir>/.agent/rules/`)_" — `official-docs/rules.md:21`; "**Modular global rules** (YAML frontmatter required): `~/.gemini/config/rules/*.md`" — `official-docs/rules.md:28`; "**Every `.md` file inside `rules/` must start with YAML frontmatter** declaring a valid `trigger`." — `official-docs/rules.md:39`; "`trigger` … `model_decision`, `always_on`, `glob`, or `manual`." — `official-docs/rules.md:51-59`.
- **Clone:** Closed in `feat(rules)` (commit `83226341`). Modular rules scanner implemented in `internal/app/prompt/agyprompt/rules.go` with YAML frontmatter parser (`rules_frontmatter.go`) supporting `always_on`, `model_decision`, `glob`, and `manual` triggers across `<dir>/.agents/rules/`, `.agent/rules/`, and `~/.gemini/config/rules/`.
- **Verdict:** closed.
- **Severity:** `missing-feature`.

### C-16 — the global and plugin rule roots are different directories — **Closed**

- **Doc claim:** "**Global rules**: `~/.gemini/AGENTS.md`, `~/.gemini/GEMINI.md`, `~/.gemini/config/rules/*.md`, or `~/.gemini/antigravity-cli/rules/*.md`." — `official-docs/rules.md:169`; "**Plugin rules**: Antigravity automatically activates rules packaged inside installed plugins under `~/.gemini/antigravity-cli/plugins/<plugin_name>/rules/`." — `official-docs/rules.md:170`.
- **Clone:** Closed in `feat(rules)` (commit `83226341`). Global rules include `~/.gemini/antigravity-cli/rules` and plugin rules scan both `~/.gemini/config/plugins` and `~/.gemini/antigravity-cli/plugins`.
- **Verdict:** closed.
- **Severity:** `exact-path`.

### C-17 — `.agents/rules.json` and the `@[label](path)` include syntax are not implemented — **Closed**

- **Doc claim:** "**`@[label](path)`**: Inlines the target file's contents directly into the rule before prompt evaluation and size-limit checks." — `official-docs/rules.md:106`; "To share rules stored outside `.agents/rules/` or to include nested subdirectories of rules while preserving their YAML frontmatter and triggers, create `.agents/rules.json`" with `inherits` and `entries` — `official-docs/rules.md:120-131`; "Antigravity ignores files nested in subdirectories … unless you explicitly register them in `.agents/rules.json`." — `official-docs/rules.md:32`.
- **Clone:** Closed in `feat(rules)` (commit `83226341`). Implemented `rules_manifest.go` (reading `.agents/rules.json` manifests with inherits/entries) and `rules_include.go` (expanding `@[label](path)` includes before truncation).
- **Verdict:** closed.
- **Severity:** `missing-feature`.

### C-18 — the aggregate rules budget is 30,000 bytes, not 20,000 tokens — **Closed**

- **Doc claim:** "**20,000-token aggregate rules budget**: All active global and `always_on` rules share a 20,000-token budget separate from the customization budget (plugins such as skills and MCP). If the total exceeds 20,000 tokens, Antigravity automatically demotes the largest rule files from inline text to lightweight pointers (`- <path>: <description>`)." — `official-docs/rules.md:139`.
- **Clone:** Closed in `feat(rules)` (commit `83226341`). Aggregate rules budget is 20,000 tokens (`userRulesTotalBudget = 20000`), calculated via `len(content)/4` with demotion fallback.
- **Verdict:** closed.
- **Severity:** `default-value`.

### C-19 — the skills roots differ, and the legacy `.agent/skills` tree is not read — **Closed**

- **Doc claim:** "| `<workspace-root>/.agents/skills/<skill-folder>/` | Workspace-specific | | `~/.gemini/antigravity-cli/skills/<skill-folder>/` | Global (all workspaces) | | `~/.gemini/antigravity-cli/plugins/<name>/skills/` | Plugin-provided skills |" — `official-docs/skills.md:131-133`; "Antigravity defaults to `.agents/skills`, but still maintains backward compatibility for `.agent/skills`." — `official-docs/skills.md:19`.
- **Clone:** Closed. Added `~/.gemini/antigravity-cli/skills` support to skills discovery alongside `~/.gemini/skills`, and added `<workspace>/.agent/skills` backward-compatibility probe in `internal/pigrt/skills.go` alongside `<workspace>/.agents/skills`.
- **Verdict:** closed.
- **Severity:** `exact-path` + `missing-feature`.

### C-20 — the doc contradicts itself about where the CLI stages a plugin; the clone follows the "manual installation" path — **Doc-Wrong**

- **Doc claim** (two contradictory statements): "When installed, the CLI stages plugin assets in your global configuration directory: `~/.gemini/antigravity-cli/plugins/<plugin_name>/`" — `official-docs/plugins.md:264-268`; versus "**Global level**: Place your plugin folder in `~/.gemini/config/plugins/`." — `official-docs/plugins.md:141` (and the same path in the JSON examples at `:220`, `:230`, `:240`).
- **Clone:** `internal/cli/subcommand_plugin.go:339` `dst := filepath.Join(legacyPluginsDir(), name)` stages into the `~/.gemini/config/plugins/` tree.
- **Verdict:** the clone matches `official-docs/plugins.md:141` and diverges from `official-docs/plugins.md:267`. **The doc is internally inconsistent**, and the same split reappears in the rules doc (C-16), which puts *plugin rules* under `~/.gemini/antigravity-cli/plugins/` while putting *plugins themselves* under `~/.gemini/config/plugins/`. The clone's own binary evidence (`internal/core/confhome/confhome.go:44-51`, "the tree the reference keeps plugin installation state in") supports `~/.gemini/config/plugins/`.
- **Severity:** `doc-wrong` (the doc, not the clone, is the defective side — but the reference binary should settle it before the doc is amended).
- **Minimal fix:** no clone change. The upstream doc should drop the `~/.gemini/antigravity-cli/plugins/<plugin_name>/` line at `official-docs/plugins.md:267` and use `~/.gemini/config/plugins/<plugin_name>/` everywhere, including `official-docs/rules.md:170`.

### C-21 — `agy plugin install <name>[@marketplace]` is not implemented — **Closed**

- **Doc claim:** "**Install from the official marketplace**: Specify `<plugin-name>@antigravity-plugins-official` … or pass a bare `<plugin-name>`, which defaults to `<plugin-name>@antigravity-plugins-official`" — `official-docs/marketplace.md:149-153`; "`agy plugin install https://github.com/<owner>/<repo>`" — `official-docs/marketplace.md:159`; "`/plugin install <plugin-name>@antigravity-plugins-official`" — `official-docs/marketplace.md:123`.
- **Clone:** Closed in `fix/c26` (deliverable §30). `agy plugin install` supports marketplace targets `<name>[@<marketplace>]` via cached marketplace feeds, falling back to `antigravity-plugins-official`.
- **Verdict:** closed.
- **Severity:** `missing-feature`.

### C-22 — three documented models are absent and `Gemini 4 Argon` is unselectable — **Closed**

- **Doc claim:** the reasoning-model table lists "Gemini 4 Argon" (`:21`), "Gemini 3.8 Flash" (`:22`), "Gemini 3.7 Flash" (`:23`), "Gemini 3.6 Flash" (`:24`), "Gemini 3.1 Pro" (`:25`), "Claude Sonnet 5.5 (thinking)" (`:26`), "Claude Opus 5.5 (thinking)" (`:27`), "Claude Sonnet 4.6 (thinking)" (`:28`), "Claude Opus 4.6 (thinking)" (`:29`), "GPT-OSS-120b" (`:30`) — `official-docs/models.md:19-30`.
- **Clone:** Closed in `fix/c22` (deliverable §22). Gemini 3.7 Flash, 3.6 Flash, and 3.1 Pro families added with recovered IDs.
- **Verdict:** closed.
- **Severity:** `enum-member`.

### C-23 — `default-cli-project` is not the default session project — **Closed**

- **Doc claim:** "When starting the CLI without any project flags, all conversations in the session run in the `default-cli-project`" — `official-docs/projects.md:62`.
- **Clone:** Closed in `fix/c23` (deliverable §24). Default project is `default-cli-project` when no `--project` flag is provided.
- **Verdict:** closed.
- **Severity:** `behaviour`.

### C-24 — the keybindings file is at `~/.gemini/config/keybindings.json`, the doc says `~/.gemini/antigravity-cli/keybindings.json` — **Closed**

- **Doc claim:** "**Keyboard shortcuts**: `~/.gemini/antigravity-cli/keybindings.json`. `/keybindings` opens the file in your editor." — `official-docs/settings.md:116`; "```text\n~/.gemini/antigravity-cli/keybindings.json\n```" — `official-docs/settings.md:344-347`.
- **Clone:** Closed in tranche `9db24608`. Keybindings reader (`internal/ui/editor/keybind/keybind.go`) and editor writer (`internal/cliapp/slash_helpers.go`) are unified on `~/.gemini/antigravity-cli/keybindings.json` matching the official documentation.
- **Verdict:** closed.
- **Severity:** `exact-path`.

### C-25 — the sandbox is a path allow-list, not an OS-level container — **Open (存疑)**

- **Doc claim:** "| **Linux** | Namespaces | Kernel namespaces isolate the filesystem, hide host processes, and cut off networking. | | **macOS** | `sandbox-exec` | Seatbelt profiles (SBPL) restrict filesystem access and socket connections. |" — `official-docs/sandbox.md:19-20`; "**Escape hatches (`unsandboxed`)**: To let specific commands run outside the sandbox without disabling it entirely, add an `unsandboxed` allow rule" — `official-docs/sandbox.md:113-117`; "**Windows** | Process security environments | Commands run in an AppContainer … Requires Windows 11 24H2 or later" — `official-docs/sandbox.md:132`.
- **Clone:** `internal/core/policy/sandbox/sandbox.go` implements a path allow-list (`Resolve` `:80`, `AllowedPaths` `:104`, `Allowed` `:131`, `CheckCommand` `:197`) and `internal/core/policy/permissions/sandbox.go:45` generates a macOS Seatbelt profile at `~/.gemini/config/sandbox.sb`. **No `sandbox-exec` process is ever spawned**, there is no Linux namespace or Windows AppContainer path, and the `unsandboxed` permission action does not exist (see C-4).
- **Verdict:** the sandbox is a *soft* gate: a command whose paths are outside the allow-list is surfaced as a prompt, not confined by the kernel. The doc's security guarantee is not reproduced.
- **Severity:** `missing-feature`.
- **Minimal fix:** in `internal/core/policy/sandbox/sandbox.go`, add a runner — `func (s Sandbox) Exec(ctx context.Context, argv []string) (stdout, stderr []byte, err error)` that on darwin writes the generated SBPL profile and invokes `sandbox-exec -f <profile> <argv…>` (the profile generator at `internal/core/policy/permissions/sandbox.go:70` already produces the file), and on Linux uses `unshare(CLONE_NEWUSER|CLONE_NEWNS|CLONE_NEWNET)`; and add the `unsandboxed` action to the permission grammar (C-4) as the documented escape hatch — **subject to the doc-vs-binary conflict noted in `## limitations`**.

### C-26 — `settings.json` is not normalised at start-up — **Closed**

- **Doc claim:** "**The file is normalized at startup.** When the CLI starts with a valid file, it rewrites the file in its own format, which also removes keys that are set to their default values." — `official-docs/settings.md:123`.
- **Clone:** Closed in `fix/c26` (deliverable §30). `NormalizeOnStartup` strips redundant default values while preserving unknown fields.
- **Verdict:** closed.
- **Severity:** `behaviour`.

### C-27 — the `permissions` panel accepts only the `command` verb, so the doc's six other actions cannot be granted — **Closed**

- **Doc claim:** "For the supported actions and how targets match, refer to [Agent permissions](/docs/permissions?tab=cli)." — `official-docs/settings.md:282`; the seven actions and their target formats — `official-docs/permissions.md:342-348`; "**`read_file`**: Matches absolute paths or paths relative to project workspace roots." — `official-docs/permissions.md:352`.
- **Clone:** Closed in tranche `4bf2e6d6` (deliverable §18) and unified in `panel_rules.go` (`ValidGrantAction` maps across the 6 live actions: `command`, `read_file`, `write_file`, `read_url`, `execute_url`, `mcp`, rejecting retired `unsandboxed`).
- **Verdict:** closed.
- **Severity:** `missing-feature`.

---

## Deliberate clone differences

These are **not** bugs. They are the clone's own choices, listed in one place.

1. **The clone's own configuration home `~/.openagy`** (`$OPENAGY_CONFIG_HOME`) — `configdir/configdir.go:38-63`. This is an **addition, not a rename**: the reference's own trees are **preserved** exactly. `~/.gemini/antigravity-cli` remains the CLI's own state tree (`settings.json`, `mcp.json`, the projects cache — `internal/core/confhome/confhome.go:34-41`), and `~/.gemini/config` remains the sibling tree that holds the plugin installation state and the project store (`internal/core/confhome/confhome.go:44-51`). `~/.openagy` holds **only** the clone's own additions: `providers.json` (`internal/features/models/providers.go`), `accounts.json` (`internal/features/accounts/core/store.go`), `config.json` (the named-endpoint overrides, `endpoints/config.go`), the proxy's capture directory and `wrapper.log` (`internal/infra/agyproxy/`). A rename would have been a far larger divergence; it did not happen.
2. **The `OPENAGY_*` environment variables are clone-named** — `OPENAGY_CONFIG_HOME` (`configdir/configdir.go:38`), `OPENAGY_PERMISSION_POLICY` (`internal/core/policy/permissions/policy.go:290`), `OPENAGY_DISABLE_SANDBOX` (`internal/app/session/setup/setup.go:92`). The reference's own `AGY_CLI_DISABLE_MCP` (`internal/features/auth/compat_mcp.go:71`) **is** implemented, because that one belongs to the reference.
3. **The permissions scope label is branded** — `internal/core/policy/permissions/panel_rules.go:23` renders the middle scope as `"Shared with Pigasus"` where the doc says **Shared** (`official-docs/settings.md:294`). The *scope model* matches (see the Match list); only the label differs. Related: `internal/core/branding/brand.go:71`, `internal/ui/views/permissionspanel/panel.go:59`.
4. **The clone's own MCP switches** — `disableAllMCP` (`mcp/agyconfig.go:111`), the per-server `alwaysLoad` list (`:137`) and the `type` transport hint (`:121`) are the reference's own keys, but the reference's docs in this slice do not name them; the clone surfaces them because its `mcp.json` reader needs them.
5. **The clone's own sandbox profile path** — `~/.gemini/config/sandbox.sb` (`internal/core/policy/permissions/sandbox.go:45`) is a clone artefact; the doc never names a profile file.
6. **The `/hooks` panel persists nothing** — `internal/ui/views/hookspanel/panel.go` is a configuration *surface* with no store behind it. The panel exists to mirror the reference's vocabulary; the missing runtime is C-11.

---

## Flags, ambiguities and confusions

These are the places where the **doc and the clone's own recovered evidence disagree**. They must be **re-checked against the reference binary before any fix** — the doc is not automatically the authority.

1. **`altScreenMode: "default"` (C-3).** The doc describes a hybrid mode — conversation inline, full-screen views in the alternate screen (`official-docs/settings.md:225`, `:189`) — and makes it the default. The clone has no such mode, and its own comment records a raw pty capture of **agy 1.4.7** in which an absent `altScreenMode` emits `?1049h` (the alternate screen) and every value other than `never` and `always` — including `default` — "is rejected by the reference's settings validation and falls back to the default" (`internal/ui/views/configpanel/configpanel.go:186-204`). The docs may describe a behaviour introduced after 1.4.7. **Capture the reference again before changing anything.** The clone's own print-mode default table still says `"default"` (`internal/printmode/print_slash_config.go:72`), so the clone is internally inconsistent here too.
2. **The plugin-tree contradiction (C-20, with C-16 and C-19).** `official-docs/plugins.md:267` says the CLI stages plugins at `~/.gemini/antigravity-cli/plugins/<plugin_name>/`; `official-docs/plugins.md:141` (and the JSON examples at `:220`, `:230`, `:240`) says `~/.gemini/config/plugins/`. `official-docs/rules.md:170` puts *plugin rules* under `~/.gemini/antigravity-cli/plugins/<plugin_name>/rules/`, while `official-docs/skills.md:133` puts *plugin skills* under `~/.gemini/antigravity-cli/plugins/<name>/skills/`. The clone uses `~/.gemini/config/plugins/` throughout (`internal/cli/subcommand_plugin.go:339`, `internal/app/prompt/agyprompt/rules.go:102`, `internal/features/tools/skills/discovery.go:91`). Either the doc has two stale paths, or the reference really has two trees. **Do not "correct" the clone until the reference's own install path is observed.**
3. **The keybindings path (C-24, and Slice B's B-17).** The doc says `~/.gemini/antigravity-cli/keybindings.json` (`official-docs/settings.md:116`, `:346`). The clone's **loader** reads `~/.gemini/config/keybindings.json` with 【反编译】 provenance — the recovered `(*ServerBackend).KeybindingsFilePath` (`internal/ui/editor/keybind/keybind.go:66-85`) — while the clone's **writer** (`/keybindings`) targets the doc's path (`internal/cliapp/slash_helpers.go:48-53`). So `/keybindings` opens a file the TUI never reads. This is a **clone-internal contradiction**, not a doc error: whichever path is right, the clone must read and write the same file.
4. **The skills root (C-19, and Slice B's B-26).** The doc says `~/.gemini/antigravity-cli/skills/` (`official-docs/skills.md:132`); the clone uses `~/.gemini/skills`, which Slice B recorded as backed by a live 1.4.6 request listing `~/.gemini/skills` paths. Re-check before changing.
5. **`unsandboxed` is documented but rejected by the reference.** `official-docs/permissions.md:347` and `:357` list `unsandboxed` as a supported action, and the reference binary's own grant regex admits it (`^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$`) — but the same binary carries the rejection string `unsandboxed is no longer a valid rule, use command() instead` (`out/local-macos-1.4.11/strings.txt`). The doc's `unsandboxed` escape hatch therefore appears to be **retired in the current reference**. This bears on C-4, C-25 and C-27: implement the six live actions and treat `unsandboxed` as a parse-time rejection carrying the reference's own message, rather than as a working action.
6. **IDE-vs-CLI confusion.** `internal/lsserver/layout.go:132` uses `<config_dir>/mcp_config.json`, and `internal/lsserver/*` is the **IDE language-server** surface — it is not evidence about the CLI's MCP paths (C-8, C-9).
7. **The Antigravity App sections were not treated as CLI claims.** `official-docs/permissions.md:11-189` (the App), `official-docs/rules.md:145-161` (the App) and the IDE sections of `hooks.md` and `plugins.md` describe other surfaces. They were read only for internal consistency.
8. **Not assessed here** (would need the live reference binary): the reference's own preference between `mcp.json` and `mcp_config.json` (C-8); the exact MCP timeout enforcement (C-10); whether the reference's `/hooks` panel persists its edits (C-11); and the reference's own start-up normalisation of `settings.json` (C-26).

---

## Doc-only

Documented features with **no implementation in the clone at all**.

1. MCP `cwd` — `official-docs/mcp.md:149`.
2. MCP `authProviderType` — `official-docs/mcp.md:151`.
3. MCP `oauth` — `official-docs/mcp.md:152`.
4. MCP `enabledTools` — `official-docs/mcp.md:154`.
5. MCP `disabledTools` — `official-docs/mcp.md:155`.
6. MCP `timeoutSeconds` (and the 180 s / 300 s defaults) — `official-docs/mcp.md:156`, `:160`.
7. Rules frontmatter (`trigger`, `description`, `globs`/`glob`) and the activation modes `always_on` / `model_decision` / `glob` / `manual` — `official-docs/rules.md:39`, `:51-60`.
8. `.agents/rules.json` (`inherits`, `entries`, `exclude`, `include_only`) — `official-docs/rules.md:120-131`.
9. The `@[label](path)` include syntax — `official-docs/rules.md:106`.
10. The legacy `.agent/skills` tree — `official-docs/skills.md:19`.
11. `${PLUGIN_ROOT}` / `${PLUGIN_DATA}` — `official-docs/plugins.md:92-93`.
12. The Gemini 3.7 Flash / 3.6 Flash / 3.1 Pro model ids — `official-docs/models.md:23-25`.

## Clone-only

Features present in the clone with **no counterpart in these 12 docs** (each is either an addition or a deliberate clone-only surface).

1. `contextWindow` settings key — `internal/printmode/print_slash_config.go:20`.
2. `seasonalThemesOff` / `seasonalThemesOptIn` settings keys — `internal/printmode/print_slash_config.go:28-29`.
3. `~/.gemini/config/sandbox.sb` — a writable macOS Seatbelt profile (`internal/core/policy/permissions/sandbox.go:45`).
4. The clone's `~/.openagy` home with `providers.json` / `accounts.json` / `config.json` / `captures/` / `wrapper.log` — `configdir/configdir.go`.
5. The remote-control stores `permissions.json` and `plugins.json` under the CLI home — `internal/remotecontrol/daemon_config.go:181`, `:208`.
6. The `localharness` hook bus (`PreSideQuestion` / `PostSideQuestion`) — `internal/features/plugins/hooks/hooks.go:52-57`; a different hook family from the documented six.
7. The plugin *validator*'s event table — `internal/cli/plugin_validate_hooks.go:14` knows the six documented events, but only to warn about unknown ones.
8. The clone's own `--settings` flag overlay — `internal/core/settings/settings.go:99` `ParseAndValidateFlag` / `:134` `SetSessionOverlay`.
9. `internal/core/policy/permissions` accepts the rule object form `{"tool":…,"pattern":…}` in addition to the string form — `internal/core/policy/permissions/settings.go:84-90`; the doc documents only the `action(target)` string form.

## Numbers

| Metric | Count |
|---|---|
| Docs read | 12 (`settings`, `permissions`, `hooks`, `mcp`, `agent-settings`, `rules`, `sandbox`, `plugins`, `marketplace`, `skills`, `models`, `projects` — 3,158 lines) |
| Claims checked | 65 |
| Matches | 26 |
| Divergences | 27 (C-1 … C-27) |
| Doc-only | 12 |
| Clone-only | 9 |
| Deliberate clone differences | 6 |

Severity split of the divergences: **behaviour** 8 (C-2, C-4, C-5, C-7, C-8, C-23, C-26, C-27) · **missing-feature** 7 (C-9, C-11, C-13, C-15, C-17, C-21, C-25) · **enum-member** 5 (C-1, C-3, C-10, C-12, C-22) · **exact-path** 4 (C-14, C-16, C-19, C-24) · **default-value** 2 (C-6, C-18) · **doc-wrong** 1 (C-20). Secondary severities: `exact-key` (C-10), `default-value` (C-1), `behaviour` (C-6), `exact-path` (C-8, C-19), `missing-feature` (C-19, C-27).

User-reachable divergences (a user action reaches the wrong behaviour): **16 of 27**.

## limitations

- The slice read **12 docs**; **65 claims** were checked, **26** matched and **27** diverged. **16 of the 27** divergences are user-reachable.
- The permission cluster's reference-binary evidence came from `out/local-macos-1.4.11/strings.txt`: the grant regex `^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$`, the format hint `Format: action(target)`, `PermissionGrantsConfig.GetAllow` / `GetDeny` / `GetAsk` (`strings.txt:280825-280827`), the rejection `unsandboxed is no longer a valid rule, use command() instead`, and `CLI settings initialized: permissions=%+v, toolPermission=%s`. These strings establish the reference's **grammar** and **list set**; they do not, on their own, establish the reference's **precedence order** or its **default verdict** — those remain read from the docs (C-5, C-6) and are not independently confirmed here.
- The three doc-vs-binary conflicts — C-3 (`altScreenMode: "default"`), C-20/C-19 (the plugin-tree contradiction) and C-24/B-17 (the keybindings path) — are **flagged, not fixed**. The clone must not be changed to match the doc until the reference binary is re-checked.
- No finding rests on running the reference binary: the survey was read-only and every claim rests on source reading in the clone plus the clone's recorded 【一手】/【反编译】 evidence and the strings quoted above.
- `internal/lsserver/*` is the **IDE language-server** surface and is **not evidence** about the CLI's paths (in particular the `mcp_config.json` question in C-8/C-9).
- The Antigravity App and IDE sections of the slice docs were read but **not** treated as CLI claims; they were used only to detect doc-internal inconsistency (C-20).
- The model ids in C-22 were **not** guessed. The clone's `Gemini 4 Argon` row is deliberately inert (`internal/ui/views/modelpicker/modelpicker.go:64`) because the slug has not been recovered; adding the three missing Gemini ids requires the binary, not the doc table.
- The MCP timeout defaults (C-10), the reference's own preference between `mcp.json` and `mcp_config.json` (C-8), the reference's start-up normalisation of `settings.json` (C-26) and whether the reference's `/hooks` panel persists its edits (C-11) were **not** settled here; they need the live reference binary.
