package selection

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
)

type scenarioEditor struct {
	value string
	line  int
	col   int
}

func (e *scenarioEditor) Value() string              { return e.value }
func (e *scenarioEditor) SetValue(value string)      { e.value = value }
func (e *scenarioEditor) MoveCursorTo(line, col int) { e.line, e.col = line, col }

const (
	highlightOpen  = "\x1b[0m\x1b[7m"
	highlightClose = "\x1b[0m"
)

// releaseDrag walks a press, a drag and a release the way the host does
// and answers whether a highlight survived. The release performs no
// clipboard write, so there is no copy request to answer.
func releaseDrag(m *Model, editor *scenarioEditor, from, to int) bool {
	m.Begin(from, editor)
	m.Drag(to, editor)
	return m.End(editor.Value(), to, true)
}

func TestScenarioForwardDragHighlights(t *testing.T) {
	// Scenario: dragging over the first word selects it and paints it in
	// reverse video. The release itself copies nothing — agy 1.5.17 wants
	// the clipboard untouched until Ctrl+C or Ctrl+X.
	m := &Model{}
	editor := &scenarioEditor{value: "hello world"}
	if !releaseDrag(m, editor, 0, 5) {
		t.Fatal("drag left no highlight")
	}
	if m.InputSelectionDragging() || !m.HasInputSelection() {
		t.Fatalf("selection state after release: dragging=%v active=%v", m.InputSelectionDragging(), m.HasInputSelection())
	}
	if start, end := m.InputSelectionRange(); start != 0 || end != 5 {
		t.Fatalf("range = [%d,%d), want [0,5)", start, end)
	}
	got := m.ApplyHighlight("> hello world", editor.Value())
	want := "> " + highlightOpen + "hello" + highlightClose + " world"
	if got != want {
		t.Fatalf("highlight = %q, want %q", got, want)
	}
}

func TestScenarioReleaseHandsCopyToCtrlC(t *testing.T) {
	// Scenario: sub-behaviour 6's nucleus — the mouse release must not
	// overwrite the clipboard. The highlight survives the release so
	// Ctrl+C can copy, and Ctrl+X cuts; only those two carry text.
	m := &Model{}
	editor := &scenarioEditor{value: "hello world"}
	releaseDrag(m, editor, 0, 5)
	if request, ok := m.Copy(editor.Value()); !ok || request.Text != "hello" {
		t.Fatalf("copy after a release = (%q,%v), want (hello,true)", request.Text, ok)
	}
	request, ok := m.Cut(editor)
	if !ok || request.Text != "hello" {
		t.Fatalf("cut = (%q,%v), want (hello,true)", request.Text, ok)
	}
	if editor.value != " world" {
		t.Fatalf("value after cut = %q, want ' world'", editor.value)
	}
	if m.HasInputSelection() {
		t.Fatal("cut left the range active")
	}
}

func TestScenarioReverseDragNormalizesRange(t *testing.T) {
	// Scenario: dragging backwards produces the same normalized byte range.
	m := &Model{}
	editor := &scenarioEditor{value: "hello"}
	if !releaseDrag(m, editor, 5, 0) {
		t.Fatal("reverse drag left no highlight")
	}
	if start, end := m.InputSelectionRange(); start != 0 || end != 5 {
		t.Fatalf("normalized range = [%d,%d), want [0,5)", start, end)
	}
	if request, ok := m.Copy(editor.Value()); !ok || request.Text != "hello" {
		t.Fatalf("copy = (%q,%v), want (hello,true)", request.Text, ok)
	}
}

func TestScenarioEmptyReleaseCancelsDrag(t *testing.T) {
	// Scenario: a click without movement leaves no selection.
	m := &Model{}
	editor := &scenarioEditor{value: "hello"}
	m.Begin(2, editor)
	if m.End(editor.Value(), 2, true) {
		t.Fatal("a bare click left a highlight")
	}
	if m.InputSelectionDragging() || m.HasInputSelection() {
		t.Fatalf("empty release left state dragging=%v active=%v", m.InputSelectionDragging(), m.HasInputSelection())
	}
}

func TestScenarioReplaceAndDeleteSelection(t *testing.T) {
	// Scenario: replacing and deleting selections update the editor and cursor
	// through the narrow port, without constructing a RootModel.
	m := &Model{}
	editor := &scenarioEditor{value: "hello world"}
	m.SetRange(6, 11)
	m.ReplaceInputSelection(editor, "earth")
	if editor.value != "hello earth" || editor.line != 0 || editor.col != 11 || m.HasInputSelection() {
		t.Fatalf("replace observed value=%q cursor=(%d,%d) active=%v", editor.value, editor.line, editor.col, m.HasInputSelection())
	}
	m.SetRange(5, 11)
	m.DeleteInputSelection(editor)
	if editor.value != "hello" || editor.line != 0 || editor.col != 5 || m.HasInputSelection() {
		t.Fatalf("delete observed value=%q cursor=(%d,%d) active=%v", editor.value, editor.line, editor.col, m.HasInputSelection())
	}
}

func TestScenarioSelectAllMultilineAndClear(t *testing.T) {
	// Scenario: select-all returns the exact copy payload, moves to the logical
	// end of a multiline value, and reset clears range and drag state.
	m := &Model{}
	editor := &scenarioEditor{value: "one\ntwo"}
	copied, ok := m.SelectAll(editor)
	if !ok || copied.Text != editor.value {
		t.Fatalf("select all = (%q,%v), want full value", copied.Text, ok)
	}
	if editor.line != 1 || editor.col != 3 {
		t.Fatalf("cursor = (%d,%d), want (1,3)", editor.line, editor.col)
	}
	want := "> " + highlightOpen + "one" + highlightClose + "\n  " + highlightOpen + "two" + highlightClose
	if got := m.ApplyHighlight("> one\n  two", editor.Value()); got != want {
		t.Fatalf("multiline highlight = %q, want %q", got, want)
	}
	m.ResetInputSelection()
	if m.HasInputSelection() || m.InputSelectionDragging() {
		t.Fatalf("reset left range active or dragging")
	}
}

func TestScenarioStaleRangeCopiesAndEditsNothing(t *testing.T) {
	// Scenario: the host value shrinks while a selection exists. A stale
	// range must not copy unrelated bytes; editing clears it safely.
	m := &Model{}
	editor := &scenarioEditor{value: "old"}
	m.SetRange(2, 9)
	if _, ok := m.Copy(editor.Value()); ok {
		t.Fatal("stale range copied bytes")
	}
	m.ReplaceInputSelection(editor, "replacement")
	if editor.value != "old" || m.HasInputSelection() {
		t.Fatalf("stale edit changed value=%q active=%v", editor.value, m.HasInputSelection())
	}
}

func TestScenarioResetCancelsDragAndIgnoresLaterMotion(t *testing.T) {
	m := &Model{}
	editor := &scenarioEditor{value: "hello"}
	m.Begin(1, editor)
	m.Drag(3, editor)
	m.ResetInputSelection()
	if m.Drag(5, editor) {
		t.Fatal("motion after cancellation changed the selection")
	}
	if got := m.ApplyHighlight("> hello", "hello"); got != "> hello" {
		t.Fatalf("cancelled drag still highlighted: %q", got)
	}
}

func TestScenarioWideRuneDragAndReplacement(t *testing.T) {
	// Scenario: explicit screen coordinates select complete UTF-8 runes and a
	// replacement moves the editor using rune columns, not bytes.
	m := &Model{}
	editor := &scenarioEditor{value: "你好世界"}
	m.Begin(inputselection.OffsetAt(editor.Value(), 1, 10, 6, 10), editor)
	m.Drag(inputselection.OffsetAt(editor.Value(), 1, 10, 10, 10), editor)
	m.End(editor.Value(), len(editor.Value()), true)
	request, ok := m.Copy(editor.Value())
	if !ok || request.Text != "世界" {
		t.Fatalf("wide-rune copy = (%q,%v), want 世界", request.Text, ok)
	}
	want := "> 你好" + highlightOpen + "世界" + highlightClose
	if got := m.ApplyHighlight("> 你好世界", editor.Value()); got != want {
		t.Fatalf("wide-rune render = %q, want %q", got, want)
	}
	m.ReplaceInputSelection(editor, "朋友")
	if editor.value != "你好朋友" || editor.line != 0 || editor.col != 4 {
		t.Fatalf("wide-rune edit value=%q cursor=(%d,%d)", editor.value, editor.line, editor.col)
	}
}

// --- double click (sub-behaviour 3) -----------------------------------

func TestClickAtCountsASecondPressInsideTheWindow(t *testing.T) {
	// Scenario: two presses on the same offset inside
	// DoubleClickWindowNanos are a double-click; a press elsewhere, or
	// one after the window, starts over. The bubbletea fork reports no
	// click count (its Mouse has only X, Y, Button and Mod), so this is
	// where the count comes from.
	m := &Model{}
	const t0 int64 = 1_800_000_000_000_000_000
	if got := m.ClickAt(4, t0); got != 1 {
		t.Fatalf("first press = %d, want 1", got)
	}
	if got := m.ClickAt(4, t0+50*1_000_000); got != 2 {
		t.Fatalf("second press on the same offset = %d, want 2", got)
	}
	if got := m.ClickAt(9, t0+60*1_000_000); got != 1 {
		t.Fatalf("press on a different offset = %d, want 1", got)
	}
	if got := m.ClickAt(9, t0+60*1_000_000+DoubleClickWindowNanos); got != 1 {
		t.Fatalf("late press = %d, want 1", got)
	}
	m.ResetClicks()
	if m.ClickCount() != 0 {
		t.Fatalf("ResetClicks left count %d", m.ClickCount())
	}
}

func TestScenarioDoubleClickSelectsTheWord(t *testing.T) {
	// Scenario: a double-click on a word selects it whole — through the
	// composer's own three-way word split, so "hello world" at offset 8
	// selects "world" and a press on "://" selects the punctuation run.
	m := &Model{}
	editor := &scenarioEditor{value: "hello world"}
	const t0 int64 = 1_800_000_000_000_000_000
	m.ClickAt(8, t0)
	if count := m.ClickAt(8, t0+40*1_000_000); count != 2 {
		t.Fatalf("click count = %d, want 2", count)
	}
	m.BeginWord(editor.Value(), 8, editor)
	start, end := m.InputSelectionRange()
	if start != 6 || end != 11 {
		t.Fatalf("word range = [%d,%d), want [6,11)", start, end)
	}
	if editor.value[start:end] != "world" {
		t.Fatalf("selected %q, want world", editor.value[start:end])
	}
	if editor.col != 6 {
		t.Fatalf("cursor col = %d, want the word's start (6)", editor.col)
	}
	m.End(editor.Value(), 8, true)
	if request, ok := m.Copy(editor.Value()); !ok || request.Text != "world" {
		t.Fatalf("copy after double click = (%q,%v), want (world,true)", request.Text, ok)
	}
}

func TestWordBoundsSelectsWholeWords(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		offset       int
		wantStart    int
		wantEnd      int
		wantSelected string
	}{
		{"word start", "hello world", 0, 0, 5, "hello"},
		{"word middle", "hello world", 3, 0, 5, "hello"},
		{"second word", "hello world", 8, 6, 11, "world"},
		{"end of value", "hello world", 11, 6, 11, "world"},
		{"past the end", "hello world", 40, 6, 11, "world"},
		{"underscore joins", "snake_case_name", 4, 0, 15, "snake_case_name"},
		{"punctuation run", "see http://example", 9, 8, 11, "://"},
		{"url host", "see http://example", 4, 4, 8, "http"},
		{"slash run", "a/b/c", 1, 1, 2, "/"},
		{"wide runes are one word", "你好世界", 10, 0, 12, "你好世界"},
		{"cjk punctuation splits", "你好，世界", 12, 9, 15, "世界"},
		{"second line", "one\ntwo", 5, 4, 7, "two"},
		{"line end", "one\ntwo", 3, 0, 3, "one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := WordBounds(tc.value, tc.offset)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("bounds = [%d,%d), want [%d,%d)", start, end, tc.wantStart, tc.wantEnd)
			}
			if got := tc.value[start:end]; got != tc.wantSelected {
				t.Fatalf("selected %q, want %q", got, tc.wantSelected)
			}
		})
	}
}

func TestWordBoundsLeavesGapsAlone(t *testing.T) {
	// Scenario: whitespace is not a word, and neither is an empty line.
	// A double-click in a gap keeps the plain caret position, so the
	// following drag still selects from wherever the user pointed.
	for _, tc := range []struct {
		name   string
		value  string
		offset int
	}{
		{"space between words", "hello world", 5},
		{"empty value", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end := WordBounds(tc.value, tc.offset)
			if start != end {
				t.Fatalf("bounds = [%d,%d), want an empty range", start, end)
			}
		})
	}
}

// --- double-click-drag (sub-behaviour 3, second half) -----------------

func TestScenarioDoubleClickDragExtendsByWord(t *testing.T) {
	// Scenario: press on "alpha", press again to pick the word, then drag
	// right. The range grows to the end of each word the pointer reaches,
	// and never ends mid-word.
	m := &Model{}
	editor := &scenarioEditor{value: "alpha beta gamma delta"}
	m.BeginWord(editor.Value(), 2, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 5 {
		t.Fatalf("anchor word = [%d,%d), want [0,5)", start, end)
	}

	// Drag into the middle of "beta": the whole word joins.
	m.Drag(8, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 10 {
		t.Fatalf("after dragging into beta = [%d,%d), want [0,10)", start, end)
	}
	// Drag into "gamma": "beta gamma" is selected.
	m.Drag(13, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 16 {
		t.Fatalf("after dragging into gamma = [%d,%d), want [0,16)", start, end)
	}
	// Drag back into "beta": "alpha beta" is selected — the range still
	// stops at the pointer's word's end, not at the pointer itself.
	m.Drag(6, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 10 {
		t.Fatalf("after dragging back into beta = [%d,%d), want [0,10)", start, end)
	}
	// Drag back into the anchor word: only "alpha" remains.
	m.Drag(2, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 5 {
		t.Fatalf("after dragging back into alpha = [%d,%d), want [0,5)", start, end)
	}
	// And into the gap right after "alpha": still the anchor word.
	m.Drag(5, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 5 {
		t.Fatalf("dragging into the gap after alpha = [%d,%d), want [0,5)", start, end)
	}

	// A word drag's release keeps the word range, not the raw pointer.
	if !m.End(editor.Value(), 7, true) {
		t.Fatal("word drag release left no highlight")
	}
	if start, end := m.InputSelectionRange(); start != 0 || end != 5 {
		t.Fatalf("release widened the range to [%d,%d), want [0,5)", start, end)
	}
	if request, ok := m.Copy(editor.Value()); !ok || request.Text != "alpha" {
		t.Fatalf("copy = (%q,%v), want (alpha,true)", request.Text, ok)
	}
}

func TestScenarioDoubleClickDragBackwardExtendsByWord(t *testing.T) {
	// Scenario: the anchor is "gamma" and the pointer moves left, so the
	// range grows back a word at a time while keeping the anchor word.
	m := &Model{}
	editor := &scenarioEditor{value: "alpha beta gamma"}
	m.BeginWord(editor.Value(), 13, editor)
	if start, end := m.InputSelectionRange(); start != 11 || end != 16 {
		t.Fatalf("anchor word = [%d,%d), want [11,16)", start, end)
	}
	m.Drag(7, editor)
	if start, end := m.InputSelectionRange(); start != 6 || end != 16 {
		t.Fatalf("dragging back into beta = [%d,%d), want [6,16)", start, end)
	}
	m.Drag(1, editor)
	if start, end := m.InputSelectionRange(); start != 0 || end != 16 {
		t.Fatalf("dragging back into alpha = [%d,%d), want [0,16)", start, end)
	}
	if request, ok := m.Copy(editor.Value()); !ok || request.Text != "alpha beta gamma" {
		t.Fatalf("copy = (%q,%v), want the whole value", request.Text, ok)
	}
}

func TestScenarioBeginWordWithoutAWordIsAPlainDrag(t *testing.T) {
	// Scenario: a double-click in the gap between words has no word to
	// take, so it falls back to the ordinary drag and a following motion
	// still selects.
	m := &Model{}
	editor := &scenarioEditor{value: "hello world"}
	m.BeginWord(editor.Value(), 5, editor)
	if m.WordDragging() {
		t.Fatal("a gap started a word drag")
	}
	m.Drag(11, editor)
	if start, end := m.InputSelectionRange(); start != 5 || end != 11 {
		t.Fatalf("plain drag after a gap press = [%d,%d), want [5,11)", start, end)
	}
}
