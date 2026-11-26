package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdminPluginControls_PluginsDisabledBlocksInstalledAndMarketplace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GEMINI_CONFIG_HOME", filepath.Join(tmp, ".gemini", "config"))
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(tmp, ".gemini", "antigravity-cli"))

	// Create an installed plugin and a marketplace.
	pluginDir := filepath.Join(tmp, ".gemini", "config", "plugins", "demo-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo-plugin","description":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mktDir := filepath.Join(tmp, ".gemini", "antigravity-cli", "cache", "marketplaces", "official")
	if err := os.MkdirAll(mktDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mktDir, "marketplace.json"), []byte(`{"plugins":[{"name":"demo-plugin","description":"demo"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	adminPath := filepath.Join(tmp, "admin_settings.json")
	if err := os.WriteFile(adminPath, []byte(`{"pluginControls":{"pluginsDisabled":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_ADMIN_SETTINGS", adminPath)

	if got := LoadedDirs(filepath.Join(tmp, ".gemini", "config", "plugins"), ""); len(got) != 0 {
		t.Fatalf("LoadedDirs with pluginsDisabled = %v, want empty", got)
	}
	if got := InstalledPlugins(); len(got) != 0 {
		t.Fatalf("InstalledPlugins with pluginsDisabled = %v, want empty", got)
	}
	if got := InstalledRows(); len(got) != 0 {
		t.Fatalf("InstalledRows with pluginsDisabled = %v, want empty", got)
	}
	if got := Marketplaces(); len(got) != 0 {
		t.Fatalf("Marketplaces with pluginsDisabled = %v, want empty", got)
	}
}
