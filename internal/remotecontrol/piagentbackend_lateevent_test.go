// Package remotecontrol — piagentbackend_lateevent_test.go: the
// regression test for the closed-channel race.
//
// PiG's event drain goroutine outlives the turn: the runtime's drain
// keeps forwarding until the session closes, so a session that has
// already returned can still have an event in flight. The backend
// used to close the event channel the moment the session returned,
// and the late write panicked with "send on closed channel" — in the
// daemon's tunnel goroutine, which took the whole process down.
//
// 【一手】 the panic, from the daemon log:
//
//	panic: send on closed channel
//	github.com/justwasm/pigasus/internal/piagentbridge.(*remoteCascadeSession).Send.func1
//		/Users/gear/Documents/GitHub/openagy/internal/piagentbridge/piagent_remote.go:276
//	github.com/justwasm/pigasus/internal/pigrt.chainEventFuncs.func1
//		/Users/gear/Documents/GitHub/openagy/internal/pigrt/hooks.go:124
//	github.com/justwasm/pigasus/internal/pigrt.(*Runtime).drain
//		/Users/gear/Documents/GitHub/openagy/internal/pigrt/hooks.go:56
package remotecontrol

import (
	"context"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
)

// lateEventSession is a Session double that emits an event *after*
// its Send returned — the shape PiG's runtime drain has.
type lateEventSession struct {
	delay   time.Duration
	late    agent.AgentEvent
	emitted chan struct{}
}

// Send spawns the late writer and returns immediately.
func (s *lateEventSession) Send(ctx context.Context, _ string, events chan<- agent.AgentEvent) ([]agent.AgentMessage, error) {
	go func() {
		defer close(s.emitted)
		time.Sleep(s.delay)
		select {
		case events <- s.late:
		case <-ctx.Done():
		}
	}()
	return nil, nil
}

// Close is a no-op.
func (s *lateEventSession) Close() error { return nil }

// TestPiagentBackend_LateEventDoesNotPanic pins the fix: the event
// channel is never closed, so the late write lands in the buffer
// instead of panicking.
//
// Before the fix this test crashed the whole test binary with
// "panic: send on closed channel".
func TestPiagentBackend_LateEventDoesNotPanic(t *testing.T) {
	sess := &lateEventSession{
		delay:   20 * time.Millisecond,
		late:    pigTextDelta("late"),
		emitted: make(chan struct{}),
	}
	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}

	if _, err := be.SendStream(context.Background(), MessagePrompt{Text: "ping"}, &captureSink{}, MessageSendOptions{}); err != nil {
		t.Fatalf("SendStream returned error: %v", err)
	}

	// The late write has to happen — and it has to not panic.
	select {
	case <-sess.emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("the late event was never written")
	}
}
