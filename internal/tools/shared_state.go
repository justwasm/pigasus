// Package tools — shared_state.go: the package-level subagent manager and
// task registry.
//
// Cycle 346: these three symbols used to live in stubs.go, next to the
// 28 v4 stub tools that are registered nowhere (the catalogue in
// catalog.go is the live surface — it holds its own `FinishTool`,
// `WaitTool`, `SendMessageTool`, …). The registry outlived the stubs
// because the /tasks panel and the status line read it, so it moved
// here rather than going with them.
package tools

import (
	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// defaultSubagentManager tracks Subagents spawned by the create_task tool so
// that subsequent send_message calls can look them up by ID. It is
// package-level so it survives across multiple Tool.Run invocations within
// the same process (the typical CLI / TUI lifetime). Locking is provided
// by Manager's RWMutex.
var defaultSubagentManager = subagent.NewManager()

// defaultTaskRegistry is the tasks-panel registry shared between
// create_task / send_message and the runMode's /tasks panel. The
// tasks-panel cycle makes /tasks and the status-line "· N tasks"
// segment both consume this registry, so every Subagent spawned by
// the bash wrapper in create_task (or its ddgr cousins) is visible
// in the panel.
//
// The registry is intentionally package-level (not on a per-Model
// basis) because the bash wrapper itself is package-level; the
// future per-session scoping TODO in
// docs/cycles/tasks-panel/deliverable-tasks.md will thread the
// registry through Model.ToolRunner and remove this global.
var defaultTaskRegistry = taskregistry.New()

// SetDefaultTaskRegistry replaces the package-level registry. It
// exists so tests can drive the wiring with a fresh registry, and
// so the runMode can inject its own registry at start-up if it
// prefers per-session isolation. Pass nil to allocate a fresh one
// (and panic-free reset between test runs).
func SetDefaultTaskRegistry(r *taskregistry.Registry) *taskregistry.Registry {
	if r == nil {
		r = taskregistry.New()
	}
	prev := defaultTaskRegistry
	defaultTaskRegistry = r
	return prev
}

// SharedTaskRegistry returns the package-level task registry. The
// tasks-panel cycle wires the cmd/wagyu runMode against this so
// every spawn done by internal/subagent (the bash wrapper in
// create_task and the ddgr/grep search tools) is visible in /tasks
// and the status-line "· N tasks" segment.
//
// The pointer is stable for the lifetime of the process; callers
// should NOT close the registry.
func SharedTaskRegistry() *taskregistry.Registry { return defaultTaskRegistry }

// SharedSubagentManager returns the package-level subagent manager.
// The /tasks panel uses it to translate a "kill this row" action
// into a subagent.Kill. The pointer is stable for the lifetime of
// the process.
func SharedSubagentManager() *subagent.Manager { return defaultSubagentManager }
