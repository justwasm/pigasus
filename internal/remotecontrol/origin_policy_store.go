// Package remotecontrol — origin_policy_store.go: the
// OriginPolicyState struct + OriginPolicyStore interface
// + OriginPolicyStateVersion constant +
// errOriginPolicyPersistenceFailed sentinel +
// FSOriginPolicyStore / onDiskOriginPolicyState /
// MemOriginPolicyStore types + the cross-store helpers
// (cloneOriginPolicyState / mapOriginPolicyError /
// NewFSOriginPolicyStore / NewMemOriginPolicyStore /
// NewMemOriginPolicyStoreWith). The load / write /
// Snapshot / SetPatterns / Subscribe / fanOut methods
// live in origin_policy_store_fs.go (FSOriginPolicyStore)
// and origin_policy_store_mem.go (MemOriginPolicyStore).
//
// Why two implementations:
//
//   - FSOriginPolicyStore is the production
//     seam: one origin_policy.json under
//     $ANTIGRAVITY_HOME/origin_policy.json. The
//     atomic-rename write matches the
//     FSFastpushStore / FSPermissionStore /
//     FSPluginStore / FSTriggerStore /
//     FSResourceStore helpers' style.
//
//   - MemOriginPolicyStore is the test seam:
//     a state value under a mutex.
//
// Both share the same single-state storage
// shape; the wire envelope the dispatcher
// emits is identical regardless of which
// backend stored the state.
package remotecontrol

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/originpolicy"
)

// FSOriginPolicyStore is the production
// OriginPolicyStore implementation. The state
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
type FSOriginPolicyStore struct {
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

// onDiskOriginPolicyState is the JSON shape
// the FSOriginPolicyStore writes to disk.
// The Version / Patterns / UpdatedAt fields
// map 1:1 onto OriginPolicy's JSON tags so
// the wire format and the on-disk format
// stay identical.
type onDiskOriginPolicyState struct {
	Version   int       `json:"version"`
	Patterns  []string  `json:"patterns"`
	UpdatedAt time.Time `json:"updated_at"`
}

// errOriginPolicyPersistenceFailed is the
// store's I/O-failure sentinel. The
// dispatcher maps it to -32603 "Internal
// error" so a dashboard can render a
// distinguished error envelope (vs. the
// -32602 envelope the per-pattern
// validator returns on malformed input).
var errOriginPolicyPersistenceFailed = errors.New("rc-origin-policy: persistence failed")

// OriginPolicyState is the on-disk / wire
// shape the JSON file holds.
type OriginPolicyState struct {
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
	// alphabetically by OriginPolicyStore
	// on every successful Set so the wire
	// surface sees a stable order.
	Patterns []string `json:"patterns"`

	// UpdatedAt is the wall-clock stamp
	// the store bumped at the most-recent
	// mutation. RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// OriginPolicyStore is the seam the single
// SetRemoteControlOriginPatterns handler
// calls into. Snapshot / SetPatterns +
// Subscribe. Cycle rc-auth-tokens.
type OriginPolicyStore interface {
	Snapshot(ctx context.Context) (OriginPolicyState, error)
	SetPatterns(ctx context.Context, patterns []string) (OriginPolicyState, []string, bool, error)
	Subscribe() (chan originpolicy.OriginPolicy, func())
}

// OriginPolicyStateVersion is the on-disk
// format version the current store writes.
const OriginPolicyStateVersion = 1

// NewFSOriginPolicyStore returns a fresh
// FSOriginPolicyStore rooted at path. The
// store's lazy-on-first-mutation pattern
// means path may not exist on return; the
// directory is created on the first write.
//
// when now is nil, the store uses time.Now
// (UTC).
func NewFSOriginPolicyStore(path string, now func() time.Time) *FSOriginPolicyStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSOriginPolicyStore{Path: path, now: now}
}

// MemOriginPolicyStore is the in-memory
// OriginPolicyStore fixture tests inject.
// It is a thin state value + mutex; the
// methods implement the OriginPolicyStore
// interface exactly the way
// FSOriginPolicyStore does, with the
// differences that:
//
//   - There is no filesystem involvement:
//     Snapshot on a fresh MemOriginPolicyStore
//     returns a state with patterns=[] and
//     the supplied now() stamp.
//
//   - It does not touch the clock —
//     UpdatedAt is stamped with the
//     supplied now function so tests can
//     drive monotonic time.
type MemOriginPolicyStore struct {
	mu sync.RWMutex
	// state is the in-memory state.
	state OriginPolicyState

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

// NewMemOriginPolicyStore returns a fresh,
// empty MemOriginPolicyStore. now is used
// to stamp UpdatedAt; nil → time.Now
// (UTC).
func NewMemOriginPolicyStore(now func() time.Time) *MemOriginPolicyStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemOriginPolicyStore{
		state: OriginPolicyState{
			Version:   OriginPolicyStateVersion,
			Patterns:  []string{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemOriginPolicyStoreWith returns a
// fresh MemOriginPolicyStore pre-populated
// with the supplied patterns slice. The
// slice is deep-copied (and sorted
// alphabetically) so a test that mutates
// the input does not race the store's
// internal state.
func NewMemOriginPolicyStoreWith(patterns []string, now func() time.Time) *MemOriginPolicyStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, strings.TrimSpace(p))
	}
	sort.Strings(out)
	return &MemOriginPolicyStore{
		state: OriginPolicyState{
			Version:   OriginPolicyStateVersion,
			Patterns:  out,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// cloneOriginPolicyState returns a
// deep-copy of state so callers can't
// observe in-place mutations.
func cloneOriginPolicyState(state OriginPolicyState) OriginPolicyState {
	out := state
	if state.Patterns != nil {
		patterns := make([]string, len(state.Patterns))
		copy(patterns, state.Patterns)
		out.Patterns = patterns
	}
	return out
}

// mapOriginPolicyError maps the store
// sentinel errors to the JSON-RPC
// envelopes the brief calls for.
//
//	errOriginPolicyPersistenceFailed → -32603 "Internal error"
//	  (other / I/O)
//	validator failure (per-pattern)   → -32602 "Invalid params"
//	  (the handler emits this directly
//	  via invalidParams; never via this
//	  helper)
//
// Mirrors mapFastpushError /
// mapPluginsError / mapTriggersError's
// shape. -32602 is the per-pattern invalid
// envelope; -32603 is the catch-all I/O /
// "no store installed" envelope.
func mapOriginPolicyError(err error, op string) *rpcErrorBody {
	if err == nil {
		return nil
	}
	if errors.Is(err, errOriginPolicyPersistenceFailed) {
		return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
	return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
}
