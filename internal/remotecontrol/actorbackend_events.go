// Package remotecontrol — actorbackend_events.go:
// the event-shaped conversion helpers for the
// ActorBackend bridge.
package remotecontrol

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

func mapStopReason(r string) string {
	switch strings.ToLower(r) {
	case "stop", "end_turn":
		return cascades.StopReasonEndTurn
	case "tooluse", "tool_use":
		return cascades.StopReasonToolUse
	case "length", "max_tokens":
		return cascades.StopReasonMaxTokens
	case "error", "aborted":
		return cascades.StopReasonError
	}
	return cascades.StopReasonEndTurn
}

func actorTurnFailure(msgs []SessionMessage) error {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.StopReason == "error" || m.StopReason == "aborted" || m.ErrorMessage != "" {
			text := m.ErrorMessage
			if text == "" {
				text = "actor: upstream stream failed"
			}
			return errors.New(text)
		}
	}
	return nil
}

func extractAssistantText(msgs []SessionMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.StopReason == "error" || m.StopReason == "aborted" {
			continue
		}
		if m.Content != "" {
			return m.Content
		}
	}
	return ""
}

func actorToolCallFromRaw(name string, args json.RawMessage) cascades.ToolCall {
	raw := []byte(args)
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return cascades.ToolCall{
		Name:   name,
		Args:   raw,
		Result: "",
		OK:     true,
	}
}
