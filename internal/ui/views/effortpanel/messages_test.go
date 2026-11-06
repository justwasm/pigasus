package effortpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_EffortPanelPureMessagePassing(t *testing.T) {
	// Build panel with zero seams
	p := New(Seams{})
	p.SetCursor(EffortLow)

	// Test Enter -> AppliedMsg
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("expected Enter to be handled")
	}
	if cmd == nil {
		t.Fatal("expected Enter to return a command")
	}
	msg := cmd()
	applied, ok := msg.(AppliedMsg)
	if !ok {
		t.Fatalf("expected AppliedMsg, got %T", msg)
	}
	if applied.Level != EffortLow {
		t.Errorf("expected level %d, got %d", EffortLow, applied.Level)
	}
	if applied.Tier != "low" {
		t.Errorf("expected tier 'low', got %q", applied.Tier)
	}

	// Test Esc -> DismissedMsg
	handled, cmd = p.HandleKey(tea.Key{Code: tea.KeyEscape})
	if !handled {
		t.Fatal("expected Escape to be handled")
	}
	if cmd == nil {
		t.Fatal("expected Escape to return a command")
	}
	msg = cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
}
