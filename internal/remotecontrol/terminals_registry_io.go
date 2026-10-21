// Package remotecontrol — terminals_registry_io.go:
// the create / send / resize side of the TerminalsRegistry.
// Split out of terminals_registry.go in cycle
// rc-filesize-internal-remotecontrol to keep each file
// under the 500-line project cap.
//
// Concurrency notes mirror the parent file: short outer-mu
// critical sections, per-state mu for the actual mutation,
// non-blocking subscriber fan-out.
package remotecontrol

import (
	"time"
)

// Create inserts a new Terminal into the registry. The
// brief pins cols=80, rows=24 defaults — applied here
// when the dashboard-supplied t.Cols or t.Rows is zero.
// The terminal_id is server-minted as tml_<16hex bytes
// hex> (8 random bytes = 16 hex chars; matches the
// cycle-5 cascade id format precisely).
//
// Returns the inserted Terminal with stamped CreatedAt
// and a freshly-minted terminal_id; the supplied t.Cols
// / t.Rows / t.Kind / t.CascadeID are honoured verbatim
// (defaulted where zero).
//
// Create is nil-safe at the receiver level (a nil
// TerminalsRegistry returns ErrTerminalNotFound — matching
// the cycle precedent for "no registry installed"
// sentinels, even though Create wouldn't normally hit
// that path).
//
// Concurrency: takes r.mu briefly to insert into the
// terms map; the per-terminal state has its own mu.
func (r *TerminalsRegistry) Create(t Terminal) (Terminal, error) {
	if r == nil {
		return Terminal{}, ErrTerminalNotFound
	}
	id, err := mintTerminalID()
	if err != nil {
		return Terminal{}, err
	}
	t = applyTerminalDefaults(t)
	t.TerminalID = id
	t.CreatedAt = r.nowFunc()
	t.ClosedAt = time.Time{} // zero — omitempty on the wire

	state := &terminalState{t: t}

	r.mu.Lock()
	r.terms[id] = state
	r.mu.Unlock()

	return t, nil
}

// applyTerminalDefaults fills in the brief-pinned 80x24
// fallback for Cols / Rows when the caller-supplied value
// is non-positive. Centralised so a future factory
// function can reuse the same defaulting rule without
// diverging from Create's behaviour.
func applyTerminalDefaults(t Terminal) Terminal {
	if t.Cols <= 0 {
		t.Cols = DefaultTerminalCols
	}
	if t.Rows <= 0 {
		t.Rows = DefaultTerminalRows
	}
	return t
}

// Send writes data to the terminal identified by
// terminalID and broadcasts the bytes to every
// subscriber of that terminal. The data is fanned out
// verbatim via each subscriber's buffered chan (cap 64;
// non-blocking send — slow subscribers lose frames, no
// pin). The per-subscriber seq stamp is taken once for
// the whole Send call so every subscriber sees the same
// Seq on the resulting frame.
//
// Returns the byte count consumed so the
// SendRemoteAgentTerminalInput envelope's
// bytes_consumed:N field mirrors the input length.
//
// Returns ErrTerminalNotFound when terminalID is
// unknown; ErrTerminalClosed when the terminal has been
// closed (the Send call lands as -32019);
// ErrTerminalInputRejected when the terminal kind is
// "output" (mirrors the brief's read-only-consumer
// contract — dashboards that want to write must create
// a "input" kind terminal).
//
// Concurrency: takes r.mu briefly to look up the state,
// then sends to every subscriber under the per-terminal
// mu. The send is non-blocking (select with default →
// drop + bumped counter) so a slow subscriber never
// pins the caller's goroutine.
//
// The dispatcher additionally routes the bytes through
// Dispatcher.BroadcastNotify so cross-ws-connection
// subscribers also see the terminal/output frame. That
// fan-out happens in the dispatcher's Send handler —
// the registry itself only does the per-terminal
// in-process fan-out.
func (r *TerminalsRegistry) Send(terminalID string, data []byte) (int, error) {
	state, ok := r.lookupState(terminalID)
	if !ok {
		return 0, ErrTerminalNotFound
	}
	state.mu.RLock()
	closed := state.closed
	kind := state.t.Kind
	subs := append([]*subscriber(nil), state.subs...)
	state.mu.RUnlock()
	if closed {
		return 0, ErrTerminalClosed
	}
	if kind == TerminalOutput {
		return 0, ErrTerminalInputRejected
	}
	if len(data) == 0 {
		return 0, nil
	}
	seq := state.seq.next()
	fanOutTerminalEvent(subs, TerminalEvent{Kind: TerminalEventOutput, Data: append([]byte(nil), data...), Seq: seq})
	return len(data), nil
}

// fanOutTerminalEvent writes one TerminalEvent to every
// subscriber's buffered chan with a non-blocking send.
// Slow subscribers see dropped++ so the registry's
// SubscriberDropped helper can surface the count to
// dashboards. Centralised so Send and forwardCascadeChunk
// share the same fan-out rule.
func fanOutTerminalEvent(subs []*subscriber, ev TerminalEvent) {
	for _, sub := range subs {
		select {
		case sub.ch <- ev:
		default:
			sub.dropped++
		}
	}
}

// Resize updates the cols / rows of an open terminal.
// Returns ErrTerminalNotFound when terminalID is
// unknown; ErrTerminalClosed when the terminal has been
// closed. Non-positive cols/rows are accepted as "no
// change" (the brief pins a 80x24 fallback which Create
// applied; a Resize post-creation with cols <= 0 would
// otherwise silently re-zero the value). The cycle's
// tests pin the "non-positive = no-op" rule.
//
// Concurrency: takes r.mu briefly for the lookup, then
// the per-terminal mu for the mutation. Mutation lands
// under write lock so a concurrent List sees a
// consistent snapshot.
func (r *TerminalsRegistry) Resize(terminalID string, cols, rows int) error {
	state, ok := r.lookupState(terminalID)
	if !ok {
		return ErrTerminalNotFound
	}
	state.mu.RLock()
	closed := state.closed
	state.mu.RUnlock()
	if closed {
		return ErrTerminalClosed
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if cols > 0 {
		state.t.Cols = cols
	}
	if rows > 0 {
		state.t.Rows = rows
	}
	return nil
}
