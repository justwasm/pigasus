package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The twelve declarations are 【一手】: the exact functionDeclarations array
// from a 1.4.4 streamGenerateContent body, identical to the 1.4.3 capture.
// Order is part of the fixture — the reference does not sort this list.
func TestModelDeclarationsMatchCapture(t *testing.T) {
	want := []string{
		"read_file", "run_command", "manage_task", "send_message", "schedule",
		"invoke_subagent", "define_subagent", "manage_subagents",
		"write_file", "edit_file", "search_web", "ask_question",
	}
	decls := ModelDeclarations()
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
		}
		if err := json.Unmarshal(decls[i].Parameters, &params); err != nil {
			t.Errorf("%s: parameters: %v", name, err)
		}
		if len(params.Required) == 0 {
			t.Errorf("%s: no required properties", name)
		}
	}
}

// The three reference-shaped calls that used to fail: read_file, write_file
// and edit_file. Their argument names are CamelCase and edit_file's halves are
// Target / Replacement, not the clone's oldString / newString.
func TestRuntimeCatalogExecutesReferenceFileTools(t *testing.T) {
	ws := t.TempDir()
	reg := RuntimeCatalog(ws)
	ctx := context.Background()

	if _, err := reg.Dispatch(ctx, "write_file", json.RawMessage(
		`{"Path":"note.txt","Content":"alpha\nbeta\n"}`)); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	got, err := reg.Dispatch(ctx, "read_file", json.RawMessage(`{"Path":"note.txt"}`))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(got, "Showing lines 1-2 of 2\n1: alpha\n2: beta") {
		t.Fatalf("read_file = %q", got)
	}
	if _, err := reg.Dispatch(ctx, "edit_file", json.RawMessage(
		`{"Path":"note.txt","Target":"beta","Replacement":"gamma"}`)); err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\ngamma\n" {
		t.Fatalf("edited content = %q", data)
	}

	// write_file's Append flag (1.4.4 declaration) must not truncate.
	if _, err := reg.Dispatch(ctx, "write_file", json.RawMessage(
		`{"Path":"note.txt","Content":"delta\n","Append":true}`)); err != nil {
		t.Fatalf("write_file append: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(ws, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\ngamma\ndelta\n" {
		t.Fatalf("appended content = %q", data)
	}
}

func TestRuntimeCatalogRunsReferenceCommand(t *testing.T) {
	reg := RuntimeCatalog(t.TempDir())
	out, err := reg.Dispatch(context.Background(), "run_command",
		json.RawMessage(`{"CommandLine":"printf hi","toolSummary":"print hi","WaitMsBeforeAsync":0}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("run_command output = %q", out)
	}
}

// RuntimeCatalog is a superset of Catalog: adding the 1.4.4 executors must not
// drop any of the 60 names print mode reports in init.tools.
func TestRuntimeCatalogKeepsCatalogue(t *testing.T) {
	reg := RuntimeCatalog(t.TempDir())
	for _, name := range []string{"view_file", "write_to_file", "replace_file_content", "read_file", "write_file", "edit_file"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("missing %q", name)
		}
	}
	if got, want := len(reg.List()), 63; got != want {
		t.Fatalf("registry size = %d, want %d", got, want)
	}
}

// TestSchemaParamsCoercesProtoInts pins the PiG-side half of the protojson
// round trip. The captured declarations spell minItems/minLength as strings
// ("minItems":"2"); draft 2020-12 requires integers, and PiG's metaschema
// check rejects the whole tool otherwise ("got string, want integer"), which
// made send_message and define_subagent unusable.
func TestSchemaParamsCoercesProtoInts(t *testing.T) {
	schema := SchemaParams(json.RawMessage(`{"type":"OBJECT","minItems":"2","properties":{"Name":{"type":"STRING","minLength":"1"}}}`))
	if schema["type"] != "object" {
		t.Errorf("type = %v, want object", schema["type"])
	}
	if schema["minItems"] != 2 {
		t.Errorf("minItems = %#v, want the integer 2", schema["minItems"])
	}
	props, _ := schema["properties"].(map[string]any)
	name, _ := props["Name"].(map[string]any)
	if name["minLength"] != 1 {
		t.Errorf("minLength = %#v, want the integer 1", name["minLength"])
	}
	if name["type"] != "string" {
		t.Errorf("nested type = %v, want string", name["type"])
	}
}
