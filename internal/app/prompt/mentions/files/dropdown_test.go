package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtFileDropdown_FiresAtEndOfLine(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d Dropdown
	d.Update("look at @ma", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible after typing @ma at end of line; got %+v", d)
	}
	if d.Len() == 0 {
		t.Fatalf("dropdown should have at least one candidate for prefix %q; got 0", d.prefix)
	}
}

func TestAtFileDropdown_FiresAfterNewline(t *testing.T) {
	dir := withAtFileFixtures(t)
	cases := []string{
		"first line\n@ma",
		"first line\n\n@ma",
		"first line ends here\n@cmd/wagyu/main",
	}
	for _, buf := range cases {
		t.Run(buf, func(t *testing.T) {
			var d Dropdown
			d.Update(buf, dir)
			if !d.Visible() {
				t.Fatalf("dropdown should be visible for buffer %q; got %+v", buf, d)
			}
		})
	}
}

func TestAtFileDropdown_FiresMidLine(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d Dropdown
	d.Update("hello @ma", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible for @ mid-line; got %+v", d)
	}
	if d.prefix != "ma" {
		t.Errorf("prefix = %q, want \"ma\"", d.prefix)
	}
}

func TestAtFileDropdown_NoFireInsideWords(t *testing.T) {
	dir := withAtFileFixtures(t)
	cases := []string{
		"foo@bar",
		"alice@example.com",
		"/path/to@something",
	}
	for _, buf := range cases {
		t.Run(buf, func(t *testing.T) {
			var d Dropdown
			d.Update(buf, dir)
			if d.Visible() {
				t.Fatalf("dropdown should NOT fire inside word %q; got %+v", buf, d)
			}
		})
	}
}

func TestAtFileDropdown_SpaceInDirectoryName(t *testing.T) {
	dir := withAtFileFixtures(t)
	withDir(t, filepath.Join(dir, "with space"))
	withFile(t, filepath.Join(dir, "with space", "nested.txt"))
	var d Dropdown
	d.Update("@with space/", dir)
	if !d.Visible() {
		t.Fatalf("dropdown should be visible for space-bearing dir")
	}
	if d.Len() == 0 {
		t.Fatalf("expected candidate inside space-bearing dir")
	}
	found := false
	for _, e := range d.candidates {
		if strings.Contains(e.Rel, "nested.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("candidate list missing nested.txt: %+v", d.candidates)
	}
}

func TestAtFileDropdown_CandidateNarrowing(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d Dropdown
	d.Update("@ma", dir)
	firstLen := d.Len()
	if firstLen == 0 {
		t.Fatalf("precondition: expected candidates for @ma")
	}
	d.Update("@mai", dir)
	if d.Len() > firstLen {
		t.Fatalf("candidates grew when narrowing prefix: %d -> %d", firstLen, d.Len())
	}
	d.Update("@ot", dir)
	if d.Len() == 0 {
		t.Fatalf("expected candidate for @ot")
	}
	for _, c := range d.candidates {
		if !strings.HasPrefix(filepath.Base(c.Path), "ot") && !strings.HasPrefix(c.Rel, "ot") {
			t.Errorf("candidate %q does not match prefix ot", c.Path)
		}
	}
}

func TestAtFileDropdown_TabCompletionValues(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d Dropdown
	d.Update("@ma", dir)
	if d.Len() == 0 {
		t.Fatalf("precondition: expected candidates for @ma")
	}
	repl, start, end := d.TabCompletion()
	if repl == "" {
		t.Fatalf("TabCompletion returned empty string")
	}
	if start != 0 || end != 3 {
		t.Errorf("span = (%d, %d), want (0, 3) for input @ma", start, end)
	}
}

func TestAtFileDropdown_Navigation(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d Dropdown
	d.Update("@", dir)
	if d.Len() < 2 {
		t.Skipf("need at least 2 candidates; got %d", d.Len())
	}
	start := d.idx
	if !d.MoveDown() {
		t.Fatalf("MoveDown failed from index %d", start)
	}
	if d.Idx() != start+1 {
		t.Fatalf("Idx()=%d, want %d", d.Idx(), start+1)
	}
	last := d.Len() - 1
	d.idx = last
	if d.MoveDown() {
		t.Fatalf("MoveDown should clamp at last index")
	}
	d.idx = 0
	if d.MoveUp() {
		t.Fatalf("MoveUp should clamp at index 0")
	}
}

func TestAtFileDropdown_Reset(t *testing.T) {
	var d Dropdown
	d.Update("@ma", t.TempDir())
	if !d.Visible() {
		t.Fatalf("precondition: dropdown should be visible")
	}
	d.Reset()
	if d.Visible() {
		t.Fatalf("dropdown should NOT be visible after Reset()")
	}
	if d.Len() != 0 {
		t.Fatalf("candidates should be empty after Reset(); got %d", d.Len())
	}
}

func TestAtFileDropdown_BareAtOpensListing(t *testing.T) {
	dir := withAtFileFixtures(t)
	var d Dropdown
	d.Update("@", dir)
	if !d.Visible() {
		t.Fatalf("bare @ should open dropdown")
	}
	if d.prefix != "" {
		t.Errorf("prefix = %q, want \"\"", d.prefix)
	}
	if d.Len() == 0 {
		t.Fatalf("expected candidates for bare @")
	}
}

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
