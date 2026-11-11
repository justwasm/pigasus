package remotetransport

import (
	"context"
	"fmt"
	"sync"
)

// DisabledDispatcher is a stub Dispatcher that rejects all RPC calls
// with a descriptive error. Used when running in UI-only mode or when
// an agent backend is not bound.
type DisabledDispatcher struct {
	Reason string

	mu    sync.Mutex
	count uint64
}

func (d *DisabledDispatcher) errMessage() string {
	if d.Reason != "" {
		return d.Reason
	}
	return "backend RPC service is disabled"
}

// Dispatch returns a JSON-RPC error frame.
func (d *DisabledDispatcher) Dispatch(_ context.Context, _ []byte) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    -32000,
			"message": d.errMessage(),
		},
	}
}

// DispatchDirect returns an error rejecting direct Connect-RPC dispatch.
func (d *DisabledDispatcher) DispatchDirect(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("rpc disabled: %s", d.errMessage())
}

// Accepted returns the cumulative connection count.
func (d *DisabledDispatcher) Accepted() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.count
}

// MarkAccepted increments the connection count.
func (d *DisabledDispatcher) MarkAccepted() {
	d.mu.Lock()
	d.count++
	d.mu.Unlock()
}

// NotifyConnected is a no-op stub.
func (d *DisabledDispatcher) NotifyConnected() {}

// NotifyDisconnected is a no-op stub.
func (d *DisabledDispatcher) NotifyDisconnected() {}

// Notify is a no-op stub.
func (d *DisabledDispatcher) Notify(_ context.Context, _, _ string, _ any) error {
	return nil
}

// SetNotifier is a no-op stub.
func (d *DisabledDispatcher) SetNotifier(_ func(ctx context.Context, connID string, frame []byte) error) {
}

// WithConnID returns the context unchanged.
func (d *DisabledDispatcher) WithConnID(ctx context.Context, _ string) context.Context {
	return ctx
}

// SetBroadcaster is a no-op stub.
func (d *DisabledDispatcher) SetBroadcaster(_ func(ctx context.Context, method string, params any) error) {
}
