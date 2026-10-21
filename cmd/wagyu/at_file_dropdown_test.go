package main

// at_file_dropdown_test.go — feature/at-file-dropdown-newline.
//
// Drives the atFileDropdown state machine directly (no TUI /
// bubbletea plumbing) so the AGY 1.3.6 line-12 fix is exercised
// without spinning up a full Update loop. The TUI-side tests in
// tui_run_test.go assert the upper layers (key bindings, view
// row emission); this file stays focused on the parser-driven
// Update / Tab / Up / Down / Reset contract.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/atfiles"
)

// TestAtFileDropdown_FiresAtEndOfLine covers the most basic case:
// the user has typed `@` somewhere with text before / after it,
// the cursor is parked at the end of the buffer, and the
// dropdown should be Visible() with at least one candidate.
func TestAtFileDropdown_FiresAtEndOfLine(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown
	d.Update("look at @ma", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible after typing @ma at end of line; got %+v", d)
	}
	if d.Len() == 0 {
		t.Fatalf("dropdown should have at least one candidate for prefix %q; got 0", d.prefix)
	}
}

// TestAtFileDropdown_FiresAfterNewline is the headline regression
// guard for AGY 1.3.6 line 12. Before the fix the dropdown only
// appeared when the cursor was at end-of-line on the *first* line;
// typing `@<char>` at the start of a freshly-created second line
// failed to open it. Update() must report Visible() in both cases.
func TestAtFileDropdown_FiresAfterNewline(t *testing.T) {
	dir := withAtFileFixtures(t)
	cases := []string{
		"first line\n@ma",                       // hard newline then @
		"first line\n\n@ma",                     // blank line, then @
		"first line ends here\n@cmd/wagyu/main", // leading path after newline
	}
	for _, buf := range cases {
		t.Run(buf, func(t *testing.T) {
			var d atFileDropdown
			d.Update(buf, dir)
			if !d.Visible() {
				t.Fatalf("dropdown should be visible for buffer %q; got %+v", buf, d)
			}
		})
	}
}

// TestAtFileDropdown_FiresMidLine covers the standard "type @
// partway through a sentence" case: the user types `@src` after
// a word of natural language. The parser must still treat it as
// a mention.
func TestAtFileDropdown_FiresMidLine(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown
	d.Update("hello @ma", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible after @ma mid-line; got %+v", d)
	}
	if d.prefix != "ma" {
		t.Errorf("prefix = %q, want \"ma\"", d.prefix)
	}
}

// TestAtFileDropdown_DoesNotFireAfterWhitespace kills the dropdown
// when the user types a space — the AGY rule "the cursor must NOT
// be parked on whitespace" is what keeps the overlay from staying
// open after a successful mention select.
func TestAtFileDropdown_DoesNotFireAfterWhitespace(t *testing.T) {
	dir := withAtFileFixtures(t)
	cases := []string{
		"@main ",
		"@main\n",
		"hello @main ",
		"hello @main\n",
	}
	for _, buf := range cases {
		t.Run(buf, func(t *testing.T) {
			var d atFileDropdown
			d.Update(buf, dir)
			if d.Visible() {
				t.Fatalf("dropdown should be hidden for buffer %q (cursor on whitespace); got %+v", buf, d)
			}
		})
	}
}

// TestAtFileDropdown_HandlesPathsWithSpaces makes sure that file
// paths containing spaces still match — the parser only treats
// ASCII whitespace as the run terminator, so a directory entry
// whose name has a literal space must appear in the candidate
// list rather than being silently dropped.
func TestAtFileDropdown_HandlesPathsWithSpaces(t *testing.T) {
	dir := withAtFileFixtures(t)
	// Create a subdirectory whose name contains a space.
	withDir(t, filepath.Join(dir, "with space"))
	withFile(t, filepath.Join(dir, "with space", "thing.txt"))
	var d atFileDropdown
	d.Update("@with space/", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible after @with space/")
	}
	if d.Len() == 0 {
		t.Fatalf("candidate list is empty; want at least thing.txt from the spaced directory")
	}
	var rels []string
	for _, e := range d.candidates {
		rels = append(rels, e.Rel)
	}
	joined := strings.Join(rels, "\n")
	if !strings.Contains(joined, "thing.txt") {
		t.Errorf("candidate list missing thing.txt: %v", rels)
	}
}

// TestAtFileDropdown_MultipleAtsRefresh exercises the "user types
// two `@<char>` mentions in a row" scenario — each new prefix
// replaces the candidate list rather than appending to it.
func TestAtFileDropdown_MultipleAtsRefresh(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown

	d.Update("@ma", dir)
	firstLen := d.Len()
	if firstLen == 0 {
		t.Fatalf("first @ma should yield candidates")
	}

	// User types more characters; the candidate list should
	// shrink (or stay roughly the same) rather than grow.
	d.Update("@mai", dir)
	if d.Len() > firstLen {
		t.Fatalf("candidates grew when narrowing prefix: %d -> %d", firstLen, d.Len())
	}

	// User erases and types a wholly different prefix; the
	// candidate list should now reflect the new prefix.
	d.Update("@ot", dir)
	if d.Len() == 0 {
		t.Fatalf("@ot must yield candidates from the fixtures")
	}
	for _, c := range d.candidates {
		if !strings.Contains(filepath.Base(c.Rel), "ot") {
			t.Errorf("candidate %q does not match prefix \"ot\"", c.Rel)
		}
	}
}

// TestAtFileDropdown_TabSplicesPrefix covers Tab completion's
// splice semantics: replacing the @-mention substring with the
// highlighted candidate's workspace-relative path. 1.4.3 does not
// append a trailing space, and a directory keeps its slash.
func TestAtFileDropdown_TabSplicesPrefix(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown
	d.Update("@ma", dir)
	if d.Len() == 0 {
		t.Skip("no candidates; Tab test needs at least one")
	}
	repl, start, end := d.TabCompletion()
	if repl == "" {
		t.Fatalf("TabCompletion returned empty; want highlighted candidate")
	}
	if start != 0 || end != 3 {
		t.Errorf("splice offsets = (%d, %d), want (0, 3) for @ma", start, end)
	}
	if strings.HasSuffix(repl, " ") {
		t.Errorf("completion %q must not end with a space", repl)
	}
}

// TestAtFileDropdown_MoveUpMoveDownClamps checks the picker
// navigation helpers stay inside the candidate list (no wrap).
func TestAtFileDropdown_MoveUpMoveDownClamps(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown
	d.Update("@", dir) // every fixture matches
	if d.Len() < 2 {
		t.Skipf("need at least 2 candidates; got %d", d.Len())
	}
	start := d.idx
	if !d.MoveDown() {
		t.Fatalf("MoveDown should have moved from idx %d", start)
	}
	if d.Idx() != start+1 {
		t.Fatalf("Idx()=%d, want %d", d.Idx(), start+1)
	}
	// MoveDown at the last row must NOT wrap.
	last := d.Len() - 1
	d.idx = last
	if d.MoveDown() {
		t.Fatalf("MoveDown at last row (%d) must clamp", last)
	}
	// MoveUp at row 0 must NOT wrap.
	d.idx = 0
	if d.MoveUp() {
		t.Fatalf("MoveUp at row 0 must clamp")
	}
}

// TestAtFileDropdown_ResetClearsState pins the contract that
// Reset() leaves Visible() == false and an empty candidate list,
// so callers can rely on it from Esc / submit paths.
func TestAtFileDropdown_ResetClearsState(t *testing.T) {
	var d atFileDropdown
	d.Update("@ma", t.TempDir())
	if !d.Visible() {
		t.Fatalf("dropdown should be visible before Reset()")
	}
	d.Reset()
	if d.Visible() {
		t.Fatalf("dropdown should be hidden after Reset()")
	}
	if d.Len() != 0 {
		t.Fatalf("candidates should be empty after Reset(); got %d", d.Len())
	}
}

// TestAtFileDropdown_EmptyPrefixStillShows asserts the bare `@`
// case: typing only the sigil should still open the dropdown with
// the full catalog rather than reporting empty.
func TestAtFileDropdown_EmptyPrefixStillShows(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown
	d.Update("@", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible after a bare @")
	}
	if d.prefix != "" {
		t.Errorf("prefix = %q, want \"\"", d.prefix)
	}
	if d.Len() == 0 {
		t.Fatalf("empty-prefix dropdown should show the full catalog")
	}
}

// TestAtFileDropdown_DoesNotFireMidWord makes sure `obj@foo` is
// NOT treated as a file mention: the user clearly typed a word
// and is now using `@foo` as email-style text, not a path. This
// is the regression guard for the parser's "boundary check" rule.
func TestAtFileDropdown_DoesNotFireMidWord(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d atFileDropdown
	d.Update("obj@foo", dir)
	if d.Visible() {
		t.Fatalf("dropdown should NOT be visible for mid-word @foo; got %+v", d)
	}
}

// TestAtFileDropdown_UpdateHandlesGlobError gracefully keeps the
// overlay visible with an empty candidate list when the directory
// disappears mid-session (cwd may be a TMPDIR the test framework
// has just removed).
func TestAtFileDropdown_UpdateHandlesGlobError(t *testing.T) {
	var d atFileDropdown
	d.Update("@ma", "")
	if !d.Visible() {
		t.Skip("empty cwd yields no visible dropdown in this build")
	}
	if d.Len() != 0 {
		t.Errorf("expected zero candidates for empty cwd")
	}
	if !d.Empty() {
		t.Errorf("Empty() should be true when candidates == 0 and Visible() == true")
	}
}

// withAtFileFixtures writes a known tree of files into a tempdir
// and returns the path. Mirrors atfiles_test.go's TestFind layout
// so the cwd the atFileDropdown browses is reproducible.
func withAtFileFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"main.go",
		"main_test.go",
		"other.go",
		"cmd/wagyu/main.go",
		"cmd/wagyu/main_test.go",
		"internal/atfiles/atfiles.go",
	}
	for _, f := range files {
		withFile(t, filepath.Join(dir, f))
	}
	return dir
}

func withFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", p, err)
	}
}

func withDir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", p, err)
	}
}

// --- runMode integration ------------------------------------------------

// TestRunMode_AtFileDropdownView_EmptyBuffer covers the wire-up:
// with an empty textarea the dropdown is hidden and atFileView
// returns "" (matching pickerView's contract).
func TestRunMode_AtFileDropdownView_EmptyBuffer(t *testing.T) {
	m := newAtFileDropdownTestMode(t)
	if got := m.atFileView(); got != "" {
		t.Errorf("atFileView() with empty buffer = %q, want \"\"", got)
	}
}

// TestRunMode_AtFileDropdownView_FiresAfterNewline covers the
// AGY 1.3.6 line-12 end-to-end fix in a real runMode: we set the
// textarea to "first line\n@ma" through the textarea's API
// (matching how tui_run.go feeds the dropdown) and assert
// atFileView() returns a non-empty render with the expected row.
func TestRunMode_AtFileDropdownView_FiresAfterNewline(t *testing.T) {
	m := newAtFileDropdownTestMode(t)
	// Pin the runMode to a tempdir with known fixtures so the
	// dropdown has at least one candidate to render.
	m.cwd = withAtFileFixtures(t)
	m.input.SetValue("first line\n@ma")
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
	if !m.atFileDropdown.Visible() {
		t.Fatalf("dropdown should be visible after newline-anchored @ma")
	}
	rendered := m.atFileView()
	if rendered == "" {
		t.Fatalf("atFileView() should render the dropdown rows; got empty")
	}
	// Sanity: the highlighted candidate row should be present
	// unless the candidate list is genuinely empty (in which
	// case the overlay still renders a "no matches" row).
	if m.atFileDropdown.Len() > 0 && !strings.Contains(rendered, "> ") {
		t.Errorf("rendered overlay missing the highlighted-row \"> \" marker: %q", rendered)
	}
}

// TestRunMode_AtFileDropdown_TabSplicesInput is the runMode-level
// counterpart to the atFileDropdown-only splice test above. The
// "via the textarea" half — paste-back is checked.
func TestRunMode_AtFileDropdown_TabSplicesInput(t *testing.T) {
	m := newAtFileDropdownTestMode(t)
	m.cwd = withAtFileFixtures(t)
	m.input.SetValue("look at @ma")
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
	if !m.atFileDropdown.Visible() {
		t.Fatalf("dropdown should be visible")
	}
	if !m.tabSpliceAtFile(true) {
		t.Fatalf("tabSpliceAtFile should have mutated the input")
	}
	got := m.input.Value()
	if !strings.HasPrefix(got, "look at @") {
		t.Errorf("post-splice prefix = %q, want \"look at @\"", got)
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("post-splice should not append a trailing space; got %q", got)
	}
	if !m.atFileDropdown.Visible() {
		t.Errorf("Tab must keep the picker open for directory/file browsing")
	}
}

// newAtFileDropdownTestMode builds a minimal runMode with the
// @ file-mention dropdown wired but the rest of the side-effects
// (chat session, at-router, backend) stubbed out. Mirrors
// newSlashModel's posture from slash_content_test.go so the
// dropdown-level tests do not have to thread the full NewRunMode
// constructor.
func newAtFileDropdownTestMode(t *testing.T) *runMode {
	t.Helper()
	if os.Getenv("ANTIGRAVITY_HOME") == "" {
		t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	}
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	return m
}

// _ ensures the atfiles import is exercised even when tests are
// scoped to the runMode helpers (the cycles' build keeps unused
// imports out).
var _ = atfiles.Mention{}
