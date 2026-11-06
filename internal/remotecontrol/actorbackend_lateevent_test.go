package remotecontrol

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

type lateEventSession struct {
	delay   time.Duration
	late    SessionEvent
	emitted chan struct{}
}

func (s *lateEventSession) Send(ctx context.Context, _ string, events chan<- SessionEvent) ([]SessionMessage, error) {
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

func (s *lateEventSession) Close() error { return nil }

func TestPiagentBackend_LateEventDoesNotPanic(t *testing.T) {
	sess := &lateEventSession{
		delay:   20 * time.Millisecond,
		late:    pigTextDelta("late"),
		emitted: make(chan struct{}),
	}
	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}

	if _, err := be.SendStream(context.Background(), cascades.Prompt{Text: "ping"}, &captureSink{}, cascades.SendOptions{}); err != nil {
		t.Fatalf("SendStream returned error: %v", err)
	}

	select {
	case <-sess.emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("the late event was never written")
	}
}
