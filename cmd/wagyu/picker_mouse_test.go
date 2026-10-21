// picker_mouse_test.go — cycle 232: picker mouse click + hover + wheel.
//
// Mirrors the AGY 1.2.13 gap fix: "Fixed picker dropdowns not
// responding to mouse clicks on individual options; only keyboard
// navigation worked. Mouse clicks on an option now select it, and
// clicks on the input field reopen the picker."
//
// Each test drives runMode.Update() with a MouseClickMsg /
// MouseMotionMsg / MouseWheelMsg and checks the resulting state.
//
// Test rendering — the View() pass populates m.pickerGeom with the
// picker's on-screen rectangle. Without a render the picker is
// "visible" but its geom is empty, so we call View() once after the
// setup keystrokes (and again after every state mutation when the
// changed state affects the rectangle) before the click.
package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPickerMouse_ClickOnOptionSelects opens the picker by typing "/",
// renders the view to populate the geometry, then clicks each picker
// list row in turn. The picker's "click selects" rule mirrors the Tab
// keybinding in tui_run.go: the highlighted row's name is copied into
// the input buffer and the picker is re-derived for that buffer.
//
// The test verifies that:
//   - a click on a list row selects that row (idx moves to the row),
//   - the buffer ends up holding the picked command's name (Tab parity),
//   - the picker stays visible (the new buffer still starts with "/"),
//   - re-rendering after the click works on the new picker's geom.
func TestPickerMouse_ClickOnOptionSelects(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	// Refresh geom so handlePickerClick can map (x, y) to a row.
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Fatal("setup: pickerGeom should be valid after first render")
	}
	if !m.pickerGeom.HasList {
		t.Fatal("setup: pickerGeom.HasList should be true for the '/' list")
	}
	// Click the 3rd list row (visible-row offset 2).
	x := 5
	y := m.pickerGeom.ListTopY + 2
	startBuf := m.input.Value()
	// Capture the picked command's name by inspecting the picker
	// state before the click mutates the buffer.
	want := m.picker.SelectedAt(int(m.pickerLayoutLocal.ListTop) + 2)
	if want == nil {
		t.Fatalf("setup: no command at list offset 2 (listTop=%d)",
			m.pickerLayoutLocal.ListTop)
	}
	m.Update(mouseClick(x, y, tea.MouseLeft))
	// Buffer should now hold the selected command's name (Tab
	// parity). Pre-click the buffer was "/".
	if startBuf != "/" {
		t.Errorf("pre-click buffer %q should be '/'", startBuf)
	}
	if m.input.Value() != want.Name {
		t.Errorf("buffer after click = %q, want %q (the clicked command's name)",
			m.input.Value(), want.Name)
	}
	// Picker should still be visible (the new buffer starts with
	// "/", so Update() re-derives it).
	if !m.picker.Visible() {
		t.Errorf("click on a list row should keep the picker visible (new buffer starts with '/'); got hidden")
	}
	// The new picker state is consistent: the picked command is the
	// only filter match for the new buffer, with idx=0.
	if got := m.picker.Len(); got != 1 {
		t.Errorf("after click, picker.Len() = %d, want 1 (only /artifact etc. matches)", got)
	}
	if got := m.picker.Idx(); got != 0 {
		t.Errorf("after click, picker.Idx() = %d, want 0 (re-derived for the new buffer)", got)
	}
	// Re-rendering with the new picker must succeed without
	// panicking, and m.pickerGeom must repopulate for the next
	// click. The fresh list is a single row — exact geom values
	// depend on transcript row counts so we only assert validity.
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Errorf("after re-render with the new picker, pickerGeom should be valid")
	}
	if !m.pickerGeom.HasList {
		t.Errorf("after re-render, pickerGeom.HasList should be true")
	}
}

// TestPickerMouse_ClickOnInputReopensPicker mirrors the AGY 1.2.13
// "clicks on the input field reopen the picker" rule. Setup: type
// "/", then Esc (which closes the picker but leaves the buffer
// intact per cycle 150). A subsequent click on the input row must
// re-derive the picker — the buffer still starts with "/", so
// the overlay returns.
func TestPickerMouse_ClickOnInputReopensPicker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	// Cycle 150: Esc on a visible picker closes the picker but
	// keeps the buffer intact. Send the escape key via the
	// keybinding path.
	m.Update(key(tea.KeyEsc, 0))
	if m.picker.Visible() {
		t.Fatal("setup: Esc should have closed the picker")
	}
	if m.input.Value() == "" {
		t.Fatal("setup: Esc should NOT have cleared the input buffer")
	}
	// Render so promptScreenY is populated from the now-hidden
	// picker's layout.
	_ = m.View()
	if m.promptScreenY < 0 {
		t.Fatal("setup: promptScreenY should be valid after first render")
	}
	// Click on the input row.
	m.Update(mouseClick(5, m.promptScreenY, tea.MouseLeft))
	if !m.picker.Visible() {
		t.Errorf("click on input row should reopen picker (buffer still starts with '/'); got hidden")
	}
}

// TestPickerMouse_ClickOutsideDismissesPicker verifies the
// close-on-outside-click behaviour: clicking on a transcript row
// while the picker is open must reset the picker (so a subsequent
// picker.View() returns "" / pickerGeom.Valid becomes false). The
// existing in-app text-selection (cycle 174) still receives the
// click — both interactions cooperate.
func TestPickerMouse_ClickOutsideDismissesPicker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Fatal("setup: pickerGeom should be valid")
	}
	// Click at the very top — well above the picker.
	m.Update(mouseClick(2, 0, tea.MouseLeft))
	if m.picker.Visible() {
		t.Errorf("click outside the picker should dismiss it; picker still visible (idx=%d)",
			m.picker.Idx())
	}
}

// TestPickerMouse_WheelOverPickerScrollsPicker pins a wheel event on
// the picker's first list row and confirms the picker's MoveDown /
// MoveUp path took effect (idx incremented by one) instead of the
// transcript's scrollBy path.
func TestPickerMouse_WheelOverPickerScrollsPicker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	_ = m.View()
	if !m.pickerGeom.HasList {
		t.Fatal("setup: pickerGeom.HasList must be true to drive MoveDown")
	}
	startIdx := m.picker.Idx()
	// Wheel-Down on the first list row should fire MoveDown.
	wheel := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.pickerGeom.ListTopY, Button: tea.MouseWheelDown})
	m.Update(wheel)
	if got := m.picker.Idx(); got != startIdx+1 {
		t.Errorf("wheel-down on picker should advance idx by 1; got %d, want %d",
			got, startIdx+1)
	}
	// Wheel-Up on the same row should reverse.
	up := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.pickerGeom.ListTopY, Button: tea.MouseWheelUp})
	m.Update(up)
	if got := m.picker.Idx(); got != startIdx {
		t.Errorf("wheel-up on picker should drop idx back; got %d, want %d",
			got, startIdx)
	}
	// A wheel event well above the picker must NOT move the picker's
	// idx — the existing transcript-scroll path takes over.
	abovePicker := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.pickerGeom.TopY - 5, Button: tea.MouseWheelDown})
	m.Update(abovePicker)
	if got := m.picker.Idx(); got != startIdx {
		t.Errorf("wheel event outside picker should not move idx; got %d, want %d",
			got, startIdx)
	}
}

// TestPickerMouse_HoverChangesHighlight covers the hover-to-highlight
// behaviour. A motion event over a list row should land the idx on
// that row, even when the current idx was somewhere else.
func TestPickerMouse_HoverChangesHighlight(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	_ = m.View()
	startIdx := m.picker.Idx()
	if startIdx != 0 {
		t.Fatalf("setup: idx should start at 0, got %d", startIdx)
	}
	// Hover over row 3.
	hoverY := m.pickerGeom.ListTopY + 3
	m.Update(mouseMotion(5, hoverY))
	if got := m.picker.Idx(); got != 3 {
		t.Errorf("hover over row 3 should set idx = 3, got %d", got)
	}
	// Hover over a row at idx 0.
	m.Update(mouseMotion(5, m.pickerGeom.ListTopY))
	if got := m.picker.Idx(); got != 0 {
		t.Errorf("hover over row 0 should set idx = 0, got %d", got)
	}
	// Motion outside the picker rows must NOT move the idx.
	m.Update(mouseMotion(5, m.pickerGeom.TopY-1))
	if got := m.picker.Idx(); got != 0 {
		t.Errorf("hover outside picker should leave idx alone; got %d", got)
	}
}

// TestPickerMouse_PickerGeomTracksRender confirms m.pickerGeom is
// populated correctly from a render pass and used correctly by the
// dispatch helpers. Without the render the handlers bail out.
func TestPickerMouse_PickerGeomTracksRender(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	// Pre-render: pickerGeom should be invalid.
	m.Update(runeKey('/'))
	if m.pickerGeom.Valid {
		t.Error("pickerGeom should start invalid before any render")
	}
	// Render: pickerGeom becomes valid and the list rows have a
	// non-empty range.
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Fatal("pickerGeom should be valid after a View() with the picker open")
	}
	if m.pickerGeom.BottomY <= m.pickerGeom.TopY {
		t.Errorf("pickerGeom should span more than one row; top=%d bottom=%d",
			m.pickerGeom.TopY, m.pickerGeom.BottomY)
	}
	// Hide the picker.
	m.picker.Reset()
	_ = m.View()
	if m.pickerGeom.Valid {
		t.Errorf("pickerGeom should clear when picker is hidden; got %+v", m.pickerGeom)
	}
}
