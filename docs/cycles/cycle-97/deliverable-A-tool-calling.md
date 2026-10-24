# Cycle 97 A — tool-calling parity deliverable

**Task:** make the recovered `agy` clone's **agent loop able to call tools**.
Before this cycle, asking the clone "what time is it" produced a guessed
answer: the loop had no tool surface at all. The gap was challenged as
"完整对标参考实现" — full parity with the reference, not a minimal demo.

**Reference (1.3.5):** `~/.local/bin/agy`, Mach-O arm64, 180,917,280 bytes,
SHA-256 `ebd71c4db14f79fca3fd0160332a38d9037146737422dcfc1f1d21ba01a13fff`.
A pristine copy of that build still exists at `/tmp/agycmp/ref/agy` and at
`~/.local/bin/agy.bak-1.3.5-*`.

> **⚠ Reference-binary version drift during this cycle.** While this cycle was
> in progress the installed reference **self-updated 1.3.5 → 1.3.6**. See
> "Reference drift" below before trusting any address in this document.

## Result

The four broken layers are now wired end to end, with tests:

```
go build ./cmd/antigravity          # ok
go vet ./internal/... ./cmd/antigravity/   # clean
go test ./internal/tools ./internal/agent ./internal/backend   # ok
```

What now works: the advertises tool set is sent on every request, the loop
executes model-requested tool calls, feeds the results back as `tool`
messages, and re-streams — bounded by an iteration budget. `run_command` is a
real executor, so "run `date`" is genuinely executed.

## The gap (all four layers were broken)

| # | Layer | Evidence |
|---|---|---|
| 1 | Agent loop | `internal/agent/loop.go` built `backend.StreamRequest{Model, SystemPrompt, Messages, MaxTokens, Temperature}` — **no tools**; the loop only forwarded `chunk.Text` and never acted on a tool call. |
| 2 | Protocol | `internal/backend/client.go` — `StreamRequest`/`Message` had no tool fields; `StreamChunk` had only `Text/Done/Error/Meta`. There was nowhere for a tool call to live. |
| 3 | Wire | `internal/backend/agw_k0s.go` POSTed `{model, messages, stream, …}` with no `tools`, and only read `choices[].delta.content`. |
| 4 | Registry | `internal/tools` was imported by **nothing** (`grep -rl 'openagy/internal/tools' --include='*.go' . | grep -v _test` → empty); `RegisterAll` was called only from its own test. |

The one runtime tool list in the tree sat behind a `--tools` flag in
`cmd/antigravity/main.go` — a flag that is **not** in `StandardFlags()`, so
`agy --tools` fails with `flags provided but not defined: -tools` (rc 2) on
both binaries. The list was unreachable.

## The authoritative tool surface

`printmode.buildInitPayload` (`0x1028b8300` on the 1.3.5 build) calls
`advertisedTools` (`0x1028b85f0`), then `sort.Strings`, then consults
`store.Manager.EffectivePermissionMode`. `advertisedTools` filters a global
tool registry against a global name list. (Ghidra's address for that global
in the 1.3.5 decompile is unreliable — the bytes there are a string blob, not
a slice header — so the list was recovered from the **pclntab** instead.)

The tool family is the `*ToolConverter` set in
`google3/third_party/jetski/cortex/tools/tools`, plus
`core/tools/tools.New*Tool`. It is **unchanged between 1.3.5 and 1.3.6**
(verified by regenerating seeds from the 1.3.6 binary:

```
cd rea-go-seeds && go run . ~/.local/bin/agy /tmp/seeds136
# gosym debug: funcs 160385        (vs 155,184 for 1.3.5)
```

). Every advertised name in `internal/tools/catalog.go` was then checked
against the binary's string table.

## The earlier "tool set" was invented

`internal/tools/tools.go` advertised 42 names (`read_file`, `create_file`,
`list_dir`, `apply_patch`, …). **None of them appear anywhere in either
strings dump** — they were a cycle-29 invention that never matched the
binary. This cycle replaces them with recovered names; the old registry is
left in place for its 1.3.3-compat callers but is no longer advertised.

## What is implemented

**Transport (`internal/backend`)**
- `ToolDecl`, `ToolCall` types; `StreamRequest.Tools`;
  `StreamChunk.ToolCalls`; `Message` gains `ToolCalls` / `ToolCallID` / `Name`.
- `agw_k0s.go`: serialises an OpenAI `tools` array (key omitted when empty, so
  a no-tools request is byte-identical to the pre-cycle body); reassembles
  fragmented `delta.tool_calls` frames by `index`; echoes assistant
  `tool_calls` and `tool` results back on the next turn.

**Loop (`internal/agent`)**
- `Loop.Send` is now a real multi-turn loop: stream → if the model requested
  tools, record the assistant turn, execute each call via an injected
  `ToolRunner`, append `tool` results, stream again. Bounded by
  `MaxToolIterations` (default 8). A `nil` runner preserves the old
  single-turn behaviour (calls surfaced to the handler, never executed).

**Catalog (`internal/tools/catalog.go`)**
- `Catalog(ws)` builds the recovered advertised set. Names with a real
  executor (`run_command`, `view_file`, `write_to_file`, `replace_file_content`,
  `list_dir`) are backed by it; the rest are placeholders that return
  `ErrNotImplemented` when called.
- `Registry.Dispatch` renders a tool result as text for the model.

**CLI (`cmd/antigravity/main.go`)**
- Print mode advertises the catalog (sorted by name, mirroring the reference's
  `sort.Strings`) and installs a `registryRunner`.

## Verification

- `internal/agent/loop_tools_test.go` — 4 tests: tool-call-then-final-answer,
  single-turn when no calls, nil-runner contract, iteration budget.
- `internal/backend/agw_k0s_tools_test.go` — 2 tests: `tools` serialisation +
  fragmented tool-call reassembly + `tool` message round-trip; `tools` key
  omitted when none advertised.
- `internal/tools/catalog_test.go` — 4 tests: reference names present,
  invented names absent, `run_command` actually executes, aliases delegate to
  the real filesystem executors, unimplemented tools fail loudly.

## 硬约束 / 存疑 / limitations

- **Wire protocol is not the reference's.** The reference speaks Cortex gRPC
  with a protobuf tool schema; the clone speaks an OpenAI-shaped HTTP stream
  to `agw.k0s.io`. Tool **names** and **user-visible behaviour** can match;
  the byte-level tool-schema encoding cannot, without a Cortex endpoint.
- **Per-tool name strings are only partially pinned.** Names confirmed as
  standalone strings: `run_command`, `view_file`, `write_to_file`, `list_dir`,
  `grep_search`, `search_web`, `read_url_content`, `read_resource`,
  `list_resources`, `command_status`, `send_command_input`, `manage_task`,
  `send_message`, `notebook_edit`, `generate_image`, `wait`, `finish`,
  `schedule`, `ask_question`, `ask_permission`, `ask_custom_permission`,
  `notify_user`, `view_content_chunk`. Not confirmed as standalone strings
  (kept on converter evidence): `find_by_name`, `codebase_search`,
  `replace_file_content`, `multi_replace_file_content`, `sed_file`,
  `manage_inbox`, `notebook_execution`, `list_permissions`, `call_mcp`.
- **Permission modes are not implemented.** The reference gates the advertised
  set (and each call) through `store.Manager.EffectivePermissionMode`; the
  clone advertises everything unconditionally.
  → **Superseded by cycle 98** (`docs/cycles/cycle-98/deliverable-A-permissions-and-tools.md`):
  `--mode` validation and a `internal/permissions` policy engine now exist.
- **Tool confirmation UI is not implemented.** The reference has
  `RespondToToolConfirmation` / `SetToolPermissionMode`; the clone auto-runs.
  → **Partly superseded by cycle 98**: the *headless* confirmation behaviour
  (soft-deny with the recovered text + end-of-run notice) is implemented; the
  interactive TUI flow is still absent.
- **Only `run_command` (plus fs aliases) is real.** The ~28 remaining
  advertised tools return `ErrNotImplemented`. This is deliberate — they are
  advertised so the surface matches, and fail loudly when called.
  → **Superseded by cycle 98**: 26 of the 32 advertised tools are real; only the
  6 network/MCP tools still return `ErrNotImplemented`.
- **No end-to-end run against a live model was performed.** `agw.k0s.io` may
  or may not honour the `tools` parameter; the tool-call parsing is proven
  against a stub server, not against a model that actually emits tool calls.
- The schemas in the catalog are reconstructions (the reference marshals Go
  structs at runtime — e.g. `run_command: failed to marshal parameter
  schema`), not byte-recovered.

## Reference drift

During this cycle `~/.local/bin/agy` **self-updated to 1.3.6**:

| | value |
|---|---|
| when | 2026-10-16 04:35 CST (mtime) |
| old build | 180,917,280 B, SHA `ebd71c4d…` (1.3.5) |
| new build | 183,407,440 B, SHA `827ba22b…` (1.3.6, `agy --version` → `1.3.6`) |
| preserved | `~/.local/bin/agy.1792096504834754000.old`, `~/.local/bin/agy.bak-1.3.5-*` |

`/tmp/agycmp/ref/agy` is still 1.3.5 (SHA `ebd71c4d…`), so the cycle-96
subcommand harness still measures 1.3.5.

**This matters because every address in `out/local-macos/` (seeds, `dec/*.c`,
the `0x1028b8300` / `0x1028b85f0` references above) is a 1.3.5 address, and
Go addresses shift on every release.** The 1.3.6 pclntab has 160,385 functions
vs 155,184 — +5,201.

Set `AGY_CLI_DISABLE_AUTO_UPDATE=1` before invoking the reference to avoid
another silent replacement.

## Retarget to 1.3.6

**Decision: 1.3.6 is now the parity target.** The head-to-head harness
(`/tmp/agycmp/harness136.sh`, same 53 cases) drove the change:

| reference | MATCH | DIFF |
|---|---|---|
| 1.3.5 (before) | 52 | 1 (`update`) |
| **1.3.6 (after)** | **53** | **0** |
| 1.3.5 (after, for the record) | 49 | 4 — the four deliberate retarget deltas |

Three real drifts were found and fixed (the fourth, `update`, was already an
intentional no-network divergence):

1. **`--version`** printed `1.3.5`. Introduced `cli.ReferenceVersion` as the
   single source of truth; `cmd/antigravity`, the TUI banner/metadata rows, the
   `/changelog` slash stub and the hidden `login`/`auth` messages all read it.
2. **`plugin validate`** changed semantics. v1.3.5 `os.Stat`-ed the joined
   manifest (`stat <path>/plugin.json: ...`); v1.3.6 `os.ReadDir`s the path
   first, so a missing or non-directory argument surfaces as the raw syscall
   error (`open <path>: no such file or directory` / `not a directory`), and a
   present-but-absent manifest yields the fixed string `file does not exist`.
   v1.3.6 also rejects malformed JSON (`invalid plugin.json: <err>`) where
   v1.3.5 silently reported `[ok]`. All three were reproduced and verified
   against 8 probe cases (no-arg, empty dir, top-level manifest,
   `.antigravity-plugin/plugin.json`, invalid JSON, regular file, missing path,
   absolute missing path) — all MATCH.
3. **The embedded changelog** grew from 148,631 B / 892 lines / 68 sections to
   150,756 B / 900 lines / 69 sections (SHA-256
   `f60d668e6c7013cef3a48e55a1fd8f8ffd6b76228b06441fd423946305fd0834`),
   re-extracted from `agy changelog`. Note the newest section is still 1.3.5 —
   one release behind the binary's own version.

Tests updated to match: `TestChangelog_*` constants, the TUI metadata-row
expectation, and `TestUpdate_Offline` (now reads `ReferenceVersion`). A latent
`want[:50]` panic in the TUI test was made bounds-safe.

**Not yet retargeted:** `out/local-macos/` still holds the 1.3.5 seeds,
Ghidra project and `dec/*.c`. Regenerating the 1.3.6 seeds is trivial
(`rea-go-seeds` → `/tmp/seeds136`, already done) but the address-bearing
decompilation evidence needs a fresh Ghidra pass.

