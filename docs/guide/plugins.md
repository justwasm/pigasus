# The plugin surfaces

> The plugin domain has two front-ends in the clone: the `/plugin` panel in
> the TUI, and the `agy plugin import` command on the shell. Both were
> touched by cycle 364 (agy 1.4.11).

| what | where |
|---|---|
| the installed plugin's tree | `~/.gemini/config/plugins/<name>/` |
| the installed plugin's manifest | `~/.gemini/config/plugins/<name>/plugin.json` |
| the marketplace feed | `~/.gemini/antigravity-cli/cache/marketplaces/<mkt>/marketplace.json` |
| the panel's state | `internal/cliapp` (`slash_panel_plugin.go`, `slash_plugin_shelf.go`) |
| the panel's rendering | `internal/pluginsui` (`sections.go`, `render.go`) |
| the import command | `internal/cli/subcommand_plugin.go` → `internal/features/plugins/import` |

---

## The Discover tab's shelves (cycle 364, agy 1.4.11)

【一手】 agy 1.4.11: "Improved the `/plugin` Discover tab for long shelves:
each shelf now shows its first 6 plugins followed by a `⋯ N more — enter to
show` row, `Ctrl+E` expands or collapses the shelf under the cursor, and `[`
and `]` jump to the previous or next shelf."

【strings】 the 1.4.11 binary carries the row's format (`%d more — enter to
show`, next to the `⋯ ` glyph) and the three hints the panel's footer shows
for the new keys: `Expand Shelf`, `Collapse Shelf` and `Prev/Next Shelf`.

| what | where |
|---|---|
| the limit (6) | `pluginsui.ShelfLimit` |
| a shelf's rows | `pluginsui.ShelfRows(items, expanded)` |
| the "more" row's text | `pluginsui.ShelfMoreText(n)` — `⋯ N more — enter to show` |
| the "more" row's rendering | `pluginsui.ShelfMoreLine` |
| the footer's legend | `pluginsui.DiscoverLegend(onLocal, search, shelfExpanded)` |
| the layout, the toggle, the jump | `internal/cliapp/slash_plugin_shelf.go` |
| the keys | `internal/cliapp/tui_keymap_plugin.go` |

**What the clone's "shelf" is.** The reference's shelf is a *marketplace*
shelf — a curated group of plugins the marketplace feed itself carries. The
feed the clone reads has no shelf of its own, so the clone's shelf is one
of the Discover **sections** the 1.4.9 cycle grouped the feed into
(`pluginsui.GroupDiscoverSections`). A feed that groups into at most one
section is shown as one unnamed shelf, exactly as the panel has always
shown it.

**The keys.** `Ctrl+E` is matched as `key.Code == 'e' &&
key.Mod.Contains(tea.ModCtrl)` — Bubble Tea v2 v2.1.0 has no
`tea.KeyCtrlE`. `[` and `]` jump to the previous or the next shelf and
**wrap around** at either end (【推测】 — the 1.4.11 note does not say; the
reference's other list jump, `[`/`]` in the `/diff` Per Turn tab, does).
`Enter` on the "more" row shows the shelf.

Report: `docs/cycles/cycle-364-agy-1.4.11-alignment/deliverable.md` §5.

---

## `agy plugin import` (cycle 364, agy 1.4.11)

【一手】 agy 1.4.11: "Fixed `agy plugin import claude` printing `No claude
extensions found.` for plugins installed from a Claude Code marketplace: it
now reads Claude Code's list of installed plugins, skipping plugins
disabled in Claude Code's settings and plugins installed for a single
project only, and imported plugins now keep their remote MCP servers
(`"type": "http"` with `url` and `headers`) and servers from `.mcp.json`
files without an `mcpServers` wrapper, expand `${CLAUDE_PLUGIN_ROOT}` to
the installed plugin's directory, and list servers that can't be
converted, such as ones using the legacy SSE transport, with a warning
instead of reporting them as installed."

The behaviour below was measured first-hand against the 1.4.11 binary with a
synthetic `$HOME`; the command needs no network and no credentials, and
the reference's own error messages name the files it reads.

### The surface

| invocation | the reference |
|---|---|
| `agy plugin import` | the Gemini CLI importer **and then** the Claude Code importer |
| `agy plugin import gemini` | the Gemini CLI importer only |
| `agy plugin import claude` | the Claude Code importer only |
| `agy plugin import claude --force` | re-imports a plugin the manifest already lists |
| `agy plugin import <other>` | `Error: unknown import source or invalid path: <other>`, exit 1 |

A source that finds nothing prints `No <source> extensions found.`. A
source that finds something ends with a blank line and
`Staged to <~/.gemini/config>` — **per source**, so the bare command prints
it twice when both sources find something.

### The report

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
stderr too, and it exits **0**.

The component lines print in the order `skills`, `agents`, `commands`,
`mcpServers`; the manifest records them in a *different* order — `skills`,
`agents`, `mcpServers`, `commands`. Both are the reference's own.

### What is read

- **Claude Code** — `~/.claude/plugins/installed_plugins.json`, shaped
  `{"plugins": {"<marketplace>": [ <install>, … ]}}`. `installPath` is the
  only field the location comes from (`path` is not accepted); the record's
  `name` is only a fallback — the manifest's name wins; `scope` decides
  whether the record is skipped (`project` and `local` are skipped
  silently, `user`, `global`, an unknown scope and an absent scope are
  imported).
- **Claude Code** — the top-level directories under `~/.claude/plugins/`
  that carry a `.claude-plugin/plugin.json`.
- **Gemini CLI** — `~/.gemini/extensions/<name>/gemini-extension.json`.

The reference's order is: the marketplaces of `installed_plugins.json` in
key order, each marketplace's installs in file order, then the top-level
directories in name order.

### What is written

- The plugin tree is copied to `~/.gemini/config/plugins/<name>/`.
- A `plugin.json` is written at the plugin root carrying only `name`,
  `version`, `description` and `author` (the author's **name**, whether the
  source manifest wrote it as a string or as `{"name","url"}`). The file
  ends with a newline.
- `~/.gemini/config/import_manifest.json` —
  `{"imports":[{"name","source","importedAt","components"}]}`, with
  `source` either `claude-code` or `gemini-cli` and `components` `null`
  when the plugin staged none. The "already imported" skip is keyed on the
  **(name, source)** pair.

### The component conversions

- **skills** — `skills/*/SKILL.md`, copied as they are.
- **agents** — `agents/*.md`, copied as they are.
- **commands** — `commands/<name>.md` becomes
  `skills/<plugin>-cmd-<name>/SKILL.md`, the bytes unchanged.
- **mcpServers** — `.mcp.json` is converted into the CLI's own
  `mcp_config.json`, and Claude Code's file name is removed from the
  staged tree afterwards.

The MCP conversion rules, as measured:

| the server entry | the result |
|---|---|
| `{"type":"http","url":…,"headers":{…}}` | `{"serverUrl":…,"headers":{…}}` |
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

`${CLAUDE_PLUGIN_ROOT}` expands to the **staged** plugin directory — not to
the Claude Code directory the plugin came from — in the command, in the
arguments and in the environment values.

### The code

| what | where |
|---|---|
| the run loop, the report, the staging, the manifest | `internal/features/plugins/import/pluginimport.go` |
| the two discovery passes, the source manifests | `internal/features/plugins/import/discover.go` |
| the MCP server conversion | `internal/features/plugins/import/mcp.go` |
| the command surface | `internal/cli/subcommand_plugin.go` (`runPluginImport`) |

The reference's discovery errors carry Go type names from the reference's
own package (`map[string]claude.pluginInstalls`,
`[]claude.pluginInstall`); the clone's package is named differently, so
those two messages are emitted as compatibility shims rather than produced
by the clone's decoder.

Report: `docs/cycles/cycle-364-agy-1.4.11-alignment/deliverable.md` §6.
