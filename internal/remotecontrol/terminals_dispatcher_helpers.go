// Package remotecontrol — terminals_dispatcher_helpers.go:
// the broadcast / error-mapping / wire-rendering helpers
// shared by the five terminals JSON-RPC handlers. Pulled
// out of terminals_dispatcher.go in cycle
// rc-filesize-internal-remotecontrol so the handler
// files stay focused on the per-method body.
package remotecontrol

import (
	"context"
	"encoding/base64"
	"time"
)

// broadcastTerminalOutput fans a terminal/output
// notification out to every registered transport's
// open connection.
//
// Why a dispatcher-side helper rather than a
// transport-side one: the single Send handler lives
// on the dispatcher, and the broadcaster is the seam a
// transport installs to deliver "frame to every open
// conn" without the dispatcher knowing the concrete
// transport type.
//
// SetBroadcaster is the seam documented in notify.go.
// The remotetransport package wires its own
// broadcaster at TCPServer construction (see the
// rc-permissions-grants cycle commit). Without a
// registered broadcaster the method is a no-op —
// in-process dispatchers and the Unix-socket transport
// have no notion of "every open connection". The
// mutation still landed in the registry; the
// SendRemoteAgentTerminalInput handler echoes
// {ok, terminal_id, bytes_consumed} so polling is
// always possible.
func (d *Dispatcher) broadcastTerminalOutput(terminalID string, data []byte) {
	if d == nil || len(data) == 0 {
		return
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	params := map[string]any{
		"terminal_id": terminalID,
		"seq":         0,
		"data":        encoded,
		"ts":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	// BroadcastNotify is best-effort; failures from the
	// broadcaster are returned but the dispatcher
	// drops them here.
	_ = d.BroadcastNotify(context.Background(), NotifTerminalOutputMethod, params)
}

// broadcastTerminalClosed fans a terminal/closed
// notification out to every open ws connection. Same
// seam as broadcastTerminalOutput — set up by the
// transport's SetBroadcaster at construction.
func (d *Dispatcher) broadcastTerminalClosed(terminalID, closedAt string) {
	if d == nil || terminalID == "" {
		return
	}
	params := map[string]any{
		"terminal_id": terminalID,
		"closed_at":   closedAt,
		"ts":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	_ = d.BroadcastNotify(context.Background(), NotifTerminalClosedMethod, params)
}

// mapTerminalsError maps a registry sentinel to the
// canonical JSON-RPC envelope. Mirrors the
// mapConversationConfigError / mapFastpushError /
// mapPluginsError helpers the previous cycles
// established; -32602 ("Invalid params") is shared with
// every other method's "bad input" envelope and is
// emitted directly from the handler, not via this
// helper.
func mapTerminalsError(err error, op string) *rpcErrorBody {
	if err == nil {
		return nil
	}
	switch err {
	case ErrTerminalNotFound:
		return &rpcErrorBody{Code: -32018, Message: "Terminal not found", Data: ""}
	case ErrTerminalClosed:
		return &rpcErrorBody{Code: -32019, Message: "Terminal closed", Data: ""}
	case ErrTerminalInputRejected:
		return &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "terminal kind is output; Send rejected"}
	}
	return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
}

// terminalToWire renders a Terminal row into the JSON
// map the wire envelope expects. The serializer is a
// manual one (rather than json.Marshal of Terminal)
// because the dispatcher threads seq stamps through
// base64-encoded Data on the broadcast path and we
// want the per-row wire shape to remain stable across
// marshalling changes.
//
// Wire keys (snake_case, brief-pinned): terminal_id,
// cascade_id (omitted when empty), kind, cols, rows,
// created_at (RFC3339Nano UTC), closed_at (RFC3339Nano
// UTC, omitted when zero).
func terminalToWire(t Terminal) map[string]any {
	out := map[string]any{
		"terminal_id": t.TerminalID,
		"kind":        string(t.Kind),
		"cols":        t.Cols,
		"rows":        t.Rows,
		"created_at":  t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if t.CascadeID != "" {
		out["cascade_id"] = t.CascadeID
	}
	if !t.ClosedAt.IsZero() {
		out["closed_at"] = t.ClosedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// closedSetList renders an AllXxx closed-set slice as
// a pipe-joined string suitable for an invalidParams
// "data" field. Mirrors the FastpushToggle / Resource
// / Trigger helpers' "must be one of X|Y|Z" format.
func closedSetList(set []TerminalKind) string {
	if len(set) == 0 {
		return ""
	}
	out := ""
	for i, k := range set {
		if i > 0 {
			out += "|"
		}
		out += string(k)
	}
	return out
}
