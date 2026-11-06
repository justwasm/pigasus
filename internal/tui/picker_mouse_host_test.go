// picker_mouse_host_test.go — the popup's mouse wiring in the host.
//
// The popup is a panel (internal/ui/views/slashpicker); the panel's own
// tests drive the panel directly. This test drives the *host*: it
// types into the composer, paints a frame so the popup's rectangle is
// computed, clicks a row of the painted popup and checks that the
// composer takes the clicked command's name.
//
// The end-to-end wiring is the point: the host renders the popup, the
// rectangle lands on the right screen rows, and the click reaches the
// panel.
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// paintedRow returns the screen row of the first painted line that
// holds the given text, and the frame's lines.
//
// The frame's line index is the terminal row: the body the host paints
// *is* the screen, and the popup's rectangle is expressed in the same
// rows.
func paintedRow(t *testing.T, m *runMode, text string) int {
	t.Helper()
	lines := strings.Split(m.View().Content, "\n")
	for i, l := range lines {
		if strings.Contains(stripAnsi(l), text) {
			return i
		}
	}
	t.Fatalf("the painted frame should carry a row with %q, got:\n%s", text, strings.Join(lines, "\n"))
	return -1
}

// TestPickerMouse_ClickOnPaintedRowCompletesTheCommand drives the
// whole path: the composer takes a "/", the frame is painted, a row of
// the painted popup is clicked, and the composer takes that row's
// command name.
func TestPickerMouse_ClickOnPaintedRowCompletesTheCommand(t *testing.T) {
	m := sizedMode(80, 24, 0)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: the popup should be visible after '/'")
	}

	// The second row of the popup — "/agents". The first row is already
	// the highlighted one, and a click on the highlighted row moves
	// nothing (the clone's behaviour, kept byte for byte).
	row := paintedRow(t, m, "/agents")
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: row, Button: tea.MouseLeft}))

	if got := m.input.Value(); got != "/agents" {
		t.Errorf("the composer after the click = %q, want %q", got, "/agents")
	}
}

// TestPickerMouse_ClickOutsideThePopupLeavesTheComposer pins the
// other half: a click that lands outside the popup's rectangle is not
// the popup's business, so the composer keeps what it holds.
func TestPickerMouse_ClickOutsideThePopupLeavesTheComposer(t *testing.T) {
	m := sizedMode(80, 24, 0)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: the popup should be visible after '/'")
	}
	m.View()

	// Row 0 is the transcript, well above the popup's rectangle.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: 0, Button: tea.MouseLeft}))

	if got := m.input.Value(); got != "/" {
		t.Errorf("the composer after a click outside the popup = %q, want %q", got, "/")
	}
}
