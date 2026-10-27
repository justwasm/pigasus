// browser_subagent.go — the reference's `browser_subagent` tool.
//
// The tool hands a browsing task to a browser agent — a subagent whose tool
// set is the browser-control surface — and answers with the agent's final
// text.
//
// Provenance
// ----------
// 【一手】 the name is one of the sixty-three the 1.4.8 binary knows
// (testdata/tool_names_1.4.7.txt), and the reference's production variants
// carry it next to the other subagent tools.
//
// 【反编译】 the reference's step for the call is `CortexStepBrowserSubagent`,
// whose fields are task / result / task_name / task_summary / recording_name
// / recording_path / recording_generation_status / subagent_id /
// reused_subagent_id / skipped / scratchpad_path / media
// (out/local-macos/protos/cortex_1.4.6.proto). The tool takes the *task*
// and answers with the *result*.
//
// 【strings】 the description is the reference's browser-agent description
// (the Chrome variant, at 0x1036c8c48); it is committed in
// testdata/tool_descriptions_1.4.7.json.
//
// The executor underneath
// ----------------------
// The tool calls through the BrowserSubagentProvider seam rather than a
// concrete runner, so internal/tools stays free of any dependency on the
// delegation machinery; the concrete runner lives in internal/browsersubagent
// and is installed with SetBrowserSubagentProvider. The seam mirrors the one
// the three MCP tools use (see mcp.go).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// BrowserSubagentProvider is the seam the browser_subagent tool uses to reach
// a browser agent.
//
// It returns the *text the model sees* — the browser agent's final answer.
type BrowserSubagentProvider interface {
	// RunBrowserTask runs one browsing task in a browser agent and returns
	// the agent's final answer.
	RunBrowserTask(ctx context.Context, task string) (string, error)
}

var (
	browserSubagentMu       sync.RWMutex
	browserSubagentProvider BrowserSubagentProvider
)

// SetBrowserSubagentProvider installs the process-wide provider the
// browser_subagent tool calls through, and returns the previous one.
//
// The provider is process-wide for the same reason the MCP one is: the
// catalogue is built by tools.Catalog, which takes no provider argument and
// is called from two places (the print path and the TUI). A test that needs
// its own provider should set the field on the executor instead.
func SetBrowserSubagentProvider(p BrowserSubagentProvider) BrowserSubagentProvider {
	browserSubagentMu.Lock()
	defer browserSubagentMu.Unlock()
	prev := browserSubagentProvider
	browserSubagentProvider = p
	return prev
}

// DefaultBrowserSubagentProvider returns the process-wide provider, or nil
// when none is installed.
func DefaultBrowserSubagentProvider() BrowserSubagentProvider {
	browserSubagentMu.RLock()
	defer browserSubagentMu.RUnlock()
	return browserSubagentProvider
}

// browserSubagentSchema is the declaration for browser_subagent.
//
// The reference builds this schema by hand — it is not one of the reflected
// shapes (the 1.4.8 binary's `utils.ToJsonSchemaString` instantiations carry
// no browser_subagent struct, 【反编译】) — so the exact JSON is not
// recoverable. The shape below follows the step's own field name: the task.
//
// The parameter is named `task`, the reference's own word for it: the step's
// first field is `task` and the reference's description says the agent acts
// "with the given task description".
var browserSubagentSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"task":{"type":"string","description":"The task the browser agent should perform."}` +
	`},"required":["task"]}`)

// BrowserSubagent is the reference's browser_subagent tool.
type BrowserSubagent struct {
	// Provider is the runner the tool calls through. Nil uses the
	// process-wide provider (see SetBrowserSubagentProvider).
	Provider BrowserSubagentProvider
}

func (t *BrowserSubagent) Name() string        { return "browser_subagent" }
func (t *BrowserSubagent) Description() string { return refDesc("browser_subagent") }
func (t *BrowserSubagent) Schema() json.RawMessage {
	return browserSubagentSchema
}

func (t *BrowserSubagent) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var fields map[string]json.RawMessage
	if len(args) > 0 {
		if err := json.Unmarshal(args, &fields); err != nil {
			return nil, fmt.Errorf("invalid tool arguments: %w", err)
		}
	}
	task := browserSubagentArgString(fields, "task")
	if task == "" {
		return nil, fmt.Errorf("task is a required parameter. Follow the function call schema exactly.")
	}
	provider := t.Provider
	if provider == nil {
		provider = DefaultBrowserSubagentProvider()
	}
	if provider == nil {
		return nil, fmt.Errorf("browser_subagent is not available in this session")
	}
	return provider.RunBrowserTask(ctx, task)
}

// browserSubagentArgString reads an argument as a string.
//
// The exact spelling wins; otherwise the argument is matched on its
// canonical (snake_case) form, so `task` and `Task` both resolve.
func browserSubagentArgString(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
