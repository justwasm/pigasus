// Package remotecontrol —
// origin_policy_dispatcher_helpers.go: helpers
// backing the single SetRemoteControlOriginPatterns
// handler in origin_policy_dispatcher.go. Extracted
// to keep the handler body under the 50-fn ceiling
// cycle rc-filesize-internal-remotecontrol enforces.
//
// decodeOriginPatternsParams: unmarshals the JSON
// params envelope, validates the required
// `patterns` array field, and returns the trimmed
// pattern slice. nil patterns → -32602.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import "encoding/json"

// decodeOriginPatternsParams unmarshals
// SetRemoteControlOriginPatterns params, validates
// the required `patterns` array, and returns the
// normalised slice + nil (or nil + *rpcErrorBody).
// nil patterns (a dashboard that posts `{"patterns":
// null}` or omits the key) → -32602 "Invalid params".
// Empty array is valid (a dashboard can reset the
// allowlist with `[]`).
//
// Extracted from handleSetRemoteControlOriginPatterns
// to keep the handler body under the 50-fn ceiling
// that cycle rc-filesize-internal-remotecontrol
// enforces.
func decodeOriginPatternsParams(params json.RawMessage) ([]string, *rpcErrorBody) {
	var p setRemoteControlOriginPatternsParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if p.Patterns == nil {
		return nil, invalidParams("patterns is required and must be an array")
	}
	return p.Patterns, nil
}
