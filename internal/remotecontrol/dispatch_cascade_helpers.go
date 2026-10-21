// Package remotecontrol — dispatch_cascade_helpers.go:
// the helper functions backing the four cascade handler
// methods in dispatch_cascade.go. Pure file split; every
// helper body was previously inline in dispatch.go and
// extracted here so the dispatcher file can stay under
// the 500-line / 50-fn budget that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Why a sibling file (not the dispatcher's switch):
// each helper is a transport-neutral unit (no JSON-RPC
// envelope dance) — they're consumed by exactly one
// handler each, and the test surface for these helpers
// lives at the *Dispatcher tests rather than at the
// handler envelope tests. Putting them here keeps
// dispatch_cascade.go readable as "the four
// dispatchers" and lets us narrow a future regression
// to a file the issue already names.
//
// Groups:
//
//   - Start helpers: decodeStart…Params,
//     resolveStartBackendModel.
//   - Send helpers: decodeSend…Params, buildSendPrompt,
//     buildSendOptions, runSendTurn,
//     renderSendTurnError, renderSendTurnResult.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// decodeStartRemoteAgentCascadeParams unmarshals StartRemoteAgentCascade
// params and applies the JSON-shape validation the brief pins:
// every field except clientInfo must be a string (or absent). The
// JSON unmarshaller would silently coerce a number to "", so a
// buggy dashboard passing `{model: 42}` would otherwise get a
// cascade with the default model. We turn that into a clear -32602.
func decodeStartRemoteAgentCascadeParams(params json.RawMessage) (*startRemoteAgentCascadeParams, *rpcErrorBody) {
	var p startRemoteAgentCascadeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
		}
		// StartCascadeRequest spells the project `project_id`; accept
		// the reference spelling as an alias for `projectId`.
		var protoShim struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal(params, &protoShim); err == nil && p.ProjectID == "" {
			p.ProjectID = protoShim.ProjectID
		}
	}
	if nonStringFieldInJSON(params, "projectId", p.ProjectID) ||
		nonStringFieldInJSON(params, "model", p.Model) ||
		nonStringFieldInJSON(params, "backend", p.Backend) ||
		nonStringFieldInJSON(params, "effort", p.Effort) ||
		nonStringFieldInJSON(params, "agent", p.Agent) ||
		nonStringFieldInJSON(params, "sessionMode", p.SessionMode) {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "string fields must be strings"}
	}
	return &p, nil
}

// resolveStartBackendModel resolves the per-cascade model and
// backend fields. Empty `params.backend` falls back to the host
// session's backend, which in turn falls back to "". The response
// echoes the resolved value so they see which path actually ran.
func (d *Dispatcher) resolveStartBackendModel(p *startRemoteAgentCascadeParams) (string, string) {
	resolvedModel := p.Model
	if resolvedModel == "" && d.cfg.Session != nil {
		resolvedModel = d.cfg.Session.Model
	}
	resolvedBackend := p.Backend
	if resolvedBackend == "" && d.cfg.Session != nil {
		resolvedBackend = d.cfg.Session.Backend
	}
	return resolvedModel, resolvedBackend
}

// decodeSendRemoteAgentCascadeMessageParams unmarshals
// SendRemoteAgentCascadeMessage params, validates string-typed
// fields, and trims whitespace. Returns the populated params and
// nil error on success; returns nil + *rpcErrorBody on failure.
func decodeSendRemoteAgentCascadeMessageParams(params json.RawMessage) (*sendRemoteAgentCascadeMessageParams, *rpcErrorBody) {
	var p sendRemoteAgentCascadeMessageParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
		}
		// The reference's SendUserCascadeMessageRequest carries the
		// turn text as `message_text`; accept it as an alias for the
		// clone's `text` so a reference-shaped caller works unchanged.
		var protoShim struct {
			MessageText string `json:"message_text"`
		}
		if err := json.Unmarshal(params, &protoShim); err == nil && p.Text == "" {
			p.Text = protoShim.MessageText
		}
	}
	if nonStringFieldInJSON(params, "cascade_id", p.CascadeID) ||
		nonStringFieldInJSON(params, "text", p.Text) ||
		nonStringFieldInJSON(params, "agent", p.Agent) ||
		nonStringFieldInJSON(params, "model", p.Model) ||
		nonStringFieldInJSON(params, "effort", p.Effort) ||
		nonStringFieldInJSON(params, "sessionMode", p.SessionMode) {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "string fields must be strings"}
	}
	cascadeID := strings.TrimSpace(p.CascadeID)
	text := strings.TrimSpace(p.Text)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascade_id is required"}
	}
	if text == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "text is required"}
	}
	p.CascadeID = cascadeID
	p.Text = text
	return &p, nil
}

// buildSendPrompt constructs the per-turn MessagePrompt. Per-turn
// overrides fall back to the cascade's pinned values. The cascade's
// pinned values may themselves be empty (the dashboard left them
// empty at Start time, or those are the host defaults); we
// surface whatever the cascade has rather than re-resolving
// against the host SessionState here, because the brief's
// "resolve once" rule means the Start-time resolution already
// happened.
func buildSendPrompt(c *Cascade, p *sendRemoteAgentCascadeMessageParams) MessagePrompt {
	return MessagePrompt{
		ConversationID: c.ConversationID,
		Text:           p.Text,
		Model:          firstNonEmpty(p.Model, c.Model),
		Agent:          firstNonEmpty(p.Agent, c.Agent),
		Effort:         firstNonEmpty(p.Effort, c.Effort),
		Scaffold:       p.Scaffold || c.Scaffold,
		SessionMode:    firstNonEmpty(p.SessionMode, c.SessionMode),
		BackendLabel:   c.BackendLabel,
	}
}

// buildSendOptions maps the per-turn TimeoutMs onto
// MessageSendOptions. TimeoutMs<=0 means "no override".
func buildSendOptions(p *sendRemoteAgentCascadeMessageParams) MessageSendOptions {
	opts := MessageSendOptions{}
	if p.TimeoutMs > 0 {
		opts.Timeout = time.Duration(p.TimeoutMs) * time.Millisecond
	}
	return opts
}

// interactionParams is the unpacked envelope of a
// HandleRemoteAgentCascadeUserInteraction call. Decoded in
// dispatch_cascade_helpers.go and consumed by the handler
// in dispatch_cascade.go.
type interactionParams struct {
	CascadeID     string
	InteractionID string
	Response      json.RawMessage
}

// decodeInteractionParams unmarshals
// HandleRemoteAgentCascadeUserInteraction params, validates
// string-typed fields, trims whitespace, and rejects
// missing / empty / null values for the three required
// fields. Extracted from the handler to keep the handler
// body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeInteractionParams(params json.RawMessage) (interactionParams, *rpcErrorBody) {
	var raw struct {
		CascadeID     string          `json:"cascade_id"`
		InteractionID string          `json:"interaction_id"`
		Response      json.RawMessage `json:"response"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &raw); err != nil {
			return interactionParams{}, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
		}
	}
	if nonStringFieldInJSON(params, "cascade_id", raw.CascadeID) ||
		nonStringFieldInJSON(params, "interaction_id", raw.InteractionID) {
		return interactionParams{}, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "string fields must be strings"}
	}
	cascadeID := strings.TrimSpace(raw.CascadeID)
	interactionID := strings.TrimSpace(raw.InteractionID)
	if cascadeID == "" {
		return interactionParams{}, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascade_id is required"}
	}
	if interactionID == "" {
		return interactionParams{}, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "interaction_id is required"}
	}
	if len(raw.Response) == 0 || string(raw.Response) == "null" {
		return interactionParams{}, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "response is required"}
	}
	return interactionParams{
		CascadeID:     cascadeID,
		InteractionID: interactionID,
		Response:      raw.Response,
	}, nil
}

// runSendTurn drives one cascade turn via the registry's
// SendStream, normalises error envelopes, and assembles the
// success envelope. Pure file split extracted from the prior
// handleSendRemoteAgentCascadeMessage body.
func (d *Dispatcher) runSendTurn(ctx context.Context, c *Cascade, cascadeID string, prompt MessagePrompt, sink EventSink, opts MessageSendOptions) (any, *rpcErrorBody) {
	start := time.Now()
	res, err := d.cascades.SendStream(ctx, cascadeID, prompt, sink, opts)
	if err != nil {
		return d.renderSendTurnError(c, cascadeID, err, sink)
	}
	return d.renderSendTurnResult(res, start), nil
}

// renderSendTurnError normalises a SendStream error into a
// JSON-RPC envelope, fans a cascade/state(error) notification
// through the sink so streaming dashboards see it before the
// synchronous -32603 envelope arrives, and appends the error to
// the cascade's bounded error ring.
func (d *Dispatcher) renderSendTurnError(c *Cascade, cascadeID string, err error, sink EventSink) (any, *rpcErrorBody) {
	sink.OnError(c, err)
	var envelope *rpcErrorBody
	switch {
	case errors.Is(err, ErrCascadeNotFound):
		envelope = &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}
	case errors.Is(err, ErrCascadeClosed):
		envelope = &rpcErrorBody{Code: ErrCodeCascadeClosed, Message: "Cascade closed", Data: cascadeID}
	case errors.Is(err, ErrTurnTimeout):
		envelope = &rpcErrorBody{Code: ErrCodeTurnTimeout, Message: "Turn timeout", Data: err.Error()}
	default:
		// ErrTurnFailed and any unmapped backend error.
		envelope = &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}
	}
	if c != nil {
		c.recordError("SendRemoteAgentCascadeMessage", err, envelope.Code, time.Now().UTC())
	}
	return nil, envelope
}

// buildCascadeOptsFromStart translates the decoded Start params
// into a fully-populated CascadeOpts bundle ready for NewCascade,
// honouring the per-cascade model / backend resolution (host
// SessionState fallback) and the *bool → bool coercion for
// sandbox / scaffold. Pure helper — extracted from
// handleStartRemoteAgentCascade to keep that handler under the
// 50-fn ceiling that cycle rc-filesize-internal-remotecontrol
// enforces.
func (d *Dispatcher) buildCascadeOptsFromStart(p *startRemoteAgentCascadeParams) (CascadeOpts, string, string) {
	sandbox := false
	if p.Sandbox != nil {
		sandbox = *p.Sandbox
	}
	scaffold := false
	if p.Scaffold != nil {
		scaffold = *p.Scaffold
	}
	workspace := ""
	if d.cfg.Session != nil {
		workspace = d.cfg.Session.Workspace
	}
	resolvedModel, resolvedBackend := d.resolveStartBackendModel(p)
	return CascadeOpts{
		ClientInfo: CascadeClientInfo{
			Name:    p.ClientInfo.Name,
			Version: p.ClientInfo.Version,
		},
		ProjectID:       p.ProjectID,
		Model:           p.Model,
		Effort:          p.Effort,
		Agent:           p.Agent,
		Sandbox:         sandbox,
		Scaffold:        scaffold,
		SessionMode:     p.SessionMode,
		Workspace:       workspace,
		BackendLabel:    resolvedBackend,
		DispatchDefault: d.cfg.MessageBackend,
	}, resolvedModel, resolvedBackend
}

// renderSendTurnResult translates the backend ToolCall slice
// (Args as json.RawMessage) into the wire's toolCall shape,
// dropping nil/empty entries defensively, and assembles the
// final synchronous response envelope.
func (d *Dispatcher) renderSendTurnResult(res MessageResult, start time.Time) map[string]any {
	toolCalls := make([]toolCall, 0, len(res.ToolCalls))
	for _, tc := range res.ToolCalls {
		if tc.Name == "" {
			continue
		}
		args := tc.Args
		if len(args) == 0 {
			args = json.RawMessage("null")
		}
		toolCalls = append(toolCalls, toolCall{
			Name:   tc.Name,
			Args:   args,
			Result: tc.Result,
			OK:     tc.OK,
		})
	}
	if toolCalls == nil {
		// JSON-encoding nil slice yields `null`; we want
		// `[]` for the dashboard (matches the brief:
		// "empty array if none").
		toolCalls = []toolCall{}
	}
	stopReason := res.StopReason
	if stopReason == "" {
		// Backend didn't supply a reason; default to
		// end_turn so the dashboard sees a well-known
		// value rather than an empty string.
		stopReason = StopReasonEndTurn
	}
	return map[string]any{
		"conversationId": res.NewConversationID,
		"reply":          res.Reply,
		"durationMs":     time.Since(start).Milliseconds(),
		"toolCalls":      toolCalls,
		"stopReason":     stopReason,
	}
}
