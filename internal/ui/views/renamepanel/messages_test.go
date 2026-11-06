package renamepanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_RenamePanelPureMessagePassing(t *testing.T) {
	p := New(Seams{})
	p.SetValue("New Session Title")

	// Enter -> RenamedMsg
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("expected Enter to be handled")
	}
	if cmd == nil {
		t.Fatal("expected Enter to return a cmd")
	}
	msg := cmd()
	renamed, ok := msg.(RenamedMsg)
	if !ok {
		t.Fatalf("expected RenamedMsg, got %T", msg)
	}
	if renamed.Name != "New Session Title" {
		t.Errorf("expected 'New Session Title', got %q", renamed.Name)
	}

	// Esc -> DismissedMsg
	handled, cmd = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if cmd == nil {
		t.Fatal("expected Esc to return a cmd")
	}
	msg = cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
}
