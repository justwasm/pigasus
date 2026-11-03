package compaction

import (
	"context"
	"sync"
)

// CompactionCancelledLiteral is the reference binary's exact copy when Esc
// cancels a compaction (rodata offset 0x1034fb1aa in agy 1.4.10, 0x103647b2e in 1.4.8):
// "Compaction cancelled. Conversation was not compacted."
const CompactionCancelledLiteral = "Compaction cancelled. Conversation was not compacted."

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
//
// The cycle-237 TUI wires this into the Esc handler in a future cycle
// (see 硬約束 in docs/cycles/cycle-237-compact/deliverable.md).
func TriggerCancel() {
	compactCancelMu.Lock()
	cancel := compactCancel
	compactCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
