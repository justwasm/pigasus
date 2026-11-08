package tui

import "testing"

// TestRemoteModel_OwnsRemoteRuntime pins the ownership move: the attached
// flag, the @ picker cache, the last terminal id and the in-process
// remote-control server hang off RemoteModel, not the transport bucket.
func TestRemoteModel_OwnsRemoteRuntime(t *testing.T) {
	var empty RemoteModel
	if empty.Attached() || empty.Files() != nil || empty.TerminalID() != "" || empty.TurnLock() != nil {
		t.Fatal("zero model reports remote state")
	}

	var r RemoteModel
	r.SetAttached(true)
	if !r.Attached() {
		t.Fatal("SetAttached did not stick")
	}
	r.SetTerminalID("term-1")
	if got := r.TerminalID(); got != "term-1" {
		t.Fatalf("terminal id = %q", got)
	}

	stopped := false
	r.SetControl(func() { stopped = true }, nil)
	r.StopControl()
	if !stopped {
		t.Fatal("StopControl did not run the stop function")
	}
	if r.Dispatcher() != nil {
		t.Fatal("StopControl kept the dispatcher")
	}
	r.StopControl() // idempotent
}

// TestRemoteModel_EnsureTurnLock pins the lazy turn lock: the first call
// creates it, later calls return the same lock.
func TestRemoteModel_EnsureTurnLock(t *testing.T) {
	var r RemoteModel
	first := r.EnsureTurnLock()
	if first == nil {
		t.Fatal("EnsureTurnLock returned nil")
	}
	if second := r.EnsureTurnLock(); second != first {
		t.Fatal("EnsureTurnLock replaced an existing lock")
	}
	if r.TurnLock() != first {
		t.Fatal("TurnLock did not return the created lock")
	}
}
