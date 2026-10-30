// catalog_test.go — the plugin and marketplace documents the
// remote-control Web UI's plugin surfaces read.
//
// 【一手】 2026-10-30: the shapes are read off this machine's
// ~/.gemini/config/plugins/*/plugin.json and
// ~/.gemini/antigravity-cli/cache/marketplaces/*/marketplace.json.
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedCatalog writes a plugins tree and a marketplace catalogue under
// a temporary $ANTIGRAVITY_HOME.
func seedCatalog(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))

	plugins := filepath.Join(home, ".gemini", "config", "plugins")
	writeCatalogFile(t, filepath.Join(plugins, "alpha", "plugin.json"), `{
  "name": "alpha",
  "displayName": "Alpha",
  "version": "1.0.0",
  "description": "The first plugin.",
  "author": {"name": "Alpha Author", "url": "https://example.invalid"}
}`)
	writeCatalogFile(t, filepath.Join(plugins, "alpha", "skills", "alpha-skill", "SKILL.md"),
		"---\nname: alpha-skill\ndescription: The alpha skill.\n---\n\nBody.\n")

	// A plugin whose author is the plain string the Claude Code
	// import carries.
	writeCatalogFile(t, filepath.Join(plugins, "beta", "plugin.json"),
		`{"name":"beta","version":"2.0.0","description":"The second plugin.","author":"Beta Author"}`)

	// A directory without a plugin.json is not a plugin.
	if err := os.MkdirAll(filepath.Join(plugins, "not-a-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	market := filepath.Join(home, ".gemini", "antigravity-cli", "cache", "marketplaces", "mkt")
	writeCatalogFile(t, filepath.Join(market, "marketplace.json"), `{
  "name": "mkt",
  "description": "A marketplace.",
  "plugins": [
    {"name":"alpha","displayName":"Alpha","version":"1.0.0","description":"The first plugin."},
    {"name":"gamma","displayName":"Gamma","version":"3.0.0","description":"A third plugin.","keywords":["gaming"]}
  ]
}`)
}

// writeCatalogFile writes a file, creating its directory.
func writeCatalogFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestInstalledPlugins_ListsThePluginsWithTheirManifests pins the
// reader.
func TestInstalledPlugins_ListsThePluginsWithTheirManifests(t *testing.T) {
	seedCatalog(t)

	plugins := InstalledPlugins()
	if len(plugins) != 2 {
		t.Fatalf("got %d plugins, want 2: %#v", len(plugins), plugins)
	}
	if plugins[0].Name != "alpha" || plugins[1].Name != "beta" {
		t.Errorf("the plugins are %q and %q, want alpha and beta", plugins[0].Name, plugins[1].Name)
	}
	alpha := plugins[0]
	if alpha.Manifest.DisplayName != "Alpha" {
		t.Errorf("alpha's display name is %q, want Alpha", alpha.Manifest.DisplayName)
	}
	if alpha.Manifest.Author.Name != "Alpha Author" {
		t.Errorf("alpha's author is %q, want Alpha Author", alpha.Manifest.Author.Name)
	}
	if alpha.Path == "" {
		t.Error("alpha carries no path")
	}
	// The author that is a plain string.
	if plugins[1].Manifest.Author.Name != "Beta Author" {
		t.Errorf("beta's author is %q, want Beta Author", plugins[1].Manifest.Author.Name)
	}
	// A plugin the configuration does not name is enabled.
	if plugins[0].Disabled {
		t.Error("alpha is reported disabled, but nothing turned it off")
	}
}

// TestFindInstalledPlugin_ByName pins the lookup.
func TestFindInstalledPlugin_ByName(t *testing.T) {
	seedCatalog(t)

	if _, ok := FindInstalledPlugin("alpha"); !ok {
		t.Error("alpha was not found")
	}
	if _, ok := FindInstalledPlugin("nope"); ok {
		t.Error("nope was found")
	}
	if _, ok := FindInstalledPlugin(""); ok {
		t.Error("the empty name was found")
	}
}

// TestPluginSkills_ReadsTheFrontmatter pins the skill reader.
func TestPluginSkills_ReadsTheFrontmatter(t *testing.T) {
	seedCatalog(t)

	skills := PluginSkills("alpha")
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1: %#v", len(skills), skills)
	}
	if skills[0].Name != "alpha-skill" {
		t.Errorf("the skill's name is %q, want alpha-skill", skills[0].Name)
	}
	if skills[0].Description != "The alpha skill." {
		t.Errorf("the skill's description is %q, want The alpha skill.", skills[0].Description)
	}
	if skills[0].Path == "" {
		t.Error("the skill carries no path")
	}
}

// TestMarketplaceDocs_ReadsTheCatalogue pins the marketplace reader.
func TestMarketplaceDocs_ReadsTheCatalogue(t *testing.T) {
	seedCatalog(t)

	docs := MarketplaceDocs()
	if len(docs) != 1 {
		t.Fatalf("got %d marketplaces, want 1: %#v", len(docs), docs)
	}
	if docs[0].Name != "mkt" {
		t.Errorf("the marketplace is %q, want mkt", docs[0].Name)
	}
	if len(docs[0].Plugins) != 2 {
		t.Fatalf("the catalogue holds %d plugins, want 2", len(docs[0].Plugins))
	}
	if docs[0].Plugins[1].Name != "gamma" {
		t.Errorf("the second entry is %q, want gamma", docs[0].Plugins[1].Name)
	}
}

// TestMarketplaceEntryByID_ByName pins the lookup.
func TestMarketplaceEntryByID_ByName(t *testing.T) {
	seedCatalog(t)

	docs := MarketplaceDocs()
	entry, doc, ok := MarketplaceEntryByID(docs, "alpha")
	if !ok {
		t.Fatal("alpha was not found")
	}
	if entry.DisplayName != "Alpha" {
		t.Errorf("alpha's display name is %q, want Alpha", entry.DisplayName)
	}
	if doc.Name != "mkt" {
		t.Errorf("alpha's marketplace is %q, want mkt", doc.Name)
	}
	if _, _, ok := MarketplaceEntryByID(docs, "nope"); ok {
		t.Error("nope was found")
	}
}

// TestSearchMarketplaceEntries_ByNameDescriptionAndKeyword pins the
// search.
func TestSearchMarketplaceEntries_ByNameDescriptionAndKeyword(t *testing.T) {
	seedCatalog(t)

	docs := MarketplaceDocs()
	cases := []struct {
		query string
		want  string
	}{
		{"alpha", "alpha"},
		{"Alpha", "alpha"},
		{"first", "alpha"},
		{"gaming", "gamma"},
		{"", "alpha"},
	}
	for _, c := range cases {
		got := SearchMarketplaceEntries(docs, c.query)
		if len(got) == 0 {
			t.Errorf("the query %q found nothing", c.query)
			continue
		}
		if got[0].Name != c.want {
			t.Errorf("the query %q found %q first, want %q", c.query, got[0].Name, c.want)
		}
	}
	if got := SearchMarketplaceEntries(docs, "nothing-matches-this"); len(got) != 0 {
		t.Errorf("the query found %d entries, want none", len(got))
	}
}

// TestStripBOM_RemovesTheMark pins the reader's byte-order-mark
// handling — the reference's own files carry one.
func TestStripBOM_RemovesTheMark(t *testing.T) {
	got := StripBOM([]byte("\ufeff{\"a\":1}"))
	if string(got) != `{"a":1}` {
		t.Errorf("StripBOM = %q, want the mark gone", string(got))
	}
	plain := []byte(`{"a":1}`)
	if string(StripBOM(plain)) != `{"a":1}` {
		t.Errorf("StripBOM changed a plain document")
	}
}

// TestManifest_UnmarshalsTheAuthorsTwoShapes pins the author's two
// shapes against the JSON decoder directly.
func TestManifest_UnmarshalsTheAuthorsTwoShapes(t *testing.T) {
	var m Manifest
	if err := json.Unmarshal([]byte(`{"name":"x","author":{"name":"Object"}}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Author.Name != "Object" {
		t.Errorf("the object's author is %q, want Object", m.Author.Name)
	}
	if err := json.Unmarshal([]byte(`{"name":"x","author":"String"}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Author.Name != "String" {
		t.Errorf("the string's author is %q, want String", m.Author.Name)
	}
}
