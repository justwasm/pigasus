package compaction

import (
	"context"
	"testing"
)

func TestCompactionCancel(t *testing.T) {
	// TriggerCancel with no active cancel is a safe no-op.
	TriggerCancel()

	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())
	cleanup := InstallCancel(func() {
		cancelled = true
		cancel()
	})

	TriggerCancel()
	if !cancelled {
		t.Errorf("expected cancel func to be triggered")
	}
	if ctx.Err() == nil {
		t.Errorf("expected context to be cancelled")
	}

	cleanup()
	cancelled = false
	TriggerCancel()
	if cancelled {
		t.Errorf("expected cancel func not to be triggered after cleanup")
	}
}
