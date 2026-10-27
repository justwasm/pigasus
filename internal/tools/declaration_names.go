// Package tools — declaration_names.go: the two spellings of one tool.
//
// The reference publishes a tool under two names:
//
//   - the **declaration** name — the twelve function declarations the model
//     receives (read_file, write_file, edit_file, run_command, search_web,
//     send_message, manage_task, schedule, invoke_subagent, define_subagent,
//     manage_subagents, ask_question). 【一手】
//     internal/tools/model_tools_1.4.6.json, byte-identical in 1.4.3–1.4.8.
//   - the **catalogue** name — the sixty-odd names the advertised surface
//     (init.tools) names, which is also the vocabulary a `--agent` custom
//     agent's own tool set uses. 【一手】
//     internal/tools/testdata/agent_decls_1.4.7.json carries the seven
//     declarations a custom agent sends: send_message, view_file,
//     read_url_content, search_web, schedule, generate_image, manage_task —
//     five of them catalogue names, none of them the twelve's.
//
// The two surfaces do the same work under different names, so a rule the
// user wrote for one has to reach the other. This file is the mapping.
//
// 【推测】 The pairing itself: the reference's binary does not publish a
// table, so each pair is read off the two surfaces' own descriptions — the
// catalogue's `view_file` reads a file and the declaration's `read_file`
// reads a file; the catalogue's `write_to_file` creates a file and the
// declaration's `write_file` writes one; the catalogue's editing family
// (replace_file_content, multi_replace_file_content, sed_file,
// notebook_edit, notebook_execution) and the declaration's `edit_file` all
// change an existing file. The pairs are one-to-one except for the editing
// family, which collapses onto the single `edit_file`.
//
// A name that has no declaration counterpart (the MCP tools, the browser
// tools, grep_search, list_dir, …) maps to itself: the reference's own
// rules for those are written against the catalogue name, because that is
// the only name they have.
package tools

// declarationAliases maps a catalogue tool name to the model-facing
// declaration that does the same work.
//
// The keys are the catalogue's own names; the values are the twelve
// declarations' names.
var declarationAliases = map[string]string{
	// reads
	"view_file": "read_file",

	// creates and writes
	"write_to_file": "write_file",

	// edits
	"replace_file_content":       "edit_file",
	"multi_replace_file_content": "edit_file",
	"sed_file":                   "edit_file",
	"notebook_edit":              "edit_file",
	"notebook_execution":         "edit_file",

	// commands
	"run_command": "run_command",

	// the web
	"search_web": "search_web",

	// the agent-to-agent surface
	"send_message":      "send_message",
	"define_subagent":   "define_subagent",
	"invoke_subagent":   "invoke_subagent",
	"manage_subagents":  "manage_subagents",
	"manage_task":       "manage_task",
	"schedule":          "schedule",
	"ask_question":      "ask_question",
}

// DeclarationName returns the model-facing declaration name of a catalogue
// tool, or "" when the tool has none.
//
// The empty string is the honest answer for the MCP tools, the browser
// tools and the rest of the catalogue: the reference's twelve declarations
// do not cover them, and a rule for them is written against the catalogue
// name alone.
func DeclarationName(catalogueName string) string {
	return declarationAliases[catalogueName]
}

// PermissionNames returns every name a tool call may be judged under.
//
// The call's own name comes first, then the declaration name it is known
// by. A permission rule the user wrote for the parent's spelling has to
// reach a subagent's call under the catalogue's spelling, and the other way
// round — the permission engine judges the call under each of them and
// takes the strictest verdict (see internal/permissions.Policy.DecideNames).
//
// The list never repeats a name, and a tool with no declaration counterpart
// yields its own name alone.
func PermissionNames(name string) []string {
	if name == "" {
		return nil
	}
	alias := DeclarationName(name)
	if alias == "" || alias == name {
		return []string{name}
	}
	return []string{name, alias}
}
