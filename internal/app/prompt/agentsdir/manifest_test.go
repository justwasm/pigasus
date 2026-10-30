// agentsdir manifest tests — pin the file-load half of the parent-walk.
//
// The first test loads a single file and verifies the bytes round-trip;
// the second test exercises LoadAllManifests with two stacked directories
// each shipping a `skills.json`; the third test confirms the legacy flat
// `[…]` form parses the same as the nested `{agents: […]}` form.
package agentsdir_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/agentsdir"
)

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadManifest_RoundTrip(t *testing.T) {
	home := fixtureCwd(t, []bool{false, true, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})
	mustWrite(t, filepath.Join(home, "a", "agents", "skills.json"),
		`{"agents":[{"name":"reviewer"}]}`)

	got, ok, err := agentsdir.LoadManifest(filepath.Join(home, "a", "agents"), "skills.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatalf("load: want ok=true")
	}
	if got.Path != filepath.Join(home, "a", "agents", "skills.json") {
		t.Fatalf("Path: want %s, got %s", filepath.Join(home, "a", "agents", "skills.json"), got.Path)
	}
	if string(got.Data) != `{"agents":[{"name":"reviewer"}]}` {
		t.Fatalf("Data: got %s", got.Data)
	}
}

func TestLoadManifest_MissingIsNotError(t *testing.T) {
	home := fixtureCwd(t, []bool{false, true, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got, ok, err := agentsdir.LoadManifest(filepath.Join(home, "a", "agents"), "skills.json")
	if err != nil {
		t.Fatalf("load: want nil err for missing file, got %v", err)
	}
	if ok {
		t.Fatalf("load: want ok=false for missing file, got true")
	}
	if got.Path != "" {
		t.Fatalf("want empty Manifest, got %+v", got)
	}
}

func TestLoadAllManifests_StacksMultipleLevels(t *testing.T) {
	// HOME and HOME/a both have skills.json. The walker surfaces
	// HOME/a first (CWD-closest), then HOME — LoadAllManifests should
	// return both in CWD-closest order.
	cwd, home := fixture(t, []bool{true, true, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})
	mustWrite(t, filepath.Join(home, "agents", "skills.json"),
		`{"agents":[{"name":"from-home"}]}`)
	mustWrite(t, filepath.Join(home, "a", "agents", "skills.json"),
		`{"agents":[{"name":"from-project"}]}`)

	ms, errs := agentsdir.LoadAllManifests(cwd, home, "skills.json", agentsdir.DefaultDirNames)
	if len(errs) > 0 {
		t.Fatalf("loadAll: unexpected errors: %v", errs)
	}
	if len(ms) != 2 {
		t.Fatalf("loadAll: want 2 manifests, got %d", len(ms))
	}
	if ms[0].Dir != filepath.Join(home, "a", "agents") {
		t.Fatalf("ms[0].Dir: want CWD-closest, got %s", ms[0].Dir)
	}
	if !bytes.Contains(ms[0].Data, []byte("from-project")) {
		t.Fatalf("ms[0] did not carry project-local bytes: %s", ms[0].Data)
	}
	if ms[1].Dir != filepath.Join(home, "agents") {
		t.Fatalf("ms[1].Dir: want HOME, got %s", ms[1].Dir)
	}
	if !bytes.Contains(ms[1].Data, []byte("from-home")) {
		t.Fatalf("ms[1] did not carry HOME bytes: %s", ms[1].Data)
	}
}

func TestParseAgentsJSON_NestedAndFlatForms(t *testing.T) {
	nested := []byte(`{"agents":[{"name":"foo","path":"agents/foo.md"}]}`)
	flat := []byte(`[{"name":"foo","path":"agents/foo.md"}]`)

	got1, err := agentsdir.ParseAgentsJSON(nested)
	if err != nil {
		t.Fatalf("nested: %v", err)
	}
	if len(got1) != 1 || got1[0].Name != "foo" {
		t.Fatalf("nested: want [foo], got %+v", got1)
	}
	got2, err := agentsdir.ParseAgentsJSON(flat)
	if err != nil {
		t.Fatalf("flat: %v", err)
	}
	if len(got2) != 1 || got2[0].Name != "foo" {
		t.Fatalf("flat: want [foo], got %+v", got2)
	}
	// Empty input is rejected — the caller decides what to do with it.
	if _, err := agentsdir.ParseAgentsJSON(nil); err == nil {
		t.Fatalf("empty input: want error, got nil")
	}
}

func TestMergeManifestBytes_Concatenates(t *testing.T) {
	a := agentsdir.Manifest{Name: "a", Data: []byte("hello")}
	b := agentsdir.Manifest{Name: "b", Data: []byte("world")}
	got := agentsdir.MergeManifestBytes([]agentsdir.Manifest{a, b})
	want := "hello\nworld"
	if string(got) != want {
		t.Fatalf("merge: want %q, got %q", want, got)
	}
	if agentsdir.MergeManifestBytes(nil) != nil {
		t.Fatalf("merge(nil): want nil")
	}
	if string(agentsdir.MergeManifestBytes([]agentsdir.Manifest{a})) != "hello" {
		t.Fatalf("merge(single): want verbatim bytes")
	}
}

// TestLoadManifest_BasenameValidation pins that names with path
// separators are rejected at the boundary rather than reaching
// filepath.Join.
func TestLoadManifest_BasenameValidation(t *testing.T) {
	if _, _, err := agentsdir.LoadManifest("/tmp", ""); err == nil {
		t.Fatalf("want error for empty name")
	}
	if _, _, err := agentsdir.LoadManifest("/tmp", "sub/file"); err == nil {
		t.Fatalf("want error for path-separator name")
	}
}

// TestParseAgentsJSON_EmptyObjectNoAgents pins the corner case where
// the file parsed as `{}` (no `agents` key, no top-level array) — the
// parser yields (nil, nil) so the caller can treat it as "no entries".
func TestParseAgentsJSON_EmptyObjectNoAgents(t *testing.T) {
	got, err := agentsdir.ParseAgentsJSON([]byte(`{}`))
	if err != nil {
		t.Fatalf("empty object: %v", err)
	}
	if got != nil {
		t.Fatalf("empty object: want nil entries, got %+v", got)
	}
	// Spot-check that the parser still handles a json.Number-y edge case
	// via a quick marshal/unmarshal round-trip.
	type sanity struct {
		Agents []agentsdir.AgentsJSONEntry `json:"agents"`
	}
	in := sanity{Agents: []agentsdir.AgentsJSONEntry{{Name: "x", Path: "y"}}}
	b, _ := json.Marshal(in)
	got2, err := agentsdir.ParseAgentsJSON(b)
	if err != nil {
		t.Fatalf("marshal-roundtrip: %v", err)
	}
	if len(got2) != 1 || got2[0].Name != "x" {
		t.Fatalf("marshal-roundtrip: want [x], got %+v", got2)
	}
}
