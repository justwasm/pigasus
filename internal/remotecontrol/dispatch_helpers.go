// Package remotecontrol — dispatch_helpers.go: the shared helpers
// the per-handler dispatch_*.go files reuse. Pure file split;
// every helper here was previously inline in dispatch.go.
//
// The helpers fall into four buckets:
//
//   - JSON-RPC envelope builders (rpcError, rpcResult) + the
//     rpcErrorBody struct they wrap.
//   - Param-shape guards. nonStringFieldInJSON re-reads the raw
//     params object so a numeric "model" can't silently
//     default-walk to the cascade's pinned value. firstNonEmpty
//     collapses per-turn overrides against cascade-pinned values.
//   - Time / formatting helpers used by status tool + handler
//     envelopes (timeFormat, formatUptime, formatSeconds).
//   - Status-tool renderers (callStatus, callListAgents,
//     callGetOutput, richStatusSnapshot). Kept here because they
//     don't fit any one handler file.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// rpcErrorBody is the structured form we return from handlers; the
// wire envelope is built by rpcError().
type rpcErrorBody struct {
	Code    int
	Message string
	Data    any
}

// rpcError wraps an error body into a JSON-RPC 2.0 envelope. id
// may be nil (for parse errors) or any JSON value (for everything
// else).
func rpcError(id json.RawMessage, code int, msg string, data any) map[string]any {
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

// rpcResult wraps a result value into a JSON-RPC 2.0 envelope.
func rpcResult(id json.RawMessage, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}

// nonStringFieldInJSON re-reads the raw params object to check
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
func nonStringFieldInJSON(raw json.RawMessage, field, coerced string) bool {
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

// firstNonEmpty returns the first of a, b that is non-empty.
// Used to collapse per-turn overrides against cascade-pinned
// values without recomputing the cascade's resolved value here
// (Start-time resolution already happened in
// handleStartRemoteAgentCascade).
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// timeFormat is the response-shape RFC3339 timestamp formatter
// the StartRemoteAgentCascade response uses. Pulled into a
// helper so it can be replaced per-cycle without grepping the
// handler body.
func timeFormat(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// formatUptime renders a Duration as a short human-readable string
// for the rich status tool. Example outputs: "0s", "12s", "1m03s",
// "1h23m", "2h05m", "25h00m". The format is intentionally compact —
// a dashboard's status pill doesn't need a verbose breakdown.
//
// Mirrors the formatUptime helper in
// internal/persistentrc/helpers.go (cycle 11). The two helpers
// differ slightly because the persistent helper is bound to
// time.Duration in seconds (the persistentrc layer works with
// int64 second counts from time.Time); richStatusSnapshot's
// helper takes a time.Duration directly. The two will be merged
// in a future cycle if more callers want it.
func formatUptime(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	totalSec := int64(d.Round(time.Second).Seconds())
	if totalSec < 60 {
		return formatSeconds(totalSec)
	}
	if totalSec < 3600 {
		m := totalSec / 60
		s := totalSec % 60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	if h < 24 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	// Long-running daemons (24h+): drop the minutes, leave the
	// days-equivalent hour count so the format stays compact.
	return fmt.Sprintf("%dh", h)
}

// formatSeconds renders a sub-minute count as "Ns" or "0s";
// allocated as a string only when needed.
func formatSeconds(s int64) string {
	if s <= 0 {
		return "0s"
	}
	return fmt.Sprintf("%ds", s)
}

// callStatus renders the status tool. Output is a JSON object so
// dashboards can parse without a separate schema.
//
// Cycle rc-status-tool: the shape is replaced with the rich
// envelope the brief pins (see richStatusSnapshot for the full
// shape). The SessionState-only fields come from the existing
// SessionState.statusSnapshot path; the new fields come from the
// dispatcher's optional AuthState + DaemonMeta + Cascades(). All
// writes are non-blocking (a torn read across fields is tolerable
// the same way the cycle-1 status tolerated it).
func (d *Dispatcher) callStatus() (string, bool) {
	snap := d.richStatusSnapshot()
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Sprintf("internal error: %v", err), true
	}
	return string(b), false
}

// callListAgents renders the list_agents tool. Same shape as
// status: a JSON object with an "agents" array, indent-formatted.
func (d *Dispatcher) callListAgents() (string, bool) {
	agents := d.cfg.Agents.ListAgents()
	if agents == nil {
		agents = []AgentInfo{}
	}
	b, err := json.MarshalIndent(map[string]any{"agents": agents}, "", "  ")
	if err != nil {
		return fmt.Sprintf("internal error: %v", err), true
	}
	return string(b), false
}

// callGetOutput renders the get_output tool. The arguments
// schema is {"conversation_id": "..."}. When the caller omits
// the id we fall back to the host's currently-running conversation
// (SessionState.ConversationID); when that's also empty we
// return ErrNotFound as a clean message.
//
// Two read paths:
//
//  1. SessionState.TurnOutput — what the host's chunk handler has
//     pushed for the host's currently-running conversation. Always
//     preferred when the caller asks for the host conversation
//     (no explicit conversation_id), because that is what a
//     dashboard wants to see in real time.
//
//  2. Config.Reader — the convstore-backed reader, used when the
//     caller asks for a historical conversation. Lets the
//     dashboard inspect any past turn, not just the live one.
func (d *Dispatcher) callGetOutput(ctx context.Context, args json.RawMessage) (string, bool) {
	var p struct {
		ConversationID string `json:"conversation_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), true
		}
	}
	id := strings.TrimSpace(p.ConversationID)
	hostConv := d.cfg.Session.TurnOutputSnapshotHostConv()
	if id == "" {
		// Fall back to the host's currently-running conversation.
		id = hostConv
	}
	if id == "" {
		return "conversation id is required (no host conversation is running either)", true
	}
	var out string
	var err error
	if id == hostConv && d.cfg.Session.HasTurnOutput() {
		// Live path: prefer the in-memory snapshot. This is what
		// the host's chunk handler just pushed; it's strictly
		// fresher than any disk-backed read.
		out = d.cfg.Session.TurnOutputSnapshot()
	} else {
		out, err = d.cfg.Reader.ReadTurn(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Sprintf("conversation %q not found", id), true
			}
			return fmt.Sprintf("read error: %v", err), true
		}
	}
	// Empty output is valid (a print-mode run that finished with
	// no model reply). Surface it as an empty JSON object so the
	// dashboard can distinguish "no output" from "not found".
	b, err := json.MarshalIndent(map[string]any{
		"conversation_id": id,
		"output":          out,
	}, "", "  ")
	if err != nil {
		return fmt.Sprintf("internal error: %v", err), true
	}
	return string(b), false
}
