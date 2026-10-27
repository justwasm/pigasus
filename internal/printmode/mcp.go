// mcp.go — wiring the clone's three MCP tools to the configured servers.
//
// The reference exposes three MCP tools to the model (call_mcp_tool,
// list_resources, read_resource — see internal/tools/mcp.go). They need a
// live MCP connection, which this file installs.
//
// What this file owns
// -------------------
//
//  1. The one place that reads the reference's `mcp.json` for the *tools*
//     path and turns it into the client pool the three tools call through.
//  2. The choice to install the pool process-wide (internal/tools keeps a
//     process-wide provider) rather than per session: the pool holds one
//     connection per server, and a session-scoped pool would spawn a second
//     server process for every turn.
//
// The pool is lazy — no server process starts until a tool call names it —
// so installing it costs nothing when no MCP server is configured.
//
// A missing configuration file is the reference's "no servers configured"
// state and installs an empty pool. A malformed one leaves the tools
// without a provider: they then fail with "no MCP servers are configured"
// rather than taking the session down. The reference drops the whole file
// in that case too.
//
// Known limitation
// ----------------
// In the print path the clone *also* runs PiG's MCP extension, which opens
// its own connection to the same servers to serve the always-loaded
// `mcp__<server>__<tool>` surface. The two surfaces therefore hold one
// connection each to the same stdio server. The fix is to make this pool
// the single owner and register the always-loaded tools from it; that
// needs the clone to drive `tools/list` itself (mcp already
// parses the payload), which is a separate cycle.
package printmode

import (
	"path/filepath"

	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/mcp"
)

// InstallMCPProvider wires the clone's three MCP tools to the servers in
// the reference's `mcp.json` under agentDir.
//
// It is safe to call more than once: the last call wins. The TUI calls it
// too, so the MCP tools work in both surfaces.
func InstallMCPProvider(agentDir string) {
	if agentDir == "" {
		return
	}
	pool, err := mcp.NewPoolFromFile(filepath.Join(agentDir, "mcp.json"))
	if err != nil || pool == nil {
		return
	}
	tools.SetMCPProvider(pool)
}
