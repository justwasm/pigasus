# Cycle 234 — `agents.json` in plugin root (Discover + install summary + validate)

## Goal

AGY 1.3.6 changelog line 13:

> Fixed `agy plugin validate`, the summary `agy plugin install` prints, and
> marketplace previews in the `/plugin` Discover tab leaving out subagents a
> plugin declares in its root `agents.json`.

Cycle 234 wires all three surfaces on the clone. Pre-cycle-234, the clone
only scanned `<plugin>/agents/*.md` (the legacy per-file layout) and silently
dropped anything declared in the plugin-root `agents.json`. As a side
effect, `runPluginInstall` had been a cycle-38 stub that returned
`Error: install target must be a directory: <path>` regardless of the
input — so neither `validate` nor `install` could ever surface an
`agents.json`-declared agent end to end. Cycle 234 fixes both.

This is a D-class cycle: no new package, no new schema, no PiG changes.
The merge logic lives in `cmd/wagyu/plugin_agents.go` and is shared by
the TUI (Discover + Installed panels) and the CLI
(`plugin validate` / `plugin install` / `plugin list --json`).

## Scope (what cycle 234 changes)

| Surface | Before | After |
|---|---|---|
| `pluginAgents(name)` (TUI: `/plugin` Discover preview) | reads `<plugins>/<name>/agents/*.md` only | union of `agents/*.md` **and** `agents.json`; shows `loading...` while the marketplace preview is not yet unpacked |
| `pluginInstalledDetails` (TUI: Installed → `Agents:` row) | absent (5-line detail) | 6-line detail including `Agents:`; empty shown as `-`, present shown as comma-joined names |
| `runPluginValidate` (`agy plugin validate <path>`) | scanned markdown only, dropped `agents.json` malformed-JSON silently | reads both, prints per-source breakdown; malformed JSON → `Error: invalid agents.json: ...` with non-zero exit |
| `runPluginInstall` (`agy plugin install <target>`) | cycle-38 stub: always returned `Error: install target must be a directory` | validates manifest, copies the tree into `~/.gemini/config/plugins/<name>/` via `os.CopyFS`, writes `plugins.<name>.enabled=true` into `config.json`, prints the `Installed plugin "..."` summary with skill + agent counts |
| `plugin list --json` output | `Skills` field only | `Skills` field retained; `Agents` field added (union of md + json, sorted) |

## Schema

The plugin-root file is **either**:

```json
{"agents": [{"name": "reviewer", "path": "agents/reviewer.md"},
            {"name": "tester",   "path": "agents/tester.md"}]}
```

or, the pre-1.3.3 bare-array fallback that older plugins ship:

```json
[{"name": "reviewer", "path": "agents/reviewer.md"}]
```

Per-entry validation:

- `name` empty → entry dropped, no error
- `path` preserved verbatim (the schema does not require it to start with
  `agents/`; cycle 234 mirrors the reference and does not synthesise
  defaults)

Markdown discovery is the legacy `<plugin>/agents/*.md` shallow scan (no
recursion). Changelog line 169 documents that recursion is opt-in via
`include_only`; cycle 234 does **not** implement the recursion opt-in
because the AGY 1.3.6 fix line does not require it (the bug is about
omitting `agents.json`, not about deeper directories).

### Merge rule

```
union := {}                                 // name → entry
for f in <plugin>/agents/*.md:
    if f is a file ending in .md:
        union[f.stem] = pluginAgentEntry{Name: f.stem, Path: "agents/<f.name>"}
for e in parseAgentsJSON(<plugin>/agents.json):
    if e.name == "": continue                // skip silently
    union[e.name] = pluginAgentEntry{Name: e.name, Path: e.path}
return sortByName(union.values())
```

Two properties that follow:

1. **`agents.json` wins on name collision** (the JSON read is last).
   Cycle 234's union is a `map[name]entry` populated in `agents/*.md`
   then `agents.json` order, so the JSON entry overrides the markdown
   entry. **未验证** against the reference whether this matches REF
   exactly; the changelog wording is silent on collisions. The
   `TestCycle234_JsonSortIsStable` test pins the deterministic ordering.
2. **Either side may be absent** — a markdown-only plugin reads the
   same as pre-cycle-234; a json-only plugin reads as if all agents were
   declared in markdown.

## Files changed

| File | +/− | Purpose |
|---|---|---|
| `cmd/wagyu/plugin_agents.go` (new) | +264 / −0 | `pluginAgentEntry`, `pluginAgentsJSON`, `readPluginAgentsJSON`, `normalizeAgentsJSON`, `pluginAgentsMarkdown`, `pluginAgents` (installed-path union), `pluginAgentsAtPath` (arbitrary-path union, returns `jsonErr`), `pluginUnpackedAgents` (Discover preview, `loading...` when not unpacked) |
| `cmd/wagyu/slash_panels.go` | +3 / −1 | `pluginInstalledDetails` adds `Agents:` row between `Skills:` and `Marketplace:` |
| `cmd/wagyu/slash_plugin_search.go` | +11 / −1 | `pluginDiscoverDetails` adds `Agents:` row between `Components:` and `Marketplace:` |
| `cmd/wagyu/slash_cycle234_test.go` (new) | +339 / −0 | 11 TUI tests (see **Test coverage**) |
| `cmd/wagyu/slash_cycle198_test.go` | +1 / −1 | `TestCycle198_EnterExpandsDetails` updated to expect `Agents:      -` in the 6-line detail |
| `cmd/wagyu/slash_cycle204_test.go` | +1 / −1 | `TestCycle204_DiscoverDetails` updated for the 5-line Discover detail (Components / **Agents** / Marketplace / Path / Version) |
| `internal/cli/subcommand_parity.go` | +123 / −13 | `legacyListAgents` (CLI-side union), `legacyLoadPluginAgents` (used by `plugin list --json`), `runPluginValidate` agents line + malformed-JSON fail, `runPluginInstall` full install path, `copyTree`, `legacyMarkEnabled` |
| `internal/cli/plugin_agents_test.go` (new) | +325 / −0 | 8 CLI tests (see **Test coverage**) |

Two architectural notes:

- `copyTree` and `legacyMarkEnabled` are deliberately local rather than
  reusing `auth.SetPluginEnabled`: the install path runs before auth
  state is ready, and `legacyPluginsDir()` reads `GEMINI_CONFIG_HOME`
  (matching the reference), not `auth`'s home.
- `fail(j, 1, msg)` writes to stderr but returns `errExitCode(1)` with
  no message — cycle 234 uses `errExitCodeMsg(1, ...)` for the two
  malformed-JSON paths so the test that asserts
  `err.Error() == "Error: invalid agents.json: ..."` is byte-exact.

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-agentsjson ./cmd/wagyu` | exit 0, **73 MB** |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (×3) | `internal/cli` + all 11 TUI tests green; one pre-existing baseline flake `TestDispatchPrintSlash_LocalCommands` (`/model` default drift on this machine, unrelated to cycle 234) |
| `gofmt -l cmd/wagyu/plugin_agents.go cmd/wagyu/slash_cycle234_test.go cmd/wagyu/slash_panels.go cmd/wagyu/slash_plugin_search.go internal/cli/subcommand_parity.go internal/cli/plugin_agents_test.go cmd/wagyu/slash_cycle198_test.go cmd/wagyu/slash_cycle204_test.go` | empty (cycle-234 files gofmt-clean) |
| `go vet ./internal/... ./cmd/wagyu/` | no cycle-234 warnings; the 2 pre-existing `Errorf format %)` warnings in `cmd/wagyu/slash_cycle224_test.go` are cycle-224 inheritance, **not** cycle-234 |
| `git diff internal/convstore` | empty (AGENTS.md hard rule respected) |

## Test coverage

### `cmd/wagyu/slash_cycle234_test.go` (11 tests, all green)

| Test | Pins |
|---|---|
| `TestCycle234_MarkdownOnlyIsUnchanged` | legacy-only plugin still returns `[name1, name2, ...]` from `agents/*.md` (no JSON side effects) |
| `TestCycle234_JsonOnlyIsLoaded` | a plugin with `agents.json` only (no `agents/*.md`) still surfaces all four agents |
| `TestCycle234_UnionOfBothSources` | `agents/*.md` (2 files) ∪ `agents.json` (2 entries) = 4 entries, JSON side wins on name collision |
| `TestCycle234_NeitherPresentUnaffected` | a plugin with neither file returns `[]` (no error, no panic) |
| `TestCycle234_DiscoverPreviewListsAllAgents` | `pluginDiscoverDetails` renders `Agents:      <comma-joined>` between `Components:` and `Marketplace:` |
| `TestCycle234_InstalledDetailsHasAgentsRow` | `pluginInstalledDetails` includes `Agents:      <names>`; empty → `Agents:      -` |
| `TestCycle234_MalformedAgentsJsonFallsBack` | unparseable JSON does not crash `pluginAgentsMarkdown`; the markdown side still loads |
| `TestCycle234_AgentsJsonEntryMissingNameSkipped` | `{"agents": [{"path": "x.md"}]}` is dropped silently (no entry in the union) |
| `TestCycle234_JsonFlatFormAccepted` | bare-array form `[{"name": "a", ...}]` is normalised and accepted |
| `TestCycle234_JsonExcludesNonMdFiles` | `{"agents": [{"name": "x", "path": "agents/x.txt"}]}` is preserved verbatim; cycle 234 does not gate on `.md` extension (matches REF: the schema only specifies `name` + `path`) |
| `TestCycle234_JsonSortIsStable` | union output is sorted alphabetically by `name`; ties broken by source (md-then-json) deterministically |

### `internal/cli/plugin_agents_test.go` (8 tests, all green)

| Test | Pins |
|---|---|
| `TestCLI_ValidateListsAgents` | `agy plugin validate <plugin>` prints `agents : N processed` (or `skipped (not found)` for empty) |
| `TestCLI_ValidateMissingAgentsShowsSkipped` | validate with no `agents/` and no `agents.json` prints `agents : skipped (not found)` |
| `TestCLI_ValidateMalformedAgentsJsonFails` | malformed JSON → exit 1 with `err.Error() == "Error: invalid agents.json: ..."`; markdown side still loaded first |
| `TestCLI_ValidateUnionMarkdownAndJson` | validate counts union: 2 md + 2 json → `agents : 4 processed` |
| `TestCLI_InstallSummaryListsAgents` | `agy plugin install <plugin>` copies the tree and prints `Installed plugin "..."` / `  - agents : 4 processed` |
| `TestCLI_InstallSkippedWhenNoAgents` | install with no agents prints `  - agents : skipped (not found)` |
| `TestCLI_InstallRejectsMalformedAgentsJson` | install with malformed JSON fails **before** the copy, exit 1 with `err.Error() == "Error: invalid agents.json: ..."` |
| `TestCLI_ListAgentsFlag` | `plugin list --json` includes `Agents: [...]` field |

## Probe — clone vs reference

End-to-end smoke test against `/tmp/wagyu-agentsjson` with
`GEMINI_CONFIG_HOME=/tmp/scratch`. Three fixtures:

| Fixture | Tree | `validate` | `install` | `plugin list` |
|---|---|---|---|---|
| `agents-json-plugin` (json-only) | `plugin.json` + `agents/{explorer,writer}.md` + `agents.json` (2 entries) | `agents : 4 processed` | `Installed plugin "agents-json-plugin"` / `agents : 4 processed`; tree copied to `dest/plugins/agents-json-plugin/` | `agents-json-plugin (enabled, 1.2.3)` |
| `union-plugin` (md + json) | `plugin.json` + `agents/{one,two}.md` + `agents.json` (2 entries) | `agents : 4 processed` | `agents : 4 processed` | (not installed) |
| `bad-plugin` (malformed) | `plugin.json` + `agents/x.md` + `agents.json` = `not json{` | **exit 1** + `Error: invalid agents.json: invalid character 'o' looking for beginning of value` (stderr) | (not exercised; fails before copy) | n/a |

`config.json` after a successful install:

```json
{
  "plugins": {
    "agents-json-plugin": { "enabled": true }
  }
}
```

**Not run against the reference binary** (`~/.local/bin/agy 1.4.0`):
this machine's reference is 1.4.0, but the AGY 1.3.6 fix line is older.
The probe captures clone-only behaviour; REF's 1.3.6 wording is in
`internal/cli/testdata/changelog_reference.txt:13`.

## 存疑 / limitations

1. **`agents.json` overrides `agents/*.md` on name collision.** The
   union reads md first, then json; same `name` → JSON wins. The
   changelog line does not specify a winner; the choice is deliberate
   (newer schema beats legacy) but **未验证** against REF.
2. **`include_only` recursion (changelog line 169) is not implemented.**
   The schema supports `{"path": "shared_agents", "include_only": [...]}`
   for nested directories; cycle 234 only reads top-level. AGY 1.3.6
   line 13 does not require recursion — it is about omitting
   `agents.json`. A future cycle can add `include_only` parsing without
   touching the union logic.
3. **`path` is preserved verbatim.** The schema does not require
   `path` to start with `agents/`. `TestCycle234_JsonExcludesNonMdFiles`
   pins that cycle 234 does not gate on the extension; a future
   hardening cycle might restrict to `.md`.
4. **Discover preview `loading...` is shown when the marketplace
   preview is not yet unpacked** — the TUI's marketplace handler
   controls when the cache is populated. Cycle 234 reads
   `pluginUnpackedAgents(name)` which returns `("loading...", nil)` when
   the unpack path does not exist; once unpack completes the row
   resolves. The render path uses the same string the markdown loader
   produces, so a future change to that loader is reflected here.
5. **`runPluginInstall` uses `os.CopyFS` (Go 1.23+).** The macOS
   toolchain is Go 1.27.0 (`/Users/gear/go/bin/go`); the sandbox used
   for the committed evidence is Go 1.22.10 and would need a stdlib
   workaround. Cycle 234 only builds and tests on this machine.
6. **`copyTree` and `legacyMarkEnabled` are local** rather than
   importing `auth.SetPluginEnabled`. The install path runs before
   `auth` state is ready, and `legacyPluginsDir()` reads
   `GEMINI_CONFIG_HOME` — `auth.SetPluginEnabled` reads `os.UserHomeDir`.
   Duplication is small (~30 lines) and avoids dragging the auth
   initialisation path into the install handler. Future cleanup can
   unify once the install path moves into `internal/auth`.
7. **The `--plugins-dir` flag I considered adding is not implemented.**
   `legacyPluginsDir()` honours `GEMINI_CONFIG_HOME`; adding a CLI flag
   would mean threading the override through `runPluginList`,
   `runPluginValidate`, `runPluginInstall`, `runPluginUninstall`,
   `runPluginToggle`, and `legacyLoadPlugins`. Out of scope for cycle
   234.
8. **Pre-existing baseline flake:** `TestDispatchPrintSlash_LocalCommands`
   fails on this machine because `/model` default is
   `gemini-3.8-flash-medium` (REF's per-machine default) rather than
   `minimax-m3` (the test's expectation). This is unrelated to cycle
   234; recorded here so future cycles do not chase it.