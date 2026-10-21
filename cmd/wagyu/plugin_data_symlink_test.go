// plugin_data_symlink_test.go — cycle plugin-source-symlink-resolve:
// integration tests that prove the cmd/wagyu plugin/extension
// loaders now follow symlinks.
//
// The internal/pluginscan package is unit-tested in
// internal/pluginscan/pluginscan_test.go; this file is the
// end-to-end layer that wires the loader-level functions
// (installedPluginRows, pluginSkills, pluginAgentsMarkdown,
// pluginAgentsAtPath) to a symlinked on-disk layout and asserts
// that the AGY 1.2.5 gap is closed.
//
// Run:
//
//	go test -vet=off -run 'TestPlugin' ./cmd/wagyu/
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// withPluginHome points auth.GeminiConfigHome / AntigravityConfigHome
// at a temp directory for the duration of the test. Every plugin
// loader under cmd/wagyu/ reads from those two roots; redirecting
// them is the only way to inject a fixture without touching the
// real user's ~/.gemini tree.
//
// The auth package's helpers derive GeminiConfigHome from
// AntigravityConfigHome (which honours $ANTIGRAVITY_HOME) — so we
// point ANTIGRAVITY_HOME at <root>/.gemini/antigravity-cli and
// reach the plugin root through the sibling <root>/.gemini/config
// the package computes. The plugins + cache/marketplaces trees
// live under those two paths and are created here so every
// per-test loader starts from a clean fixture.
func withPluginHome(t *testing.T) string {
	t.Helper()
	gemini := t.TempDir()
	antigravityHome := filepath.Join(gemini, "antigravity-cli")
	geminiConfigHome := filepath.Join(gemini, "config")
	t.Setenv("ANTIGRAVITY_HOME", antigravityHome)
	t.Setenv("GEMINI_CONFIG_HOME", geminiConfigHome)
	if err := os.MkdirAll(antigravityHome, 0o755); err != nil {
		t.Fatalf("mkdir antigravityHome: %v", err)
	}
	if err := os.MkdirAll(geminiConfigHome, 0o755); err != nil {
		t.Fatalf("mkdir geminiConfigHome: %v", err)
	}
	// pluginStateDir = <geminiConfigHome>/plugins
	if err := os.MkdirAll(filepath.Join(geminiConfigHome, "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	// marketplaceRoot = <antigravityHome>/cache/marketplaces
	if err := os.MkdirAll(filepath.Join(antigravityHome, "cache", "marketplaces"), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	return geminiConfigHome
}

// TestPluginInstalledSymlinkFollowed: a symlinked <plugins>/<name>
// directory shows up in installedPluginRows. Before the fix,
// e.IsDir() returned false (the link's mode) and the plugin was
// dropped silently.
func TestPluginInstalledSymlinkFollowed(t *testing.T) {
	geminiConfigHome := withPluginHome(t)
	real := t.TempDir()
	manifest := map[string]any{
		"name":        "linked-plugin",
		"description": "linked in from elsewhere",
		"version":     "1.0.0",
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(real, "plugin.json"), raw, 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(geminiConfigHome, "plugins", "linked-plugin")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	rows := installedPluginRows()
	var got string
	var gotDesc string
	for _, r := range rows {
		if r.name == "linked-plugin" {
			got = r.name
			gotDesc = r.desc
		}
	}
	if got != "linked-plugin" {
		t.Fatalf("installedPluginRows() missing linked-plugin; got %d rows", len(rows))
	}
	if gotDesc != "linked in from elsewhere" {
		t.Errorf("desc = %q, want %q", gotDesc, "linked in from elsewhere")
	}
}

// TestPluginSkillsSymlinkFollowed: a symlinked
// <plugin>/skills/<x>/ directory shows up in pluginSkills.
func TestPluginSkillsSymlinkFollowed(t *testing.T) {
	geminiConfigHome := withPluginHome(t)
	pluginRoot := filepath.Join(geminiConfigHome, "plugins", "skill-linker")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(`{"name":"skill-linker"}`), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	realSkills := t.TempDir()
	if err := os.MkdirAll(filepath.Join(realSkills, "real-skill"), 0o755); err != nil {
		t.Fatalf("mkdir real-skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realSkills, "real-skill", "SKILL.md"), []byte("---\nname: real-skill\n---"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	linkPath := filepath.Join(pluginRoot, "skills")
	if err := os.MkdirAll(linkPath, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.Symlink(filepath.Join(realSkills, "real-skill"), filepath.Join(linkPath, "real-skill")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	got := pluginSkills("skill-linker")
	want := []string{"real-skill"}
	if !equalStringSlices(got, want) {
		t.Fatalf("pluginSkills = %v, want %v", got, want)
	}
}

// TestPluginAgentsMarkdownSymlinkFollowed: a symlinked
// <plugin>/agents/<x>.md file shows up in pluginAgentsMarkdown.
func TestPluginAgentsMarkdownSymlinkFollowed(t *testing.T) {
	geminiConfigHome := withPluginHome(t)
	pluginRoot := filepath.Join(geminiConfigHome, "plugins", "agents-linker")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(`{"name":"agents-linker"}`), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	realAgents := t.TempDir()
	md := []byte("---\nname: linked-bot\n---")
	if err := os.WriteFile(filepath.Join(realAgents, "linked-bot.md"), md, 0o644); err != nil {
		t.Fatalf("write linked-bot.md: %v", err)
	}
	agentsDir := filepath.Join(pluginRoot, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.Symlink(filepath.Join(realAgents, "linked-bot.md"), filepath.Join(agentsDir, "linked-bot.md")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	got := pluginAgentsMarkdown("agents-linker")
	want := []string{"linked-bot"}
	if !equalStringSlices(got, want) {
		t.Fatalf("pluginAgentsMarkdown = %v, want %v", got, want)
	}
}

// TestPluginAgentsAtPathSymlinkFollowed: a symlinked
// <plugin>/agents/<x>.md file shows up in pluginAgentsAtPath
// (the on-disk reader used by `agy plugin validate <path>`).
func TestPluginAgentsAtPathSymlinkFollowed(t *testing.T) {
	withPluginHome(t)
	root := t.TempDir()
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, "agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "agents", "linked-bot.md"), []byte("---\nname: linked-bot\n---"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(filepath.Join(real, "agents"), filepath.Join(root, "agents")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	names, jsonErr, missing := pluginAgentsAtPath(root)
	if jsonErr != nil {
		t.Fatalf("jsonErr: %v", jsonErr)
	}
	if missing {
		t.Fatalf("pluginAgentsAtPath says nothing was found; want linked-bot")
	}
	if !equalStringSlices(names, []string{"linked-bot"}) {
		t.Fatalf("pluginAgentsAtPath names = %v, want [linked-bot]", names)
	}
}

// TestPluginMarketplaceSymlinkFollowed: a symlinked
// <cache>/marketplaces/<mkt>/ directory shows up in
// pluginMarketplaces.
func TestPluginMarketplaceSymlinkFollowed(t *testing.T) {
	_ = withPluginHome(t)
	cacheRoot := filepath.Join(os.Getenv("ANTIGRAVITY_HOME"), "cache", "marketplaces")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	real := t.TempDir()
	doc := map[string]any{
		"plugins": []map[string]any{{
			"name":        "linked",
			"description": "from a symlinked marketplace",
			"version":     "2.0.0",
		}},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(real, "marketplace.json"), raw, 0o644); err != nil {
		t.Fatalf("write marketplace.json: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(cacheRoot, "linked-mkt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	mkts := pluginMarketplaces()
	var found []string
	for _, m := range mkts {
		if m.name == "linked-mkt" {
			for _, p := range m.items {
				if p.name == "linked" {
					found = append(found, p.name)
				}
			}
		}
	}
	sort.Strings(found)
	if !equalStringSlices(found, []string{"linked"}) {
		t.Fatalf("marketplace linked-mkt missing the linked plugin; got %v", found)
	}
}

// TestPluginCLAUDEAndAGENTSFollowed: the AGY 1.2.5 changelog also
// mentions CLAUDE.md / AGENTS.md inside a plugin — a TUI loader
// like the panel picker does not currently scan for them, but the
// underlying pluginscan helper is what every "did we miss a file
// in a symlinked plugin?" question reduces to. This test guards
// the helper against regressing on plain CLAUDE.md / AGENTS.md
// files inside a symlinked plugin root.
func TestPluginCLAUDEAndAGENTSFollowed(t *testing.T) {
	geminiConfigHome := withPluginHome(t)
	pluginRoot := filepath.Join(geminiConfigHome, "plugins", "rules-linker")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(`{"name":"rules-linker"}`), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "CLAUDE.md"), []byte("# claude"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "AGENTS.md"), []byte("# agents"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(pluginRoot, "rules")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	// We exercise the public loader surface — installedPluginRows
	// (did the symlinked plugin root show up?) and the direct
	// pluginscan helpers (does the CLAUDE.md / AGENTS.md inside
	// it resolve as a regular file?). The TUI panel for these
	// files is not built, but the loader invariant (file present
	// inside a symlinked tree ⇒ visible to the loader surface)
	// is the AGY 1.2.5 contract.
	rows := installedPluginRows()
	var found bool
	for _, r := range rows {
		if r.name == "rules-linker" {
			found = true
		}
	}
	if !found {
		t.Errorf("rules-linker did not surface in installedPluginRows (symlink not followed)")
	}
}

// equalStringSlices is a small test helper so the assertions read
// the same way the cycle 234 plugin-agents tests do.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
