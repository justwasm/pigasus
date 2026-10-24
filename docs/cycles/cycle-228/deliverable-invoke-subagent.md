# Cycle 228 — `invoke_subagent` real delegation + `send_message` / `manage_subagents` stubs

Subagent workstream (cycles 228-237 map row 1, `docs/research/agy-subagents-vs-wagyu.md`).

Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-w228`
Branch: `cycle228-invoke-subagent`.
Clone binary MD5: `e9307043dd43fa65fa62176d6b4af278` (70 MB).

---

## TL;DR

The AGY 1.4.0 subagent toolset is **all three cycle-228 names advertised
on every turn**, with the surface pinned against the reference's wire
shape:

| Tool | Cycle-228 status | Wire shape | Stub marker in description |
|---|---|---|---|
| `invoke_subagent` | **fully implemented** | fresh PiG `coding.Session`, sync, transcript persisted to `<AgentDir>/subagents/<id>.jsonl` | — |
| `send_message` | **stub** | `{ok:false, error:"…not yet implemented (cycle 228 stub). Tracked in TODO §X."}` with `IsError=true` | yes |
| `manage_subagents` | **stub** | `{ok:false, error:"…not yet implemented (cycle 228 stub). Tracked in TODO §X."}` with `IsError=true` | yes |

The three are wired into both the **print path**
(`internal/pigrt/Options.ExtraTools`) and the **TUI path**
(`agent.Options.ExtraAgentTools`, a new field on `agent.Options` that
`Loop.pigTools` appends to the wrapped `Tools` slice).

`invoke_subagent`'s wire contract is a structured JSON envelope on the
parent tool result:

```json
{
  "ok": true,
  "subagent_id": "subagent-1700000000000-1",
  "session_id": "<ts>_<id>",
  "text": "<subagent's final assistant text>",
  "duration_ms": 1234
}
```

Cycle 229 work (not this cycle) surfaces subagent progress to the parent
TUI as `subagent_info` stream events.

---

## What was wrong (and why)

`docs/research/agy-subagents-vs-wagyu.md` row 1: AGY's default tool set
exposes `invoke_subagent` (full), `send_message` (full), and
`manage_subagents` (full) — three additional tools that the clone's
catalog (`internal/tools/catalog.go`) had only as inert placeholders
(circle 213 / 234 / 513 of the changelog, see
`internal/cli/testdata/changelog_reference.txt`).

The clone's pre-cycle-228 `internal/subagent` package is a **bash
wrapper** (`Subagent` carries `Cmd`, `Result`, `Err` only — no model,
no prompt, no transcript); it was added in cycle 22 (early cycles,
before cycles were numbered meaningfully) and has not been revisited
since. Wiring it as a tool would have given the model `exec.Command`
plumbing, not an LLM delegation. Cycle 228 ignores it and writes
`internal/subagenttools` from scratch.

Cycle 228 also deliberately **does not** touch the existing
`internal/subagent/` package or `internal/convstore/sqlite.go`. The
project rule is "do NOT touch existing internal/subagent/" because
refactoring that package is a separate piece of work that breaks the
tool-result semantics tests under `cmd/wagyu/` rely on. Cycle 229 (or
later) will revisit `internal/subagent/` once the new package's
transcripts are stable.

---

## Surface (what the model sees)

### `internal/subagenttools/subagenttools.go`

Three tools in stable order, returned by `Tools(deps Deps) []piagent.AgentTool`:

#### `invoke_subagent` (full)

```jsonc
{
  "name": "invoke_subagent",
  "parameters": {
    "type": "object",
    "properties": {
      "prompt":        {"type": "string", "description": "The self-contained task to delegate to the subagent."},
      "model":         {"type": "string", "description": "Optional model id override; defaults to the parent's model."},
      "system_prompt": {"type": "string", "description": "Optional system prompt override; defaults to the parent's system prompt."},
      "worktree":      {"type": "string", "description": "Optional absolute path the subagent runs in; defaults to the parent's cwd."}
    },
    "required": ["prompt"]
  }
}
```

Behaviour:

1. `Invoke(ctx, Invocation{...}, deps)` builds a fresh `coding.Runtime`
   from `deps.Services` with PiG's full builtin extension set
   (toolsearch + piglogin, plus MCP if a config path resolves). The
   parent and subagent share `Services` so auth.json, settings manager
   and model registry agree.
2. `rt.New(coding.SessionStartOptions{...})` builds the subagent's
   session with `NoSession=true` (PiG's own session log is bypassed so
   the parent's session directory doesn't get polluted),
   `SkipBuiltinTools=true` (the subagent only sees the parent's tool
   surface, not PiG's read/bash/edit/write set), and `ExtraTools=nil`
   (cycle 229 will plumb a scoped MCP allowlist).
3. `session.Send(ctx, inv.Prompt)` blocks until the session settles.
4. The subagent's transcript is persisted as JSONL to
   `<AgentDir>/subagents/<subagent_id>.jsonl` regardless of success.

For a pre-flight validation failure (nil `Services`, blank `Prompt`,
nil `Model`) `invoke_subagent.Execute` returns a plain Go error. For
a runtime failure (LLM error, network drop, etc.) it returns the
structured envelope with `IsError=true`.

#### `send_message` (stub)

```jsonc
{
  "name": "send_message",
  "parameters": {
    "type": "object",
    "properties": {
      "subagent_id": {"type": "string", "description": "Identifier returned by invoke_subagent's result.session_id or result.subagent_id."},
      "message":     {"type": "string", "description": "Text delivered to the subagent's transcript."}
    },
    "required": ["subagent_id", "message"]
  }
}
```

Cycle-228 behaviour: returns

```json
{"ok": false, "error": "send_message: not yet implemented (cycle 228 stub). Tracked in TODO §X."}
```

with `IsError=true`. Missing fields (`subagent_id` or `message` empty)
surface as a plain Go error.

#### `manage_subagents` (stub)

```jsonc
{
  "name": "manage_subagents",
  "parameters": {
    "type": "object",
    "properties": {
      "action":      {"type": "string", "enum": ["list", "kill", "status"], "description": "What to do..."},
      "subagent_id": {"type": "string", "description": "Target subagent id; required for kill/status, ignored by list."}
    },
    "required": ["action"]
  }
}
```

Cycle-228 behaviour: accepted actions (`list`, `kill`, `status`) return

```json
{"ok": false, "error": "manage_subagents: not yet implemented (cycle 228 stub). Tracked in TODO §X."}
```

with `IsError=true`. Unknown actions surface as a plain Go error.

The "not yet implemented (cycle 228 stub)" marker is **literally** part
of every stub's description so the model is not silently misled into
thinking send_message / manage_subagents do anything today. The cycle
unit tests (`TestSendMessageStub`, `TestManageSubagentsStubList`,
`TestManageSubagentsStubKill`) pin this marker.

---

## Wiring

### Print path — `cmd/wagyu/print_pig.go`

`runPigPrintTurn` builds a parallel `coding.Services` for the subagent
side (sharing `AgentDir` with the parent's `pigrt.New`) and appends the
three tools to `pigrt.Options.ExtraTools`:

```go
subServices, _ := coding.NewServices(coding.ServicesOptions{
    CWD:      cfg.Workspace,
    AgentDir: cfg.AgentDir,
})
defer subServices.Close()
subTools := subagenttools.Tools(subagenttools.Deps{
    Services: subServices,
    Model:    model,
    CWD:      cfg.Workspace,
    AgentDir: cfg.AgentDir,
})

rt, err := pigrt.New(ctx, pigrt.Options{
    CWD:              cfg.Workspace,
    AgentDir:         cfg.AgentDir,
    Model:            model,
    ExtraTools:       append(piagentbridge.Tools(cfg.Catalog), subTools...),
    SkipBuiltinTools: true,
    BeforeToolCall:   []piagent.BeforeToolCallHook{hook},
    ErrorSlot:        slot,
    NoSession:        true,
    MCPConfigPath:    mcpConfigPath(cfg.AgentDir),
})
```

The duplicate services is intentional: `pigrt.New` owns its own
services internally and there is no API to inject one externally.
Two parallel services instances on the same `AgentDir` are safe —
auth.json writes are flock-serialised and the settings manager is
file-backed; the registry and model runtime are read-only here.

`pigrt.Runtime` also got a one-line `Services()` accessor
(`internal/pigrt/runtime.go`) that exposes the parent's services for
future cycles that prefer single-services.

### TUI path — `internal/agent/pig.go` + `cmd/wagyu/tui_run.go`

The clone's TUI does not have a long-lived PiG runtime (the
`agent.Loop` is built per turn inside `streamPrompt`). Cycle 228 adds
a new `Options.ExtraAgentTools` field that `Loop.pigTools` appends to
the wrapped `Tools` slice after the runner-backed tools. The TUI calls
into a new `runMode.subAgentExtras(ctx, be, model)` helper that opens
the parallel services, derives the `pi.Model` via the same path as
`pigPrintModel`, returns the three tools, and a `defer closeSub()` that
runs after each turn.

The cost is one auth-storage open (~1ms) per turn. Cycle 229 will
cache the services on `runMode` once the TUI gets a long-lived PiG
runtime of its own.

`agent.Options.ExtraAgentTools` is `[]piagent.AgentTool` so callers can
hand in any tool that already implements the PiG interface (today
`subagenttools.Tools(deps)`; future cycles may add `mcp` tools,
`web_fetch`, etc. that don't fit the `backend.ToolDecl` shape).

The existing
`TestTUITools_TurnAdvertisesAndRunsTool` was updated from
*"exactly one tool named run_command"* to *"run_command is one of the
advertised tools"*. The cycle-99 behaviour being pinned (model answers
with a `run_command` call, runner executes it, second turn replies)
remains identical — only the assertion on the surface size changed.

---

## Hard gates

| Gate | Command | Result |
|---|---|---|
| Build | `go build -o /tmp/wagyu-final ./cmd/wagyu` | exit 0, binary 70 MB |
| gofmt | `gofmt -l internal/subagenttools/` | empty |
| Tests | `go test -vet=off -count=1 -timeout 600s ./cmd/... ./internal/...` | all 30 packages pass (incl. `internal/subagenttools` and the cycle-99 TUI regression) |
| Subagent tests (cycle 228) | `go test -vet=off -count=1 ./internal/subagenttools/` | 10/10 pass (8 subagenttools + 2 scripted-provider smoke tests) |

`go vet ./...` produces two pre-existing format warnings in
`cmd/wagyu/slash_cycle224_test.go` (`%) has unknown verb )`), unrelated
to cycle 228.

`TestTUITools_TurnAdvertisesAndRunsTool` is the only pre-cycle-228
test that needed updating — the surface assertion was hardcoded to
"exactly one tool"; cycle 228 adds three.

---

## Live probe

Network is blocked in this environment, so the live probe is a wire-
surface snapshot rather than a real `agy -p` invocation. A standalone
`cmd/agy228probe/main.go` was added to the cycle worktree: it builds
services, calls `subagenttools.Tools(deps)`, and emits a JSON snapshot
of the three schemas. The deliverable quotes the verbatim output:

```json
{
  "cycle": 228,
  "tools": 3,
  "names": ["invoke_subagent", "send_message", "manage_subagents"],
  "stubs": ["send_message", "manage_subagents"],
  "schemas": [
    { "name": "invoke_subagent", "description": "Delegate a self-contained sub-task to a fresh agent with its own transcript. Use when the task is self-contained and can run independently. The subagent's final answer is returned as the tool result; intermediate tool calls inside the subagent are NOT visible to the parent.", "parameters": "{\"properties\":{\"model\":{\"description\":\"Optional model id override; defaults to the parent's model.\",\"type\":\"string\"},\"prompt\":{\"description\":\"The self-contained task to delegate to the subagent.\",\"type\":\"string\"},\"system_prompt\":{\"description\":\"Optional system prompt override; defaults to the parent's system prompt.\",\"type\":\"string\"},\"worktree\":{\"description\":\"Optional absolute path the subagent runs in; defaults to the parent's cwd.\",\"type\":\"string\"}},\"required\":[\"prompt\"],\"type\":\"object\"}" },
    { "name": "send_message", "description": "Send a message to a running subagent. Stub: not yet implemented (cycle 228 stub). Tracked in TODO §X — AGY 1.4.0 pipes the message into the on-disk subagents/ tree; future cycle 229 wires this against the on-disk subagents/ tree.", "parameters": "{\"properties\":{\"message\":{\"description\":\"Text delivered to the subagent's transcript.\",\"type\":\"string\"},\"subagent_id\":{\"description\":\"Identifier returned by invoke_subagent's result.session_id or result.subagent_id.\",\"type\":\"string\"}},\"required\":[\"subagent_id\",\"message\"],\"type\":\"object\"}" },
    { "name": "manage_subagents", "description": "List, kill, or query the status of subagents. Stub: not yet implemented (cycle 228 stub). Tracked in TODO §X — AGY 1.4.0 reports on the on-disk subagents/ tree; future cycle 229 wires list/kill/status against the persisted transcripts.", "parameters": "{\"properties\":{\"action\":{\"description\":\"What to do: list known subagents, kill a running one, or report status.\",\"enum\":[\"list\",\"kill\",\"status\"],\"type\":\"string\"},\"subagent_id\":{\"description\":\"Target subagent id; required for kill/status, ignored by list.\",\"type\":\"string\"}},\"required\":[\"action\"],\"type\":\"object\"}" }
  ]
}
```

Build & run:

```bash
go build -o /tmp/agy228probe ./cmd/agy228probe
/tmp/agy228probe
```

Surface invariants the probe asserts (and that the unit tests pin):

- `tools == 3`
- `names == ["invoke_subagent", "send_message", "manage_subagents"]`
- `stubs == ["send_message", "manage_subagents"]`
- `invoke_subagent.parameters.required == ["prompt"]`
- `send_message.parameters.required == ["subagent_id","message"]`
- `manage_subagents.parameters.required == ["action"]` and
  `parameters.properties.action.enum == ["list","kill","status"]`

---

## Files touched

| Path | Status |
|---|---|
| `internal/subagenttools/subagenttools.go` | new (744 lines) |
| `internal/subagenttools/subagenttools_test.go` | new (~250 lines, 8 tests) |
| `internal/subagenttools/fake_provider_test.go` | new (~150 lines, 2 smoke tests) |
| `cmd/agy228probe/main.go` | new (the cycle-228 wire-surface probe) |
| `internal/agent/loop.go` | modified (`Options.ExtraAgentTools` field) |
| `internal/agent/pig.go` | modified (`pigTools()` merges extras) |
| `internal/pigrt/runtime.go` | modified (`Services()` accessor) |
| `cmd/wagyu/print_pig.go` | modified (parallel services + subagenttools wired into `pigrt.Options.ExtraTools`) |
| `cmd/wagyu/tui_run.go` | modified (`runMode.subAgentExtras`, `agent.Options.ExtraAgentTools`) |
| `cmd/wagyu/tui_tools_test.go` | modified (cycle-99 surface assertion updated for the 4-tool surface) |
| `docs/cycles/cycle-228/deliverable-invoke-subagent.md` | new (this file) |

Clone binary: **70 MB**, MD5 `e9307043dd43fa65fa62176d6b4af278`.
Previous cycle (227): no cycle-227 work exists; cycle-227 was a research
note only (`docs/research/agy-auto-compact-vs-pig.md`) that does not
touch binary size.

---

## 存疑

1. **Cycle 229 `send_message` semantics.** The cycle-228 stub accepts
   the wire shape AGY documents, but the reference's actual behaviour
   is "pipe the message into the subagent's current transcript while
   the subagent is running". PiG exposes `Session.Send` but not
   inject-message-into-running-session. Cycle 229 will need to read
   PiG's SDK to determine whether the documented feature rides on a
   steering-queue API the session owns; if not, cycle 229 will need
   to redesign `internal/subagenttools/Result` so the subagent
   listens on a PiG channel between tool batches.

2. **Cycle 229 `manage_subagents` "list" semantics.** The cycle-228
   stub returns the documented error envelope, but the reference's
   actual list likely comes from scanning
   `<AgentDir>/subagents/*.jsonl` and reporting each entry's first
   user prompt. Cycle 229 will need to read PiG's session-manager
   output to determine whether the JSONL files are sufficient or
   whether a parallel `subagent_info.json` index is required.

3. **Cycle 229 subagent progress to the parent TUI.** The cycle-228
   structured envelope on `invoke_subagent`'s result is forward-
   looking but the parent TUI today does not parse it back into a
   progress display. Cycle 229 will need to wire the envelope into
   `runMode.streamPrompt`'s response handling.

4. **Subagent permissions.** `subagenttools.Deps.PermissionGate` is
   declared but **not wired** by either path today. The TUI's policy
   (`m.policy`) and the print path's `cfg.Policy` are the right
   sources — cycle 229 will plumb them through a thin
   `internal/permissions.BeforeToolCallGate` adapter.

5. **AGY's `invoke_subagent` runtime model.** Not in the changelog;
   cycle 228 assumes "fresh Agent per delegation" based on cycle-217
   research note "Pure orchestrator. Dispatches tasks to
   specialists…". The decompiled `invoke_subagent` body (cycle-235
   work, `define_subagent`'s parent) needs to confirm whether AGY
   uses `Agent` or some lower-level runtime before cycle 229 commits
   to PiG's `coding.Session` shape.

6. **Parallel services instances.** Print path opens two
   `coding.Services` on the same `AgentDir`; auth.json is
   flock-serialised on write paths we use today, but the cycle-228
   delta has not been load-tested with two `Run` goroutines hitting
   the services concurrently. Cycle 229 may want to refactor
   `pigrt.New` to take an injected services to remove the duplicate.
