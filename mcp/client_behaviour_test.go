// client_behaviour_test.go — the documented per-server members the pool
// enforces: the tool allow/deny lists, the timeouts and the working
// directory of a stdio server.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolAllowed pins the documented allow/deny rules.
func TestToolAllowed(t *testing.T) {
	cases := []struct {
		name string
		spec ServerSpec
		tool string
		want bool
	}{
		{"no list allows everything", ServerSpec{}, "any", true},
		{"the allowlist hides the others", ServerSpec{EnabledTools: []string{"a"}}, "b", false},
		{"the allowlist keeps its own", ServerSpec{EnabledTools: []string{"a"}}, "a", true},
		{"the denylist withholds", ServerSpec{DisabledTools: []string{"a"}}, "a", false},
		{"the denylist keeps the others", ServerSpec{DisabledTools: []string{"a"}}, "b", true},
		{"the allowlist wins", ServerSpec{EnabledTools: []string{"a"}, DisabledTools: []string{"a"}}, "a", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.ToolAllowed(tc.tool); got != tc.want {
				t.Errorf("ToolAllowed(%q) = %v, want %v", tc.tool, got, tc.want)
			}
		})
	}
}

// TestPoolCallToolRejectsAWithheldTool: the pool refuses a call to a tool
// the server's list withholds, and never opens a connection for it.
func TestPoolCallToolRejectsAWithheldTool(t *testing.T) {
	fake, transport := newFakeServer(t)
	pool := NewPool([]ServerSpec{{Name: "srv", Type: "stdio", Enabled: true, DisabledTools: []string{"search"}}})
	pool.SetTransportFactory(func(ServerSpec) (sdk.Transport, error) { return transport, nil })
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CallTool(context.Background(), "srv", "search", nil)
	if err == nil {
		t.Fatal("CallTool: want an error for the withheld tool")
	}
	if !strings.Contains(err.Error(), "tool search is not enabled for server srv") {
		t.Errorf("error = %q, want the reference's message", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 0 {
		t.Errorf("server calls = %v, want none (the call never left the client)", fake.calls)
	}
}

// TestPoolCallToolAllowsAListedTool: the allowlist keeps its own tools.
func TestPoolCallToolAllowsAListedTool(t *testing.T) {
	_, transport := newFakeServer(t)
	pool := NewPool([]ServerSpec{{Name: "srv", Type: "stdio", Enabled: true, EnabledTools: []string{"search"}}})
	pool.SetTransportFactory(func(ServerSpec) (sdk.Transport, error) { return transport, nil })
	t.Cleanup(func() { _ = pool.Close() })

	out, err := pool.CallTool(context.Background(), "srv", "search", json.RawMessage(`{"q":"x"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "hello from the tool" {
		t.Errorf("CallTool = %q, want the tool's text", out)
	}
}

// TestTimeouts pins the documented default and the override.
func TestTimeouts(t *testing.T) {
	if got := (ServerSpec{}).CallTimeout(); got != 180*time.Second {
		t.Errorf("default call timeout = %v, want 3m0s", got)
	}
	if got := (ServerSpec{}).ConnectTimeout(); got != 300*time.Second {
		t.Errorf("default connect timeout = %v, want 5m0s", got)
	}
	spec := ServerSpec{TimeoutSeconds: 600}
	if got := spec.CallTimeout(); got != 600*time.Second {
		t.Errorf("call timeout = %v, want 600s", got)
	}
	if got := spec.ConnectTimeout(); got != 600*time.Second {
		t.Errorf("connect timeout = %v, want 600s", got)
	}
	off := ServerSpec{TimeoutSeconds: -1}
	if got := off.CallTimeout(); got != 0 {
		t.Errorf("call timeout = %v, want the limit off", got)
	}
	if got := off.ConnectTimeout(); got != 0 {
		t.Errorf("connect timeout = %v, want the limit off", got)
	}
}

// TestPoolCallToolTimeout: a call that runs past the server's timeout
// fails with the documented message.
func TestPoolCallToolTimeout(t *testing.T) {
	transport := newSlowServer(t)
	pool := NewPool([]ServerSpec{{Name: "srv", Type: "stdio", Enabled: true, TimeoutSeconds: 1}})
	pool.SetTransportFactory(func(ServerSpec) (sdk.Transport, error) { return transport, nil })
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CallTool(context.Background(), "srv", "slow", nil)
	if err == nil {
		t.Fatal("CallTool: want the timeout error")
	}
	want := `MCP tool call to server "srv" timed out after 1s`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

// TestTransportUsesTheConfiguredCWD: the documented `cwd` is the working
// directory of the spawned server process.
func TestTransportUsesTheConfiguredCWD(t *testing.T) {
	transport, err := transportFor(ServerSpec{
		Name:    "srv",
		Type:    "stdio",
		Command: "node",
		CWD:     "/work",
	})
	if err != nil {
		t.Fatalf("transportFor: %v", err)
	}
	cmd, ok := transport.(*sdk.CommandTransport)
	if !ok {
		t.Fatalf("transport = %T, want a command transport", transport)
	}
	if cmd.Command.Dir != "/work" {
		t.Errorf("command dir = %q, want /work", cmd.Command.Dir)
	}
	if cmd.Command.Args[0] != "node" {
		t.Errorf("command = %v, want node", cmd.Command.Args)
	}
}

// TestTransportWithoutCWD: an entry without a `cwd` runs in the clone's own
// working directory.
func TestTransportWithoutCWD(t *testing.T) {
	transport, err := transportFor(ServerSpec{Name: "srv", Type: "stdio", Command: "node"})
	if err != nil {
		t.Fatalf("transportFor: %v", err)
	}
	cmd := transport.(*sdk.CommandTransport)
	if cmd.Command.Dir != "" {
		t.Errorf("command dir = %q, want the empty dir (the process's own)", cmd.Command.Dir)
	}
}

// newSlowServer returns a transport for a server whose only tool sleeps
// for two seconds.
func newSlowServer(t *testing.T) sdk.Transport {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "slow", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{
		Name:        "slow",
		Description: "a slow tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: "late"}},
		}, nil
	})
	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the slow server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return clientTransport
}
