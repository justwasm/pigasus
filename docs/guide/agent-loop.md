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

### Turn handles, per-turn configuration and the control plane

The runtime's contract is narrower than the word "actor" suggests; three rules
are load-bearing:

- **A turn is a request.** `StartTurn(ctx, TurnRequest)` is the only way into
  the mailbox; `TurnRequest` is a prompt plus a `TurnConfig`. There is no
  message type and no broadcast. Control is method-only (`CancelTurn`,
  `TurnHandle.Cancel`, `Steer`, `Approve`) because anything queued behind the
  running turn would arrive too late. The `TurnHandle` is the cancellation
  identity — there is no turn id to guess.
- **Configuration travels with the turn.** `StartTurn` resolves the request
  against `Config.Default` at submission and captures the result, so a queued
  turn cannot be rewritten mid-flight. A host overrides model, prompt,
  provider or tools per turn; it never mutates the actor.
- **Completion is lossless.** `TurnHandle.Events()` is per-turn and closes
  when the turn ends; `Done` closes just after it and `Err` reports the
  terminal error. There is no shared broadcast: a slow reader applies
  backpressure instead of losing the completion event. Cancelling closes the
  handle immediately, so a tool that ignores its context cannot hold a caller
  hostage.

A turn is staged as a `running` record before the first model call and
finalised at the end. A transport interruption keeps the partial step
(`Status: "interrupted"`) and continues the same turn with the reference's
recovery copy; truncation (`length`) does the same; both are bounded by
`AutoContMaxDepth = 3`. Cancellation keeps the partial step (`cancelled`) and
its text in `FinalOutput`. A user-authored "continue" is a new turn.

The actor owns no scheduling or step cap. Compaction is host policy exposed as
a **step-boundary seam**: before every model call the actor asks
`TurnConfig.Checkpoint` whether to replace the context. The host's
`internal/features/checkpoint` policy applies the reference's per-model
limits, writes the continuation summary, and returns the
`# Resuming from a compaction` block. The actor records a `Checkpoint`
boundary and continues the **same** turn; it never calls `ResetHistory` for an
automatic checkpoint.

`Trajectory.Revision()` changes on every mutation; `ResetEpoch()` changes only
when the trajectory is reset or re-seeded. `agentturn.Local` uses the epoch
(not a message-count comparison) to keep its actor across a checkpoint and
rebuild it after `/clear` or `/resume`. Automatic compaction therefore cannot
discard an in-flight turn or its steps.

### Stream Contract & Typed Error Preservation

- **Typed Error Recovery**: Streaming preserves typed errors (`*backend.HTTPError`) out-of-band in an error slot (`errorSlot`), ensuring precise HTTP status recovery across the stream boundary.
- **Tool Non-Zero Exits**: Execution in `run_command` treats non-zero return codes as successful command execution containing an error payload, routing error output under `response.error` rather than aborting the agent loop.
- **In-flight Steer Reconciliation**: Mid-turn steering (`Steerer` / `SteerController`) notifies hosts via `NoticeSteerApplied` when consumed, keeping UI sending state aligned with actor execution.
- **Detached Cancellation**: When an agent turn context cancels, cancellation RPCs (such as ACP session cancellation) use detached timeout contexts to ensure prompt delivery and clean shutdown.

### Where a failed model request is retried

There are two retry seams, and a status the first one retries must also be
retried by the second — otherwise the same upstream failure ends a turn or not
depending on whether the provider reported it before or after the first byte.

| seam | owns | budget | schedule |
|---|---|---|---|
| `streampipe.ExecuteWithRetry` | the *open*: the request gets no 2xx status line | `retry.MaxConnectionRetries` (9, so 10 attempts) | `retry.NextConnectionRetryDelay`, overridden by the server's `Retry-After` / RPC `RetryInfo`, capped at `retry.MaxConnectionRetryInterval` (4 min) |
| `streampipe.Pipeline.Run` | the *stream*: the body breaks after a 200 | same budget, minus the attempt already spent | same schedule, same `Retry-After` override |

Both classify through `streampipe.isMidStreamRetryable`: a connection drop, or
any `*apierror.HTTPError` whose status `IsRetryableStatus` accepts (408, 429,
5xx). Quota-reached, auth-failed and prepaid-balance failures are excluded —
an account-pool switch, not a retry, is the right answer to those.

Two details are easy to get wrong and both were bugs:

- **The gateway reports an upstream failure as an SSE `error` frame on an
  already-open 200 stream.** `sse_error.go` turns that frame into the same
  `*apierror.HTTPError` the status line would have produced, so
  `gateway: upstream 500: Unknown error` is indistinguishable from a 500 in the
  status line. `Pipeline.Run` used to retry only connection drops and the
  AGY 1.5.17 capacity case (503), so this — the gateway's most common upstream
  failure — killed the turn on the first attempt with no retry at all. That is
  the 1.5.0 rule ("Fixed certain models failing a turn on rate-limit (`429`)
  and server (`5xx`) errors instead of retrying them") applied to the half of
  the traffic it was written for.
- **An unrecognised error-frame shape is not automatically a 500.**
  `classifySSEErrorStatus` defaults to 500 for shapes it does not know, which is
  the safe direction for *reporting* but the wrong direction for *retrying*: it
  would turn a 400 into an endlessly replayed request. A bare numeric `code`
  (`400`, `422`, `429`) is therefore trusted as the status, and the OpenAI
  `invalid_request_error` / `request_too_large` / `context_length_exceeded`
  types map to 400 while `not_found_error` maps to 404. A billing cue in the
  message (spend cap, prepaid balance) wins over the frame's type and maps to
  402, because the prepaid-wallet stop must never be retried.

The resumed attempt re-streams the answer from the first token; `attemptTap`
swallows the prefix the caller already received, so a retry neither repeats nor
discards model output (see `pipeline_resume.go`).

Every attempt that dies mid-stream now logs one line, retried or not:

```
[streampipe] endpoint=… mid_stream attempt=2/10 resumed_after=3 retry_in=1s err=gateway: upstream 500: Unknown error
[streampipe] endpoint=… mid_stream abort err=gateway: upstream 400: bad request
```

A session log with no `[streampipe]` line at all means the failure never reached
the transport — look at `[agentturn] should_recover=` instead.

### A retryable frame plus a fake server is an eight-minute test

The schedule is 1s, 2s, 4s … capped at four minutes, and nine retries sum to
**8m15s**. That is the right production budget and the wrong test budget: a
test whose `httptest` server answers the *same* failing frame on every attempt
— which is what a classification test does, since it wants to see how one frame
is classified — retries for as long as the schedule says. Before the mid-stream
retry rule those tests never retried (429/5xx mid-stream were terminal), so the
cost was invisible; after it, three gateway subtests began taking eight minutes
each, and one run hung past the package timeout.

`internal/infra/backend/retrytest` is the shared answer:

```go
func TestMain(m *testing.M) {                 // once per package
	restore := retrytest.Short()              // schedule → ms, restore on exit
	code := testguard.Run(m)
	restore()
	os.Exit(code)
}

retrytest.Fast(t)                             // a test that *observes* a retry
ctx := retrytest.NoRetries(context.Background())  // a test that classifies one frame
retrytest.Production(t)                       // a test that asserts the schedule
```

`Short` in `TestMain` is safety — a test that forgets its own pin can no longer
cost a coffee break. `NoRetries` is the sharper assertion where it matters: it
pins the number of attempts instead of merely making them cheap, so it fails if
the transport starts retrying something the test says it should not. The gateway
package proves both halves: `TestGatewaySSE_TypedErrorClassification` runs on
`NoRetries`, and `TestMidStream500AlwaysFailingTerminatesQuickly` deliberately
omits the pin and asserts the whole exhausted budget finishes in milliseconds.



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

The clone keeps the parent's half itself, and it has exactly one shape:

| piece | where | what it is |
|---|---|---|
| the record | `internal/app/subagenttools/parentcontext.go` — `ParentContext` | the parent's id, the task, and the parent's messages (oldest first, at most `MaxParentMessages` = 10) |
| the rendering | `ParentContext.Lines()` | the task and then the parent's messages, each one agent-message line |
| the transport | `checkpoint.Snapshot.ParentMessages` | `launch.go` sets it before **every** turn, so a message delivered after an earlier checkpoint still reaches the next one |
| the block | `compaction.ResumeBlock` → `ParentBlock` | renders `# Parent Agent Messages` in place of `# User Requests` |

【一手】 agy 1.5.17 keeps the two halves strictly **apart** rather than
composing them. The reference has a symmetric pair of helpers —

```
utils.CaptureUserRequests       utils.CaptureParentMessages
utils.FormatCheckpointUserRequests   utils.FormatCheckpointParentMessages
```

— feeding two parallel fields of `CortexStepCheckpoint`
(`captured_user_requests = 30`, `captured_parent_messages = 31`), and
`ResumeBlock` emits **one or the other**: `ParentMessages` replaces the
`# User Requests` section. The child's own half is the model-written
`session_summary`, never a Go-side digest of its tool results.

So there is no combined "parent + my own work" document to build. An earlier
`SubagentSummary` / `CompactionPreparation` pair in
`internal/app/subagenttools` modelled exactly that combined shape; it had no
callers and no counterpart in the reference, and was removed.

**The cap bounds the messages only, never the task.** The task is the
delegation itself, and it is the *oldest* thing the parent said — so a cap
applied to the combined list would drop it first, which is precisely the
defect agy 1.4.10 fixed. `Record` is the single place that enforces the cap,
so the retained count and the retained lines cannot disagree.

The parent's messages are recorded while the delegation is **waited on**
(`internal/app/subagenttools/launch.go`): the parent context is built *before*
the first turn, and every message the parent sends to the subagent is recorded
as it arrives. System wakeup notifications are deliberately excluded
(`From == "system"` keeps the `system` sender and is not recorded), so a
chatty board cannot crowd out the coordinator's instructions.

PiG covers **both** compaction paths — the manual `/compact` and the automatic
one — because both go through `checkpoint.Policy`, which reads
`Snapshot.ParentMessages` from the same snapshot.

【推测】 The reference's own summary is written by its model and its exact
shape is not recoverable from the release note. The clone delegates the same
half to `checkpoint.Policy`, whose `summarize` calls
`compaction.ComputeSummaryAuto` on the child's own transcript — so the split
matches the reference's, without a second Go-side document to drift out of
sync.

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
