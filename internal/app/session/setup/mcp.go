// setup — mcp.go: wiring the clone's three MCP tools to the configured
// servers.
//
// The reference exposes three MCP tools to the model (call_mcp_tool,
// list_resources, read_resource — see internal/tools/mcp.go). They need a
// live MCP connection, which this file installs.
//
// What this file owns
// -------------------
//
//  1. The one place that reads the MCP configuration for the *tools* path
//     and turns it into the client pool the three tools call through.
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
// The path set is not this file's fact: the `mcp` package owns it (see
// mcp/configpaths.go), so the writer of a configuration file and this
// reader cannot drift apart. This file only says *which workspace* the
// session runs in.
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
package setup

import (
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/mcp"
)

// InstallMCPProvider wires the clone's three MCP tools to the servers in
// the MCP configuration.
//
// The configuration is the merged set of the files the `mcp` package
// resolves: the documented global file, the reference binary's own file
// and — when the session runs in a workspace — the workspace's own file.
// The workspace's file wins over the global ones for the same server
// name.
//
// An empty workspace means the session has none, and the workspace file
// does not apply.
//
// It is safe to call more than once: the last call wins. The TUI calls it
// too, so the MCP tools work in both surfaces.
func InstallMCPProvider(agentDir, workspace string) {
	sources := mcp.ConfigPaths(agentDir, workspace)
	if len(sources) == 0 {
		return
	}
	// A file that cannot be parsed is dropped rather than half-loaded,
	// and the servers of the files that did load still work — so the
	// parse error is not fatal here.
	merged, _ := mcp.LoadMergedConfig(sources)
	web.SetMCPProvider(mcp.NewPool(merged.Specs()))
}
