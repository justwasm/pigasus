// panel_test.go — the dropdown's behaviour.
//
// The dropdown is a panel; the panel's own tests drive the panel
// directly. The host's wiring is pinned in internal/cliapp.

package mentionpicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// harness is a panel over a buffer and a workspace root.
//
// The buffer is a field so the SetBuffer seam can write back into it —
// the completion splices into the composer's text — and so a test can
// type into the composer the way the host does: one keystroke, one
// re-derivation.
type harness struct {
	p     *Panel
	buf   string
	dir   string
	width int
}

// newHarness builds the harness over a workspace root.
func newHarness(t *testing.T, dir string) *harness {
	t.Helper()
	h := &harness{dir: dir, width: 120}
	h.p = New(Seams{
		Buffer:    func() string { return h.buf },
		SetBuffer: func(s string) { h.buf = s },
		Cwd:       func() string { return h.dir },
		Width:     func() int { return h.width },
	})
	return h
}

// type_ replaces the composer's text and re-derives the dropdown.
func (h *harness) type_(buf string) {
	h.buf = buf
	h.p.Update()
}

// TestPanel_FiresAtEndOfLine covers the dropdown appearing when the
// user types "@<char>" at the END of a line.
func TestPanel_FiresAtEndOfLine(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("look at @ma")
	if !h.p.Visible() {
		t.Fatal("the dropdown should be visible after typing @ma at the end of the line")
	}
	if h.p.Len() == 0 {
		t.Fatalf("the dropdown should have at least one candidate for prefix %q; got 0", h.p.Prefix())
	}
}

// TestPanel_FiresAfterNewline is the AGY 1.3.6 fix: the dropdown shows
// just like at the end of a line.
func TestPanel_FiresAfterNewline(t *testing.T) {
	dir := withAtFileFixtures(t)
	cases := []string{
		"first line\n@ma",
		"first line\n\n@ma",
		"first line ends here\n@cmd/wagyu/main",
	}
	for _, buf := range cases {
		t.Run(buf, func(t *testing.T) {
			h := newHarness(t, dir)
			h.type_(buf)
			if !h.p.Visible() {
				t.Fatalf("the dropdown should be visible for buffer %q", buf)
			}
		})
	}
}

// TestPanel_FiresMidLine covers the dropdown appearing in the middle
// of a line.
func TestPanel_FiresMidLine(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("hello @ma")
	if !h.p.Visible() {
		t.Fatal("the dropdown should be visible for @ mid-line")
	}
	if h.p.Prefix() != "ma" {
		t.Errorf("prefix = %q, want \"ma\"", h.p.Prefix())
	}
}

// TestPanel_NoFireInsideWords covers the mid-word "@" being refused.
func TestPanel_NoFireInsideWords(t *testing.T) {
	dir := withAtFileFixtures(t)
	cases := []string{
		"foo@bar",
		"alice@example.com",
		"/path/to@something",
	}
	for _, buf := range cases {
		t.Run(buf, func(t *testing.T) {
			h := newHarness(t, dir)
			h.type_(buf)
			if h.p.Visible() {
				t.Fatalf("the dropdown should NOT fire inside the word %q", buf)
			}
		})
	}
}

// TestPanel_SpaceInDirectoryName covers a directory whose name holds
// a space.
func TestPanel_SpaceInDirectoryName(t *testing.T) {
	dir := withAtFileFixtures(t)
	withDir(t, filepath.Join(dir, "with space"))
	withFile(t, filepath.Join(dir, "with space", "nested.txt"))
	h := newHarness(t, dir)
	h.type_("@with space/")
	if !h.p.Visible() {
		t.Fatal("the dropdown should be visible for the space-bearing directory")
	}
	if h.p.Len() == 0 {
		t.Fatal("expected a candidate inside the space-bearing directory")
	}
	found := false
	for _, e := range h.p.candidates {
		if strings.Contains(e.Rel, "nested.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("the candidate list is missing nested.txt: %+v", h.p.candidates)
	}
}

// TestPanel_CandidateNarrowing covers the candidate list shrinking as
// the prefix grows — the workspace is scanned once and re-filtered on
// every keystroke.
func TestPanel_CandidateNarrowing(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("@ma")
	firstLen := h.p.Len()
	if firstLen == 0 {
		t.Fatal("precondition: expected candidates for @ma")
	}
	h.type_("@mai")
	if h.p.Len() > firstLen {
		t.Fatalf("the candidates grew when narrowing the prefix: %d -> %d", firstLen, h.p.Len())
	}
	h.type_("@ot")
	if h.p.Len() == 0 {
		t.Fatal("expected a candidate for @ot")
	}
	for _, c := range h.p.candidates {
		if !strings.HasPrefix(filepath.Base(c.Path), "ot") && !strings.HasPrefix(c.Rel, "ot") {
			t.Errorf("the candidate %q does not match the prefix ot", c.Path)
		}
	}
}

// TestPanel_CompleteValues covers the span the completion replaces.
func TestPanel_CompleteValues(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("@ma")
	if h.p.Len() == 0 {
		t.Fatal("precondition: expected candidates for @ma")
	}
	repl, start, end := h.p.tabCompletion()
	if repl == "" {
		t.Fatal("the completion is empty")
	}
	if start != 0 || end != 3 {
		t.Errorf("span = (%d, %d), want (0, 3) for the input @ma", start, end)
	}
}

// TestPanel_Navigation covers the highlight moving and clamping at
// both ends.
func TestPanel_Navigation(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("@")
	if h.p.Len() < 2 {
		t.Skipf("need at least 2 candidates; got %d", h.p.Len())
	}
	start := h.p.Idx()
	if !h.p.MoveDown() {
		t.Fatalf("MoveDown failed from index %d", start)
	}
	if h.p.Idx() != start+1 {
		t.Fatalf("Idx()=%d, want %d", h.p.Idx(), start+1)
	}
	last := h.p.Len() - 1
	h.p.idx = last
	if h.p.MoveDown() {
		t.Fatal("MoveDown should clamp at the last index")
	}
	h.p.idx = 0
	if h.p.MoveUp() {
		t.Fatal("MoveUp should clamp at index 0")
	}
}

// TestPanel_Reset covers the dropdown closing.
func TestPanel_Reset(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.type_("@ma")
	if !h.p.Visible() {
		t.Fatal("precondition: the dropdown should be visible")
	}
	h.p.Reset()
	if h.p.Visible() {
		t.Fatal("the dropdown should NOT be visible after Reset()")
	}
	if h.p.Len() != 0 {
		t.Fatalf("the candidates should be empty after Reset(); got %d", h.p.Len())
	}
}

// TestPanel_BareAtOpensListing covers the bare "@" listing the
// workspace.
func TestPanel_BareAtOpensListing(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("@")
	if !h.p.Visible() {
		t.Fatal("the bare @ should open the dropdown")
	}
	if h.p.Prefix() != "" {
		t.Errorf("prefix = %q, want \"\"", h.p.Prefix())
	}
	if h.p.Len() == 0 {
		t.Fatal("expected candidates for the bare @")
	}
}

// TestPanel_HandleKey covers the keys the dropdown owns.
//
// A hidden dropdown claims none; a visible one claims the arrows and
// Tab, and answers handled=false for everything else — the composer is
// live and the letters the user types have to reach it.
func TestPanel_HandleKey(t *testing.T) {
	h := newHarness(t, withAtFileFixtures(t))
	h.type_("@")

	if h.p.HandleKey(tea.Key{Code: 'x'}) {
		t.Error("the dropdown should not claim an ordinary letter")
	}
	if h.p.HandleKey(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}) {
		t.Error("the dropdown should not claim Shift+Tab")
	}
	if !h.p.HandleKey(tea.Key{Code: tea.KeyDown}) {
		t.Error("the dropdown should claim the Down arrow while it is up")
	}
	if h.p.Idx() != 1 {
		t.Errorf("the highlight after Down = %d, want 1", h.p.Idx())
	}
	if !h.p.HandleKey(tea.Key{Code: tea.KeyUp}) {
		t.Error("the dropdown should claim the Up arrow while it is up")
	}
	if h.p.Idx() != 0 {
		t.Errorf("the highlight after Up = %d, want 0", h.p.Idx())
	}

	h.p.Reset()
	if h.p.HandleKey(tea.Key{Code: tea.KeyDown}) {
		t.Error("a hidden dropdown should claim no key")
	}
}

// TestPanel_CompleteKeepsOrClosesTheDropdown covers the two
// completion paths: Tab keeps the dropdown up, the Enter path closes
// it.
func TestPanel_CompleteKeepsOrClosesTheDropdown(t *testing.T) {
	dir := withAtFileFixtures(t)

	h := newHarness(t, dir)
	h.type_("@ma")
	if !h.p.Complete(true) {
		t.Fatal("the completion should have replaced the mention")
	}
	if !h.p.Visible() {
		t.Error("the Tab completion should keep the dropdown open")
	}

	h = newHarness(t, dir)
	h.type_("@ma")
	if !h.p.Complete(false) {
		t.Fatal("the completion should have replaced the mention")
	}
	if h.p.Visible() {
		t.Error("the Enter completion should close the dropdown")
	}
}

// withAtFileFixtures builds a workspace with known files.
func withAtFileFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"main.go",
		"main_test.go",
		"other.go",
		"cmd/wagyu/main.go",
		"cmd/wagyu/main_test.go",
		"internal/app/prompt/mentions/files/atfiles.go",
	}
	for _, f := range files {
		withFile(t, filepath.Join(dir, f))
	}
	return dir
}

// withFile creates an empty file, making its parent directories.
func withFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", p, err)
	}
}

// withDir creates a directory.
func withDir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", p, err)
	}
}
