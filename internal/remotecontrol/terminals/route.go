// Package terminals — route.go: the domain's own routing table.
//
// Every domain package exposes a Route function with the same
// shape: the host's dispatcher calls it with the method name and
// the raw params, and it answers (result, error, handled). The
// third value is false when the method is not the domain's — the
// dispatcher then tries the next domain.
//
// The shape is deliberate: the dispatcher keeps the fan-out, the
// domain keeps the table. A domain that owned the fan-out would
// own every other domain's method names too.
//
// The terminals domain serves one protocol — the JSON-RPC
// dashboard surface. The Web UI's own terminal surface
// (CreateTerminal / ListTerminals / …) is a *different* contract
// with different member names, and it lives in the host's
// language-server surface.
package terminals

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the terminals domain owns.
const (
	// MethodCreate creates a terminal.
	MethodCreate = "CreateTerminal"
	// MethodList lists the terminals (optionally filtered by
	// cascade id).
	MethodList = "ListTerminals"
	// MethodSend submits input bytes to an input-kind terminal.
	MethodSend = "SendTerminalInput"
	// MethodResize changes a terminal's dimensions.
	MethodResize = "ResizeTerminal"
	// MethodClose closes a terminal.
	MethodClose = "CloseTerminal"

	// Legacy aliases kept for backward compatibility with early dashboard callers.
	LegacyMethodCreate = "CreateRemoteAgentTerminal"
	LegacyMethodList   = "ListRemoteAgentTerminals"
	LegacyMethodSend   = "SendRemoteAgentTerminalInput"
	LegacyMethodResize = "ResizeRemoteAgentTerminal"
	LegacyMethodClose  = "CloseRemoteAgentTerminal"
)

// Route dispatches the terminals domain's JSON-RPC method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodCreate, LegacyMethodCreate:
		r, e := HandleCreate(s, params)
		return r, e, true
	case MethodList, LegacyMethodList:
		r, e := HandleList(s, params)
		return r, e, true
	case MethodSend, LegacyMethodSend:
		r, e := HandleSend(s, params)
		return r, e, true
	case MethodResize, LegacyMethodResize:
		r, e := HandleResize(s, params)
		return r, e, true
	case MethodClose, LegacyMethodClose:
		r, e := HandleClose(s, params)
		return r, e, true
	}
	return nil, nil, false
}
