package pigrt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/pigrt"
)

// The prompt's <skills> list includes the workspace's .agents/skills and the
// skills of every loadable plugin, but excludes the shared ~/.gemini/skills
// library and plugins without a plugin.json or with enabled=false. Verified
// against live 1.4.3 captures.
func TestAgySkillPaths_WorkspaceAndPluginFiltering(t *testing.T) {
	root := t.TempDir()
	agentHome := filepath.Join(root, ".gemini", "antigravity-cli")
	workspace := filepath.Join(root, "ws")
	mk := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join(agentHome, "builtin", "skills"))
	mk(filepath.Join(root, ".gemini", "skills", "shared-only"))
	mk(filepath.Join(workspace, ".agents", "skills", "ws-skill"))

	userPlugin := filepath.Join(root, ".gemini", "config", "plugins", "up")
	mk(filepath.Join(userPlugin, "skills", "up-skill"))
	if err := os.WriteFile(filepath.Join(userPlugin, "plugin.json"), []byte(`{"name":"up"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	disabled := filepath.Join(root, ".gemini", "config", "plugins", "off")
	mk(filepath.Join(disabled, "skills", "off-skill"))
	if err := os.WriteFile(filepath.Join(disabled, "plugin.json"), []byte(`{"name":"off"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gemini", "config", "config.json"),
		[]byte(`{"plugins":{"off":{"enabled":false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wsPlugin := filepath.Join(workspace, ".agents", "plugins", "wp")
	mk(filepath.Join(wsPlugin, "skills", "wp-skill"))
	if err := os.WriteFile(filepath.Join(wsPlugin, "plugin.json"), []byte(`{"name":"wp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A folder without plugin.json contributes nothing.
	mk(filepath.Join(workspace, ".agents", "plugins", "ghost", "skills", "ghost-skill"))

	paths := pigrt.AgySkillPaths(agentHome, workspace)
	has := func(want string) bool {
		for _, p := range paths {
			if p == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		filepath.Join(agentHome, "builtin", "skills"),
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(userPlugin, "skills"),
		filepath.Join(wsPlugin, "skills"),
	} {
		if !has(want) {
			t.Errorf("missing root %q in %v", want, paths)
		}
	}
	for _, absent := range []string{
		filepath.Join(root, ".gemini", "skills"),
		filepath.Join(disabled, "skills"),
		filepath.Join(workspace, ".agents", "plugins", "ghost", "skills"),
	} {
		if has(absent) {
			t.Errorf("unexpected root %q in %v", absent, paths)
		}
	}
}
