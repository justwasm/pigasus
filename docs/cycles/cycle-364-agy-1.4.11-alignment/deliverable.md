# Cycle 364 deliverable: the clone against agy 1.4.11

**Cycle:** `cycle-364-agy-1.4.11-alignment`
**Subject:** the vendor's 1.4.11 release notes carry **five** bullets. This
cycle maps every one of them onto the clone: three are implemented, one is
implemented in its documentation half only (its runtime is recorded as
open, with the reason), and one is not applicable to the clone's
architecture.
**Evidence:** 【一手】 first-hand against the 1.4.11 binary — its own string
table, and a black-box probe of `agy plugin import` against a synthetic
`$HOME` · 【strings】 string-table hits only · 【推测】 hypothesis

---

## 0. The short answer

| # | the upstream bullet | the clone |
|---|---|---|
| 1 | a billing-model choice after the Google Cloud sign-in | **not applicable** — the clone has no billing-model screen to choose on (§2) |
| 2 | `PreInvocation` hooks gain `terminationBehavior` and an injected `errorMessage.userMessage` | **doc only** — the clone ships the reference's 1.4.11 `hooks.md` byte for byte; the runtime is open, because the clone has no hook-command host (§3) |
| 3 | `finished_reason` in the `--output-format json` / `stream-json` result | **done** — `internal/printmode` (§4) |
| 4 | the `/plugin` Discover tab's shelves: six plugins, a `⋯ N more` row, `Ctrl+E`, `[`/`]` | **done** — `internal/pluginsui`, `internal/cliapp` (§5) |
| 5 | `agy plugin import claude` reads Claude Code's installed plugins and keeps their MCP servers | **done** — `internal/features/plugins/import` (§6) |

Three of five are done, one is doc-only, one is not applicable.

---

## 1. Where the 1.4.11 notes came from

The notes are the vendor's own words, taken from
<https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md>
and recorded **verbatim** in
[`internal/cli/testdata/changelog_upstream_1.4.11.md`](../../../internal/cli/testdata/changelog_upstream_1.4.11.md),
the same way the 1.4.8, 1.4.9 and 1.4.10 notes were. The *served* changelog
feed — the one `agy changelog` prints — lags the vendor's repository, so the
clone's changelog still prints exactly what the reference prints.

The 1.4.11 binary **is** on this machine (`~/.local/bin/agy`, sha256
`09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd`,
captured 2026-10-30). Its string table is in
[`out/local-macos-1.4.11/strings.txt`](../../../out/local-macos-1.4.11/strings.txt),
and the reference's 1.4.11 `hooks.md` — the document the clone ships — was
captured to
[`out/local-macos-1.4.11/hooks-doc.txt`](../../../out/local-macos-1.4.11/hooks-doc.txt).

The version pin and the drift lock move with the binary — that is the RE
refresh's own work, in `internal/cli/version.go` and
`internal/referencedrift/reference.lock.json`, not this cycle's.

---

## 2. Item 1 — the billing-model choice (not applicable)

> "Added a billing model choice after you sign in with Google Cloud, so you
> can pick either a Gemini subscription or Pay As You Go."

【strings】 the 1.4.11 binary carries the whole screen: the model symbol
`model.GcpSetupModel.renderBillingModelSelectionMenu` (next to
`renderStepMenu`), the prompt `Select billing model:` and
`Select subscription:`, the choice's own text `Pay As You Go – Pay only for
the tokens you use, billed through Gemini platform.`, the confirmation
`User selected Pay As You Go billing model`, and the auth-state transition
`[Auth State Transition] Step %v -> Step %v: Selecting billing model`.

The clone has **no such screen**: the strings `Select billing model`,
`Select subscription`, `Pay As You Go` and `GcpSetup` do not occur anywhere
in the clone's tree, and there is no model-setup step to add a choice to.
The clone only *reads* the tier the choice would set —
`codeassistclient.IsBusinessPayGo` → `TierPayGo` — which is the
consequence of the choice, not the choice itself.

So the failure the note describes cannot arise in the clone: there is no
menu to render the new option in. The clone's Google Cloud sign-in asks for
a project and a tier and moves on. This is the same shape of conclusion as
cycle 363's item 6 (the browser snapshot) and cycle 362's 1.4.9 sibling
("the clone does not speak Chrome DevTools MCP"): the *precondition* is
missing.

---

## 3. Item 2 — `PreInvocation` termination (doc done, runtime open)

> "Added `terminationBehavior` to `PreInvocation` hooks: returning
> `"terminate"` stops the turn before the model is called, for example to
> block a prompt that contains sensitive data, and hooks can now inject an
> `errorMessage` whose `userMessage` is shown to you without being sent to
> the model, which also makes a `-p` run exit with a non-zero code."

**The document half is done.** The clone ships the reference's own
`hooks.md` inside its builtin skill bundle
(`internal/features/tools/skills/skills/agy-customizations/docs/hooks.md`).
That file was replaced with the 1.4.11 text — byte for byte, captured from
the binary into `out/local-macos-1.4.11/hooks-doc.txt`. The new sections
are there: `terminationBehavior` (`"terminate"` stops the turn before the
model is called; `"force_continue"` continues it), the injected
`errorMessage` with its `userMessage`, and the rule that the message is
shown to the user and **not** sent to the model.

**The runtime half is open, and it is open for a structural reason.** The
clone's hook surface is `internal/features/plugins/hooks`: a typed,
**in-process** bus. Its own package comment says it plainly — "we do not
actually invoke shell commands here. The clone has no subprocess-hook host;
the dispatch is a typed in-process bus." Only two of the five hook events
are fired by production code (`PreSideQuestion` and `PostSideQuestion`,
from `/btw`).

That has three consequences, all of them the same consequence:

- A `PreInvocation` hook **command** is never run, so its JSON verdict
  never arrives and `terminationBehavior` never has anything to act on.
- There is no place to inject an `errorMessage`, so no `userMessage` is
  ever shown to the user and none is ever withheld from the model.
- The print path has no hook host either, so the note's "which also makes
  a `-p` run exit with a non-zero code" has no trigger: a `-p` run's exit
  code comes from the turn's own outcome (see §4), not from a hook.

Implementing this properly means building the subprocess hook host the
reference has — spawn the hook's command, write the payload on stdin, read
the verdict on stdout, honour the timeout — and then wiring `PreInvocation`
into the turn loop's pre-model-call point, plus a channel from the hook's
verdict to the print path's exit status. That is a cycle of its own; it is
recorded here as **open**, not as done.

---

## 4. Item 3 — `finished_reason`

> "Added a `finished_reason` field to the result of `--output-format json`
> and `stream-json` runs that says why a turn ended early, `print_timeout`
> when `--print-timeout` expired and the response is partial,
> `interrupted` when the run was canceled such as with `Ctrl+C`, or `error`
> for any other failure, so scripts can tell a cut-off answer from a
> complete one; the field is omitted when the turn finishes normally."

【一手】 the field's place in the wire was captured from the 1.4.11 binary
on its error path (a `-p` run with no credentials):

```json
{"conversation_id":"","status":"ERROR","response":"",
 "error":"authentication failed or timed out","duration_seconds":0,
 "num_turns":0,"usage":{"input_tokens":0,"output_tokens":0,
 "thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0},
 "finished_reason":"error"}
```

The field is **last** in the envelope, and it is omitted entirely on a
normal finish.

The clone's implementation:

- `internal/printmode/print_format.go` — `jsonEnvelope.FinishedReason`
  (`json:"finished_reason,omitempty"`), placed last in the struct, and the
  three constants `print_timeout`, `interrupted`, `error`. The envelope is
  shared by `--output-format json` and by the `result` member of
  `--output-format stream-json`, so both formats carry the field.
- `internal/printmode/print_interrupt.go` — the **SIGINT guard**. For the
  field to exist at all the signal has to be *caught*: an uncaught SIGINT
  kills the process, and a killed process writes no envelope. The guard
  cancels the turn's context on the first `Ctrl+C`; the handler is stopped
  and `signal.Reset(os.Interrupt)` restores the default disposition
  **before** the cancel, so a second `Ctrl+C` is still fatal — the graceful
  path must not be a trap.
- `internal/printmode/main_print.go` — the wiring: the guard is armed
  next to the turn's context, and after the turn the reason is chosen
  (`print_timeout` when the deadline fired, `interrupted` when the guard
  saw the signal, `error` for any other failure).

The three reasons are the note's own words; the *ordering* between them is
the clone's, and the note does not rank them.

`internal/printmode/print_finished_reason_test.go` pins the envelope
shape, the three values, the omission on a normal finish and the guard's
behaviour.

---

## 5. Item 4 — the Discover tab's shelves

> "Improved the `/plugin` Discover tab for long shelves: each shelf now
> shows its first 6 plugins followed by a `⋯ N more — enter to show` row,
> `Ctrl+E` expands or collapses the shelf under the cursor, and `[` and `]`
> jump to the previous or next shelf."

【strings】 the 1.4.11 binary carries the row's format (`%d more — enter to
show`, next to the `⋯ ` glyph) and the three hints the panel's footer shows
for the new keys: `Expand Shelf`, `Collapse Shelf` and `Prev/Next Shelf`.

The clone's implementation:

- `internal/pluginsui/sections.go` — `ShelfLimit` (6), `ShelfRows` (a
  shelf's rows: the first six plugins plus the "more" row, or every plugin
  when the shelf is expanded) and `ShelfMoreText` (`⋯ N more — enter to
  show`).
- `internal/pluginsui/render.go` — `ShelfMoreLine` and the footer legend,
  which names `Expand Shelf` or `Collapse Shelf` depending on the shelf
  under the cursor, and `Prev/Next Shelf` for the jump.
- `internal/cliapp/slash_plugin_shelf.go` — the shelf layout, the
  expand/collapse toggle and the shelf jump.
- `internal/cliapp/slash_plugin_local_install.go`,
  `slash_plugin_search.go`, `slash_panel_plugin.go` — the rows carry their
  shelf, whether they are the "more" row, and how many plugins are hidden
  behind it; `Enter` on the "more" row shows the shelf.
- `internal/cliapp/tui_keymap_plugin.go` — `Ctrl+E` (matched as
  `key.Code == 'e' && key.Mod.Contains(tea.ModCtrl)`, because Bubble Tea
  v2 v2.1.0 has no `tea.KeyCtrlE`), plus `[` and `]`, ahead of the
  typed-text fallback.

**What the clone's "shelf" is.** The reference's shelf is a *marketplace*
shelf — a curated group of plugins the marketplace feed itself carries.
The feed the clone reads (`internal/plugins`) has no shelf of its own, so
the clone's shelf is one of the Discover **sections** the 1.4.9 cycle
grouped the feed into (`pluginsui.GroupDiscoverSections`). A feed that
groups into at most one section is shown as one unnamed shelf, exactly as
the panel has always shown it. Everything else — the six-plugin limit, the
"more" row, the three keys, the legend — follows the reference.

---

## 6. Item 5 — `agy plugin import claude`

> "Fixed `agy plugin import claude` printing `No claude extensions found.`
> for plugins installed from a Claude Code marketplace: it now reads Claude
> Code's list of installed plugins, skipping plugins disabled in Claude
> Code's settings and plugins installed for a single project only, and
> imported plugins now keep their remote MCP servers (`"type": "http"`
> with `url` and `headers`) and servers from `.mcp.json` files without an
> `mcpServers` wrapper, expand `${CLAUDE_PLUGIN_ROOT}` to the installed
> plugin's directory, and list servers that can't be converted, such as
> ones using the legacy SSE transport, with a warning instead of reporting
> them as installed."

This bullet is the cycle's biggest piece of work. Everything below was
measured **first-hand** against the 1.4.11 binary with a synthetic `HOME`
(`HOME=/tmp/agy1411home agy plugin import claude`), because the command
needs no network and the reference's own error messages name the files it
reads — which is the fastest way to find them.

### 6.1 The command's surface

| invocation | the reference |
|---|---|
| `agy plugin import` | the Gemini CLI importer **and then** the Claude Code importer, each with its own `Staged to` line |
| `agy plugin import gemini` | the Gemini CLI importer only |
| `agy plugin import claude` | the Claude Code importer only |
| `agy plugin import claude --force` | re-imports a plugin the manifest already lists |
| `agy plugin import <other>` | `Error: unknown import source or invalid path: <other>` on stderr, **exit 1** |

A source that finds nothing prints `No <source> extensions found.`. A
source that finds something ends with a blank line and
`Staged to <~/.gemini/config>`.

### 6.2 The report

```
  [ok]    <name>
          ✔ skills      : 2 installed (a, b)
          ✔ agents      : 1 installed (ag)
          ✔ commands    : 1 installed (converted to skills) (hello)
          ✔ mcpServers  : 1 installed (srv)
          ⚠ MCP server "srv" skipped: the legacy SSE transport isn't supported
  [skip]  <name> (already imported, use --force to re-import)
  [error] <name>: staging MCP servers: parsing <path>: mcpServers: json: cannot unmarshal string into Go value of type map[string]jsontext.Value
```

Every line but the `[error]` one goes to **stdout**; the `[error]` line is
the only one the reference writes to **stderr**. A discovery failure is
also stderr, and it exits **0** — the error is the command's whole output.

The component lines print in the order `skills`, `agents`, `commands`,
`mcpServers`; the `import_manifest.json` records the same components in a
*different* order — `skills`, `agents`, `mcpServers`, `commands`. The
difference is real and is reproduced, not tidied away (§7).

### 6.3 What is read

**Claude Code** — `~/.claude/plugins/installed_plugins.json`:

```json
{"plugins": {"<marketplace>": [ <install>, … ]}}
```

- `installPath` is the only field the location comes from; `path` is **not**
  accepted. The record's `name` is only a fallback — the manifest's name
  wins.
- `scope` decides whether the record is skipped: `project` and `local` are
  skipped, `user`, `global`, an unknown scope and an absent scope are
  imported. A skipped record prints nothing at all.
- A marketplace whose value is an **object** instead of an array parses
  but yields **no** plugins (the reference's decoder accepts both shapes;
  only the array shape imports anything).

**Claude Code** — the top-level directories under `~/.claude/plugins/`
that carry a `.claude-plugin/plugin.json` are imported as well. The
reference's order is: the marketplaces of `installed_plugins.json` in key
order, each marketplace's installs in file order, then the top-level
directories in name order.

**Gemini CLI** — `~/.gemini/extensions/<name>/gemini-extension.json`.

### 6.4 What is written

- The plugin tree is copied to `~/.gemini/config/plugins/<name>/`.
- A `plugin.json` is written at the plugin root carrying only `name`,
  `version`, `description` and `author` (the author's **name**, whether
  the source manifest wrote it as a string or as `{"name","url"}`). Every
  other field of the source manifest is dropped. The file ends with a
  newline.
- `~/.gemini/config/import_manifest.json`:

```json
{"imports":[{"name":"full","source":"claude-code","importedAt":"2026-10-30T08:26:48Z","components":["skills","agents","mcpServers","commands"]}]}
```

The `source` is `claude-code` or `gemini-cli`. `components` is `null` when
the plugin staged none. The "already imported" skip is keyed on the
**(name, source)** pair — the same name imported from the other source is
*not* a skip.

### 6.5 The component conversions

- **skills** — the plugin's `skills/*/SKILL.md` directories, copied as they
  are.
- **agents** — the plugin's `agents/*.md` files, copied as they are.
- **commands** — the plugin's `commands/*.md` files are **converted to
  skills**: `commands/<name>.md` becomes
  `skills/<plugin>-cmd-<name>/SKILL.md`, with the file's bytes unchanged
  (its frontmatter included). The report says
  `commands    : 1 installed (converted to skills) (hello)`.
- **mcpServers** — the plugin's `.mcp.json` is read, converted and written
  as the CLI's own `mcp_config.json`; Claude Code's `.mcp.json` is removed
  from the staged tree afterwards (unless the conversion failed, in which
  case the file stays and the plugin is reported as `[error]`).

The MCP conversion rules, as measured:

| the server entry | the result |
|---|---|
| `{"type":"http","url":…,"headers":{…}}` | `{"serverUrl":…,"headers":{…}}` — the headers come along, a `command` beside them is dropped |
| `{"type":"http"}` | skipped: `an http server needs a url` |
| `{"type":"sse","url":…}` | skipped: `the legacy SSE transport isn't supported` |
| `{"type":"stdio","command":…}` | `{"command":…,"args":[…],"env":{…}}` |
| `{"type":"stdio"}` | skipped: `a stdio server needs a command` |
| `{"type":"other"}` | skipped: `unsupported type "other"` |
| `{"url":…}` (no type) | `{"serverUrl":…}` |
| `{"command":…}` (no type) | `{"command":…}` |
| `{}` | skipped: `it has neither a command nor a url` |
| not a JSON object | skipped: `it isn't a JSON object` |
| `{"mcpServers":{…}}` | the wrapper is unwrapped |
| `{…}` (no wrapper) | the whole object is the server map |

`${CLAUDE_PLUGIN_ROOT}` expands to the **staged** plugin directory — the
directory the plugin now lives in, not the Claude Code directory it came
from. It is expanded in the command, in the arguments and in the
environment values.

### 6.6 The clone's implementation

`internal/features/plugins/import` (package `pluginimport`):

- `pluginimport.go` — the run loop, the report writer, the staging and the
  `import_manifest.json`.
- `discover.go` — the two discovery passes and the source manifests.
- `mcp.go` — the MCP server conversion.

`internal/cli/subcommand_plugin.go` — `runPluginImport` is the command
surface: it parses `--force` and the source names, rejects anything else
with the reference's error, and picks the two importers for the bare
command.

The reference's discovery errors carry **Go type names from the
reference's own package** (`map[string]claude.pluginInstalls`,
`[]claude.pluginInstall`). The clone's package is named differently, so
those two messages are emitted as compatibility shims rather than produced
by the clone's decoder. Every other error is the clone's own.

### 6.7 The comparison

The clone and the reference were run against **six** synthetic trees — a
plugin with every component, the seven MCP edge cases, the scope filter,
the top-level directories, a malformed `installed_plugins.json`, a
`"plugins"` member of the wrong type — and their stdout, stderr, exit
status and the resulting `~/.gemini` tree were diffed. The diffs were the
`$HOME` paths and the `importedAt` timestamps, and nothing else: same
report lines, same warnings, same `mcp_config.json`, same `plugin.json`,
same `import_manifest.json`, same `[skip]` on the second run, same
re-import under `--force`.

---

## 7. 硬约束 / hard constraints

- **`finished_reason` is the last member of the envelope** and it is
  **omitted** when the turn finishes normally. It must not appear as an
  empty string.
- **The interrupt has to be caught.** An uncaught SIGINT kills the process
  and a killed process writes no envelope, so the `interrupted` reason
  would be unreachable. The first `Ctrl+C` cancels the turn; the second is
  fatal (`signal.Reset` restores the default disposition **before** the
  cancel).
- **The shelf limit is 6**, and the "more" row's text is
  `⋯ N more — enter to show` — the reference's own glyph and wording.
- **`Ctrl+E` is matched as `key.Code == 'e' && key.Mod.Contains(tea.ModCtrl)`**,
  because Bubble Tea v2 v2.1.0 has no `tea.KeyCtrlE`.
- **The import's report lines go to stdout; only the `[error]` line goes to
  stderr.** A discovery failure also exits 0; an unknown source exits 1.
- **The import's components are recorded in two different orders** — the
  report prints `skills, agents, commands, mcpServers`, the manifest stores
  `skills, agents, mcpServers, commands`. This is the reference's own
  behaviour; do not "tidy" one of them into the other.
- **The staged `plugin.json` carries only `name`, `version`, `description`
  and `author`.** Everything else in the source manifest is dropped, and
  the file ends with a newline.
- **`${CLAUDE_PLUGIN_ROOT}` expands to the staged directory**, not to the
  Claude Code directory the plugin came from.
- **`scope` `project` and `local` are skipped silently** — no line at all,
  not a `[skip]` line.
- **The "already imported" skip is keyed on (name, source)**, not on the
  name alone, and it is decided by `import_manifest.json`, not by the
  staged directory's existence.
- **The clone does not reformat `internal/`.** `gofmt -l` is not clean
  for `internal/` and was not run over it; the files this cycle wrote are
  formatted, and the files it edited that were already non-clean on `HEAD`
  were left as they were.

## 8. 存疑 / open questions

- **"Skipping plugins disabled in Claude Code's settings" is not
  implemented in the clone, and the reference's behaviour could not be
  reproduced.** Ten candidate locations were probed with four key shapes
  (`name@marketplace`, the bare `name`, `marketplace/name`, `name@name`):
  `~/.claude/settings.json`, `~/.claude/settings.local.json`,
  `~/.claude/managed-settings.json`, `~/.claude.json`,
  `~/.claude/plugins/config.json`, `~/.claude/plugins/settings.json`,
  `~/.claude/plugins/enabled.json`, the project's `.claude/settings.json`,
  the project's `.claude/settings.local.json` and the project's
  `.claude.json` — plus `$CLAUDE_CONFIG_DIR` as the config root. **None** of
  them produced a skip, and the 1.4.11 binary carries **no** string of the
  shape "disabled in Claude Code's settings" (the phrase does not occur in
  its string table at all). Either the reference reads a file this cycle
  did not find, or the note describes a behaviour the binary does not
  exhibit. The clone therefore does **not** implement the skip: inventing a
  settings file the reference does not read would be worse than leaving it
  out.
- **The object form of a marketplace in `installed_plugins.json`** (the
  reference's decoder accepts it) yields no plugins, and the reason is not
  recoverable from the outside. The clone reproduces the *effect* (nothing
  is imported), not the mechanism.
- **The exit status of an interrupted headless run.** The clone uses 130
  (128 + SIGINT, the shell's own convention). The 1.4.11 note pins the
  *field*, not the status (【推测】).
- **The rank between the three `finished_reason` values** when more than
  one applies (a run that was interrupted *and* timed out) is the clone's
  own choice; the note does not rank them.
- **Whether the shelf jump wraps around** at either end. The clone wraps
  (【推测】), following the reference's other list jump (`[`/`]` in the
  `/diff` Per Turn tab, which the 1.4.8 note says wraps). The 1.4.11 note
  does not say.
- **The clone's shelf is a Discover section**, not a marketplace shelf —
  the feed the clone reads has no shelf of its own (§5).
- **The reference's `Staged to` line is printed per source**, not once
  for the command: `agy plugin import` with plugins on both sides prints
  it twice. The clone reproduces that.

## Limitations

- **The 1.4.11 binary was driven only for `plugin import`.** That command
  needs no network and no credentials, so it could be probed freely; every
  other item's behaviour is taken from the note and from the binary's
  string table, not from a live session. No 1.4.11 TUI session was
  rendered and no 1.4.11 hook command was run.
- **The `finished_reason` capture is the error path only.** The binary was
  driven with a `-p` run that had no credentials, which is the path that
  produces an envelope without a model. A successful run's envelope was
  not captured, so the field's position on the *success* path is inferred
  from the shared struct.
- **The clone's `plugin import` was compared against the reference on six
  synthetic trees only.** A real Claude Code installation — with a real
  `installed_plugins.json`, real marketplace layouts and real
  `enabledPlugins` settings — was not available, so the discovery rules
  are the ones the synthetic trees could distinguish.
- **The clone writes its own `~/.gemini/antigravity-cli` tree on start-up**
  (the builtin skills, the installation id, the log file). The reference
  writes its own. The comparison harness ignored that subtree; it is not
  part of the import command's behaviour.
- **Item 2's runtime is not implemented** (§3) and **item 1 is not
  applicable** (§2).
- The **served changelog feed** still lacks the 1.4.11 section — the
  vendor's repository has it, the feed does not — so the clone's
  `agy changelog` prints what the reference prints and no more.

---

Reports: the upstream notes are in
[`internal/cli/testdata/changelog_upstream_1.4.11.md`](../../../internal/cli/testdata/changelog_upstream_1.4.11.md);
the 1.4.11 string table is in
[`out/local-macos-1.4.11/strings.txt`](../../../out/local-macos-1.4.11/strings.txt),
and the reference's 1.4.11 `hooks.md` in
[`out/local-macos-1.4.11/hooks-doc.txt`](../../../out/local-macos-1.4.11/hooks-doc.txt).
