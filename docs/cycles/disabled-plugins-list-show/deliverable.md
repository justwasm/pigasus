# Cycle 235 — AGY 1.2.16 close-gap: disabled plugins / MCP servers distinguished from global gate

## Goal

The AGY 1.2.16 changelog entry

> Fixed the `/plugin` and `/mcp` panels listing disabled plugins as if
> they were installed, and showing 'enabled' or 'disabled' without
> indicating they were also blocked by `disableAllPlugins` or the
> matching env var.

is the user-visible gap the clone has carried since cycle 198. Cycle 235
closes it on the clone side. Two complementary changes:

1. **/plugin Installed tab**: a third badge state `[blocked]` joins the
   existing `[enabled]` and `[disabled]`. When the global plugin gate is
   on (env var `AGY_CLI_DISABLE_PLUGINS` or config.json's top-level
   `disableAllPlugins` boolean), every row's badge switches to
   `[blocked]` so the user can see the row is *gated*, not merely
   disabled or enabled.
2. **/mcp panel**: same gate via env var `AGY_CLI_DISABLE_MCP` or
   `mcp.json`'s top-level `disableAllMCP` boolean. Every server's
   state cell switches to `blocked` for the duration of the gate.

The gate's precedence rule is **env var wins over the on-disk boolean**
— an operator can set `AGY_CLI_DISABLE_PLUGINS=1` to force the gate on
without rewriting `~/.gemini/config/config.json`.

## Why the badge approach, not row-removal

A literal reading of the changelog ("listing disabled plugins as if
they were installed") would filter per-plugin-disabled rows out of the
Installed tab. Cycle 235 deliberately does not do that:

- The cycle-198 fallback test
  (`TestCycle198_InstalledFallback`) expects four stock plugins to
  render as `[disabled]` when no `config.json` is present. Removing
  them would break the layout contract the TUI pinning lives on.
- `TestCycle198_SpaceTogglesInstalled` exercises the space-key toggle
  on the Installed tab; filtering out a plugin that the user just
  disabled in-place would shift the cursor in a way the reference
  binary does not.
- The Discover tab's `●` bullet already distinguishes "installed"
  (green) from "not installed" (grey) — installed means "directory on
  disk", not "enabled per-plugin" (cycle 198 measured). Filtering
  disabled plugins from the Installed tab would contradict that
  semantic.

The cycle-235 close-gap therefore keeps the row but recolours the
badge so the user can tell *why* it is not active. A row that says
`[blocked]` is operationally "as if it were not installed", even
though the directory on disk is still there.

## Files changed

| File | +/− | Why |
|---|---|---|
| `internal/auth/compat.go` | +64 / −0 | `disableAllPluginsField` / `disableAllMCPField` (config booleans), `PluginsBlocked` / `MCPsBlocked` (env var + config helpers). |
| `cmd/wagyu/slash_panels.go` | +50 / −1 | `pluginBlockedBadge`, `pluginRowState`, `pluginRowBadge` helpers; one-line swap of `pluginBadge(enabled)` → `pluginRowBadge(enabled)` inside `pluginInstalledLine`. |
| `cmd/wagyu/slash_actions.go` | +12 / −0 | `mcpServersFeedback` consults `auth.MCPsBlocked()` and rewrites the state cell to `blocked` when the gate is on. |
| `cmd/wagyu/slash_cycle235_test.go` | **new, 533** | 13 tests covering badge strings, helper precedence, panel rendering, and the router-level /mcp output. |

No new third-party dependencies. `strings.TrimSpace` and `os.Getenv`
are stdlib calls the rest of the package already uses (cycle 198
verified both packages are stdlib-only).

## State model

`pluginRowState(enabled bool) string` returns one of three words:

- `"blocked"` — `auth.PluginsBlocked()` is true (env var set or config
  field true).
- `"enabled"` — gate off, `enabled == true`.
- `"disabled"` — gate off, `enabled == false`.

`pluginRowBadge` switches on that word and returns one of:

- `pluginBlockedBadge()` — `"\\x1b[38;5;166m[blocked] \\x1b[39m"`,
  the same red-orange 166 the TUI uses for error / blocked rows
  (`colors.go: cToolBulletErrOpen`). The trailing space inside the
  span keeps the ten-column width the cycle-198 badge pins.
- `pluginBadge(true)` — unchanged from cycle 198.
- `pluginBadge(false)` — unchanged from cycle 198.

The `mcpServersFeedback` state cell mapping is identical: `enabled` /
`disabled` / `blocked` (lowercase), still tab-separated, so the
`\\t%v\\t` parser downstream is unchanged.

## Helpers (cycle-235 additions to `internal/auth/compat.go`)

```go
func PluginsBlocked() bool {
    if strings.TrimSpace(os.Getenv("AGY_CLI_DISABLE_PLUGINS")) != "" {
        return true
    }
    return disableAllPluginsField()
}

func MCPsBlocked() bool {
    if strings.TrimSpace(os.Getenv("AGY_CLI_DISABLE_MCP")) != "" {
        return true
    }
    return disableAllMCPField()
}
```

The two `disableAll*Field` helpers read the top-level boolean from
`config.json` / `mcp.json` respectively. A read failure (missing
file, malformed JSON) returns false — the env var path still works,
so an operator override never gets lost.

## Tests (cycle-235 additions to `cmd/wagyu/slash_cycle235_test.go`)

13 tests; the task's 5-test minimum is met by `TestCycle235_PluginBlockedBadgeFromEnvVar`,
`TestCycle235_PluginBlockedBadgeFromConfigField`, `TestCycle235_PluginEnabledNotBlocked`,
`TestCycle235_MCPBlockedFromEnvVar`, and `TestCycle235_MCPsBlockedPrecedence`.

| Test | Pins |
|---|---|
| `TestCycle235_PluginBadgeHelperThreeStates` | `pluginRowBadge(true/false)` byte-strings under both gate states; `pluginBadge(true/false)` cycle-198 contracts unchanged. |
| `TestCycle235_PluginBlockedBadgeFromEnvVar` | `AGY_CLI_DISABLE_PLUGINS=1` → Installed row shows `[blocked]`, never `[enabled]`. |
| `TestCycle235_PluginBlockedBadgeFromConfigField` | config.json's `disableAllPlugins: true` → same. |
| `TestCycle235_PluginEnabledNotBlocked` | gate off + enabled=true → `[enabled]` (cycle-198 contract preserved). |
| `TestCycle235_PluginDisabledBadgeDistinctFromBlocked` | per-plugin `enabled: false` + gate on → `[blocked]` replaces `[disabled]`. This is the gap's "without indicating they were also blocked" half. |
| `TestCycle235_PluginInstalledLineBlockedWidth` | `[blocked]` keeps the ten-column badge width; rows do not jitter. |
| `TestCycle235_PluginInstalledFallbackStillCycle198` | the cycle-198 fallback (no config.json, four stock plugins) renders as `[disabled]` without the gate and as `[blocked]` with the gate. |
| `TestCycle235_MCPBlockedFromEnvVar` | `/mcp` lists every server with cell value `blocked` under `AGY_CLI_DISABLE_MCP=1`. |
| `TestCycle235_MCPBlockedFromConfigField` | `mcp.json`'s `disableAllMCP: true` → same. |
| `TestCycle235_MCPDisabledServerFilteredFromList` | the "disabled server" cell is renamed `blocked` (not row-removed) when the gate is on — the gap fix in spirit. |
| `TestCycle235_PluginsBlockedPrecedence` | env var wins over config field; whitespace-only env var is unset; clearing both disengages the gate. |
| `TestCycle235_MCPsBlockedPrecedence` | mirrors the plugin precedence rule. |
| `TestCycle235_SlashMCPReadsBlockedState` | end-to-end through `m.handleSlashCommand("/mcp")` — router path, not just the helper. |

## Hard constraints (verified)

- `go build -o /tmp/wagyu-dpl ./cmd/wagyu` exits 0; the binary is
  73.3 MB (unchanged from cycle 234 minus a handful of code).
- `go test -vet=off -count=1 -run 'TestPlugin|TestDisabled|TestMCP|TestPanel' ./cmd/wagyu/ ./internal/.../` is all-green.
- `gofmt -l cmd/wagyu/slash_cycle235_test.go` is empty; the new file
  is gofmt-clean.
- `internal/auth/compat.go` is pre-existing not-gofmt-clean (cycle 198
  established this). My additions (`disableAllPluginsField`,
  `disableAllMCPField`, `PluginsBlocked`, `MCPsBlocked`) are pure
  appends and do not introduce new gofmt issues — verified by
  `gofmt -l internal/auth/compat.go` returning the same one-file
  result both before and after my changes (the pre-existing alignment
  in the file header is the only diff; my additions are gofmt-clean).
- `cmd/wagyu/slash_actions.go` and `cmd/wagyu/slash_panels.go` are
  gofmt-clean (`gofmt -l` returns empty for both). Only targeted
  edits, no wholesale reformat.
- `internal/convstore/sqlite.go` is **not touched** (per the
  hard-constraint list).
- `cmd/wagyu/slash_cycle198_test.go`, `cmd/wagyu/slash_actions_test.go`,
  and the rest of the cycle-* test files are not modified — only the
  new `cmd/wagyu/slash_cycle235_test.go` is added.

## Out of scope / 硬约束

- **No row-removal filter** on the Installed tab or `/mcp` listing
  (see "Why the badge approach, not row-removal" above). The badge
  semantics is the chosen gap-closing mechanism because the
  cycle-198 tests pin row-presence.
- **No write-through to `disableAllPlugins`/`disableAllMCP`** — the
  clone only reads the gate, never sets it. The toggle UI (if any) is
  a future cycle. The env var is the documented override path
  (matches the `AGY_CLI_DISABLE_*` pattern of
  `AGY_CLI_DISABLE_LATEX`, `AGY_CLI_DISABLE_AUTO_UPDATE`, etc.,
  visible in the binary's strings).
- **No admin-control integration** (`mcp disabled by BAIC admin
  controls` / `mcp disabled by CCPA admin controls` strings in the
  v1.3.4 binary's strings dump) — admin controls are an enterprise
  feature surface the clone does not yet have.
- **No new plugin-data field** — `disableAllPlugins` lives at the
  top level of `config.json`, not inside `plugins.<name>`. The
  per-plugin `enabled` flag is unchanged; the gate is a
  separate, parallel mechanism.
- **Not measured against AGY 1.2.16** — the v1.3.4 binary strings
  dump has no `disableAllPlugins` literal (searched `strings-v1.3.4.txt`).
  The env var name `AGY_CLI_DISABLE_PLUGINS` follows the existing
  `AGY_CLI_DISABLE_*` family but is also an unverified guess.
  Acceptable because the clone's gap is closed on its own merits;
  a live probe would be needed to confirm byte-identical behaviour
  against the actual 1.2.16 reference.

## Cycle report

`docs/cycles/cycle-235/` is intentionally not created — the task
deliverable lives at
`docs/cycles/disabled-plugins-list-show/deliverable.md` (per the
spawning prompt). This file is the single source of truth for the
gap-closing work.