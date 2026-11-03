// Package permissions — dispatcher.go: the
// per-method JSON-RPC handlers for the four permissions
// methods introduced in cycle rc-permissions-grants:
//
//	SetRemoteAgentPermissionPreset
//	SetRemoteAgentArtifactReviewMode
//	AddRemoteAgentPermissionGrants
//	RemoveRemoteAgentPermissionGrants
//
// Why a dedicated file (not another switch arm in
// the dispatcher's route table): the four handlers
// share the Store seam + a small sentinel-error map
// (ErrGrantExists → -32014,
// ErrGrantMalformed → -32602), and a
// dashboard-driven Set always needs to emit a
// permissions/changed broadcast notification. Putting
// the handlers here, next to the type definitions
// (permissions.go) and the store (store_mem.go /
// store_fs.go), keeps the per-method bodies focused on
// the JSON-RPC envelope shape rather than the
// dispatcher's switch table.
//
// Sentinel map:
//
//	ErrGrantExists    → -32014 "Permission grant exists"
//	ErrGrantMalformed → -32602 "Invalid params"
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
// new state. The broadcast goes out through the
// Seams.Broadcast callback the host installs: the
// domain does not hold the conn registry — that lives on
// the transport — and it does not know the concrete
// transport type. In-process callers without a
// transport fall back to a no-op broadcast.
//
// Stdlib-only. No third-party deps.
package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// NotifChangedMethod is the JSON-RPC method name
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
const NotifChangedMethod = "permissions/changed"

// StateWireFormat is the wire / on-disk
// format version. Must match onDiskState's
// `version` key. Surfaced in the wire envelope so a
// future dashboard can branch on the version before
// unmarshalling.
const StateWireFormat = 1

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
// a required non-empty array of grant strings. The
// handler re-decodes them via ParseGrantKey so the
// store's defensive Malformed check is a
// second-line-of-defence rather than the only check.
//
// A grant string is the reference's own "action(target)"
// form; the clone's legacy "tool:pattern" one is still
// accepted.
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
// is a required non-empty array of grant strings.
type removeRemoteAgentPermissionGrantsParams struct {
	ToolPatterns []string `json:"tool_patterns"`
}

// HandleSetPreset is the
// JSON-RPC handler for
// "SetRemoteAgentPermissionPreset". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - preset: required, must be one of
//     PresetDefault / Strict / Permissive.
//     Anything else → -32602.
//
// Store call:
//
//   - s.Store == nil → -32603 (no store
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
func HandleSetPreset(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no permission store installed")
	}
	var p setRemoteAgentPermissionPresetParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "preset", p.Preset) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	preset := Preset(strings.TrimSpace(p.Preset))
	if !IsValidPreset(preset) {
		return nil, rpc.InvalidParams("preset must be one of default|strict|permissive")
	}
	state, err := s.Store.SetPreset(nil, preset)
	if err != nil {
		return nil, MapError(err, "set preset")
	}
	broadcastChanged(s, state)
	return StateToWire(state), nil
}

// HandleSetReviewMode is the
// JSON-RPC handler for
// "SetRemoteAgentArtifactReviewMode". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - mode: required, must be one of
//     ReviewModeAlways / Never / Auto.
//     Anything else → -32602.
//
// Store call: matches SetPreset's pattern (nil store →
// -32603; other errors → -32603).
//
// On success the handler emits a
// permissions/changed notification.
func HandleSetReviewMode(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no permission store installed")
	}
	var p setRemoteAgentArtifactReviewModeParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "mode", p.Mode) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	mode := ReviewMode(strings.TrimSpace(p.Mode))
	if !IsValidReviewMode(mode) {
		return nil, rpc.InvalidParams("mode must be one of always|never|auto")
	}
	state, err := s.Store.SetReview(nil, mode)
	if err != nil {
		return nil, MapError(err, "set review mode")
	}
	broadcastChanged(s, state)
	return StateToWire(state), nil
}

// HandleAddGrants is the
// JSON-RPC handler for
// "AddRemoteAgentPermissionGrants". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - tool_patterns: required, non-empty array of
//     grant strings. Empty / missing → -32602.
//     Non-string entries → -32602. Any entry that does
//     not parse via ParseGrantKey → -32602, and the
//     `data` field carries the reference's own error.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrGrantExists (the store refuses a
//     duplicate grant) → -32014.
//   - other error → -32603.
//
// On success the handler emits a
// permissions/changed notification. The wire
// envelope carries the full post-mutation state so a
// dashboard sees the new grants without re-Listing.
func HandleAddGrants(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no permission store installed")
	}
	var p addRemoteAgentPermissionGrantsParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "reason", p.Reason) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	if len(p.ToolPatterns) == 0 {
		return nil, rpc.InvalidParams("tool_patterns is required and must be non-empty")
	}
	// Re-type-check every entry so a dashboard that
	// sends [42, "ok:1"] gets -32602 (rather than a
	// silent ignore). The per-entry Parse check
	// catches malformed shapes.
	grants, err := parseAddGrants(p.ToolPatterns, strings.TrimSpace(p.Reason))
	if err != nil {
		return nil, rpc.InvalidParams(grantParseError(err))
	}
	state, err := s.Store.AddGrants(nil, grants)
	if err != nil {
		return nil, MapError(err, "add grants")
	}
	broadcastChanged(s, state)
	return StateToWire(state), nil
}

// HandleRemoveGrants is the
// JSON-RPC handler for
// "RemoveRemoteAgentPermissionGrants". Cycle
// rc-permissions-grants.
//
// Param validation:
//
//   - tool_patterns: required, non-empty array of
//     grant strings. Empty / missing → -32602.
//     Malformed entries → -32602.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrGrantMalformed (defence-in-depth;
//     the handler's ParseGrantKey should have
//     caught these) → -32602.
//   - ErrGrantExists is NOT in the Remove
//     path; Remove is idempotent (unknown keys are
//     silently dropped).
//   - other error → -32603.
//
// On success the handler emits a
// permissions/changed notification. The wire
// envelope carries the full post-mutation state so a
// dashboard sees the new grants without re-Listing.
func HandleRemoveGrants(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no permission store installed")
	}
	var p removeRemoteAgentPermissionGrantsParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if len(p.ToolPatterns) == 0 {
		return nil, rpc.InvalidParams("tool_patterns is required and must be non-empty")
	}
	// Validate every entry up-front so the dashboard
	// gets the reference's own words about the offending
	// string.
	for _, raw := range p.ToolPatterns {
		if _, err := ParseGrantKey(raw); err != nil {
			return nil, rpc.InvalidParams(grantParseError(err))
		}
	}
	state, err := s.Store.RemoveGrants(nil, p.ToolPatterns)
	if err != nil {
		return nil, MapError(err, "remove grants")
	}
	broadcastChanged(s, state)
	return StateToWire(state), nil
}

// parseAddGrants is the wire-decode helper for
// AddRemoteAgentPermissionGrants. Walks the grant
// entries and produces a []Grant ready for the store. The
// per-entry Reason is the request-level Reason;
// per-entry override is parked for a future cycle.
//
// Returns the reference's own error on any entry it
// refuses, so the dashboard's `data` field carries the
// reference's wording.
func parseAddGrants(toolPatterns []string, reason string) ([]Grant, error) {
	out := make([]Grant, 0, len(toolPatterns))
	for _, raw := range toolPatterns {
		g, err := ParseGrantKey(raw)
		if err != nil {
			return nil, err
		}
		g.Reason = reason
		out = append(out, g)
	}
	return out, nil
}

// StateToWire renders a State as
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
func StateToWire(state State) map[string]any {
	grants := state.Grants
	if grants == nil {
		grants = []Grant{}
	}
	grantList := make([]any, 0, len(grants))
	for _, g := range grants {
		entry := map[string]any{
			"tool_name": g.ToolName,
			"pattern":   g.Pattern,
			"added_at":  g.AddedAt.UTC().Format(time.RFC3339Nano),
		}
		// A grant that names an action carries the reference's own
		// action; a legacy grant carries no action and keeps its
		// tool name.
		if g.Action != "" {
			entry["action"] = g.Action
		}
		if g.Reason != "" {
			entry["reason"] = g.Reason
		}
		grantList = append(grantList, entry)
	}
	version := state.Version
	if version == 0 {
		version = StateWireFormat
	}
	return map[string]any{
		"version":    version,
		"preset":     string(state.Preset),
		"review":     string(state.Review),
		"grants":     grantList,
		"updated_at": state.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// MapError maps the Store
// sentinel errors to the JSON-RPC envelopes the
// brief calls for:
//
//	ErrGrantExists    → -32014 "Permission grant exists"
//	ErrGrantMalformed → -32602 "Invalid params"
//	(other / I/O)               → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed. Matches the projects domain's
// MapError pattern; the cycle auditor will compare
// the two.
func MapError(err error, op string) *rpc.ErrorBody {
	switch {
	case errors.Is(err, ErrGrantExists):
		return &rpc.ErrorBody{Code: -32014, Message: "Permission grant exists", Data: err.Error()}
	case errors.Is(err, ErrGrantMalformed):
		return &rpc.ErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
	default:
		return &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
}

// broadcastPermissionsChanged fans a
// permissions/changed notification out to every
// registered transport's open connection.
//
// Why a domain-side helper rather than a
// transport-side one: the four handlers all live on
// the domain, and the broadcaster (Seams.Broadcast)
// is the seam a transport installs to deliver "frame to
// every open conn" without the domain knowing the
// concrete transport type.
//
// Without a registered broadcaster the method is a
// no-op — in-process dispatchers and the Unix-socket
// transport have no notion of "every open
// connection". The mutation still landed in the
// store; the four current methods all emit the
// post-state in their response envelope, so polling
// is always possible.
func broadcastChanged(s Seams, state State) {
	if s.Broadcast == nil {
		return
	}
	params := StateToWire(state)
	s.Broadcast(context.Background(), NotifChangedMethod, params)
}
