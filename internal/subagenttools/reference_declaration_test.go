// reference_declaration_test.go — the contract between the clone's parser
// and the reference's own captured declaration.
//
// 【一手】 internal/tools/model_tools_1.4.6.json is the reference's captured
// model-facing surface: twelve declarations, captured from a live request
// body. The three switches the clone reads are that file's *own* property
// names, and the file did not move at agy 1.4.8 (the twelve declarations are
// byte-for-byte identical — cycle-350).
//
// So this test drives the clone's parser with the capture's spelling rather
// than with a copy of it: a property the capture does not carry, or one it
// carries that the clone does not know, fails the test loudly — instead of
// the clone silently reading nothing and running a subagent with no grant.
package subagenttools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// capturedDecl is one declaration out of the reference's captured surface.
type capturedDecl struct {
	Name       string `json:"name"`
	Parameters struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	} `json:"parameters"`
}

// referenceDeclarations reads the reference's captured model-facing surface.
func referenceDeclarations(t *testing.T) map[string]capturedDecl {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "tools", "model_tools_1.4.6.json"))
	if err != nil {
		t.Fatalf("read the reference's captured declarations: %v", err)
	}
	var decls []capturedDecl
	if err := json.Unmarshal(raw, &decls); err != nil {
		t.Fatalf("parse the reference's captured declarations: %v", err)
	}
	out := make(map[string]capturedDecl, len(decls))
	for _, d := range decls {
		out[d.Name] = d
	}
	return out
}

// sortedNames lists a set of names, in order.
func sortedNames(set map[string]struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestCapturedDefineSubagentCarriesTheThreeSwitches pins the capture's own
// shape: the six properties, the three switches among them (all BOOLEAN),
// and the required list.
//
// This is the evidence the whole authorization model rests on — if the
// reference's declaration ever loses a switch, the model can no longer ask
// for it and the clone's parser would be reading a name that no longer
// exists.
func TestCapturedDefineSubagentCarriesTheThreeSwitches(t *testing.T) {
	decl, ok := referenceDeclarations(t)["define_subagent"]
	if !ok {
		t.Fatal("the capture has no define_subagent declaration")
	}

	want := []string{
		"Description", "EnableMCPTools", "EnableSubagentTools",
		"EnableWriteTools", "Name", "SystemPrompt",
	}
	got := sortedNames(decl.Parameters.Properties)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("define_subagent's properties = %v, want %v", got, want)
	}

	for _, name := range []string{"EnableMCPTools", "EnableSubagentTools", "EnableWriteTools"} {
		prop, ok := decl.Parameters.Properties[name]
		if !ok {
			t.Errorf("the capture's define_subagent has no %q property", name)
			continue
		}
		if prop.Type != "BOOLEAN" {
			t.Errorf("%s is %q, want BOOLEAN", name, prop.Type)
		}
		if prop.Description == "" {
			t.Errorf("%s carries no description", name)
		}
	}

	wantRequired := []string{"Description", "Name", "SystemPrompt"}
	gotRequired := append([]string(nil), decl.Parameters.Required...)
	sort.Strings(gotRequired)
	if strings.Join(gotRequired, ",") != strings.Join(wantRequired, ",") {
		t.Errorf("define_subagent's required = %v, want %v", decl.Parameters.Required, wantRequired)
	}
}

// TestCloneReadsTheCapturedDefineSpelling drives the clone's parser with the
// capture's own property names, in both directions:
//
//   - every property the capture carries, the clone reads;
//   - every property the clone knows, the capture carries.
//
// The body is built from the capture, so a spelling that moves breaks the
// test rather than the grant.
func TestCloneReadsTheCapturedDefineSpelling(t *testing.T) {
	decl, ok := referenceDeclarations(t)["define_subagent"]
	if !ok {
		t.Fatal("the capture has no define_subagent declaration")
	}

	// The clone's spelling of the capture's properties.
	values := map[string]any{
		"Name":                "viewer",
		"Description":         "Reads the tree.",
		"SystemPrompt":        "You are a reader.",
		"EnableWriteTools":    true,
		"EnableMCPTools":      true,
		"EnableSubagentTools": true,
	}
	for name := range decl.Parameters.Properties {
		if _, known := values[name]; !known {
			t.Errorf("the capture's define_subagent carries %q, which the clone does not read", name)
		}
	}
	for name := range values {
		if _, captured := decl.Parameters.Properties[name]; !captured {
			t.Errorf("the clone reads define_subagent's %q, which the capture does not carry", name)
		}
	}

	body, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal the define body: %v", err)
	}

	defs := NewStore()
	tool := &defineTool{defs: defs}
	if _, err := tool.Execute(context.Background(), "call-cap", body, nil); err != nil {
		t.Fatalf("define_subagent (the capture's spelling): %v", err)
	}
	def, ok := defs.Get("viewer")
	if !ok {
		t.Fatal("the definition was not stored")
	}
	if def.Grant != (Grant{Write: true, MCP: true, Subagent: true}) {
		t.Errorf("the stored grant = %+v, want every switch on", def.Grant)
	}
	if def.SystemPrompt != "You are a reader." {
		t.Errorf("the stored prompt = %q, want the capture's SystemPrompt", def.SystemPrompt)
	}

	// The same body with the switches off: the grant is empty, and the
	// definition is still stored — a switch off is not a refusal.
	for _, name := range []string{"EnableWriteTools", "EnableMCPTools", "EnableSubagentTools"} {
		values[name] = false
	}
	body, err = json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal the define body: %v", err)
	}
	if _, err := tool.Execute(context.Background(), "call-cap-off", body, nil); err != nil {
		t.Fatalf("define_subagent (the switches off): %v", err)
	}
	def, ok = defs.Get("viewer")
	if !ok {
		t.Fatal("the definition was not stored")
	}
	if def.Grant != (Grant{}) {
		t.Errorf("the stored grant = %+v, want every switch off", def.Grant)
	}
}

// TestCloneReadsTheCapturedInvokeSpelling is the same contract for
// invoke_subagent: every property the capture carries, the clone reads.
func TestCloneReadsTheCapturedInvokeSpelling(t *testing.T) {
	decl, ok := referenceDeclarations(t)["invoke_subagent"]
	if !ok {
		t.Fatal("the capture has no invoke_subagent declaration")
	}

	values := map[string]any{
		"TypeName":  "viewer",
		"Role":      "Codebase Researcher",
		"Prompt":    "Read the tree.",
		"Model":     "flash",
		"Workspace": "branch",
	}
	for name := range decl.Parameters.Properties {
		if _, known := values[name]; !known {
			t.Errorf("the capture's invoke_subagent carries %q, which the clone does not read", name)
		}
	}
	for name := range values {
		if _, captured := decl.Parameters.Properties[name]; !captured {
			t.Errorf("the clone reads invoke_subagent's %q, which the capture does not carry", name)
		}
	}

	body, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal the invoke body: %v", err)
	}
	args, err := parseInvokeArgs(body, Deps{})
	if err != nil {
		t.Fatalf("invoke_subagent (the capture's spelling): %v", err)
	}
	if args.TypeName != "viewer" {
		t.Errorf("TypeName = %q, want %q", args.TypeName, "viewer")
	}
	if args.Role != "Codebase Researcher" {
		t.Errorf("Role = %q, want %q", args.Role, "Codebase Researcher")
	}
	if args.Prompt != "Read the tree." {
		t.Errorf("Prompt = %q, want %q", args.Prompt, "Read the tree.")
	}
	if args.Model != "flash" {
		t.Errorf("Model = %q, want %q", args.Model, "flash")
	}
	if args.Workspace != "branch" {
		t.Errorf("Workspace = %q, want %q", args.Workspace, "branch")
	}
}
