package composer

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/foundations/history"
)

// fakeEditor is an Editor the test drives: it holds the value, the
// cursor and the calls the composer made.
type fakeEditor struct {
	value   string
	line    int
	col     int
	offset  int
	applied []tea.Msg
	moves   []move
}

// move is one MoveCursorTo call.
type move struct {
	line, col int
}

func (e *fakeEditor) Value() string         { return e.value }
func (e *fakeEditor) SetValue(s string)     { e.value = s }
func (e *fakeEditor) Line() int             { return e.line }
func (e *fakeEditor) Column() int           { return e.col }
func (e *fakeEditor) CursorOffset() int     { return e.offset }
func (e *fakeEditor) InsertRune(r rune)     { e.value += string(r) }
func (e *fakeEditor) Backspace()            { e.value = e.value[:len(e.value)-1] }
func (e *fakeEditor) MoveCursorTo(l, c int) { e.moves = append(e.moves, move{l, c}) }

func (e *fakeEditor) Update(msg tea.Msg) tea.Cmd {
	e.applied = append(e.applied, msg)
	if key, ok := msg.(tea.KeyPressMsg); ok && key.Text != "" {
		e.value += key.Text
	}
	return nil
}

// fakeState is what the seams' callbacks record.
type fakeState struct {
	selectAll    int
	clearSel     int
	deleteSel    int
	replaceSel   []string
	copied       []string
	cmd          tea.Cmd
	cmdCalls     int
	notice       int
	modeHint     int
	historyReset int
}

// newSeams builds the seams over a fake editor and a fake state.
func newSeams(e *fakeEditor, st *fakeState, sel Range, hist *history.List) Seams {
	st.cmd = func() tea.Msg { st.cmdCalls++; return nil }
	return Seams{
		Editor:           e,
		History:          hist,
		Selection:        sel,
		SelectAll:        func() (tea.Cmd, bool) { st.selectAll++; return st.cmd, true },
		ClearSelection:   func() { st.clearSel++ },
		DeleteSelection:  func() { st.deleteSel++ },
		ReplaceSelection: func(s string) { st.replaceSel = append(st.replaceSel, s) },
		Copy: func(text string) tea.Cmd {
			st.copied = append(st.copied, text)
			return st.cmd
		},
		ClearEligibilityNotice: func() { st.notice++ },
		DismissModeHint:        func() { st.modeHint++ },
	}
}

// TestHandleKeySelectAll pins the select-all key.
func TestHandleKeySelectAll(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	cmd, handled := HandleKey(seams, tea.Key{Code: 'a', Mod: tea.ModSuper})
	if !handled {
		t.Fatalf("select all was not handled")
	}
	if st.selectAll != 1 {
		t.Errorf("select all = %d, want 1", st.selectAll)
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want the host's command")
	}
	cmd()
	if st.cmdCalls != 1 {
		t.Errorf("the returned command is not the host's own")
	}
}

// TestHandleKeySelectAllEmptyValue pins that the key is consumed even
// when there is nothing to select.
func TestHandleKeySelectAllEmptyValue(t *testing.T) {
	e := &fakeEditor{value: ""}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)
	seams.SelectAll = func() (tea.Cmd, bool) { st.selectAll++; return nil, false }

	_, handled := HandleKey(seams, tea.Key{Code: 'a', Mod: tea.ModSuper})
	if !handled {
		t.Fatalf("select all was not handled")
	}
	if st.selectAll != 1 {
		t.Errorf("select all = %d, want 1", st.selectAll)
	}
}

// TestHandleKeyCopy pins that the copy key copies the selection and
// nothing else.
func TestHandleKeyCopy(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{Start: 1, End: 4}, nil)

	cmd, handled := HandleKey(seams, tea.Key{Code: 'c', Mod: tea.ModSuper})
	if !handled {
		t.Fatalf("copy was not handled")
	}
	if len(st.copied) != 1 || st.copied[0] != "ell" {
		t.Errorf("copied = %v, want [ell]", st.copied)
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want the host's command")
	}
	cmd()
	if st.cmdCalls != 1 {
		t.Errorf("the returned command is not the host's own")
	}
	if st.deleteSel != 0 || st.selectAll != 0 {
		t.Errorf("the copy key edited the value")
	}
}

// TestHandleKeyCopyWithoutSelection pins that the copy key is
// consumed with nothing selected.
func TestHandleKeyCopyWithoutSelection(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: 'c', Mod: tea.ModSuper})
	if !handled {
		t.Fatalf("copy was not handled")
	}
	if len(st.copied) != 0 {
		t.Errorf("copied = %v, want nothing", st.copied)
	}
}

// TestHandleKeyDeleteSelection pins the delete key's tail: the
// selection goes, the mode hint goes, the notice goes and the history
// is reset.
func TestHandleKeyDeleteSelection(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	var hist history.List
	hist.Push("old")
	hist.Up("current")
	seams := newSeams(e, st, Range{Start: 0, End: 2}, &hist)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyBackspace})
	if !handled {
		t.Fatalf("backspace was not handled")
	}
	if st.deleteSel != 1 {
		t.Errorf("delete selection = %d, want 1", st.deleteSel)
	}
	if st.modeHint != 1 {
		t.Errorf("mode hint = %d, want 1", st.modeHint)
	}
	if st.notice != 1 {
		t.Errorf("eligibility notice = %d, want 1", st.notice)
	}
	if hist.Index() != 0 {
		t.Errorf("history index = %d, want 0", hist.Index())
	}
}

// TestHandleKeyPlaceholder pins that a backspace inside a pasted-text
// placeholder removes the whole placeholder.
func TestHandleKeyPlaceholder(t *testing.T) {
	const value = "a [Pasted text #1 +16 lines] b"
	e := &fakeEditor{value: value, offset: 12}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyBackspace})
	if !handled {
		t.Fatalf("backspace was not handled")
	}
	want := "a  b"
	if e.value != want {
		t.Errorf("value = %q, want %q", e.value, want)
	}
	if len(e.moves) != 1 || e.moves[0].line != 0 || e.moves[0].col != 2 {
		t.Errorf("moves = %v, want one move to 0,2", e.moves)
	}
}

// TestHandleKeyPlaceholderDelete pins the delete key's half of the
// placeholder scan.
func TestHandleKeyPlaceholderDelete(t *testing.T) {
	const value = "a [Pasted text #1 42 chars] b"
	e := &fakeEditor{value: value, offset: 5}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyDelete})
	if !handled {
		t.Fatalf("delete was not handled")
	}
	if want := "a  b"; e.value != want {
		t.Errorf("value = %q, want %q", e.value, want)
	}
}

// TestHandleKeyPlaceholderOutside pins that a key outside a
// placeholder leaves it alone.
func TestHandleKeyPlaceholderOutside(t *testing.T) {
	const value = "a [Pasted text #1 42 chars] b"
	e := &fakeEditor{value: value, offset: 2}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyBackspace})
	if handled {
		t.Fatalf("backspace outside the placeholder was handled")
	}
	if e.value != value {
		t.Errorf("value = %q, want %q", e.value, value)
	}
}

// TestHandleKeyCollapse pins that an arrow key collapses the
// selection to the edge it names.
func TestHandleKeyCollapse(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{Start: 1, End: 4}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyLeft})
	if !handled {
		t.Fatalf("left was not handled")
	}
	if st.clearSel != 1 {
		t.Errorf("clear selection = %d, want 1", st.clearSel)
	}
	if len(e.moves) != 1 || e.moves[0].col != 1 {
		t.Errorf("moves = %v, want one move to column 1", e.moves)
	}
	if st.modeHint != 0 || st.notice != 0 {
		t.Errorf("the collapse dismissed the mode hint or the notice")
	}
}

// TestHandleKeyCollapseRight pins the right arrow's edge.
func TestHandleKeyCollapseRight(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{Start: 1, End: 4}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatalf("right was not handled")
	}
	if len(e.moves) != 1 || e.moves[0].col != 4 {
		t.Errorf("moves = %v, want one move to column 4", e.moves)
	}
}

// TestHandleKeyShiftArrowKeepsSelection pins that a shifted arrow key
// is not the composer's.
func TestHandleKeyShiftArrowKeepsSelection(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{Start: 1, End: 4}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: tea.KeyLeft, Mod: tea.ModShift})
	if handled {
		t.Fatalf("shift+left was handled")
	}
	if st.clearSel != 0 {
		t.Errorf("clear selection = %d, want 0", st.clearSel)
	}
}

// TestHandleKeyReplace pins that a printable key replaces the
// selection.
func TestHandleKeyReplace(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{Start: 1, End: 4}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: 'x', Text: "x"})
	if !handled {
		t.Fatalf("the printable key was not handled")
	}
	if len(st.replaceSel) != 1 || st.replaceSel[0] != "x" {
		t.Errorf("replaced = %v, want [x]", st.replaceSel)
	}
	if st.modeHint != 1 || st.notice != 1 {
		t.Errorf("the replacement did not dismiss the mode hint or the notice")
	}
}

// TestHandleKeyUnowned pins that a key the composer does not own is
// left to the host.
func TestHandleKeyUnowned(t *testing.T) {
	e := &fakeEditor{value: "hello"}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	_, handled := HandleKey(seams, tea.Key{Code: 'x', Text: "x"})
	if handled {
		t.Fatalf("the printable key was handled with no selection")
	}
	if st.deleteSel != 0 || st.selectAll != 0 || len(st.copied) != 0 {
		t.Errorf("the key edited the composer")
	}
}

// TestBackslashNewline pins the fallback: a backslash before the
// cursor becomes a newline.
func TestBackslashNewline(t *testing.T) {
	e := &fakeEditor{value: "a\\", offset: 2}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	if !BackslashNewline(seams) {
		t.Fatalf("the fallback did not apply")
	}
	if e.value != "a\n" {
		t.Errorf("value = %q, want %q", e.value, "a\n")
	}
	if st.modeHint != 1 {
		t.Errorf("mode hint = %d, want 1", st.modeHint)
	}
}

// TestBackslashNewlineWithoutBackslash pins that the fallback does not
// apply without a backslash.
func TestBackslashNewlineWithoutBackslash(t *testing.T) {
	e := &fakeEditor{value: "ab", offset: 2}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)

	if BackslashNewline(seams) {
		t.Fatalf("the fallback applied without a backslash")
	}
	if e.value != "ab" {
		t.Errorf("value = %q, want %q", e.value, "ab")
	}
}

// TestUpdateTextarea pins the update's tail: the dropdowns follow the
// value and the history's navigation is reset when the text changed.
func TestUpdateTextarea(t *testing.T) {
	e := &fakeEditor{value: "old"}
	st := &fakeState{}
	var hist history.List
	hist.Push("one")
	hist.Up("current")
	seams := newSeams(e, st, Range{}, &hist)

	cmd := UpdateTextarea(seams, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if len(e.applied) != 1 {
		t.Errorf("applied = %v, want one message", e.applied)
	}
	if e.value != "oldx" {
		t.Errorf("value = %q, want %q", e.value, "oldx")
	}
	if st.modeHint != 1 || st.notice != 1 {
		t.Errorf("the update did not dismiss the mode hint or the notice")
	}
	if hist.Index() != 0 {
		t.Errorf("history index = %d, want 0", hist.Index())
	}
}

// TestUpdateTextareaKeepsIdleHistory pins that an idle history is
// left alone.
func TestUpdateTextareaKeepsIdleHistory(t *testing.T) {
	e := &fakeEditor{value: "old"}
	st := &fakeState{}
	var hist history.List
	seams := newSeams(e, st, Range{}, &hist)

	UpdateTextarea(seams, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if hist.Index() != 0 {
		t.Errorf("history index = %d, want 0", hist.Index())
	}
}
