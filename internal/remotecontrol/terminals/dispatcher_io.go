// Package terminals — dispatcher_io.go: the Send / Resize /
// Close JSON-RPC handlers (the mutation paths that drive
// broadcasts). Pulled out of dispatcher.go in cycle
// rc-filesize-internal-remotecontrol.
package terminals

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// HandleSend is the JSON-RPC handler for
// "SendRemoteAgentTerminalInput". Cycle rc-terminals.
//
// Param validation:
//
//   - terminal_id: required, non-empty string. Empty /
//     whitespace / non-string → -32602.
//
//   - data: required base64-encoded string. The handler decodes
//     it; a malformed base64 → -32602 "data must be base64"
//     (clear error rather than silent -32603). An empty string
//     is also rejected as -32602 (Send on zero bytes is a
//     dashboard bug — bytes_consumed can't be 0 per the brief's
//     wire shape).
//
// Store call:
//
//   - ErrNotFound → -32018.
//   - ErrClosed → -32019.
//   - ErrInputRejected (kind=output) → -32602.
//   - other → -32603.
//
// Broadcast on success: the bytes (encoded as base64) are routed
// through Seams.Broadcast with method "terminal/output", params
// {terminal_id, seq:0, data:<base64>, ts:<rfc3339>} (matching
// the brief's notification shape). The originating connection's
// outbound channel holds [notification, response]. The seq is
// taken from the registry's per-terminal counter so subscribers
// can detect drops; ts is the server's wall clock at broadcast
// time.
//
// Response envelope: {ok, terminal_id, bytes_consumed}.
// bytes_consumed mirrors len(decoded data).
func HandleSend(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no terminals registry installed")
	}
	terminalID, decoded, perr := decodeSendParams(params)
	if perr != nil {
		return nil, perr
	}
	consumed, err := s.Store.Send(terminalID, decoded)
	if err != nil {
		return nil, MapError(err, "send terminal input")
	}
	// Broadcast the echo as terminal/output to every other
	// open connection. The originating connection's outbound
	// channel holds [notification, response] — the broadcast
	// fires before the response is marshalled, so a polling
	// caller sees the bytes consumed value AFTER the broadcast
	// has been dispatched.
	broadcastOutput(s, terminalID, decoded)
	return map[string]any{
		"ok":             true,
		"terminal_id":    terminalID,
		"bytes_consumed": consumed,
	}, nil
}

// decodeSendParams unmarshals SendRemoteAgentTerminalInput
// params, applies the validation chain (non-string terminal_id
// / data, required terminal_id, required + non-empty
// base64-decodable data), and returns the (terminal_id, decoded
// bytes, *rpc.ErrorBody) tuple. Extracted from HandleSend to
// keep the handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeSendParams(params json.RawMessage) (string, []byte, *rpc.ErrorBody) {
	var p sendParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return "", nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "terminal_id", p.TerminalID) {
		return "", nil, rpc.InvalidParams("string fields must be strings")
	}
	if rpc.NonStringFieldInJSON(params, "data", p.Data) {
		return "", nil, rpc.InvalidParams("invalid params: data must be a string")
	}
	terminalID := normalizeID(p.TerminalID)
	if terminalID == "" {
		return "", nil, rpc.InvalidParams("terminal_id is required and must be non-empty")
	}
	if p.Data == "" {
		return "", nil, rpc.InvalidParams("data is required and must be non-empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return "", nil, rpc.InvalidParams("data must be base64")
	}
	if len(decoded) == 0 {
		return "", nil, rpc.InvalidParams("data must decode to non-empty bytes")
	}
	return terminalID, decoded, nil
}

// HandleResize is the JSON-RPC handler for
// "ResizeRemoteAgentTerminal". Cycle rc-terminals.
//
// Param validation:
//
//   - terminal_id: required, non-empty string.
//
//   - cols / rows: zero is acceptable (no-op on a per-field
//     basis); negative is rejected as -32602 (the dashboard's
//     "must be >= 0" rule). Both zero is also a no-op success
//     (the registry treats it as "leave the dims alone").
//
// Store call: same map as Send (without ErrInputRejected —
// Resize doesn't care about kind).
//
// Response envelope: {ok, terminal_id, cols, rows} echoing the
// post-resize dimensions. No broadcast (the brief pins Resize as
// a no-broadcast mutation; the cascade/terminal state stream
// already has the updated dims visible via the next List poll).
func HandleResize(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no terminals registry installed")
	}
	var p resizeParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "terminal_id", p.TerminalID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	terminalID := normalizeID(p.TerminalID)
	if terminalID == "" {
		return nil, rpc.InvalidParams("terminal_id is required and must be non-empty")
	}
	if p.Cols < 0 {
		return nil, rpc.InvalidParams("cols must be >= 0")
	}
	if p.Rows < 0 {
		return nil, rpc.InvalidParams("rows must be >= 0")
	}

	err := s.Store.Resize(terminalID, p.Cols, p.Rows)
	if err != nil {
		return nil, MapError(err, "resize terminal")
	}

	// Read the post-resize state so the response envelope
	// echoes the post-state verbatim (the dims the registry
	// actually stores, which match what the next List will
	// return).
	post, _ := s.Store.Get(terminalID)
	return map[string]any{
		"ok":          true,
		"terminal_id": terminalID,
		"cols":        post.Cols,
		"rows":        post.Rows,
	}, nil
}

// HandleClose is the JSON-RPC handler for
// "CloseRemoteAgentTerminal". Cycle rc-terminals.
//
// Param validation:
//
//   - terminal_id: required, non-empty string.
//
// Store call:
//
//   - ErrNotFound → -32018 (terminal was never created, or
//     already-closed → the registry returns ErrClosed). The
//     handler surfaces -32019 for already-closed (matches the
//     project surface).
//   - ErrClosed → -32019.
//   - other → -32603.
//
// Broadcast on success: a terminal/closed notification
// {terminal_id, closed_at, ts} fans out to every open connection
// via Seams.Broadcast. Same precedent as the Send / Create paths
// — broadcast before the response is marshalled.
//
// Response envelope: {ok, terminal_id, closed_at}. The closed_at
// stamp is whatever the registry applied at Close time (server
// clock).
func HandleClose(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no terminals registry installed")
	}
	var p closeParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "terminal_id", p.TerminalID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	terminalID := normalizeID(p.TerminalID)
	if terminalID == "" {
		return nil, rpc.InvalidParams("terminal_id is required and must be non-empty")
	}

	err := s.Store.Close(terminalID)
	if err != nil {
		return nil, MapError(err, "close terminal")
	}

	closedAt := time.Now().UTC().Format(time.RFC3339Nano)
	broadcastClosed(s, terminalID, closedAt)

	return map[string]any{
		"ok":          true,
		"terminal_id": terminalID,
		"closed_at":   closedAt,
	}, nil
}
