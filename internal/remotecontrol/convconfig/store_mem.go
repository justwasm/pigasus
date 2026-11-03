// Package remotecontrol —
// conversation_config_store_mem.go: the
// MemStore implementation plus the
// clone / sort helpers both the Mem and FS variants
// use (the FS path keeps its on-disk JSON loading
// local in conversation_config_store_fs.go).
//
// Why a sibling file (not the parent
// conversation_config_store.go): the Mem path
// doesn't touch the filesystem at all, so it
// imports a different and smaller set (sync,
// sort, context, time). Splitting keeps both
// halves under the 500-line ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Stdlib-only. No third-party deps.
package convconfig

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemStore is the in-memory
// Store fixture tests inject. It
// is a thin state-value + mutex; the methods
// implement the Store interface
// exactly the way FSStore does,
// with the differences that:
//
//   - There is no filesystem involvement:
//     Snapshot on a fresh MemStore
//     returns a state with configs={}.
//
//   - It does not touch the clock — UpdatedAt is
//     stamped with the supplied now function so
//     tests can drive monotonic time.
//
// The fields are unexported; tests construct it
// via the zero value, NewMemStore
// (empty + now), or NewMemStoreWith
// (custom snapshot) and call the methods directly.
type MemStore struct {
	mu    sync.RWMutex
	state State

	now func() time.Time
}

// NewMemStore returns a fresh,
// empty MemStore. now is used
// to stamp UpdatedAt; nil → time.Now (UTC).
func NewMemStore(now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Configs:   map[string]Config{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemStoreWith returns a
// fresh MemStore pre-populated
// with the supplied configs map. The map is
// deep-copied (and the configs are deep-copied per
// row) so a test that mutates the input does not
// race the store's internal state. UpdatedAt is
// stamped with the supplied now (or time.Now if
// now is nil).
//
// Used by tests that want to exercise the Upsert
// path against an explicit starting snapshot (the
// NewMemStore default has no
// entries).
func NewMemStoreWith(configs map[string]Config, now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	cloned := make(map[string]Config, len(configs))
	for k, v := range configs {
		cloned[k] = cloneConfig(v)
	}
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Configs:   cloned,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Snapshot returns a defensive copy of the
// in-memory state. The Configs map is deep-copied
// (and so is each Config value) so a
// Snapshot call doesn't perturb subsequent state,
// but the returned state is always in the
// canonical shape.
func (m *MemStore) Snapshot(ctx context.Context) (State, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneState(m.state), nil
}

// Upsert writes the supplied config for convID
// and returns whether the row was newly created.
// Mirrors (*FSStore).Upsert
// semantics — the in-memory store is a map
// mutation under the lock.
func (m *MemStore) Upsert(ctx context.Context, convID string, c Config) (bool, error) {
	_ = ctx
	convID = normaliseConversationID(convID)
	if convID == "" {
		return false, ErrPersistenceFailed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Configs == nil {
		m.state.Configs = map[string]Config{}
	}
	_, created := m.state.Configs[convID]
	now := m.now()
	c.UpdatedAt = now
	m.state.Configs[convID] = c
	m.state.UpdatedAt = now
	return !created, nil
}

// Get returns the config for convID. Mirrors
// (*FSStore).Get semantics —
// unknown conversation_id → (zero, false).
func (m *MemStore) Get(ctx context.Context, convID string) (Config, bool) {
	_ = ctx
	convID = normaliseConversationID(convID)
	if convID == "" {
		return Config{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.state.Configs[convID]
	if !ok {
		return Config{}, false
	}
	return cloneConfig(c), true
}

// cloneState returns a
// deep-copy of state so callers can't observe
// in-place mutations (and tests that compare
// pre/post snapshots don't share the same Configs
// map).
func cloneState(state State) State {
	out := state
	if state.Configs != nil {
		configs := make(map[string]Config, len(state.Configs))
		for k, v := range state.Configs {
			configs[k] = cloneConfig(v)
		}
		out.Configs = configs
	}
	return out
}

// cloneConfig returns a deep-copy of
// c so a caller can't mutate the store's internal
// state through the returned snapshot. Strings and
// time.Time are immutable, so they pass through
// verbatim; the slices (AllowedTools /
// DisallowedTools) and the map (Metadata) get
// fresh allocations.
func cloneConfig(c Config) Config {
	out := c
	if c.AllowedTools != nil {
		tools := make([]string, len(c.AllowedTools))
		copy(tools, c.AllowedTools)
		out.AllowedTools = tools
	}
	if c.DisallowedTools != nil {
		tools := make([]string, len(c.DisallowedTools))
		copy(tools, c.DisallowedTools)
		out.DisallowedTools = tools
	}
	if c.Metadata != nil {
		md := make(map[string]string, len(c.Metadata))
		for k, v := range c.Metadata {
			md[k] = v
		}
		out.Metadata = md
	}
	return out
}

// sortConversationIDs orders a slice of ids
// ascending. Currently the dispatcher doesn't
// expose a list method (the future
// rc-conversation-config-read cycle adds one),
// but the helper exists for parity with the
// rc-resources / rc-triggers / rc-plugins
// precedent so the future cycle only has to
// iterate the Snapshot() state in sorted order
// without re-touching this file.
//
// Kept unexported today; the future cycle that
// wants a List method will export it (or call it
// from inside the dispatcher's new list handler).
func sortConversationIDs(ids []string) {
	sort.Strings(ids)
}
