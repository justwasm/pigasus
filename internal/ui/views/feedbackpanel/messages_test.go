package feedbackpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_FeedbackPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{})

	// Test Esc from stage 0 -> DismissedMsg
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if cmd == nil {
		t.Fatal("expected Esc to return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}

	// Press '1' to pick category 1 and advance to stage 1
	handled, _ = p.HandleKey(tea.Key{Text: "1"})
	if !handled || p.Stage() != 1 {
		t.Fatalf("expected stage 1 after '1', got handled=%v, stage=%d", handled, p.Stage())
	}

	// Type text
	p.HandleKey(tea.Key{Text: "something broke"})

	// Enter to submit -> SubmittedMsg
	handled, cmd = p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("expected Enter to be handled")
	}
	if cmd == nil {
		t.Fatal("expected Enter to return a cmd")
	}
	msg = cmd()
	submitted, ok := msg.(SubmittedMsg)
	if !ok {
		t.Fatalf("expected SubmittedMsg, got %T", msg)
	}
	if submitted.Text != "something broke" {
		t.Errorf("expected text 'something broke', got %q", submitted.Text)
	}
	if submitted.Category != CategoryLabel(Categories[0]) {
		t.Errorf("expected category %q, got %q", CategoryLabel(Categories[0]), submitted.Category)
	}
}
