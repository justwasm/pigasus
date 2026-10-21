// picker_mouse.go — cycle 232: mouse click + hover + wheel for the
// slash-command picker overlay.
//
// Gap (AGY 1.2.13): "Fixed picker dropdowns not responding to mouse
// clicks on individual options; only keyboard navigation worked.
// Mouse clicks on an option now select it, and clicks on the input
// field reopen the picker."
//
// The reference binary dispatches mouse coordinates against three
// regions:
//
//   - the input row (reopens the picker if the user had Esc'd it
//     while keeping the "/" prefix);
//   - the picker overlay's list rows (a click highlights and commits
//     the row, a hover changes the highlight);
//   - outside the picker (a click dismisses the picker, matching the
//     standard dropdown close-on-outside-click pattern).
//
// Wheel events over the picker scroll the picker instead of the
// transcript; the existing handleMouseWheel still scrolls the
// transcript, so the wheel-over-picker path adds a new branch in
// that helper.
//
// The picker region lives in runMode.pickerGeom, populated by View()
// at render time and consumed by the mouse handlers below.
package main

import (
	tea "charm.land/bubbletea/v2"
)

// pickerLayout is the picker's internal structure, populated by
// pickerView() as a side effect. The offsets are 0-based row indices
// within the picker overlay's own row sequence (i.e. before placement
// in the body, and before any top-trim). viewBody() reads these back
// to derive m.pickerGeom, the screen rectangle in terminal
// coordinates.
type pickerLayout struct {
	HasList    bool // at least one list row was rendered (false in the "No matches" branch)
	ListTop    int  // row offset of the first LIST row (skips the ↑ N more tail)
	ListBottom int  // row offset of the last  LIST row
	UpMore     bool // ↑ N more tail was emitted
	DownMore   bool // ↓ N more tail was emitted
	HintRow    int  // row offset of the keyboard hint row (= Rows - 1)
	Rows       int  // total rows the picker wrote, including tails and hint
}

// pickerGeom is the picker's on-screen rectangle. Rows are 0-based
// terminal rows. Valid is false when the picker is hidden or the
// terminal size is unknown.
//
// TopY and BottomY bracket every row the picker emitted, including
// the optional ↑ N more / ↓ N more tail rows, the blank separator,
// and the ↑/↓ Navigate · enter Select · tab Complete hint row.
// ListTopY and ListBottomY point at the LIST rows that a click
// should select — the scroll-tail rows and the hint row are
// excluded. HasList mirrors pickerLayoutLocal.HasList so a click
// dispatched against an empty ("No matches") picker is dropped
// instead of picking a non-existent command.
type pickerGeom struct {
	Valid       bool
	HasList     bool
	TopY        int
	BottomY     int
	ListTopY    int
	ListBottomY int
	HintY       int
}

// commitPickerSelectionToBuffer mirrors the Tab keybinding in
// tui_run.go: replace the input buffer with the currently highlighted
// picker's command name and re-derive the picker. Used by the click
// handler so a click selects the row in the same way the keyboard
// would.
func (m *runMode) commitPickerSelectionToBuffer() bool {
	sel := m.picker.Selected()
	if sel == nil {
		return false
	}
	m.input.SetValue(sel.Name)
	m.picker.Update([]rune(sel.Name))
	return true
}

// handlePickerClick is called from the MouseClickMsg dispatcher in
// tui_run.go BEFORE the existing transcript-selection path runs. It
// returns true when the click landed in a region the picker owns
// (input row, picker list row, or the hint row), in which case the
// caller should NOT also start a text selection.
//
// When the click lands on a picker list row the picker highlights
// that row and, for parity with the Tab keybinding, copies the
// command's name into the input buffer so the user can press Enter
// to dispatch it. The buffer update re-derives the picker so the
// overlay reflects the new query.
//
// When the click lands on the input row the picker state is
// re-derived from the buffer; if the buffer still starts with "/"
// (e.g. the user Esc'd the picker and the buffer was preserved) this
// re-opens the overlay.
func (m *runMode) handlePickerClick(ev tea.Mouse) bool {
	if !m.altScreen {
		return false
	}
	x, y := ev.X, ev.Y

	// Click on the input row — re-derive the picker state from the
	// current buffer. This is the "reopen" path for Esc-then-click
	// (Esc closes the picker but leaves the buffer intact, so the
	// picker re-opens on the next Update). Returns false so the
	// input field's mouse selection / cursor positioning can also
	// process the click.
	if m.isInputY(y) {
		m.picker.Update([]rune(firstLine(m.input.Value())))
		return false
	}

	// Click on the picker overlay rows. Only LIST rows dispatch;
	// clicks on the ↑ / ↓ tail rows or the keyboard hint row are
	// no-ops (the hint row stays a label, no further action).
	if !m.pickerGeom.Valid || !m.pickerGeom.HasList {
		return false
	}
	if y >= m.pickerGeom.ListTopY && y <= m.pickerGeom.ListBottomY {
		if x < 0 || x >= m.width {
			return false
		}
		rowOffset := y - m.pickerGeom.ListTopY
		idx := m.pickerLayoutLocal.ListTop + rowOffset
		if !m.picker.SetIdx(idx) {
			return false
		}
		m.commitPickerSelectionToBuffer()
		return true
	}

	// Click anywhere else is not a picker concern — let the caller
	// fall through to the transcript-selection / panel logic.
	return false
}

// handlePickerHover updates the picker highlight to match the row
// under the mouse pointer. It is called from the MouseMotionMsg
// dispatcher on every motion event while the picker is visible.
//
// Returns true when the highlight actually changed (callers can use
// this to skip the no-op redraw path). False is the dominant
// outcome: most motion events fall outside the picker rows.
func (m *runMode) handlePickerHover(ev tea.Mouse) bool {
	if !m.altScreen {
		return false
	}
	x, y := ev.X, ev.Y
	if !m.pickerGeom.Valid || !m.pickerGeom.HasList {
		return false
	}
	if y < m.pickerGeom.ListTopY || y > m.pickerGeom.ListBottomY {
		return false
	}
	if x < 0 || x >= m.width {
		return false
	}
	rowOffset := y - m.pickerGeom.ListTopY
	idx := m.pickerLayoutLocal.ListTop + rowOffset
	return m.picker.SetIdx(idx)
}

// handlePickerWheel is called from handleMouseWheel when a wheel
// event lands over the picker overlay. Returns true when the picker
// consumed the event so the caller does not also scroll the
// transcript.
//
// Wheel events outside the picker continue to scroll the transcript
// via the existing handleMouseWheel path.
func (m *runMode) handlePickerWheel(ev tea.Mouse) bool {
	if !m.altScreen {
		return false
	}
	if !m.pickerGeom.Valid || !m.pickerGeom.HasList {
		return false
	}
	// The picker rows include the ↑ / ↓ tail rows and the hint row;
	// any wheel event anywhere in that rectangle scrolls the picker
	// (the user is "in the picker"), not the transcript.
	if ev.Y < m.pickerGeom.TopY || ev.Y > m.pickerGeom.BottomY {
		return false
	}
	switch ev.Button {
	case tea.MouseWheelUp:
		return m.picker.MoveUp()
	case tea.MouseWheelDown:
		return m.picker.MoveDown()
	}
	return false
}
