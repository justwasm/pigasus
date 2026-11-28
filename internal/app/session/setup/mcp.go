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
// The always-loaded surface
// -------------------------
// A server may flag some of its tools as always loaded (AGY 1.3.3, the
// `_meta: {"antigravity/alwaysLoad": true}` hint on `tools/list`). The
// reference then advertises those tools to the model directly, as
// `mcp_<server>_<tool>`, instead of leaving them reachable only through
// `call_mcp_tool`.
//
// This file is the one place that reads the hint: the pool it installs is
// the single owner of the servers' connections, and
// RegisterAlwaysLoadedTools advertises the flagged tools from the same
// pool. The tool surface picks them up through
// surface.RegisterPluginTool (see InstallMCPProvider's callers), so the
// tool the model is shown and the tool it can run are the same object.
package setup

import (
	"context"
	"time"

	"github.com/justwasm/pigasus/internal/app/tools/surface"
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
	pool := mcp.NewPool(merged.Specs())
	web.SetMCPProvider(pool)
	go installAlwaysLoadedTools(pool)
}

// alwaysLoadSweepBudget bounds the sweep that reads every server's
// `tools/list`.
//
// It is a backstop, not the mechanism that keeps startup fast: the sweep
// runs off the startup path (see installAlwaysLoadedTools), so this only
// stops a wedged server from holding a goroutine for the documented
// five-minute connect limit.
//
// It cannot be tight. The SDK's (*Client).Connect does not return when its
// context expires — measured at 1.5.18, it returns after
// `budget + 5s` for any budget, because it sends a best-effort
// `notifications/cancelled` and waits out `notifyCancellationTimeout`
// before unwinding. A 5s budget therefore costs 10s of wall clock, which is
// exactly why the sweep is not on the startup path.
const alwaysLoadSweepBudget = 5 * time.Second

// installAlwaysLoadedTools advertises the tools the configured servers
// flagged as always loaded, and unregisters the ones they no longer flag.
//
// It runs on its own goroutine: the sweep connects to every configured
// server, which the pool otherwise does not do until a tool call names one,
// and a server that is slow or wedged must not delay the session's start.
// The cost of running it late is bounded and small — a flagged tool is
// advertised a moment after the first turn begins instead of before it,
// and until then it is reachable through `call_mcp_tool` like any other
// lazy tool.
//
// The listing is the only `tools/list` the clone makes. A server that
// cannot be reached contributes nothing rather than failing the install —
// one broken server must not cost the model the tools of the healthy ones
// — so a workspace with an unreachable server still gets its lazy tools
// and whatever the other servers flagged.
//
// Registration goes through surface.RegisterPluginTool, which is the seam
// the advertised declaration list already merges
// (toolrunner.AdvertisedToolDecls*), so the tool the model is shown and the
// tool the registry can run are the same object.
func installAlwaysLoadedTools(pool *mcp.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), alwaysLoadSweepBudget)
	defer cancel()
	listed := pool.AlwaysLoadedTools(ctx)
	flagged := make([]web.AlwaysLoadedTool, 0, len(listed))
	wanted := make(map[string]bool, len(listed))
	for _, t := range listed {
		flagged = append(flagged, web.AlwaysLoadedTool{
			Server:      t.Server,
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
		wanted[web.AlwaysLoadedToolName(t.Server, t.Name)] = true
	}
	for _, t := range web.AlwaysLoadedExecutors(flagged, pool) {
		surface.RegisterPluginTool(t)
		web.NoteAlwaysLoadedAdvertised(t.Name())
	}
	// Withdraw the tools a previous install advertised and this one no
	// longer finds: a server removed from the configuration, or a server
	// that stopped flagging a tool, must not leave a dead name behind.
	for _, name := range web.AlwaysLoadedToolNames() {
		if !wanted[name] {
			surface.UnregisterPluginTool(name)
			web.ForgetAlwaysLoadedAdvertised(name)
		}
	}
}
