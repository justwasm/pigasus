package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func responsesMockHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-resp-test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			http.Error(w, "missing user-agent", http.StatusBadRequest)
			return
		}

		var body responsesRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}

		send := func(ev, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev, data)
			flusher.Flush()
		}

		send("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello-resp"}`)
		send("response.output_item.done", `{"type":"response.output_item.done","item":{"type":"function_call","id":"call_1","name":"test_fn","arguments":"{\"q\":\"foo\"}"}}`)
		send("response.completed", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`)
	})
}

func TestProviderResponses_Chat(t *testing.T) {
	srv := httptest.NewServer(responsesMockHandler(t))
	defer srv.Close()

	m := Model{
		Name:              "resp",
		Provider:          ProviderTypeResponses,
		APIURL:            srv.URL,
		ExternalModelName: "gpt-6.1-sol",
		APIKey:            "sk-resp-test",
	}

	p, err := NewProvider(m)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := p.Chat(context.Background(), ChatRequest{
		SystemPrompt: "you are a test",
		Messages:     []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "hello-resp" {
		t.Errorf("Content = %q, want hello-resp", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "test_fn" {
		t.Errorf("ToolCalls = %+v, want 1 test_fn call", resp.ToolCalls)
	}
	if resp.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.TotalTokens)
	}
}

func TestProviderResponses_Stream(t *testing.T) {
	srv := httptest.NewServer(responsesMockHandler(t))
	defer srv.Close()

	m := Model{
		Name:              "resp",
		Provider:          ProviderTypeResponses,
		APIURL:            srv.URL,
		ExternalModelName: "gpt-6.1-sol",
		APIKey:            "sk-resp-test",
	}

	p, err := NewProvider(m)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	events, err := p.Stream(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var (
		accumulatedText string
		gotToolCalls    []ToolCall
		gotDone         bool
		gotMeta         *StreamMeta
	)

	for ev := range events {
		if ev.Error != nil {
			t.Fatalf("stream ev error: %v", ev.Error)
		}
		if ev.Text != "" {
			accumulatedText += ev.Text
		}
		if len(ev.ToolCalls) > 0 {
			gotToolCalls = append(gotToolCalls, ev.ToolCalls...)
		}
		if ev.Done {
			gotDone = true
			gotMeta = ev.Meta
		}
	}

	if !gotDone {
		t.Fatal("expected done event")
	}
	if accumulatedText != "hello-resp" {
		t.Errorf("accumulated = %q, want hello-resp", accumulatedText)
	}
	if len(gotToolCalls) != 1 || gotToolCalls[0].Name != "test_fn" {
		t.Errorf("ToolCalls = %+v, want 1 call", gotToolCalls)
	}
	if gotMeta == nil || gotMeta.TotalTokens != 15 {
		t.Errorf("meta = %+v, want total_tokens=15", gotMeta)
	}
}
