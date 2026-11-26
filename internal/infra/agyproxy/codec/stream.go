package codec

import (
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

// OpenAIStream converts decoded CloudCode chunks to Chat Completions chunks.
// It owns protocol state only: the host supplies identity/time and owns reads,
// cancellation, heartbeats, errors' side effects, and the final SSE [DONE].
type OpenAIStream struct {
	id         string
	model      string
	created    int64
	inThinking bool
	hasCalls   bool
}

// NewOpenAIStream creates an isolated converter for one response.
func NewOpenAIStream(id, model string, created int64) *OpenAIStream {
	return &OpenAIStream{id: id, model: model, created: created}
}

func (s *OpenAIStream) chunk(delta map[string]any, reason any, usage any) map[string]any {
	out := map[string]any{
		"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": reason}},
	}
	if usage != nil {
		out["usage"] = usage
	}
	return out
}

// Feed maps one decoded chunk. Error values are inspected only to encode the
// existing wire error marker; the caller retains the original typed error.
func (s *OpenAIStream) Feed(c geminicode.Chunk) []map[string]any {
	if c.Error != nil {
		return []map[string]any{s.chunk(map[string]any{}, "error", nil)}
	}
	text := c.Text
	if text == "<think>" {
		s.inThinking = true
		return nil
	}
	if text == "</think>" {
		s.inThinking = false
		return nil
	}
	if strings.HasPrefix(text, "<think>") {
		s.inThinking = true
		text = strings.TrimPrefix(text, "<think>")
	}
	if strings.HasSuffix(text, "</think>") {
		s.inThinking = false
		text = strings.TrimSuffix(text, "</think>")
	}
	var out []map[string]any
	if text != "" {
		key := "content"
		if s.inThinking {
			key = "reasoning_content"
		}
		out = append(out, s.chunk(map[string]any{key: text}, nil, nil))
	}
	if len(c.ToolCalls) > 0 {
		s.hasCalls = true
		var calls []map[string]any
		for index, call := range c.ToolCalls {
			calls = append(calls, map[string]any{
				"index": index, "id": call.ID, "type": "function",
				"function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)},
			})
		}
		out = append(out, s.chunk(map[string]any{"tool_calls": calls}, nil, nil))
	}
	if c.Done {
		reason := "stop"
		if s.hasCalls {
			reason = "tool_calls"
		}
		var usage any
		if c.Meta != nil {
			usage = map[string]any{"prompt_tokens": c.Meta.PromptTokens, "completion_tokens": c.Meta.CompletionTokens, "total_tokens": c.Meta.TotalTokens}
		}
		out = append(out, s.chunk(map[string]any{}, reason, usage))
	}
	return out
}

// Event is one Anthropic SSE event before transport framing.
type Event struct {
	Name    string
	Payload map[string]any
}

// AnthropicStream owns Messages API content-block ordering and indices, not
// transport lifecycle. Start/Feed/Finish encode messages at the host's request.
type AnthropicStream struct {
	inThinking        bool
	blockIndex        int
	activeBlockType   string
	finalOutputTokens int
}

// Start returns the initial Messages API event for the supplied response IDs.
func (s *AnthropicStream) Start(id, model string) Event {
	return Event{Name: "message_start", Payload: map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
		},
	}}
}

func (s *AnthropicStream) closeBlock() []Event {
	if s.activeBlockType == "" {
		return nil
	}
	event := Event{Name: "content_block_stop", Payload: map[string]any{"type": "content_block_stop", "index": s.blockIndex}}
	s.blockIndex++
	s.activeBlockType = ""
	return []Event{event}
}

// Feed maps a decoded CloudCode chunk while preserving the original error for
// the host's account/failure handling. It never performs I/O or policy actions.
func (s *AnthropicStream) Feed(c geminicode.Chunk) []Event {
	if c.Error != nil {
		return []Event{{Name: "error", Payload: map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": c.Error.Error()}}}}
	}
	var out []Event
	text := c.Text
	if text == "<think>" {
		out = append(out, s.closeBlock()...)
		s.inThinking = true
		return out
	}
	if text == "</think>" {
		out = append(out, s.closeBlock()...)
		s.inThinking = false
		return out
	}
	if strings.HasPrefix(text, "<think>") {
		out = append(out, s.closeBlock()...)
		s.inThinking = true
		text = strings.TrimPrefix(text, "<think>")
	}
	if strings.HasSuffix(text, "</think>") {
		out = append(out, s.closeBlock()...)
		s.inThinking = false
		text = strings.TrimSuffix(text, "</think>")
	}
	if text != "" {
		desiredType := "text"
		if s.inThinking {
			desiredType = "thinking"
		}
		if s.activeBlockType != desiredType {
			out = append(out, s.closeBlock()...)
			s.activeBlockType = desiredType
			out = append(out, Event{Name: "content_block_start", Payload: map[string]any{
				"type": "content_block_start", "index": s.blockIndex,
				"content_block": map[string]any{"type": desiredType, desiredType: ""},
			}})
		}
		out = append(out, Event{Name: "content_block_delta", Payload: map[string]any{
			"type": "content_block_delta", "index": s.blockIndex,
			"delta": map[string]any{"type": desiredType + "_delta", desiredType: text},
		}})
	}
	if len(c.ToolCalls) > 0 {
		out = append(out, s.closeBlock()...)
		for _, call := range c.ToolCalls {
			out = append(out,
				Event{Name: "content_block_start", Payload: map[string]any{
					"type": "content_block_start", "index": s.blockIndex,
					"content_block": map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": map[string]any{}},
				}},
				Event{Name: "content_block_delta", Payload: map[string]any{
					"type": "content_block_delta", "index": s.blockIndex,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": string(call.Arguments)},
				}},
				Event{Name: "content_block_stop", Payload: map[string]any{"type": "content_block_stop", "index": s.blockIndex}},
			)
			s.blockIndex++
		}
	}
	if c.Meta != nil {
		s.finalOutputTokens = c.Meta.CompletionTokens
	}
	return out
}

// Finish encodes final block closure and message usage. The host decides when a
// stream has ended and whether cancellation should suppress these events.
// hasCalls includes calls buffered by the CloudCode decoder, even if an error
// or EOF prevented their terminating chunk from being emitted.
func (s *AnthropicStream) Finish(hasCalls bool) []Event {
	out := s.closeBlock()
	reason := "end_turn"
	if hasCalls {
		reason = "tool_use"
	}
	return append(out,
		Event{Name: "message_delta", Payload: map[string]any{
			"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": s.finalOutputTokens},
		}},
		Event{Name: "message_stop", Payload: map[string]any{"type": "message_stop"}},
	)
}
