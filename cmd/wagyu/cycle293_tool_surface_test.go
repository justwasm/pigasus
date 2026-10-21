package main

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/agenttools"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// Cycle 293: the default agent advertises the reference's twelve captured
// declarations, not the 60-name catalogue print mode reports in init.tools.
func TestAdvertisedToolDeclsDefaultAgent(t *testing.T) {
	reg := tools.RuntimeCatalog(t.TempDir())
	decls := advertisedToolDecls("", reg)
	want := []string{
		"read_file", "run_command", "manage_task", "send_message", "schedule",
		"invoke_subagent", "define_subagent", "manage_subagents",
		"write_file", "edit_file", "search_web", "ask_question",
	}
	if len(decls) != len(want) {
		t.Fatalf("declarations = %d, want %d", len(decls), len(want))
	}
	for i, name := range want {
		if decls[i].Name != name {
			t.Errorf("declaration[%d] = %q, want %q", i, decls[i].Name, name)
		}
		if decls[i].Description == "" {
			t.Errorf("%s: empty description", name)
		}
	}

	// The wire envelope is the fixture's declarations, compacted by
	// buildToolDecls and wrapped in the CloudCode `tools` object. 13,152 B is
	// the reference's 13,449 minus the protojson ", " separators this clone's
	// encoding/json does not emit.
	if got, want := len(backend.WireToolsJSON(decls)), 13152; got != want {
		t.Errorf("tools wire bytes = %d, want %d", got, want)
	}

	required := func(name string) []string {
		t.Helper()
		for _, d := range decls {
			if d.Name != name {
				continue
			}
			var p struct {
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(d.Parameters, &p); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return p.Required
		}
		t.Fatalf("%s: not advertised", name)
		return nil
	}
	if got := required("read_file"); len(got) != 1 || got[0] != "Path" {
		t.Errorf("read_file required = %v", got)
	}
	if got := required("edit_file"); len(got) != 3 || got[0] != "Path" || got[1] != "Target" || got[2] != "Replacement" {
		t.Errorf("edit_file required = %v", got)
	}
	if got := required("run_command"); len(got) != 3 || got[0] != "CommandLine" {
		t.Errorf("run_command required = %v", got)
	}
}

// A --agent custom agent keeps its captured seven-tool surface.
func TestAdvertisedToolDeclsCustomAgent(t *testing.T) {
	reg := agenttools.Registry(tools.Catalog(t.TempDir()))
	decls := advertisedToolDecls("custom agent body", reg)
	if len(decls) != 7 {
		t.Fatalf("custom agent declarations = %d, want 7", len(decls))
	}
}
