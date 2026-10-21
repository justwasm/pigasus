package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAgwK0sSendsToolsAndRoundTripsToolCalls covers the cycle-97 wire
// behaviour end to end:
//
//   - an advertised tool set is serialised as an OpenAI `tools` array
//   - an assistant message carrying tool_calls is echoed back verbatim
//   - a `tool` role result message carries its tool_call_id
//   - fragmented `delta.tool_calls` frames are reassembled into one ToolCall
func TestAgwK0sSendsToolsAndRoundTripsToolCalls(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		captured = body

		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// Fragment the call the way a real gateway does: the first frame
		// carries id + name + the opening brace, the second the rest.
		frames := []map[string]any{
			{
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"index": 0,
							"id":    "call_abc",
							"type":  "function",
							"function": map[string]any{
								"name":      "run_command",
								"arguments": `{"command":`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			},
			{
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{{
							"index":    0,
							"function": map[string]any{"arguments": `"date"}`},
						}},
					},
				}},
			},
		}
		for _, frame := range frames {
			data, _ := json.Marshal(frame)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		if flusher != nil {
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewAgwK0sWithConfig(AgwK0sConfig{URL: srv.URL + "/v1/chat/completions"})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{
			{Role: "user", Content: "what time is it"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_prev", Name: "run_command", Arguments: json.RawMessage(`{"command":"uname"}`)}}},
			{Role: "tool", ToolCallID: "call_prev", Name: "run_command", Content: "Linux"},
		},
		Tools: []ToolDecl{{
			Name:        "run_command",
			Description: "Execute a command.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var calls []ToolCall
	var done bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		calls = append(calls, chunk.ToolCalls...)
		if chunk.Done {
			done = true
		}
	}
	if !done {
		t.Error("stream ended without a Done chunk")
	}
	if len(calls) != 1 {
		t.Fatalf("assembled %d tool calls, want 1 (%+v)", len(calls), calls)
	}
	if calls[0].Name != "run_command" || calls[0].ID != "call_abc" {
		t.Errorf("call = %+v, want run_command/call_abc", calls[0])
	}
	if string(calls[0].Arguments) != `{"command":"date"}` {
		t.Errorf("arguments = %q, want {\"command\":\"date\"}", calls[0].Arguments)
	}

	// ---- request-side assertions ----
	toolsAny := captured["tools"]
	if toolsAny == nil {
		t.Fatal("request body has no `tools` key")
	}
	tools, ok := toolsAny.([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want 1 entry", toolsAny)
	}
	first := tools[0].(map[string]any)
	if first["type"] != "function" {
		t.Errorf("tool type = %v, want function", first["type"])
	}
	if fn := first["function"].(map[string]any); fn["name"] != "run_command" {
		t.Errorf("tool name = %v, want run_command", fn["name"])
	}

	msgs, _ := captured["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	asst := msgs[1].(map[string]any)
	asstCalls, _ := asst["tool_calls"].([]any)
	if len(asstCalls) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(asstCalls))
	}
	if id := asstCalls[0].(map[string]any)["id"]; id != "call_prev" {
		t.Errorf("assistant tool_call id = %v, want call_prev", id)
	}
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_prev" {
		t.Errorf("tool result message = %#v, want role=tool tool_call_id=call_prev", toolMsg)
	}
}

// TestAgwK0sOmitsToolsWhenNoneAdvertised pins that a no-tools request is
// byte-identical to the pre-cycle-97 body (the key is absent, not null).
func TestAgwK0sOmitsToolsWhenNoneAdvertised(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewAgwK0sWithConfig(AgwK0sConfig{URL: srv.URL + "/v1/chat/completions"})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	if _, present := captured["tools"]; present {
		t.Errorf("tools key present with no advertised tools: %#v", captured["tools"])
	}
}
