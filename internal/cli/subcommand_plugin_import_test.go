// subcommand_plugin_import_test.go — cycle 364 (agy 1.4.11): the
// `agy plugin import` surface.
//
// 【一手】 agy 1.4.11: the bare command imports from both sources, an
// unknown source is an error ("Error: unknown import source or invalid
// path: <arg>", exit 1) and a discovery failure is reported on stderr
// with exit 0.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runPluginImportCLI runs `plugin import` against a fake home and returns
// stdout, stderr and the error.
func runPluginImportCLI(t *testing.T, home string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))
	var out, errOut bytes.Buffer
	j := &JetskiCLI{Out: &out, ErrOut: &errOut}
	err := runPluginImport(j, args)
	return out.String(), errOut.String(), err
}

// An unknown source is an error.
func TestPluginImportRejectsAnUnknownSource(t *testing.T) {
	home := t.TempDir()
	_, errOut, err := runPluginImportCLI(t, home, "bogus")
	if err == nil {
		t.Fatal("the command did not fail")
	}
	if !strings.Contains(errOut, "Error: unknown import source or invalid path: bogus") {
		t.Errorf("stderr = %q", errOut)
	}
}

// The bare command reports both empty sources.
func TestPluginImportWithoutSources(t *testing.T) {
	home := t.TempDir()
	out, _, err := runPluginImportCLI(t, home)
	if err != nil {
		t.Fatalf("the command failed: %v", err)
	}
	want := "No gemini extensions found.\nNo claude extensions found.\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// A Claude Code plugin is staged into ~/.gemini/config/plugins.
func TestPluginImportClaude(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "plugins", "cp", ".claude-plugin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"cp","version":"1.0.0"}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, _, err := runPluginImportCLI(t, home, "claude")
	if err != nil {
		t.Fatalf("the command failed: %v", err)
	}
	if !strings.Contains(out, "  [ok]    cp\n") {
		t.Errorf("stdout = %q", out)
	}
	staged := filepath.Join(home, ".gemini", "config", "plugins", "cp", "plugin.json")
	if _, err := os.Stat(staged); err != nil {
		t.Errorf("the plugin was not staged: %v", err)
	}
}
