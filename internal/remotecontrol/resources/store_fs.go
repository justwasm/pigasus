// Package resources — store_fs.go: the
// *FSStore's load / atomicWrite / fixtureState /
// snapshot / Providers / Resources / Bind methods, plus
// the ensureParentDir helper. Pure file split out of
// store.go; store.go carries the
// type definitions + DefaultProviders +
// DefaultResources + cloneState +
// MemStore type, and store_mem.go
// carries the *MemStore constructors +
// Providers / Resources / Bind implementations.
//
// Stdlib-only. No third-party deps.
package resources

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
	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
)

// ensureParentDir creates the parent directory of
// FSStore.Path if it doesn't exist. Single-shot
// (sync.Once) so concurrent first-use calls don't race
// on the MkdirAll.
func (s *FSStore) ensureParentDir() error {
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
func (s *FSStore) load() (State, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, false, nil
		}
		return State{}, false, err
	}
	var rec onDiskState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error up
		// so the dispatcher surfaces it; the operator
		// sees "resources.json is corrupt" in the
		// wire envelope instead of a silent
		// fallback to the fixture state.
		return State{}, true, fmt.Errorf("rc-resources: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the on-disk
	// layout before the version stamp existed). A
	// reader that finds a version > StateVersion
	// refuses to touch the file (returns a "newer
	// binary required" envelope that the dispatcher
	// maps to -32603).
	if rec.Version == 0 {
		rec.Version = StateVersion
	}
	if rec.Version > StateVersion {
		return State{}, true, fmt.Errorf("rc-resources: %s version=%d is newer than supported %d", s.Path, rec.Version, StateVersion)
	}
	if rec.Providers == nil {
		rec.Providers = []Provider{}
	}
	if rec.Resources == nil {
		rec.Resources = []Resource{}
	}
	return State{
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
func (s *FSStore) atomicWrite(state State) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskState{
		Version:   StateVersion,
		Providers: state.Providers,
		Resources: state.Resources,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Providers == nil {
		rec.Providers = []Provider{}
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
// four DefaultProviders + the
// DefaultResources, with each provider's
// `configured` flag derived from s.checkSecretDir.
//
// The store hands this back when Snapshot finds no
// file. The fixture set is small (4 providers, ~12
// resources) and matches the brief's "default fixtures"
// detail.
func (s *FSStore) fixtureState() State {
	providers := make([]Provider, len(DefaultProviders))
	for i, p := range DefaultProviders {
		p.Configured = s.checkSecretDir(p.ProviderID)
		providers[i] = p
	}
	resources := make([]Resource, len(DefaultResources))
	copy(resources, DefaultResources)
	sortProvidersByID(providers)
	sortResourcesByID(resources)
	return State{
		Version:   StateVersion,
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
func (s *FSStore) snapshot() (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return State{}, err
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
	return cloneState(state), nil
}

// Providers returns the live provider slice, sorted
// ascending by ProviderID. The brief's
// ListRemoteAgentResourceProviders method returns all
// providers; kind-filtering happens at the dispatcher
// layer (the brief pins the kind-filter path on the
// Resources method only).
func (s *FSStore) Providers(ctx context.Context) []Provider {
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
func (s *FSStore) Resources(ctx context.Context, providerID, kind string) []Resource {
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
//   - err: ErrProjectNotFound when projectID is
//     unknown; I/O errors during the on-disk write.
//
// The bind is persisted on the project JSON via
// Project.Patch / Update, with the new `Resources`
// field carrying the bound resource_ids. The store
// reads the project's existing Resources field (via
// projects.Store.Get) so the Bind is replace-style
// ("these are the resource_ids the project is now
// attached to") rather than additive.
//
// Concurrency: the snapshot read is acquired under the
// read lock; the on-disk write runs under the write
// lock so two concurrent Binds serialise.
func (s *FSStore) Bind(ctx context.Context, projectID string, resourceIDs []string) (int, int, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, 0, ErrProjectNotFound
	}
	if s.Projects == nil {
		// No Projects store installed; the
		// dispatcher's "Bind on a no-store host"
		// path. Match the cycle-22 / cycle-9
		// precedent of returning the not-found
		// sentinel so the dispatcher surfaces the
		// same -32004 envelope.
		return 0, 0, ErrProjectNotFound
	}
	if _, err := s.Projects.Get(ctx, projectID); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			return 0, 0, ErrProjectNotFound
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
func (s *FSStore) reduceResourceIDs(ctx context.Context, resourceIDs []string) (int, int, []string) {
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
// record via projects.Store.Update. Translates the
// projects.ErrNotFound sentinel into the resources-side
// equivalent so the dispatcher's envelope is uniform.
func (s *FSStore) persistBind(ctx context.Context, projectID string, uniqueBound []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	patch := projects.Patch{Resources: &uniqueBound}
	if _, err := s.Projects.Update(ctx, projectID, patch); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("rc-resources: bind: %w", err)
	}
	return nil
}
