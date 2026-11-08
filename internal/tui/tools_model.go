package tui

// tools_model.go — the tool runtime sub-model.
//
// ToolsModel owns the session's advertised tool declarations, the runner
// that executes them, the sandbox installed on the tool gate and the
// catalogue the browser/subagent surfaces read. It is runtime wiring, not
// view state, so it does not belong in the transport bucket.
//
// Upstream: the reference's RootModel keeps its tool surface beside the
// turn engine; the clone gives that surface a named owner.

import (
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
)

// ToolsModel owns the advertised tools and the sandbox that gates them.
type ToolsModel struct {
	decls   []backend.ToolDecl
	runner  backend.ToolRunner
	sandbox sandbox.Sandbox
	catalog *tools.Registry
}
