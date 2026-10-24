# Cycle 209 — PiG's coding runtime becomes the default turn engine

## TL;DR

The clone's print-mode turn no longer runs on `internal/agent`'s loop by
default. It now runs on PiG's `coding.Runtime` / `coding.Session`
(`internal/pigrt`), so the two features this cycle was opened for — **MCP** and
**skills** — arrive from PiG instead of being reimplemented:

* an MCP server configured in the *reference's* `mcp.json`
  (`~/.gemini/antigravity-cli/mcp.json`, schema `{"servers": {...}}`) is
  connected by PiG's built-in `mcp` extension, and its tools are declared to the
  model as `mcp__<server>__<tool>`;
* skills under the reference's roots (`<agentDir>/builtin/skills`,
  `~/.gemini/skills`, each plugin's `skills/`) are discovered by PiG's resource
  loader and enter the system prompt PiG builds.

The agw transport is unchanged: the session's model is built from the clone's
own `piagentbridge.Provider`, so the gateway URL, the system fingerprint, the
`User-Agent` and the `*backend.HTTPError` classification all stay the clone's.

This is the first step of the migration the plan calls *"PiG as the default
agent runtime, jetski sunset gradually"*. Nothing under `internal/agent` or
`internal/tools` was deleted: the old engine is still there, still tested, and
reachable with `OPENAGY_RUNTIME=jetski`.

## What "the runtime" now is

```
cmd/wagyu runPrintMode
  └─ printShouldUsePigRuntime ──┬─ true  ──► internal/pigrt ──► coding.Session
                                │             │  model.Provider = clone's agw provider
                                │             │  ExtraTools     = tools.Catalog (60 names)
                                │             │  MCP            = piG mcp extension ← agy mcp.json
                                │             │  skills         = agy skill roots
                                │             └─ BeforeToolCall = recovered permission policy
                                └─ false ──► internal/agent.Loop (unchanged)
```

The **output contract is not forked**. `print_pig.go` synthesises the same
`backend.StreamChunk` sequence the old loop emitted (text deltas, one terminating
chunk with usage) and feeds it to the *same* handler closure
`runPrintMode` already had — the sanitizer, the `user_input` / `agent_response`
steps, the `usage` block, the trailing newline and the exit codes all still come
from one place.

### Selection rule (`printShouldUsePigRuntime`)

| Condition | Engine |
|---|---|
| `OPENAGY_RUNTIME=jetski` | clone's loop (rollback switch) |
| backend does not describe an agw endpoint (mock, lifecycle wrappers) | clone's loop |
| `-c` / `--conversation` (resumed history) | clone's loop |
| otherwise | **PiG runtime** |

The mock clause is what keeps every existing capture-driven test on the path it
was written against: a parity harness drives `backend.NewMock()`, which has no
provider to hand a `coding.Session`, so those tests cannot silently move.

## Files

| File | Δ | What |
|---|---|---|
| `internal/pigrt/runtime.go` (new) | ~410 | `New`/`Run`/`RunWith`/`Close`, `Options`, `SkillNames`, `UsageOf`; builds Services + Runtime + one Session |
| `internal/pigrt/mcpconfig.go` (new) | ~150 | agy `mcp.json` → PiG `LoadedMcpConfig` (key rename, `env` ordering, `exposure: direct`) |
| `internal/pigrt/skills.go` (new) | ~60 | `AgySkillPaths` — the three roots `printSlashSkillInfos` uses, restated (this package cannot import `package main`) |
| `internal/pigrt/errors.go` (new) | ~85 | `ErrorSlot` + `RecordProvider`: keeps `*backend.HTTPError` across PiG's error-flattening stream boundary |
| `internal/pigrt/*_test.go` (new) | ~430 | turn + skills + MCP-config translation + in-process MCP round trip |
| `cmd/wagyu/print_pig.go` (new) | ~215 | `runPigPrintTurn`, `pigPrintConfig`, `printShouldUsePigRuntime` |
| `cmd/wagyu/print_pig_test.go` (new) | ~150 | agw SSE server → PiG session → handler; selection rule |
| `cmd/wagyu/main.go` | ~40 | handler hoisted to `chunkHandler`; `ctx` hoisted; `if pig { … } else { <old loop> }` (the diff is mostly `gofmt` re-indentation of the old block: `git diff -w` shows +43/−11) |
| `internal/agent/pig.go` | +14 | `NativeProvider` exported (the derivation was already there) |
| `cmd/wagyu/print_slash.go`, `main.go`, `print_skill_test.go` | +~130/-20 | print-mode `/<skill>` expansion (see below) |
| `cmd/wagyu/slash_panels.go`, `slash_panels_test.go` | +~150/-60 | `/skills` panel reads the live catalogue (see below) |

## Evidence

Measured on this machine (macOS/arm64, Go 1.27.2, `PiG v0.4.1`).

| Claim | Evidence | Grade |
|---|---|---|
| A `coding.Session` accepts the clone's provider | `TestRunPigPrintTurnStreamsText`: httptest agw SSE server → `*backend.AgwK0sWithConfig` → `agent.NativeProvider` → session → one answer ("Hello from the pig runtime!") and `usage{prompt:7,completion:5,total:12}` reach the print handler | 【一手】 |
| MCP servers come from the *agy* file | `TestRuntimeConnectsMCPFromAgyConfig`: `{"servers":{"docs":{...}}}` written to `<agentDir>/mcp.json` → tool `mcp__docs__echo` appears in `Session().Tools()` → `Execute` returns the server's `echo: hi` | 【一手】 |
| **The real binary does the whole MCP loop** | `ANTIGRAVITY_HOME=<tmp> OPENAGY_AGW_URL=<stub> /tmp/wagyu -p 'use the echo tool'` with `<tmp>/mcp.json` naming a `python3` stdio server: the child received `initialize` / `notifications/initialized` / `tools/list` / `tools/call {"text":"hi"}`; the stub gateway saw **POST 1 advertise 61 tools** (the 60 reference names + `mcp__echo__echo`) and POST 2 carry the tool result; stdout printed the answer | 【一手】 |
| The runtime switch actually routes | same command with `OPENAGY_RUNTIME=jetski`: the MCP child is **never contacted** | 【一手】 |
| The JSON envelope still works on the new engine | same command with `--output-format json`: `{"status":"SUCCESS","response":"done","num_turns":1,"usage":{"input_tokens":9,"output_tokens":1,"total_tokens":10}}` | 【一手】 |
| Skills are discovered by the loader | `TestRuntimeDiscoversAgySkills`: `SKILL.md` under `~/.gemini/skills/demo-skill` → `SkillNames()` contains `demo-skill` | 【一手】 |
| Translation is faithful | `TestLoadAgyMCPTranslate`: name-sorted, stdio/http split, `enabled` preserved both ways, `env` key-sorted | 【一手】 |
| The mock backend can never take the PiG path | `TestPrintShouldUsePigRuntime` | 【一手】 |
| The dependency closure is affordable | `go build ./internal/pigrt` adds 19 lines to `go.mod`; only `wazero` had to be downloaded | 【一手】 |

## Design notes worth keeping

1. **`ai.Model.Provider` is the seam.** `coding.Session` streams through
   `model.Provider.Stream` when the model's provider is not in PiG's registry
   (`coding/services.go`: `registryOwnsModelBackend` defaults false). Handing it
   `piagentbridge.Provider.Model(id)` — whose `Provider` is the clone's
   `pinnedProvider` — is the whole transport integration; `SessionStartOptions`
   has no separate provider knob.

2. **`ProviderMeta.ProviderID` must not be a PiG-known provider.**
   `checkModelAuth` requires credentials for every provider except
   `ollama`/`amazon-bedrock`/`test-faux` (`coding/services.go`). The clone's own
   `ProviderID` is used, and it is only consulted on `SetModel`, which the
   print path never calls.

3. **PiG drops the error type.** `agent_loop.go` turns a failing provider into an
   assistant message, so the caller sees a fresh `errors.New(msg)`.
   `ErrorSlot`/`RecordProvider` is the clone's answer — the same trick
   `internal/agent` already used — because `api_error.go` still needs
   `errors.As(&backend.HTTPError)` for exit code 3.

4. **`SkipBuiltinTools: true`.** The session gets the reference's 60 tool names as
   `ExtraTools` and PiG's `read`/`bash`/`edit`/`write` are suppressed. PiG's own
   tool surface is a deliberate later step, not something to widen by accident
   while the parity captures still pin byte output.

5. **MCP `exposure` is forced to `direct`.** PiG defaults to `codemode`, which
   hides server tools behind its sandbox; the reference treats configured
   servers' tools as eagerly declared, so `direct` is the faithful mapping.

## Also in this cycle

The two print/panel fixes this cycle was opened with landed too:

* **Print-mode `/<skill>`** (`print_slash.go`): `printSlashResult` gained a
  `Prompt` field; `dispatchPrintSlash` resolves a skill *after* the known-command
  switch and *before* the "forward as literal text" fallback, so
  `wagyu -p "/my-skill args"` now sends the SKILL.md body (with the args) to the
  model. Rejections and `--disable-slash-commands` still win. Six tests in
  `print_skill_test.go`.
* **`/skills` panel** (`slash_panels.go`): `skillsFromDisk` now delegates to
  `printSlashSkillInfos()` — builtin bundle + `~/.gemini/skills` + plugins — so
  the panel, `/<skill>` and `agy -p /skills` share one catalogue. The hardcoded
  `63 skills` is gone (this host: 57), real SKILL.md descriptions are used, and
  the 11-row viewport cap was removed. `TestPanelSkillsRowsMatchCatalog` pins
  that every catalogue name has a row.

## 硬约束 / 存疑

* **No live-model verification.** Every test above drives a scripted endpoint or
  an in-process MCP pair, and the CLI run uses a stub gateway; nobody has run
  the clone against a real agw endpoint with a real model, and the `http`
  transport path of the MCP mapping (`url` entries) is unit-tested but not
  exercised with a live HTTP MCP server. What *is* verified live is the
  plumbing: the binary spawns a real MCP child, negotiates, declares its tool,
  calls it and feeds the result back.
* **Resumed conversations stay on the old engine.** `-c` replays history from the
  clone's `convstore`, and PiG's session has no "send with a seeded transcript"
  entry point that preserves the reference's resume semantics. Migrating that is
  the next step; it is a deliberate exclusion, not an oversight.
* **PiG's built-in tools are off.** So the session is *not yet* "PiG's agent";
  it is PiG's runtime with the reference's tool list. Turning the built-ins on
  will change what the model can do and is a separate, capture-breaking cycle.
* **Turn budget.** The old loop ran `MaxTurns: 1`; a `coding.Session` uses PiG's
  default iteration budget. A prompt that makes the model call tools repeatedly
  can now iterate further in one `-p` run than before. Not measured against the
  reference.
* **Skills are prompt-side only.** The loader discovers them and PiG puts them in
  the system prompt, but no capture shows the reference's own prompt text, so
  "the model now sees the reference's skills" is verified only as "the skill
  list reaches the loader the session builds its prompt from" — not by diffing
  the outgoing prompt body against the reference's.
* **System prompt.** The old path passed none; PiG now composes its default
  coding prompt plus the discovered resources (skills, `AGENTS.md`). The
  reference's own system prompt has never been recovered, so "closer to the
  reference" is unproven for this text — it is simply no longer empty.
* **Binary cost.** Measured on this machine: `cmd/wagyu` went from 37.6 MB to
  **69.1 MB** (+31.5 MB, +84 %) by pulling PiG's `coding` graph — the MCP client,
  the resource loader, the session machinery and the session store's sqlite
  driver, all of which build even for a `NoSession` print run. Building with
  `-tags nocodemode` (drops the QuickJS-on-wazero codemode sandbox, which the
  clone does not use) gives 64.1 MB, so codemode is only ~5 MB of it. The
  module graph grew by 19 `go.mod` lines. Under the reference's own 180 MB this
  is still small, but it is a real, one-off cost of the migration and not
  something to hide.
* The two `settings.json`-dependent failures on this host
  (`TestCycle197_ConfigVisibleRows`, `TestPanelConfigLayout`, 21 rows vs 20)
  are environmental — the host sets `editorMode: vim`, adding a nested row. Both
  pass with a clean `ANTIGRAVITY_HOME`, and both fail identically before this
  cycle's changes.
