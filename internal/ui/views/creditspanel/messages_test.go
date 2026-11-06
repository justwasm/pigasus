package creditspanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_CreditsPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{})

	// 1. Esc emits MsgClose
	claimed, cmd := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg := cmd()
	if _, ok := msg.(MsgClose); !ok {
		t.Fatalf("expected MsgClose, got %T", msg)
	}

	// 2. Enter on cursor 0 emits MsgOpenURL for AI Premium
	p.SetCursor(0)
	claimed, cmd = p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !claimed || cmd == nil {
		t.Fatalf("expected Enter to be claimed with command")
	}
	msg = cmd()
	openURL, ok := msg.(MsgOpenURL)
	if !ok || openURL.URL != "https://one.google.com/ai-premium" {
		t.Fatalf("expected MsgOpenURL with ai-premium, got %v", msg)
	}

	// 3. Enter on cursor 1 emits MsgOpenURL for subscriptions
	p.SetCursor(1)
	claimed, cmd = p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !claimed || cmd == nil {
		t.Fatalf("expected Enter to be claimed with command")
	}
	msg = cmd()
	openURL, ok = msg.(MsgOpenURL)
	if !ok || openURL.URL != "https://myaccount.google.com/subscriptions" {
		t.Fatalf("expected MsgOpenURL with subscriptions, got %v", msg)
	}
}
