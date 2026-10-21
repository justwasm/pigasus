// Package remotecontrol — resources_store.go: the
// ResourceStore / FSResourceStore / MemResourceStore
// types + the JSON-file on-disk record + the
// DefaultResourceProviders / DefaultResourceResources
// fixture set + cloneResourceState. The actual load /
// write / snapshot / Bind methods live in
// resources_store_fs.go (FSResourceStore) and
// resources_store_mem.go (MemResourceStore).
//
// Why two implementations:
//
//   - FSResourceStore is the production seam: one
//     resources.json under
//     $ANTIGRAVITY_HOME/resources.json. The atomic-rename
//     write matches the convstore / antigravity-token /
//     permissions / plugins / triggers / fastpush helpers'
//     style.
//
//   - MemResourceStore is the test seam: a state value
//     under a mutex. Tests inject this through
//     Config.ResourceStore so they don't need a tempdir +
//     real filesystem.
//
// Both share the same single-state storage shape; the
// wire envelope the dispatcher emits is identical
// regardless of which backend stored the state.
//
// Bind requires cross-store coordination (Bind reads the
// ProjectStore to validate project_id, then writes the
// project's `resources` field back through the project
// disk path). The store therefore takes an explicit
// ProjectLookup closure (`Projects ProjectStore`); the FS
// store loads it lazily via the injected Projects pointer,
// while the Mem store accepts it directly.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"sync"
	"time"
)

// FSResourceStore is the production ResourceStore
// implementation. The state lives in a single JSON file
// at Path; the store serialises every read-modify-write
// (Bind) through a single mutex. Snapshot reads
// (Providers / Resources) take the read lock so
// concurrent reads don't race a writer's in-progress
// mutation.
//
// Concurrency: the store uses a single RWMutex so the
// dashboard's resource surface is serialised. The brief
// doesn't call for high concurrency on the resource
// surface — listing providers / resources is
// human-paced, not turn-paced — so a single mutex is
// correct and cheap. A future cycle that grows to
// high-frequency polling only has to swap mu for a
// per-id RWMutex.
//
// Bind writes the project's `resources` field back
// through the bound ProjectStore (cycle-9 / cycle-22
// project store). The store DOES NOT own the project
// file directly — it asks the ProjectStore to Update
// the project's `Resources` field.
type FSResourceStore struct {
	// Path is the absolute path to the JSON file the
	// store reads / writes. Production wiring uses
	// auth.AntigravityHome()/resources.json; tests
	// pass a t.TempDir()-rooted path.
	Path string

	// Projects is the seam Bind uses to validate
	// project_id and to write the project's
	// `Resources` field back. nil → Bind returns
	// ErrResourceProjectNotFound unconditionally,
	// matching the dispatcher's "no ProjectStore
	// installed" path. Production wires a
	// *FSProjectStore (cycle-9).
	Projects ProjectStore

	// checkSecretDir is the optional closure Bind's
	// underlying fixture-seed step uses to determine
	// whether a provider_id is `configured`. The
	// closure returns true iff there is a non-empty
	// `<secretDir>/<provider_id>` file. nil →
	// defaults to `configured:false` for every
	// provider (matches the test seam; production
	// wires a closure built from
	// auth.AntigravityHome()/secrets).
	checkSecretDir func(providerID string) (configured bool)

	// now is the clock the store stamps UpdatedAt
	// with. nil → time.Now (production). Tests
	// override it to make the timestamps
	// deterministic.
	now func() time.Time

	// mu serialises the per-mutation read-modify-write
	// cycle. Held only across the in-memory mutation
	// + atomic save; snapshot reads (Providers /
	// Resources) take the read lock so concurrent
	// reads don't race a writer's in-progress
	// mutation.
	mu sync.RWMutex

	// ensureDirOnce guards the directory MkdirAll so
	// concurrent first-use calls don't race.
	ensureDirOnce sync.Once
}

// onDiskResourceState is the JSON shape the
// FSResourceStore writes to disk. The Version /
// Providers / Resources / UpdatedAt fields map 1:1 onto
// ResourceState's JSON tags so the wire format and the
// on-disk format stay identical.
//
// Future versions of the file format (cycle n+1) write
// a new struct and branch on the Version stamp inside
// the reader (see (*FSResourceStore).load for the
// version-upgrade branch).
type onDiskResourceState struct {
	Version   int                `json:"version"`
	Providers []ResourceProvider `json:"providers"`
	Resources []Resource         `json:"resources"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// NewFSResourceStore returns a fresh FSResourceStore
// rooted at path. The store's lazy-on-first-mutation
// pattern means path may not exist on return; the
// directory is created on the first write.
//
// projects is the production seam the Bind path writes
// the project's `Resources` field through. nil is
// allowed (Bind returns ErrResourceProjectNotFound
// unconditionally for every project_id).
//
// checkSecretDir is the optional closure the store
// calls when seeding its snapshot; nil disables the
// discovery and every fixture provider gets
// `configured:false`. The brief calls out the discovery
// path: "check $ANTIGRAVITY_HOME/secrets/ for a non-
// empty file per provider".
//
// when now is nil, the store uses time.Now (UTC).
func NewFSResourceStore(path string, projects ProjectStore, checkSecretDir func(string) bool, now func() time.Time) *FSResourceStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if checkSecretDir == nil {
		// Default: nothing is configured. The brief
		// allows this fallback for tests / hosts that
		// don't ship a secrets directory.
		checkSecretDir = func(string) bool { return false }
	}
	return &FSResourceStore{
		Path:           path,
		Projects:       projects,
		checkSecretDir: checkSecretDir,
		now:            now,
	}
}

// cloneResourceState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Providers / Resources slices).
func cloneResourceState(state ResourceState) ResourceState {
	out := state
	if state.Providers != nil {
		providers := make([]ResourceProvider, 0, len(state.Providers))
		for _, p := range state.Providers {
			pCopy := p
			if p.Scopes != nil {
				scopes := make([]string, len(p.Scopes))
				copy(scopes, p.Scopes)
				pCopy.Scopes = scopes
			}
			providers = append(providers, pCopy)
		}
		out.Providers = providers
	}
	if state.Resources != nil {
		resources := make([]Resource, 0, len(state.Resources))
		for _, r := range state.Resources {
			resources = append(resources, r)
		}
		out.Resources = resources
	}
	return out
}

// DefaultResourceProviders is the fresh-host fixture
// provider set the store seeds when no resources.json
// exists yet. Four well-known providers the brief calls
// out: GitHub, Sentry, Linear, Google Docs. All four
// carry scopes that match the brief's "default
// advertised scopes" detail.
//
// The fixture's `configured` flag is computed from
// s.checkSecretDir on every snapshot (see the
// snapshot method's rationale); here we pin the
// default-true-or-false shape that the brief calls out:
// production wiring passes a closure that reads
// `$ANTIGRAVITY_HOME/secrets/<provider_id>` non-empty
// → configured; absent → false. Tests inject
// deterministic closures.
var DefaultResourceProviders = []ResourceProvider{
	{
		ProviderID: "github",
		Name:       "GitHub",
		Kind:       ResourceKindVCS,
		// Configured is set at snapshot time via
		// checkSecretDir("github"). Fixture default
		// value here is false so an FSResourceStore
		// with no checkSecretDir closure falls back
		// to the documented behaviour ("no
		// $ANTIGRAVITY_HOME/secrets/ directory →
		// every provider is unconfigured").
		Configured: false,
		Scopes:     []string{"repo", "issues", "prs"},
		Version:    "1.0.0",
	},
	{
		ProviderID: "sentry",
		Name:       "Sentry",
		Kind:       ResourceKindObservability,
		Configured: false,
		Scopes:     []string{"alerts", "issues"},
		Version:    "1.0.0",
	},
	{
		ProviderID: "linear",
		Name:       "Linear",
		Kind:       ResourceKindPM,
		Configured: false,
		Scopes:     []string{"cycles", "issues", "projects"},
		Version:    "1.0.0",
	},
	{
		ProviderID: "gdocs",
		Name:       "Google Docs",
		Kind:       ResourceKindDocs,
		Configured: false,
		Scopes:     []string{"docs.read", "docs.write"},
		Version:    "1.0.0",
	},
}

// DefaultResourceResources is the fresh-host fixture
// resource set the store seeds when no resources.json
// exists yet. Twelve records spread across the four
// DefaultResourceProviders:
//
//   - github: 3 repositories ("openagy/openagy",
//     "openagy/rea-go-seeds",
//     "wagyu/wagyu"). Each carries a brief-pinned
//     `payload_summary` like "12 open issues, 4 PRs".
//
//   - sentry: 3 projects ("production", "staging",
//     "edge"). Each carries a brief-pinned
//     `payload_summary` like "3 unresolved alerts".
//
//   - linear: 3 cycles ("Cycle 23 — rc-resources",
//     "Cycle 22 — conversation-deletions",
//     "Cycle 21 — triggers"). Each carries
//     `payload_summary` like "12 issues, 3 in review".
//
//   - gdocs: 3 docs ("Architecture Overview",
//     "Roadmap Q4 2026", "Internal RFC 2026-10-12").
//     Each carries `payload_summary` like
//     "32 pages, last edited 2026-10-12".
//
// All twelve carry ResourceID shaped
// `<provider_id>:<ref>` so the brief's "github:owner/
// repo" example matches one of the rows verbatim.
//
// The fixture set is intentionally non-trivial (12
// items rather than 4) so the
// ListRemoteAgentAvailableResources' filter
// combinations have a meaningful test surface.
var DefaultResourceResources = []Resource{
	// GitHub resources.
	{
		ResourceID:     "github:openagy/openagy",
		ProviderID:     "github",
		Kind:           ResourceKindVCS,
		Label:          "openagy/openagy",
		Ref:            "openagy/openagy",
		PayloadSummary: "12 open issues, 4 PRs",
	},
	{
		ResourceID:     "github:openagy/rea-go-seeds",
		ProviderID:     "github",
		Kind:           ResourceKindVCS,
		Label:          "openagy/rea-go-seeds",
		Ref:            "openagy/rea-go-seeds",
		PayloadSummary: "2 open issues, 0 PRs",
	},
	{
		ResourceID:     "github:wagyu/wagyu",
		ProviderID:     "github",
		Kind:           ResourceKindVCS,
		Label:          "wagyu/wagyu",
		Ref:            "wagyu/wagyu",
		PayloadSummary: "5 open issues, 1 PR",
	},
	// Sentry resources.
	{
		ResourceID:     "sentry:production",
		ProviderID:     "sentry",
		Kind:           ResourceKindObservability,
		Label:          "production",
		Ref:            "production",
		PayloadSummary: "3 unresolved alerts",
	},
	{
		ResourceID:     "sentry:staging",
		ProviderID:     "sentry",
		Kind:           ResourceKindObservability,
		Label:          "staging",
		Ref:            "staging",
		PayloadSummary: "0 unresolved alerts",
	},
	{
		ResourceID:     "sentry:edge",
		ProviderID:     "sentry",
		Kind:           ResourceKindObservability,
		Label:          "edge",
		Ref:            "edge",
		PayloadSummary: "1 unresolved alert",
	},
	// Linear resources.
	{
		ResourceID:     "linear:cycle-23",
		ProviderID:     "linear",
		Kind:           ResourceKindPM,
		Label:          "Cycle 23 — rc-resources",
		Ref:            "cycle-23",
		PayloadSummary: "5 issues, 1 in review",
	},
	{
		ResourceID:     "linear:cycle-22",
		ProviderID:     "linear",
		Kind:           ResourceKindPM,
		Label:          "Cycle 22 — conversation-deletions",
		Ref:            "cycle-22",
		PayloadSummary: "8 issues, 0 in review",
	},
	{
		ResourceID:     "linear:cycle-21",
		ProviderID:     "linear",
		Kind:           ResourceKindPM,
		Label:          "Cycle 21 — triggers",
		Ref:            "cycle-21",
		PayloadSummary: "12 issues, 3 in review",
	},
	// Google Docs resources.
	{
		ResourceID:     "gdocs:architecture-overview",
		ProviderID:     "gdocs",
		Kind:           ResourceKindDocs,
		Label:          "Architecture Overview",
		Ref:            "architecture-overview",
		PayloadSummary: "32 pages, last edited 2026-10-12",
	},
	{
		ResourceID:     "gdocs:roadmap-q4-2026",
		ProviderID:     "gdocs",
		Kind:           ResourceKindDocs,
		Label:          "Roadmap Q4 2026",
		Ref:            "roadmap-q4-2026",
		PayloadSummary: "8 pages, last edited 2026-10-08",
	},
	{
		ResourceID:     "gdocs:rfc-2026-10-12",
		ProviderID:     "gdocs",
		Kind:           ResourceKindDocs,
		Label:          "Internal RFC 2026-10-12",
		Ref:            "rfc-2026-10-12",
		PayloadSummary: "4 pages, last edited 2026-10-12",
	},
}

// MemResourceStore is the in-memory ResourceStore
// fixture tests inject. It is a thin state-value +
// mutex; the methods implement the ResourceStore
// interface exactly the way FSResourceStore does, with
// the differences that:
//
//   - There is no filesystem involvement: Snapshot on
//     a fresh MemResourceStore returns a state seeded
//     with DefaultResourceProviders +
//     DefaultResourceResources (the same fixture shape
//     the FS store hands back on a fresh host).
//
//   - It does not touch the clock — UpdatedAt is
//     stamped with the supplied now function so tests
//     can drive monotonic time.
//
// The fields are unexported; tests construct it via the
// zero value, NewMemResourceStore (empty-fixture + now +
// projects), or NewMemResourceStoreWith (custom fixture)
// and call the methods directly.
type MemResourceStore struct {
	mu       sync.RWMutex
	state    ResourceState
	now      func() time.Time
	projects ProjectStore
}
