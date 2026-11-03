// Package plugins — store_fs.go: the
// FSStore implementation. The production seam
// that writes $ANTIGRAVITY_HOME/plugins.json on every
// mutation.
//
// Why a sibling file (not the parent
// store.go): the FS path pulls in
// encoding/json / os / path/filepath / fmt — the
// Mem variant below doesn't need any of that — and
// each implementation is naturally ~200 lines. Split
// to keep both files comfortably under the 500-line
// ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Atomicity: every write goes through atomicWrite
// (tempfile + rename + best-effort dir fsync). Read
// paths (Snapshot) take the read lock so a write in
// flight doesn't hand back a half-mutated state.
//
// Stdlib-only. No third-party deps.
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/justwasm/pigasus/atomicfile"
)

// NewFSStore returns a fresh FSStore rooted at
// path. The store's lazy-on-first-mutation pattern means
// path may not exist on return; the directory is created on
// the first write.
//
// when now is nil, the store uses time.Now (UTC).
func NewFSStore(path string, now func() time.Time) *FSStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSStore{Path: path, now: now}
}

// ensureParentDir creates the parent directory of
// FSStore.Path if it doesn't exist. Single-shot
// (sync.Once) so concurrent first-use calls don't race on
// the MkdirAll.
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
//
//   - (zero, false, nil) — no file exists (a fresh host).
//     The caller hands back the fixture-seeded state;
//   - (state, true, nil) — the file existed and parsed
//     cleanly;
//   - (state, true, err) — the file existed but is
//     malformed; the dispatcher maps this to -32603 because
//     the dashboard should not see "no plugins" when the
//     JSON is corrupt (the operator needs to know).
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
		// Malformed file. Bubble the parse error up so
		// the dispatcher surfaces it; the operator sees
		// "plugins.json is corrupt" in the wire envelope
		// instead of a silent fallback to fixtures.
		return State{}, true, fmt.Errorf("rc-plugins: parse %s: %w", s.Path, err)
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
		return State{}, true, fmt.Errorf("rc-plugins: %s version=%d is newer than supported %d", s.Path, rec.Version, StateVersion)
	}
	if rec.Plugins == nil {
		// Normalise nil → empty slice so the wire emits
		// `[]` rather than `null`.
		rec.Plugins = []Plugin{}
	}
	return State{
		Version:   rec.Version,
		Plugins:   rec.Plugins,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state to disk. Temp
// file + fsync-via-Rename gives crash-consistent semantics;
// the directory is fsynced too (matches antigravity-token's
// style).
func (s *FSStore) atomicWrite(state State) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskState{
		Version:   StateVersion,
		Plugins:   state.Plugins,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Plugins == nil {
		rec.Plugins = []Plugin{}
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

// fixtureSeedState returns the fresh-host fixture state:
// the three DefaultFixtures (each stamped with
// today as ConnectedAt), an empty plugins slice otherwise,
// and the wall-clock UpdatedAt. The store hands this back
// when Snapshot finds no file.
//
// Defensive copies of each fixture keep the global
// DefaultFixtures slice untouched, so a test that
// mutates the returned state's Plugins does not poison
// future fixture seeds (the slice is shared otherwise).
func (s *FSStore) fixtureSeedState() State {
	now := s.now()
	plugins := make([]Plugin, 0, len(DefaultFixtures))
	for _, p := range DefaultFixtures {
		caps := append([]string(nil), p.Capabilities...)
		plugins = append(plugins, Plugin{
			PluginID:     p.PluginID,
			Name:         p.Name,
			Version:      p.Version,
			ConnectedAt:  now,
			Capabilities: caps,
			Origin:       p.Origin,
		})
	}
	return State{
		Version:   StateVersion,
		Plugins:   plugins,
		UpdatedAt: now,
	}
}

// Snapshot returns the current state. No file → the
// fixture-seeded state (three DefaultFixtures with
// ConnectedAt = now). A malformed file bubbles the parse
// error up.
//
// The returned Plugins slice is sorted by PluginID
// ascending so dashboards see a stable sequence; the
// OnDiskPluginState round-trip preserves insertion order,
// but the fixture-seeded and empty-state paths benefit from
// a deterministic ordering (the three fixtures arrive in
// DefaultFixtures' alphabet order today, but
// sorting pins the order regardless of map-iteration
// noise a future fixture mutation might introduce).
func (s *FSStore) Snapshot(ctx context.Context) (State, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		state = s.fixtureSeedState()
	}
	sortPluginsByID(state.Plugins)
	return cloneState(state), nil
}

// Disconnect removes the plugin identified by pluginID. The
// store refuses an unknown id with ErrNotFound — the
// dispatcher maps that to JSON-RPC -32015. Returns the
// post-mutation state so the dispatcher can echo it in the
// response envelope.
//
// The read-modify-write runs under s.mu so concurrent
// Disconnect calls serialise. A Disconnect's read snap
// and write commit happen under the same lock; Snapshot
// reads take the read lock so they don't race a writer's
// in-progress mutation.
func (s *FSStore) Disconnect(ctx context.Context, pluginID string) (State, error) {
	_ = ctx
	pluginID = normalisePluginID(pluginID)
	if pluginID == "" {
		return State{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
	}
	if !exists {
		state = s.fixtureSeedState()
	}
	// Find-and-drop. O(n) over the plugin count, which is
	// fine for the dashboard's surface (a host rarely has
	// more than a dozen bridges).
	idx := -1
	for i, p := range state.Plugins {
		if p.PluginID == pluginID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return State{}, ErrNotFound
	}
	// Defensive copy so the JSON encoder doesn't see a
	// shared backing array.
	kept := make([]Plugin, 0, len(state.Plugins)-1)
	kept = append(kept, state.Plugins[:idx]...)
	kept = append(kept, state.Plugins[idx+1:]...)
	state.Plugins = kept
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return State{}, err
	}
	sortPluginsByID(state.Plugins)
	return cloneState(state), nil
}
