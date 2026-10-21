// slash_diff_trim_test.go — unit + integration tests for the
// /diff-viewer trailing-whitespace trim (AGY 1.2.16).
//
// Pure-function tests exercise trimDiffTrailing directly (no git
// dependency, run anywhere). The end-to-end test stages a real git repo
// with trailing-tab content and asserts that panelDiffInlineLines no
// longer surfaces trailing tabs in its inline diff body.
//
// The test names all start with "TestDiff" so the standard targeted
// regex `TestDiff|TestTrail|TestRender` (used by the cycle's quick
// smoke run) catches the whole set.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestDiffTrimTrailingTab removed: a line that ends with one or more
// tabs returns with those tabs gone, the leading marker byte intact.
func TestDiffTrimTrailingTab(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+hello\t", "+hello"},
		{"+hello\t\t", "+hello"},
		{"plain\t", "plain"},
		{"@@ -1,2 +1,2 @@\t", "@@ -1,2 +1,2 @@"},
	}
	for _, c := range cases {
		if got := trimDiffTrailing(c.in); got != c.want {
			t.Errorf("trimDiffTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiffTrimTrailingSpace removed: a line that ends with one or more
// spaces returns with those spaces gone, the leading marker byte intact.
func TestDiffTrimTrailingSpace(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+hello ", "+hello"},
		{"-world   ", "-world"},
		{" trailing space  ", " trailing space"},
		{"single", "single"},
	}
	for _, c := range cases {
		if got := trimDiffTrailing(c.in); got != c.want {
			t.Errorf("trimDiffTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiffTrimPreservesInternalWhitespace: the trim is restricted to the
// tail. Internal whitespace — multiple consecutive spaces, tabs between
// tokens, a space-indented hunk header — must pass through untouched.
func TestDiffTrimPreservesInternalWhitespace(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Multiple internal spaces preserved.
		{"+a   b", "+a   b"},
		// Tab between tokens preserved.
		{"+a\tb", "+a\tb"},
		// Trailing space only: trailing trimmed, everything before kept.
		{"+a   b   ", "+a   b"},
		// Leading space (a git context-line marker) preserved.
		{" context line", " context line"},
	}
	for _, c := range cases {
		if got := trimDiffTrailing(c.in); got != c.want {
			t.Errorf("trimDiffTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiffTrimBlankLinePreserved: an empty line stays empty, and a line
// that is purely whitespace collapses to empty (which the diff renderer
// paints as a blank row, not a row of markers).
func TestDiffTrimBlankLinePreserved(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{" ", ""},
		{"\t", ""},
		{" \t \t", ""},
		{"  keep me  ", "  keep me"},
	}
	for _, c := range cases {
		if got := trimDiffTrailing(c.in); got != c.want {
			t.Errorf("trimDiffTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiffTrimPlusMarkerNotTrimmed: the leading `+` must stay even when
// the content after it is entirely whitespace. Trailing tabs/spaces
// after the marker still go.
func TestDiffTrimPlusMarkerNotTrimmed(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+", "+"},
		{"+ ", "+"},
		{"+\t\t", "+"},
		{"+ \t \t", "+"},
		{"+hi\t", "+hi"},
		{"+ real content", "+ real content"},
	}
	for _, c := range cases {
		if got := trimDiffTrailing(c.in); got != c.want {
			t.Errorf("trimDiffTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiffTrimMinusMarkerNotTrimmed: identical property for `-` lines
// (a `-` line with nothing but trailing whitespace should render as a
// visible removal of blank content, not as a phantom blank).
func TestDiffTrimMinusMarkerNotTrimmed(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"-", "-"},
		{"- ", "-"},
		{"-\t\t", "-"},
		{"-row\t", "-row"},
	}
	for _, c := range cases {
		if got := trimDiffTrailing(c.in); got != c.want {
			t.Errorf("trimDiffTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiffTrimHunkHeaderPreserved: hunk headers like `@@ -1,2 +1,2 @@`
// are signalled by the leading `@@` and have no trailing whitespace;
// the trim must pass them through byte-identical so a future edit that
// depends on hunk-header byte content still works.
func TestDiffTrimHunkHeaderPreserved(t *testing.T) {
	hdr := "@@ -1,2 +1,2 @@ context"
	if got := trimDiffTrailing(hdr); got != hdr {
		t.Errorf("trimDiffTrailing(%q) = %q, want unchanged", hdr, got)
	}
}

// TestDiffTrimFileHeaderPreserved: `diff --git a/foo b/foo`,
// `index 1234..5678 100644`, `--- a/foo`, `+++ b/foo`. None of these
// carry trailing whitespace in `git --no-color` output and must be
// passed through.
func TestDiffTrimFileHeaderPreserved(t *testing.T) {
	cases := []string{
		"diff --git a/foo b/foo",
		"index 1234..5678 100644",
		"--- a/foo",
		"+++ b/foo",
		"new file mode 100644",
	}
	for _, in := range cases {
		if got := trimDiffTrailing(in); got != in {
			t.Errorf("trimDiffTrailing(%q) = %q, want unchanged", in, got)
		}
	}
}

// TestDiffTrimEndToEndRealRepo is the integration test: a tracked file
// with trailing tabs is edited to swap trailing-tab content for clean
// content, and the inline-diff subview's body is asserted to contain no
// trailing-tab characters in its rendered form. The file on disk still
// has whatever git committed (the clone never modifies the file).
//
// Runs only when git is on PATH; skipped otherwise so the targeted
// regex stays runnable in environments without git.
func TestDiffTrimEndToEndRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Commit a file with trailing tabs.
	git("init", "-q")
	write("tab.txt", "line without tabs\n")
	git("add", "-A")
	git("commit", "-qm", "init")
	// Dirty it: delete the clean line, add a line with trailing tabs.
	if err := os.WriteFile(filepath.Join(dir, "tab.txt"),
		[]byte("line without tabs\nline WITH TRAILING TAB\t\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	m.panel.cursor = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 1 || m.panel.diffViewPath != "tab.txt" {
		t.Fatalf("diffView=%d path=%q, want enter-on-file inline view",
			m.panel.diffView, m.panel.diffViewPath)
	}
	lines := m.panelDiffInlineLines()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// The body (rows after the title row) must contain the added line,
	// but the rendered string must not end with the trailing tab — only
	// with the marker `+`.
	const want = "  +line WITH TRAILING TAB"
	found := false
	for _, l := range plain {
		if strings.Contains(l, want) {
			found = true
			// And no trailing whitespace markers leaked past the trim.
			if strings.HasSuffix(l, "\t") || strings.HasSuffix(strings.TrimPrefix(l, "  "), " \t") {
				// Allow other content but fail if the trimmed line
				// ends in a tab or trailing space beyond the marker.
				t.Errorf("rendered diff line still ends in whitespace marker: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("no +line rendered in the trimmed body, lines=%q", plain)
	}
}
