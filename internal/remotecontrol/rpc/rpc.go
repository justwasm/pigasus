// Package rpc — the JSON-RPC 2.0 envelope of the remote-control wire.
//
// The remote-control dispatcher answers every method with a
// JSON-RPC 2.0 envelope: a "result" member on success, an "error"
// member on failure. The envelope's shape is shared by the
// dispatcher, by every domain package the dispatcher fans out to
// (plugins, projects, resources, triggers, fastpush, permissions,
// conversation config, origin policy) and by the transports that
// carry it.
//
// It lives in its own package because it is the one fact every
// domain needs and no domain owns: a domain that builds its own
// error envelope is a domain that can drift from the reference's
// code table.
//
// Stdlib-only. No third-party deps. No dependency on any other
// package of the clone — the envelope is the bottom of the wire.
package rpc

import (
	"encoding/json"
	"fmt"
)

// ErrorBody is the structured form a handler returns; the wire
// envelope is built by Error().
type ErrorBody struct {
	Code    int
	Message string
	Data    any
	Status  int
}

// CodedError is the error a failed dispatch returns.
//
// It carries the JSON-RPC code and the message apart from the
// formatted string, so a transport that speaks Connect can answer
// with the code the reference answers with: the reference answers a
// terminal that is gone with HTTP 404 and
// {"code":"not_found","message":"not_found: terminal … not found"},
// where the clone used to answer HTTP 200 with `{}`.
//
// The transport learns the code through a duck-typed interface (the
// import direction is the other way round: the dispatcher must not
// import the transport).
type CodedError struct {
	// Code is the JSON-RPC code of the failure.
	Code int
	// Message is the message a client sees.
	Message string
	// Data is the failure's detail, if it has one.
	Data any
	// Status is the optional gRPC status code.
	Status int
}

// Error implements error.
func (e *CodedError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("rpc error %d: %s (%v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// JSONRPCCode is the JSON-RPC code the failure carries.
func (e *CodedError) JSONRPCCode() int { return e.Code }

// GRPCStatus is the gRPC status code the failure carries.
func (e *CodedError) GRPCStatus() int { return e.Status }

// ConnectMessage is the message a Connect client sees.
//
// The detail (the failure's data) is the message when it has one: the
// clone's JSON-RPC envelope carries the detail in the error's "data"
// member, while the reference's Connect answer carries it in the
// "message" member.
func (e *CodedError) ConnectMessage() string {
	if detail, ok := e.Data.(string); ok && detail != "" {
		return detail
	}
	return e.Message
}

// Error wraps an error body into a JSON-RPC 2.0 envelope. id
// may be nil (for parse errors) or any JSON value (for everything
// else).
func Error(id json.RawMessage, code int, msg string, data any) map[string]any {
	body := map[string]any{
		"code":    code,
		"message": msg,
	}
	if data != nil {
		body["data"] = data
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   body,
	}
}

// Result wraps a result value into a JSON-RPC 2.0 envelope.
func Result(id json.RawMessage, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}

// InvalidParams returns a JSON-RPC -32602 envelope. The "data"
// field carries the human-readable reason and is what a dashboard
// renders to its user.
func InvalidParams(msg string) *ErrorBody {
	return &ErrorBody{Code: -32602, Message: "Invalid params", Data: msg}
}

// InternalError returns a JSON-RPC -32603 envelope. The "data"
// field carries the underlying error message so a dashboard can
// log it.
func InternalError(msg string) *ErrorBody {
	return &ErrorBody{Code: -32603, Message: "Internal error", Data: msg}
}

// Custom JSON-RPC error codes reserved for the
// rc-cascade-message surface. The -32000 .. -32099 range is
// the JSON-RPC 2.0 "custom error" space; the brief's
// reservation is:
//
//	-32004 Cascade not found
//	-32005 Cascade closed
//	-32006 Interaction not found       (rc-handle-user-interaction)
//	-32010 Turn timeout
//
// Anything else from the cascade-message handler stays on
// the standard -32600 Invalid Request / -32602 Invalid params
// / -32603 Internal error codes.
//
// The codes live here, next to the envelope they belong to:
// a domain that reported "Cascade not found" under its own
// private number would break every dashboard's error filter.
const (
	ErrCodeCascadeNotFound     = -32004
	ErrCodeCascadeClosed       = -32005
	ErrCodeInteractionNotFound = -32006
	ErrCodeTurnTimeout         = -32010
)

// NonStringFieldInJSON re-reads the raw params object to check
// that a field the parser coerced to "" was either absent or a
// string. The cycle brief's "model must be string" rule is the
// concrete reason this exists: json.Unmarshal silently converts
// a number to "" for a string-typed field, so a dashboard that
// sent `{model: 42}` would otherwise look like "empty model" —
// silent default-walk, not a hard error.
//
// The implementation is a tiny Unmarshal into a map[string]any;
// if the key is present and not a string, we return true (the
// handler will report -32602). Allocations are bounded by the
// size of the params object, which the dispatcher enforces via
// the bufio.Scanner 16 MiB cap at the transport layer.
//
// The guard belongs to the envelope: it is the one rule every
// domain's param decoding shares, and a domain that re-implemented
// it would be a domain that silently accepted a numeric id.
func NonStringFieldInJSON(raw json.RawMessage, field, coerced string) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	v, ok := m[field]
	if !ok {
		return false
	}
	// "absent" means the dashboard didn't include the field at
	// all — that path is fine.
	if string(v) == "null" {
		return coerced != ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return true
	}
	return false
}

// NonNullObjectInJSON reports whether the
// supplied params block has a non-null object at
// field. Mirrors nonStringFieldInJSON's shape but
// is stricter: the field must be present (per the
// dispatcher's "params is JSON object" assumption)
// AND parseable as a JSON object ({...}). A
// missing / null / non-object value returns false.
//
// The dispatcher uses this to enforce the brief's
// "config is a JSON object, not null" rule — a
// dashboard that posts {"conversation_id":"..."}
// without a config key (or with config:null) lands
// as -32602 "config is required" before any
// validation runs.
func NonNullObjectInJSON(raw json.RawMessage, field string) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	v, ok := m[field]
	if !ok {
		return false
	}
	if len(v) == 0 {
		return false
	}
	// "null" → false (treat as absent).
	if string(v) == "null" {
		return false
	}
	// Must parse as a JSON object — that's the
	// brief's "config is a JSON object" rule.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(v, &probe); err != nil {
		return false
	}
	return true
}
