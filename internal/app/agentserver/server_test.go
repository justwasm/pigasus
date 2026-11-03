// agentserver_test.go — the server half of the client/server split.
//
// The server owns the workspace, the tools and the credentials; the
// client sends a transcript and receives the turn's frames. The tests
// below drive the server through the real wire, over a real websocket,
// because the wire is the contract.
package agentserver

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestMain(m *testing.M) {
	// The server reads the session's own configuration — the permission
	// rules, the MCP servers — so the tests must not read the user's.
	dir, err := os.MkdirTemp("", "agentserver-test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("ANTIGRAVITY_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// startServer brings a server up on a loopback listener.
func startServer(t *testing.T, cfg Config) (*Server, string) {
	t.Helper()
	if cfg.BackendFor == nil {
		cfg.BackendFor = func(string) backend.Client { return backend.NewMock() }
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx, ln) }()
	return srv, srv.Addr()
}

// TestServerMintsAToken pins that a server always demands a token: it
// runs shell commands against its own workspace, so an unauthenticated
// peer is never welcome.
func TestServerMintsAToken(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Token() == "" {
		t.Fatal("a server with no configured token minted none")
	}
	other, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if other.Token() == srv.Token() {
		t.Error("two servers minted the same token")
	}
}

// TestServerRejectsAWrongToken pins the gate: a client without the
// server's token never reaches a websocket.
func TestServerRejectsAWrongToken(t *testing.T) {
	_, addr := startServer(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := agentlink.Dial(ctx, "ws://"+addr, agentlink.DialOptions{Token: "wrong"})
	if err == nil {
		client.Close()
		t.Fatal("a client with the wrong token was accepted")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error = %v, want the 401 the server answers", err)
	}
}

// TestServerAnswersATurn is the end-to-end check: a client greets the
// server, asks for a turn, and receives the turn's frames.
func TestServerAnswersATurn(t *testing.T) {
	srv, addr := startServer(t, Config{Model: "test-model"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := agentlink.Dial(ctx, "ws://"+addr, agentlink.DialOptions{Token: srv.Token()})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	// The greeting names the server's workspace and model.
	if err := client.Send(ctx, agentlink.Frame{Kind: agentlink.KindHello}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	f, err := client.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Kind != agentlink.KindHello {
		t.Fatalf("the server answered the greeting with %q", f.Kind)
	}
	if f.Model != "test-model" {
		t.Errorf("the greeting named the model %q, want %q", f.Model, "test-model")
	}
	if f.Workspace == "" {
		t.Error("the greeting named no workspace")
	}

	// The turn replays the transcript the client sends.
	if err := client.Send(ctx, agentlink.Frame{
		Kind:    agentlink.KindPrompt,
		ID:      "turn-1",
		History: []agentlink.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var text strings.Builder
	var result *agentlink.Frame
	for {
		f, err := client.Next(ctx)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if f.ID != "" && f.ID != "turn-1" {
			t.Errorf("the frame carries the turn id %q, want %q", f.ID, "turn-1")
		}
		switch f.Kind {
		case agentlink.KindText:
			text.WriteString(f.Text)
		case agentlink.KindResult:
			fc := f
			result = &fc
		}
		if result != nil {
			break
		}
	}
	if !strings.Contains(text.String(), "mock reply") {
		t.Errorf("the turn's text frames carry %q, want the model's answer", text.String())
	}
	if result.Result == nil || !strings.Contains(result.Result.Reply, "mock reply") {
		t.Errorf("the turn ended with %+v, want the model's answer", result)
	}
}

// TestServerStopsACancelledTurn pins that a cancel frame reaches the
// turn and that the connection survives it: the session carries the
// next turn on the same websocket.
func TestServerStopsACancelledTurn(t *testing.T) {
	srv, addr := startServer(t, Config{Model: "test-model"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := agentlink.Dial(ctx, "ws://"+addr, agentlink.DialOptions{Token: srv.Token()})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if err := client.Send(ctx, agentlink.Frame{
		Kind:    agentlink.KindPrompt,
		ID:      "turn-1",
		History: []agentlink.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := client.Send(ctx, agentlink.Frame{Kind: agentlink.KindCancel, ID: "turn-1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The turn ends — with a result frame or with the connection's
	// end — and the session is still there afterwards.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, err := client.Next(ctx)
		if err != nil {
			break
		}
		if f.Kind == agentlink.KindResult {
			break
		}
	}

	// The connection still works: the server greets a second client
	// turn on it.
	if err := client.Send(ctx, agentlink.Frame{Kind: agentlink.KindHello}); err != nil {
		t.Fatalf("the connection did not survive the cancellation: %v", err)
	}
	f, err := client.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Kind != agentlink.KindHello {
		t.Fatalf("the server answered the greeting with %q", f.Kind)
	}
}
