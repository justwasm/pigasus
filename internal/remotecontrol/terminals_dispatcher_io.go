// Package remotecontrol — terminals_dispatcher_io.go:
// the Send / Resize / Close JSON-RPC handlers (the
// mutation paths that drive broadcasts). Pulled out of
// terminals_dispatcher.go in cycle
// rc-filesize-internal-remotecontrol.
package remotecontrol

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// handleSendRemoteAgentTerminalInput is the JSON-RPC
// handler for "SendRemoteAgentTerminalInput". Cycle
// rc-terminals.
//
// Param validation:
//
//   - terminal_id: required, non-empty string. Empty /
//     whitespace / non-string → -32602.
//
//   - data: required base64-encoded string. The
//     dispatcher decodes it; a malformed base64 →
//     -32602 "data must be base64" (clear error rather
//     than silent -32603). An empty string is also
//     rejected as -32602 (Send on zero bytes is a
//     dashboard bug — bytes_consumed can't be 0 per
//     the brief's wire shape).
//
// Registry call:
//
//   - ErrTerminalNotFound → -32018.
//   - ErrTerminalClosed → -32019.
//   - ErrTerminalInputRejected (kind=output) → -32602.
//   - other → -32603.
//
// Broadcast on success: the bytes (encoded as base64)
// are routed through (*Dispatcher).BroadcastNotify
// with method "terminal/output", params
// {terminal_id, seq:0, data:<base64>, ts:<rfc3339>}
// (matching the brief's notification shape). The
// originating conn's outbound channel holds
// [notification, response]. The seq is taken from the
// registry's per-terminal counter so subscribers can
// detect drops; ts is the server's wall clock at
// broadcast time.
//
// Response envelope: {ok, terminal_id, bytes_consumed}.
// bytes_consumed mirrors len(decoded data).
func (d *Dispatcher) handleSendRemoteAgentTerminalInput(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.TerminalsRegistry == nil {
		return nil, internalError("no terminals registry installed")
	}
	terminalID, decoded, perr := decodeSendTerminalInputParams(params)
	if perr != nil {
		return nil, perr
	}
	consumed, err := d.cfg.TerminalsRegistry.Send(terminalID, decoded)
	if err != nil {
		return nil, mapTerminalsError(err, "send terminal input")
	}
	// Broadcast the echo as terminal/output to every
	// other open ws connection. The originating conn's
	// outbound channel holds [notification, response]
	// — BroadcastNotify fires before the response is
	// marshalled, so a polling caller sees the bytes
	// consumed value AFTER the broadcast has been
	// dispatched.
	d.broadcastTerminalOutput(terminalID, decoded)
	return map[string]any{
		"ok":             true,
		"terminal_id":    terminalID,
		"bytes_consumed": consumed,
	}, nil
}

// decodeSendTerminalInputParams unmarshals
// SendRemoteAgentTerminalInput params, applies the
// validation chain (non-string terminal_id / data,
// required terminal_id, required + non-empty
// base64-decodable data), and returns the
// (terminal_id, decoded bytes, *rpcErrorBody)
// tuple. Extracted from
// handleSendRemoteAgentTerminalInput to keep the
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeSendTerminalInputParams(params json.RawMessage) (string, []byte, *rpcErrorBody) {
	var p sendRemoteAgentTerminalInputParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return "", nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "terminal_id", p.TerminalID) {
		return "", nil, invalidParams("string fields must be strings")
	}
	if nonStringFieldInJSON(params, "data", p.Data) {
		return "", nil, invalidParams("invalid params: data must be a string")
	}
	terminalID := normalizeTerminalID(p.TerminalID)
	if terminalID == "" {
		return "", nil, invalidParams("terminal_id is required and must be non-empty")
	}
	if p.Data == "" {
		return "", nil, invalidParams("data is required and must be non-empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return "", nil, invalidParams("data must be base64")
	}
	if len(decoded) == 0 {
		return "", nil, invalidParams("data must decode to non-empty bytes")
	}
	return terminalID, decoded, nil
}

// handleResizeRemoteAgentTerminal is the JSON-RPC
// handler for "ResizeRemoteAgentTerminal". Cycle
// rc-terminals.
//
// Param validation:
//
//   - terminal_id: required, non-empty string.
//
//   - cols / rows: zero is acceptable (no-op on a
//     per-field basis); negative is rejected as -32602
//     (the dashboard's "must be >= 0" rule). Both zero
//     is also a no-op success (the registry treats it
//     as "leave the dims alone").
//
// Registry call: same map as Send (without
// ErrTerminalInputRejected — Resize doesn't care about
// kind).
//
// Response envelope: {ok, terminal_id, cols, rows}
// echoing the post-resize dimensions. No broadcast
// (the brief pins Resize as a no-broadcast mutation;
// the cascade/terminal state stream already has the
// updated dims visible via the next List poll).
func (d *Dispatcher) handleResizeRemoteAgentTerminal(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.TerminalsRegistry == nil {
		return nil, internalError("no terminals registry installed")
	}
	var p resizeRemoteAgentTerminalParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "terminal_id", p.TerminalID) {
		return nil, invalidParams("string fields must be strings")
	}
	terminalID := normalizeTerminalID(p.TerminalID)
	if terminalID == "" {
		return nil, invalidParams("terminal_id is required and must be non-empty")
	}
	if p.Cols < 0 {
		return nil, invalidParams("cols must be >= 0")
	}
	if p.Rows < 0 {
		return nil, invalidParams("rows must be >= 0")
	}

	err := d.cfg.TerminalsRegistry.Resize(terminalID, p.Cols, p.Rows)
	if err != nil {
		return nil, mapTerminalsError(err, "resize terminal")
	}

	// Read the post-resize state so the response
	// envelope echoes the post-state verbatim (the dims
	// the registry actually stores, which match what
	// the next List will return).
	post, _ := d.cfg.TerminalsRegistry.Get(terminalID)
	return map[string]any{
		"ok":          true,
		"terminal_id": terminalID,
		"cols":        post.Cols,
		"rows":        post.Rows,
	}, nil
}

// handleCloseRemoteAgentTerminal is the JSON-RPC handler
// for "CloseRemoteAgentTerminal". Cycle rc-terminals.
//
// Param validation:
//
//   - terminal_id: required, non-empty string.
//
// Registry call:
//
//   - ErrTerminalNotFound → -32018 (terminal was never
//     created, or already-closed → the registry returns
//     ErrTerminalClosed). The dispatcher surfaces -32019
//     for already-closed (matches the project surface).
//   - ErrTerminalClosed → -32019.
//   - other → -32603.
//
// Broadcast on success: a terminal/closed notification
// {terminal_id, closed_at, ts} fans out to every open
// ws conn via (*Dispatcher).BroadcastNotify. Same
// precedent as the Send / Create paths — broadcast
// before the response is marshalled.
//
// Response envelope: {ok, terminal_id, closed_at}. The
// closed_at stamp is whatever the registry applied at
// Close time (server clock).
func (d *Dispatcher) handleCloseRemoteAgentTerminal(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.TerminalsRegistry == nil {
		return nil, internalError("no terminals registry installed")
	}
	var p closeRemoteAgentTerminalParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "terminal_id", p.TerminalID) {
		return nil, invalidParams("string fields must be strings")
	}
	terminalID := normalizeTerminalID(p.TerminalID)
	if terminalID == "" {
		return nil, invalidParams("terminal_id is required and must be non-empty")
	}

	err := d.cfg.TerminalsRegistry.Close(terminalID)
	if err != nil {
		return nil, mapTerminalsError(err, "close terminal")
	}

	closedAt := time.Now().UTC().Format(time.RFC3339Nano)
	d.broadcastTerminalClosed(terminalID, closedAt)

	return map[string]any{
		"ok":          true,
		"terminal_id": terminalID,
		"closed_at":   closedAt,
	}, nil
}
