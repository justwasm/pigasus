package agenttools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools/surface"
)

func TestDeclarations_MatchCapturedOrder(t *testing.T) {
	got := Names()
	want := []string{"send_message", "view_file", "read_url_content", "search_web", "schedule", "generate_image", "manage_task"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", got, want)
	}
	decls, err := Declarations()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range decls {
		if d.Description == "" || d.Parameters == nil {
			t.Errorf("%s: empty declaration", d.Name)
		}
	}
}

func TestTools_RunsRegistryExecutor(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := Tools(surface.Catalog(dir))
	if len(ts) != 7 {
		t.Fatalf("got %d tools", len(ts))
	}
	target := ts[1] // view_file
	if target.Name() != "view_file" {
		t.Fatalf("tools[1] = %s", target.Name())
	}
	params, _ := json.Marshal(map[string]any{"AbsolutePath": file})
	res, err := target.Execute(context.Background(), string(params))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Fatalf("result = %#v", res.Output)
	}
}

// TestDeclarationsMatch147Capture re-evidences the seven custom-agent
// declarations against agy 1.4.7.
func TestDeclarationsMatch147Capture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "agent_decls_1.4.7.json"))
	if err != nil {
		t.Fatal(err)
	}
	var captured []Declaration
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatal(err)
	}
	got, err := Declarations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(captured) {
		t.Fatalf("Declarations() has %d entries, capture has %d", len(got), len(captured))
	}
	for i := range captured {
		if got[i].Name != captured[i].Name {
			t.Fatalf("declaration %d name = %q, capture = %q", i, got[i].Name, captured[i].Name)
		}
		if got[i].Description != captured[i].Description {
			t.Errorf("%s: description drifted from the 1.4.7 capture", got[i].Name)
		}
		a, _ := json.Marshal(got[i].Parameters)
		b, _ := json.Marshal(captured[i].Parameters)
		if string(a) != string(b) {
			t.Errorf("%s: parameters drifted from the 1.4.7 capture:\n got %s\nwant %s", got[i].Name, a, b)
		}
	}
}

// TestDeclarationsCarryNoToolSummaryOrAction pins the 1.4.7 shape of the
// custom-agent surface.
func TestDeclarationsCarryNoToolSummaryOrAction(t *testing.T) {
	decls, err := Declarations()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range decls {
		props, _ := d.Parameters["properties"].(map[string]any)
		for _, banned := range []string{"toolSummary", "toolAction"} {
			if _, ok := props[banned]; ok {
				t.Errorf("%s: still advertises %q (the 1.4.7 capture has no such parameter)", d.Name, banned)
			}
		}
	}
}

// TestMainAgentToolsIncludesRunCommand pins the full default-agent surface
// used by the remote-agent bridge. The restricted custom-agent surface has
// seven tools and no run_command, which made the model answer that command
// execution was unavailable.
func TestMainAgentToolsIncludesRunCommand(t *testing.T) {
	tools := MainAgentTools(nil)
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if !names["run_command"] {
		t.Errorf("MainAgentTools missing run_command: %v", names)
	}
	if !names["write_file"] || !names["edit_file"] {
		t.Errorf("MainAgentTools missing file tools: %v", names)
	}
	if len(tools) != 12 {
		t.Errorf("MainAgentTools count = %d, want 12", len(tools))
	}
}
