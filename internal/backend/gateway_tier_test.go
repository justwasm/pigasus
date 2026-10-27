// Package backend — gateway_tier_test.go
//
// Tests for the reasoning level the configured transport sends upstream.
//
// A model that declares reasoning tiers runs at one of them, and the tier
// rides the request as the OpenAI dialect's `reasoning_effort` hint. The
// Anthropic dialect has no such field, so the hint is dropped there.
package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// tierEchoHandler answers with one SSE frame and records the request body.
func tierEchoHandler(t *testing.T, captured *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		*captured = body
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		frame := map[string]any{
			"id":      "tier-test",
			"object":  "chat.completion.chunk",
			"model":   "MiniMax-M3.1-Flash",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": "ok"}}},
		}
		data, _ := json.Marshal(frame)
		_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
	}
}

// TestGatewaySendsTheReasoningEffort pins the tier's ride on the request.
func TestGatewaySendsTheReasoningEffort(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(tierEchoHandler(t, &captured))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:             srv.URL + "/v1/chat/completions",
		ExternalModel:   "MiniMax-M3.1-Flash",
		APIKey:          "tenant-secret-xyz",
		ReasoningEffort: "xhigh",
	})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Model: "minimax-m3.1-flash-xhigh",
		Messages: []Message{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	if got := captured["reasoning_effort"]; got != "xhigh" {
		t.Errorf("reasoning_effort = %v, want xhigh", got)
	}
	if got := captured["model"]; got != "MiniMax-M3.1-Flash" {
		t.Errorf("model = %v, want MiniMax-M3.1-Flash", got)
	}
}

// TestGatewayOmitsTheReasoningEffortWhenThereIsNone pins that a model
// that says nothing sends no hint at all.
func TestGatewayOmitsTheReasoningEffortWhenThereIsNone(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(tierEchoHandler(t, &captured))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
		APIKey:        "tenant-secret-xyz",
	})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Model:    "minimax-m3",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
	if _, ok := captured["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort was sent: %v", captured["reasoning_effort"])
	}
}
