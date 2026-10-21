// Package remotecontrol — projects_store_mem.go: the
// *MemProjectStore's NewMemProjectStore + List +
// Create + Get + Update + Delete methods. Pure file
// split out of projects_store.go; projects_store.go
// carries the type definitions + cross-store helpers,
// and projects_store_fs.go carries the *FSProjectStore
// load / write / List / Create / Get / Update / Delete /
// sweepName / readRecord / writeRecord methods.
package remotecontrol

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NewMemProjectStore returns a fresh, empty
// MemProjectStore. now is used to stamp CreatedAt /
// UpdatedAt; nil → time.Now (so the UpdatedAt > CreatedAt
// tests don't need a time.Sleep).
func NewMemProjectStore(now func() time.Time) *MemProjectStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemProjectStore{
		rows:   map[string]Project{},
		byName: map[string]string{},
		now:    now,
	}
}

// List enumerates the rows whose workspace path
// starts with the supplied prefix, sorted by
// Creation time ascending. Limit is applied after
// the prefix filter.
func (m *MemProjectStore) List(ctx context.Context, prefix string, limit int) ([]Project, error) {
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
// ErrProjectExists. A new ID is minted via newProjectID.
func (m *MemProjectStore) Create(ctx context.Context, p NewProject) (Project, error) {
	_ = ctx
	name := normaliseProjectName(p.Name)
	ws := normaliseProjectWorkspace(p.Workspace)
	if name == "" {
		return Project{}, fmt.Errorf("rc-projects: name is required")
	}
	if ws == "" {
		return Project{}, fmt.Errorf("rc-projects: workspace is required")
	}
	if p.DefaultEffort != "" && !IsValidEffort(p.DefaultEffort) {
		return Project{}, fmt.Errorf("rc-projects: default_effort must be one of low|medium|high|xhigh|max")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byName[name]; exists {
		return Project{}, ErrProjectExists
	}
	id, err := newProjectID()
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
// ErrProjectNotFound.
func (m *MemProjectStore) Get(ctx context.Context, id string) (Project, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, ErrProjectNotFound
	}
	row, ok := m.rows[id]
	if !ok {
		return Project{}, ErrProjectNotFound
	}
	return row, nil
}

// Update applies the patch to the row identified by
// id. Empty patch fields are skipped. A name patch
// that collides with a different row →
// ErrProjectExists. Unknown id → ErrProjectNotFound.
func (m *MemProjectStore) Update(ctx context.Context, id string, patch ProjectPatch) (Project, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, ErrProjectNotFound
	}
	if patch.DefaultEffort != "" && !IsValidEffort(patch.DefaultEffort) {
		return Project{}, fmt.Errorf("rc-projects: default_effort must be one of low|medium|high|xhigh|max")
	}
	row, ok := m.rows[id]
	if !ok {
		return Project{}, ErrProjectNotFound
	}
	newName := strings.TrimSpace(patch.Name)
	if newName != "" && newName != row.Name {
		if existingID, exists := m.byName[newName]; exists && existingID != id {
			return Project{}, ErrProjectExists
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
	// Mirrors (*FSProjectStore).Update's Resources
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
// (*FSProjectStore).Delete semantics: missing id →
// ErrProjectNotFound, failIfBound=true AND
// BoundChecker reports >0 conversations →
// ErrProjectBound.
//
// The bound-check happens under the same lock as
// the actual delete (MemProjectStore has no out-of-
// band call to make).
func (m *MemProjectStore) Delete(ctx context.Context, id string, failIfBound bool) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrProjectNotFound
	}
	row, ok := m.rows[id]
	if !ok {
		return ErrProjectNotFound
	}
	if failIfBound && m.BoundChecker != nil {
		count, berr := m.BoundChecker(row.Workspace)
		if berr != nil {
			return fmt.Errorf("rc-projects: bound check failed: %w", berr)
		}
		if count > 0 {
			return ErrProjectBound
		}
	}
	delete(m.rows, id)
	if row.Name != "" {
		delete(m.byName, row.Name)
	}
	return nil
}
