// Package remotecontrol — fastpush_store.go: the
// JSON-file-backed FastpushStore implementation, plus
// the small in-memory MemFastpushStore tests use.
//
// Why two implementations in one file:
//
//   - FSFastpushStore is the production seam: one
//     fastpush.json under $ANTIGRAVITY_HOME/fastpush.json.
//     The atomic-rename write matches the
//     antigravity-token / permissions / plugins /
//     triggers helpers' style.
//
//   - MemFastpushStore is the test seam: a state
//     value under a mutex. Tests inject this through
//     Config.FastpushStore so they don't need a
//     tempdir + real filesystem.
//
// Both share the same single-state storage shape; the
// wire envelope the dispatcher emits is identical
// regardless of which backend stored the state.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - fastpush_store.go         — package doc +
//     MemFastpushStore +
//     cloneFastpushState +
//     cloneFastpushSetting +
//     sortSettingsByCascadeID +
//     upsertFastpushSetting
//   - fastpush_store_fs.go      — FSFastpushStore
//     (struct / constructor /
//     load / atomicWrite /
//     ensureParentDir /
//     emptyState / Snapshot /
//     SetEnabled / Get)
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemFastpushStore is the in-memory FastpushStore
// fixture tests inject. It is a thin state-value +
// mutex; the methods implement the FastpushStore
// interface exactly the way FSFastpushStore does, with
// the differences that:
//
//   - There is no filesystem involvement: Snapshot on
//     a fresh MemFastpushStore returns a state with
//     settings=[].
//
//   - It does not touch the clock — UpdatedAt is
//     stamped with the supplied now function so tests
//     can drive monotonic time.
//
// The fields are unexported; tests construct it via the
// zero value, NewMemFastpushStore (empty + now), or
// NewMemFastpushStoreWith (custom snapshot) and call
// the methods directly.
type MemFastpushStore struct {
	mu    sync.RWMutex
	state FastpushState

	now func() time.Time

	// checkCascade is the optional existence-check
	// closure (see FSFastpushStore.checkCascade for
	// the contract). nil disables the check.
	checkCascade func(cascadeID string) bool
}

// NewMemFastpushStore returns a fresh, empty
// MemFastpushStore. now is used to stamp UpdatedAt;
// nil → time.Now (UTC). checkCascade is the optional
// existence-check closure; nil disables the check
// (every SetEnabled / Get accepts any cascade_id).
func NewMemFastpushStore(now func() time.Time, checkCascade func(cascadeID string) bool) *MemFastpushStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemFastpushStore{
		state: FastpushState{
			Version:   FastpushStateVersion,
			Settings:  []FastpushSetting{},
			UpdatedAt: now(),
		},
		now:          now,
		checkCascade: checkCascade,
	}
}

// NewMemFastpushStoreWith returns a fresh
// MemFastpushStore pre-populated with the supplied
// settings slice. The slice is deep-copied (and sorted
// by CascadeID ascending) so a test that mutates the
// input does not race the store's internal state.
// UpdatedAt is stamped with the supplied now (or
// time.Now if now is nil). checkCascade mirrors
// NewMemFastpushStore's signature.
//
// Used by tests that want to exercise the SetEnabled
// path against an explicit starting snapshot (the
// NewMemFastpushStore default has no entries).
func NewMemFastpushStoreWith(settings []FastpushSetting, now func() time.Time, checkCascade func(cascadeID string) bool) *MemFastpushStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := make([]FastpushSetting, 0, len(settings))
	for _, set := range settings {
		out = append(out, cloneFastpushSetting(set))
	}
	sortSettingsByCascadeID(out)
	return &MemFastpushStore{
		state: FastpushState{
			Version:   FastpushStateVersion,
			Settings:  out,
			UpdatedAt: now(),
		},
		now:          now,
		checkCascade: checkCascade,
	}
}

// Snapshot returns a defensive copy of the in-memory
// state. The Settings slice is sorted ascending by
// CascadeID — matches the FSFastpushStore semantics so
// the wire envelope is identical regardless of which
// backend stored the state.
//
// The sort is applied to the cloned slice (not the
// in-memory backing array) so a Snapshot call doesn't
// perturb subsequent state, but the returned state is
// always in the canonical order.
func (m *MemFastpushStore) Snapshot(ctx context.Context) (FastpushState, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := cloneFastpushState(m.state)
	sortSettingsByCascadeID(out.Settings)
	return out, nil
}

// SetEnabled upserts the entry for cascadeID with the
// supplied enabled value. Mirrors
// (*FSFastpushStore).SetEnabled semantics. Returns the
// post-mutation setting so the dispatcher can echo the
// new value in the response envelope.
func (m *MemFastpushStore) SetEnabled(ctx context.Context, cascadeID string, enabled bool) (FastpushSetting, error) {
	_ = ctx
	if rerr := validateFastpushCascadeID(cascadeID, m.checkCascade); rerr != nil {
		return FastpushSetting{}, rerr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	setting := m.upsertSetting(cascadeID, enabled, now)
	m.state.UpdatedAt = now
	return cloneFastpushSetting(setting), nil
}

// Get returns the setting for cascadeID. Mirrors
// (*FSFastpushStore).Get semantics. Unknown
// cascadeID → ErrFastpushCascadeNotFound. A cascade
// that has no stored row returns a zero-valued
// FastpushSetting with no error.
func (m *MemFastpushStore) Get(ctx context.Context, cascadeID string) (FastpushSetting, error) {
	_ = ctx
	if rerr := validateFastpushCascadeID(cascadeID, m.checkCascade); rerr != nil {
		return FastpushSetting{}, rerr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, set := range m.state.Settings {
		if set.CascadeID == cascadeID {
			return cloneFastpushSetting(set), nil
		}
	}
	return FastpushSetting{CascadeID: cascadeID}, nil
}

// upsertSetting is the lock-held helper that inserts or
// replaces the row for cascadeID on m.state.Settings.
// Pulled out of SetEnabled so the per-row upsert logic
// is auditable without paging through the validate +
// clock + mutex choreography.
func (m *MemFastpushStore) upsertSetting(cascadeID string, enabled bool, now time.Time) FastpushSetting {
	setting := FastpushSetting{
		CascadeID:    cascadeID,
		Enabled:      enabled,
		Channel:      FastpushChannelName,
		PushDelivery: FastpushPushDeliveryDeferred,
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
// ErrFastpushCascadeNotFound when the id is empty or
// the closure reports unknown; nil on success. Pulled
// out so both SetEnabled and Get share the exact same
// gating rule.
func validateFastpushCascadeID(raw string, checkCascade func(string) bool) error {
	id := normaliseCascadeID(raw)
	if id == "" {
		return ErrFastpushCascadeNotFound
	}
	if checkCascade != nil && !checkCascade(id) {
		return ErrFastpushCascadeNotFound
	}
	return nil
}

// cloneFastpushState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Settings slice).
func cloneFastpushState(state FastpushState) FastpushState {
	out := state
	if state.Settings != nil {
		settings := make([]FastpushSetting, 0, len(state.Settings))
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
func cloneFastpushSetting(set FastpushSetting) FastpushSetting {
	return FastpushSetting{
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
func sortSettingsByCascadeID(settings []FastpushSetting) {
	sort.SliceStable(settings, func(i, j int) bool {
		if settings[i].CascadeID == settings[j].CascadeID {
			return settings[i].UpdatedAt.Before(settings[j].UpdatedAt)
		}
		return settings[i].CascadeID < settings[j].CascadeID
	})
}
