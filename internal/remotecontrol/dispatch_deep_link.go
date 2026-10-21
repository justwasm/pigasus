// Package remotecontrol — dispatch_deep_link.go: the
// "remoteControlDeepLink" JSON-RPC handler. Cycle rc-deep-link.
// Parses an `agy://...` URL the dashboard's browser companion
// delivers and dispatches on the resolved verb (open /
// focus-cascade / paste-and-send).
//
// Pure file split; the prior handleRemoteControlDeepLink was
// 174 LOC inline in dispatch.go and is decomposed here into:
//
//   - handleRemoteControlDeepLink (parse + dispatch entry).
//   - handleDeepLinkOpen (open verb).
//   - handleDeepLinkFocusCascade (focus-cascade verb).
//   - handleDeepLinkPasteAndSend (paste-and-send verb).
//   - decodeDeepLinkParams (param shape guard).
package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// handleRemoteControlDeepLink dispatches one deep-link RPC.
//
// Param validation:
//
//   - url: required, non-empty string. Whitespace-trimmed.
//   - cascade_id: optional at the params level. For
//     focus-cascade and paste-and-send verbs the handler
//     resolves the cascade by either top-level `cascade_id`
//     (preferred) or the URL-embedded `cascade_id=...` query
//     param.
//
// Errors:
//
//   - empty params → -32602 "url is required"
//   - unparseable URL → -32602 wrapping ErrDeepLinkMalformed
//   - scheme != "agy" → -32602 wrapping ErrDeepLinkBadScheme
//   - missing host / verb → -32602 wrapping
//     ErrDeepLinkMissingVerb
//   - unknown verb → -32602 wrapping ErrDeepLinkUnknownVerb
//   - cascade_id missing → -32602 "cascade_id is required"
//   - cascade_id unknown → -32004 "Cascade not found"
//   - paste-and-send submit failure before the goroutine
//     launches → -32603.
//
// Wire envelope (success):
//
//	{
//	  "ok":          true,
//	  "action":      "open" | "focus-cascade" | "paste-and-send",
//	  "echo":        {"host":"<verb>", "query":{<key>:<value>}, "raw":"<url>"},
//	  "cascade_id":  "<cas_…>",   // focus-cascade + paste-and-send
//	  "received_at": "<RFC3339Nano>",  // open + focus-cascade
//	  "accepted_at": "<RFC3339Nano>"   // paste-and-send
//	}
func (d *Dispatcher) handleRemoteControlDeepLink(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "dispatcher not initialised"}
	}
	p, perr := decodeDeepLinkParams(params)
	if perr != nil {
		return nil, perr
	}
	dl, err := ParseDeepLink(p.URL)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
	}
	now := time.Now().UTC()
	ts := now.Format(time.RFC3339Nano)
	echo := dl.Echo()
	switch dl.Action {
	case DeepLinkOpen:
		return d.handleDeepLinkOpen(now, ts, echo), nil
	case DeepLinkFocusCascade:
		return d.handleDeepLinkFocusCascade(ctx, p.CascadeID, dl, ts, echo)
	case DeepLinkPasteAndSend:
		return d.handleDeepLinkPasteAndSend(ctx, p.CascadeID, dl, ts, echo)
	}
	// ParseDeepLink returned Action=="" — the switch's
	// exhaustive set covers every action constant. We
	// shouldn't land here, but if a future cycle adds a new
	// DeepLinkAction without updating this handler, return a
	// defensive -32602 instead of panicking.
	return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "unsupported deep-link action"}
}

// decodeDeepLinkParams unmarshals remoteControlDeepLink params
// and validates the URL field is a non-empty string. Returns
// the populated struct and nil error on success; returns nil +
// an *rpcErrorBody on validation failure.
type deepLinkParams struct {
	URL       string
	CascadeID string
}

func decodeDeepLinkParams(params json.RawMessage) (*deepLinkParams, *rpcErrorBody) {
	var p struct {
		URL       string `json:"url"`
		CascadeID string `json:"cascade_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
		}
	}
	if nonStringFieldInJSON(params, "url", p.URL) ||
		nonStringFieldInJSON(params, "cascade_id", p.CascadeID) {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "string fields must be strings"}
	}
	raw := strings.TrimSpace(p.URL)
	if raw == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "url is required"}
	}
	return &deepLinkParams{URL: raw, CascadeID: p.CascadeID}, nil
}

// handleDeepLinkOpen stamps the dispatcher's last-deep-link
// state and returns the open verb's envelope.
func (d *Dispatcher) handleDeepLinkOpen(now time.Time, ts string, echo map[string]any) map[string]any {
	if d.cfg.LastDeepLink != nil {
		d.cfg.LastDeepLink.RecordOpen(now)
	}
	return map[string]any{
		"ok":          true,
		"action":      string(DeepLinkOpen),
		"echo":        echo,
		"received_at": ts,
	}
}

// handleDeepLinkFocusCascade flips the cascade's focused flag
// via Focus() and emits a cascade/state{phase:"focused"}
// notification on the originating ws connection.
//
// The top-level `cascade_id` wins over the URL-embedded one —
// the top-level field is the dashboard's explicit choice; the
// URL-embedded one is the canonical-agy fallback.
func (d *Dispatcher) handleDeepLinkFocusCascade(ctx context.Context, topCascadeID string, dl DeepLink, ts string, echo map[string]any) (any, *rpcErrorBody) {
	cascadeID := firstNonEmpty(strings.TrimSpace(topCascadeID), dl.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascade_id is required"}
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}
	}
	c.Focus()
	connID := connIDFromContext(ctx)
	if connID != "" {
		_ = d.Notify(context.Background(), connID, NotifCascadeState, map[string]any{
			"cascade_id": cascadeID,
			"phase":      "focused",
			"focused_at": ts,
			"ts":         ts,
		})
	}
	return map[string]any{
		"ok":          true,
		"action":      string(DeepLinkFocusCascade),
		"cascade_id":  cascadeID,
		"echo":        echo,
		"received_at": ts,
	}, nil
}

// handleDeepLinkPasteAndSend spawns a goroutine that drives the
// cascade's backend through (*CascadeRegistry).SendStream with
// the parsed text. The reply is synchronous (with
// `accepted_at`); the turn's output lands on the same ws via
// the normal cascade/state + cascade/text_delta notifications.
//
// Errors:
//
//   - cascade_id missing → -32602.
//   - cascade_id unknown → -32004.
//   - cascade closed → -32005 "Cascade closed" (the rare path
//     when the cascade was closed between Start and the
//     deep-link's arrival).
func (d *Dispatcher) handleDeepLinkPasteAndSend(ctx context.Context, topCascadeID string, dl DeepLink, ts string, echo map[string]any) (any, *rpcErrorBody) {
	cascadeID, text, perr := validateDeepLinkTargets(topCascadeID, dl)
	if perr != nil {
		return nil, perr
	}
	c, perr := d.lookupOpenCascadeForDeepLink(cascadeID)
	if perr != nil {
		return nil, perr
	}
	// Capture the connID before spawning the goroutine so the
	// goroutine's notifications land on the originating ws.
	connID := connIDFromContext(ctx)
	prompt := MessagePrompt{
		ConversationID: c.ConversationID,
		Text:           text,
		Model:          c.Model,
		Agent:          c.Agent,
		Effort:         c.Effort,
		Scaffold:       c.Scaffold,
		SessionMode:    c.SessionMode,
		BackendLabel:   c.BackendLabel,
	}
	cascadeIDCopy := cascadeID
	go d.runDeepLinkAsync(cascadeIDCopy, prompt, c, connID)
	return map[string]any{
		"ok":          true,
		"action":      string(DeepLinkPasteAndSend),
		"cascade_id":  cascadeID,
		"echo":        echo,
		"accepted_at": ts,
	}, nil
}

// validateDeepLinkTargets unifies the topCascadeID /
// dl.CascadeID override path (the dashboard can pass
// either; the former wins) and the non-empty trim
// for both cascade_id and text. Extracted from
// handleDeepLinkPasteAndSend to keep that handler
// under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func validateDeepLinkTargets(topCascadeID string, dl DeepLink) (string, string, *rpcErrorBody) {
	cascadeID := firstNonEmpty(strings.TrimSpace(topCascadeID), dl.CascadeID)
	if cascadeID == "" {
		return "", "", &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascade_id is required"}
	}
	text := strings.TrimSpace(dl.Text)
	if text == "" {
		return "", "", &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "text is required"}
	}
	return cascadeID, text, nil
}

// lookupOpenCascadeForDeepLink returns the live
// cascade or the appropriate -32004 / -32005
// envelope. The deep-link handler treats a
// closed-cascade arrival as a -32005 surface (the
// same envelope SendRemoteAgentCascadeMessage would
// emit) rather than -32004 / "unknown id", because
// the dashboard opened and the cascade was closed
// mid-flight rather than the id being wrong.
//
// Extracted from handleDeepLinkPasteAndSend to keep
// the handler under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func (d *Dispatcher) lookupOpenCascadeForDeepLink(cascadeID string) (*Cascade, *rpcErrorBody) {
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}
	}
	if c.IsClosed() {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeClosed, Message: "Cascade closed", Data: cascadeID}
	}
	return c, nil
}

// runDeepLinkAsync drives the goroutine the deep-link
// handler spawns. The goroutine's failures surface via
// the sink (cascade/state{phase:"error"}) and via the
// cascade's bounded error ring; the RPC reply has
// already landed with accepted:true, so this helper
// does not surface goroutine errors as a second
// JSON-RPC envelope.
func (d *Dispatcher) runDeepLinkAsync(cascadeID string, prompt MessagePrompt, c *Cascade, connID string) {
	sink := NewConnNotifierSink(d, connID, c)
	sendCtx := context.Background()
	_, _ = d.cascades.SendStream(sendCtx, cascadeID, prompt, sink, MessageSendOptions{})
}
