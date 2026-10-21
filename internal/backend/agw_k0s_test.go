package backend

// Cycle 40 tests for the agw.k0s.io direct Client. The non-network
// tests below cover the wire-neutral translation + the SSE parser
// using a httptest server that emulates the gateway's stream shape.
// The integration test against the real agw.k0s.io endpoint is gated
// behind AGW_K0S_INTEGRATION=1 so it doesn't run in CI by default.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestAgwK0sConfigDefaults asserts that a zero-value AgwK0sConfig
// produces a usable client pointing at the public dev sentinel.
func TestAgwK0sConfigDefaults(t *testing.T) {
	cfg := AgwK0sConfig{}.withDefaults()
	if cfg.URL != "https://agw.k0s.io/v1/chat/completions" {
		t.Errorf("URL default: got %q, want https://agw.k0s.io/v1/chat/completions", cfg.URL)
	}
	if cfg.APIKey != "-" {
		t.Errorf("APIKey default: got %q, want \"-\"", cfg.APIKey)
	}
	if cfg.ExternalModel != "MiniMax-M3" {
		t.Errorf("ExternalModel default: got %q, want MiniMax-M3", cfg.ExternalModel)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("TimeoutSeconds default: got %d, want 60", cfg.TimeoutSeconds)
	}
}

// TestAgwK0sConfigEnvOverride ensures OPENAGY_AGW_KEY is honored when
// the config field is empty (cycle 40 dev-sandbox override).
func TestAgwK0sConfigEnvOverride(t *testing.T) {
	t.Setenv("OPENAGY_AGW_KEY", "tenant-secret-xyz")
	cfg := AgwK0sConfig{}.withDefaults()
	if cfg.APIKey != "tenant-secret-xyz" {
		t.Errorf("APIKey env override: got %q, want tenant-secret-xyz", cfg.APIKey)
	}
	// Sanity: empty env value should fall through to default.
	t.Setenv("OPENAGY_AGW_KEY", "   ")
	cfg2 := AgwK0sConfig{}.withDefaults()
	if cfg2.APIKey != "-" {
		t.Errorf("APIKey blank env should fall through to default: got %q", cfg2.APIKey)
	}
}

// TestAgwK0sEndpointDescription makes sure the Endpoint() string is
// safe to log (no leaked API key) and mentions the model so operators
// can spot a misconfiguration at a glance.
func TestAgwK0sEndpointDescription(t *testing.T) {
	c := NewAgwK0sWithConfig(AgwK0sConfig{}).(*AgwK0sClient)
	desc := c.Endpoint()
	if !strings.Contains(desc, "agw.k0s.io") {
		t.Errorf("Endpoint() missing gateway name: %q", desc)
	}
	if !strings.Contains(desc, "MiniMax-M3") {
		t.Errorf("Endpoint() missing model name: %q", desc)
	}
	if strings.Contains(desc, c.apiKey) {
		// The dev sentinel "-" is fine, but a tenant key must never
		// appear in the description.
		if c.apiKey != "-" {
			t.Errorf("Endpoint() leaks API key: %q (key=%q)", desc, c.apiKey)
		}
	}
}

// fakeAGWSSEHandler emulates the agw.k0s.io gateway's streaming
// behaviour: a few `data: {delta.content}` chunks followed by a
// final empty-choices frame with a usage block. We capture the
// request body so the test can assert that the upstream model name
// + system prompt + message order are correct.
func fakeAGWSSEHandler(t *testing.T, captured *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Capture request: auth header + body.
		if got := r.Header.Get("Authorization"); got != "Bearer -" {
			t.Errorf("Authorization header: got %q, want \"Bearer -\"", got)
		}
		if got := r.Header.Get("content-type"); got != "application/json" {
			t.Errorf("content-type: got %q, want application/json", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		*captured = body
		// Stream the canned reply. 3 chunks of text, then a final
		// empty-choices frame with usage, then EOF.
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		reply := []string{"Hello", " from", " agw.k0s.io test!"}
		for i, piece := range reply {
			frame := map[string]any{
				"id":     "test-id",
				"object": "chat.completion.chunk",
				"model":  "MiniMax-M3",
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{
						"role":    "assistant",
						"content": piece,
					},
				}},
				"created": 1791999999,
			}
			data, _ := json.Marshal(frame)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil && i == 0 {
				// Flush early so the client sees the response is live.
				flusher.Flush()
			}
		}
		// Final frame: empty choices + usage block (this is what
		// agw.k0s.io does, not the OpenAI-standard [DONE] sentinel).
		final := map[string]any{
			"id":      "test-id",
			"object":  "chat.completion.chunk",
			"model":   "MiniMax-M3",
			"choices": []any{},
			"usage": map[string]int{
				"prompt_tokens":     7,
				"completion_tokens": 5,
				"total_tokens":      12,
			},
			"created": 1791999999,
		}
		data, _ := json.Marshal(final)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
}

// TestAgwK0sStreamParsing drives a real HTTP round-trip through a
// stub server and asserts:
//   - upstream model is MiniMax-M3 (registry/handle ignored)
//   - system prompt is hoisted into a system message
//   - SSE chunks are concatenated into a single text reply
//   - usage block on the terminal frame is mapped to ChunkMeta
//   - the loop contract (always Done=true) is satisfied even though
//     agw.k0s.io does NOT emit a [DONE] sentinel
func TestAgwK0sStreamParsing(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(fakeAGWSSEHandler(t, &captured))
	defer srv.Close()

	c := NewAgwK0sWithConfig(AgwK0sConfig{URL: srv.URL + "/v1/chat/completions"})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Model:        "claude-4.5-sonnet", // should be IGNORED
		SystemPrompt: "you are a test bot",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var (
		gotText string
		gotDone bool
		gotMeta *ChunkMeta
	)
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		gotText += chunk.Text
		if chunk.Done {
			gotDone = true
			gotMeta = chunk.Meta
		}
	}
	if !gotDone {
		t.Fatal("stream closed without Done=true (loop contract violation)")
	}
	if gotText != "Hello from agw.k0s.io test!" {
		t.Errorf("assembled text: got %q, want %q", gotText, "Hello from agw.k0s.io test!")
	}
	if gotMeta == nil {
		t.Fatal("terminal chunk missing Meta (usage block dropped)")
	}
	if gotMeta.PromptTokens != 7 || gotMeta.CompletionTokens != 5 || gotMeta.TotalTokens != 12 {
		t.Errorf("usage: got %+v, want {7 5 12}", gotMeta)
	}
	if !strings.Contains(gotMeta.SystemFingerprint, "agw.k0s.io") {
		t.Errorf("SystemFingerprint missing agw tag: %q", gotMeta.SystemFingerprint)
	}
	// Verify the upstream saw the right model + system prompt hoist.
	if captured["model"] != "MiniMax-M3" {
		t.Errorf("upstream model: got %v, want MiniMax-M3", captured["model"])
	}
	msgs, ok := captured["messages"].([]any)
	if !ok {
		t.Fatalf("messages: not a list, got %T", captured["messages"])
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "you are a test bot" {
		t.Errorf("first message: got %+v, want role=system content=\"you are a test bot\"", first)
	}
	second, _ := msgs[1].(map[string]any)
	if second["role"] != "user" || second["content"] != "hello" {
		t.Errorf("second message: got %+v, want role=user content=hello", second)
	}
}

// TestAgwK0sErrorStatus ensures non-2xx responses surface a readable
// error (with the upstream body truncated to fit a log line).
func TestAgwK0sErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad model"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewAgwK0sWithConfig(AgwK0sConfig{URL: srv.URL + "/v1/chat/completions"})
	_, err := c.Stream(context.Background(), StreamRequest{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if !strings.Contains(err.Error(), "upstream 400") {
		t.Errorf("error message missing status code: %v", err)
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Errorf("error message missing body: %v", err)
	}
}

// TestAgwK0sIntegration runs only when AGW_K0S_INTEGRATION=1 (CI /
// verifier hook). It hits the real agw.k0s.io endpoint and confirms
// the SSE framing works end-to-end against the live gateway. We
// skip by default because the dev sandbox is the only environment
// with confirmed egress; running this in an offline sandbox would
// just hang the test.
func TestAgwK0sIntegration(t *testing.T) {
	if os.Getenv("AGW_K0S_INTEGRATION") != "1" {
		t.Skip("set AGW_K0S_INTEGRATION=1 to run the live integration test")
	}
	c := NewAgwK0s()
	stream, err := c.Stream(context.Background(), StreamRequest{
		Model:    "ignored",
		Messages: []Message{{Role: "user", Content: "say hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var (
		gotText string
		gotDone bool
	)
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		gotText += chunk.Text
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("integration: no Done chunk")
	}
	if gotText == "" {
		t.Fatal("integration: empty text from real gateway")
	}
	t.Logf("integration ok, got %d chars: %q", len(gotText), truncateAGW(gotText, 80))
}

// keep bufio referenced even when all current tests go through
// net/http's reader; future tests that hand-roll an SSE pipe will
// use this import to avoid a "imported and not used" build break.
var _ = bufio.NewScanner

// TestAgwK0sReasoningTokens pins the cycle 100 addition: the gateway's
// usage.completion_tokens_details.reasoning_tokens is surfaced on ChunkMeta so
// the conversation view can render the recovered "Thought for Ns, N tokens"
// step. Shape captured from a live agw.k0s.io response:
//
//	"usage":{"total_tokens":212,"prompt_tokens":184,"completion_tokens":28,
//	         "completion_tokens_details":{"reasoning_tokens":24}, ...}
func TestAgwK0sReasoningTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":184,"+
			"\"completion_tokens\":28,\"total_tokens\":212,"+
			"\"completion_tokens_details\":{\"reasoning_tokens\":24}}}\n\n")
	}))
	defer srv.Close()

	c := NewAgwK0sWithConfig(AgwK0sConfig{URL: srv.URL})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var meta *ChunkMeta
	for chunk := range stream {
		if chunk.Meta != nil {
			meta = chunk.Meta
		}
	}
	if meta == nil {
		t.Fatal("no ChunkMeta on the stream")
	}
	if meta.ReasoningTokens != 24 {
		t.Errorf("ReasoningTokens = %d; want 24", meta.ReasoningTokens)
	}
	if meta.CompletionTokens != 28 || meta.PromptTokens != 184 {
		t.Errorf("usage = %d/%d; want 184/28", meta.PromptTokens, meta.CompletionTokens)
	}
}

// TestAgwTimeoutDefaults pins the per-request timeout semantics added in cycle
// 117: 0 keeps the client's 60s default (the interactive path), a negative value
// (AgwTimeoutUnlimited) survives withDefaults untouched so the request is
// bounded only by the caller's context (the headless path).
func TestAgwTimeoutDefaults(t *testing.T) {
	if got := (AgwK0sConfig{}).withDefaults().TimeoutSeconds; got != 60 {
		t.Errorf("zero config TimeoutSeconds = %d, want 60", got)
	}
	if got := (AgwK0sConfig{TimeoutSeconds: AgwTimeoutUnlimited}).withDefaults().TimeoutSeconds; got != AgwTimeoutUnlimited {
		t.Errorf("AgwTimeoutUnlimited became %d, want %d", got, AgwTimeoutUnlimited)
	}
	if got := (AgwK0sConfig{TimeoutSeconds: 5}).withDefaults().TimeoutSeconds; got != 5 {
		t.Errorf("explicit 5 became %d", got)
	}
	// The unlimited config must still produce a usable client.
	if c := NewAgwK0sWithConfig(AgwK0sConfig{TimeoutSeconds: AgwTimeoutUnlimited}); c == nil {
		t.Error("NewAgwK0sWithConfig returned nil")
	}
}
