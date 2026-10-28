package agent

import (
	"encoding/base64"
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
			Content:   userContent(message),
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

// userContent renders a user message's content for PiG.
//
// A message with no picture stays the union's plain-text arm, so a
// text-only turn is byte for byte what it was before the clone learned to
// attach images. A message that carries pictures becomes a block list: the
// text first, then one image block per picture.
//
// That is the shape PiG's own BuildUserContent builds (coding/session.go)
// and the shape the transports read back — piagentbridge.userImages
// (CloudCode, Gemini, Anthropic) and piagentbridge.userContentParts (the
// gateway's OpenAI-shaped array) both read the image blocks and nothing
// else. The picture's bytes travel base64-encoded, because PiG's image
// block holds the payload as a string.
//
// An image with no bytes is dropped rather than sent as an empty picture:
// the model would answer a broken image with a guess.
func userContent(message backend.Message) pi.UserContent {
	if len(message.Images) == 0 {
		return pi.UserText(message.Content)
	}
	blocks := make(pi.UserContentBlocks, 0, len(message.Images)+1)
	blocks = append(blocks, pi.TextContent{Text: message.Content})
	for _, image := range message.Images {
		if len(image.Data) == 0 {
			continue
		}
		blocks = append(blocks, pi.ImageContent{
			Data:     base64.StdEncoding.EncodeToString(image.Data),
			MimeType: image.MIMEType,
		})
	}
	if len(blocks) == 1 {
		// Every picture was empty — the turn carries text and nothing else,
		// so it stays the plain-text arm.
		return pi.UserText(message.Content)
	}
	return blocks
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
