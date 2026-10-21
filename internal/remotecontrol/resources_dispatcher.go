// Package remotecontrol — resources_dispatcher.go: the
// per-method JSON-RPC handlers for the three
// resource-management methods introduced in cycle
// rc-resources:
//
//	ListRemoteAgentResourceProviders
//	ListRemoteAgentAvailableResources
//	UpdateRemoteAgentProjectResources
//
// Why a dedicated file (rather than another switch
// arm in dispatch.go): the three handlers share the
// ResourceStore seam + a small sentinel-error map
// (ErrResourceProviderUnknown → -32602,
// ErrResourceProjectNotFound → -32004, I/O → -32603)
// and the broadcast helper that emits
// `resources/changed` on every successful Bind.
// Splitting the handlers out keeps dispatch.go's
// switch arm in one place; the seam mirrors
// plugins_dispatcher.go /
// triggers_dispatcher.go / fastpush_dispatcher.go.
//
// Sentinel map:
//
//	ErrResourceProviderUnknown  → -32602 "Invalid params"
//	ErrResourceProjectNotFound  → -32004 "Project not found"
//	(other / I/O)                → -32603 "Internal error"
//
// The -32004 code is shared with the project-CRUD
// surface's "Project not found" envelope
// (project_dispatcher.go's mapProjectError) and the
// cascade "Cascade not found" envelope
// (ErrCodeCascadeNotFound). -32602 is shared with the
// fastpush-toggle / plugins / triggers "Invalid
// params" envelope; the handler emits that directly
// from the per-method param-validation path before
// calling the store, so the mapResourceError helper
// only sees the (rare) I/O + ErrResourceProjectNotFound
// cases.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// listRemoteAgentResourceProvidersParams is the
// JSON shape the dashboard posts for
// ListRemoteAgentResourceProviders. The brief pins
// this method as a pure read; no params are
// required today.
//
// kind: optional filter on the closed-set
// AllResourceKinds ("vcs" / "observability" / "pm"
// / "docs" / "ci"). An absent / empty kind
// returns every provider. An unknown kind → -32602
// before the store call (matches the brief's
// pre-flight validation rule — the dashboard
// should consult the closed set before posting).
type listRemoteAgentResourceProvidersParams struct {
	Kind string `json:"kind"`
}

// listRemoteAgentAvailableResourcesParams is the
// JSON shape the dashboard posts for
// ListRemoteAgentAvailableResources. Both filters
// are optional; empty means "no filter".
//
// provider_id: optional. Filters to resources
// belonging to that provider. Empty = "all
// providers". The handler validates the filter
// against the live registry before the store call;
// an unknown id → -32602 (matches the brief's
// "unknown provider filter is invalid params"
// rule — the dashboard should call
// ListRemoteAgentResourceProviders first).
//
// kind: optional, same closed-set as
// ListRemoteAgentResourceProviders. Empty = "all
// kinds". Unknown kind → -32602.
type listRemoteAgentAvailableResourcesParams struct {
	ProviderID string `json:"provider_id"`
	Kind       string `json:"kind"`
}

// updateRemoteAgentProjectResourcesParams is the
// JSON shape the dashboard posts for
// UpdateRemoteAgentProjectResources.
//
// project_id: required, non-empty after trim.
// -32602 otherwise.
//
// resource_ids: required field (the brief pins it
// as a JSON array — not null). The handler
// normalises to []string{""} on absence so a
// dashboard that posts `{"project_id":"..."}`
// without a `resource_ids` key is treated as "bind
// to an empty set" (which is a valid unbind). The
// brief's "replace-style bind" semantic means the
// supplied slice IS the project's new bound set;
// an explicit empty list is a valid "unbind
// everything" operation.
type updateRemoteAgentProjectResourcesParams struct {
	ProjectID   string   `json:"project_id"`
	ResourceIDs []string `json:"resource_ids"`
}

// handleListRemoteAgentResourceProviders is the
// JSON-RPC handler for
// "ListRemoteAgentResourceProviders". Cycle
// rc-resources.
//
// Param validation:
//
//   - kind: optional, must be in AllResourceKinds
//     when present. Empty = no filter. Unknown
//     kind → -32602.
//
// Store call:
//
//   - cfg.ResourceStore == nil → -32603 (no
//     resource store installed; production wiring
//     in cmd/wagyu/remote_control.go always
//     installs one — nil means the dispatcher was
//     constructed in a test that didn't bother).
//
// Wire envelope:
//
//	{
//	  "providers": [
//	    {provider_id, name, kind, configured,
//	     scopes[], version}, ...
//	  ],
//	  "total": <len>
//	}
//
// providers is always non-nil (the dispatcher
// coerces nil slices to empty so the wire emits
// `[]` rather than `null`).
func (d *Dispatcher) handleListRemoteAgentResourceProviders(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.ResourceStore == nil {
		return nil, internalError("no resource store installed")
	}
	var p listRemoteAgentResourceProvidersParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "kind", p.Kind) {
		return nil, invalidParams("string fields must be strings")
	}
	kind := strings.TrimSpace(p.Kind)
	if kind != "" && !IsValidResourceKind(kind) {
		return nil, invalidParams("kind must be one of vcs|observability|pm|docs|ci")
	}

	providers := d.cfg.ResourceStore.Providers(nil)
	// Filter by kind when supplied.
	if kind != "" {
		filtered := make([]ResourceProvider, 0, len(providers))
		for _, p := range providers {
			if p.Kind == kind {
				filtered = append(filtered, p)
			}
		}
		providers = filtered
	}
	if providers == nil {
		providers = []ResourceProvider{}
	}
	return map[string]any{
		"providers": providers,
		"total":     len(providers),
	}, nil
}

// handleListRemoteAgentAvailableResources is the
// JSON-RPC handler for
// "ListRemoteAgentAvailableResources". Cycle
// rc-resources.
//
// Param validation:
//
//   - provider_id: optional. Empty = no filter.
//     When supplied, must be one of the
//     providers in the live registry (the
//     handler validates before the store call).
//     Unknown id → -32602 (matches the brief's
//     "unknown provider filter is invalid
//     params" rule).
//   - kind: optional, must be in
//     AllResourceKinds when present. Empty = no
//     filter. Unknown kind → -32602.
//
// Store call:
//
//   - cfg.ResourceStore == nil → -32603.
//
// Wire envelope:
//
//	{
//	  "resources": [
//	    {resource_id, provider_id, kind, label,
//	     ref, payload_summary}, ...
//	  ],
//	  "total": <len>
//	}
//
// resources is always non-nil (the dispatcher
// coerces nil slices to empty so the wire emits
// `[]` rather than `null`).
func (d *Dispatcher) handleListRemoteAgentAvailableResources(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.ResourceStore == nil {
		return nil, internalError("no resource store installed")
	}
	providerID, kind, perr := decodeListResourcesParams(params)
	if perr != nil {
		return nil, perr
	}
	if perr := d.requireKnownResourceProvider(providerID); perr != nil {
		return nil, perr
	}
	resources := d.cfg.ResourceStore.Resources(nil, providerID, kind)
	if resources == nil {
		resources = []Resource{}
	}
	return map[string]any{
		"resources": resources,
		"total":     len(resources),
	}, nil
}

// decodeListResourcesParams unmarshals
// ListRemoteAgentAvailableResources params, validates
// the string-typed fields, trims whitespace, and
// applies the closed-set ResourceKind check. Returns
// (provider_id, kind, *rpcErrorBody). Empty provider_id
// / kind is allowed (no filter). Extracted from
// handleListRemoteAgentAvailableResources to keep the
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeListResourcesParams(params json.RawMessage) (string, string, *rpcErrorBody) {
	var p listRemoteAgentAvailableResourcesParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return "", "", invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "provider_id", p.ProviderID) ||
		nonStringFieldInJSON(params, "kind", p.Kind) {
		return "", "", invalidParams("string fields must be strings")
	}
	providerID := strings.TrimSpace(p.ProviderID)
	kind := strings.TrimSpace(p.Kind)
	if kind != "" && !IsValidResourceKind(kind) {
		return "", "", invalidParams("kind must be one of vcs|observability|pm|docs|ci")
	}
	return providerID, kind, nil
}

// requireKnownResourceProvider returns -32602 when a
// non-empty provider_id doesn't match a live
// registry entry. The dispatcher exposes this as
// -32602 (the "dashboard bug" envelope) rather than
// an empty slice so the dashboard learns the filter
// is stale rather than rendering "no resources".
// Empty provider_id → nil (no filter, skip the check).
// Extracted from handleListRemoteAgentAvailableResources
// to keep the handler body under the 50-fn ceiling
// that cycle rc-filesize-internal-remotecontrol
// enforces.
func (d *Dispatcher) requireKnownResourceProvider(providerID string) *rpcErrorBody {
	if providerID == "" {
		return nil
	}
	for _, pr := range d.cfg.ResourceStore.Providers(nil) {
		if pr.ProviderID == providerID {
			return nil
		}
	}
	return &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "unknown provider_id: " + providerID}
}

// handleUpdateRemoteAgentProjectResources is the
// JSON-RPC handler for
// "UpdateRemoteAgentProjectResources". Cycle
// rc-resources.
//
// Param validation:
//
//   - project_id: required, non-empty after
//     trim. -32602 otherwise.
//   - resource_ids: required JSON field. The
//     brief pins it as a JSON array — the
//     handler coerces a null/missing field to
//     []string{} so a dashboard that posts
//     without the key is treated as "bind to
//     an empty set" (valid unbind). Each id is
//     trimmed; empty ids are skipped.
//
// Store call:
//
//   - cfg.ResourceStore == nil → -32603.
//   - ErrResourceProjectNotFound → -32004
//     (shares the project-not-found envelope
//     with the project-CRUD surface).
//   - other error → -32603.
//
// On success the result envelope carries the same
// shape the broadcast notification emits
// (resourcesChangedToWire):

//	{
//	  "ok":            true,
//	  "project_id":    "<id>",
//	  "bound_count":   <int>,
//	  "unbound_count": <int>,
//	  "updated_at":    "<RFC3339>"
//	}
//
// The handler then broadcasts a
// "resources/changed" notification on every open
// ws connection via
// (*Dispatcher).BroadcastNotify (the seam
// rc-permissions-grants added; shared with
// rc-plugins, rc-triggers, and rc-fastpush-toggle).
// The notification carries the same params so a
// subscriber and a polling caller see
// byte-for-byte identical envelopes.
func (d *Dispatcher) handleUpdateRemoteAgentProjectResources(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.ResourceStore == nil {
		return nil, internalError("no resource store installed")
	}
	projectID, resourceIDs, perr := decodeUpdateProjectResourcesParams(params)
	if perr != nil {
		return nil, perr
	}
	bound, unbound, err := d.cfg.ResourceStore.Bind(ctx, projectID, resourceIDs)
	if err != nil {
		return nil, mapResourceError(err, "update project resources")
	}
	// updated_at = bind-time stamp (the wire
	// envelope describes when the bind landed,
	// not when the registry last changed).
	updatedAt := time.Now().UTC()
	resp := resourcesChangedToWire(projectID, bound, unbound, updatedAt)
	// Broadcast "resources/changed" on every
	// open ws connection via the BroadcastNotify
	// seam (rc-permissions-grants added; shared
	// with rc-plugins, rc-triggers, and
	// rc-fastpush-toggle).
	d.BroadcastNotify(ctx, NotifResourcesChangedMethod, resp)
	return resp, nil
}

// decodeUpdateProjectResourcesParams unmarshals
// UpdateRemoteAgentProjectResources params, validates
// the required project_id, normalises the optional
// resource_ids slice, and returns the (project_id,
// resource_ids, *rpcErrorBody) tuple. nil resource_ids
// is coerced to an empty slice so a "bind to nothing"
// semantic is consistent with the store-level Bind
// behaviour.
//
// Extracted from
// handleUpdateRemoteAgentProjectResources to keep the
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeUpdateProjectResourcesParams(params json.RawMessage) (string, []string, *rpcErrorBody) {
	var p updateRemoteAgentProjectResourcesParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return "", nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "project_id", p.ProjectID) {
		return "", nil, invalidParams("string fields must be strings")
	}
	projectID := strings.TrimSpace(p.ProjectID)
	if projectID == "" {
		return "", nil, invalidParams("project_id is required")
	}
	resourceIDs := p.ResourceIDs
	if resourceIDs == nil {
		resourceIDs = []string{}
	}
	return projectID, resourceIDs, nil
}
