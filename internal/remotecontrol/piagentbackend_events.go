// Package remotecontrol — piagentbackend_events.go:
// the event-shaped conversion helpers for the
// PiagentBackend bridge. Pulled out of piagentbackend.go
// in cycle rc-filesize-internal-remotecontrol.
//
// The three helpers here are:
//
//   - piagentToolCall    — converts PiG's ai.ToolCall
//     into the cycle's ToolCall wire shape.
//   - stopReasonFromPiG  — maps PiG's ai.StopReason into
//     the cycle's StopReason* constants.
//   - piagentTurnFailure — recovers an upstream failure
//     when PiG returns success but encoded the error
//     into the assistant message.
package remotecontrol

import (
	"encoding/json"
	"errors"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// piagentToolCall converts PiG's `ai.ToolCall` (the block
// inside `assistant.Content[]`) into the cycle's
// `ToolCall` shape. The two are close but not identical:
//
//   - PiG's call carries `ID`, `Name`, and `Arguments`
//     (a JsonObject). The cycle's ToolCall carries
//     `Name`, `Args` (json.RawMessage), `Result`, and `OK`.
//   - PiG's ID is opaque to the dashboard; we drop it.
//   - PiG's Arguments is the raw JSON object the model
//     emitted. We serialise back to bytes so the wire
//     surface is `json.RawMessage` (the cycle's
//     `ConnNotifierSink` reads it verbatim).
//   - `Result` is empty for the cycle's wiring today:
//     PiG's session executes tools internally and emits
//     `ToolCallEndEvent` with the call only; a future
//     cycle that wants the tool result string can read
//     the post-call assistant message's `ToolResultMessage`
//     or extend the bridge with a side-channel.
//   - `OK` defaults to true: the cycle-5 `ToolCall.OK`
//     is the call's success flag, and PiG emits the
//     end event after the tool has run; failures carry
//     a separate `ToolResultMessage` that we don't
//     capture here today. A future cycle that wants
//     OK=false propagation should extend the bridge.
func piagentToolCall(call ai.ToolCall) ToolCall {
	raw, err := json.Marshal(call.Arguments)
	if err != nil || len(raw) == 0 {
		raw = []byte("{}")
	}
	return ToolCall{
		Name:   call.Name,
		Args:   raw,
		Result: "",   // see doc; populated by future cycle
		OK:     true, // see doc; populated by future cycle
	}
}

// stopReasonFromPiG maps PiG's stop reasons into the cycle's
// vocabulary (StopReason* constants in backend.go). PiG's
// `ai.StopReason` string type exposes (upstream types.ts:343):
//
//   - StopReasonStop     ("stop")     → end_turn
//   - StopReasonLength   ("length")   → max_tokens (the cycle's
//     wire vocabulary; PiG has
//     no separate MaxTokens
//     constant)
//   - StopReasonToolUse  ("toolUse")  → tool_use
//   - StopReasonError    ("error")    → error
//   - StopReasonAborted  ("aborted")  → error (cycle's collapsed
//     surface; future cycles
//     may introduce a distinct
//     StopReasonAborted
//     constant)
//   - StopReasonPending / StopReasonDeferred / unknown → end_turn
//     so a future PiG reason doesn't crash the dispatcher.
func stopReasonFromPiG(r ai.StopReason) string {
	switch r {
	case ai.StopReasonStop:
		return StopReasonEndTurn
	case ai.StopReasonToolUse:
		return StopReasonToolUse
	case ai.StopReasonLength:
		return StopReasonMaxTokens
	case ai.StopReasonError, ai.StopReasonAborted:
		return StopReasonError
	}
	return StopReasonEndTurn
}

// piagentTurnFailure mirrors pigrt.turnFailure: when PiG
// reports success but encoded the failure into the
// assistant message, recover the inner error so the
// dispatcher's errors.As logic still matches. The agent
// loop returns a nil Go error in this case (agent_loop.go
// copies only err.Error() across the stream boundary), so
// the cycle must walk the messages to recover.
func piagentTurnFailure(msgs []agent.AgentMessage) error {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i].Assistant
		if m == nil {
			continue
		}
		switch m.StopReason {
		case ai.StopReasonError, ai.StopReasonAborted:
			text := m.ErrorMessage
			if text == "" {
				text = "piagent: upstream stream failed"
			}
			return errors.New(text)
		default:
			return nil
		}
	}
	return nil
}

// extractAssistantText joins the TextContent blocks of assistant messages.
func extractAssistantText(msgs []agent.AgentMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i].Assistant
		if m == nil {
			continue
		}
		if m.StopReason == ai.StopReasonError || m.StopReason == ai.StopReasonAborted {
			continue
		}
		var out string
		for _, block := range m.Content {
			if text, ok := block.(ai.TextContent); ok {
				out += text.Text
			}
		}
		if out != "" {
			return out
		}
	}
	return ""
}
