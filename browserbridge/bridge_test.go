package browserbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBridge is a stand-in for the project's own runtime: it serves the
// port, greets a client, and answers tool requests with what the test
// says.
type fakeBridge struct {
	ln net.Listener
	// answer is the result the fake sends back for any tool request.
	answer func(tool string, args json.RawMessage) (any, string)

	mu sync.Mutex
	// seen records the requests that arrived.
	seen []request
	// hellos counts the greetings that arrived.
	hellos int
	// conns are the accepted connections, so a test can drop them.
	conns []net.Conn
}

// newFakeBridge starts a fake bridge on a free port.
func newFakeBridge(t *testing.T, answer func(tool string, args json.RawMessage) (any, string)) *fakeBridge {
	t.Helper()
	return newFakeBridgeOn(t, 0, answer)
}

// newFakeBridgeOn starts a fake bridge on a given port.
//
// A port of 0 asks the kernel for a free one; a real port is taken
// exactly, which is what a test about the port race needs.
func newFakeBridgeOn(t *testing.T, port int, answer func(tool string, args json.RawMessage) (any, string)) *fakeBridge {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on 127.0.0.1:%d: %v", port, err)
	}
	f := &fakeBridge{ln: ln, answer: answer}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

// port is the fake's port.
func (f *fakeBridge) port() int {
	return f.ln.Addr().(*net.TCPAddr).Port
}

// serve accepts connections and speaks the client's side of the protocol.
func (f *fakeBridge) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

// handle serves one connection.
func (f *fakeBridge) handle(conn net.Conn) {
	f.mu.Lock()
	f.conns = append(f.conns, conn)
	f.mu.Unlock()
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var msg map[string]any
			if err := json.Unmarshal(line, &msg); err == nil {
				switch msg["type"] {
				case "client_hello":
					f.mu.Lock()
					f.hellos++
					f.mu.Unlock()
					fmt.Fprintf(conn, "{\"type\":\"client_ack\",\"clientId\":\"1\"}\n")
				case "tool_request":
					var req request
					_ = json.Unmarshal(line, &req)
					f.mu.Lock()
					f.seen = append(f.seen, req)
					f.mu.Unlock()
					result, toolErr := f.answer(req.Tool, req.Args)
					if toolErr != "" {
						fmt.Fprintf(conn, "{\"id\":%q,\"type\":\"tool_error\",\"error\":%q}\n", req.ID, toolErr)
					} else {
						raw, _ := json.Marshal(result)
						fmt.Fprintf(conn, "{\"id\":%q,\"type\":\"tool_response\",\"result\":%s}\n", req.ID, raw)
					}
				}
			}
		}
		if err != nil {
			conn.Close()
			return
		}
	}
}

// stop drops the fake: the listener and every connection it accepted.
//
// A client whose primary dies must find the port free, so the test
// needs a primary that really goes away — closing the listener alone
// leaves the accepted connections alive.
func (f *fakeBridge) stop() {
	f.ln.Close()
	f.mu.Lock()
	conns := f.conns
	f.conns = nil
	f.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

// helloCount is how many greetings the fake has seen.
func (f *fakeBridge) helloCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hellos
}

// requests is what the fake has been asked.
func (f *fakeBridge) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.seen...)
}

// TestClientRoleCallsATool checks the client role: the clone joins a
// bridge that is already there and asks it for a tool.
func TestClientRoleCallsATool(t *testing.T) {
	fake := newFakeBridge(t, func(tool string, args json.RawMessage) (any, string) {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "hello from " + tool}}}, ""
	})

	ctx := context.Background()
	c, err := Connect(ctx, Config{Host: "127.0.0.1", Port: fake.port(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	res, err := c.Call(ctx, "get_page_text", json.RawMessage(`{"tabId":7}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var got struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(res, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Content) != 1 || got.Content[0].Text != "hello from get_page_text" {
		t.Fatalf("unexpected result: %s", res)
	}
	seen := fake.requests()
	if len(seen) != 1 || seen[0].Tool != "get_page_text" {
		t.Fatalf("unexpected requests: %+v", seen)
	}
	if string(seen[0].Args) != `{"tabId":7}` {
		t.Fatalf("unexpected args: %s", seen[0].Args)
	}
}

// TestClientRoleReportsAToolError checks that the bridge's own error
// reaches the caller.
func TestClientRoleReportsAToolError(t *testing.T) {
	fake := newFakeBridge(t, func(tool string, args json.RawMessage) (any, string) {
		return nil, "Unknown tool: " + tool
	})

	ctx := context.Background()
	c, err := Connect(ctx, Config{Host: "127.0.0.1", Port: fake.port(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	_, err = c.Call(ctx, "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "Unknown tool") {
		t.Fatalf("expected the bridge's error, got %v", err)
	}
}

// TestPrimaryRoleDrivesTheNativeHost checks the primary role: the clone
// takes the port and asks the extension's native host, which says nothing
// until it is asked.
func TestPrimaryRoleDrivesTheNativeHost(t *testing.T) {
	// A free port: the clone binds it.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	ctx := context.Background()
	c, err := Connect(ctx, Config{Host: "127.0.0.1", Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// The native host connects and stays silent.
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The extension answers the request.
	go func() {
		br := bufio.NewReader(conn)
		line, err := br.ReadBytes('\n')
		if err != nil {
			return
		}
		var req request
		_ = json.Unmarshal(line, &req)
		fmt.Fprintf(conn, "{\"id\":%q,\"type\":\"tool_response\",\"result\":{\"ok\":true,\"tool\":%q}}\n", req.ID, req.Tool)
	}()

	res, err := c.Call(ctx, "navigate", json.RawMessage(`{"url":"https://example.test"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(string(res), "navigate") {
		t.Fatalf("unexpected result: %s", res)
	}
}

// TestPrimaryRoleServesOtherClients checks that taking the port does not
// break a client that joins later: its request is forwarded to the native
// host and the answer is routed back.
func TestPrimaryRoleServesOtherClients(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	ctx := context.Background()
	c, err := Connect(ctx, Config{Host: "127.0.0.1", Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// The native host attaches and answers whatever it is asked.
	native, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial native: %v", err)
	}
	defer native.Close()
	go func() {
		br := bufio.NewReader(native)
		for {
			line, err := br.ReadBytes('\n')
			if err != nil {
				return
			}
			var req request
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}
			fmt.Fprintf(native, "{\"id\":%q,\"type\":\"tool_response\",\"result\":{\"echo\":%q}}\n", req.ID, req.Tool)
		}
	}()
	time.Sleep(700 * time.Millisecond) // past the classification grace

	// A client joins the port.
	client, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer client.Close()
	fmt.Fprintf(client, "{\"type\":\"client_hello\"}\n")

	br := bufio.NewReader(client)
	ack, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if !strings.Contains(string(ack), "client_ack") {
		t.Fatalf("unexpected greeting: %s", ack)
	}

	fmt.Fprintf(client, "{\"type\":\"tool_request\",\"id\":\"abc\",\"tool\":\"find\",\"args\":{}}\n")
	answer, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	var res struct {
		ID     string `json:"id"`
		Result struct {
			Echo string `json:"echo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(answer, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.ID != "abc" {
		t.Fatalf("the answer must keep the client's own id, got %q", res.ID)
	}
	if res.Result.Echo != "find" {
		t.Fatalf("unexpected answer: %s", answer)
	}
}

// TestBridgePortReadsTheProjectsConfig checks the project's own config
// file is the source of the port.
func TestBridgePortReadsTheProjectsConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "open-claude-in-chrome")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"port":19999}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := bridgePort(); got != 19999 {
		t.Fatalf("bridgePort() = %d, want 19999", got)
	}
}

// TestLiveBridge drives the real bridge, when one is running.
//
// It is skipped unless PIGASUS_BRIDGE_LIVE is set: the live bridge
// belongs to the user, and a test must not touch their browser without
// being asked to.
func TestLiveBridge(t *testing.T) {
	if os.Getenv("PIGASUS_BRIDGE_LIVE") == "" {
		t.Skip("set PIGASUS_BRIDGE_LIVE=1 to drive the real bridge")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c, err := Connect(ctx, DefaultConfig())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	res, err := c.Call(ctx, "tabs_context_mcp", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("empty answer")
	}
	t.Logf("tabs: %s", truncate(string(res), 200))
}

// truncate shortens a string for a log line.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
