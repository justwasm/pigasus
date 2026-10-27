// Package backendselect — tier_test.go
//
// Tests for the tier's ride from the catalogue id into the request.
//
// The configured transport is built from the model the catalogue id names,
// so a tier row ("minimax-m3.1-flash-xhigh") reaches the endpoint as the
// OpenAI dialect's `reasoning_effort` hint.
package backendselect

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/configtest"
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

// TestConfiguredTransportSendsTheTier pins the tier's ride: the row's id
// names the model, and the model's tier is the request's hint.
func TestConfiguredTransportSendsTheTier(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(tierEchoHandler(t, &captured))
	defer srv.Close()

	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "`+srv.URL+`/v1/chat/completions",
  "api_key": "-",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
     "reasoning_effort": "high"}
  ]}}}`)

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("backend", "gateway", "")
	identity := func(val string) string { return val }
	client := Pick(fs, "minimax-m3.1-flash-xhigh", identity)
	stream, err := client.Stream(context.Background(), backend.StreamRequest{
		Model:    "minimax-m3.1-flash-xhigh",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
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

// TestConfiguredTransportSendsTheBaseModelHint pins the plain model: the
// configuration's own word is the hint, and no tier is invented.
func TestConfiguredTransportSendsTheBaseModelHint(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(tierEchoHandler(t, &captured))
	defer srv.Close()

	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "`+srv.URL+`/v1/chat/completions",
  "api_key": "-",
  "models": [
    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
  ]}}}`)

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("backend", "gateway", "")
	identity := func(val string) string { return val }
	client := Pick(fs, "minimax-m3", identity)
	stream, err := client.Stream(context.Background(), backend.StreamRequest{
		Model:    "minimax-m3",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
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
