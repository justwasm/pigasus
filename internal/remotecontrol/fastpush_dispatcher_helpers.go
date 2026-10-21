// Package remotecontrol — fastpush_dispatcher_helpers.go:
// the helpers backing the single SetRemoteAgentFastpushEnabled
// handler in fastpush_dispatcher.go. Extracted to keep the
// handler under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// decodeFastpushParams: unmarshal + non-string field
// guard + trim + required-value checks, all in one
// helper. Returns the (cascade_id, enabled, *rpcErrorBody)
// tuple so the handler can stay focused on store
// mutation + broadcast + response envelope.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import "encoding/json"

// decodeFastpushParams unmarshals
// SetRemoteAgentFastpushEnabled params, applies the
// JSON-shape validation the brief pins, and returns the
// (cascade_id, enabled, *rpcErrorBody) tuple. Empty /
// missing / wrong-type cascade_id and missing enabled
// → -32602. Extracted from handleSet… to keep the
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeFastpushParams(params json.RawMessage) (string, bool, *rpcErrorBody) {
	var p setRemoteAgentFastpushEnabledParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return "", false, invalidParams(err.Error())
		}
	}
	// Type-check string-typed fields so a numeric
	// `cascade_id` doesn't silently default-walk to
	// "" (mirrors the rc-cascade-message /
	// rc-cancel-cascade pattern).
	if nonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return "", false, invalidParams("string fields must be strings")
	}
	cascadeID := normaliseCascadeID(p.CascadeID)
	if cascadeID == "" {
		return "", false, invalidParams("cascade_id is required and must be non-empty")
	}
	// The enabled field is required. The pointer-
	// typed field distinguishes "absent" from
	// "explicit false" so a dashboard that sends
	// {"enabled": false} lands as a legitimate
	// "toggle off" mutation (not a silent
	// default-walk).
	if p.Enabled == nil {
		return "", false, invalidParams("enabled is required")
	}
	return cascadeID, *p.Enabled, nil
}
