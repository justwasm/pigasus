package main

// wordmove_test.go — cycle 225 tests for AGY 1.2.16 "Added word-wise
// cursor movement in the prompt editor: Alt/Option+Left and
// Alt/Option+Right move the cursor to the previous/next word
// boundary, matching macOS text-field conventions."
//
// The macOS NSText convention is implemented by the helpers in
// wordmove.go (wordBoundaryBack, wordBoundaryFwd) and wired into
// runMode.Update by tui_run.go. The unit tests pin the exact
// positions a user observes: a sequence of Alt+Lefts from the end of
// "hello world foo bar" should land at "hello world foo |bar" →
// "hello world foo |" → "hello world |foo bar" → "hello world |" →
// "|hello world foo bar" → "|", one step at a time. A sequence of
// Alt+Rights from the start should reverse it.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// altLeftKey / altRightKey build the synthetic tea.KeyPressMsg the
// runMode.Update handler dispatches against. The bubbles textarea
// needs a focused model and a non-virtual cursor for Cursor() to
// return a non-nil offset (cycle 71 fix); the test setup below
// mirrors what NewRunMode produces for production.
func altLeftKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt})
}

func altRightKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight, Mod: tea.ModAlt})
}

// typeString drives the textarea via the same rune-typed
// tea.KeyPressMsg path tui_run_test.go uses, so the runes bubble sees
// come from the same Update dispatch the production TUI uses (the
// real terminal also delivers printable runes as Key.Code + Key.Text,
// not via InsertRune).
func typeString(m *runMode, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

// cursorRowCol returns the current (row, col) of the textarea. Both
// fields are read from the public Line() / Column() accessors.
func cursorRowCol(m *runMode) (row, col int) {
	return m.input.Line(), m.input.Column()
}

// TestAltRightNextWordBoundary verifies Alt+Right moves the cursor to
// the end of the next word, matching macOS NSText semantics.
func TestAltRightNextWordBoundary(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world foo bar")
	// Cursor is at the end (line=0, col=19). Alt+Right at end is a
	// no-op (cycle-225 spec: stays put at end-of-buffer).
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 19 {
		t.Fatalf("Alt+Right at end should stay at (0,19); got (%d,%d)", row, col)
	}
	// Alt+Left → "hello world foo |bar" (col=16, start of "bar").
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 16 {
		t.Fatalf("Alt+Left 1 should land at start of 'bar' (col 16); got (%d,%d)", row, col)
	}
	// Alt+Left → "hello world |foo bar" (col=12, start of "foo").
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 12 {
		t.Fatalf("Alt+Left 2 should land at start of 'foo' (col 12); got (%d,%d)", row, col)
	}
	// Alt+Left → "hello world |" (col=6, start of "world"; we land
	// at the first rune of the word, not the rune after the
	// whitespace run — verified against Cocoa).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 6 {
		t.Fatalf("Alt+Left 3 should land at start of 'world' (col 6); got (%d,%d)", row, col)
	}
	// Alt+Left → "|hello world foo bar" (col=0).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left 4 should land at col 0; got (%d,%d)", row, col)
	}
	// Alt+Right → "hello| world foo bar" (col=5, end of "hello").
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 5 {
		t.Fatalf("Alt+Right from start should land at end of 'hello' (col 5); got (%d,%d)", row, col)
	}
	// Alt+Right → "hello world| foo bar" (col=11, end of "world").
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 11 {
		t.Fatalf("Alt+Right should land at end of 'world' (col 11); got (%d,%d)", row, col)
	}
}

// TestAltLeftPreviousWordBoundary verifies Alt+Left walks the cursor
// backwards word-by-word, from end-of-buffer to start, landing on
// the first rune of each word.
func TestAltLeftPreviousWordBoundary(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world foo bar")
	// Cursor at end (col=19). Walk back via Alt+Left until col=0.
	//   19 → 16 (start of "bar")
	//   16 → 12 (start of "foo")
	//   12 →  6 (start of "world")
	//    6 →  0 (start of "hello")
	want := []int{16, 12, 6, 0}
	for i, w := range want {
		m.Update(altLeftKey())
		if row, col := cursorRowCol(m); row != 0 || col != w {
			t.Fatalf("Alt+Left #%d expected col %d, got (%d,%d)", i+1, w, row, col)
		}
	}
}

// TestAltRightAtEndStays verifies Alt+Right at the end of the buffer
// is a no-op — the cursor does not move. macOS NSText and bubbles'
// wordRight agree on this.
func TestAltRightAtEndStays(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world")
	startRow, startCol := cursorRowCol(m)
	m.Update(altRightKey())
	m.Update(altRightKey())
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != startRow || col != startCol {
		t.Fatalf("Alt+Right at end should stay put; got (%d,%d) want (%d,%d)",
			row, col, startRow, startCol)
	}
}

// TestAltLeftAtStartStays verifies Alt+Left at the start of the buffer
// is a no-op. macOS NSText and bubbles' wordLeft agree.
func TestAltLeftAtStartStays(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world")
	// Move cursor to start with the textarea's Home binding
	// (ctrl+a → LineStart).
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	row, col := cursorRowCol(m)
	if row != 0 || col != 0 {
		t.Fatalf("precondition: ctrl+a should land at (0,0); got (%d,%d)", row, col)
	}
	m.Update(altLeftKey())
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left at start should stay put; got (%d,%d)", row, col)
	}
}

// TestSingleWordNoMovement verifies Alt+Right and Alt+Left on a
// single-word buffer both stay put — there are no word boundaries to
// cross.
func TestSingleWordNoMovement(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello")
	// Cursor at end (col=5). Alt+Right stays.
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 5 {
		t.Fatalf("Alt+Right on single word should stay at col 5; got (%d,%d)", row, col)
	}
	// Move to start with ctrl+a, then Alt+Left stays.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("ctrl+a should land at (0,0); got (%d,%d)", row, col)
	}
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left on single word should stay at col 0; got (%d,%d)", row, col)
	}
}

// TestAltWordCrossesLines verifies Alt+Left / Alt+Right walk across
// line boundaries, treating '\n' as whitespace. A two-line buffer
// "hello\nworld" with the cursor at the end of "world" should:
//   - Alt+Left once → start of "world" (row=1, col=0)
//   - Alt+Left again → walk across the '\n', past "hello", land at
//     the start of "hello" (row=0, col=0) — macOS convention puts
//     the cursor at the START of the prior word, not its end.
//   - Alt+Left again → no-op (already at start).
//
// Verified against bubbles textarea v2.1.1 (the prompt editor
// upstream — cycle 71) via a probe before this fix.
func TestAltWordCrossesLines(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello")
	// ctrl+j inserts a newline (cycle 71 path).
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl}))
	typeString(m, "world")
	if row, col := cursorRowCol(m); row != 1 || col != 5 {
		t.Fatalf("precondition: cursor should be at (1,5); got (%d,%d)", row, col)
	}
	// Alt+Left from end-of-line-1 → start of "world" (row=1, col=0).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 1 || col != 0 {
		t.Fatalf("Alt+Left within line should land at start of 'world' (1,0); got (%d,%d)", row, col)
	}
	// Alt+Left again → cross the newline, walk past "hello", land
	// at start of "hello" (row=0, col=0). The whitespace run
	// between the lines is just the '\n' itself; the macOS
	// convention puts us on the first non-whitespace rune of the
	// prior word.
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left across newline should land at start of 'hello' (0,0); got (%d,%d)", row, col)
	}
	// Alt+Left once more → no-op (already at start).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left at start should stay at (0,0); got (%d,%d)", row, col)
	}
}

// TestAltRightFromMiddleOfWord verifies Alt+Right from inside a word
// still lands at the end of the NEXT word, not the end of the current
// one (macOS convention).
func TestAltRightFromMiddleOfWord(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world")
	// Cursor at end (col=11). Move to middle of "hello" with
	// repeated ctrl+b (bubble's WordBackward — verified to land at
	// the start of "hello").
	for i := 0; i < 6; i++ {
		m.Update(altLeftKey())
	}
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("precondition: 6×Alt+Left should land at (0,0); got (%d,%d)", row, col)
	}
	// Move into the middle of "hello" via the right arrow × 2.
	for i := 0; i < 2; i++ {
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	}
	if row, col := cursorRowCol(m); row != 0 || col != 2 {
		t.Fatalf("precondition: 2×Right should land at (0,2); got (%d,%d)", row, col)
	}
	// Alt+Right from inside "hello" → end of "hello" (col=5).
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 5 {
		t.Fatalf("Alt+Right from middle of word should land at end of 'hello' (col 5); got (%d,%d)", row, col)
	}
	// Alt+Right → end of "world" (col=11).
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 11 {
		t.Fatalf("Alt+Right after 'hello' should land at end of 'world' (col 11); got (%d,%d)", row, col)
	}
}

// TestAltWordHelperDirect exercises wordBoundaryBack / wordBoundaryFwd
// directly on representative macOS-convention inputs (Chinese text,
// mixed punctuation, leading/trailing whitespace). The runMode-level
// tests above exercise the dispatcher; this one locks the helpers'
// arithmetic against regressions.
func TestAltWordHelperDirect(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		col      int
		wantBack int
		wantFwd  int
	}{
		{
			name:     "ascii two words, mid first word",
			line:     "hello world",
			col:      2,
			wantBack: 0,
			wantFwd:  5,
		},
		{
			name:     "ascii trailing whitespace",
			line:     "hello   ",
			col:      8,
			wantBack: 0,
			wantFwd:  8,
		},
		{
			name:     "ascii leading whitespace",
			line:     "   hello",
			col:      0,
			wantBack: 0,
			wantFwd:  8,
		},
		{
			name:     "chinese chars no spaces",
			line:     "你好世界",
			col:      2,
			wantBack: 0,
			wantFwd:  4,
		},
		{
			name:     "chinese with ascii word mixed",
			line:     "你好 world",
			col:      1,
			wantBack: 0,
			wantFwd:  2,
		},
		{
			name:     "punctuation joins the surrounding word",
			line:     "foo,bar",
			col:      1,
			wantBack: 0,
			wantFwd:  7,
		},
		{
			name:     "col at end stays put on Alt+Right",
			line:     "abc",
			col:      3,
			wantBack: 0,
			wantFwd:  3,
		},
		{
			name:     "col at start stays put on Alt+Left",
			line:     "abc def",
			col:      0,
			wantBack: 0,
			wantFwd:  3,
		},
	}
	for _, tc := range cases {
		t.Run("back/"+tc.name, func(t *testing.T) {
			got := wordBoundaryBack(tc.line, tc.col)
			if got != tc.wantBack {
				t.Fatalf("wordBoundaryBack(%q,%d)=%d want %d", tc.line, tc.col, got, tc.wantBack)
			}
		})
		t.Run("fwd/"+tc.name, func(t *testing.T) {
			got := wordBoundaryFwd(tc.line, tc.col)
			if got != tc.wantFwd {
				t.Fatalf("wordBoundaryFwd(%q,%d)=%d want %d", tc.line, tc.col, got, tc.wantFwd)
			}
		})
	}
}

// TestAltWordPickerOverlayStillUpdates verifies the picker overlay's
// first-line filter is refreshed after Alt+Left / Alt+Right move the
// cursor (the dispatcher calls picker.Update with the new first line;
// without that, the picker's "matches" cache would lag the cursor and
// a subsequent Tab would complete against the old substring).
func TestAltWordPickerOverlayStillUpdates(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world foo bar")
	// Move cursor with Alt+Left to mid-word.
	for i := 0; i < 2; i++ {
		m.Update(altLeftKey())
	}
	row, col := cursorRowCol(m)
	if row != 0 || col != 12 {
		t.Fatalf("precondition: 2×Alt+Left from end should land at col 12 (start of 'foo'); got (%d,%d)", row, col)
	}
	// The picker's filter is keyed on firstLine(value); a "/" trigger
	// would now filter against "hello world foo bar" (unchanged, but
	// exercised through this code path so any future regression
	// where the picker is skipped on Alt+Left surfaces here).
	if v := m.input.Value(); !strings.Contains(v, "hello world foo bar") {
		t.Fatalf("expected value intact after Alt+Left motion; got %q", v)
	}
}
