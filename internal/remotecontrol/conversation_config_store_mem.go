// Package remotecontrol —
// conversation_config_store_mem.go: the
// MemConversationConfigStore implementation plus the
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
package remotecontrol

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemConversationConfigStore is the in-memory
// ConversationConfigStore fixture tests inject. It
// is a thin state-value + mutex; the methods
// implement the ConversationConfigStore interface
// exactly the way FSConversationConfigStore does,
// with the differences that:
//
//   - There is no filesystem involvement:
//     Snapshot on a fresh MemConversationConfigStore
//     returns a state with configs={}.
//
//   - It does not touch the clock — UpdatedAt is
//     stamped with the supplied now function so
//     tests can drive monotonic time.
//
// The fields are unexported; tests construct it
// via the zero value, NewMemConversationConfigStore
// (empty + now), or NewMemConversationConfigStoreWith
// (custom snapshot) and call the methods directly.
type MemConversationConfigStore struct {
	mu    sync.RWMutex
	state ConversationConfigState

	now func() time.Time
}

// NewMemConversationConfigStore returns a fresh,
// empty MemConversationConfigStore. now is used
// to stamp UpdatedAt; nil → time.Now (UTC).
func NewMemConversationConfigStore(now func() time.Time) *MemConversationConfigStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemConversationConfigStore{
		state: ConversationConfigState{
			Version:   ConversationConfigStateVersion,
			Configs:   map[string]ConversationConfig{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemConversationConfigStoreWith returns a
// fresh MemConversationConfigStore pre-populated
// with the supplied configs map. The map is
// deep-copied (and the configs are deep-copied per
// row) so a test that mutates the input does not
// race the store's internal state. UpdatedAt is
// stamped with the supplied now (or time.Now if
// now is nil).
//
// Used by tests that want to exercise the Upsert
// path against an explicit starting snapshot (the
// NewMemConversationConfigStore default has no
// entries).
func NewMemConversationConfigStoreWith(configs map[string]ConversationConfig, now func() time.Time) *MemConversationConfigStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	cloned := make(map[string]ConversationConfig, len(configs))
	for k, v := range configs {
		cloned[k] = cloneConversationConfig(v)
	}
	return &MemConversationConfigStore{
		state: ConversationConfigState{
			Version:   ConversationConfigStateVersion,
			Configs:   cloned,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Snapshot returns a defensive copy of the
// in-memory state. The Configs map is deep-copied
// (and so is each ConversationConfig value) so a
// Snapshot call doesn't perturb subsequent state,
// but the returned state is always in the
// canonical shape.
func (m *MemConversationConfigStore) Snapshot(ctx context.Context) (ConversationConfigState, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConversationConfigState(m.state), nil
}

// Upsert writes the supplied config for convID
// and returns whether the row was newly created.
// Mirrors (*FSConversationConfigStore).Upsert
// semantics — the in-memory store is a map
// mutation under the lock.
func (m *MemConversationConfigStore) Upsert(ctx context.Context, convID string, c ConversationConfig) (bool, error) {
	_ = ctx
	convID = normaliseConversationID(convID)
	if convID == "" {
		return false, ErrConversationConfigPersistenceFailed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Configs == nil {
		m.state.Configs = map[string]ConversationConfig{}
	}
	_, created := m.state.Configs[convID]
	now := m.now()
	c.UpdatedAt = now
	m.state.Configs[convID] = c
	m.state.UpdatedAt = now
	return !created, nil
}

// Get returns the config for convID. Mirrors
// (*FSConversationConfigStore).Get semantics —
// unknown conversation_id → (zero, false).
func (m *MemConversationConfigStore) Get(ctx context.Context, convID string) (ConversationConfig, bool) {
	_ = ctx
	convID = normaliseConversationID(convID)
	if convID == "" {
		return ConversationConfig{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.state.Configs[convID]
	if !ok {
		return ConversationConfig{}, false
	}
	return cloneConversationConfig(c), true
}

// cloneConversationConfigState returns a
// deep-copy of state so callers can't observe
// in-place mutations (and tests that compare
// pre/post snapshots don't share the same Configs
// map).
func cloneConversationConfigState(state ConversationConfigState) ConversationConfigState {
	out := state
	if state.Configs != nil {
		configs := make(map[string]ConversationConfig, len(state.Configs))
		for k, v := range state.Configs {
			configs[k] = cloneConversationConfig(v)
		}
		out.Configs = configs
	}
	return out
}

// cloneConversationConfig returns a deep-copy of
// c so a caller can't mutate the store's internal
// state through the returned snapshot. Strings and
// time.Time are immutable, so they pass through
// verbatim; the slices (AllowedTools /
// DisallowedTools) and the map (Metadata) get
// fresh allocations.
func cloneConversationConfig(c ConversationConfig) ConversationConfig {
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
