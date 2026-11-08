package tuistore

import (
	"context"
	"testing"
)

func TestStore_BashCancellation(t *testing.T) {
	var s Store
	if s.BashRunning() || s.CancelBash() {
		t.Fatal("empty store reports a running command")
	}

	cancelled := 0
	s.SetBashCancel(func() { cancelled++ })
	if !s.BashRunning() {
		t.Fatal("SetBashCancel did not mark the command running")
	}
	if !s.CancelBash() {
		t.Fatal("CancelBash did not cancel the running command")
	}
	if cancelled != 1 || s.BashRunning() {
		t.Fatalf("cancel calls = %d running=%v", cancelled, s.BashRunning())
	}
	if s.CancelBash() {
		t.Fatal("a second CancelBash reported a running command")
	}
}

func TestStore_BashCancelUsesContext(t *testing.T) {
	var s Store
	ctx, cancel := context.WithCancel(context.Background())
	s.SetBashCancel(cancel)
	s.CancelBash()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("CancelBash did not cancel the command context")
	}
}
