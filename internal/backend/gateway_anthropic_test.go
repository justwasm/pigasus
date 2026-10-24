package backend

// Tests for the Anthropic Messages dialect of the gateway transport.
//
// The request shape and the event stream are transcribed from a live
// capture against an Anthropic-format endpoint (see
// docs/cycles/provider-config-gateway/captures.md): the endpoint is
// "<base>/v1/messages", the credential travels in `x-api-key`, and the
// stream is the Anthropic one (message_start / content_block_* /
// message_delta / message_stop).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// anthropicSSE is the captured stream, reduced to the frames the transport
// consumes. The `ping` frame is included because the real endpoint sends
// one and the parser must skip it.
const anthropicSSE = `event: message_start
data: {"type":"message_start","message":{"id":"0729b75f","type":"message","role":"assistant","content":[],"model":"MiniMax-M3","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0,"service_tier":"standard"}}}

event: ping
data: {"type":"ping"}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":55,"output_tokens":2,"service_tier":"standard"}}

event: message_stop
data: {"type":"message_stop"}

`

// TestGatewayAnthropicRequestShape pins the request translation.
//
// The two places where a mechanical translation from the OpenAI dialect
// would be wrong are both pinned here: the system prompt is a top-level
// field rather than a message, and a tool result is a *user* turn — the
// Messages API has no `tool` role.
func TestGatewayAnthropicRequestShape(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "tenant-secret-xyz" {
			t.Errorf("x-api-key: got %q, want the configured credential", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header is missing")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header = %q, want none on the Anthropic dialect", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicSSE))
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/messages",
		APIKey:        "tenant-secret-xyz",
		ExternalModel: "Upstream-1",
		Type:          GatewayProtocolAnthropic,
	})

	req := StreamRequest{
		Model:        "m",
		SystemPrompt: "you are a helpful assistant",
		Messages: []Message{
			{Role: "user", Content: "list the files"},
			{Role: "assistant", Content: "sure", ToolCalls: []ToolCall{{
				ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"/etc/hosts"}`),
			}}},
			{Role: "tool", ToolCallID: "call-1", Content: "127.0.0.1 localhost"},
		},
		Tools: []ToolDecl{{
			Name:        "read_file",
			Description: "read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	}

	ch, err := c.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if got, _ := body["model"].(string); got != "Upstream-1" {
		t.Errorf("model = %q, want the configured upstream id", got)
	}
	if got, _ := body["system"].(string); got != "you are a helpful assistant" {
		t.Errorf("system = %q, want the system prompt as a top-level field", got)
	}
	if _, ok := body["messages"].([]any); !ok {
		t.Fatalf("messages missing from the body: %v", body)
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant, tool result)", len(msgs))
	}

	// The tool result must be a user turn holding a tool_result block.
	toolTurn := msgs[2].(map[string]any)
	if toolTurn["role"] != "user" {
		t.Errorf("tool result role = %v, want user (the Messages API has no tool role)", toolTurn["role"])
	}
	blocks := toolTurn["content"].([]any)
	block := blocks[0].(map[string]any)
	if block["type"] != "tool_result" {
		t.Errorf("tool result block type = %v, want tool_result", block["type"])
	}
	if block["tool_use_id"] != "call-1" {
		t.Errorf("tool_use_id = %v, want call-1", block["tool_use_id"])
	}

	// The assistant turn carries the tool_use block.
	assistant := msgs[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Errorf("assistant role = %v", assistant["role"])
	}
	aBlocks := assistant["content"].([]any)
	aBlock := aBlocks[1].(map[string]any)
	if aBlock["type"] != "tool_use" || aBlock["name"] != "read_file" {
		t.Errorf("assistant block = %v, want a tool_use block named read_file", aBlock)
	}

	// Tools are advertised with input_schema, not OpenAI's parameters.
	tools := body["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["name"] != "read_file" {
		t.Errorf("tool name = %v", tool["name"])
	}
	if _, ok := tool["input_schema"]; !ok {
		t.Errorf("tool has no input_schema: %v", tool)
	}
	if _, ok := tool["parameters"]; ok {
		t.Errorf("tool carries the OpenAI parameters key: %v", tool)
	}
}

// TestGatewayAnthropicMaxTokens pins that `max_tokens` is always sent.
//
// The Messages API requires the field: a request without it is refused.
// The caller's value wins; otherwise the model's configured output cap is
// used.
func TestGatewayAnthropicMaxTokens(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSE))
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:              srv.URL + "/v1/messages",
		ExternalModel:    "Upstream-1",
		Type:             GatewayProtocolAnthropic,
		DefaultMaxTokens: 8192,
	})

	ch, err := c.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}
	if got, _ := body["max_tokens"].(float64); got != 8192 {
		t.Errorf("max_tokens = %v, want the configured output cap (8192)", body["max_tokens"])
	}

	// The caller's value wins over the configured cap.
	ch, err = c.Stream(context.Background(), StreamRequest{Model: "m", MaxTokens: 128})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}
	if got, _ := body["max_tokens"].(float64); got != 128 {
		t.Errorf("max_tokens = %v, want the caller's value (128)", body["max_tokens"])
	}
}

// TestGatewayAnthropicStreamParsing pins the response translation: the text
// deltas become text chunks, and the terminal chunk carries the token
// counts the endpoint reported.
func TestGatewayAnthropicStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicSSE))
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/messages",
		ExternalModel: "Upstream-1",
		Type:          GatewayProtocolAnthropic,
	})

	ch, err := c.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	var done StreamChunk
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		text.WriteString(chunk.Text)
		if chunk.Done {
			done = chunk
		}
	}
	if text.String() != "ok" {
		t.Errorf("text = %q, want %q", text.String(), "ok")
	}
	if !done.Done {
		t.Fatal("no terminal chunk")
	}
	if done.Meta == nil {
		t.Fatal("terminal chunk has no meta")
	}
	if done.Meta.PromptTokens != 55 {
		t.Errorf("prompt tokens = %d, want 55", done.Meta.PromptTokens)
	}
	if done.Meta.CompletionTokens != 2 {
		t.Errorf("completion tokens = %d, want 2", done.Meta.CompletionTokens)
	}
}

// TestGatewayAnthropicToolCallStream pins the tool-call translation: the
// arguments arrive as JSON fragments and have to be concatenated into one
// tool call.
func TestGatewayAnthropicToolCallStream(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"id":"1","type":"message","role":"assistant","model":"Upstream-1","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-9","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"/etc/hosts\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":10,"output_tokens":7}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/messages",
		ExternalModel: "Upstream-1",
		Type:          GatewayProtocolAnthropic,
	})

	ch, err := c.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var calls []ToolCall
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		calls = append(calls, chunk.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if calls[0].ID != "call-9" || calls[0].Name != "read_file" {
		t.Errorf("tool call = %+v, want call-9/read_file", calls[0])
	}
	if got := string(calls[0].Arguments); got != `{"path":"/etc/hosts"}` {
		t.Errorf("arguments = %q, want the concatenated JSON object", got)
	}
}

// TestGatewayAnthropicThinkingIsDropped pins that a thinking block does not
// leak into the answer.
//
// The transport seam carries text only, and the clone's sanitiser turns
// reasoning into a separate surface, so the thinking delta is dropped
// rather than mixed into the reply.
func TestGatewayAnthropicThinkingIsDropped(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"id":"1","type":"message","role":"assistant","model":"Upstream-1","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me think"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/messages",
		ExternalModel: "Upstream-1",
		Type:          GatewayProtocolAnthropic,
	})

	ch, err := c.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		text.WriteString(chunk.Text)
	}
	if text.String() != "answer" {
		t.Errorf("text = %q, want %q (the thinking block must be dropped)", text.String(), "answer")
	}
}
