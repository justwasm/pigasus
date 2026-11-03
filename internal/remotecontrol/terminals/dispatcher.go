// Package terminals — dispatcher.go: the per-method JSON-RPC
// handlers for the five RemoteAgentTerminal* methods introduced
// in cycle rc-terminals.
//
// Why a dedicated file (not another switch arm in the host's
// dispatch.go): the handlers share the Store seam + a small
// sentinel-error map (ErrNotFound → -32018 "Terminal not found",
// ErrClosed → -32019 "Terminal closed", ErrInputRejected →
// -32602 "Invalid params"), and the Send fan-out requires both
// the registry's subscriber fan-out AND a cross-connection
// broadcast (the multi-connection fan-out helper added in
// rc-permissions-grants). Putting the handlers here, next to the
// type definitions (terminals.go) and the registry (registry.go),
// keeps the per-method bodies focused on the JSON-RPC envelope
// shape rather than the host's switch table.
//
// File layout:
//
//   - dispatcher.go        — package doc + params types +
//     HandleCreate + HandleList
//   - dispatcher_io.go     — HandleSend + HandleResize +
//     HandleClose
//   - dispatcher_helpers.go — broadcastOutput / broadcastClosed /
//     MapError / toWire / closedSetList
//   - route.go             — the domain's own routing table
//   - seams.go             — what the host hands the domain
//
// Sentinel map:
//
//	ErrNotFound       → -32018 "Terminal not found"
//	ErrClosed         → -32019 "Terminal closed"
//	ErrInputRejected  → -32602 "Invalid params"
//	(other / I/O)     → -32603 "Internal error"
//	empty / non-string id
//	                   → -32602 "Invalid params"
//	bogus base64 data
//	                   → -32602 "Invalid params"
//	unknown kind
//	                   → -32602 "Invalid params"
//
// Cycle rc-terminals also adds the "broadcast on change" path:
//
//   - On every successful SendRemoteAgentTerminalInput, the
//     handler routes the bytes through Seams.Broadcast with
//     method "terminal/output" so the bytes echo to every open
//     connection (not just the per-terminal subscribers the
//     registry manages). The same pipeline also handles cascade
//     output → terminals via the registry's AttachCascade (the
//     Create path attaches the cascade's output chan when the
//     dashboard asked for a cascade-bound terminal).
//
//   - On every successful CloseRemoteAgentTerminal, the handler
//     routes {terminal_id, closed_at} through Seams.Broadcast
//     with method "terminal/closed" so dashboards on other
//     connections see a terminal close they didn't initiate.
//
// The broadcaster is a seam the host installs. Without one the
// broadcast becomes a no-op — in-process dispatchers and the
// Unix-socket transport have no notion of "every open
// connection". Mutations still landed in the registry and the
// response envelope echoes the post-state; a polling caller is
// always up-to-date.
//
// Stdlib-only. No third-party deps.
package terminals

import (
	"encoding/json"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// createParams is the JSON shape the dashboard posts for
// CreateRemoteAgentTerminal. terminal_id is server-minted;
// cascade_id is optional (empty → the terminal is unattached).
// cols / rows default to 80/24 when zero or absent.
//
// Missing / out-of-set kind → -32602 (the closed-set IsValidKind
// validator). Empty / non-string cascade_id is allowed (means "no
// cascade"). Unknown keys are silently dropped (a future cycle
// that wants strict closed-set validation adds it; today's wire
// is permissive to keep parity with `agy`).
type createParams struct {
	CascadeID string `json:"cascade_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	Kind      Kind   `json:"kind"`
}

// sendParams is the JSON shape the dashboard posts for
// SendRemoteAgentTerminalInput. terminal_id is required; data is
// required, base64-encoded UTF-8 bytes. The handler decodes data
// and forwards the raw bytes to the registry's Send path.
//
// Bytes_consumed in the response envelope mirrors len(decoded
// data). The "data must be base64" rule is pinned as -32602
// ("Invalid params") — dashboards that send plain UTF-8 land as
// a clear bad-params envelope rather than the silent -32603 some
// clones return.
type sendParams struct {
	TerminalID string `json:"terminal_id"`
	Data       string `json:"data"`
}

// resizeParams is the JSON shape the dashboard posts for
// ResizeRemoteAgentTerminal. terminal_id is required; cols / rows
// are required-positive integers (zero / negative lands as
// -32602 — the registry's Resize treats them as a no-op, but the
// handler guards earlier so dashboards get a clear error).
type resizeParams struct {
	TerminalID string `json:"terminal_id"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
}

// closeParams is the JSON shape the dashboard posts for
// CloseRemoteAgentTerminal. Just terminal_id — the close
// timestamp is server-stamped at close time.
type closeParams struct {
	TerminalID string `json:"terminal_id"`
}

// HandleCreate is the JSON-RPC handler for
// "CreateRemoteAgentTerminal". Cycle rc-terminals.
//
// Param validation:
//
//   - cascade_id: optional. Empty / absent is fine — means "no
//     cascade binding". Non-string → -32602 (the "dashboard bug"
//     envelope, via rpc.NonStringFieldInJSON).
//
//   - cols: required-positive for the wire envelope to be
//     sensible; the registry applies 80 default when cols <= 0,
//     so a missing / zero cols lands cleanly (the handler
//     doesn't pin cols > 0). A negative cols value, however, is
//     a definite dashboard bug — flags as -32602 here so the
//     dashboard gets a clear error rather than a silent 80
//     default.
//
//   - rows: same rule as cols.
//
//   - kind: required, closed-set {"output","input"}. Empty /
//     unknown → -32602 via IsValidKind.
//
// Cascade existence: if cascade_id is non-empty, the host's
// cascade seam validates it before the registry call. An unknown
// cascade_id maps to -32004 "Cascade not found" (the same
// envelope StartRemoteAgentCascade / SendMessage surfaces, so
// dashboards' error filters are uniform).
//
// Store call: s.Store == nil → -32603 (matches the project
// surface for "no store installed"). Other errors → -32603.
//
// On success the handler returns the canonical Terminal record
// (with server-stamped created_at + freshly-minted terminal_id).
// No broadcast on Create — the brief pins Create as the moment a
// terminal "appears", and subscribers only learn about it via the
// terminal/output stream once they explicitly Subscribe.
// ListRemoteAgentTerminals is the polling path.
func HandleCreate(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no terminals registry installed")
	}
	var p createParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rerr := validateCreateInput(params, p); rerr != nil {
		return nil, rerr
	}
	cascadeID := normalizeID(p.CascadeID)
	if cascadeID != "" {
		if s.Cascades == nil {
			return nil, rpc.InternalError("registry not initialised")
		}
		if _, ok := s.Cascades(cascadeID); !ok {
			return nil, &rpc.ErrorBody{Code: -32004, Message: "Cascade not found", Data: cascadeID}
		}
	}

	t, rerr := createTerminal(s, cascadeID, p)
	if rerr != nil {
		return nil, rerr
	}
	attachCascadeOutput(s, cascadeID, t.TerminalID)

	return createResponse(t), nil
}

// validateCreateInput runs the closed-set + range checks the
// Create handler applies to the dashboard-supplied params.
// Pulled out so the HandleCreate body stays compact and the
// per-check logic is independently testable.
//
// Returns nil on success or a non-nil *rpc.ErrorBody the caller
// surfaces verbatim.
func validateCreateInput(params json.RawMessage, p createParams) *rpc.ErrorBody {
	// cascade_id is optional — non-string numeric / object
	// would be a dashboard bug (mirrors the triggers / resources
	// validation).
	if rpc.NonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return rpc.InvalidParams("string fields must be strings")
	}
	// cols / rows: reject negative explicitly. The registry's
	// Create defaults 0 → 80/24 which is a reasonable "no
	// override" semantic; negative is just buggy.
	if p.Cols < 0 {
		return rpc.InvalidParams("cols must be >= 0")
	}
	if p.Rows < 0 {
		return rpc.InvalidParams("rows must be >= 0")
	}
	// kind: closed-set.
	if !IsValidKind(p.Kind) {
		return rpc.InvalidParams("kind must be one of " + closedSetList(AllKinds))
	}
	return nil
}

// createTerminal is the store-call half of HandleCreate.
// Pulled out so the handler body stays compact and the
// cascade-attach + response-shaping helpers can reuse the
// registry's returned Terminal record without a second struct.
//
// On any registry error returns MapError (no store / I/O →
// -32603). On success returns the server-stamped Terminal
// record.
func createTerminal(s Seams, cascadeID string, p createParams) (Terminal, *rpc.ErrorBody) {
	t, err := s.Store.Create(Terminal{
		CascadeID: cascadeID,
		Kind:      p.Kind,
		Cols:      p.Cols,
		Rows:      p.Rows,
	})
	if err != nil {
		return Terminal{}, MapError(err, "create terminal")
	}
	return t, nil
}

// attachCascadeOutput wires the cascade's output chan into the
// freshly-created terminal's subscriber fan-out. No-op when
// cascadeID is empty (no binding) or when the cascade has no
// output chan (a Send-only cascade). Errors from AttachCascade
// are dropped — the cascade binding is best-effort and the
// response envelope already echoes the created terminal.
func attachCascadeOutput(s Seams, cascadeID, terminalID string) {
	if cascadeID == "" {
		return
	}
	if s.Cascades == nil {
		return
	}
	c, ok := s.Cascades(cascadeID)
	if !ok || c == nil {
		return
	}
	ch := c.TerminalOutput()
	if ch == nil {
		return
	}
	_ = s.Store.AttachCascade(terminalID, ch)
}

// createResponse renders the brief-pinned flat response shape:
// {ok, terminal_id, kind, cols, rows, created_at}. Matches the
// project surface convention (other Create-style responses are
// similarly lean: short headers without a top-level "terminal"
// object).
func createResponse(t Terminal) map[string]any {
	return map[string]any{
		"ok":          true,
		"terminal_id": t.TerminalID,
		"kind":        string(t.Kind),
		"cols":        t.Cols,
		"rows":        t.Rows,
		"created_at":  t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// HandleList is the JSON-RPC handler for
// "ListRemoteAgentTerminals". Cycle rc-terminals.
//
// Params: {cascade_id?: "" | "<id>"} (optional filter). An
// empty / missing cascade_id means "every terminal" (the
// no-filter branch); a non-empty cascade_id means "terminals
// whose CascadeID matches". Unknown cascade_ids return an empty
// list — the brief's "filter is best-effort" semantic (mirrors
// the triggers / resources precedent).
//
// Nil store → -32603 (matches the project surface). A
// non-numeric cascade_id lands as -32602 (mirrors the other
// methods' "string fields must be strings" envelope).
//
// On success: {ok, terminals:[{…}, …]}. The list is sorted by
// created_at ascending, terminal_id tiebreaker — matches the
// registry's internal sort order. No broadcast.
func HandleList(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no terminals registry installed")
	}
	var p struct {
		CascadeID string `json:"cascade_id"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	cascadeID := normalizeID(p.CascadeID)
	rows := s.Store.List(cascadeID)

	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		items = append(items, toWire(t))
	}
	return map[string]any{
		"ok":        true,
		"terminals": items,
	}, nil
}
