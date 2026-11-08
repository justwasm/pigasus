package tui

import "testing"

func diffTestModel() *RootModel {
	return &RootModel{
		session:  sessionState{cwd: "/tmp"},
		viewport: uiState{width: 100, height: 30},
	}
}

// TestDiffModel_PanelIsLazyAndReused pins ownership: the diff panel is
// built on first use and kept, so its folds survive re-renders.
func TestDiffModel_PanelIsLazyAndReused(t *testing.T) {
	m := diffTestModel()
	if m.diff.panel != nil {
		t.Fatal("diff panel was built before first use")
	}
	first := m.diff.Panel(m)
	if first == nil || m.diff.panel != first {
		t.Fatal("Panel did not build and keep the panel")
	}
	if second := m.diff.Panel(m); second != first {
		t.Fatal("Panel built a second panel")
	}
	m.diff.Reopen(m)
	if m.diff.panel == first {
		t.Fatal("Reopen kept the old panel")
	}
}

// TestDiffModel_RemoteCache keeps the remote turn diff on the model, not
// on the engine bucket.
func TestDiffModel_RemoteCache(t *testing.T) {
	var d DiffModel
	if d.Remote() != nil {
		t.Fatal("zero model has a remote cache")
	}
	cache := &remoteDiffCache{loaded: true}
	d.SetRemote(cache)
	if d.Remote() != cache {
		t.Fatal("SetRemote did not keep the cache")
	}
}
