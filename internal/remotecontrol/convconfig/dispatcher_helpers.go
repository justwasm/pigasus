// Package remotecontrol —
// conversation_config_dispatcher_helpers.go:
// helpers backing the single
// UpsertRemoteAgentConversationConfig handler in
// conversation_config_dispatcher.go. Extracted to
// keep the handler body under the 50-fn ceiling
// cycle rc-filesize-internal-remotecontrol
// enforces.
//
// decodeUpsertParams: full
// validation chain — JSON unmarshal, non-string
// type checks, trim + required-field checks,
// non-null config-object check, and the per-field
// config validator. Returns the (conversation_id,
// config, *rpc.ErrorBody) tuple so the handler can
// stay focused on store mutation + broadcast +
// response envelope.
//
// Stdlib-only. No third-party deps.
package convconfig

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// decodeUpsertParams unmarshals
// UpsertRemoteAgentConversationConfig params,
// applies the full validation chain the brief pins,
// and returns the (conversation_id, config,
// *rpc.ErrorBody) tuple.
//
// Validation order:
//
//   - JSON parse failures → -32602.
//   - non-string conversation_id → -32602
//     (defence against a numeric id default-walking
//     to "").
//   - empty / whitespace conversation_id → -32602.
//   - missing / null `config` object → -32602.
//   - per-field config validation failures
//     (Validate) → -32602 —
//     defends against a dashboard bypassing the
//     closed-set enum / bounded-range rules.
//
// Extracted from handleUpsert… to keep the handler
// body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeUpsertParams(params json.RawMessage) (*upsertParams, *rpc.ErrorBody) {
	var p upsertParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	// Type-check string-typed fields so a numeric
	// `conversation_id` doesn't silently default-
	// walk to "" (mirrors the rc-cascade-message /
	// rc-cancel-cascade pattern).
	if rpc.NonStringFieldInJSON(params, "conversation_id", p.ConversationID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	convID := normaliseConversationID(p.ConversationID)
	if convID == "" {
		return nil, rpc.InvalidParams("conversation_id is required and must be non-empty")
	}
	// The config field is required. The
	// dispatcher's "len(params) > 0" guard above
	// accepts a JSON null as a present-but-empty
	// params block; we explicitly require the
	// field to be a JSON object ({"config":{...}})
	// — a missing key or null lands as -32602.
	if !rpc.NonNullObjectInJSON(params, "config") {
		return nil, rpc.InvalidParams("config is required")
	}
	// Validation chain: walk the per-field
	// closed-set enum / bounded-range rules. A
	// failure here is a dashboard bug, lands as
	// -32602 verbatim, and short-circuits before
	// the store call so a bad upsert never
	// triggers an on-disk write.
	if err := Validate(p.Config); err != nil {
		return nil, rpc.InvalidParams(err.Error())
	}
	p.ConversationID = convID
	return &p, nil
}
