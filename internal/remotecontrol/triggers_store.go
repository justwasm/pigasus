// Package remotecontrol — triggers_store.go: the
// FSTriggerStore + onDiskTriggerState + MemTriggerStore
// types + the cross-store helpers (cloneTriggerState /
// cloneTrigger / cloneTriggerSpec / cloneTriggerSpecValue /
// sortTriggersByCreatedAt / readerFunc / readerFuncType /
// NewFSTriggerStore). The load / write / mutate / Snapshot /
// List / Create / Update / Delete methods live in
// triggers_store_fs.go (FSTriggerStore) and
// triggers_store_mem.go (MemTriggerStore).
//
// Why two implementations:
//
//   - FSTriggerStore is the production seam: one
//     triggers.json under
//     $ANTIGRAVITY_HOME/triggers.json. The atomic-rename
//     write matches convstore / antigravity-token /
//     permissions / plugins helpers' style.
//
//   - MemTriggerStore is the test seam: a state value
//     under a mutex. Tests inject this through
//     Config.TriggersStore so they don't need a tempdir +
//     real filesystem.
//
// Both share the same single-state storage shape; the
// wire envelope the dispatcher emits is identical
// regardless of which backend stored the state.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"io"
	"sort"
	"sync"
	"time"
)

// FSTriggerStore is the production TriggerStore
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
type FSTriggerStore struct {
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

// onDiskTriggerState is the JSON shape the FSTriggerStore
// writes to disk. The Version / Triggers / UpdatedAt
// fields map 1:1 onto TriggerState's JSON tags so the wire
// format and the on-disk format stay identical. Future
// versions of the file format (cycle n+1) write a new
// struct and branch on the Version stamp inside the
// reader.
type onDiskTriggerState struct {
	Version   int       `json:"version"`
	Triggers  []Trigger `json:"triggers"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewFSTriggerStore returns a fresh FSTriggerStore
// rooted at path. The store's lazy-on-first-mutation
// pattern means path may not exist on return; the
// directory is created on the first write.
//
// When now is nil the store uses time.Now (UTC). When
// randRead is nil the store uses crypto/rand.Read.
func NewFSTriggerStore(path string, now func() time.Time) *FSTriggerStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSTriggerStore{Path: path, now: now}
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

// MemTriggerStore is the in-memory TriggerStore fixture
// tests inject. It is a thin state-value + mutex; the
// methods implement the TriggerStore interface exactly
// the way FSTriggerStore does, with the differences
// that:
//
//   - There is no filesystem involvement: Snapshot on
//     a fresh MemTriggerStore returns a state with
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
// NewMemTriggerStore (empty + now) and call the
// methods directly.
type MemTriggerStore struct {
	mu    sync.RWMutex
	state TriggerState

	now func() time.Time

	// nextID is the integer id counter the in-memory
	// store stamps on Create. Tests can read it after a
	// Create to assert the id's structure.
	nextID int
}

// cloneTriggerState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Triggers slice).
func cloneTriggerState(state TriggerState) TriggerState {
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
		out.Spec = cloneTriggerSpec(t.Spec)
	}
	return out
}

// cloneTriggerSpec returns a deep-copy of spec so a
// caller can't mutate the inner map / slice the store
// has stashed.
func cloneTriggerSpec(spec TriggerSpec) TriggerSpec {
	if spec == nil {
		return nil
	}
	out := make(TriggerSpec, len(spec))
	for k, v := range spec {
		out[k] = cloneTriggerSpecValue(v)
	}
	return out
}

// cloneTriggerSpecValue deep-copies a TriggerSpec value
// so a caller can't mutate an inner slice / map through
// the snapshot. Strings and time.Time are immutable, so
// they pass through verbatim. Slices / maps get their own
// copies. nil maps / slices stay nil.
func cloneTriggerSpecValue(v any) any {
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
			out[k] = cloneTriggerSpecValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = cloneTriggerSpecValue(vv)
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

// sortTriggersByCreatedAt orders triggers in place by
// CreatedAt ascending. Stable secondary sort by
// TriggerID ascending so the wire surface (List) sees a
// stable sequence regardless of slice growth history.
// Both stores apply this so the wire surface sees a
// stable order regardless of which backend stored the
// state.
func sortTriggersByCreatedAt(triggers []Trigger) {
	sort.SliceStable(triggers, func(i, j int) bool {
		if triggers[i].CreatedAt.Equal(triggers[j].CreatedAt) {
			return triggers[i].TriggerID < triggers[j].TriggerID
		}
		return triggers[i].CreatedAt.Before(triggers[j].CreatedAt)
	})
}
