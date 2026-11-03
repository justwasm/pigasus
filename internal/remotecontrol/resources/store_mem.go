// Package resources — store_mem.go: the
// *MemStore's NewMemStore +
// NewMemStoreWith + Providers + Resources + Bind
// methods. Pure file split out of store.go;
// store.go carries the type definitions +
// DefaultProviders + DefaultResources +
// cloneState + MemStore type, and
// store_fs.go carries the *FSStore
// load / atomicWrite / fixtureState / snapshot /
// Providers / Resources / Bind methods.
//
// Stdlib-only. No third-party deps.
package resources

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
)

// NewMemStore returns a fresh MemStore
// seeded with DefaultProviders +
// DefaultResources. now is used to stamp
// UpdatedAt; nil → time.Now (UTC). projects is the seam
// the Bind path writes the project's `Resources` field
// through; nil disables Bind (returns
// ErrProjectNotFound unconditionally).
func NewMemStore(now func() time.Time, projectStore projects.Store) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	providers := make([]Provider, len(DefaultProviders))
	copy(providers, DefaultProviders)
	resources := make([]Resource, len(DefaultResources))
	copy(resources, DefaultResources)
	sortProvidersByID(providers)
	sortResourcesByID(resources)
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Providers: providers,
			Resources: resources,
			UpdatedAt: now(),
		},
		now:      now,
		projects: projectStore,
	}
}

// NewMemStoreWith returns a fresh
// MemStore pre-populated with the supplied
// providers + resources slices. Both slices are
// deep-copied (and sorted) so a test that mutates the
// inputs does not race the store's internal state.
// UpdatedAt is stamped with the supplied now (or
// time.Now if now is nil). projects mirrors
// NewMemStore's signature.
//
// Used by tests that want to exercise the Providers /
// Resources / Bind paths against an explicit starting
// snapshot (the NewMemStore default mirrors the
// brief's fixture set, which is convenient but not
// always what a test wants).
func NewMemStoreWith(providers []Provider, resources []Resource, now func() time.Time, projectStore projects.Store) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	cpProviders := make([]Provider, 0, len(providers))
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
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Providers: cpProviders,
			Resources: cpResources,
			UpdatedAt: now(),
		},
		now:      now,
		projects: projectStore,
	}
}

// Providers returns a defensive copy of the in-memory
// provider slice (sorted ascending by ProviderID —
// matches the FSStore semantics so the wire
// envelope is identical regardless of which backend
// stored the state).
func (m *MemStore) Providers(ctx context.Context) []Provider {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Provider, 0, len(m.state.Providers))
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
func (m *MemStore) Resources(ctx context.Context, providerID, kind string) []Resource {
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

// Bind mirrors (*FSStore).Bind semantics:
// validates project_id through the bound ProjectStore,
// computes bound / unbound counts against the live
// resource registry, persists the bind via
// ProjectStore.Update with the Resources patch.
//
// Unknown project_id → ErrProjectNotFound
// (the dispatcher's -32004 envelope). Unknown
// resource_id → counted as `unbound` (the brief's
// "unknown resource is no-op" rule).
func (m *MemStore) Bind(ctx context.Context, projectID string, resourceIDs []string) (int, int, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, 0, ErrProjectNotFound
	}
	if m.projects == nil {
		return 0, 0, ErrProjectNotFound
	}
	// Validate project_id first.
	if _, err := m.projects.Get(ctx, projectID); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			return 0, 0, ErrProjectNotFound
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
func (m *MemStore) reduceResourceIDs(ctx context.Context, resourceIDs []string) (int, int, []string) {
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
func (m *MemStore) persistBind(ctx context.Context, projectID string, uniqueBound []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	patch := projects.Patch{Resources: &uniqueBound}
	if _, err := m.projects.Update(ctx, projectID, patch); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("rc-resources: bind: %w", err)
	}
	return nil
}
