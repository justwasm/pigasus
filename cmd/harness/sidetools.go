// sidetools.go — the tool surface a harness session advertises.
//
// # The client owns the surface
//
// The reference harness does not have a fixed tool list. The client tells
// it which tools exist through `HarnessConfig.harness_side_tools`, and the
// harness advertises exactly those.
//
// The SDK builds that field from the session's `CapabilitiesConfig`:
//
//	find                → FIND_FILE
//	run_command         → RUN_COMMAND
//	subagents           → INVOKE_SUBAGENT
//	user_questions      → ASK_QUESTION
//	file_edit           → EDIT_FILE
//	view_file           → VIEW_FILE
//	write_to_file       → CREATE_FILE
//	grep_search         → SEARCH_DIR
//	list_dir            → LIST_DIR
//	generate_image      → GENERATE_IMAGE
//	search_web          → SEARCH_WEB
//	read_url_content    → READ_URL_CONTENT
//	schedule            → SCHEDULE
//	run_workflow        → RUN_WORKFLOW
//	manage_task         → RUN_COMMAND or SCHEDULE
//
// The SDK's default capability set enables everything except `ask_question`,
// `list_directory`, `search_directory` and `find_file`.
//
// # The names are the harness's
//
// The harness's tool names are not the CLI's: it reads a file with
// `view_file`, writes one with `write_to_file` and edits one with
// `replace_file_content`. The declarations the model sees come from the
// reference harness's own capture (`internal/tools/harness_tools_1.4.7.json`).
//
// # The client's own tools
//
// `HarnessConfig.tools` carries the client's tools — the SDK's Python
// callables. They are advertised alongside the harness-side ones and
// executed by the client: the harness asks, the client answers.
package main

import (
	"encoding/json"
	"sort"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/internal/tools"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// sideToolNames maps the protocol's `HarnessSideTools` onto the
// model-facing tool names.
//
// The keys are the harness's tool names, not the protocol's field names:
// the protocol groups several tools under one config message and the
// harness names each one separately.
func sideToolNames(side *pb.HarnessSideTools) map[string]bool {
	out := map[string]bool{}
	if side == nil {
		return out
	}
	if side.GetFind().GetEnabled() {
		out["find_by_name"] = true
	}
	if side.GetRunCommand().GetEnabled() {
		out["run_command"] = true
	}
	if side.GetSubagents().GetEnabled() {
		out["invoke_subagent"] = true
		out["define_subagent"] = true
		out["manage_subagents"] = true
	}
	if side.GetUserQuestions().GetEnabled() {
		out["ask_question"] = true
	}
	if side.GetFileEdit().GetEnabled() {
		out["replace_file_content"] = true
	}
	if side.GetViewFile().GetEnabled() {
		out["view_file"] = true
	}
	if side.GetWriteToFile().GetEnabled() {
		out["write_to_file"] = true
	}
	if side.GetGrepSearch().GetEnabled() {
		out["grep_search"] = true
	}
	if side.GetListDir().GetEnabled() {
		out["list_dir"] = true
	}
	if side.GetGenerateImage().GetEnabled() {
		out["generate_image"] = true
	}
	if side.GetSearchWeb().GetEnabled() {
		out["search_web"] = true
	}
	if side.GetReadUrlContent().GetEnabled() {
		out["read_url_content"] = true
	}
	if side.GetManageTask().GetEnabled() {
		out["manage_task"] = true
	}
	if side.GetSchedule().GetEnabled() {
		out["schedule"] = true
	}
	if side.GetRunWorkflow().GetEnabled() {
		out["run_workflow"] = true
	}
	return out
}

// alwaysOnTools are the harness-side tools the reference advertises
// whatever the configuration says.
//
// `send_message` is the messaging channel every session has, and
// `finish` is the tool that ends a conversation — the client names its
// schema, but the tool itself is always there.
func alwaysOnTools() map[string]bool {
	return map[string]bool{
		"send_message": true,
		"finish":       true,
	}
}

// sideToolSubset returns the names the session advertises.
func (c *conversation) sideToolSubset() map[string]bool {
	out := alwaysOnTools()
	for name, ok := range sideToolNames(c.settings.SideTools) {
		out[name] = ok
	}
	return out
}

// harnessDeclarations returns the harness's declarations, filtered to the
// enabled tools.
//
// The order is the capture's order: the reference sends the declarations
// in a fixed order and the model reads them in that order.
//
// The `finish` declaration is the one the client owns: the reference
// advertises it only when the client named a schema for it, and it sorts
// into the list by name — between `find_by_name` and `generate_image`.
func harnessDeclarations(enabled map[string]bool, finishSchema json.RawMessage) []tools.ModelToolDecl {
	decls := tools.HarnessDeclarations()
	out := make([]tools.ModelToolDecl, 0, len(decls)+1)
	for _, decl := range decls {
		if enabled[decl.Name] {
			out = append(out, decl)
		}
	}
	if len(finishSchema) > 0 && enabled["finish"] {
		out = insertFinishDeclaration(out, finishSchema)
	}
	return out
}

// insertFinishDeclaration puts the client's `finish` declaration into the
// list at the position its name sorts into.
func insertFinishDeclaration(decls []tools.ModelToolDecl, schema json.RawMessage) []tools.ModelToolDecl {
	decl := tools.ModelToolDecl{
		Name:        "finish",
		Description: finishDescription(),
		Parameters:  schema,
	}
	for i, existing := range decls {
		if existing.Name > decl.Name {
			out := make([]tools.ModelToolDecl, 0, len(decls)+1)
			out = append(out, decls[:i]...)
			out = append(out, decl)
			out = append(out, decls[i:]...)
			return out
		}
	}
	return append(decls, decl)
}

// finishDescription is the reference's own text for the `finish` tool.
//
// The reference reads it out of the converter layer, and the capture of
// that layer is the fixture the tools package carries.
func finishDescription() string {
	if desc, ok := tools.ReferenceDescriptions()["finish"]; ok {
		return desc.Text
	}
	return ""
}

// filterToolsByName keeps the tools whose name is in the set.
func filterToolsByName(list []piagent.AgentTool, enabled map[string]bool) []piagent.AgentTool {
	out := make([]piagent.AgentTool, 0, len(list))
	for _, tool := range list {
		if tool == nil {
			continue
		}
		if enabled[tool.Name()] {
			out = append(out, tool)
		}
	}
	return out
}

// enabledToolNames lists the enabled names, sorted. It exists for the
// log line that reports the session's surface.
func enabledToolNames(enabled map[string]bool) []string {
	out := make([]string, 0, len(enabled))
	for name := range enabled {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
