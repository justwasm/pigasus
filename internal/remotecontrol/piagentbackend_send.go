// Package remotecontrol — piagentbackend_send.go:
// SendStream implementation + the per-turn drain helpers
// (deriveTurnCtx / startSession / drainEvents /
// assembleStreamResult / mapStreamFailure). Pulled out
// of piagentbackend.go in cycle
// rc-filesize-internal-remotecontrol so each file stays
// under the 500-line project cap.
package remotecontrol

import (
	"context"
	"fmt"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// turnResult is the inner struct the Send goroutine
// returns. Pulled out of SendStream so the helper
// signatures stay short.
type turnResult struct {
	msgs []agent.AgentMessage
	err  error
}

// drainState bundles the mutable accumulator the drain
// loop writes into. Returning it as a single value (vs.
// passing five pointers) keeps the helper signatures
// tight and lets the apply* helper mutate the loop-local
// state without an awkward out-parameter ceremony.
type drainState struct {
	result    MessageResult
	doneEv    *ai.DoneEvent
	errEv     *ai.ErrorEvent
	cancelled bool
}

// SendStream implements StreamingMessageBackend. It is the
// cycle's headline surface: drive one turn on a fresh
// `Session`, drain its event channel, translate each
// `agent.AgentEvent` into the matching `EventSink` call,
// and assemble the end-of-turn MessageResult.
//
// Lifecycle:
//
//   - SessionFactory mints a fresh Session. (One per
//     SendStream call. No state is shared across calls.)
//   - The session's event channel is fed to the
//     background `Send` invocation. PiG's event channel
//     is buffered (cap 128), so a slow sink does not
//     stall the agent loop. The buffered size mirrors
//     `internal/agent.pig.go`'s `events := make(chan
//     piagent.AgentEvent, 128)`.
//   - The driver goroutine reads events until the channel
//     closes. Each event maps to one sink call. The
//     driver is the one consumer (PiG's emit blocks on a
//     full channel, so a single drain is the canonical
//     shape).
//   - On `Done` / `Error`, the driver returns the
//     accumulated MessageResult. Close runs whether the
//     turn succeeded or failed; the defer is the
//     symmetric teardown.
//
// Error mapping:
//
//   - SessionFactory returns an error before any turn
//     runs → that error is the SendStream return value,
//     no sink call fires. The dispatcher maps it to
//     -32603 via the existing ErrTurnFailed wrap.
//   - ctx deadline before the session returns → ctx.Err();
//     OnState(starting) DOES fire (the session opened),
//     OnError fires, OnState(done) does NOT fire (the
//     registry maps to -32010 Turn timeout).
//   - session returns a non-nil error after at least one
//     event → that error is the SendStream return value,
//     OnError fires. The registry wraps in ErrTurnFailed
//     for -32603.
//   - drain closes without a Done event (PiG promises one,
//     but the dispatcher doesn't crash if it doesn't show
//     up) → SendStream returns ErrSessionClosed so the
//     dispatcher's switch can route it; OnState(done) does
//     NOT fire.
//
// Concurrency: PiagentBackend itself has no mutable state;
// the SendStream call's stack frame owns the per-turn
// goroutine, channel and accumulated state. Multiple
// SendStream calls run concurrently (the dispatcher does
// not serialise them; the per-cascade mu in
// `(*CascadeRegistry).SendStream` does, but that's a
// separate concern).
//
// Nil-safety:
//
//   - nil receiver: every method returns a zero
//     MessageResult and a typed error (the dispatcher
//     handles the wrap). No panics.
//   - nil SessionFactory: SendStream returns
//     ErrNilSessionFactory verbatim. The dispatcher
//     surfaces as -32603 with a clear message.
//   - nil sink: the sink calls are skipped (the events
//     are still drained so the session doesn't stall);
//     the returned MessageResult still carries the reply
//     / tools / stop reason.
func (b *PiagentBackend) SendStream(ctx context.Context, prompt MessagePrompt, sink EventSink, opts MessageSendOptions) (MessageResult, error) {
	if b == nil || b.SessionFactory == nil {
		return MessageResult{}, ErrNilSessionFactory
	}

	turnCtx, cancel := deriveTurnCtx(ctx, opts)
	if cancel != nil {
		defer cancel()
	}

	sess, err := b.SessionFactory(turnCtx, prompt, opts)
	if err != nil {
		return MessageResult{}, fmt.Errorf("piagent: session factory: %w", err)
	}
	defer func() {
		// Best-effort close. A Close error is silently
		// dropped: production callers don't care, and a
		// future cycle can layer structured logging here
		// without changing SendStream's signature.
		_ = sess.Close()
	}()

	events, done := b.startSession(turnCtx, prompt.Text, sess)

	state := b.drainEvents(events, turnCtx, sink)

	// The turn goroutine has already exited (Send
	// returned before close(events)). Read its result
	// for the NewConversationID and the final error.
	turn := <-done

	emitResultToSink(&state, turn, sink)
	return state.result, mapStreamFailure(state, turn, turnCtx)
}

// deriveTurnCtx returns a derived context honouring
// opts.Timeout. Returns (ctx, nil) when opts.Timeout <= 0
// — the caller skips the cancel-defer path. Mirrors
// (*CascadeRegistry).SendStream's wrapping so the
// PiagentBackend is usable outside the registry's path
// — a future caller that bypasses the cascade surface
// still sees the contract honoured.
func deriveTurnCtx(ctx context.Context, opts MessageSendOptions) (context.Context, context.CancelFunc) {
	if opts.Timeout <= 0 {
		return ctx, nil
	}
	turnCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	return turnCtx, cancel
}

// startSession mints the event channel (cap 128, matching
// internal/agent.pig.go's drain size) and spawns a
// goroutine that runs sess.Send and closes the events
// channel when it returns. Returns the (events, done)
// channel pair the drain loop and the result-assembler
// use.
//
// The session's `Send` call drives the turn on its own
// goroutine — we do not need the returned []agent.AgentMessage
// for anything the dispatcher sees (the dispatcher cares
// about Reply / ToolCalls / StopReason / NewConversationID,
// which we assemble from the event stream) — but we read
// them to honour ctx cancellation cleanly.
func (b *PiagentBackend) startSession(turnCtx context.Context, promptText string, sess Session) (chan agent.AgentEvent, chan turnResult) {
	events := make(chan agent.AgentEvent, 128)
	done := make(chan turnResult, 1)
	go func() {
		msgs, err := sess.Send(turnCtx, promptText, events)
		close(events)
		done <- turnResult{msgs: msgs, err: err}
	}()
	return events, done
}

// drainEvents consumes the session's event channel and
// translates each agent.AgentEvent into an EventSink call
// (or an in-place mutation of result when sink is nil).
//
// Order matches PiG's event vocabulary:
//
//	agent.MessageUpdateEvent (every agent action carries one)
//	  .AssistantMessageEvent is one of:
//	    ai.StartEvent         → OnState(starting)
//	    ai.TextStartEvent     → (no-op; OnTextDelta fires)
//	    ai.TextDeltaEvent     → OnTextDelta(delta)
//	    ai.TextEndEvent       → (no-op; the joined text is the result)
//	    ai.ToolCallStartEvent → (no-op; the End carries the call)
//	    ai.ToolCallEndEvent   → OnToolCall(call)
//	    ai.DoneEvent          → OnState(done, StopReason)
//	    ai.ErrorEvent         → OnError(err)
//	agent.CompactionStartEvent / CompactionEndEvent
//	                          → (no-op for the dashboard;
//	                              rc-final-parity-verify may
//	                              surface these via the
//	                              cascade/status tool.)
//	agent.MessageUpdateEvent with non-TextDelta content
//	                          → ignored.
//
// The accumulator tracks the joined reply and the tool
// calls the model emitted across the turn. StopReason is
// the last DoneEvent.Reason we saw.
//
// Cycle rc-cancel-cascade: the loop checks turnCtx.Done()
// between events. When the dashboard's
// CancelRemoteAgentCascade fires the cascade's cancel
// func, turnCtx is cancelled; the select below picks the
// ctx.Done() arm, stops processing further events, and
// returns the partial result with ctx.Err(). The session
// goroutine unblocks (PiG's internal ctx propagation
// should honour the cancel — see
// internal/pigrt/runtime.go's post-call ctx.Err() check)
// and closes events cleanly so this loop exits. We do NOT
// emit OnState(cancelled) here — the dispatcher's
// CancelRemoteAgentCascade handler is the single source
// of truth for the cancelled notification so the
// dashboard sees exactly one cascade/state{phase:cancelled}
// frame.
func (b *PiagentBackend) drainEvents(events chan agent.AgentEvent, turnCtx context.Context, sink EventSink) drainState {
	state := drainState{}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return state
			}
			applyMessageUpdateEvent(ev, &state, sink)
		case <-turnCtx.Done():
			// Cycle rc-cancel-cascade: the cascade's
			// cancel func fired. Mark the loop as
			// cancelled so subsequent events are
			// dropped without reaching the sink.
			// We keep draining so the session
			// goroutine can finish its close(events)
			// cleanly — otherwise a session that's
			// mid-send could block on its own emit.
			//
			// The dispatcher's Cancel handler is
			// responsible for emitting the
			// cascade/state{phase:cancelled}
			// notification; this loop just stops
			// processing events and returns the
			// partial result. Avoids the dashboard
			// seeing two cancelled frames.
			state.cancelled = true
		}
	}
}

// applyMessageUpdateEvent is the per-event branch of
// drainEvents. Pulled out so the loop body stays compact
// and the per-kind code paths are individually readable.
// Mutates state in place via the *drainState pointer.
func applyMessageUpdateEvent(ev agent.AgentEvent, state *drainState, sink EventSink) {
	if state.cancelled {
		return
	}
	switch event := ev.(type) {
	case agent.MessageUpdateEvent:
		switch inner := event.AssistantMessageEvent.(type) {
		case ai.StartEvent:
			if sink != nil {
				sink.OnState(nil, StateFrame{Phase: StatePhaseStarting})
			}
		case ai.TextDeltaEvent:
			if inner.Delta != "" {
				state.result.Reply += inner.Delta
				if sink != nil {
					sink.OnTextDelta(nil, inner.Delta)
				}
			}
		case ai.ToolCallEndEvent:
			call := piagentToolCall(inner.ToolCall)
			state.result.ToolCalls = append(state.result.ToolCalls, call)
			if sink != nil {
				sink.OnToolCall(nil, call)
			}
		case ai.DoneEvent:
			cp := inner
			state.doneEv = &cp
		case ai.ErrorEvent:
			cp := inner
			state.errEv = &cp
		}
	case agent.MessageEndEvent:
		if event.Message.Assistant != nil {
			asst := event.Message.Assistant
			if asst.StopReason == ai.StopReasonError || asst.StopReason == ai.StopReasonAborted {
				state.errEv = &ai.ErrorEvent{
					Reason: asst.StopReason,
					Error: &ai.AssistantMessage{
						StopReason:   asst.StopReason,
						ErrorMessage: asst.ErrorMessage,
					},
				}
			} else {
				state.doneEv = &ai.DoneEvent{
					Reason: asst.StopReason,
				}
			}
		}
	}
}

// emitResultToSink fires the OnState / OnError side
// effects based on the drain state. Pulled out so
// SendStream stays a thin driver and the per-state
// mapping logic is individually auditable. Mutates
// state.result.StopReason when the drain captured a
// DoneEvent so the SendStream caller sees the canonical
// stop reason alongside text + tool calls — even when
// sink is nil (Send's cycle-3a fallback path relies on
// the message-reply shape carrying the stop reason).
func emitResultToSink(state *drainState, turn turnResult, sink EventSink) {
	if state.cancelled && state.doneEv == nil && state.errEv == nil {
		// Cancel branch: no OnError fires here; the
		// dispatcher's Cancel handler already
		// emitted the cancelled frame.
		return
	}
	if state.doneEv == nil && state.errEv == nil && turn.err == nil {
		state.doneEv = &ai.DoneEvent{Reason: ai.StopReasonStop}
		if state.result.Reply == "" && len(turn.msgs) > 0 {
			state.result.Reply = extractAssistantText(turn.msgs)
		}
	}
	switch {
	case state.doneEv != nil:
		stopReason := stopReasonFromPiG(state.doneEv.Reason)
		state.result.StopReason = stopReason
		if sink != nil {
			sink.OnState(nil, StateFrame{Phase: StatePhaseDone, StopReason: stopReason})
			if turn.err != nil {
				sink.OnError(nil, turn.err)
			} else if failure := piagentTurnFailure(turn.msgs); failure != nil {
				sink.OnError(nil, failure)
			}
		}
	case state.errEv != nil:
		if sink != nil {
			var msg string
			if state.errEv.Error != nil {
				msg = state.errEv.Error.ErrorMessage
			}
			if msg == "" {
				msg = "piagent: upstream stream failed"
			}
			sink.OnError(nil, fmt.Errorf("piagent: %s", msg))
		}
	default:
		if sink != nil {
			if turn.err != nil {
				sink.OnError(nil, turn.err)
			} else {
				sink.OnError(nil, ErrSessionClosed)
			}
		}
	}
}

// mapStreamFailure returns the Go error SendStream hands
// back to the dispatcher. The accumulator's On* calls
// already fired in emitResultToSink; this function only
// reports the matching Go error.
func mapStreamFailure(state drainState, turn turnResult, turnCtx context.Context) error {
	if state.cancelled && state.doneEv == nil && state.errEv == nil {
		return turnCtx.Err()
	}
	switch {
	case state.doneEv != nil:
		if turn.err != nil {
			return turn.err
		}
		if failure := piagentTurnFailure(turn.msgs); failure != nil {
			return failure
		}
		return nil
	case state.errEv != nil:
		var msg string
		if state.errEv.Error != nil {
			msg = state.errEv.Error.ErrorMessage
		}
		if msg == "" {
			msg = "piagent: upstream stream failed"
		}
		return fmt.Errorf("piagent: %s", msg)
	default:
		if turn.err != nil {
			return turn.err
		}
		if failure := piagentTurnFailure(turn.msgs); failure != nil {
			return failure
		}
		if len(turn.msgs) > 0 {
			return nil
		}
		return ErrSessionClosed
	}
}
