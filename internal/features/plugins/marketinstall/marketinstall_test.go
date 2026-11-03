// marketinstall_test.go — the marketplace installer's own tests.
//
// Every test points $ANTIGRAVITY_HOME at a scratch directory: the feed is
// read from `<antigravity home>/cache/marketplaces/`, so a test without the
// override would read the developer's own cached feed.
package marketinstall

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

// scratchHome points $ANTIGRAVITY_HOME at a fresh directory.
func scratchHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	return home
}

// writeFeed writes a marketplace.json with the given entries.
//
// An entry is the reference's own shape: a name, a version and a source
// that names the archive and its digest.
func writeFeed(t *testing.T, marketplace string, entries []map[string]any) {
	t.Helper()
	dir := filepath.Join(os.Getenv("ANTIGRAVITY_HOME"), "cache", "marketplaces", marketplace)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	doc := map[string]any{
		"name":        marketplace,
		"description": "the test marketplace",
		"plugins":     entries,
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal feed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), data, 0600); err != nil {
		t.Fatalf("write feed: %v", err)
	}
}

// zipBytes builds a zip archive from a name → content map.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
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
	return buf.Bytes()
}

// digest is the hex sha256 of some bytes.
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestSplitTarget pins the documented target grammar: a bare name resolves
// to the official marketplace, and "<name>@<marketplace>" names one.
func TestSplitTarget(t *testing.T) {
	tgt, err := SplitTarget("firebase")
	if err != nil {
		t.Fatalf("SplitTarget(firebase): %v", err)
	}
	if tgt.Name != "firebase" || tgt.Marketplace != DefaultMarketplace {
		t.Errorf("SplitTarget(firebase) = %q/%q, want firebase/%s", tgt.Name, tgt.Marketplace, DefaultMarketplace)
	}
	if tgt.Explicit {
		t.Error("a bare name reported an explicit marketplace")
	}

	tgt, err = SplitTarget("firebase@antigravity-plugins-official")
	if err != nil {
		t.Fatalf("SplitTarget(firebase@…): %v", err)
	}
	if tgt.Name != "firebase" || tgt.Marketplace != "antigravity-plugins-official" {
		t.Errorf("SplitTarget(firebase@…) = %q/%q", tgt.Name, tgt.Marketplace)
	}
	if !tgt.Explicit {
		t.Error("a named marketplace reported itself as implicit")
	}

	if _, err := SplitTarget("https://github.com/owner/repo"); err == nil {
		t.Error("SplitTarget accepted a repository URL")
	}
	if _, err := SplitTarget("   "); err == nil {
		t.Error("SplitTarget accepted an empty target")
	}
}

// TestLookupReadsTheCachedFeed pins that the entry comes from the cached
// marketplace.json under <antigravity home>/cache/marketplaces/<marketplace>/.
func TestLookupReadsTheCachedFeed(t *testing.T) {
	scratchHome(t)
	writeFeed(t, DefaultMarketplace, []map[string]any{
		{
			"name":        "firebase",
			"version":     "1.0.3",
			"description": "the firebase plugin",
			"source": map[string]any{
				"type":   "archive",
				"url":    "https://example.invalid/firebase.zip",
				"sha256": "abc123",
			},
		},
	})

	entry, err := Lookup(DefaultMarketplace, "firebase")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if entry.Name != "firebase" || entry.Version != "1.0.3" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.URL != "https://example.invalid/firebase.zip" || entry.SHA256 != "abc123" {
		t.Errorf("entry source = %+v", entry)
	}
	if entry.Marketplace != DefaultMarketplace {
		t.Errorf("entry marketplace = %q, want %q", entry.Marketplace, DefaultMarketplace)
	}
}

// TestLookupFailsForAnUnknownPluginAndMarketplace pins the two refusals.
func TestLookupFailsForAnUnknownPluginAndMarketplace(t *testing.T) {
	scratchHome(t)
	writeFeed(t, DefaultMarketplace, []map[string]any{{"name": "firebase"}})

	if _, err := Lookup(DefaultMarketplace, "nope"); err == nil {
		t.Error("Lookup accepted a plugin the feed does not hold")
	}
	if _, err := Lookup("no-such-marketplace", "firebase"); err == nil {
		t.Error("Lookup accepted a marketplace that is not cached")
	}
}

// TestFetchUnpacksTheArchive pins that the archive is downloaded, its
// digest verified and its plugin's files unpacked.
func TestFetchUnpacksTheArchive(t *testing.T) {
	scratchHome(t)
	archive := zipBytes(t, map[string]string{
		"plugin.json":              `{"name":"firebase","version":"1.0.3"}`,
		"skills/firebase/SKILL.md": "# firebase",
		"agents/helper.md":         "# helper",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	writeFeed(t, DefaultMarketplace, []map[string]any{{
		"name":    "firebase",
		"version": "1.0.3",
		"source": map[string]any{
			"type":   "archive",
			"url":    srv.URL + "/firebase.zip",
			"sha256": digest(archive),
		},
	}})

	entry, err := Lookup(DefaultMarketplace, "firebase")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	root, cleanup, err := Fetch(entry)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(root, "plugin.json")); err != nil {
		t.Errorf("plugin.json missing from %s: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "firebase", "SKILL.md")); err != nil {
		t.Errorf("skill missing from %s: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "agents", "helper.md")); err != nil {
		t.Errorf("agent missing from %s: %v", root, err)
	}
}

// TestFetchRefusesAWrongDigest pins that the feed's digest is checked: an
// archive that does not match is not unpacked.
func TestFetchRefusesAWrongDigest(t *testing.T) {
	scratchHome(t)
	archive := zipBytes(t, map[string]string{"plugin.json": `{"name":"firebase"}`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	writeFeed(t, DefaultMarketplace, []map[string]any{{
		"name": "firebase",
		"source": map[string]any{
			"type":   "archive",
			"url":    srv.URL + "/firebase.zip",
			"sha256": digest([]byte("something else")),
		},
	}})

	entry, err := Lookup(DefaultMarketplace, "firebase")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if _, _, err := Fetch(entry); err == nil {
		t.Fatal("Fetch accepted an archive with the wrong digest")
	}
}

// TestFetchRefusesAnUnsafeArchivePath pins that an archive cannot write
// outside the directory it is unpacked into.
func TestFetchRefusesAnUnsafeArchivePath(t *testing.T) {
	scratchHome(t)
	archive := zipBytes(t, map[string]string{"../escape.txt": "nope"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	entry := Entry{
		Marketplace: DefaultMarketplace,
		Name:        "escape",
		URL:         srv.URL + "/escape.zip",
		SHA256:      digest(archive),
	}
	_, _, err := Fetch(entry)
	if err == nil {
		t.Fatal("Fetch accepted an archive with an escaping path")
	}
	if !strings.Contains(err.Error(), "unsafe path") {
		t.Errorf("error = %v, want it to name the unsafe path", err)
	}
}

// TestFetchRefusesAnEntryWithoutAnArchive pins the two refusals the feed's
// own shape allows: an entry with no archive and an entry with no digest.
func TestFetchRefusesAnEntryWithoutAnArchive(t *testing.T) {
	scratchHome(t)
	if _, _, err := Fetch(Entry{Name: "x"}); err == nil {
		t.Error("Fetch accepted an entry with no archive")
	}
	if _, _, err := Fetch(Entry{Name: "x", URL: "https://example.invalid/x.zip"}); err == nil {
		t.Error("Fetch accepted an entry with no digest")
	}
}
