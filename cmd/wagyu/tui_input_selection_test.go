package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// setupInputMode creates a runMode in altScreen mode with an initialized terminal size.
func setupInputMode(val string) *runMode {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue(val)
	// Render once to populate promptScreenY
	_ = m.View()
	return m
}

func TestInputSelection_MouseClickWithoutDrag(t *testing.T) {
	m := setupInputMode("hello world")
	if m.promptScreenY < 0 {
		t.Fatalf("promptScreenY should be >= 0, got %d", m.promptScreenY)
	}

	// Click at 'w' (col = 2 (prompt "> ") + 6 ("hello ") = 8)
	m.Update(mouseClick(8, m.promptScreenY, tea.MouseLeft))
	if !m.inputSelDragging {
		t.Errorf("expected inputSelDragging to be true during click")
	}

	// Release without moving
	_, cmd := m.Update(mouseRelease(8, m.promptScreenY, tea.MouseLeft))
	if cmd != nil {
		t.Errorf("expected nil cmd for click without drag, got %v", cmd)
	}
	if m.hasInputSelection() {
		t.Errorf("expected no selection after click without drag")
	}
	if m.input.Column() != 6 {
		t.Errorf("expected cursor column 6, got %d", m.input.Column())
	}
}

func TestInputSelection_MouseDragForwardAutoCopy(t *testing.T) {
	m := setupInputMode("hello world")

	// Click at start of "hello" (x=2)
	m.Update(mouseClick(2, m.promptScreenY, tea.MouseLeft))
	// Drag to end of "hello" (x=7)
	m.Update(mouseMotion(7, m.promptScreenY))

	if !m.hasInputSelection() {
		t.Fatalf("expected active input selection during drag")
	}
	start, end := m.inputSelectionRange()
	if start != 0 || end != 5 {
		t.Fatalf("expected range [0, 5], got [%d, %d]", start, end)
	}

	// Verify visual highlight in View()
	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7mhello\x1b[0m") {
		t.Errorf("expected reverse video highlight for 'hello', got: %q", viewStr)
	}

	// Release: should auto-copy "hello" to clipboard
	_, cmd := m.Update(mouseRelease(7, m.promptScreenY, tea.MouseLeft))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on release")
	}
	// Selection stays active after release
	if !m.hasInputSelection() {
		t.Errorf("expected selection to remain highlighted after mouse release")
	}
}

func TestInputSelection_MouseDragBackwardAutoCopy(t *testing.T) {
	m := setupInputMode("hello world")

	// Click at end of "hello" (x=7)
	m.Update(mouseClick(7, m.promptScreenY, tea.MouseLeft))
	// Drag backward to start of "hello" (x=2)
	m.Update(mouseMotion(2, m.promptScreenY))

	start, end := m.inputSelectionRange()
	if start != 0 || end != 5 {
		t.Fatalf("expected normalized range [0, 5], got [%d, %d]", start, end)
	}

	// Release
	_, cmd := m.Update(mouseRelease(2, m.promptScreenY, tea.MouseLeft))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on release")
	}
}

func TestInputSelection_DragOutsideClamps(t *testing.T) {
	m := setupInputMode("hello")

	// Click at x=2
	m.Update(mouseClick(2, m.promptScreenY, tea.MouseLeft))

	// Drag way beyond right wall and below screen
	m.Update(mouseMotion(150, 40))
	start, end := m.inputSelectionRange()
	if start != 0 || end != len("hello") {
		t.Errorf("expected clamped [0, 5], got [%d, %d]", start, end)
	}

	// Drag way above and to the left
	m.Update(mouseMotion(-10, -5))
	start, end = m.inputSelectionRange()
	if start != 0 || end != 0 {
		t.Errorf("expected clamped [0, 0], got [%d, %d]", start, end)
	}
}

func TestInputSelection_CmdA_SelectAllAndAutoCopy(t *testing.T) {
	m := setupInputMode("echo 123")

	// Press Cmd+A (ModSuper)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModSuper}))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on Cmd+A")
	}
	if !m.hasInputSelection() {
		t.Fatalf("expected active input selection on Cmd+A")
	}
	start, end := m.inputSelectionRange()
	if start != 0 || end != len("echo 123") {
		t.Errorf("expected [0, 8], got [%d, %d]", start, end)
	}

	// Verify reverse-video highlight over all text
	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7mecho 123\x1b[0m") {
		t.Errorf("expected reverse video highlight for 'echo 123', got: %q", viewStr)
	}
}

func TestInputSelection_CtrlShiftA_SelectAll(t *testing.T) {
	m := setupInputMode("cat file.txt")

	// Press Ctrl+Shift+A
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl | tea.ModShift}))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on Ctrl+Shift+A")
	}
	if !m.hasInputSelection() {
		t.Fatalf("expected active input selection on Ctrl+Shift+A")
	}
	start, end := m.inputSelectionRange()
	if start != 0 || end != len("cat file.txt") {
		t.Errorf("expected [0, 12], got [%d, %d]", start, end)
	}
}

func TestInputSelection_BackspaceDeletesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "world" [6, 11]
	m.inputSelStart = 6
	m.inputSelEnd = 11

	// Press Backspace
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if m.hasInputSelection() {
		t.Errorf("expected selection cleared after backspace")
	}
	if got := m.input.Value(); got != "hello " {
		t.Errorf("expected 'hello ', got %q", got)
	}
}

func TestInputSelection_DeleteDeletesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "hello " [0, 6]
	m.inputSelStart = 0
	m.inputSelEnd = 6

	// Press Delete
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDelete}))
	if m.hasInputSelection() {
		t.Errorf("expected selection cleared after delete")
	}
	if got := m.input.Value(); got != "world" {
		t.Errorf("expected 'world', got %q", got)
	}
}

func TestInputSelection_TypeReplacesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "world" [6, 11]
	m.inputSelStart = 6
	m.inputSelEnd = 11

	// Type '!'
	m.Update(tea.KeyPressMsg(tea.Key{Code: '!', Text: "!"}))
	if m.hasInputSelection() {
		t.Errorf("expected selection cleared after typing")
	}
	if got := m.input.Value(); got != "hello !" {
		t.Errorf("expected 'hello !', got %q", got)
	}
}

func TestInputSelection_PasteReplacesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "world" [6, 11]
	m.inputSelStart = 6
	m.inputSelEnd = 11

	// Paste "there"
	m.insertPastedText("there")
	if m.hasInputSelection() {
		t.Errorf("expected selection cleared after paste")
	}
	if got := m.input.Value(); got != "hello there" {
		t.Errorf("expected 'hello there', got %q", got)
	}
}

func TestInputSelection_EscapeClearsSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select all via Cmd+A
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModSuper}))
	if !m.hasInputSelection() {
		t.Fatalf("expected active selection")
	}

	// Press Esc
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.hasInputSelection() {
		t.Errorf("expected selection cleared after Esc")
	}
	// Buffer should NOT be cleared by the first Esc when selection was active
	if got := m.input.Value(); got != "hello world" {
		t.Errorf("expected buffer preserved after Esc-cleared selection, got %q", got)
	}
}

func TestInputSelection_ArrowKeysClearSelection(t *testing.T) {
	m := setupInputMode("hello world")

	m.inputSelStart = 0
	m.inputSelEnd = 5

	// Press Left arrow
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if m.hasInputSelection() {
		t.Errorf("expected selection cleared after KeyLeft")
	}
	if col := m.input.Column(); col != 0 {
		t.Errorf("expected cursor at col 0, got %d", col)
	}
}

func TestInputSelection_MultiLine(t *testing.T) {
	m := setupInputMode("first line\nsecond line")
	if m.inputLineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", m.inputLineCount())
	}

	// Select all
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModSuper}))
	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7mfirst line\x1b[0m") {
		t.Errorf("expected highlight on first line, got: %q", viewStr)
	}
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7msecond line\x1b[0m") {
		t.Errorf("expected highlight on second line, got: %q", viewStr)
	}
}

func TestInputSelection_UTF8Accuracy(t *testing.T) {
	m := setupInputMode("你好世界")

	// Click at start of "世界": prompt "> " (2 cells) + "你好" (4 cells) = cell 6
	m.Update(mouseClick(6, m.promptScreenY, tea.MouseLeft))
	// Drag to end of "世界": cell 6 + 4 = 10
	m.Update(mouseMotion(10, m.promptScreenY))

	start, end := m.inputSelectionRange()
	val := m.input.Value()
	if val[start:end] != "世界" {
		t.Errorf("expected '世界' selected, got %q (range %d..%d)", val[start:end], start, end)
	}

	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7m世界\x1b[0m") {
		t.Errorf("expected reverse video highlight for '世界', got: %q", viewStr)
	}
}
