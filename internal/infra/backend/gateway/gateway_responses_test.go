package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestGatewayResponses_BuildRequest(t *testing.T) {
	c := &GatewayClient{
		cfg: GatewayConfig{
			Type:             GatewayProtocolResponses,
			DefaultMaxTokens: 4096,
			ReasoningEffort:  "high",
		},
	}

	req := backend.StreamRequest{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []backend.Message{
			{Role: "user", Content: "Hello world"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "Run ls"},
		},
		Tools: []backend.ToolDecl{
			{
				Name:        "run_command",
				Description: "Run shell command",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
			},
		},
	}

	raw, err := c.buildResponsesRequest(req, "gpt-6.1-sol")
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}

	var parsed responsesRequestBody
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Model != "gpt-6.1-sol" {
		t.Errorf("model = %q, want gpt-6.1-sol", parsed.Model)
	}
	if parsed.Instructions != "You are a helpful assistant." {
		t.Errorf("instructions = %q", parsed.Instructions)
	}
	if parsed.Reasoning == nil || parsed.Reasoning.Effort != "high" {
		t.Errorf("reasoning effort = %v, want high", parsed.Reasoning)
	}
	if len(parsed.Input) != 3 {
		t.Errorf("len(input) = %d, want 3", len(parsed.Input))
	}
	if len(parsed.Tools) != 1 {
		t.Errorf("len(tools) = %d, want 1", len(parsed.Tools))
	}
}

func TestGatewayResponses_StreamTextAndToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("no flusher")
		}

		send := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		send("response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hello "}`)
		send("response.output_text.delta", `{"type":"response.output_text.delta","delta":"world!"}`)
		send("response.output_item.added", `{"type":"response.output_item.added","item":{"type":"function_call","id":"call_1","call_id":"call_1","name":"test_tool"}}`)
		send("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"{\"arg\":"}`)
		send("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"\"val\"}"}`)
		send("response.output_item.done", `{"type":"response.output_item.done","item":{"type":"function_call","id":"call_1","name":"test_tool","arguments":"{\"arg\":\"val\"}"}}`)
		send("response.completed", `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`)
	}))
	defer srv.Close()

	client := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/responses",
		Type:          GatewayProtocolResponses,
		ExternalModel: "gpt-6.1-sol",
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{
		Messages: []backend.Message{
			{Role: "user", Content: "test"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var (
		accumulatedText string
		gotToolCalls    []backend.ToolCall
		gotDone         bool
		gotMeta         *backend.ChunkMeta
	)

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		if chunk.Text != "" {
			accumulatedText += chunk.Text
		}
		if len(chunk.ToolCalls) > 0 {
			gotToolCalls = append(gotToolCalls, chunk.ToolCalls...)
		}
		if chunk.Done {
			gotDone = true
			gotMeta = chunk.Meta
		}
	}

	if !gotDone {
		t.Fatal("expected done chunk")
	}
	if accumulatedText != "Hello world!" {
		t.Errorf("text = %q, want 'Hello world!'", accumulatedText)
	}
	if len(gotToolCalls) != 1 {
		t.Fatalf("len(toolCalls) = %d, want 1", len(gotToolCalls))
	}
	if gotToolCalls[0].Name != "test_tool" {
		t.Errorf("tool name = %q, want test_tool", gotToolCalls[0].Name)
	}
	if string(gotToolCalls[0].Arguments) != `{"arg":"val"}` {
		t.Errorf("tool args = %s, want {\"arg\":\"val\"}", gotToolCalls[0].Arguments)
	}
	if gotMeta == nil || gotMeta.TotalTokens != 30 {
		t.Errorf("meta = %+v, want total_tokens=30", gotMeta)
	}
}
