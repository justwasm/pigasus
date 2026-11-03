// Package remotecontrol — origin_policy_store.go: the
// State struct + Store interface
// + StateVersion constant +
// errPersistenceFailed sentinel +
// FSStore / onDiskState /
// MemStore types + the cross-store helpers
// (cloneState / mapError /
// NewFSStore / NewMemStore /
// NewMemStoreWith). The load / write /
// Snapshot / SetPatterns / Subscribe / fanOut methods
// live in origin_policy_store_fs.go (FSStore)
// and origin_policy_store_mem.go (MemStore).
//
// Why two implementations:
//
//   - FSStore is the production
//     seam: one origin_policy.json under
//     $ANTIGRAVITY_HOME/origin_policy.json. The
//     atomic-rename write matches the
//     FSFastpushStore / FSPermissionStore /
//     plugins.FSStore / FSTriggerStore /
//     resources.FSStore helpers' style.
//
//   - MemStore is the test seam:
//     a state value under a mutex.
//
// Both share the same single-state storage
// shape; the wire envelope the dispatcher
// emits is identical regardless of which
// backend stored the state.
package origins

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
	"github.com/justwasm/pigasus/originpolicy"
)

// FSStore is the production
// Store implementation. The state
// lives in a single JSON file at Path; the
// store serialises every read-modify-write
// (SetPatterns + Subscribe fan-out) through a
// single RWMutex.
//
// Concurrency: a single RWMutex guards both
// the persistence side and the in-memory
// subscriber fan-out. Snapshot reads take the
// read lock; SetPatterns takes the write lock
// so the on-disk JSON, the in-memory snapshot,
// and the broadcast iterator stay lock-step.
type FSStore struct {
	// Path is the absolute path to the JSON
	// file the store reads / writes.
	// Production wiring uses
	// auth.AntigravityHome()/origin_policy.json;
	// tests pass a t.TempDir()-rooted path.
	Path string

	// now is the clock the store stamps
	// UpdatedAt with. nil → time.Now
	// (UTC).
	now func() time.Time

	// mu serialises the per-mutation
	// read-modify-write cycle.
	mu sync.RWMutex

	// subMu guards the subscribers slice. A
	// separate mutex (from mu above) lets a
	// Subscribe call complete without
	// holding mu — a Subscribe-blocked
	// writer would stall every Set; the
	// brief's "drop-on-backpressure"
	// semantics (cap=16 buffered chans)
	// means a slow subscriber should NOT
	// block the writer path either.
	subMu sync.Mutex
	// subscribers is the list of buffered
	// chans the store broadcasts each
	// successful Set on. cap=16 per the
	// brief; nil → no subscribers.
	subscribers []chan originpolicy.OriginPolicy
	// nextSubID is the monotonically-
	// increasing ID used to mint the
	// unsubscribe token.
	nextSubID uint64

	// ensureDirOnce guards the directory
	// MkdirAll so concurrent first-use
	// calls don't race.
	ensureDirOnce sync.Once
}

// onDiskState is the JSON shape
// the FSStore writes to disk.
// The Version / Patterns / UpdatedAt fields
// map 1:1 onto OriginPolicy's JSON tags so
// the wire format and the on-disk format
// stay identical.
type onDiskState struct {
	Version   int       `json:"version"`
	Patterns  []string  `json:"patterns"`
	UpdatedAt time.Time `json:"updated_at"`
}

// errPersistenceFailed is the
// store's I/O-failure sentinel. The
// dispatcher maps it to -32603 "Internal
// error" so a dashboard can render a
// distinguished error envelope (vs. the
// -32602 envelope the per-pattern
// validator returns on malformed input).
var errPersistenceFailed = errors.New("rc-origin-policy: persistence failed")

// State is the on-disk / wire
// shape the JSON file holds.
type State struct {
	// Version is the on-disk format
	// version. Bumped on breaking changes
	// (renamed keys, removed fields);
	// non-breaking additions (new optional
	// fields with omitempty) leave the
	// version alone.
	Version int `json:"version"`

	// Patterns is the allowlist. nil →
	// empty slice ([]string{}) so the wire
	// always sees `[]`. Sorted
	// alphabetically by Store
	// on every successful Set so the wire
	// surface sees a stable order.
	Patterns []string `json:"patterns"`

	// UpdatedAt is the wall-clock stamp
	// the store bumped at the most-recent
	// mutation. RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the seam the single
// SetRemoteControlOriginPatterns handler
// calls into. Snapshot / SetPatterns +
// Subscribe. Cycle rc-auth-tokens.
type Store interface {
	Snapshot(ctx context.Context) (State, error)
	SetPatterns(ctx context.Context, patterns []string) (State, []string, bool, error)
	Subscribe() (chan originpolicy.OriginPolicy, func())
}

// StateVersion is the on-disk
// format version the current store writes.
const StateVersion = 1

// NewFSStore returns a fresh
// FSStore rooted at path. The
// store's lazy-on-first-mutation pattern
// means path may not exist on return; the
// directory is created on the first write.
//
// when now is nil, the store uses time.Now
// (UTC).
func NewFSStore(path string, now func() time.Time) *FSStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSStore{Path: path, now: now}
}

// MemStore is the in-memory
// Store fixture tests inject.
// It is a thin state value + mutex; the
// methods implement the Store
// interface exactly the way
// FSStore does, with the
// differences that:
//
//   - There is no filesystem involvement:
//     Snapshot on a fresh MemStore
//     returns a state with patterns=[] and
//     the supplied now() stamp.
//
//   - It does not touch the clock —
//     UpdatedAt is stamped with the
//     supplied now function so tests can
//     drive monotonic time.
type MemStore struct {
	mu sync.RWMutex
	// state is the in-memory state.
	state State

	// subMu guards the subscribers slice.
	subMu sync.Mutex
	// subscribers is the list of buffered
	// chans the store broadcasts each
	// successful Set on. cap=16 per the
	// brief.
	subscribers []chan originpolicy.OriginPolicy
	// nextSubID is reserved for the
	// unsubscribe token's cross-store ID.
	nextSubID uint64

	// now is the clock UpdatedAt is
	// stamped with. nil → time.Now (UTC).
	now func() time.Time
}

// NewMemStore returns a fresh,
// empty MemStore. now is used
// to stamp UpdatedAt; nil → time.Now
// (UTC).
func NewMemStore(now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Patterns:  []string{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemStoreWith returns a
// fresh MemStore pre-populated
// with the supplied patterns slice. The
// slice is deep-copied (and sorted
// alphabetically) so a test that mutates
// the input does not race the store's
// internal state.
func NewMemStoreWith(patterns []string, now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, strings.TrimSpace(p))
	}
	sort.Strings(out)
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Patterns:  out,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// cloneState returns a
// deep-copy of state so callers can't
// observe in-place mutations.
func cloneState(state State) State {
	out := state
	if state.Patterns != nil {
		patterns := make([]string, len(state.Patterns))
		copy(patterns, state.Patterns)
		out.Patterns = patterns
	}
	return out
}

// mapError maps the store
// sentinel errors to the JSON-RPC
// envelopes the brief calls for.
//
//	errPersistenceFailed → -32603 "Internal error"
//	  (other / I/O)
//	validator failure (per-pattern)   → -32602 "Invalid params"
//	  (the handler emits this directly
//	  via invalidParams; never via this
//	  helper)
//
// Mirrors mapFastpushError /
// plugins.MapError / triggers.MapError's
// shape. -32602 is the per-pattern invalid
// envelope; -32603 is the catch-all I/O /
// "no store installed" envelope.
func mapError(err error, op string) *rpc.ErrorBody {
	if err == nil {
		return nil
	}
	if errors.Is(err, errPersistenceFailed) {
		return &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
	return &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
}
