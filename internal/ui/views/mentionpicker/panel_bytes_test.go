// panel_bytes_test.go — the dropdown's rendered bytes, pinned.
//
// A panel's rendered bytes are pinned, not eyeballed: the column
// widths, the escape order and the row sequence are the reference's,
// and a refactor that moves one of them changes what the user sees.
//
// The two goldens below were taken from the pre-tranche tree — the
// dropdown that lived in internal/app/prompt/mentions/files — and are
// reproduced byte for byte.
//
// 【一手】 agy 1.4.3, live: the dropdown's colours are not the
// palette's scheme slots, they are the reference's own values.
package mentionpicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenQueryRows is the dropdown's query mode: the relative path
// alone, the highlighted row in the brand blue, the empty row that
// pads the window, the blank separator and the hint row.
//
// The mode ignores the terminal's width, so the golden holds at any
// width.
const goldenQueryRows = "\x1b[38;2;66;133;244m> main.go\x1b[0m\n  main_test.go\n  cmd/wagyu/main.go\n  cmd/wagyu/main_test.go\n\n\n\n  \x1b[38;2;66;133;244m↑/↓\x1b[38;2;95;99;104m\x1b[38;2;95;99;104m Navigate · \x1b[38;2;66;133;244menter\x1b[38;2;95;99;104m\x1b[38;2;95;99;104m Select · \x1b[38;2;66;133;244mtab\x1b[38;2;95;99;104m\x1b[38;2;95;99;104m Complete\x1b[0m\n"

// goldenEmptyRows is the dropdown's placeholder listing: the row the
// reference shows before the user has typed a query, the empty rows
// that pad the window, the blank separator and the hint row.
//
// The width has to leave room for the placeholder's description
// column, so the golden is pinned at 120 columns.
const goldenEmptyRows = "\x1b[38;2;66;133;244m> Type to search workspace files...\x1b[0m  \x1b[38;2;95;99;104mFile        Keep typing to filter files or directories\x1b[0m\n\n\n\n\n\n\n  \x1b[38;2;66;133;244m↑/↓\x1b[38;2;95;99;104m\x1b[38;2;95;99;104m Navigate · \x1b[38;2;66;133;244menter\x1b[38;2;95;99;104m\x1b[38;2;95;99;104m Select · \x1b[38;2;66;133;244mtab\x1b[38;2;95;99;104m\x1b[38;2;95;99;104m Complete\x1b[0m\n"

// TestRows_QueryMode pins the query mode's bytes.
func TestRows_QueryMode(t *testing.T) {
	h := newHarness(t, withByteFixtures(t))
	h.width = 120
	h.type_("@ma")
	if !h.p.Visible() {
		t.Fatal("setup: the dropdown should be visible for @ma")
	}
	if got := h.p.Rows(); got != goldenQueryRows {
		t.Errorf("the query mode's bytes changed:\n got %q\nwant %q", got, goldenQueryRows)
	}
}

// TestRows_EmptyListing pins the placeholder listing's bytes.
func TestRows_EmptyListing(t *testing.T) {
	h := newHarness(t, withByteFixtures(t))
	h.width = 120
	h.type_("@zzznope")
	if !h.p.Empty() {
		t.Fatal("setup: the dropdown should hold no candidate for @zzznope")
	}
	if got := h.p.Rows(); got != goldenEmptyRows {
		t.Errorf("the empty listing's bytes changed:\n got %q\nwant %q", got, goldenEmptyRows)
	}
}

// TestRows_ColumnMode pins the column mode's bytes: the 33-column
// name field, the 12-column type cell and the path cell.
//
// The width is narrow on purpose: the path cell then always holds the
// reference's "/..." truncation, so the golden does not depend on how
// long the machine's temporary directory is.
func TestRows_ColumnMode(t *testing.T) {
	h := newHarness(t, withByteFixtures(t))
	h.width = 40
	h.type_("@")
	rows := h.p.Rows()
	if rows == "" {
		t.Fatal("setup: the dropdown should be visible for the bare @")
	}
	first := rows[:strings.Index(rows, "\n")+1]
	want := colorNameHi + "> cmd/" + colorReset + strings.Repeat(" ", 33-len("> cmd/")) +
		colorDescDim + "Directory   /..." + colorReset + "\n"
	if first != want {
		t.Errorf("the listing's first row changed:\n got %q\nwant %q", first, want)
	}
}

// withByteFixtures builds the workspace the byte goldens were taken
// over.
//
// 【一手】 agy 1.4.3, live: the listing shows the directories and the
// files side by side, the directories first.
func withByteFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"main.go",
		"main_test.go",
		"other.go",
		"README.md",
		"cmd/wagyu/main.go",
		"cmd/wagyu/main_test.go",
		"internal/pkg/sub/file.go",
		"with space/nested.txt",
	}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", p, err)
		}
	}
	return dir
}
