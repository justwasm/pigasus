// Tests for the --remote-control daemon wiring (cmd/wagyu/remote_control.go).
//
// These tests cover the host-side helper functions, not the wire
// protocol itself (which is covered exhaustively in
// internal/remotecontrol/daemon_test.go). The helpers are thin —
// "did the flag trigger Start?", "did Shutdown actually tear the
// daemon down?" — but the helper-level tests are what an operator
// running the targeted `TestRemote|TestDaemon|TestMCP|TestSocket`
// filter would expect to find, so they live here.
package rchost

import (
	"bufio"
	"encoding/json"
	"flag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// shortTmpDir returns a fresh directory under $TMPDIR with a
// short name (so macOS's 104-byte Unix-socket path limit is
// respected). See internal/remotecontrol/daemon_test.go for the
// same helper there.
func shortTmpDirRC(t *testing.T) string {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("TMPDIR"))
	if base == "" {
		base = os.TempDir()
	}
	name := filepath.Join(base, "rchost")
	if err := os.RemoveAll(name); err != nil {
		t.Fatalf("clean %s: %v", name, err)
	}
	if err := os.MkdirAll(name, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(name) })
	return name
}

// flagSetWithFlag builds a *flag.FlagSet with a single bool flag
// set to v, the same shape the jetski parser would have after
// processing the user's --remote-control flag.
func flagSetWithFlag(name string, v bool) *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool(name, false, "")
	if v {
		if err := fs.Set(name, "true"); err != nil {
			panic(err)
		}
	}
	return fs
}

// TestRemoteControlFlagStartsDaemon covers the headline ask:
// setting --remote-control must cause the host's helper to bring
// up a daemon. We assert by socket file: the daemon creates the
// socket when it accepts the listener.
//
// The socket path is set via $OPENAGY_RC_SOCKET (the env var the
// host helper honours) so the test can pin it to a short path
// inside $TMPDIR without expanding MaybeStartDaemon's
// signature.
func TestRemoteControlFlagStartsDaemon(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	t.Setenv("OPENAGY_RC_SOCKET", sock)
	sess := remotecontrol.NewSessionState("test")
	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d == nil {
		t.Fatalf("MaybeStartDaemon returned nil despite --remote-control=true")
	}
	defer ShutdownDaemon(d, os.Stderr)

	if d.SocketPath() != sock {
		t.Errorf("socket = %s; want %s", d.SocketPath(), sock)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
}

// TestRemoteControlFlagUnsetSkipsDaemon covers the negative
// path: when --remote-control is not set, the host helper must
// return nil so the deferred shutdown is a no-op.
func TestRemoteControlFlagUnsetSkipsDaemon(t *testing.T) {
	sess := remotecontrol.NewSessionState("test")
	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", false), sess)
	if d != nil {
		t.Fatalf("MaybeStartDaemon returned %v despite --remote-control=false", d)
	}
	defer ShutdownDaemon(d, os.Stderr) // must be a no-op
}

// TestRemoteControlDaemonListensOnUnixSocket drives the daemon
// over a real Unix-socket connection and reads back the JSON-RPC
// initialize response. This is the smallest end-to-end check that
// the host helper actually produced a live, MCP-speaking listener.
func TestRemoteControlDaemonListensOnUnixSocket(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	t.Setenv("OPENAGY_RC_SOCKET", sock)
	sess := remotecontrol.NewSessionState("1.3.4-test")
	sess.Backend = "gateway"
	sess.Model = "Gemini 3.8 Flash (Medium)"

	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d == nil {
		t.Fatalf("MaybeStartDaemon returned nil")
	}
	defer ShutdownDaemon(d, os.Stderr)

	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial %s: %v", d.SocketPath(), err)
	}
	defer c.Close()

	enc := json.NewEncoder(c)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
		},
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	rd := bufio.NewReader(c)
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	deadline := time.Now().Add(2 * time.Second)
	if err := c.SetReadDeadline(deadline); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if !sc.Scan() {
		t.Fatalf("scan: %v", sc.Err())
	}
	var resp map[string]any
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["result"]; !ok {
		t.Fatalf("initialize: no result, got %#v", resp)
	}
}

// TestMCPStatusToolReturnsLiveSessionState drives the status tool
// end-to-end through the host helper. The session state the host
// passed in must round-trip through the wire protocol — pid,
// backend, model — proving that the daemon and the host share a
// single source of truth.
func TestMCPStatusToolReturnsLiveSessionState(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	t.Setenv("OPENAGY_RC_SOCKET", sock)
	sess := remotecontrol.NewSessionState("test")
	sess.Backend = "gateway"
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Workspace = "/tmp/test-workspace"
	sess.SetConversationID("conv-host")

	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d == nil {
		t.Fatalf("MaybeStartDaemon returned nil")
	}
	defer ShutdownDaemon(d, os.Stderr)

	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	enc := json.NewEncoder(c)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	rd := bufio.NewReader(c)
	var resp map[string]any
	if err := json.NewDecoder(rd).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	var snap map[string]any
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatalf("status text not JSON: %v\n%s", err, text)
	}
	if snap["model"] != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("status.model = %v", snap["model"])
	}
	if snap["backend"] != "gateway" {
		t.Errorf("status.backend = %v", snap["backend"])
	}
	if snap["conversation_id"] != "conv-host" {
		t.Errorf("status.conversation_id = %v", snap["conversation_id"])
	}
}

// TestMCPListAgentsToolReturnsAgentList drives list_agents
// end-to-end. The default agent roster (matched by name) must
// surface through the daemon's wire protocol — proving the
// AgentLister interface is wired into the host helper, not just
// the standalone daemon.
func TestMCPListAgentsToolReturnsAgentList(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	t.Setenv("OPENAGY_RC_SOCKET", sock)
	sess := remotecontrol.NewSessionState("test")

	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d == nil {
		t.Fatalf("MaybeStartDaemon returned nil")
	}
	defer ShutdownDaemon(d, os.Stderr)

	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	enc := json.NewEncoder(c)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "list_agents",
			"arguments": map[string]any{},
		},
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp map[string]any
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(text, "default") {
		t.Errorf("list_agents output missing 'default' agent: %s", text)
	}
}

// TestMCPGetOutputToolReturnsTurnOutput drives get_output with
// the host's conversation id. SetTurnOutput simulates what the
// host's chunk handler would push; the daemon's wire response
// must contain that text. The test covers the empty-id fallback
// (the daemon uses the host's SetConversationID value).
func TestMCPGetOutputToolReturnsTurnOutput(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	t.Setenv("OPENAGY_RC_SOCKET", sock)
	sess := remotecontrol.NewSessionState("test")
	sess.SetConversationID("conv-test")
	sess.SetTurnOutput("hello dashboard world")

	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d == nil {
		t.Fatalf("MaybeStartDaemon returned nil")
	}
	defer ShutdownDaemon(d, os.Stderr)

	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	enc := json.NewEncoder(c)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_output",
			"arguments": map[string]any{},
		},
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp map[string]any
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	res, _ := resp["result"].(map[string]any)
	env, _ := res["content"].([]any)
	first, _ := env[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(text, "hello dashboard world") {
		t.Errorf("get_output missing turn text: %s", text)
	}
	if !strings.Contains(text, "conv-test") {
		t.Errorf("get_output missing conversation id: %s", text)
	}
}

// TestSocketCleanupOnShutdown verifies the host helper tears the
// daemon down cleanly: after ShutdownDaemon, the
// socket file is gone AND a re-bind on the same path succeeds
// (no EADDRINUSE from a stale socket).
func TestSocketCleanupOnShutdown(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	sess := remotecontrol.NewSessionState("test")

	d := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d == nil {
		t.Fatalf("MaybeStartDaemon returned nil")
	}
	ShutdownDaemon(d, os.Stderr)

	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file not removed on shutdown: %v", err)
	}
	// Re-bind on the same path must work; this proves Shutdown
	// removed the socket file (otherwise Start would fail with
	// EADDRINUSE on Linux / "address already in use" on macOS).
	d2 := MaybeStartDaemon(t.Context(), flagSetWithFlag("remote-control", true), sess)
	if d2 == nil {
		t.Fatalf("re-bind after shutdown failed")
	}
	ShutdownDaemon(d2, os.Stderr)
}

// flagSetWithString builds a *flag.FlagSet with a single string flag
// set to v. Mirrors flagSetWithFlag (above) but for the
// --remote-control-tcp string flag.
func flagSetWithString(name, v string) *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String(name, "", "")
	if v != "" {
		if err := fs.Set(name, v); err != nil {
			panic(err)
		}
	}
	return fs
}

// TestRemoteControlTCPFlagStartsWSAdapter covers the new
// --remote-control-tcp flag (cycle rc-ws-transport): setting it
// must bring up the TCP+WebSocket adapter on the requested bind
// address. The OS-assigned port lives in TCPServer.Addr() (and the
// printable URL in HTTPURL()); we assert the URL is non-empty and
// parses to a ws:// scheme pointing at a TCP listener.
func TestRemoteControlTCPFlagStartsWSAdapter(t *testing.T) {
	sess := remotecontrol.NewSessionState("rc-ws-transport-test")
	srv := MaybeStartTCPDaemon(t.Context(), flagSetWithString("remote-control-tcp", "127.0.0.1:0"), sess)
	if srv == nil {
		t.Fatalf("MaybeStartTCPDaemon returned nil despite --remote-control-tcp=127.0.0.1:0")
	}
	defer ShutdownTCPDaemon(srv, os.Stderr)

	url := srv.HTTPURL()
	if !strings.HasPrefix(url, "ws://") {
		t.Errorf("HTTPURL = %q; want ws:// scheme", url)
	}
	if !strings.HasSuffix(url, "/rpc") {
		t.Errorf("HTTPURL = %q; want /rpc path suffix", url)
	}
}

// TestRemoteControlTCPFlagUnsetSkipsAdapter covers the negative
// path — when --remote-control-tcp is unset (or absent), the host
// helper must return nil so the deferred shutdown is a no-op.
func TestRemoteControlTCPFlagUnsetSkipsAdapter(t *testing.T) {
	sess := remotecontrol.NewSessionState("test")
	srv := MaybeStartTCPDaemon(t.Context(), flagSetWithString("remote-control-tcp", ""), sess)
	if srv != nil {
		t.Fatalf("MaybeStartTCPDaemon returned %v despite --remote-control-tcp unset", srv)
	}
	defer ShutdownTCPDaemon(srv, os.Stderr) // must be a no-op
}

// TestRemoteControlBothFlagsSetPrefersTCP covers the precedence
// rule documented in remote_control.go: when both --remote-control
// (Unix socket) and --remote-control-tcp (WebSocket) are set, the
// WebSocket adapter wins. The Unix-socket helper returns nil so
// the deferred shutdown is a no-op.
func TestRemoteControlBothFlagsSetPrefersTCP(t *testing.T) {
	dir := shortTmpDirRC(t)
	sock := filepath.Join(dir, "rc.sock")
	t.Setenv("OPENAGY_RC_SOCKET", sock)
	sess := remotecontrol.NewSessionState("test")

	// Unix-socket helper must NOT bring up a daemon when the TCP
	// flag is also set — the WS adapter owns the
	// --remote-control semantic in that case.
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("remote-control", false, "")
	fs.String("remote-control-tcp", "", "")
	if err := fs.Set("remote-control", "true"); err != nil {
		t.Fatalf("set remote-control: %v", err)
	}
	if err := fs.Set("remote-control-tcp", "127.0.0.1:0"); err != nil {
		t.Fatalf("set remote-control-tcp: %v", err)
	}
	d := MaybeStartDaemon(t.Context(), fs, sess)
	if d != nil {
		t.Fatalf("MaybeStartDaemon returned %v despite --remote-control-tcp being set; the WS adapter should win", d)
	}
	defer ShutdownDaemon(d, os.Stderr) // must be a no-op

	// The WS adapter starts up cleanly on the same FlagSet.
	srv := MaybeStartTCPDaemon(t.Context(), fs, sess)
	if srv == nil {
		t.Fatalf("MaybeStartTCPDaemon returned nil despite both flags set")
	}
	defer ShutdownTCPDaemon(srv, os.Stderr)
}
