// Tests for the openagy recovered --remote-control daemon.
//
// All tests run against a temp-dir Unix socket so they don't
// require root, don't collide with another instance, and clean
// themselves up via Shutdown.
package remotecontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubReader is a hand-rolled OutputReader for tests. TurnOutputs
// maps id → text; NotFound returns ErrNotFound.
type stubReader struct {
	mu        sync.Mutex
	Turns     map[string]string
	NotFound  map[string]bool  // ids that should return ErrNotFound
	ErrOn     map[string]error // ids that should return err verbatim
	callCount atomic.Int64
}

func (s *stubReader) ReadTurn(ctx context.Context, id string) (string, error) {
	s.callCount.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ErrOn != nil {
		if e, ok := s.ErrOn[id]; ok {
			return "", e
		}
	}
	if s.NotFound != nil && s.NotFound[id] {
		return "", ErrNotFound
	}
	if t, ok := s.Turns[id]; ok {
		return t, nil
	}
	return "", ErrNotFound
}

// newTestDaemon brings up a daemon on a private socket and returns
// it + a teardown. The session state is built so that
// get_output / status have something to report.
//
// macOS's Unix-socket path is capped at 104 bytes (sun_path), so
// the temp dir's default 60+ char path makes Start fail. We use
// MkdirTemp under $TMPDIR with a short prefix to keep the path
// short enough.
func newTestDaemon(t *testing.T, agents AgentLister, reader OutputReader) (*Daemon, *SessionState, func()) {
	t.Helper()
	base := shortTmpDir(t)
	sock := filepath.Join(base, "rc.sock")

	sess := NewSessionState("1.3.4-test")
	sess.Workspace = "/tmp/workspace"
	sess.Backend = "gateway"
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.SetConversationID("conv-abc")
	sess.SetTurnOutput("hello from the host")

	d, err := Start(context.Background(), Config{
		SocketPath: sock,
		Session:    sess,
		Agents:     agents,
		Reader:     reader,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	teardown := func() { d.Shutdown() }
	return d, sess, teardown
}

// shortTmpDir returns a fresh directory under $TMPDIR with a
// short name so the resulting socket path stays under macOS's
// 104-byte sun_path limit. On Linux the limit is 108 bytes —
// still safe to honour the macOS limit.
func shortTmpDir(t *testing.T) string {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("TMPDIR"))
	if base == "" {
		base = os.TempDir()
	}
	name := fmt.Sprintf("rc%d", os.Getpid())
	dir := filepath.Join(base, name)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clean %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// dial opens a Unix socket connection, returning (reader, writer,
// closer) for sending newline-delimited JSON-RPC and reading
// newline-delimited responses.
func dial(t *testing.T, sock string) (*bufio.Scanner, *json.Encoder, net.Conn) {
	t.Helper()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	rd := bufio.NewReader(c)
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(c)
	return sc, enc, c
}

// call sends one request and decodes the response into out. The
// id is hard-coded to "1" for simplicity; tests don't depend on
// matching id fields.
func call(t *testing.T, sc *bufio.Scanner, enc *json.Encoder, method string, params any) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !sc.Scan() {
		t.Fatalf("scan: %v", sc.Err())
	}
	var resp map[string]any
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp
}

// TestStartCreatesSocket verifies --remote-control brings up a
// daemon that listens on a Unix socket: after Start returns, the
// file must exist and a Dial must succeed.
func TestStartCreatesSocket(t *testing.T) {
	d, _, done := newTestDaemon(t, nil, nil)
	defer done()

	if _, err := os.Stat(d.SocketPath()); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.Close()
}

// TestStartDefaultSocketPath verifies the daemon picks a sensible
// default socket path (in os.TempDir) when Config.SocketPath is
// empty. Used by main.go so an operator doesn't have to pass the
// flag explicitly.
func TestStartDefaultSocketPath(t *testing.T) {
	sess := NewSessionState("")
	d, err := Start(context.Background(), Config{Session: sess})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Shutdown()
	if !strings.HasPrefix(d.SocketPath(), os.TempDir()) {
		t.Errorf("socket %q not under os.TempDir() %q", d.SocketPath(), os.TempDir())
	}
	if !strings.Contains(d.SocketPath(), fmt.Sprintf("-%d.sock", os.Getpid())) {
		t.Errorf("socket %q should embed the PID", d.SocketPath())
	}
}

// TestStartRefusesBadPath verifies Config validation rejects
// relative socket paths, which would otherwise silently create
// the socket in the daemon's CWD.
func TestStartRefusesBadPath(t *testing.T) {
	sess := NewSessionState("")
	if _, err := Start(context.Background(), Config{
		SocketPath: "relative.sock",
		Session:    sess,
	}); err == nil {
		t.Fatalf("Start with relative socket path should fail")
	}
}

// TestInitialize covers the MCP handshake. The response must
// carry the protocolVersion and serverInfo we advertise, plus a
// tools capability so dashboards know we expose tools.
func TestInitialize(t *testing.T) {
	d, _, done := newTestDaemon(t, nil, nil)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "dashboard", "version": "0.1.0"},
	})
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: no result, got %#v", resp)
	}
	if res["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", res["protocolVersion"])
	}
	srv, _ := res["serverInfo"].(map[string]any)
	if srv["name"] != ServerName {
		t.Errorf("serverInfo.name = %v, want %q", srv["name"], ServerName)
	}
	if _, ok := res["capabilities"].(map[string]any); !ok {
		t.Errorf("capabilities missing in initialize response")
	}
}

// TestToolsList covers the three tools exposed by the changelog:
// status, list_agents, get_output. Names must match exactly.
func TestToolsList(t *testing.T) {
	d, _, done := newTestDaemon(t, nil, nil)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/list", nil)
	res, _ := resp["result"].(map[string]any)
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
			t.Errorf("tool %q missing from tools/list", name)
		}
	}
}

// TestStatusToolReturnsSnapshot exercises the status tool end to
// end, against the rich shape cycle rc-status-tool ships.
//
// Cycle 1 pinned the cycle-1 fields (pid / version / model /
// workspace). Cycle rc-status-tool extended the shape with
// `started_at` / `uptime` / `connected_clients` / a `cascades`
// block / an `auth` block / a `daemon` block. The session's PID,
// model, workspace, and version still round-trip; the new fields
// must all be present and structurally valid (block types are
// maps with the documented keys; cascades.total == 0 / active ==
// 0 / max == default for a freshly-started daemon with no
// activity).
func TestStatusToolReturnsSnapshot(t *testing.T) {
	d, _, done := newTestDaemon(t, nil, nil)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/call", map[string]any{
		"name":      "status",
		"arguments": map[string]any{},
	})
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
		t.Fatalf("status text is not JSON: %v\n%s", err, text)
	}

	// Cycle-1 fields still round-trip.
	if got := int(snap["pid"].(float64)); got != os.Getpid() {
		t.Errorf("status.pid = %d, want %d", got, os.Getpid())
	}
	if snap["model"] != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("status.model = %v", snap["model"])
	}
	if snap["workspace"] != "/tmp/workspace" {
		t.Errorf("status.workspace = %v", snap["workspace"])
	}
	if snap["version"] != "1.3.4-test" {
		t.Errorf("status.version = %v", snap["version"])
	}

	// Cycle-rc-status-tool adds the rich shape.
	if _, ok := snap["started_at"].(string); !ok {
		t.Errorf("status.started_at missing or not a string")
	}
	if uptime, ok := snap["uptime"].(string); !ok || uptime == "" {
		t.Errorf("status.uptime missing or empty: %v", snap["uptime"])
	}
	if got, ok := snap["connected_clients"].(float64); !ok {
		// The dial inside `call` is what bumps the counter;
		// at the moment the status response is decoded the
		// connection is still open, so the value is at
		// least 0 and may be 1 depending on scheduler
		// timing. The only invariant we pin here is
		// "it's a number".
		t.Errorf("status.connected_clients = %v (%T), want a number", snap["connected_clients"], snap["connected_clients"])
	} else if int(got) < 0 {
		t.Errorf("status.connected_clients = %v, want ≥ 0", got)
	}

	// cascades block: {total, active, max}. A freshly-started
	// daemon has 0 / 0 / default-cap.
	cascades, ok := snap["cascades"].(map[string]any)
	if !ok {
		t.Fatalf("status.cascades missing or not an object: %v", snap["cascades"])
	}
	if got := int(cascades["total"].(float64)); got != 0 {
		t.Errorf("status.cascades.total = %d, want 0", got)
	}
	if got := int(cascades["active"].(float64)); got != 0 {
		t.Errorf("status.cascades.active = %d, want 0", got)
	}
	if got := int(cascades["max"].(float64)); got != CascadeRegistryDefaultMax {
		t.Errorf("status.cascades.max = %d, want %d", got, CascadeRegistryDefaultMax)
	}

	// auth block: {method, session_id, user_id, scopes, expires_at}.
	// The dispatcher fabricates a non-nil AuthState when the
	// caller didn't; with method = "" the auth block falls back
	// to "none", and the rest of the fields are zero values.
	auth, ok := snap["auth"].(map[string]any)
	if !ok {
		t.Fatalf("status.auth missing or not an object: %v", snap["auth"])
	}
	if auth["method"] != AuthMethodNone {
		t.Errorf("status.auth.method = %v, want %q", auth["method"], AuthMethodNone)
	}
	if auth["session_id"] != "" {
		t.Errorf("status.auth.session_id = %v, want empty", auth["session_id"])
	}
	if auth["user_id"] != "" {
		t.Errorf("status.auth.user_id = %v, want empty", auth["user_id"])
	}
	scopes, ok := auth["scopes"].([]any)
	if !ok {
		t.Errorf("status.auth.scopes not an array: %T", auth["scopes"])
	}
	if len(scopes) != 0 {
		t.Errorf("status.auth.scopes has %d entries, want 0", len(scopes))
	}
	// expires_at may be either a string or JSON null; both are
	// valid for a session with no expiry.
	if got, ok := auth["expires_at"]; ok && got != nil {
		if _, ok := got.(string); !ok {
			t.Errorf("status.auth.expires_at = %v (%T), want null or string", got, got)
		}
	}

	// daemon block: {transport, addr, mode}. The cycle-1
	// daemon uses Unix-socket; newTestDaemon doesn't install a
	// DaemonMeta so the values are zero strings. That's fine —
	// the block is present and the dashboard's renderer
	// doesn't have to nil-check.
	daemon, ok := snap["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("status.daemon missing or not an object: %v", snap["daemon"])
	}
	if _, ok := daemon["transport"].(string); !ok {
		t.Errorf("status.daemon.transport not a string: %v", daemon["transport"])
	}
	if _, ok := daemon["addr"].(string); !ok {
		t.Errorf("status.daemon.addr not a string: %v", daemon["addr"])
	}
	if _, ok := daemon["mode"].(string); !ok {
		t.Errorf("status.daemon.mode not a string: %v", daemon["mode"])
	}
}

// TestListAgentsTool covers the list_agents tool. It must return
// at least one agent ("default") and respect a custom AgentLister.
func TestListAgentsTool(t *testing.T) {
	lister := StaticAgentLister{Agents: []AgentInfo{
		{Name: "default", Label: "Default"},
		{Name: "explorer", Label: "Explorer"},
	}}
	d, _, done := newTestDaemon(t, lister, nil)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/call", map[string]any{
		"name":      "list_agents",
		"arguments": map[string]any{},
	})
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)

	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("list_agents text not JSON: %v", err)
	}
	arr, _ := payload["agents"].([]any)
	if len(arr) != 2 {
		t.Fatalf("list_agents returned %d agents, want 2: %#v", len(arr), arr)
	}
	got := map[string]bool{}
	for _, ai := range arr {
		m, _ := ai.(map[string]any)
		got[m["name"].(string)] = true
	}
	for _, want := range []string{"default", "explorer"} {
		if !got[want] {
			t.Errorf("agent %q missing from list_agents output", want)
		}
	}
}

// TestGetOutputToolByID exercises the explicit conversation_id
// path: get_output("conv-xyz") returns the text the stub reader
// produced, and isError stays false.
func TestGetOutputToolByID(t *testing.T) {
	rd := &stubReader{Turns: map[string]string{
		"conv-xyz": "the dashboard saw this",
	}}
	d, _, done := newTestDaemon(t, nil, rd)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/call", map[string]any{
		"name":      "get_output",
		"arguments": map[string]any{"conversation_id": "conv-xyz"},
	})
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("get_output returned isError: %s", text)
	}
	if !strings.Contains(text, "the dashboard saw this") {
		t.Errorf("get_output text missing turn content: %q", text)
	}
	if !strings.Contains(text, "conv-xyz") {
		t.Errorf("get_output text missing conversation id: %q", text)
	}
}

// TestGetOutputToolNotFound covers the not-found path. The tool
// must surface ErrNotFound as isError=true with a clear message;
// the wire response is still HTTP-200-shaped.
func TestGetOutputToolNotFound(t *testing.T) {
	rd := &stubReader{NotFound: map[string]bool{"conv-xyz": true}}
	d, _, done := newTestDaemon(t, nil, rd)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/call", map[string]any{
		"name":      "get_output",
		"arguments": map[string]any{"conversation_id": "conv-xyz"},
	})
	res, _ := resp["result"].(map[string]any)
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("get_output for missing id should set isError=true, got %#v", res)
	}
}

// TestGetOutputFallsBackToHost covers the no-id path: when the
// caller omits conversation_id, the daemon falls back to the
// session's currently-running conversation. The in-memory
// TurnOutput (which newTestDaemon pre-populates with "hello from
// the host") takes precedence over the stub reader because the
// snapshot is strictly fresher than any disk-backed read.
func TestGetOutputFallsBackToHost(t *testing.T) {
	rd := &stubReader{Turns: map[string]string{
		"conv-abc": "stale-disk-output",
	}}
	d, _, done := newTestDaemon(t, nil, rd)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/call", map[string]any{
		"name":      "get_output",
		"arguments": map[string]any{},
	})
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(text, "hello from the host") {
		t.Errorf("get_output fallback text: %q", text)
	}
	if !strings.Contains(text, "conv-abc") {
		t.Errorf("get_output fallback id: %q", text)
	}
}

// TestGetOutputPropagatesReaderError covers non-ErrNotFound errors:
// the daemon must still set isError=true but the message must
// wrap the original error verbatim so the dashboard can show it.
func TestGetOutputPropagatesReaderError(t *testing.T) {
	rd := &stubReader{ErrOn: map[string]error{
		"conv-err": errors.New("disk on fire"),
	}}
	d, _, done := newTestDaemon(t, nil, rd)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "tools/call", map[string]any{
		"name":      "get_output",
		"arguments": map[string]any{"conversation_id": "conv-err"},
	})
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true, got %#v", res)
	}
	if !strings.Contains(text, "disk on fire") {
		t.Errorf("get_output error text missing original error: %q", text)
	}
}

// TestUnknownMethodReturnsJSONRPCError covers the JSON-RPC error
// path: an unrecognised method must produce {"error": {"code":
// -32601, "message": "Method not found", ...}} rather than
// hanging or closing the connection.
func TestUnknownMethodReturnsJSONRPCError(t *testing.T) {
	d, _, done := newTestDaemon(t, nil, nil)
	defer done()

	sc, enc, c := dial(t, d.SocketPath())
	defer c.Close()

	resp := call(t, sc, enc, "not-a-real-method", nil)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown method: no error envelope, got %#v", resp)
	}
	if int(errObj["code"].(float64)) != -32601 {
		t.Errorf("error.code = %v, want -32601", errObj["code"])
	}
}

// TestMalformedRequestReturnsParseError covers the JSON-RPC
// parse-error path. A line that isn't valid JSON must produce
// {"jsonrpc": "2.0", "id": null, "error": {"code": -32700, …}};
// the connection must stay open and serve subsequent requests.
func TestMalformedRequestReturnsParseError(t *testing.T) {
	d, _, done := newTestDaemon(t, nil, nil)
	defer done()

	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("not-json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	rd := bufio.NewReader(c)
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !sc.Scan() {
		t.Fatalf("scan: %v", sc.Err())
	}
	var resp map[string]any
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errObj, _ := resp["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("parse error: no error envelope, got %#v", resp)
	}
	if int(errObj["code"].(float64)) != -32700 {
		t.Errorf("parse error code = %v, want -32700", errObj["code"])
	}

	// Follow-up valid request: connection still serves.
	enc := json.NewEncoder(c)
	dec := json.NewDecoder(rd)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "ping",
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp2 map[string]any
	if err := dec.Decode(&resp2); err != nil {
		t.Fatalf("decode followup: %v", err)
	}
	if _, ok := resp2["result"].(map[string]any); !ok {
		t.Errorf("followup ping: no result, got %#v", resp2)
	}
}

// TestShutdownRemovesSocket verifies Shutdown closes the listener
// AND removes the socket file. A re-Start on the same path must
// then succeed (no EADDRINUSE).
func TestShutdownRemovesSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "rc.sock")
	sess := NewSessionState("")
	d, err := Start(context.Background(), Config{SocketPath: sock, Session: sess})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	d.Shutdown()

	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file not cleaned up after Shutdown: %v", err)
	}

	// A second Start on the same path must succeed — proves we
	// aren't leaving a stale socket behind.
	d2, err := Start(context.Background(), Config{SocketPath: sock, Session: NewSessionState("")})
	if err != nil {
		t.Fatalf("re-Start on cleaned path: %v", err)
	}
	d2.Shutdown()
}

// TestSessionStateTruncatesLargeOutput verifies the 1 MiB turn
// buffer cap. A 3 MiB string is dropped to the most-recent 1 MiB.
func TestSessionStateTruncatesLargeOutput(t *testing.T) {
	s := NewSessionState("")
	big := strings.Repeat("x", 3<<20) // 3 MiB
	s.SetTurnOutput(big)
	s.mu.RLock()
	got := s.TurnOutput
	s.mu.RUnlock()
	if len(got) != MaxTurnOutputBytes {
		t.Errorf("TurnOutput len = %d, want %d", len(got), MaxTurnOutputBytes)
	}
	if !strings.HasPrefix(got, "x") {
		t.Errorf("TurnOutput should be all x's")
	}
}

// TestStatusConnectedReflectsClients verifies the connected
// counter increments while a connection is open and decrements
// after it closes. The daemon's accept loop is what drives this.
func TestStatusConnectedReflectsClients(t *testing.T) {
	d, sess, done := newTestDaemon(t, nil, nil)
	defer done()

	// Open a connection and verify connected > 0 before
	// disconnecting. We don't send any data — the daemon counts
	// the connection as soon as it's accepted, regardless of
	// whether the client speaks the protocol.
	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Connected() == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := sess.Connected(); got != 1 {
		t.Fatalf("connected = %d after Dial, want 1", got)
	}
	_ = c.Close()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Connected() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := sess.Connected(); got != 0 {
		t.Errorf("connected = %d after Close, want 0", got)
	}
}
