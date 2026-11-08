package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// TestTurnService_OwnsTransport pins the ownership move: the backend,
// its model-change factory, the engine, the active bridge and the program
// hang off TurnService.
func TestTurnService_OwnsTransport(t *testing.T) {
	var tsvc TurnService
	if tsvc.Backend() != nil || tsvc.BackendFor() != nil || tsvc.Engine() != nil || tsvc.ActiveBridge() != nil || tsvc.Program() != nil {
		t.Fatal("zero service reports transport state")
	}
	if tsvc.CancelTurn() || tsvc.IsRunning() {
		t.Fatal("zero service reports a running turn")
	}

	be := backend.NewMock()
	tsvc.SetBackend(be)
	if tsvc.Backend() != be {
		t.Fatal("SetBackend did not stick")
	}
	factoryCalled := ""
	tsvc.SetBackendFor(func(model string) backend.Client {
		factoryCalled = model
		return be
	})
	if got := tsvc.RebuildBackend("m"); got != be || factoryCalled != "m" {
		t.Fatalf("RebuildBackend = %v, factory saw %q", got, factoryCalled)
	}
}

// TestTurnService_DelegatesCancellation pins the bridge delegation: the
// service is the single read/cancel point for the in-flight turn.
func TestTurnService_DelegatesCancellation(t *testing.T) {
	bridge, ctx := armRunningBridge(t)
	var tsvc TurnService
	tsvc.SetActiveBridge(bridge)
	if !tsvc.IsRunning() {
		t.Fatal("IsRunning did not read the bridge")
	}
	if !tsvc.CancelTurn() {
		t.Fatal("CancelTurn did not cancel")
	}
	requireCancelled(t, ctx)
	if tsvc.IsRunning() {
		t.Fatal("IsRunning stayed true after cancel")
	}
	tsvc.ClearActiveBridge()
	if tsvc.CancelTurn() {
		t.Fatal("cleared service cancelled again")
	}
}
