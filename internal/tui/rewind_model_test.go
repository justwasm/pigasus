package tui

import "testing"

// TestRewindModel_PanelIsLazyAndReused pins the panel's ownership: it is
// built on first use, reused across re-renders and replaced on /rewind.
func TestRewindModel_PanelIsLazyAndReused(t *testing.T) {
	m := diffTestModel()
	if m.rewind.panel != nil {
		t.Fatal("rewind panel was built before first use")
	}
	first := m.rewind.Panel(m)
	if first == nil || m.rewind.panel != first {
		t.Fatal("Panel did not build and keep the panel")
	}
	if second := m.rewind.Panel(m); second != first {
		t.Fatal("Panel built a second panel")
	}
	m.rewind.Reopen(m)
	if m.rewind.panel == first {
		t.Fatal("Reopen kept the old panel")
	}
}
