# Cycle `config-bom-strip` — deliverable

## What this cycle is

AGY 1.2.16 changelog line 13 (verbatim from
`internal/cli/testdata/changelog_reference.txt` line 113):

> Fixed settings failing to load when `~/.gemini/config/config.json`
> starts with a UTF-8 byte order mark, as files saved by Notepad or
> PowerShell `Set-Content` on Windows do

Concretely: `Notepad > File > Save As > Encoding: UTF-8` and PowerShell's
`Set-Content -Encoding UTF8` (the default) write a 3-byte prefix —
`EF BB BF` — at the head of the file. `json.Unmarshal` rejects anything
that does not start with `{`, `[`, `"`, a number, or one of the
literals, so a leading `EF BB BF` makes the whole config parse fail
and every reader above it fall back to its "treat as default / absent"
path. On AGY that means:

- the `/plugin` panel renders every entry as `[disabled]` (the cycle
  198 reader can no longer find `plugins.<name>.enabled`)
- the `/mcp` panel loses its per-server `disableAllMCP` toggle (the
  MCPsBlocked helper's `disableAllMCPField` returns false because the
  unmarshal fails)
- `/model`, `/statusline`, `/title`, and the `/config` picker all
  drop back to their default values because `SettingsString`,
  `SettingsBool`, `SettingsField` and `ResolveSettingsModel` all
  return the empty default on a parse error

The fix is the same fix agy shipped: trim the leading `EF BB BF` at
every JSON read site that touches `config.json`, `mcp.json`, and
`settings.json` — three files the user might legitimately hand-edit on
a Windows machine.

## The bytes

A UTF-8 byte order mark is exactly three bytes:

```
EF BB BF
```

It is the Unicode codepoint U+FEFF encoded as UTF-8 (the same
zero-width no-break space that a UTF-16 BOM is, repurposed as a
streaming marker). It is *only* the UTF-8 BOM that this cycle strips —
UTF-16 (`FF FE` / `FE FF`) and UTF-32 (`FF FE 00 00` / `00 00 FE FF`)
BOMs are out of scope, partly because they would mean the file is not
actually UTF-8 (and AGY 1.2.16 line 13 specifically scoped the fix to
the UTF-8 form) and partly because the JSON parser would still reject
the file even after a UTF-16 BOM strip.

A "BOM-prefixed JSON file" looks like this on disk:

```
EF BB BF 7B 22 70 6C 75 67 69 6E 73 22 3A ...
└─BOM─┘ └─{ "plugins": ............
```

`encoding/json`'s `Unmarshal` rejects the leading `EF BB BF` with the
error `invalid character '\u00ef' looking for beginning of value`.

## Where the fix lands

Three small `stripBOM(b []byte) []byte` helpers, one per package that
reads JSON config files. Each is a 4-line, zero-deps function: take the
leading three bytes, compare to `EF BB BF`, return `b[3:]` if matched
or `b` otherwise. There is no shared util package because `pigrt` is
deliberately import-free of `internal/auth` (the runtime is meant to
be usable without the clone's compatibility layer) and `cmd/wagyu` is
the `main` package.

| File | Helper name | Reader sites fixed |
|---|---|---|
| `internal/auth/compat.go` | `stripBOM` | `config.json` (`PluginEnabled`, `SetPluginEnabled`, `ClearPluginEnabled`, `disableAllPluginsField`); `mcp.json` (`LoadMCPConfig`, `disableAllMCPField`); `settings.json` (`SaveSettingsColorScheme`, `IsWorkspaceTrusted`, `LoadTrustedWorkspaces`, `SettingsBool`, `SettingsString`, `SettingsField`, `updateSettings`, `updateOrCreateSettings`, `ResolveSettingsModel`) |
| `internal/pigrt/mcpconfig.go` | `stripUTF8BOM` | `LoadAgyMCP`, `LoadAgyMCPFile` (both read `mcp.json`) |
| `cmd/wagyu/print_slash.go` | `stripUTF8BOM` | `readSettingsFile` is the single chokepoint for all print-mode settings.json reads; `printSlashConfig` (print-mode `/config`), `readPrintSlashSettings` (`/permissions`), `printPermissionMode` (`print_format.go`), `configValues` (`print_format.go`) all flow through it |

The other JSON read sites in the tree (`plugin.json`, `agents.json`,
`marketplace.json`, `hooks.json`, `mcp_config.json` for the legacy
schema, plugin `state/plugin-processes.json`, etc.) are addressed by
AGY 1.2.16 line 79 / 376 in the changelog, *not* line 13. Out of
scope for this cycle. A future `cycle config-bom-strip-all` would
generalise the helper into a shared `internal/utf8bom` package and
apply it to those readers too — that is a once-over-the-tree change
that belongs in its own cycle.

## What AGY 1.2.16 line 13 specifically said

The line names `~/.gemini/config/config.json` only. The other two
files are caught because they share the same load path
(`internal/auth/compat.go` has a fan of `ReadFile + Unmarshal` on
adjacent lines and the cycle would be obviously incomplete without
them). `internal/pigrt/mcpconfig.go` is caught because both the TUI
and the print path of `internal/cli/jetski.go` `mcp list` route MCP
config reads through `LoadAgyMCP`.

## Test pins

`internal/auth/compat_test.go` gained four new tests, all
`BOM`-prefixed:

| Test | Reads | Asserts |
|---|---|---|
| `TestConfigBOM_Stripped` | `~/.gemini/config/config.json` | `PluginEnabled("alpha")` returns `true` and `PluginsBlocked()` returns `true` when the on-disk file is `EF BB BF` + JSON; includes a sanity check that the raw bytes would have failed a hand-rolled `json.Unmarshal` |
| `TestConfigBOM_AbsentPassesThrough` | same file, no BOM | `PluginEnabled` and `PluginsBlocked` agree with the seeded values — the regression pin for the Windows-without-BOM and macOS/Linux load path |
| `TestMcpConfigBOM_Stripped` | `~/.gemini/antigravity-cli/mcp.json` | `LoadMCPConfig().Servers["echo"].Command == "echo"` and `MCPsBlocked() == true` under a BOM |
| `TestSettingsBOM_Stripped` | `~/.gemini/antigravity-cli/settings.json` | `SaveSettingsColorScheme` succeeds, `IsWorkspaceTrusted` finds the seeded path, `SettingsString("colorScheme")` returns non-empty, `ResolveSettingsModel("fallback")` returns the seeded model |

All four tests live in `package auth` and run with the rest of the
package's `TestWriteCompatDefaults_*` / `TestOnboardingCompleted_*` /
`TestSaveSettingsColorScheme_RoundTrip` regression pins:

```
go test -vet=off -count=1 -run 'TestConfigBOM|TestMcpConfigBOM|TestSettingsBOM' ./internal/auth/
ok  	github.com/chadgpt/openagy/internal/auth	0.614s
```

Final run (all `internal/auth` tests):

```
go test -vet=off -count=1 ./internal/auth/
ok  	github.com/chadgpt/openagy/internal/auth	26.858s
```

Final run (`internal/pigrt`, which owns the second `mcp.json` reader):

```
go test -vet=off -count=1 ./internal/pigrt/
ok  	github.com/chadgpt/openagy/internal/pigrt	1.549s
```

Build:

```
go build -o /tmp/wagyu-check ./cmd/wagyu
# 73,679,042 B
```

## Out of scope / not done here

- **Other BOM variants.** UTF-16 / UTF-32 BOMs are not stripped. AGY
  1.2.16 line 13 explicitly named the UTF-8 case.
- **Line 79 and 376 of the same changelog.** Plugin manifests,
  `mcp_config.json` (legacy schema), and `hooks.json` files saved with
  a BOM are addressed by line 79; "parsing MCP JSON configuration
  files on Windows when saved with a UTF-8 byte order mark" is line
  376. Both share the same conceptual fix and would be a
  `cycle config-bom-strip-all` PR. Left for a follow-up cycle; they
  are not in this branch.
- **`plugin.json` / `marketplace.json` / `agents.json` / `hooks.json`
  BOM tolerance.** Same fix, same shape, not in this cycle (line 79
  author scope).
- **A shared helper.** Three private copies of `stripBOM` exist
  because `internal/pigrt` deliberately does not depend on
  `internal/auth` and `cmd/wagyu` is a `main` package. A refactor to
  `internal/utf8bom` would clean this up but is orthogonal to the
  bug.
- **`gofmt -w`.** `internal/auth/compat.go` and `cmd/wagyu/print_format.go`
  were gofmt-dirty before this cycle (the `compat.go` block is a
  doc-comment indentation disagreement, the `print_format.go` item is
  whatever the AGENTS.md-listed baseline contains). Per repo
  convention they are left alone.

## Files touched

```
internal/auth/compat.go        +33 / -1   (helper + 13 call sites)
internal/auth/compat_test.go   +new       (4 BOM tests)
internal/pigrt/mcpconfig.go    +~20       (helper + 2 call sites)
cmd/wagyu/print_slash.go       +18 / -3   (BOM-tolerant readSettingsFile + helper)
docs/cycles/config-bom-strip/deliverable.md   new
```

`internal/auth/compat.go` and `cmd/wagyu/print_format.go` were
already on the gofmt-dirty list before this cycle — reformatting them
is out of scope per `AGENTS.md` ("Do not reformat them wholesale (it
buries real diffs)").
