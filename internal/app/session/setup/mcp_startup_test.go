// mcp_startup_test.go — the always-loaded sweep must not delay startup.
//
// The sweep connects to every configured MCP server, which the pool
// otherwise does not do until a tool call names one. That is a change in
// kind: before the sweep existed, a wedged server cost nothing until the
// model called it; with the sweep on the startup path, it would cost the
// whole session's start.
//
// The measurement that makes this a test rather than a comment: the SDK's
// (*Client).Connect does not return when its context expires. Measured at
// 1.5.18, it returns after `budget + 5s` for any budget, because it sends a
// best-effort `notifications/cancelled` and waits out the transport's
// `notifyCancellationTimeout` before unwinding. So a synchronous sweep with
// even a one-second budget costs six seconds, and a five-second budget —
// the documented per-server connect limit is five *minutes* — costs ten.
//
// The fix is to run the sweep off the startup path. These tests pin both
// halves: the install returns immediately, and the sweep still delivers
// when it does finish.
package setup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/mcp"
)

// TestInstallMCPProviderReturnsWithoutWaitingForServers pins that a
// configured-but-wedged server does not delay the caller.
//
// The server is `sleep 300`: it starts, never answers the MCP handshake,
// and would hold a synchronous sweep for the whole budget (plus the SDK's
// fixed five-second unwind).
func TestInstallMCPProviderReturnsWithoutWaitingForServers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	agentDir := filepath.Join(home, ".gemini", "antigravity-cli")
	writeFile(t, filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		`{"mcpServers":{"wedged":{"command":"sleep","args":["300"]}}}`)

	start := time.Now()
	InstallMCPProvider(agentDir, "")
	elapsed := time.Since(start)

	// A generous ceiling: the point is that the caller does not wait for
	// the network, not that it is instant. A synchronous sweep against
	// this server measures ~10s.
	if elapsed > 2*time.Second {
		t.Errorf("InstallMCPProvider took %v; the always-loaded sweep must run off the startup path", elapsed)
	}
}

// TestAlwaysLoadedSweepEventuallyAdvertises pins the other half: running
// off the startup path does not mean never running.
//
// It drives InstallMCPProvider against a real in-memory server, then waits
// for the advertised name to appear. The wait is a bounded poll rather than
// a sleep: the goroutine's latency is a scheduling detail, and a fixed
// sleep would be both slow and flaky.
func TestAlwaysLoadedSweepEventuallyAdvertises(t *testing.T) {
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{
		Meta:        sdk.Meta{mcp.AlwaysLoadKey: true},
		Name:        "search_docs",
		Description: "search the docs",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, nil
	})
	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the fake server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	pool := mcp.NewPool([]mcp.ServerSpec{{Name: "docs", Type: "stdio", Enabled: true}})
	pool.SetTransportFactory(func(mcp.ServerSpec) (sdk.Transport, error) {
		return clientTransport, nil
	})
	t.Cleanup(func() { _ = pool.Close() })

	prev := web.SetMCPProvider(pool)
	t.Cleanup(func() {
		web.SetMCPProvider(prev)
		surface.UnregisterPluginTool("mcp_docs_search_docs")
		web.ForgetAlwaysLoadedAdvertised("mcp_docs_search_docs")
	})

	go installAlwaysLoadedTools(pool)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, tool := range surface.DynamicPluginTools() {
			if tool.Name() == "mcp_docs_search_docs" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the async sweep never advertised mcp_docs_search_docs")
}
