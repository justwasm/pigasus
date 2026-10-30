// symlink_test.go — cycle plugin-source-symlink-resolve:
// integration tests that prove the plugins package loaders now follow symlinks.
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// withPluginHome points auth.GeminiConfigHome / AntigravityConfigHome
// at a temp directory for the duration of the test. Every plugin
// loader reads from those two roots; redirecting
// them is the only way to inject a fixture without touching the
// real user's ~/.gemini tree.
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
	// StateDir = <geminiConfigHome>/plugins
	if err := os.MkdirAll(filepath.Join(geminiConfigHome, "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	// MarketplaceRoot = <antigravityHome>/cache/marketplaces
	if err := os.MkdirAll(filepath.Join(antigravityHome, "cache", "marketplaces"), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	return geminiConfigHome
}

// TestPluginInstalledSymlinkFollowed: a symlinked <plugins>/<name>
// directory shows up in InstalledRows. Before the fix,
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

	rows := InstalledRows()
	var got string
	var gotDesc string
	for _, r := range rows {
		if r.Name == "linked-plugin" {
			got = r.Name
			gotDesc = r.Desc
		}
	}
	if got != "linked-plugin" {
		t.Fatalf("InstalledRows() missing linked-plugin; got %d rows", len(rows))
	}
	if gotDesc != "linked in from elsewhere" {
		t.Errorf("desc = %q, want %q", gotDesc, "linked in from elsewhere")
	}
}

// TestPluginSkillsSymlinkFollowed: a symlinked
// <plugin>/skills/<x>/ directory shows up in Skills.
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

	got := Skills("skill-linker")
	want := []string{"real-skill"}
	if !equalStringSlices(got, want) {
		t.Fatalf("Skills = %v, want %v", got, want)
	}
}

// TestPluginAgentsMarkdownSymlinkFollowed: a symlinked
// <plugin>/agents/<x>.md file shows up in AgentsMarkdown.
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

	got := AgentsMarkdown("agents-linker")
	want := []string{"linked-bot"}
	if !equalStringSlices(got, want) {
		t.Fatalf("AgentsMarkdown = %v, want %v", got, want)
	}
}

// TestPluginAgentsAtPathSymlinkFollowed: a symlinked
// <plugin>/agents/<x>.md file shows up in AgentsAtPath
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

	names, jsonErr, missing := AgentsAtPath(root)
	if jsonErr != nil {
		t.Fatalf("jsonErr: %v", jsonErr)
	}
	if missing {
		t.Fatalf("AgentsAtPath says nothing was found; want linked-bot")
	}
	if !equalStringSlices(names, []string{"linked-bot"}) {
		t.Fatalf("AgentsAtPath names = %v, want [linked-bot]", names)
	}
}

// TestPluginMarketplaceSymlinkFollowed: a symlinked
// <cache>/marketplaces/<mkt>/ directory shows up in
// Marketplaces.
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

	mkts := Marketplaces()
	var found []string
	for _, m := range mkts {
		if m.Name == "linked-mkt" {
			for _, p := range m.Items {
				if p.Name == "linked" {
					found = append(found, p.Name)
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
// mentions CLAUDE.md / AGENTS.md inside a plugin.
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

	rows := InstalledRows()
	var found bool
	for _, r := range rows {
		if r.Name == "rules-linker" {
			found = true
		}
	}
	if !found {
		t.Errorf("rules-linker did not surface in InstalledRows (symlink not followed)")
	}
}

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
