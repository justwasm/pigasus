package diffaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStripANSI pins the 1.3.4 changelog line-33 regression fix: the
// /diff body must lose its CSI escape sequences so a clean repository
// configured with colour.ui = always still reports "No uncommitted
// changes".
func TestStripANSI(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"plain", "abc", "abc"},
		{"sgr-only", "\x1b[m\x1b[m", ""},
		{"single-sgr-around-text", "\x1b[31mfoo\x1b[m", "foo"},
		{"multi-param-sgr", "\x1b[38;5;208;48;5;52mwarn\x1b[0m", "warn"},
		{"questionmark-param", "\x1b[?25hshown\x1b[?25l", "shown"},
		{"mixed-newlines", "\x1b[m\n\x1b[m\n", "\n\n"},
		{"non-sgr-csi", "\x1b[2Jcursor home\x1b[H", "cursor home"},
		{"non-letter-final-not-matched", "\x1b[mz", "z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripANSI(tc.in); got != tc.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDetectVCS pins the three VCS markers the reference supports
// (git / hg / jj) and the walk up the directory chain.
func TestDetectVCS(t *testing.T) {
	for _, tc := range []struct {
		marker, want string
	}{
		{".git", "git"},
		{".hg", "hg"},
		{".jj", "jj"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, tc.marker), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := DetectVCS(dir); got != tc.want {
				t.Errorf("DetectVCS(%q dir) = %q, want %q", tc.marker, got, tc.want)
			}
			// A nested directory resolves to the same repository.
			sub := filepath.Join(dir, "sub")
			if err := os.Mkdir(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if got := DetectVCS(sub); got != tc.want {
				t.Errorf("DetectVCS(nested) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetectVCSNone pins the empty result outside any repository.
func TestDetectVCSNone(t *testing.T) {
	if got := DetectVCS(t.TempDir()); got != "" {
		t.Errorf("DetectVCS(no repo) = %q, want %q", got, "")
	}
}

// TestPathExists pins the os.Stat-backed existence check.
func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	if !PathExists(dir) {
		t.Errorf("PathExists(%q) = false, want true", dir)
	}
	if PathExists(filepath.Join(dir, "nope")) {
		t.Errorf("PathExists(missing) = true, want false")
	}
}

// TestVCSDiffUnsupported pins the error for an unknown VCS name.
func TestVCSDiffUnsupported(t *testing.T) {
	out, err := VCSDiff("svn", t.TempDir())
	if err == nil {
		t.Fatalf("VCSDiff(svn) = %q, want an error", out)
	}
	if !strings.Contains(err.Error(), "unsupported VCS") {
		t.Errorf("VCSDiff(svn) error = %q, want it to name the unsupported VCS", err)
	}
}

// TestRenderDiffFooter pins the recovered `  [1-N of N]  (end of diff)`
// trailer and the whitespace-only empty case.
func TestRenderDiffFooter(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"empty", "", "No uncommitted changes"},
		{"whitespace-only", "  \n\t\n", "No uncommitted changes"},
		{"single-line", "one", "one\n  [1-1 of 1]  (end of diff)"},
		{"multi-line", "one\ntwo\nthree", "one\ntwo\nthree\n  [1-3 of 3]  (end of diff)"},
		{"trailing-newline-trimmed", "one\n", "one\n  [1-1 of 1]  (end of diff)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderDiffFooter(tc.in); got != tc.want {
				t.Errorf("RenderDiffFooter(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFilesPreambleHoverMarkers pins the AGY 1.3.3 changelog
// line-60 invariant in the /diff preamble: every emitted link carries
// a `?h=<index>` hover-card disambiguator, and the indices are
// distinct across the link list (0, 1, 2, …) so that even two links
// pointing at the same file and line in the same diff still resolve
// to distinct hover cards.
//
// The diff body below touches three files: a.txt, b.txt and a.txt
// again. The third hunk references a.txt at the same line the first
// one did, so the two emitted links to a.txt are *textually
// identical* except for the marker. That is the exact case the
// changelog is fixing.
func TestFilesPreambleHoverMarkers(t *testing.T) {
	body := "diff --git a/a.txt b/a.txt\n" +
		"index 0000000..1111111 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"-one\n" +
		"+two\n" +
		"diff --git a/b.txt b/b.txt\n" +
		"index 0000000..2222222 100644\n" +
		"--- a/b.txt\n" +
		"+++ b/b.txt\n" +
		"@@ -5 +5 @@\n" +
		"-foo\n" +
		"+bar\n" +
		"diff --git a/a.txt b/a.txt\n" +
		"index 1111111..3333333 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"-two\n" +
		"+three\n"
	got := FilesPreamble(body)
	if got == "" {
		t.Fatalf("FilesPreamble = empty, want non-empty preamble")
	}
	// The three emitted links must each carry a distinct marker.
	for _, want := range []string{"?h=0", "?h=1", "?h=2"} {
		if !strings.Contains(got, want) {
			t.Errorf("preamble missing %q:\n%s", want, got)
		}
	}
	// The two a.txt links must each be present (changelog case).
	if c := strings.Count(got, "a.txt%23L1"); c < 2 {
		t.Errorf("preamble has %d a.txt%%23L1 references, want >=2:\n%s", c, got)
	}
	// The preamble still starts with the recovered `Files: ` prefix
	// (other tests / contracts rely on it).
	if !strings.HasPrefix(got, "Files: ") {
		t.Errorf("preamble missing `Files: ` prefix: %q", got)
	}
	// The links are percent-encoded (AGY 1.3.3 changelog line 59
	// is not regressed by the cycle-60 marker addition).
	if strings.Contains(got, "a.txt#L1") || strings.Contains(got, "b.txt#L5") {
		t.Errorf("preamble leaked raw #L<n> fragment:\n%s", got)
	}
}

// TestFilesPreambleSingleLinkMarker pins the "single link gets
// marker index 0" half of the changelog line-60 contract: a diff
// touching one file produces a preamble whose single emitted link
// carries `?h=0`, not the bare `path%23LN` form.
func TestFilesPreambleSingleLinkMarker(t *testing.T) {
	body := "diff --git a/a.txt b/a.txt\n" +
		"index 0000000..1111111 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -3 +3 @@\n" +
		"-old\n" +
		"+new\n"
	got := FilesPreamble(body)
	if !strings.Contains(got, "?h=0") {
		t.Errorf("single-link preamble missing ?h=0 marker:\n%s", got)
	}
	// No higher indices in a one-file diff.
	if strings.Contains(got, "?h=1") {
		t.Errorf("single-link preamble leaked ?h=1:\n%s", got)
	}
}

// TestFilesPreambleEmpty pins the empty result for a body with no
// file path in it.
func TestFilesPreambleEmpty(t *testing.T) {
	if got := FilesPreamble(""); got != "" {
		t.Errorf("FilesPreamble(empty) = %q, want %q", got, "")
	}
	if got := FilesPreamble("index 0000000..1111111 100644\n"); got != "" {
		t.Errorf("FilesPreamble(no file) = %q, want %q", got, "")
	}
}

func TestUnifiedDiff(t *testing.T) {
	oldT := "hello\nworld\n"
	newT := "hello\nearth\n"
	diff := UnifiedDiff("a.txt", "b.txt", oldT, newT)
	if !strings.Contains(diff, "--- a.txt") || !strings.Contains(diff, "+++ b.txt") {
		t.Fatalf("unexpected diff header: %s", diff)
	}
	if !strings.Contains(diff, "-world") || !strings.Contains(diff, "+earth") {
		t.Fatalf("missing expected change: %s", diff)
	}
	if same := UnifiedDiff("a.txt", "b.txt", oldT, oldT); same != "" {
		t.Fatalf("expected empty diff for identical inputs, got %q", same)
	}
}
