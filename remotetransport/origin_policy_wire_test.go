// Package remotetransport — origin_policy_wire_test.go:
// wire-level tests for the
// SetRemoteControlOriginPatterns JSON-RPC method
// (cycle rc-auth-tokens).
//
// Three concerns:
//
//   - happy-path round-trip: a SetRemoteControlOriginPatterns
//     call lands in the store, the WS transport's
//     active OriginPolicy mirrors the new state, and
//     the response envelope carries the brief-pinned
//     {ok, patterns, applied_count, rejected,
//     updated_at} shape.
//
//   - per-pattern validation: a request carrying
//     mixed valid + invalid entries rejects only the
//     invalid ones; the valid ones land; the
//     response echoes the rejected list verbatim.
//
//   - cross-conn broadcast: a successful Set on
//     connection A emits an origin_policy/changed
//     notification to a second open connection B
//     (the BroadcastNotify seam rc-permissions-grants
//     adds, shared with rc-plugins … rc-terminals).
//
// Mirrors the in-process
// internal/remotecontrol/origin_policy_dispatcher_test.go
// coverage: the in-process tests pin the
// envelope shape; the wire tests pin the
// envelope shape end-to-end through the
// ws upgrade + broadcaster + connection
// registry.
//
// Stdlib-only test setup; uses
// github.com/coder/websocket for the dial.
package remotetransport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// newTestTCPServerWithOriginPolicy brings up a
// TCPServer with the supplied
// remotecontrol.OriginPolicyStore pre-installed
// and the dispatcher's originPolicySetter wired
// to (*TCPServer).SetOriginPolicy. Mirrors
// newTestTCPServerWithFastpush in shape.
func newTestTCPServerWithOriginPolicy(t *testing.T, os_ remotecontrol.OriginPolicyStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:           sess,
		Agents:            remotecontrol.DefaultAgentLister,
		Reader:            remotecontrol.DefaultOutputReader{},
		OriginPolicyStore: os_,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	srv, err := NewTCPServer(context.Background(), disp, TCPConfig{
		Addr: "127.0.0.1:0",
		Path: "/rpc",
	})
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	// Wire the dispatcher's setter so the WS
	// transport's active policy mirrors the
	// store on every successful Set. This is
	// the production wiring from
	// cmd/wagyu/remote_control.go.
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// TestSetRemoteControlOriginPatterns_WS_Ok pins the
// happy path: a Set call lands in the store, the
// transport's OriginPolicy mirrors the new state,
// and the response envelope carries the
// {ok:true, patterns:[…sorted], applied_count,
// rejected:[], updated_at} shape. A follow-up
// Dial with a non-allowlisted origin returns 403
// because the WS transport now sees the new
// policy.
func TestSetRemoteControlOriginPatterns_WS_Ok(t *testing.T) {
	store := remotecontrol.NewMemOriginPolicyStore(nil)
	srv, done := newTestTCPServerWithOriginPolicy(t, store)
	defer done()

	// Dial with an allow-all empty policy so
	// the connect doesn't 403 on the upgrade.
	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	// Post the SetRemoteControlOriginPatterns
	// call.
	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteControlOriginPatterns",
		"params": map[string]any{
			"patterns": []string{
				"https://antigravity.google",
				"https://antigravity-companion.google",
			},
		},
	})
	resp := readJSONRPCResponse(t, c1)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteControlOriginPatterns: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteControlOriginPatterns: no result, got %#v", resp)
	}
	// Envelope shape.
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	patterns, _ := res["patterns"].([]any)
	if len(patterns) != 2 {
		t.Fatalf("result.patterns len = %d, want 2; got %#v", len(patterns), patterns)
	}
	wantOrder := []string{
		"https://antigravity-companion.google",
		"https://antigravity.google",
	}
	for i, w := range wantOrder {
		if got, _ := patterns[i].(string); got != w {
			t.Errorf("patterns[%d] = %q, want %q", i, got, w)
		}
	}
	if ua, _ := res["updated_at"].(string); ua == "" {
		t.Errorf("updated_at = %q, want non-empty", res["updated_at"])
	}
	rej, _ := res["rejected"].([]any)
	if len(rej) != 0 {
		t.Errorf("result.rejected = %v, want []", rej)
	}
	// Store snapshot reflects the new patterns.
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 2 {
		t.Errorf("store Patterns len = %d, want 2", len(state.Patterns))
	}
	// Transport's active policy mirrors the
	// on-disk state — an immediately-following
	// SetOriginPolicySnapshot read sees the new
	// patterns.
	snap := srv.OriginPolicySnapshot()
	if len(snap.Patterns) != 2 {
		t.Errorf("transport snapshot Patterns len = %d, want 2", len(snap.Patterns))
	}
}

// TestSetRemoteControlOriginPatterns_WS_BadParams pins
// the per-pattern validator's split-and-echo
// response. Mixed valid + invalid entries
// surface as a happy-path response with rejected
// carrying the verbatim invalid entries; the
// store lands only the accepted ones.
func TestSetRemoteControlOriginPatterns_WS_BadParams(t *testing.T) {
	store := remotecontrol.NewMemOriginPolicyStore(nil)
	srv, done := newTestTCPServerWithOriginPolicy(t, store)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteControlOriginPatterns",
		"params": map[string]any{
			"patterns": []string{
				"https://antigravity.google",
				"not a url",
				"file:///etc/passwd",
			},
		},
	})
	resp := readJSONRPCResponse(t, c1)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteControlOriginPatterns: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	patterns, _ := res["patterns"].([]any)
	if len(patterns) != 1 {
		t.Fatalf("patterns len = %d, want 1 (only the valid one); got %#v", len(patterns), patterns)
	}
	if got, _ := patterns[0].(string); got != "https://antigravity.google" {
		t.Errorf("patterns[0] = %q, want https://antigravity.google", got)
	}
	rej, _ := res["rejected"].([]any)
	if len(rej) != 2 {
		t.Fatalf("rejected len = %d, want 2", len(rej))
	}
	wantRejected := []string{"not a url", "file:///etc/passwd"}
	for i, w := range wantRejected {
		if got, _ := rej[i].(string); got != w {
			t.Errorf("rejected[%d] = %q, want %q", i, got, w)
		}
	}
}

// TestSetRemoteControlOriginPatterns_WS_NullPatterns
// pins that an explicit null patterns field
// surfaces -32602 "Invalid params".
func TestSetRemoteControlOriginPatterns_WS_NullPatterns(t *testing.T) {
	store := remotecontrol.NewMemOriginPolicyStore(nil)
	srv, done := newTestTCPServerWithOriginPolicy(t, store)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteControlOriginPatterns",
		"params":  map[string]any{"patterns": nil},
	})
	resp := readJSONRPCResponse(t, c1)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope; got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
	if msg, _ := errObj["message"].(string); msg != "Invalid params" {
		t.Errorf("error.message = %q, want Invalid params", msg)
	}
}

// TestSetRemoteControlOriginPatterns_WS_NonArrayPatterns
// pins that a non-array patterns field surfaces
// -32602 (the standard JSON unmarshal failure
// path).
func TestSetRemoteControlOriginPatterns_WS_NonArrayPatterns(t *testing.T) {
	store := remotecontrol.NewMemOriginPolicyStore(nil)
	srv, done := newTestTCPServerWithOriginPolicy(t, store)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteControlOriginPatterns",
		"params":  map[string]any{"patterns": "https://antigravity.google"},
	})
	resp := readJSONRPCResponse(t, c1)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope; got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestSetRemoteControlOriginPatterns_WS_BroadcastCrossConn
// pins the brief's
// `origin_policy/changed` notification reaches
// every other open ws connection. Connection A
// issues a successful Set, Connection B (a
// separate ws client that has not subscribed
// to anything) receives the broadcast
// notification through the server's per-conn
// outbound channel.
func TestSetRemoteControlOriginPatterns_WS_BroadcastCrossConn(t *testing.T) {
	store := remotecontrol.NewMemOriginPolicyStore(nil)
	srv, done := newTestTCPServerWithOriginPolicy(t, store)
	defer done()

	// Connection B: the listener. The server's
	// broadcaster fans the notification to
	// every open conn including B.
	cB := dialWS(t, srv.HTTPURL())
	defer cB.CloseNow()

	// Connection A: the mutator.
	cA := dialWS(t, srv.HTTPURL())
	defer cA.CloseNow()

	writeJSONRPC(t, cA, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteControlOriginPatterns",
		"params": map[string]any{
			"patterns": []string{"https://antigravity.google"},
		},
	})
	// cA gets the response first.
	respA := readJSONRPCResponse(t, cA)
	if _, ok := respA["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteControlOriginPatterns: error = %#v", respA["error"])
	}
	// cB must receive the origin_policy/changed
	// notification (it's not paired with a
	// response, just the broadcast). The first
	// frame on cB is the notification.
	notif := readJSONRPCNotification(t, cB)
	if notif.Method != "origin_policy/changed" {
		t.Errorf("notification method = %q, want origin_policy/changed", notif.Method)
	}
	pm := notif.Params
	if got, _ := pm["ok"].(bool); !got {
		t.Errorf("params.ok = %v, want true", pm["ok"])
	}
	patterns, _ := pm["patterns"].([]any)
	if len(patterns) != 1 {
		t.Errorf("params.patterns len = %d, want 1", len(patterns))
	}
	if got, _ := patterns[0].(string); got != "https://antigravity.google" {
		t.Errorf("params.patterns[0] = %q, want https://antigravity.google", got)
	}
	if ua, _ := pm["updated_at"].(string); ua == "" {
		t.Errorf("params.updated_at = %q, want non-empty", pm["updated_at"])
	}
}

// readJSONRPCNotification reads one JSON-RPC
// notification frame off the ws connection.
// Notifications are framed as
// {jsonrpc:"2.0", method:<name>, params:{…}}
// with no id — the dispatcher returns nil for
// notifications, but the wire envelope still
// arrives at the client.
//
// Returns the package-level jsonRPCNotification
// struct (the same struct tcp_test.go's
// readJSONRPCNotifications consumes).
func readJSONRPCNotification(t *testing.T, c *websocket.Conn) jsonRPCNotification {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, raw, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal notification: %v\nraw=%s", err, raw)
	}
	if env.Method == "" {
		t.Fatalf("not a notification: %s", raw)
	}
	params := map[string]any{}
	if len(env.Params) > 0 {
		if err := json.Unmarshal(env.Params, &params); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
	}
	return jsonRPCNotification{Method: env.Method, Params: params}
}

// TestSetRemoteControlOriginPatterns_WS_TransportMirror
// pins the post-mutation WS transport mirror:
// after a successful Set, the server's
// OriginPolicySnapshot reflects the new
// patterns. A subsequent Dial with a
// non-allowlisted Origin header is denied
// because the WS upgrade now consults the new
// policy.
//
// Note: the originating connection (cA) ALSO
// receives the origin_policy/changed
// notification (the broadcaster fans to every
// conn, including the caller) — but because
// the server's writer pushes the
// notification before the response frame,
// readJSONRPCResponse consumes and discards
// the notification on its way to the
// response. The cross-conn broadcast is
// pinned by
// TestSetRemoteControlOriginPatterns_WS_BroadcastCrossConn
// via a separate listener conn.
func TestSetRemoteControlOriginPatterns_WS_TransportMirror(t *testing.T) {
	store := remotecontrol.NewMemOriginPolicyStore(nil)
	srv, done := newTestTCPServerWithOriginPolicy(t, store)
	defer done()

	// Mutator conn.
	cA := dialWS(t, srv.HTTPURL())
	defer cA.CloseNow()

	writeJSONRPC(t, cA, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteControlOriginPatterns",
		"params": map[string]any{
			"patterns": []string{"https://antigravity.google"},
		},
	})
	respA := readJSONRPCResponse(t, cA)
	if _, ok := respA["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteControlOriginPatterns: error = %#v", respA["error"])
	}

	// The transport snapshot reflects the new state.
	snap := srv.OriginPolicySnapshot()
	if len(snap.Patterns) != 1 || snap.Patterns[0] != "https://antigravity.google" {
		t.Errorf("transport snapshot = %v, want [https://antigravity.google]", snap.Patterns)
	}

	// A new Dial with an attacker.example origin
	// is denied (the WS upgrade applies the
	// new policy). The library's
	// InsecureSkipVerify:true means the
	// library's authenticateOrigin is
	// bypassed; (*TCPServer).handleOriginCheck
	// is the canonical gatekeeper.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, srv.HTTPURL(), &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Origin": {"https://attacker.example"},
		},
	})
	if err == nil {
		_ = c.CloseNow()
		t.Fatalf("Dial with non-allowlisted origin should fail; got success")
	}
	if resp == nil {
		t.Fatalf("Dial returned no http.Response")
	}
	if resp.StatusCode != 403 {
		t.Errorf("status: got %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}
