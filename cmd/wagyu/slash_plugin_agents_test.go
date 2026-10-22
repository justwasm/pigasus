package main

// slash_cycle234_test.go — cycle 234: agents.json in plugin roots.
//
// AGY 1.3.6 changelog line 13 closed three surfaces that were reading
// only the <plugin>/agents/*.md sidecar:
//
//   - /plugin Discover tab previews
//   - agy plugin install summary
//   - agy plugin validate output
//
// Every test below pins the on-disk merge of agents.json + agents/*.md
// and surfaces that merge into the TUI panel detail block plus the CLI
// subcommand handlers. The tests deliberately don't drive the rendered
// cells (the TUI panel layer is byte-pinned elsewhere — cycles 198, 204,
// 219, 220 own that); they own the new merge logic in plugin_agents.go
// and the subcommand_parity.go handlers.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCycle234_MarkdownOnlyIsUnchanged verifies the merge is a true union:
// a plugin that ships only the legacy <plugin>/agents/*.md sidecar keeps
// every entry (no shadowing, no filtering).
func TestCycle234_MarkdownOnlyIsUnchanged(t *testing.T) {
	root := cycle198Root(t)
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
	got := pluginAgents("md-only")
	want := []string{"alpha", "beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(md-only) = %v, want %v", got, want)
	}
}

// TestCycle234_JsonOnlyIsLoaded mirrors the changelog line 13 fix: a plugin
// with *only* an agents.json at the root still gets every declared subagent
// surfaced. Pre-cycle-234 the discover / install / validate paths read
// nothing from agents.json.
func TestCycle234_JsonOnlyIsLoaded(t *testing.T) {
	root := cycle198Root(t)
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
	writeJSONFile(t, filepath.Join(dir, "agents.json"), doc)
	got := pluginAgents("json-only")
	want := []string{"delta", "epsilon"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(json-only) = %v, want %v", got, want)
	}
}

// TestCycle234_UnionOfBothSources: the merge of agents/*.md and agents.json
// returns the union, sorted, with neither source shadowing.
func TestCycle234_UnionOfBothSources(t *testing.T) {
	root := cycle198Root(t)
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
	writeJSONFile(t, filepath.Join(dir, "agents.json"), doc)
	got := pluginAgents("both")
	want := []string{"json-x", "json-y", "md-a", "md-b", "shared"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(both) = %v, want %v (shared dedupes)", got, want)
	}
}

// TestCycle234_NeitherPresentUnaffected: a plugin with no agents source
// at all returns nil — the empty-state row the Installed detail block
// turns into "-".
func TestCycle234_NeitherPresentUnaffected(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "empty")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"empty"}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got := pluginAgents("empty")
	if got != nil {
		t.Errorf("pluginAgents(empty) = %v, want nil", got)
	}
}

// TestCycle234_DiscoverPreviewListsAllAgents: the /plugin Discover detail
// block's `Agents:` value mirrors pluginAgents(name) on the unpacked-cache
// mirror (the same way Components does). When the plugin has been unpacked
// into <cache>/marketplaces/<mkt>/plugins/<name>/<ver>/, the agents.json + the
// agents/*.md sidecar both contribute.
func TestCycle234_DiscoverPreviewListsAllAgents(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{
		{"p", "P plugin.", "9.9.9"},
	})
	// Unpack path: cache/marketplaces/mkt/plugins/p/9.9.9/agents.json
	unpack := filepath.Join(root, "antigravity-cli", "cache", "marketplaces",
		"mkt", "plugins", "p", "9.9.9")
	if err := os.MkdirAll(filepath.Join(unpack, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONFile(t, filepath.Join(unpack, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "alpha-agent", "path": "agents/alpha.md"},
		},
	})
	if err := os.WriteFile(filepath.Join(unpack, "agents", "beta.md"),
		[]byte("# beta"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines := pluginDiscoverDetails("p", pluginMarketplaces(), 1<<20)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "alpha-agent, beta") {
		t.Errorf("Agents line missing union: %q", joined)
	}
}

// TestCycle234_InstalledDetailsHasAgentsRow pins the Installed-tab detail
// block's six-line shape after cycle 234: Description, Skills, Agents,
// Marketplace, Path, Version — the new Agents row sits between Skills and
// Marketplace so the panel does not break the cycle 198 byte-layout for
// any existing column.
func TestCycle234_InstalledDetailsHasAgentsRow(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "cycle234")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"cycle234","description":"x","version":"1.0.0"}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "zeta.md"),
		[]byte("# zeta"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	details := pluginInstalledDetails("cycle234", nil)
	joined := strings.Join(details, "\n")
	// The SGR codes pluginDetailLine wraps each row in make the literal
	// "      Version:     1.0.0" not appear as a substring; we assert the
	// row labels individually and check that the agents row carries
	// "zeta" verbatim (no comma separator on a single entry).
	wantSubstrs := []string{
		"Description:",
		"Skills:", // empty value (no skills seeded) — the label itself proves the row is present
		"Agents:      zeta",
		"Marketplace:",
		"Path:        ",
		"Version:     1.0.0",
	}
	for _, w := range wantSubstrs {
		if !strings.Contains(joined, w) {
			t.Errorf("Installed detail block missing %q:\n%s", w, joined)
		}
	}
	// And there are exactly six detail rows now (Description / Skills /
	// Agents / Marketplace / Path / Version).
	if len(details) != 6 {
		t.Errorf("Installed detail block has %d rows, want 6: %q", len(details), details)
	}
}

// TestCycle234_MalformedAgentsJsonFallsBack: a malformed agents.json does
// NOT crash the loader. pluginAgents is the TUI-side entry point and is
// expected to silently treat the JSON as missing; pluginAgentsAtPath (used
// by the CLI handlers) does surface the parse error and the listings are
// empty — both behaviours are deliberately different.
func TestCycle234_MalformedAgentsJsonFallsBack(t *testing.T) {
	root := cycle198Root(t)
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
	got := pluginAgents("broken-json")
	if strings.Join(got, ",") != "alpha" {
		t.Errorf("pluginAgents(broken-json) = %v, want [alpha]", got)
	}

	// CLI side: the parse error must surface so `agy plugin validate`
	// can refuse the install.
	_, jsonErr, _ := pluginAgentsAtPath(dir)
	if jsonErr == nil {
		t.Errorf("pluginAgentsAtPath did not surface JSON parse error")
	}
}

// TestCycle234_AgentsJsonEntryMissingNameSkipped: a parseable JSON file
// whose entries lack `name` (the only required field) drops the
// nameless entries silently. The on-disk filename is the fallback name in
// the markdown sidecar; the JSON manifest has no equivalent.
func TestCycle234_AgentsJsonEntryMissingNameSkipped(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "nameless")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONFile(t, filepath.Join(dir, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "ok", "path": "x.md"},
			{"path": "no-name.md"}, // missing name
			{"name": "", "path": "empty.md"},
		},
	})
	got := pluginAgents("nameless")
	if strings.Join(got, ",") != "ok" {
		t.Errorf("pluginAgents(nameless) = %v, want [ok]", got)
	}
}

// TestCycle234_JsonFlatFormAccepted: an accidentally-flat JSON array (a
// common pre-1.3.3 typo that AGY keeps tolerating) parses the same way.
func TestCycle234_JsonFlatFormAccepted(t *testing.T) {
	root := cycle198Root(t)
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
	got := pluginAgents("flat")
	want := []string{"f1", "f2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(flat) = %v, want %v", got, want)
	}
}

// TestCycle234_JsonExcludesNonMdFiles: an agents/ directory with
// non-markdown sidecars (README.md's only the suffix `.md`, but
// `agents.txt` has no suffix) doesn't contribute to the listing. Same
// filter as the cycle-198 skills/ reader.
func TestCycle234_JsonExcludesNonMdFiles(t *testing.T) {
	root := cycle198Root(t)
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
	got := pluginAgents("no-md")
	if strings.Join(got, ",") != "real" {
		t.Errorf("pluginAgents(no-md) = %v, want [real]", got)
	}
}

// TestCycle234_JsonSortIsStable: the order is alphabetical regardless of
// the on-disk file order or the JSON entry order. Pins the `sort.Strings`
// call in pluginAgents.
func TestCycle234_JsonSortIsStable(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "sort")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONFile(t, filepath.Join(dir, "agents.json"), map[string]any{
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
	got := pluginAgents("sort")
	want := []string{"alpha", "mike", "yankee", "zulu"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(sort) = %v, want %v", got, want)
	}
}
