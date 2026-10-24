package agenttools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/tools"
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
	ts := Tools(tools.Catalog(dir))
	if len(ts) != 7 {
		t.Fatalf("got %d tools", len(ts))
	}
	target := ts[1] // view_file
	if target.Name() != "view_file" {
		t.Fatalf("tools[1] = %s", target.Name())
	}
	params, _ := json.Marshal(map[string]any{"AbsolutePath": file})
	res, err := target.Execute(context.Background(), "call", params, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content = %d", len(res.Content))
	}
	text, ok := res.Content[0].(pi.TextContent)
	if !ok || !strings.Contains(text.Text, "hello") {
		t.Fatalf("result = %#v", res.Content[0])
	}
}

// TestDeclarationsMatch147Capture re-evidences the seven custom-agent
// declarations against agy 1.4.7. The 1.4.3/1.4.4 capture was taken through
// the cycle-274 wrapper; this test pins a fresh 1.4.7 capture (a real
// `--agent test` turn, recorded through the clone's own proxy) and asserts
// the two are identical, which is why tools_1.4.7.json is authoritative.
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
// custom-agent surface: the reference dropped the `toolSummary` and
// `toolAction` parameters (they belong to its subagent surface) and the
// clone's wire must not offer them either — a model that still sent them
// would be asking for a parameter the reference does not accept.
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
