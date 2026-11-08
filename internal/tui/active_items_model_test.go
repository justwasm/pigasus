package tui

import "testing"

// TestActiveItemsModel_OwnsPanel pins the ownership move: the active-items
// panel hangs off the model, and the root reaches it through that field.
func TestActiveItemsModel_OwnsPanel(t *testing.T) {
	m := diffTestModel()
	if m.activeItems.Panel != nil {
		t.Fatal("active-items panel exists before construction")
	}
	m.activeItems.Panel = m.newActiveItemsPanel()
	if m.activeItems.Panel == nil {
		t.Fatal("the model did not keep the panel")
	}
	// The panel is nil-safe; an unbuilt model answers the same as an empty
	// list.
	var empty ActiveItemsModel
	if empty.Count() != 0 || empty.Focused() || empty.Hidden() {
		t.Fatal("empty model reports active items")
	}
	if empty.Rows() != "" {
		t.Fatal("empty model rendered rows")
	}
}
