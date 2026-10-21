package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/tools"
)

// scriptedAGW serves the agw SSE shape (chunks + a usage-only terminal frame,
// no [DONE] sentinel) the way agw.k0s.io does, so the run exercises the real
// transport through PiG's session rather than a stub provider.
func scriptedAGW(t *testing.T, pieces ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i, piece := range pieces {
			frame := map[string]any{
				"id": "test-id", "object": "chat.completion.chunk", "model": "MiniMax-M3",
				"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": piece}}},
				"created": 1791999999,
			}
			data, _ := json.Marshal(frame)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil && i == 0 {
				flusher.Flush()
			}
		}
		final, _ := json.Marshal(map[string]any{
			"id": "test-id", "object": "chat.completion.chunk", "model": "MiniMax-M3",
			"choices": []any{}, "created": 1791999999,
			"usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 5, "total_tokens": 12},
		})
		fmt.Fprintf(w, "data: %s\n\n", final)
	}))
}

// TestRunPigPrintTurnStreamsText drives one print turn on PiG's coding runtime
// end to end: the clone's agw provider rides in as the model's ai.Provider, the
// session streams the answer back, and the synthesised chunks reach the same
// handler runPrintMode uses.
func TestRunPigPrintTurnStreamsText(t *testing.T) {
	srv := scriptedAGW(t, "Hello", " from", " the pig runtime!")
	defer srv.Close()

	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	client := backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{
		URL:           srv.URL,
		ExternalModel: "MiniMax-M3",
		HTTPClient:    srv.Client(),
	})

	var mu sync.Mutex
	var got strings.Builder
	var done bool
	var meta *backend.ChunkMeta
	handler := func(chunk backend.StreamChunk) error {
		mu.Lock()
		defer mu.Unlock()
		got.WriteString(chunk.Text)
		if chunk.Done {
			done = true
			meta = chunk.Meta
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	denials, err := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:   client,
		Model:     "MiniMax-M3",
		Prompt:    "say hi",
		Catalog:   tools.Catalog(dir),
		Policy:    nil,
		Handler:   handler,
		Workspace: dir,
		AgentDir:  agentDir,
	})
	if err != nil {
		t.Fatalf("runPigPrintTurn: %v", err)
	}
	if len(denials) != 0 {
		t.Fatalf("unexpected denials: %v", denials)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "Hello from the pig runtime!"; got.String() != want {
		t.Fatalf("streamed text = %q, want %q", got.String(), want)
	}
	if !done {
		t.Fatal("no terminating chunk reached the handler")
	}
	if meta == nil || meta.PromptTokens != 7 || meta.CompletionTokens != 5 || meta.TotalTokens != 12 {
		t.Fatalf("usage meta = %+v, want prompt=7 completion=5 total=12", meta)
	}
}

// TestRunPigPrintTurnSurfacesProviderError drives a failing gateway and asserts
// the turn reports the failure. PiG folds a provider error into an assistant
// message with a nil Go error beside it, so without the recovery in
// pigrt.Runtime.RunWith this run looks like an empty success — and api_error.go
// never gets the *backend.HTTPError it needs for the AGY_ERROR envelope and
// exit code 3.
func TestRunPigPrintTurnSurfacesProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// PiG retries a 429 with backoff; disable that so the test is one round trip.
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"retry":{"enabled":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	client := backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{
		URL:           srv.URL,
		ExternalModel: "MiniMax-M3",
		HTTPClient:    srv.Client(),
	})

	var mu sync.Mutex
	sawErrorChunk := false
	handler := func(chunk backend.StreamChunk) error {
		mu.Lock()
		defer mu.Unlock()
		if chunk.Error != nil {
			sawErrorChunk = true
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:   client,
		Model:     "MiniMax-M3",
		Prompt:    "hi",
		Catalog:   tools.Catalog(dir),
		Handler:   handler,
		Workspace: dir,
		AgentDir:  agentDir,
	})
	if err == nil {
		t.Fatal("a 429 turn must report an error, got nil")
	}
	mu.Lock()
	emitted := sawErrorChunk
	mu.Unlock()
	if !emitted {
		t.Fatal("the handler never saw the terminal error chunk (num_turns/step accounting depends on it)")
	}
	api, ok := classifyAPIError(err)
	if !ok {
		t.Fatalf("error not classified as an API failure: %v", err)
	}
	if api.ErrorCode != http.StatusTooManyRequests {
		t.Fatalf("api error code = %d, want 429", api.ErrorCode)
	}
	if !strings.Contains(api.Message, "Resource has been exhausted") {
		t.Fatalf("api message = %q", api.Message)
	}
}

// TestPrintShouldUsePigRuntime was the cycle-209 selection rule. Cycle 210
// removed the choice entirely: PiG is the only engine a print turn runs on, so
// the rule, the OPENAGY_RUNTIME switch and the clone's loop fallback are gone.
// What remains to pin is that every backend still yields a *ai.Model the
// session can call — the agw client through the native provider, the offline
// mock through the generic adapter.
func TestPigPrintModelForBackend(t *testing.T) {
	agw := backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{URL: "http://127.0.0.1:1", ExternalModel: "MiniMax-M3"})
	if model := pigPrintModel(agw, "claude-sonnet-4-6"); model == nil || model.Provider == nil {
		t.Fatal("agw backend must yield a model")
	} else if model.ID != "claude-sonnet-4-6" {
		t.Fatalf("model id = %q, want the label", model.ID)
	}
	if model := pigPrintModel(backend.NewMock(), "MiniMax-M3"); model == nil || model.Provider == nil {
		t.Fatal("mock backend must yield a model")
	}
}

// TestRunPigPrintTurnMockBackend proves the offline mock — what the shims and
// the parity harnesses drive — now runs on PiG's coding runtime too, through
// piagentbridge.BackendProvider. Before cycle 210 this backend kept the clone's
// loop because it had no provider to hand the session.
func TestRunPigPrintTurnMockBackend(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var got strings.Builder
	var done bool
	handler := func(chunk backend.StreamChunk) error {
		mu.Lock()
		defer mu.Unlock()
		got.WriteString(chunk.Text)
		if chunk.Done {
			done = true
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	denials, err := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:   backend.NewMock(),
		Model:     "MiniMax-M3",
		Prompt:    "hi",
		Catalog:   tools.Catalog(dir),
		Handler:   handler,
		Workspace: dir,
		AgentDir:  filepath.Join(dir, "agent"),
	})
	if err != nil {
		t.Fatalf("runPigPrintTurn: %v", err)
	}
	if len(denials) != 0 {
		t.Fatalf("unexpected denials: %v", denials)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got.String(), "mock reply from model=MiniMax-M3") {
		t.Fatalf("streamed text = %q, want the mock reply", got.String())
	}
	if !done {
		t.Fatal("no terminating chunk reached the handler")
	}
}

// TestRunPigPrintTurnReplaysHistory drives a resumed conversation and asserts
// the earlier turns reach the provider: the scripted gateway captures the
// request body, and the replay must appear as user/assistant messages ahead of
// the new prompt.
func TestRunPigPrintTurnReplaysHistory(t *testing.T) {
	var mu sync.Mutex
	var lastReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(body, &lastReq)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"model\":\"MiniMax-M3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}],\"created\":1}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"model\":\"MiniMax-M3\",\"choices\":[],\"created\":1,\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	dir := t.TempDir()
	client := backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{URL: srv.URL, ExternalModel: "MiniMax-M3", HTTPClient: srv.Client()})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := runPigPrintTurn(ctx, pigPrintConfig{
		Backend: client,
		Model:   "MiniMax-M3",
		Prompt:  "and then?",
		History: []pigrt.HistoryTurn{
			{Role: "user", Text: "what is the capital of France?"},
			{Role: "assistant", Text: "Paris."},
		},
		Catalog:   tools.Catalog(dir),
		Handler:   func(backend.StreamChunk) error { return nil },
		Workspace: dir,
		AgentDir:  filepath.Join(dir, "agent"),
	})
	if err != nil {
		t.Fatalf("runPigPrintTurn: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	roles := make([]string, 0, len(lastReq.Messages))
	contents := make([]string, 0, len(lastReq.Messages))
	for _, m := range lastReq.Messages {
		roles = append(roles, m.Role)
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "\n")
	for _, want := range []string{"what is the capital of France?", "Paris.", "and then?"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("request body missing %q; roles=%v contents=%q", want, roles, contents)
		}
	}
}
