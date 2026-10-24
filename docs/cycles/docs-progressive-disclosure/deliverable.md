# Progressive disclosure for AGENTS.md and README.md

## Scope

`AGENTS.md` had grown to **1,538 lines** and `README.md` to **556 lines**,
and both mixed the current state with history: PiG v0.5.1 notes next to a
v0.5.2 bump, a 102 MB binary size next to a 110 MB one, `cmd/wagyu` paths
next to `cmd/pigasus`, a 99-entry slash catalogue next to a 102-entry one,
and a RE chronology from cycle 11 next to the current command surface.

Both are now **one screen** plus a topic guide. Nothing was deleted — every
section was *moved*, and the stale references inside it were corrected.

| File | Before | After |
|---|---|---|
| `AGENTS.md` | 1,538 lines | **222 lines** |
| `README.md` | 556 lines | **218 lines** |
| `docs/guide/` | — | **13 files** (12 topics + an index) |

## What `AGENTS.md` keeps

Only what is cheap to read and expensive to rediscover:

1. **Orientation** — what the repository is, the three subtrees, the module
   path, the build/test commands.
2. **Ground rules** — never `go test ./...`; `gofmt` is not clean; a second
   agent shares the tree; the twelve visible subcommands are byte-pinned; the
   evidence grades; the report language.
3. **The clone in one map** — the two turn engines, the two transports, the
   one configuration file, the prompt, the two tool lists.
4. **Load-bearing invariants** — sixteen one-line facts, grouped as *agent
   loop* / *transports and tools* / *the workspace*. Each one is a trap that
   breaks something subtle when it is violated.
5. **Commands**, **this machine's toolchain** (four gotchas that cost real
   time), and a **topic → file** index.

## What `docs/guide/` holds

| File | Topic |
|---|---|
| `architecture.md` | the package map, the two turn engines, the two transports (new: the Mermaid diagram moved out of the README) |
| `agent-loop.md` | PiG's stream contract, the bridge, the five traps |
| `models.md` | model routing, `providers.json`, the one writer, the catalogue |
| `accounts.md` | the registry, the shared credential, the tier/quota cache |
| `prompt.md` | the system prompt, the skill/rule discovery, `--agent` |
| `tools.md` | 12 declarations vs 60 names, the result texts, the budget |
| `tui.md` | the transcript display, the picker, argument completion, bash mode |
| `compaction.md` | the two paths, the reference's numbers, the context window |
| `reference-drift.md` | the gate, the version-scoped artefacts, re-evidencing |
| `proxy.md` | the CloudCode proxy, the launcher, the capture directory |
| `reverse-engineering.md` | pclntab → seeds → Ghidra → reports, the formats |
| `environment.md` | the two RE environments, the toolchain, the gotchas |

The sections were moved **verbatim** — the split is mechanical, so a claim
that is wrong in the guide is wrong in the cycle report it came from.

## The stale references the move surfaced

The other agent's `cmd/pigasus → internal/cliapp` / `internal/printmode`
extraction had invalidated a good part of the old text:

| Stale | Now |
|---|---|
| `cmd/pigasus/print_pig.go` | `internal/printmode/print_pig.go` |
| `cmd/pigasus/tui_*.go`, `slash_panel_*.go`, `picker_*.go` | `internal/cliapp/…` |
| `cmd/pigasus/slash_compact_rows.go` | `internal/compaction/rows.go` |
| `cmd/pigasus/api_error.go` | `internal/apierror/api_error.go` |
| `cmd/pigasus/main_backend.go`, `main_proxy.go` | `internal/cliapp/run.go`, `internal/cliapp/main_dispatch.go` |
| `cmd/pigasus/composer_token.go` | `internal/tuilib/composer_token.go` |
| `internal/tuilib/picker.go` | still there — the *catalogue* side moved to `internal/cliapp/picker_model.go` |
| "99 entries … ↓ 94 more" | **102** entries; the tail count is derived (`internal/cliapp/picker_view.go`) |
| "the prompt is 70,853 B" | the *capture fixture* is 70,853 B; a bare `Build(Options{})` is 16,320 B |
| "32 v4 tool", "26 packages", "155,184 functions" | the current counts (60 advertised names, 116 internal packages, 161,286 seeds) |

Every relative markdown link in the three trees resolves (checked with a
script: 15 files, 0 broken), and every backticked repo path exists (the
remaining misses are runtime files such as `plugin.json` and `mcp.json`).

## Release surface

`scripts/publish-snapshot.sh`'s allowlist gains `docs/guide`, so the published
tree's new links resolve — the snapshot grows by ~80 KB. `bash -n` and the
dry run are clean.

## Verification

- `go build ./cmd/...` ✓ · `go test ./cmd/... ./internal/...` ✓ (green)
- `pigasus --help` → exactly the **twelve** visible subcommands ✓, the hidden
  groups (`provider`, `model`, `accounts`, `proxy`) still hidden ✓
- `publish-snapshot.sh -n` ✓ (the allowlist prints `docs/guide`)

## 存疑

1. **The guide is a snapshot of a tree that is moving.** The other agent is
   extracting packages into new ones right now (`internal/cliapp`,
   `internal/bashmode`, `internal/providerpanel`, …), so file-level references
   inside the guide will rot again. Package-level references are preferred
   where possible.
2. **The counts (102 catalogue entries, 116 packages, 161,286 seeds) are
   measurements, not gates.** Nothing watches them; they drift silently.

## Limitations

- **The split is mechanical, not editorial.** The guide inherits the old
  text's density — some sections are still reference notes rather than prose.
- **`README.md` is Chinese, `AGENTS.md` and the guide are English.** That was
  the repo's convention before and stays; the README links out to the guide
  rather than duplicating it.
- **The README no longer carries the Mermaid diagrams of the recovered
  surface** (the 32 v4 tools, the 6 gRPC services). They described the
  *recovered* snapshot, not the clone's current surface, and the clone's
  `--tool` / `--services` flags were removed to keep the flag surface
  byte-identical to the reference's — so keeping them in the README would
  have been advertising a surface that no longer exists.
