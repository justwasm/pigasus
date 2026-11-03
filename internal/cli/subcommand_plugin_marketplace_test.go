// subcommand_plugin_marketplace_test.go — `agy plugin install <name>[@marketplace]`.
//
// The tests point $ANTIGRAVITY_HOME at a scratch directory (the feed is
// read from <antigravity home>/cache/marketplaces/) and $GEMINI_CONFIG_HOME
// at another one (the plugin lands in <config home>/plugins/), so neither
// the developer's own feed nor the developer's own plugin tree is touched.
package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// marketplaceArchive builds a plugin archive and its digest.
func marketplaceArchive(t *testing.T) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	files := map[string]string{
		"plugin.json":              `{"name":"firebase","version":"1.0.3"}`,
		"skills/firebase/SKILL.md": "# firebase",
		"agents/helper.md":         "# helper",
	}
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// seedMarketplace writes a cached marketplace feed with one plugin.
func seedMarketplace(t *testing.T, marketplace, name, url, sum string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("ANTIGRAVITY_HOME"), "cache", "marketplaces", marketplace)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	doc := map[string]any{
		"name":        marketplace,
		"description": "the test marketplace",
		"plugins": []map[string]any{{
			"name":    name,
			"version": "1.0.3",
			"source": map[string]any{
				"type":   "archive",
				"url":    url,
				"sha256": sum,
			},
		}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal feed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), data, 0600); err != nil {
		t.Fatalf("write feed: %v", err)
	}
}

// TestCLI_InstallFromMarketplace: a bare plugin name installs from the
// official marketplace.
//
// 【一手】 official-docs/marketplace.md:149-153 — a bare <plugin-name>
// defaults to <plugin-name>@antigravity-plugins-official.
func TestCLI_InstallFromMarketplace(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	archive, sum := marketplaceArchive(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	seedMarketplace(t, "antigravity-plugins-official", "firebase", srv.URL+"/firebase.zip", sum)

	var out, errOut strings.Builder
	j := newTestJ()
	j.Out = &out
	j.ErrOut = &errOut
	if err := runPluginInstall(j, []string{"firebase"}); err != nil {
		t.Fatalf("install: %v (stderr %q)", err, errOut.String())
	}

	if !strings.Contains(out.String(), `Successfully installed plugin "firebase" from "antigravity-plugins-official".`) {
		t.Errorf("install output = %q", out.String())
	}
	if !strings.Contains(out.String(), "skills      : 1 processed") {
		t.Errorf("skills count missing: %q", out.String())
	}
	if !strings.Contains(out.String(), "agents      : 1 processed") {
		t.Errorf("agents count missing: %q", out.String())
	}

	// The plugin is installed under <config home>/plugins/<name>/.
	dst := filepath.Join(cfgHome, "plugins", "firebase")
	if _, err := os.Stat(filepath.Join(dst, "plugin.json")); err != nil {
		t.Errorf("installed plugin missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "skills", "firebase", "SKILL.md")); err != nil {
		t.Errorf("installed skill missing: %v", err)
	}

	// And it is enabled in config.json.
	data, err := os.ReadFile(filepath.Join(cfgHome, "config.json"))
	if err != nil {
		t.Fatalf("config.json missing: %v", err)
	}
	if !strings.Contains(string(data), `"firebase"`) || !strings.Contains(string(data), `"enabled": true`) {
		t.Errorf("config.json missing the enabled row: %s", data)
	}
}

// TestCLI_InstallFromNamedMarketplace: "<name>@<marketplace>" installs from
// that marketplace.
func TestCLI_InstallFromNamedMarketplace(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	archive, sum := marketplaceArchive(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	seedMarketplace(t, "my-marketplace", "firebase", srv.URL+"/firebase.zip", sum)

	var out, errOut strings.Builder
	j := newTestJ()
	j.Out = &out
	j.ErrOut = &errOut
	if err := runPluginInstall(j, []string{"firebase@my-marketplace"}); err != nil {
		t.Fatalf("install: %v (stderr %q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), `Successfully installed plugin "firebase" from "my-marketplace".`) {
		t.Errorf("install output = %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(cfgHome, "plugins", "firebase", "plugin.json")); err != nil {
		t.Errorf("installed plugin missing: %v", err)
	}
}

// TestCLI_InstallFromMarketplaceFails: an unknown plugin and an uncached
// marketplace are refused, and nothing is installed.
//
// A bare target that names no plugin is refused with the reference's own
// sentence — "Error: install target must be a directory: %s", the 1.4.6
// capture of `agy plugin install --help"
// (internal/cli/testdata/help_1.4.6.txt:278). A target that names a
// marketplace explicitly gets the reason instead.
func TestCLI_InstallFromMarketplaceFails(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	archive, sum := marketplaceArchive(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	seedMarketplace(t, "antigravity-plugins-official", "firebase", srv.URL+"/firebase.zip", sum)

	var out, errOut strings.Builder
	j := newTestJ()
	j.Out = &out
	j.ErrOut = &errOut
	if err := runPluginInstall(j, []string{"nope"}); err == nil {
		t.Error("install accepted a plugin the feed does not hold")
	}
	if !strings.Contains(errOut.String(), "Error: install target must be a directory: nope") {
		t.Errorf("stderr = %q, want the reference's sentence", errOut.String())
	}

	errOut.Reset()
	if err := runPluginInstall(j, []string{"firebase@no-such-marketplace"}); err == nil {
		t.Error("install accepted a marketplace that is not cached")
	}
	if !strings.Contains(errOut.String(), "no-such-marketplace") {
		t.Errorf("stderr = %q, want it to name the marketplace", errOut.String())
	}

	errOut.Reset()
	if err := runPluginInstall(j, []string{"https://github.com/owner/repo"}); err == nil {
		t.Error("install accepted a repository URL")
	}
	if !strings.Contains(errOut.String(), "repository URL") {
		t.Errorf("stderr = %q, want it to name the limitation", errOut.String())
	}

	// A target that looks like a filesystem path is a path, even when it
	// carries an "@" — the marketplace form is a bare plugin name.
	errOut.Reset()
	if err := runPluginInstall(j, []string{"./local@dir"}); err == nil {
		t.Error("install accepted a path that does not exist")
	}
	if !strings.Contains(errOut.String(), "Error: install target must be a directory: ./local@dir") {
		t.Errorf("stderr = %q, want the reference's sentence", errOut.String())
	}

	if _, err := os.Stat(filepath.Join(cfgHome, "plugins", "firebase")); !os.IsNotExist(err) {
		t.Errorf("a failed install left a plugin behind: %v", err)
	}
}
