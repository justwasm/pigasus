// Package remotecontrol — backend_stub.go: the
// StubBackend method implementations (emitInteractions /
// Send / SendStream). Pulled out of backend.go in cycle
// rc-filesize-internal-remotecontrol so the parent file
// stays focused on the type definitions and the
// dispatcher-agnostic seam.
package remotecontrol

import (
	"context"
	"time"
)

// emitInteractions walks StubBackend.Interactions in order.
// For each entry, it mints a PendingInteraction, calls
// sink.OnUserInteraction (which adds it to the cascade's
// registry + emits the cascade/user_interaction_required
// notification), then parks on intr.ResultCh up to
// InteractionWait (default 200ms). On receipt, it pushes
// the answer back to the dispatcher via the channel's
// blocking send (ResultCh is buffered len 1, so the send
// returns immediately after the receive completes).
//
// Returns the first interaction's ID + nil on full success
// through every configured interaction; returns the failing
// interaction's ID + ErrCascadeInteractionTimeout when the
// park times out before the dispatcher's handler resolves.
//
// Nil-safe: emitInteractions on a stub with no Interactions
// configured is a no-op (returns "", nil).
func (s *StubBackend) emitInteractions(ctx context.Context, sink EventSink) (string, error) {
	if len(s.Interactions) == 0 {
		return "", nil
	}
	wait := s.InteractionWait
	if wait <= 0 {
		wait = 200 * time.Millisecond
	}
	for i := range s.Interactions {
		intr, err := s.mintInteraction(i)
		if err != nil {
			return "", err
		}
		if sink != nil {
			sink.OnUserInteraction(nil, intr)
		}
		// Park on ResultCh. The dashboard's
		// HandleRemoteAgentCascadeUserInteraction
		// resolves the entry by ID, which fires the
		// receive on the buffered channel; an empty
		// Answers map leaves the test-side path idle
		// and the timeout fires.
		t := time.NewTimer(wait)
		select {
		case <-intr.ResultCh:
			t.Stop()
			// Got an answer; move on to the next
			// configured interaction.
		case <-t.C:
			return intr.ID, ErrCascadeInteractionTimeout
		case <-ctx.Done():
			t.Stop()
			return intr.ID, ctx.Err()
		}
	}
	return "", nil
}

// mintInteraction is the per-config helper that mints a
// fresh PendingInteraction from the StubBackend's
// Interactions[i] config. Pulled out of emitInteractions so
// the per-step construction is auditable.
func (s *StubBackend) mintInteraction(i int) (*PendingInteraction, error) {
	cfg := &s.Interactions[i]
	// Mint a fresh id; collisions within a stub's
	// lifetime are vanishingly unlikely (6 random
	// bytes; the test drives at most a handful of
	// interactions per turn).
	id, err := mintInteractionID()
	if err != nil {
		return nil, err
	}
	return &PendingInteraction{
		ID:        id,
		Kind:      cfg.Kind,
		Title:     cfg.Title,
		Prompt:    cfg.Prompt,
		Options:   cfg.Options,
		Dangerous: cfg.Dangerous,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: cfg.ExpiresAt,
		ResultCh:  make(chan InteractionResult, 1),
	}, nil
}

// Send implements MessageBackend. The contract:
//
//   - Sleep > 0 + ctx already cancelled → returns ctx.Err().
//   - Err != nil → returns Err verbatim.
//   - default → builds MessageResult from the configured
//     fields, defaulting StopReason to StopReasonEndTurn
//     when not set.
//
// Send never panics on nil receiver; the zero-value
// *StubBackend is a usable "do nothing" backend. A nil
// *StubBackend is also handled (the zero-value path).
func (s *StubBackend) Send(ctx context.Context, _ MessagePrompt) (MessageResult, error) {
	if s == nil {
		return MessageResult{StopReason: StopReasonEndTurn}, nil
	}
	if s.Sleep > 0 {
		t := time.NewTimer(s.Sleep)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return MessageResult{}, ctx.Err()
		}
	}
	if s.Err != nil {
		return MessageResult{}, s.Err
	}
	stop := s.StopReason
	if stop == "" {
		stop = StopReasonEndTurn
	}
	return MessageResult{
		Reply:             s.Reply,
		ToolCalls:         s.ToolCalls,
		StopReason:        stop,
		NewConversationID: s.ConversationID,
	}, nil
}

// SendStream implements StreamingMessageBackend (cycle
// rc-cascade-stream). It is the streaming counterpart of Send:
// the same canned Reply / ConversationID / ToolCalls / StopReason
// / Err / Sleep fields drive the same content, but the
// emission happens through the sink so a connected dashboard
// sees mid-turn notifications before the synchronous
// end-of-turn envelope.
//
// Event sequence emitted (one sink call per item):
//
//   - OnState(phase=starting)               ← always
//   - OnUserInteraction(intr) for each cfg  ← cycle-6, only when Interactions is non-empty
//   - OnTextDelta(Reply)                     ← when Reply != ""
//   - OnToolCall(call) for each ToolCalls[i] ← when non-empty
//   - OnState(phase=done, stopReason)        ← always
//
// Error path: Err != nil short-circuits to OnError (which the
// ConnNotifierSink turns into a cascade/state notification
// with phase=error) and returns the same error Send would.
// The cascade's LastReplyAt is left untouched — the brief:
// "an error is not a successful reply".
//
// Sleep handling matches Send: when Sleep > 0 the call blocks
// for that long (honouring ctx) before any emission. A test
// that wants a fast stub sets Sleep=0 and Reply="…".
//
// Nil receiver / nil sink: SendStream never panics on a nil
// receiver (it returns the zero MessageResult with
// StopReasonEndTurn, matching Send's behaviour) and tolerates
// a nil sink by skipping the event calls — same defensive
// discipline Send applies.
//
// Cycle rc-handle-user-interaction: between OnState(starting)
// and the text/tool calls, SendStream walks
// StubBackend.Interactions. Each entry produces one
// OnUserInteraction call (the sink adds the entry to the
// cascade's pending registry + emits the
// cascade/user_interaction_required notification); the
// stub then parks on the interaction's ResultCh up to
// InteractionWait (200ms default). If no answer arrives
// the stub returns ErrCascadeInteractionTimeout wrapped in
// ErrTurnFailed by the registry; the dispatcher surfaces
// that as -32603 (the brief: no custom code yet).
func (s *StubBackend) SendStream(ctx context.Context, _ MessagePrompt, sink EventSink, _ MessageSendOptions) (MessageResult, error) {
	if s == nil {
		emitStubNoSink(&sink)
		return MessageResult{StopReason: StopReasonEndTurn}, nil
	}
	if s.Sleep > 0 {
		t := time.NewTimer(s.Sleep)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			emitStubSinkError(&sink, ctx.Err())
			return MessageResult{}, ctx.Err()
		}
	}
	if s.Err != nil {
		emitStubSinkError(&sink, s.Err)
		return MessageResult{}, s.Err
	}

	// Cycle rc-handle-user-interaction: drive the
	// configured user-interaction sequence before any
	// text/tool emission. The dispatcher must see all
	// OnState / OnUserInteraction events before any text
	// (so a dashboard's UI knows the prompt is upcoming
	// in time to receive the next frames). On error
	// here, surface via sink.OnError and return
	// ErrCascadeInteractionTimeout (the registry wraps it
	// in ErrTurnFailed for the -32603 envelope).
	if _, err := s.emitInteractions(ctx, sink); err != nil {
		emitStubSinkError(&sink, err)
		return MessageResult{}, err
	}

	s.emitStubReplyEvents(sink)
	return MessageResult{
		Reply:             s.Reply,
		ToolCalls:         s.ToolCalls,
		StopReason:        s.resolveStubStopReason(),
		NewConversationID: s.ConversationID,
	}, nil
}

// emitStubNoSink handles the nil-receiver / non-nil-sink
// case — the stub still wants to drive the starting + done
// state frames on the sink so downstream observers see a
// complete lifecycle. Extracted from SendStream to keep
// the body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func emitStubNoSink(sink *EventSink) {
	if sink == nil || *sink == nil {
		return
	}
	(*sink).OnState(nil, StateFrame{Phase: StatePhaseStarting})
	(*sink).OnState(nil, StateFrame{Phase: StatePhaseDone, StopReason: StopReasonEndTurn})
}

// emitStubSinkError fires OnError on the sink (nil-safe)
// so a non-nil sink sees the failure frame even when
// SendStream returns early. Extracted from SendStream
// to keep the body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func emitStubSinkError(sink *EventSink, err error) {
	if sink == nil || *sink == nil || err == nil {
		return
	}
	(*sink).OnError(nil, err)
}

// resolveStubStopReason returns s.StopReason when
// non-empty, otherwise the canonical StopReasonEndTurn
// default. Extracted from SendStream to keep the body
// under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func (s *StubBackend) resolveStubStopReason() string {
	if s.StopReason != "" {
		return s.StopReason
	}
	return StopReasonEndTurn
}

// emitStubReplyEvents fires the post-interaction state
// + text + tool frames the stub runs in reply order.
// The sink nil-check is per-method so a future test that
// wants to drive a stub without a sink (e.g. to assert
// "no panics when sink is nil") can pass nil without
// crashing. Extracted from SendStream to keep the body
// under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func (s *StubBackend) emitStubReplyEvents(sink EventSink) {
	if sink == nil {
		return
	}
	stop := s.resolveStubStopReason()
	sink.OnState(nil, StateFrame{Phase: StatePhaseStarting})
	if s.Reply != "" {
		sink.OnTextDelta(nil, s.Reply)
	}
	for _, tc := range s.ToolCalls {
		if tc.Name == "" {
			continue
		}
		sink.OnToolCall(nil, tc)
	}
	sink.OnState(nil, StateFrame{Phase: StatePhaseDone, StopReason: stop})
}
