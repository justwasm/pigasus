package tui

import "testing"

// TestHelpModel_OwnsOverlay pins the ownership move: the help panel hangs
// off HelpModel, and the root reaches it through that field.
func TestHelpModel_OwnsOverlay(t *testing.T) {
	m := diffTestModel()
	if m.help.Panel != nil {
		t.Fatal("help panel exists before construction")
	}
	m.help.Panel = m.newHelpPanel()
	if m.help.Panel == nil {
		t.Fatal("the model did not keep the panel")
	}
	if m.help.Panel.Active() {
		t.Fatal("fresh help panel is active")
	}
	m.help.Panel.Open(0)
	if !m.help.Panel.Active() {
		t.Fatal("Open did not activate the panel")
	}
	m.help.Panel.Cancel()
	if m.help.Panel.Active() {
		t.Fatal("Cancel did not close the panel")
	}
}
