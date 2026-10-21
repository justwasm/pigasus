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

package main

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	return m.inputSelStart != m.inputSelEnd
}

// inputSelectionRange returns normalized [start, end] byte offsets in m.input.Value()
// such that start <= end.
func (m *runMode) inputSelectionRange() (start, end int) {
	if m.inputSelStart <= m.inputSelEnd {
		return m.inputSelStart, m.inputSelEnd
	}
	return m.inputSelEnd, m.inputSelStart
}

// clearInputSelection resets the input field selection state.
func (m *runMode) clearInputSelection() {
	m.inputSelDragging = false
	m.inputSelStart = 0
	m.inputSelEnd = 0
}

// inputOffsetAt maps screen coordinate (x, y) to a byte offset within m.input.Value().
func (m *runMode) inputOffsetAt(x, y int) int {
	val := m.input.Value()
	if len(val) == 0 {
		return 0
	}
	lineCount := m.inputLineCount()
	lineIdx := y - m.promptScreenY
	if lineIdx < 0 {
		return 0
	}
	if lineIdx >= lineCount {
		return len(val)
	}

	// Locate start and end of lineIdx within val.
	lineStart := 0
	curLine := 0
	for i := 0; i < len(val); i++ {
		if curLine == lineIdx {
			lineStart = i
			break
		}
		if val[i] == '\n' {
			curLine++
			lineStart = i + 1
		}
	}
	lineEnd := len(val)
	if nl := strings.IndexByte(val[lineStart:], '\n'); nl >= 0 {
		lineEnd = lineStart + nl
	}
	lineText := val[lineStart:lineEnd]

	// The prompt occupies 2 display cells ("> " on line 0, "  " on continuation lines).
	targetCol := x - 2
	if targetCol <= 0 {
		return lineStart
	}

	curCol := 0
	byteOff := 0
	for byteOff < len(lineText) {
		r, size := utf8.DecodeRuneInString(lineText[byteOff:])
		w := ansi.StringWidth(string(r))
		if curCol+w > targetCol {
			// If click lands in the second half of a wide rune, advance past it.
			if targetCol >= curCol+w/2+1 {
				byteOff += size
			}
			break
		}
		curCol += w
		byteOff += size
	}
	return lineStart + byteOff
}

// setInputCursorOffset moves the textarea cursor to the given byte offset in m.input.Value().
func (m *runMode) setInputCursorOffset(offset int) {
	val := m.input.Value()
	if offset < 0 {
		offset = 0
	}
	if offset > len(val) {
		offset = len(val)
	}
	targetLine := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if val[i] == '\n' {
			targetLine++
			lineStart = i + 1
		}
	}
	targetCol := utf8.RuneCountInString(val[lineStart:offset])

	m.input.MoveToBegin()
	for l := 0; l < targetLine; l++ {
		m.input.CursorDown()
	}
	m.input.SetCursorColumn(targetCol)
}

// selectAllInput selects all text in the input field, moves cursor to the end,
// and auto-copies the text to clipboard.
func (m *runMode) selectAllInput() (tea.Cmd, bool) {
	val := m.input.Value()
	if len(val) == 0 {
		return nil, false
	}
	m.inputSelStart = 0
	m.inputSelEnd = len(val)
	m.setInputCursorOffset(len(val))
	_ = copyToClipboard(val)
	return m.copySelectionCmd(val), true
}

// deleteInputSelection deletes the selected substring from m.input and clears selection.
func (m *runMode) deleteInputSelection() {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.inputSelectionRange()
	val := m.input.Value()
	if start <= len(val) && end <= len(val) {
		newVal := val[:start] + val[end:]
		m.input.SetValue(newVal)
		m.setInputCursorOffset(start)
	}
	m.clearInputSelection()
}

// replaceInputSelection replaces the selected substring with replacement and clears selection.
func (m *runMode) replaceInputSelection(replacement string) {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.inputSelectionRange()
	val := m.input.Value()
	if start <= len(val) && end <= len(val) {
		newVal := val[:start] + replacement + val[end:]
		m.input.SetValue(newVal)
		m.setInputCursorOffset(start + len(replacement))
	}
	m.clearInputSelection()
}

// applyInputSelectionHighlight applies reverse-video styling to the selected
// range within the rendered inputView string.
func (m *runMode) applyInputSelectionHighlight(inputView string) string {
	start, end := m.inputSelectionRange()
	if start >= end {
		return inputView
	}
	val := m.input.Value()
	if start >= len(val) {
		return inputView
	}
	if end > len(val) {
		end = len(val)
	}

	lines := strings.Split(inputView, "\n")
	hasTrailingNewline := strings.HasSuffix(inputView, "\n")
	if hasTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	lineStart := 0
	for lineIdx := 0; lineIdx < len(lines); lineIdx++ {
		lineEnd := len(val)
		if nl := strings.IndexByte(val[lineStart:], '\n'); nl >= 0 {
			lineEnd = lineStart + nl
		}

		selStartInLine := start
		if selStartInLine < lineStart {
			selStartInLine = lineStart
		}
		selEndInLine := end
		if selEndInLine > lineEnd {
			selEndInLine = lineEnd
		}

		if selStartInLine < selEndInLine {
			lineText := val[lineStart:lineEnd]
			offsetFromLine := selStartInLine - lineStart
			offsetToLine := selEndInLine - lineStart

			prefixText := lineText[:offsetFromLine]
			selText := lineText[offsetFromLine:offsetToLine]

			fromCell := 2 + ansi.StringWidth(prefixText)
			toCell := fromCell + ansi.StringWidth(selText)

			lines[lineIdx] = applySelectionHighlightToRow(lines[lineIdx], fromCell, toCell)
		}

		if lineEnd < len(val) {
			lineStart = lineEnd + 1
		} else {
			lineStart = len(val)
		}
	}

	res := strings.Join(lines, "\n")
	if hasTrailingNewline {
		res += "\n"
	}
	return res
}

// applySelectionHighlightToRow highlights the cell range [fromCell, toCell] in reverse video.
func applySelectionHighlightToRow(row string, fromCell, toCell int) string {
	rowWidth := ansi.StringWidth(row)
	if rowWidth == 0 || fromCell >= toCell {
		return row
	}
	if fromCell > rowWidth {
		fromCell = rowWidth
	}
	if toCell > rowWidth {
		toCell = rowWidth
	}
	if fromCell >= toCell {
		return row
	}
	prefix := ansi.Cut(row, 0, fromCell)
	selected := ansi.Strip(ansi.Cut(row, fromCell, toCell))
	suffix := ansi.Cut(row, toCell, rowWidth)
	return prefix + selHighlightOpen + selected + selHighlightClose + suffix
}
