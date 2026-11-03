// Package remotecontrol — terminals_registry_lifecycle.go:
// the close / subscribe / attach-cascade side of the
// Registry, plus the per-cascade drain goroutine
// that fans Output frames into terminal subscribers.
// Split out of terminals_registry.go in cycle
// rc-filesize-internal-remotecontrol.
//
// The lifecycle methods (Close / Subscribe / AttachCascade)
// take r.mu briefly for the map lookup and acquire the
// per-state mu for the rest. Subscribe runs a per-terminal
// unsubscribe callback that must be called from the
// goroutine that owns the drain loop — the helper takes
// state.mu as a writer so the unsubscribe path is
// race-free against a concurrent Close.
package terminals

import (
	"sync"
)

// close closes the chan once. Idempotent (sync.Once).
// Lives next to the subscriber struct definition so the
// lifecycle methods can call it without importing a
// helper file.
func (s *subscriber) close() {
	s.once.Do(func() {
		close(s.ch)
		close(s.closing)
	})
}

// Close marks the terminal as closed, stamps ClosedAt,
// and emits a single EventClose to every
// subscriber. Subsequent Send calls return
// ErrClosed.
//
// Returns ErrNotFound when terminalID is
// unknown. Closing an already-closed terminal returns
// ErrClosed — the dispatcher's handler maps that
// to -32019, matching the project surface convention
// (a re-close attempt by the same dashboard is a benign
// no-op flagged as "the terminal is no longer
// accepting input", not an unknown-id error).
//
// Concurrency: takes r.mu briefly for the lookup, then
// writes to the per-terminal state under its mu. The
// subscriber fan-out happens under the same write lock
// so a concurrent Subscribe can't interleave between the
// close broadcast and the close signal.
//
// The cascaded cascadeStop shutdown (if non-nil) runs
// inside the same critical section; the helper goroutine
// reads from the cascade's Output chan until the
// cancellation signal fires, then exits.
func (r *Registry) Close(terminalID string) error {
	state, ok := r.lookupState(terminalID)
	if !ok {
		return ErrNotFound
	}

	subs, cascadeStop, alreadyClosed := r.beginClose(state)
	if alreadyClosed {
		return ErrClosed
	}

	if cascadeStop != nil {
		close(cascadeStop)
	}

	fanOutClose(subs)
	return nil
}

// beginClose is the lock-held half of Close. It stamps
// ClosedAt, swaps the subscriber list to nil, captures
// the cascadeStop channel for the caller's helper to
// signal, and returns (subs, cascadeStop, alreadyClosed).
// Split out so Close itself stays under the 50-line
// function cap.
func (r *Registry) beginClose(state *termState) ([]*subscriber, chan struct{}, bool) {
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return nil, nil, true
	}
	state.closed = true
	state.t.ClosedAt = r.nowFunc()
	subs := append([]*subscriber(nil), state.subs...)
	state.subs = nil // drop the reference; subscribers
	// drain their buffered chans at their own pace.
	cascadeStop := state.cascadeStop
	state.cascadeStop = nil
	state.mu.Unlock()
	return subs, cascadeStop, false
}

// fanOutClose emits one EventClose to every
// subscriber, then closes each subscriber's buffered
// chan. Slow subscribers that already overran their
// buffered chan see a dropped close event — that's
// acceptable; the chan close below is the authoritative
// "terminal closed" signal.
func fanOutClose(subs []*subscriber) {
	for _, sub := range subs {
		select {
		case sub.ch <- Event{Kind: EventClose}:
		default:
			sub.dropped++
		}
		sub.close()
	}
}

// Subscribe registers a buffered chan on the terminal.
// The returned chan has capacity EventBufferCap
// (64); events the cascade chose to log (via the
// cascade's Output chan path) are routed in,
// plus the EventClose event when Close fires.
// The second return value is an Unsubscribe callback
// that removes the subscription and closes the chan; it
// is idempotent.
//
// Subscribe on an unknown terminalID returns
// ErrNotFound; Subscribe on an already-closed
// terminal returns ErrClosed (the chan is still
// returned, but every Send the registry makes on it
// has already landed; the consumer sees the close event
// immediately and exits).
//
// Concurrency: takes r.mu briefly for the lookup, then
// the per-terminal mu for the insert. Close's
// subscriber-fan-out is mutually exclusive with the
// subscribe path (both hold state.mu as writers); the
// guard is the per-state mutex.
func (r *Registry) Subscribe(terminalID string) (chan Event, func(), error) {
	state, ok := r.lookupState(terminalID)
	if !ok {
		return nil, nil, ErrNotFound
	}

	sub := &subscriber{
		ch:      make(chan Event, EventBufferCap),
		closing: make(chan struct{}),
	}

	state.mu.Lock()
	alreadyClosed := state.closed
	if !alreadyClosed {
		state.subs = append(state.subs, sub)
	}
	state.mu.Unlock()

	if alreadyClosed {
		// The terminal is gone — deliver one close
		// notification so the subscriber's read loop
		// exits cleanly. Don't bother returning the
		// unsubscribe func since there's nothing to
		// remove; the caller's drain-the-chan loop
		// terminates on the close event.
		select {
		case sub.ch <- Event{Kind: EventClose}:
		default:
		}
		sub.close()
		// Return the chan verbatim so the caller can
		// drain it (and discover the close event) in
		// the same code path it would have taken on
		// the happy path. The unsubscribe func is
		// still returned as a no-op so the caller's
		// defer doesn't have to special-case.
		return sub.ch, func() {}, nil
	}

	return sub.ch, buildUnsubscribe(state, sub), nil
}

// buildUnsubscribe returns the closure the
// Subscribe path hands back to the caller. The closure
// is idempotent (sync.Once) so the caller's defer is
// safe to call from any goroutine. Split out so the
// happy / already-closed branches of Subscribe stay
// visually compact.
func buildUnsubscribe(state *termState, sub *subscriber) func() {
	var unsubscribeOnce sync.Once
	return func() {
		unsubscribeOnce.Do(func() {
			state.mu.Lock()
			// Linear scan over subs; the slice is
			// small (a terminal has at most a few
			// subscribers in practice). Remove
			// the matching pointer and close the
			// chan.
			for i, s := range state.subs {
				if s == sub {
					state.subs = append(state.subs[:i], state.subs[i+1:]...)
					break
				}
			}
			state.mu.Unlock()
			sub.close()
		})
	}
}

// AttachCascade marks the terminal as bound to a
// cascade's Output chan and spawns a drain
// goroutine that forwards every cascade stream chunk
// as a EventOutput to the terminal's
// subscribers. The goroutine exits when either:
//
//	(a) the cascade's chan closes (cascade
//	    end-of-stream),
//	(b) the terminal is closed (Close calls close on
//	    state.cascadeStop), or
//	(c) the dispatcher's bridge loop drops the chan.
//
// Returns ErrNotFound when terminalID is
// unknown; ErrClosed when the terminal is
// already closed.
//
// AttachCascade is idempotent — a second call replaces
// the existing drain (signalled by closing the prior
// cascadeStop) and starts a new drain.
func (r *Registry) AttachCascade(terminalID string, cascadeOutput <-chan []byte) error {
	state, ok := r.lookupState(terminalID)
	if !ok {
		return ErrNotFound
	}

	state.mu.RLock()
	closed := state.closed
	state.mu.RUnlock()
	if closed {
		return ErrClosed
	}

	if cascadeOutput == nil {
		// Detach path: the dispatcher can call
		// AttachCascade(terminalID, nil) to
		// uninstall a previously-attached
		// cascade without closing the terminal.
		// The drain goroutine sees the close via
		// state.cascadeStop and returns.
		state.mu.Lock()
		if state.cascadeStop != nil {
			close(state.cascadeStop)
			state.cascadeStop = nil
		}
		state.mu.Unlock()
		return nil
	}

	state.mu.Lock()
	if state.cascadeStop != nil {
		close(state.cascadeStop) // signal prior drain to exit
	}
	stop := make(chan struct{})
	state.cascadeStop = stop
	state.mu.Unlock()

	// Spawn the drain goroutine outside the lock.
	go r.drainCascadeOutput(state, cascadeOutput, stop)
	return nil
}

// drainCascadeOutput is the per-(terminal, cascade)
// goroutine that fans cascade chunks into the
// terminal's subscribers. Exits when cascadeOutput
// closes (cascade end-of-stream), stop closes (terminal
// closed), or any subscribed channel's write would
// block forever (the chan send uses default-fallback to
// drop on a slow consumer — same pattern the registry
// uses for in-process Send).
//
// Idempotency / safety: closes are managed by the
// registry's subscribers themselves; the drain goroutine
// does not close them, it only writes to them.
func (r *Registry) drainCascadeOutput(state *termState, cascadeOutput <-chan []byte, stop <-chan struct{}) {
	if state == nil || cascadeOutput == nil {
		return
	}
	for {
		select {
		case chunk, ok := <-cascadeOutput:
			if !ok {
				return
			}
			r.forwardCascadeChunk(state, chunk)
		case <-stop:
			return
		}
	}
}

// forwardCascadeChunk fans one chunk to every
// subscriber of state. Mirrors Send's fan-out but uses
// the cascade's chunk (rather than dashboard-supplied
// data). The seq counter is per-terminal — the function
// bumps state.seq once per call and stamps the same seq
// on every subscriber's frame.
func (r *Registry) forwardCascadeChunk(state *termState, chunk []byte) {
	if state == nil || len(chunk) == 0 {
		return
	}
	seq := state.seq.next()

	state.mu.RLock()
	subs := append([]*subscriber(nil), state.subs...)
	state.mu.RUnlock()

	fanOutEvent(subs, Event{Kind: EventOutput, Data: append([]byte(nil), chunk...), Seq: seq})
}
