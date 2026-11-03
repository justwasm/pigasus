// provider_test.go — tests for the provider implementations.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// chatHandlerForOpenAI emulates the OpenAI /v1/chat/completions endpoint
// well enough to drive the OpenAI-shape providers through their Chat and
// Stream paths.
func chatHandlerForOpenAI(t *testing.T, wantAuth string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" {
			got := r.Header.Get("Authorization")
			if got != wantAuth {
				t.Errorf("Authorization = %q, want %q", got, wantAuth)
			}
		}
		var body openAIRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), 400)
			return
		}
		_ = body
		w.Header().Set("content-type", "application/json")
		if body.Stream {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi-from-openai\"}}]}\n\n")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n")
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(openAIResponseBody{
			ID:    "chatcmpl-test",
			Model: body.Model,
			Choices: []struct {
				Index        int           `json:"index"`
				Message      openAIMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{{Index: 0, Message: openAIMessage{Role: "assistant", Content: "hi-from-openai"}, FinishReason: "stop"}},
		})
	})
}

func TestProviderOpenAI(t *testing.T) {
	srv := httptest.NewServer(chatHandlerForOpenAI(t, "Bearer sk-test"))
	defer srv.Close()
	m := Model{
		Name: "openai", Provider: "openai",
		APIURL: srv.URL, ExternalModelName: "gpt-test", APIKey: "sk-test",
		TimeoutSeconds: 10,
	}
	p, err := NewProvider(m)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want openai", p.Name())
	}
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(resp.Content, "hi-from-openai") {
		t.Errorf("Chat content = %q, want hi-from-openai", resp.Content)
	}
	testProviderOpenAIStream(t, p)
}

func testProviderOpenAIStream(t *testing.T, p Provider) {
	t.Helper()
	stream, err := p.Stream(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var acc strings.Builder
	var done bool
	for ev := range stream {
		if ev.Error != nil {
			t.Fatalf("stream error: %v", ev.Error)
		}
		acc.WriteString(ev.Text)
		if ev.Done {
			done = true
		}
	}
	if !done {
		t.Error("stream did not terminate with Done=true")
	}
	if !strings.Contains(acc.String(), "hi-from-openai") {
		t.Errorf("stream acc = %q, want hi-from-openai", acc.String())
	}
}

func TestProviderGatewaySendsBearer(t *testing.T) {
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	m := Model{
		Name: "gateway", Provider: "openai",
		APIURL: srv.URL, ExternalModelName: "MiniMax-M3", APIKey: "-",
	}
	p, err := NewProvider(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "smoke"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	select {
	case auth := <-gotAuth:
		if auth != "Bearer -" {
			t.Errorf("Authorization = %q, want 'Bearer -'", auth)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for auth header")
	}
}

func anthropicMockHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Errorf("x-api-key = %q, want sk-ant-test", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want 2023-06-01", got)
		}
		var body anthropicRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad", 400)
			return
		}
		w.Header().Set("content-type", "application/json")
		if body.Stream {
			fmt.Fprint(w, "event: content_block_delta\n")
			fmt.Fprint(w, "data: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hello-ant\"}}\n\n")
			fmt.Fprint(w, "event: message_stop\n")
			fmt.Fprint(w, "data: {}\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			ID:    "msg_test",
			Model: body.Model,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "text", Text: "hello-ant"}},
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 4, OutputTokens: 2},
		})
	})
}

func TestProviderAnthropic(t *testing.T) {
	srv := httptest.NewServer(anthropicMockHandler(t))
	defer srv.Close()
	m := Model{
		Name: "ant", Provider: "anthropic",
		APIURL: srv.URL, ExternalModelName: "claude-test", APIKey: "sk-ant-test",
	}
	p, err := NewProvider(m)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), ChatRequest{
		SystemPrompt: "you are a test",
		Messages:     []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(resp.Content, "hello-ant") {
		t.Errorf("Content = %q, want hello-ant", resp.Content)
	}
	if resp.TotalTokens != 6 {
		t.Errorf("TotalTokens = %d, want 6", resp.TotalTokens)
	}
}

func TestNewProviderDispatch(t *testing.T) {
	supported := []string{"openai", "anthropic", "ollama", "openrouter", "google", "custom"}
	for _, prov := range supported {
		m := Model{
			Name: "x", Provider: prov,
			APIURL:            "http://localhost:0",
			ExternalModelName: "m",
		}
		if _, err := NewProvider(m); err != nil {
			t.Errorf("NewProvider(%q) = %v", prov, err)
		}
	}
	if _, err := NewProvider(Model{Name: "x", Provider: "weird", APIURL: "u", ExternalModelName: "m"}); err == nil {
		t.Error("NewProvider(weird) should error")
	}
}

func TestProviderOpenAIShapeAcceptsNoAPIKey(t *testing.T) {
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	m := Model{
		Name: "ollama", Provider: "ollama",
		APIURL: srv.URL, ExternalModelName: "llama3", APIKey: "ignored",
	}
	p, _ := NewProvider(m)
	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case auth := <-gotAuth:
		if auth != "" {
			t.Errorf("ollama Authorization = %q, want empty", auth)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestGatewayLiveE2E(t *testing.T) {
	if os.Getenv("AGY_LIVE") != "1" {
		t.Skip("set AGY_LIVE=1 to run live the configured gateway test")
	}
	m := Model{
		Name:              "gateway",
		Provider:          "openai",
		APIURL:            "https://gateway.example.com/v1/chat/completions",
		ExternalModelName: "MiniMax-M3",
		APIKey:            "-",
		TimeoutSeconds:    30,
	}
	p, err := NewProvider(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Chat(ctx, ChatRequest{
		Messages:  []ChatMessage{{Role: "user", Content: "SMOKE"}},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if strings.Contains(resp.Content, "mock reply") {
		t.Errorf("got mock reply from real provider: %q", resp.Content)
	}
	if resp.Content == "" {
		t.Errorf("empty content from real provider: %+v", resp)
	}
}

var (
	_ Provider = (*openAIShapeProvider)(nil)
	_ Provider = (*anthropicProvider)(nil)
	_ Provider = (*googleProvider)(nil)
)

type silentReader struct{ r io.Reader }

func (s *silentReader) Read(p []byte) (int, error) { return s.r.Read(p) }

var _ = silentReader{}
