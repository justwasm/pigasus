package viewer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestGutterRowPutsTextAtAFixedColumn — the whole point of the gutter is
// that the text starts at the same column on every row, whatever the line
// number and whether or not the cursor is on it.
func TestGutterRowPutsTextAtAFixedColumn(t *testing.T) {
	const total = 102
	want := GutterIndent(total)
	for _, lineNo := range []int{1, 9, 10, 99, 102} {
		for _, isCursor := range []bool{false, true} {
			row := GutterRow(lineNo, total, isCursor, ">") + "text"
			got := ansi.StringWidth(row[:strings.Index(row, "text")])
			if got != want {
				t.Errorf("line %d (cursor=%v): text starts at column %d, want %d",
					lineNo, isCursor, got, want)
			}
		}
	}
}

// TestGutterNumberGrowsForALongDocument — a document past 9999 lines must
// widen the cell rather than truncating the number.
func TestGutterNumberGrowsForALongDocument(t *testing.T) {
	if got := NumberCellWidth(102); got != 4 {
		t.Errorf("NumberCellWidth(102) = %d, want 4", got)
	}
	if got := NumberCellWidth(123456); got != 6 {
		t.Errorf("NumberCellWidth(123456) = %d, want 6", got)
	}
	if got := LineNumber(123456, 123456); got != "123456" {
		t.Errorf("LineNumber = %q, want the whole number", got)
	}
}

// TestCursorMarkOccupiesExactlyOneCell — a mark wider than the column
// would shift the number and break the alignment above.
func TestCursorMarkOccupiesExactlyOneCell(t *testing.T) {
	for _, mark := range []string{">", "", " ", ">>", "\x1b[34m>\x1b[39m"} {
		if got := ansi.StringWidth(CursorMark(true, mark)); got != GutterCursorWidth {
			t.Errorf("CursorMark(%q) is %d cells, want %d", mark, got, GutterCursorWidth)
		}
	}
	if got := CursorMark(false, ">"); got != " " {
		t.Errorf("CursorMark(false) = %q, want a single space", got)
	}
}

// TestPositionCounterTracksWindowAndCursor — the counter's split is what
// makes "cursor moved, window did not" legible.
func TestPositionCounterTracksWindowAndCursor(t *testing.T) {
	// Top of a 102-line document with a 28-row window.
	if got, want := PositionCounter(1, 1, 28, 102), "[0%  L1  1-28/102]"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
	// Cursor moved to L4, window unchanged.
	if got, want := PositionCounter(4, 1, 28, 102), "[0%  L4  1-28/102]"; got != want {
		t.Errorf("cursor-only move = %q, want %q", got, want)
	}
	// At the end.
	if got, want := PositionCounter(102, 75, 102, 102), "[100%  L102  75-102/102]"; got != want {
		t.Errorf("end = %q, want %q", got, want)
	}
}

// TestWindowForCursorFollowsOnlyWhenNeeded — moving inside the window
// must not scroll it.
func TestWindowForCursorFollowsOnlyWhenNeeded(t *testing.T) {
	const visible, total = 28, 102
	// Cursor inside the window: the window stays.
	if got := WindowForCursor(0, 3, visible, total); got != 0 {
		t.Errorf("cursor at line 3 inside window [0,28) scrolled to %d, want 0", got)
	}
	// Cursor below the window: the window scrolls just enough.
	if got := WindowForCursor(0, 28, visible, total); got != 1 {
		t.Errorf("cursor at line 28 scrolled to %d, want 1", got)
	}
	// Cursor above the window: the window's top becomes the cursor.
	if got := WindowForCursor(40, 10, visible, total); got != 10 {
		t.Errorf("cursor at line 10 scrolled to %d, want 10", got)
	}
	// Never past the end.
	if got := WindowForCursor(500, 101, visible, total); got != total-visible {
		t.Errorf("window = %d, want the last page %d", got, total-visible)
	}
}

// TestExpandTabsCountsCells — a tab expands to the next stop, counted in
// cells so a wide rune does not shift the stop.
func TestExpandTabsCountsCells(t *testing.T) {
	if got, want := ExpandTabs("a\tb", 8), "a       b"; got != want {
		t.Errorf("ExpandTabs = %q, want %q", got, want)
	}
	// No tabs: unchanged, and the same string value.
	if got := ExpandTabs("plain", 8); got != "plain" {
		t.Errorf("ExpandTabs(plain) = %q", got)
	}
}
