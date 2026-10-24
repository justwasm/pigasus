package agent

import (
	"encoding/json"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// pigMessages converts the clone's conversation into PiG's transcript.
//
// Role mapping is the inverse of piagentbridge.ToStreamRequest: the clone's
// flat Message becomes one PiG message union arm, with an assistant turn's
// ToolCalls becoming ToolCall content blocks and a `tool` turn becoming a
// ToolResultMessage carrying ToolCallID + Name.
//
// An empty conversation gets one empty user message. PiG's SendMessages is a
// documented no-op on an empty slice (agent.go:1126) because upstream's
// prompt(messages) always has a message to start the turn from, whereas the
// clone's loop would happily issue a request with no messages at all. The
// synthesized turn is the narrowest thing that still starts exactly one
// upstream request; every real caller hands over a populated conversation.
func pigMessages(messages []backend.Message) []piagent.AgentMessage {
	out := make([]piagent.AgentMessage, 0, len(messages))
	now := time.Now().UnixMilli()
	for _, message := range messages {
		if agentMsg, ok := convertMessage(message, now); ok {
			out = append(out, agentMsg)
		}
	}
	if len(out) == 0 {
		out = append(out, piagent.AgentMessage{User: &piagent.UserMessage{
			Role:      piagent.RoleUser,
			Content:   pi.UserText(""),
			Timestamp: now,
		}})
	}
	return out
}

func convertMessage(message backend.Message, now int64) (piagent.AgentMessage, bool) {
	switch message.Role {
	case "user":
		return piagent.AgentMessage{User: &piagent.UserMessage{
			Role:      piagent.RoleUser,
			Content:   pi.UserText(message.Content),
			Timestamp: now,
		}}, true
	case "assistant":
		return convertAssistantMessage(message, now), true
	case "tool":
		return piagent.AgentMessage{ToolResult: &piagent.ToolResultMessage{
			Role:       piagent.RoleToolResult,
			ToolCallID: message.ToolCallID,
			ToolName:   message.Name,
			Content:    []pi.ToolResultMessageContent{pi.TextContent{Text: message.Content}},
			Timestamp:  now,
		}}, true
	case "system":
		return piagent.AgentMessage{System: &pi.SystemMessage{
			Content:   pi.SystemText(message.Content),
			Timestamp: now,
		}}, true
	default:
		return piagent.AgentMessage{}, false
	}
}

func convertAssistantMessage(message backend.Message, now int64) piagent.AgentMessage {
	blocks := make([]pi.AssistantContentBlock, 0, len(message.ToolCalls)+1)
	if message.Content != "" {
		blocks = append(blocks, pi.TextContent{Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, pi.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: decodeToolArguments(call.Arguments),
		})
	}
	return piagent.AgentMessage{Assistant: &piagent.AssistantMessage{
		Role:      piagent.RoleAssistant,
		Content:   blocks,
		Timestamp: now,
	}}
}

func decodeToolArguments(raw json.RawMessage) pi.JsonObject {
	if len(raw) == 0 {
		return pi.JsonObject{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return pi.JsonObject{}
	}
	return pi.JsonObject(decoded)
}
