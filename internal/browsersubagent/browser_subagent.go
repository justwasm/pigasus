// Package browsersubagent implements the reference's `browser_subagent` tool.
//
// What the reference's tool is
// -----------------------------
// 【一手】 `browser_subagent` is one of the sixty-three tool names the 1.4.8
// binary knows (internal/tools/testdata/tool_names_1.4.7.txt), and the
// reference's production variants carry it next to the other subagent tools
// (`invoke_subagent`, `define_subagent`, `manage_subagents`,
// `send_message`) — so it is a *tool*, not an agent type.
//
// 【反编译】 the step the reference produces for the call is
// `CortexStepBrowserSubagent`, whose fields are
//
//	task, result, task_name, task_summary, recording_name, recording_path,
//	recording_generation_status, subagent_id, reused_subagent_id, skipped,
//	scratchpad_path, media
//
// (out/local-macos/protos/cortex_1.4.6.proto). The tool therefore takes a
// *task* and answers with a *result*.
//
// 【strings】 the tool's description is the reference's browser-agent
// description. Four variants of it live in the binary, all in
// `cortex/customizations/builtin/agents`:
//
//	0x1036c8c48  the Chrome variant
//	0x1036d8b82  the Chrome variant, with the recording note
//	0x1036da128  the webview variant (script harness)
//	0x1036db1f8  the webview variant (the built-in webview browser)
//
// The clone ships the first one — the Chrome variant without the recording
// note — because the clone drives Chrome over the DevTools Protocol
// (browsercdp) and records nothing.
//
// 【反编译】 the reference's browser agent is a real agent: the
// `cortex/customizations/builtin/agents` package carries `BrowserAgent`,
// `productBrowserAgent`, `BrowserAgentWithConfig`, `browserWebviewAgent`,
// `AddBrowserToMainAgent` and `WithBrowserBackendInstructions`, and the
// launch path is
// `oneharness/internal/tools/tools.resolveBrowserSubagentBackend`
// (0x101dec530),
// called from `(*InvokeSubagentTool).launchSubagent`. Its prompt lead is the
// reference's own sentence "You are a browser agent." (0x1035588eb).
//
// What the clone's tool does
// --------------------------
// The reference's browser agent drives the browser through the
// chrome-devtools MCP server. The clone has no MCP browser server; its
// browser surface is its own CDP client, and the browser tools in
// internal/tools are the clone's equivalent of that MCP surface. So the
// clone's `browser_subagent` launches a subagent whose tool set is exactly
// the browser tools, and returns the subagent's final answer.
//
// The delegation machinery is internal/subagenttools' — the same one
// `invoke_subagent` uses. This package only supplies the browser agent's
// prompt, its tool set, and the call itself.
package browsersubagent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// ToolName is the advertised name of the tool.
const ToolName = "browser_subagent"

// PromptLead is the reference's own opening sentence for the browser agent
// (【strings】 the literal at 0x1035588eb).
const PromptLead = "You are a browser agent."

// BrowserToolNames are the browser-control tools the browser agent may call.
//
// They are the clone's twenty-three browser tools — the same names the
// reference advertises (internal/tools/testdata/tool_names_1.4.7.txt). The
// reference's browser agent calls the chrome-devtools MCP tools instead;
// the clone's browser surface is its own CDP client, so the equivalent
// inventory is these.
var BrowserToolNames = []string{
	"browser_click_element",
	"browser_drag_pixel_to_pixel",
	"browser_get_dom",
	"browser_get_network_request",
	"browser_input",
	"browser_list_network_requests",
	"browser_mouse_down",
	"browser_mouse_up",
	"browser_move_mouse",
	"browser_press_key",
	"browser_refresh_page",
	"browser_resize_window",
	"browser_scroll",
	"browser_scroll_dom",
	"browser_select_option",
	"capture_browser_console_logs",
	"capture_browser_screenshot",
	"click_browser_pixel",
	"execute_browser_javascript",
	"list_browser_pages",
	"open_browser_url",
	"read_browser_page",
}

// Prompt builds the browser agent's system prompt.
//
// The opening sentence is the reference's own (【strings】). The rest is the
// clone's: the reference assembles its browser prompt from named sections
// ('Identity', 'Tools', 'Recording', 'MCP Connection Troubleshooting' —
// 【strings】) whose bodies live in a part of the binary the string table
// does not expose, so the clone writes its own inventory of the tools it
// actually has.
//
// The reference also tells its browser agent never to launch Chrome itself
// ("**Do NOT Attempt to Launch Chrome**", 【strings】 0x1036db503) because the
// reference attaches to the *user's* Chrome over remote debugging. The
// clone's browser client launches its own headless Chrome
// (browsercdp), so that rule does not apply here and is not
// carried over.
//
// The lead sentence follows the surface. The managed surface speaks of
// the Chrome DevTools Protocol and of the Chrome the clone drives; the
// live surface speaks of the user's own browser, which the agent reaches
// through the Open Claude in Chrome extension — there the rule about
// never launching a browser is true again, because the browser is
// already there and the agent must not start another one.
func Prompt(inventory []string, surface Surface) string {
	var b strings.Builder
	b.WriteString(PromptLead)
	b.WriteString("\n\n")
	switch surface {
	case SurfaceLive:
		b.WriteString("You drive the user's own Chrome browser, through the Open Claude in Chrome extension. ")
		b.WriteString("It is the browser they already have open: their tabs, their logins, their session. ")
		b.WriteString("Do not start another browser and do not ask them to sign in again. ")
	default:
		b.WriteString("You drive a real Chrome browser over the Chrome DevTools Protocol. ")
	}
	b.WriteString("Complete the task you were given using the browser tools; ")
	b.WriteString("never answer from memory what the page can tell you.\n\n")
	b.WriteString("Tools:\n")
	for _, line := range inventory {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\nWhen the task is done, answer with what the browser found — ")
	b.WriteString("the text or the values the task asked for — and nothing else.\n")
	return b.String()
}

// Runner runs a browsing task in a browser agent.
//
// The zero value is not usable: build one with New.
type Runner struct {
	deps  subagenttools.Deps
	tools []piagent.AgentTool
}

// New builds a Runner.
//
// browserTools are the tools the browser agent may call; they are handed to
// the subagent's session as its own tool set. The caller normally gets them
// from BrowserTools below.
func New(deps subagenttools.Deps, browserTools []piagent.AgentTool) *Runner {
	return &Runner{deps: deps, tools: browserTools}
}

// BrowserTools collects the browser tools out of a registry and adapts them
// to PiG's tool interface.
//
// A registry that does not carry a tool is skipped, so a registry built from
// a restricted surface (the --agent body path) simply yields fewer tools.
func BrowserTools(reg *tools.Registry) []piagent.AgentTool {
	if reg == nil {
		return nil
	}
	var out []piagent.AgentTool
	for _, name := range BrowserToolNames {
		tool, ok := reg.Get(name)
		if !ok || tool == nil {
			continue
		}
		out = append(out, piagentbridge.Tool(tool))
	}
	return out
}

// inventory renders the tool list for the prompt: the tool's advertised
// description when it has one, the name otherwise.
func inventory(agentTools []piagent.AgentTool) []string {
	out := make([]string, 0, len(agentTools))
	for _, t := range agentTools {
		if t == nil {
			continue
		}
		line := t.Name()
		if desc := t.Schema().Description; desc != "" {
			line += " — " + desc
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

// RunBrowserTask runs one browsing task in the browser agent and returns the
// agent's final answer.
//
// The task is the tool's `task` argument, verbatim: the reference hands the
// task description to the browser agent unchanged (the step's `task` field
// is the same text).
func (r *Runner) RunBrowserTask(ctx context.Context, task string) (string, error) {
	if strings.TrimSpace(task) == "" {
		return "", fmt.Errorf("task is required")
	}
	res, err := subagenttools.Invoke(ctx, subagenttools.Invocation{
		Prompt:       task,
		SystemPrompt: Prompt(inventory(r.tools), SurfaceOf(r.tools)),
		Tools:        r.tools,
	}, r.deps)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
