# Package `actor`

Package `actor` is a standalone, public Go package that provides an Actor-model runtime for autonomous AI agent orchestration (benchmarked against the conceptual architecture of `jetski/cortex`).

## Design Philosophy

In classical agent runtimes, agent state is often accessed or mutated concurrently from various UI callbacks, webhooks, or background threads, leading to race conditions and complex lifecycle state.

In `actor`, an AI Agent is an **Actor**:
1. **Mailbox-Driven Execution**: All inputs (user messages, reactive timer wake-ups, human approval decisions, steering instructions, and cancellation signals) are posted to the actor's mailbox as typed messages.
2. **Encapsulated State Machine**: The actor executes sequentially inside a background event loop. State transitions (`Idle` -> `Running` -> `WaitingApproval` -> `Idle` / `Closed`) are guarded and free of concurrent data races.
3. **Multi-Step Tool Resolution**: When a model requests tool executions, the actor executes them (or pauses for approval), feeds results back into the conversation, and loops until the model reaches completion or max steps.
4. **Decoupled Provider & Tool Interfaces**: Zero coupling to internal infrastructure or concrete models. Any LLM provider or custom tool can be injected cleanly.
5. **Built-in Reactive Scheduling**: Native delay timers and standard 5-field cron parsing (`NextCronTime`) allow the actor to schedule self-addressed wakeup messages (`ScheduledWakeup`).
6. **Zero Internal Dependencies**: Obeying architecture rule R6, `actor` has zero dependencies on `internal/` and can be imported and consumed directly by third parties.

---

## Interfaces & Architecture Boundaries

Package `actor` exposes focused capability interfaces so callers can consume exactly the control surface they need:

- **`TurnRunner`**: Turn execution surface (`Ask`, `Prompt`, `Run`).
- **`TurnController`**: Real-time out-of-band turn intervention (`Steer`, `CancelTurn`, `Approve`).
- **`SteerTracker`**: Observation and queue manipulation of steering instructions (`PendingSteer`, `PopSteer`, `ClearSteer`).
- **`EventStreamer`**: Event pub-sub subscription (`Subscribe`, `Events`).
- **`ActorHandle`**: Unified handle composing all above capabilities along with actor metadata and lifecycle controls.

## Core Primitives

### 1. In-Band Mailbox Messages vs Out-of-Band Interventions

- **Mailbox Messages (Sequential execution)**:
  - `UserMessage`: Explicit prompt from a user or client. Starts a turn when idle.
  - `ScheduledWakeup`: Wake-up message triggered by a delay timer or recurring cron.
  - `SystemPromptUpdate`: Dynamically updates base persona instructions between turns.
  - `CustomMessage`: Application-defined message payload.
- **Turn Interventions (Out-of-band concurrent controls)**:
  - For active turns, call `act.Steer(...)`, `act.CancelTurn(...)`, or `act.Approve(...)` directly. These operate concurrently without waiting for mailbox processing.
  - `SteerMessage`, `CancelSignal`, `ApprovalDecision` are provided for IPC / wire protocol serialization across network or process boundaries.

### 2. Events (`Event`)
Subscribed via `act.Events() <-chan Event`, `act.Subscribe()`, or `act.Ask(ctx, msg)`:
- `EventTurnStarted`: Turn starts execution.
- `EventThought`: Reasoning delta from the model.
- `EventDelta`: Response text chunk.
- `EventToolCallStarted`: Tool invocation requested.
- `EventToolCallFinished`: Tool invocation finished with result or error.
- `EventApprovalRequired`: Tool execution paused waiting for human approval.
- `EventScheduled`: Schedule registered.
- `EventScheduleFired`: Schedule triggered.
- `EventSteerReceived`: Steering guidance received and queued.
- `EventSteerApplied`: Steering guidance popped and injected into active step conversation history.
- `EventCompacted`: Trajectory history compacted.
- `EventTurnCompleted`: Turn finished with full summary metrics.
- `EventCancelled`: Turn cancelled.
- `EventError`: Turn or actor encountered an error.
- `EventUsage`: Token usage reported for a step.

### 3. State & Trajectory
- `LifecycleState`: `StateIdle`, `StateRunning`, `StateWaitingApproval`, `StatePaused`, `StateClosed`.
- `Trajectory`: Chronological history of turns and steps, with auto-compaction and export to conversation history.

---

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func main() {
	// 1. Define custom tools
	calcTool := actor.NewFuncTool("calculate", "Performs math", func(ctx context.Context, input string) (string, error) {
		return "42", nil
	})

	// 2. Spawn actor with provider & tools
	act, err := actor.Spawn(actor.Config{
		Name:         "assistant",
		SystemPrompt: "You are a helpful assistant.",
		Provider:     myProvider, // implements actor.Provider
		Tools:        []actor.Tool{calcTool},
	})
	if err != nil {
		panic(err)
	}
	defer act.Close()

	// 3. Prompt for streaming events or Run for direct completion
	stream, err := act.Prompt(context.Background(), "Calculate the answer")
	if err != nil {
		panic(err)
	}

	for ev := range stream {
		switch ev.Type {
		case actor.EventThought:
			fmt.Printf("[thought] %s\n", ev.Thought)
		case actor.EventDelta:
			fmt.Print(ev.Delta)
		case actor.EventTurnCompleted:
			fmt.Printf("\nDone in %v!\n", ev.TurnSummary.Duration)
		}
	}

	// Or simple synchronous execution:
	// answer, err := act.Run(context.Background(), "Summarize today's logs")

	// 4. Reactive Wake-up: schedule self-reminder after 10 seconds
	act.Schedule(10*time.Second, "Check background task status")
}
```
