package browserbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestClientPromotesItselfWhenThePrimaryDies pins the client's own
// answer to a dead primary.
//
// The project's own client promotes itself: two seconds after the
// primary's socket closes it takes the port, and the extension's
// native host finds it there on its next retry. Without that, a client
// whose primary died is stranded — every later call fails with "the
// extension is not connected", for good.
//
// The proof is that the port answers again, and that it answers with
// the primary's own protocol: the promoted client greets a newcomer
// with the same acknowledgement the fake bridge used to send.
func TestClientPromotesItselfWhenThePrimaryDies(t *testing.T) {
	fake := newFakeBridge(t, func(tool string, args json.RawMessage) (any, string) {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}, ""
	})
	port := fake.port()

	ctx := context.Background()
	c, err := Connect(ctx, Config{Host: "127.0.0.1", Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// The primary goes away.
	fake.stop()

	// The client takes the port for itself.
	deadline := time.Now().Add(20 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			defer conn.Close()
			if _, err := conn.Write([]byte("{\"type\":\"client_hello\"}\n")); err != nil {
				t.Fatalf("greet the promoted client: %v", err)
			}
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			if err != nil {
				t.Fatalf("read the acknowledgement: %v", err)
			}
			var msg map[string]any
			if err := json.Unmarshal(line, &msg); err != nil {
				t.Fatalf("unmarshal the acknowledgement: %v (%s)", err, line)
			}
			if msg["type"] != "client_ack" {
				t.Fatalf("the promoted client answered %v, want a client_ack", msg["type"])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the client never took the port: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestClientJoinsTheWinnerWhenAnotherProcessTakesThePort pins the
// other half of the promotion.
//
// The port is a race: whoever binds it first is the primary. If
// another process won it while the client was waiting, the client
// joins that one instead of taking the port away from it — the same
// order the project's runtime uses.
func TestClientJoinsTheWinnerWhenAnotherProcessTakesThePort(t *testing.T) {
	fake := newFakeBridge(t, func(tool string, args json.RawMessage) (any, string) {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}, ""
	})
	port := fake.port()

	ctx := context.Background()
	c, err := Connect(ctx, Config{Host: "127.0.0.1", Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// The primary goes away, and somebody else takes the port before
	// the client's own promotion fires.
	fake.stop()
	winner := newFakeBridgeOn(t, port, func(tool string, args json.RawMessage) (any, string) {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}, ""
	})

	// The client finds the new primary and greets it.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if winner.helloCount() > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the client never joined the new primary")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
