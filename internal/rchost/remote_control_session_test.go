package rchost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

func TestRemoteCascadeSession_Send(t *testing.T) {
	mockBackend := backend.NewMock()
	factory := NewRemoteCascadeSession(RemoteCascadeOptions{
		Backend:   mockBackend,
		Workspace: t.TempDir(),
		AgentDir:  t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := factory(ctx, cascades.Prompt{
		Model:          "mock-model",
		ConversationID: "test-conv-1",
	}, cascades.SendOptions{})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	defer sess.Close()

	events := make(chan remotecontrol.SessionEvent, 32)
	done := make(chan struct{})
	var textReceived strings.Builder
	var sawStart, sawDone bool

	go func() {
		defer close(done)
		for ev := range events {
			switch ev.Type {
			case remotecontrol.SessionEventStart:
				sawStart = true
			case remotecontrol.SessionEventTextDelta:
				textReceived.WriteString(ev.Delta)
			case remotecontrol.SessionEventDone:
				sawDone = true
			}
		}
	}()

	msgs, err := sess.Send(ctx, "Hello from remote cascade test", events)
	close(events)
	<-done

	if err != nil {
		t.Fatalf("sess.Send failed: %v", err)
	}
	if !sawStart {
		t.Error("expected SessionEventStart")
	}
	if !sawDone {
		t.Error("expected SessionEventDone")
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one SessionMessage")
	}
	if !strings.Contains(msgs[0].Content, "mock reply") {
		t.Errorf("expected mock reply in message content, got: %q", msgs[0].Content)
	}
	if !strings.Contains(textReceived.String(), "mock reply") {
		t.Errorf("expected mock reply in streamed text, got: %q", textReceived.String())
	}
}
