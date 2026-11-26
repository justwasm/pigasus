package codec_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/infra/agyproxy/codec"
)

const openAIChatFixture = `{
  "model": "openai/gpt-4o",
  "messages": [
    {"role":"system","content":"Be concise."},
    {"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]},
    {"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]},
    {"role":"tool","tool_call_id":"call-1","content":"contents"}
  ],
  "tools": [{"type":"function","function":{"name":"read_file","description":"read a file","parameters":{"type":"object"}}},{"type":"computer","function":{"name":"ignored"}}],
  "temperature":0.25,
  "max_tokens":123,
  "reasoning_effort":"MEDIUM"
}`

func TestOpenAIToTurn_ChatFixtureWithToolRoundTrip(t *testing.T) {
	t.Run("chat fixture maps aliases media and tool names", func(t *testing.T) {
		var request codec.OpenAIChatRequest
		if err := json.Unmarshal([]byte(openAIChatFixture), &request); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		turn := codec.OpenAIToTurn(request, codec.Media{}, "session-fixture", "request-fixture")
		if turn.Model != "gemini-3.8-flash-high" || turn.SystemPrompt != "Be concise." {
			t.Fatalf("model/system = %q/%q", turn.Model, turn.SystemPrompt)
		}
		if turn.SessionID != "session-fixture" || turn.RequestID != "request-fixture" || turn.Project != geminicode.DefaultProject {
			t.Fatalf("host-provided turn metadata not preserved: %+v", turn)
		}
		if turn.Temperature != 0.25 || turn.MaxOutputTokens != 123 || turn.ThinkingLevel != "medium" {
			t.Fatalf("generation settings = temp %v, max %d, thinking %q", turn.Temperature, turn.MaxOutputTokens, turn.ThinkingLevel)
		}
		if len(turn.Messages) != 3 || turn.Messages[0].Role != "user" || turn.Messages[0].Content != "inspect" || len(turn.Messages[0].Images) != 1 {
			t.Fatalf("user content/image mapping = %+v", turn.Messages)
		}
		if !reflect.DeepEqual(turn.Messages[0].Images[0].Data, []byte{1, 2, 3}) || turn.Messages[0].Images[0].MIMEType != "image/png" {
			t.Fatalf("image mapping = %+v", turn.Messages[0].Images[0])
		}
		if len(turn.Messages[1].ToolCalls) != 1 || turn.Messages[1].ToolCalls[0].Name != "read_file" {
			t.Fatalf("assistant tool mapping = %+v", turn.Messages[1])
		}
		if turn.Messages[2].Name != "read_file" || turn.Messages[2].ToolCallID != "call-1" || turn.Messages[2].Content != "contents" {
			t.Fatalf("tool result mapping = %+v", turn.Messages[2])
		}
		if len(turn.Tools) != 1 || turn.Tools[0].Name != "read_file" || string(turn.Tools[0].Parameters) != `{"type":"object"}` {
			t.Fatalf("tool declarations = %+v", turn.Tools)
		}
	})

	t.Run("invalid media is ignored without host resolver", func(t *testing.T) {
		content := []any{
			map[string]any{"type": "text", "text": "kept"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,%%%"}},
			map[string]any{"type": "unknown"},
			42,
		}
		text, images := codec.ParseOpenAIContent(content, codec.Media{})
		if text != "kept" || len(images) != 0 {
			t.Fatalf("malformed content = %q, %#v", text, images)
		}
	})

	t.Run("unresolved media error is best effort", func(t *testing.T) {
		content := []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://media.invalid/image.png"}}}
		_, images := codec.ParseOpenAIContent(content, codec.Media{Resolve: func(string) (string, []byte, error) {
			return "", nil, errors.New("fixture resolver failure")
		}})
		if len(images) != 0 {
			t.Fatalf("failed resolver yielded images: %#v", images)
		}
	})
}

const anthropicFixture = `{
  "model":"claude-sonnet-4-6",
  "system":[{"type":"text","text":"First."},{"type":"text","text":"Second."}],
  "messages":[
    {"role":"assistant","content":[{"type":"tool_use","id":"tool-9","name":"search","input":{"query":"abc"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-9","content":{"hits":2},"is_error":true},{"type":"text","text":"next"}]}
  ],
  "tools":[{"name":"search","description":"search docs","input_schema":{"type":"object"}}],
  "max_tokens":64,
  "temperature":0.5
}`

func TestAnthropicToTurn_MessagesFixtureMapsToolResult(t *testing.T) {
	var request codec.AnthropicRequest
	if err := json.Unmarshal([]byte(anthropicFixture), &request); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	turn := codec.AnthropicToTurn(request, codec.Media{}, "session-anthropic", "request-anthropic")
	if turn.Model != "claude-sonnet-4-6" || turn.SystemPrompt != "First.\n\nSecond." {
		t.Fatalf("model/system = %q/%q", turn.Model, turn.SystemPrompt)
	}
	if turn.MaxOutputTokens != 64 || turn.Temperature != 0.5 || turn.SessionID != "session-anthropic" {
		t.Fatalf("turn settings = %+v", turn)
	}
	if len(turn.Messages) != 3 || len(turn.Messages[0].ToolCalls) != 1 {
		t.Fatalf("messages = %+v", turn.Messages)
	}
	call := turn.Messages[0].ToolCalls[0]
	if call.ID != "tool-9" || call.Name != "search" || string(call.Arguments) != `{"query":"abc"}` {
		t.Fatalf("tool call = %+v", call)
	}
	result := turn.Messages[1]
	if result.Role != "tool" || result.ToolCallID != "tool-9" || result.Name != "search" || result.Content != `{"hits":2}` || !result.IsError {
		t.Fatalf("tool result = %+v", result)
	}
	if result.Images != nil || len(turn.Tools) != 1 || turn.Tools[0].Name != "search" || turn.Messages[2].Content != "next" {
		t.Fatalf("tool mappings = %+v / %+v", result, turn.Tools)
	}
}

func TestOpenAICompletion_ResponseFixtureIncludesToolCallsAndUsage(t *testing.T) {
	response := codec.OpenAICompletion("chatcmpl-fixture", "gemini-test", 123, "done", "reason", []geminicode.TurnToolCall{{ID: "call-7", Name: "search", Arguments: json.RawMessage(`{"q":"x"}`)}}, geminicode.Meta{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6})
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode response fixture: %v", err)
	}
	if got["id"] != "chatcmpl-fixture" || got["created"] != float64(123) {
		t.Fatalf("response identity = %#v", got)
	}
	choices := got["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish reason = %#v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	calls := message["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if fn["name"] != "search" || fn["arguments"] != `{"q":"x"}` || message["reasoning_content"] != "reason" {
		t.Fatalf("response tool/reasoning mapping = %#v", message)
	}
	usage := got["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(4) || usage["total_tokens"] != float64(6) {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestAnthropicMessageResponse_MalformedToolArgumentsFallback(t *testing.T) {
	response := codec.AnthropicMessageResponse("msg-fixture", "claude-test", "", "thought", []geminicode.TurnToolCall{{ID: "call-bad", Name: "search", Arguments: json.RawMessage(`{broken`)}}, geminicode.Meta{PromptTokens: 9, CompletionTokens: 3})
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["stop_reason"] != "tool_use" || got["stop_sequence"] != nil {
		t.Fatalf("stop metadata = %#v/%#v", got["stop_reason"], got["stop_sequence"])
	}
	blocks := got["content"].([]any)
	if len(blocks) != 2 || blocks[0].(map[string]any)["type"] != "thinking" {
		t.Fatalf("content blocks = %#v", blocks)
	}
	tool := blocks[1].(map[string]any)
	if tool["id"] != "call-bad" || !reflect.DeepEqual(tool["input"], map[string]any{}) {
		t.Fatalf("malformed tool arguments mapping = %#v", tool)
	}
}

func TestOpenAIToTurn_DuplicateToolCallIDDifferentFunctionNames(t *testing.T) {
	// Reused IDs must resolve chronologically, not from a global last-name map.
	const payload = `{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "edited"},
			{"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "shell", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "output"},
			{"role": "user", "content": "continue"}
		]
	}`
	var request codec.OpenAIChatRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatal(err)
	}
	turn := codec.OpenAIToTurn(request, codec.Media{}, "session", "request")
	var names []string
	for _, message := range turn.Messages {
		if message.Role == "tool" {
			names = append(names, message.Name)
		}
	}
	if !reflect.DeepEqual(names, []string{"edit", "shell"}) {
		t.Fatalf("chronological tool names = %v, want [edit shell]", names)
	}
}
