package codec_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/infra/agyproxy/codec"
)

func TestOpenAIStream_ThinkingToolAndErrorBoundaries(t *testing.T) {
	t.Run("named fixture keeps thinking and tool stop reason", func(t *testing.T) {
		stream := codec.NewOpenAIStream("chunk-id", "gemini-test", 42)
		if chunks := stream.Feed(geminicode.Chunk{Text: "<think>"}); len(chunks) != 0 {
			t.Fatalf("marker emitted: %#v", chunks)
		}
		thinking := stream.Feed(geminicode.Chunk{Text: "reason"})
		text := stream.Feed(geminicode.Chunk{Text: "</think>"})
		text = append(text, stream.Feed(geminicode.Chunk{Text: "answer"})...)
		tool := stream.Feed(geminicode.Chunk{ToolCalls: []geminicode.TurnToolCall{{ID: "call-1", Name: "search", Arguments: json.RawMessage(`{"q":"x"}`)}}})
		final := stream.Feed(geminicode.Chunk{Done: true, Meta: &geminicode.Meta{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}})
		if len(thinking) != 1 || thinking[0]["choices"].([]map[string]any)[0]["delta"].(map[string]any)["reasoning_content"] != "reason" {
			t.Fatalf("thinking chunk = %#v", thinking)
		}
		if len(text) != 1 || text[0]["choices"].([]map[string]any)[0]["delta"].(map[string]any)["content"] != "answer" {
			t.Fatalf("text chunk = %#v", text)
		}
		if len(tool) != 1 || tool[0]["choices"].([]map[string]any)[0]["delta"].(map[string]any)["tool_calls"].([]map[string]any)[0]["function"].(map[string]any)["name"] != "search" {
			t.Fatalf("tool chunk = %#v", tool)
		}
		end := final[0]["choices"].([]map[string]any)[0]
		if end["finish_reason"] != "tool_calls" || final[0]["usage"].(map[string]any)["total_tokens"] != 6 {
			t.Fatalf("final chunk = %#v", final)
		}
		if final[0]["id"] != "chunk-id" || final[0]["created"] != int64(42) {
			t.Fatalf("chunk metadata = %#v", final[0])
		}
	})

	t.Run("typed stream error remains available to caller", func(t *testing.T) {
		stream := codec.NewOpenAIStream("id", "model", 1)
		typed := &geminicode.StreamError{Code: 429, Status: "RESOURCE_EXHAUSTED", Message: "quota"}
		c := geminicode.Chunk{Error: typed}
		chunks := stream.Feed(c)
		if c.Error != typed {
			t.Fatal("codec changed the typed error passed by caller")
		}
		got := chunks[0]["choices"].([]map[string]any)[0]["finish_reason"]
		if got != "error" {
			t.Fatalf("error boundary = %#v", chunks)
		}
	})
}

func TestAnthropicStream_OrderedBlocksAndFinish(t *testing.T) {
	stream := &codec.AnthropicStream{}
	start := stream.Start("msg-fixture", "claude-test")
	if start.Name != "message_start" || start.Payload["message"].(map[string]any)["id"] != "msg-fixture" {
		t.Fatalf("start event = %+v", start)
	}
	thinking := stream.Feed(geminicode.Chunk{Text: "<think>"})
	thinking = append(thinking, stream.Feed(geminicode.Chunk{Text: "reason"})...)
	thinking = append(thinking, stream.Feed(geminicode.Chunk{Text: "</think>"})...)
	text := stream.Feed(geminicode.Chunk{Text: "answer"})
	tool := stream.Feed(geminicode.Chunk{ToolCalls: []geminicode.TurnToolCall{{ID: "call-2", Name: "edit", Arguments: json.RawMessage(`{"x":1}`)}}})
	final := stream.Finish(true)
	if len(thinking) != 3 || thinking[0].Name != "content_block_start" || thinking[1].Payload["delta"].(map[string]any)["thinking"] != "reason" || thinking[2].Name != "content_block_stop" {
		t.Fatalf("thinking sequence = %+v", thinking)
	}
	if len(text) != 2 || text[0].Name != "content_block_start" || text[1].Payload["index"] != 1 {
		t.Fatalf("text block = %+v", text)
	}
	if len(tool) != 4 || tool[0].Name != "content_block_stop" || tool[1].Name != "content_block_start" || tool[1].Payload["index"] != 2 {
		t.Fatalf("tool sequence = %+v", tool)
	}
	if len(final) != 2 || final[0].Payload["delta"].(map[string]any)["stop_reason"] != "tool_use" || final[1].Name != "message_stop" {
		t.Fatalf("finish sequence = %+v", final)
	}
}

func TestAnthropicStream_ErrorPayloadAndRepeatedFinish(t *testing.T) {
	stream := &codec.AnthropicStream{}
	typed := &geminicode.StreamError{Code: 502, Status: "UNAVAILABLE", Message: "upstream"}
	chunk := geminicode.Chunk{Error: typed}
	events := stream.Feed(chunk)
	if chunk.Error != typed || len(events) != 1 || events[0].Name != "error" || events[0].Payload["error"].(map[string]any)["message"] != "geminicode: stream error 502: upstream" {
		t.Fatalf("error mapping = %#v / %#v", chunk, events)
	}
	first := stream.Finish(false)
	second := stream.Finish(false)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("finish must remain deterministic: first=%+v second=%+v", first, second)
	}
}
