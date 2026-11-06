# The agent loop

> The unified turn engine is `actor.Actor` (`actor/`), bridged into hosts via `internal/app/agentturn`. Interactive TUI runs `agentturn.Engine` via `internal/tui/bridge.go` (`EngineBridge`); headless `--print` runs `internal/printmode/print_actor.go`; and agentserver serves ACP/AHP sessions.

## The agent loop & runtime architecture

Pigasus uses a clean-room actor-based turn runtime (`actor.Actor` in `actor/`) that unifies turn execution across all entry surfaces:

| Host / Surface | Runtime Seam | Implementation |
|---|---|---|
| **Interactive TUI** | `agentturn.Engine` via `internal/tui/bridge.go` (`EngineBridge`) | `agentturn.Local` (wrapping `actor.Actor`) or remote engines |
| **Headless `--print`** | `internal/printmode/print_actor.go` | `actor.Actor` with streaming frame sink |
| **Agent Server / ACP** | `internal/app/agentserver` | `agentturn.Local` session orchestration over ACP v1/v2 / AHP |
| **ACP Remote Client** | `internal/app/agentturn/acp_remote.go` | `agentturn.ACPRemote` connecting to external ACP agents |

### The `actor.Actor` state machine

Execution in `actor.Actor` follows a structured concurrent loop:

1. **Ingest & Context Assembly**: Ingests user prompt, rehydrates conversation history, and merges system prompt guidelines and available tools (`surface.Registry`).
2. **Model Stream Execution**: Streams response tokens from the configured `backend.Client`. Dispatches `agentlink.Frame` events (`KindText`, `KindThought`, `KindTool`, `KindResult`).
3. **Mid-Turn Steer Ingestion**: Before dispatching pending tool executions, the loop checks `PendingSteer()`. Queued guidance is seamlessly injected into context without cancelling in-flight operations or dropping completed tool output.
4. **Tool Approval & Dispatch**: Invokes policy check (`permissions.Engine`). If user approval is required, requests confirmation via `OnApprovalRequired`. Upon approval, executes the tool via sandboxed tool surface and emits the result.
5. **Iteration & Completion**: Loops back to evaluate additional tool calls or delivers the final response when the model terminates the turn.

### Stream Contract & Typed Error Preservation

- **Typed Error Recovery**: Streaming preserves typed errors (`*backend.HTTPError`) out-of-band in an error slot (`errorSlot`), ensuring precise HTTP status recovery across the stream boundary.
- **Tool Non-Zero Exits**: Execution in `run_command` treats non-zero return codes as successful command execution containing an error payload, routing error output under `response.error` rather than aborting the agent loop.
- **In-flight Steer Reconciliation**: Mid-turn steering (`Steerer` / `SteerController`) notifies hosts via `NoticeSteerApplied` when consumed, keeping UI sending state aligned with actor execution.
- **Detached Cancellation**: When an agent turn context cancels, cancellation RPCs (such as ACP session cancellation) use detached timeout contexts to ensure prompt delivery and clean shutdown.



## The subagent's parent context (`internal/app/subagenttools`, cycle 363, agy 1.4.10)

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
| the record | `internal/app/subagenttools/parentcontext.go` — `ParentContext` | the parent's id, the task, and the parent's messages (oldest first, at most `MaxParentMessages` = 10) |
| the rendering | `ParentContext.Summary()` | the task in the reference's own message envelope, the parent's id, and the ten most recent messages one line each |
| the summary | `internal/app/subagenttools/compaction.go` — `SubagentSummary` | the parent's half followed by a digest of the subagent's own work (at most `MaxDigestLines` = 40 lines) |
| the hook | `parentContextExtension` | answers PiG's `session_before_compact` with `extension.CompactionResult` |

Two rules make the hook work:

- **The extension is loaded into the subagent's own session**, not into the
  parent's — so the parent's half travels with the session and not with the
  process (`createSubagentSession` in `internal/app/subagenttools/session.go`).
  The extension is loaded from a factory through
  `factoryload.LoadExtensionFromFactory`, under the name
  `subagent-parent-context`.
- **The `*extension.ExtensionRuntime` must be the shared one.** The same
  rule as the built-in factories in
  *The agent loop (cycle 183)*: an extension loaded into a runtime the
  session's runner does not use never fires its handler. `loadParentContextExtension`
  therefore takes the runtime the built-in extensions were loaded into.

The parent's messages are recorded while the delegation is **waited on**
(`internal/app/subagenttools/launch.go`): the parent context is built *before*
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

---

## The TUI's turn seam (cycle 374)

The TUI's turn does not drive raw backend streams directly. It goes
through **`internal/app/agentturn`**, the seam between *what a turn is* and
*where it runs*:

| engine | what it does |
|---|---|
| `agentturn.Local` | runs the turn **here**, over `actor.Actor` |
| `agentturn.Remote` / `ACPRemote` | sends the transcript to a remote server (e.g. `pigasus server` or ACP agent) and reports its frames |

Both answer with the same `agentlink.Frame`s, so the transcript does not know
which one it is rendering — the local path and the remote path cannot render
differently. The TUI's collector (`internal/tui/tui_stream.go`) is the one place
that knows how a frame becomes a row.

Guide: [`agent-server.md`](agent-server.md). Report:
`docs/cycles/cycle-374-agent-server-client-split/deliverable.md`.
