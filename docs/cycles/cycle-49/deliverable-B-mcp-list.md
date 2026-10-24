> **SUPERSEDED — the claims below are wrong.** This cycle-49 deliverable was
> written against an early, un-verified baseline. Measurements taken directly
> against the shipped v1.3.5 binary (`~/.local/bin/agy`, SHA-256
> `ebd71c4db14f79fca3fd0160332a38d9037146737422dcfc1f1d21ba01a13fff`) contradict it:
> the config is `$HOME/.gemini/config/mcp_config.json` with the `mcpServers`
> schema (not `$ANTIGRAVITY_HOME/mcp.json`); empty/missing prints
> `No MCP servers configured.` (not `no MCP servers configured (missing
> PATH/mcp.json)`); and the column widths are `max(4,maxlen)+2` /
> `max(6,maxlen)+2`, not the fixed 7/7/9 shown here.
> See `docs/cycles/cycle-96/deliverable-A-subcommand-parity.md` for the
> authoritative byte-verified behaviour. Original text retained below for history.

# Cycle 49 B — mcp list 1:1 deliverable

**Task:** `mcp list` 4-col table byte-level 1:1 with v1.3.5 binary
(`/tmp/agy-cmp-049/v135-9sub-baseline.txt`).

## What I changed

| File | Change |
|---|---|
| `internal/cli/jetski.go` (imports) | added `encoding/json`, `path/filepath`, and `internal/auth` (for `auth.MCPConfig` / `auth.MCPServer`) |
| `internal/cli/jetski.go` (`dispatch`) | added branch (5) for `mcp list` → 4-col table or "no MCP servers configured" notice |
| `internal/cli/jetski.go` (helpers) | 3 new functions: `v135MCPListConfigPath()`, `v135LoadMCPConfig()`, `v135RenderMCPList()`, plus `v135MCPCommandOrURL()` |
| `internal/cli/testdata/mcp.json` (new) | 2-server fixture (dummy=http, foo=stdio) matching the v1.3.5 binary baseline |
| `internal/cli/mcp_list_test.go` (new) | 7 `TestMCPList*` cases asserting exact 4-col output + edge cases |

## 1:1 verification

```
$ ANTIGRAVITY_HOME=/tmp/agy-c49b /tmp/agy-c49b/antigravity mcp list
NAME   TYPE   STATUS   COMMAND/URL
dummy  http   enabled  http://localhost:8080
foo    stdio  enabled  bar
```

Byte-identical to baseline (verified with `diff`).

## Format notes (cycle 48 v135_compat.go schema)

- Column widths: NAME=7, TYPE=7, STATUS=9 (left-aligned, **no
  separator** — `fmt.Fprintf("%-7s%-7s%-9s%s\n", ...)`). The
  trailing space inside `%-Ns` produces the visible gap; the gap
  varies (1-3 chars) because each value's own length determines
  it.
- Sort: alphabetical by server name (Go map iteration is
  non-deterministic; sorting matches baseline `dummy` < `foo`).
- `type=http` → COMMAND/URL = `URL` field.
- `type=stdio` → COMMAND/URL = `Command` (or `Command + Args` if
  args are present, joined with `" "`).
- Missing file OR empty `servers:{}` → single line
  `no MCP servers configured (missing PATH/mcp.json)\n`.

## Tests (7 total, all PASS)

```
=== RUN   TestMCPList_1to1          --- PASS
=== RUN   TestMCPList_Empty         --- PASS
=== RUN   TestMCPList_Missing       --- PASS
=== RUN   TestMCPList_StdioArgs     --- PASS
=== RUN   TestMCPList_Disabled      --- PASS
=== RUN   TestMCPList_Dispatch      --- PASS
=== RUN   TestMCPList_PathOverride  --- PASS
PASS
ok  github.com/chadgpt/openagy/internal/cli  0.101s
```

`TestMCPList_Dispatch` exercises the full `Parser.ExecuteArgs` path
through the jetski `dispatch()` intercept, so a future refactor that
moves the branch out of `dispatch()` will fail loudly.

## Why hard-code in jetski.go (not just patch `runMcpList`)

The recovered `runMcpList` in `internal/cli/dispatch.go` reads from
`$XDG_CONFIG_HOME/antigravity/mcp.json` (the v4 path) and prints
the `configured MCP servers (N):` header — **wrong** path **and**
**wrong format** vs the v1.3.5 binary. The cycle 49 sub-agent A
pattern (intercept in `jetski.go dispatch()` before the registered
sub-sub Run fires) gives us byte-exact output without touching
`parser_specs.go` / `dispatchRunXxx` / `runMcpList` plumbing, and
without leaving dead-code branches behind.

## Test override

`$ANTIGRAVITY_HOME` is honoured by `auth.AntigravityConfigHome()`,
so the test rig sets it to a `t.TempDir()` and copies the fixture
into `mcp.json`. No global state is mutated; the env var is scoped
via `t.Setenv` and auto-restored on test exit.
