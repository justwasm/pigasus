package session

// turn_runtime.go — the session's turn cancellation state.
//
// The runtime owns the one thing a turn's cancellation needs: the cancel
// function of the context the turn runs under, together with whether a
// turn is in flight. It lives in the app tier so the UI host does not carry
// a mutex and two booleans of its own; EngineBridge and any future local
// turn runner can share it.

import (
	"context"
	"sync"
)

// TurnRuntime tracks the in-flight turn and its cancellation.
//
// The zero value is usable. Every method is safe for concurrent use: the
// UI goroutine cancels while the turn's goroutine begins and ends.
type TurnRuntime struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	runID   uint64
}

// NewTurnRuntime returns an empty turn runtime.
func NewTurnRuntime() *TurnRuntime { return &TurnRuntime{} }

// Begin starts a turn under parent and returns its context together with
// the function that ends it. The end function is idempotent and only
// clears the state when this run is still the current one.
func (r *TurnRuntime) Begin(parent context.Context) (context.Context, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	r.mu.Lock()
	r.runID++
	id := r.runID
	r.cancel = cancel
	r.running = true
	r.mu.Unlock()

	return ctx, func() {
		r.mu.Lock()
		if r.runID == id {
			r.cancel = nil
			r.running = false
		}
		r.mu.Unlock()
		cancel()
	}
}

// Cancel cancels the in-flight turn. It reports whether a turn was active.
func (r *TurnRuntime) Cancel() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel == nil {
		return false
	}
	r.cancel()
	r.cancel = nil
	r.running = false
	return true
}

// Running reports whether a turn is in flight.
func (r *TurnRuntime) Running() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
