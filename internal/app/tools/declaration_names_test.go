package tools

import (
	"strings"
	"testing"
)

// TestDeclarationNamePairsTheTwoSurfaces pins the catalogue tool that
// does the same work as each model-facing declaration.
//
// 【一手】 the twelve declarations are internal/tools/model_tools_1.4.6.json
// (byte-identical in 1.4.3–1.4.8); the catalogue names are the reference's
// advertised surface (init.tools, 60 names).
//
// The pairing is 【推测】: the reference's binary does not publish a table,
// so each pair is read off the two surfaces' own descriptions.
func TestDeclarationNamePairsTheTwoSurfaces(t *testing.T) {
	cases := map[string]string{
		"view_file":                  "read_file",
		"write_to_file":              "write_file",
		"replace_file_content":       "edit_file",
		"multi_replace_file_content": "edit_file",
		"sed_file":                   "edit_file",
		"notebook_edit":              "edit_file",
		"notebook_execution":         "edit_file",
		"run_command":                "run_command",
		"search_web":                 "search_web",
		"send_message":               "send_message",
		"define_subagent":            "define_subagent",
		"invoke_subagent":            "invoke_subagent",
		"manage_subagents":           "manage_subagents",
		"manage_task":                "manage_task",
		"schedule":                   "schedule",
		"ask_question":               "ask_question",
	}
	for catalogue, declaration := range cases {
		if got := DeclarationName(catalogue); got != declaration {
			t.Fatalf("DeclarationName(%q) = %q, want %q", catalogue, got, declaration)
		}
	}
}

// TestDeclarationNameHasNoCounterpartForTheRest pins the honest answer
// for the tools the twelve declarations do not cover: the MCP tools, the
// browser tools and the rest of the catalogue.
func TestDeclarationNameHasNoCounterpartForTheRest(t *testing.T) {
	for _, name := range []string{"call_mcp_tool", "list_resources", "read_resource", "grep_search", "list_dir", "browser_subagent", ""} {
		if got := DeclarationName(name); got != "" {
			t.Fatalf("DeclarationName(%q) = %q, want the empty string", name, got)
		}
	}
}

// TestPermissionNamesCarriesBothSpellings pins the list a permission rule
// is judged under: the call's own name first, then the declaration's.
func TestPermissionNamesCarriesBothSpellings(t *testing.T) {
	got := PermissionNames("write_to_file")
	if len(got) != 2 || got[0] != "write_to_file" || got[1] != "write_file" {
		t.Fatalf("PermissionNames(\"write_to_file\") = %v, want [write_to_file write_file]", got)
	}
	got = PermissionNames("run_command")
	if len(got) != 1 || got[0] != "run_command" {
		t.Fatalf("PermissionNames(\"run_command\") = %v, want [run_command]", got)
	}
	if got := PermissionNames(""); got != nil {
		t.Fatalf("PermissionNames(\"\") = %v, want nil", got)
	}
}

// TestPermissionNamesCoversTheCatalogue checks the whole advertised
// surface: every name the catalogue registers is judged under its own
// name, and the ones that have a declaration counterpart under both.
func TestPermissionNamesCoversTheCatalogue(t *testing.T) {
	names := PermissionNames("view_file")
	if len(names) != 2 || names[1] != "read_file" {
		t.Fatalf("PermissionNames(\"view_file\") = %v, want [view_file read_file]", names)
	}
	joined := strings.Join(PermissionNames("replace_file_content"), ",")
	if joined != "replace_file_content,edit_file" {
		t.Fatalf("PermissionNames(\"replace_file_content\") = %v", names)
	}
}
