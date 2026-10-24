package printmode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// recordingGateway serves the gateway SSE shape and keeps the request body, so a test
// can assert what the clone actually puts on the wire.
func recordingGateway(t *testing.T) (*httptest.Server, func() []byte) {
	t.Helper()
	var mu sync.Mutex
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = body
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunk, _ := json.Marshal(map[string]any{
			"id": "test-id", "object": "chat.completion.chunk", "model": "MiniMax-M3",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": "ok"}}},
			"created": 1791999999,
		})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		final, _ := json.Marshal(map[string]any{
			"id": "test-id", "object": "chat.completion.chunk", "model": "MiniMax-M3",
			"choices": []any{}, "created": 1791999999,
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
		fmt.Fprintf(w, "data: %s\n\n", final)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []byte {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

// TestRunPigPrintTurnSendsAgySystemPrompt proves the reference prompt reaches
// the wire: print mode -> pigrt -> PiG session -> the clone's gateway provider ->
// the HTTP request body. It is the end-to-end counterpart of
// internal/agyprompt's byte-for-byte capture test.
func TestRunPigPrintTurnSendsAgySystemPrompt(t *testing.T) {
	srv, body := recordingGateway(t)
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	client := backend.NewGatewayWithConfig(backend.GatewayConfig{
		URL:           srv.URL,
		ExternalModel: "MiniMax-M3",
		HTTPClient:    srv.Client(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:        client,
		Model:          "MiniMax-M3",
		Prompt:         "say hi",
		Catalog:        tools.Catalog(dir),
		Handler:        func(backend.StreamChunk) error { return nil },
		Workspace:      dir,
		AgentDir:       agentDir,
		ConversationID: "99999999-8888-7777-6666-555555555555",
	}); err != nil {
		t.Fatalf("runPigPrintTurn: %v", err)
	}

	raw := body()
	if len(raw) == 0 {
		t.Fatal("no request was recorded")
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Fatalf("first message is not the system prompt: %+v", req.Messages)
	}
	var system string
	if err := json.Unmarshal(req.Messages[0].Content, &system); err != nil {
		t.Fatalf("system content is not a JSON string: %v", err)
	}
	for _, want := range []string{
		"<identity>",
		"You are Antigravity, a powerful agentic AI coding assistant",
		"<user_information>",
		"Command Working Directory: " + dir,
		"Conversation ID: 99999999-8888-7777-6666-555555555555",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("wire system prompt is missing %q", want)
		}
	}
	if strings.Contains(system, "operating inside pig") {
		t.Fatal("wire system prompt is PiG's default, not the reference's")
	}
}
