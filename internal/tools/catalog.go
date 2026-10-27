// catalog.go defines the advertised tool surface of the reference agy binary,
// and a registry that presents those names to the model.
//
// Provenance
// ----------
// The set below is 【一手】 — captured live from the reference on 1.3.6 with
//
//	agy -p='x' --output-format stream-json
//
// whose first line is
//
//	{"event":"init","conversation_id":"…","init":{"cwd":"…","tools":[…],"permission_mode":"always-proceed"}}
//
// `init.tools` is the advertised list, already sorted by the reference
// (`printmode.buildInitPayload` → `advertisedTools` → `sort.Strings`). The
// capture was repeated from three working directories — a bare /tmp, a temp
// workspace holding .git and an .ipynb, and this Go repository — and the list
// was byte-identical every time, so the surface is fixed rather than
// workspace-filtered. The 60 names of that capture are checked in as
// testdata/advertised_tools_1.3.6.txt.
//
// 1.4.8 (【一手】, cycle `305-tool-descriptions`; the addresses re-derived
// in cycle 359): the binary knows **63**
// tool names — the three additions over the 1.4.3 capture are `code_search`,
// `moma_search` and `skill_search`. The list is a `[]string` global in the
// reference's rodata; its slice header sits at vmaddr `0x10b5ba450` and its
// backing array at `0x10b623bb0`, with a length of 63. The 63 names are
// checked in as testdata/tool_names_1.4.7.txt.
//
// The reference does **not** advertise all 63: a live session's
// `init.tools` holds 60 — the three searches are withheld (they need a
// service the CLI does not have). The catalogue therefore keeps advertising
// the 60 names the reference prints, and
// `TestAdvertisedListIsTheLiveSubset` pins the difference.
//
// This supersedes the earlier reconstruction, which was assembled from the
// `tools.New*ToolConverter` / `tools.New*Tool` symbol names in the pclntab.
// That approach (a) missed the 32 browser / subagent / marketplace / workflow
// tools, which have no dedicated converter symbol, and (b) advertised three
// names the reference never emits: codebase_search, notify_user and
// view_content_chunk. The MCP tool is advertised as call_mcp_tool.
//
// Descriptions
// ------------
// The descriptions in this file are the **reference's own text**, read out of
// the binary and re-verified against agy 1.4.8 in cycle 359 — every text
// in the fixture reconstructs from 1.4.8 literals — and committed at
// testdata/tool_descriptions_1.4.7.json; `refDesc` returns them. The
// twenty-three advertised names the reference describes nowhere — the
// twenty-two browser tools and `delete_knowledge` — keep the clone's own
// prose, and `run_workflow`, of which only fragments were recovered, keeps
// its own too. `browser_subagent` is described by the reference, but in its
// built-in agents package rather than in a tool layer (cycle 311).
//
// Executors
// ---------
// A spec with a real executor is backed by it and can be invoked end to end
// (run_command, the filesystem family, the git-free browser executors, …).
// Every other name is registered as a placeholder so the advertised surface
// matches the reference exactly and a call fails loudly with ErrNotImplemented
// rather than silently succeeding.
//
// Fidelity note: the reference advertises these through a Cortex protobuf tool
// schema; this reconstruction advertises the same NAMES through the
// OpenAI function-calling shape, which is what our gateway transport speaks. The
// names and the user-visible behaviour match; the wire encoding does not (and
// cannot, without a Cortex endpoint).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// refSpec is one advertised tool. back, when non-nil, is a real executor whose
// schema is the one advertised (the executor's own name is ignored; spec.name
// wins).
//
// desc is the reference's own description for the tool — the text in
// testdata/tool_descriptions_1.4.7.json, fetched with refDesc. For the
// twenty-four names the reference describes nowhere it is the clone's own
// prose instead.
type refSpec struct {
	name   string
	desc   string
	schema json.RawMessage
	back   Tool
}

// obj is a tiny helper for building JSON-schema object literals.
func obj(props string, required ...string) json.RawMessage {
	req := ""
	for i, r := range required {
		if i > 0 {
			req += ","
		}
		req += fmt.Sprintf("%q", r)
	}
	return json.RawMessage(fmt.Sprintf(
		`{"type":"object","properties":{%s},"required":[%s]}`, props, req))
}

func referenceSpecsPart1(ws string) []refSpec {
	return []refSpec{
		// ---- permissions / questions (real) ----
		{name: "ask_custom_permission", desc: refDesc("ask_custom_permission"),
			back: &AskCustomPermission{}},
		{name: "ask_permission", desc: refDesc("ask_permission"),
			back: &AskPermission{}},
		{name: "ask_question", desc: refDesc("ask_question"),
			back: &AskQuestion{}},

		// ---- browser control (executors connected to browsercdp) ----
		//
		// The browser tools' descriptions are the clone's own prose and
		// are marked as invented: the reference describes these names
		// nowhere (the other browser names sit in the MCP / knowledge
		// block below, for the same reason). `browser_subagent`, the last
		// entry here, is the exception — the reference describes it, in
		// its built-in agents package.
		{name: "browser_click_element", desc: "Click an element in the controlled browser page.",
			back: &BrowserClick{Workspace: ws}},
		{name: "browser_drag_pixel_to_pixel", desc: "Drag the pointer between two pixel coordinates.",
			back: &BrowserDrag{Workspace: ws}},
		{name: "browser_get_dom", desc: "Return the DOM of the controlled browser page.",
			back: &BrowserGetDOM{Workspace: ws}},
		{name: "browser_get_network_request", desc: "Inspect one captured network request.",
			back: &BrowserGetNetworkRequest{Workspace: ws}},
		{name: "browser_input", desc: "Type text into a focused browser field.",
			back: &BrowserInput{Workspace: ws}},
		{name: "browser_list_network_requests", desc: "List the network requests the browser made.",
			back: &BrowserListNetworkRequests{Workspace: ws}},
		{name: "browser_mouse_down", desc: "Press a mouse button at the current pointer position.",
			back: &BrowserMouseDown{Workspace: ws}},
		{name: "browser_mouse_up", desc: "Release a mouse button at the current pointer position.",
			back: &BrowserMouseUp{Workspace: ws}},
		{name: "browser_move_mouse", desc: "Move the pointer to a coordinate.",
			back: &BrowserMouseMove{Workspace: ws}},
		{name: "browser_press_key", desc: "Send a key press to the browser page.",
			back: &BrowserPressKey{Workspace: ws}},
		{name: "browser_refresh_page", desc: "Reload the current browser page.",
			back: &BrowserRefreshPage{Workspace: ws}},
		{name: "browser_resize_window", desc: "Resize the controlled browser viewport.",
			back: &BrowserResizeWindow{Workspace: ws}},
		{name: "browser_scroll", desc: "Scroll the browser page.",
			back: &BrowserScroll{Workspace: ws}},
		{name: "browser_scroll_dom", desc: "Scroll an element of the browser DOM.",
			back: &BrowserScrollDOM{Workspace: ws}},
		{name: "browser_select_option", desc: "Select an option in a browser dropdown.",
			back: &BrowserSelect{Workspace: ws}},
		{name: "browser_subagent", desc: refDesc("browser_subagent"),
			back: &BrowserSubagent{}},

		// ---- MCP / knowledge ----
		{name: "call_mcp_tool", desc: refDesc("call_mcp_tool"),
			back: &CallMcpTool{}},
		{name: "capture_browser_console_logs", desc: "Return the browser console log.",
			back: &BrowserConsole{Workspace: ws}},
		{name: "capture_browser_screenshot", desc: "Screenshot the controlled browser page.",
			back: &BrowserScreenshot{Workspace: ws}},
		{name: "click_browser_pixel", desc: "Click a pixel coordinate in the browser page.",
			back: &ClickBrowserPixel{Workspace: ws}},


		// ---- command execution (real) ----
		{name: "command_status", desc: refDesc("command_status"),
			back: &CommandStatus{}},
	}
}

func referenceSpecsPart2(ws string) []refSpec {
	return []refSpec{
		// ---- subagents / knowledge ----
		{name: "define_subagent", desc: refDesc("define_subagent")},
		{name: "delete_knowledge", desc: "Delete a stored knowledge item."},
		{name: "execute_browser_javascript", desc: "Evaluate JavaScript in the browser page.",
			back: &ExecuteBrowserJavaScript{Workspace: ws}},

		// ---- filesystem / search (real) ----
		{name: "find_by_name", desc: refDesc("find_by_name"),
			back: &FindByName{Workspace: ws}},
		{name: "finish", desc: refDesc("finish"),
			back: &FinishTool{}},
		{name: "generate_image", desc: refDesc("generate_image"),
			back: &GenerateImage{Workspace: ws}},
		{name: "grep_search", desc: refDesc("grep_search"),
			back: &GrepSearchTool{Workspace: ws}},

		// ---- subagents ----
		{name: "invoke_subagent", desc: refDesc("invoke_subagent")},
		{name: "list_browser_pages", desc: "List the pages the browser controls.",
			back: &ListBrowserPages{Workspace: ws}},

		// ---- filesystem / permissions / plugins ----
		{name: "list_dir", desc: refDesc("list_dir"),
			back: &ListDir{Workspace: ws}},
		{name: "list_permissions", desc: refDesc("list_permissions"),
			back: &ListPermissions{Workspace: ws}},
		{name: "list_plugin_accounts", desc: refDesc("list_plugin_accounts")},
		{name: "list_resources", desc: refDesc("list_resources"),
			back: &ListResources{}},

		// ---- inbox / subagents / tasks (real) ----
		{name: "manage_inbox", desc: refDesc("manage_inbox"),
			back: &ManageInbox{}},
		{name: "manage_subagents", desc: refDesc("manage_subagents")},
		{name: "manage_task", desc: refDesc("manage_task"),
			back: &ManageTask{}},


		// ---- filesystem edits (real) ----
		{name: "multi_replace_file_content", desc: refDesc("multi_replace_file_content"),
			back: &MultiReplaceFileContent{Workspace: ws}},

		// ---- notebooks (real) ----
		{name: "notebook_edit", desc: refDesc("notebook_edit"),
			back: &NotebookEdit{Workspace: ws}},
		{name: "notebook_execution", desc: refDesc("notebook_execution"),
			back: &NotebookExecution{Workspace: ws}},
	}
}

func referenceSpecsPart3(ws string) []refSpec {
	return []refSpec{
		// ---- browser navigation ----
		{name: "open_browser_url", desc: "Open a URL in the controlled browser.",
			back: &OpenBrowserURL{Workspace: ws}},
		{name: "read_browser_page", desc: "Read the current browser page as text.",
			back: &ReadBrowserPage{Workspace: ws}},

		// ---- resources / web (network) ----
		{name: "read_resource", desc: refDesc("read_resource"),
			back: &ReadResource{}},
		{name: "read_url_content", desc: refDesc("read_url_content"),
			back: &ReadURLContent{Workspace: ws}},
		{name: "replace_file_content", desc: refDesc("replace_file_content"),
			back: &EditFile{Workspace: ws}},
		{name: "run_command", desc: runCommandDescription(), schema: runCommandSchema,
			back: &RunCommand{Workspace: ws}},
		{name: "run_workflow", desc: "Run a workflow."},

		// ---- scheduling / marketplace / search ----
		{name: "schedule", desc: refDesc("schedule"),
			back: &ScheduleTool{}},
		{name: "search_marketplace", desc: refDesc("search_marketplace")},
		{name: "search_web", desc: refDesc("search_web"),
			back: &SearchWeb{Workspace: ws, Provider: performWebSearch}},
		{name: "sed_file", desc: refDesc("sed_file"),
			back: &SedFile{Workspace: ws}},

		// ---- command input / messaging (real) ----
		{name: "send_command_input", desc: refDesc("send_command_input"),
			back: &SendCommandInput{}},
		{name: "send_message", desc: refDesc("send_message"),
			back: &SendMessageTool{}},


		// ---- filesystem read/write (real) ----
		{name: "view_file", desc: viewFileDescription, schema: viewFileSchema,
			back: &ViewFile{Workspace: ws}},
		{name: "wait", desc: refDesc("wait"),
			back: &WaitTool{}},
		{name: "wait_5_seconds", desc: refDesc("wait_5_seconds")},
		{name: "write_to_file", desc: refDesc("write_to_file"),
			back: &CreateFile{Workspace: ws}},
	}
}

// referenceSpecs returns the advertised tool set for a workspace, in the
// reference's sorted order. Tools with a real executor are backed by it; the
// rest are placeholders so the advertised surface matches the reference
// exactly.
func referenceSpecs(ws string) []refSpec {
	specs := make([]refSpec, 0, 63)
	specs = append(specs, referenceSpecsPart1(ws)...)
	specs = append(specs, referenceSpecsPart2(ws)...)
	specs = append(specs, referenceSpecsPart3(ws)...)
	return specs
}

// Describer is the optional interface a Tool implements to expose the
// model-facing description the reference advertises for it. Tool does not
// require it — the description is not needed by the executor path — but
// anything that re-advertises the registry to a model (the pig loop bridge,
// a future Anthropic-compatible shim) does need it.
//
// The description text is the reference's, captured with the rest of the
// advertised surface; see the provenance note at the top of this file.
type Describer interface {
	Description() string
}

// Describe returns the tool's advertised description, or "" when it carries
// none. Callers that need a non-empty string should fall back to Name().
func Describe(t Tool) string {
	if d, ok := t.(Describer); ok {
		return d.Description()
	}
	return ""
}

// describedTool attaches the reference's description to an executor that was
// registered under its own name. The executor structs (ReadFile, RunCommand,
// …) predate the description requirement and carry no `desc` field; wrapping
// keeps referenceSpecs the single source of truth for the text.
type describedTool struct {
	Tool
	desc string
}

func (d *describedTool) Description() string { return d.desc }

// aliasTool implements Tool by delegating to a real executor under a
// different (reference) name.
type aliasTool struct {
	name, desc string
	schema     json.RawMessage
	inner      Tool
}

func (a *aliasTool) Name() string            { return a.name }
func (a *aliasTool) Schema() json.RawMessage { return a.schema }
func (a *aliasTool) Description() string     { return a.desc }
func (a *aliasTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return a.inner.Run(ctx, args)
}

// pendingTool is an advertised tool with no executor yet.
type pendingTool struct {
	name, desc string
	schema     json.RawMessage
}

func (p *pendingTool) Name() string            { return p.name }
func (p *pendingTool) Schema() json.RawMessage { return p.schema }
func (p *pendingTool) Description() string     { return p.desc }
func (p *pendingTool) Run(context.Context, json.RawMessage) (any, error) {
	return nil, fmt.Errorf("%s: %w", p.name, ErrNotImplemented)
}

// Catalog builds the advertised tool registry for a workspace.
func Catalog(ws string) *Registry {
	reg := NewRegistry()
	for _, s := range referenceSpecs(ws) {
		// Cycle 275: a captured spec schema (一手) outranks the executor's own
		// Schema(), even when names match. run_command and view_file used to
		// advertise the clone's reconstructed lowercase-tag shapes, so the
		// model was told `command` / `path` where the reference says
		// CommandLine / AbsolutePath.
		switch {
		case s.back != nil && s.back.Name() == s.name && s.schema == nil:
			reg.Register(&describedTool{Tool: s.back, desc: s.desc})
		case s.back != nil:
			schema := s.schema
			if schema == nil {
				schema = s.back.Schema()
			}
			if schema == nil {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			reg.Register(&aliasTool{name: s.name, desc: s.desc, schema: schema, inner: s.back})
		default:
			schema := s.schema
			if schema == nil {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			reg.Register(&pendingTool{name: s.name, desc: s.desc, schema: schema})
		}
	}
	return reg
}

// Dispatch runs a named tool with raw JSON arguments and renders its result as
// the text fed back to the model. Unknown tools and errors are reported as
// text rather than Go errors so the loop always has a result to feed back.
//
// Bash and Read tool results are additionally run through StripCitationFootnotes
// before being returned to the model. AGY 1.2.10 (cycle 230): search-result
// citation extraction was running on every tool result, so Bash / Read output
// was getting stray `[N] https://...` footnotes appended. The model never
// asked for them and they polluted the transcript.
func (r *Registry) Dispatch(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return "", err
	}
	var resText string
	switch v := out.(type) {
	case nil:
		return "", nil
	case string:
		if ShouldStripCitations(name) {
			v = StripCitationFootnotes(v)
		}
		resText = v
	default:
		b, mErr := json.Marshal(v)
		if mErr != nil {
			resText = fmt.Sprint(v)
		} else {
			s := string(b)
			if ShouldStripCitations(name) {
				s = StripCitationFootnotes(s)
			}
			resText = s
		}
	}
	budget := ApplyOutputBudget(resText, "", DefaultMaxResultTokens)
	return budget.Content, nil
}
