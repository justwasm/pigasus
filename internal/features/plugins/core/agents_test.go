// agents_test.go — the plugin agents loader.
//
// The loader is the plugin package's own (internal/features/plugins/core):
// it reads a plugin's agents/*.md sidecar and its agents.json manifest and
// merges the two. Cycle 381 moved these tests out of internal/cliapp,
// where they sat next to the /plugin panel's own tests.

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentsRoot builds <tmp>/.gemini/{antigravity-cli,config} and points
// ANTIGRAVITY_HOME at the antigravity-cli one. Returns the .gemini root so
// callers can drop fixtures into the config tree.
func agentsRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// agentsWriteJSONFile writes v as the JSON document at path.
func agentsWriteJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCycle234_MarkdownOnlyIsUnchanged verifies the merge is a true union:
// a plugin that ships only the legacy <plugin>/agents/*.md sidecar keeps
// every entry (no shadowing, no filtering).
func TestCycle234_MarkdownOnlyIsUnchanged(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "md-only")
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agents", name+".md"),
			[]byte("# "+name+"\n"), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got := Agents("md-only")
	want := []string{"alpha", "beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Agents(md-only) = %v, want %v", got, want)
	}
}

// TestCycle234_JsonOnlyIsLoaded mirrors the changelog line 13 fix: a plugin
// with *only* an agents.json at the root still gets every declared subagent
// surfaced. Pre-cycle-234 the discover / install / validate paths read
// nothing from agents.json.
func TestCycle234_JsonOnlyIsLoaded(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "json-only")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	doc := map[string]any{
		"agents": []map[string]any{
			{"name": "delta", "path": "agents/delta.md"},
			{"name": "epsilon", "path": "agents/epsilon.md"},
		},
	}
	agentsWriteJSONFile(t, filepath.Join(dir, "agents.json"), doc)
	got := Agents("json-only")
	want := []string{"delta", "epsilon"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Agents(json-only) = %v, want %v", got, want)
	}
}

// TestCycle234_UnionOfBothSources: the merge of agents/*.md and agents.json
// returns the union, sorted, with neither source shadowing.
func TestCycle234_UnionOfBothSources(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "both")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	// Three markdown entries.
	for _, name := range []string{"md-a", "md-b", "shared"} {
		if err := os.WriteFile(filepath.Join(dir, "agents", name+".md"),
			[]byte("# "+name), 0600); err != nil {
			t.Fatalf("write %s.md: %v", name, err)
		}
	}
	// Three JSON entries — one shared with the markdown sidecar,
	// two JSON-only.
	doc := map[string]any{
		"agents": []map[string]any{
			{"name": "shared", "path": "agents/shared.md"},
			{"name": "json-x", "path": "agents/json-x.md"},
			{"name": "json-y", "path": "agents/json-y.md"},
		},
	}
	agentsWriteJSONFile(t, filepath.Join(dir, "agents.json"), doc)
	got := Agents("both")
	want := []string{"json-x", "json-y", "md-a", "md-b", "shared"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Agents(both) = %v, want %v (shared dedupes)", got, want)
	}
}

// TestCycle234_NeitherPresentUnaffected: a plugin with no agents source
// at all returns nil — the empty-state row the Installed detail block
// turns into "-".
func TestCycle234_NeitherPresentUnaffected(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "empty")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"empty"}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got := Agents("empty")
	if got != nil {
		t.Errorf("Agents(empty) = %v, want nil", got)
	}
}

// TestCycle234_MalformedAgentsJsonFallsBack: a malformed agents.json does
// NOT crash the loader. pluginAgents is the TUI-side entry point and is
// expected to silently treat the JSON as missing; pluginAgentsAtPath (used
// by the CLI handlers) does surface the parse error and the listings are
// empty — both behaviours are deliberately different.
func TestCycle234_MalformedAgentsJsonFallsBack(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "broken-json")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"broken-json"}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents.json"),
		[]byte(`{"agents": [{"name": "x"`), 0600); err != nil { // missing closer
		t.Fatalf("write broken: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "alpha.md"),
		[]byte("# alpha"), 0600); err != nil {
		t.Fatalf("write md: %v", err)
	}
	// TUI: the markdown sidecar is the source of truth when the JSON
	// is broken — the panel still has something to show.
	got := Agents("broken-json")
	if strings.Join(got, ",") != "alpha" {
		t.Errorf("Agents(broken-json) = %v, want [alpha]", got)
	}

	// CLI side: the parse error must surface so `agy plugin validate`
	// can refuse the install.
	_, jsonErr, _ := AgentsAtPath(dir)
	if jsonErr == nil {
		t.Errorf("pluginAgentsAtPath did not surface JSON parse error")
	}
}

// TestCycle234_AgentsJsonEntryMissingNameSkipped: a parseable JSON file
// whose entries lack `name` (the only required field) drops the
// nameless entries silently. The on-disk filename is the fallback name in
// the markdown sidecar; the JSON manifest has no equivalent.
func TestCycle234_AgentsJsonEntryMissingNameSkipped(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "nameless")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	agentsWriteJSONFile(t, filepath.Join(dir, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "ok", "path": "x.md"},
			{"path": "no-name.md"}, // missing name
			{"name": "", "path": "empty.md"},
		},
	})
	got := Agents("nameless")
	if strings.Join(got, ",") != "ok" {
		t.Errorf("Agents(nameless) = %v, want [ok]", got)
	}
}

// TestCycle234_JsonFlatFormAccepted: an accidentally-flat JSON array (a
// common pre-1.3.3 typo that AGY keeps tolerating) parses the same way.
func TestCycle234_JsonFlatFormAccepted(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "flat")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []map[string]any{
		{"name": "f1", "path": "a.md"},
		{"name": "f2", "path": "b.md"},
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), raw, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := Agents("flat")
	want := []string{"f1", "f2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Agents(flat) = %v, want %v", got, want)
	}
}

// TestCycle234_JsonExcludesNonMdFiles: an agents/ directory with
// non-markdown sidecars (README.md's only the suffix `.md`, but
// `agents.txt` has no suffix) doesn't contribute to the listing. Same
// filter as the cycle-198 skills/ reader.
func TestCycle234_JsonExcludesNonMdFiles(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "no-md")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "real.md"),
		[]byte("# real"), 0600); err != nil {
		t.Fatalf("write real: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "notes.txt"),
		[]byte("not a subagent"), 0600); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "agents", "subdir"), 0700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	got := Agents("no-md")
	if strings.Join(got, ",") != "real" {
		t.Errorf("Agents(no-md) = %v, want [real]", got)
	}
}

// TestCycle234_JsonSortIsStable: the order is alphabetical regardless of
// the on-disk file order or the JSON entry order. Pins the `sort.Strings`
// call in pluginAgents.
func TestCycle234_JsonSortIsStable(t *testing.T) {
	root := agentsRoot(t)
	dir := filepath.Join(root, "config", "plugins", "sort")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	agentsWriteJSONFile(t, filepath.Join(dir, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "zulu"},
			{"name": "alpha"},
			{"name": "mike"},
		},
	})
	if err := os.WriteFile(filepath.Join(dir, "agents", "yankee.md"),
		[]byte("# yankee"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := Agents("sort")
	want := []string{"alpha", "mike", "yankee", "zulu"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Agents(sort) = %v, want %v", got, want)
	}
}
