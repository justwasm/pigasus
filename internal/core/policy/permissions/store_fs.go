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

// FSStore is the JSON-file-backed production Store implementation.
type FSStore struct {
	Path string

	now func() time.Time

	mu sync.RWMutex

	ensureDirOnce sync.Once
}

type onDiskState struct {
	Version   int        `json:"version"`
	Preset    Preset     `json:"preset"`
	Review    ReviewMode `json:"review"`
	Grants    []Grant    `json:"grants"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// NewFSStore returns a fresh FSStore rooted at path.
func NewFSStore(path string, now func() time.Time) *FSStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSStore{Path: path, now: now}
}

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
		return State{}, true, fmt.Errorf("rc-permissions: parse %s: %w", s.Path, err)
	}
	if rec.Version == 0 {
		rec.Version = StateVersion
	}
	if rec.Version > StateVersion {
		return State{}, true, fmt.Errorf("rc-permissions: %s version=%d is newer than supported %d", s.Path, rec.Version, StateVersion)
	}
	if rec.Grants == nil {
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
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

func (s *FSStore) defaultState() State {
	return State{
		Version:   StateVersion,
		Preset:    DefaultPreset,
		Review:    DefaultReviewMode,
		Grants:    []Grant{},
		UpdatedAt: s.now(),
	}
}

// Get returns the current state.
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

// SetPreset sets the preset, bumps UpdatedAt, and saves.
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

// SetReview sets the artifact-review mode, bumps UpdatedAt, and saves.
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

// AddGrants appends grants to the state.
func (s *FSStore) AddGrants(ctx context.Context, grants []Grant) (State, error) {
	_ = ctx
	if len(grants) == 0 {
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

// RemoveGrants drops the grants identified by the supplied keys.
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
