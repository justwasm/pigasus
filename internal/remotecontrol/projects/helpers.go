// Package projects — helpers.go: the helpers backing the
// per-method JSON-RPC handlers in dispatcher.go.
// Extracted to keep each
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// decodeCreateParams: full validation chain for
// CreateRemoteAgentProject (non-string field check,
// trim, required name/workspace, closed-set effort).
//
// toWire: per-row wire-shape projector. The
// Handler runs this for single-row responses; the
// List-side projection lives next to its caller in
// sliceToWire in dispatcher.go.
//
// Stdlib-only. No third-party deps.
package projects

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/effort"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// decodeCreateParams unmarshals
// CreateRemoteAgentProject params, applies the
// validation chain (non-string fields, trim,
// required name+workspace, closed-set effort), and
// returns the populated struct + nil (or zero +
// *rpc.ErrorBody on failure). Extracted from
// handleCreateRemoteAgentProject to keep the handler
// body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeCreateParams(params json.RawMessage) (*createParams, *rpc.ErrorBody) {
	var p createParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	// Type-check the string fields so a dashboard
	// that sends {"name": 42} doesn't silently
	// default-walk to "" → -32602. Matches the
	// pattern dispatch.go's handleStartRemoteAgentCascade
	// uses for the same reason.
	if rpc.NonStringFieldInJSON(params, "name", p.Name) ||
		rpc.NonStringFieldInJSON(params, "workspace", p.Workspace) ||
		rpc.NonStringFieldInJSON(params, "default_model", p.DefaultModel) ||
		rpc.NonStringFieldInJSON(params, "default_effort", p.DefaultEffort) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, rpc.InvalidParams("name is required")
	}
	if strings.TrimSpace(p.Workspace) == "" {
		return nil, rpc.InvalidParams("workspace is required")
	}
	if p.DefaultEffort != "" && !effort.IsValid(p.DefaultEffort) {
		return nil, rpc.InvalidParams("default_effort must be one of low|medium|high|xhigh|max")
	}
	return &p, nil
}

// toWire projects one Project row onto the
// dashboard-facing envelope shape. Matches the
// per-row shape sliceToWire emits. Timestamps
// are RFC3339Nano UTC (cross-cycle consistency with
// the rc-list-readonly conversation envelopes).
func toWire(row Project) map[string]any {
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
