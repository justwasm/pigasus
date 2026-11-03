package usagepanel

import "testing"

// TestQuotaTickMessage pins the tick's message type.
//
// The host's update switch names the message the tick produces. A tick
// command that produces a *different* message type is a spinner that
// never moves, and nothing else in the tree would notice: the command
// returns, the host ignores the message, and the panel sits on its
// first frame until the panel closes.
func TestQuotaTickMessage(t *testing.T) {
	cmd := QuotaTick()
	if cmd == nil {
		t.Fatal("QuotaTick() = nil")
	}
	msg := cmd()
	if _, ok := msg.(QuotaTickMsg); !ok {
		t.Errorf("QuotaTick() produced %T, want QuotaTickMsg", msg)
	}
}
