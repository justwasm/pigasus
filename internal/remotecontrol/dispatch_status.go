// Package remotecontrol — dispatch_status.go: the four JSON-RPC
// handlers the rc-status-tool cycle ships. Each is small (≤ 60 LOC)
// and shares the same dispatch.go switch entry as the
// cascade/project/delete surfaces.
//
//   - RemoteAgentErrorStatus returns the per-cascade bounded error
//     ring the SendMessage path records into via
//     (*Cascade).recordError.
//   - SetRemoteControlSession binds a dashboard-supplied session
//     id to the dispatcher's AuthState.
//   - remoteControlVersion returns the binary's version + build-time
//     label + commit SHA.
//
// Note: rc-status-tool originally shipped a fifth method
// (EnsureRemoteAgentACEVM, parity stub). Cycle rc-ace-real removed
// the method entirely — the recovered ACE VM is reachable only via
// the production codeassistclient (gRPC over a remote Code Assist
// server), with no local Unix socket, no $ACE_VM_SOCKET env var,
// and no local ACE binary to probe. The default arm in
// (*Dispatcher).Dispatch returns -32601 "Method not found" for any
// caller that still posts the method name.
package remotecontrol

import (
	"encoding/json"
	"strings"
	"time"
)

// richStatusSnapshot is the JSON-able view the rich
// status tool emits. Built independently of the cycle-1
// SessionState.statusSnapshot so the legacy shape is
// preserved as a fallback for tests that still assert
// the simple field set (pid/version/backend/model/
// workspace) — those tests pin cycle-1's behaviour until
// they migrate.
//
// Nil-safety: each subsnapshot is taken with its own
// nil-check so a dispatcher with bare-bones Config
// (no AuthState, no DaemonMeta, no cascades registry)
// returns a populated snapshot with the auth/daemon
// blocks showing zero values rather than panicking.
//
// Wire shape:
//
//	{
//	  "pid":               <int>,
//	  "version":           "<cli-version>",
//	  "backend":           "gateway" | "shim",
//	  "model":             "<label>",
//	  "workspace":         "<abs>",
//	  "started_at":        "<RFC3339>",
//	  "uptime":            "<1h23m>",
//	  "connected_clients": <int>,
//	  "cascades":          {total, active, max},
//	  "auth":              {method, session_id, user_id, scopes, expires_at},
//	  "daemon":            {transport, addr, mode}
//	}
func (d *Dispatcher) richStatusSnapshot() map[string]any {
	out := map[string]any{}
	if d.cfg.Session != nil {
		d.cfg.Session.mu.RLock()
		pid := d.cfg.Session.PID
		version := d.cfg.Session.Version
		backend := d.cfg.Session.Backend
		model := d.cfg.Session.Model
		workspace := d.cfg.Session.Workspace
		conversationID := d.cfg.Session.ConversationID
		startedAt := d.cfg.Session.StartedAt
		connected := d.cfg.Session.connected.Load()
		d.cfg.Session.mu.RUnlock()
		out["pid"] = pid
		out["version"] = version
		out["backend"] = backend
		out["model"] = model
		out["workspace"] = workspace
		out["conversation_id"] = conversationID
		out["started_at"] = startedAt.UTC().Format(time.RFC3339Nano)
		out["uptime"] = formatUptime(time.Since(startedAt))
		out["connected_clients"] = connected
	}
	d.richStatusCascades(out)
	d.richStatusAuth(out)
	d.richStatusDaemon(out)
	return out
}

// richStatusCascades writes the "cascades" block: total = registry
// Len(); active = total - len(closed cascades); max = registry Max().
func (d *Dispatcher) richStatusCascades(out map[string]any) {
	cascadesTotal := 0
	cascadesActive := 0
	cascadesMax := CascadeRegistryDefaultMax
	if d.cascades != nil {
		cascadesTotal = d.cascades.Len()
		cascadesMax = d.cascades.Max()
		d.cascades.mu.RLock()
		for _, c := range d.cascades.cascades {
			if c == nil {
				continue
			}
			if !c.IsClosed() {
				cascadesActive++
			}
		}
		d.cascades.mu.RUnlock()
	}
	out["cascades"] = map[string]any{
		"total":  cascadesTotal,
		"active": cascadesActive,
		"max":    cascadesMax,
	}
}

// richStatusAuth writes the "auth" block. A nil AuthState yields a
// synthetic "none" method and zero values so the block is always
// present.
func (d *Dispatcher) richStatusAuth(out map[string]any) {
	auth := AuthSnapshot{}
	if d.cfg.AuthState != nil {
		auth = d.cfg.AuthState.Snapshot()
	}
	method := auth.Method
	if method == "" {
		method = AuthMethodNone
	}
	// Normalise Scopes to a non-nil slice so the wire emits
	// `[]` rather than `null` regardless of whether the
	// dispatcher installed an AuthState.
	scopes := auth.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	authBlock := map[string]any{
		"method":     method,
		"session_id": auth.SessionID,
		"user_id":    auth.UserID,
		"scopes":     scopes,
	}
	if !auth.ExpiresAt.IsZero() {
		authBlock["expires_at"] = auth.ExpiresAt.UTC().Format(time.RFC3339)
	} else {
		authBlock["expires_at"] = nil
	}
	out["auth"] = authBlock
}

// richStatusDaemon writes the "daemon" block. nil DaemonMeta yields
// zero values (the block is always present so dashboards never
// nil-check).
func (d *Dispatcher) richStatusDaemon(out map[string]any) {
	meta := DaemonMeta{}
	if d.cfg.DaemonMeta != nil {
		meta = d.cfg.DaemonMeta.Snapshot()
	}
	out["daemon"] = map[string]any{
		"transport": meta.Transport,
		"addr":      meta.Addr,
		"mode":      meta.Mode,
	}
}

// handleRemoteAgentErrorStatus is the JSON-RPC handler for
// "RemoteAgentErrorStatus". Param validation:
//
//   - cascade_id: required, non-empty. Absent → -32602.
//   - limit: optional int 1..MaxCascadeErrors. Out-of-range
//     values are clamped.
//
// Error envelopes:
//
//   - missing cascade_id → -32602 "Invalid params".
//   - unknown cascade_id → -32004 "Cascade not found"
//     (matches SendRemoteAgentCascadeMessage's envelope).
func (d *Dispatcher) handleRemoteAgentErrorStatus(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}
	}
	var p struct {
		CascadeID string `json:"cascade_id"`
		Limit     int    `json:"limit"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
		}
	}
	if nonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "string fields must be strings"}
	}
	cascadeID := strings.TrimSpace(p.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascade_id is required"}
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}
	}
	snap := c.ErrorsSnapshot()
	if snap == nil {
		// Normalise to an empty slice so the wire emits `[]`
		// rather than `null` — matches the brief's
		// "errors: []" framing.
		snap = []CascadeError{}
	}
	if p.Limit > 0 && p.Limit < len(snap) {
		snap = snap[len(snap)-p.Limit:]
	} else if p.Limit > MaxCascadeErrors {
		// Hard cap at the ring's natural size; a dashboard
		// that asks for more gets the full ring.
		p.Limit = MaxCascadeErrors
	}
	return map[string]any{
		"cascade_id": cascadeID,
		"errors":     snap,
	}, nil
}

// handleSetRemoteControlSession binds a dashboard-supplied session
// id to the dispatcher's AuthState (Config.AuthState). Subsequent
// `status` calls reflect the new auth block verbatim.
//
// Param validation:
//
//   - session_id: required, non-empty. Absent → -32602.
//   - user_id: optional.
//   - scopes: optional array of strings.
//   - expires_at: optional RFC3339 string.
//
// The handler always fabricates a non-nil AuthState when
// Config.AuthState is nil. Error envelopes:
//
//   - missing session_id → -32602 "Invalid params".
func (d *Dispatcher) handleSetRemoteControlSession(params json.RawMessage) (any, *rpcErrorBody) {
	p, perr := d.decodeSetSessionParams(params)
	if perr != nil {
		return nil, perr
	}
	if d.cfg.AuthState == nil {
		d.cfg.AuthState = NewAuthState("", "", "", nil, time.Time{})
	}
	d.cfg.AuthState.Set(AuthMethodGoogle, p.SessionID, strings.TrimSpace(p.UserID), p.Scopes, p.ExpiresAt)
	snap := d.cfg.AuthState.Snapshot()
	return sessionSnapshotToWire(snap), nil
}

// decodeSetSessionParams unmarshals SetRemoteControlSession params
// and validates them. Returns a populated setSessionParams and nil
// error on success; returns nil + an *rpcErrorBody on validation
// failure.
type setSessionParams struct {
	SessionID string
	UserID    string
	Scopes    []string
	ExpiresAt time.Time
}

func (d *Dispatcher) decodeSetSessionParams(params json.RawMessage) (*setSessionParams, *rpcErrorBody) {
	var p struct {
		SessionID string   `json:"session_id"`
		UserID    string   `json:"user_id"`
		Scopes    []string `json:"scopes"`
		ExpiresAt string   `json:"expires_at"`
		Method    string   `json:"method"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
		}
	}
	if nonStringFieldInJSON(params, "session_id", p.SessionID) ||
		nonStringFieldInJSON(params, "user_id", p.UserID) ||
		nonStringFieldInJSON(params, "expires_at", p.ExpiresAt) {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "string fields must be strings"}
	}
	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "session_id is required"}
	}
	var expiresAt time.Time
	if p.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, p.ExpiresAt)
		if err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "expires_at must be RFC3339: " + err.Error()}
		}
		expiresAt = t
	}
	return &setSessionParams{
		SessionID: sessionID,
		UserID:    p.UserID,
		Scopes:    p.Scopes,
		ExpiresAt: expiresAt,
	}, nil
}

// sessionSnapshotToWire renders the AuthSnapshot into the response
// envelope SetRemoteControlSession returns on the wire.
func sessionSnapshotToWire(snap AuthSnapshot) map[string]any {
	caps := snap.Scopes
	if caps == nil {
		caps = []string{}
	}
	resp := map[string]any{
		"session_id": snap.SessionID,
		"user_id":    snap.UserID,
		"scopes":     caps,
	}
	if !snap.ExpiresAt.IsZero() {
		resp["expires_at"] = snap.ExpiresAt.UTC().Format(time.RFC3339)
	} else {
		resp["expires_at"] = nil
	}
	return resp
}

// handleRemoteControlVersion is the JSON-RPC handler for
// "remoteControlVersion". Cycle rc-status-tool adds this as a
// dedicated version probe so a dashboard's "what version is the
// daemon?" check doesn't have to unmarshal the entire status
// envelope.
//
// Wire envelope:
//
//	{
//	  "version":     "1.4.0",
//	  "build_label": "local" | "ci" | "release" | "unknown",
//	  "daemon_sha":  "<40-char SHA>" | "unknown"
//	}
//
// Field sources:
//
//   - version: cli.ReferenceVersion (cycle 1 constant).
//   - build_label: Config.BuildLabel (cycle 12). Empty → "unknown".
//   - daemon_sha: Config.Commit (cycle 12). Empty → "unknown".
func (d *Dispatcher) handleRemoteControlVersion() (any, *rpcErrorBody) {
	version := ServerVersion
	if d.cfg.Session != nil {
		d.cfg.Session.mu.RLock()
		if v := d.cfg.Session.Version; v != "" {
			version = v
		}
		d.cfg.Session.mu.RUnlock()
	}
	buildLabel := strings.TrimSpace(d.cfg.BuildLabel)
	if buildLabel == "" {
		buildLabel = "unknown"
	}
	sha := strings.TrimSpace(d.cfg.Commit)
	if sha == "" {
		sha = "unknown"
	}
	return map[string]any{
		"version":     version,
		"build_label": buildLabel,
		"daemon_sha":  sha,
	}, nil
}
