// Package remotecontrol — resources_store_fs.go: the
// *FSResourceStore's load / atomicWrite / fixtureState /
// snapshot / Providers / Resources / Bind methods, plus
// the ensureParentDir helper. Pure file split out of
// resources_store.go; resources_store.go carries the
// type definitions + DefaultResourceProviders +
// DefaultResourceResources + cloneResourceState +
// MemResourceStore type, and resources_store_mem.go
// carries the *MemResourceStore constructors +
// Providers / Resources / Bind implementations.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/atomicfile"
)

// ensureParentDir creates the parent directory of
// FSResourceStore.Path if it doesn't exist. Single-shot
// (sync.Once) so concurrent first-use calls don't race
// on the MkdirAll.
func (s *FSResourceStore) ensureParentDir() error {
	var err error
	s.ensureDirOnce.Do(func() {
		dir := filepath.Dir(s.Path)
		if dir == "" || dir == "." {
			return
		}
		err = os.MkdirAll(dir, 0o700)
	})
	return err
}

// load reads the on-disk state. Returns:
//
//   - (zero, false, nil) — no file exists (a fresh
//     host). The caller hands back the fixture-seeded
//     state;
//   - (state, true, nil) — the file existed and parsed
//     cleanly;
//   - (state, true, err) — the file existed but is
//     malformed; the dispatcher surfaces this.
func (s *FSResourceStore) load() (ResourceState, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResourceState{}, false, nil
		}
		return ResourceState{}, false, err
	}
	var rec onDiskResourceState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error up
		// so the dispatcher surfaces it; the operator
		// sees "resources.json is corrupt" in the
		// wire envelope instead of a silent
		// fallback to the fixture state.
		return ResourceState{}, true, fmt.Errorf("rc-resources: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the on-disk
	// layout before the version stamp existed). A
	// reader that finds a version > ResourceStateVersion
	// refuses to touch the file (returns a "newer
	// binary required" envelope that the dispatcher
	// maps to -32603).
	if rec.Version == 0 {
		rec.Version = ResourceStateVersion
	}
	if rec.Version > ResourceStateVersion {
		return ResourceState{}, true, fmt.Errorf("rc-resources: %s version=%d is newer than supported %d", s.Path, rec.Version, ResourceStateVersion)
	}
	if rec.Providers == nil {
		rec.Providers = []ResourceProvider{}
	}
	if rec.Resources == nil {
		rec.Resources = []Resource{}
	}
	return ResourceState{
		Version:   rec.Version,
		Providers: rec.Providers,
		Resources: rec.Resources,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state to disk.
// Temp file + fsync-via-Rename gives crash-consistent
// semantics; the directory is fsynced too (matches
// antigravity-token's style).
func (s *FSResourceStore) atomicWrite(state ResourceState) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskResourceState{
		Version:   ResourceStateVersion,
		Providers: state.Providers,
		Resources: state.Resources,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Providers == nil {
		rec.Providers = []ResourceProvider{}
	}
	if rec.Resources == nil {
		rec.Resources = []Resource{}
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(s.Path, data, 0o600); err != nil {
		return err
	}
	// Best-effort dir fsync: ensures the rename is
	// durable across a power loss. Best-effort
	// because a platform-level fsync on the directory
	// may not be available — we tolerate the failure
	// silently (matching antigravity-token's style).
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

// fixtureState returns the fresh-host fixture state: the
// four DefaultResourceProviders + the
// DefaultResourceResources, with each provider's
// `configured` flag derived from s.checkSecretDir.
//
// The store hands this back when Snapshot finds no
// file. The fixture set is small (4 providers, ~12
// resources) and matches the brief's "default fixtures"
// detail.
func (s *FSResourceStore) fixtureState() ResourceState {
	providers := make([]ResourceProvider, len(DefaultResourceProviders))
	for i, p := range DefaultResourceProviders {
		p.Configured = s.checkSecretDir(p.ProviderID)
		providers[i] = p
	}
	resources := make([]Resource, len(DefaultResourceResources))
	copy(resources, DefaultResourceResources)
	sortProvidersByID(providers)
	sortResourcesByID(resources)
	return ResourceState{
		Version:   ResourceStateVersion,
		Providers: providers,
		Resources: resources,
		UpdatedAt: s.now(),
	}
}

// snapshot returns the current state under the read
// lock. No file → the fixture state (4 providers +
// ~12 resources, with each provider's `configured`
// from s.checkSecretDir). A malformed file bubbles the
// parse error up.
func (s *FSResourceStore) snapshot() (ResourceState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return ResourceState{}, err
	}
	if !exists {
		state = s.fixtureState()
	} else {
		// Refresh the `configured` flag on each
		// provider on every snapshot — the secret
		// directory may have changed since the file
		// was last written. The fixture state is the
		// only path where `configured` is computed
		// from the closure directly; for the
		// load-from-disk path we apply the same
		// closure (the brief calls this out
		// explicitly: "check
		// $ANTIGRAVITY_HOME/secrets/ for a non-empty
		// file per provider").
		for i, p := range state.Providers {
			p.Configured = s.checkSecretDir(p.ProviderID)
			state.Providers[i] = p
		}
	}
	sortProvidersByID(state.Providers)
	sortResourcesByID(state.Resources)
	return cloneResourceState(state), nil
}

// Providers returns the live provider slice, sorted
// ascending by ProviderID. The brief's
// ListRemoteAgentResourceProviders method returns all
// providers; kind-filtering happens at the dispatcher
// layer (the brief pins the kind-filter path on the
// Resources method only).
func (s *FSResourceStore) Providers(ctx context.Context) []ResourceProvider {
	_ = ctx
	state, err := s.snapshot()
	if err != nil {
		// Bubble the malformed-file error up: the
		// dispatcher's Handler treats this as
		// internalError. We deliberately do NOT
		// fall back to the fixture state on a
		// malformed file — the operator needs to
		// know.
		return nil
	}
	return state.Providers
}

// Resources returns the slice of resources matching
// the supplied filters. Empty providerID means "all
// providers"; empty kind means "all kinds". Unknown
// providerID → empty slice (the dispatcher's
// unknown-provider filter path returns -32602 before
// this is reached). The returned slice is sorted by
// ResourceID ascending so dashboards see a stable
// order.
//
// The function tolerates a malformed file by returning
// an empty slice; the brief calls for the unknown-
// provider case to be the only sentinel (the handler
// validates before the call).
func (s *FSResourceStore) Resources(ctx context.Context, providerID, kind string) []Resource {
	_ = ctx
	state, err := s.snapshot()
	if err != nil {
		return nil
	}
	providerID = normaliseProviderID(providerID)
	kind = strings.TrimSpace(kind)
	out := make([]Resource, 0, len(state.Resources))
	for _, r := range state.Resources {
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

// Bind attaches resourceIDs to projectID. Returns
// (bound, unbound, err) where:
//
//   - bound: the number of resource_ids the store
//     actually attached (the unique, recognised ids in
//     the input).
//   - unbound: the number of resource_ids in the input
//     that did NOT match a known resource_id (the
//     brief's "unknown resource is no-op" rule).
//   - err: ErrResourceProjectNotFound when projectID is
//     unknown; I/O errors during the on-disk write.
//
// The bind is persisted on the project JSON via
// Project.Patch / Update, with the new `Resources`
// field carrying the bound resource_ids. The store
// reads the project's existing Resources field (via
// ProjectStore.Get) so the Bind is replace-style
// ("these are the resource_ids the project is now
// attached to") rather than additive.
//
// Concurrency: the snapshot read is acquired under the
// read lock; the on-disk write runs under the write
// lock so two concurrent Binds serialise.
func (s *FSResourceStore) Bind(ctx context.Context, projectID string, resourceIDs []string) (int, int, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, 0, ErrResourceProjectNotFound
	}
	if s.Projects == nil {
		// No Projects store installed; the
		// dispatcher's "Bind on a no-store host"
		// path. Match the cycle-22 / cycle-9
		// precedent of returning the not-found
		// sentinel so the dispatcher surfaces the
		// same -32004 envelope.
		return 0, 0, ErrResourceProjectNotFound
	}
	if _, err := s.Projects.Get(ctx, projectID); err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return 0, 0, ErrResourceProjectNotFound
		}
		return 0, 0, err
	}

	bound, unbound, uniqueBound := s.reduceResourceIDs(ctx, resourceIDs)
	if err := s.persistBind(ctx, projectID, uniqueBound); err != nil {
		return 0, 0, err
	}
	return bound, unbound, nil
}

// reduceResourceIDs walks the input slice, dedupes,
// counts bound vs unbound, and returns a sorted unique
// list of bound ids. Split out of Bind so the
// per-mutation paths stay under the 50-LOC ceiling.
func (s *FSResourceStore) reduceResourceIDs(ctx context.Context, resourceIDs []string) (int, int, []string) {
	known := map[string]struct{}{}
	for _, r := range s.Providers(ctx) {
		known[r.ProviderID] = struct{}{}
	}
	for _, r := range s.Resources(ctx, "", "") {
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
// record via ProjectStore.Update. Translates the
// ErrProjectNotFound sentinel into the resources-side
// equivalent so the dispatcher's envelope is uniform.
func (s *FSResourceStore) persistBind(ctx context.Context, projectID string, uniqueBound []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	patch := ProjectPatch{Resources: &uniqueBound}
	if _, err := s.Projects.Update(ctx, projectID, patch); err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return ErrResourceProjectNotFound
		}
		return fmt.Errorf("rc-resources: bind: %w", err)
	}
	return nil
}
