package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The eighteen harness declarations are 【一手】: the exact
// functionDeclarations array from a 1.4.7 Gemini request body the
// reference `localharness` binary sent. The order is part of the fixture —
// the reference does not sort this list.
func TestHarnessDeclarationsMatchCapture(t *testing.T) {
	want := []string{
		"ask_question", "define_subagent", "find_by_name", "generate_image",
		"grep_search", "invoke_subagent", "list_dir", "manage_subagents",
		"manage_task", "read_url_content", "replace_file_content",
		"run_command", "schedule", "search_web", "send_message", "view_file",
		"write_to_file", "run_workflow",
	}
	decls := HarnessDeclarations()
	if len(decls) != len(want) {
		t.Fatalf("declaration count = %d, want %d", len(decls), len(want))
	}
	for i, name := range want {
		if decls[i].Name != name {
			t.Errorf("declaration %d = %q, want %q", i, decls[i].Name, name)
		}
		if strings.TrimSpace(decls[i].Description) == "" {
			t.Errorf("%s: empty description", name)
		}
		var params struct {
			Required []string `json:"required"`
			Schema   string   `json:"$schema"`
			Type     string   `json:"type"`
		}
		if err := json.Unmarshal(decls[i].Parameters, &params); err != nil {
			t.Errorf("%s: parameters: %v", name, err)
			continue
		}
		if len(params.Required) == 0 {
			t.Errorf("%s: no required properties", name)
		}
		// The harness's schemas are plain JSON Schema (draft 2020-12),
		// not the OpenAI subset the CLI sends.
		if params.Schema != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("%s: $schema = %q, want the 2020-12 dialect", name, params.Schema)
		}
		if params.Type != "object" {
			t.Errorf("%s: type = %q, want object", name, params.Type)
		}
	}
}

// The embedded fixture must be the file on disk, byte for byte: the wire
// bytes come from the fixture, not from a reconstruction.
func TestHarnessToolsFixtureMatchesDisk(t *testing.T) {
	onDisk, err := os.ReadFile("harness_tools_1.4.7.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(onDisk, HarnessDeclarationsJSON()) {
		t.Errorf("embedded fixture differs from harness_tools_1.4.7.json (%d vs %d bytes)",
			len(HarnessDeclarationsJSON()), len(onDisk))
	}
}

// The harness names its tools differently from the CLI: it reads a file
// with `view_file`, writes one with `write_to_file` and edits one with
// `replace_file_content`.
func TestHarnessNamesAreNotTheCLINames(t *testing.T) {
	names := map[string]bool{}
	for _, decl := range HarnessDeclarations() {
		names[decl.Name] = true
	}
	for _, unwanted := range []string{"read_file", "write_file", "edit_file"} {
		if names[unwanted] {
			t.Errorf("%s is declared, want the harness's own name", unwanted)
		}
	}
	for _, want := range []string{"view_file", "write_to_file", "replace_file_content"} {
		if !names[want] {
			t.Errorf("%s is not declared", want)
		}
	}
}

// RelaxedSchema drops the `required` list so the runtime accepts a call
// whose arguments miss a field the schema requires.
//
// The reference's harness runs such a call: the captured `view_file` call
// carries only `AbsolutePath`, and the reference ran the tool. The
// advertised schema keeps the requirement — the model is still told which
// fields the tool wants.
func TestRelaxedSchemaDropsTheRequiredList(t *testing.T) {
	var decl ModelToolDecl
	for _, d := range HarnessDeclarations() {
		if d.Name == "view_file" {
			decl = d
		}
	}
	if decl.Name == "" {
		t.Fatal("no view_file declaration")
	}
	relaxed := RelaxedSchema(decl.Parameters)
	var got struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(relaxed, &got); err != nil {
		t.Fatalf("decode the relaxed schema: %v", err)
	}
	if len(got.Required) != 0 {
		t.Errorf("the relaxed schema still requires %v", got.Required)
	}
	// The rest of the schema survives.
	var params struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(relaxed, &params); err != nil {
		t.Fatalf("decode the relaxed properties: %v", err)
	}
	if _, ok := params.Properties["AbsolutePath"]; !ok {
		t.Error("the relaxed schema lost the AbsolutePath property")
	}
}

// A schema with no `required` list is returned unchanged.
func TestRelaxedSchemaLeavesASchemaWithoutRequirementsAlone(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)
	got := RelaxedSchema(raw)
	if string(got) != string(raw) {
		t.Errorf("the schema = %s, want it unchanged", got)
	}
}

// A schema that cannot be decoded is returned unchanged: the caller then
// validates exactly as before.
func TestRelaxedSchemaLeavesBrokenSchemasAlone(t *testing.T) {
	raw := json.RawMessage(`not json`)
	if got := RelaxedSchema(raw); string(got) != string(raw) {
		t.Errorf("the schema = %s, want it unchanged", got)
	}
	if got := RelaxedSchema(nil); got != nil {
		t.Errorf("the schema = %s, want nil", got)
	}
}
