// Package remotecontrol — plugins_store_mem.go: the
// MemPluginStore implementation plus the clone / sort
// helpers both the Mem and FS variants use (the FS path
// keeps its on-disk JSON loading local in
// plugins_store_fs.go).
//
// Why a sibling file (not the parent plugins_store.go):
// the Mem path doesn't touch the filesystem at all, so
// it imports a different and smaller set (sync, sort,
// context, time). Splitting keeps both halves under the
// 500-line ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemPluginStore is the in-memory PluginStore fixture tests
// inject. It is a thin state-value + mutex; the methods
// implement the PluginStore interface exactly the way
// FSPluginStore does, with the differences that:
//
//   - There is no filesystem involvement: Snapshot on a
//     fresh MemPluginStore returns a state seeded with
//     DefaultPluginFixtures unless the test pre-populated
//     via a custom constructor.
//
//   - It does not touch the clock — UpdatedAt and
//     ConnectedAt are stamped with the supplied now
//     function so tests can drive monotonic time.
//
// The fields are unexported; tests construct it via the
// zero value, NewMemPluginStore (fixture-seeded), or
// NewMemPluginStoreWith (custom snapshot) and call the
// methods directly.
type MemPluginStore struct {
	mu    sync.RWMutex
	state PluginState

	now func() time.Time
}

// NewMemPluginStore returns a fresh fixture-seeded
// MemPluginStore. now is used to stamp UpdatedAt /
// ConnectedAt; nil → time.Now (UTC).
func NewMemPluginStore(now func() time.Time) *MemPluginStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	plugins := make([]Plugin, 0, len(DefaultPluginFixtures))
	for _, p := range DefaultPluginFixtures {
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
	return &MemPluginStore{
		state: PluginState{
			Version:   PluginStateVersion,
			Plugins:   plugins,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemPluginStoreWith returns a fresh MemPluginStore
// holding exactly the supplied plugins slice. The slice is
// deep-copied (and sorted by PluginID ascending) so a test
// that mutates the input does not race the store's
// internal state. UpdatedAt is stamped with the supplied
// now (or time.Now if now is nil).
//
// Used by tests that want to exercise the Disconnect path
// against an explicit starting snapshot (the
// NewMemPluginStore default fixture-seeded state has
// three entries; tests that need two, or a custom plugin
// id, build their own).
func NewMemPluginStoreWith(plugins []Plugin, now func() time.Time) *MemPluginStore {
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
	return &MemPluginStore{
		state: PluginState{
			Version:   PluginStateVersion,
			Plugins:   out,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Snapshot returns a defensive copy of the in-memory state.
// The Plugins slice is sorted ascending by PluginID —
// matches the FSPluginStore semantics so the wire envelope
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
func (m *MemPluginStore) Snapshot(ctx context.Context) (PluginState, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := clonePluginState(m.state)
	sortPluginsByID(out.Plugins)
	return out, nil
}

// Disconnect removes the entry whose PluginID matches
// pluginID. Unknown ids return ErrPluginNotFound (defence-
// in-depth — the handler layer's normalisePluginID should
// have rejected empty ids, but the store enforces
// strictness). Returns the post-mutation state with the
// Plugins slice re-sorted by PluginID ascending.
func (m *MemPluginStore) Disconnect(ctx context.Context, pluginID string) (PluginState, error) {
	_ = ctx
	pluginID = normalisePluginID(pluginID)
	if pluginID == "" {
		return PluginState{}, ErrPluginNotFound
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
		return PluginState{}, ErrPluginNotFound
	}
	kept := make([]Plugin, 0, len(m.state.Plugins)-1)
	kept = append(kept, m.state.Plugins[:idx]...)
	kept = append(kept, m.state.Plugins[idx+1:]...)
	m.state.Plugins = kept
	m.state.UpdatedAt = m.now()
	sortPluginsByID(m.state.Plugins)
	return clonePluginState(m.state), nil
}

// clonePluginState returns a deep-copy of state so callers
// can't observe in-place mutations (and tests that compare
// pre/post snapshots don't share the same Plugins slice).
func clonePluginState(state PluginState) PluginState {
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
