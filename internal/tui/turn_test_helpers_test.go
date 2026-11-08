package tui

import (
	"context"
	"testing"
)

// armRunningBridge installs a bridge whose Tier-4 turn runtime is already
// running, so a test can exercise cancellation without starting a real
// turn. The returned context is the turn's context.
func armRunningBridge(t *testing.T) (*EngineBridge, context.Context) {
	t.Helper()
	bridge := NewEngineBridge(BridgeOptions{})
	ctx, end := bridge.turns.Begin(context.Background())
	t.Cleanup(end)
	return bridge, ctx
}

// requireCancelled fails when ctx was not cancelled.
func requireCancelled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("the turn context was not cancelled")
	}
}

// requireNotCancelled fails when ctx was cancelled.
func requireNotCancelled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatal("the turn context was cancelled")
	default:
	}
}
