package diffpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_DiffPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{
		Cwd:   func() string { return t.TempDir() },
		Size:  func() (int, int) { return 80, 24 },
		Turns: func() int { return 0 },
	})

	// Esc on top view emits MsgDismissed
	claimed, cmd := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg := cmd()
	if _, ok := msg.(MsgDismissed); !ok {
		t.Fatalf("expected MsgDismissed, got %T", msg)
	}
}
