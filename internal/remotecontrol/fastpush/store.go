// Package remotecontrol — fastpush_store.go: the
// JSON-file-backed Store implementation, plus
// the small in-memory MemStore tests use.
//
// Why two implementations in one file:
//
//   - FSStore is the production seam: one
//     fastpush.json under $ANTIGRAVITY_HOME/fastpush.json.
//     The atomic-rename write matches the
//     antigravity-token / permissions / plugins /
//     triggers helpers' style.
//
//   - MemStore is the test seam: a state
//     value under a mutex. Tests inject this through
//     Config.Store so they don't need a
//     tempdir + real filesystem.
//
// Both share the same single-state storage shape; the
// wire envelope the dispatcher emits is identical
// regardless of which backend stored the state.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - fastpush_store.go         — package doc +
//     MemStore +
//     cloneFastpushState +
//     cloneFastpushSetting +
//     sortSettingsByCascadeID +
//     upsertFastpushSetting
//   - fastpush_store_fs.go      — FSStore
//     (struct / constructor /
//     load / atomicWrite /
//     ensureParentDir /
//     emptyState / Snapshot /
//     SetEnabled / Get)
//
// Stdlib-only. No third-party deps.
package fastpush

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemStore is the in-memory Store
// fixture tests inject. It is a thin state-value +
// mutex; the methods implement the Store
// interface exactly the way FSStore does, with
// the differences that:
//
//   - There is no filesystem involvement: Snapshot on
//     a fresh MemStore returns a state with
//     settings=[].
//
//   - It does not touch the clock — UpdatedAt is
//     stamped with the supplied now function so tests
//     can drive monotonic time.
//
// The fields are unexported; tests construct it via the
// zero value, NewMemStore (empty + now), or
// NewMemStoreWith (custom snapshot) and call
// the methods directly.
type MemStore struct {
	mu    sync.RWMutex
	state State

	now func() time.Time

	// checkCascade is the optional existence-check
	// closure (see FSStore.checkCascade for
	// the contract). nil disables the check.
	checkCascade func(cascadeID string) bool
}

// NewMemStore returns a fresh, empty
// MemStore. now is used to stamp UpdatedAt;
// nil → time.Now (UTC). checkCascade is the optional
// existence-check closure; nil disables the check
// (every SetEnabled / Get accepts any cascade_id).
func NewMemStore(now func() time.Time, checkCascade func(cascadeID string) bool) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Settings:  []Setting{},
			UpdatedAt: now(),
		},
		now:          now,
		checkCascade: checkCascade,
	}
}

// NewMemStoreWith returns a fresh
// MemStore pre-populated with the supplied
// settings slice. The slice is deep-copied (and sorted
// by CascadeID ascending) so a test that mutates the
// input does not race the store's internal state.
// UpdatedAt is stamped with the supplied now (or
// time.Now if now is nil). checkCascade mirrors
// NewMemStore's signature.
//
// Used by tests that want to exercise the SetEnabled
// path against an explicit starting snapshot (the
// NewMemStore default has no entries).
func NewMemStoreWith(settings []Setting, now func() time.Time, checkCascade func(cascadeID string) bool) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := make([]Setting, 0, len(settings))
	for _, set := range settings {
		out = append(out, cloneFastpushSetting(set))
	}
	sortSettingsByCascadeID(out)
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Settings:  out,
			UpdatedAt: now(),
		},
		now:          now,
		checkCascade: checkCascade,
	}
}

// Snapshot returns a defensive copy of the in-memory
// state. The Settings slice is sorted ascending by
// CascadeID — matches the FSStore semantics so
// the wire envelope is identical regardless of which
// backend stored the state.
//
// The sort is applied to the cloned slice (not the
// in-memory backing array) so a Snapshot call doesn't
// perturb subsequent state, but the returned state is
// always in the canonical order.
func (m *MemStore) Snapshot(ctx context.Context) (State, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := cloneFastpushState(m.state)
	sortSettingsByCascadeID(out.Settings)
	return out, nil
}

// SetEnabled upserts the entry for cascadeID with the
// supplied enabled value. Mirrors
// (*FSStore).SetEnabled semantics. Returns the
// post-mutation setting so the dispatcher can echo the
// new value in the response envelope.
func (m *MemStore) SetEnabled(ctx context.Context, cascadeID string, enabled bool) (Setting, error) {
	_ = ctx
	if rerr := validateFastpushCascadeID(cascadeID, m.checkCascade); rerr != nil {
		return Setting{}, rerr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	setting := m.upsertSetting(cascadeID, enabled, now)
	m.state.UpdatedAt = now
	return cloneFastpushSetting(setting), nil
}

// Get returns the setting for cascadeID. Mirrors
// (*FSStore).Get semantics. Unknown
// cascadeID → ErrCascadeNotFound. A cascade
// that has no stored row returns a zero-valued
// Setting with no error.
func (m *MemStore) Get(ctx context.Context, cascadeID string) (Setting, error) {
	_ = ctx
	if rerr := validateFastpushCascadeID(cascadeID, m.checkCascade); rerr != nil {
		return Setting{}, rerr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, set := range m.state.Settings {
		if set.CascadeID == cascadeID {
			return cloneFastpushSetting(set), nil
		}
	}
	return Setting{CascadeID: cascadeID}, nil
}

// upsertSetting is the lock-held helper that inserts or
// replaces the row for cascadeID on m.state.Settings.
// Pulled out of SetEnabled so the per-row upsert logic
// is auditable without paging through the validate +
// clock + mutex choreography.
func (m *MemStore) upsertSetting(cascadeID string, enabled bool, now time.Time) Setting {
	setting := Setting{
		CascadeID:    cascadeID,
		Enabled:      enabled,
		Channel:      ChannelName,
		PushDelivery: PushDeliveryDeferred,
		UpdatedAt:    now,
	}
	idx := -1
	for i, set := range m.state.Settings {
		if set.CascadeID == cascadeID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		m.state.Settings[idx] = setting
	} else {
		m.state.Settings = append(m.state.Settings, setting)
	}
	sortSettingsByCascadeID(m.state.Settings)
	return setting
}

// validateFastpushCascadeID normalises the supplied id
// and runs the optional checkCascade closure. Returns
// ErrCascadeNotFound when the id is empty or
// the closure reports unknown; nil on success. Pulled
// out so both SetEnabled and Get share the exact same
// gating rule.
func validateFastpushCascadeID(raw string, checkCascade func(string) bool) error {
	id := normaliseCascadeID(raw)
	if id == "" {
		return ErrCascadeNotFound
	}
	if checkCascade != nil && !checkCascade(id) {
		return ErrCascadeNotFound
	}
	return nil
}

// cloneFastpushState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Settings slice).
func cloneFastpushState(state State) State {
	out := state
	if state.Settings != nil {
		settings := make([]Setting, 0, len(state.Settings))
		for _, set := range state.Settings {
			settings = append(settings, cloneFastpushSetting(set))
		}
		out.Settings = settings
	}
	return out
}

// cloneFastpushSetting returns a deep-copy of set so a
// caller can't mutate the store's internal state
// through the returned snapshot. Strings and
// time.Time are immutable, so they pass through
// verbatim.
func cloneFastpushSetting(set Setting) Setting {
	return Setting{
		CascadeID:    set.CascadeID,
		Enabled:      set.Enabled,
		Channel:      set.Channel,
		PushDelivery: set.PushDelivery,
		UpdatedAt:    set.UpdatedAt,
	}
}

// sortSettingsByCascadeID orders settings in place by
// CascadeID ascending. Both stores apply this so the
// wire surface sees a stable order regardless of which
// backend stored the state. Stable secondary sort by
// UpdatedAt (oldest first) so a snapshot that has two
// rows with the same CascadeID (impossible today — the
// store dedups — but defensive) has a deterministic
// order.
func sortSettingsByCascadeID(settings []Setting) {
	sort.SliceStable(settings, func(i, j int) bool {
		if settings[i].CascadeID == settings[j].CascadeID {
			return settings[i].UpdatedAt.Before(settings[j].UpdatedAt)
		}
		return settings[i].CascadeID < settings[j].CascadeID
	})
}
