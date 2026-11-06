// picker_enter_test.go — the popup's Enter semantics in the host.
//
// The popup is a panel (internal/ui/views/slashpicker); this test pins
// what the *host* does with the popup's highlighted row.
package tui

import (
	"strings"
	"testing"
)

// Cycle 151: when the picker overlay is visible and the user presses
// Enter, the highlighted command is dispatched — not the literal
// buffer text. Typing "/ch" with "/changelog" highlighted and pressing
// Enter should open /changelog, not the literal /ch (which the cycle
// 149 implementation would have routed to "Unknown command: /ch").
// Captured against the v1.3.6 reference at /tmp/ourpk.txt.
func TestPickerView_EnterUsesHighlighted(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	// Type "/ch" — narrows the picker to /changelog.
	m.Update(runeKey('/'))
	m.Update(runeKey('c'))
	m.Update(runeKey('h'))
	sel := m.composer.picker.Selected()
	if sel == nil || sel.Name != "/changelog" {
		t.Fatalf("setup: highlighted should be /changelog, got %v", sel)
	}
	// Press Enter — dispatches the picker-selected command, not the
	// typed "/ch".
	m.Update(enterKey())
	joined := strings.Join(m.engine.response, "\n")
	// /changelog case opens the /help overlay (it would render the panel)
	// but cycle 149 dropped the slash-command echo, so transcript is
	// empty after dispatch. The picker substitution correctness is
	// verified by checking that the picker closed (handleSlashCommand
	// reset it via m.composer.picker.Reset) and that the literal "/ch" was NOT
	// dispatched to the default case ("Unknown command: /ch").
	if m.composer.picker.Visible() {
		t.Errorf("picker should be reset after Enter (cycle 150)")
	}
	if strings.Contains(joined, "Unknown command: /ch") {
		t.Errorf("Enter dispatched literal buffer text %q instead of picker selection %q",
			"/ch", "/changelog")
	}
}
