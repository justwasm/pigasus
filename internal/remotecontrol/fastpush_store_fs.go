// Package remotecontrol — fastpush_store_fs.go:
// FSFastpushStore (the JSON-file-backed production
// store). Pulled out of fastpush_store.go in cycle
// rc-filesize-internal-remotecontrol so the parent file
// can hold the test fixture (MemFastpushStore) +
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

// FSFastpushStore is the production FastpushStore
// implementation. The state lives in a single JSON file
// at Path; the store serialises every read-modify-write
// (SetEnabled) through a single mutex. Snapshot / Get
// read paths take the read lock so concurrent reads
// don't race a writer's in-progress mutation.
//
// Concurrency: the store uses a single RWMutex so the
// dashboard's per-cascade toggle surface is serialised.
// The brief doesn't call for high concurrency on the
// fastpush surface — toggling a per-cascade bit is
// human-paced, not high-frequency — so a single mutex
// is correct and cheap. A future cycle that grows to
// high-frequency polling only has to swap mu for a
// per-cascade RWMutex.
type FSFastpushStore struct {
	// Path is the absolute path to the JSON file the
	// store reads / writes. Production wiring uses
	// auth.AntigravityHome()/fastpush.json; tests
	// pass a t.TempDir()-rooted path.
	Path string

	// now is the clock the store stamps UpdatedAt
	// with. nil → time.Now (production). Tests
	// override it to make the timestamps
	// deterministic.
	now func() time.Time

	// checkCascade is the optional existence-check
	// closure SetEnabled / Get invoke before any
	// mutation. The closure returns true iff the
	// supplied cascadeID is currently registered with
	// the dispatcher's CascadeRegistry; the store
	// returns ErrFastpushCascadeNotFound otherwise.
	//
	// nil disables the check (the store happily
	// accepts any cascade_id — useful for tests that
	// don't bother constructing a registry). Today
	// every production caller wires a checkCascade
	// closure built from the dispatcher it's serving.
	checkCascade func(cascadeID string) bool

	// mu serialises the per-mutation read-modify-write
	// cycle. Held only across the in-memory mutation +
	// atomic save; snapshot reads (Snapshot / Get)
	// take the read lock so concurrent reads don't
	// race a writer's in-progress mutation.
	mu sync.RWMutex

	// ensureDirOnce guards the directory MkdirAll so
	// concurrent first-use calls don't race.
	ensureDirOnce sync.Once
}

// onDiskFastpushState is the JSON shape the
// FSFastpushStore writes to disk. The Version /
// Settings / UpdatedAt fields map 1:1 onto
// FastpushState's JSON tags so the wire format and the
// on-disk format stay identical. Future versions of the
// file format (cycle n+1) write a new struct and branch
// on the Version stamp inside the reader.
type onDiskFastpushState struct {
	Version   int               `json:"version"`
	Settings  []FastpushSetting `json:"settings"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// NewFSFastpushStore returns a fresh FSFastpushStore
// rooted at path. The store's lazy-on-first-mutation
// pattern means path may not exist on return; the
// directory is created on the first write.
//
// when now is nil, the store uses time.Now (UTC). The
// returned store has no cascade-existence check; the
// dispatcher surfaces unknown cascade_ids via its
// (*CascadeRegistry).Get path.
func NewFSFastpushStore(path string, now func() time.Time) *FSFastpushStore {
	return NewFSFastpushStoreWithCheck(path, now, nil)
}

// NewFSFastpushStoreWithCheck mirrors NewFSFastpushStore
// but installs a checkCascade closure the store invokes
// before every SetEnabled / Get. Useful for production
// wiring where the dispatcher already has a registry
// reference and wants the store's existence check to
// delegate to it.
func NewFSFastpushStoreWithCheck(path string, now func() time.Time, checkCascade func(cascadeID string) bool) *FSFastpushStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSFastpushStore{Path: path, now: now, checkCascade: checkCascade}
}

// ensureParentDir creates the parent directory of
// FSFastpushStore.Path if it doesn't exist.
// Single-shot (sync.Once) so concurrent first-use
// calls don't race on the MkdirAll.
func (s *FSFastpushStore) ensureParentDir() error {
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
//     host). The caller hands back emptyState;
//   - (state, true, nil) — the file existed and parsed
//     cleanly;
//   - (state, true, err) — the file existed but is
//     malformed; the dispatcher maps this to -32603
//     because the dashboard should not see "no
//     fastpush settings" when one is corrupt (the
//     operator needs to know).
func (s *FSFastpushStore) load() (FastpushState, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FastpushState{}, false, nil
		}
		return FastpushState{}, false, err
	}
	var rec onDiskFastpushState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error up
		// so the dispatcher surfaces it; the operator
		// sees "fastpush.json is corrupt" in the wire
		// envelope instead of a silent fallback to
		// defaults.
		return FastpushState{}, true, fmt.Errorf("rc-fastpush: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the on-disk
	// layout before the version stamp existed). A
	// reader that finds a version > FastpushStateVersion
	// refuses to touch the file (returns a "newer
	// binary required" envelope).
	if rec.Version == 0 {
		rec.Version = FastpushStateVersion
	}
	if rec.Version > FastpushStateVersion {
		return FastpushState{}, true, fmt.Errorf("rc-fastpush: %s version=%d is newer than supported %d", s.Path, rec.Version, FastpushStateVersion)
	}
	if rec.Settings == nil {
		rec.Settings = []FastpushSetting{}
	}
	return FastpushState{
		Version:   rec.Version,
		Settings:  rec.Settings,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state to disk.
// Temp file + fsync-via-Rename gives crash-consistent
// semantics; the directory is fsynced too (matches
// antigravity-token's style).
func (s *FSFastpushStore) atomicWrite(state FastpushState) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskFastpushState{
		Version:   FastpushStateVersion,
		Settings:  state.Settings,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Settings == nil {
		rec.Settings = []FastpushSetting{}
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
	// durable across a power loss. Best-effort because
	// a platform-level fsync on the directory may not
	// be available — we tolerate the failure silently
	// (matching antigravity-token's style).
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

// emptyState returns the fresh-host state: a v1
// FastpushState with no settings. The store hands this
// back when load() reports the file is missing.
func (s *FSFastpushStore) emptyState() FastpushState {
	return FastpushState{
		Version:   FastpushStateVersion,
		Settings:  []FastpushSetting{},
		UpdatedAt: s.now(),
	}
}

// Snapshot returns a defensive copy of the on-disk
// state (sorted by CascadeID). A fresh host with no
// fastpush.json returns emptyState (no error).
//
// Concurrency: takes s.mu read lock so concurrent
// writes don't race. The sort is applied to the cloned
// slice (not the in-memory backing array) so a Snapshot
// call doesn't perturb subsequent state, but the
// returned state is always in the canonical order.
func (s *FSFastpushStore) Snapshot(ctx context.Context) (FastpushState, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return FastpushState{}, err
	}
	if !exists {
		return s.emptyState(), nil
	}
	out := cloneFastpushState(state)
	sortSettingsByCascadeID(out.Settings)
	return out, nil
}

// SetEnabled upserts the entry for cascadeID with the
// supplied enabled value and atomic-writes the result.
// Unknown / whitespace-only cascade_id, or a cascade_id
// the checkCascade closure rejects, returns
// ErrFastpushCascadeNotFound (the dispatcher surfaces
// this as -32004 "Cascade not found" — matches the
// SendMessage surface so dashboards' error filters stay
// uniform).
//
// The cascade-existence check uses the dispatcher's
// (*CascadeRegistry).Get via a callback (checkCascade),
// installed at NewFSFastpushStoreWithCheck time. nil
// checkCascade disables the existence check (the store
// happily accepts any cascade_id — useful for tests that
// don't bother constructing a registry). Today every
// production caller wires a checkCascade closure built
// from the dispatcher it's serving.
func (s *FSFastpushStore) SetEnabled(ctx context.Context, cascadeID string, enabled bool) (FastpushSetting, error) {
	_ = ctx
	id, ok := s.normalisedCascadeID(cascadeID)
	if !ok {
		return FastpushSetting{}, ErrFastpushCascadeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return FastpushSetting{}, err
	}
	if !exists {
		state = s.emptyState()
	}
	now := s.now()
	setting := upsertFastpushSetting(&state.Settings, id, enabled, now)
	state.UpdatedAt = now
	if err := s.atomicWrite(state); err != nil {
		return FastpushSetting{}, err
	}
	sortSettingsByCascadeID(state.Settings)
	return cloneFastpushSetting(setting), nil
}

// Get returns the setting for cascadeID. Unknown
// cascadeID → ErrFastpushCascadeNotFound. A cascade
// that has no stored row returns the zero-valued
// FastpushSetting with no error — callers that need a
// strict "has it been touched?" answer can compare
// UpdatedAt.IsZero().
func (s *FSFastpushStore) Get(ctx context.Context, cascadeID string) (FastpushSetting, error) {
	_ = ctx
	id, ok := s.normalisedCascadeID(cascadeID)
	if !ok {
		return FastpushSetting{}, ErrFastpushCascadeNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return FastpushSetting{}, err
	}
	if !exists {
		// Fresh host with no fastpush.json yet —
		// the cascade has no stored row. Return a
		// zero-valued setting (no error) so the
		// dispatcher's "Get" path can distinguish
		// "no row" from "unknown cascade".
		return FastpushSetting{CascadeID: id}, nil
	}
	for _, set := range state.Settings {
		if set.CascadeID == id {
			return cloneFastpushSetting(set), nil
		}
	}
	return FastpushSetting{CascadeID: id}, nil
}

// normalisedCascadeID centralises the trim + existence
// check. Returns (id, true) on success or ("", false)
// when the id is empty / fails the closure. Pulled out
// of SetEnabled + Get so the two share the exact same
// gating rule.
func (s *FSFastpushStore) normalisedCascadeID(raw string) (string, bool) {
	id := normaliseCascadeID(raw)
	if id == "" {
		return "", false
	}
	if s.checkCascade != nil && !s.checkCascade(id) {
		return "", false
	}
	return id, true
}

// upsertFastpushSetting inserts or replaces the row
// for cascadeID on *settings. Pulled out of the two
// SetEnabled implementations so the per-row upsert
// logic is auditable without paging through the
// validate + clock + mutex choreography. The settings
// slice is NOT sorted in-place here — the caller is
// responsible for the post-mutation sort.
func upsertFastpushSetting(settings *[]FastpushSetting, cascadeID string, enabled bool, now time.Time) FastpushSetting {
	setting := FastpushSetting{
		CascadeID:    cascadeID,
		Enabled:      enabled,
		Channel:      FastpushChannelName,
		PushDelivery: FastpushPushDeliveryDeferred,
		UpdatedAt:    now,
	}
	idx := -1
	for i, set := range *settings {
		if set.CascadeID == cascadeID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		(*settings)[idx] = setting
	} else {
		*settings = append(*settings, setting)
	}
	return setting
}
