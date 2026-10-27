// chroma_reference_test.go -- cycle 300 parity fence for the chroma
// dependency version.
//
// The reference (agy 1.4.7) vendors chroma v2.27.0 unmodified: all
// 353 embedded chroma files in its binary (279 XML lexers under the
// embed FS root "embedded/" and 74 XML styles at the FS root) are
// byte-identical to the upstream v2.27.0 tree -- verified by
// sha256 on 2026-10-25 (see
// docs/cycles/cycle-300-chroma-parity/deliverable.md).
//
// The clone pinned chroma v2.24.1, which ships 275 XML lexers. The
// four it was missing -- Arturo, Gemfile.lock, LilyPond, Templ --
// were therefore unhighlightable in the clone while the reference
// highlighted them. Cycle 300 bumps the dependency to v2.27.0 and
// this test is the fence that keeps it there.
//
// The test asserts the *language coverage* rather than the rendered
// bytes: the 30-language matrix in the cycle report showed that
// v2.24.1 and v2.27.0 produce byte-identical output for every
// common language (sha256 per language), so the version bump is
// invisible on the wire -- which is exactly why it needs a fence of
// its own instead of being caught by a rendering regression.
package mdrender

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
)

// referenceLexerAlias returns the alias the reference's chroma
// registers for the given XML lexer file base name, or "" if the file
// is not in the reference set.
func referenceLexerAlias(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join("testdata", "reference_lexers.txt")
	fh, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer fh.Close()

	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		if fields[0] == base {
			return fields[1]
		}
	}
	return ""
}

// TestChromaReference_LexerCoverage is the cycle-300 parity fence:
// every one of the 279 XML lexers the reference embeds must resolve
// in the clone's chroma too. A downgrade of the dependency (or an
// upstream removal) fails here.
func TestChromaReference_LexerCoverage(t *testing.T) {
	path := filepath.Join("testdata", "reference_lexers.txt")
	fh, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer fh.Close()

	total := 0
	missing := []string{}
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		base, alias := fields[0], fields[1]
		total++
		if lexers.Get(alias) == nil {
			missing = append(missing, base+" ("+alias+")")
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}

	// The reference embeds exactly 279 XML lexers. A different count
	// means the reference moved -- re-run the recovery, do not silently
	// accept a new baseline.
	if total != 279 {
		t.Errorf("reference lexer set has %d entries, expected 279 (agy 1.4.7, 2026-10-25)", total)
	}
	if len(missing) != 0 {
		t.Errorf("%d reference lexers unresolved by the clone's chroma: %v",
			len(missing), missing)
	}
}

// TestChromaReference_NewlyAvailableLanguages pins the four lexers
// that cycle 300's version bump brought in. They are the whole
// observable difference between chroma v2.24.1 and v2.27.0 in the
// reference's own file set, so they are the concrete payload of the
// bump.
func TestChromaReference_NewlyAvailableLanguages(t *testing.T) {
	for _, lang := range []string{"arturo", "gemfile-lock", "lilypond", "templ"} {
		if lexers.Get(lang) == nil {
			t.Errorf("chroma lexer %q unavailable -- the dependency is not v2.27.0", lang)
		}
	}
}

// TestChromaReference_AliasResolves is the small spot check that the
// alias recorded in the testdata file is the one chroma actually
// registers, for one lexer from each corner of the alphabet.
func TestChromaReference_AliasResolves(t *testing.T) {
	for _, base := range []string{"abap", "html", "templ", "yaml"} {
		alias := referenceLexerAlias(t, base)
		if alias == "" {
			t.Errorf("no testdata entry for %q", base)
			continue
		}
		if lexers.Get(alias) == nil {
			t.Errorf("alias %q (from %s) does not resolve", alias, base)
		}
	}
}
