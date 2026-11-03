// cache_test.go — the /changelog panel's fallback ladder.
//
// The reference fetches the published release notes and caches them at
// ~/.gemini/antigravity-cli/cache/CHANGELOG.md; when the network is not
// reachable it falls back to that copy, and when there is no copy either,
// to the release notes embedded in the binary
// (official-docs/cli/commands/changelog.md:11).
//
// The ladder is pinned here: the cached copy wins, an empty cache file is
// no cache, and a home without a cache leaves the document alone.

package changelog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/changelog"
)

// The cached document the fixtures write. Its version section is one the
// binary never carried, so a document that shows it can only come from the
// cache.
const cachedDoc = "# Antigravity CLI Changelog\n" +
	"\n" +
	"<!-- a comment -->\n" +
	"\n" +
	"The cached release notes.\n" +
	"\n" +
	"## 9.9.9\n" +
	"\n" +
	"- Cached bullet one.\n" +
	"- Cached bullet two.\n"

// writeCache points $ANTIGRAVITY_HOME at a fresh home and writes the
// cached release notes into it.
func writeCache(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", home)
}

// The cached copy is the first rung of the ladder: the panel shows it even
// when the reference's own release-notes file is reachable.
func TestDocument_CacheWins(t *testing.T) {
	writeCache(t, cachedDoc)
	doc := strings.Join(changelog.Document(120, 40), "\n")
	if !strings.Contains(doc, "Cached bullet one.") {
		t.Errorf("the document does not carry the cached release notes:\n%s", truncDoc(doc))
	}
	if !strings.Contains(doc, "## 9.9.9") {
		t.Errorf("the document does not carry the cached version header:\n%s", truncDoc(doc))
	}
}

// The cached copy is read from the CLI's own home: the path's shape is
// <home>/cache/CHANGELOG.md.
func TestDocument_CachePathShape(t *testing.T) {
	home := t.TempDir()
	// A cache file in the wrong place must not be picked up.
	wrong := filepath.Join(home, "CHANGELOG.md")
	if err := os.WriteFile(wrong, []byte(cachedDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", home)
	doc := strings.Join(changelog.Document(120, 40), "\n")
	if strings.Contains(doc, "Cached bullet one.") {
		t.Error("the document picked up a cache file outside the cache directory")
	}
}

// The static shape (a terminal of unknown size) is the cached copy's own
// transcription under the panel's title and tagline.
func TestDocument_CacheStaticShape(t *testing.T) {
	writeCache(t, cachedDoc)
	doc := changelog.Document(0, 0)
	if len(doc) < 4 {
		t.Fatalf("the document has %d rows, want at least 4", len(doc))
	}
	if doc[0] != changelog.TitleRow {
		t.Errorf("the first row = %q, want the title %q", doc[0], changelog.TitleRow)
	}
	if doc[1] != "" {
		t.Errorf("the second row = %q, want a blank", doc[1])
	}
	if doc[2] != changelog.Tagline {
		t.Errorf("the third row = %q, want the tagline %q", doc[2], changelog.Tagline)
	}
	want := "  \x1b[1m\x1b[38;5;27m## 9.9.9\x1b[0m"
	if !containsRow(doc, want) {
		t.Errorf("the document does not carry the header row %q:\n%q", want, doc)
	}
	wantBullet := "  \x1b[38;5;235m• Cached bullet one.\x1b[39m"
	if !containsRow(doc, wantBullet) {
		t.Errorf("the document does not carry the bullet row %q:\n%q", wantBullet, doc)
	}
}

// An empty cache file is no cache: the ladder falls through to the next
// rung instead of showing an empty document.
func TestDocument_EmptyCacheFallsThrough(t *testing.T) {
	writeCache(t, "   \n")
	doc := strings.Join(changelog.Document(120, 40), "\n")
	if strings.Contains(doc, "Cached bullet one.") {
		t.Error("the document shows an empty cache")
	}
	if len(doc) == 0 {
		t.Error("the document is empty")
	}
}

// A home without a cache leaves the document alone: the reference's own
// release-notes file is still the source when the clone runs from a
// checkout.
func TestDocument_NoCacheKeepsTheReferenceFile(t *testing.T) {
	src := changelog.Lines()
	if src == nil {
		t.Skip("the reference's release notes are not reachable from this working directory")
	}
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	if !sameRows(changelog.Document(0, 0), changelog.Styled(src)) {
		t.Error("the static document is not the styled source")
	}
	if !sameRows(changelog.Document(120, 40), changelog.Wrapped(src, 120)) {
		t.Error("the wrapped document is not the wrapped source")
	}
}

// containsRow reports whether a row set carries a row.
func containsRow(rows []string, want string) bool {
	for _, r := range rows {
		if r == want {
			return true
		}
	}
	return false
}

// truncDoc shortens a document for an error message.
func truncDoc(doc string) string {
	if len(doc) > 400 {
		return doc[:400]
	}
	return doc
}
