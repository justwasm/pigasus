// helpers_test.go — the shared helpers of the /diff panel's tests.
//
// The panel is tested through its own surface: the rows it draws, the
// keys it owns and the state it keeps. The helpers here build a panel
// whose seams read from the test itself, so a test can move the
// workspace, change the conversation's turn count and observe the way
// out.

package diffpanel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

// testPanel is the /diff panel under test: the panel plus the facts
// its seams read.
type testPanel struct {
	*Panel

	// cwd is the workspace the panel reads the working copy from.
	cwd string
	// turns is the conversation's user-turn count.
	turns int
	// closed records that the panel's way out was taken.
	closed bool
	// width and height are the terminal's size in cells.
	width, height int
}

// newTestPanel builds the panel with the test's seams.
//
// The terminal is 0×0 — the size a panel sees when it is built outside
// a TUI — so the visible window stays closed and every list is drawn
// whole. The tests that exercise the window set the size themselves.
func newTestPanel(t *testing.T, cwd string) *testPanel {
	t.Helper()
	tp := &testPanel{cwd: cwd}
	tp.Panel = New(Seams{
		Cwd:   func() string { return tp.cwd },
		Size:  func() (int, int) { return tp.width, tp.height },
		Turns: func() int { return tp.turns },
		Close: func() tea.Cmd { tp.closed = true; return nil },
	})
	return tp
}

// stripSGR removes every ANSI SGR sequence, so a row's plain text can
// be compared.
func stripSGR(s string) string {
	return termcolor.StripSGR(s)
}

// plainLines renders the panel and strips the SGR from every row.
func plainLines(p *testPanel) []string {
	raw := p.Lines()
	out := make([]string, len(raw))
	for i, l := range raw {
		out[i] = stripSGR(l)
	}
	return out
}

// git runs a git subcommand in dir with a fixed identity.
func git(t *testing.T, dir string, args ...string) {
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

// initDiffFoldingRepo stages a single-file git repo with two
// non-adjacent change blocks so `git diff` emits exactly two
// `@@ ... @@` hunks. The repo is rooted at dir; the file is "f.txt"
// and the delta is:
//   - L5–L7 (3 lines) changed: each becomes +CHANGED-A1 / +CHANGED-A2 / +CHANGED-A3
//   - L25–L27 (3 lines) changed: each becomes +CHANGED-B1 / +CHANGED-B2 / +CHANGED-B3
//
// The hunk separation (15 unchanged lines) keeps git from merging
// them into one big hunk via its default 3-context heuristic.
func initDiffFoldingRepo(t *testing.T, dir string) {
	t.Helper()
	// Construct a 30-line file with two change blocks at known
	// locations. Block A occupies lines 5..7; block B occupies
	// lines 25..27. The middle 17 lines (8..24) are unchanged
	// context, which forces git to emit two separate @@ hunks.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "ctx"
	}
	for i := 4; i <= 6; i++ {
		lines[i] = "oldA"
	}
	for i := 24; i <= 26; i++ {
		lines[i] = "oldB"
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	// Now mutate the same lines so the diff has two hunks in the
	// modified file (not added/removed whole blocks).
	lines[4] = "CHANGED-A1"
	lines[5] = "CHANGED-A2"
	lines[6] = "CHANGED-A3"
	lines[24] = "CHANGED-B1"
	lines[25] = "CHANGED-B2"
	lines[26] = "CHANGED-B3"
	body2 := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initCycle226GitRepo is the shared helper that creates a one-commit
// git repo at dir with a tracked file `f.txt` and the test fixture
// "modified f.txt + added g.txt + untracked h.txt" applied on top.
func initCycle226GitRepo(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q")
	write("f.txt", "old\n")
	write("g.txt", "new\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	// Now dirty the working copy.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("old\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("new\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("h.txt", "u\n")
}

// openFoldingDiffSubview sets up the panel with cwd=dir, opens it on
// tab 0 (All Changes), moves the cursor to row 0 (the single tracked
// file "f.txt") and presses Enter — the standard hand-off from the file
// list to the inline-diff subview. The returned panel is ready for the
// f/j/k bindings.
func openFoldingDiffSubview(t *testing.T, dir string) *testPanel {
	t.Helper()
	p := newTestPanel(t, dir)
	p.tab = 0
	p.cursor = 0
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if p.view != 1 {
		t.Fatalf("view = %d, want 1 (file inline)", p.view)
	}
	return p
}

// indexOfPrefix returns the first index where plain[i] starts with
// prefix — skipping the leading two-space indent + " > " cursor
// marker — or -1 if no row matches. Used by the tests to locate the
// @@ header / specific body lines without depending on row indices
// the renderer is free to rearrange.
func indexOfPrefix(plain []string, prefix string) int {
	for i, l := range plain {
		if rowStartsWith(l, prefix) {
			return i
		}
	}
	return -1
}

// diffInlineBodyRows returns the rendered rows minus the title row,
// leading blank row, trailing blank row, and legend row — the rows
// whose cursor index is the inline row.
//
// Each body row has a leading 2-space indent ("  " before @@/+/-/context)
// plus, on the cursor row, an extra " > " blue marker prefix. The
// helper strips neither — tests inspect prefix/content directly
// and account for the indent + cursor row explicitly via the
// rowStartsWith helper below.
//
// Layout: the inline view emits [title, blank, body..., blank,
// legend]. body is rows[2..N-3] — the slice between the leading
// blank and the trailing blank. The trailing blank + legend pair
// is dropped by walking back from the end to find the legend (the
// last non-empty row).
func diffInlineBodyRows(plain []string) []string {
	if len(plain) < 5 {
		return nil
	}
	// Find the legend: the last non-empty row.
	legendIdx := -1
	for i := len(plain) - 1; i >= 0; i-- {
		if plain[i] != "" {
			legendIdx = i
			break
		}
	}
	if legendIdx < 0 || legendIdx < 3 {
		return nil
	}
	// Everything between row 2 and row legendIdx-2 is body content:
	//   [title=0, blank=1, body=2..legendIdx-2, blank=legendIdx-1, legend=legendIdx]
	bodyStart := 2
	bodyEnd := legendIdx - 1
	if bodyStart >= bodyEnd {
		return nil
	}
	return plain[bodyStart:bodyEnd]
}

// rowStartsWith reports whether s has the given content prefix
// once the leading two-space indent (and any " > " cursor marker)
// have been skipped. The renderer's standard indent is two spaces,
// and the cursor-marked row adds a third space + "> ", producing
// rows like " >   diff --git..."; this helper treats both forms
// identically so tests don't have to special-case the cursor row.
func rowStartsWith(s, prefix string) bool {
	return strings.HasPrefix(stripRowPrefix(s), prefix)
}

// stripRowPrefix returns s with the leading "  " (renderer
// standard indent) and any " > " cursor marker stripped, leaving
// the content tail. rowStartsWith is built on top of this helper so
// the prefix-stripping logic only lives in one place.
func stripRowPrefix(s string) string {
	strip := s
	// Skip leading spaces.
	for len(strip) > 0 && strip[0] == ' ' {
		strip = strip[1:]
	}
	// Skip the cursor marker, if present.
	if strings.HasPrefix(strip, "> ") {
		strip = strip[2:]
	}
	// Re-skip any spaces that followed the cursor marker — the
	// cursor-row prefix is " > " + "  " + content, so once the
	// marker is gone there are still two spaces of indent left.
	for len(strip) > 0 && strip[0] == ' ' {
		strip = strip[1:]
	}
	return strip
}

// pressF sends a bare 'f' key through the panel's key handler and
// asserts the panel still owns the key (returns true). The diff
// subview's 'f' binding returns true on both the toggle branch and
// the fall-through no-op branch, so this is the same contract as every
// other panel-owning letter.
func pressF(t *testing.T, p *testPanel) {
	t.Helper()
	if handled, _ := p.HandleKey(tea.Key{Text: "f", Code: 'f'}); !handled {
		t.Fatal("f not handled")
	}
}

// cursorOnFirstHunkHeader advances the inline row cursor to the
// first "@@" row in the body. Assumes lines have just been
// rendered (so the cursor mapping is current). Reports the
// obvious failure when the body has no @@ rows.
func cursorOnFirstHunkHeader(t *testing.T, p *testPanel, lines []string) {
	t.Helper()
	body := diffInlineBodyRows(lines)
	idx := indexOfPrefix(body, "@@")
	if idx < 0 {
		t.Fatalf("no @@ header in body: %q", body)
	}
	p.inlineRow = idx
}

// stripInlineLines renders the inline diff and strips SGR. A
// t.Helper so failures include the caller's line.
func stripInlineLines(t *testing.T, p *testPanel) []string {
	t.Helper()
	raw := p.inlineLines()
	out := make([]string, len(raw))
	for i, l := range raw {
		out[i] = stripSGR(l)
	}
	return out
}

// countPrefix returns the number of rows in lines that start with
// prefix — skipping the leading two-space indent + " > " cursor
// marker. Used by the tests to count "@@" headers regardless of
// their internal variation (different hunk ranges produce
// different "@@ -A,B +C,D @@" forms).
func countPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if rowStartsWith(l, prefix) {
			n++
		}
	}
	return n
}

// anyContains reports whether at least one row of lines contains
// substr. A small helper for the positive/negative change-line
// presence checks.
func anyContains(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}
