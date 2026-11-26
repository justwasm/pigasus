package tui

// tui_prompt_mouse_test.go — agy 1.5.17's prompt-box mouse selection,
// driven the way a terminal drives it: a mouse/key message in, the
// selection state, the composer buffer and the rendered prompt out.
//
// The changelog entry, verbatim:
//
//	Added mouse cursor placement and text selection inside the prompt box
//	in the `full screen (altscreen)` rendering mode: click to move the
//	cursor, or drag and double-click (and double-click-drag by word) to
//	highlight text for typing or paste replacement, `Backspace`/`Delete`,
//	`Ctrl+C` copy, and `Ctrl+X` cut without overwriting your clipboard on
//	release. When Vim mode is on, mouse selections outside Insert mode
//	enter characterwise Visual mode so Vim operators and motions act on
//	the selection, and Normal-mode clicks past the end of a line land on
//	the last character.
//
// Each test names the sub-behaviour it pins.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/vim"
)

// promptMouseModel is a RootModel in alt-screen mode holding text in the
// composer, rendered once so promptScreenY is populated, with the
// default keybindings loaded (Ctrl+C is cli.escape, which is the chord
// the copy must win over).
func promptMouseModel(t *testing.T, text string) *RootModel {
	t.Helper()
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.prompt.input.SetValue(text)
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"cli.escape":["ctrl+c","esc"],"cli.exit":["ctrl+d"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, err := keybind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m.prompt.keybindings = kb
	_ = m.View()
	if m.prompt.promptScreenY < 0 {
		t.Fatalf("promptScreenY = %d, the composer is not on screen", m.prompt.promptScreenY)
	}
	return m
}

// recordClipboard swaps the clipboard's write seam and returns the
// writes observed so far.
func recordClipboard(t *testing.T) *[]string {
	t.Helper()
	writes := &[]string{}
	prev := clipboard.Writer
	clipboard.Writer = func(text string) error {
		*writes = append(*writes, text)
		return nil
	}
	t.Cleanup(func() { clipboard.Writer = prev })
	return writes
}

// pressClick and friends drive one press / motion / release the way a
// terminal does, with the fork's own message types.
func pressClick(m *RootModel, x, y int)  { m.Update(mouseClick(x, y, tea.MouseLeft)) }
func pressMotion(m *RootModel, x, y int) { m.Update(mouseMotion(x, y)) }
func pressRelease(m *RootModel, x, y int) {
	m.Update(mouseRelease(x, y, tea.MouseLeft))
}

// clickOffset clicks the composer at the screen column that maps to
// byte offset in its value ("hello" at offset 8 is cell 2+8).
func clickOffset(m *RootModel, offset int) {
	pressClick(m, 2+offset, m.prompt.promptScreenY)
}

func TestPromptMouse_1_ClickPlacesTheCursor(t *testing.T) {
	// Sub-behaviour 1: click to move the cursor.
	m := promptMouseModel(t, "hello world")
	clickOffset(m, 8)
	if got := m.prompt.input.Column(); got != 8 {
		t.Fatalf("cursor column after a click at offset 8 = %d, want 8", got)
	}
	pressRelease(m, 2+8, m.prompt.promptScreenY)
	// A click without a drag selects nothing and leaves the caret put.
	if m.prompt.HasInputSelection() {
		t.Fatal("a bare click left a selection")
	}
	if got := m.prompt.input.Column(); got != 8 {
		t.Fatalf("cursor column after release = %d, want 8", got)
	}
	// Typing lands at the clicked position, not at the end of the buffer.
	m.Update(tea.KeyPressMsg(tea.Key{Code: '!', Text: "!"}))
	if got := m.prompt.input.Value(); got != "hello wo!rld" {
		t.Fatalf("after typing = %q, want 'hello wo!rld'", got)
	}
}

func TestPromptMouse_2_DragSelectsAndHighlights(t *testing.T) {
	// Sub-behaviour 2: drag to select. The rendered prompt carries the
	// reverse-video cue over exactly the dragged bytes.
	m := promptMouseModel(t, "hello world")
	clickOffset(m, 0)
	pressMotion(m, 2+5, m.prompt.promptScreenY)

	start, end := m.prompt.InputSelectionRange()
	if start != 0 || end != 5 {
		t.Fatalf("drag range = [%d,%d), want [0,5)", start, end)
	}
	if got := m.prompt.input.Value()[start:end]; got != "hello" {
		t.Fatalf("dragged %q, want hello", got)
	}
	view := m.View().Content
	if !strings.Contains(view, "\x1b[0m\x1b[7mhello\x1b[0m") {
		t.Fatalf("rendered prompt is missing the reverse-video highlight; got:\n%s", view)
	}
}

func TestPromptMouse_3_DoubleClickSelectsTheWord(t *testing.T) {
	// Sub-behaviour 3: double-click selects a word.
	m := promptMouseModel(t, "hello world")
	// Two presses on the same cell, back to back: the click tracker sees
	// the second one inside DoubleClickWindow.
	clickOffset(m, 8)
	pressRelease(m, 2+8, m.prompt.promptScreenY)
	clickOffset(m, 8)
	pressRelease(m, 2+8, m.prompt.promptScreenY)

	start, end := m.prompt.InputSelectionRange()
	if m.prompt.input.Value()[start:end] != "world" {
		t.Fatalf("double-click selected %q, want 'world'", m.prompt.input.Value()[start:end])
	}
	view := m.View().Content
	if !strings.Contains(view, "\x1b[0m\x1b[7mworld\x1b[0m") {
		t.Fatalf("rendered prompt is missing the word highlight; got:\n%s", view)
	}
}

func TestPromptMouse_3_DoubleClickDragExtendsByWord(t *testing.T) {
	// Sub-behaviour 3: double-click-drag by word.
	m := promptMouseModel(t, "alpha beta gamma")
	clickOffset(m, 2)
	pressRelease(m, 2+2, m.prompt.promptScreenY)
	clickOffset(m, 2) // second press: "alpha" whole
	if start, end := m.prompt.InputSelectionRange(); m.prompt.input.Value()[start:end] != "alpha" {
		t.Fatalf("after the double-click = %q, want alpha", m.prompt.input.Value()[start:end])
	}
	// Drag into the middle of "gamma": both words join, whole.
	pressMotion(m, 2+13, m.prompt.promptScreenY)
	start, end := m.prompt.InputSelectionRange()
	if got := m.prompt.input.Value()[start:end]; got != "alpha beta gamma" {
		t.Fatalf("after dragging into gamma = %q, want 'alpha beta gamma'", got)
	}
	// The release keeps the word ranges.
	pressRelease(m, 2+12, m.prompt.promptScreenY)
	start, end = m.prompt.InputSelectionRange()
	if got := m.prompt.input.Value()[start:end]; got != "alpha beta gamma" {
		t.Fatalf("after the release = %q, want 'alpha beta gamma'", got)
	}
}

func TestPromptMouse_4_BackspaceAndDeleteRemoveTheSelection(t *testing.T) {
	// Sub-behaviour 4: Backspace/Delete operate on the selection.
	for _, tc := range []struct {
		name string
		key  tea.Key
		want string
	}{
		{"backspace", tea.Key{Code: tea.KeyBackspace}, " world"},
		{"delete", tea.Key{Code: tea.KeyDelete}, " world"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := promptMouseModel(t, "hello world")
			// Select "hello" with the mouse, release, then press the key.
			clickOffset(m, 0)
			pressMotion(m, 2+5, m.prompt.promptScreenY)
			pressRelease(m, 2+5, m.prompt.promptScreenY)

			m.Update(tea.KeyPressMsg(tc.key))
			if got := m.prompt.input.Value(); got != tc.want {
				t.Fatalf("after %s = %q, want %q", tc.name, got, tc.want)
			}
			if m.prompt.HasInputSelection() {
				t.Fatalf("%s left the selection active", tc.name)
			}
		})
	}
}

func TestPromptMouse_4_TypingReplacesTheSelection(t *testing.T) {
	// Sub-behaviour 4's other half: "highlight text for typing or paste
	// replacement".
	m := promptMouseModel(t, "hello world")
	clickOffset(m, 6)
	pressMotion(m, 2+11, m.prompt.promptScreenY)
	pressRelease(m, 2+11, m.prompt.promptScreenY)

	m.Update(tea.KeyPressMsg(tea.Key{Code: '!', Text: "!"}))
	if got := m.prompt.input.Value(); got != "hello !" {
		t.Fatalf("after typing = %q, want 'hello !'", got)
	}
}

func TestPromptMouse_4_PasteReplacesTheSelection(t *testing.T) {
	// Sub-behaviour 4: paste replacement over a mouse selection.
	m := promptMouseModel(t, "hello world")
	clickOffset(m, 6)
	pressMotion(m, 2+11, m.prompt.promptScreenY)
	pressRelease(m, 2+11, m.prompt.promptScreenY)

	m.insertPastedText("there")
	if got := m.prompt.input.Value(); got != "hello there" {
		t.Fatalf("after a paste = %q, want 'hello there'", got)
	}
}

func TestPromptMouse_5_CtrlCCopiesTheSelection(t *testing.T) {
	// Sub-behaviour 5: Ctrl+C copies. It must win over cli.escape, which
	// the default keybindings bind to ctrl+c: before this, Ctrl+C armed
	// the two-step exit and copied nothing.
	writes := recordClipboard(t)
	m := promptMouseModel(t, "hello world")
	clickOffset(m, 0)
	pressMotion(m, 2+5, m.prompt.promptScreenY)
	pressRelease(m, 2+5, m.prompt.promptScreenY)

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("Ctrl+C over a selection produced no copy command")
	}
	if len(*writes) != 1 || (*writes)[0] != "hello" {
		t.Fatalf("clipboard writes = %q, want [hello]", *writes)
	}
	if m.display.exitConfirm {
		t.Fatal("Ctrl+C armed the exit confirmation instead of copying")
	}
	// The copy leaves the text and the highlight alone.
	if got := m.prompt.input.Value(); got != "hello world" {
		t.Fatalf("Ctrl+C changed the buffer to %q", got)
	}
	if !m.prompt.HasInputSelection() {
		t.Fatal("Ctrl+C dropped the selection; a copy keeps it")
	}
}

func TestPromptMouse_6_CtrlXCutAndReleaseLeavesTheClipboardAlone(t *testing.T) {
	// Sub-behaviour 6, the whole point: Ctrl+X cuts *and* a mouse release
	// never overwrites the clipboard. The scenario seeds the clipboard
	// with something the user had, drags a selection, and shows that:
	//
	//   - the release writes nothing (the clipboard still holds the
	//     user's text),
	//   - Ctrl+X is what puts the selection on the clipboard and removes
	//     it from the composer,
	//   - a second release after the cut still writes nothing.
	writes := recordClipboard(t)

	m := promptMouseModel(t, "hello world")
	clipboardMustHold(t, writes, "PASTE-PENDING")

	clickOffset(m, 0)
	pressMotion(m, 2+5, m.prompt.promptScreenY)
	pressRelease(m, 2+5, m.prompt.promptScreenY)
	if len(*writes) != 1 {
		t.Fatalf("clipboard writes after the drag release = %q, want only the seeded value", *writes)
	}
	if (*writes)[0] != "PASTE-PENDING" {
		t.Fatalf("the release overwrote the clipboard with %q", (*writes)[0])
	}

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("Ctrl+X produced no copy command")
	}
	if got := m.prompt.input.Value(); got != " world" {
		t.Fatalf("after Ctrl+X = %q, want ' world'", got)
	}
	if len(*writes) != 2 || (*writes)[1] != "hello" {
		t.Fatalf("clipboard writes = %q, want the cut's 'hello' on top", *writes)
	}
	if m.prompt.HasInputSelection() {
		t.Fatal("Ctrl+X left the selection active")
	}

	// A further release (the button coming up over the composer) writes
	// nothing more.
	pressRelease(m, 2+5, m.prompt.promptScreenY)
	if len(*writes) != 2 {
		t.Fatalf("a release after the cut wrote to the clipboard: %q", *writes)
	}
}

// clipboardMustHold seeds the clipboard through the same seam the code
// writes to, so "what the user had" is observable in the write log.
func clipboardMustHold(t *testing.T, writes *[]string, text string) {
	t.Helper()
	if err := clipboard.Write(text); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0] != text {
		t.Fatalf("clipboard seed did not land: %q", *writes)
	}
}

func TestPromptMouse_6_CtrlXWithoutSelectionDoesNothing(t *testing.T) {
	// Sub-behaviour 6's guard: with nothing selected Ctrl+X must not
	// write an empty string over the user's clipboard.
	writes := recordClipboard(t)
	m := promptMouseModel(t, "hello world")
	clipboardMustHold(t, writes, "PASTE-PENDING")

	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Mod: tea.ModCtrl}))
	if len(*writes) != 1 {
		t.Fatalf("Ctrl+X with no selection wrote to the clipboard: %q", *writes)
	}
	if got := m.prompt.input.Value(); got != "hello world" {
		t.Fatalf("Ctrl+X with no selection changed the buffer to %q", got)
	}
}

func TestPromptMouse_7_VimModeReportsTheMouseSelectionAsVisual(t *testing.T) {
	// Sub-behaviour 7, first half: with Vim on and the engine outside
	// Insert mode, a mouse drag enters characterwise Visual mode, and a
	// Vim operator acts on the dragged range.
	m := setupVimPromptMouse(t, "hello world")
	clickOffset(m, 0)
	pressMotion(m, 2+5, m.prompt.promptScreenY)
	pressRelease(m, 2+5, m.prompt.promptScreenY)

	if got := m.vimMode(); got != vim.Visual {
		t.Fatalf("mode after the drag = %v, want Visual", got)
	}
	// The mode badge is the rendered proof: "-- VISUAL --".
	if body := stripANSI(m.View().Content); !strings.Contains(body, "-- VISUAL --") {
		t.Fatalf("status row is missing the VISUAL badge; got:\n%s", body)
	}
	// "d" deletes the dragged range, not a phantom caret's word.
	_, cmd := m.handleVimKey(tea.Key{Code: 'd', Text: "d"})
	_ = cmd
	if got := m.prompt.input.Value(); got != " world" {
		t.Fatalf("after d = %q, want ' world'", got)
	}
}

func TestPromptMouse_7_VimInsertModeKeepsThePlainSelection(t *testing.T) {
	// Sub-behaviour 7's boundary: the clause is "outside Insert mode".
	// In Insert mode the drag must stay a plain composer selection and
	// the engine must not leave Insert.
	m := setupVimPromptMouse(t, "hello world")
	m.vimBridge().Engine().Handle(vim.Key{Rune: 'i'})
	if m.vimMode() != vim.Insert {
		t.Fatalf("setup left mode %v, want Insert", m.vimMode())
	}

	clickOffset(m, 0)
	pressMotion(m, 2+5, m.prompt.promptScreenY)
	pressRelease(m, 2+5, m.prompt.promptScreenY)

	if got := m.vimMode(); got != vim.Insert {
		t.Fatalf("the drag left Insert mode (mode = %v)", got)
	}
	if !m.prompt.HasInputSelection() {
		t.Fatal("the plain composer selection was lost")
	}
	start, end := m.prompt.InputSelectionRange()
	if got := m.prompt.input.Value()[start:end]; got != "hello" {
		t.Fatalf("selected %q, want hello", got)
	}
}

func TestPromptMouse_7_NormalModeClickPastEndOfLineLandsOnLastCharacter(t *testing.T) {
	// Sub-behaviour 7, second half: a Normal-mode click past the end of a
	// line lands on the last character, not on a phantom position one
	// past the text. The engine's cursor *and* the textarea's must agree.
	for _, tc := range []struct {
		name      string
		text      string
		wantCol   int
		rowOffset int
		clickCell int
	}{
		{"single word", "abc", 2, 0, 40},
		{"two words", "hello world", 10, 0, 60},
		{"empty second line", "one\n\nthree", 0, 1, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := setupVimPromptMouse(t, tc.text)
			m.Update(mouseClick(tc.clickCell, m.prompt.promptScreenY+tc.rowOffset, tea.MouseLeft))
			eng := m.vimBridge().Engine()
			if eng.Col != tc.wantCol {
				t.Fatalf("engine column = %d, want %d (the last character)", eng.Col, tc.wantCol)
			}
			if got := m.prompt.input.Column(); got != tc.wantCol {
				t.Fatalf("textarea column = %d, want %d — the caret must render where the engine is", got, tc.wantCol)
			}
			if eng.Row != tc.rowOffset {
				t.Fatalf("engine row = %d, want %d", eng.Row, tc.rowOffset)
			}
		})
	}
}

// setupVimPromptMouse is promptMouseModel with Vim mode on and the
// engine seeded from the composer, in Normal mode.
func setupVimPromptMouse(t *testing.T, text string) *RootModel {
	t.Helper()
	m := promptMouseModel(t, text)
	m.SetVimMode(true)
	m.prompt.vimOn = true
	if eng := m.vimBridge().Engine(); eng == nil {
		t.Fatal("vim mode on but no engine")
	} else {
		eng.SetText(text)
		eng.Mode = vim.Normal
	}
	_ = m.View()
	return m
}

// stripANSI drops escape sequences so a test can assert on visible text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		switch {
		case inEsc:
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
				inEsc = false
			}
		case s[i] == 0x1b:
			inEsc = true
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
