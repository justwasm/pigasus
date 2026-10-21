// Package remotecontrol — resources.go: the resource-provider
// data model + sentinel errors for the three JSON-RPC
// methods introduced in cycle rc-resources
// (`ListRemoteAgentResourceProviders`,
// `ListRemoteAgentAvailableResources`,
// `UpdateRemoteAgentProjectResources`).
//
// Why split (rather than embedding in resources_store.go):
// the dispatcher doesn't care about the on-disk format, and
// keeping the per-record types + sentinels in a single small
// file lets the storage layer (resources_store.go) and the
// per-method handlers (resources_dispatcher.go) talk to a
// stable interface without re-touching each other when the
// wire shape evolves.
//
// Data model (mirror the brief):
//
//   - ResourceProvider — a registered integration (GitHub,
//     GitLab, Sentry, Linear, …). The brief's fields are
//     provider_id, name, kind, configured, scopes, version.
//     Kind is one of "vcs" / "observability" / "pm" / "docs"
//     / "ci" — closed-set via IsValidResourceKind.
//
//   - Resource — a single addressable record a provider
//     surfaces (GitHub repository, Sentry project, Linear
//     cycle, …). The brief's fields are resource_id,
//     provider_id, kind, label, ref, payload_summary.
//
//   - ResourceState — the on-disk / wire envelope the store
//     holds. Versioned ({"version":1,
//     "providers":[...], "resources":[...], "updated_at":...}).
//
// Sentinel-error map (mirrors the rc-projects /
// rc-permissions-grants cycle pattern):
//
//	ErrResourceProviderUnknown
//	  → -32602 "Invalid params" (only on
//	    ListRemoteAgentAvailableResources' provider_id
//	    filter — the dispatcher validates against the
//	    fixture set before the store call)
//	ErrResourceProjectNotFound
//	  → -32004 "Project not found" (on
//	    UpdateRemoteAgentProjectResources; the brief
//	    reuses the project surface's -32004)
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// ResourceKind is the closed set of integration categories
// the cycle recognises. The brief pins five values
// ("vcs" / "observability" / "pm" / "docs" / "ci"); an
// unknown kind is rejected by the store (the assertion fires
// once, during the fixture-seeded snapshot).
//
// Constrained set today; a future cycle that adds
// (e.g.) "ai" or "design" only has to extend the slice +
// the AllResourceKinds / IsValidResourceKind helpers.
const (
	ResourceKindVCS           = "vcs"
	ResourceKindObservability = "observability"
	ResourceKindPM            = "pm"
	ResourceKindDocs          = "docs"
	ResourceKindCI            = "ci"
)

// AllResourceKinds is the canonical order of the closed-set
// `kind` enum. Useful for validator loops + future
// `ListResourceProviderKindOptions` shapes.
//
// Insertion order is intentional (matches the
// rc-triggers / rc-plugins conventions: most-frequent
// first).
var AllResourceKinds = []string{
	ResourceKindVCS,
	ResourceKindObservability,
	ResourceKindPM,
	ResourceKindDocs,
	ResourceKindCI,
}

// IsValidResourceKind reports whether k is one of the
// closed-set values in AllResourceKinds. A closed-set
// validator that returns false flips the dispatcher's
// error mapping to -32602 (matches the
// IsValidTriggerKind / IsValidPluginID rules the previous
// cycles established).
func IsValidResourceKind(k string) bool {
	for _, v := range AllResourceKinds {
		if v == k {
			return true
		}
	}
	return false
}

// ResourceProvider is one row of the
// ListRemoteAgentResourceProviders response. The fields
// mirror the brief exactly:
//
//   - ProviderID: the stable id dashboards use to address
//     a provider (e.g. "github", "gitlab", "sentry",
//     "linear"). Required, unique within a registry;
//     trimmed for whitespace during normalisation.
//
//   - Name: the human-facing display name ("GitHub",
//     "Sentry", "Linear", "Google Docs"). Today the
//     fixtures pin a Title-cased display label.
//
//   - Kind: the closed-set category
//     ("vcs" / "observability" / "pm" / "docs" /
//     "ci"). Drives the ListRemoteAgentAvailableResources'
//     kind filter.
//
//   - Configured: whether the host has credentials for the
//     provider (the cycle's fixture seed reads
//     `$ANTIGRAVITY_HOME/secrets/<provider_id>` for a
//     non-empty file; absent → false; present → true).
//
//   - Scopes: the brief-pinned capability list
//     (["repo","issues","prs"] for GitHub, ["alerts"] for
//     Sentry, etc.). Empty slice means "provider exists
//     but is in a degraded state"; nil → "no advertised
//     scopes". omitempty so the wire drops the empty
//     slice.
//
//   - Version: the provider-reported version
//     (e.g. "1.0.0", "2024-09-15"). omitempty so the wire
//     drops empty versions.
type ResourceProvider struct {
	// ProviderID is the stable id dashboards use to
	// address a provider. Required and unique within
	// the registry.
	ProviderID string `json:"provider_id"`
	// Name is the human-facing display name.
	Name string `json:"name"`
	// Kind is one of AllResourceKinds. Closed-set
	// validated by IsValidResourceKind.
	Kind string `json:"kind"`
	// Configured reports whether the host has
	// credentials for the provider. Today the
	// fixtures seed false unless
	// `$ANTIGRAVITY_HOME/secrets/<provider_id>` is
	// non-empty.
	Configured bool `json:"configured"`
	// Scopes is the capability list the provider
	// advertises. omitempty so the wire drops the
	// nil slice.
	Scopes []string `json:"scopes,omitempty"`
	// Version is the provider-reported version
	// string. omitempty so the wire drops empty
	// values.
	Version string `json:"version,omitempty"`
}

// Resource is one row of the
// ListRemoteAgentAvailableResources response. The fields
// mirror the brief:
//
//   - ResourceID: the dashboard-facing id ("github:owner/repo",
//     "sentry:project-slug", "linear:cycle-123"). Required
//     and unique within the registry; today's fixtures pin
//     `<provider_id>:<canonical-ref>` shape.
//
//   - ProviderID: the resource's parent provider
//     (e.g. "github" / "sentry" / "linear"). Required;
//     matches one of the registry's ResourceProvider IDs.
//
//   - Kind: the resource's category (must match the
//     parent provider's Kind for the brief's filter
//     combinations to be coherent; the cycle's fixture
//     set ensures that).
//
//   - Label: the human-facing display label
//     ("owner/repo", "project-slug", "Cycle 123"). Today
//     mirrors `Ref`; the field is separate so a future
//     cycle can carry a friendlier label without rewriting
//     the canonical ref.
//
//   - Ref: the canonical, parseable reference
//     ("owner/repo" / "project-slug" / "issue-number").
//     Unique within a provider; this is the slice the
//     future rc-resources-sync cycle would dedup on.
//
//   - PayloadSummary: a short, dashboard-friendly status
//     string ("123 open issues, 7 PRs" / "5 unresolved
//     alerts"). Optional (omitempty so absent → "").
//     The cycle's fixtures seed a small static summary;
//     a future cycle that pulls live provider data replaces
//     the fixture with the actual summary.
type Resource struct {
	// ResourceID is the dashboard-facing id.
	// Required and unique within the registry.
	ResourceID string `json:"resource_id"`
	// ProviderID is the resource's parent provider.
	ProviderID string `json:"provider_id"`
	// Kind is one of AllResourceKinds. Must match the
	// parent provider's Kind (enforced by the fixture
	// seed; a future cycle that pulls live data keeps
	// the invariant).
	Kind string `json:"kind"`
	// Label is the human-facing display label.
	Label string `json:"label"`
	// Ref is the canonical, parseable reference.
	Ref string `json:"ref"`
	// PayloadSummary is a short dashboard-friendly
	// status string. omitempty so the wire drops "".
	PayloadSummary string `json:"payload_summary,omitempty"`
}

// ResourceState is the on-disk / wire envelope the store
// holds. One instance per
// $ANTIGRAVITY_HOME/resources.json; the store is
// single-document by design — there is no "list of
// states" because there is exactly one registry per host.
//
// The struct is intentionally JSON-serialised with
// lowercase keys so the on-disk format matches the wire
// format the dashboard reads. The Version field is bumped
// on breaking changes (the brief pins v1).
type ResourceState struct {
	// Version is the on-disk format version. Bumped
	// on breaking changes (renamed keys, removed
	// fields); non-breaking additions (new optional
	// fields with omitempty) leave the version alone
	// so older readers still parse the file.
	Version int `json:"version"`
	// Providers is the live provider slice. nil →
	// empty so the wire always sees "[]".
	Providers []ResourceProvider `json:"providers"`
	// Resources is the live resource slice. nil →
	// empty so the wire always sees "[]".
	Resources []Resource `json:"resources"`
	// UpdatedAt is the wall-clock stamp the store
	// bumped at the most-recent mutation.
	// RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// ResourceStore is the seam the three JSON-RPC methods
// call into. Cycle rc-resources adds Providers /
// Resources / Bind; future cycles
// (rc-resources-sync) will extend it with a per-provider
// refresh hook + a SetConfigured call once the
// `$ANTIGRAVITY_HOME/secrets/` discovery path is live.
//
// Providers returns the full provider slice, sorted by
// ProviderID ascending so dashboards see a stable
// order. The dispatcher wraps the result into the wire
// envelope unchanged; filtering by kind happens at the
// dispatcher layer (today's brief only calls out the
// `kind` filter on Resources, but the brief also notes
// "List* methods return all rows" — implementing the
// filter on Providers is one extra line at the handler
// site, deferred to the future cycle).
//
// Resources returns the resources matching the supplied
// filters. An empty providerID means "all providers"; an
// empty kind means "all kinds". The returned slice is
// sorted by ResourceID ascending so dashboards see a
// stable order.
//
// Bind attaches resourceIDs to projectID. Returns
// (bound, unbound, err) where:
//
//   - bound: the number of resource_ids the store
//     actually attached (the unique, recognised ids in
//     the input).
//   - unbound: the number of resource_ids that were
//     NOT recognised (the store's behaviour on unknown
//     ids is "silently no-op", per the brief: "unknown
//     resource is no-op"). Today's dash callers learn
//     this from the bound/unbound counts rather than
//     from a hard error.
//   - err: ErrResourceProjectNotFound when projectID is
//     unknown; ErrPermission-style I/O when the on-disk
//     write fails.
//
// Unknown resourceIDs are silently dropped: a dashboard
// that retries with a stale list (a resource was deleted
// between List and Update) sees the unbound count grow
// without a hard error. The brief calls this out
// explicitly: "Unbound by unknown resource is no-op".
//
// All three methods take a context the dispatcher already
// passes through; neither method blocks on I/O today (the
// FS store uses an in-process mutex + synchronous
// write), but the signature is future-proof for a cycle
// that promotes the write path to async / batched.
type ResourceStore interface {
	Providers(ctx context.Context) []ResourceProvider
	Resources(ctx context.Context, providerID, kind string) []Resource
	Bind(ctx context.Context, projectID string, resourceIDs []string) (bound int, unbound int, err error)
}

// ErrResourceProviderUnknown is the sentinel
// `ListRemoteAgentAvailableResources` returns when the
// caller asks for a `provider_id` filter that does not
// exist in the registry. The dispatcher translates this
// to JSON-RPC -32602 "Invalid params" (the brief calls
// for -32602 explicitly; -32603 "Internal error" would
// imply a server bug rather than a dashboard bug, which
// is not what's happening here).
//
// Today's caller (a hypothetical dashboard) should
// validate against
// `ListRemoteAgentResourceProviders` first; the sentinel
// is the defence-in-depth path when the dashboard skips
// that preflight.
//
// Cycle rc-resources adds this code.
var ErrResourceProviderUnknown = errors.New("rc-resources: unknown resource provider")

// ErrResourceProjectNotFound is the sentinel
// `Bind` returns when projectID doesn't exist in the
// underlying ProjectStore. The dispatcher translates
// to JSON-RPC -32004 "Project not found" — matches the
// cycle-9 / cycle-22 `-32004` envelope the dashboard
// already recognises on `UpdateRemoteAgentProject` /
// `DeleteRemoteAgentProject`.
//
// Cycle rc-resources adds this code (and reuses the
// existing -32004 envelope).
var ErrResourceProjectNotFound = errors.New("rc-resources: project not found")

// ResourceStateVersion is the on-disk format version the
// current store writes. Bumped on breaking changes
// (renamed keys, removed fields); non-breaking additions
// (new optional fields with omitempty) leave the version
// alone so older readers still parse the file.
//
// v1 (cycle rc-resources): the initial layout.
//
// The on-disk `version` stamp is mandatory; a future
// cycle that introduces a breaking change only has to
// bump the constant AND update the version-mismatch
// branch in (*FSResourceStore).load.
const ResourceStateVersion = 1

// NotifResourcesChangedMethod is the JSON-RPC method
// name the dispatcher uses for the server-push
// notification emitted on every successful Bind. Wired
// via Dispatcher.BroadcastNotify(method, params) once
// per mutated conn.
//
// A `resources/changed` notification carries the same
// shape the response envelope emits (see
// resourcesChangedToWire).
const NotifResourcesChangedMethod = "resources/changed"

// normaliseProviderID trims whitespace from the user-
// supplied provider_id. Empty after trim → "". Used by
// the dispatcher layer (before validating against the
// registry) and the store layer (when comparing for
// membership). Returns "" when the trimmed result is
// empty so the same check trips everywhere.
//
// Today the store does not auto-normalise (Bind that
// pass "  github  " and "github" are considered
// distinct), but the helper is here so a future cycle
// that wants server-side canonicalisation (typical for
// id-shaped fields) only has to swap the lookup line
// in resources_store.go for normaliseProviderID(...).
// The dispatcher path is the only validation site
// today.
func normaliseProviderID(id string) string {
	return strings.TrimSpace(id)
}

// normaliseResourceID trims whitespace from the user-
// supplied resource_id. Mirrors normaliseProviderID;
// kept separate so a future cycle that wants stricter
// normalisation on resource_ids (e.g. lower-casing)
// only has to extend this helper without touching
// normaliseProviderID.
func normaliseResourceID(id string) string {
	return strings.TrimSpace(id)
}

// sortProvidersByID orders providers in place by
// ProviderID ascending. The store applies this before
// handing the slice to the dispatcher so dashboards see
// a stable order. Stable secondary sort by Name
// (alphabetical) so two providers with the same id
// (impossible today — the store's snapshot is the only
// writer — but defensive) have a deterministic order.
func sortProvidersByID(providers []ResourceProvider) {
	sort.SliceStable(providers, func(i, j int) bool {
		if providers[i].ProviderID == providers[j].ProviderID {
			return providers[i].Name < providers[j].Name
		}
		return providers[i].ProviderID < providers[j].ProviderID
	})
}

// sortResourcesByID orders resources in place by
// ResourceID ascending. Mirrors sortProvidersByID. The
// dashboard's render loop relies on this stable order
// for the deduplication preview.
func sortResourcesByID(resources []Resource) {
	sort.SliceStable(resources, func(i, j int) bool {
		if resources[i].ResourceID == resources[j].ResourceID {
			return resources[i].Ref < resources[j].Ref
		}
		return resources[i].ResourceID < resources[j].ResourceID
	})
}

// resourcesChangedToWire renders the
// `resources/changed` notification params. The brief's
// wire envelope is:
//
//	{
//	  "ok":           true,
//	  "project_id":   "<id>",
//	  "bound_count":  <int>,
//	  "unbound_count":<int>,
//	  "updated_at":   "<RFC3339>"
//	}
//
// The same helper renders the UpdateRemoteAgentProjectResources
// response envelope so a polling caller and a
// notification subscriber see byte-for-byte identical
// shapes. Mirrors broadcastFastpushSettingChanged /
// broadcastPluginsChanged / broadcastTriggersChanged
// (the rc-plugins / rc-triggers / rc-fastpush-toggle
// precedent).
func resourcesChangedToWire(projectID string, bound, unbound int, updatedAt time.Time) map[string]any {
	return map[string]any{
		"ok":            true,
		"project_id":    projectID,
		"bound_count":   bound,
		"unbound_count": unbound,
		"updated_at":    updatedAt.UTC().Format(time.RFC3339),
	}
}

// mapResourceError maps the ResourceStore sentinel
// errors to the JSON-RPC envelopes the brief calls for:
//
//	ErrResourceProjectNotFound → -32004 "Project not found"
//	ErrResourceProviderUnknown → -32602 "Invalid params"
//	(other / I/O)              → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed. Mirrors mapFastpushError /
// mapPluginsError / mapTriggersError's shape; the
// preceding cycles established the pattern.
//
// -32602 ("Invalid params") is shared with every other
// method's "bad input" envelope and is emitted directly
// from the handler, not via this helper.
func mapResourceError(err error, op string) *rpcErrorBody {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrResourceProjectNotFound) {
		return &rpcErrorBody{Code: -32004, Message: "Project not found", Data: err.Error()}
	}
	if errors.Is(err, ErrResourceProviderUnknown) {
		// The brief pins -32602 for the
		// unknown-provider-filter case; the mapResourceError
		// helper exists because it's the only
		// sentinel path where the helper is needed.
		return &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
	}
	return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
}
