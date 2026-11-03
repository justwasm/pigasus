// Package triggers — store.go: the
// FSStore + onDiskState + MemStore
// types + the cross-store helpers (cloneState /
// cloneTrigger / cloneSpec / cloneSpecValue /
// sortByCreatedAt / readerFunc / readerFuncType /
// NewFSStore). The load / write / mutate / Snapshot /
// List / Create / Update / Delete methods live in
// store_fs.go (FSStore) and
// store_mem.go (MemStore).
//
// Why two implementations:
//
//   - FSStore is the production seam: one
//     triggers.json under
//     $ANTIGRAVITY_HOME/triggers.json. The atomic-rename
//     write matches convstore / antigravity-token /
//     permissions / plugins helpers' style.
//
//   - MemStore is the test seam: a state value
//     under a mutex. Tests inject this through
//     Config.TriggersStore so they don't need a tempdir +
//     real filesystem.
//
// Both share the same single-state storage shape; the
// wire envelope the dispatcher emits is identical
// regardless of which backend stored the state.
//
// Stdlib-only. No third-party deps.
package triggers

import (
	"io"
	"sort"
	"sync"
	"time"
)

// FSStore is the production Store
// implementation. The state lives in a single JSON file at
// Path; the store serialises every read-modify-write
// (Create / Update / Delete) through a single mutex.
// Snapshot / List read paths take the read lock so
// concurrent reads don't race a writer's in-progress
// mutation.
//
// Concurrency: the store uses a single RWMutex so the
// dashboard CRUD surface is serialised. The brief doesn't
// call for high concurrency on the trigger surface —
// dashboard mutations are human-paced — so a single mutex
// is correct and cheap. A future cycle that grows to
// high-frequency polling only has to swap mu for a
// per-id RWMutex.
type FSStore struct {
	// Path is the absolute path to the JSON file the
	// store reads / writes. Production wiring uses
	// auth.AntigravityHome()/triggers.json; tests pass
	// a t.TempDir()-rooted path.
	Path string

	// now is the clock the store stamps UpdatedAt (and
	// CreatedAt on Create) with. nil → time.Now
	// (production). Tests override it to make the
	// timestamps deterministic.
	now func() time.Time

	// randRead reads 16 bytes of entropy for the
	// generated trigger id. nil → crypto/rand.Read
	// (production). Tests may inject a deterministic
	// reader for snapshot-style verification.
	randRead func(p []byte) (int, error)

	// mu serialises the per-mutation read-modify-write
	// cycle. Held only across the in-memory mutation +
	// atomic save; read paths (Snapshot / List) take
	// the read lock so concurrent reads don't race a
	// writer's in-progress mutation.
	mu sync.RWMutex

	// ensureDirOnce guards the directory MkdirAll so
	// concurrent first-use calls don't race.
	ensureDirOnce sync.Once
}

// onDiskState is the JSON shape the FSStore
// writes to disk. The Version / Triggers / UpdatedAt
// fields map 1:1 onto State's JSON tags so the wire
// format and the on-disk format stay identical. Future
// versions of the file format (cycle n+1) write a new
// struct and branch on the Version stamp inside the
// reader.
type onDiskState struct {
	Version   int       `json:"version"`
	Triggers  []Trigger `json:"triggers"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewFSStore returns a fresh FSStore
// rooted at path. The store's lazy-on-first-mutation
// pattern means path may not exist on return; the
// directory is created on the first write.
//
// When now is nil the store uses time.Now (UTC). When
// randRead is nil the store uses crypto/rand.Read.
func NewFSStore(path string, now func() time.Time) *FSStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSStore{Path: path, now: now}
}

// readerFunc adapts a `(p []byte) (int, error)` callback
// into an io.Reader so we can pass it to io.ReadFull.
// Today only crypto/rand.Read is wrapped; future support
// for testing may wrap a slice-filling closure.
func readerFunc(read func(p []byte) (int, error)) io.Reader {
	return readerFuncType{read: read}
}

type readerFuncType struct {
	read func(p []byte) (int, error)
}

func (r readerFuncType) Read(p []byte) (int, error) {
	return r.read(p)
}

// MemStore is the in-memory Store fixture
// tests inject. It is a thin state-value + mutex; the
// methods implement the Store interface exactly
// the way FSStore does, with the differences
// that:
//
//   - There is no filesystem involvement: Snapshot on
//     a fresh MemStore returns a state with
//     triggers=[].
//
//   - It does not touch the clock — UpdatedAt and
//     CreatedAt are stamped with the supplied now
//     function so tests can drive monotonic time.
//
//   - It does not touch crypto/rand — TriggerID is
//     stamped with a deterministic counter the test
//     can predict (e.g. "tri_test_0001"), keeping
//     snapshot-style tests free from entropy.
//
// The fields are unexported; tests construct it via
// NewMemStore (empty + now) and call the
// methods directly.
type MemStore struct {
	mu    sync.RWMutex
	state State

	now func() time.Time

	// nextID is the integer id counter the in-memory
	// store stamps on Create. Tests can read it after a
	// Create to assert the id's structure.
	nextID int
}

// cloneState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Triggers slice).
func cloneState(state State) State {
	out := state
	if state.Triggers != nil {
		triggers := make([]Trigger, 0, len(state.Triggers))
		for _, t := range state.Triggers {
			triggers = append(triggers, cloneTrigger(t))
		}
		out.Triggers = triggers
	}
	return out
}

// cloneTrigger returns a deep-copy of t so a caller
// can't mutate the store's internal state through the
// returned snapshot.
func cloneTrigger(t Trigger) Trigger {
	out := t
	if t.Spec != nil {
		out.Spec = cloneSpec(t.Spec)
	}
	return out
}

// cloneSpec returns a deep-copy of spec so a
// caller can't mutate the inner map / slice the store
// has stashed.
func cloneSpec(spec Spec) Spec {
	if spec == nil {
		return nil
	}
	out := make(Spec, len(spec))
	for k, v := range spec {
		out[k] = cloneSpecValue(v)
	}
	return out
}

// cloneSpecValue deep-copies a Spec value
// so a caller can't mutate an inner slice / map through
// the snapshot. Strings and time.Time are immutable, so
// they pass through verbatim. Slices / maps get their own
// copies. nil maps / slices stay nil.
func cloneSpecValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return x
	case bool:
		return x
	case int:
		return x
	case int64:
		return x
	case float64:
		return x
	case time.Time:
		return x
	case []string:
		out := make([]string, len(x))
		copy(out, x)
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = cloneSpecValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = cloneSpecValue(vv)
		}
		return out
	default:
		// Unknown type — pass through verbatim
		// rather than silently dropping. A future
		// spec field type only has to extend this
		// switch.
		return v
	}
}

// sortByCreatedAt orders triggers in place by
// CreatedAt ascending. Stable secondary sort by
// TriggerID ascending so the wire surface (List) sees a
// stable sequence regardless of slice growth history.
// Both stores apply this so the wire surface sees a
// stable order regardless of which backend stored the
// state.
func sortByCreatedAt(triggers []Trigger) {
	sort.SliceStable(triggers, func(i, j int) bool {
		if triggers[i].CreatedAt.Equal(triggers[j].CreatedAt) {
			return triggers[i].TriggerID < triggers[j].TriggerID
		}
		return triggers[i].CreatedAt.Before(triggers[j].CreatedAt)
	})
}
