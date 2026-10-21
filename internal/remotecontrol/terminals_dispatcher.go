// Package remotecontrol — terminals_dispatcher.go: the
// per-method JSON-RPC handlers for the five
// RemoteAgentTerminal* methods introduced in cycle
// rc-terminals.
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the handlers share the TerminalsRegistry
// seam + a small sentinel-error map
// (ErrTerminalNotFound → -32018 "Terminal not found",
// ErrTerminalClosed → -32019 "Terminal closed",
// ErrTerminalInputRejected → -32602 "Invalid params"),
// and Send fan-out requires both the registry's
// subscriber fan-out AND a cross-ws BroadcastNotify
// (the multi-conn fan-out helper added in
// rc-permissions-grants). Putting the handlers here,
// next to the type definitions (terminals.go) and the
// registry (terminals_registry.go), keeps the
// per-method bodies focused on the JSON-RPC envelope
// shape rather than the dispatcher's switch table.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - terminals_dispatcher.go         — package doc +
//     params types + handleCreate + handleList +
//     validateCreateInput / createTerminal /
//     attachCascadeOutput helpers
//   - terminals_dispatcher_io.go      — handleSend +
//     handleResize + handleClose
//   - terminals_dispatcher_helpers.go — broadcastTerminalOutput /
//     broadcastTerminalClosed /
//     mapTerminalsError / terminalToWire /
//     closedSetList
//
// Sentinel map:
//
//	ErrTerminalNotFound       → -32018 "Terminal not found"
//	ErrTerminalClosed         → -32019 "Terminal closed"
//	ErrTerminalInputRejected  → -32602 "Invalid params"
//	(other / I/O)             → -32603 "Internal error"
//	empty / non-string id
//	                         → -32602 "Invalid params"
//	bogus base64 data
//	                         → -32602 "Invalid params"
//	unknown kind
//	                         → -32602 "Invalid params"
//
// Cycle rc-terminals also adds the "broadcast on
// change" path:
//
//   - On every successful SendRemoteAgentTerminalInput,
//     the dispatcher routes the bytes through
//     (*Dispatcher).BroadcastNotify with method
//     "terminal/output" so the bytes echo to every
//     open ws connection (not just the per-terminal
//     subscribers the registry manages). The same
//     pipeline also handles cascade output → terminals
//     via (*TerminalsRegistry).AttachCascade (the
//     dispatcher's Create path attaches the cascade's
//     TerminalOutput chan when the dashboard asked
//     for a cascade-bound terminal).
//
//   - On every successful CloseRemoteAgentTerminal,
//     the dispatcher routes {terminal_id, closed_at}
//     through BroadcastNotify with method
//     "terminal/closed" so dashboards on other conns
//     see a terminal close they didn't initiate.
//
// The (registered) broadcaster is the seam
// documented in notify.go. Without a registered
// broadcaster the broadcast becomes a no-op —
// in-process dispatchers and the Unix-socket
// transport have no notion of "every open
// connection". Mutations still landed in the registry
// and the response envelope echoes the post-state; a
// polling caller is always up-to-date.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"encoding/json"
	"time"
)

// createRemoteAgentTerminalParams is the JSON shape the
// dashboard posts for CreateRemoteAgentTerminal.
// terminal_id is server-minted; cascade_id is optional
// (empty → the terminal is unattached). cols / rows
// default to 80/24 when zero or absent.
//
// Missing / out-of-set kind → -32602 (the closed-set
// IsValidTerminalKind validator). Empty / non-string
// cascade_id is allowed (means "no cascade"). Unknown
// keys are silently dropped (a future cycle that wants
// strict closed-set validation adds it; today's wire is
// permissive to keep parity with `agy`).
type createRemoteAgentTerminalParams struct {
	CascadeID string       `json:"cascade_id"`
	Cols      int          `json:"cols"`
	Rows      int          `json:"rows"`
	Kind      TerminalKind `json:"kind"`
}

// sendRemoteAgentTerminalInputParams is the JSON shape
// the dashboard posts for SendRemoteAgentTerminalInput.
// terminal_id is required; data is required, base64-
// encoded UTF-8 bytes. The handler decodes data and
// forwards the raw bytes to the registry's Send path.
//
// Bytes_consumed in the response envelope mirrors
// len(decoded data). The "data must be base64" rule is
// pinned as -32602 ("Invalid params") — dashboards
// that send plain UTF-8 land as a clear bad-params
// envelope rather than the silent -32603 some
// clones return.
type sendRemoteAgentTerminalInputParams struct {
	TerminalID string `json:"terminal_id"`
	Data       string `json:"data"`
}

// resizeRemoteAgentTerminalParams is the JSON shape
// the dashboard posts for ResizeRemoteAgentTerminal.
// terminal_id is required; cols / rows are
// required-positive integers (zero / negative lands as
// -32602 — the registry's Resize treats them as a
// no-op, but the dispatcher guards earlier so
// dashboards get a clear error).
type resizeRemoteAgentTerminalParams struct {
	TerminalID string `json:"terminal_id"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
}

// closeRemoteAgentTerminalParams is the JSON shape the
// dashboard posts for CloseRemoteAgentTerminal. Just
// terminal_id — the close timestamp is server-stamped
// at close time.
type closeRemoteAgentTerminalParams struct {
	TerminalID string `json:"terminal_id"`
}

// handleCreateRemoteAgentTerminal is the JSON-RPC
// handler for "CreateRemoteAgentTerminal". Cycle
// rc-terminals.
//
// Param validation:
//
//   - cascade_id: optional. Empty / absent is fine —
//     means "no cascade binding". Non-string → -32602
//     (the "dashboard bug" envelope, via
//     nonStringFieldInJSON).
//
//   - cols: required-positive for the wire envelope to
//     be sensible; the registry applies 80 default when
//     cols <= 0, so a missing / zero cols lands
//     cleanly (the dispatcher doesn't pin cols > 0).
//     A negative cols value, however, is a definite
//     dashboard bug — flags as -32602 here so the
//     dashboard gets a clear error rather than a
//     silent 80 default.
//
//   - rows: same rule as cols.
//
//   - kind: required, closed-set
//     {"output","input"}. Empty / unknown → -32602 via
//     IsValidTerminalKind.
//
// Cascade existence: if cascade_id is non-empty,
// (*Dispatcher).cascades.Get validates it before the
// registry call. An unknown cascade_id maps to -32004
// "Cascade not found" (the same envelope
// StartRemoteAgentCascade / SendMessage surfaces, so
// dashboards' error filters are uniform).
//
// Registry call: cfg.TerminalsRegistry == nil → -32603
// (matches the project surface for "no store
// installed"). Other errors → -32603.
//
// On success the handler returns the canonical
// Terminal record (with server-stamped created_at +
// freshly-minted terminal_id). No broadcast on
// Create — the brief pins Create as the moment a
// terminal "appears", and subscribers only learn
// about it via the terminal/output stream once they
// explicitly Subscribe. ListRemoteAgentTerminals is
// the polling path.
func (d *Dispatcher) handleCreateRemoteAgentTerminal(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.TerminalsRegistry == nil {
		return nil, internalError("no terminals registry installed")
	}
	var p createRemoteAgentTerminalParams
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if rerr := validateCreateTerminalInput(params, p); rerr != nil {
		return nil, rerr
	}
	cascadeID := normalizeTerminalID(p.CascadeID)
	if cascadeID != "" {
		if d.cascades == nil {
			return nil, internalError("registry not initialised")
		}
		if _, ok := d.cascades.Get(cascadeID); !ok {
			return nil, &rpcErrorBody{Code: -32004, Message: "Cascade not found", Data: cascadeID}
		}
	}

	t, rerr := d.createTerminal(cascadeID, p)
	if rerr != nil {
		return nil, rerr
	}
	d.attachCascadeOutput(cascadeID, t.TerminalID)

	return terminalCreateResponse(t), nil
}

// validateCreateTerminalInput runs the closed-set + range
// checks the Create handler applies to the
// dashboard-supplied params. Pulled out so the
// handleCreate body stays compact and the per-check
// logic is independently testable.
//
// Returns nil on success or a non-nil *rpcErrorBody the
// caller surfaces verbatim.
func validateCreateTerminalInput(params json.RawMessage, p createRemoteAgentTerminalParams) *rpcErrorBody {
	// cascade_id is optional — non-string numeric /
	// object would be a dashboard bug (mirrors
	// rc-triggers / rc-resources validation).
	if nonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return invalidParams("string fields must be strings")
	}
	// cols / rows: reject negative explicitly. The
	// registry's Create defaults 0 → 80/24 which is a
	// reasonable "no override" semantic; negative is
	// just buggy.
	if p.Cols < 0 {
		return invalidParams("cols must be >= 0")
	}
	if p.Rows < 0 {
		return invalidParams("rows must be >= 0")
	}
	// kind: closed-set.
	if !IsValidTerminalKind(p.Kind) {
		return invalidParams("kind must be one of " + closedSetList(AllTerminalKinds))
	}
	return nil
}

// createTerminal is the registry-call half of
// handleCreateRemoteAgentTerminal. Pulled out so the
// handler body stays compact and the cascade-attach +
// response-shaping helpers can reuse the registry's
// returned Terminal record without a second struct.
//
// On any registry error returns mapTerminalsError
// (no store / I/O → -32603). On success returns the
// server-stamped Terminal record.
func (d *Dispatcher) createTerminal(cascadeID string, p createRemoteAgentTerminalParams) (Terminal, *rpcErrorBody) {
	t, err := d.cfg.TerminalsRegistry.Create(Terminal{
		CascadeID: cascadeID,
		Kind:      p.Kind,
		Cols:      p.Cols,
		Rows:      p.Rows,
	})
	if err != nil {
		return Terminal{}, mapTerminalsError(err, "create terminal")
	}
	return t, nil
}

// attachCascadeOutput wires the cascade's
// TerminalOutput chan into the freshly-created
// terminal's subscriber fan-out. No-op when
// cascadeID is empty (no binding) or when the cascade
// has no TerminalOutput chan (a Send-only cascade).
// Errors from AttachCascade are dropped — the cascade
// binding is best-effort and the response envelope
// already echoes the created terminal.
func (d *Dispatcher) attachCascadeOutput(cascadeID, terminalID string) {
	if cascadeID == "" {
		return
	}
	if d.cascades == nil {
		return
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok || c == nil {
		return
	}
	ch := c.TerminalOutput()
	if ch == nil {
		return
	}
	_ = d.cfg.TerminalsRegistry.AttachCascade(terminalID, ch)
}

// terminalCreateResponse renders the brief-pinned
// flat response shape: {ok, terminal_id, kind, cols,
// rows, created_at}. Matches the project surface
// convention (other Create-style responses are
// similarly lean: short headers without a top-level
// "terminal" object).
func terminalCreateResponse(t Terminal) map[string]any {
	return map[string]any{
		"ok":          true,
		"terminal_id": t.TerminalID,
		"kind":        string(t.Kind),
		"cols":        t.Cols,
		"rows":        t.Rows,
		"created_at":  t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// handleListRemoteAgentTerminals is the JSON-RPC
// handler for "ListRemoteAgentTerminals". Cycle
// rc-terminals.
//
// Params: {cascade_id?: "" | "<id>"} (optional filter).
// An empty / missing cascade_id means "every terminal"
// (the no-filter branch); a non-empty cascade_id means
// "terminals whose CascadeID matches". Unknown
// cascade_ids return an empty list — the brief's
// "filter is best-effort" semantic (mirrors the
// rc-triggers / rc-resources precedent).
//
// Nil registry → -32603 (matches the project surface).
// A non-numeric cascade_id lands as -32602 (mirrors the
// other methods' "string fields must be strings"
// envelope).
//
// On success: {ok, terminals:[{…}, …]}. The list is
// sorted by created_at ascending, terminal_id
// tiebreaker — matches the registry's internal sort
// order. No broadcast.
func (d *Dispatcher) handleListRemoteAgentTerminals(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.TerminalsRegistry == nil {
		return nil, internalError("no terminals registry installed")
	}
	var p struct {
		CascadeID string `json:"cascade_id"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return nil, invalidParams("string fields must be strings")
	}
	cascadeID := normalizeTerminalID(p.CascadeID)
	rows := d.cfg.TerminalsRegistry.List(cascadeID)

	terminals := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		terminals = append(terminals, terminalToWire(t))
	}
	return map[string]any{
		"ok":        true,
		"terminals": terminals,
	}, nil
}
