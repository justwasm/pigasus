package codec

import (
	"encoding/json"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

// Completion is the decoded output of a unary bridge response. Error retains
// the original typed stream error; the host decides whether partial output or
// that error should be returned to the client.
type Completion struct {
	Text      string
	Thinking  string
	ToolCalls []geminicode.TurnToolCall
	Meta      geminicode.Meta
	Error     error
}

// Accumulator converts a sequence of CloudCode chunks into unary output.
// The zero value is ready to use; readers and stream termination stay in the host.
type Accumulator struct {
	text       strings.Builder
	thinking   strings.Builder
	calls      []geminicode.TurnToolCall
	meta       geminicode.Meta
	lastError  error
	inThinking bool
}

// Feed consumes one decoded chunk without wrapping or erasing typed errors.
func (a *Accumulator) Feed(c geminicode.Chunk) {
	if c.Error != nil {
		a.lastError = c.Error
		return
	}
	text := c.Text
	if text == "<think>" {
		a.inThinking = true
		return
	}
	if text == "</think>" {
		a.inThinking = false
		return
	}
	if strings.HasPrefix(text, "<think>") {
		a.inThinking = true
		text = strings.TrimPrefix(text, "<think>")
	}
	if strings.HasSuffix(text, "</think>") {
		a.inThinking = false
		text = strings.TrimSuffix(text, "</think>")
	}
	if text != "" {
		if a.inThinking {
			a.thinking.WriteString(text)
		} else {
			a.text.WriteString(text)
		}
	}
	if len(c.ToolCalls) > 0 {
		a.calls = append(a.calls, c.ToolCalls...)
	}
	if c.Meta != nil {
		a.meta = *c.Meta
	}
}

// Completion returns the accumulated output without deciding transport success.
func (a *Accumulator) Completion() Completion {
	return Completion{Text: a.text.String(), Thinking: a.thinking.String(), ToolCalls: a.calls, Meta: a.meta, Error: a.lastError}
}

// OpenAICompletion assembles a unary Chat Completions response from decoded
// CloudCode output. created and id are supplied by the transport host.
func OpenAICompletion(id, model string, created int64, text, thinking string, calls []geminicode.TurnToolCall, meta geminicode.Meta) map[string]any {
	message := map[string]any{"role": "assistant", "content": text}
	if thinking != "" {
		message["reasoning_content"] = thinking
	}
	finishReason := "stop"
	if len(calls) > 0 {
		finishReason = "tool_calls"
		toolCalls := make([]map[string]any, 0, len(calls))
		for _, call := range calls {
			toolCalls = append(toolCalls, map[string]any{
				"id": call.ID, "type": "function",
				"function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)},
			})
		}
		message["tool_calls"] = toolCalls
	}
	return map[string]any{
		"id": id, "object": "chat.completion", "created": created, "model": model,
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finishReason}},
		"usage":   map[string]any{"prompt_tokens": meta.PromptTokens, "completion_tokens": meta.CompletionTokens, "total_tokens": meta.TotalTokens},
	}
}

// AnthropicMessageResponse assembles a unary Anthropic Messages response.
func AnthropicMessageResponse(id, model string, text, thinking string, calls []geminicode.TurnToolCall, meta geminicode.Meta) map[string]any {
	var blocks []map[string]any
	if thinking != "" {
		blocks = append(blocks, map[string]any{"type": "thinking", "thinking": thinking})
	}
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, call := range calls {
		var input any
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			input = map[string]any{}
		}
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
	}
	stopReason := "end_turn"
	if len(calls) > 0 {
		stopReason = "tool_use"
	}
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model,
		"content": blocks, "stop_reason": stopReason, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": meta.PromptTokens, "output_tokens": meta.CompletionTokens},
	}
}
