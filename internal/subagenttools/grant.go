package subagenttools

import (
	piagent "github.com/MichaelKinsy/PiG/agent"
)

// Grant is the tool authorization a subagent type carries.
//
// 【一手】 agy 1.4.6's captured `define_subagent` schema carries exactly
// three switches, and the tool's own description fixes the base set:
//
//	EnableWriteTools    "Grant tools to create and edit files, and run commands."
//	EnableMCPTools      "Grant access to MCP tools."
//	EnableSubagentTools "Grant tools to define and invoke its own subagents."
//
//	"Subagents always have tools to research the codebase and to
//	 communicate with other agents."
//
// So every subagent gets the research tools and the messaging tool, and
// the three switches add the write tools, the MCP tools and the
// subagent tools. The last one is what makes a subagent able to open
// subagents of its own.
type Grant struct {
	// Write grants the file-editing and command tools.
	Write bool
	// MCP grants the MCP tools.
	MCP bool
	// Subagent grants define_subagent and invoke_subagent — the
	// reference's own "define and invoke its own subagents".
	Subagent bool
}

// The catalogue names, grouped by what the grant text says they do.
//
// 【一手】 the 1.4.7 tool descriptions (internal/tools/testdata/
// tool_descriptions_1.4.7.json) mark the converter layer's tools. The
// read-only ones are the research set, the ones that write are the
// write set, and the three MCP ones are the MCP set.
//
// The names are the clone's catalogue names (the reference's
// init.tools surface), not the twelve model-facing declarations: a
// subagent's tool set is drawn from the catalogue, the way the parent
// draws its own executors from it.
var (
	// researchToolNames are the always-on research tools: explore the
	// codebase (grep_search, list_dir), search the web (search_web,
	// read_url_content) and read files (view_file).
	researchToolNames = []string{
		"view_file",
		"grep_search",
		"list_dir",
		"search_web",
		"read_url_content",
	}

	// writeToolNames are the tools the write grant adds: create and
	// edit files (write_to_file, replace_file_content,
	// multi_replace_file_content, notebook_edit, sed_file) and run
	// commands (run_command, command_status).
	writeToolNames = []string{
		"write_to_file",
		"replace_file_content",
		"multi_replace_file_content",
		"notebook_edit",
		"sed_file",
		"run_command",
		"command_status",
	}

	// mcpToolNames are the tools the MCP grant adds.
	mcpToolNames = []string{
		"call_mcp_tool",
		"list_resources",
		"read_resource",
	}
)

// grantedNames returns the catalogue tool names the grant covers.
//
// The research names are always there: the reference's own sentence
// says every subagent can research the codebase.
func (g Grant) grantedNames() []string {
	out := make([]string, 0, len(researchToolNames)+len(writeToolNames)+len(mcpToolNames))
	out = append(out, researchToolNames...)
	if g.Write {
		out = append(out, writeToolNames...)
	}
	if g.MCP {
		out = append(out, mcpToolNames...)
	}
	return out
}

// All returns the grant with every switch on. It is the reference's
// `self` type — "the parent agent's full configuration".
func (g Grant) All() Grant {
	return Grant{Write: true, MCP: true, Subagent: true}
}

// grantedToolSet returns the tools a subagent with the given grant may
// call.
//
// The set is the reference's own: the research tools and the messaging
// tool are always there, and the grant's switches add the rest.
//
// The messaging tool is this package's own send_message — the
// catalogue's send_message is the background command's stdin, while the
// reference's send_message is "Send a message to another agent, such as
// a subagent or a peer".
//
// The subagent grant adds this package's define_subagent and
// invoke_subagent, wired to the same definition store and the same
// deps: that is what makes a granted subagent able to define and open
// subagents of its own, recursively.
func grantedToolSet(deps Deps, defs *Store, grant Grant) []piagent.AgentTool {
	out := make([]piagent.AgentTool, 0, len(researchToolNames)+4)
	out = append(out, selectByName(deps.Catalogue, grant.grantedNames())...)
	out = append(out, &sendMessageTool{registry: deps.Registry, owner: deps.ConversationID})
	if grant.Subagent {
		out = append(out, &defineTool{defs: defs}, &invokeTool{deps: deps, defs: defs})
	}
	return out
}

// selfToolSet returns the tools the reference's `self` type inherits:
// the parent's own tool set, with the four subagent tools replaced by
// this package's own implementations.
//
// The catalogue's own define_subagent / invoke_subagent /
// manage_subagents are the reference's catalogue placeholders (they
// answer "not implemented"), and its send_message is the background
// command's stdin — the parent's own surface replaces all four with
// this package's real ones, and so does the child's.
func selfToolSet(deps Deps, defs *Store) []piagent.AgentTool {
	out := make([]piagent.AgentTool, 0, len(deps.Catalogue)+4)
	for _, t := range deps.Catalogue {
		if t == nil {
			continue
		}
		switch t.Name() {
		case "send_message", "define_subagent", "invoke_subagent", "manage_subagents":
			continue
		}
		out = append(out, t)
	}
	return append(out, subagentToolSet(deps, defs)...)
}

// subagentToolSet returns this package's own four tools.
func subagentToolSet(deps Deps, defs *Store) []piagent.AgentTool {
	return []piagent.AgentTool{
		&sendMessageTool{registry: deps.Registry, owner: deps.ConversationID},
		&defineTool{defs: defs},
		&invokeTool{deps: deps, defs: defs},
		&manageSubagentsTool{registry: deps.Registry, owner: deps.ConversationID},
	}
}

// selectByName picks the named tools out of the parent's catalogue.
//
// A name the catalogue does not carry is skipped: the grant says what
// the subagent *may* call, and a tool the parent does not have is not
// something the child can have either.
func selectByName(catalogue []piagent.AgentTool, names []string) []piagent.AgentTool {
	if len(catalogue) == 0 || len(names) == 0 {
		return nil
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var out []piagent.AgentTool
	for _, t := range catalogue {
		if t == nil || !want[t.Name()] {
			continue
		}
		delete(want, t.Name())
		out = append(out, t)
	}
	return out
}

// toolNames lists the names of a tool set, in order.
func toolNames(list []piagent.AgentTool) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		if t != nil {
			out = append(out, t.Name())
		}
	}
	return out
}
