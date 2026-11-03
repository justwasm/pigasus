// tui_input_selection.go — mouse selection, auto copy, and select-all in the prompt input.
//
// In addition to in-app transcript text selection (tui_selection.go), the
// reference input box allows selecting text by mouse drag, automatically
// copying the selection to the system clipboard upon mouse release, and
// selecting the entire input buffer via Cmd+A (or Ctrl+Shift+A).
//
// When an input selection is active:
//   - Visual feedback is rendered in reverse video (\x1b[7m...\x1b[0m), matching
//     transcript selection.
//   - Mouse release immediately writes to the clipboard (OSC 52 + pbcopy).
//   - Typing a character or pasting replaces the selected text.
//   - Backspace or Delete removes the selected text.
//   - Arrow keys or Escape clear the selection.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// isInputY reports whether the screen row y falls within the input field.
func (m *runMode) isInputY(y int) bool {
	if m.promptScreenY < 0 {
		return false
	}
	return y >= m.promptScreenY && y < m.promptScreenY+m.inputLineCount()
}

// inputLineCount returns the visual line count of the input field (at least 1).
func (m *runMode) inputLineCount() int {
	if m.input.LineCount() > 0 {
		return m.input.LineCount()
	}
	return 1
}

// hasInputSelection reports whether a non-empty selection is active in the input field.
func (m *runMode) hasInputSelection() bool {
	return m.store.InputSelStart != m.store.InputSelEnd
}

// inputOffsetAt maps screen coordinate (x, y) to a byte offset within m.input.Value().
//
// The arithmetic is the inputselection package's; the host supplies the
// values it cannot know.
func (m *runMode) inputOffsetAt(x, y int) int {
	return inputselection.OffsetAt(m.input.Value(), m.inputLineCount(), m.promptScreenY, x, y)
}

// setInputCursorOffset moves the textarea cursor to the given byte offset in m.input.Value().
//
// The arithmetic is the inputselection package's; the host supplies the
// value it cannot know.
func (m *runMode) setInputCursorOffset(offset int) {
	line, col := inputselection.CursorPosition(m.input.Value(), offset)
	m.input.MoveToBegin()
	for l := 0; l < line; l++ {
		m.input.CursorDown()
	}
	m.input.SetCursorColumn(col)
}

// selectAllInput selects all text in the input field, moves cursor to the end,
// and auto-copies the text to clipboard.
func (m *runMode) selectAllInput() (tea.Cmd, bool) {
	val := m.input.Value()
	if len(val) == 0 {
		return nil, false
	}
	m.store.InputSelStart = 0
	m.store.InputSelEnd = len(val)
	m.setInputCursorOffset(len(val))
	_ = clipboard.Write(val)
	return m.copySelectionCmd(val), true
}

// deleteInputSelection deletes the selected substring from m.input and clears selection.
//
// The value arithmetic is the inputselection package's; the host supplies
// the value and the range, and owns the textarea's write.
func (m *runMode) deleteInputSelection() {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.store.InputSelectionRange()
	val := m.input.Value()
	if newVal, ok := inputselection.DeleteRange(val, start, end); ok {
		m.input.SetValue(newVal)
		m.setInputCursorOffset(start)
	}
	m.store.ResetInputSelection()
}

// replaceInputSelection replaces the selected substring with replacement and clears selection.
//
// The value arithmetic is the inputselection package's; the host supplies
// the value and the range, and owns the textarea's write.
func (m *runMode) replaceInputSelection(replacement string) {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.store.InputSelectionRange()
	val := m.input.Value()
	if newVal, ok := inputselection.ReplaceRange(val, start, end, replacement); ok {
		m.input.SetValue(newVal)
		m.setInputCursorOffset(start + len(replacement))
	}
	m.store.ResetInputSelection()
}

// applyInputSelectionHighlight applies reverse-video styling to the selected
// range within the rendered inputView string.
//
// The arithmetic is the inputselection package's; the host supplies the
// selection's range and the value it is a range in.
func (m *runMode) applyInputSelectionHighlight(inputView string) string {
	start, end := m.store.InputSelectionRange()
	return inputselection.ApplyHighlight(inputView, m.input.Value(), start, end)
}

var clipboardReadText = clipboard.Read

// insertPastedText is the single insertion point for both the terminal's
// bracketed paste and ctrl+v. It never submits.
func (m *runMode) insertPastedText(s string) tea.Cmd {
	s = clipboard.NormalizeText(s)
	if s == "" {
		return nil
	}
	if m.panel.kind == panelregistry.Rename {
		// Cycle 381: the /rename panel owns its input, so the paste
		// goes through the panel.
		return m.renamePanel().HandlePaste(s)
	}
	if m.panel.kind == panelregistry.Provider {
		return m.providerPanel().HandlePaste(s)
	}
	if m.helpPanel.Active() || m.panel.kind != panelregistry.None {
		return nil
	}
	if m.resumePicker.Active() {
		m.resumePicker.TypeText(s)
		return nil
	}
	if m.hasInputSelection() {
		m.deleteInputSelection()
	}
	m.input.InsertString(s)
	m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
	m.atFilePicker.Update()
	m.clearEligibilityNotice()
	return nil
}
