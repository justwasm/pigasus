// Package projects — store_mem.go: the
// *MemStore's NewMemStore + List +
// Create + Get + Update + Delete methods. Pure file
// split out of store.go; store.go
// carries the type definitions + cross-store helpers,
// and store_fs.go carries the *FSStore
// load / write / List / Create / Get / Update / Delete /
// sweepName / readRecord / writeRecord methods.
package projects

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/effort"
)

// NewMemStore returns a fresh, empty
// MemStore. now is used to stamp CreatedAt /
// UpdatedAt; nil → time.Now (so the UpdatedAt > CreatedAt
// tests don't need a time.Sleep).
func NewMemStore(now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemStore{
		rows:   map[string]Project{},
		byName: map[string]string{},
		now:    now,
	}
}

// List enumerates the rows whose workspace path
// starts with the supplied prefix, sorted by
// Creation time ascending. Limit is applied after
// the prefix filter.
func (m *MemStore) List(ctx context.Context, prefix string, limit int) ([]Project, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix = strings.TrimSpace(prefix)
	out := make([]Project, 0, len(m.rows))
	for _, p := range m.rows {
		if prefix != "" && !strings.HasPrefix(p.Workspace, prefix) {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Create inserts a new row. Empty name / workspace →
// "is required" error. Duplicate name →
// ErrExists. A new ID is minted via newID.
func (m *MemStore) Create(ctx context.Context, p Create) (Project, error) {
	_ = ctx
	name := normaliseName(p.Name)
	ws := normaliseWorkspace(p.Workspace)
	if name == "" {
		return Project{}, fmt.Errorf("rc-projects: name is required")
	}
	if ws == "" {
		return Project{}, fmt.Errorf("rc-projects: workspace is required")
	}
	if p.DefaultEffort != "" && !effort.IsValid(p.DefaultEffort) {
		return Project{}, fmt.Errorf("rc-projects: default_effort must be one of low|medium|high|xhigh|max")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byName[name]; exists {
		return Project{}, ErrExists
	}
	id, err := newID()
	if err != nil {
		return Project{}, err
	}
	now := m.now()
	row := Project{
		ID:            id,
		Name:          name,
		Workspace:     ws,
		DefaultModel:  strings.TrimSpace(p.DefaultModel),
		DefaultEffort: strings.TrimSpace(p.DefaultEffort),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m.rows[id] = row
	m.byName[name] = id
	return row, nil
}

// Get returns the row by id. Unknown id →
// ErrNotFound.
func (m *MemStore) Get(ctx context.Context, id string) (Project, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, ErrNotFound
	}
	row, ok := m.rows[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	return row, nil
}

// Update applies the patch to the row identified by
// id. Empty patch fields are skipped. A name patch
// that collides with a different row →
// ErrExists. Unknown id → ErrNotFound.
func (m *MemStore) Update(ctx context.Context, id string, patch Patch) (Project, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, ErrNotFound
	}
	if patch.DefaultEffort != "" && !effort.IsValid(patch.DefaultEffort) {
		return Project{}, fmt.Errorf("rc-projects: default_effort must be one of low|medium|high|xhigh|max")
	}
	row, ok := m.rows[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	newName := strings.TrimSpace(patch.Name)
	if newName != "" && newName != row.Name {
		if existingID, exists := m.byName[newName]; exists && existingID != id {
			return Project{}, ErrExists
		}
		delete(m.byName, row.Name)
		row.Name = newName
		m.byName[newName] = id
	}
	if patch.DefaultModel != "" {
		row.DefaultModel = strings.TrimSpace(patch.DefaultModel)
	}
	if patch.DefaultEffort != "" {
		row.DefaultEffort = strings.TrimSpace(patch.DefaultEffort)
	}
	// Cycle rc-resources: when the patch carries a
	// non-nil Resources pointer, replace-style bind.
	// Mirrors (*FSStore).Update's Resources
	// patch handling.
	if patch.Resources != nil {
		resources := *patch.Resources
		if resources == nil {
			row.Resources = []string{}
		} else {
			resourcesCopy := make([]string, len(resources))
			copy(resourcesCopy, resources)
			row.Resources = resourcesCopy
		}
	}
	row.UpdatedAt = m.now()
	m.rows[id] = row
	return row, nil
}

// Delete removes the row keyed by id. Mirrors
// (*FSStore).Delete semantics: missing id →
// ErrNotFound, failIfBound=true AND
// BoundChecker reports >0 conversations →
// ErrBound.
//
// The bound-check happens under the same lock as
// the actual delete (MemStore has no out-of-
// band call to make).
func (m *MemStore) Delete(ctx context.Context, id string, failIfBound bool) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	row, ok := m.rows[id]
	if !ok {
		return ErrNotFound
	}
	if failIfBound && m.BoundChecker != nil {
		count, berr := m.BoundChecker(row.Workspace)
		if berr != nil {
			return fmt.Errorf("rc-projects: bound check failed: %w", berr)
		}
		if count > 0 {
			return ErrBound
		}
	}
	delete(m.rows, id)
	if row.Name != "" {
		delete(m.byName, row.Name)
	}
	return nil
}
