// Package remotecontrol — terminals_registry.go: the
// concurrency-safe in-memory registry the cycle's five
// JSON-RPC methods call into.
//
// Why a dedicated file (not embedded in terminals.go): the
// data model is small and immutable-feeling; the registry
// owns the per-terminal state machine (Create / List /
// Send / Resize / Close / Subscribe / AttachCascade) and
// the per-cascade drain goroutine that bridges the
// cascade's Output chan to the terminal's
// subscribers.
//
// Why a single registry, not per-cascade stores: the brief
// expects "every terminal is reachable from every ws
// connection a dashboard opens on a given dispatcher", so
// the registry sits on the dispatcher
// (Config.Registry) just like the cascade registry
// does. The registry's internal state is per-terminal;
// multiple cascades share the same registry and route
// events through cascade_id filter down-stream.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - terminals_registry.go         — struct, constructor, Get, List, nowFunc
//   - terminals_registry_io.go      — Create / Send / Resize
//   - terminals_registry_lifecycle.go — Close / Subscribe / AttachCascade +
//     drainCascadeOutput / forwardCascadeChunk / subscriber.close
//   - terminals_registry_helpers.go — mintID / sortByCreatedAt /
//     normalizeID / trimSpaceASCII
//
// Concurrency discipline:
//
//   - Per-terminal state lives inside a per-terminal
//     *termState pointer with its own mutex.
//     The outer mutex (r.mu) only guards the terms slice
//     map itself; the slice map lookups are O(1) hash
//     hits, and the per-terminal accessors hold their own
//     mutex for state mutations. This mirrors the
//     rc-resources / rc-triggers precedent: short
//     critical sections, read-heavy access paths don't
//     serialise on the outer mutex.
//
//   - Subscribers get a buffered chan (cap
//     EventBufferCap = 64). The drain path
//     sends non-blocking — a slow subscriber falls
//     behind, the chan fills, and subsequent sends
//     drop events with a counter bump (sub.dropped).
//     The drain goroutine only blocks on the cascade
//     output close signal.
//
//   - Close broadcasts a single EventClose to
//     every subscriber, then closes the buffered chans.
//     A subscriber that reads after a close sees the
//     chan yield zero values (the closed chan yields
//     the zero Event{} on receive) and exits
//     its read loop.
//
//   - Subscribe returns both a chan and an unsubscribe
//     func. The unsubscribe func removes the subscriber
//     from the registry's subscriber list and closes
//     the chan (idempotent). The caller must call
//     unsubscribe from the goroutine that owns the
//     drain loop to avoid a "close-while-sending" race;
//     the cycle's helper takes the per-terminal
//     subscriber mutex to make that safe.
//
// Sentinel map:
//
//	missing terminal_id     → ErrNotFound
//	resize / close / send
//	  on a closed terminal  → ErrClosed
//	send on output-kind    → ErrInputRejected
//	unknown id              → ErrNotFound
//
// Stdlib-only. No third-party deps.
package terminals

import (
	"sync"
	"time"
)

// subscriber is one per-terminal subscription. The chan is
// buffered with EventBufferCap (64) slots; the
// dropped counter bumps on a non-blocking send that
// returned false (slow client overran its chan).
type subscriber struct {
	ch chan Event

	// dropped is the optional drop counter. Tests can
	// use SubscriberDropped(id) to read it; the field
	// itself is guarded by Registry's atomic
	// helpers (not directly exported).
	dropped uint64

	// closing lets Send / forwardCascadeChunk exit
	// cleanly when the chan is closed under their feet.
	closing chan struct{}
	once    sync.Once
}

// termState is the per-terminal mutable state. The
// per-terminal mu guards every field except closed (which
// is a sync/atomic.Bool for the hot read in Send / Resize
// — lock-free reads against the per-state mu keep the hot
// path cheap).
type termState struct {
	mu sync.RWMutex

	// t is the canonical Terminal record. Cols / Rows /
	// ClosedAt are mutating fields the registry reads /
	// writes under mu.
	t Terminal

	// closed is the atomic "Close has fired" flag.
	// Read on the Send / Resize hot path without taking
	// the per-terminal mu.
	closed bool

	// subs is the per-terminal subscriber list. The
	// slice is guarded by the per-terminal mu — a
	// Send walks it under read lock; an Unsubscribe
	// mutates it under write lock.
	subs []*subscriber

	// seq drives the per-terminal monotonic sequence
	// number the registry stamps on every output
	// event. Reset to zero in fresh-state; the
	// Subscribe path also starts at zero. The
	// terminal/output notification exposes seq so a
	// subscriber can detect drops.
	seq seqCounter

	// cascadeStop is the per-cascade drain goroutine's
	// cancellation channel. nil for unattached
	// terminals; non-nil when AttachCascade has been
	// called. The cancellation channel is closed by
	// Close to stop the drain.
	cascadeStop chan struct{}
}

// Registry is the in-memory registry of all
// currently-open terminals the dispatcher has seen. The
// structure is per-process — a fresh dispatcher has its
// own registry; multiple dispatchers (rare; the test
// harness sometimes spins several) get their own
// isolated state.
//
// Lifecycle: NewRegistry returns a fresh empty
// registry. The registry survives any single dispatcher
// reconstruction (Close-on-empty is a no-op). Today's
// cycle does not persist the registry to disk — a future
// rc-terminals-persist cycle adds FS support.
type Registry struct {
	// mu guards the `terms` map only. The per-terminal
	// state has its own mu; the outer mu is held only
	// for the duration of an insert / lookup.
	mu sync.RWMutex

	// terms maps terminal_id → *termState. A nil
	// termState is unreachable (Create always
	// seeds one); a closed state remains in the map
	// until future cycles decide to GC (today the GC
	// is deferred to dispatcher shutdown).
	terms map[string]*termState

	// now is the clock source. nil →
	// time.Now().UTC(). Set via
	// NewRegistryWithNow so tests can pin
	// the timestamp.
	now func() time.Time
}

// Store is the domain's own store interface; the in-memory
// registry is the implementation the host installs today.
var _ Store = (*Registry)(nil)

// NewRegistry returns a fresh, empty
// Registry with the default clock source
// (time.Now().UTC()).
func NewRegistry() *Registry {
	return NewRegistryWithNow(nil)
}

// NewRegistryWithNow returns a fresh registry
// with the supplied clock source. now == nil falls back
// to time.Now().UTC(); tests inject a fixed-time function
// so the stamped CreatedAt / ClosedAt timestamps are
// deterministic.
func NewRegistryWithNow(now func() time.Time) *Registry {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Registry{
		terms: map[string]*termState{},
		now:   now,
	}
}

// nowFunc returns the registry's clock source. Centralised
// so future getters (if any) reuse the same nil-safe
// fallback the constructors apply.
func (r *Registry) nowFunc() time.Time {
	if r == nil {
		return time.Now().UTC()
	}
	return r.now()
}

// lookupState is the shared helper that resolves a
// terminalID to its *termState. It is the only
// caller that takes the outer r.mu read lock; every
// accessor funnels through here so the lock-pair ordering
// (r.mu first, then per-state mu) stays consistent across
// the file.
//
// Returns (nil, false) when the registry is nil, when the
// id is unknown, or when the stored state pointer is
// itself nil. Callers map a "not found" result to
// ErrNotFound at the public surface.
func (r *Registry) lookupState(terminalID string) (*termState, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	state, ok := r.terms[terminalID]
	r.mu.RUnlock()
	if !ok || state == nil {
		return nil, false
	}
	return state, true
}

// Get returns the Terminal record for terminalID, or
// (_, false) when the registry has no row (either it
// was never created or it's already been GC'd). The
// returned Terminal is a value copy so the caller can
// mutate it without racing the registry.
//
// Concurrency: takes r.mu read lock briefly for the map
// hit, then acquires the per-terminal mu briefly to
// copy the record. The lock-pair ordering is consistent
// across the file (r.mu first, then per-state mu).
func (r *Registry) Get(terminalID string) (Terminal, bool) {
	state, ok := r.lookupState(terminalID)
	if !ok {
		return Terminal{}, false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.t, true
}

// List returns the canonical rows of the registry,
// filtered by cascadeID. Empty cascadeID means "every
// row" (the ListRemoteAgentTerminals no-filter branch);
// non-empty cascadeID means "rows with a matching
// CascadeID". Rows are sorted by CreatedAt ascending so
// dashboards see a stable enumeration order (matches the
// rc-triggers / rc-resources / rc-fastpush-toggle
// precedent).
//
// Concurrency: takes r.mu read lock briefly for the
// snapshot, then walks the per-terminal rows under their
// own mu. The returned slice is freshly allocated so the
// caller can sort / filter without racing the registry.
func (r *Registry) List(cascadeID string) []Terminal {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	rows := r.snapshotRows(cascadeID)
	r.mu.RUnlock()
	sortByCreatedAt(rows)
	return rows
}

// snapshotRows is the inner helper of List that walks the
// terms map under the outer read lock and copies each
// matching Terminal record into a fresh slice. Split out
// so the caller (List) can release the outer mu before
// the sort step runs — sortByCreatedAt is
// allocation-heavy and does not need r.mu.
func (r *Registry) snapshotRows(cascadeID string) []Terminal {
	rows := make([]Terminal, 0, len(r.terms))
	for _, state := range r.terms {
		if state == nil {
			continue
		}
		state.mu.RLock()
		row := state.t
		closed := state.closed
		state.mu.RUnlock()
		_ = closed
		if cascadeID == "" || row.CascadeID == cascadeID {
			rows = append(rows, row)
		}
	}
	return rows
}
