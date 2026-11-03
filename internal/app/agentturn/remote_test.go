package agentturn

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
)

// startLink brings an agentlink server up on a loopback listener and
// returns its address.
//
// The handler is the server half of the test: it answers the prompts
// the client sends.
func startLink(t *testing.T, h agentlink.Handler) (string, string) {
	t.Helper()
	srv, err := agentlink.NewServer(agentlink.ServerConfig{Token: "secret-token"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx, ln, h) }()
	return ln.Addr().String(), srv.Token()
}

// TestRemoteTurnCarriesTheTranscript pins the remote engine's contract:
// the transcript travels with the prompt, and the server's frames come
// back unchanged.
func TestRemoteTurnCarriesTheTranscript(t *testing.T) {
	var (
		mu       sync.Mutex
		received agentlink.Frame
	)
	addr, token := startLink(t, agentlink.HandlerFunc(func(ctx context.Context, conn *agentlink.Conn) error {
		for {
			f, err := conn.Next(ctx)
			if err != nil {
				return err
			}
			if f.Kind != agentlink.KindPrompt {
				continue
			}
			mu.Lock()
			received = f
			mu.Unlock()
			if err := conn.Send(ctx, agentlink.Frame{Kind: agentlink.KindText, ID: f.ID, Text: "the "}); err != nil {
				return err
			}
			if err := conn.Send(ctx, agentlink.Frame{Kind: agentlink.KindText, ID: f.ID, Text: "answer"}); err != nil {
				return err
			}
			return conn.Send(ctx, agentlink.Frame{
				Kind:   agentlink.KindResult,
				ID:     f.ID,
				Result: &agentlink.Result{Reply: "the answer"},
			})
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := agentlink.Dial(ctx, "ws://"+addr, agentlink.DialOptions{Token: token})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	engine := NewRemote(client)
	frames, err := engine.Turn(ctx, Turn{
		Model:   "the-model",
		History: []agentlink.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	got := collect(t, frames)

	var text string
	var result *agentlink.Frame
	for i := range got {
		f := got[i]
		switch f.Kind {
		case agentlink.KindText:
			text += f.Text
		case agentlink.KindResult:
			result = &f
		}
	}
	if text != "the answer" {
		t.Errorf("the text frames carry %q, want %q", text, "the answer")
	}
	if result == nil || result.Result == nil || result.Result.Reply != "the answer" {
		t.Fatalf("the turn ended with %+v, want the answer", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Model != "the-model" {
		t.Errorf("the prompt asked for %q, want %q", received.Model, "the-model")
	}
	if len(received.History) != 1 || received.History[0].Content != "hello" {
		t.Errorf("the prompt carried %+v, want the transcript", received.History)
	}
}

// TestRemoteTurnSendsTheCancelFrame pins that a cancelled turn asks the
// server to stop, instead of letting it run to the end.
func TestRemoteTurnSendsTheCancelFrame(t *testing.T) {
	cancelled := make(chan struct{}, 1)
	addr, token := startLink(t, agentlink.HandlerFunc(func(ctx context.Context, conn *agentlink.Conn) error {
		for {
			f, err := conn.Next(ctx)
			if err != nil {
				return err
			}
			if f.Kind == agentlink.KindCancel {
				select {
				case cancelled <- struct{}{}:
				default:
				}
			}
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := agentlink.Dial(ctx, "ws://"+addr, agentlink.DialOptions{Token: token})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	engine := NewRemote(client)
	turnCtx, cancelTurn := context.WithCancel(ctx)
	frames, err := engine.Turn(turnCtx, Turn{})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	cancelTurn()
	collect(t, frames)

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Error("the cancelled turn sent no cancel frame")
	}
}

// TestRemoteTurnRefusesASecondTurn pins that the protocol carries one
// turn at a time per connection: a second turn would read the first
// one's frames.
func TestRemoteTurnRefusesASecondTurn(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	addr, token := startLink(t, agentlink.HandlerFunc(func(ctx context.Context, conn *agentlink.Conn) error {
		for {
			f, err := conn.Next(ctx)
			if err != nil {
				return err
			}
			if f.Kind == agentlink.KindPrompt {
				<-release
				return conn.Send(ctx, agentlink.Frame{
					Kind:   agentlink.KindResult,
					ID:     f.ID,
					Result: &agentlink.Result{Reply: "done"},
				})
			}
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := agentlink.Dial(ctx, "ws://"+addr, agentlink.DialOptions{Token: token})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	engine := NewRemote(client)
	if _, err := engine.Turn(ctx, Turn{}); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	// The first turn is still in flight: it has not read its result
	// yet, because the frames are read by the turn's own goroutine.
	if _, err := engine.Turn(ctx, Turn{}); err == nil {
		t.Error("a second turn was accepted while the first was in flight")
	}
}
