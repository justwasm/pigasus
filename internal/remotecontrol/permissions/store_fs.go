// Package permissions — store_fs.go:
// FSStore (the JSON-file-backed production
// store). Pulled out of store_mem.go in cycle
// rc-filesize-internal-remotecontrol so the parent file
// can hold the test fixture (MemStore) +
// shared helpers without exceeding the 500-line cap.
package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/justwasm/pigasus/atomicfile"
)

// FSStore is the production Store
// implementation. The state lives in a single JSON file at
// Path; the store serialises every read-modify-write
// (Set / AddGrants / RemoveGrants) through a single
// mutex.
//
// Concurrency: the store uses a single RWMutex so the
// dashboard CRUD surface is serialised. The brief
// doesn't call for high concurrency on the permissions
// surface — four short-lived mutations on a single file
// per minute is typical — so a single mutex is correct
// and cheap. A future cycle that grows to high-frequency
// polling only has to swap mu for a per-mutation RWMutex.
type FSStore struct {
	// Path is the absolute path to the JSON file the
	// store reads / writes. Production wiring uses
	// auth.AntigravityHome()/permissions.json; tests pass
	// a t.TempDir()-rooted path.
	Path string

	// now is the clock the store stamps UpdatedAt
	// (and AddedAt on AddGrants) with. nil → time.Now
	// (production). Tests override it to make the
	// timestamps deterministic.
	now func() time.Time

	// mu serialises the per-mutation read-modify-write
	// cycle. Held only across the in-memory mutation +
	// atomic save; snapshot reads (Get) take the read
	// lock so concurrent reads don't race a writer's
	// in-progress mutation.
	mu sync.RWMutex

	// ensureDirDone guards the directory MkdirAll so
	// concurrent first-use calls don't race.
	ensureDirOnce sync.Once
}

// onDiskState is the JSON shape the
// FSStore writes to disk. The Version /
// Preset / Review / Grants / UpdatedAt fields map
// 1:1 onto State's JSON tags so the wire
// format and the on-disk format stay identical. Future
// versions of the file format (cycle n+1) write a new
// struct and branch on the Version stamp inside the
// reader.
type onDiskState struct {
	Version   int        `json:"version"`
	Preset    Preset     `json:"preset"`
	Review    ReviewMode `json:"review"`
	Grants    []Grant    `json:"grants"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// NewFSStore returns a fresh FSStore
// rooted at path. The store's lazy-on-first-mutation
// pattern means path may not exist on return; the
// directory is created on the first write.
//
// when now is nil, the store uses time.Now (UTC).
func NewFSStore(path string, now func() time.Time) *FSStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSStore{Path: path, now: now}
}

// ensureParentDir creates the parent directory of
// FSStore.Path if it doesn't exist.
// Single-shot (sync.Once) so concurrent first-use
// calls don't race on the MkdirAll.
func (s *FSStore) ensureParentDir() error {
	var err error
	s.ensureDirOnce.Do(func() {
		dir := filepath.Dir(s.Path)
		if dir == "" || dir == "." {
			return
		}
		err = os.MkdirAll(dir, 0o700)
	})
	return err
}

// load reads the on-disk state. Returns:
//   - (zero, false, nil) — no file exists (a fresh
//     host). The caller hands back the defaults;
//   - (state, true, nil) — the file existed and parsed
//     cleanly;
//   - (state, true, err) — the file existed but is
//     malformed; the dispatcher maps this to -32603
//     because the dashboard should not see "no
//     permissions" when one is corrupt (the operator
//     needs to know).
func (s *FSStore) load() (State, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, false, nil
		}
		return State{}, false, err
	}
	var rec onDiskState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error up so the
		// dispatcher surfaces it; the operator sees
		// "permissions.json is corrupt" in the wire
		// envelope instead of a silent fallback to
		// defaults.
		return State{}, true, fmt.Errorf("rc-permissions: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the on-disk layout
	// before the version stamp existed). A reader that
	// finds a version > StateVersion refuses to
	// touch the file (returns a "newer binary required"
	// envelope).
	if rec.Version == 0 {
		rec.Version = StateVersion
	}
	if rec.Version > StateVersion {
		return State{}, true, fmt.Errorf("rc-permissions: %s version=%d is newer than supported %d", s.Path, rec.Version, StateVersion)
	}
	if rec.Grants == nil {
		// Normalise nil → empty slice so the wire emits
		// `[]` rather than `null`.
		rec.Grants = []Grant{}
	}
	return State{
		Version:   rec.Version,
		Preset:    rec.Preset,
		Review:    rec.Review,
		Grants:    rec.Grants,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state to disk.
// Temp file + fsync-via-Rename gives crash-consistent
// semantics; the directory is fsynced too (matches
// antigravity-token's style).
func (s *FSStore) atomicWrite(state State) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskState{
		Version:   StateVersion,
		Preset:    state.Preset,
		Review:    state.Review,
		Grants:    state.Grants,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Grants == nil {
		rec.Grants = []Grant{}
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(s.Path, data, 0o600); err != nil {
		return err
	}
	// Best-effort dir fsync: ensures the rename is
	// durable across a power loss. Best-effort because a
	// platform-level fsync on the directory may not be
	// available — we tolerate the failure silently
	// (matching antigravity-token's style).
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

// defaultState returns the fresh-host state: the
// defaults documented in Preset /
// ReviewMode, an empty grants slice, and the
// wall-clock UpdatedAt. The store hands this back
// when Get finds no file.
func (s *FSStore) defaultState() State {
	return State{
		Version:   StateVersion,
		Preset:    DefaultPreset,
		Review:    DefaultReviewMode,
		Grants:    []Grant{},
		UpdatedAt: s.now(),
	}
}

// Get returns the current state. No file → the
// default state (preset=default, review=always,
// grants=[]). A malformed file bubbles the parse
// error up.
func (s *FSStore) Get(ctx context.Context) (State, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		return s.defaultState(), nil
	}
	return state, nil
}

// SetPreset sets the preset, bumps UpdatedAt, and
// atomic-saves. The dispatcher has already validated
// the preset against IsValidPreset, so the store
// trusts the value.
//
// The read-modify-write runs under s.mu so concurrent
// Set / Add / Remove calls serialise; a Set's read
// snap and write commit happen under the same lock.
func (s *FSStore) SetPreset(ctx context.Context, preset Preset) (State, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	state.Preset = preset
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return State{}, err
	}
	return state, nil
}

// SetReview sets the artifact-review mode, bumps
// UpdatedAt, and atomic-saves. Same locking
// semantics as SetPreset.
func (s *FSStore) SetReview(ctx context.Context, mode ReviewMode) (State, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	state.Review = mode
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return State{}, err
	}
	return state, nil
}

// AddGrants appends grants to the state. Each grant is
// keyed on its own key — the reference's "action(target)"
// or the legacy "tool_name:pattern"; a duplicate key
// returns ErrGrantExists without mutating.
//
// Updates to an existing grant's Reason are allowed:
// when a duplicate key comes in, the existing entry's
// Reason is overwritten and AddedAt is left alone (so
// the audit trail reflects the original add time, not
// the latest re-add).
//
// Nil grants → no-op (returns the current state
// unchanged). Per-grant validation rejects an empty
// action, an empty tool_name, an empty pattern, and zero
// AddedAt (the store stamps AddedAt on first add).
//
// The dispatcher parses the grant string into the
// Grant.Action / .ToolName / .Pattern before
// calling; this method does not re-decode them.
func (s *FSStore) AddGrants(ctx context.Context, grants []Grant) (State, error) {
	_ = ctx
	if len(grants) == 0 {
		// Idempotent no-op: round-trip the current state so the
		// dashboard's request envelope lands coherently.
		s.mu.RLock()
		state, exists, err := s.load()
		s.mu.RUnlock()
		if err != nil {
			return State{}, err
		}
		if !exists {
			return s.defaultState(), nil
		}
		return state, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	now := s.now()
	if err := validateAndAppendGrants(&state.Grants, grants, now, nil); err != nil {
		return State{}, err
	}
	state.UpdatedAt = now
	if err := s.atomicWrite(state); err != nil {
		return State{}, err
	}
	return state, nil
}

// RemoveGrants drops the grants identified by the
// supplied keys — the reference's "action(target)" grant
// or the legacy "<tool_name>:<pattern>" one. A malformed
// key (neither form) surfaces as ErrGrantMalformed — the
// dispatcher's param-validation should have caught most of
// these, but the store enforces defence-in-depth.
//
// Unknown keys are silently dropped (idempotent
// remove — the dashboard can call Remove twice and
// the second call is a no-op).
//
// Nil or empty keys → no-op (returns the current
// state unchanged).
func (s *FSStore) RemoveGrants(ctx context.Context, keys []string) (State, error) {
	_ = ctx
	if len(keys) == 0 {
		s.mu.RLock()
		state, exists, err := s.load()
		s.mu.RUnlock()
		if err != nil {
			return State{}, err
		}
		if !exists {
			return s.defaultState(), nil
		}
		return state, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	parsed, err := parseRemoveKeys(keys)
	if err != nil {
		return State{}, err
	}
	filterGrantsByKeys(&state.Grants, parsed)
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return State{}, err
	}
	return state, nil
}
