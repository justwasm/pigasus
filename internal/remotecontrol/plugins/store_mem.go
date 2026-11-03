// Package plugins — store_mem.go: the
// MemStore implementation plus the clone / sort
// helpers both the Mem and FS variants use (the FS path
// keeps its on-disk JSON loading local in
// store_fs.go).
//
// Why a sibling file (not the parent store.go):
// the Mem path doesn't touch the filesystem at all, so
// it imports a different and smaller set (sync, sort,
// context, time). Splitting keeps both halves under the
// 500-line ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Stdlib-only. No third-party deps.
package plugins

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemStore is the in-memory Store fixture tests
// inject. It is a thin state-value + mutex; the methods
// implement the Store interface exactly the way
// FSStore does, with the differences that:
//
//   - There is no filesystem involvement: Snapshot on a
//     fresh MemStore returns a state seeded with
//     DefaultFixtures unless the test pre-populated
//     via a custom constructor.
//
//   - It does not touch the clock — UpdatedAt and
//     ConnectedAt are stamped with the supplied now
//     function so tests can drive monotonic time.
//
// The fields are unexported; tests construct it via the
// zero value, NewMemStore (fixture-seeded), or
// NewMemStoreWith (custom snapshot) and call the
// methods directly.
type MemStore struct {
	mu    sync.RWMutex
	state State

	now func() time.Time
}

// NewMemStore returns a fresh fixture-seeded
// MemStore. now is used to stamp UpdatedAt /
// ConnectedAt; nil → time.Now (UTC).
func NewMemStore(now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	plugins := make([]Plugin, 0, len(DefaultFixtures))
	for _, p := range DefaultFixtures {
		caps := append([]string(nil), p.Capabilities...)
		plugins = append(plugins, Plugin{
			PluginID:     p.PluginID,
			Name:         p.Name,
			Version:      p.Version,
			ConnectedAt:  now(),
			Capabilities: caps,
			Origin:       p.Origin,
		})
	}
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Plugins:   plugins,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemStoreWith returns a fresh MemStore
// holding exactly the supplied plugins slice. The slice is
// deep-copied (and sorted by PluginID ascending) so a test
// that mutates the input does not race the store's
// internal state. UpdatedAt is stamped with the supplied
// now (or time.Now if now is nil).
//
// Used by tests that want to exercise the Disconnect path
// against an explicit starting snapshot (the
// NewMemStore default fixture-seeded state has
// three entries; tests that need two, or a custom plugin
// id, build their own).
func NewMemStoreWith(plugins []Plugin, now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := make([]Plugin, 0, len(plugins))
	for _, p := range plugins {
		caps := append([]string(nil), p.Capabilities...)
		out = append(out, Plugin{
			PluginID:     p.PluginID,
			Name:         p.Name,
			Version:      p.Version,
			ConnectedAt:  p.ConnectedAt,
			Capabilities: caps,
			Origin:       p.Origin,
		})
	}
	sortPluginsByID(out)
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Plugins:   out,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Snapshot returns a defensive copy of the in-memory state.
// The Plugins slice is sorted ascending by PluginID —
// matches the FSStore semantics so the wire envelope
// is identical regardless of which backend stored the
// state.
//
// The sort is applied to the cloned slice (not the
// in-memory backing array) so a Snapshot call doesn't
// perturb subsequent state, but the returned state is
// always in the canonical order. Constructors sort their
// starting slices; Snapshot re-sorts so a caller that
// mutated the state through a non-public surface still
// observes the canonical order on the wire.
func (m *MemStore) Snapshot(ctx context.Context) (State, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := cloneState(m.state)
	sortPluginsByID(out.Plugins)
	return out, nil
}

// Disconnect removes the entry whose PluginID matches
// pluginID. Unknown ids return ErrNotFound (defence-
// in-depth — the handler layer's normalisePluginID should
// have rejected empty ids, but the store enforces
// strictness). Returns the post-mutation state with the
// Plugins slice re-sorted by PluginID ascending.
func (m *MemStore) Disconnect(ctx context.Context, pluginID string) (State, error) {
	_ = ctx
	pluginID = normalisePluginID(pluginID)
	if pluginID == "" {
		return State{}, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, p := range m.state.Plugins {
		if p.PluginID == pluginID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return State{}, ErrNotFound
	}
	kept := make([]Plugin, 0, len(m.state.Plugins)-1)
	kept = append(kept, m.state.Plugins[:idx]...)
	kept = append(kept, m.state.Plugins[idx+1:]...)
	m.state.Plugins = kept
	m.state.UpdatedAt = m.now()
	sortPluginsByID(m.state.Plugins)
	return cloneState(m.state), nil
}

// cloneState returns a deep-copy of state so callers
// can't observe in-place mutations (and tests that compare
// pre/post snapshots don't share the same Plugins slice).
func cloneState(state State) State {
	out := state
	if state.Plugins != nil {
		plugins := make([]Plugin, 0, len(state.Plugins))
		for _, p := range state.Plugins {
			caps := append([]string(nil), p.Capabilities...)
			plugins = append(plugins, Plugin{
				PluginID:     p.PluginID,
				Name:         p.Name,
				Version:      p.Version,
				ConnectedAt:  p.ConnectedAt,
				Capabilities: caps,
				Origin:       p.Origin,
			})
		}
		out.Plugins = plugins
	}
	return out
}

// sortPluginsByID orders plugins in place by PluginID
// ascending. Both stores apply this so the wire surface
// (List) sees a stable, sort-pinned order regardless of
// which backend stored the state. Stable secondary sort
// by ConnectedAt (oldest first) so the fixture-seeded
// paths (which all stamp ConnectedAt = now) have a
// deterministic order even when PluginIDs collide — they
// won't, but the project's `sort.SliceStable` style keeps
// the helper's contract honest.
func sortPluginsByID(plugins []Plugin) {
	sort.SliceStable(plugins, func(i, j int) bool {
		if plugins[i].PluginID == plugins[j].PluginID {
			return plugins[i].ConnectedAt.Before(plugins[j].ConnectedAt)
		}
		return plugins[i].PluginID < plugins[j].PluginID
	})
}
