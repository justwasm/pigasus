package agentlink

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startServer brings a server up on a loopback listener and returns its
// address and the handler's error channel.
func startServer(t *testing.T, cfg ServerConfig, h Handler) (string, <-chan error) {
	t.Helper()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errc := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { errc <- srv.Serve(ctx, ln, h) }()
	return ln.Addr().String(), errc
}

// TestServerMintsAToken pins the rule that a server always demands a
// token: one is minted when the configuration leaves it empty.
func TestServerMintsAToken(t *testing.T) {
	srv, err := NewServer(ServerConfig{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv.Token() == "" {
		t.Fatal("a server with no configured token minted none")
	}
	other, err := NewServer(ServerConfig{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if other.Token() == srv.Token() {
		t.Error("two servers minted the same token")
	}
}

// TestTokenRoundTrip pins the token's shape and its comparison.
func TestTokenRoundTrip(t *testing.T) {
	tok, err := MintToken()
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if len(tok) < 16 {
		t.Errorf("minted token is too short: %q", tok)
	}
	if strings.ContainsAny(tok, "=+/") {
		t.Errorf("minted token is not base64url without padding: %q", tok)
	}
	if !tokenMatches(tok, tok) {
		t.Error("a token does not match itself")
	}
	if !tokenMatches(tok, "  "+tok+"\t") {
		t.Error("a token does not match itself with whitespace around it")
	}
	if tokenMatches(tok, "other") {
		t.Error("a token matched a different one")
	}
	if tokenMatches("", tok) {
		t.Error("an empty expected token matched")
	}
	if tokenMatches(tok, "") {
		t.Error("an empty presented token matched")
	}
}

// TestClientRoundTrip is the end-to-end check: a client dials a server,
// sends a prompt, and reads the frames the server sends back.
func TestClientRoundTrip(t *testing.T) {
	const token = "s3cret-token-value"
	addr, _ := startServer(t, ServerConfig{Token: token}, HandlerFunc(func(ctx context.Context, conn *Conn) error {
		for {
			f, err := conn.Next(ctx)
			if err != nil {
				return err
			}
			if f.Kind != KindPrompt {
				continue
			}
			if err := conn.Send(ctx, Frame{Kind: KindHello, ID: f.ID, Version: "1.2.3", Workspace: "/tmp/ws"}); err != nil {
				return err
			}
			if err := conn.Send(ctx, Frame{Kind: KindText, ID: f.ID, Text: "hello "}); err != nil {
				return err
			}
			if err := conn.Send(ctx, Frame{Kind: KindText, ID: f.ID, Text: "world"}); err != nil {
				return err
			}
			if err := conn.Send(ctx, Frame{Kind: KindResult, ID: f.ID, Result: &Result{Reply: "hello world"}}); err != nil {
				return err
			}
			return nil
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, "ws://"+addr, DialOptions{Token: token})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if err := client.Send(ctx, Frame{Kind: KindPrompt, ID: "turn-1", Text: "hi", History: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var kinds []Kind
	var reply string
	for {
		f, err := client.Next(ctx)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		kinds = append(kinds, f.Kind)
		if f.Kind == KindResult && f.Result != nil {
			reply = f.Result.Reply
			break
		}
	}
	want := []Kind{KindHello, KindText, KindText, KindResult}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	if reply != "hello world" {
		t.Errorf("reply = %q, want %q", reply, "hello world")
	}
}

// TestServerRejectsAWrongToken pins the gate: a client without the
// server's token never reaches a websocket.
func TestServerRejectsAWrongToken(t *testing.T) {
	const token = "s3cret-token-value"
	served := make(chan struct{}, 1)
	addr, _ := startServer(t, ServerConfig{Token: token}, HandlerFunc(func(ctx context.Context, conn *Conn) error {
		served <- struct{}{}
		<-ctx.Done()
		return nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, "ws://"+addr, DialOptions{Token: "wrong"})
	if err == nil {
		client.Close()
		t.Fatal("a client with the wrong token was accepted")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error = %v, want the 401 the server answers", err)
	}

	client2, err := Dial(ctx, "ws://"+addr, DialOptions{})
	if err == nil {
		client2.Close()
		t.Fatal("a client with no token was accepted")
	}

	select {
	case <-served:
		t.Error("the handler ran for a rejected client")
	case <-time.After(100 * time.Millisecond):
	}
}

// TestServerServesOnItsOwnListener pins that Serve takes an existing
// listener, so a caller can learn the port before the serving starts.
func TestServerServesOnItsOwnListener(t *testing.T) {
	srv, err := NewServer(ServerConfig{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- srv.Serve(ctx, ln, HandlerFunc(func(context.Context, *Conn) error { return nil }))
	}()
	if got := srv.Addr(); got != ln.Addr().String() {
		t.Errorf("Addr = %q, want %q", got, ln.Addr().String())
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Serve did not return after the context was cancelled")
	}
}
