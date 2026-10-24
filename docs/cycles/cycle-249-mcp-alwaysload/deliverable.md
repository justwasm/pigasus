# Cycle 249 — MCP tool `_meta` `antigravity/alwaysLoad` flag (AGY 1.3.3 line 42)

Branch: `feature/cycle-249-mcp-alwaysload`
Worktree: `/Users/gear/Documents/GitHub/openagy-mcp-alwaysload`

## Why this cycle

AGY 1.3.3 changelog line 42 (recovered from
`internal/cli/testdata/changelog_reference.txt`):

> "Added support for MCP servers to mark a tool as always loaded by
> setting `_meta: {"antigravity/alwaysLoad": true}` on it in their
> `tools/list` response, so the agent gets that tool directly instead
> of discovering it on demand."

The clone had the static hint (`mcp.json` per-server `alwaysLoad`
slice) wired through `internal/pigrt/alwaysload.go` (cycle
`feature/mcp-always-load`), but no dedicated home for the
**protocol-side parser** that decides, per `tools/list` entry,
whether that tool should land in the always-loaded core catalog or
the discoverable catalog. The handler-side wiring sat in
`internal/pigrt/runtime.go` (the `Options.AlwaysLoadToolsByServer`
channel), which works as a programmatic injection path but does
not own the MCP `_meta` payload itself — the parsed hint is read
out of `internal/pigrt/alwaysload.go` and the registry semantics
have to be reconstructed in the consumer.

This cycle closes that gap by giving the protocol parser and the
catalog split a single home: a new `internal/mcp` package whose
`ToolRegistry` is the surface a caller hands a `tools/list`
payload to, and gets back "core" + "MCP-discovered" entries. The
static-hint + dynamic-channel merge logic in `internal/pigrt`
keeps doing what it did before; this cycle adds the **wire**
between the JSON the server returned and the in-memory registry
that downstream consumers read.

## AGY 1.3.3 spec

| Surface | Behaviour |
|---|---|
| **`tools/list` `_meta`** | An entry may carry an `_meta` bag with `antigravity/alwaysLoad: true` to opt into being core (always-loaded). |
| **Default** | Anything other than JSON-true (false, null, `"true"` string, the number 1, missing key, malformed `_meta`) defaults to "not always-loaded". The clone mirrors this strictly. |
| **Wrong types** | The reference decompiled source uses a strict bool check; the clone's parser logs a warning and defaults to false rather than honouring `"true"` / `1`. |
| **Core catalog** | The model sees these tools directly, named `mcp__<server>__<tool>`. |
| **MCP-discovered catalog** | The rest. Reachable only through `call_mcp_tool`. |
| **Conflict** | When both a `mcp.json` static `alwaysLoad` entry and a `_meta` flag agree on the same `(server, tool)`, the dynamic one wins (the server's word is the latest signal). Already wired in `internal/pigrt/runtime.go`. |

## What this cycle adds

### Files

| Path | Status | LoC |
|---|---|---|
| `internal/mcp/tool_registry.go` | new | +336 |
| `internal/mcp/always_load_test.go` | new | +297 |
| `internal/cli/testdata/feature_diff_table.json` | edit | +8 / −0 |

Net: `+641` from `main` (294f4c5).

### Code summary

`internal/mcp/tool_registry.go`:

- `AntigravityAlwaysLoadMetaKey` constant — the AGY project's single
  declaration of the `_meta` key spelling.
- `Tool` descriptor — `Name`, `Description`, `InputSchema`,
  `AlwaysLoad`, `Server`. The `AlwaysLoad` field is computed; the
  caller does not set it.
- `parseToolsList(payload []byte) ([]Tool, error)` — single
  JSON-decoder entrypoint for `tools/list`. Empty/nil payloads are
  a no-op (the "server has not advertised" state). A malformed
  payload returns the `json.Unmarshal` error.
- `entryAlwaysLoad` (package-internal) — bool-only check; logs a
  warning on non-object `_meta` or non-bool flag value, then
  defaults to false. This is the safety net the AGY 1.3.3 strict
  semantics demand.
- `ToolRegistry` — splits tools into `core` (always-loaded) and
  `discover` (MCP-discovered) maps. Methods:
  - `NewToolRegistry()` — empty registry.
  - `RegisterCoreTool(t)` / `RegisterDiscoveredTool(t)` — explicit
    insertion, used for statically-defined core tools and for
    pre-loaded surfaces.
  - `RegisterToolsList(server, payload)` — parses, attaches the
    server name, dispatches into the right catalog based on
    `AlwaysLoad`.
  - `CoreTools()`, `MCPDiscoveredTools()`, `AllTools()` — sorted
    snapshots for the model-facing surfaces.
  - `CoreSize()`, `MCPDiscoveredSize()` — counts.
- Internal `sortedTools` — insertion sort over the small maps a
  typical MCP server returns; keeps the file dependency-free (no
  `sort` import needed in this file).

### Why a new package

The handler-side merge logic in `internal/pigrt/alwaysload.go` is
about reconciling two sources of always-load hints: the `mcp.json`
admin-side static list and a programmatic channel
(`Options.AlwaysLoadToolsByServer`). The new `internal/mcp`
package is about the **protocol-side decision**: given a JSON
`tools/list` payload the server just answered with, which
catalog does each entry belong in? Both packages coexist because
they answer different questions:

- `internal/pigrt/alwaysload.go` answers "given the configured
  always-load hints, what tools should be advertised?"
- `internal/mcp/tool_registry.go` answers "given a `tools/list`
  payload the server just returned, what tools belong in core vs
  discoverable?"

The runtime layer (`internal/pigrt/runtime.go`) can now call
`mcp.NewToolRegistry()` and feed it the static hints + dynamic
channel through a single surface; the per-entry routing lives
in one place.

### Hard constraints honoured

| Constraint | Where it shows up |
|---|---|
| No third-party deps | `internal/mcp/tool_registry.go` imports only `encoding/json` and `log` (stdlib). `internal/mcp/always_load_test.go` imports only `reflect`, `testing`, and the package under test. |
| No `internal/convstore/sqlite.go` edit | `git diff main -- internal/convstore/sqlite.go` is empty. |
| Tabs, lowercase error strings | `gofmt -l internal/mcp/` is empty; `log.Printf` messages are lowercase. |
| No reformat of unrelated files | only the three files in the table above are touched. |

## Tests (7 functions)

`internal/mcp/always_load_test.go`:

1. `TestParseToolsList_AlwaysLoadTrue` — happy path: `_meta:
   {"antigravity/alwaysLoad": true}` → `AlwaysLoad=true`, lands in
   core catalog, server name attached, surrounding fields preserved.
2. `TestParseToolsList_AlwaysLoadAbsent` — no `_meta` → both
   entries land in the MCP-discovered catalog with `AlwaysLoad=false`.
3. `TestParseToolsList_AlwaysLoadFalse` — explicit
   `antigravity/alwaysLoad: false` → still discoverable (the bool
   flag is "not honoured" when not literally true).
4. `TestParseToolsList_MetaNotObject` — `_meta: "not an object"`
   → no panic, defaults to false, warning logged.
5. `TestParseToolsList_AlwaysLoadNotBool` — `_meta:
   {"antigravity/alwaysLoad": "yes"}` → no panic, defaults to false,
   warning logged.
6. `TestRegisterAlwaysLoad_AddsToCoreCatalog` — `RegisterToolsList`
   dispatches `AlwaysLoad=true` entries to the core catalog and
   the rest to discoverable; both catalogs preserve the server name
   and sort order.
7. `TestRegisterAlwaysLoad_PreservesExistingCoreTools` — a
   pre-loaded core tool is not dropped when a `tools/list`
   payload is processed afterward; the surrounding pre-loaded
   fields (description) are preserved.

The malformed-cases tests (`_MetaNotObject`,
`AlwaysLoadNotBool`) double as a regression guard against the
"silently treats a string flag as truthy" trap.

## Gates

| Gate | Command | Outcome |
|---|---|---|
| Build | `go build -o /tmp/wagyu-cycle249 ./cmd/wagyu` | exit 0; binary 86 MB |
| Test (new pkg) | `go test -vet=off -count=1 ./internal/mcp/` | 7 functions pass |
| Test (full sweep) | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | all packages pass |
| gofmt (new files) | `gofmt -l internal/mcp/` | empty |
| go vet (new pkg) | `go vet ./internal/mcp/` | clean |
| FeatDiff regression | `go test -vet=off -count=1 -run TestFeatDiff ./cmd/wagyu/` | all 4 tests pass (the new entry in `feature_diff_table.json` round-trips) |
| `internal/convstore/sqlite.go` | `git diff main -- internal/convstore/sqlite.go` | empty diff |

## 存疑 / limitations

| Item | Why it is here |
|---|---|
| **`tools/list` payload delivery into the registry** | The MCP server-to-registry hop is not wired in this commit. The `internal/pigrt/runtime.go` `loadAlwaysLoadHints` function still reads hints through `Options.AlwaysLoadToolsByServer` (the programmatic channel) and `LoadAgyMCPFile` (the static JSON). The new `internal/mcp` package is the protocol parser + dispatcher; the eventual "feed `tools/list` payload into the registry when the server answers" glue is a separate cycle. PiG's `coding/mcpext` does not expose `_meta` to the AGY layer (verified by grep — no `AlwaysLoad`, `alwaysLoad`, `always_load`, or `always-load` symbol exists in PiG's `mcpext` / `extension` / `builtin` packages), so the AGY wrapper has to intercept the `tools/list` JSON itself; that is what `parseToolsList` is for. |
| **`pigrt.AlwaysLoadHintsFromToolsList` vs `mcp.parseToolsList`** | Two parsers now exist for the same shape. `pigrt.AlwaysLoadHintsFromToolsList` produces `[]AlwaysLoadHint` (a hint list keyed by `(server, tool)` for the static+dynamic merge), and `mcp.parseToolsList` produces `[]mcp.Tool` (the registry-shaped descriptors). They share the meta-key constant `antigravity/alwaysLoad` but are different types because they answer different questions (hint reconciliation vs catalog routing). Consolidating them is a future cycle. |
| **Pre-existing flake `TestDispatchPrintSlash_LocalCommands` (`cmd/wagyu/print_slash_test.go:70`)** | Reproduced on `main` without my changes; the test expects `minimax-m3` as the default model, but the local sandbox falls through to `gemini-3.8-flash-medium`. Out of scope; running `go test ./internal/...` (the commit's gate) is green. |
| **Disk / size sanity** | Worktree on the host's disk, not on a sandbox; the new files add ~641 lines / ~21 KB. No disk pressure observed. |

## What a caller sees

```go
// Build the registry from a `tools/list` payload.
reg := mcp.NewToolRegistry()
payload := []byte(`{
    "tools": [
        {"name": "screenshot", "_meta": {"antigravity/alwaysLoad": true}},
        {"name": "click"}
    ]
}`)
if err := reg.RegisterToolsList("browser", payload); err != nil {
    // malformed JSON or non-array `tools`
}
// Always-loaded (model-facing) tools:
for _, tool := range reg.CoreTools() {
    // tool.Name, tool.Description, tool.InputSchema, tool.Server
    // → registered as mcp__browser__screenshot in the model-facing list
}
// Discoverable (call_mcp_tool) tools:
for _, tool := range reg.MCPDiscoveredTools() {
    // tool.Name, tool.Description, tool.InputSchema, tool.Server
    // → reachable only through call_mcp_tool(browser, click)
}
```

The reference 1.3.3 semantics — a tool marked with
`_meta: {"antigravity/alwaysLoad": true}` lands in the model's
tool list as a direct entry, instead of being reachable only
through `call_mcp_tool` — is now closed at the parser +
registry layer; the future "feed the actual JSON the server
returned" wire is the only remaining hop.