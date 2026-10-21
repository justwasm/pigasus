// Package remotecontrol — dispatch_cascade.go: the four cascade
// lifecycle JSON-RPC handlers the dispatcher routes through:
//
//   - StartRemoteAgentCascade — allocates a *Cascade and registers
//     it with the dispatcher's registry.
//   - SendRemoteAgentCascadeMessage — drives one turn end-to-end.
//   - CancelRemoteAgentCascade — fires the per-turn cancel func.
//   - HandleRemoteAgentCascadeUserInteraction — resolves a
//     pending permission prompt with the dashboard's reply.
//
// Shared types (sendRemoteAgentCascadeMessageParams / Result /
// toolCall) live here too because they're consumed by both the
// dispatcher and the cascade registry. Pure file split; every
// handler body was previously inline in dispatch.go.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// startRemoteAgentCascadeParams is the JSON shape the dashboard
// posts for StartRemoteAgentCascade. Every field is optional;
// the dashboard is allowed to send an empty object.
type startRemoteAgentCascadeParams struct {
	ClientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	ProjectID string `json:"projectId"`
	Model     string `json:"model"`
	// rc-polish (G16): backend is a per-cascade override the
	// dashboard can pin. With this field, an explicit non-empty
	// value wins over the host's backend (matching the brief).
	Backend     string `json:"backend"`
	Effort      string `json:"effort"`
	Agent       string `json:"agent"`
	Sandbox     *bool  `json:"sandbox"`
	Scaffold    *bool  `json:"scaffold"`
	SessionMode string `json:"sessionMode"`
}

// sendRemoteAgentCascadeMessageParams is the JSON shape the
// dashboard posts for SendRemoteAgentCascadeMessage. Field names
// mirror the antigravity wire format; opt fields use pointer-ish
// semantics (omitempty on Agent / Model / Effort / SessionMode
// means "absent = use cascade default").
type sendRemoteAgentCascadeMessageParams struct {
	CascadeID   string `json:"cascade_id"`
	Text        string `json:"text"`
	Agent       string `json:"agent,omitempty"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Scaffold    bool   `json:"scaffold"`
	SessionMode string `json:"sessionMode,omitempty"`
	TimeoutMs   int    `json:"timeoutMs,omitempty"`
}

// sendRemoteAgentCascadeMessageResult is the JSON shape the
// dispatcher returns on a successful turn. conversationId is the
// cascade's ConversationID; reply is the assistant's final text;
// durationMs is the wall-clock elapsed during Send; toolCalls is
// the JSON-faithful slice of tool invocations; stopReason is the
// four-value vocabulary (end_turn | tool_use | max_tokens | error).
type sendRemoteAgentCascadeMessageResult struct {
	ConversationID string     `json:"conversationId"`
	Reply          string     `json:"reply"`
	DurationMs     int64      `json:"durationMs"`
	ToolCalls      []toolCall `json:"toolCalls"`
	StopReason     string     `json:"stopReason"`
}

// toolCall is one tool invocation the model produced during the
// turn. Args is the raw JSON object the model emitted (preserved
// as json.RawMessage so the dashboard sees the same shape the
// model produced); Result is the textual result string; OK is
// false when the tool errored.
type toolCall struct {
	Name   string          `json:"name"`
	Args   json.RawMessage `json:"args,omitempty"`
	Result string          `json:"result"`
	OK     bool            `json:"ok"`
}

// handleStartRemoteAgentCascade is the rpc-handler for
// "StartRemoteAgentCascade". It parses the params (every field
// is optional), constructs a *Cascade via NewCascade, and
// registers it with the dispatcher's cascade registry.
//
// Errors:
//
//   - JSON parse failure → -32602 Invalid params.
//   - ID mint failure / registry full → -32603 Internal error.
//
// On success the response carries the dashboard-facing routing
// handle (cascade_id), the empty conversation id, the resolved
// model, the backend, and the daemon version. clientInfo is
// echoed only in logs.
//
// Per-cascade model resolution: when params omits "model" (or
// sends ""), the handler falls back to the dispatcher's
// SessionState.Model — the resolved model the host is running.
func (d *Dispatcher) handleStartRemoteAgentCascade(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}
	}
	p, perr := decodeStartRemoteAgentCascadeParams(params)
	if perr != nil {
		return nil, perr
	}
	opts, resolvedModel, resolvedBackend := d.buildCascadeOptsFromStart(p)
	c, err := NewCascade(opts)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "failed to mint cascade id: " + err.Error()}
	}
	if err := d.cascades.Register(c); err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}
	}
	return map[string]any{
		"cascade_id":     c.ID,
		"conversationId": c.ConversationID,
		// rc-polish (G15): the previous call passed the
		// *formatted* timestamp string back into Format as
		// if it were a layout constant, which produced
		// nonsense like "18186-100-108T180:08:180.78Z".
		// Match the project surface used by
		// project_dispatcher.go (line 251) and
		// conversations lists: .UTC().Format(time.RFC3339Nano).
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
		"model":      resolvedModel,
		"backend":    resolvedBackend,
		"version":    ServerVersion,
	}, nil
}

// handleSendRemoteAgentCascadeMessage is the JSON-RPC handler
// for "SendRemoteAgentCascadeMessage". Cycle rc-cascade-message.
// Drives one cascade turn end-to-end, blocking the request until
// the backend returns.
//
// Param validation:
//
//   - cascade_id: required, non-empty.
//   - text: required, non-empty.
//   - effort: optional, must be one of DefaultEffortValues when
//     present. Empty means "use cascade's pinned effort".
//   - agent / model / sessionMode: optional; empty means "use
//     cascade's pinned value". Type-checked via
//     nonStringFieldInJSON.
//   - timeoutMs: optional, > 0. Bounds the backend Send call.
//
// Cascade resolution (after validation):
//   - look up via dispatcher.cascades.Get; missing → -32004
//     "Cascade not found";
//   - closed sentinel → -32005 "Cascade closed";
//   - per-turn overrides applied to MessagePrompt;
//   - opts carries Timeout = TimeoutMs * ms (when > 0).
//
// On success the result carries the final assistant text, the
// conversationId, the wall-clock duration, the tool-calls slice,
// and the stop reason. On failure the dispatcher maps the
// registry's error sentinel to the JSON-RPC envelope: -32004 /
// -32005 / -32010 / -32603.
func (d *Dispatcher) handleSendRemoteAgentCascadeMessage(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}
	}
	p, perr := decodeSendRemoteAgentCascadeMessageParams(params)
	if perr != nil {
		return nil, perr
	}
	if p.Effort != "" && !IsValidEffort(p.Effort) {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "effort must be one of low|medium|high|xhigh|max"}
	}
	if p.TimeoutMs < 0 {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "timeoutMs must be >= 0"}
	}
	c, ok := d.cascades.Get(p.CascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: p.CascadeID}
	}
	if c.IsClosed() {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeClosed, Message: "Cascade closed", Data: p.CascadeID}
	}
	prompt := buildSendPrompt(c, p)
	opts := buildSendOptions(p)
	connID := connIDFromContext(ctx)
	sink := NewConnNotifierSink(d, connID, c)
	return d.runSendTurn(ctx, c, p.CascadeID, prompt, sink, opts)
}

// handleCancelRemoteAgentCascade is the JSON-RPC handler for
// "CancelRemoteAgentCascade". Cycle rc-cancel-cascade adds this
// method as the dashboard-side surface for interrupting an
// in-flight turn.
//
// Param validation:
//
//   - cascade_id: required, non-empty.
//
// Error envelopes:
//
//   - missing cascade_id → -32602 "Invalid params".
//   - cascade_id present but no cascade registered → -32004.
//   - cancel plumbing failed → -32603.
//
// Wire side-effect: a successful cancel emits a
// cascade/state{phase:"cancelled"} notification on the originating
// ws connection. Idempotent: a second Cancel returns ok:true with
// already_cancelled:true and emits NO notification.
func (d *Dispatcher) handleCancelRemoteAgentCascade(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}
	}
	var p struct {
		CascadeID string `json:"cascade_id"`
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
	now := time.Now().UTC()
	fired := c.Cancel("user requested")
	if fired {
		c.recordCancel("user requested", now)
		connID := connIDFromContext(ctx)
		if connID != "" {
			_ = d.Notify(context.Background(), connID, NotifCascadeState, map[string]any{
				"cascade_id":   cascadeID,
				"phase":        StatePhaseCancelled,
				"cancelled_at": now.Format(time.RFC3339Nano),
				"reason":       "user requested",
				"ts":           now.Format(time.RFC3339Nano),
			})
		}
	}
	return map[string]any{
		"ok":                true,
		"already_cancelled": !fired,
		"cancelled_at":      now.Format(time.RFC3339Nano),
	}, nil
}

// handleRemoteAgentCascadeUserInteraction is the JSON-RPC
// handler for "HandleRemoteAgentCascadeUserInteraction". Cycle
// rc-handle-user-interaction. The dashboard calls this method to
// deliver a response to a previously-pushed
// cascade/user_interaction_required notification.
//
// Param validation:
//
//   - cascade_id: required, non-empty.
//   - interaction_id: required, non-empty.
//   - response: required (may be any JSON value — string,
//     object, number).
//
// Error envelopes:
//
//   - missing cascade_id / interaction_id → -32602.
//   - cascade_id present but no cascade registered → -32004.
//   - cascade exists, interaction_id unknown → -32006.
//
// On success the result envelope is:
//
//	{
//	  "ok":         true,
//	  "decided_at": "<RFC3339-nano>",
//	  "applied_to": ["tool:<name>", ...]    // placeholder; empty today
//	}
func (d *Dispatcher) handleRemoteAgentCascadeUserInteraction(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}
	}
	p, perr := decodeInteractionParams(params)
	if perr != nil {
		return nil, perr
	}
	c, ok := d.cascades.Get(p.CascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: p.CascadeID}
	}
	pending := c.Pending()
	if _, ok := pending.Get(p.InteractionID); !ok {
		return nil, &rpcErrorBody{Code: ErrCodeInteractionNotFound, Message: "Interaction not found", Data: p.InteractionID}
	}
	now := time.Now().UTC()
	if err := pending.Resolve(p.InteractionID, InteractionResult{Response: p.Response, DecidedAt: now}); err != nil {
		if errors.Is(err, ErrInteractionNotFound) {
			return nil, &rpcErrorBody{Code: ErrCodeInteractionNotFound, Message: "Interaction not found", Data: p.InteractionID}
		}
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}
	}
	return map[string]any{
		"ok":         true,
		"decided_at": now.Format(time.RFC3339Nano),
		// applied_to is a placeholder for future cycles:
		// the brief explicitly leaves the tool-list
		// surface as an empty array today; the PiG side
		// will populate it in rc-piagent-bind once we
		// know which tool the prompt was for.
		"applied_to": []string{},
	}, nil
}
