// Package remotecontrol — resources_store_mem.go: the
// *MemResourceStore's NewMemResourceStore +
// NewMemResourceStoreWith + Providers + Resources + Bind
// methods. Pure file split out of resources_store.go;
// resources_store.go carries the type definitions +
// DefaultResourceProviders + DefaultResourceResources +
// cloneResourceState + MemResourceStore type, and
// resources_store_fs.go carries the *FSResourceStore
// load / atomicWrite / fixtureState / snapshot /
// Providers / Resources / Bind methods.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NewMemResourceStore returns a fresh MemResourceStore
// seeded with DefaultResourceProviders +
// DefaultResourceResources. now is used to stamp
// UpdatedAt; nil → time.Now (UTC). projects is the seam
// the Bind path writes the project's `Resources` field
// through; nil disables Bind (returns
// ErrResourceProjectNotFound unconditionally).
func NewMemResourceStore(now func() time.Time, projects ProjectStore) *MemResourceStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	providers := make([]ResourceProvider, len(DefaultResourceProviders))
	copy(providers, DefaultResourceProviders)
	resources := make([]Resource, len(DefaultResourceResources))
	copy(resources, DefaultResourceResources)
	sortProvidersByID(providers)
	sortResourcesByID(resources)
	return &MemResourceStore{
		state: ResourceState{
			Version:   ResourceStateVersion,
			Providers: providers,
			Resources: resources,
			UpdatedAt: now(),
		},
		now:      now,
		projects: projects,
	}
}

// NewMemResourceStoreWith returns a fresh
// MemResourceStore pre-populated with the supplied
// providers + resources slices. Both slices are
// deep-copied (and sorted) so a test that mutates the
// inputs does not race the store's internal state.
// UpdatedAt is stamped with the supplied now (or
// time.Now if now is nil). projects mirrors
// NewMemResourceStore's signature.
//
// Used by tests that want to exercise the Providers /
// Resources / Bind paths against an explicit starting
// snapshot (the NewMemResourceStore default mirrors the
// brief's fixture set, which is convenient but not
// always what a test wants).
func NewMemResourceStoreWith(providers []ResourceProvider, resources []Resource, now func() time.Time, projects ProjectStore) *MemResourceStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	cpProviders := make([]ResourceProvider, 0, len(providers))
	for _, p := range providers {
		if p.Scopes != nil {
			scopes := make([]string, len(p.Scopes))
			copy(scopes, p.Scopes)
			p.Scopes = scopes
		}
		cpProviders = append(cpProviders, p)
	}
	cpResources := make([]Resource, 0, len(resources))
	copy(cpResources, resources)
	sortProvidersByID(cpProviders)
	sortResourcesByID(cpResources)
	return &MemResourceStore{
		state: ResourceState{
			Version:   ResourceStateVersion,
			Providers: cpProviders,
			Resources: cpResources,
			UpdatedAt: now(),
		},
		now:      now,
		projects: projects,
	}
}

// Providers returns a defensive copy of the in-memory
// provider slice (sorted ascending by ProviderID —
// matches the FSResourceStore semantics so the wire
// envelope is identical regardless of which backend
// stored the state).
func (m *MemResourceStore) Providers(ctx context.Context) []ResourceProvider {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ResourceProvider, 0, len(m.state.Providers))
	for _, p := range m.state.Providers {
		if p.Scopes != nil {
			scopes := make([]string, len(p.Scopes))
			copy(scopes, p.Scopes)
			p.Scopes = scopes
		}
		out = append(out, p)
	}
	return out
}

// Resources returns the slice of resources matching
// the supplied filters. Empty providerID means "all
// providers"; empty kind means "all kinds". The
// returned slice is sorted by ResourceID ascending.
//
// Unknown providerID → empty slice (the dispatcher's
// unknown-provider filter path returns -32602 before
// this is reached — the cycle's filter-validation seam
// is the dispatcher's first defensive check).
func (m *MemResourceStore) Resources(ctx context.Context, providerID, kind string) []Resource {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	providerID = normaliseProviderID(providerID)
	kind = strings.TrimSpace(kind)
	out := make([]Resource, 0, len(m.state.Resources))
	for _, r := range m.state.Resources {
		if providerID != "" && r.ProviderID != providerID {
			continue
		}
		if kind != "" && r.Kind != kind {
			continue
		}
		out = append(out, r)
	}
	sortResourcesByID(out)
	return out
}

// Bind mirrors (*FSResourceStore).Bind semantics:
// validates project_id through the bound ProjectStore,
// computes bound / unbound counts against the live
// resource registry, persists the bind via
// ProjectStore.Update with the Resources patch.
//
// Unknown project_id → ErrResourceProjectNotFound
// (the dispatcher's -32004 envelope). Unknown
// resource_id → counted as `unbound` (the brief's
// "unknown resource is no-op" rule).
func (m *MemResourceStore) Bind(ctx context.Context, projectID string, resourceIDs []string) (int, int, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, 0, ErrResourceProjectNotFound
	}
	if m.projects == nil {
		return 0, 0, ErrResourceProjectNotFound
	}
	// Validate project_id first.
	if _, err := m.projects.Get(ctx, projectID); err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return 0, 0, ErrResourceProjectNotFound
		}
		return 0, 0, err
	}

	bound, unbound, uniqueBound := m.reduceResourceIDs(ctx, resourceIDs)
	if err := m.persistBind(ctx, projectID, uniqueBound); err != nil {
		return 0, 0, err
	}
	return bound, unbound, nil
}

// reduceResourceIDs walks the input slice, dedupes,
// counts bound vs unbound, and returns a sorted unique
// list of bound ids. Split out of Bind so the
// per-mutation paths stay under the 50-LOC ceiling.
func (m *MemResourceStore) reduceResourceIDs(ctx context.Context, resourceIDs []string) (int, int, []string) {
	known := map[string]struct{}{}
	for _, r := range m.Resources(ctx, "", "") {
		known[r.ResourceID] = struct{}{}
	}
	bound := 0
	unbound := 0
	seen := map[string]struct{}{}
	uniqueBound := make([]string, 0, len(resourceIDs))
	for _, rawID := range resourceIDs {
		id := normaliseResourceID(rawID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := known[id]; !ok {
			unbound++
			continue
		}
		bound++
		uniqueBound = append(uniqueBound, id)
	}
	sort.Strings(uniqueBound)
	return bound, unbound, uniqueBound
}

// persistBind writes the bound ids back to the project
// record via ProjectStore.Update. Mirrors the FS
// store's helper so the two Bind paths stay
// behaviourally identical.
func (m *MemResourceStore) persistBind(ctx context.Context, projectID string, uniqueBound []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	patch := ProjectPatch{Resources: &uniqueBound}
	if _, err := m.projects.Update(ctx, projectID, patch); err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return ErrResourceProjectNotFound
		}
		return fmt.Errorf("rc-resources: bind: %w", err)
	}
	return nil
}
