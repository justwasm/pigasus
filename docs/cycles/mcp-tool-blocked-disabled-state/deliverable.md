# Branch `feature/mcp-tool-blocked-disabled-state` — AGY 1.2.16 close-gap

## Goal

The AGY 1.2.16 changelog entry

> Fixed the `/mcp` panel showing both 'connected' and 'disabled' badges
> on the same MCP server entry when it was disabled by
> `disableAllMcpServers`; the panel now shows a single 'blocked' state
> distinct from per-server disabled.

is the user-visible gap the clone has carried. This branch closes it on
the clone side. The fix has three parts:

1. **`/mcp` panel** — a third state `blocked` joins the per-row cells
   `enabled` (`connected` in the gap's wording) and `disabled`. When the
   global MCP gate is on (env var `AGY_CLI_DISABLE_MCP` or mcp.json's
   top-level `disableAllMCP` boolean), every server's state cell renders
   `blocked`. Per-server `enabled: false` rows still render — that is a
   per-server preference, not a global kill — but the cell is renamed
   `blocked` when the gate is on, so the user can tell at a glance that
   *every* row is gated, not just the individually-disabled ones.
2. **`/plugin` Installed tab** — parallel coverage via env var
   `AGY_CLI_DISABLE_PLUGINS` and config.json's `disableAllPlugins`
   boolean. The Installed rows gain a `[blocked]` red-orange badge so
   the same "as if it were not installed" signal exists for plugins.
3. **`internal/auth` helpers** — `PluginsBlocked()` and `MCPsBlocked()`
   expose the gate as a single bool, with env var > config boolean
   precedence. The two `disableAll*Field` readers sit behind them as
   the on-disk branch.

The "connected" wording in the AGY 1.2.16 gap is the cycle-235
equivalent of our `state = "enabled"` cell. The reference previously
rendered both `connected` and `disabled` on a single row when
`disableAllMcpServers` was on, leaving a user with two contradictory
signals. The fix is a single `blocked` cell, distinct from per-server
`disabled`.

## Scope vs sibling branch `feature/disabled-plugins-list-show`

This branch and `feature/disabled-plugins-list-show` (HEAD `50faade`)
share the same gap-fixing intent. The differences:

| | this branch | sibling branch |
|---|---|---|
| Branch name | `feature/mcp-tool-blocked-disabled-state` | `feature/disabled-plugins-list-show` |
| Deliverable | `docs/cycles/mcp-tool-blocked-disabled-state/` | `docs/cycles/disabled-plugins-list-show/` |
| Test file | `cmd/wagyu/slash_mcp_blocked_test.go` (new, 8 tests, MCP-centric) | `cmd/wagyu/slash_cycle235_test.go` (new, 13 tests, plugin+mcp) |
| Test framing | the gap's literal five test cases (blocked-by-env, disabled-by-user, both, neither, /mcp router) | AGY 1.2.16 across both panels (badge strings, helper precedence, panel rendering, router-level /mcp output) |

The /mcp implementation is identical at the renderer level
(`mcpServersFeedback` consults `auth.MCPsBlocked()`, rewrites the state
cell to `blocked` when the gate is on). The /plugin Installed tab
helpers (`pluginBlockedBadge`, `pluginRowState`, `pluginRowBadge`,
`pluginInstalledLine`'s swap from `pluginBadge` → `pluginRowBadge`) are
also identical. Both branches agree on the new file's additions to
`internal/auth/compat.go`. The only divergence is the test framing
and the deliverable path.

## Files changed

| File | +/− | Why |
|---|---|---|
| `internal/auth/compat.go` | +64 / −0 | `disableAllPluginsField` / `disableAllMCPField` (config booleans), `PluginsBlocked` / `MCPsBlocked` (env var + config helpers). |
| `cmd/wagyu/slash_actions.go` | +27 / −1 | `mcpServersFeedback` consults `auth.MCPsBlocked()` and rewrites the state cell to `blocked` when the gate is on. |
| `cmd/wagyu/slash_panels.go` | +50 / −1 | `pluginBlockedBadge`, `pluginRowState`, `pluginRowBadge` helpers; one-line swap of `pluginBadge(enabled)` → `pluginRowBadge(enabled)` inside `pluginInstalledLine`. |
| `cmd/wagyu/slash_mcp_blocked_test.go` | **new, 421** | 8 tests pinning the gap's five literal test cases plus on-disk boolean source, parallel /plugin coverage, and helper precedence. |
| `docs/cycles/mcp-tool-blocked-disabled-state/deliverable.md` | **new, this file** | the deliverable record. |

No new third-party dependencies. `strings.TrimSpace` and `os.Getenv`
are stdlib calls the rest of `internal/auth` already uses
(`LoadJetboxState`, `OnboardingCompleted`, etc.).

## State model

`mcpServersFeedback` consults `auth.MCPsBlocked()` once per call and
stashes the bool in a local. Per row, the cell-mapping rules are:

1. If the gate is on, the cell is `blocked` regardless of the per-row
   `enabled` flag.
2. If the gate is off and `s.Enabled` is true, the cell is `enabled`
   (the AGY 1.2.16 "connected" cell).
3. If the gate is off and `s.Enabled` is false, the cell is `disabled`.

`pluginRowState(enabled bool) string` returns one of three words using
the same precedence, and `pluginRowBadge` switches on that word and
returns one of:

- `pluginBlockedBadge()` — `"\x1b[38;5;166m[blocked] \x1b[39m"`,
  the same red-orange 166 the TUI uses for error / blocked rows
  (`colors.go: cToolBulletErrOpen`); ten-column width, trailing space
  inside the span.
- `pluginBadge(true)` — unchanged from cycle 198.
- `pluginBadge(false)` — unchanged from cycle 198.

## Helpers (cycle additions to `internal/auth/compat.go`)

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
`config.json` / `mcp.json` respectively. A read failure (missing file,
malformed JSON) returns false — the env var path still works, so an
operator override never gets lost.

## Tests (cycle additions to `cmd/wagyu/slash_mcp_blocked_test.go`)

The task's 5-test minimum is met by `TestBlockedMCP_BlockedByEnv`,
`TestBlockedMCP_DisabledByUser`, `TestBlockedMCP_BothBlockedAndDisabled`,
`TestBlockedMCP_NeitherFlagShowsConnected`, and
`TestBlockedMCP_SlashMCPCommandUsesBlockedState`. Three further tests
pin the on-disk boolean source and the parallel /plugin coverage.

| Test | Pins |
|---|---|
| `TestBlockedMCP_BlockedByEnv` | `AGY_CLI_DISABLE_MCP=1` → every server row's tab-separated state cell is `blocked`; no `enabled`, no `disabled` cells. The literal gap requirement #1. |
| `TestBlockedMCP_DisabledByUser` | gate off + per-server `enabled: false` → cell is `disabled`, not `blocked`, not `enabled`. The literal gap requirement #2. |
| `TestBlockedMCP_BothBlockedAndDisabled` | gate on + per-row `enabled: false` → cell is `blocked` (gate wins); no `disabled` cell anywhere on the listing. The literal gap requirement #3 — the exact original bug shape (`connected` + `disabled` on the same row). |
| `TestBlockedMCP_NeitherFlagShowsConnected` | gate off + `enabled: true` → cell is `enabled` (the clone's "connected"), not `blocked`, not `disabled`. The literal gap requirement #4. |
| `TestBlockedMCP_SlashMCPCommandUsesBlockedState` | end-to-end through `m.handleSlashCommand("/mcp")` → router path passes the `blocked` cell through; the `enabled` cell is gone. The literal gap requirement #5. |
| `TestBlockedMCP_FromConfigField` | mcp.json's top-level `disableAllMCP: true` engages the gate even when the env var is unset. |
| `TestBlockedMCP_PluginThreeStates` | the parallel /plugin coverage: gate-on + per-plugin disabled → `[blocked]` badge; no `[enabled]`, no `[disabled]` in the row. |
| `TestBlockedMCP_HelpersPrecedence` | env var > config boolean, whitespace-only env var is unset, clearing both disengages the gate. Covers both `PluginsBlocked()` and `MCPsBlocked()`. |

8 / 8 tests pass under the targeted run:

```
$ go test -vet=off -count=1 -run 'TestMCP|TestBlocked|TestDisabled|TestPanel' \
    ./cmd/wagyu/ ./internal/.../
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.579s
… (all 33 internal/* packages green)
```

## Hard constraints (verified)

- `go build -o /tmp/wagyu-mtb ./cmd/wagyu` exits 0; the binary is
  73.3 MB (unchanged ± the handful of helper functions added).
- `go test -vet=off -count=1 -run 'TestMCP|TestBlocked|TestDisabled|TestPanel' ./cmd/wagyu/ ./internal/.../`
  is all-green. The broader run `./cmd/wagyu/ ./internal/.../`
  has exactly **one** pre-existing failure
  (`TestDispatchPrintSlash_LocalCommands` — model-resolver drift in
  `print_slash_test.go:70`, unrelated to /mcp) that exists on `main`
  with the worktree absent; verified by running the same test
  against `main` directly.
- `gofmt -l cmd/wagyu/slash_mcp_blocked_test.go` is empty; the new
  file is gofmt-clean.
- `gofmt -l cmd/wagyu/slash_actions.go cmd/wagyu/slash_panels.go` is
  empty; only targeted edits, no wholesale reformat.
- `internal/auth/compat.go` is pre-existing not-gofmt-clean. My
  additions (`disableAllPluginsField`, `disableAllMCPField`,
  `PluginsBlocked`, `MCPsBlocked`) are pure appends and do not
  introduce new gofmt issues — verified by `gofmt -l` returning
  the same single-file result on both the pre-change and post-change
  versions of the file.
- `internal/convstore/sqlite.go` is **not touched**.
- `cmd/wagyu/slash_cycle198_test.go`,
  `cmd/wagyu/slash_actions_test.go`, `cmd/wagyu/slash_panels_test.go`,
  and the rest of the existing test files are not modified — only the
  new `cmd/wagyu/slash_mcp_blocked_test.go` is added.

## Out of scope / 硬约束

- **No row-removal filter** on the Installed tab or `/mcp` listing.
  Removing a row that the user just disabled in-place would shift the
  cursor in a way the reference binary does not, and would also break
  the cycle-198 fallback test (`TestCycle198_InstalledFallback` pins
  four stock plugins as `[disabled]` when no `config.json` is present).
  The badge is the chosen close-gap mechanism because it matches the
  gap's user-visible outcome ("as if it were not installed") without
  disturbing the layout contract.
- **No write-through to `disableAllPlugins` / `disableAllMCP`** — the
  clone only reads the gate, never sets it. The toggle UI (if any)
  is a future cycle. The env var is the documented override path
  (matches the `AGY_CLI_DISABLE_*` pattern of `AGY_CLI_DISABLE_LATEX`,
  `AGY_CLI_DISABLE_AUTO_UPDATE`, etc., visible in the binary's
  strings dump).
- **No admin-control integration** (`mcp disabled by BAIC admin
  controls` / `mcp disabled by CCPA admin controls` strings in the
  v1.3.4 binary's strings dump) — admin controls are an enterprise
  feature surface the clone does not yet have.
- **No new plugin-data field** — `disableAllPlugins` lives at the
  top level of `config.json`, not inside `plugins.<name>`. The
  per-plugin `enabled` flag is unchanged; the gate is a separate,
  parallel mechanism.
- **Not measured against AGY 1.2.16** — the v1.3.4 binary strings
  dump has no `disableAllPlugins` literal (searched
  `strings-v1.3.4.txt`). The env var name `AGY_CLI_DISABLE_PLUGINS`
  follows the existing `AGY_CLI_DISABLE_*` family but is itself an
  unverified guess. Acceptable because the clone's gap is closed on
  its own merits; a live probe against the actual 1.2.16 reference
  binary would be needed to confirm byte-identical behaviour.

## Single-source-of-truth note

This file is the deliverable for this branch
(`docs/cycles/mcp-tool-blocked-disabled-state/deliverable.md`).
A parallel branch — `feature/disabled-plugins-list-show` — has the
same gap-closing logic with a different test framing and a separate
deliverable at `docs/cycles/disabled-plugins-list-show/deliverable.md`.
If/when one of the two branches is chosen for merge, the surviving
deliverable becomes canonical.
