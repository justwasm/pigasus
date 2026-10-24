package compaction

import (
	"context"
	"sync"
)

var (
	compactCancelMu sync.Mutex
	compactCancel   context.CancelFunc
)

// InstallCancel atomically installs cancel as the active compaction cancel func.
// The returned cleanup func clears the slot when the compaction finishes.
func InstallCancel(cancel context.CancelFunc) func() {
	compactCancelMu.Lock()
	compactCancel = cancel
	compactCancelMu.Unlock()
	return func() {
		compactCancelMu.Lock()
		compactCancel = nil
		compactCancelMu.Unlock()
	}
}

// TriggerCancel fires the in-flight compaction cancellation if one is active.
// Calling it when no compaction is running is a safe no-op.
func TriggerCancel() {
	compactCancelMu.Lock()
	cancel := compactCancel
	compactCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
