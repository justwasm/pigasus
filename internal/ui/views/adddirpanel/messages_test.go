package adddirpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_AddDirPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{})
	p.SetText("/usr/local/workspace")

	// Enter -> AddedMsg
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected Enter handled with cmd, got %v, %v", handled, cmd)
	}
	msg := cmd()
	added, ok := msg.(AddedMsg)
	if !ok || added.Path != "/usr/local/workspace" {
		t.Fatalf("expected AddedMsg with path, got %v", msg)
	}

	// Esc -> DismissedMsg
	handled, cmd = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || cmd == nil {
		t.Fatalf("expected Esc handled with cmd, got %v, %v", handled, cmd)
	}
	msg = cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
}
