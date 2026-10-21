// Package remotecontrol —
// top_level_dispatcher_helpers.go: helpers backing
// the three top-level JSON-RPC handlers in
// top_level_dispatcher.go. The gatekeeper allowlist
// lives here so a reader can audit the method
// surface in one place, and so the post-cycle
// rc-top-level-rpc expansion only has to add a single
// entry to a single map.
//
// decodeRemoteControlOnMessage: unmarshals the
// outer {message:{...}} envelope, validates the
// required message object + string method, and
// trims whitespace. Pure refactor of the head of
// handleRemoteControlOnMessage — extracted so the
// dispatcher file's handler bodies stay under the
// 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// buildRemoteControlInnerFrame: serialises the
// inner JSON-RPC 2.0 frame the handler
// re-dispatches. Pure refactor of the tail of
// handleRemoteControlOnMessage.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"encoding/json"
	"log"
	"strings"
)

// decodeRemoteControlOnMessage unmarshals the outer
// remoteControlOnMessage envelope {"message": {"jsonrpc":
// "2.0", "method": "<name>", "params": {…}}}, validates
// the required message-object + non-empty string-method
// fields, and returns the trimmed inner method / params
// pair + nil (or "" / nil + *rpcErrorBody on failure).
// Extracted from handleRemoteControlOnMessage to keep
// the handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeRemoteControlOnMessage(params json.RawMessage) (string, json.RawMessage, *rpcErrorBody) {
	var pWrap struct {
		Message struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		} `json:"message"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &pWrap); err != nil {
			log.Printf("rc-top-level-rpc: remoteControlOnMessage bad params: %v", err)
			return "", nil, invalidParams(err.Error())
		}
	}
	// -32602 envelope: the "message" object must be
	// present and non-null. A dashboard that posts
	// `{}` (no message key) or `{"message": null}`
	// hits this branch.
	if !nonNullObjectInJSON(params, "message") {
		log.Printf("rc-top-level-rpc: remoteControlOnMessage missing message object")
		return "", nil, invalidParams("message is required and must be a non-null object")
	}
	if nonStringFieldInJSON(params, "message.method", pWrap.Message.Method) {
		log.Printf("rc-top-level-rpc: remoteControlOnMessage method not string")
		return "", nil, invalidParams("message.method must be a string")
	}
	method := strings.TrimSpace(pWrap.Message.Method)
	if method == "" {
		log.Printf("rc-top-level-rpc: remoteControlOnMessage empty method")
		return "", nil, invalidParams("message.method is required and must be non-empty")
	}
	return method, pWrap.Message.Params, nil
}

// buildRemoteControlInnerFrame serialises the
// JSON-RPC 2.0 inner frame the handler
// re-dispatches to the dispatcher's normal handler
// table. The inner id is hard-coded to "1" so the
// caller can correlate the inner response in
// logs; the outer envelope is the confirmation
// (the inner frame's response is intentionally
// discarded by the caller).
//
// Extracted from handleRemoteControlOnMessage to
// keep that handler body under the 50-fn ceiling
// cycle rc-filesize-internal-remotecontrol enforces.
func buildRemoteControlInnerFrame(method string, params json.RawMessage) (json.RawMessage, error) {
	inner := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(`1`),
		"method":  method,
	}
	if len(params) > 0 && string(params) != "null" {
		inner["params"] = json.RawMessage(params)
	}
	return json.Marshal(inner)
}
