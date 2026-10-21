// Package remotecontrol — permissions_store_fs.go:
// FSPermissionStore (the JSON-file-backed production
// store). Pulled out of permissions_store.go in cycle
// rc-filesize-internal-remotecontrol so the parent file
// can hold the test fixture (MemPermissionStore) +
// shared helpers without exceeding the 500-line cap.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FSPermissionStore is the production PermissionStore
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
type FSPermissionStore struct {
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

// onDiskPermissionState is the JSON shape the
// FSPermissionStore writes to disk. The Version /
// Preset / Review / Grants / UpdatedAt fields map
// 1:1 onto PermissionState's JSON tags so the wire
// format and the on-disk format stay identical. Future
// versions of the file format (cycle n+1) write a new
// struct and branch on the Version stamp inside the
// reader.
type onDiskPermissionState struct {
	Version   int                `json:"version"`
	Preset    PermissionPreset   `json:"preset"`
	Review    ArtifactReviewMode `json:"review"`
	Grants    []PermissionGrant  `json:"grants"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// NewFSPermissionStore returns a fresh FSPermissionStore
// rooted at path. The store's lazy-on-first-mutation
// pattern means path may not exist on return; the
// directory is created on the first write.
//
// when now is nil, the store uses time.Now (UTC).
func NewFSPermissionStore(path string, now func() time.Time) *FSPermissionStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSPermissionStore{Path: path, now: now}
}

// ensureParentDir creates the parent directory of
// FSPermissionStore.Path if it doesn't exist.
// Single-shot (sync.Once) so concurrent first-use
// calls don't race on the MkdirAll.
func (s *FSPermissionStore) ensureParentDir() error {
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
func (s *FSPermissionStore) load() (PermissionState, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PermissionState{}, false, nil
		}
		return PermissionState{}, false, err
	}
	var rec onDiskPermissionState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error up so the
		// dispatcher surfaces it; the operator sees
		// "permissions.json is corrupt" in the wire
		// envelope instead of a silent fallback to
		// defaults.
		return PermissionState{}, true, fmt.Errorf("rc-permissions: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the on-disk layout
	// before the version stamp existed). A reader that
	// finds a version > PermissionStateVersion refuses to
	// touch the file (returns a "newer binary required"
	// envelope).
	if rec.Version == 0 {
		rec.Version = PermissionStateVersion
	}
	if rec.Version > PermissionStateVersion {
		return PermissionState{}, true, fmt.Errorf("rc-permissions: %s version=%d is newer than supported %d", s.Path, rec.Version, PermissionStateVersion)
	}
	if rec.Grants == nil {
		// Normalise nil → empty slice so the wire emits
		// `[]` rather than `null`.
		rec.Grants = []PermissionGrant{}
	}
	return PermissionState{
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
func (s *FSPermissionStore) atomicWrite(state PermissionState) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskPermissionState{
		Version:   PermissionStateVersion,
		Preset:    state.Preset,
		Review:    state.Review,
		Grants:    state.Grants,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Grants == nil {
		rec.Grants = []PermissionGrant{}
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
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
// defaults documented in PermissionPreset /
// ArtifactReviewMode, an empty grants slice, and the
// wall-clock UpdatedAt. The store hands this back
// when Get finds no file.
func (s *FSPermissionStore) defaultState() PermissionState {
	return PermissionState{
		Version:   PermissionStateVersion,
		Preset:    DefaultPermissionPreset,
		Review:    DefaultArtifactReviewMode,
		Grants:    []PermissionGrant{},
		UpdatedAt: s.now(),
	}
}

// Get returns the current state. No file → the
// default state (preset=default, review=always,
// grants=[]). A malformed file bubbles the parse
// error up.
func (s *FSPermissionStore) Get(ctx context.Context) (PermissionState, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return PermissionState{}, err
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
func (s *FSPermissionStore) SetPreset(ctx context.Context, preset PermissionPreset) (PermissionState, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return PermissionState{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	state.Preset = preset
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return PermissionState{}, err
	}
	return state, nil
}

// SetReview sets the artifact-review mode, bumps
// UpdatedAt, and atomic-saves. Same locking
// semantics as SetPreset.
func (s *FSPermissionStore) SetReview(ctx context.Context, mode ArtifactReviewMode) (PermissionState, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return PermissionState{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	state.Review = mode
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return PermissionState{}, err
	}
	return state, nil
}

// AddGrants appends grants to the state. Each grant
// is keyed on (tool_name, pattern); a duplicate key
// returns ErrPermissionGrantExists without mutating.
//
// Updates to an existing grant's Reason are allowed:
// when a duplicate key comes in, the existing entry's
// Reason is overwritten and AddedAt is left alone (so
// the audit trail reflects the original add time, not
// the latest re-add).
//
// Nil grants → no-op (returns the current state
// unchanged). Per-grant validation rejects empty
// tool_name, empty pattern, and zero AddedAt (the
// store stamps AddedAt on first add).
//
// The dispatcher parses "<tool_name>:<pattern>" into
// the PermissionGrant.ToolName / .Pattern before
// calling; this method does not re-decode them.
func (s *FSPermissionStore) AddGrants(ctx context.Context, grants []PermissionGrant) (PermissionState, error) {
	_ = ctx
	if len(grants) == 0 {
		// Idempotent no-op: round-trip the current state so the
		// dashboard's request envelope lands coherently.
		s.mu.RLock()
		state, exists, err := s.load()
		s.mu.RUnlock()
		if err != nil {
			return PermissionState{}, err
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
		return PermissionState{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	now := s.now()
	if err := validateAndAppendGrants(&state.Grants, grants, now, nil); err != nil {
		return PermissionState{}, err
	}
	state.UpdatedAt = now
	if err := s.atomicWrite(state); err != nil {
		return PermissionState{}, err
	}
	return state, nil
}

// RemoveGrants drops the grants identified by the
// supplied "<tool_name>:<pattern>" keys. A malformed
// key (no colon or one half empty) surfaces as
// ErrPermissionGrantMalformed — the dispatcher's
// param-validation should have caught most of these,
// but the store enforces defence-in-depth.
//
// Unknown keys are silently dropped (idempotent
// remove — the dashboard can call Remove twice and
// the second call is a no-op).
//
// Nil or empty keys → no-op (returns the current
// state unchanged).
func (s *FSPermissionStore) RemoveGrants(ctx context.Context, keys []string) (PermissionState, error) {
	_ = ctx
	if len(keys) == 0 {
		s.mu.RLock()
		state, exists, err := s.load()
		s.mu.RUnlock()
		if err != nil {
			return PermissionState{}, err
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
		return PermissionState{}, err
	}
	if !exists {
		state = s.defaultState()
	}
	parsed, err := parseRemoveKeys(keys)
	if err != nil {
		return PermissionState{}, err
	}
	filterGrantsByKeys(&state.Grants, parsed)
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return PermissionState{}, err
	}
	return state, nil
}
