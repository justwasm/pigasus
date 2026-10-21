// tui_cursor_boundary_test.go — unit tests for the cursor-line
// boundary rule that gates Up/Down history recall against the
// textarea's built-in cursor-movement bindings (see
// tui_cursor_boundary.go and the cycle-multiline-cursor-history-
// boundary comment in tui_run.go).
//
// The first half drives the pure (value, cursor) helpers directly
// with synthetic inputs; the second half drives a real runMode via
// tea.KeyMsg and asserts which path the key dispatch took (history
// recall vs. fall-through to m.input.Update).
package main

import (
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// upKey / downKey build plain arrow tea.KeyPressMsg with no
// modifier — matches the keys the runtime dispatches when the user
// presses Up/Down on the input box.
func upKey() tea.KeyPressMsg   { return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}) }
func downKey() tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}) }

// --- pure helpers ----------------------------------------------------

func TestCursorLine_SingleLine_NoNewlines(t *testing.T) {
	cases := []struct {
		value  string
		cursor int
		want   int
	}{
		{"", 0, 0},
		{"hello", 0, 0},
		{"hello", 3, 0},
		{"hello", 5, 0}, // end-of-input on the only line
	}
	for _, c := range cases {
		if got := cursorLine(c.value, c.cursor); got != c.want {
			t.Errorf("cursorLine(%q, %d) = %d, want %d", c.value, c.cursor, got, c.want)
		}
	}
}

func TestCursorLine_MultiLine(t *testing.T) {
	// Layout: a\nb\nc
	// Bytes:  0 1 2 3 4
	//         a \n b \n c
	// Lines:  0   1   2
	cases := []struct {
		cursor int
		want   int
	}{
		{0, 0}, // start of "a"
		{1, 0}, // end of "a", just before the first \n
		{2, 1}, // start of "b", just after the first \n
		{3, 1}, // middle of "b"
		{4, 2}, // start of "c", just after the second \n
		{5, 2}, // end of "c"
	}
	for _, c := range cases {
		if got := cursorLine("a\nb\nc", c.cursor); got != c.want {
			t.Errorf("cursorLine(\"a\\nb\\nc\", %d) = %d, want %d", c.cursor, got, c.want)
		}
	}
}

func TestCursorLine_ClampOutOfRange(t *testing.T) {
	// Negative cursor clamps to 0; cursor past end clamps to len.
	if got := cursorLine("a\nb", -5); got != 0 {
		t.Errorf("cursorLine(_, -5) = %d, want 0", got)
	}
	if got := cursorLine("a\nb", 100); got != 1 {
		t.Errorf("cursorLine(_, 100) = %d, want 1 (last line)", got)
	}
	// Cursor == len(value) sits on the last line (one byte past
	// the last rune is still inside the value).
	if got := cursorLine("a\nb", 3); got != 1 {
		t.Errorf("cursorLine(\"a\\nb\", 3) = %d, want 1 (end-of-input is on the last line)", got)
	}
}

func TestIsOnFirstLine_TrueCases(t *testing.T) {
	// Empty value: trivially on the first (and only) line.
	if !isOnFirstLine("", 0) {
		t.Error(`isOnFirstLine("", 0) = false, want true`)
	}
	// Single-line value: every cursor is on the first line.
	for i := 0; i <= 5; i++ {
		if !isOnFirstLine("hello", i) {
			t.Errorf(`isOnFirstLine("hello", %d) = false, want true`, i)
		}
	}
	// Multi-line: cursor before the first \n is on the first line.
	if !isOnFirstLine("a\nb\nc", 0) {
		t.Error(`isOnFirstLine("a\nb\nc", 0) = false, want true`)
	}
	if !isOnFirstLine("a\nb\nc", 1) {
		t.Error(`isOnFirstLine("a\nb\nc", 1) = false, want true (just before first \n)`)
	}
}

func TestIsOnFirstLine_FalseCases(t *testing.T) {
	// Cursor at the start of "b" is on the second line.
	if isOnFirstLine("a\nb\nc", 2) {
		t.Error(`isOnFirstLine("a\nb\nc", 2) = true, want false`)
	}
	// Cursor at the start of "c" is on the third (last) line.
	if isOnFirstLine("a\nb\nc", 5) {
		t.Error(`isOnFirstLine("a\nb\nc", 5) = true, want false`)
	}
}

func TestIsOnLastLine_TrueCases(t *testing.T) {
	// Empty value: trivially on the last (and only) line.
	if !isOnLastLine("", 0) {
		t.Error(`isOnLastLine("", 0) = false, want true`)
	}
	// Single-line value: every cursor is on the last line.
	for i := 0; i <= 5; i++ {
		if !isOnLastLine("hello", i) {
			t.Errorf(`isOnLastLine("hello", %d) = false, want true`, i)
		}
	}
	// Multi-line: cursor on the last line is on the last line.
	if !isOnLastLine("a\nb\nc", 5) {
		t.Error(`isOnLastLine("a\nb\nc", 5) = false, want true`)
	}
	if !isOnLastLine("a\nb\nc", 6) {
		t.Error(`isOnLastLine("a\nb\nc", 6) = false, want true (end-of-input on last line)`)
	}
}

func TestIsOnLastLine_FalseCases(t *testing.T) {
	// Cursor on the first line has more content after it.
	if isOnLastLine("a\nb\nc", 0) {
		t.Error(`isOnLastLine("a\nb\nc", 0) = true, want false`)
	}
	// Cursor on the middle line has more content after it.
	if isOnLastLine("a\nb\nc", 2) {
		t.Error(`isOnLastLine("a\nb\nc", 2) = true, want false`)
	}
}

func TestInputCursorByteOffset_MultiLine(t *testing.T) {
	m := tas.New()
	// Pin a wide width so the textarea doesn't soft-wrap short
	// single-character rows. The default width is 40 but the
	// prompt is "┃ " which eats 2 columns and active style may
	// add borders; 100 leaves plenty of headroom.
	m.SetWidth(100)
	m.SetHeight(10)
	m.SetValue("a\nb\nc")
	// bubbles v2 textarea's SetValue leaves the cursor at the end
	// of the inserted text, not at row 0 col 0. MoveToBegin
	// anchors it back at the start so the byte offset tests start
	// from a known position.
	m.MoveToBegin()
	if got := inputCursorByteOffset(m); got != 0 {
		t.Errorf("cursor at start after MoveToBegin = %d, want 0", got)
	}
	// Move to row 1, col 0 via CursorDown. CursorDown preserves
	// the column when the row below is at least as long as the
	// cursor's current column — col 0 stays col 0 here.
	m.CursorDown()
	if got := inputCursorByteOffset(m); got != 2 {
		t.Errorf("cursor on row 1, col 0 = %d, want 2 (a + \\n)", got)
	}
	// Move to row 1, col 1 (inside "b"). Byte offset 3.
	m.SetCursorColumn(1)
	if got := inputCursorByteOffset(m); got != 3 {
		t.Errorf("cursor on row 1, col 1 = %d, want 3", got)
	}
	// Move to row 2. CursorDown preserves the column when the
	// row below has the column in range; "c" is 1 char so the
	// cursor lands at (2, 1) — end-of-row. Byte offset 5.
	m.CursorDown()
	if got := inputCursorByteOffset(m); got != 5 {
		t.Errorf("cursor on row 2 (col preserved as 1) = %d, want 5", got)
	}
	// Back up to (2, 0) via SetCursorColumn(0). Byte offset 4.
	m.SetCursorColumn(0)
	if got := inputCursorByteOffset(m); got != 4 {
		t.Errorf("cursor on row 2, col 0 = %d, want 4 (start of c)", got)
	}
}

func TestInputCursorByteOffset_UTF8(t *testing.T) {
	// 2-byte UTF-8 runes: "αβγ" each is 2 bytes. "α\nβ" should
	// produce a byte offset of 3 (after α + the \n) when the
	// cursor sits at the start of "β".
	m := tas.New()
	m.SetValue("α\nβ")
	// SetValue leaves the cursor at the end; MoveToBegin anchors
	// it back at row 0 col 0.
	m.MoveToBegin()
	// Row 0 col 0 → byte offset 0.
	if got := inputCursorByteOffset(m); got != 0 {
		t.Errorf("UTF8 cursor on row 0, col 0 = %d, want 0", got)
	}
	// Row 1 col 0 → byte offset 3 (α is 2 bytes + the \n).
	m.CursorDown()
	if got := inputCursorByteOffset(m); got != 3 {
		t.Errorf("UTF8 cursor on row 1, col 0 = %d, want 3 (α is 2 bytes + \\n)", got)
	}
	// Row 1 col 1 → byte offset 5 (β starts at byte 3 and is
	// 2 bytes long).
	m.SetCursorColumn(1)
	if got := inputCursorByteOffset(m); got != 5 {
		t.Errorf("UTF8 cursor on row 1, col 1 = %d, want 5", got)
	}
}

// --- integration: runMode + tea.KeyMsg ------------------------------

// runModeForTest returns a freshly-constructed runMode with no
// history and no picker state, suitable for driving KeyMsg paths
// from a unit test without leaking state across cases. Reuses
// fakeClient declared in tui_run_test.go (same package).
func runModeForTest() *runMode {
	return NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
}

func TestUpOnFirstLineSingleLine_TriggersHistory(t *testing.T) {
	m := runModeForTest()
	// Push a history entry directly. Single-line input value
	// (cursor on the first / only line) should recall it.
	m.pushHistory("first prompt")
	if got := m.input.Value(); got != "" {
		t.Fatalf("pre-condition: empty input got %q", got)
	}
	// Up arrow on empty input → first (and only) line → historyUp.
	m.Update(upKey())
	if got := m.input.Value(); got != "first prompt" {
		t.Errorf("Up on empty input should recall history; got input = %q, want %q",
			got, "first prompt")
	}
	if m.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1 after one historyUp", m.historyIdx)
	}
}

func TestUpOnFirstLineMultiLine_TriggersHistory(t *testing.T) {
	m := runModeForTest()
	m.pushHistory("previous prompt")
	m.input.SetValue("first\nsecond\nthird")
	// SetValue leaves the cursor at the end (line 2). MoveToBegin
	// anchors it at row 0 col 0 so the test exercises the
	// first-line history path.
	m.input.MoveToBegin()
	if m.input.Line() != 0 {
		t.Fatalf("pre-condition: cursor should be on line 0, got line %d", m.input.Line())
	}
	valueBefore := m.input.Value()
	m.Update(upKey())
	if got := m.input.Value(); got == valueBefore {
		t.Errorf("Up on first line of multi-line input should call historyUp; input unchanged (%q)", got)
	}
	if got := m.input.Value(); got != "previous prompt" {
		t.Errorf("Up on first line should recall history %q, got %q", "previous prompt", got)
	}
	if m.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1 after one historyUp", m.historyIdx)
	}
}

func TestUpOnMiddleLine_DoesNotTriggerHistory(t *testing.T) {
	m := runModeForTest()
	m.pushHistory("should not be recalled")
	m.input.SetValue("first\nsecond\nthird")
	// SetValue leaves cursor on line 2; MoveToBegin + CursorDown
	// lands it on the middle line (row 1).
	m.input.MoveToBegin()
	m.input.CursorDown()
	if m.input.Line() != 1 {
		t.Fatalf("pre-condition: cursor should be on line 1, got line %d", m.input.Line())
	}
	valueBefore := m.input.Value()
	m.Update(upKey())
	// Input value must NOT change (no historyUp).
	if got := m.input.Value(); got != valueBefore {
		t.Errorf("Up on middle line should NOT call historyUp; input changed from %q to %q",
			valueBefore, got)
	}
	// historyIdx stays at 0 (no history navigation occurred).
	if m.historyIdx != 0 {
		t.Errorf("historyIdx = %d, want 0 (no history recall)", m.historyIdx)
	}
	// Cursor should have moved up one logical line via the
	// textarea's LinePrevious binding.
	if m.input.Line() != 0 {
		t.Errorf("cursor Line() = %d, want 0 after Up moved cursor up", m.input.Line())
	}
}

func TestDownOnLastLine_TriggersHistory(t *testing.T) {
	m := runModeForTest()
	// Two history entries so historyDown has somewhere to land.
	m.pushHistory("older prompt")
	m.pushHistory("newer prompt")
	// Navigate up to the newest entry so historyIdx > 0 and
	// historyDown has work to do.
	m.Update(upKey()) // historyIdx=1, value="newer prompt"
	m.Update(upKey()) // historyIdx=2, value="older prompt"
	if m.historyIdx != 2 {
		t.Fatalf("setup: historyIdx = %d, want 2", m.historyIdx)
	}
	// Put a fresh multi-line prompt and place the cursor on the
	// last line.
	m.input.SetValue("alpha\nbeta\ngamma")
	m.input.CursorDown() // row=1
	m.input.CursorDown() // row=2, last line
	if m.input.Line() != 2 {
		t.Fatalf("pre-condition: cursor should be on last line (2), got line %d", m.input.Line())
	}
	m.Update(downKey())
	// historyDown: historyIdx goes from 2 back to 1, value
	// becomes "newer prompt".
	if m.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1 after one historyDown", m.historyIdx)
	}
	if got := m.input.Value(); got != "newer prompt" {
		t.Errorf("Down on last line should call historyDown; got %q, want %q", got, "newer prompt")
	}
}

func TestDownOnMiddleLine_DoesNotTriggerHistory(t *testing.T) {
	m := runModeForTest()
	m.pushHistory("must not be recalled")
	m.input.SetValue("first\nsecond\nthird")
	// SetValue leaves cursor on line 2; MoveToBegin + CursorDown
	// lands it on the middle line (row 1).
	m.input.MoveToBegin()
	m.input.CursorDown()
	if m.input.Line() != 1 {
		t.Fatalf("pre-condition: cursor should be on line 1, got line %d", m.input.Line())
	}
	valueBefore := m.input.Value()
	idxBefore := m.historyIdx
	m.Update(downKey())
	// Input value must NOT change (no historyDown).
	if got := m.input.Value(); got != valueBefore {
		t.Errorf("Down on middle line should NOT call historyDown; input changed from %q to %q",
			valueBefore, got)
	}
	if m.historyIdx != idxBefore {
		t.Errorf("historyIdx = %d, want %d (no history navigation)", m.historyIdx, idxBefore)
	}
	// Cursor should have moved down one logical line via the
	// textarea's LineNext binding.
	if m.input.Line() != 2 {
		t.Errorf("cursor Line() = %d, want 2 after Down moved cursor down", m.input.Line())
	}
}

func TestPickerVisible_OverridesUpHistory(t *testing.T) {
	m := runModeForTest()
	m.pushHistory("would be recalled if picker were hidden")
	// Open the picker by typing '/'. After this m.picker is
	// visible with idx == 0.
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatalf("setup: picker should be visible after typing '/'")
	}
	// Move the picker highlight to row 1 so an Up arrow has
	// somewhere to land.
	m.Update(downKey()) // picker.MoveDown → idx=1
	if m.picker.Idx() != 1 {
		t.Fatalf("setup: picker.Idx() = %d, want 1", m.picker.Idx())
	}
	valueBefore := m.input.Value()
	idxBefore := m.historyIdx
	m.Update(upKey()) // picker.MoveUp → idx=0; historyUp MUST NOT fire
	if m.picker.Idx() != 0 {
		t.Errorf("Up with picker visible should call picker.MoveUp; idx = %d, want 0",
			m.picker.Idx())
	}
	if m.historyIdx != idxBefore {
		t.Errorf("historyIdx = %d, want %d (picker should override historyUp)",
			m.historyIdx, idxBefore)
	}
	if got := m.input.Value(); got != valueBefore {
		t.Errorf("input changed from %q to %q — picker path should not touch history",
			valueBefore, got)
	}
}
