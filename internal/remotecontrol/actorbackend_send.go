// Package remotecontrol — actorbackend_send.go:
// SendStream implementation + per-turn drain helpers.
package remotecontrol

import (
	"context"
	"fmt"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

type turnResult struct {
	msgs []SessionMessage
	err  error
}

type drainState struct {
	result     cascades.Result
	doneReason string
	hasDone    bool
	err        error
	hasErr     bool
	cancelled  bool
}

func deriveTurnCtx(ctx context.Context, opts cascades.SendOptions) (context.Context, context.CancelFunc) {
	if opts.Timeout <= 0 {
		return ctx, nil
	}
	return context.WithTimeout(ctx, opts.Timeout)
}

func (b *ActorBackend) startSession(turnCtx context.Context, promptText string, sess Session) (chan SessionEvent, chan turnResult, chan struct{}) {
	events := make(chan SessionEvent, 128)
	done := make(chan turnResult, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		msgs, err := sess.Send(turnCtx, promptText, events)
		done <- turnResult{msgs: msgs, err: err}
	}()
	return events, done, finished
}

func (b *ActorBackend) drainEvents(events chan SessionEvent, finished <-chan struct{}, turnCtx context.Context, sink cascades.EventSink) drainState {
	state := drainState{}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return state
			}
			applyEvent(ev, &state, sink)
		case <-finished:
			drainRemaining(events, &state, sink)
			return state
		case <-turnCtx.Done():
			state.cancelled = true
		}
	}
}

func drainRemaining(events chan SessionEvent, state *drainState, sink cascades.EventSink) {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			applyEvent(ev, state, sink)
		default:
			return
		}
	}
}

func applyEvent(ev SessionEvent, state *drainState, sink cascades.EventSink) {
	if state.cancelled {
		return
	}
	switch ev.Type {
	case SessionEventStart:
		if sink != nil {
			sink.OnState(nil, cascades.StateFrame{Phase: cascades.StatePhaseStarting})
		}
	case SessionEventTextDelta:
		if ev.Delta != "" {
			state.result.Reply += ev.Delta
			if sink != nil {
				sink.OnTextDelta(nil, ev.Delta)
			}
		}
	case SessionEventToolCall:
		state.result.ToolCalls = append(state.result.ToolCalls, ev.ToolCall)
		if sink != nil {
			sink.OnToolCall(nil, ev.ToolCall)
		}
	case SessionEventDone:
		state.hasDone = true
		state.doneReason = ev.StopReason
		if state.doneReason == "" {
			state.doneReason = cascades.StopReasonEndTurn
		}
	case SessionEventError:
		state.hasErr = true
		state.err = ev.Error
	}
}

func emitResultToSink(state *drainState, turn turnResult, sink cascades.EventSink) {
	if state.cancelled && !state.hasDone && !state.hasErr {
		return
	}
	if !state.hasDone && !state.hasErr && turn.err == nil {
		state.hasDone = true
		state.doneReason = cascades.StopReasonEndTurn
		if state.result.Reply == "" && len(turn.msgs) > 0 {
			state.result.Reply = extractAssistantText(turn.msgs)
		}
	}
	switch {
	case state.hasDone:
		stopReason := mapStopReason(state.doneReason)
		state.result.StopReason = stopReason
		if sink != nil {
			sink.OnState(nil, cascades.StateFrame{Phase: cascades.StatePhaseDone, StopReason: stopReason})
			if turn.err != nil {
				sink.OnError(nil, turn.err)
			} else if failure := actorTurnFailure(turn.msgs); failure != nil {
				sink.OnError(nil, failure)
			}
		}
	case state.hasErr:
		if sink != nil {
			msg := "actor: upstream stream failed"
			if state.err != nil {
				msg = state.err.Error()
			}
			sink.OnError(nil, fmt.Errorf("%s", msg))
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

func assembleStreamResult(state drainState, turn turnResult, turnCtx context.Context) (cascades.Result, error) {
	if state.cancelled && !state.hasDone && !state.hasErr {
		return state.result, turnCtx.Err()
	}
	switch {
	case state.hasDone:
		if turn.err != nil {
			return state.result, turn.err
		}
		if failure := actorTurnFailure(turn.msgs); failure != nil {
			return state.result, failure
		}
		if state.result.Reply == "" && len(turn.msgs) > 0 {
			state.result.Reply = extractAssistantText(turn.msgs)
		}
		return state.result, nil
	case state.hasErr:
		return state.result, state.err
	default:
		if turn.err != nil {
			return state.result, turn.err
		}
		if failure := actorTurnFailure(turn.msgs); failure != nil {
			return state.result, failure
		}
		if len(turn.msgs) > 0 {
			return state.result, nil
		}
		return state.result, ErrSessionClosed
	}
}

// SendStream implements cascades.StreamingBackend.
func (b *ActorBackend) SendStream(ctx context.Context, prompt cascades.Prompt, sink cascades.EventSink, opts cascades.SendOptions) (cascades.Result, error) {
	if b == nil || b.SessionFactory == nil {
		return cascades.Result{}, ErrNilSessionFactory
	}

	turnCtx, cancel := deriveTurnCtx(ctx, opts)
	if cancel != nil {
		defer cancel()
	}

	sess, err := b.SessionFactory(turnCtx, prompt, opts)
	if err != nil {
		return cascades.Result{}, fmt.Errorf("actor: session factory: %w", err)
	}
	defer func() {
		_ = sess.Close()
	}()

	events, done, finished := b.startSession(turnCtx, prompt.Text, sess)
	state := b.drainEvents(events, finished, turnCtx, sink)
	turn := <-done

	emitResultToSink(&state, turn, sink)
	return assembleStreamResult(state, turn, turnCtx)
}
