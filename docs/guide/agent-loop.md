# The agent loop

> Two turn engines: the print path runs PiG's `coding.Session`, the TUI runs PiG's `agent.Agent`. This file lists the upstream contracts the bridge depends on.

## The agent loop (cycle 183; print mode moved onto PiG in cycle 210)

There are **two** turn engines in the tree, and the split is now sharp:

| Engine | Owns | Where |
|---|---|---|
| `internal/pigrt` (PiG `coding.Session`) | **`--print`** — the whole headless surface | `internal/printmode/print_pig.go` |
| `internal/agent.Loop` (PiG `agent.Agent`) | the **interactive TUI** only | `internal/cliapp/tui_run.go` |

`internal/agent.Loop` was the clone's turn engine. Until cycle 182 it walked the
model → tool → model cycle itself; since cycle 183 `Send` delegates to
`github.com/MichaelKinsy/PiG`'s `agent.Agent`, with `internal/piagentbridge` as
the only translation layer. Since cycle 210 a `-p` turn does not go through it
at all — the print path runs PiG's `coding.Runtime`/`coding.Session` directly,
which is what brings MCP and skills. Read in this order:

| File | What it is |
|---|---|
| `internal/printmode/print_pig.go` | the print path: `runPigPrintTurn`, `pigPrintModel` (which `ai.Provider`), `pigHistory` (`-c` seeding) |
| `internal/pigrt/runtime.go` | the `coding.Session` wrapper: `New`, `Run`/`RunWith`, the event drain, `turnFailure` |
| `internal/pigrt/mcpconfig.go` | the agy `mcp.json` → PiG `mcp`-extension config bridge |
| `internal/pigrt/skills.go`, `errors.go`, `history.go` | skill roots, the out-of-band `ErrorSlot`, `SeedHistory` |
| `internal/piagentbridge/backendprovider.go` | any `backend.Client` → `ai.Provider` for the session (mock/shim; the OpenAI-shaped configured endpoint uses the native provider) |
| `internal/agent/pig.go` | the *TUI*'s translation: `sendPig`, `pigMessages`, `chunkSink`, `runnerTool`, `toolCallFromPig`, `metaFromPig`, and `nativeProvider` |
| `internal/agent/loop.go` | `agent.Loop`'s **public API, unchanged since 183**. `Send` is a one-line delegation. `Options` gained `Provider` as an *override* |
| `internal/piagentbridge/bridge.go` | `backend.Client` → PiG, the tool catalogue bridge, `Describer`/`Describe` |
| `internal/piagentbridge/stream.go` | `BackendStreamFn`, `pump`, `ToStreamRequest`; `BackendStreamFnWithObserver` carries chunk metadata that `ai.Usage` cannot |
| `internal/piagentbridge/gatewayprovider.go` | the self-written native `ai.Provider` over the configured endpoint's OpenAI-shaped SSE endpoint |

Four things about PiG's stream contract are **undocumented upstream** and the
adapter depends on all four. They were found by reading `agent/agent_loop.go`:

1. Event order is `Start → Text* → ToolCall* → Done|Error`.
2. `End(result)` is **mandatory** — `consumeStream` reads `stream.Result()`.
3. `StopReasonToolUse` is the only signal that triggers the tool batch.
4. Tool declarations ride on the initial `SystemMessage.ToolsAdded`.

Five further traps, all load-bearing:

- `ai.Provider.Stream(ctx, transcript, opts)` **does not receive the model**
  (`agent_loop.go:412`), so a multi-model provider must be pinned per model —
  hence `Provider.Model(label)` / `pinnedProvider`.
- PiG copies only `err.Error()` across the stream boundary
  (`agent_loop.go:494`, `return errorAssistant, nil, nil`). Any error whose
  concrete type matters — `*backend.HTTPError`, which
  `internal/apierror/api_error.go:102` recovers with `errors.As` — has to be recorded
  out-of-band. Hence `errorSlot` in `pig.go` (TUI) and `pigrt.ErrorSlot`
  (print), and the rule that both transports return a **pre-stream** failure as
  `(nil, err)` rather than an in-stream event.
- Because of that same line **`coding.Session.Send` can return a nil error for a
  turn that failed.** The print path recovers the failure from the returned
  messages (`pigrt.turnFailure`) and prefers the recorded original; check
  `ctx.Err()` *before* it so `--print-timeout` stays a partial-output success.
- PiG's `emit` **blocks** until the event is received, so the event drain
  goroutine must run independently of `Send` (128-buffer channel, `EventDone`
  left open).
- `Send` returns before the drain has *processed* the last events, so a trailing
  text delta can still be in flight. `pigrt.RunWith` calls
  `session.FlushEvents(ctx)` (a FIFO barrier the drain acknowledges) before
  clearing `onText`, or stdout gets truncated.

**Do not re-add a second transport.** The clone's gateway client and the native
provider must stay behaviourally identical: `ExternalModel` remap,
`SystemFingerprint` (`backend.GatewaySystemFingerprint`), `User-Agent`
(`backend.GatewayUserAgent`), and the `*backend.HTTPError` classification. The
switch is implicit — `nativeProvider` derives a provider from any backend that
implements `GatewayConfig()`, so `cmd/pigasus` is edited by zero lines and both the
`--print` and TUI paths move together. `backend.NewMock()` and the lifecycle
wrappers do not describe themselves: the TUI keeps the `StreamFn` path, and the
print path wraps them in `piagentbridge.BackendProvider` (the same `StreamFn`).

**The native provider is OpenAI-shaped only.** `NativeProvider` returns nil for
the Anthropic dialect, so an Anthropic-configured provider goes through the
generic `BackendProvider`/`StreamFn` path. That is deliberate: one
implementation of the Messages dialect (`internal/backend/gateway_anthropic.go`),
not two.

Cost, and the reason to be careful: `cmd/pigasus` was **+12.1 MB (+50.8 %)** since
`b36b3cb` (24.9 MB → 37.6 MB) when cycle 183 added PiG, and PiG brings 1 direct
+ 24 indirect module requirements. Cycle 210 then put PiG's `coding` runtime on
the print path unconditionally, so the binary is now **~69 MB**
(`-tags nocodemode` → ~64 MB). Cycle 278 moved PiG to **v0.5.1**, another
**+11 MB** (→ 102 MB) because 0.5 links `go/types`, `go/doc`, `go/format`,
`runtime/pprof` and `internal/zstd`. The binary is **109.8 MB** at PiG
**v0.5.2** (111.2 MB at v0.5.1 — the 0.5.2 upgrade is **−1.3 MB**; the rest of
the growth since the 102 MB figure is the clone's own code). Three things
about that upgrade are worth remembering:

- **The checksum database is unreachable from this machine, not empty.**
  `sum.golang.org` times out for `github.com/MichaelKinsy/PiG` at *every*
  version, so the first `go get` of a new PiG release needs
  `GONOSUMDB='github.com/MichaelKinsy/PiG,github.com/MichaelKinsy/PiG/*'`
  (already set in `.github/workflows/release.yml`). That is only needed while a
  *new* version is being added to `go.sum`: once the hash is in `go.sum`, the
  checksum database is never consulted, and a cold-cache `go mod download`
  succeeds with `GONOSUMDB` unset — so CI's `test.yml` needs no such env var.
- **The extension runtime must be shared.** Built-ins are factories in 0.5,
  and the `*extension.ExtensionRuntime` passed to `RuntimeOptions` has to be
  the same one the factories registered into, or MCP servers never reach
  `session_start` (`TestRuntimeConnectsMCPFromAgyConfig`). 0.5.2 makes
  `factoryload`'s `refreshTools` a no-op *while a factory loads*, which is
  exactly the state that shared runtime relies on.
- **PiG 0.5.2 stamps a client identity on every stream request**
  (`agent_loop.go` merges `ai.DefaultAgentIdentity` into the stream options).
  The clone's own `ai.Provider` implementations build their HTTP requests
  themselves and keep `backend.GatewayUserAgent`, so the wire is unchanged —
  do **not** "fix" this by adopting PiG's identity, which would replace the
  clone's User-Agent with PiG's.

Reports: `docs/cycles/cycle-183/`, `docs/cycles/cycle-209/`,
`docs/cycles/cycle-210/`, `docs/cycles/cycle-278/`,
`docs/cycles/pig-bump-052/`.


## The subagent's parent context (`internal/subagenttools`, cycle 363, agy 1.4.10)

【一手】 agy 1.4.10: "Fixed subagents losing the task their parent agent
gave them, and the parent's ID, after their context was compacted,
including with `/compact`; a subagent's summary now keeps the parent
agent's 10 most recent messages."

A subagent's conversation is **two conversations in one**: the parent's half
(the task it was given, the messages it was sent afterwards) and the child's
own half (the turns it worked through). A compaction rewrites the child's
half into a summary — and a summary that only knows about the child's own
turns leaves the child without the task it was given and without the id it
has to answer on. The parent's task and id are the **oldest** messages of
the conversation, so they are the first ones a summary drops.

The clone keeps the parent's half itself:

| piece | where | what it is |
|---|---|---|
| the record | `internal/subagenttools/parentcontext.go` — `ParentContext` | the parent's id, the task, and the parent's messages (oldest first, at most `MaxParentMessages` = 10) |
| the rendering | `ParentContext.Summary()` | the task in the reference's own message envelope, the parent's id, and the ten most recent messages one line each |
| the summary | `internal/subagenttools/compaction.go` — `SubagentSummary` | the parent's half followed by a digest of the subagent's own work (at most `MaxDigestLines` = 40 lines) |
| the hook | `parentContextExtension` | answers PiG's `session_before_compact` with `extension.CompactionResult` |

Two rules make the hook work:

- **The extension is loaded into the subagent's own session**, not into the
  parent's — so the parent's half travels with the session and not with the
  process (`createSubagentSession` in `internal/subagenttools/session.go`).
  The extension is loaded from a factory through
  `factoryload.LoadExtensionFromFactory`, under the name
  `subagent-parent-context`.
- **The `*extension.ExtensionRuntime` must be the shared one.** The same
  rule as the built-in factories in
  *The agent loop (cycle 183)*: an extension loaded into a runtime the
  session's runner does not use never fires its handler. `loadParentContextExtension`
  therefore takes the runtime the built-in extensions were loaded into.

The parent's messages are recorded while the delegation is **waited on**
(`internal/subagenttools/launch.go`): the parent context is built *before*
the session is created, and every message the parent sends to the subagent
is recorded as it arrives.

PiG asks the hook on **both** compaction paths — the manual `compact` and
the automatic one — so the clone's hook covers `/compact` and the
auto-compaction alike.

【推测】 The reference's own summary is written by its model and its exact
shape is not recoverable from the release note; the clone's summary is its
own document carrying exactly the three things the note names.

Report: `docs/cycles/cycle-363-agy-1.4.10-alignment/deliverable.md` §4.

---

## The print envelope's `finished_reason` (cycle 364, agy 1.4.11)

【一手】 agy 1.4.11: "Added a `finished_reason` field to the result of
`--output-format json` and `stream-json` runs that says why a turn ended
early, `print_timeout` when `--print-timeout` expired and the response is
partial, `interrupted` when the run was canceled such as with `Ctrl+C`, or
`error` for any other failure, so scripts can tell a cut-off answer from a
complete one; the field is omitted when the turn finishes normally."

The field was captured first-hand from the 1.4.11 binary on its error path
(a `-p` run with no credentials), where it is the **last** member of the
envelope:

```json
{"conversation_id":"","status":"ERROR","response":"",
 "error":"authentication failed or timed out","duration_seconds":0,
 "num_turns":0,"usage":{…},"finished_reason":"error"}
```

| what | where |
|---|---|
| the envelope's member | `printmode.jsonEnvelope.FinishedReason` (`json:"finished_reason,omitempty"`) |
| the three values | `printmode.finishedReasonPrintTimeout` / `finishedReasonInterrupted` / `finishedReasonError` |
| the reason's choice | `internal/printmode/main_print.go` — after the turn, in the order timeout → interrupt → error |
| the SIGINT guard | `internal/printmode/print_interrupt.go` |

**The field is omitted when the turn finishes normally** — `omitempty` and
no assignment, never an empty string.

**The interrupt has to be caught.** An uncaught `SIGINT` kills the process,
and a killed process writes no envelope at all, so the `interrupted` reason
would be unreachable. `printInterruptGuard` therefore installs a handler,
cancels the turn's context on the first `Ctrl+C` and lets the turn unwind
so the envelope can be written.

**A second `Ctrl+C` is still fatal.** The guard restores the default
disposition (`signal.Reset(os.Interrupt)`) **before** it cancels, so a
user who wants the process gone can always have it — the graceful path must
not be a trap.

`command` and `finished_reason` never travel together (a command result is
answered locally and finishes normally), so their relative order is
unobservable; `finished_reason` is last in the struct because the reference
*added* it to the 1.3.6 field order.

【推测】 The exit status of an interrupted headless run is 130 (128 +
`SIGINT`, the shell's own convention) — the 1.4.11 note pins the *field*,
not the status. The rank between the three values when more than one
applies (a run that was interrupted *and* timed out) is the clone's own
choice; the note does not rank them.

Report: `docs/cycles/cycle-364-agy-1.4.11-alignment/deliverable.md` §4.
