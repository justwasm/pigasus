// Package remotecontrol — permissions_dispatcher.go: the
// per-method JSON-RPC handlers for the four permissions
// methods introduced in cycle rc-permissions-grants:
//
//	SetRemoteAgentPermissionPreset
//	SetRemoteAgentArtifactReviewMode
//	AddRemoteAgentPermissionGrants
//	RemoveRemoteAgentPermissionGrants
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the four handlers share the
// PermissionStore seam + a small sentinel-error map
// (ErrPermissionGrantExists → -32014,
// ErrPermissionGrantMalformed → -32602), and a
// dashboard-driven Set always needs to emit a
// permissions/changed broadcast notification. Putting
// the handlers here, next to the type definitions
// (permissions.go) and the store (permissions_store.go),
// keeps the per-method bodies focused on the JSON-RPC
// envelope shape rather than the dispatcher's switch
// table.
//
// Sentinel map:
//
//	ErrPermissionGrantExists    → -32014 "Permission grant exists"
//	ErrPermissionGrantMalformed → -32602 "Invalid params"
//	(other / I/O)               → -32603 "Internal error"
//
// The -32014 code is brand-new this cycle; -32602 is
// shared with every other method's "bad input"
// envelope (the brief explicitly says so). 401 / 403
// are not used — the dashboard is always allowed to
// mutate its own permissions state; the operator-
// friendly "you forgot a colon" envelope is the
// store-defence's only feedback path.
//
// Cycle rc-permissions-grants also adds the "broadcast
// on change" path: every successful Set /
// Add / Remove emits a `permissions/changed`
// notification to every open ws connection so a
// dashboard that subscribed to the channel sees the
// new state. The broadcast is implemented by walking
// the dispatcher's registered notifier and dispatching
// to each known connID. (The dispatcher itself doesn't
// hold the conn registry — that lives on the
// transport — so the broadcast helper uses the same
// Notify callback the per-connID path uses, walking
// the transport's conn map. In-process callers without
// a transport fall back to a no-op broadcast.)
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// PermissionsChangedMethod is the JSON-RPC method name
// the dispatcher uses for the server-push notification
// emitted on every successful permission mutation.
// Wired via Dispatcher.Notify(method, params) once per
// changed conn.
//
// A "permissions/changed" notification carries:
//
//	{"version": 1, "preset": "<preset>",
//	 "review": "<mode>",
//	 "grants": [{"tool_name": ..., "pattern": ...,
//	            "reason": ..., "added_at": ...}, ...],
//	 "updated_at": "<RFC3339Nano>"}
//
// — the same shape the four Set/Add/Remove return
// envelopes emit. The notification lets a dashboard
// that watches the channel re-render without
// re-polling.
const PermissionsChangedMethod = "permissions/changed"

// PermissionStateWireFormat is the wire / on-disk
// format version. Must match onDiskPermissionState's
// `version` key. Surfaced in the wire envelope so a
// future dashboard can branch on the version before
// unmarshalling.
const PermissionStateWireFormat = 1

// setRemoteAgentPermissionPresetParams is the JSON
// shape the dashboard posts for
// SetRemoteAgentPermissionPreset. preset is a
// required string; invalid values are mapped to
// -32602 by the handler before the store sees them.
type setRemoteAgentPermissionPresetParams struct {
	Preset string `json:"preset"`
}

// setRemoteAgentArtifactReviewModeParams is the JSON
// shape the dashboard posts for
// SetRemoteAgentArtifactReviewMode. mode is a
// required string; invalid values are mapped to
// -32602 by the handler before the store sees them.
type setRemoteAgentArtifactReviewModeParams struct {
	Mode string `json:"mode"`
}

// addRemoteAgentPermissionGrantsParams is the JSON
// shape the dashboard posts for
// AddRemoteAgentPermissionGrants. tool_patterns is
// a required non-empty array of
// "<tool_name>:<pattern>" strings. The handler
// re-decodes them via ParseToolPattern so the
// store's defensive Malformed check is a
// second-line-of-defence rather than the only check.
type addRemoteAgentPermissionGrantsParams struct {
	ToolPatterns []string `json:"tool_patterns"`
	// Reason is an optional human-readable rationale
	// that's stamped on every grant in this call. The
	// brief's "reason" field goes here. Per-grant
	// override (rare) can be added in a future cycle
	// by extending the wire with
	// `grants:[{tool, pattern, reason}, ...]`.
	Reason string `json:"reason,omitempty"`
}

// removeRemoteAgentPermissionGrantsParams is the
// JSON shape the dashboard posts for
// RemoveRemoteAgentPermissionGrants. tool_patterns
// is a required non-empty array of
// "<tool_name>:<pattern>" strings.
type removeRemoteAgentPermissionGrantsParams struct {
	ToolPatterns []string `json:"tool_patterns"`
}

// handleSetRemoteAgentPermissionPreset is the
// JSON-RPC handler for
// "SetRemoteAgentPermissionPreset". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - preset: required, must be one of
//     PermissionPresetDefault / Strict / Permissive.
//     Anything else → -32602.
//
// Store call:
//
//   - cfg.PermissionsStore == nil → -32603 (no store
//     installed; production wiring always installs
//     one — nil means the dispatcher was constructed
//     in a test that didn't bother).
//   - the store's error → -32603 (no
//     store-sent sentinel error today; future cycles
//     can add one).
//
// On success the handler emits a
// permissions/changed notification to every open ws
// connection so a dashboard watching the channel
// re-renders without polling.
func (d *Dispatcher) handleSetRemoteAgentPermissionPreset(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.PermissionsStore == nil {
		return nil, internalError("no permission store installed")
	}
	var p setRemoteAgentPermissionPresetParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "preset", p.Preset) {
		return nil, invalidParams("string fields must be strings")
	}
	preset := PermissionPreset(stringsTrimSpace(p.Preset))
	if !IsValidPreset(preset) {
		return nil, invalidParams("preset must be one of default|strict|permissive")
	}
	state, err := d.cfg.PermissionsStore.SetPreset(nil, preset)
	if err != nil {
		return nil, mapPermissionsError(err, "set preset")
	}
	d.broadcastPermissionsChanged(state)
	return permissionStateToWire(state), nil
}

// handleSetRemoteAgentArtifactReviewMode is the
// JSON-RPC handler for
// "SetRemoteAgentArtifactReviewMode". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - mode: required, must be one of
//     ArtifactReviewModeAlways / Never / Auto.
//     Anything else → -32602.
//
// Store call: matches SetPreset's pattern (nil store →
// -32603; other errors → -32603).
//
// On success the handler emits a
// permissions/changed notification.
func (d *Dispatcher) handleSetRemoteAgentArtifactReviewMode(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.PermissionsStore == nil {
		return nil, internalError("no permission store installed")
	}
	var p setRemoteAgentArtifactReviewModeParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "mode", p.Mode) {
		return nil, invalidParams("string fields must be strings")
	}
	mode := ArtifactReviewMode(stringsTrimSpace(p.Mode))
	if !IsValidReviewMode(mode) {
		return nil, invalidParams("mode must be one of always|never|auto")
	}
	state, err := d.cfg.PermissionsStore.SetReview(nil, mode)
	if err != nil {
		return nil, mapPermissionsError(err, "set review mode")
	}
	d.broadcastPermissionsChanged(state)
	return permissionStateToWire(state), nil
}

// handleAddRemoteAgentPermissionGrants is the
// JSON-RPC handler for
// "AddRemoteAgentPermissionGrants". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - tool_patterns: required, non-empty array of
//     strings in the "<tool_name>:<pattern>" wire
//     encoding. Empty / missing → -32602. Non-string
//     entries → -32602. Any entry that does not
//     parse via ParseToolPattern → -32602.
//
// Store call:
//
//   - cfg.PermissionsStore == nil → -32603.
//   - ErrPermissionGrantExists (the store refuses a
//     (tool_name, pattern) duplicate) → -32014.
//   - other error → -32603.
//
// On success the handler emits a
// permissions/changed notification. The wire
// envelope carries the full post-mutation state so a
// dashboard sees the new grants without re-Listing.
func (d *Dispatcher) handleAddRemoteAgentPermissionGrants(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.PermissionsStore == nil {
		return nil, internalError("no permission store installed")
	}
	var p addRemoteAgentPermissionGrantsParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "reason", p.Reason) {
		return nil, invalidParams("string fields must be strings")
	}
	if len(p.ToolPatterns) == 0 {
		return nil, invalidParams("tool_patterns is required and must be non-empty")
	}
	// Re-type-check every entry so a dashboard that
	// sends [42, "ok:1"] gets -32602 (rather than a
	// silent ignore). The per-entry Parse check
	// catches malformed shapes.
	grants, err := parseAddGrants(p.ToolPatterns, stringsTrimSpace(p.Reason))
	if err != nil {
		return nil, invalidParams(err.Error())
	}
	state, err := d.cfg.PermissionsStore.AddGrants(nil, grants)
	if err != nil {
		return nil, mapPermissionsError(err, "add grants")
	}
	d.broadcastPermissionsChanged(state)
	return permissionStateToWire(state), nil
}

// handleRemoveRemoteAgentPermissionGrants is the
// JSON-RPC handler for
// "RemoveRemoteAgentPermissionGrants". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - tool_patterns: required, non-empty array of
//     "<tool_name>:<pattern>" strings. Empty /
//     missing → -32602. Malformed entries → -32602.
//
// Store call:
//
//   - cfg.PermissionsStore == nil → -32603.
//   - ErrPermissionGrantMalformed (defence-in-depth;
//     the handler's ParseToolPattern should have
//     caught these) → -32602.
//   - ErrPermissionGrantExists is NOT in the Remove
//     path; Remove is idempotent (unknown keys are
//     silently dropped).
//   - other error → -32603.
//
// On success the handler emits a
// permissions/changed notification. The wire
// envelope carries the full post-mutation state so a
// dashboard sees the new grants without re-Listing.
func (d *Dispatcher) handleRemoveRemoteAgentPermissionGrants(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.PermissionsStore == nil {
		return nil, internalError("no permission store installed")
	}
	var p removeRemoteAgentPermissionGrantsParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if len(p.ToolPatterns) == 0 {
		return nil, invalidParams("tool_patterns is required and must be non-empty")
	}
	// Validate every entry up-front so the dashboard
	// gets the verbatim offending string in `data`.
	for _, raw := range p.ToolPatterns {
		if _, _, ok := ParseToolPattern(raw); !ok {
			return nil, invalidParams("tool_patterns entry must be \"<tool_name>:<pattern>\": " + raw)
		}
	}
	state, err := d.cfg.PermissionsStore.RemoveGrants(nil, p.ToolPatterns)
	if err != nil {
		return nil, mapPermissionsError(err, "remove grants")
	}
	d.broadcastPermissionsChanged(state)
	return permissionStateToWire(state), nil
}

// parseAddGrants is the wire-decode helper for
// AddRemoteAgentPermissionGrants. Walks the
// "<tool_name>:<pattern>" entries and produces a
// []PermissionGrant ready for the store. The
// per-entry Reason is the request-level Reason;
// per-entry override is parked for a future cycle.
//
// Returns a verbatim error message on any malformed
// entry so the dashboard's `data` field carries the
// rejected input.
func parseAddGrants(toolPatterns []string, reason string) ([]PermissionGrant, error) {
	out := make([]PermissionGrant, 0, len(toolPatterns))
	for _, raw := range toolPatterns {
		tool, pattern, ok := ParseToolPattern(raw)
		if !ok {
			return nil, errors.New("tool_patterns entry must be \"<tool_name>:<pattern>\": " + raw)
		}
		out = append(out, PermissionGrant{
			ToolName: tool,
			Pattern:  pattern,
			Reason:   reason,
		})
	}
	return out, nil
}

// permissionStateToWire renders a PermissionState as
// the dashboard-facing wire shape. The struct mirrors
// the on-disk layout:
//
//	{
//	  "version":    1,
//	  "preset":     "<default|strict|permissive>",
//	  "review":     "<always|never|auto>",
//	  "grants":     [{"tool_name": ..., "pattern": ...,
//	                 "reason": ..., "added_at": ...}, ...],
//	  "updated_at": "<RFC3339Nano>"
//	}
//
// The handler also uses this for the broadcast
// notification's params. nil grants → empty slice
// (`[]`) on the wire so dashboards never see
// `grants: null`.
//
// `grants` is `[]any` (not `[]map[string]any`) so the
// in-process return value matches what the JSON
// round-trip produces: `encoding/json` decodes a JSON
// array into `[]interface{}` (`==` `[]any`), so a
// in-process caller (no JSON round-trip) and a wire
// caller (post JSON) see the same shape. Without this
// an in-process caller would see
// `res["grants"].([]map[string]any)` and a wire caller
// would see `res["grants"].([]any)` — the same field,
// two types, and the in-process tests would fail to
// cast.
func permissionStateToWire(state PermissionState) map[string]any {
	grants := state.Grants
	if grants == nil {
		grants = []PermissionGrant{}
	}
	grantList := make([]any, 0, len(grants))
	for _, g := range grants {
		entry := map[string]any{
			"tool_name": g.ToolName,
			"pattern":   g.Pattern,
			"added_at":  g.AddedAt.UTC().Format(time.RFC3339Nano),
		}
		if g.Reason != "" {
			entry["reason"] = g.Reason
		}
		grantList = append(grantList, entry)
	}
	version := state.Version
	if version == 0 {
		version = PermissionStateWireFormat
	}
	return map[string]any{
		"version":    version,
		"preset":     string(state.Preset),
		"review":     string(state.Review),
		"grants":     grantList,
		"updated_at": state.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// mapPermissionsError maps the PermissionStore
// sentinel errors to the JSON-RPC envelopes the
// brief calls for:
//
//	ErrPermissionGrantExists    → -32014 "Permission grant exists"
//	ErrPermissionGrantMalformed → -32602 "Invalid params"
//	(other / I/O)               → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed. Matches project_dispatcher's
// mapProjectError pattern; the cycle auditor will
// compare the two.
func mapPermissionsError(err error, op string) *rpcErrorBody {
	switch {
	case errors.Is(err, ErrPermissionGrantExists):
		return &rpcErrorBody{Code: -32014, Message: "Permission grant exists", Data: err.Error()}
	case errors.Is(err, ErrPermissionGrantMalformed):
		return &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
	default:
		return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
}

// broadcastPermissionsChanged fans a
// permissions/changed notification out to every
// registered transport's open connection.
//
// Why a dispatcher-side helper rather than a
// transport-side one: the four handlers all live on
// the dispatcher, and the broadcaster (Set
// Broadcaster registered it) is the seam a
// transport installs to deliver "frame to every
// open conn" without the dispatcher knowing the
// concrete transport type.
//
// SetBroadcaster is the seam documented in
// notify.go. The internal/remotetransport package
// wires its own broadcaster at TCPServer
// construction (see the rc-permissions-grants cycle
// commit). Without a registered broadcaster the
// method is a no-op — in-process dispatchers and the
// Unix-socket transport have no notion of "every
// open connection". The mutation still landed in the
// store; the four current methods all emit the
// post-state in their response envelope, so polling
// is always possible.
func (d *Dispatcher) broadcastPermissionsChanged(state PermissionState) {
	if d == nil {
		return
	}
	params := permissionStateToWire(state)
	d.BroadcastNotify(context.Background(), PermissionsChangedMethod, params)
}
