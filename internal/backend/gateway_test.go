package backend

// Cycle 40 tests for the configured gateway direct Client. The non-network
// tests below cover the wire-neutral translation + the SSE parser
// using a httptest server that emulates the gateway's stream shape.
// The integration test against the real the configured gateway endpoint is gated
// behind GATEWAY_INTEGRATION=1 so it doesn't run in CI by default.

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

// TestGatewayConfigDefaults asserts that a zero-value GatewayConfig fills
// in only what can be defaulted without inventing an endpoint.
//
// The endpoint, the credential and the upstream model id are configuration:
// a transport that defaulted them would talk to a host nobody chose and ask
// it for a model nobody asked for. The wire protocol and the timeout are
// the two things the package owns.
func TestGatewayConfigDefaults(t *testing.T) {
	t.Setenv("OPENAGY_GATEWAY_URL", "")
	t.Setenv("OPENAGY_GATEWAY_KEY", "")
	cfg := GatewayConfig{}.withDefaults()
	if cfg.URL != "" {
		t.Errorf("URL default: got %q, want empty (the endpoint is configuration)", cfg.URL)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey default: got %q, want empty (the credential is configuration)", cfg.APIKey)
	}
	if cfg.ExternalModel != "" {
		t.Errorf("ExternalModel default: got %q, want empty (the model is configuration)", cfg.ExternalModel)
	}
	if cfg.Type != GatewayProtocolOpenAI {
		t.Errorf("Type default: got %q, want %q", cfg.Type, GatewayProtocolOpenAI)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("TimeoutSeconds default: got %d, want 60", cfg.TimeoutSeconds)
	}
}

// TestGatewayConfigEnvOverride ensures OPENAGY_GATEWAY_KEY and
// OPENAGY_GATEWAY_URL are honored when the config field is empty. They are
// the escape hatch for a parity harness that has to point the CLI at a
// scripted endpoint without rewriting the configuration.
func TestGatewayConfigEnvOverride(t *testing.T) {
	t.Setenv("OPENAGY_GATEWAY_KEY", "tenant-secret-xyz")
	t.Setenv("OPENAGY_GATEWAY_URL", "https://example.invalid/v1/chat/completions")
	cfg := GatewayConfig{}.withDefaults()
	if cfg.APIKey != "tenant-secret-xyz" {
		t.Errorf("APIKey env override: got %q, want tenant-secret-xyz", cfg.APIKey)
	}
	if cfg.URL != "https://example.invalid/v1/chat/completions" {
		t.Errorf("URL env override: got %q", cfg.URL)
	}
	// Sanity: a blank env value must not become a credential.
	t.Setenv("OPENAGY_GATEWAY_KEY", "   ")
	cfg2 := GatewayConfig{}.withDefaults()
	if cfg2.APIKey != "" {
		t.Errorf("APIKey blank env should stay empty: got %q", cfg2.APIKey)
	}
}

// TestGatewayEndpointDescription makes sure the Endpoint() string is
// safe to log (no leaked API key) and mentions the model so operators
// can spot a misconfiguration at a glance.
func TestGatewayEndpointDescription(t *testing.T) {
	c := NewGatewayWithConfig(GatewayConfig{
		URL:           "https://gateway.example/v1/chat/completions",
		APIKey:        "tenant-secret-xyz",
		ExternalModel: "Upstream-1",
	}).(*GatewayClient)
	desc := c.Endpoint()
	if !strings.Contains(desc, "gateway.example") {
		t.Errorf("Endpoint() missing the endpoint host: %q", desc)
	}
	if !strings.Contains(desc, "Upstream-1") {
		t.Errorf("Endpoint() missing model name: %q", desc)
	}
	if strings.Contains(desc, c.apiKey) {
		t.Errorf("Endpoint() leaks API key: %q (key=%q)", desc, c.apiKey)
	}
}

// fakeGatewaySSEHandler emulates the configured gateway's streaming
// behaviour: a few `data: {delta.content}` chunks followed by a
// final empty-choices frame with a usage block. We capture the
// request body so the test can assert that the upstream model name
// + system prompt + message order are correct.
func fakeGatewaySSEHandler(t *testing.T, captured *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Capture request: auth header + body.
		if got := r.Header.Get("Authorization"); got != "Bearer tenant-secret-xyz" {
			t.Errorf("Authorization header: got %q, want \"Bearer tenant-secret-xyz\"", got)
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
		reply := []string{"Hello", " from", " the configured gateway test!"}
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
		// the configured gateway does, not the OpenAI-standard [DONE] sentinel).
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

// TestGatewayStreamParsing drives a real HTTP round-trip through a
// stub server and asserts:
//   - upstream model is MiniMax-M3 (registry/handle ignored)
//   - system prompt is hoisted into a system message
//   - SSE chunks are concatenated into a single text reply
//   - usage block on the terminal frame is mapped to ChunkMeta
//   - the loop contract (always Done=true) is satisfied even though
//     the configured gateway does NOT emit a [DONE] sentinel
func TestGatewayStreamParsing(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(fakeGatewaySSEHandler(t, &captured))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{URL: srv.URL + "/v1/chat/completions", ExternalModel: "Upstream-1", APIKey: "tenant-secret-xyz"})
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
	if gotText != "Hello from the configured gateway test!" {
		t.Errorf("assembled text: got %q, want %q", gotText, "Hello from the configured gateway test!")
	}
	if gotMeta == nil {
		t.Fatal("terminal chunk missing Meta (usage block dropped)")
	}
	if gotMeta.PromptTokens != 7 || gotMeta.CompletionTokens != 5 || gotMeta.TotalTokens != 12 {
		t.Errorf("usage: got %+v, want {7 5 12}", gotMeta)
	}
	// The fingerprint names the endpoint and the model: two providers are
	// two fingerprints, and a fingerprint that named one fixed host would
	// misreport every other one.
	if !strings.Contains(gotMeta.SystemFingerprint, "/Upstream-1") {
		t.Errorf("SystemFingerprint missing the model tag: %q", gotMeta.SystemFingerprint)
	}
	// Verify the upstream saw the right model + system prompt hoist.
	if captured["model"] != "Upstream-1" {
		t.Errorf("upstream model: got %v, want Upstream-1", captured["model"])
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

// TestGatewayErrorStatus ensures non-2xx responses surface a readable
// error (with the upstream body truncated to fit a log line).
func TestGatewayErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad model"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewGatewayWithConfig(GatewayConfig{URL: srv.URL + "/v1/chat/completions", ExternalModel: "Upstream-1", APIKey: "tenant-secret-xyz"})
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

// TestGatewayIntegration runs only when GATEWAY_INTEGRATION=1 (CI /
// verifier hook). It hits the real the configured gateway endpoint and confirms
// the SSE framing works end-to-end against the live gateway. We
// skip by default because the dev sandbox is the only environment
// with confirmed egress; running this in an offline sandbox would
// just hang the test.
func TestGatewayIntegration(t *testing.T) {
	if os.Getenv("GATEWAY_INTEGRATION") != "1" {
		t.Skip("set GATEWAY_INTEGRATION=1 to run the live integration test")
	}
	c := NewGateway()
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
	t.Logf("integration ok, got %d chars: %q", len(gotText), truncateGateway(gotText, 80))
}

// keep bufio referenced even when all current tests go through
// net/http's reader; future tests that hand-roll an SSE pipe will
// use this import to avoid a "imported and not used" build break.
var _ = bufio.NewScanner

// TestGatewayReasoningTokens pins the cycle 100 addition: the gateway's
// usage.completion_tokens_details.reasoning_tokens is surfaced on ChunkMeta so
// the conversation view can render the recovered "Thought for Ns, N tokens"
// step. Shape captured from a live the configured gateway response:
//
//	"usage":{"total_tokens":212,"prompt_tokens":184,"completion_tokens":28,
//	         "completion_tokens_details":{"reasoning_tokens":24}, ...}
func TestGatewayReasoningTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":184,"+
			"\"completion_tokens\":28,\"total_tokens\":212,"+
			"\"completion_tokens_details\":{\"reasoning_tokens\":24}}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{URL: srv.URL, ExternalModel: "Upstream-1"})
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

// TestGatewayTimeoutDefaults pins the per-request timeout semantics added in cycle
// 117: 0 keeps the client's 60s default (the interactive path), a negative value
// (GatewayTimeoutUnlimited) survives withDefaults untouched so the request is
// bounded only by the caller's context (the headless path).
func TestGatewayTimeoutDefaults(t *testing.T) {
	if got := (GatewayConfig{}).withDefaults().TimeoutSeconds; got != 60 {
		t.Errorf("zero config TimeoutSeconds = %d, want 60", got)
	}
	if got := (GatewayConfig{TimeoutSeconds: GatewayTimeoutUnlimited}).withDefaults().TimeoutSeconds; got != GatewayTimeoutUnlimited {
		t.Errorf("GatewayTimeoutUnlimited became %d, want %d", got, GatewayTimeoutUnlimited)
	}
	if got := (GatewayConfig{TimeoutSeconds: 5}).withDefaults().TimeoutSeconds; got != 5 {
		t.Errorf("explicit 5 became %d", got)
	}
	// The unlimited config must still produce a usable client.
	if c := NewGatewayWithConfig(GatewayConfig{TimeoutSeconds: GatewayTimeoutUnlimited}); c == nil {
		t.Error("NewGatewayWithConfig returned nil")
	}
}
