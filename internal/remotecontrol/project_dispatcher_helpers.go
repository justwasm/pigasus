// Package remotecontrol — project_dispatcher_helpers.go:
// helpers backing the per-method JSON-RPC handlers in
// project_dispatcher.go. Extracted to keep each
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// decodeCreateProjectParams: full validation chain for
// CreateRemoteAgentProject (non-string field check,
// trim, required name/workspace, closed-set effort).
//
// projectToWire: per-row wire-shape projector. The
// Handler runs this for single-row responses; the
// List-side projection lives next to its caller in
// projectSliceToWire in project_dispatcher.go.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"encoding/json"
	"time"
)

// decodeCreateProjectParams unmarshals
// CreateRemoteAgentProject params, applies the
// validation chain (non-string fields, trim,
// required name+workspace, closed-set effort), and
// returns the populated struct + nil (or zero +
// *rpcErrorBody on failure). Extracted from
// handleCreateRemoteAgentProject to keep the handler
// body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeCreateProjectParams(params json.RawMessage) (*createRemoteAgentProjectParams, *rpcErrorBody) {
	var p createRemoteAgentProjectParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	// Type-check the string fields so a dashboard
	// that sends {"name": 42} doesn't silently
	// default-walk to "" → -32602. Matches the
	// pattern dispatch.go's handleStartRemoteAgentCascade
	// uses for the same reason.
	if nonStringFieldInJSON(params, "name", p.Name) ||
		nonStringFieldInJSON(params, "workspace", p.Workspace) ||
		nonStringFieldInJSON(params, "default_model", p.DefaultModel) ||
		nonStringFieldInJSON(params, "default_effort", p.DefaultEffort) {
		return nil, invalidParams("string fields must be strings")
	}
	if stringsTrimSpace(p.Name) == "" {
		return nil, invalidParams("name is required")
	}
	if stringsTrimSpace(p.Workspace) == "" {
		return nil, invalidParams("workspace is required")
	}
	if p.DefaultEffort != "" && !IsValidEffort(p.DefaultEffort) {
		return nil, invalidParams("default_effort must be one of low|medium|high|xhigh|max")
	}
	return &p, nil
}

// projectToWire projects one Project row onto the
// dashboard-facing envelope shape. Matches the
// per-row shape projectSliceToWire emits. Timestamps
// are RFC3339Nano UTC (cross-cycle consistency with
// the rc-list-readonly conversation envelopes).
func projectToWire(row Project) map[string]any {
	return map[string]any{
		"id":             row.ID,
		"name":           row.Name,
		"workspace":      row.Workspace,
		"default_model":  row.DefaultModel,
		"default_effort": row.DefaultEffort,
		"created_at":     row.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":     row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
