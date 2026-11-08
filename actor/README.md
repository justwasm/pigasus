# Package `actor`

Package `actor` is a standalone, public Go package that runs one agent's turns
serially: a mailbox queues turns, one goroutine owns the loop, and every turn
has its own lossless event stream.

## Design

1. **A turn is a request, not a message.** `StartTurn(ctx, TurnRequest)` queues
   the prompt and returns a `*TurnHandle`. There is no message interface, no
   broadcast, and no control plane in the mailbox: cancel, steer and approval
   are method calls, because they must take effect while the loop is busy.
2. **Configuration travels with the turn.** `TurnRequest` embeds `TurnConfig`
   (model, system prompt, provider, tools, temperature, token cap). Two queued
   turns keep their own configuration instead of the later submission rewriting
   what the earlier one still uses. `Config.Default` is the immutable set a
   turn inherits when its request does not override a field.
3. **One event surface.** `TurnHandle.Events()` is lossless, per turn, and
   closes when the turn ends; a slow reader applies backpressure instead of
   losing the completion event. `Done` closes just after the stream and `Err`
   reports the terminal error.
4. **A guarded state machine.** `Idle` → `Running` → `WaitingApproval` →
   `Idle`/`Closed`; illegal transitions are errors, not silent writes.
5. **Decoupled provider and tools.** Any `Provider` and `Tool` can be
   injected; the package has no dependency on `internal/`.
6. **The actor owns no policy it cannot enforce.** Scheduling, step caps and
   compaction belong to the host; what remains here is the turn loop, the
   approval wait and the trajectory.

## API

| Call | Meaning |
|---|---|
| `Spawn(Config)` | create the runtime; `Config.Default.Provider` is required |
| `StartTurn(ctx, TurnRequest)` | queue a turn and return its handle |
| `Prompt(ctx, text)` | the same with the actor's default configuration |
| `TurnHandle.Events/Done/Err/Cancel` | follow, await, cancel that turn |
| `Steer / PendingSteer / PopSteer / ClearSteer` | guidance queue for the next step boundary |
| `CancelTurn(reason)` | cancel the running turn (the host has no handle for it) |
| `Approve(toolCallID, …)` | answer the waiting approval |
| `State / Trajectory / ResetHistory` | inspect and reset history |
| `Close` | shut the runtime down |

`Close` waits for the turn loop, so it must not be called from inside a
`Tool.Execute`; a tool calls `CancelTurn` instead.

## Core primitives

### 1. Turns and interventions

Turns queued in the mailbox run one at a time. Interventions take effect
immediately:

| Method | Effect |
|---|---|
| `CancelTurn(reason)` | interrupts the running turn, whichever submission started it |
| `TurnHandle.Cancel(reason)` | cancels that turn, running or still queued |
| `Steer(instruction)` | queues guidance for the next step boundary |
| `Approve(toolCallID, …)` | answers the waiting approval |

Cancellation is scoped by the **handle**, not by an id: a caller can only
cancel a turn it holds.

### 2. Events

A `TurnHandle` yields, in order: `EventTurnStarted`, any of `EventThought`,
`EventDelta`, `EventUsage`, `EventSteerApplied`, `EventToolCallStarted`,
`EventApprovalRequired`, `EventToolCallFinished`, and exactly one terminal
event — `EventTurnCompleted` or `EventCancelled` (with `EventError` before it
when the turn failed).

There is no turn id. The `TurnHandle` is the turn's identity; events carry
`ActorID` only. The reference has no turn-id either: a turn is located by
`(conversation_id, step_index)` and `getTurnDiff` reports it as
`turn_start_index..turn_end_index_exclusive`.

### 3. Configuration precedence

A turn runs under `Config.Default` with the request's non-zero fields
overlaid. For `Tools`, `nil` inherits the default and an empty non-nil slice
means "this turn has no tools". The resolved configuration is captured when the
turn is submitted, so it cannot be rewritten while queued.

### 4. Interruption and recovery

A turn is not discarded when a step is cut short:

- **Transport interruption** (a transient `Recv` error): the partial step is
  kept with `Status: "interrupted"`, its delta is added to `FinalOutput`, and
  the turn continues with the reference's recovery instruction
  (`StreamInterruptedMessage`) as guidance. This is the same turn.
- **Truncation** (`FinishReason` `length`/`MAX_TOKENS`): the completed step
  keeps its finish reason and the turn continues the same way.
- **Both are bounded** by `AutoContMaxDepth` (3). Past that the turn ends with
  `ErrAutoContinueLimit` and the user decides; a user-authored "continue" is a
  new turn, not a recovery of the old one.
- **Cancellation** keeps the partial step with `Status: "cancelled"` and its
  text in `FinalOutput`, so the next turn starts from what the user actually
  saw.

### 5. State and trajectory

- `LifecycleState`: `StateIdle`, `StateRunning`, `StateWaitingApproval`,
  `StateClosed`.
- `TurnRecord`: `Model`, `Status` (`running`/`done`/`error`/`cancelled`),
  `CreatedAt`/`CompletedAt`, the prompt, steps, final output and usage. A turn
  is **staged** at start: `Turns`/`LastTurn` see it while it
  runs, `ToConversationMessages` excludes it so the live turn is not replayed
  twice.
- `Step`: `Status` (`running`/`done`/`error`/`cancelled`/`interrupted`),
  `FinishReason`, `StartedAt`/`CompletedAt`, its **own** `Usage`, plus the
  thought/delta, tool calls, tool results and steering.
- `Turns`/`LastTurn` return deep copies; `Revision` changes
  on every mutation. `ResetEpoch` changes only on `ResetHistory`/`SeedHistory`,
  so a host can tell a checkpoint from a reset it did not perform.
- `Checkpoints` are step-boundary context replacements, not turn steps. A
  checkpoint records `IncludedThroughStep`, `KeepRecent`, `ResumeText`, its
  own model/strategy/tokens and `KeepInput`. `ToConversationMessages` starts
  at the latest checkpoint's resume block and includes only steps after the
  boundary.

### 6. Checkpoint seam

`TurnConfig.Checkpoint` is asked before every model call:

```go
Checkpoint: func(ctx context.Context, probe actor.CheckpointProbe) (actor.CheckpointDecision, error) {
    return policy.BeforeStep(ctx, act, probe)
}
```

The callback only inspects; it must not call `ResetHistory`, `CancelTurn` or
mutate the actor. A returned `Apply` records the checkpoint and replaces the
next call's context; the turn continues, and `FinalOutput` keeps the user's
answer. `ErrCheckpointRequired` means the context cannot fit another call and
no replacement was produced. `Trajectory.CheckpointView()` groups the
transcript by global step for the host's policy.

## Quickstart

```go
act, err := actor.Spawn(actor.Config{
    Default: actor.TurnConfig{
        Provider: myProvider,
        Model:    "gemini-3.8-flash-high",
        Tools:    []actor.Tool{calcTool},
    },
})
if err != nil {
    panic(err)
}
defer act.Close()

turn, err := act.StartTurn(ctx, actor.TurnRequest{Prompt: "Calculate the answer"})
if err != nil {
    panic(err)
}

for ev := range turn.Events() {
    switch ev.Type {
    case actor.EventThought:
        fmt.Printf("[thought] %s\n", ev.Thought)
    case actor.EventDelta:
        fmt.Print(ev.Delta)
    case actor.EventTurnCompleted:
        fmt.Printf("\nDone in %v\n", ev.TurnSummary.Duration)
    }
}
<-turn.Done()
if err := turn.Err(); err != nil {
    log.Fatal(err)
}
```

A turn that overrides the defaults sets the fields on the request:

```go
turn, err := act.StartTurn(ctx, actor.TurnRequest{
    Prompt: "Use the cheap model",
    TurnConfig: actor.TurnConfig{Model: "gemini-3.8-flash-low"},
})
```
