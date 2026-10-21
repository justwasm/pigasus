// Package remotecontrol — streaming_sink.go:
// ConnNotifierSink method implementations (OnState /
// OnTextDelta / OnToolCall / OnError / OnUserInteraction).
// Pulled out of streaming.go in cycle
// rc-filesize-internal-remotecontrol.
package remotecontrol

import (
	"context"
	"encoding/json"
	"time"
)

// OnState emits a cascade/state notification and, when phase
// is "done", stamps the cascade's LastReplyAt via recordReply.
// The cascade parameter is accepted for interface symmetry
// (the brief's signature); the captured cascade is what the
// sink actually mutates.
//
// Param shape (matches the brief's wire sketch):
//
//	{
//	  "cascade_id":  "cas_<hex>",
//	  "phase":       "starting" | "working" | "done" | "error" | "cancelled",
//	  "ts":          "<rfc3339-nano>",
//	  "stopReason":  "<...>",   // phase == "done"
//	  "durationMs":  <int>,     // phase == "done", computed from sink startedAt
//	  "totalTokens": <int>,     // phase == "done", omitted when zero
//	  "error":       "<msg>",   // phase == "error" / "cancelled"
//	}
//
// The dispatcher's Notify path is best-effort: a slow client
// whose ws Writer blocks times out internally and returns an
// error; the sink drops the error. See package doc for the
// back-pressure rationale.
//
// Cycle rc-cancel-cascade: the "cancelled" phase is
// emitted by the dispatcher's CancelRemoteAgentCascade
// handler (not by the sink — the cancel flow is
// RPC-driven, not sink-driven). LastReplyAt is left
// untouched (a cancelled turn is not a successful reply,
// matching the OnError rule).
func (s *ConnNotifierSink) OnState(cascade *Cascade, frame StateFrame) {
	if s == nil || s.cascade == nil || s.disp == nil {
		return
	}
	params := buildStateFrameParams(s.cascade.ID, frame, s.startedAt)
	_ = s.disp.Notify(context.Background(), s.connID, NotifCascadeState, params)

	// Side effect: stamp LastReplyAt when the turn completes
	// successfully. We pass empty newConvID so recordReply's
	// "only update ConversationID when non-empty" rule keeps
	// the existing value (the sink doesn't see the new id —
	// that's the registry.Send path's job; for streaming,
	// the backend returns newConvID via the SendStream
	// return value and the dispatcher's success path stamps
	// it on the cascade the same way the sync path does).
	if frame.Phase == StatePhaseDone {
		s.cascade.recordReply("", time.Now().UTC())
	}
}

// buildStateFrameParams renders the cascade/state
// notification params map. Pulled out of OnState so the
// per-field conditional stays visually compact in the
// hot path.
func buildStateFrameParams(cascadeID string, frame StateFrame, startedAt time.Time) map[string]any {
	params := map[string]any{
		"cascade_id": cascadeID,
		"phase":      frame.Phase,
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
	}
	if frame.StopReason != "" {
		params["stopReason"] = frame.StopReason
	}
	if frame.Phase == StatePhaseDone && !startedAt.IsZero() {
		if d := time.Since(startedAt).Milliseconds(); d > 0 {
			params["durationMs"] = d
		}
	}
	if frame.TotalTokens > 0 {
		params["totalTokens"] = frame.TotalTokens
	}
	if frame.Error != "" {
		params["error"] = frame.Error
	}
	return params
}

// OnTextDelta emits a cascade/text_delta notification and
// appends delta to the cascade's LastReply via appendReplyText.
// The cascade parameter is accepted for interface symmetry;
// the captured cascade is what the sink mutates.
//
// Param shape:
//
//	{
//	  "cascade_id": "cas_<hex>",
//	  "delta":      "<chunk>"
//	}
func (s *ConnNotifierSink) OnTextDelta(cascade *Cascade, delta string) {
	if s == nil || s.cascade == nil || s.disp == nil {
		return
	}
	if delta == "" {
		return
	}
	params := map[string]any{
		"cascade_id": s.cascade.ID,
		"delta":      delta,
	}
	_ = s.disp.Notify(context.Background(), s.connID, NotifCascadeTextDelta, params)

	// Side effect: accumulate the chunk into LastReply. Tests
	// inspect this after a streaming turn to verify that the
	// OnTextDelta path actually mutated the cascade (the
	// brief's "forward LastReply" rule).
	s.cascade.appendReplyText(delta)
}

// OnToolCall emits a cascade/tool_call notification. The
// cascade parameter is accepted for interface symmetry; the
// captured cascade is what the sink uses for the cascade_id
// field in the params.
//
// Param shape:
//
//	{
//	  "cascade_id": "cas_<hex>",
//	  "name":       "<tool>",
//	  "args":       { ... } | null,
//	  "result":     "<string>",   // optional, omitted when empty
//	  "ok":         <bool>
//	}
//
// Note: args is the raw JSON object the model emitted. We
// preserve it as json.RawMessage so the dashboard sees the
// same shape the model produced; an empty/missing args is
// emitted as JSON null, matching the cycle-3a toolCalls
// envelope rule.
func (s *ConnNotifierSink) OnToolCall(cascade *Cascade, call ToolCall) {
	if s == nil || s.cascade == nil || s.disp == nil {
		return
	}
	params := buildToolCallParams(s.cascade.ID, call)
	_ = s.disp.Notify(context.Background(), s.connID, NotifCascadeToolCall, params)
}

// buildToolCallParams renders the cascade/tool_call
// notification params map. Pulled out of OnToolCall so
// the args-vs-null conditional stays visually compact
// in the hot path.
func buildToolCallParams(cascadeID string, call ToolCall) map[string]any {
	params := map[string]any{
		"cascade_id": cascadeID,
		"name":       call.Name,
		"ok":         call.OK,
	}
	if len(call.Args) > 0 {
		params["args"] = json.RawMessage(call.Args)
	} else {
		params["args"] = nil
	}
	if call.Result != "" {
		params["result"] = call.Result
	}
	return params
}

// OnError emits a StateFrame{Phase: "error"} notification and
// surfaces the error to the cascade's side of the world. We
// deliberately do NOT mutate the cascade's LastReplyAt (the
// brief: an error is not a successful reply). The dispatcher's
// Send…Message handler maps the underlying error to a JSON-RPC
// -32603 envelope (cycle-3a's rule); the OnError notification
// is informational, for the dashboard to surface the failure
// before the synchronous response arrives.
//
// Param shape:
//
//	{
//	  "cascade_id": "cas_<hex>",
//	  "phase":      "error",
//	  "error":      "<err.Error()>",
//	  "ts":         "<rfc3339-nano>"
//	}
func (s *ConnNotifierSink) OnError(cascade *Cascade, err error) {
	if s == nil || s.cascade == nil || s.disp == nil || err == nil {
		return
	}
	params := map[string]any{
		"cascade_id": s.cascade.ID,
		"phase":      StatePhaseError,
		"error":      err.Error(),
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
	}
	_ = s.disp.Notify(context.Background(), s.connID, NotifCascadeState, params)
}

// OnUserInteraction adds intr to the cascade's pending-
// interaction registry (lazily allocated) and emits a
// cascade/user_interaction_required notification carrying
// the prompt metadata. The notification's params:
//
//	{
//	  "cascade_id":     "cas_<hex>",
//	  "interaction_id": "int_<12 hex>",
//	  "kind":           "permission" | "question" | "confirmation" | "choice",
//	  "title":          "<human summary>",
//	  "prompt":         "<the question>",
//	  "options":        [ {label,value,description}, ... ],   // optional, omitted when empty
//	  "dangerous":      true,                                  // optional, kind == permission
//	  "expires_at":     "<rfc3339>"                            // optional, omitted when zero
//	}
//
// The dashboard sees the notification, asks the user, and
// calls HandleRemoteAgentCascadeUserInteraction with the
// same interaction_id + a response payload. The
// dispatcher's handler looks up intr via
// (*Cascade).Pending().Get(interaction_id) and pushes the
// response onto intr.ResultCh; the backend — parked on
// ResultCh — wakes up and continues the turn.
//
// Nil-safety: a nil sink is a no-op; a sink with a nil
// captured cascade is a no-op; a sink without a registered
// dispatcher is a no-op. A nil intr is a no-op (the brief:
// no spurious notifications).
//
// Why the registry lives on the cascade (not the sink):
// the cascade is the lifecycle-scoped value the dispatcher
// resolves by id on HandleRemoteAgentCascadeUserInteraction.
// Putting the registry on the sink would force the
// dispatcher to thread the sink through to the handler
// (cleaner architecturally but more wiring); putting it on
// the cascade means the handler does a single Get(id) on
// *Cascade.Pending(), which is the model the cycle brief
// pins.
//
// Cycle rc-handle-user-interaction adds this method.
func (s *ConnNotifierSink) OnUserInteraction(cascade *Cascade, intr *PendingInteraction) {
	if s == nil || s.cascade == nil || s.disp == nil || intr == nil {
		return
	}
	// Register the interaction on the cascade's pending
	// registry. AddInteraction mints a fresh ID + ResultCh,
	// but the cycle's brief asks the SINK (not the backend)
	// to do the registration — the backend's job is just to
	// invoke sink.OnUserInteraction with a populated prompt;
	// the dispatcher-side seam is what turns it into a
	// registry entry + notification. The cascade we register
	// against is s.cascade (the captured one) so the entry
	// lives under the right lifecycle scope even when a
	// future multi-cascade sink passes a different cascade
	// param.
	s.cascade.Pending().Add(intr)

	params := buildUserInteractionParams(s.cascade.ID, intr)
	_ = s.disp.Notify(context.Background(), s.connID, NotifUserInteractionRequired, params)
}

// buildUserInteractionParams renders the
// cascade/user_interaction_required notification params
// map. Pulled out of OnUserInteraction so the optional
// fields (options / dangerous / expires_at) stay visually
// compact and the per-field conditional can be audited
// without paging through the registry-add side effect.
func buildUserInteractionParams(cascadeID string, intr *PendingInteraction) map[string]any {
	params := map[string]any{
		"cascade_id":     cascadeID,
		"interaction_id": intr.ID,
		"kind":           intr.Kind,
		"title":          intr.Title,
		"prompt":         intr.Prompt,
	}
	if len(intr.Options) > 0 {
		params["options"] = intr.Options
	}
	if intr.Dangerous {
		params["dangerous"] = true
	}
	if !intr.ExpiresAt.IsZero() {
		params["expires_at"] = intr.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return params
}
