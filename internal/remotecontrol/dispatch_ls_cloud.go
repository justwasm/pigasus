// Package remotecontrol — dispatch_ls_cloud.go: the language server's
// own cloud-backed surfaces.
//
// # What this file is
//
// The language server answers four methods that describe the *cloud*
// the reference runs beside: a project's licence, the cloud's
// environments (capsules), the cloud's resource providers and the
// resources those providers hold.
//
// The clone had no answer of its own for them. Three of them fell
// through the reference-surface alias table onto the clone's *legacy*
// dashboard methods, whose shapes belong to another service:
//
//	ListEnvironments       → ListRemoteAgentEnvironments
//	ListResourceProviders  → ListRemoteAgentResourceProviders
//	ListAvailableResources → ListRemoteAgentAvailableResources
//
// Those legacy methods answer the clone's own contracts — a host
// snapshot (os / arch / hostname / cwd), a registry row
// (provider_id / name / kind / configured) — and *none* of those
// members is a member the language server's schema knows. proto-JSON
// drops a member the schema does not know, and drops the whole list
// with it when one of its rows carries one: the Web UI's resource
// picker read `providers` and `resources` as undefined.
//
// So the alias was a shape mismatch, not an equivalence. The three
// methods are answered here, in the schema's own members, and the
// alias entries are gone.
//
// # What the clone answers
//
// The cloud's registries are Google's: a capsule is a cloud sandbox,
// a resource provider is an account the user connected to the cloud.
// The clone runs on the user's own machine and owns none of them, so
// the lists are empty — an empty list is a true answer, and it is the
// one the Web UI expects (the page reads `environments ?? []`).
//
// The one member the answers do carry is the clone's own plan tier:
// the account behind the shared credential is entitled to it, and that
// is the same entitlement the account chip reports.
package remotecontrol

import (
	"encoding/json"
	"strings"
)

// routeLSCloudMethods fans the language server's cloud surfaces out to
// their per-method handlers.
//
// It is a table of its own so the route table in
// dispatch_languageserver.go stays a table: that file's function is
// already over the 50-line budget the rc-filesize-internal-remotecontrol
// cycle enforces.
func (d *Dispatcher) routeLSCloudMethods(method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ListEnvironments", "listEnvironments":
		return d.handleLSListEnvironments(params)
	case "ListResourceProviders", "listResourceProviders":
		return d.handleLSListResourceProviders(params)
	case "ListAvailableResources", "listAvailableResources":
		return d.handleLSListAvailableResources(params)
	}
	return nil, nil, false
}

// handleLSSelfAssignLicense answers SelfAssignLicense.
//
// 【反编译】 exa.language_server_pb.SelfAssignLicenseRequest carries
// project_id (1) and location (2); the response carries license (1),
// assigned (2), error_message (3) and data_sharing_disclaimer (4).
//
// The answer is *not* an authResult. The clone used to answer this
// method with the shape of LoginResponse and SelectLicenseResponse
// ({"authResult": …}), and proto-JSON dropped the member the schema
// does not know — so the Web UI's inspectAutoAssign() read `assigned`
// and `license` as undefined and reported "no licence" for an account
// that is entitled.
//
// The licence the clone reports is the account's own tier under the
// project the caller named: there is no cloud project to assign a
// licence to, and the tier is the one the account chip already shows.
func (d *Dispatcher) handleLSSelfAssignLicense(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ProjectID string `json:"projectId"`
		Location  string `json:"location"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		projectID = "pigasus-local"
	}
	location := strings.TrimSpace(req.Location)
	if location == "" {
		location = "us-central1"
	}
	return map[string]any{
		"license": map[string]any{
			"userTier":        lsPlanTierID,
			"tierDisplayName": lsPlanTierName,
			"projectId":       projectID,
			"location":        location,
		},
		"assigned": true,
	}, nil, true
}

// handleLSListEnvironments answers ListEnvironments.
//
// 【反编译】 exa.language_server_pb.ListEnvironmentsResponse carries
// `environments` — a repeated
// exa.remote_agent_proxy_pb.EnvironmentInfo.
//
// An EnvironmentInfo describes a cloud environment: a capsule
// (capsule_id, tags, expiration_time) or an ace, plus is_default and
// connection_state. The clone owns no capsule and no ace — it runs on
// the user's own machine — so the list is empty.
//
// The request's `backend` names the backend whose environments are
// wanted (BACKEND_TYPE_LOCAL, BACKEND_TYPE_REMOTE_AICODE,
// BACKEND_TYPE_REMOTE_ENTERPRISE); the clone has one backend and no
// environment of its own, so the answer does not depend on it.
func (d *Dispatcher) handleLSListEnvironments(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{"environments": []any{}}, nil, true
}

// handleLSListResourceProviders answers ListResourceProviders.
//
// 【反编译】 exa.language_server_pb.ListResourceProvidersResponse
// carries `providers` — a repeated
// exa.remote_agent_proxy_pb.ResourceProviderInfo, whose members are
// type (1), display_name (2) and icon_url (3).
//
// A provider is an account the user connected to the cloud (GitHub,
// Google Drive, …). The clone has no cloud connection, so the list is
// empty.
//
// The clone's *own* registry — the one the dashboard's project panel
// binds resources against — stays on the dashboard's own surface
// (ListRemoteAgentResourceProviders): it is a different contract, with
// different members, and the two are not the same thing.
func (d *Dispatcher) handleLSListResourceProviders(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{"providers": []any{}}, nil, true
}

// handleLSListAvailableResources answers ListAvailableResources.
//
// 【反编译】 exa.language_server_pb.ListAvailableResourcesResponse
// carries `resources` (a repeated exa.project_pb.RemoteResource:
// type, resource_id, display_name, icon_url, url, is_private,
// default_branch), plus oauth_access_token and oauth_client_id.
//
// The clone holds no cloud resource and no cloud OAuth token, so the
// lists and the two token members are empty.
func (d *Dispatcher) handleLSListAvailableResources(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{
		"resources":        []any{},
		"oauthAccessToken": "",
		"oauthClientId":    "",
	}, nil, true
}
