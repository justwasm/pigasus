// Package remotecontrol — triggers_store_mem.go: the
// *MemTriggerStore's NewMemTriggerStore +
// NewMemTriggerStoreWith + Snapshot + List + Create +
// Update + Delete methods. Pure file split out of
// triggers_store.go; triggers_store.go carries the
// type definitions + cross-store helpers, and
// triggers_store_fs.go carries the *FSTriggerStore load /
// write / mutate / Snapshot / List / Create / Update /
// Delete methods.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"fmt"
	"time"
)

// NewMemTriggerStore returns a fresh, empty
// MemTriggerStore. now is used to stamp UpdatedAt /
// CreatedAt; nil → time.Now (UTC).
func NewMemTriggerStore(now func() time.Time) *MemTriggerStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemTriggerStore{
		state: TriggerState{
			Version:   TriggerStateVersion,
			Triggers:  []Trigger{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// NewMemTriggerStoreWith returns a fresh MemTriggerStore
// pre-populated with the supplied triggers slice. The
// slice is deep-copied (and sorted by CreatedAt
// ascending) so a test that mutates the input does not
// race the store's internal state. UpdatedAt is stamped
// with the supplied now (or time.Now if now is nil).
//
// Used by tests that want to exercise the Update /
// Delete paths against an explicit starting snapshot
// (the NewMemTriggerStore default has no entries).
func NewMemTriggerStoreWith(triggers []Trigger, now func() time.Time) *MemTriggerStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := make([]Trigger, 0, len(triggers))
	for _, t := range triggers {
		out = append(out, cloneTrigger(t))
	}
	sortTriggersByCreatedAt(out)
	return &MemTriggerStore{
		state: TriggerState{
			Version:   TriggerStateVersion,
			Triggers:  out,
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Snapshot returns a defensive copy of the in-memory
// state.
func (m *MemTriggerStore) Snapshot(ctx context.Context) (TriggerState, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneTriggerState(m.state), nil
}

// List returns the triggers filtered by projectID, sorted
// by CreatedAt ascending. Empty projectID returns all.
func (m *MemTriggerStore) List(ctx context.Context, projectID string) ([]Trigger, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Trigger, 0, len(m.state.Triggers))
	for _, t := range m.state.Triggers {
		if projectID == "" || t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sortTriggersByCreatedAt(out)
	return out, nil
}

// Create adds a new trigger. Mirrors
// (*FSTriggerStore).Create semantics. The TriggerID
// is stamped with the test-friendly form
// "tri_test_NNNN" where N is a zero-padded counter the
// test can assert.
func (m *MemTriggerStore) Create(ctx context.Context, t Trigger) (Trigger, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := validateCreateInput(t); err != nil {
		return Trigger{}, err
	}
	name := normaliseName(t.Name)
	projectID := stringsTrimSpace(t.ProjectID)
	for _, existing := range m.state.Triggers {
		if existing.ProjectID == projectID && existing.Name == name {
			return Trigger{}, ErrTriggerExists
		}
	}
	m.nextID++
	id := fmt.Sprintf("%stest_%04d", TriggerIDPrefix, m.nextID)
	now := m.now()
	out := Trigger{
		TriggerID: id,
		ProjectID: projectID,
		Name:      name,
		Kind:      t.Kind,
		Spec:      cloneTriggerSpec(t.Spec),
		Enabled:   t.Enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.state.Triggers = append(m.state.Triggers, out)
	m.state.UpdatedAt = now
	return out, nil
}

// Update applies the patch. Mirrors
// (*FSTriggerStore).Update semantics.
func (m *MemTriggerStore) Update(ctx context.Context, id string, patch TriggerPatch) (Trigger, error) {
	_ = ctx
	id = normaliseName(id)
	if id == "" {
		return Trigger{}, ErrTriggerNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, t := range m.state.Triggers {
		if t.TriggerID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Trigger{}, ErrTriggerNotFound
	}
	t := m.state.Triggers[idx]
	if patch.Name != nil {
		newName := normaliseName(*patch.Name)
		if newName == "" {
			return Trigger{}, fmt.Errorf("%w: name is required", ErrInvalidSpec)
		}
		if newName != t.Name {
			for j, existing := range m.state.Triggers {
				if j == idx {
					continue
				}
				if existing.ProjectID == t.ProjectID && existing.Name == newName {
					return Trigger{}, ErrTriggerExists
				}
			}
			t.Name = newName
		}
	}
	if patch.Enabled != nil {
		t.Enabled = *patch.Enabled
	}
	if patch.Spec != nil {
		if err := ValidateTriggerSpec(t.Kind, *patch.Spec); err != nil {
			return Trigger{}, err
		}
		t.Spec = cloneTriggerSpec(*patch.Spec)
	}
	t.UpdatedAt = m.now()
	m.state.Triggers[idx] = t
	m.state.UpdatedAt = t.UpdatedAt
	return t, nil
}

// Delete removes the trigger identified by id. Mirrors
// (*FSTriggerStore).Delete semantics. Returns the
// post-mutation state.
func (m *MemTriggerStore) Delete(ctx context.Context, id string) (TriggerState, error) {
	_ = ctx
	id = normaliseName(id)
	if id == "" {
		return TriggerState{}, ErrTriggerNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, t := range m.state.Triggers {
		if t.TriggerID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return TriggerState{}, ErrTriggerNotFound
	}
	kept := make([]Trigger, 0, len(m.state.Triggers)-1)
	kept = append(kept, m.state.Triggers[:idx]...)
	kept = append(kept, m.state.Triggers[idx+1:]...)
	m.state.Triggers = kept
	m.state.UpdatedAt = m.now()
	return cloneTriggerState(m.state), nil
}

// keep the time package usage stable under goimports
// (the now() callback returns time.Time which is
// referenced via the field type declared in
// triggers_store.go).
var _ time.Time
