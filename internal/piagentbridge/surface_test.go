// surface_test.go — the model-facing tool surface.
//
// The harness advertises the reference's own declarations and runs the
// calls the model makes against them. Two rules matter:
//
//   - the advertised schema is the captured one, byte for byte, and
//   - the *runtime* check is relaxed: the reference's harness runs a call
//     whose arguments miss a field the schema requires.
package piagentbridge

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/tools"
)

// harnessDeclaration is one declaration of the reference's harness
// surface.
func harnessDeclaration(name string) tools.ModelToolDecl {
	for _, decl := range tools.HarnessDeclarations() {
		if decl.Name == name {
			return decl
		}
	}
	return tools.ModelToolDecl{}
}

// The strict surface validates against the schema it advertises.
func TestSurfaceToolsForKeepsTheStrictSchema(t *testing.T) {
	decl := harnessDeclaration("view_file")
	if decl.Name == "" {
		t.Fatal("no view_file declaration")
	}
	surface := SurfaceToolsFor(nil, nil, []tools.ModelToolDecl{decl})
	if len(surface) != 1 {
		t.Fatalf("surface = %d tools, want 1", len(surface))
	}
	schema, ok := surface[0].(interface {
		ArgumentSchema() json.RawMessage
	})
	if !ok {
		t.Fatal("the tool does not carry an argument schema")
	}
	raw := schema.ArgumentSchema()
	var got struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode the schema: %v", err)
	}
	if len(got.Required) == 0 {
		t.Error("the strict schema lost its required list")
	}
}

// The lenient surface drops the requirement: the reference's harness runs
// a call that misses a required field.
func TestLenientSurfaceToolsForDropsTheRequiredList(t *testing.T) {
	decl := harnessDeclaration("view_file")
	if decl.Name == "" {
		t.Fatal("no view_file declaration")
	}
	surface := LenientSurfaceToolsFor(nil, nil, []tools.ModelToolDecl{decl})
	if len(surface) != 1 {
		t.Fatalf("surface = %d tools, want 1", len(surface))
	}
	schema, ok := surface[0].(interface {
		ArgumentSchema() json.RawMessage
	})
	if !ok {
		t.Fatal("the tool does not carry an argument schema")
	}
	raw := schema.ArgumentSchema()
	var got struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode the schema: %v", err)
	}
	if len(got.Required) != 0 {
		t.Errorf("the lenient schema still requires %v", got.Required)
	}
	if _, ok := got.Properties["AbsolutePath"]; !ok {
		t.Error("the lenient schema lost the AbsolutePath property")
	}
}

// The advertised schema is the captured one in both surfaces: the model
// sees the same declaration the reference sent.
func TestBothSurfacesAdvertiseTheCapturedDeclaration(t *testing.T) {
	decl := harnessDeclaration("view_file")
	strict := SurfaceToolsFor(nil, nil, []tools.ModelToolDecl{decl})
	lenient := LenientSurfaceToolsFor(nil, nil, []tools.ModelToolDecl{decl})
	if len(strict) != 1 || len(lenient) != 1 {
		t.Fatalf("the surfaces are %d and %d tools, want 1 each", len(strict), len(lenient))
	}
	strictSchema := strict[0].Schema()
	lenientSchema := lenient[0].Schema()
	if strictSchema.Name != decl.Name || lenientSchema.Name != decl.Name {
		t.Errorf("the names are %q and %q, want %q", strictSchema.Name, lenientSchema.Name, decl.Name)
	}
	if strictSchema.Description != decl.Description || lenientSchema.Description != decl.Description {
		t.Error("the descriptions differ from the capture")
	}
	if len(strictSchema.Parameters) == 0 || len(lenientSchema.Parameters) == 0 {
		t.Fatal("a surface advertises no parameters")
	}
}
