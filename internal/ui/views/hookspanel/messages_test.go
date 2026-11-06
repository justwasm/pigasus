package hookspanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_HooksPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{})

	// Esc from stage 0 -> DismissedMsg
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || cmd == nil {
		t.Fatalf("expected Esc handled with cmd, got %v, %v", handled, cmd)
	}
	msg := cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
}
