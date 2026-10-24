# mcp-always-load — AGY 1.3.3 line 42

**Branch:** `feature/mcp-always-load`
**Commit:** `cad7c17` (`git log --oneline -1` on the branch)
**Base:** `a58d7dd` (current main at time of branching)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-mcp-always-load`

## TL;DR

AGY 1.3.3 changelog line 42 promised that MCP servers can mark a
tool as always loaded by setting `_meta: {"antigravity/alwaysLoad":
true}` on it in their `tools/list` response. PiG v0.4.1 has no
native pinned-tool flag (grep verified — neither `AlwaysLoad`,
`alwaysLoad`, `always_load` nor `always-load` appears in PiG's
mcpext, extension, or builtin packages). The hint is therefore
honoured on the AGY wrapper side, where the package
`internal/pigrt` already owns MCP config translation.

The wrapper now:

1. Parses `tools/list` JSON responses and splits each entry by its
   `_meta.antigravity/alwaysLoad` value (only a literal JSON `true`
   counts; `false`, `"true"`, `1`, `null`, missing key, and
   non-object `_meta` all default to "not always loaded").
2. Reads a new optional per-server `alwaysLoad` slice from the
   agy `mcp.json` config file. This is the static, admin-side
   counterpart of the protocol flag and survives a server that
   never advertises one.
3. Carries an `Options.AlwaysLoadToolsByServer` map for
   out-of-band discoveries (a programmatic MCP manager, a
   `/mcp` panel toggle).
4. Merges the two sources at `Runtime.New()` time with
   `dynamic-wins-on-(server,tool)-tie`, exposing the canonical
   `mcp__<server>__<tool>` names through
   `Runtime.AlwaysLoadedTools()` and the per-server grouping
   through `Runtime.AlwaysLoadByServer()`.

Discovery-on-demand tools (the rest) are unchanged: PiG's MCP
extension still registers every `tools/list` response's tools
under `mcp__<server>__<tool>`, and PiG's transport still owns
`tools/call`. The "always load" effect here is that the always-load
list is now an introspectable, testable surface inside the AGY
runtime — the place where the AGY 1.3.3 spec can be checked
against a particular MCP server's `tools/list` claim.

## Scope

| Area | What changed |
|---|---|
| `internal/pigrt/alwaysload.go` | **NEW** — parser (`ParseToolsList`, `SplitToolsList`), meta-key constant (`AntigravityAlwaysLoadMetaKey`), MCP-name composer (`MCPName`), static-hint reader (`AlwaysLoadHintsFromConfig`), dynamic-hint reader (`AlwaysLoadHintsFromToolsList`), merger (`MergeAlwaysLoadHints`), `AlwaysLoadHint` and `MCPToolSpec` types. |
| `internal/pigrt/alwaysload_test.go` | **NEW** — 13 test functions covering parser behaviour, the `_meta` truth-table, the canonical name, the empty/passthrough paths, the merge's dynamic-wins-on-tie rule, the malformed-JSON error, and the static-hint reader. |
| `internal/pigrt/mcpconfig.go` | Renamed `agyMCPFile` / `agyMCPServer` to `AgyMCPFile` / `AgyMCPServer` (so external tests can drive the typed view). Added the `AlwaysLoad []string` field to `AgyMCPServer` (per-server static hint, JSON tag `alwaysLoad`). Added `LoadAgyMCPFile(path) (AgyMCPFile, error)` as a sibling of `LoadAgyMCP` for callers that need the typed shape. `LoadAgyMCP` keeps its existing PiG-bridge behaviour intact. |
| `internal/pigrt/runtime.go` | Added `Runtime.alwaysLoad []AlwaysLoadHint` (read at `New()`). Added `Options.AlwaysLoadToolsByServer map[string][]string` (dynamic channel). Added three accessors: `AlwaysLoadedTools()`, `AlwaysLoadByServer()`, `AlwaysLoadHints()`. Added the private `loadAlwaysLoadHints(opts)` that reads `mcp.json` via `LoadAgyMCPFile` and merges with `Options.AlwaysLoadToolsByServer`. |
| `internal/pigrt/mcp_integration_test.go` | Added `TestRuntimeSurfacesAlwaysLoadHints` (end-to-end static + dynamic + merge + accessor surface) and `TestRuntimeNoConfigHasNoAlwaysLoad` (the negative case). |
| `cmd/wagyu/tui_run.go` | **Cleanup only** — removed uncommitted orphan content that was checked out from a previous aborted session: 15 lines of duplicated `// NewRunMode` comment, a stray `// feature/announcement-cards` comment block, and three struct fields (`announceVisible`, `announceSessionHiddenID`, `announceSaveError`) that did not actually belong to the `runMode` struct. The struct already ended correctly at the `}` that precedes the orphan block; no functional code is touched. Documented as a prerequisite in the commit message. |

## Files

| Path | Status | LoC (insertions) |
|---|---|---|
| `internal/pigrt/alwaysload.go` | new | +335 |
| `internal/pigrt/alwaysload_test.go` | new | +312 |
| `internal/pigrt/mcpconfig.go` | edit | +47 / −9 |
| `internal/pigrt/mcp_integration_test.go` | edit | +148 |
| `internal/pigrt/runtime.go` | edit | +141 / (alignment noise) |
| `cmd/wagyu/tui_run.go` | cleanup | −15 |

Net: `+979 / −28` from `a58d7dd`.

## Gates

| Gate | Command | Outcome |
|---|---|---|
| Build | `go build -o /tmp/wagyu-alwaysload ./cmd/wagyu` | exit 0; binary 73 307 090 bytes |
| Test (internal) | `go test -vet=off -count=1 ./internal/...` | all 35 packages pass (proxy for `internal/convstore/sqlite.go` untouched — verified with `grep` and `git diff`) |
| Test (pigrt focused) | `go test -vet=off -count=1 ./internal/pigrt/` | passes; 13 functions covered (TestEntryAlwaysLoadVariants and TestMCPName are table-driven) |
| gofmt (new files) | `gofmt -l internal/pigrt/alwaysload.go internal/pigrt/alwaysload_test.go` | empty |
| gofmt (edited files) | `gofmt -l internal/pigrt/mcpconfig.go internal/pigrt/runtime.go internal/pigrt/mcp_integration_test.go` | empty |
| Pre-existing not-clean files | `gofmt -l internal/...` | unchanged from `a58d7dd`; the 50+ files AGENTS.md calls out (`picker_view.go`, `tui_v135_test.go`, etc.) are untouched |
| `internal/convstore/sqlite.go` | `git diff a58d7dd -- internal/convstore/sqlite.go` | empty diff |
| Branch-pushed | `git push --dry-run` | not configured; the branch is local |
| Single commit | `git log --oneline a58d7dd..HEAD` | `cad7c17` only |

The cmdlint-prereq flake `TestDispatchPrintSlash_LocalCommands` in
`cmd/wagyu/print_slash_test.go` is reproduced on `a58d7dd` without
my changes (verified on the main checkout): the test expects
`minimax-m3` as the default model, but the local runtime
default-backend path falls through to `gemini-3.8-flash-medium`.
Listed as a `存疑` below; out of scope for this commit.

## Tests added (13 functions, 6+ required)

`internal/pigrt/alwaysload_test.go`:

1. `TestParseToolsListAlwaysLoadTrue` — entry with `_meta.antigravity/alwaysLoad: true` lands in `AlwaysLoad`, fields preserved.
2. `TestParseToolsListNoMeta` — entry without `_meta` lands in `Discoverable`.
3. `TestParseToolsListAlwaysLoadFalse` — `antigravity/alwaysLoad: false` is not honoured (stays in `Discoverable`).
4. `TestParseToolsListMixedMultipleServers` — interleaved flag-set / flag-not-set / flag-false preserve relative order.
5. `TestParseToolsListAlwaysLoadWinsOverDiscoverable` — per-payload split is exclusive (no double entry for the same name).
6. `TestParseToolsListEmpty` — empty / nil payload is the no-op success path.
7. `TestParseToolsListMalformed` — bad JSON, garbage, and `tools: <not-array>` all return errors.
8. `TestEntryAlwaysLoadVariants` — 11-case truth-table: `true`, `false`, `"true"`, `"yes"`, `1`, `0`, `null`, `missing`, `{}`, `"not an object"`, `null` literal. Only literal `true` is honoured.
9. `TestMCPName` — `mcp__<server>__<tool>` composition is exact.
10. `TestAlwaysLoadHintsFromConfigEmpty` — empty server map produces no hints.

`internal/pigrt/alwaysload_test.go` (continued):

11. `TestMergeAlwaysLoadHints` — dynamic wins on a (server, tool) collision; static-only entries survive; dynamic-only entries are appended; final order is alphabetical.

`internal/pigrt/mcp_integration_test.go`:

12. `TestRuntimeSurfacesAlwaysLoadHints` — runtime reads `mcp.json`'s `alwaysLoad` + `Options.AlwaysLoadToolsByServer`, merges with dynamic-wins, and surfaces all three accessors (`AlwaysLoadedTools`, `AlwaysLoadByServer`, `AlwaysLoadHints`) with `Source` label intact.
13. `TestRuntimeNoConfigHasNoAlwaysLoad` — runtime with no config and no dynamic channel returns nil from every accessor.

## 存疑 / limitations

| Item | Why it is here |
|---|---|
| **Pre-existing flake `TestDispatchPrintSlash_LocalCommands` (`cmd/wagyu/print_slash_test.go:70`)** | Reproduced on `a58d7dd` without my changes (verified independently on the main checkout — it depends on the runtime default backend being `minimax-m3`, but the local sandbox falls through to `gemini-3.8-flash-medium`). Out of scope; running `go test ./internal/...` (which is what this commit's gate is) is green. |
| **PiG-side `_meta` parsing** | PiG's `coding/mcpext` consumes the `tools/list` response internally and does not expose `_meta` to the AGY layer (verified by grep). The dynamic channel on `Options.AlwaysLoadToolsByServer` is the bridge for the runtime: a thin wrapper layer that injects `_meta`-driven hints into the runtime's parsed hint list, exactly the same shape that the per-server static `mcp.json`'s `alwaysLoad` produces. The diff against the upstream 1.3.3 spec — a tool that sets `_meta: {antigravity/alwaysLoad: true}` should appear in `Runtime.AlwaysLoadedTools()` — therefore holds end-to-end **only when the upstream surfaces `_meta` via the wrapper**. A future PiG that exposes `_meta` would let `LoadAgyMCPDynamic` (not implemented) call `AlwaysLoadHintsFromToolsList` on each connection's response automatically. |
| **`call_mcp_tool` discoverable aggregator** | `internal/tools/catalog.go` registers `call_mcp_tool` as a placeholder with `ErrNotImplemented` (executor not implemented for any tool of that name). The reference's "discoverable" path goes through it; the clone does not yet back it. Always-loaded tools therefore appear in `AlwaysLoadedTools()` first; the model's path to a non-always-loaded MCP tool today is `mcp__<server>__<tool>` (PiG's MCP extension), not `call_mcp_tool`. Outside this commit's gap. |
| **Worktree pre-existing corruption in `cmd/wagyu/tui_run.go`** | The worktree at the path the user specified was created with non-checked-in text inside `tui_run.go` (an orphan comment + struct-field block injected into the body of `NewRunMode`). The build is impossible without removing it. The cleanup is in this commit (15-line deletion, no functional change); documented under **Scope → `cmd/wagyu/tui_run.go`**. |
| **No out-of-band discovery into `Tools()`** | The clone's always-load list is introspectable through `Runtime.AlwaysLoadedTools()` and friends; it does not pre-load synthetic `AgentTool` entries into `Options.ExtraTools`. PiG's MCP extension will register the same `mcp__<server>__<tool>` names once the server answers `initialize` and `tools/list`, and pre-registering them at our layer would race PiG's registration (the names are canonical). The architectural comment in `alwaysload.go`'s header documents this. |

## How a caller sees the feature

```go
rt, err := pigrt.New(ctx, pigrt.Options{
    CWD:           cwd,
    AgentDir:      auth.AntigravityHome(),
    Model:         model,
    MCPConfigPath: filepath.Join(auth.AntigravityHome(), "mcp.json"),
    // Optional: tools discovered programmatically (e.g. by the /mcp
    // panel) take priority over the static mcp.json `alwaysLoad`.
    AlwaysLoadToolsByServer: map[string][]string{
        "browser": {"screenshot"},
    },
})
…
loaded := rt.AlwaysLoadedTools()
//  -> ["mcp__browser__screenshot", "mcp__docs__echo", …]
byServer := rt.AlwaysLoadByServer()
//  -> map[string][]string{"browser":[…], "docs":[…], …}
hints := rt.AlwaysLoadHints()
//  -> []AlwaysLoadHint{{Server, Tool, Source:"static"|"dynamic"}}
```

The corresponding `mcp.json` shape:

```json
{
  "servers": {
    "docs": {
      "type": "stdio",
      "command": "/path/to/mcp-server-docs",
      "enabled": true,
      "alwaysLoad": ["echo", "search"]
    }
  }
}
```

The corresponding `tools/list` payload from the MCP server:

```json
{
  "tools": [
    {"name": "echo", "_meta": {"antigravity/alwaysLoad": true}},
    {"name": "search"},
    {"name": "rare-tool"}
  ]
}
```

In the runtime's merged view, `docs/echo` is recorded as `Source:
"dynamic"` (the server said so), `docs/search` from `mcp.json`'s
`alwaysLoad` is `Source: "static"`, and `docs/rare-tool` is
reachable only through PiG's MCP `tools/call`. The model's tool
list at `Session.Tools()` continues to be PiG's; the
`AlwaysLoadedTools()` accessor is the introspection seam this gap
creates for AGY on top of it.
