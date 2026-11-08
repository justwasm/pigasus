package accountspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestStandaloneModel_Init(t *testing.T) {
	m := NewStandaloneModel(Seams{})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil Init cmd")
	}
	view := m.View()
	if !view.AltScreen {
		t.Error("expected AltScreen to be true")
	}
	// Verify lines rendered
	lines := m.panel.Lines()
	viewStr := strings.Join(lines, "\n")
	if !strings.Contains(viewStr, "Accounts") {
		t.Errorf("view missing Accounts title: %s", viewStr)
	}
	if !strings.Contains(viewStr, "Pool") {
		t.Errorf("view missing Pool tab: %s", viewStr)
	}
}

func TestStandaloneModel_QuitKeys(t *testing.T) {
	m := NewStandaloneModel(Seams{})
	m.Init()

	// Pressing q should return tea.Quit
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	if cmd == nil {
		t.Fatal("expected tea.Quit command on 'q'")
	}

	// Pressing ctrl+c should return tea.Quit
	_, cmd2 := m.Update(tea.KeyPressMsg(tea.Key{Code: 3, Text: "ctrl+c"}))
	if cmd2 == nil {
		t.Fatal("expected tea.Quit command on 'ctrl+c'")
	}
}

func TestStandaloneModel_DismissMsg(t *testing.T) {
	m := NewStandaloneModel(Seams{})
	_, cmd := m.Update(DismissedMsg{})
	if cmd == nil {
		t.Fatal("expected tea.Quit command on DismissedMsg")
	}
}

func TestStandaloneModel_WindowSize(t *testing.T) {
	m := NewStandaloneModel(Seams{})
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.width != 100 || m.height != 40 {
		t.Fatalf("expected 100x40, got %dx%d", m.width, m.height)
	}
}
