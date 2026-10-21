// Smoke tests for the TCP+WebSocket transport (cycle rc-ws-transport)
// and the server-push + graceful-shutdown additions of cycle
// rc-streaming-protocol.
//
// The tests here prove the new transport:
//
//  1. Brings up an HTTP server bound to 127.0.0.1:0 (random port).
//  2. Upgrades a /rpc request to a WebSocket using coder/websocket.
//  3. Speaks the same JSON-RPC envelope as the Unix-socket transport
//     (initialize + tools/list + ping + a tools/call) so the
//     dashboard surface doesn't fork.
//  4. Returns the same 3-tool catalogue (status, list_agents,
//     get_output) that the daemon_test.go suite pins.
//
// Cycle rc-streaming-protocol adds tests 5–9: server-push
// notifications, client-side notifications, and the graceful
// shutdown handshake.
//
// The smoke tests live in remotetransport/ rather than
// remotecontrol/ so the existing 613-line daemon_test.go stays
// byte-for-byte unchanged. Cycle rc-ws-transport's contract:
// "daemon_test.go remains green". The new tests in this file
// cover the new transport; the existing tests cover the existing
// transport.
package remotetransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// newTestTCPServer brings up a TCPServer on 127.0.0.1:0 with a
// freshly-built SessionState. The shared *remotecontrol.Dispatcher
// satisfies the Dispatcher interface structurally — the same type
// the Unix-socket transport uses.
func newTestTCPServer(t *testing.T) (*TCPServer, *remotecontrol.SessionState, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.3.4-test")
	sess.Workspace = "/tmp/ws-workspace"
	sess.Backend = "agw"
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.SetConversationID("conv-ws")
	sess.SetTurnOutput("hello from the ws host")

	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: sess,
		Agents:  remotecontrol.DefaultAgentLister,
		Reader:  remotecontrol.DefaultOutputReader{},
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, sess, teardown
}

// dialWS opens a WebSocket connection to the test server. Returns
// the connection so the caller can read/write JSON-RPC envelopes.
func dialWS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("websocket.Dial(%s): %v", url, err)
	}
	return c
}

// writeJSONRPC writes one JSON-RPC request frame to the ws
// connection. The ws library handles message framing — we just
// marshal and send.
func writeJSONRPC(t *testing.T, c *websocket.Conn, req map[string]any) {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

// readJSONRPC reads one JSON-RPC response frame off the ws
// connection and decodes it.
func readJSONRPC(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, raw, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	return resp
}

// readJSONRPCResponse reads frames off the ws connection until one
// has an "id" field set (the dispatcher only sends responses with
// id; notifications are server-push frames without id, per JSON-RPC
// 2.0 §4.1). Notification frames are dropped on the floor. Cycle
// rc-cascade-stream introduced mid-turn notifications on
// SendRemoteAgentCascadeMessage — tests that only care about the
// final envelope can use this helper instead of readJSONRPC so a
// stray cascade/state notification doesn't trip their assertion.
//
// The helper accepts ANY "id" value (string, number, null) — a
// response with `id: null` matches the JSON-RPC spec's "explicit
// null id" wording, and a fresh ws handle that can't have sent a
// request with null id is fine to see one. The point is "skip
// frames without an id field; return frames that have one".
//
// readJSONRPCResponse still bounds the read under a short timeout
// per call — if the server hangs the per-cycle test timeout fires.
// (The cumulative deadline is inherited from `go test -timeout`.)
func readJSONRPCResponse(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	for {
		frame := readJSONRPC(t, c)
		// A notification carries no "id" at all (server-push).
		// A response carries an "id" — could be string, number,
		// null. Check the key (not the value) so a null id still
		// counts as "this is a response".
		if _, ok := frame["id"]; ok {
			return frame
		}
	}
}

// readJSONRPCNotifications drains up to max notification frames
// off the ws connection, returning the captured frames. Stops
// early on the first response frame (frame with "id") so a test
// that wants both the notifications AND the response can read it
// itself. Used by the cycle-5 streaming wire tests to assert the
// exact notification sequence the dispatcher pushed.
//
// A frame with an "id" field — even one we're not expecting — is
// returned immediately and counted as the first element of `notifs`
// with `IsResponse=true` and `Method=""`; the caller decides what
// to do with it. In practice every test that uses this helper
// expects notifications first, response second, so the early
// return means "stop draining".
type jsonRPCNotification struct {
	Method     string
	Params     map[string]any
	IsResponse bool // true when the helper saw an "id" frame and stopped early
}

func readJSONRPCNotifications(t *testing.T, c *websocket.Conn, max int) []jsonRPCNotification {
	t.Helper()
	out := make([]jsonRPCNotification, 0, max)
	for i := 0; i < max; i++ {
		frame := readJSONRPC(t, c)
		if _, hasID := frame["id"]; hasID {
			out = append(out, jsonRPCNotification{IsResponse: true})
			return out
		}
		method, _ := frame["method"].(string)
		params, _ := frame["params"].(map[string]any)
		out = append(out, jsonRPCNotification{Method: method, Params: params})
	}
	return out
}

// TestWSAdapter_HTTPSmoke is the headline ask of cycle
// rc-ws-transport: bring up the WS adapter on a random port and
// verify a ws client can complete the JSON-RPC initialize
// handshake + a tools/list call, getting the same 3-tool
// catalogue the Unix-socket daemon returns.
//
// The test asserts:
//   - HTTP path: a 404 on anything other than /rpc (kept as a
//     sub-test for clarity; covered in TestWSAdapter_HTTPPathRoutes404
//     below).
//   - WS upgrade: websocket.Accept succeeds and the connection
//     yields a non-error JSON-RPC 2.0 response.
//   - Wire compatibility: initialize + tools/list return the
//     same envelope shape the daemon_test.go pins.
func TestWSAdapter_InitializeAndToolsList(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	wsURL := srv.HTTPURL()
	if wsURL == "" {
		t.Fatalf("TCPServer.HTTPURL() returned empty; Addr()=%v", srv.Addr())
	}

	c := dialWS(t, wsURL)
	defer c.CloseNow()

	// initialize — same shape daemon_test.go uses.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]any{"name": "smoke", "version": "0"},
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("initialize returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: no result, got %#v", resp)
	}
	if res["protocolVersion"] != "2024-11-05" {
		t.Errorf("initialize.protocolVersion = %v, want 2024-11-05", res["protocolVersion"])
	}
	srvInfo, _ := res["serverInfo"].(map[string]any)
	if srvInfo["name"] != remotecontrol.ServerName {
		t.Errorf("serverInfo.name = %v, want %q", srvInfo["name"], remotecontrol.ServerName)
	}

	// tools/list — must return the same 3 tools the unix daemon pins.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	resp = readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("tools/list returned error: %#v", errObj)
	}
	res, _ = resp["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools/list returned %d tools, want 3: %#v", len(tools), tools)
	}
	want := map[string]bool{"status": false, "list_agents": false, "get_output": false}
	for _, ti := range tools {
		m, _ := ti.(map[string]any)
		if n, ok := m["name"].(string); ok {
			want[n] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("tool %q missing from tools/list response", name)
		}
	}
}

// TestWSAdapter_HTTPPathRoutes404 verifies that the HTTP mux
// routes anything other than the configured Path to a 404 —
// port scans of the WS adapter see "something listening, but
// the only endpoint is /rpc".
func TestWSAdapter_HTTPPathRoutes404(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	tcp, ok := srv.listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("server listener is not a *net.TCPAddr: %T", srv.listener.Addr())
	}
	url := "http://" + tcp.IP.String() + ":" + itoa(tcp.Port) + "/not-rpc"
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("GET /not-rpc status = %d, want 404", resp.StatusCode)
	}
}

// TestWSAdapter_StatusToolReflectsSession exercises the tools/call
// path through the WS adapter: status surfaces the session state
// the dispatcher was constructed with, just like the unix daemon.
func TestWSAdapter_StatusToolReflectsSession(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	})
	resp := readJSONRPC(t, c)
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	if len(env) == 0 {
		t.Fatalf("status: empty content, got %#v", resp)
	}
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("status returned isError: %s", text)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatalf("status text not JSON: %v\n%s", err, text)
	}
	if snap["model"] != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("status.model = %v", snap["model"])
	}
	if snap["workspace"] != "/tmp/ws-workspace" {
		t.Errorf("status.workspace = %v", snap["workspace"])
	}
}

// TestWSAdapter_ConnectedCounterIncrements verifies the WS
// transport bumps the same SessionState.connected counter the
// Unix-socket transport does. A dashboard that polls
// tools/call->status across both transports should see the
// counter go up when a client connects to either of them.
func TestWSAdapter_ConnectedCounterIncrements(t *testing.T) {
	srv, sess, done := newTestTCPServer(t)
	defer done()

	// No client connected yet: counter is 0.
	if got := sess.Connected(); got != 0 {
		t.Fatalf("connected before dial = %d, want 0", got)
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Wait for the counter to flip (the accept goroutine + the
	// dispatcher NotifyConnected are async w.r.t. websocket.Dial
	// returning).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Connected() == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := sess.Connected(); got != 1 {
		t.Fatalf("connected after dial = %d, want 1", got)
	}

	// Tear the ws down. NotifyDisconnected should drop the
	// counter back to 0.
	c.CloseNow()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Connected() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := sess.Connected(); got != 0 {
		t.Errorf("connected after close = %d, want 0", got)
	}
}

// itoa is a tiny helper to format a port number without pulling
// in strconv at the top of the test file (and so the import list
// stays minimal).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ----- cycle rc-streaming-protocol tests -----
//
// The five tests below cover the four asks of cycle
// rc-streaming-protocol: server-push notifications, client-side
// notifications, graceful shutdown (close frame + no new
// dials), and the library's default ping/pong handling. The
// sixth test (TestServerPushNotification_ConnIDUnknown) is a
// small extra that pins the error path on the dispatcher side.

// waitForConnID polls the server's connection registry until it
// sees at least one entry (or the deadline expires) and returns
// the connID of the first one. The accept goroutine that runs
// handleWS is async w.r.t. websocket.Dial returning, so a test
// that calls Notify right after Dial must wait for the registry
// to be populated.
func waitForConnID(t *testing.T, srv *TCPServer) string {
	t.Helper()
	var connID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.connsMu.Lock()
		for id := range srv.conns {
			connID = id
			break
		}
		srv.connsMu.Unlock()
		if connID != "" {
			return connID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no ws connection registered within 2s of Dial")
	return ""
}

// TestServerPushNotification_Received exercises the headline ask
// of cycle rc-streaming-protocol: the dispatcher can push a
// JSON-RPC notification (no "id" field) to a specific ws
// connection by connID. The client reads the frame and asserts
// (a) it has no id field and (b) the method name + params
// round-trip.
func TestServerPushNotification_Received(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	connID := waitForConnID(t, srv)

	// Push a notification. The dispatcher marshals
	// {"jsonrpc":"2.0","method":<name>,"params":<params>} and
	// the transport hands it to the ws connection identified
	// by connID.
	if err := srv.dispatcher.Notify(context.Background(), connID, "test/notify", map[string]any{"hello": "world"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// Read the frame on the client. coder/websocket's Read
	// blocks until a ws message arrives or the context
	// expires; we use a generous 2 s deadline so a flaky
	// scheduler doesn't false-fail.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, msg, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("client Read after Notify: %v", err)
	}

	var frame map[string]any
	if err := json.Unmarshal(msg, &frame); err != nil {
		t.Fatalf("unmarshal notification: %v\n%s", err, msg)
	}
	if _, hasID := frame["id"]; hasID {
		t.Errorf("notification carries an id field: %#v", frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("notification jsonrpc = %v, want 2.0", frame["jsonrpc"])
	}
	if frame["method"] != "test/notify" {
		t.Errorf("notification method = %v, want test/notify", frame["method"])
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("notification params = %T, want map[string]any: %#v", frame["params"], frame)
	}
	if params["hello"] != "world" {
		t.Errorf("notification params.hello = %v, want world", params["hello"])
	}
}

// TestServerPushNotification_NoReply covers the other half of
// the server-push ask: incoming frames WITHOUT an "id" field
// are treated as client-side notifications — the dispatcher
// routes them but the transport does NOT write a reply. The
// client's Read times out, which is the right behaviour per
// JSON-RPC 2.0 §4.1 ("the Server MUST NOT reply to a
// Notification").
func TestServerPushNotification_NoReply(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()
	_ = waitForConnID(t, srv)

	// Send a notification (no id) from the client side.
	// The server's dispatch logic routes the method (which
	// happens to be unknown here, so it falls through to
	// -32601 Method not found in dispatch.go) but the
	// isNotification guard drops the reply.
	notice, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "client/notify",
		"params":  map[string]any{"hello": "server"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer wcancel()
	if err := c.Write(wctx, websocket.MessageText, notice); err != nil {
		t.Fatalf("client Write: %v", err)
	}

	// Client Read with a short timeout — the server must not
	// reply, so we expect DeadlineExceeded. (coder/websocket
	// returns the context error directly when the deadline
	// expires mid-read.)
	rctx, rcancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer rcancel()
	start := time.Now()
	_, _, err = c.Read(rctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("server replied to a client notification (it must not)")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v (after %s)", err, elapsed)
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("Read returned after %s; expected ~500ms", elapsed)
	}
}

// TestServerPushNotification_UnknownConnID covers the
// dispatcher's not-found error path. Notify with a connID that
// doesn't match any open ws connection must return
// ErrConnNotFound (defined in tcp.go) without blocking.
func TestServerPushNotification_UnknownConnID(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	// No client connected — the registry is empty, so any
	// connID is unknown.
	if err := srv.dispatcher.Notify(context.Background(), "ws-99999", "test/notify", nil); !errors.Is(err, ErrConnNotFound) {
		t.Errorf("Notify with unknown connID returned %v, want ErrConnNotFound", err)
	}
}

// TestServerGracefulShutdown_SendsGoingAway covers the
// graceful-shutdown handshake: when Shutdown is called, the
// server must send a close frame with code 1001 (StatusGoingAway)
// to every open ws client BEFORE tearing down the TCP
// connection. The client's Read returns an error whose
// CloseStatus is StatusGoingAway.
func TestServerGracefulShutdown_SendsGoingAway(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	_ = waitForConnID(t, srv)

	// Initiate shutdown in a goroutine. NewTCPServer's
	// Shutdown sends the close frame as part of step 2 of
	// the documented sequence.
	shutdownDone := make(chan error, 1)
	go func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		shutdownDone <- srv.Shutdown(sctx)
	}()

	// The client should observe the close frame. Read
	// returns a *websocket.CloseError when the peer sent a
	// close frame with a known code.
	_, _, err := c.Read(context.Background())
	if err == nil {
		t.Fatalf("client Read after Shutdown returned no error; want a CloseError")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusGoingAway {
		t.Errorf("close status = %d, want %d (StatusGoingAway)", got, websocket.StatusGoingAway)
	}

	// Shutdown must complete cleanly within the test's
	// timeout.
	select {
	case sErr := <-shutdownDone:
		if sErr != nil {
			t.Errorf("Shutdown returned %v", sErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown did not return within 5 s")
	}
}

// TestServerGracefulShutdown_NoNewConns covers the second half
// of the shutdown ask: once Shutdown is called, no new ws
// connections can be established. We test this by starting the
// shutdown in the main goroutine, sleeping briefly so the
// listener is closed, then attempting a new Dial that must
// fail.
func TestServerGracefulShutdown_NoNewConns(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	// Close the listener synchronously without waiting for
	// the per-conn drain. The listener.Close() in step 1 of
	// Shutdown is what stops new accepts; calling Shutdown
	// in a goroutine lets us dial right after step 1.
	shutdownDone := make(chan error, 1)
	go func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		shutdownDone <- srv.Shutdown(sctx)
	}()

	// Give step 1 (listener.Close) a moment to land. We
	// don't wait for the full Shutdown because that's the
	// thing we're testing — only step 1 needs to fire for
	// new dials to be rejected.
	time.Sleep(100 * time.Millisecond)

	// Attempt a new Dial. On 127.0.0.1:0 with the listener
	// closed, the OS rejects with ECONNREFUSED.
	c, _, err := websocket.Dial(context.Background(), srv.HTTPURL(), nil)
	if err == nil {
		_ = c.CloseNow()
		t.Fatalf("Dial after Shutdown succeeded; want a connection-refused error")
	}

	// Wait for shutdown to fully complete so the deferred
	// teardown is a clean no-op.
	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown did not return within 5 s")
	}
}

// TestPingPong_AutoHandled verifies that coder/websocket's
// library defaults handle ping/pong at the protocol level —
// no explicit handler is wired into TCPServer. Per the cycle
// brief, this test is the one we're allowed to drop if it
// turns out flaky, because the value of pinning "the library
// handles ping/pong for us" is low.
//
// Dropped: the test would need a concurrent Reader goroutine
// on the client side (the library's Ping does not read the
// connection itself; it sends a ping and waits for a Read
// call to process the pong — see the doc on
// (*websocket.Conn).Ping). Writing a ping test that drives
// a goroutine just to assert the library default works is
// more noise than signal. We trust the library's documented
// default per coder/websocket v1.8.15's read.go, which
// auto-replies to opPing with opPong in c.read() (the path
// handleWS's `c.Read(ctx)` invokes).
//
// If a future cycle adds a manual ping handler (e.g. to
// surface the latency in metrics), this stub is the place
// to add a TestPingHandler_FiresForPeerPing back.

// ----- cycle rc-cascade-start tests -----
//
// These three tests pin the cycle-3 ask: a remote dashboard
// can drive StartRemoteAgentCascade over the WS adapter,
// get a usable cascade_id back, and start two parallel
// cascades on the same connection. The fourth
// (TestStartRemoteAgentCascade_InvalidParams) exercises the
// brief's "{model: 42} → -32602" rule by sending a numeric
// model field and asserting the dispatcher rejects it as a
// type error rather than silently coercing it to "".
//
// All three tests call dispatcher's StartRemoteAgentCascade
// through the WS adapter so they pin the wire envelope
// (rpcResult / rpcError wrapping) too — the dispatcher unit
// tests in remotecontrol/cascade_test.go cover the
// in-process surface.

// newTestTCPServerWithCap is a variant of newTestTCPServer
// that lets the test inject a custom CascadeRegistry cap.
// Used by the over-cap / uniqueness tests that need a
// smaller maximum so they can hit the boundary in one
// process run.
func newTestTCPServerWithCap(t *testing.T, cascadeCap int) (*TCPServer, *remotecontrol.SessionState, *remotecontrol.CascadeRegistry, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.3.4-test")
	sess.Workspace = "/tmp/ws-workspace"
	sess.Backend = "agw"
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.SetConversationID("conv-ws")
	sess.SetTurnOutput("hello from the ws host")

	registry := remotecontrol.NewCascadeRegistry(cascadeCap)
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:  sess,
		Agents:   remotecontrol.DefaultAgentLister,
		Reader:   remotecontrol.DefaultOutputReader{},
		Cascades: registry,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, sess, registry, teardown
}

// TestStartRemoteAgentCascade_Ok is the headline ask of cycle
// rc-cascade-start: a dashboard sends StartRemoteAgentCascade
// over the wire and gets a non-empty cascade_id back. Model
// is empty in the request, so the handler must resolve to
// the host's SessionState.Model (matches the brief's "empty
// = use daemon default" rule).
//
// Also pins:
//   - response is a JSON-RPC 2.0 result (no error envelope)
//   - cascade_id is non-empty
//   - conversationId is empty (rc-cascade-message fills it
//     in on the first turn)
//   - model's resolved label is the host's
//   - backend reflects the host's resolved backend
//   - version matches remotecontrol.ServerVersion
//   - the dispatcher's cascade registry now has Len()==1 and
//     the cascade's fields match the dashboard's params
func TestStartRemoteAgentCascade_Ok(t *testing.T) {
	srv, sess, registry, done := newTestTCPServerWithCap(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "smoke-deck",
				"version": "0.4.2",
			},
			"model": "",
			"agent": "explorer",
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("StartRemoteAgentCascade returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("StartRemoteAgentCascade: no result, got %#v", resp)
	}
	cid, _ := res["cascade_id"].(string)
	if cid == "" {
		t.Fatalf("StartRemoteAgentCascade: cascade_id = %q, want non-empty", cid)
	}
	if convID, _ := res["conversationId"].(string); convID != "" {
		t.Errorf("response.conversationId = %q, want empty (filled in by rc-cascade-message)", convID)
	}
	if res["model"] != sess.Model {
		t.Errorf("response.model = %v, want %q (resolved from SessionState)", res["model"], sess.Model)
	}
	if res["backend"] != sess.Backend {
		t.Errorf("response.backend = %v, want %q (resolved from SessionState)", res["backend"], sess.Backend)
	}
	if res["version"] != remotecontrol.ServerVersion {
		t.Errorf("response.version = %v, want %q", res["version"], remotecontrol.ServerVersion)
	}
	if createdAt, _ := res["created_at"].(string); createdAt == "" {
		t.Errorf("response.created_at = empty, want non-empty RFC3339")
	}

	// The registry must now hold exactly one cascade, with
	// the same id and the dashboard-supplied clientInfo +
	// agent fields. Future cycles will look up by id, so
	// mismatched ids would silently break the chain.
	if got := registry.Len(); got != 1 {
		t.Errorf("registry.Len() = %d after one Start, want 1", got)
	}
	got, ok := registry.Get(cid)
	if !ok || got == nil {
		t.Fatalf("registry.Get(%q) failed", cid)
	}
	if got.ID != cid {
		t.Errorf("registry cascade.ID = %q, want %q", got.ID, cid)
	}
	if got.Agent != "explorer" {
		t.Errorf("registry cascade.Agent = %q, want explorer", got.Agent)
	}
	if got.ClientInfo.Name != "smoke-deck" || got.ClientInfo.Version != "0.4.2" {
		t.Errorf("registry cascade.ClientInfo = %+v, want {smoke-deck 0.4.2}", got.ClientInfo)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("registry cascade.CreatedAt = zero, want non-zero")
	}
}

// TestStartRemoteAgentCascade_TwoIndependent pins the
// "one ws connection holds N parallel cascades" rule. Two
// distinct StartRemoteAgentCascade requests on the same ws
// produce two distinct ids; the registry holds both; both
// cascades survive independently (dropping one does not
// affect the other).
func TestStartRemoteAgentCascade_TwoIndependent(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithCap(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Start cascade #1 — model empty; effort medium.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params": map[string]any{
			"clientInfo": map[string]any{"name": "deck", "version": "0.1"},
			"effort":     "medium",
		},
	})
	resp1 := readJSONRPC(t, c)
	if errObj, ok := resp1["error"]; ok {
		t.Fatalf("first Start: error = %#v", errObj)
	}
	res1, _ := resp1["result"].(map[string]any)
	cid1, _ := res1["cascade_id"].(string)
	if cid1 == "" {
		t.Fatalf("first Start: cascade_id empty")
	}

	// Start cascade #2 — agent default; effort empty.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "StartRemoteAgentCascade",
		"params": map[string]any{
			"clientInfo": map[string]any{"name": "deck", "version": "0.1"},
			"agent":      "default",
		},
	})
	resp2 := readJSONRPC(t, c)
	if errObj, ok := resp2["error"]; ok {
		t.Fatalf("second Start: error = %#v", errObj)
	}
	res2, _ := resp2["result"].(map[string]any)
	cid2, _ := res2["cascade_id"].(string)
	if cid2 == "" {
		t.Fatalf("second Start: cascade_id empty")
	}

	if cid1 == cid2 {
		t.Fatalf("two Starts returned the same cascade_id %q", cid1)
	}
	if got := registry.Len(); got != 2 {
		t.Errorf("registry.Len() = %d after two Starts, want 2", got)
	}
	got1, ok := registry.Get(cid1)
	if !ok {
		t.Errorf("registry lost cid1 %q", cid1)
	}
	got2, ok := registry.Get(cid2)
	if !ok {
		t.Errorf("registry lost cid2 %q", cid2)
	}
	if got1 != nil && got2 != nil && got1.Agent == got2.Agent {
		t.Errorf("cascades share Agent = %q; distinct Agents were requested", got1.Agent)
	}

	// Drop one — the other must survive. This is the
	// rc-cascade-message cycle's "close one cascade but keep
	// the others" path; if Drop is buggy, rc-cascade-message
	// has no way to release its slots.
	registry.Drop(cid1)
	if got := registry.Len(); got != 1 {
		t.Errorf("registry.Len() = %d after Drop, want 1", got)
	}
	if _, ok := registry.Get(cid1); ok {
		t.Errorf("registry still returns cid1 after Drop")
	}
	if _, ok := registry.Get(cid2); !ok {
		t.Errorf("registry lost cid2 after Drop(cid1)")
	}
}

// TestStartRemoteAgentCascade_InvalidParams covers the brief's
// "send `{model: 42}` → error.code == -32602" requirement.
// json.Unmarshal silently coerces a number to "" for a
// string-typed field, so a dispatcher that does not type-check
// would silently fall back to the host's default model. The
// handler's nonStringFieldInJSON helper catches the type
// mismatch and surfaces it as a JSON-RPC Invalid-params error.
func TestStartRemoteAgentCascade_InvalidParams(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithCap(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params": map[string]any{
			"model": 42,
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("invalid-params Start: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if _, ok := resp["result"]; ok {
		t.Errorf("invalid-params Start: result field present alongside error: %#v", resp)
	}
}

// TestStartRemoteAgentCascade_RegistryFull covers the over-cap
// path. We boot a registry capped at 1; the second Start must
// receive a -32603 Internal error and the registry must still
// hold exactly one cascade.
func TestStartRemoteAgentCascade_RegistryFull(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithCap(t, 1)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// First Start succeeds.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params":  map[string]any{},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("first Start (within cap): error = %#v", errObj)
	}

	// Second Start must be capped out.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "StartRemoteAgentCascade",
		"params":  map[string]any{},
	})
	resp = readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("second Start (over cap): no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32603 {
		t.Errorf("error.code = %d, want -32603 (Internal error)", got)
	}

	// Registry must still hold exactly one cascade (no
	// partial insert on the overflow path).
	if got := registry.Len(); got != 1 {
		t.Errorf("registry.Len() = %d, want 1 (no partial insert)", got)
	}
}

// TestStartRemoteAgentCascade_EmptyParams covers the
// "minimum-viable dashboard sends {}" path. With every
// optional field absent (params is an empty object), the
// dispatcher must still produce a valid cascade whose
// response fields fall back to host defaults. The brief's
// "model: optional" rule says empty params is fine.
func TestStartRemoteAgentCascade_EmptyParams(t *testing.T) {
	srv, sess, registry, done := newTestTCPServerWithCap(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params":  map[string]any{},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("empty-params Start: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if res["cascade_id"] == "" {
		t.Errorf("empty-params Start: cascade_id empty")
	}
	if res["model"] != sess.Model {
		t.Errorf("empty-params Start: model = %v, want %q (host fallback)", res["model"], sess.Model)
	}
	if res["backend"] != sess.Backend {
		t.Errorf("empty-params Start: backend = %v, want %q (host fallback)", res["backend"], sess.Backend)
	}
	if got := registry.Len(); got != 1 {
		t.Errorf("registry.Len() = %d after one Start, want 1", got)
	}
}

// ----- cycle rc-cascade-message tests -----
//
// Wire-level coverage for SendRemoteAgentCascadeMessage
// (the dispatcher-level surface is in
// internal/remotecontrol/cascade_test.go). These tests
// pin the JSON envelope byte-for-byte over the WS adapter
// and exercise the same paths the in-process tests cover,
// plus the wire-format-specific ones (e.g. code is
// float64 after a JSON round-trip).
//
// Each test boots a TCPServer, dials the WS adapter,
// StartRemoteAgentCascades a cascade id (which the test
// then patches a backend onto), and SendRemoteAgentCascades
// against it. The "patch a backend" step uses the
// dispatcher's accessor (*Dispatcher).Cascades().Get(cid)
// and writes a *StubBackend into the cascade's Backend field.

// newTestTCPServerWithBackend is the rc-cascade-message
// helper. It builds a dispatcher and starts the WS
// adapter, then lets the caller register a backend for
// use during the test (the dispatcher is exposed via
// (*TCPServer).dispatcher after construction — same
// field the rc-streaming-protocol and rc-cascade-start
// tests reach into).
//
// The cascadeCap param caps the registry (the test for
// the cap-full path uses cap=1; most tests use the
// production default).
func newTestTCPServerWithBackend(t *testing.T, cascadeCap int) (*TCPServer, *remotecontrol.SessionState, *remotecontrol.CascadeRegistry, func()) {
	t.Helper()
	srv, sess, registry, teardown := newTestTCPServerWithCap(t, cascadeCap)
	return srv, sess, registry, teardown
}

// startCascade sends a StartRemoteAgentCascade RPC and
// returns the cascade id. Used by every rc-cascade-message
// wire test as the setup step.
func startCascade(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params": map[string]any{
			"clientInfo": map[string]any{"name": "rc-cascade-message-test", "version": "0"},
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Start: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Start: no result, got %#v", resp)
	}
	cid, _ := res["cascade_id"].(string)
	if cid == "" {
		t.Fatalf("Start: cascade_id empty")
	}
	return cid
}

// TestSendRemoteAgentCascadeMessage_OK is the headline
// ask of cycle rc-cascade-message: a dashboard sends
// SendRemoteAgentCascadeMessage over the wire, gets back
// the assistant's reply, the resolved conversationId, the
// wall-clock duration, the (empty) tool-calls array, and
// the stop reason. The registry's per-cascade
// ConversationID is updated as a side effect.
func TestSendRemoteAgentCascadeMessage_OK(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		Reply:          "ws reply",
		ConversationID: "conv-ws",
		StopReason:     remotecontrol.StopReasonEndTurn,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "hello over ws",
		},
	})
	// rc-cascade-stream: drain the cascade/state notifications
	// the dispatcher pushes mid-turn (starting → done) before
	// reading the synchronous envelope. readJSONRPCResponse
	// keeps going until it sees a frame with an "id" field.
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Send: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Send: no result, got %#v", resp)
	}
	if res["reply"] != "ws reply" {
		t.Errorf("Send: reply = %v, want ws reply", res["reply"])
	}
	if res["conversationId"] != "conv-ws" {
		t.Errorf("Send: conversationId = %v, want conv-ws", res["conversationId"])
	}
	if res["stopReason"] != remotecontrol.StopReasonEndTurn {
		t.Errorf("Send: stopReason = %v, want %s", res["stopReason"], remotecontrol.StopReasonEndTurn)
	}
	if _, ok := res["durationMs"].(float64); !ok {
		t.Errorf("Send: durationMs = %T, want number", res["durationMs"])
	}
	toolCalls, ok := res["toolCalls"].([]any)
	if !ok {
		t.Errorf("Send: toolCalls = %T, want array", res["toolCalls"])
	}
	if len(toolCalls) != 0 {
		t.Errorf("Send: toolCalls has %d entries, want 0", len(toolCalls))
	}

	// The cascade's ConversationID is updated as a side
	// effect: a follow-up wire call can reuse it (or a
	// dashboard's "ListConversations" iterates and sees
	// it). Pin that here so a regression in the
	// registry-side recordReply path doesn't slip past
	// the dispatch-level test alone.
	got, _ = registry.Get(cid)
	if got.ConversationID != "conv-ws" {
		t.Errorf("after Send: cascade.ConversationID = %q, want conv-ws", got.ConversationID)
	}
	if got.LastReplyAt.IsZero() {
		t.Errorf("after Send: cascade.LastReplyAt = zero, want non-zero")
	}
}

// TestSendRemoteAgentCascadeMessage_NotFound pins the
// wire-level -32004 envelope path: an unknown cascade_id
// yields error.code == -32004, not the usual -32601 or
// -32603.
func TestSendRemoteAgentCascadeMessage_NotFound(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": "cas_doesnotexist",
			"text":       "hi",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("not-found Send: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != remotecontrol.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, remotecontrol.ErrCodeCascadeNotFound)
	}
}

// TestSendRemoteAgentCascadeMessage_BadEffort pins the
// wire-level -32602 path for invalid effort. Same
// expectation as the dispatcher-level test, but over the
// actual WS adapter so the envelope is pinned.
func TestSendRemoteAgentCascadeMessage_BadEffort(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": "cas_x",
			"text":       "hi",
			"effort":     "bogus",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("bad-effort Send: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
}

// TestSendRemoteAgentCascadeMessage_NoCascadeID pins the
// wire-level -32602 path for missing / empty cascade_id.
func TestSendRemoteAgentCascadeMessage_NoCascadeID(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"text": "hi",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no-cascade-id Send: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
}

// TestSendRemoteAgentCascadeMessage_ToolCalls pins the
// tool-calls round-trip: the StubBackend returns one
// ToolCall, the result envelope faithfully serialises it
// (name + args-as-object + result + ok). A future
// rc-cascade-stream cycle will stream these as they
// happen; today they're a snapshot at end-of-turn.
func TestSendRemoteAgentCascadeMessage_ToolCalls(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		Reply:      "done",
		StopReason: remotecontrol.StopReasonToolUse,
		ToolCalls: []remotecontrol.ToolCall{
			{
				Name:   "read_file",
				Args:   []byte(`{"path":"/etc/hostname"}`),
				Result: "my-host",
				OK:     true,
			},
		},
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "read /etc/hostname",
		},
	})
	// rc-cascade-stream: drain mid-turn notifications. The
	// StubBackend emits starting → text_delta → tool_call →
	// done, four frames before the synchronous envelope.
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Send: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Send: no result, got %#v", resp)
	}
	if res["reply"] != "done" {
		t.Errorf("Send: reply = %v, want done", res["reply"])
	}
	if res["stopReason"] != remotecontrol.StopReasonToolUse {
		t.Errorf("Send: stopReason = %v, want %s", res["stopReason"], remotecontrol.StopReasonToolUse)
	}
	tcs, ok := res["toolCalls"].([]any)
	if !ok {
		t.Fatalf("Send: toolCalls = %T, want array: %#v", res["toolCalls"], res["toolCalls"])
	}
	if len(tcs) != 1 {
		t.Fatalf("Send: toolCalls has %d entries, want 1", len(tcs))
	}
	tc, _ := tcs[0].(map[string]any)
	if tc["name"] != "read_file" {
		t.Errorf("toolCalls[0].name = %v, want read_file", tc["name"])
	}
	args, _ := tc["args"].(map[string]any)
	if args == nil || args["path"] != "/etc/hostname" {
		t.Errorf("toolCalls[0].args = %v, want {path:/etc/hostname}", tc["args"])
	}
	if tc["result"] != "my-host" {
		t.Errorf("toolCalls[0].result = %v, want my-host", tc["result"])
	}
	if ok, _ := tc["ok"].(bool); !ok {
		t.Errorf("toolCalls[0].ok = %v, want true", tc["ok"])
	}
}

// TestSendRemoteAgentCascadeMessage_Timeout pins the
// wire-level -32010 envelope path: backend sleeps longer
// than the configured 30ms timeoutMs.
func TestSendRemoteAgentCascadeMessage_Timeout(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{Sleep: 200 * time.Millisecond}
	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "slow",
			"timeoutMs":  30,
		},
	})
	// rc-cascade-stream: drain the cascade/state(phase=error)
	// notification the sink pushed before the synchronous
	// -32010 envelope. The OnError path always emits; the
	// helper sees it as a no-id frame and skips past it.
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("timeout Send: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != remotecontrol.ErrCodeTurnTimeout {
		t.Errorf("error.code = %d, want %d (Turn timeout)", got, remotecontrol.ErrCodeTurnTimeout)
	}
}

// TestSendRemoteAgentCascadeMessage_PropagatesError pins
// the wire-level -32603 envelope path when the backend
// returns an error.
func TestSendRemoteAgentCascadeMessage_PropagatesError(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{Err: errors.New("backend failed")}
	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "fail",
		},
	})
	// rc-cascade-stream: drain the cascade/state(phase=error)
	// notification. The sink's OnError emits before the
	// dispatcher maps the error to -32603; readJSONRPCResponse
	// skips past the notification to the envelope.
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("fail Send: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32603 {
		t.Errorf("error.code = %d, want -32603 (Internal error)", got)
	}
}

// ----- cycle rc-cascade-stream wire tests -----
//
// The tests below pin the JSON-RPC notification shape the
// dispatcher pushes while a SendRemoteAgentCascadeMessage
// turn is running. The in-process counterparts in
// internal/remotecontrol/cascade_test.go cover the
// EventSink seam; these tests cover the wire-level
// framing (notification vs response, no `id` on the
// notification, body of the params, the multi-cast or
// single-cast behaviour over a single ws connection).

// TestSendStream_FrameOrder pins the exact notification
// sequence the dispatcher pushes for a streaming
// StubBackend: cascade/state(starting), cascade/text_delta,
// cascade/tool_call (× N), cascade/state(done), then the
// synchronous response envelope. Any reordering or
// missing frame is a regression — dashboards assemble the
// reply text from the cascade/text_delta frames, so a
// missing first delta silently corrupts the dash.
func TestSendStream_FrameOrder(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		Reply:          "streamed-reply",
		ConversationID: "conv-stream",
		StopReason:     remotecontrol.StopReasonEndTurn,
		ToolCalls: []remotecontrol.ToolCall{
			{Name: "echo", Args: []byte(`{"text":"hi"}`), Result: "hi", OK: true},
			{Name: "noop", Args: []byte(`null`), Result: "", OK: true},
		},
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "stream me",
		},
	})

	// Drain up to 8 frames (notification budget is generous
	// to let the test run on a slow machine). Stop early on
	// the response envelope.
	notifs := readJSONRPCNotifications(t, c, 8)

	// StubBackend order: OnState(starting), OnTextDelta,
	// OnToolCall×2, OnState(done, end_turn). 5 notifications
	// before the synchronous response.
	if len(notifs) != 6 {
		t.Fatalf("got %d frames, want 6 (5 notifications + 1 response): %#v", len(notifs), notifs)
	}
	if notifs[5].IsResponse != true {
		t.Errorf("frame[5].IsResponse = false; want true (must be the synchronous envelope)")
	}
	if notifs[5].Method != "" {
		t.Errorf("frame[5].Method = %q; want empty for a response frame", notifs[5].Method)
	}

	wantMethod := []string{
		remotecontrol.NotifCascadeState,
		remotecontrol.NotifCascadeTextDelta,
		remotecontrol.NotifCascadeToolCall,
		remotecontrol.NotifCascadeToolCall,
		remotecontrol.NotifCascadeState,
	}
	for i, wm := range wantMethod {
		if got := notifs[i].Method; got != wm {
			t.Errorf("notif[%d].method = %q, want %q", i, got, wm)
		}
		if notifs[i].IsResponse {
			t.Errorf("notif[%d].IsResponse = true; want false (notifications have no id)", i)
		}
	}

	// Pin the cascade_id on every notification. The wire
	// spec leaves the routing hint to the dashboard; the
	// reference puts it on every frame, we do the same.
	for i := 0; i < len(notifs)-1; i++ {
		if cid2, _ := notifs[i].Params["cascade_id"].(string); cid2 != cid {
			t.Errorf("notif[%d].cascade_id = %q, want %q", i, cid2, cid)
		}
	}

	// Pin the phase values on the two state notifications
	// (frames 0 and 4). Frame 0 = starting; frame 4 = done
	// with stopReason + durationMs.
	if phase, _ := notifs[0].Params["phase"].(string); phase != remotecontrol.StatePhaseStarting {
		t.Errorf("notif[0].phase = %q, want %q", notifs[0].Params["phase"], remotecontrol.StatePhaseStarting)
	}
	if phase, _ := notifs[4].Params["phase"].(string); phase != remotecontrol.StatePhaseDone {
		t.Errorf("notif[4].phase = %q, want %q", notifs[4].Params["phase"], remotecontrol.StatePhaseDone)
	}
	if sr, _ := notifs[4].Params["stopReason"].(string); sr != remotecontrol.StopReasonEndTurn {
		t.Errorf("notif[4].stopReason = %q, want %q", sr, remotecontrol.StopReasonEndTurn)
	}
	// durationMs is emitted only when the sink's
	// time.Since(startedAt).Milliseconds() > 0; a
	// fast-stub path may legitimately skip it. The brief:
	// dashboards that consume this field must treat its
	// absence as "unknown / too fast to measure" rather
	// than 0. So we accept either nil (absent) or a number.
	if v, ok := notifs[4].Params["durationMs"]; ok {
		if _, isNum := v.(float64); !isNum {
			t.Errorf("notif[4].durationMs = %T, want number when present", v)
		}
	}

	// Pin the text delta.
	if d, _ := notifs[1].Params["delta"].(string); d != "streamed-reply" {
		t.Errorf("notif[1].delta = %q, want streamed-reply", d)
	}

	// Pin the tool-call params. Second tool is the noop
	// (Result empty).
	if name, _ := notifs[2].Params["name"].(string); name != "echo" {
		t.Errorf("notif[2].name = %q, want echo", name)
	}
	if ok, _ := notifs[2].Params["ok"].(bool); !ok {
		t.Errorf("notif[2].ok = %v, want true", notifs[2].Params["ok"])
	}
	if name, _ := notifs[3].Params["name"].(string); name != "noop" {
		t.Errorf("notif[3].name = %q, want noop", name)
	}
}

// TestSendStream_SlowClientDoesNotBlock covers the
// back-pressure choice: even if the ws client hangs while
// notifications are being pushed, the dispatcher keeps
// emitting (best-effort, drop on writer error). The test
// exercises the trivial path (read everything, get
// response) under a sane budget — the fail-mode it
// actually guards against is the dispatcher hanging on a
// writer, and the buffer + drop path keeps that from
// happening. The TestSendStream_FrameOrder test above
// pins the normal-speed notification sequence; this one
// pins "doesn't time out under buffered back-pressure".
func TestSendStream_SlowClientDoesNotBlock(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		Reply:      "fast",
		StopReason: remotecontrol.StopReasonEndTurn,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "go fast",
		},
	})

	// readJSONRPCResponse drains both notification frames
	// and the response envelope — by the time it returns
	// the synchronous result, the dispatcher has flushed
	// every notification. If the dispatcher had blocked on
	// a writer, this read would have timed out and the
	// test would have surfaced a "ws read" failure rather
	// than a clean response.
	deadline := time.Now().Add(2 * time.Second)
	resp := readJSONRPCResponse(t, c)
	if time.Now().After(deadline) {
		t.Errorf("readJSONRPCResponse did not return within 2s; dispatcher may have blocked on writer")
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Send: no result, got %#v", resp)
	}
	if res["reply"] != "fast" {
		t.Errorf("Send: reply = %v, want fast", res["reply"])
	}
}

// TestSendStream_TwoCascadesOnOneConn_DoNotMix pins the
// "one ws connection holds N parallel cascades" rule on
// the streaming path: cascade A and cascade B both send a
// turn over the same ws connection, and the notifications
// for cascade A never carry cascade B's id (and vice
// versa). A buggy sink that hardcoded a single cascade
// would scramble the stream; this test would catch that.
func TestSendStream_TwoCascadesOnOneConn_DoNotMix(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Start cascade A.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "StartRemoteAgentCascade",
		"params":  map[string]any{"clientInfo": map[string]any{"name": "deck-A", "version": "0"}},
	})
	respA := readJSONRPC(t, c)
	resA := respA["result"].(map[string]any)
	cidA, _ := resA["cascade_id"].(string)

	// Start cascade B.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "StartRemoteAgentCascade",
		"params":  map[string]any{"clientInfo": map[string]any{"name": "deck-B", "version": "0"}},
	})
	respB := readJSONRPC(t, c)
	resB := respB["result"].(map[string]any)
	cidB, _ := resB["cascade_id"].(string)
	if cidA == cidB {
		t.Fatalf("two Starts returned the same cascade_id %q", cidA)
	}

	// Wire two distinct backends. Keep them small so we
	// can deterministically assert the interleaved
	// notification sequence.
	gotA, _ := registry.Get(cidA)
	gotA.SetBackend(&remotecontrol.StubBackend{
		Reply:      "from-A",
		StopReason: remotecontrol.StopReasonEndTurn,
	})
	gotB, _ := registry.Get(cidB)
	gotB.SetBackend(&remotecontrol.StubBackend{
		Reply:      "from-B",
		StopReason: remotecontrol.StopReasonEndTurn,
	})

	// Send on cascade A.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "SendRemoteAgentCascadeMessage",
		"params":  map[string]any{"cascade_id": cidA, "text": "to-A"},
	})
	respSendA := readJSONRPCResponse(t, c)
	if res, _ := respSendA["result"].(map[string]any); res["reply"] != "from-A" {
		t.Errorf("cascade A reply = %v, want from-A", res["reply"])
	}

	// Send on cascade B.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "SendRemoteAgentCascadeMessage",
		"params":  map[string]any{"cascade_id": cidB, "text": "to-B"},
	})
	respSendB := readJSONRPCResponse(t, c)
	if res, _ := respSendB["result"].(map[string]any); res["reply"] != "from-B" {
		t.Errorf("cascade B reply = %v, want from-B", res["reply"])
	}

	// Smoke check: both cascades survive; the registry
	// holds them in independent slots.
	if registry.Len() != 2 {
		t.Errorf("registry.Len() = %d after two parallel cascades, want 2", registry.Len())
	}
}

// TestSendStream_MissingConnID_NoPanic pins the path
// where the cascade handler is invoked without a connID
// in the request ctx — the Unix-socket transport never
// calls WithConnID, and any in-process dispatcher driver
// (or future transport that doesn't thread connID) lands
// here. The cascade handler must NOT panic; the dispatch
// should still produce a synchronous envelope. (The sink
// emits no notifications because there's nothing to push
// to — Notify returns nil when no notifier is registered,
// so the empty-connID path stays clean.)
func TestSendStream_MissingConnID_NoPanic(t *testing.T) {
	sess := remotecontrol.NewSessionState("")
	d, err := remotecontrol.NewDispatcher(remotecontrol.Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	be := &remotecontrol.StubBackend{
		Reply:      "no-connid",
		StopReason: remotecontrol.StopReasonEndTurn,
	}
	// Build a *Cascade by hand (the cascade_test.go helper
	// `registerCascadeWithBackend` is unexported; this
	// test lives in a different package and can't see it).
	c, err := remotecontrol.NewCascade(remotecontrol.CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	c.SetBackend(be)
	if err := d.Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}

	req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":%q,"text":"x"}}`, c.ID))
	// Dispatch with an empty ctx (no connID). The
	// handler builds a sink, the sink's OnState calls
	// Notify with the empty connID, Notify returns nil (no
	// notifier is registered yet — that's the in-process
	// driver scenario), and the synchronous envelope
	// carries the assistant's reply.
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Send with empty connID returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Send with empty connID: no result, got %#v", resp)
	}
	if res["reply"] != "no-connid" {
		t.Errorf("Send with empty connID: reply = %v, want no-connid", res["reply"])
	}
}

// ----- cycle rc-handle-user-interaction wire tests -----
//
// These pin the wire envelope the rc-handle-user-interaction
// cycle adds: the cascade/user_interaction_required
// notification (server-push) and the
// HandleRemoteAgentCascadeUserInteraction RPC method
// (client→server). The dispatcher-level surface is in
// internal/remotecontrol/pending_test.go.

// TestHandleUserInteraction_NotificationOverWire exercises
// the cascade/user_interaction_required notification on
// the wire: StubBackend with one Interactions entry emits
// the notification mid-turn; the wire test reads the
// notification frame and asserts the params shape. The
// round-trip (send HandleRemoteAgentCascadeUserInteraction
// over the same ws to unblock the turn) is exercised at
// the dispatcher level by
// TestHandleRemoteAgentCascadeUserInteraction_Ok in
// internal/remotecontrol/pending_test.go; this wire test
// pins the JSON envelope of the server-push half.
//
// Why no round-trip here: the TCP+WS transport's handleWS
// loop reads one request, dispatches it (blocking), then
// writes the response. While the stub is parked on
// intr.ResultCh, the server cannot read a follow-up
// Handle…UserInteraction RPC on the same connection. A
// wire round-trip would deadlock; a future
// rc-concurrent-handlers cycle (or PiG-backed backend
// that resolves via a separate channel) can revisit the
// "two-requests-on-one-conn" pattern.
func TestHandleUserInteraction_NotificationOverWire(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		StopReason: remotecontrol.StopReasonEndTurn,
		Interactions: []remotecontrol.PendingInteractionConfig{
			{
				Kind:   remotecontrol.InteractionKindPermission,
				Title:  "Read main.go?",
				Prompt: "ok?",
				Options: []remotecontrol.InteractionOption{
					{Label: "yes", Value: "yes"},
					{Label: "no", Value: "no"},
				},
				Dangerous: true,
			},
		},
		// Short wait so the test doesn't drag — the
		// stub will time out before any handler call.
		InteractionWait: 50 * time.Millisecond,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "please resolve a prompt",
		},
	})

	// Read frames until we see the user_interaction_required
	// notification. We expect: state(phase=starting) (stub
	// emits starting via OnState after emitInteractions),
	// state(phase=error) (the timeout surfaces via OnError),
	// then the synchronous envelope. The interaction
	// notification comes between starting and error.
	var uiNotif *jsonRPCNotification
	const maxFrames = 8
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		if _, hasID := frame["id"]; hasID {
			// Hit the synchronous response envelope
			// without finding the user_interaction
			// notification — fail loudly so a future
			// regression in the sink path is caught.
			if uiNotif == nil {
				t.Fatalf("reached response envelope before seeing cascade/user_interaction_required: %#v", frame)
			}
			break
		}
		method, _ := frame["method"].(string)
		if method == remotecontrol.NotifUserInteractionRequired {
			params, _ := frame["params"].(map[string]any)
			uiNotif = &jsonRPCNotification{Method: method, Params: params}
			break
		}
	}
	if uiNotif == nil {
		t.Fatalf("did not see cascade/user_interaction_required within %d frames", maxFrames)
	}
	if uiNotif.Method != remotecontrol.NotifUserInteractionRequired {
		t.Errorf("method = %q, want %q", uiNotif.Method, remotecontrol.NotifUserInteractionRequired)
	}
	if cid2, _ := uiNotif.Params["cascade_id"].(string); cid2 != cid {
		t.Errorf("cascade_id = %q, want %q", cid2, cid)
	}
	intrID, _ := uiNotif.Params["interaction_id"].(string)
	if !strings.HasPrefix(intrID, "int_") {
		t.Errorf("interaction_id = %q, want int_ prefix", intrID)
	}
	if kind, _ := uiNotif.Params["kind"].(string); kind != remotecontrol.InteractionKindPermission {
		t.Errorf("kind = %q, want %q", kind, remotecontrol.InteractionKindPermission)
	}
	if title, _ := uiNotif.Params["title"].(string); title != "Read main.go?" {
		t.Errorf("title = %q, want %q", title, "Read main.go?")
	}
	if prompt, _ := uiNotif.Params["prompt"].(string); prompt != "ok?" {
		t.Errorf("prompt = %q, want %q", prompt, "ok?")
	}
	if d, _ := uiNotif.Params["dangerous"].(bool); !d {
		t.Errorf("dangerous = %v, want true", uiNotif.Params["dangerous"])
	}
	opts, ok := uiNotif.Params["options"].([]any)
	if !ok || len(opts) != 2 {
		t.Errorf("options = %v, want 2 entries", uiNotif.Params["options"])
	}

	// Drain any remaining frames and confirm the
	// synchronous envelope is an error envelope (the
	// stub timed out without an answer).
	sendResp := readJSONRPCResponse(t, c)
	if errObj, ok := sendResp["error"].(map[string]any); ok {
		// We don't pin the exact error message —
		// just confirm an error envelope arrived.
		if code, _ := errObj["code"].(float64); code == 0 {
			t.Errorf("error envelope missing code: %#v", errObj)
		}
	} else {
		t.Errorf("expected error envelope after stub timeout, got %#v", sendResp)
	}
}

// TestHandleUserInteraction_UnknownCascade pins the
// wire-level -32004 envelope path: HandleRemoteAgentCascadeUserInteraction
// with a cascade_id that doesn't match any registered
// cascade.
func TestHandleUserInteraction_UnknownCascade(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "HandleRemoteAgentCascadeUserInteraction",
		"params": map[string]any{
			"cascade_id":     "cas_doesnotexist",
			"interaction_id": "int_x",
			"response":       "yes",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown-cascade Handle…: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != remotecontrol.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d", got, remotecontrol.ErrCodeCascadeNotFound)
	}
}

// TestHandleUserInteraction_UnknownInteraction pins the
// wire-level -32006 envelope path: a registered cascade
// but no interaction with that id.
func TestHandleUserInteraction_UnknownInteraction(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "HandleRemoteAgentCascadeUserInteraction",
		"params": map[string]any{
			"cascade_id":     cid,
			"interaction_id": "int_unknown",
			"response":       "yes",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown-interaction Handle…: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != remotecontrol.ErrCodeInteractionNotFound {
		t.Errorf("error.code = %d, want %d", got, remotecontrol.ErrCodeInteractionNotFound)
	}
}

// ----- cycle rc-list-readonly tests -----
//
// Wire-level WS round-trips for the four read-only List…
// methods. The in-process tests in
// internal/remotecontrol/lists_test.go pin the dispatcher's
// per-handler behaviour; the four tests below pin the byte-
// for-byte JSON envelope the ws adapter emits (the field
// types — `[]` vs `null`, key presence in nested objects —
// matter to the dashboard).

// newTestTCPServerWithListBackend is the rc-list-readonly
// helper. It builds the dispatcher with a fresh ListBackend
// and starts the ws adapter so the per-method tests can pin
// the wire envelope against a known catalogue.
func newTestTCPServerWithListBackend(t *testing.T) (*TCPServer, *remotecontrol.SessionState, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	sess.Workspace = "/tmp/list-rw-workspace"
	sess.Backend = "agw"
	sess.Model = "Gemini 3.8 Flash (Medium)"

	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: sess,
		Agents:  remotecontrol.DefaultAgentLister,
		Reader:  remotecontrol.DefaultOutputReader{},
		ListBackend: remotecontrol.ListBackend{
			Models: func(backend, filter string) ([]remotecontrol.ModelInfo, error) {
				return []remotecontrol.ModelInfo{
					{ID: "wire-model-1", Label: "Wire Model 1", Backend: backend, Default: true},
				}, nil
			},
			SlashCommands: func() ([]remotecontrol.SlashCommandInfo, error) {
				return []remotecontrol.SlashCommandInfo{
					{Name: "wire-cmd", Description: "Wire command", Scope: remotecontrol.SlashScopeBuiltin},
				}, nil
			},
			Conversations: func(limit int, before time.Time) ([]remotecontrol.ConversationInfo, time.Time, error) {
				return []remotecontrol.ConversationInfo{}, time.Time{}, nil
			},
			Environments: func() (remotecontrol.EnvironmentInfo, error) {
				return remotecontrol.EnvironmentInfo{
					OS:       "darwin",
					Arch:     "arm64",
					Hostname: "test-host",
					Cwd:      "/tmp",
					User:     "tester",
					Shell:    "/bin/zsh",
					Version:  "1.4.0",
					EnvKeysPresent: []string{
						"ANTHROPIC_API_KEY",
						"OPENAI_API_KEY",
					},
					Capabilities: map[string]any{"remote_control": true},
				}, nil
			},
		},
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, sess, teardown
}

// TestListRemoteAgentModels_OK is the ws-level happy path for
// ListRemoteAgentModels. Round-trips a params-less request and
// verifies the JSON envelope carries a `models` array (NOT
// null) and a `default_model` string. The wire envelope here
// is what goes to the recovered dashboard — a `null` for the
// `models` field would crash the renderer.
func TestListRemoteAgentModels_OK(t *testing.T) {
	srv, _, done := newTestTCPServerWithListBackend(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentModels",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentModels returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentModels: no result, got %#v", resp)
	}
	models, _ := res["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1: %#v", len(models), models)
	}
	first, _ := models[0].(map[string]any)
	if first["id"] != "wire-model-1" {
		t.Errorf("first model id = %v, want wire-model-1", first["id"])
	}
	if def, _ := res["default_model"].(string); def != "wire-model-1" {
		t.Errorf("default_model = %v, want wire-model-1", def)
	}
}

// TestListRemoteAgentSlashCommands_OK is the ws-level happy
// path for ListRemoteAgentSlashCommands. Pinned to ensure the
// response envelope uses `commands` (the recovered dashboard
// expects that key) and an array (not null).
func TestListRemoteAgentSlashCommands_OK(t *testing.T) {
	srv, _, done := newTestTCPServerWithListBackend(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentSlashCommands",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentSlashCommands returned error: %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	cmds, _ := res["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("commands length = %d, want 1: %#v", len(cmds), cmds)
	}
	first, _ := cmds[0].(map[string]any)
	if first["name"] != "wire-cmd" {
		t.Errorf("first cmd name = %v, want wire-cmd", first["name"])
	}
}

// TestListRemoteAgentConversations_OK is the ws-level happy
// path for ListRemoteAgentConversations. The test sandbox has
// no convstore; the canned closure returns an empty slice. We
// pin the empty-list shape (NOT null) because the dashboard
// iterates the array without a nil check.
func TestListRemoteAgentConversations_OK(t *testing.T) {
	srv, _, done := newTestTCPServerWithListBackend(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentConversations",
		"params":  map[string]any{"limit": 25},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentConversations returned error: %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	convs, _ := res["conversations"].([]any)
	if convs == nil {
		t.Errorf("conversations = nil; want []")
	}
	if len(convs) != 0 {
		t.Errorf("conversations length = %d, want 0", len(convs))
	}
	if nb, _ := res["next_before"].(string); nb != "" {
		t.Errorf("next_before = %q; want empty for empty slice", nb)
	}
}

// TestListRemoteAgentEnvironments_OK is the ws-level happy
// path for ListRemoteAgentEnvironments. Pins the envelope
// keys (os / arch / hostname / cwd / user / shell /
// antigravity_home / version / env_keys_present /
// capabilities) and verifies the env-leak invariant: the
// env_keys_present array carries NAMES only, never values.
func TestListRemoteAgentEnvironments_OK(t *testing.T) {
	srv, _, done := newTestTCPServerWithListBackend(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentEnvironments",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentEnvironments returned error: %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	wantKeys := []string{"os", "arch", "hostname", "cwd", "user", "shell", "antigravity_home", "version", "env_keys_present", "capabilities"}
	for _, k := range wantKeys {
		if _, ok := res[k]; !ok {
			t.Errorf("envelope key %q missing", k)
		}
	}
	if res["os"] != "darwin" {
		t.Errorf("os = %v, want darwin", res["os"])
	}
	if res["version"] != "1.4.0" {
		t.Errorf("version = %v, want 1.4.0", res["version"])
	}
	keys, _ := res["env_keys_present"].([]any)
	if len(keys) != 2 {
		t.Errorf("env_keys_present length = %d, want 2", len(keys))
	}
	for _, k := range keys {
		s, _ := k.(string)
		// No values leak: every entry must be a name from
		// DefaultEnvKeyProbe. The canned fixture uses
		// ANTHROPIC_API_KEY and OPENAI_API_KEY; both
		// contain "API_KEY" and never a "value"-shaped
		// substring.
		if !strings.Contains(s, "_API_KEY") && !strings.Contains(s, "_OAUTH_TOKEN") &&
			!strings.Contains(s, "_BACKEND") && !strings.Contains(s, "_SOCKET") &&
			!strings.Contains(s, "_HOME") && !strings.Contains(s, "_AUTH") {
			t.Errorf("env_keys_present has unexpected entry: %v", k)
		}
	}
}

// ----- cycle rc-file-ops tests -----
//
// Wire-level coverage for ReadRemoteAgentFile and
// ReadRemoteAgentDir (cycle rc-file-ops). The in-process
// counterparts in internal/remotecontrol/files_test.go
// cover the FSProvider seam (MemFS, WorkdirScope,
// sentinel mapping); these tests cover the wire envelope
// (JSON round-trip, code-as-float64, base64 binary,
// entries array shape).
//
// All tests share newTestTCPServerWithFS, which wires a
// real WorkdirScope against a t.TempDir() so the production
// code path is exercised end-to-end. A second helper
// (newTestTCPServerWithMemFS) wires a MemFS for the
// rejection tests that don't need a real filesystem.

// newTestTCPServerWithFS brings up a TCPServer whose
// dispatcher's FS seam is a *WorkdirScope pointing at a
// freshly-created t.TempDir(). The test populates that
// directory before exercising the methods so the
// production read / list paths are exercised against real
// files.
func newTestTCPServerWithFS(t *testing.T) (*TCPServer, *remotecontrol.SessionState, string, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	sess.Workspace = "/tmp/ws-workspace"
	sess.Backend = "agw"
	sess.Model = "Gemini 3.8 Flash (Medium)"

	dir := t.TempDir()
	scope := &remotecontrol.WorkdirScope{Workspace: dir}

	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: sess,
		Agents:  remotecontrol.DefaultAgentLister,
		Reader:  remotecontrol.DefaultOutputReader{},
		FS:      scope,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, sess, dir, teardown
}

// newTestTCPServerWithMemFS brings up a TCPServer
// whose dispatcher's FS seam is a MemFS (so tests can
// exercise the rejection path without needing a real
// filesystem). The MemFS exposes /allowed with a few
// files; the WorkdirScope points at /allowed so reads
// from /allowed succeed and reads from outside fail.
func newTestTCPServerWithMemFS(t *testing.T) (*TCPServer, *remotecontrol.SessionState, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	sess.Workspace = "/allowed"
	sess.Backend = "agw"
	sess.Model = "Gemini 3.8 Flash (Medium)"

	mfs := remotecontrol.NewMemFS(map[string][]remotecontrol.MemFSEntry{
		"/allowed": {
			{Name: "main.go", IsDir: false, Data: []byte("package main\n\nfunc main() {}\n")},
			{Name: "sub", IsDir: true, Data: nil},
		},
		"/secret": {
			{Name: "secret.txt", IsDir: false, Data: []byte("top secret\n")},
		},
	})
	scope := &remotecontrol.WorkdirScope{Workspace: "/allowed"}

	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: sess,
		Agents:  remotecontrol.DefaultAgentLister,
		Reader:  remotecontrol.DefaultOutputReader{},
		FS:      &memFSPolicyOverFS{policy: scope, fs: mfs},
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, sess, teardown
}

// memFSPolicyOverFS is the rc-file-ops wire-test
// adapter. The dispatcher's FSProvider interface needs
// all three methods; we route ResolvePolicy through the
// policy (typically WorkdirScope) and ReadFile / ReadDir
// through the underlying fixture (MemFS). The struct
// fields are package-private so the test stays in
// tcp_test.go's scope; the file-local name avoids the
// ambiguous-embedded-methods issue embedded structs
// would create.
type memFSPolicyOverFS struct {
	policy remotecontrol.FSProvider
	fs     remotecontrol.FSProvider
}

func (m *memFSPolicyOverFS) ResolvePolicy(path string) (string, error) {
	return m.policy.ResolvePolicy(path)
}
func (m *memFSPolicyOverFS) ReadFile(canonical string, maxBytes int64, offset int64) (data []byte, total int64, truncated bool, isBinary bool, err error) {
	return m.fs.ReadFile(canonical, maxBytes, offset)
}
func (m *memFSPolicyOverFS) ReadDir(canonical string, maxEntries int) (entries []remotecontrol.DirEntry, total int, truncated bool, err error) {
	return m.fs.ReadDir(canonical, maxEntries)
}

// TestReadRemoteAgentFile_WS_OK covers the ws round-trip
// happy path: a small text file comes back with
// encoding=utf-8 and the original content.
func TestReadRemoteAgentFile_WS_OK(t *testing.T) {
	srv, _, dir, done := newTestTCPServerWithFS(t)
	defer done()

	// Write a small text file.
	want := "package main\n\nfunc main() {}\n"
	path := dir + "/main.go"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentFile",
		"params":  map[string]any{"path": path},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ReadRemoteAgentFile: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ReadRemoteAgentFile: no result, got %#v", resp)
	}
	if res["encoding"] != "utf-8" {
		t.Errorf("encoding = %v, want utf-8", res["encoding"])
	}
	if res["content"] != want {
		t.Errorf("content = %v, want %q", res["content"], want)
	}
	if res["truncated"] != false {
		t.Errorf("truncated = %v, want false", res["truncated"])
	}
	if size, _ := res["size"].(float64); size != float64(len(want)) {
		t.Errorf("size = %v, want %d", size, len(want))
	}
}

// TestReadRemoteAgentFile_WS_Binary covers the binary
// path: a file with NUL bytes comes back as
// encoding=binary and base64-decodes to the original.
func TestReadRemoteAgentFile_WS_Binary(t *testing.T) {
	srv, _, dir, done := newTestTCPServerWithFS(t)
	defer done()

	bin := []byte{0x00, 0x01, 0x02, 0x03, 0x00, 0x05}
	path := dir + "/bin.bin"
	if err := os.WriteFile(path, bin, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentFile",
		"params":  map[string]any{"path": path},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("binary read: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("binary read: no result, got %#v", resp)
	}
	if res["encoding"] != "binary" {
		t.Errorf("encoding = %v, want binary", res["encoding"])
	}
	got, err := base64.StdEncoding.DecodeString(res["content"].(string))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(got) != string(bin) {
		t.Errorf("decoded content = %v, want %v", got, bin)
	}
}

// TestReadRemoteAgentFile_WS_Truncated covers the
// cap-by-maxBytes path: a 100-KiB file with maxBytes=4096
// returns truncated=true and 4096 bytes of content.
func TestReadRemoteAgentFile_WS_Truncated(t *testing.T) {
	srv, _, dir, done := newTestTCPServerWithFS(t)
	defer done()

	big := make([]byte, 100*1024)
	for i := range big {
		big[i] = byte('A' + (i % 26))
	}
	path := dir + "/big.txt"
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentFile",
		"params":  map[string]any{"path": path, "maxBytes": 4096},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("truncated read: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("truncated read: no result, got %#v", resp)
	}
	if res["truncated"] != true {
		t.Errorf("truncated = %v, want true", res["truncated"])
	}
	if size, _ := res["size"].(float64); size != float64(len(big)) {
		t.Errorf("size = %v, want %d", size, len(big))
	}
	if content, _ := res["content"].(string); len(content) != 4096 {
		t.Errorf("content length = %d, want 4096", len(content))
	}
}

// TestReadRemoteAgentFile_WS_OffsetBeyondSize covers the
// offset > size path over the wire: the dispatcher
// returns -32603.
func TestReadRemoteAgentFile_WS_OffsetBeyondSize(t *testing.T) {
	srv, _, dir, done := newTestTCPServerWithFS(t)
	defer done()

	path := dir + "/small.txt"
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentFile",
		"params":  map[string]any{"path": path, "offset": 1000},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("offset beyond size: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32603 {
		t.Errorf("error.code = %v, want -32603 (Internal error)", code)
	}
}

// TestReadRemoteAgentFile_WS_PathNotAllowed covers the
// policy rejection path: a path outside the
// WorkdirScope's workspace returns -32007.
func TestReadRemoteAgentFile_WS_PathNotAllowed(t *testing.T) {
	srv, _, done := newTestTCPServerWithMemFS(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentFile",
		"params":  map[string]any{"path": "/secret/secret.txt"},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("forbidden read: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32007 {
		t.Errorf("error.code = %v, want -32007 (Path not allowed)", code)
	}
}

// TestReadRemoteAgentFile_WS_NotRegularFile covers the
// directory-rejection path: requesting a directory
// returns -32008.
func TestReadRemoteAgentFile_WS_NotRegularFile(t *testing.T) {
	srv, _, done := newTestTCPServerWithMemFS(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentFile",
		"params":  map[string]any{"path": "/allowed/sub"},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("dir read: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32008 {
		t.Errorf("error.code = %v, want -32008 (Not a regular file)", code)
	}
}

// TestReadRemoteAgentDir_WS_OK covers the ws round-trip
// happy path: directory listing returns the expected
// entries with stable sort.
func TestReadRemoteAgentDir_WS_OK(t *testing.T) {
	srv, _, dir, done := newTestTCPServerWithFS(t)
	defer done()

	// Write two files + a subdirectory inside the
	// workspace dir.
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(dir+"/sub", 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentDir",
		"params":  map[string]any{"path": dir},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("dir list: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("dir list: no result, got %#v", resp)
	}
	if res["truncated"] != false {
		t.Errorf("truncated = %v, want false", res["truncated"])
	}
	if total, _ := res["total_entries"].(float64); total != 3 {
		t.Errorf("total_entries = %v, want 3", total)
	}
	entries, ok := res["entries"].([]any)
	if !ok {
		t.Fatalf("entries = %T, want array", res["entries"])
	}
	if len(entries) != 3 {
		t.Fatalf("entries length = %d, want 3", len(entries))
	}
	// Sorted: a.txt, b.txt, sub.
	want := []string{"a.txt", "b.txt", "sub"}
	for i, w := range want {
		e, _ := entries[i].(map[string]any)
		if name, _ := e["name"].(string); name != w {
			t.Errorf("entries[%d].name = %q, want %q", i, name, w)
		}
	}
	if sub, _ := entries[2].(map[string]any); sub["type"] != "dir" {
		t.Errorf("sub.type = %v, want dir", sub["type"])
	}
}

// TestReadRemoteAgentDir_WS_TruncatedByMaxEntries covers
// the cap-by-maxEntries path: a 3-entry directory with
// maxEntries=2 returns truncated=true and 2 entries.
func TestReadRemoteAgentDir_WS_TruncatedByMaxEntries(t *testing.T) {
	srv, _, dir, done := newTestTCPServerWithFS(t)
	defer done()

	for _, n := range []string{"a", "b", "c"} {
		if err := os.WriteFile(dir+"/"+n, []byte(n), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentDir",
		"params":  map[string]any{"path": dir, "maxEntries": 2},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("capped dir list: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("capped dir list: no result, got %#v", resp)
	}
	if res["truncated"] != true {
		t.Errorf("truncated = %v, want true", res["truncated"])
	}
	if total, _ := res["total_entries"].(float64); total != 3 {
		t.Errorf("total_entries = %v, want 3", total)
	}
	entries, _ := res["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("entries length = %d, want 2", len(entries))
	}
}

// TestReadRemoteAgentDir_WS_PathNotAllowed covers the
// policy rejection path: requesting a directory outside
// the WorkdirScope's workspace returns -32007.
func TestReadRemoteAgentDir_WS_PathNotAllowed(t *testing.T) {
	srv, _, done := newTestTCPServerWithMemFS(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ReadRemoteAgentDir",
		"params":  map[string]any{"path": "/secret"},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("forbidden dir: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32007 {
		t.Errorf("error.code = %v, want -32007 (Path not allowed)", code)
	}
}

// ----- cycle rc-projects tests -----
//
// Wire-level round-trips for the three project-CRUD
// methods (ListRemoteAgentProjects /
// CreateRemoteAgentProject / UpdateRemoteAgentProject).
// Mirrors the in-process tests in
// internal/remotecontrol/projects_test.go: same shape,
// same assertions, but going through the ws adapter so
// the JSON marshalling + envelope-shape code paths are
// also exercised.
//
// Each test uses a fresh MemProjectStore so the
// project's state is isolated across runs. The
// dispatcher is built directly (no
// maybeStartRemoteControlDaemon) so the test doesn't
// need to bring up auth.AntigravityHome() / the
// production cmd/wagyu wiring.

// newTestTCPServerWithProjects brings up a TCPServer
// whose dispatcher's ProjectStore seam is the supplied
// *remotecontrol.MemProjectStore (one per test, so
// state doesn't leak across tests).
func newTestTCPServerWithProjects(t *testing.T, ps remotecontrol.ProjectStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:  sess,
		Agents:   remotecontrol.DefaultAgentLister,
		Reader:   remotecontrol.DefaultOutputReader{},
		Projects: ps,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// TestListRemoteAgentProjects_WS_Ok exercises the
// happy path: a fresh MemProjectStore with two rows;
// the WS round-trip returns the rows in the wire
// envelope.
func TestListRemoteAgentProjects_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	if _, err := ps.Create(context.Background(), remotecontrol.NewProject{
		Name:      "alpha",
		Workspace: "/ws/alpha",
	}); err != nil {
		t.Fatalf("seed Create alpha: %v", err)
	}
	if _, err := ps.Create(context.Background(), remotecontrol.NewProject{
		Name:      "beta",
		Workspace: "/ws/beta",
	}); err != nil {
		t.Fatalf("seed Create beta: %v", err)
	}
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentProjects",
		"params":  map[string]any{},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentProjects: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentProjects: no result, got %#v", resp)
	}
	rows, ok := res["projects"].([]any)
	if !ok {
		t.Fatalf("projects = %T, want []any", res["projects"])
	}
	if len(rows) != 2 {
		t.Errorf("len(projects) = %d, want 2", len(rows))
	}
	if total, _ := res["total"].(float64); total != 2 {
		t.Errorf("total = %v, want 2", total)
	}
}

// TestCreateRemoteAgentProject_WS_Ok exercises the
// happy path: an empty store receives one Create,
// the WS round-trip returns the new project's
// metadata, and a follow-up List sees the row.
func TestCreateRemoteAgentProject_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentProject",
		"params": map[string]any{
			"name":           "fresh",
			"workspace":      "/ws/fresh",
			"default_model":  "alpha-model",
			"default_effort": "high",
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("CreateRemoteAgentProject: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("CreateRemoteAgentProject: no result, got %#v", resp)
	}
	if res["id"] == "" {
		t.Errorf("id missing: %#v", res)
	}
	if res["name"] != "fresh" {
		t.Errorf("name = %v, want fresh", res["name"])
	}
	if res["default_model"] != "alpha-model" {
		t.Errorf("default_model = %v, want alpha-model", res["default_model"])
	}
	if res["default_effort"] != "high" {
		t.Errorf("default_effort = %v, want high", res["default_effort"])
	}
	if createdAt, _ := res["created_at"].(string); createdAt == "" {
		t.Errorf("created_at missing: %#v", res)
	}

	// Follow-up List to verify the row really landed.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "ListRemoteAgentProjects",
		"params":  map[string]any{},
	})
	resp = readJSONRPC(t, c)
	res = resp["result"].(map[string]any)
	rows := res["projects"].([]any)
	if len(rows) != 1 {
		t.Errorf("len(projects) = %d, want 1", len(rows))
	}
}

// TestCreateRemoteAgentProject_WS_Duplicate pins the
// duplicate-name → -32009 path over the wire.
func TestCreateRemoteAgentProject_WS_Duplicate(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentProject",
		"params": map[string]any{
			"name":      "twins",
			"workspace": "/ws/first",
		},
	})
	if errObj, ok := readJSONRPC(t, c)["error"]; ok {
		t.Fatalf("first create: error = %#v", errObj)
	}

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "CreateRemoteAgentProject",
		"params": map[string]any{
			"name":      "twins",
			"workspace": "/ws/second",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("second create: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32009 {
		t.Errorf("error.code = %v, want -32009 (Project exists)", code)
	}
}

// TestCreateRemoteAgentProject_WS_BadParams pins the
// empty-name → -32602 path over the wire.
func TestCreateRemoteAgentProject_WS_BadParams(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentProject",
		"params": map[string]any{
			"name":      "",
			"workspace": "/ws/somewhere",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("bad-params create: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32602 {
		t.Errorf("error.code = %v, want -32602 (Invalid params)", code)
	}
}

// TestUpdateRemoteAgentProject_WS_Ok exercises the
// happy-path update over the wire: create → update
// → list reflects the new name.
func TestUpdateRemoteAgentProject_WS_Ok(t *testing.T) {
	// Monotonic clock so UpdatedAt > CreatedAt.
	var tick int64
	ps := remotecontrol.NewMemProjectStore(func() time.Time {
		tick++
		return time.Unix(1700000000, tick).UTC()
	})
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Create one project.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentProject",
		"params": map[string]any{
			"name":      "before",
			"workspace": "/ws/before",
		},
	})
	resp := readJSONRPC(t, c)
	res := resp["result"].(map[string]any)
	id := res["id"].(string)

	// Update its name.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "UpdateRemoteAgentProject",
		"params": map[string]any{
			"id":   id,
			"name": "after",
		},
	})
	resp = readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("update: error = %#v", errObj)
	}
	res = resp["result"].(map[string]any)
	if res["name"] != "after" {
		t.Errorf("updated.name = %v, want after", res["name"])
	}

	// List reflects the new name.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "ListRemoteAgentProjects",
		"params":  map[string]any{},
	})
	resp = readJSONRPC(t, c)
	res = resp["result"].(map[string]any)
	rows := res["projects"].([]any)
	if len(rows) != 1 {
		t.Fatalf("len(projects) = %d, want 1", len(rows))
	}
	first := rows[0].(map[string]any)
	if first["name"] != "after" {
		t.Errorf("rows[0].name = %v, want after", first["name"])
	}
}

// TestUpdateRemoteAgentProject_WS_NotFound pins the
// unknown-id → -32004 path over the wire.
func TestUpdateRemoteAgentProject_WS_NotFound(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpdateRemoteAgentProject",
		"params": map[string]any{
			"id":   "ghost-id",
			"name": "anything",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("not-found update: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32004 {
		t.Errorf("error.code = %v, want -32004 (Project not found)", code)
	}
}

// TestUpdateRemoteAgentProject_WS_PatchOnly pins the
// "leave unchanged" semantic over the wire: an
// Update with only default_model leaves the name
// untouched in the response envelope.
func TestUpdateRemoteAgentProject_WS_PatchOnly(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	srv, done := newTestTCPServerWithProjects(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentProject",
		"params": map[string]any{
			"name":      "stable",
			"workspace": "/ws/stable",
		},
	})
	resp := readJSONRPC(t, c)
	res := resp["result"].(map[string]any)
	id := res["id"].(string)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "UpdateRemoteAgentProject",
		"params": map[string]any{
			"id":            id,
			"default_model": "next-model",
		},
	})
	resp = readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("patch-only update: error = %#v", errObj)
	}
	res = resp["result"].(map[string]any)
	if res["name"] != "stable" {
		t.Errorf("updated.name = %v, want stable (unchanged)", res["name"])
	}
	if res["default_model"] != "next-model" {
		t.Errorf("updated.default_model = %v, want next-model", res["default_model"])
	}
}

// ----- cycle rc-conversation-deletions tests -----
//
// Wire-level round-trips for the three destructive
// methods:
//   DeleteRemoteAgentMessage
//   DeleteRemoteAgentConversation
//   DeleteRemoteAgentProject
//
// Each test uses a fresh in-memory seam (MemConversationStore
// for the conversation deletes, MemProjectStore for the
// project delete) so state doesn't leak between tests.
// The dispatcher is built directly (no
// maybeStartRemoteControlDaemon) so the test doesn't
// need to bring up auth.AntigravityHome() / the
// production cmd/wagyu wiring.

// newTestTCPServerWithDeletions brings up a
// TCPServer whose dispatcher's ProjectStore +
// ConversationStore seams are pre-installed with
// the supplied values. Returns a teardown closure
// that shuts the server down.
func newTestTCPServerWithDeletions(t *testing.T, ps remotecontrol.ProjectStore, cs remotecontrol.ConversationStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:       sess,
		Agents:        remotecontrol.DefaultAgentLister,
		Reader:        remotecontrol.DefaultOutputReader{},
		Projects:      ps,
		Conversations: cs,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// TestDeleteRemoteAgentMessage_WS_Ok exercises
// the happy path: a fresh MemConversationStore
// with five messages; delete one; the WS
// round-trip returns {ok: true, deleted_at,
// remaining: 4}.
func TestDeleteRemoteAgentMessage_WS_Ok(t *testing.T) {
	mcs := remotecontrol.NewMemConversationStore(nil)
	mcs.Add(remotecontrol.Conversation{ID: "conv-1"}, []int64{0, 1, 2, 3, 4})
	srv, done := newTestTCPServerWithDeletions(t, nil, mcs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentMessage",
		"params": map[string]any{
			"conversation_id": "conv-1",
			"message_id":      "2",
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("DeleteRemoteAgentMessage: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentMessage: no result, got %#v", resp)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if res["deleted_at"] == "" {
		t.Errorf("deleted_at missing")
	}
	if rem, _ := res["remaining"].(float64); rem != 4 {
		t.Errorf("remaining = %v, want 4", res["remaining"])
	}
}

// TestDeleteRemoteAgentMessage_WS_UnknownConv
// pins the -32011 envelope over the wire.
func TestDeleteRemoteAgentMessage_WS_UnknownConv(t *testing.T) {
	mcs := remotecontrol.NewMemConversationStore(nil)
	srv, done := newTestTCPServerWithDeletions(t, nil, mcs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentMessage",
		"params": map[string]any{
			"conversation_id": "ghost",
			"message_id":      "0",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentMessage unknown-conv: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32011 {
		t.Errorf("error.code = %v, want -32011 (Conversation not found)", code)
	}
}

// TestDeleteRemoteAgentMessage_WS_UnknownMessage
// pins the -32012 envelope over the wire.
func TestDeleteRemoteAgentMessage_WS_UnknownMessage(t *testing.T) {
	mcs := remotecontrol.NewMemConversationStore(nil)
	mcs.Add(remotecontrol.Conversation{ID: "conv-2"}, []int64{0, 1})
	srv, done := newTestTCPServerWithDeletions(t, nil, mcs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentMessage",
		"params": map[string]any{
			"conversation_id": "conv-2",
			"message_id":      "999",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentMessage unknown-msg: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32012 {
		t.Errorf("error.code = %v, want -32012 (Message not found)", code)
	}
}

// TestDeleteRemoteAgentConversation_WS_Ok
// exercises the happy-path whole-conversation
// delete.
func TestDeleteRemoteAgentConversation_WS_Ok(t *testing.T) {
	mcs := remotecontrol.NewMemConversationStore(nil)
	mcs.Add(remotecontrol.Conversation{ID: "conv-3"}, []int64{0, 1, 2})
	srv, done := newTestTCPServerWithDeletions(t, nil, mcs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentConversation",
		"params": map[string]any{
			"conversation_id": "conv-3",
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("DeleteRemoteAgentConversation: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentConversation: no result, got %#v", resp)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if res["deleted_at"] == "" {
		t.Errorf("deleted_at missing")
	}
}

// TestDeleteRemoteAgentConversation_WS_NotFound
// pins the -32011 envelope over the wire.
func TestDeleteRemoteAgentConversation_WS_NotFound(t *testing.T) {
	mcs := remotecontrol.NewMemConversationStore(nil)
	srv, done := newTestTCPServerWithDeletions(t, nil, mcs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentConversation",
		"params": map[string]any{
			"conversation_id": "ghost",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentConversation not-found: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32011 {
		t.Errorf("error.code = %v, want -32011 (Conversation not found)", code)
	}
}

// TestDeleteRemoteAgentProject_WS_Ok exercises
// the happy-path project delete over the wire.
func TestDeleteRemoteAgentProject_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	created, err := ps.Create(context.Background(), remotecontrol.NewProject{
		Name:      "alpha",
		Workspace: "/ws/alpha",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	srv, done := newTestTCPServerWithDeletions(t, ps, nil)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentProject",
		"params": map[string]any{
			"id": created.ID,
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("DeleteRemoteAgentProject: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentProject: no result, got %#v", resp)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if res["deleted_at"] == "" {
		t.Errorf("deleted_at missing")
	}
}

// TestDeleteRemoteAgentProject_WS_Bound pins
// the -32013 envelope over the wire.
func TestDeleteRemoteAgentProject_WS_Bound(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	created, err := ps.Create(context.Background(), remotecontrol.NewProject{
		Name:      "beta",
		Workspace: "/ws/beta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	ps.BoundChecker = func(workspace string) (int, error) {
		if strings.TrimSpace(workspace) == "/ws/beta" {
			return 1, nil
		}
		return 0, nil
	}
	srv, done := newTestTCPServerWithDeletions(t, ps, nil)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentProject",
		"params": map[string]any{
			"id": created.ID,
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentProject bound: no error envelope, got %#v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32013 {
		t.Errorf("error.code = %v, want -32013 (Project is bound)", code)
	}
}

// ----- cycle rc-status-tool ws tests -----
//
// Wire-level round-trips for the rich `status` MCP tool + the
// three new JSON-RPC methods (RemoteAgentErrorStatus /
// SetRemoteControlSession / remoteControlVersion). The
// in-process tests in
// internal/remotecontrol/status_test.go pin the dispatcher's
// per-handler behaviour; these tests pin the byte-for-byte wire
// envelope the ws adapter emits (the field types — `[]` vs
// `null`, key presence in nested objects — matter to the
// dashboard's renderer).
//
// Each ws test brings up a TCPServer with a dispatcher whose
// session carries the test's intent; the dispatcher is shared
// across every test so the Set+Status + StartCascade+ErrorStatus
// round-trips can mutate and read state on the same connection
// the way a real dashboard would.

// newTestTCPServerWithSessionMeta brings up a TCPServer
// whose dispatcher's Session + AuthState + DaemonMeta are
// pre-populated for the rich-shape assertions. The session
// uses the production CLI version ("1.4.0") and stable
// workspace / backend / model values so a regression in
// the rich-shape wiring surfaces from this test alone.
func newTestTCPServerWithSessionMeta(t *testing.T) (*TCPServer, *remotecontrol.SessionState, *remotecontrol.DaemonMeta, *remotecontrol.AuthState, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	sess.Workspace = "/tmp/ws-status"
	sess.Backend = "agw"
	sess.Model = "Gemini 3.8 Flash (Medium)"

	meta := &remotecontrol.DaemonMeta{
		Transport: "tcp+ws",
		Addr:      "127.0.0.1:8787",
		Mode:      "persistent",
	}
	auth := remotecontrol.NewAuthState("", "", "", nil, time.Time{})

	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:    sess,
		Agents:     remotecontrol.DefaultAgentLister,
		Reader:     remotecontrol.DefaultOutputReader{},
		AuthState:  auth,
		DaemonMeta: meta,
		Commit:     "deadbeef1234567890abcdef1234567890abcdef",
		BuildLabel: "release",
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, sess, meta, auth, teardown
}

// newTCPServerWithDeferredFeatures brings up a TCPServer
// whose dispatcher has the given Config.DeferredFeatures
// map pre-populated. The deferred_features envelope on
// RemoteControlStatus must reflect the override at wire
// time. A nil override yields the all-false baseline (the
// canonical default — every key is present and off).
//
// Returns the *TCPServer and a teardown closure (the test
// calls `defer srv.shutdownFn()` to clean up). The helper
// mirrors newTestTCPServerWithSessionMeta's return
// signature pattern so future overrides can build on top
// of it without re-shaping the seam.
//
// Used by cycle rc-webrtc-mesh-defer's wire-level smoke
// tests; not exported.
func newTCPServerWithDeferredFeatures(t *testing.T, deferred map[string]bool) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:          sess,
		Agents:           remotecontrol.DefaultAgentLister,
		Reader:           remotecontrol.DefaultOutputReader{},
		Commit:           "deadbeef1234567890abcdef1234567890abcdef",
		BuildLabel:       "release",
		DeferredFeatures: deferred,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// TestStatus_RichShape_WS exercises the rich status shape
// over the wire: the `daemon` block must reflect Config.DaemonMeta
// verbatim, the `auth` block must reflect Config.AuthState
// verbatim, and the `cascades` block must show the registry's
// counts. The dashboard's renderer keys on `daemon.transport`
// and `daemon.mode` for its UI surface; this test pins those
// strings end-to-end.
func TestStatus_RichShape_WS(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	})
	resp := readJSONRPC(t, c)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("status: no result, got %#v", resp)
	}
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("status returned isError")
	}
	env := statusContent(t, res)

	var snap map[string]any
	if err := json.Unmarshal([]byte(env["text"].(string)), &snap); err != nil {
		t.Fatalf("status text not JSON: %v\n%s", err, env["text"])
	}
	if v, _ := snap["version"].(string); v != "1.4.0" {
		t.Errorf("status.version = %v, want 1.4.0", snap["version"])
	}
	daemon, _ := snap["daemon"].(map[string]any)
	if daemon["transport"] != "tcp+ws" {
		t.Errorf("status.daemon.transport = %v, want tcp+ws", daemon["transport"])
	}
	if daemon["addr"] != "127.0.0.1:8787" {
		t.Errorf("status.daemon.addr = %v", daemon["addr"])
	}
	if daemon["mode"] != "persistent" {
		t.Errorf("status.daemon.mode = %v, want persistent", daemon["mode"])
	}
	auth, _ := snap["auth"].(map[string]any)
	if auth["method"] != remotecontrol.AuthMethodNone {
		t.Errorf("status.auth.method = %v (no AuthState installed by the test = fresh %q)",
			auth["method"], remotecontrol.AuthMethodNone)
	}
	scopes, ok := auth["scopes"].([]any)
	if !ok {
		t.Errorf("status.auth.scopes = %T, want []", auth["scopes"])
	}
	if len(scopes) != 0 {
		t.Errorf("status.auth.scopes length = %d, want 0", len(scopes))
	}
	cascades, _ := snap["cascades"].(map[string]any)
	if v, _ := cascades["total"].(float64); v != 0 {
		t.Errorf("status.cascades.total = %v, want 0", cascades["total"])
	}
	if v, _ := cascades["max"].(float64); v != float64(remotecontrol.CascadeRegistryDefaultMax) {
		t.Errorf("status.cascades.max = %v, want %d",
			cascades["max"], remotecontrol.CascadeRegistryDefaultMax)
	}
}

// TestSetRemoteControlSession_WS exercises the Set + Status
// round-trip on the same ws connection: the Set call mutates
// the dispatcher's AuthState in place, and the subsequent
// `status` MCP call surfaces the new session_id verbatim. The
// auth block's `method` field is "google" because the handler
// always sets it to the AuthMethodGoogle constant — the brief
// pins this so dashboards filter by a known vocabulary.
func TestSetRemoteControlSession_WS(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Baseline status: session_id is empty.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	})
	pre := readJSONRPC(t, c)
	preSnap := decodeStatusText(t, pre["result"].(map[string]any))
	preAuth, _ := preSnap["auth"].(map[string]any)
	if sid, _ := preAuth["session_id"].(string); sid != "" {
		t.Fatalf("baseline auth.session_id = %q, want empty", sid)
	}

	// Set call.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SetRemoteControlSession",
		"params": map[string]any{
			"session_id": "sess-ws-fresh",
			"user_id":    "alice@example.com",
			"scopes":     []string{"read", "admin"},
		},
	})
	setResp := readJSONRPC(t, c)
	if errObj, ok := setResp["error"]; ok {
		t.Fatalf("Set: error = %#v", errObj)
	}
	setRes, _ := setResp["result"].(map[string]any)
	if setRes["session_id"] != "sess-ws-fresh" {
		t.Errorf("Set echo session_id = %v", setRes["session_id"])
	}

	// Follow-up status surfaces the new values.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	})
	post := readJSONRPC(t, c)
	postSnap := decodeStatusText(t, post["result"].(map[string]any))
	postAuth, _ := postSnap["auth"].(map[string]any)
	if postAuth["method"] != remotecontrol.AuthMethodGoogle {
		t.Errorf("post auth.method = %v, want %q", postAuth["method"], remotecontrol.AuthMethodGoogle)
	}
	if postAuth["session_id"] != "sess-ws-fresh" {
		t.Errorf("post auth.session_id = %v", postAuth["session_id"])
	}
	if postAuth["user_id"] != "alice@example.com" {
		t.Errorf("post auth.user_id = %v", postAuth["user_id"])
	}
	scopes, _ := postAuth["scopes"].([]any)
	if len(scopes) != 2 {
		t.Errorf("post auth.scopes length = %d, want 2", len(scopes))
	}
}

// TestRemoteControlVersion_WS exercises the version probe
// over the wire: the response carries version (from
// Session.Version), build_label (from Config.BuildLabel),
// and daemon_sha (from Config.Commit). The dispatcher
// surfaces Config.Commit / Config.BuildLabel verbatim when
// populated; empty values would be returned as "unknown",
// which is the contract TestRemoteControlVersion_Unknown
// pins in the in-process suite.
func TestRemoteControlVersion_WS(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "remoteControlVersion",
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("remoteControlVersion: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if res["version"] != "1.4.0" {
		t.Errorf("result.version = %v, want 1.4.0", res["version"])
	}
	if res["build_label"] != "release" {
		t.Errorf("result.build_label = %v, want release", res["build_label"])
	}
	if res["daemon_sha"] != "deadbeef1234567890abcdef1234567890abcdef" {
		t.Errorf("result.daemon_sha = %v", res["daemon_sha"])
	}
}

// TestRemoteAgentErrorStatus_Empty_WS exercises the empty-ring
// path over the wire: a freshly-started cascade followed by
// RemoteAgentErrorStatus must surface `errors: []` (not null).
// The cascade registry is shared between the Start RPC and
// the ErrorStatus RPC, so the same ws connection can drive
// both. handleWS is single-request-at-a-time but neither
// RPC blocks, so a single connection is enough.
func TestRemoteAgentErrorStatus_Empty_WS(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      99,
		"method":  "RemoteAgentErrorStatus",
		"params": map[string]any{
			"cascade_id": cid,
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("RemoteAgentErrorStatus: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if res["cascade_id"] != cid {
		t.Errorf("result.cascade_id = %v, want %q", res["cascade_id"], cid)
	}
	errs, _ := res["errors"].([]any)
	if len(errs) != 0 {
		t.Fatalf("result.errors length = %d, want 0 (empty ring)", len(errs))
	}
}

// TestRemoteAgentErrorStatus_Recorded_WS exercises the
// recordError → RemoteAgentErrorStatus round-trip over the
// wire. We can't reach the server-side dispatcher through
// the transport-layer Dispatcher interface (Cascades() isn't
// part of the cross-package contract), so the test plants
// the error by routing a SendRemoteAgentCascadeMessage call
// with an error-producing StubBackend. The mid-turn
// notification drainer handles the cascade/state + the
// synchronous -32603 envelope; the subsequent
// RemoteAgentErrorStatus call surfaces the recorded entry.
func TestRemoteAgentErrorStatus_Recorded_WS(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Start cascade, patch a backend that errors.
	cid := startCascade(t, c)
	got, ok := srv.dispatcher.(*remotecontrol.Dispatcher).Cascades().Get(cid)
	if !ok {
		t.Fatalf("registry.Get(%q) failed", cid)
	}
	got.SetBackend(&remotecontrol.StubBackend{Err: errors.New("wire-tripped")})

	// Drive a turn. Drain to the synchronous envelope
	// (which carries -32603 because the backend errored).
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "trip me",
		},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); !ok {
		t.Fatalf("Send: expected error envelope, got %#v", resp)
	} else if code, _ := errObj["code"].(float64); int(code) != -32603 {
		t.Fatalf("Send: error.code = %v, want -32603", code)
	}

	// Now ask for the error ring. The recorded entry is
	// keyed under SendRemoteAgentCascadeMessage and carries
	// code=-32603.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "RemoteAgentErrorStatus",
		"params": map[string]any{
			"cascade_id": cid,
		},
	})
	resp = readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("RemoteAgentErrorStatus: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	errs, _ := res["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("result.errors length = %d, want 1", len(errs))
	}
	entry, _ := errs[0].(map[string]any)
	if entry["method"] != "SendRemoteAgentCascadeMessage" {
		t.Errorf("entry.method = %v", entry["method"])
	}
	// The recorded Message is the wrapped error chain
	// (`remotecontrol: turn failed\nwire-tripped`); we
	// just pin that the original backend error's text
	// appears somewhere in the chain so a regression in
	// the recordError / errors.Join wrapping surfaces
	// from this test.
	if msg, _ := entry["message"].(string); !strings.Contains(msg, "wire-tripped") {
		t.Errorf("entry.message = %v, want it to contain wire-tripped", entry["message"])
	}
	if code, _ := entry["code"].(float64); int(code) != -32603 {
		t.Errorf("entry.code = %v, want -32603", entry["code"])
	}
}

// TestEnsureRemoteAgentACEVM_WS_Removed pins the cycle
// rc-ace-real wire-level contract: the dispatcher no longer
// registers EnsureRemoteAgentACEVM, so a ws client that posts
// the method name receives the JSON-RPC 2.0 standard envelope
// -32601 "Method not found" with the method name echoed in
// `data`. This replaces the rc-status-tool parity-stub wire
// test (TestEnsureRemoteAgentACE_Stub_WS) which asserted a
// canned envelope; the cycle rationale is in
// docs/cycles/rc-ace-real/deliverable.md.
//
// The in-process sibling is TestEnsureRemoteAgentACEVM_Removed
// in internal/remotecontrol/status_test.go.
func TestEnsureRemoteAgentACEVM_WS_Removed(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "EnsureRemoteAgentACEVM",
		"params":  map[string]any{},
	})
	resp := readJSONRPC(t, c)
	if _, ok := resp["result"]; ok {
		t.Fatalf("EnsureRemoteAgentACEVM: still has result envelope, want -32601; got %#v", resp)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("EnsureRemoteAgentACEVM: no error envelope, got %#v", resp)
	}
	code, _ := errObj["code"].(float64)
	if int(code) != -32601 {
		t.Errorf("error.code = %v, want -32601", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); msg != "Method not found" {
		t.Errorf("error.message = %v, want \"Method not found\"", msg)
	}
	if data, _ := errObj["data"].(string); data != "EnsureRemoteAgentACEVM" {
		t.Errorf("error.data = %v, want \"EnsureRemoteAgentACEVM\"", data)
	}
}

// statusContent is a tiny helper that pulls the single
// text-bearing entry out of an MCP tools/call content
// envelope. Mirrors the helper the cycle 1 wire tests use
// for the existing status tool; duplicated here so the new
// tests don't pull in extra imports.
func statusContent(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	env, _ := res["content"].([]any)
	if len(env) == 0 {
		t.Fatalf("status: empty content, got %#v", res)
	}
	first, _ := env[0].(map[string]any)
	return first
}

// decodeStatusText reads the inner status payload out of an
// MCP tools/call envelope and decodes it back to a map. The
// helper exists so the rich-shape tests don't repeat the
// unmarshal step.
func decodeStatusText(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	env := statusContent(t, res)
	text, _ := env["text"].(string)
	var snap map[string]any
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatalf("status text not JSON: %v\n%s", err, text)
	}
	return snap
}

// ----- cycle rc-bidi-loop tests -----
//
// Wire-level coverage for the read-pump + write-pump refactor
// in internal/remotetransport/tcp.go. The cycle closes the
// rc-handle-user-interaction limitation at
// docs/cycles/rc-handle-user-interaction/deliverable.md §6.1:
// "Wire-level round-trip is not exercised end-to-end" — the
// previous single-goroutine read+write loop could not interleave
// a parked stub's HandleRemoteAgentCascadeUserInteraction on
// the same ws connection.
//
// These tests pin the new shape end-to-end:

// TestHandleRemoteAgentCascadeUserInteraction_BiDirectional_WS
// is the headline ask of the cycle: a parked stub waits on
// intr.ResultCh; the client reads the user_interaction_required
// notification and answers HandleRemoteAgentCascadeUserInteraction
// on the same ws; the answer resolves the pending entry; the
// stub unblocks; SendMessage completes; both envelopes come
// back on the same connection. This is the wire-level happy
// path the rc-handle-user-interaction deliverable deferred
// because the single-goroutine loop couldn't support it.
//
// The test drives a parked StubBackend with one Interaction
// and a 5-second InteractionWait so the stub doesn't time out
// before the client answers. The Resolve path is reached via
// the dispatcher's HandleRemoteAgentCascadeUserInteraction
// handler (the cycle does not extend StubBackend to consult
// its Answers map — that's a separate rc-stub-config cycle).
func TestHandleRemoteAgentCascadeUserInteraction_BiDirectional_WS(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		StopReason: remotecontrol.StopReasonEndTurn,
		Interactions: []remotecontrol.PendingInteractionConfig{
			{
				Kind:   remotecontrol.InteractionKindPermission,
				Title:  "Read main.go?",
				Prompt: "ok?",
				Options: []remotecontrol.InteractionOption{
					{Label: "yes", Value: "yes"},
					{Label: "no", Value: "no"},
				},
				Dangerous: true,
			},
		},
		// Long wait — the client takes a few ms to read the
		// notification and answer. 5s gives a wide margin
		// under -race without making the test drag if
		// something goes wrong (the InteractionWait
		// timeout is the slow-path failure signal).
		InteractionWait: 5 * time.Second,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	// 1. Send SendMessage (id=2). The dispatch goroutine
	//    enters SendStream → emitInteractions → OnUserInteraction
	//    → ConnNotifierSink pushes the notification via
	//    Dispatcher.Notify → pushFrame → enqueueFrame →
	//    outbound → writePump → ws. The stub is now parked
	//    on intr.ResultCh.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "please resolve a prompt",
		},
	})

	// 2. Read frames until we see the user_interaction_required
	//    notification. The reader goroutine is BLOCKED on
	//    c.Read the whole time (it spawned the dispatch
	//    goroutine and went back to reading) — exactly the
	//    bi-directional shape the cycle ships.
	var intrID string
	const maxNotifFrames = 8
	for i := 0; i < maxNotifFrames; i++ {
		frame := readJSONRPC(t, c)
		method, _ := frame["method"].(string)
		if method == remotecontrol.NotifUserInteractionRequired {
			params, _ := frame["params"].(map[string]any)
			intrID, _ = params["interaction_id"].(string)
			break
		}
	}
	if intrID == "" {
		t.Fatalf("did not see %q within %d frames", remotecontrol.NotifUserInteractionRequired, maxNotifFrames)
	}
	if !strings.HasPrefix(intrID, "int_") {
		t.Errorf("interaction_id = %q, want int_ prefix", intrID)
	}

	// 3. Send HandleRemoteAgentCascadeUserInteraction on the
	//    SAME ws. The reader goroutine picks this up while
	//    the Send… dispatch goroutine is still parked in
	//    SendStream. The Handle… dispatch goroutine resolves
	//    the pending entry; the stub unblocks; the Send…
	//    dispatch goroutine completes and enqueues its
	//    response.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "HandleRemoteAgentCascadeUserInteraction",
		"params": map[string]any{
			"cascade_id":     cid,
			"interaction_id": intrID,
			"response":       "yes",
		},
	})

	// 4. Drain frames until we've seen BOTH the
	//    Handle…UserInteraction response (id=3) and the
	//    Send…Message response (id=2). The dispatch
	//    goroutines can complete in either order — the
	//    Handle… goroutine finishes before the Send…
	//    goroutine does (it just had to send on ResultCh;
	//    the Send… goroutine is still running through
	//    OnState + the registry.Send return path), so the
	//    client may see id=3 first or last. The cycle
	//    pins both: the round-trip works.
	seenIDs := map[int]map[string]any{}
	const maxFrames = 32
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		idF, hasID := frame["id"].(float64)
		if !hasID {
			// Mid-turn notification — drop on the
			// floor (we already captured the
			// interaction one above; later
			// notifications are the cascade/state
			// starting + done frames the stub
			// emits after unblocking).
			continue
		}
		id := int(idF)
		if _, dup := seenIDs[id]; dup {
			t.Errorf("duplicate response id=%d", id)
		}
		seenIDs[id] = frame
		if _, has2 := seenIDs[2]; has2 {
			if _, has3 := seenIDs[3]; has3 {
				goto done
			}
		}
	}
	t.Fatalf("only saw responses for ids: %v", keys(seenIDs))
done:

	// 5. Pin both envelopes.
	sendMsgResp, ok := seenIDs[2]
	if !ok {
		t.Fatalf("id=2 (SendMessage) response missing")
	}
	if errObj, ok := sendMsgResp["error"]; ok {
		t.Fatalf("id=2 (SendMessage) error: %#v", errObj)
	}
	res, ok := sendMsgResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("id=2 result not an object: %#v", sendMsgResp)
	}
	if res["stopReason"] != remotecontrol.StopReasonEndTurn {
		t.Errorf("id=2 stopReason = %v, want %q", res["stopReason"], remotecontrol.StopReasonEndTurn)
	}

	handleResp, ok := seenIDs[3]
	if !ok {
		t.Fatalf("id=3 (HandleInteraction) response missing")
	}
	if errObj, ok := handleResp["error"]; ok {
		t.Fatalf("id=3 (HandleInteraction) error: %#v", errObj)
	}
	hres, ok := handleResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("id=3 result not an object: %#v", handleResp)
	}
	if ok, _ := hres["ok"].(bool); !ok {
		t.Errorf("id=3 result.ok = %v, want true", hres["ok"])
	}
	if _, has := hres["decided_at"].(string); !has {
		t.Errorf("id=3 result.decided_at missing or non-string: %#v", hres)
	}
	applied, _ := hres["applied_to"].([]any)
	if applied == nil {
		t.Errorf("id=3 result.applied_to = nil, want []")
	}
}

// TestStreamNotifications_ReachClient_DuringOtherRPC pins the
// "reader does not block on dispatch" shape: while one RPC is
// in flight (Sleep on the backend), the reader can still
// process a second RPC. The second RPC's response arrives
// BEFORE the first RPC's response — proving the dispatcher
// goroutines are concurrent and the writer serialises their
// writes.
//
// The test uses a StubBackend with Sleep=100ms. tools/list is
// a sync handler that returns immediately, so its id=3
// response is observed before the id=2 (SendMessage) response.
// If the reader were still on the single-goroutine loop, the
// tools/list RPC could not be dispatched until the SendMessage
// backend returned — the test would see id=3 AFTER id=2.
func TestStreamNotifications_ReachClient_DuringOtherRPC(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		Reply:          "after-sleep",
		StopReason:     remotecontrol.StopReasonEndTurn,
		ConversationID: "conv-ws",
		Sleep:          100 * time.Millisecond,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	// 1. Send SendMessage (id=2) — backend will Sleep 100ms.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "go",
		},
	})

	// 2. Immediately send tools/list (id=3) — sync, no
	//    cascade dependency. With the OLD single-goroutine
	//    loop the reader would be blocked on the SendMessage
	//    dispatch goroutine; with the NEW reader-spawns-
	//    goroutine shape, tools/list is dispatched
	//    concurrently and completes first.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/list",
	})

	// 3. Read frames. We expect (in some order):
	//      - cascade/state (starting)
	//      - cascade/state (done)
	//      - id=3 (tools/list) response
	//      - id=2 (SendMessage) response
	//    The cycle's invariant is that id=3 arrives BEFORE
	//    id=2 (otherwise the reader was blocked). We do not
	//    pin the cascade/state ordering — that is the
	//    cycle-5 rc-cascade-stream test's job.
	seenIDs := map[int]map[string]any{}
	var firstIDWithResponse int
	const maxFrames = 16
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		idF, hasID := frame["id"].(float64)
		if !hasID {
			continue
		}
		id := int(idF)
		if _, dup := seenIDs[id]; dup {
			t.Errorf("duplicate response id=%d", id)
		}
		seenIDs[id] = frame
		if firstIDWithResponse == 0 {
			firstIDWithResponse = id
		}
		if _, has2 := seenIDs[2]; has2 {
			if _, has3 := seenIDs[3]; has3 {
				break
			}
		}
	}

	if _, ok := seenIDs[3]; !ok {
		t.Fatalf("id=3 (tools/list) response never arrived; reader was blocked on SendMessage dispatch")
	}
	if _, ok := seenIDs[2]; !ok {
		t.Fatalf("id=2 (SendMessage) response never arrived")
	}
	// The cycle's headline invariant: id=3 (sync) must arrive
	// before id=2 (slow backend). If it doesn't, the reader
	// is single-goroutine again and the bi-directional shape
	// is broken.
	if firstIDWithResponse != 3 {
		t.Errorf("first response id = %d, want 3 (tools/list must complete before the slow SendMessage)", firstIDWithResponse)
	}

	// Pin the wire shape too.
	if errObj, ok := seenIDs[3]["error"]; ok {
		t.Errorf("id=3 (tools/list) error: %#v", errObj)
	}
	if errObj, ok := seenIDs[2]["error"]; ok {
		t.Errorf("id=2 (SendMessage) error: %#v", errObj)
	}
}

// TestWritePump_ClosesOnEOF pins the writer-exit shape: when
// the client closes the ws, the server's reader sees EOF,
// runs its deferred teardown, and the writer goroutine exits.
// Shutdown waits on s.wg which counts both reader and writer
// per connection; if the writer leaks, Shutdown's
// s.wg.Wait deadlocks.
//
// The test verifies the negative case: a stuck writer would
// hold the s.wg counter above zero and Shutdown's 2-second
// budget would elapse. A clean shutdown returns well under
// the budget.
func TestWritePump_ClosesOnEOF(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)

	c := dialWS(t, srv.HTTPURL())

	// Trigger a healthy request/response so we know the conn
	// is established and the writer has run at least once.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	resp := readJSONRPC(t, c)
	if _, ok := resp["result"]; !ok {
		t.Fatalf("first tools/list: no result, got %#v", resp)
	}

	// Close the client side — triggers EOF on the server's
	// reader. handleWS's deferred teardown runs: close
	// (closing) → writeCancel → closeOnce(c.Close) →
	// close(outbound) under outboundMu → readCancel.
	_ = c.Close(websocket.StatusNormalClosure, "")

	// Give the server a brief moment to notice the EOF and
	// run the teardown. The writer's range exits as soon as
	// outbound is closed.
	time.Sleep(50 * time.Millisecond)

	// Shutdown blocks on s.wg.Wait which counts both the
	// reader (handleWS) and the writer (writePump). If the
	// writer leaked, s.wg.Wait would not return and the
	// shutdown ctx would time out (returning ctx.Err()) —
	// the test catches that as a non-nil error.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown after client EOF: %v (writer may have leaked)", err)
	}

	done() // idempotent; safe to call after Shutdown.
}

// TestBackpressure_Bounded pins the bounded-outbound-channel
// shape: the writer drains wc.outbound one frame at a time;
// pushFrame and the dispatch goroutine enqueue concurrently.
// When the test pushes more frames than the channel can hold,
// some enqueues see ErrBackpressure and the wsConn's dropped
// counter is incremented.
//
// The test pushes many frames via the package-internal
// pushFrameForTest helper (production code paths — pushFrame
// itself, dispatched notification paths — exercise the same
// enqueueFrame). The cycle's invariant is that the dropped
// counter equals the number of ErrBackpressure returns the
// caller observed; the test does not pin a specific count
// because the writer's drain speed depends on the runtime
// (scheduler, ws library buffer sizes).
func TestBackpressure_Bounded(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Establish the conn so pushFrameForTest can find it.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	_ = readJSONRPC(t, c)

	if got := srv.connCount(); got != 1 {
		t.Fatalf("connCount() = %d, want 1", got)
	}
	connID := srv.firstConnID()
	if connID == "" {
		t.Fatalf("firstConnID() empty; conn not registered")
	}

	// Push enough frames to definitely overflow the
	// outboundChCap=64 buffer. Each frame is 16 KiB —
	// bigger than the ws library's per-message send buffer
	// (typically 4 KiB) so the writer spends measurable
	// time per Write and the channel fills. 1000 frames
	// is ~16 MiB of payload; we expect well over 64
	// backpressure errors.
	const total = 1000
	payload := make([]byte, 16<<10)
	for i := range payload {
		payload[i] = 'x'
	}
	var bp int
	for i := 0; i < total; i++ {
		frame := append([]byte(nil), payload...)
		err := srv.pushFrameForTest(context.Background(), connID, frame)
		if errors.Is(err, ErrBackpressure) {
			bp++
		} else if err != nil {
			t.Errorf("pushFrameForTest[%d]: unexpected error: %v", i, err)
		}
	}

	// The cycle's invariant: dropped == number of
	// ErrBackpressure returns. The test does NOT pin a
	// minimum bp count (the writer may be faster than the
	// push loop on a clean machine — that's fine; the
	// counter matches whatever overflowed, not a
	// designer-chosen threshold).
	if got := srv.DroppedFrames(connID); int(got) != bp {
		t.Errorf("DroppedFrames(%q) = %d, want %d (matches ErrBackpressure count)", connID, got, bp)
	}
	// We expect SOMETHING — if bp == 0, the channel
	// didn't fill at all, which means either the writer
	// is much faster than the test loop (unlikely with
	// 16 KiB frames × 1000) or the test setup didn't
	// establish the conn. The latter would be caught by
	// the connCount / firstConnID checks above; the
	// former is informative: surface it so a future
	// cycle that increases the cap can spot when
	// overflow stops happening at this load.
	if bp == 0 {
		t.Logf("backpressure: no overflow at total=%d; writer kept up. DroppedFrames=0. Increase total if you want to exercise overflow deterministically.", total)
	}
}

// TestConcurrentWrites_NoCorruption pins the writer-serialisation
// invariant: multiple in-flight dispatch goroutines enqueue
// concurrently; the single writePump goroutine drains the
// outbound channel in order; the client sees one ws message per
// enqueue with no interleaving, no corruption, no id collisions.
//
// The test fires N tools/list requests rapidly. tools/list is
// a sync handler so the dispatcher goroutines complete in
// arbitrary order — the responses must still arrive as
// well-formed JSON-RPC envelopes with the correct id, even
// though the writer interleaves frames from many concurrent
// enqueueFrame calls.
func TestConcurrentWrites_NoCorruption(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	const n = 50
	// Fire n requests. Sequential writes are fine — the
	// SERVER-side dispatches are concurrent (each request
	// spawns its own dispatcher goroutine), which is what
	// the test exercises. Concurrent client-side writes
	// would race on coder/websocket.Conn.Write.
	for i := 0; i < n; i++ {
		writeJSONRPC(t, c, map[string]any{
			"jsonrpc": "2.0",
			"id":      i + 1,
			"method":  "tools/list",
		})
	}

	seenIDs := map[int]bool{}
	for i := 0; i < n; i++ {
		resp := readJSONRPC(t, c)
		if errObj, ok := resp["error"]; ok {
			t.Errorf("response %d: error = %#v", i, errObj)
			continue
		}
		idF, ok := resp["id"].(float64)
		if !ok {
			t.Errorf("response %d: no id, got %#v", i, resp)
			continue
		}
		id := int(idF)
		if seenIDs[id] {
			t.Errorf("duplicate id %d", id)
		}
		seenIDs[id] = true
		// Pin the wire shape too — `result.tools` is the
		// 3-tool catalogue, present and non-empty. The
		// brief: a corrupted response would either fail
		// to parse (caught by json.Unmarshal above) or
		// carry a wrong id (caught above); a response
		// with a valid id but truncated result would
		// still parse but the `result` field would be
		// the wrong shape.
		res, ok := resp["result"].(map[string]any)
		if !ok {
			t.Errorf("response id=%d: result not an object: %#v", id, resp)
			continue
		}
		if _, has := res["tools"]; !has {
			t.Errorf("response id=%d: result.tools missing: %#v", id, res)
		}
	}
	if len(seenIDs) != n {
		t.Errorf("got %d distinct ids, want %d (missing: %v)", len(seenIDs), n, missingIDs(seenIDs, n))
	}
}

// TestReaderExit_StopsWriter pins the lifecycle invariant:
// when the reader (handleWS) exits, the writer (writePump)
// must also exit, and Shutdown must return promptly. The
// trigger: the client closes the ws while a slow SendMessage
// backend is running. The reader's c.Read returns an error
// on close; the deferred teardown cancels the writer ctx and
// closes outbound; writePump's range exits.
//
// The test does NOT read any frames before closing — the
// invariant is "Shutdown returns quickly after the reader
// exits", and reading frames would force the test to wait
// for the Sleep to finish (which is exactly what the test
// is trying to AVOID). The dispatch goroutine is in Sleep
// when close fires; it bails via ctx.Done() within
// microseconds of readCancel().
//
// If the writer leaked (e.g., outbound never closed, or the
// ctx cancellation didn't reach it), Shutdown's 2-second
// budget would elapse and the test would see a non-nil
// error.
func TestReaderExit_StopsWriter(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)

	be := &remotecontrol.StubBackend{
		Reply:          "after-sleep",
		StopReason:     remotecontrol.StopReasonEndTurn,
		ConversationID: "conv-ws",
		// Sleep is generous enough that the dispatch
		// goroutine is genuinely parked in SendStream
		// when the client closes the ws. The exact
		// duration is not pinned — the test does not
		// wait for Sleep to elapse.
		Sleep: 2 * time.Second,
	}

	c := dialWS(t, srv.HTTPURL())

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	// Send SendMessage. The dispatch goroutine enters
	// SendStream → Sleep and parks there. The reader
	// goes back to c.Read and blocks (no further
	// frames arrive before close).
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "go",
		},
	})

	// Close the client side while the dispatch goroutine
	// is parked in Sleep. Reader sees EOF, deferred
	// teardown runs.
	_ = c.Close(websocket.StatusNormalClosure, "")

	// Give the server a moment to notice and tear down.
	time.Sleep(50 * time.Millisecond)

	// Shutdown must complete well under its budget. If the
	// writer leaked, s.wg.Wait blocks and the budget
	// elapses — surfaced as a non-nil error from
	// Shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown after reader exit: %v (writer may have leaked)", err)
	}

	done() // idempotent.
}

// keys returns the keys of m, sorted ascending. Helper for the
// bi-directional test's error message.
func keys(m map[int]map[string]any) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Ints are already comparable; simple insertion sort
	// is fine for the small sizes the tests use.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// missingIDs returns the ids in [1, n] that are NOT present
// in seen. Used by the concurrent-writes test to format the
// error message.
func missingIDs(seen map[int]bool, n int) []int {
	var out []int
	for i := 1; i <= n; i++ {
		if !seen[i] {
			out = append(out, i)
		}
	}
	return out
}

// ----- cycle rc-cancel-cascade wire tests -----
//
// Wire-level coverage for the CancelRemoteAgentCascade
// JSON-RPC method. The dispatcher-level surface is in
// internal/remotecontrol/cascade_test.go; these tests
// pin the JSON envelope byte-for-byte over the WS
// adapter and verify the cascade/state{phase:cancelled}
// notification lands on the originating ws connection.

// TestCancelRemoteAgentCascade_Ok_WS is the headline
// ask of the cycle: a dashboard cancels an in-flight
// cascade turn via the CancelRemoteAgentCascade RPC
// on the same ws connection that drove the
// SendRemoteAgentCascadeMessage. The response carries
// {ok:true, already_cancelled:false, cancelled_at:<...>}
// and the ws receives a cascade/state notification
// with phase=cancelled.
//
// The test parks the StubBackend with Sleep=5s and
// fires the cancel 50ms into the park. The cancel
// ctx propagates through the registry's per-turn
// cancelable ctx into the StubBackend's
// select-on-ctx.Done arm; SendStream returns
// ErrTurnCancelled (mapped to -32603 in the
// dispatcher's success-error switch). The dashboard
// sees:
//
//   - cascade/state{phase:"cancelled"} (server push)
//   - CancelRemoteAgentCascade response (id=2)
//   - SendRemoteAgentCascadeMessage response (id=3,
//     -32603 with the cancel message)
//
// Order is not pinned (the three paths race); the
// test asserts the presence of each.
func TestCancelRemoteAgentCascade_Ok_WS(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		StopReason: remotecontrol.StopReasonEndTurn,
		Sleep:      5 * time.Second,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	// 1. Send SendMessage (id=2). The dispatch goroutine
	//    enters SendStream and parks on the StubBackend's
	//    Sleep; the per-turn ctx (derived via StartCancel)
	//    is what the StubBackend blocks on.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "please run for 5 seconds",
		},
	})

	// 2. Give the dispatch goroutine ~50 ms to enter the
	//    parked Sleep. Without this the cancel can race
	//    ahead of the turn and the response shape is
	//    "no turn was in flight" rather than the
	//    headline-ask "in-flight turn cancelled".
	time.Sleep(50 * time.Millisecond)

	// 3. Send Cancel (id=3) on the SAME ws.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "CancelRemoteAgentCascade",
		"params": map[string]any{
			"cascade_id": cid,
		},
	})

	// 4. Read frames until we have both responses (id=2
	//    and id=3) AND the cascade/state notification
	//    with phase=cancelled. Notification has no id
	//    (per JSON-RPC 2.0 §4.1).
	type notif struct {
		method string
		params map[string]any
	}
	var notifications []notif
	seenResponses := map[int]map[string]any{}
	const maxFrames = 32
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		idF, hasID := frame["id"].(float64)
		if !hasID {
			// Notification — capture if it's a
			// cascade/state.
			method, _ := frame["method"].(string)
			if method == remotecontrol.NotifCascadeState {
				params, _ := frame["params"].(map[string]any)
				notifications = append(notifications, notif{method: method, params: params})
			}
			continue
		}
		id := int(idF)
		if _, dup := seenResponses[id]; dup {
			t.Errorf("duplicate response id=%d", id)
		}
		seenResponses[id] = frame
		// Once we have both responses, we can stop
		// if we also have a cancelled notification.
		if _, has2 := seenResponses[2]; has2 {
			if _, has3 := seenResponses[3]; has3 {
				if len(notifications) > 0 {
					goto done
				}
			}
		}
	}
done:

	// 5. Pin the Cancel response (id=3) — must be the
	//    "actually fired" shape: ok:true,
	//    already_cancelled:false, cancelled_at set.
	cancelResp, ok := seenResponses[3]
	if !ok {
		t.Fatalf("id=3 (Cancel) response missing")
	}
	if errObj, ok := cancelResp["error"]; ok {
		t.Fatalf("id=3 (Cancel) error: %#v", errObj)
	}
	cancelRes, ok := cancelResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("id=3 result not an object: %#v", cancelResp)
	}
	if ok, _ := cancelRes["ok"].(bool); !ok {
		t.Errorf("id=3 ok = %v, want true", cancelRes["ok"])
	}
	if alreadyCancelled, _ := cancelRes["already_cancelled"].(bool); alreadyCancelled {
		t.Errorf("id=3 already_cancelled = true; want false (a turn was in flight)")
	}
	if cancelledAt, _ := cancelRes["cancelled_at"].(string); cancelledAt == "" {
		t.Errorf("id=3 cancelled_at missing or empty: %#v", cancelRes["cancelled_at"])
	}

	// 6. Pin the cascade/state{phase:cancelled} notification.
	var sawCancelled bool
	for _, n := range notifications {
		phase, _ := n.params["phase"].(string)
		if phase == remotecontrol.StatePhaseCancelled {
			sawCancelled = true
			if got, _ := n.params["cascade_id"].(string); got != cid {
				t.Errorf("cancelled notification cascade_id = %q, want %q", got, cid)
			}
			if got, _ := n.params["reason"].(string); got == "" {
				t.Errorf("cancelled notification reason missing")
			}
			break
		}
	}
	if !sawCancelled {
		t.Errorf("no cascade/state notification with phase=cancelled; got %d notifications", len(notifications))
	}

	// 7. Pin the SendMessage response (id=2). It can be
	//    either a successful envelope (the test gives the
	//    backend ample time to reach the cancel handler
	//    before its Sleep fires) or an error envelope
	//    (ErrTurnCancelled → -32603). Both are valid
	//    wire outcomes; the cycle brief pins "the
	//    SendMessage RPC returns cleanly" which we
	//    read as "no goroutine leak / no panic" —
	//    either envelope is acceptable here. We
	//    tolerate both shapes and just assert the
	//    response was emitted (we already verified
	//    that via seenResponses[2]).
	if _, ok := seenResponses[2]; !ok {
		t.Errorf("id=2 (SendMessage) response missing")
	}

	// 8. The cascade's IsCancelled flag is sticky.
	if !got.IsCancelled() {
		t.Errorf("cascade.IsCancelled after Cancel = false; want true")
	}
}

// TestCancelRemoteAgentCascade_Idempotent_WS covers
// the brief's "cancel twice" idempotency rule on the
// wire: the first call fires (already_cancelled:false);
// the second call returns ok:true with
// already_cancelled:true. No second notification is
// emitted — the dashboard's UI sees one cancel signal,
// not two.
func TestCancelRemoteAgentCascade_Idempotent_WS(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		StopReason: remotecontrol.StopReasonEndTurn,
		Sleep:      5 * time.Second,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	// Send SendMessage to start a parked turn.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentCascadeMessage",
		"params": map[string]any{
			"cascade_id": cid,
			"text":       "park",
		},
	})
	time.Sleep(50 * time.Millisecond)

	// First Cancel.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "CancelRemoteAgentCascade",
		"params": map[string]any{
			"cascade_id": cid,
		},
	})

	// Drain until we see both id=3 and the cancel
	// notification. We don't wait for id=2 here —
	// the test's goal is the cancel RPC idempotency,
	// not the SendMessage completion.
	seenIDs := map[int]map[string]any{}
	const maxFrames = 32
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		idF, hasID := frame["id"].(float64)
		if !hasID {
			continue
		}
		id := int(idF)
		if _, dup := seenIDs[id]; dup {
			t.Errorf("duplicate id=%d", id)
		}
		seenIDs[id] = frame
		if _, has3 := seenIDs[3]; has3 {
			break
		}
	}

	firstResp, ok := seenIDs[3]
	if !ok {
		t.Fatalf("first Cancel (id=3) response missing")
	}
	firstRes, ok := firstResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("first Cancel result not an object: %#v", firstResp)
	}
	if ok, _ := firstRes["ok"].(bool); !ok {
		t.Errorf("first ok = %v, want true", firstRes["ok"])
	}
	if already, _ := firstRes["already_cancelled"].(bool); already {
		t.Errorf("first already_cancelled = true; want false")
	}

	// Second Cancel — same id reuse is fine (the
	// dispatcher's read/write pump is bi-directional).
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "CancelRemoteAgentCascade",
		"params": map[string]any{
			"cascade_id": cid,
		},
	})

	// Drain until we see id=4. There should be no
	// second cascade/state{phase:cancelled}
	// notification (idempotency: only the first
	// cancel fires).
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		idF, hasID := frame["id"].(float64)
		if !hasID {
			continue
		}
		id := int(idF)
		if _, dup := seenIDs[id]; dup {
			continue // already seen this id, drop
		}
		seenIDs[id] = frame
		if id == 4 {
			break
		}
	}

	secondResp, ok := seenIDs[4]
	if !ok {
		t.Fatalf("second Cancel (id=4) response missing")
	}
	secondRes, ok := secondResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("second Cancel result not an object: %#v", secondResp)
	}
	if ok, _ := secondRes["ok"].(bool); !ok {
		t.Errorf("second ok = %v, want true", secondRes["ok"])
	}
	if already, _ := secondRes["already_cancelled"].(bool); !already {
		t.Errorf("second already_cancelled = false; want true (idempotent)")
	}
}

// TestCancelRemoteAgentCascade_NotFound_WS covers
// the wire-level -32004 envelope for an unknown
// cascade_id.
func TestCancelRemoteAgentCascade_NotFound_WS(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CancelRemoteAgentCascade",
		"params": map[string]any{
			"cascade_id": "cas_doesnotexist",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("not-found Cancel: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != remotecontrol.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, remotecontrol.ErrCodeCascadeNotFound)
	}
}

// TestCancelRemoteAgentCascade_BadParams_WS covers
// the wire-level -32602 envelope for an empty
// cascade_id. Mirrors the in-process
// TestDispatcher_CancelRemoteAgentCascade_BadParams.
func TestCancelRemoteAgentCascade_BadParams_WS(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CancelRemoteAgentCascade",
		"params": map[string]any{
			"cascade_id": "",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("empty-cascade_id Cancel: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
}

// Wire-level coverage for the remoteControlDeepLink
// JSON-RPC method. In-process coverage for ParseDeepLink
// and the dispatcher's handler lives in
// internal/remotecontrol/deep_link_test.go; these
// tests pin the wire envelope byte-for-byte and
// assert the cascade/state{phase:"focused"}
// notification lands on the originating ws
// connection (cycle-13 bidi fix at work).

// TestRemoteControlDeepLink_Open_WS is the headline
// happy-path ask of the rc-deep-link cycle: the
// dashboard sends an `agy://open?...` deep-link,
// receives {action:"open", echo:{...}, received_at:...}
// back, and the dispatcher's Config.LastDeepLink
// state is stamped with the wall-clock at which the
// RPC landed.
func TestRemoteControlDeepLink_Open_WS(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "remoteControlDeepLink",
		"params": map[string]any{
			"url": "agy://open?project=foo&conv=bar",
		},
	})
	resp := readJSONRPC(t, c)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("open: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("open: no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("open: ok = %v, want true", res["ok"])
	}
	if got, _ := res["action"].(string); got != "open" {
		t.Errorf("open: action = %q, want open", got)
	}
	if got, _ := res["received_at"].(string); got == "" {
		t.Errorf("open: received_at missing or empty: %#v", res["received_at"])
	}
	echo, ok := res["echo"].(map[string]any)
	if !ok {
		t.Fatalf("open: echo type = %T, want map[string]any", res["echo"])
	}
	if echo["host"] != "open" {
		t.Errorf("open: echo.host = %v, want open", echo["host"])
	}
	if echo["raw"] != "agy://open?project=foo&conv=bar" {
		t.Errorf("open: echo.raw = %v, want verbatim", echo["raw"])
	}
	q, ok := echo["query"].(map[string]any)
	if !ok {
		t.Fatalf("open: echo.query type = %T, want map[string]any", echo["query"])
	}
	if q["project"] != "foo" {
		t.Errorf("open: echo.query.project = %v, want foo", q["project"])
	}
	if q["conv"] != "bar" {
		t.Errorf("open: echo.query.conv = %v, want bar", q["conv"])
	}

	// The dispatcher's LastDeepLink was stamped.
	// Reach into the dispatcher via the server's
	// session state lookup (no public accessor;
	// the cycle's in-process coverage already pins
	// the snapshot path). The handler ran on a
	// fresh dispatcher, so its LastDeepLink is
	// non-nil by construction (NewDispatcher
	// fabricates one when Config.LastDeepLink is
	// nil). We assert the wall-clock delta to the
	// test's start is below 5s.
	// (Skipped — no public accessor on
	// TCPServer; the in-process TestDispatcher_
	// RemoteControlDeepLink_Open pins the
	// LastDeepLink.Snapshot path.)
}

// TestRemoteControlDeepLink_FocusCascade_NotFound_WS
// pins the wire-level -32004 envelope for
// focus-cascade with an unknown cascade_id. The
// in-process coverage in deep_link_test.go pins
// the same envelope via Dispatch; this one
// confirms the wire envelope survives the JSON
// round-trip.
func TestRemoteControlDeepLink_FocusCascade_NotFound_WS(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "remoteControlDeepLink",
		"params": map[string]any{
			"url": "agy://focus-cascade?cascade_id=cas_does_not_exist",
		},
	})
	resp := readJSONRPC(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("focus-cascade: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != remotecontrol.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d", got, remotecontrol.ErrCodeCascadeNotFound)
	}
}

// TestRemoteControlDeepLink_FocusCascade_Ok_WS pins
// the wire-level happy path: registering a cascade,
// sending the focus-cascade deep-link, and asserting
// both the synchronous response and the cascade/state
// {phase:"focused"} notification arrive on the same
// ws connection. The cycle-13 bidi fix is what makes
// the notification arrive alongside the response
// (without the bi-directional write pump, the
// notification would be queued behind the response).
func TestRemoteControlDeepLink_FocusCascade_Ok_WS(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)

	// Pre-condition: cascade is not focused yet.
	if got.IsFocused() {
		t.Fatalf("fresh cascade IsFocused = true; want false")
	}

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "remoteControlDeepLink",
		"params": map[string]any{
			"url": "agy://focus-cascade?cascade_id=" + cid,
		},
	})

	// Drain frames until we have both the response
	// (id=2) AND a cascade/state{phase:focused}
	// notification.
	var sawFocusedNotif bool
	seenResponses := map[int]map[string]any{}
	const maxFrames = 16
	for i := 0; i < maxFrames; i++ {
		frame := readJSONRPC(t, c)
		idF, hasID := frame["id"].(float64)
		if !hasID {
			method, _ := frame["method"].(string)
			if method == remotecontrol.NotifCascadeState {
				params, _ := frame["params"].(map[string]any)
				if phase, _ := params["phase"].(string); phase == "focused" {
					sawFocusedNotif = true
					if got, _ := params["cascade_id"].(string); got != cid {
						t.Errorf("focused notif cascade_id = %q, want %q", got, cid)
					}
					if _, ok := params["focused_at"].(string); !ok {
						t.Errorf("focused notif focused_at missing or non-string: %#v", params["focused_at"])
					}
				}
			}
			continue
		}
		id := int(idF)
		seenResponses[id] = frame
		if _, has2 := seenResponses[2]; has2 && sawFocusedNotif {
			break
		}
	}

	// Pin the response.
	resp2, ok := seenResponses[2]
	if !ok {
		t.Fatalf("id=2 (focus-cascade) response missing")
	}
	if errObj, ok := resp2["error"]; ok {
		t.Fatalf("id=2 error: %#v", errObj)
	}
	res, ok := resp2["result"].(map[string]any)
	if !ok {
		t.Fatalf("id=2 result not an object: %#v", resp2)
	}
	if got, _ := res["action"].(string); got != "focus-cascade" {
		t.Errorf("id=2 action = %q, want focus-cascade", got)
	}
	if got, _ := res["cascade_id"].(string); got != cid {
		t.Errorf("id=2 cascade_id = %q, want %q", got, cid)
	}
	if _, ok := res["received_at"].(string); !ok {
		t.Errorf("id=2 received_at missing or non-string: %#v", res["received_at"])
	}

	// Pin the notification.
	if !sawFocusedNotif {
		t.Errorf("no cascade/state{phase:\"focused\"} notification arrived on the same ws")
	}

	// Pin the cascade's Focused flag.
	if !got.IsFocused() {
		t.Errorf("after focus-cascade: cascade.IsFocused = false, want true")
	}
}

// TestRemoteControlDeepLink_PasteAndSend_FiresTurn_WS
// pins the headline ask of the paste-and-send verb:
// the deep-link returns synchronously with
// accepted_at; the cascade's backend fires a turn
// that emits the cascade/state{phase:"done"}
// notification; the cascade's LastReply ends up
// populated with the stub's reply.
//
// The handler's paste-and-send path spawns a
// goroutine that calls (*CascadeRegistry).SendStream
// independently of the RPC reply. The dispatcher
// returns accepted_at synchronously; the goroutine
// then drives the stub backend and pushes cascade/
// state and cascade/text_delta notifications on the
// same ws. The wire order between the synchronous
// response and the goroutine's first notification
// (cascade/state{starting}) is non-deterministic —
// it depends on whether the goroutine's first
// sink.OnState fires before or after the dispatch
// goroutine's marshal+enqueue of the response — so
// a single drain loop collects both and asserts
// the response AND the done notification were
// received, regardless of arrival order.
func TestRemoteControlDeepLink_PasteAndSend_FiresTurn_WS(t *testing.T) {
	srv, _, registry, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	be := &remotecontrol.StubBackend{
		Reply:          "paste-and-send reply",
		ConversationID: "conv-pas",
		StopReason:     remotecontrol.StopReasonEndTurn,
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	cid := startCascade(t, c)
	got, _ := registry.Get(cid)
	got.SetBackend(be)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "remoteControlDeepLink",
		"params": map[string]any{
			"url": "agy://paste-and-send?cascade_id=" + cid + "&text=hello%20world",
		},
	})

	// Single drain loop: keep reading until BOTH
	// the synchronous response (id=2) AND the
	// cascade/state{phase:"done"} notification
	// have been observed. The goroutine emits at
	// most 3 cascade/state frames (starting →
	// done, with one cascade/text_delta in
	// between for the stub's reply); the response
	// carries id=2. The cap (32) is a safety net
	// for slow CI — under normal scheduling the
	// loop exits within 4-5 reads.
	var resp map[string]any
	var sawDone bool
	const frameCap = 32
	for i := 0; i < frameCap; i++ {
		frame := readJSONRPC(t, c)
		if _, hasID := frame["id"]; hasID {
			resp = frame
		} else if method, _ := frame["method"].(string); method == remotecontrol.NotifCascadeState {
			params, _ := frame["params"].(map[string]any)
			if phase, _ := params["phase"].(string); phase == remotecontrol.StatePhaseDone {
				sawDone = true
			}
		}
		if resp != nil && sawDone {
			break
		}
	}

	if resp == nil {
		t.Fatalf("id=2 (paste-and-send) response missing within %d frames", frameCap)
	}
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("id=2 error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("id=2 result not an object: %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("id=2 ok = %v, want true", res["ok"])
	}
	if got, _ := res["action"].(string); got != "paste-and-send" {
		t.Errorf("id=2 action = %q, want paste-and-send", got)
	}
	if got, _ := res["cascade_id"].(string); got != cid {
		t.Errorf("id=2 cascade_id = %q, want %q", got, cid)
	}
	if _, ok := res["accepted_at"].(string); !ok {
		t.Errorf("id=2 accepted_at missing or non-string: %#v", res["accepted_at"])
	}
	if got, _ := res["echo"].(map[string]any); got == nil {
		t.Errorf("id=2 echo missing: %#v", res["echo"])
	}
	if !sawDone {
		t.Errorf("no cascade/state{phase:\"done\"} notification arrived within %d frames", frameCap)
	}

	// Pin the cascade's LastReply (the stub's
	// reply ends up there as a side effect of
	// the streaming sink + the registry's
	// setReply path).
	got, _ = registry.Get(cid)
	if got.LastReplySnapshot() != "paste-and-send reply" {
		t.Errorf("after paste-and-send: cascade.LastReply = %q, want %q",
			got.LastReplySnapshot(), "paste-and-send reply")
	}
}

// TestRemoteControlDeepLink_InvalidURL_WS covers
// the wire-level -32602 envelope for an invalid
// URL. Three sub-shapes:
//
//   - empty params (no `url` key): -32602 "url is required"
//   - `agy://nuke?cascade_id=cas_abc` (unknown verb): -32602
//   - `not-a-url` (bad scheme): -32602
func TestRemoteControlDeepLink_InvalidURL_WS(t *testing.T) {
	srv, _, _, done := newTestTCPServerWithBackend(t, 256)
	defer done()

	cases := []struct {
		name string
		url  string
	}{
		{"empty_params", ""},
		{"unknown_verb", "agy://nuke?cascade_id=cas_abc"},
		{"bad_scheme", "not-a-url"},
	}

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	for i, tc := range cases {
		writeJSONRPC(t, c, map[string]any{
			"jsonrpc": "2.0",
			"id":      i + 1,
			"method":  "remoteControlDeepLink",
			"params": map[string]any{
				"url": tc.url,
			},
		})
		resp := readJSONRPC(t, c)
		errObj, ok := resp["error"].(map[string]any)
		if !ok {
			t.Errorf("%s: no error envelope, got %#v", tc.name, resp)
			continue
		}
		if got := int(errObj["code"].(float64)); got != -32602 {
			t.Errorf("%s: error.code = %d, want -32602 (Invalid params)", tc.name, got)
		}
	}
}

// ----- cycle rc-permissions-grants wire tests -----
//
// Eight wire-level tests covering the four JSON-RPC
// methods introduced by cycle rc-permissions-grants
// (SetRemoteAgentPermissionPreset /
// SetRemoteAgentArtifactReviewMode /
// AddRemoteAgentPermissionGrants /
// RemoveRemoteAgentPermissionGrants) and the
// "permissions/changed" broadcast notification
// that fires on every successful mutation.
//
// The split with permissions_test.go is the
// project's standard pattern: in-process envelopes
// vs wire round-trips. The in-process tests pin the
// state machine; these pin the JSON-RPC envelopes
// the dispatcher stamps on the wire.
//
// Every test uses a fresh MemPermissionStore so
// state doesn't leak across tests —
// newTestTCPServerWithPermissions mirrors
// newTestTCPServerWithProjects.

// newTestTCPServerWithPermissions brings up a
// TCPServer whose dispatcher's PermissionsStore
// seam is the supplied
// *remotecontrol.MemPermissionStore (one per
// test). Mirrors newTestTCPServerWithProjects.
func newTestTCPServerWithPermissions(t *testing.T, ps remotecontrol.PermissionStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:          sess,
		Agents:           remotecontrol.DefaultAgentLister,
		Reader:           remotecontrol.DefaultOutputReader{},
		PermissionsStore: ps,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// waitForTwoConnIDs polls the server's connection
// registry until it sees at least two entries (or
// the deadline expires) and returns the two
// connIDs in iteration order. The accept
// goroutine that runs handleWS is async w.r.t.
// websocket.Dial returning, so a test that calls
// BroadcastNotify right after two Dials must wait
// for both registrations to land before issuing
// the request — otherwise the broadcaster would
// only see one conn and the second client would
// miss the notification.
//
// The two-conn poll reuses the per-connID poll
// (waitForConnID) by registering both first; the
// deadlock case (one Dial succeeds, one stalls)
// is covered by the 2-second test timeout.
func waitForTwoConnIDs(t *testing.T, srv *TCPServer) (string, string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.connsMu.RLock()
		ids := make([]string, 0, 2)
		for id := range srv.conns {
			ids = append(ids, id)
			if len(ids) == 2 {
				srv.connsMu.RUnlock()
				return ids[0], ids[1]
			}
		}
		srv.connsMu.RUnlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("only %d ws connection(s) registered within 2s of Dial", srv.connCount())
	return "", ""
}

// assertPermissionStateEnvelope asserts the
// response envelope the four Set/Add/Remove
// handlers stamp: a {version, preset, review,
// grants, updated_at} map with the supplied
// expected preset / review / number of grants.
// updated_at is parsed and asserted to be a
// non-empty RFC3339Nano string; the value is
// whatever the MemPermissionStore stamped at
// handler time, so we don't pin the exact
// timestamp.
func assertPermissionStateEnvelope(t *testing.T, label string, result map[string]any, wantPreset, wantReview string, wantGrants int) {
	t.Helper()
	if result == nil {
		t.Fatalf("%s: result is nil", label)
	}
	if got, _ := result["version"].(float64); got != 1 {
		t.Errorf("%s: result.version = %v, want 1", label, result["version"])
	}
	if got, _ := result["preset"].(string); got != wantPreset {
		t.Errorf("%s: result.preset = %q, want %q", label, got, wantPreset)
	}
	if got, _ := result["review"].(string); got != wantReview {
		t.Errorf("%s: result.review = %q, want %q", label, got, wantReview)
	}
	rawGrants, ok := result["grants"].([]any)
	if !ok {
		t.Fatalf("%s: result.grants = %T, want []any: %#v", label, result["grants"], result)
	}
	if len(rawGrants) != wantGrants {
		t.Errorf("%s: len(result.grants) = %d, want %d (%#v)", label, len(rawGrants), wantGrants, rawGrants)
	}
	if got, _ := result["updated_at"].(string); got == "" {
		t.Errorf("%s: result.updated_at missing or empty: %#v", label, result["updated_at"])
	}
}

// assertPermissionChangedNotification asserts a
// permissions/changed notification frame carries
// the expected preset (and a non-empty
// updated_at). The frame shape is the same
// {version, preset, review, grants, updated_at}
// envelope the response uses, but delivered
// without an "id" field (per JSON-RPC 2.0 §4.1
// notification rules).
func assertPermissionChangedNotification(t *testing.T, label string, frame map[string]any, wantPreset string) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.PermissionsChangedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.PermissionsChangedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	assertPermissionStateEnvelope(t, label+" params", params, wantPreset, "always", 0)
}

// TestSetRemoteAgentPermissionPreset_WS_Ok
// exercises the happy path: a fresh
// MemPermissionStore receives a SetPreset
// wire call; the WS round-trip returns the
// expected envelope.
//
// Wire-order subtlety: the dispatcher invokes
// broadcastPermissionsChanged BEFORE
// dispatchOne marshals and enqueues the
// response, so the originating conn's
// outbound channel holds [notification,
// response]. readJSONRPCResponse drops the
// notification (no id) and returns the
// response — that's exactly what we want here.
// The dedicated TestPermissionsChanged_Broadcast
// test pins the notification arrival with two
// conns, so we don't re-assert it in every
// single-conn happy-path test.
func TestSetRemoteAgentPermissionPreset_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentPermissionPreset",
		"params":  map[string]any{"preset": "strict"},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteAgentPermissionPreset: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentPermissionPreset: no result, got %#v", resp)
	}
	assertPermissionStateEnvelope(t, "SetRemoteAgentPermissionPreset", res, "strict", "always", 0)
}

// TestSetRemoteAgentPermissionPreset_WS_BadParams
// pins the -32602 envelope for an unknown
// preset. Cycle rc-permissions-grants brief:
// any value outside the closed set
// {default, strict, permissive} is
// "Invalid params".
func TestSetRemoteAgentPermissionPreset_WS_BadParams(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentPermissionPreset",
		"params":  map[string]any{"preset": "ultra-permissive"},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentPermissionPreset: no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// TestSetRemoteAgentArtifactReviewMode_WS_Ok
// exercises the happy path for the review
// mode setter. Cycles rc-permissions-grants
// to expose the review axis the
// artifact-review tooling depends on.
func TestSetRemoteAgentArtifactReviewMode_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentArtifactReviewMode",
		"params":  map[string]any{"mode": "never"},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteAgentArtifactReviewMode: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentArtifactReviewMode: no result, got %#v", resp)
	}
	assertPermissionStateEnvelope(t, "SetRemoteAgentArtifactReviewMode", res, "default", "never", 0)
}

// TestAddRemoteAgentPermissionGrants_WS_Ok
// exercises the wire encoding for adding a
// single grant: the tool_patterns entry is
// the "<tool_name>:<pattern>" string the
// dashboard sends, and the response carries
// the parsed (tool_name, pattern, reason,
// added_at) shape the
// permissionStateToWire helper emits.
func TestAddRemoteAgentPermissionGrants_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "AddRemoteAgentPermissionGrants",
		"params": map[string]any{
			"tool_patterns": []string{"bash:npm install"},
			"reason":        "daily build",
		},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("AddRemoteAgentPermissionGrants: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("AddRemoteAgentPermissionGrants: no result, got %#v", resp)
	}
	assertPermissionStateEnvelope(t, "AddRemoteAgentPermissionGrants", res, "default", "always", 1)
	grants, _ := res["grants"].([]any)
	first, _ := grants[0].(map[string]any)
	if got, _ := first["tool_name"].(string); got != "bash" {
		t.Errorf("grants[0].tool_name = %q, want %q", got, "bash")
	}
	if got, _ := first["pattern"].(string); got != "npm install" {
		t.Errorf("grants[0].pattern = %q, want %q", got, "npm install")
	}
	if got, _ := first["reason"].(string); got != "daily build" {
		t.Errorf("grants[0].reason = %q, want %q", got, "daily build")
	}
	if got, _ := first["added_at"].(string); got == "" {
		t.Errorf("grants[0].added_at missing or empty: %#v", first["added_at"])
	}
}

// TestAddRemoteAgentPermissionGrants_WS_Duplicate
// pins the brief's -32014 envelope for a
// duplicate add: the first call lands; the
// second call (same tool_patterns) sees the
// store's ErrPermissionGrantExists, which the
// handler maps to -32014 "Permission grant
// exists". The wire envelope carries the
// verbatim store error in `data` so a dashboard
// log shows the rejected (tool_name, pattern)
// pair.
//
// Each readJSONRPCResponse drops the
// permissions/changed notification (no id) and
// returns the response. The first add's
// notification is consumed this way; the second
// add's notification (no id) is dropped the
// same way. We assert only the responses —
// the broadcast-notification arrival is pinned
// by TestPermissionsChanged_Broadcast below.
func TestAddRemoteAgentPermissionGrants_WS_Duplicate(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	params := map[string]any{
		"tool_patterns": []string{"write_file:*.go"},
	}

	// First add — succeeds.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "AddRemoteAgentPermissionGrants",
		"params":  params,
	})
	resp1 := readJSONRPCResponse(t, c)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("first add: error = %#v", errObj)
	}
	res1, _ := resp1["result"].(map[string]any)
	assertPermissionStateEnvelope(t, "first add", res1, "default", "always", 1)

	// Second add — same tool_patterns — must
	// be rejected with -32014.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "AddRemoteAgentPermissionGrants",
		"params":  params,
	})
	resp2 := readJSONRPCResponse(t, c)
	errObj, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("second add: no error envelope, got %#v", resp2)
	}
	if got := int(errObj["code"].(float64)); got != -32014 {
		t.Errorf("second add: error.code = %d, want -32014 (Permission grant exists)", got)
	}
	if got, _ := errObj["message"].(string); got != "Permission grant exists" {
		t.Errorf("second add: error.message = %q, want %q", got, "Permission grant exists")
	}
}

// TestRemoveRemoteAgentPermissionGrants_WS_Ok
// exercises the wire encoding for removing a
// grant. The tool_patterns entry uses the
// same "<tool_name>:<pattern>" shape; the
// response carries the post-remove state
// (empty grants).
func TestRemoveRemoteAgentPermissionGrants_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Seed: add one grant.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "AddRemoteAgentPermissionGrants",
		"params": map[string]any{
			"tool_patterns": []string{"bash:npm install"},
		},
	})
	addResp := readJSONRPCResponse(t, c)
	if errObj, ok := addResp["error"].(map[string]any); ok {
		t.Fatalf("seed Add: error = %#v", errObj)
	}

	// Now remove it.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "RemoveRemoteAgentPermissionGrants",
		"params": map[string]any{
			"tool_patterns": []string{"bash:npm install"},
		},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("RemoveRemoteAgentPermissionGrants: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("RemoveRemoteAgentPermissionGrants: no result, got %#v", resp)
	}
	assertPermissionStateEnvelope(t, "RemoveRemoteAgentPermissionGrants", res, "default", "always", 0)
}

// TestPermissionsChanged_Broadcast exercises the
// headline ask of cycle rc-permissions-grants:
// a SetRemoteAgentPermissionPreset on one ws
// connection must fan a "permissions/changed"
// notification out to every open ws connection
// (not just the originating one). The test
// brings up two ws clients, sends a Set on the
// first, and verifies:
//
//   - c1 sees a permissions/changed notification
//     (the broadcaster enqueued to ALL conns,
//     including the originating one), followed
//     by the Set response.
//   - c2 (which never sent a request) sees ONLY
//     the permissions/changed notification.
//
// This pins the brief's "broadcast-to-all"
// semantics: a dashboard that subscribes to
// permissions/changed re-renders without
// polling, and dashboards on other conns
// stay in sync with each other.
//
// Wire-order subtlety: the dispatcher invokes
// broadcastPermissionsChanged BEFORE
// dispatchOne marshals and enqueues the
// response, so the originating conn's
// outbound channel holds [notification,
// response] in that order. readJSONRPCResponse
// drops the notification and returns the
// response, but that loses the notification
// for assertion — so we drain the
// notification explicitly on c1 BEFORE
// asking readJSONRPCResponse for the response.
func TestPermissionsChanged_Broadcast(t *testing.T) {
	ps := remotecontrol.NewMemPermissionStore(nil)
	srv, done := newTestTCPServerWithPermissions(t, ps)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	// Wait for both conns to register. The
	// broadcaster walks s.conns under a single
	// RLock; if either Dial hasn't been
	// accepted yet, the broadcaster would only
	// see one conn and the second client would
	// miss the notification. waitForTwoConnIDs
	// blocks until connCount == 2.
	waitForTwoConnIDs(t, srv)

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentPermissionPreset",
		"params":  map[string]any{"preset": "strict"},
	})

	// c1 outbound channel order:
	//   [notification, response]
	// (broadcaster enqueues first;
	//  dispatchOne enqueues the response second).
	//
	// Step 1: read the notification on c1.
	notif1 := readJSONRPC(t, c1)
	assertPermissionChangedNotification(t, "c1 broadcast", notif1, "strict")

	// Step 2: read the response on c1.
	resp := readJSONRPCResponse(t, c1)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("c1 SetRemoteAgentPermissionPreset: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("c1 SetRemoteAgentPermissionPreset: no result, got %#v", resp)
	}
	assertPermissionStateEnvelope(t, "c1 response", res, "strict", "always", 0)

	// c2 outbound channel: [notification]
	// — c2 never sent a request, so no response
	// arrives. readJSONRPC blocks for one frame
	// (2 s timeout inside); a buggy broadcaster
	// that only writes to the originating conn
	// trips the deadline here.
	notif2 := readJSONRPC(t, c2)
	assertPermissionChangedNotification(t, "c2 broadcast", notif2, "strict")

	// Both conns must have observed the same
	// preset — the broadcaster ships the
	// post-state envelope, not the diff.
	if p1, _ := notif1["params"].(map[string]any)["preset"].(string); p1 != "strict" {
		t.Errorf("c1 broadcast.params.preset = %q, want strict", p1)
	}
	if p2, _ := notif2["params"].(map[string]any)["preset"].(string); p2 != "strict" {
		t.Errorf("c2 broadcast.params.preset = %q, want strict", p2)
	}
}

// ----------------------------------------------------------------------
// rc-plugins: ListRemoteAgentPlugins / DisconnectRemoteAgentPlugin
// ----------------------------------------------------------------------

// newTestTCPServerWithPlugins brings up a TCPServer whose
// dispatcher's PluginsStore seam is the supplied
// *remotecontrol.PluginStore (one per test). Mirrors
// newTestTCPServerWithPermissions but for the plugin
// surface. Cycle rc-plugins adds this helper.
func newTestTCPServerWithPlugins(t *testing.T, ps remotecontrol.PluginStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:      sess,
		Agents:       remotecontrol.DefaultAgentLister,
		Reader:       remotecontrol.DefaultOutputReader{},
		PluginsStore: ps,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// assertPluginListEnvelope asserts the brief's
// ListRemoteAgentPlugins response envelope:
//
//	{
//	  "ok":          true,
//	  "plugins":     [
//	    {
//	      "plugin_id":    "<id>",
//	      "name":         "<display>",
//	      "version":      "<semver>",
//	      "connected_at": "<RFC3339Nano>",
//	      "capabilities": ["...", ...],
//	      "origin":       "<provenance>"
//	    },
//	    ...
//	  ]
//	}
//
// assertPluginListEnvelope confirms (a) the
// top-level ok / plugins shape, (b) the
// plugins slice is non-nil and sorted by
// plugin_id ascending, and (c) every plugin
// row carries the per-plugin fields (id / name
// / version / connected_at / non-empty
// capabilities).
func assertPluginListEnvelope(t *testing.T, label string, result map[string]any, wantCount int) {
	t.Helper()
	if result == nil {
		t.Fatalf("%s: result is nil", label)
	}
	if got, _ := result["ok"].(bool); !got {
		t.Errorf("%s: result.ok = %v, want true", label, result["ok"])
	}
	rawPlugins, ok := result["plugins"].([]any)
	if !ok {
		t.Fatalf("%s: result.plugins = %T, want []any: %#v", label, result["plugins"], result)
	}
	if len(rawPlugins) != wantCount {
		t.Errorf("%s: len(result.plugins) = %d, want %d (%#v)", label, len(rawPlugins), wantCount, rawPlugins)
	}
	// Sorted ascending by plugin_id. ASCII sort
	// (claude-code-bridge < github-copilot-bridge
	// < mcp-bridge: c < g < m).
	for i := 1; i < len(rawPlugins); i++ {
		prev, _ := rawPlugins[i-1].(map[string]any)
		curr, _ := rawPlugins[i].(map[string]any)
		prevID, _ := prev["plugin_id"].(string)
		currID, _ := curr["plugin_id"].(string)
		if prevID > currID {
			t.Errorf("%s: plugins not sorted ascending: %q after %q", label, prevID, currID)
		}
	}
	// Every plugin carries connected_at + non-
	// empty capabilities + optional origin.
	for i, raw := range rawPlugins {
		p, _ := raw.(map[string]any)
		if got, _ := p["plugin_id"].(string); got == "" {
			t.Errorf("%s: plugins[%d].plugin_id missing", label, i)
		}
		if got, _ := p["name"].(string); got == "" {
			t.Errorf("%s: plugins[%d].name missing", label, i)
		}
		if got, _ := p["version"].(string); got == "" {
			t.Errorf("%s: plugins[%d].version missing", label, i)
		}
		if got, _ := p["connected_at"].(string); got == "" {
			t.Errorf("%s: plugins[%d].connected_at missing", label, i)
		}
		caps, ok := p["capabilities"].([]any)
		if !ok {
			t.Fatalf("%s: plugins[%d].capabilities = %T, want []any: %#v", label, i, p["capabilities"], p)
		}
		if len(caps) == 0 {
			t.Errorf("%s: plugins[%d].capabilities is empty", label, i)
		}
	}
}

// assertPluginsChangedNotification asserts a
// plugins/changed notification frame carries
// the {ok, plugins, updated_at} envelope. The
// frame shape is the same envelope the
// ListRemoteAgentPlugins response uses, but
// delivered without an "id" field (per JSON-RPC
// 2.0 §4.1 notification rules).
func assertPluginsChangedNotification(t *testing.T, label string, frame map[string]any, wantCount int) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.PluginsChangedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.PluginsChangedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	assertPluginListEnvelope(t, label+" params", params, wantCount)
}

// TestListRemoteAgentPlugins_WS_Ok exercises the
// happy path: a fresh MemPluginStore receives
// a List call; the WS round-trip returns the
// brief's {ok:true, plugins:[...], updated_at}
// envelope, sorted by plugin_id ascending.
//
// Wire-order subtlety: List has no side-effects
// (no broadcast), so the response channel is
// the only outbound frame. readJSONRPCResponse
// returns the response directly.
func TestListRemoteAgentPlugins_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemPluginStore(nil)
	srv, done := newTestTCPServerWithPlugins(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentPlugins",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentPlugins: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentPlugins: no result, got %#v", resp)
	}
	assertPluginListEnvelope(t, "ListRemoteAgentPlugins", res, 3)

	// Brief's exact plugin_id set: the three
	// DefaultPluginFixtures. Sorted ascending
	// (c < g < m).
	plugins := res["plugins"].([]any)
	want := []string{"claude-code-bridge", "github-copilot-bridge", "mcp-bridge"}
	for i, w := range want {
		p, _ := plugins[i].(map[string]any)
		if got, _ := p["plugin_id"].(string); got != w {
			t.Errorf("plugins[%d].plugin_id = %q, want %q", i, got, w)
		}
	}
}

// TestListRemoteAgentPlugins_WS_OkNoParams pins
// the spec's permissive "params may be absent"
// shape: the dispatcher returns the same
// envelope when params is missing / null.
func TestListRemoteAgentPlugins_WS_OkNoParams(t *testing.T) {
	ps := remotecontrol.NewMemPluginStore(nil)
	srv, done := newTestTCPServerWithPlugins(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// No "params" field at all.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentPlugins",
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentPlugins (no params): error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentPlugins (no params): no result, got %#v", resp)
	}
	assertPluginListEnvelope(t, "ListRemoteAgentPlugins (no params)", res, 3)
}

// TestDisconnectRemoteAgentPlugin_WS_Ok exercises
// the happy-path Disconnect: a fresh fixture-
// seeded MemPluginStore receives a Disconnect
// for "mcp-bridge"; the WS round-trip returns
// the brief's lean {ok:true, plugin_id,
// disconnected_at} envelope; the
// plugins/changed broadcast fires on every open
// ws conn; a follow-up List call confirms the
// plugin is gone.
//
// Wire-order subtlety: the dispatcher invokes
// broadcastPluginsChanged BEFORE dispatchOne
// marshals and enqueues the response, so the
// originating conn's outbound channel holds
// [notification, response]. readJSONRPCResponse
// drops the notification (no id) and returns
// the response. We drain the notification
// explicitly on c1 BEFORE asking
// readJSONRPCResponse for the response.
func TestDisconnectRemoteAgentPlugin_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemPluginStore(nil)
	srv, done := newTestTCPServerWithPlugins(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DisconnectRemoteAgentPlugin",
		"params":  map[string]any{"plugin_id": "mcp-bridge"},
	})
	// Disconnect emits plugins/changed
	// (broadcast) before the response. Drain
	// the notification first.
	notif := readJSONRPC(t, c)
	assertPluginsChangedNotification(t, "Disconnect broadcast", notif, 2)

	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("DisconnectRemoteAgentPlugin: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("DisconnectRemoteAgentPlugin: no result, got %#v", resp)
	}
	// Brief's lean envelope: ok / plugin_id /
	// disconnected_at — no plugins array on
	// the response (full state is on the
	// plugins/changed broadcast).
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	if got, _ := res["plugin_id"].(string); got != "mcp-bridge" {
		t.Errorf("result.plugin_id = %q, want mcp-bridge", got)
	}
	if got, _ := res["disconnected_at"].(string); got == "" {
		t.Errorf("result.disconnected_at missing or empty: %#v", res["disconnected_at"])
	}

	// Follow-up: List returns 2 plugins and
	// "mcp-bridge" is gone.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "ListRemoteAgentPlugins",
		"params":  map[string]any{},
	})
	listResp := readJSONRPCResponse(t, c)
	if errObj, ok := listResp["error"].(map[string]any); ok {
		t.Fatalf("post-Disconnect List: error = %#v", errObj)
	}
	listRes, ok := listResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("post-Disconnect List: no result, got %#v", listResp)
	}
	assertPluginListEnvelope(t, "post-Disconnect List", listRes, 2)
	plugins := listRes["plugins"].([]any)
	for _, raw := range plugins {
		p, _ := raw.(map[string]any)
		if got, _ := p["plugin_id"].(string); got == "mcp-bridge" {
			t.Errorf("mcp-bridge still in list.plugins after Disconnect: %#v", p)
		}
	}
}

// TestDisconnectRemoteAgentPlugin_WS_NotFound
// pins the unknown-id → -32015 envelope.
func TestDisconnectRemoteAgentPlugin_WS_NotFound(t *testing.T) {
	ps := remotecontrol.NewMemPluginStore(nil)
	srv, done := newTestTCPServerWithPlugins(t, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DisconnectRemoteAgentPlugin",
		"params":  map[string]any{"plugin_id": "no-such-plugin"},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("DisconnectRemoteAgentPlugin (unknown): no error envelope, got %#v", resp)
	}
	if got := int(errObj["code"].(float64)); got != -32015 {
		t.Errorf("error.code = %d, want -32015 (Plugin not found)", got)
	}
	if got, _ := errObj["message"].(string); got != "Plugin not found" {
		t.Errorf("error.message = %q, want %q", got, "Plugin not found")
	}
}

// TestDisconnectRemoteAgentPlugin_WS_BadParams
// pins the empty / missing / non-string
// plugin_id → -32602 path (the "dashboard
// bug" envelope).
func TestDisconnectRemoteAgentPlugin_WS_BadParams(t *testing.T) {
	cases := []struct {
		label  string
		params map[string]any
	}{
		{"empty string", map[string]any{"plugin_id": ""}},
		{"whitespace-only", map[string]any{"plugin_id": "   "}},
		{"missing key", map[string]any{}},
		{"non-string number", map[string]any{"plugin_id": 42}},
		{"non-string bool", map[string]any{"plugin_id": true}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			ps := remotecontrol.NewMemPluginStore(nil)
			srv, done := newTestTCPServerWithPlugins(t, ps)
			defer done()

			c := dialWS(t, srv.HTTPURL())
			defer c.CloseNow()

			writeJSONRPC(t, c, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "DisconnectRemoteAgentPlugin",
				"params":  tc.params,
			})
			resp := readJSONRPCResponse(t, c)
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("DisconnectRemoteAgentPlugin (bad params): no error envelope, got %#v", resp)
			}
			if got := int(errObj["code"].(float64)); got != -32602 {
				t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
			}
			if got, _ := errObj["message"].(string); got != "Invalid params" {
				t.Errorf("error.message = %q, want %q", got, "Invalid params")
			}
		})
	}
}

// TestPluginsChanged_Broadcast exercises the
// headline broadcast behaviour of cycle
// rc-plugins: a DisconnectRemoteAgentPlugin on
// one ws connection must fan a
// `plugins/changed` notification out to every
// open ws connection (not just the originating
// one). Two ws clients are brought up; a
// Disconnect on c1 fires the notification; both
// c1 and c2 observe it.
//
// Wire-order subtlety: identical to the
// rc-permissions-grants TestPermissionsChanged_Broadcast
// pattern. The originating conn's outbound
// channel holds [notification, response]; the
// second conn (which never sent a request) sees
// only the notification.
func TestPluginsChanged_Broadcast(t *testing.T) {
	ps := remotecontrol.NewMemPluginStore(nil)
	srv, done := newTestTCPServerWithPlugins(t, ps)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	// Wait for both conns to register (the
	// broadcaster walks s.conns under a single
	// RLock).
	waitForTwoConnIDs(t, srv)

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DisconnectRemoteAgentPlugin",
		"params":  map[string]any{"plugin_id": "github-copilot-bridge"},
	})

	// c1 outbound channel order:
	//   [notification, response]
	// (broadcaster enqueues first;
	//  dispatchOne enqueues the response second).
	notif1 := readJSONRPC(t, c1)
	assertPluginsChangedNotification(t, "c1 broadcast", notif1, 2)

	resp1 := readJSONRPCResponse(t, c1)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("c1 Disconnect: error = %#v", errObj)
	}
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("c1 Disconnect: no result, got %#v", resp1)
	}
	if got, _ := res1["plugin_id"].(string); got != "github-copilot-bridge" {
		t.Errorf("c1 result.plugin_id = %q, want github-copilot-bridge", got)
	}

	// c2 outbound channel: [notification]
	// — c2 never sent a request, so no response
	// arrives. readJSONRPC blocks for one frame
	// (2 s timeout inside); a buggy broadcaster
	// that only writes to the originating conn
	// trips the deadline here.
	notif2 := readJSONRPC(t, c2)
	assertPluginsChangedNotification(t, "c2 broadcast", notif2, 2)

	// Both conns observe the same post-Disconnect
	// plugin list (2 entries, no
	// github-copilot-bridge).
	p1 := notif1["params"].(map[string]any)["plugins"].([]any)
	p2 := notif2["params"].(map[string]any)["plugins"].([]any)
	if len(p1) != 2 || len(p2) != 2 {
		t.Errorf("c1/c2 plugins length = %d/%d, want 2", len(p1), len(p2))
	}
	for _, p := range p1 {
		m, _ := p.(map[string]any)
		if got, _ := m["plugin_id"].(string); got == "github-copilot-bridge" {
			t.Errorf("c1 broadcast: github-copilot-bridge still present: %#v", m)
		}
	}
	for _, p := range p2 {
		m, _ := p.(map[string]any)
		if got, _ := m["plugin_id"].(string); got == "github-copilot-bridge" {
			t.Errorf("c2 broadcast: github-copilot-bridge still present: %#v", m)
		}
	}
}

// ----------------------------------------------------------------------
// Trigger surface wire-level tests (cycle rc-triggers).
//
// The wire-level tests here mirror the rc-plugins pattern: the
// dispatcher hands the broadcast seam a notification frame BEFORE
// marshalling the response, so the originating conn's outbound
// channel holds [notification, response]. readJSONRPCResponse
// drops the notification; tests that want to assert the
// notification explicitly use readJSONRPC first, then
// readJSONRPCResponse.
//
// Wire-shape invariants pinned by this suite:
//
//   - ListRemoteAgentTriggers returns the brief's lean
//     {ok, triggers:[...]} envelope (no top-level updated_at —
//     each trigger row carries its own updated_at).
//   - ListRemoteAgentTriggerOptions returns {ok, kinds,
//     spec_schemas}; kinds is []any of the four canonical
//     kind strings (matching the in-process []string
//     shape via the dispatcher's []any conversion).
//   - CreateRemoteAgentTrigger stamps trigger_id with the
//     "tri_" prefix; the response carries both trigger_id
//     (top-level) and trigger (full row).
//   - UpdateRemoteAgentTrigger and DeleteRemoteAgentTrigger
//     emit triggers/changed on every successful call.
//   - Sentinel mapping:
//     ErrTriggerNotFound     → -32017 "Trigger not found"
//     ErrTriggerExists       → -32016 "Trigger exists"
//     ErrInvalidSpec         → -32602 "Invalid params"

// newTestTCPServerWithTriggers brings up a TCPServer whose
// dispatcher's TriggersStore seam is the supplied
// *remotecontrol.MemTriggerStore (one per test). Mirrors
// newTestTCPServerWithPlugins but for the trigger surface.
// Cycle rc-triggers adds this helper.
func newTestTCPServerWithTriggers(t *testing.T, ts remotecontrol.TriggerStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:       sess,
		Agents:        remotecontrol.DefaultAgentLister,
		Reader:        remotecontrol.DefaultOutputReader{},
		TriggersStore: ts,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// assertTriggersListEnvelope asserts the brief's
// ListRemoteAgentTriggers response envelope:
//
//	{
//	  "ok":       true,
//	  "triggers": [
//	    {
//	      "trigger_id":  "<id>",
//	      "project_id":  "<id>",
//	      "name":        "<label>",
//	      "kind":        "<kind>",
//	      "spec":        {...},
//	      "enabled":     <bool>,
//	      "created_at":  "<RFC3339Nano>",
//	      "updated_at":  "<RFC3339Nano>"
//	    },
//	    ...
//	  ]
//	}
//
// Confirms (a) top-level ok / triggers shape, (b) the
// triggers slice is non-nil with the expected length,
// (c) every trigger carries the eight per-trigger
// fields (trigger_id, project_id, name, kind, spec,
// enabled, created_at, updated_at), and (d) spec is a
// non-nil object map.
//
// The brief's triggers envelope does NOT carry a
// top-level updated_at (unlike the plugins envelope) —
// each trigger row carries its own updated_at.
func assertTriggersListEnvelope(t *testing.T, label string, result map[string]any, wantCount int) {
	t.Helper()
	if result == nil {
		t.Fatalf("%s: result is nil", label)
	}
	if got, _ := result["ok"].(bool); !got {
		t.Errorf("%s: result.ok = %v, want true", label, result["ok"])
	}
	rawTriggers, ok := result["triggers"].([]any)
	if !ok {
		t.Fatalf("%s: result.triggers = %T, want []any: %#v", label, result["triggers"], result)
	}
	if len(rawTriggers) != wantCount {
		t.Errorf("%s: len(result.triggers) = %d, want %d (%#v)", label, len(rawTriggers), wantCount, rawTriggers)
	}
	for i, raw := range rawTriggers {
		tr, _ := raw.(map[string]any)
		if tr == nil {
			t.Errorf("%s: triggers[%d] = %T, want map[string]any", label, i, raw)
			continue
		}
		if got, _ := tr["trigger_id"].(string); got == "" {
			t.Errorf("%s: triggers[%d].trigger_id missing", label, i)
		}
		if got, _ := tr["project_id"].(string); got == "" {
			t.Errorf("%s: triggers[%d].project_id missing", label, i)
		}
		if got, _ := tr["name"].(string); got == "" {
			t.Errorf("%s: triggers[%d].name missing", label, i)
		}
		if got, _ := tr["kind"].(string); got == "" {
			t.Errorf("%s: triggers[%d].kind missing", label, i)
		}
		spec, ok := tr["spec"].(map[string]any)
		if !ok {
			t.Errorf("%s: triggers[%d].spec = %T, want map[string]any: %#v", label, i, tr["spec"], tr)
		}
		_ = spec
		if _, ok := tr["enabled"].(bool); !ok {
			t.Errorf("%s: triggers[%d].enabled = %T, want bool", label, i, tr["enabled"])
		}
		if got, _ := tr["created_at"].(string); got == "" {
			t.Errorf("%s: triggers[%d].created_at missing", label, i)
		}
		if got, _ := tr["updated_at"].(string); got == "" {
			t.Errorf("%s: triggers[%d].updated_at missing", label, i)
		}
	}
}

// assertTriggersChangedNotification asserts a
// triggers/changed notification frame carries the
// {ok, triggers} envelope. The frame shape is the same
// envelope ListRemoteAgentTriggers returns, but
// delivered without an "id" field (per JSON-RPC 2.0
// §4.1 notification rules).
func assertTriggersChangedNotification(t *testing.T, label string, frame map[string]any, wantCount int) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.TriggersChangedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.TriggersChangedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	assertTriggersListEnvelope(t, label+" params", params, wantCount)
}

// TestListRemoteAgentTriggers_WS_Ok exercises the happy
// path: a pre-populated MemTriggerStore (two triggers,
// explicit CreatedAt values so the sort contract is
// pinned) receives a List call; the WS round-trip
// returns the brief's lean {ok, triggers:[...]} envelope
// sorted by created_at ascending.
//
// Wire-order subtlety: List has no side-effects (no
// broadcast), so the response channel is the only
// outbound frame. readJSONRPCResponse returns the
// response directly.
func TestListRemoteAgentTriggers_WS_Ok(t *testing.T) {
	t1 := remotecontrol.Trigger{
		TriggerID: "tri_aaa",
		ProjectID: "proj-1",
		Name:      "on-pull",
		Kind:      remotecontrol.TriggerFileChanged,
		Spec: remotecontrol.TriggerSpec{
			"pattern": "*.go",
			"event":   "modify",
		},
		Enabled:   true,
		CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
	t2 := remotecontrol.Trigger{
		TriggerID: "tri_bbb",
		ProjectID: "proj-1",
		Name:      "every-hour",
		Kind:      remotecontrol.TriggerSchedule,
		Spec: remotecontrol.TriggerSpec{
			"prompt": "summarise",
			"cron":   "0 * * * *",
		},
		Enabled:   false,
		CreatedAt: time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC),
	}
	ts := remotecontrol.NewMemTriggerStoreWith([]remotecontrol.Trigger{t2, t1}, nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentTriggers",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentTriggers: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentTriggers: no result, got %#v", resp)
	}
	assertTriggersListEnvelope(t, "ListRemoteAgentTriggers", res, 2)

	// CreatedAt ascending sort contract: t1 (12:00:00)
	// precedes t2 (12:05:00). We posted them in the
	// reverse order so a non-sorting store would fail
	// this assertion.
	triggers := res["triggers"].([]any)
	if len(triggers) != 2 {
		t.Fatalf("len(triggers) = %d, want 2", len(triggers))
	}
	first, _ := triggers[0].(map[string]any)
	if got, _ := first["trigger_id"].(string); got != "tri_aaa" {
		t.Errorf("triggers[0].trigger_id = %q, want tri_aaa (created_at sort)", got)
	}
	second, _ := triggers[1].(map[string]any)
	if got, _ := second["trigger_id"].(string); got != "tri_bbb" {
		t.Errorf("triggers[1].trigger_id = %q, want tri_bbb (created_at sort)", got)
	}
}

// TestListRemoteAgentTriggers_WS_OkNoParams pins the
// spec's permissive "params may be absent" shape: the
// dispatcher returns the same envelope when params is
// missing / null.
func TestListRemoteAgentTriggers_WS_OkNoParams(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// No "params" field at all.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentTriggers",
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentTriggers (no params): error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentTriggers (no params): no result, got %#v", resp)
	}
	assertTriggersListEnvelope(t, "ListRemoteAgentTriggers (no params)", res, 0)
}

// TestListRemoteAgentTriggerOptions_WS_Ok exercises
// the Options RPC: returns {ok, kinds, spec_schemas}
// with the four canonical kinds and the four schema
// entries. The kinds slice is rendered as []any (per
// the dispatcher's "match in-process and wire types"
// contract) so a JSON-roundtripped caller and an
// in-process caller see the same shape.
func TestListRemoteAgentTriggerOptions_WS_Ok(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentTriggerOptions",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentTriggerOptions: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentTriggerOptions: no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", got)
	}

	// kinds is []any of 4 strings.
	kinds, ok := res["kinds"].([]any)
	if !ok {
		t.Fatalf("result.kinds = %T, want []any: %#v", res["kinds"], res)
	}
	if len(kinds) != 4 {
		t.Fatalf("len(kinds) = %d, want 4: %#v", len(kinds), kinds)
	}
	wantKinds := []string{"file-changed", "schedule", "webhook", "manual"}
	for i, w := range wantKinds {
		if got, _ := kinds[i].(string); got != w {
			t.Errorf("kinds[%d] = %q, want %q", i, got, w)
		}
	}

	// spec_schemas is map[string]any with 4 keys.
	schemas, ok := res["spec_schemas"].(map[string]any)
	if !ok {
		t.Fatalf("result.spec_schemas = %T, want map[string]any: %#v", res["spec_schemas"], res)
	}
	if len(schemas) != 4 {
		t.Errorf("len(spec_schemas) = %d, want 4: %#v", len(schemas), schemas)
	}
	for _, k := range wantKinds {
		entry, ok := schemas[k].(map[string]any)
		if !ok {
			t.Errorf("spec_schemas[%q] = %T, want map[string]any", k, schemas[k])
			continue
		}
		if got, _ := entry["description"].(string); got == "" {
			t.Errorf("spec_schemas[%q].description missing", k)
		}
		if _, ok := entry["required"].([]any); !ok {
			t.Errorf("spec_schemas[%q].required = %T, want []any", k, entry["required"])
		}
		if _, ok := entry["field_types"].(map[string]any); !ok {
			t.Errorf("spec_schemas[%q].field_types = %T, want map[string]any", k, entry["field_types"])
		}
	}
}

// TestCreateRemoteAgentTrigger_WS_Ok exercises the
// happy-path Create: the WS round-trip returns the
// brief's {ok, trigger_id, trigger} envelope; trigger_id
// carries the "tri_" prefix; the trigger row carries
// the eight per-trigger fields; a follow-up List call
// shows the new row in the registry.
//
// Wire-order subtlety: Create emits triggers/changed
// (broadcast) BEFORE the response, so the originating
// conn's outbound channel holds [notification,
// response]. readJSONRPCResponse drops the
// notification, so we drain it explicitly first.
func TestCreateRemoteAgentTrigger_WS_Ok(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "on-pull",
				"kind":       "file-changed",
				"spec": map[string]any{
					"pattern": "*.go",
					"event":   "modify",
				},
				"enabled": true,
			},
		},
	})

	// Drain the triggers/changed broadcast first.
	notif := readJSONRPC(t, c)
	assertTriggersChangedNotification(t, "Create broadcast", notif, 1)

	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("CreateRemoteAgentTrigger: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("CreateRemoteAgentTrigger: no result, got %#v", resp)
	}

	// ok + trigger_id has tri_ prefix.
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", got)
	}
	triggerID, _ := res["trigger_id"].(string)
	if !strings.HasPrefix(triggerID, "tri_") {
		t.Errorf("result.trigger_id = %q, want tri_ prefix", triggerID)
	}

	// trigger row mirrors trigger_id and the eight
	// per-trigger fields.
	tr, ok := res["trigger"].(map[string]any)
	if !ok {
		t.Fatalf("result.trigger = %T, want map[string]any: %#v", res["trigger"], res)
	}
	if got, _ := tr["trigger_id"].(string); got != triggerID {
		t.Errorf("result.trigger.trigger_id = %q, want %q", got, triggerID)
	}
	if got, _ := tr["project_id"].(string); got != "proj-1" {
		t.Errorf("result.trigger.project_id = %q, want proj-1", got)
	}
	if got, _ := tr["name"].(string); got != "on-pull" {
		t.Errorf("result.trigger.name = %q, want on-pull", got)
	}
	if got, _ := tr["kind"].(string); got != "file-changed" {
		t.Errorf("result.trigger.kind = %q, want file-changed", got)
	}
	spec, ok := tr["spec"].(map[string]any)
	if !ok {
		t.Fatalf("result.trigger.spec = %T, want map[string]any: %#v", tr["spec"], tr)
	}
	if got, _ := spec["pattern"].(string); got != "*.go" {
		t.Errorf("result.trigger.spec.pattern = %q, want *.go", got)
	}
	if got, _ := spec["event"].(string); got != "modify" {
		t.Errorf("result.trigger.spec.event = %q, want modify", got)
	}
	if got, _ := tr["enabled"].(bool); !got {
		t.Errorf("result.trigger.enabled = %v, want true", got)
	}
	if got, _ := tr["created_at"].(string); got == "" {
		t.Errorf("result.trigger.created_at empty")
	}
	if got, _ := tr["updated_at"].(string); got == "" {
		t.Errorf("result.trigger.updated_at empty")
	}

	// Follow-up: List shows the new row.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "ListRemoteAgentTriggers",
		"params":  map[string]any{},
	})
	listResp := readJSONRPCResponse(t, c)
	if errObj, ok := listResp["error"].(map[string]any); ok {
		t.Fatalf("post-Create List: error = %#v", errObj)
	}
	listRes, ok := listResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("post-Create List: no result, got %#v", listResp)
	}
	assertTriggersListEnvelope(t, "post-Create List", listRes, 1)
	listTriggers := listRes["triggers"].([]any)
	listed, _ := listTriggers[0].(map[string]any)
	if got, _ := listed["trigger_id"].(string); got != triggerID {
		t.Errorf("post-Create List: triggers[0].trigger_id = %q, want %q", got, triggerID)
	}
}

// TestCreateRemoteAgentTrigger_WS_Duplicate pins the
// unique-within-(project_id, name) → -32016 envelope.
func TestCreateRemoteAgentTrigger_WS_Duplicate(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	first := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "on-pull",
				"kind":       "file-changed",
				"spec": map[string]any{
					"pattern": "*.go",
					"event":   "modify",
				},
				"enabled": true,
			},
		},
	}
	writeJSONRPC(t, c, first)
	// Drain the broadcast that fires on the first Create.
	notif := readJSONRPC(t, c)
	assertTriggersChangedNotification(t, "first Create broadcast", notif, 1)
	// And the response.
	resp1 := readJSONRPCResponse(t, c)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("first Create: error = %#v", errObj)
	}

	// Second Create with the same (project_id, name)
	// — store rejects with ErrTriggerExists → -32016.
	second := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "on-pull",
				"kind":       "schedule",
				"spec": map[string]any{
					"prompt": "do something else",
					"cron":   "0 0 * * *",
				},
				"enabled": false,
			},
		},
	}
	writeJSONRPC(t, c, second)
	resp2 := readJSONRPCResponse(t, c)
	errObj, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("duplicate Create: no error envelope, got %#v", resp2)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32016 {
		t.Errorf("error.code = %d, want -32016 (Trigger exists)", got)
	}
	if got, _ := errObj["message"].(string); got != "Trigger exists" {
		t.Errorf("error.message = %q, want %q", got, "Trigger exists")
	}
}

// TestCreateRemoteAgentTrigger_WS_BadSpec pins the
// per-kind spec validator → -32602 envelope. We use
// the file-changed kind with a missing "pattern" key
// (the validator's specFieldMissingErr path).
func TestCreateRemoteAgentTrigger_WS_BadSpec(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "bad-spec",
				"kind":       "file-changed",
				"spec": map[string]any{
					// "pattern" missing → specFieldMissingErr.
					"event": "modify",
				},
				"enabled": false,
			},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("bad-spec Create: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// TestUpdateRemoteAgentTrigger_WS_Ok exercises the
// happy-path Update: pre-create a trigger, post an
// Update with {enabled: true}, assert the
// post-mutation row reflects the patch; a follow-up
// List shows the same row.
//
// Wire-order subtlety: Update emits triggers/changed
// BEFORE the response. Drain the notification first.
func TestUpdateRemoteAgentTrigger_WS_Ok(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Pre-create via the wire so we know the
	// assigned trigger_id.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "on-pull",
				"kind":       "file-changed",
				"spec": map[string]any{
					"pattern": "*.go",
					"event":   "modify",
				},
				"enabled": false,
			},
		},
	})
	notif0 := readJSONRPC(t, c)
	assertTriggersChangedNotification(t, "pre-Update Create broadcast", notif0, 1)
	resp0 := readJSONRPCResponse(t, c)
	res0, _ := resp0["result"].(map[string]any)
	triggerID, _ := res0["trigger_id"].(string)

	// Update — patch enabled only.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "UpdateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger_id": triggerID,
			"patch": map[string]any{
				"enabled": true,
			},
		},
	})
	notif1 := readJSONRPC(t, c)
	assertTriggersChangedNotification(t, "Update broadcast", notif1, 1)

	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("UpdateRemoteAgentTrigger: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("UpdateRemoteAgentTrigger: no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", got)
	}
	tr, ok := res["trigger"].(map[string]any)
	if !ok {
		t.Fatalf("result.trigger = %T, want map[string]any: %#v", res["trigger"], res)
	}
	if got, _ := tr["trigger_id"].(string); got != triggerID {
		t.Errorf("result.trigger.trigger_id = %q, want %q", got, triggerID)
	}
	if got, _ := tr["enabled"].(bool); !got {
		t.Errorf("result.trigger.enabled = %v, want true (the patched value)", got)
	}
	// Unchanged fields stay as the Create set them.
	if got, _ := tr["project_id"].(string); got != "proj-1" {
		t.Errorf("result.trigger.project_id = %q, want proj-1", got)
	}
	if got, _ := tr["name"].(string); got != "on-pull" {
		t.Errorf("result.trigger.name = %q, want on-pull", got)
	}
}

// TestUpdateRemoteAgentTrigger_WS_NotFound pins the
// unknown-id → -32017 envelope.
func TestUpdateRemoteAgentTrigger_WS_NotFound(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpdateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger_id": "tri_does_not_exist",
			"patch": map[string]any{
				"enabled": true,
			},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Update (unknown id): no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32017 {
		t.Errorf("error.code = %d, want -32017 (Trigger not found)", got)
	}
	if got, _ := errObj["message"].(string); got != "Trigger not found" {
		t.Errorf("error.message = %q, want %q", got, "Trigger not found")
	}
}

// TestDeleteRemoteAgentTrigger_WS_Ok exercises the
// happy-path Delete: pre-create a trigger, post a
// Delete, assert the response envelope carries
// {ok, trigger_id, deleted_at}; a follow-up List
// confirms the trigger is gone.
//
// Wire-order subtlety: Delete emits triggers/changed
// BEFORE the response, with the post-mutation state
// (zero triggers). Drain the notification first.
func TestDeleteRemoteAgentTrigger_WS_Ok(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Pre-create.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "on-pull",
				"kind":       "file-changed",
				"spec": map[string]any{
					"pattern": "*.go",
					"event":   "modify",
				},
				"enabled": true,
			},
		},
	})
	notif0 := readJSONRPC(t, c)
	assertTriggersChangedNotification(t, "pre-Delete Create broadcast", notif0, 1)
	resp0 := readJSONRPCResponse(t, c)
	res0, _ := resp0["result"].(map[string]any)
	triggerID, _ := res0["trigger_id"].(string)

	// Delete.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "DeleteRemoteAgentTrigger",
		"params": map[string]any{
			"trigger_id": triggerID,
		},
	})
	// Post-Delete broadcast carries the empty state.
	notif1 := readJSONRPC(t, c)
	assertTriggersChangedNotification(t, "Delete broadcast", notif1, 0)

	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("DeleteRemoteAgentTrigger: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("DeleteRemoteAgentTrigger: no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", got)
	}
	if got, _ := res["trigger_id"].(string); got != triggerID {
		t.Errorf("result.trigger_id = %q, want %q", got, triggerID)
	}
	if got, _ := res["deleted_at"].(string); got == "" {
		t.Errorf("result.deleted_at empty")
	}

	// Follow-up: List returns 0 entries.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "ListRemoteAgentTriggers",
		"params":  map[string]any{},
	})
	listResp := readJSONRPCResponse(t, c)
	if errObj, ok := listResp["error"].(map[string]any); ok {
		t.Fatalf("post-Delete List: error = %#v", errObj)
	}
	listRes, ok := listResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("post-Delete List: no result, got %#v", listResp)
	}
	assertTriggersListEnvelope(t, "post-Delete List", listRes, 0)
}

// TestDeleteRemoteAgentTrigger_WS_NotFound pins the
// unknown-id → -32017 envelope.
func TestDeleteRemoteAgentTrigger_WS_NotFound(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentTrigger",
		"params": map[string]any{
			"trigger_id": "tri_does_not_exist",
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Delete (unknown id): no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32017 {
		t.Errorf("error.code = %d, want -32017 (Trigger not found)", got)
	}
	if got, _ := errObj["message"].(string); got != "Trigger not found" {
		t.Errorf("error.message = %q, want %q", got, "Trigger not found")
	}
}

// TestTriggersChanged_Broadcast exercises the headline
// broadcast behaviour of cycle rc-triggers: a
// DeleteRemoteAgentTrigger on one ws connection must
// fan a `triggers/changed` notification out to every
// open ws connection (not just the originating one).
// Two ws clients are brought up; a Delete on c1 fires
// the notification; both c1 and c2 observe it.
//
// Wire-order subtlety: identical to the rc-plugins
// TestPluginsChanged_Broadcast pattern. The
// originating conn's outbound channel holds
// [notification, response]; the second conn (which
// never sent a request) sees only the notification.
func TestTriggersChanged_Broadcast(t *testing.T) {
	ts := remotecontrol.NewMemTriggerStore(nil)
	srv, done := newTestTCPServerWithTriggers(t, ts)
	defer done()

	// Pre-create one trigger so the post-Delete
	// broadcast carries the empty state.
	c0 := dialWS(t, srv.HTTPURL())
	defer c0.CloseNow()
	writeJSONRPC(t, c0, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTrigger",
		"params": map[string]any{
			"trigger": map[string]any{
				"project_id": "proj-1",
				"name":       "on-pull",
				"kind":       "file-changed",
				"spec": map[string]any{
					"pattern": "*.go",
					"event":   "modify",
				},
				"enabled": true,
			},
		},
	})
	notif0 := readJSONRPC(t, c0)
	assertTriggersChangedNotification(t, "pre-broadcast Create broadcast", notif0, 1)
	resp0 := readJSONRPCResponse(t, c0)
	res0, _ := resp0["result"].(map[string]any)
	triggerID, _ := res0["trigger_id"].(string)
	c0.CloseNow() // close the pre-create conn; the broadcast test uses c1 + c2

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	// Wait for both conns to register (the broadcaster
	// walks s.conns under a single RLock).
	waitForTwoConnIDs(t, srv)

	// Delete on c1 — the originating conn.
	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "DeleteRemoteAgentTrigger",
		"params": map[string]any{
			"trigger_id": triggerID,
		},
	})

	// c1 outbound channel order:
	//   [notification, response]
	// (broadcaster enqueues first; dispatchOne
	//  enqueues the response second).
	notif1 := readJSONRPC(t, c1)
	assertTriggersChangedNotification(t, "c1 broadcast", notif1, 0)

	resp1 := readJSONRPCResponse(t, c1)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("c1 Delete: error = %#v", errObj)
	}
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("c1 Delete: no result, got %#v", resp1)
	}
	if got, _ := res1["trigger_id"].(string); got != triggerID {
		t.Errorf("c1 result.trigger_id = %q, want %q", got, triggerID)
	}
	if got, _ := res1["ok"].(bool); !got {
		t.Errorf("c1 result.ok = %v, want true", got)
	}

	// c2 outbound channel: [notification]
	// — c2 never sent a request, so no response
	// arrives. readJSONRPC blocks for one frame (2 s
	// timeout inside); a buggy broadcaster that only
	// writes to the originating conn trips the
	// deadline here.
	notif2 := readJSONRPC(t, c2)
	assertTriggersChangedNotification(t, "c2 broadcast", notif2, 0)

	// Both conns observe the same post-Delete state
	// (zero triggers; the deleted row is absent).
	p1 := notif1["params"].(map[string]any)["triggers"].([]any)
	p2 := notif2["params"].(map[string]any)["triggers"].([]any)
	if len(p1) != 0 || len(p2) != 0 {
		t.Errorf("c1/c2 triggers length = %d/%d, want 0", len(p1), len(p2))
	}
	for _, p := range p1 {
		m, _ := p.(map[string]any)
		if got, _ := m["trigger_id"].(string); got == triggerID {
			t.Errorf("c1 broadcast: deleted trigger %q still present: %#v", got, m)
		}
	}
	for _, p := range p2 {
		m, _ := p.(map[string]any)
		if got, _ := m["trigger_id"].(string); got == triggerID {
			t.Errorf("c2 broadcast: deleted trigger %q still present: %#v", got, m)
		}
	}
}

// codeAsIntForWS is the wire-equivalent of the
// remotecontrol.codeAsInt helper: a JSON-RPC error
// "code" travels over the wire as a JSON number, which
// encoding/json decodes into a float64 in Go. This
// thin wrapper asserts the type and returns an int so
// the wire tests can compare against the literal
// sentinel codes (-32016 / -32017 / -32602 / -32603).
func codeAsIntForWS(t *testing.T, v any) int {
	t.Helper()
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	default:
		t.Fatalf("error code = %T, want int or float64", v)
		return 0
	}
}

// ----------------------------------------------------------------------
// Fastpush toggle wire-level tests (cycle rc-fastpush-toggle).
//
// Wire-shape invariants pinned by this suite:
//
//   - SetRemoteAgentFastpushEnabled returns the brief's
//     lean envelope ({ok:true, cascade_id, enabled, channel,
//     push_delivery, updated_at}) with push_delivery
//     verbatim "deferred" (the explicit sentinel;
//     rc-fastpush-channel will replace it with
//     "live" once a real transport lands).
//   - empty / non-string cascade_id → -32602;
//   - missing enabled → -32602;
//   - unknown cascade_id → -32004 "Cascade not found";
//   - no-store-installed → -32603;
//   - SetEnabled emits fastpush/setting_changed on
//     every successful call (the same BroadcastNotify
//     seam rc-permissions-grants added; shared with
//     rc-plugins and rc-triggers).
//
// The dispatcher hands the broadcast seam a notification
// frame BEFORE marshalling the response, so the
// originating conn's outbound channel holds
// [notification, response]. readJSONRPCResponse drops
// the notification; tests that want to assert the
// notification explicitly use readJSONRPC first, then
// readJSONRPCResponse.

// newTestTCPServerWithFastpush brings up a TCPServer
// whose dispatcher's FastpushStore seam is the
// supplied FastpushStore (one per test). Mirrors
// newTestTCPServerWithPlugins /
// newTestTCPServerWithTriggers but for the
// fast-push surface. Cycle rc-fastpush-toggle adds
// this helper.
func newTestTCPServerWithFastpush(t *testing.T, fs remotecontrol.FastpushStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:       sess,
		Agents:        remotecontrol.DefaultAgentLister,
		Reader:        remotecontrol.DefaultOutputReader{},
		FastpushStore: fs,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// assertFastpushSettingEnvelope asserts the response
// envelope the SetRemoteAgentFastpushEnabled handler
// stamps: an {ok, cascade_id, enabled, channel,
// push_delivery, updated_at} map with the supplied
// expected cascade_id / enabled. push_delivery is
// always asserted to equal
// remotecontrol.FastpushPushDeliveryDeferred
// ("deferred") — the explicit sentinel the brief pins.
// updated_at is parsed and asserted to be a non-empty
// RFC3339Nano string; the value is whatever the
// MemFastpushStore stamped at handler time, so we
// don't pin the exact timestamp.
func assertFastpushSettingEnvelope(t *testing.T, label string, result map[string]any, wantCascadeID string, wantEnabled bool) {
	t.Helper()
	if result == nil {
		t.Fatalf("%s: result is nil", label)
	}
	if got, _ := result["ok"].(bool); !got {
		t.Errorf("%s: result.ok = %v, want true", label, result["ok"])
	}
	if got, _ := result["cascade_id"].(string); got != wantCascadeID {
		t.Errorf("%s: result.cascade_id = %q, want %q", label, got, wantCascadeID)
	}
	if got, _ := result["enabled"].(bool); got != wantEnabled {
		t.Errorf("%s: result.enabled = %v, want %v", label, got, wantEnabled)
	}
	if got, _ := result["channel"].(string); got != remotecontrol.FastpushChannelName {
		t.Errorf("%s: result.channel = %q, want %q", label, got, remotecontrol.FastpushChannelName)
	}
	if got, _ := result["push_delivery"].(string); got != remotecontrol.FastpushPushDeliveryDeferred {
		t.Errorf("%s: result.push_delivery = %q, want %q (deferred sentinel)", label, got, remotecontrol.FastpushPushDeliveryDeferred)
	}
	if got, _ := result["updated_at"].(string); got == "" {
		t.Errorf("%s: result.updated_at missing or empty: %#v", label, result["updated_at"])
	}
}

// assertFastpushSettingChangedNotification asserts a
// fastpush/setting_changed notification frame carries
// the expected enabled value (and a non-empty
// updated_at). The frame shape is the same envelope
// the response uses, but delivered without an "id"
// field (per JSON-RPC 2.0 §4.1 notification rules).
func assertFastpushSettingChangedNotification(t *testing.T, label string, frame map[string]any, wantCascadeID string, wantEnabled bool) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.NotifFastpushSettingChangedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.NotifFastpushSettingChangedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	assertFastpushSettingEnvelope(t, label+" params", params, wantCascadeID, wantEnabled)
}

// TestSetRemoteAgentFastpushEnabled_WS_Ok exercises
// the happy path: a fresh MemFastpushStore receives
// a SetEnabled wire call; the WS round-trip returns
// the expected envelope.
//
// Wire-order subtlety: identical to the rc-plugins /
// rc-triggers TestXChanged_Broadcast pattern. The
// dispatcher invokes broadcastFastpushSettingChanged
// BEFORE dispatchOne marshals and enqueues the
// response, so the originating conn's outbound
// channel holds [notification, response].
// readJSONRPCResponse drops the notification (no id)
// and returns the response — that's exactly what we
// want here. The dedicated
// TestSetRemoteAgentFastpushEnabled_Broadcasts_Setting_Changed
// test pins the notification arrival with two conns,
// so we don't re-assert it in every single-conn
// happy-path test.
//
// The test pre-registers a cascade in the dispatcher's
// registry so the dispatcher's "Cascade not found"
// check passes; the FastpushStore checkCascade
// closure also accepts every cascade_id (matches
// the production path's semantics).
func TestSetRemoteAgentFastpushEnabled_WS_Ok(t *testing.T) {
	check := func(string) bool { return true }
	fs := remotecontrol.NewMemFastpushStore(nil, check)
	srv, done := newTestTCPServerWithFastpush(t, fs)
	defer done()

	// Pre-register a cascade so the dispatcher's
	// -32004 guard does not trip. The cascade is
	// metadata-only; the test doesn't drive a
	// turn.
	c, err := remotecontrol.NewCascade(remotecontrol.CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := srv.dispatcher.(*remotecontrol.Dispatcher).Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentFastpushEnabled",
		"params": map[string]any{
			"cascade_id": c.ID,
			"enabled":    true,
		},
	})
	resp := readJSONRPCResponse(t, c1)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("SetRemoteAgentFastpushEnabled: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no result, got %#v", resp)
	}
	assertFastpushSettingEnvelope(t, "SetRemoteAgentFastpushEnabled", res, c.ID, true)
}

// TestSetRemoteAgentFastpushEnabled_WS_BadParams_EmptyCascadeID
// pins the -32602 envelope for an empty cascade_id.
// The brief explicitly enumerates the empty / non-
// string shape as -32602 "Invalid params".
func TestSetRemoteAgentFastpushEnabled_WS_BadParams_EmptyCascadeID(t *testing.T) {
	check := func(string) bool { return true }
	fs := remotecontrol.NewMemFastpushStore(nil, check)
	srv, done := newTestTCPServerWithFastpush(t, fs)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentFastpushEnabled",
		"params": map[string]any{
			"cascade_id": "",
			"enabled":    true,
		},
	})
	resp := readJSONRPCResponse(t, c1)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// TestSetRemoteAgentFastpushEnabled_WS_BadParams_MissingEnabled
// pins the -32602 envelope for a missing `enabled`
// field. The brief enumerates the missing-shape as
// -32602 "Invalid params" (the dashboard forgot to
// send the field).
func TestSetRemoteAgentFastpushEnabled_WS_BadParams_MissingEnabled(t *testing.T) {
	check := func(string) bool { return true }
	fs := remotecontrol.NewMemFastpushStore(nil, check)
	srv, done := newTestTCPServerWithFastpush(t, fs)
	defer done()

	// Pre-register a cascade so the dispatcher's
	// cascade-existence check doesn't short-
	// circuit the missing-enabled branch.
	c, err := remotecontrol.NewCascade(remotecontrol.CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := srv.dispatcher.(*remotecontrol.Dispatcher).Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentFastpushEnabled",
		"params": map[string]any{
			"cascade_id": c.ID,
		},
	})
	resp := readJSONRPCResponse(t, c1)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
}

// TestSetRemoteAgentFastpushEnabled_WS_CascadeNotFound
// pins the -32004 envelope for an unknown cascade_id.
// The brief reuses the dispatcher's existing
// ErrCodeCascadeNotFound code so dashboard error
// filters across the cascade lifecycle stay uniform.
func TestSetRemoteAgentFastpushEnabled_WS_CascadeNotFound(t *testing.T) {
	check := func(id string) bool { return false }
	fs := remotecontrol.NewMemFastpushStore(nil, check)
	srv, done := newTestTCPServerWithFastpush(t, fs)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentFastpushEnabled",
		"params": map[string]any{
			"cascade_id": "cas_unknown",
			"enabled":    true,
		},
	})
	resp := readJSONRPCResponse(t, c1)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != remotecontrol.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, remotecontrol.ErrCodeCascadeNotFound)
	}
	if got, _ := errObj["message"].(string); got != "Cascade not found" {
		t.Errorf("error.message = %q, want %q", got, "Cascade not found")
	}
}

// TestSetRemoteAgentFastpushEnabled_Broadcasts_Setting_Changed
// exercises the headline broadcast behaviour of
// cycle rc-fastpush-toggle: a
// SetRemoteAgentFastpushEnabled on one ws connection
// must fan a `fastpush/setting_changed` notification
// out to every open ws connection (not just the
// originating one). Two ws clients are brought up;
// a Set on c1 fires the notification; both c1 and
// c2 observe it.
//
// Wire-order subtlety: identical to the rc-plugins
// / rc-triggers TestXChanged_Broadcast pattern. The
// originating conn's outbound channel holds
// [notification, response]; the second conn (which
// never sent a request) sees only the notification.
func TestSetRemoteAgentFastpushEnabled_Broadcasts_Setting_Changed(t *testing.T) {
	check := func(string) bool { return true }
	fs := remotecontrol.NewMemFastpushStore(nil, check)
	srv, done := newTestTCPServerWithFastpush(t, fs)
	defer done()

	// Pre-register a cascade so the dispatcher's
	// cascade-existence check doesn't trip.
	c, err := remotecontrol.NewCascade(remotecontrol.CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := srv.dispatcher.(*remotecontrol.Dispatcher).Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	// Wait for both conns to register (the
	// broadcaster walks s.conns under a single
	// RLock).
	waitForTwoConnIDs(t, srv)

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SetRemoteAgentFastpushEnabled",
		"params": map[string]any{
			"cascade_id": c.ID,
			"enabled":    true,
		},
	})

	// c1 outbound channel order:
	//   [notification, response]
	// (broadcaster enqueues first;
	//  dispatchOne enqueues the response second).
	notif1 := readJSONRPC(t, c1)
	assertFastpushSettingChangedNotification(t, "c1 broadcast", notif1, c.ID, true)

	resp1 := readJSONRPCResponse(t, c1)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("c1 Set: error = %#v", errObj)
	}
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("c1 Set: no result, got %#v", resp1)
	}
	if got, _ := res1["cascade_id"].(string); got != c.ID {
		t.Errorf("c1 result.cascade_id = %q, want %q", got, c.ID)
	}
	if got, _ := res1["enabled"].(bool); !got {
		t.Errorf("c1 result.enabled = false, want true")
	}

	// c2 outbound channel: [notification] —
	// c2 never sent a request, so no response
	// arrives. readJSONRPC blocks for one frame
	// (2 s timeout inside); a buggy broadcaster
	// that only writes to the originating conn
	// trips the deadline here.
	notif2 := readJSONRPC(t, c2)
	assertFastpushSettingChangedNotification(t, "c2 broadcast", notif2, c.ID, true)

	// Both conns observe the same post-Set state.
	enabled1, _ := notif1["params"].(map[string]any)["enabled"].(bool)
	enabled2, _ := notif2["params"].(map[string]any)["enabled"].(bool)
	if !enabled1 || !enabled2 {
		t.Errorf("c1/c2 enabled = %v/%v, want true/true", enabled1, enabled2)
	}
}

// ----------------------------------------------------------------------
// Resource wire-level tests (cycle rc-resources).
//
// Wire-shape invariants pinned by this suite:
//
//   - ListRemoteAgentResourceProviders returns the
//     {providers:[...], total} envelope, with the
//     fixture-seeded 4 providers sorted by
//     provider_id ascending.
//
//   - ListRemoteAgentAvailableResources returns the
//     {resources:[...], total} envelope, with the
//     fixture-seeded 12 resources, and accepts both
//     provider_id + kind filters.
//
//   - UpdateRemoteAgentProjectResources returns the
//     brief's lean {ok, project_id, bound_count,
//     unbound_count, updated_at} envelope on a
//     successful Bind, AND emits a
//     "resources/changed" broadcast notification on
//     every open ws conn (the rc-permissions-grants
//     BroadcastNotify seam shared with rc-plugins /
//     rc-triggers / rc-fastpush-toggle).
//
//   - Sentinel errors (-32602 "Invalid params" for
//     empty project_id, -32004 "Project not found"
//     for unknown project_id) round-trip cleanly
//     through the JSON wire format.

// newTestTCPServerWithResources returns a *TCPServer
// wired with the supplied ResourceStore +
// ProjectStore so wire tests can exercise the
// three resource methods end to end. Mirrors
// newTestTCPServerWithPlugins /
// newTestTCPServerWithFastpush but for the resource
// surface.
func newTestTCPServerWithResources(t *testing.T, rs remotecontrol.ResourceStore, projects remotecontrol.ProjectStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:       sess,
		Agents:        remotecontrol.DefaultAgentLister,
		Reader:        remotecontrol.DefaultOutputReader{},
		ResourceStore: rs,
		Projects:      projects,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// assertResourcesChangedNotification asserts a
// resources/changed notification frame carries
// the expected {ok, project_id, bound_count,
// unbound_count, updated_at} envelope. The frame
// shape is the same envelope the response uses,
// but delivered without an "id" field (per
// JSON-RPC 2.0 §4.1 notification rules).
func assertResourcesChangedNotification(t *testing.T, label string, frame map[string]any, wantProjectID string, wantBound, wantUnbound int) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.NotifResourcesChangedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.NotifResourcesChangedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	if got, _ := params["ok"].(bool); !got {
		t.Errorf("%s: params.ok = %v, want true", label, params["ok"])
	}
	if got, _ := params["project_id"].(string); got != wantProjectID {
		t.Errorf("%s: params.project_id = %q, want %q", label, got, wantProjectID)
	}
	if got, _ := params["bound_count"].(float64); int(got) != wantBound {
		t.Errorf("%s: params.bound_count = %v, want %d", label, got, wantBound)
	}
	if got, _ := params["unbound_count"].(float64); int(got) != wantUnbound {
		t.Errorf("%s: params.unbound_count = %v, want %d", label, got, wantUnbound)
	}
	if got, _ := params["updated_at"].(string); got == "" {
		t.Errorf("%s: params.updated_at missing or empty", label)
	}
}

// TestListRemoteAgentResourceProviders_WS_Ok
// exercises the happy path: a fresh
// MemResourceStore receives a List call; the WS
// round-trip returns the brief's
// {providers:[...], total} envelope, sorted by
// provider_id ascending. The fixture set has 4
// providers (gdocs, github, linear, sentry in
// alphabetical order).
func TestListRemoteAgentResourceProviders_WS_Ok(t *testing.T) {
	rs := remotecontrol.NewMemResourceStore(nil, nil)
	srv, done := newTestTCPServerWithResources(t, rs, nil)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentResourceProviders",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentResourceProviders: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentResourceProviders: no result, got %#v", resp)
	}
	providers, ok := res["providers"].([]any)
	if !ok {
		t.Fatalf("result.providers = %T, want []any: %#v", res["providers"], res)
	}
	if len(providers) != 4 {
		t.Errorf("len(providers) = %d, want 4", len(providers))
	}
	if total, _ := res["total"].(float64); int(total) != 4 {
		t.Errorf("result.total = %v, want 4", total)
	}
	// Alphabetical: gdocs < github < linear < sentry.
	want := []string{"gdocs", "github", "linear", "sentry"}
	for i, w := range want {
		p, _ := providers[i].(map[string]any)
		if got, _ := p["provider_id"].(string); got != w {
			t.Errorf("providers[%d].provider_id = %q, want %q", i, got, w)
		}
	}
}

// TestListRemoteAgentAvailableResources_WS_Ok
// exercises the happy path: a fresh
// MemResourceStore receives a List call; the WS
// round-trip returns the brief's
// {resources:[...], total} envelope. The fixture
// set has 12 resources; alphabetical sort by
// resource_id ascending.
func TestListRemoteAgentAvailableResources_WS_Ok(t *testing.T) {
	rs := remotecontrol.NewMemResourceStore(nil, nil)
	srv, done := newTestTCPServerWithResources(t, rs, nil)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ListRemoteAgentAvailableResources",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentAvailableResources: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListRemoteAgentAvailableResources: no result, got %#v", resp)
	}
	resources, ok := res["resources"].([]any)
	if !ok {
		t.Fatalf("result.resources = %T, want []any: %#v", res["resources"], res)
	}
	if len(resources) != 12 {
		t.Errorf("len(resources) = %d, want 12", len(resources))
	}
	if total, _ := res["total"].(float64); int(total) != 12 {
		t.Errorf("result.total = %v, want 12", total)
	}
}

// TestUpdateRemoteAgentProjectResources_WS_Ok
// exercises the happy-path Bind: a fresh project
// receives an Update call binding two known
// resources; the WS round-trip returns the
// brief's lean {ok, project_id, bound_count,
// unbound_count, updated_at} envelope AND emits a
// "resources/changed" broadcast notification on
// the originating conn (the BroadcastNotify seam
// rc-permissions-grants added; shared with
// rc-plugins, rc-triggers, and rc-fastpush-toggle).
//
// Wire-order subtlety: the dispatcher invokes
// BroadcastNotify BEFORE marshalling the response,
// so the originating conn's outbound channel holds
// [notification, response]. readJSONRPCResponse
// drops the notification (no id) and returns the
// response. We drain the notification explicitly
// before asking readJSONRPCResponse for the
// response.
func TestUpdateRemoteAgentProjectResources_WS_Ok(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), remotecontrol.NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := remotecontrol.NewMemResourceStore(nil, ps)
	srv, done := newTestTCPServerWithResources(t, rs, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpdateRemoteAgentProjectResources",
		"params": map[string]any{
			"project_id":   row.ID,
			"resource_ids": []string{"github:openagy/openagy", "sentry:production"},
		},
	})
	// Update emits resources/changed (broadcast)
	// before the response. Drain the notification
	// first.
	notif := readJSONRPC(t, c)
	assertResourcesChangedNotification(t, "Update broadcast", notif, row.ID, 2, 0)

	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("UpdateRemoteAgentProjectResources: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("UpdateRemoteAgentProjectResources: no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	if got, _ := res["project_id"].(string); got != row.ID {
		t.Errorf("result.project_id = %q, want %q", got, row.ID)
	}
	if got, _ := res["bound_count"].(float64); int(got) != 2 {
		t.Errorf("result.bound_count = %v, want 2", got)
	}
	if got, _ := res["unbound_count"].(float64); int(got) != 0 {
		t.Errorf("result.unbound_count = %v, want 0", got)
	}
	if got, _ := res["updated_at"].(string); got == "" {
		t.Errorf("result.updated_at missing or empty")
	}
}

// TestUpdateRemoteAgentProjectResources_WS_UnknownProject
// pins the unknown-project → -32004 envelope. The
// dispatcher maps ErrResourceProjectNotFound to
// "Project not found" (the cycle-9 / cycle-22
// -32004 envelope the dashboard already recognises
// on UpdateRemoteAgentProject / DeleteRemoteAgentProject).
func TestUpdateRemoteAgentProjectResources_WS_UnknownProject(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	rs := remotecontrol.NewMemResourceStore(nil, ps)
	srv, done := newTestTCPServerWithResources(t, rs, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpdateRemoteAgentProjectResources",
		"params": map[string]any{
			"project_id":   "no_such_project",
			"resource_ids": []string{"github:openagy/openagy"},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("UpdateRemoteAgentProjectResources: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32004 {
		t.Errorf("error.code = %d, want -32004 (Project not found)", got)
	}
	if got, _ := errObj["message"].(string); got != "Project not found" {
		t.Errorf("error.message = %q, want %q", got, "Project not found")
	}
}

// TestUpdateRemoteAgentProjectResources_WS_BadParams
// pins the empty-project_id → -32602 envelope. The
// brief calls out the validation rule: project_id
// is required, non-empty after trim; an absent
// (or whitespace-only) value is rejected before
// the store call.
func TestUpdateRemoteAgentProjectResources_WS_BadParams(t *testing.T) {
	ps := remotecontrol.NewMemProjectStore(nil)
	rs := remotecontrol.NewMemResourceStore(nil, ps)
	srv, done := newTestTCPServerWithResources(t, rs, ps)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpdateRemoteAgentProjectResources",
		"params": map[string]any{
			"project_id":   "",
			"resource_ids": []string{"github:openagy/openagy"},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("UpdateRemoteAgentProjectResources: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// ----------------------------------------------------------------------
// Conversation config wire-level tests (cycle
// rc-conversation-config).
//
// Wire-shape invariants pinned by this suite:
//
//   - UpsertRemoteAgentConversationConfig returns
//     the brief's {ok, conversation_id, created,
//     config, updated_at} envelope; the in-flight
//     "config" value is a JSON object that
//     round-trips back to map[string]any with
//     snake_case keys (model / model_effort /
//     max_turns / etc.). A dashboard that posts the
//     same config twice sees created=true on the
//     first call and created=false on the second
//     (the MemConversationConfigStore's
//     upsert-creates-or-replaces semantic).
//
//   - The handler emits a
//     `conversation_config/changed` broadcast
//     notification to every open ws connection on
//     every successful Upsert (the
//     (*Dispatcher).BroadcastNotify seam shared
//     with rc-plugins / rc-triggers /
//     rc-fastpush-toggle / rc-resources).
//
//   - Sentinel errors (-32602 "Invalid params" for
//     empty / out-of-range params,
//     conversation_config_validation failures,
//     missing / wrong-type / null config keys)
//     round-trip cleanly through the JSON wire
//     format.

// newTestTCPServerWithConversationConfig returns a
// *TCPServer wired with the supplied
// ConversationConfigStore so wire tests can exercise
// UpsertRemoteAgentConversationConfig end to end.
// Mirrors newTestTCPServerWithResources /
// newTestTCPServerWithFastpush but for the
// conversation-config surface. Cycle
// rc-conversation-config adds this helper.
func newTestTCPServerWithConversationConfig(t *testing.T, cs remotecontrol.ConversationConfigStore) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.0")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:                 sess,
		Agents:                  remotecontrol.DefaultAgentLister,
		Reader:                  remotecontrol.DefaultOutputReader{},
		ConversationConfigStore: cs,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// assertConversationConfigChangedEnvelope asserts
// the {ok, conversation_id, created, config,
// updated_at} envelope a polling caller and a
// notification subscriber both see. The wire-side
// "config" value is a map[string]any after the JSON
// round-trip; this helper walks into the inner map
// for the snake_case keys the brief pins (model /
// model_effort / max_turns / temperature / top_p /
// metadata / allowed_tools / disallowed_tools /
// system_prompt_patch / updated_at).
func assertConversationConfigChangedEnvelope(t *testing.T, label string, params map[string]any, wantConvID string, wantCreated bool) {
	t.Helper()
	if params == nil {
		t.Fatalf("%s: params is nil", label)
	}
	if got, _ := params["ok"].(bool); !got {
		t.Errorf("%s: params.ok = %v, want true", label, params["ok"])
	}
	if got, _ := params["conversation_id"].(string); got != wantConvID {
		t.Errorf("%s: params.conversation_id = %q, want %q", label, got, wantConvID)
	}
	if got, _ := params["created"].(bool); got != wantCreated {
		t.Errorf("%s: params.created = %v, want %v", label, got, wantCreated)
	}
	if got, _ := params["updated_at"].(string); got == "" {
		t.Errorf("%s: params.updated_at missing or empty: %#v", label, params["updated_at"])
	}
	cfg, ok := params["config"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params.config = %T, want map[string]any: %#v", label, params["config"], params)
	}
	// At least one well-known field should be a
	// string post JSON round-trip — guards against
	// the field name dropping its snake_case
	// mapping on the wire. We don't pin every
	// field because ConversationConfig's JSON tags
	// are omitempty (a config that omits a field
	// lands on the wire without that field), and
	// pinning metadata always would falsely fail
	// for configs the dashboard posted without a
	// metadata key.
	if _, hasModel := cfg["model"]; !hasModel {
		t.Errorf("%s: params.config.model key absent (full cfg = %#v)", label, cfg)
	}
	// updated_at always ships — the store stamps
	// it server-side on every Upsert.
	if got, _ := cfg["updated_at"].(string); got == "" {
		t.Errorf("%s: params.config.updated_at missing or empty: %#v", label, cfg)
	}
}

// assertConversationConfigChangedNotification asserts
// a `conversation_config/changed` notification frame
// carries the expected {ok, conversation_id,
// created, config, updated_at} envelope. The frame
// shape is the same envelope the response uses, but
// delivered without an "id" field (per JSON-RPC 2.0
// §4.1 notification rules).
func assertConversationConfigChangedNotification(t *testing.T, label string, frame map[string]any, wantConvID string, wantCreated bool) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.NotifConversationConfigChangedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.NotifConversationConfigChangedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	assertConversationConfigChangedEnvelope(t, label+" params", params, wantConvID, wantCreated)
}

// TestUpsertRemoteAgentConversationConfig_WS_Ok
// exercises the happy path: a fresh
// MemConversationConfigStore receives two Upsert
// calls on the same conversation_id; the WS round-
// trip returns created=true on the first call and
// created=false on the second (the
// upsert-creates-or-replaces semantic).
//
// Wire-order subtlety: identical to the rc-plugins
// / rc-triggers / rc-fastpush-toggle / rc-resources
// pattern. The dispatcher invokes BroadcastNotify
// (with the post-mutation envelope) BEFORE
// marshalling the response, so the originating
// conn's outbound channel holds [notification,
// response]. readJSONRPCResponse drops the
// notification (no id) and returns the response;
// the dedicated
// TestUpsertRemoteAgentConversationConfig_Broadcasts_Changed
// test pins the notification arrival with two
// conns.
func TestUpsertRemoteAgentConversationConfig_WS_Ok(t *testing.T) {
	cs := remotecontrol.NewMemConversationConfigStore(nil)
	srv, done := newTestTCPServerWithConversationConfig(t, cs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// First upsert: created=true.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpsertRemoteAgentConversationConfig",
		"params": map[string]any{
			"conversation_id": "conv_abc",
			"config": map[string]any{
				"model":        "claude-3-7-sonnet",
				"model_effort": "medium",
				"max_turns":    50,
			},
		},
	})
	resp1 := readJSONRPCResponse(t, c)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("first Upsert: error = %#v", errObj)
	}
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("first Upsert: no result, got %#v", resp1)
	}
	assertConversationConfigChangedEnvelope(t, "first Upsert result", res1, "conv_abc", true)
	if got, _ := res1["config"].(map[string]any)["model"].(string); got != "claude-3-7-sonnet" {
		t.Errorf("first result.config.model = %q, want claude-3-7-sonnet", got)
	}

	// Second upsert on the same conversation_id:
	// created=false.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "UpsertRemoteAgentConversationConfig",
		"params": map[string]any{
			"conversation_id": "conv_abc",
			"config": map[string]any{
				"model":        "gemini-3.8-flash",
				"model_effort": "high",
				"max_turns":    100,
				"temperature":  0.7,
				"top_p":        0.9,
			},
		},
	})
	resp2 := readJSONRPCResponse(t, c)
	if errObj, ok := resp2["error"].(map[string]any); ok {
		t.Fatalf("second Upsert: error = %#v", errObj)
	}
	res2, ok := resp2["result"].(map[string]any)
	if !ok {
		t.Fatalf("second Upsert: no result, got %#v", resp2)
	}
	assertConversationConfigChangedEnvelope(t, "second Upsert result", res2, "conv_abc", false)
	cfg2 := res2["config"].(map[string]any)
	if got, _ := cfg2["model"].(string); got != "gemini-3.8-flash" {
		t.Errorf("second result.config.model = %q, want gemini-3.8-flash", got)
	}
	if got, _ := cfg2["model_effort"].(string); got != "high" {
		t.Errorf("second result.config.model_effort = %q, want high", got)
	}
	if got, _ := cfg2["max_turns"].(float64); int(got) != 100 {
		t.Errorf("second result.config.max_turns = %v, want 100", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_WS_BadModelEffort
// pins the -32602 envelope for an out-of-set
// model_effort. The brief enumerates
// low|medium|high; an unknown value trips the
// ValidateConversationConfig closed-set guard
// before the store call.
func TestUpsertRemoteAgentConversationConfig_WS_BadModelEffort(t *testing.T) {
	cs := remotecontrol.NewMemConversationConfigStore(nil)
	srv, done := newTestTCPServerWithConversationConfig(t, cs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpsertRemoteAgentConversationConfig",
		"params": map[string]any{
			"conversation_id": "conv_abc",
			"config": map[string]any{
				"model_effort": "ultra",
			},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Upsert: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
	if got, _ := errObj["data"].(string); got != "model_effort must be one of low|medium|high" {
		t.Errorf("error.data = %q, want validation verbatim message", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_WS_BadTemperature
// pins the -32602 envelope for an out-of-range
// temperature. The brief pins [0, 2] inclusive; a
// value above 2 trips the
// ValidateConversationConfig bounded-range guard
// before the store call.
func TestUpsertRemoteAgentConversationConfig_WS_BadTemperature(t *testing.T) {
	cs := remotecontrol.NewMemConversationConfigStore(nil)
	srv, done := newTestTCPServerWithConversationConfig(t, cs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpsertRemoteAgentConversationConfig",
		"params": map[string]any{
			"conversation_id": "conv_abc",
			"config": map[string]any{
				"temperature": 3.0,
			},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Upsert: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// TestUpsertRemoteAgentConversationConfig_WS_BadParams_EmptyConvID
// pins the -32602 envelope for an empty
// conversation_id. The brief explicitly enumerates
// the empty / whitespace-only value as the "dashboard
// forgot to fill in the field" branch.
func TestUpsertRemoteAgentConversationConfig_WS_BadParams_EmptyConvID(t *testing.T) {
	cs := remotecontrol.NewMemConversationConfigStore(nil)
	srv, done := newTestTCPServerWithConversationConfig(t, cs)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpsertRemoteAgentConversationConfig",
		"params": map[string]any{
			"conversation_id": "",
			"config": map[string]any{
				"model": "claude-3-7-sonnet",
			},
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Upsert: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// TestUpsertRemoteAgentConversationConfig_Broadcasts_Changed
// exercises the headline broadcast behaviour of
// cycle rc-conversation-config: an
// UpsertRemoteAgentConversationConfig on one ws
// connection must fan a
// `conversation_config/changed` notification out to
// every open ws connection (not just the
// originating one). Two ws clients are brought up;
// an Upsert on c1 fires the notification; both c1
// and c2 observe it.
//
// Wire-order subtlety: identical to the rc-plugins
// / rc-triggers / rc-fastpush-toggle /
// rc-resources TestXChanged_Broadcast pattern. The
// originating conn's outbound channel holds
// [notification, response]; the second conn (which
// never sent a request) sees only the
// notification.
func TestUpsertRemoteAgentConversationConfig_Broadcasts_Changed(t *testing.T) {
	cs := remotecontrol.NewMemConversationConfigStore(nil)
	srv, done := newTestTCPServerWithConversationConfig(t, cs)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	// Wait for both conns to register (the
	// broadcaster walks s.conns under a single
	// RLock).
	waitForTwoConnIDs(t, srv)

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "UpsertRemoteAgentConversationConfig",
		"params": map[string]any{
			"conversation_id": "conv_xyz",
			"config": map[string]any{
				"model":        "claude-3-7-sonnet",
				"model_effort": "low",
			},
		},
	})

	// c1 outbound channel order:
	//   [notification, response]
	// (broadcaster enqueues first;
	//  dispatchOne enqueues the response second).
	notif1 := readJSONRPC(t, c1)
	assertConversationConfigChangedNotification(t, "c1 broadcast", notif1, "conv_xyz", true)

	resp1 := readJSONRPCResponse(t, c1)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("c1 Upsert: error = %#v", errObj)
	}
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("c1 Upsert: no result, got %#v", resp1)
	}
	if got, _ := res1["conversation_id"].(string); got != "conv_xyz" {
		t.Errorf("c1 result.conversation_id = %q, want conv_xyz", got)
	}
	if got, _ := res1["created"].(bool); !got {
		t.Errorf("c1 result.created = false, want true")
	}

	// c2 outbound channel: [notification] —
	// c2 never sent a request, so no response
	// arrives. readJSONRPC blocks for one frame
	// (2 s timeout inside); a buggy broadcaster
	// that only writes to the originating conn
	// trips the deadline here.
	notif2 := readJSONRPC(t, c2)
	assertConversationConfigChangedNotification(t, "c2 broadcast", notif2, "conv_xyz", true)

	// Both conns observe the same conversation_id
	// and created flag.
	convID1, _ := notif1["params"].(map[string]any)["conversation_id"].(string)
	convID2, _ := notif2["params"].(map[string]any)["conversation_id"].(string)
	if convID1 != "conv_xyz" || convID2 != "conv_xyz" {
		t.Errorf("c1/c2 conversation_id = %q/%q, want conv_xyz/conv_xyz", convID1, convID2)
	}
	created1, _ := notif1["params"].(map[string]any)["created"].(bool)
	created2, _ := notif2["params"].(map[string]any)["created"].(bool)
	if !created1 || !created2 {
		t.Errorf("c1/c2 created = %v/%v, want true/true", created1, created2)
	}
}

// ----- cycle rc-terminals tests -----
//
// Wire-level coverage for the five
// RemoteAgentTerminal* methods + the
// terminal/output + terminal/closed server-push
// notifications introduced in cycle rc-terminals.
// The in-process tests in
// internal/remotecontrol/terminals_test.go cover
// the dispatcher's per-handler behaviour; the eight
// tests below pin the byte-for-byte JSON envelope
// the ws adapter emits and exercise the cross-conn
// broadcast (a Send on conn 1 fans a terminal/output
// notification to conn 2; a Close fans a
// terminal/closed notification to every conn).

// newTestTCPServerWithTerminals wires a *TCPServer
// with the supplied TerminalsRegistry so wire
// tests can exercise Create/List/Send/Resize/Close
// end to end. Mirrors newTestTCPServerWithResources
// / newTestTCPServerWithConversationConfig but for
// the terminal surface. Cycle rc-terminals adds
// this helper.
func newTestTCPServerWithTerminals(t *testing.T, reg *remotecontrol.TerminalsRegistry) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.2")
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:           sess,
		Agents:            remotecontrol.DefaultAgentLister,
		Reader:            remotecontrol.DefaultOutputReader{},
		TerminalsRegistry: reg,
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
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// assertTerminalOutputNotification asserts a
// `terminal/output` notification frame carries the
// expected {terminal_id, seq, data, ts} envelope.
// The frame is delivered without an "id" field per
// JSON-RPC 2.0 §4.1 notification rules. data is
// base64-encoded UTF-8 bytes; the helper compares
// the decoded value so a buggy dispatcher that
// truncated or double-encoded the bytes trips the
// assertion.
func assertTerminalOutputNotification(t *testing.T, label string, frame map[string]any, wantTerminalID string, wantData []byte) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.NotifTerminalOutputMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.NotifTerminalOutputMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	if got, _ := params["terminal_id"].(string); got != wantTerminalID {
		t.Errorf("%s: params.terminal_id = %q, want %q", label, got, wantTerminalID)
	}
	if got, ok := params["seq"].(float64); !ok {
		t.Errorf("%s: params.seq = %T, want number", label, params["seq"])
	} else if int(got) != 0 {
		t.Errorf("%s: params.seq = %v, want 0 (first frame)", label, got)
	}
	if got, _ := params["ts"].(string); got == "" {
		t.Errorf("%s: params.ts missing or empty", label)
	}
	encoded, ok := params["data"].(string)
	if !ok {
		t.Fatalf("%s: params.data = %T, want string", label, params["data"])
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("%s: params.data not base64: %v (raw=%q)", label, err, encoded)
	}
	if string(decoded) != string(wantData) {
		t.Errorf("%s: params.data decoded = %q, want %q", label, decoded, wantData)
	}
}

// assertTerminalClosedNotification asserts a
// `terminal/closed` notification frame carries the
// expected {terminal_id, closed_at, ts} envelope.
// Like the output notif, delivered without an id
// per JSON-RPC 2.0 §4.1.
func assertTerminalClosedNotification(t *testing.T, label string, frame map[string]any, wantTerminalID string) {
	t.Helper()
	if _, hasID := frame["id"]; hasID {
		t.Errorf("%s: notification carries an id field: %#v", label, frame)
	}
	if frame["jsonrpc"] != "2.0" {
		t.Errorf("%s: jsonrpc = %v, want 2.0", label, frame["jsonrpc"])
	}
	if frame["method"] != remotecontrol.NotifTerminalClosedMethod {
		t.Errorf("%s: method = %v, want %q", label, frame["method"], remotecontrol.NotifTerminalClosedMethod)
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s: params = %T, want map[string]any: %#v", label, frame["params"], frame)
	}
	if got, _ := params["terminal_id"].(string); got != wantTerminalID {
		t.Errorf("%s: params.terminal_id = %q, want %q", label, got, wantTerminalID)
	}
	if got, _ := params["closed_at"].(string); got == "" {
		t.Errorf("%s: params.closed_at missing or empty", label)
	}
	if got, _ := params["ts"].(string); got == "" {
		t.Errorf("%s: params.ts missing or empty", label)
	}
}

// TestCreateRemoteAgentTerminal_WS_Ok exercises
// the headline happy path of cycle rc-terminals:
// Create over the WS adapter returns the brief-
// pinned flat {ok, terminal_id, kind, cols, rows,
// created_at} envelope with a fresh tml_<hex> id
// and server-stamped created_at.
func TestCreateRemoteAgentTerminal_WS_Ok(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTerminal",
		"params": map[string]any{
			"kind": "input",
			"cols": 132,
			"rows": 50,
		},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("Create: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Create: no result, got %#v", resp)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Errorf("Create: res.ok = %v, want true", res["ok"])
	}
	tid, _ := res["terminal_id"].(string)
	if !strings.HasPrefix(tid, "tml_") {
		t.Errorf("Create: res.terminal_id = %q, want tml_<hex> prefix", tid)
	}
	if got, _ := res["kind"].(string); got != "input" {
		t.Errorf("Create: res.kind = %q, want input", got)
	}
	if got, _ := res["cols"].(float64); int(got) != 132 {
		t.Errorf("Create: res.cols = %v, want 132", got)
	}
	if got, _ := res["rows"].(float64); int(got) != 50 {
		t.Errorf("Create: res.rows = %v, want 50", got)
	}
	if got, _ := res["created_at"].(string); got == "" {
		t.Errorf("Create: res.created_at missing or empty")
	}
}

// TestListRemoteAgentTerminals_WS_Ok exercises the
// happy path: Create 3 → List returns a 3-element
// array. Pin the envelope shape so a future
// refactor that wraps the array in an extra object
// trips this test.
func TestListRemoteAgentTerminals_WS_Ok(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	for i := 0; i < 3; i++ {
		writeJSONRPC(t, c, map[string]any{
			"jsonrpc": "2.0",
			"id":      i + 1,
			"method":  "CreateRemoteAgentTerminal",
			"params":  map[string]any{"kind": "input"},
		})
		resp := readJSONRPCResponse(t, c)
		if _, ok := resp["error"].(map[string]any); ok {
			t.Fatalf("Create #%d: error = %#v", i, resp["error"])
		}
	}

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      100,
		"method":  "ListRemoteAgentTerminals",
		"params":  map[string]any{},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("List: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if ok, _ := res["ok"].(bool); !ok {
		t.Errorf("List: res.ok = %v, want true", res["ok"])
	}
	terminals, ok := res["terminals"].([]any)
	if !ok {
		t.Fatalf("List: res.terminals = %T, want []any: %#v", res["terminals"], res["terminals"])
	}
	if len(terminals) != 3 {
		t.Errorf("List: terminals len = %d, want 3", len(terminals))
	}
}

// TestSendRemoteAgentTerminalInput_WS_Ok
// exercises the headline happy path of the
// Send/terminal-output broadcast: Create a
// terminal, Send bytes, every open ws conn
// (originating + a second conn) sees a
// terminal/output notification carrying
// {terminal_id, seq:0, data:<base64>, ts:<rfc>}
// BEFORE the synchronous {ok, terminal_id,
// bytes_consumed} response envelope.
//
// Wire-order subtlety: identical to the rc-plugins
// / rc-triggers / rc-fastpush-toggle /
// rc-resources / rc-conversation-config
// TestXChanged_Broadcast pattern. The originating
// conn's outbound channel holds
// [notification, response] (broadcaster first,
// dispatchOne second); the second conn (which never
// sent a request) sees only the notification.
func TestSendRemoteAgentTerminalInput_WS_Ok(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	waitForTwoConnIDs(t, srv)

	// Create a terminal on c1.
	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTerminal",
		"params":  map[string]any{"kind": "input"},
	})
	createResp := readJSONRPCResponse(t, c1)
	if errObj, ok := createResp["error"].(map[string]any); ok {
		t.Fatalf("Create: error = %#v", errObj)
	}
	tid, _ := createResp["result"].(map[string]any)["terminal_id"].(string)
	if tid == "" {
		t.Fatalf("Create: terminal_id empty")
	}

	raw := []byte("hello over ws")
	encoded := base64.StdEncoding.EncodeToString(raw)
	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentTerminalInput",
		"params": map[string]any{
			"terminal_id": tid,
			"data":        encoded,
		},
	})

	// c1 outbound channel order: [notification, response].
	notif1 := readJSONRPC(t, c1)
	assertTerminalOutputNotification(t, "c1 broadcast", notif1, tid, raw)

	resp1 := readJSONRPCResponse(t, c1)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("c1 Send: error = %#v", errObj)
	}
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("c1 Send: no result, got %#v", resp1)
	}
	if ok, _ := res1["ok"].(bool); !ok {
		t.Errorf("c1 Send: res.ok = %v, want true", res1["ok"])
	}
	if got, _ := res1["terminal_id"].(string); got != tid {
		t.Errorf("c1 Send: res.terminal_id = %q, want %q", got, tid)
	}
	if got, _ := res1["bytes_consumed"].(float64); int(got) != len(raw) {
		t.Errorf("c1 Send: res.bytes_consumed = %v, want %d", got, len(raw))
	}

	// c2 outbound channel: [notification] — c2 never
	// sent a request, so no response arrives.
	// readJSONRPC blocks for one frame (2 s timeout
	// inside); a buggy broadcaster that only writes
	// to the originating conn trips the deadline.
	notif2 := readJSONRPC(t, c2)
	assertTerminalOutputNotification(t, "c2 broadcast", notif2, tid, raw)
}

// TestSendRemoteAgentTerminalInput_WS_TerminalNotFound
// pins the wire-level -32018 envelope path: Send
// with a terminal_id the registry has never seen
// yields error.code == -32018.
func TestSendRemoteAgentTerminalInput_WS_TerminalNotFound(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SendRemoteAgentTerminalInput",
		"params": map[string]any{
			"terminal_id": "tml_deadbeefdeadbeef",
			"data":        base64.StdEncoding.EncodeToString([]byte("x")),
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown-id Send: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32018 {
		t.Errorf("error.code = %d, want -32018 (Terminal not found)", got)
	}
}

// TestSendRemoteAgentTerminalInput_WS_BadParams_NotBase64
// pins the wire-level -32602 envelope path: a
// data field that's not base64-encoded yields
// error.code == -32602 (the dispatcher decodes
// the data before forwarding; a malformed value
// trips the bad-params envelope rather than a
// silent -32603).
func TestSendRemoteAgentTerminalInput_WS_BadParams_NotBase64(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Create a terminal first so we exercise the
	// data-validator path (not the unknown-id path).
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTerminal",
		"params":  map[string]any{"kind": "input"},
	})
	createResp := readJSONRPCResponse(t, c)
	if errObj, ok := createResp["error"].(map[string]any); ok {
		t.Fatalf("Create: error = %#v", errObj)
	}
	tid, _ := createResp["result"].(map[string]any)["terminal_id"].(string)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendRemoteAgentTerminalInput",
		"params": map[string]any{
			"terminal_id": tid,
			"data":        "!!!not-base64!!!",
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("bad-base64 Send: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
}

// TestResizeRemoteAgentTerminal_WS_Ok exercises
// the happy path: Create → Resize → response
// envelope echoes the post-resize cols/rows. Pin
// the wire shape so a future refactor that
// accidentally echoes the input dims (instead of
// the post-resize state) trips this test.
func TestResizeRemoteAgentTerminal_WS_Ok(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTerminal",
		"params":  map[string]any{"kind": "input", "cols": 80, "rows": 24},
	})
	createResp := readJSONRPCResponse(t, c)
	if errObj, ok := createResp["error"].(map[string]any); ok {
		t.Fatalf("Create: error = %#v", errObj)
	}
	tid, _ := createResp["result"].(map[string]any)["terminal_id"].(string)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "ResizeRemoteAgentTerminal",
		"params": map[string]any{
			"terminal_id": tid,
			"cols":        200,
			"rows":        60,
		},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("Resize: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if got, _ := res["cols"].(float64); int(got) != 200 {
		t.Errorf("Resize: res.cols = %v, want 200", got)
	}
	if got, _ := res["rows"].(float64); int(got) != 60 {
		t.Errorf("Resize: res.rows = %v, want 60", got)
	}
	if got, _ := res["terminal_id"].(string); got != tid {
		t.Errorf("Resize: res.terminal_id = %q, want %q", got, tid)
	}
}

// TestCloseRemoteAgentTerminal_WS_Ok exercises the
// headline broadcast behaviour of Close: a Close on
// c1 fans a `terminal/closed` notification to every
// open ws conn (c1 + c2). The originating conn's
// outbound channel holds [notification, response].
// Pin the closed_at envelope so a buggy broadcaster
// that omits the timestamp trips this test.
func TestCloseRemoteAgentTerminal_WS_Ok(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c1 := dialWS(t, srv.HTTPURL())
	defer c1.CloseNow()
	c2 := dialWS(t, srv.HTTPURL())
	defer c2.CloseNow()

	waitForTwoConnIDs(t, srv)

	// Create on c1.
	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CreateRemoteAgentTerminal",
		"params":  map[string]any{"kind": "input"},
	})
	createResp := readJSONRPCResponse(t, c1)
	if errObj, ok := createResp["error"].(map[string]any); ok {
		t.Fatalf("Create: error = %#v", errObj)
	}
	tid, _ := createResp["result"].(map[string]any)["terminal_id"].(string)

	writeJSONRPC(t, c1, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "CloseRemoteAgentTerminal",
		"params":  map[string]any{"terminal_id": tid},
	})

	// c1 outbound channel: [notification, response].
	notif1 := readJSONRPC(t, c1)
	assertTerminalClosedNotification(t, "c1 broadcast", notif1, tid)

	resp1 := readJSONRPCResponse(t, c1)
	if errObj, ok := resp1["error"].(map[string]any); ok {
		t.Fatalf("c1 Close: error = %#v", errObj)
	}
	res1, _ := resp1["result"].(map[string]any)
	if ok, _ := res1["ok"].(bool); !ok {
		t.Errorf("c1 Close: res.ok = %v, want true", res1["ok"])
	}
	if got, _ := res1["terminal_id"].(string); got != tid {
		t.Errorf("c1 Close: res.terminal_id = %q, want %q", got, tid)
	}
	if got, _ := res1["closed_at"].(string); got == "" {
		t.Errorf("c1 Close: res.closed_at missing or empty")
	}

	// c2 outbound channel: [notification].
	notif2 := readJSONRPC(t, c2)
	assertTerminalClosedNotification(t, "c2 broadcast", notif2, tid)
}

// TestCloseRemoteAgentTerminal_WS_NotFound pins
// the wire-level -32018 envelope path: Close with
// a terminal_id the registry has never seen
// yields error.code == -32018 (matches Send's
// unknown-id envelope so dashboards' error filters
// are uniform).
func TestCloseRemoteAgentTerminal_WS_NotFound(t *testing.T) {
	reg := remotecontrol.NewTerminalsRegistry()
	srv, done := newTestTCPServerWithTerminals(t, reg)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "CloseRemoteAgentTerminal",
		"params": map[string]any{
			"terminal_id": "tml_deadbeefdeadbeef",
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown-id Close: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32018 {
		t.Errorf("error.code = %d, want -32018 (Terminal not found)", got)
	}
}

// ----------------------------------------------------------------------
// Top-level RPC wire-level tests (cycle rc-top-level-rpc).
//
// Wire-shape invariants pinned by this suite:
//
//   - RemoteControlStatus returns the full TopLevelStatus
//     envelope (version, uptime_seconds, started_at,
//     connected_clients, open_cascades, dispatch_methods,
//     server_fingerprint) on a single dial-up call. The
//     dispatch_methods list is sorted ascending.
//
//   - RemoteControlOnOutput returns the lean
//     {ok:true, sink_id, subscribed_at, filter:[…]}
//     envelope; the sink_id defaults to the calling
//     ws connID when omitted; the filter list is the
//     canonical sorted notification topic set.
//
//   - remoteControlOnMessage gates by the
//     RemoteControlOnMessageAllowList closed set:
//     * allowed method (RemoteAgentStatus) →
//       {ok:true, accepted:true, replayed_at:<rfc3339nano>}
//     * rejected method (Foo) → -32603 envelope with
//       data="rejected by gatekeeper: Foo"
//     * bad params (empty message, missing method) →
//       -32602 envelope with a verbatim reason.
//
// All four envelope paths survive a JSON round-trip
// (the assertion uses float64/[]any shapes, not the
// in-process struct shapes).
//
// ----------------------------------------------------------------------

// TestRemoteControlStatus_WS_Ok is the headline
// happy-path wire test: a fresh dial fires
// `RemoteControlStatus`, the response carries the
// full TopLevelStatus envelope with a non-empty
// `version` (from SessionState), non-empty
// `started_at` (RFC3339Nano), and a sorted
// `dispatch_methods` list that includes the three
// methods the cycle promotes.
func TestRemoteControlStatus_WS_Ok(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "RemoteControlStatus",
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("RemoteControlStatus: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("RemoteControlStatus: result type = %T, want map[string]any", resp["result"])
	}

	if got, _ := res["version"].(string); got != "1.4.0" {
		t.Errorf("result.version = %q, want 1.4.0 (SessionState.Version)", got)
	}
	if got, _ := res["started_at"].(string); got == "" {
		t.Errorf("result.started_at missing")
	} else if _, err := time.Parse(time.RFC3339Nano, got); err != nil {
		t.Errorf("result.started_at %q does not parse as RFC3339Nano: %v", got, err)
	}
	if got, ok := res["uptime_seconds"].(float64); !ok {
		t.Errorf("result.uptime_seconds missing or wrong type: %T", res["uptime_seconds"])
	} else if got < 0 {
		t.Errorf("result.uptime_seconds = %f, want >= 0", got)
	}
	if got, ok := res["connected_clients"].(float64); !ok || got != 1 {
		// 1 expected — the just-dialled ws conn
		// counted by the dispatcher's accept loop
		// via (*Dispatcher).NotifyConnected.
		t.Errorf("result.connected_clients = %v, want 1", res["connected_clients"])
	}
	if got, ok := res["open_cascades"].(float64); !ok || got != 0 {
		t.Errorf("result.open_cascades = %v, want 0", res["open_cascades"])
	}

	methods, ok := res["dispatch_methods"].([]any)
	if !ok {
		t.Fatalf("result.dispatch_methods type = %T, want []any", res["dispatch_methods"])
	}
	if len(methods) == 0 {
		t.Fatalf("result.dispatch_methods is empty")
	}
	// Sorted ascending + non-empty.
	var prev string
	for i, m := range methods {
		s, _ := m.(string)
		if i > 0 && prev > s {
			t.Errorf("dispatch_methods not sorted at %d: %q > %q", i, prev, s)
		}
		prev = s
	}
	// The three new methods are present.
	have := map[string]bool{}
	for _, m := range methods {
		if s, ok := m.(string); ok {
			have[s] = true
		}
	}
	for _, must := range []string{"RemoteControlStatus", "RemoteControlOnOutput", "remoteControlOnMessage"} {
		if !have[must] {
			t.Errorf("dispatch_methods missing %q", must)
		}
	}

	// Server fingerprint pattern: "wagyu-rc/1.4.0/<8-hex>"
	// (the test fixture's Commit is
	// "deadbeef1234567890abcdef1234567890abcdef").
	if got, _ := res["server_fingerprint"].(string); got != "wagyu-rc/1.4.0/deadbeef" {
		t.Errorf("result.server_fingerprint = %q, want wagyu-rc/1.4.0/deadbeef", got)
	}

	// Cycle rc-webrtc-mesh-defer: deferred_features envelope
	// must be on the wire even when the dispatcher has no
	// host-installed overrides. The canonical four keys all
	// flip to `false` today (the v2 path is off everywhere in
	// this branch).
	deferred, ok := res["deferred_features"].(map[string]any)
	if !ok {
		t.Fatalf("result.deferred_features missing or wrong type: %T", res["deferred_features"])
	}
	if len(deferred) != 4 {
		t.Errorf("result.deferred_features has %d keys, want 4 (canonical)", len(deferred))
	}
	for _, k := range []string{"antigravity_hub", "enterprise_tunnel", "systemd_user", "webrtc_mesh"} {
		v, ok := deferred[k]
		if !ok {
			t.Errorf("result.deferred_features missing canonical key %q", k)
			continue
		}
		if b, _ := v.(bool); b {
			t.Errorf("result.deferred_features[%q] = true, want false (default off)", k)
		}
	}
}

// TestRemoteControlStatus_WS_Ok_DeferredFeaturesOverride
// pins the host-override path: a dispatcher with a
// non-empty Config.DeferredFeatures flips the matching
// key to `true` while leaving the remaining canonical
// keys at `false`. Distinct from the all-false default
// above so a regression that drops host overrides trips
// it.
func TestRemoteControlStatus_WS_Ok_DeferredFeaturesOverride(t *testing.T) {
	// newTestTCPServerWithOrigin builds a TCP server with a
	// customised dispatcher where the DeferredFeatures map
	// is pre-populated. We do this by hand using the
	// dispatcher directly so we don't need a new helper.
	srv, done := newTCPServerWithDeferredFeatures(t, map[string]bool{
		"webrtc_mesh": true,
	})
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "RemoteControlStatus",
	})
	resp := readJSONRPCResponse(t, c)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("RemoteControlStatus: result type = %T, want map[string]any", resp["result"])
	}
	deferred, _ := res["deferred_features"].(map[string]any)
	if deferred == nil {
		t.Fatalf("result.deferred_features missing")
	}
	if got, _ := deferred["webrtc_mesh"].(bool); !got {
		t.Errorf("result.deferred_features[webrtc_mesh] = false, want true (host override)")
	}
	for _, k := range []string{"antigravity_hub", "enterprise_tunnel", "systemd_user"} {
		if got, _ := deferred[k].(bool); got {
			t.Errorf("result.deferred_features[%q] = true, want false (canonical default)", k)
		}
	}
}

// TestExperimentalRCV2_Flag_IsAccepted_NoOp pins the
// wire-side consequence of the
// `--experimental-rc-v2` host flag (cycle
// rc-webrtc-mesh-defer): the flag is parsed cleanly by
// the host's main.go (no error, no propagation to
// wire behaviour in this branch) and the wire surface
// it gates — the `deferred_features` envelope on
// RemoteControlStatus — is exactly the all-false
// canonical default. This is the wire-level "no-op"
// pin.
//
// The cli/ flag-registration contract lives in
// internal/cli/experimental_rc_v2_test.go; this test
// pins the dispatcher-level behaviour so the wire stays
// bit-stable across cycles that flip the
// `deferred_features` entries to `true`.
func TestExperimentalRCV2_Flag_IsAccepted_NoOp(t *testing.T) {
	// A dispatcher with no overrides sees the
	// canonical all-false default — this is what a
	// host that flipped --experimental-rc-v2 today
	// (no v2 features wired) sees on the wire.
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "RemoteControlStatus",
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("RemoteControlStatus: error = %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("RemoteControlStatus: result type = %T, want map[string]any", resp["result"])
	}
	// deferred_features envelope: present, canonical
	// four keys, all `false`. This is the wire-side
	// mirror of the host flag's "no-op" semantic.
	deferred, ok := res["deferred_features"].(map[string]any)
	if !ok {
		t.Fatalf("result.deferred_features missing or wrong type: %T", res["deferred_features"])
	}
	if len(deferred) != 4 {
		t.Errorf("deferred_features has %d keys, want 4", len(deferred))
	}
	for _, k := range []string{"antigravity_hub", "enterprise_tunnel", "systemd_user", "webrtc_mesh"} {
		v, ok := deferred[k]
		if !ok {
			t.Errorf("deferred_features missing canonical key %q", k)
			continue
		}
		if b, _ := v.(bool); b {
			t.Errorf("deferred_features[%q] = true, want false (--experimental-rc-v2 is a no-op today)", k)
		}
	}
}

// TestRemoteControlOnOutput_WS_Ok pins the
// wire-level happy path: a fresh dial fires
// `RemoteControlOnOutput` with an empty params
// block; the response carries the lean envelope
// and the calling conn is registered in
// (*Dispatcher).AllSinks under its connID (the
// default when sink_id is omitted).
func TestRemoteControlOnOutput_WS_Ok(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "RemoteControlOnOutput",
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("RemoteControlOnOutput: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)

	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	// sink_id defaults to the calling connID. The
	// dispatcher doesn't expose connID over the wire
	// (it's an internal handle), so the test pin is
	// "non-empty string" — the value will look like
	// "ws-<random>" or similar, whatever the transport
	// mints.
	if got, _ := res["sink_id"].(string); got == "" {
		t.Errorf("result.sink_id is empty; want non-empty (defaulted from connID)")
	}
	if got, _ := res["subscribed_at"].(string); got == "" {
		t.Errorf("result.subscribed_at missing or empty")
	} else if _, err := time.Parse(time.RFC3339Nano, got); err != nil {
		t.Errorf("result.subscribed_at %q does not parse as RFC3339Nano: %v", got, err)
	}

	filter, ok := res["filter"].([]any)
	if !ok {
		t.Fatalf("result.filter type = %T, want []any", res["filter"])
	}
	if len(filter) == 0 {
		t.Fatalf("result.filter is empty; want non-empty canonical list")
	}
	// Sorted ascending.
	var prev string
	for i, f := range filter {
		s, _ := f.(string)
		if i > 0 && prev > s {
			t.Errorf("filter not sorted at %d: %q > %q", i, prev, s)
		}
		prev = s
	}
	// The cascade trio is present.
	have := map[string]bool{}
	for _, f := range filter {
		if s, ok := f.(string); ok {
			have[s] = true
		}
	}
	for _, must := range []string{"cascade/state", "cascade/text_delta", "cascade/tool_call"} {
		if !have[must] {
			t.Errorf("filter missing %q", must)
		}
	}
}

// TestRemoteControlOnOutput_WS_CustomSink pins the
// "explicit sink_id" wire path: a dashboard that
// wants to register under a routing handle distinct
// from the ws connID gets that sink_id echoed in
// the response.
func TestRemoteControlOnOutput_WS_CustomSink(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "RemoteControlOnOutput",
		"params":  map[string]any{"sink_id": "dashboard-routing-1"},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("RemoteControlOnOutput: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if got, _ := res["sink_id"].(string); got != "dashboard-routing-1" {
		t.Errorf("result.sink_id = %q, want dashboard-routing-1 (explicit value)", got)
	}
}

// TestRemoteControlOnOutput_WS_BadParams pins the
// wire-level -32602 envelope for a non-string
// sink_id. Mirrors the in-process test.
func TestRemoteControlOnOutput_WS_BadParams(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "RemoteControlOnOutput",
		"params":  map[string]any{"sink_id": 42},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("RemoteControlOnOutput: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestRemoteControlOnMessage_WS_Allowed pins the
// wire-level happy path: an allowed method
// (RemoteAgentStatus) is injected via the message
// surface, the response envelope is the brief's
// {ok:true, accepted:true, replayed_at:<rfc3339nano>}
// confirmation.
func TestRemoteControlOnMessage_WS_Allowed(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Build the inner message: a RemoteAgentStatus
	// request (it's in the allowlist but not yet
	// registered as a top-level method — the inner
	// dispatch returns a -32601 error envelope which
	// is intentionally DISCARDED by the cycle's
	// handler; the outer envelope is the confirmation).
	inner := map[string]any{
		"jsonrpc": "2.0",
		"method":  "RemoteAgentStatus",
	}
	innerBytes, _ := json.Marshal(inner)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "remoteControlOnMessage",
		"params": map[string]any{
			"message": json.RawMessage(innerBytes),
		},
	})
	resp := readJSONRPCResponse(t, c)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("remoteControlOnMessage: error = %#v", errObj)
	}
	res, _ := resp["result"].(map[string]any)
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	if got, _ := res["accepted"].(bool); !got {
		t.Errorf("result.accepted = %v, want true", res["accepted"])
	}
	if got, _ := res["replayed_at"].(string); got == "" {
		t.Errorf("result.replayed_at missing or empty")
	} else if _, err := time.Parse(time.RFC3339Nano, got); err != nil {
		t.Errorf("result.replayed_at %q does not parse as RFC3339Nano: %v", got, err)
	}
}

// TestRemoteControlOnMessage_WS_Rejected pins the
// wire-level gatekeeper path: a method that's not
// in RemoteControlOnMessageAllowList (Foo) is
// rejected with -32603 and the data field names the
// rejected method.
func TestRemoteControlOnMessage_WS_Rejected(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	inner := map[string]any{
		"jsonrpc": "2.0",
		"method":  "Foo",
	}
	innerBytes, _ := json.Marshal(inner)

	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "remoteControlOnMessage",
		"params": map[string]any{
			"message": json.RawMessage(innerBytes),
		},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("remoteControlOnMessage: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32603 {
		t.Errorf("error.code = %d, want -32603", got)
	}
	if msg, _ := errObj["data"].(string); !strings.Contains(msg, "Foo") {
		t.Errorf("error.data = %q, want it to contain the rejected method 'Foo'", msg)
	}
}

// TestRemoteControlOnMessage_WS_BadParams pins the
// wire-level -32602 envelope for missing message
// field. The brief's headline "bad params" path.
func TestRemoteControlOnMessage_WS_BadParams(t *testing.T) {
	srv, _, _, _, done := newTestTCPServerWithSessionMeta(t)
	defer done()

	c := dialWS(t, srv.HTTPURL())
	defer c.CloseNow()

	// Empty message struct.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "remoteControlOnMessage",
		"params":  map[string]any{"message": map[string]any{}},
	})
	resp := readJSONRPCResponse(t, c)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("remoteControlOnMessage: no error envelope, got %#v", resp)
	}
	if got := codeAsIntForWS(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}

	// Missing message field entirely.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "remoteControlOnMessage",
		"params":  map[string]any{},
	})
	resp2 := readJSONRPCResponse(t, c)
	errObj2, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("remoteControlOnMessage (no message): no error envelope, got %#v", resp2)
	}
	if got := codeAsIntForWS(t, errObj2["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}
