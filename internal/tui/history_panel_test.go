package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestOpenHistoryPanelViaSlash(t *testing.T) {
	m := &RootModel{}
	m.prompt.input = textarea.New()
	m.pushHistory("echo hello from history")
	m.pushHistory("git status")

	if !m.openSlashPanel("history") {
		t.Fatal("openSlashPanel(\"history\") = false, want true")
	}
	if m.overlays.panel.kind != panelregistry.History {
		t.Fatalf("panel kind = %v, want panelregistry.History", m.overlays.panel.kind)
	}
	if m.overlays.panel.cmd != "/history" {
		t.Fatalf("panel cmd = %q, want \"/history\"", m.overlays.panel.cmd)
	}

	p := m.historyPanel()
	if p == nil {
		t.Fatal("historyPanel() is nil")
	}
	if p.RowCount() < 2 {
		t.Fatalf("expected at least 2 history rows, got %d", p.RowCount())
	}

	lines := m.panelLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Prompt History") {
		t.Errorf("missing Prompt History title in lines:\n%s", joined)
	}
	if !strings.Contains(joined, "git status") {
		t.Errorf("missing 'git status' in lines:\n%s", joined)
	}

	// Press Enter to pick the first entry ("git status")
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Errorf("expected Enter to be handled by history panel")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel should be closed after Enter, got %v", m.overlays.panel.kind)
	}
	if m.prompt.input.Value() != "git status" {
		t.Errorf("composer input = %q, want 'git status'", m.prompt.input.Value())
	}
}

func TestOpenHistoryPanelViaKeys(t *testing.T) {
	m := &RootModel{}
	m.pushHistory("some past prompt")

	// Press Ctrl+H
	_, _, handled := m.handleGlobalOrModalKeys(tea.Key{Code: 'h', Mod: tea.ModCtrl})
	if !handled {
		t.Fatal("Ctrl+H was not handled")
	}
	if m.overlays.panel.kind != panelregistry.History {
		t.Fatalf("after Ctrl+H: panel kind = %v, want History", m.overlays.panel.kind)
	}

	// Dismiss with Esc
	handled, _ = m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled || m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("after Esc: panel = %v, want None", m.overlays.panel.kind)
	}

	// Press Alt+H
	_, _, handled = m.handleGlobalOrModalKeys(tea.Key{Code: 'h', Mod: tea.ModAlt})
	if !handled {
		t.Fatal("Alt+H was not handled")
	}
	if m.overlays.panel.kind != panelregistry.History {
		t.Fatalf("after Alt+H: panel kind = %v, want History", m.overlays.panel.kind)
	}
}
