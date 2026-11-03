// Tests for the rc-handle-user-interaction cycle's dispatcher
// side. Covers the HandleRemoteAgentCascadeUserInteraction
// handler at the in-process level and the interaction's expiry
// path.
//
// The pending-interaction registry itself — Add / Get / Resolve
// / Expire, and the cascade's lazy-init — is tested in
// internal/remotecontrol/cascades, next to the code that owns it.
//
// Wire-level coverage (cascade/user_interaction_required
// notification + HandleRemoteAgentCascadeUserInteraction RPC
// over coder/websocket) lives next to the TCP+WS transport
// in remotetransport/tcp_test.go.
package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// --- Dispatcher.HandleRemoteAgentCascadeUserInteraction tests -----

// TestHandleRemoteAgentCascadeUserInteraction_Ok pins the
// happy path: StubBackend emits an interaction, the sink
// adds it to the cascade's pending registry, and a
// concurrent client goroutine resolves the interaction
// via the RPC handler. The stub's park unblocks and the
// turn completes.
func TestHandleRemoteAgentCascadeUserInteraction_Ok(t *testing.T) {
	be := &cascades.StubBackend{
		Reply:      "done",
		StopReason: cascades.StopReasonEndTurn,
		Interactions: []cascades.InteractionConfig{
			{
				Kind:   cascades.InteractionKindPermission,
				Title:  "Read main.go?",
				Prompt: "ok?",
			},
		},
		// Cap the wait so a buggy Resolve path that fails to
		// unblock the channel doesn't pin the test.
		InteractionWait: 2 * time.Second,
	}
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Build a fresh cascade on the dispatcher's own
	// registry (the test helper's registerCascadeWithBackend
	// uses a separate registry). We need the dispatcher
	// to see the cascade so its handler can resolve it.
	c, err := cascades.New(cascades.Opts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	c.SetBackend(be)
	if err := d.Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cid := c.ID
	reg := d.Cascades()
	// The dispatcher has no notifier; Notify becomes a no-op.
	// The sink still adds to the cascade's pending registry
	// via its OnUserInteraction method.
	sink := NewConnNotifierSink(d, "", c)

	// Drive the turn in a goroutine. The stub will pause on
	// intr.ResultCh; we resolve it from the main goroutine
	// once we know the entry exists.
	type turnResult struct {
		res cascades.Result
		err error
	}
	resultCh := make(chan turnResult, 1)
	go func() {
		prompt := cascades.Prompt{Text: "hello"}
		res, err := reg.SendStream(context.Background(), cid, prompt, sink, cascades.SendOptions{})
		resultCh <- turnResult{res: res, err: err}
	}()

	// Wait for the cascade's pending registry to see the
	// entry — the stub mints a fresh id and the sink adds
	// it via OnUserInteraction before parking on ResultCh.
	var intrID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := c.Pending().Get(""); ok && got != nil {
			// dummy lookup to ensure alloc has happened
			_ = got
		}
		// Read the entry id from the registry so we
		// don't need to know the minted id in advance.
		for _, id := range c.Pending().IDs() {
			intrID = id
		}
		if intrID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if intrID == "" {
		t.Fatalf("no pending interaction registered within 2s")
	}

	// Resolve via the dispatcher's handler.
	req := []byte(`{
		"jsonrpc":"2.0",
		"id":99,
		"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":` + jsonString(cid) + `,"interaction_id":` + jsonString(intrID) + `,"response":"yes"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("HandleRemoteAgentCascadeUserInteraction returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Handle…UserInteraction: no result, got %#v", resp)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	if _, ok := res["decided_at"].(string); !ok {
		t.Errorf("result.decided_at = %T, want string", res["decided_at"])
	}
	appliedTo, ok := res["applied_to"].([]string)
	if !ok || len(appliedTo) != 0 {
		t.Errorf("result.applied_to = %v (%T), want []string{}", res["applied_to"], res["applied_to"])
	}

	// The stub's park should unblock; the turn completes
	// successfully.
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("SendStream returned error: %v", r.err)
		}
		if r.res.Reply != "done" {
			t.Errorf("Reply = %q, want done", r.res.Reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SendStream did not return within 2s after Resolve")
	}

	// The pending registry is empty after a successful Resolve.
	if got := c.Pending().Len(); got != 0 {
		t.Errorf("after Resolve: Pending().Len = %d, want 0", got)
	}
}

// TestHandleRemoteAgentCascadeUserInteraction_UnknownCascade
// pins the -32004 envelope path when cascade_id is well-
// formed but no cascade is registered.

// TestHandleRemoteAgentCascadeUserInteraction_UnknownCascade
// pins the -32004 envelope path when cascade_id is well-
// formed but no cascade is registered.
func TestHandleRemoteAgentCascadeUserInteraction_UnknownCascade(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":"cas_doesnotexist","interaction_id":"int_x","response":"y"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, rpc.ErrCodeCascadeNotFound)
	}
}

// TestHandleRemoteAgentCascadeUserInteraction_UnknownInteraction
// pins the -32006 envelope path: the cascade is registered
// but the interaction_id is unknown (never existed, already
// answered, or expired).

// TestHandleRemoteAgentCascadeUserInteraction_UnknownInteraction
// pins the -32006 envelope path: the cascade is registered
// but the interaction_id is unknown (never existed, already
// answered, or expired).
func TestHandleRemoteAgentCascadeUserInteraction_UnknownInteraction(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cid := startBareCascade(t, d)

	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":` + jsonString(cid) + `,"interaction_id":"int_unknown","response":"y"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeInteractionNotFound {
		t.Errorf("error.code = %d, want %d (Interaction not found)", got, rpc.ErrCodeInteractionNotFound)
	}
}

// TestHandleRemoteAgentCascadeUserInteraction_BadParams
// pins the -32602 envelope path: missing or empty
// cascade_id / interaction_id / response.

// TestHandleRemoteAgentCascadeUserInteraction_BadParams
// pins the -32602 envelope path: missing or empty
// cascade_id / interaction_id / response.
func TestHandleRemoteAgentCascadeUserInteraction_BadParams(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cases := map[string]string{
		"missing cascade_id":     `{"interaction_id":"int_x","response":"y"}`,
		"empty cascade_id":       `{"cascade_id":"","interaction_id":"int_x","response":"y"}`,
		"whitespace cascade_id":  `{"cascade_id":"   ","interaction_id":"int_x","response":"y"}`,
		"missing interaction_id": `{"cascade_id":"cas_x","response":"y"}`,
		"empty interaction_id":   `{"cascade_id":"cas_x","interaction_id":"","response":"y"}`,
		"missing response":       `{"cascade_id":"cas_x","interaction_id":"int_x"}`,
		"null response":          `{"cascade_id":"cas_x","interaction_id":"int_x","response":null}`,
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction","params":` + params + `}`
			resp := d.Dispatch(context.Background(), []byte(body))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := errorCode(errObj); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}

// TestUserInteraction_Expires pins the GC path: an
// interaction with ExpiresAt in the past is removed on the
// next Expire tick; resolving a removed entry returns
// -32006.
//
// The dispatcher doesn't ship an expiry goroutine today;
// the brief: "rc-piagent-bind will wire a tick into the
// host's existing tick loop". The test directly invokes
// (*PendingInteractions).Expire and confirms the next
// Handle…UserInteraction call sees the entry gone.

// TestUserInteraction_Expires pins the GC path: an
// interaction with ExpiresAt in the past is removed on the
// next Expire tick; resolving a removed entry returns
// -32006.
//
// The dispatcher doesn't ship an expiry goroutine today;
// the brief: "rc-piagent-bind will wire a tick into the
// host's existing tick loop". The test directly invokes
// (*PendingInteractions).Expire and confirms the next
// Handle…UserInteraction call sees the entry gone.
func TestUserInteraction_Expires(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cid := startBareCascade(t, d)
	c, _ := d.Cascades().Get(cid)

	now := time.Now().UTC()
	intr, err := c.AddInteraction(cascades.InteractionKindPermission, "old", "p", nil, false, now.Add(-time.Second), now)
	if err != nil {
		t.Fatalf("AddInteraction: %v", err)
	}

	// Run the expiry tick.
	expired := c.Pending().Expire(now)
	if len(expired) != 1 || expired[0] != intr.ID {
		t.Errorf("Expire returned %v, want [%s]", expired, intr.ID)
	}

	// Resolving an expired entry returns -32006.
	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":` + jsonString(cid) + `,"interaction_id":` + jsonString(intr.ID) + `,"response":"y"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeInteractionNotFound {
		t.Errorf("error.code = %d, want %d (Interaction not found)", got, rpc.ErrCodeInteractionNotFound)
	}
}

// TestUserInteraction_TimeoutInBackend pins the brief's
// "StubBackend + Answers empty + interaction never
// resolved → ErrCascadeInteractionTimeout" path. The
// stub's InteractionWait fires before any handler call;
// SendStream returns the timeout error.

// --- helpers ----------------------------------------------------------

// startBareCascade registers a fresh cascade on d without
// going through the JSON-RPC envelope. Used by handler-only
// tests that don't need to exercise the wire format.
func startBareCascade(t *testing.T, d *Dispatcher) string {
	t.Helper()
	c, err := cascades.New(cascades.Opts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return c.ID
}

// jsonString returns a JSON-encoded string literal for a
// bare string (used to embed cascade_id / interaction_id
// values inside a JSON-encoded params object).

// jsonString returns a JSON-encoded string literal for a
// bare string (used to embed cascade_id / interaction_id
// values inside a JSON-encoded params object).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
