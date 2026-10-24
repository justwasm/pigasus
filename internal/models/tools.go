// Package models — tools.go
//
// Cycle 274 added function-calling to the provider layer so the agy wrapper
// (internal/agyproxy) can round-trip Gemini `functionCall` parts through an
// OpenAI-shaped endpoint. Everything here is additive: a provider that never
// populates these fields behaves exactly as it did before.
//
// The shapes deliberately mirror the OpenAI wire (a tool is
// {name, description, JSON-Schema}; a call is {id, name, arguments-string})
// because every provider we ship except Anthropic speaks that dialect. The
// proxy is responsible for translating to/from Gemini's
// {functionDeclarations} / {functionCall, functionResponse} form.

package models

import "encoding/json"

// ToolSpec is one callable function offered to the model. Parameters is a raw
// JSON Schema object (not a string) so it can be forwarded verbatim; an empty
// slice means "no parameters".
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is one function call produced by the model. Arguments is the raw
// JSON string exactly as the provider emitted it (OpenAI streams it as a
// string, so keeping it undecoded avoids losing precision or ordering).
type ToolCall struct {
	// ID is the provider-assigned call id. Some gateways omit it; callers
	// should synthesise one (agyproxy uses "call_<name>").
	ID string
	// Name is the function name.
	Name string
	// Arguments is a JSON object encoded as a string, e.g. `{"q":"x"}`.
	Arguments string
}
