// Package models — models_test.go
//
// Tests are split into three groups:
//
//  1. Registry tests (TestRegistry*): pure in-memory, no I/O. These verify
//     the add/remove/get/save/load contract.
//
//  2. Provider tests (TestProvider*): spin up an httptest.NewServer that
//     emulates each provider's wire format, and exercise the provider
//     end-to-end through the public Chat / Stream methods.
//
//  3. Live E2E test (TestAgwLiveE2E): skipped unless AGY_LIVE=1 is set in
//     the env, because the test depends on agw.k0s.io being reachable.
//     The smoke test verifies the actual recovery: a real OpenAI-shape
//     provider talking to a real gateway should NOT return "mock reply".
//
// The live test is the one that proves the cycle 33 spec's headline claim
// ("让 chadgpt/openagy 原生支持自定义模型, 跟 kelvinzer0 思路, 但不做
// binary patch") holds.

package models

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
)

// ---- Registry tests ----------------------------------------------------

func TestRegistryAddRemoveGet(t *testing.T) {
	r := &Registry{}
	if err := r.Add(Model{
		Name:             "agw",
		Provider:         "agw",
		APIURL:           "https://agw.k0s.io/v1/chat/completions",
		ExternalModelName: "MiniMax-M3",
		APIKey:           "-",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("Len after Add = %d, want 1", r.Len())
	}
	m, ok := r.GetByName("agw")
	if !ok {
		t.Fatal("GetByName(agw) returned not-found")
	}
	if m.ExternalModelName != "MiniMax-M3" {
		t.Errorf("ExternalModelName = %q, want MiniMax-M3", m.ExternalModelName)
	}
	if !r.Remove("agw") {
		t.Error("Remove returned false; want true")
	}
	if r.Len() != 0 {
		t.Errorf("Len after Remove = %d, want 0", r.Len())
	}
	if _, ok := r.GetByName("agw"); ok {
		t.Error("GetByName(agw) after Remove returned found")
	}
}

func TestRegistryValidate(t *testing.T) {
	cases := []struct {
		name    string
		m       Model
		wantErr bool
	}{
		{"ok", Model{Name: "x", Provider: "openai", APIURL: "https://x", ExternalModelName: "y"}, false},
		{"no name", Model{Provider: "openai", APIURL: "https://x", ExternalModelName: "y"}, true},
		{"no provider", Model{Name: "x", APIURL: "https://x", ExternalModelName: "y"}, true},
		{"no url", Model{Name: "x", Provider: "openai", ExternalModelName: "y"}, true},
		{"no model", Model{Name: "x", Provider: "openai", APIURL: "https://x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestRegistrySaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r := &Registry{}
	if err := r.Add(Model{Name: "agw", Provider: "agw", APIURL: "https://agw.k0s.io/v1/chat/completions", ExternalModelName: "MiniMax-M3", APIKey: "-", ContextWindow: 128000, TimeoutSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(Model{Name: "openai", Provider: "openai", APIURL: "https://api.openai.com/v1/chat/completions", ExternalModelName: "gpt-4o-mini", APIKey: "sk-test"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveToFile(path, r); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	r2, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if r2.Len() != 2 {
		t.Fatalf("loaded Len = %d, want 2", r2.Len())
	}
	if m, ok := r2.GetByName("agw"); !ok || m.ContextWindow != 128000 || m.TimeoutSeconds != 30 {
		t.Errorf("round-trip lost fields: %+v", m)
	}
}

func TestRegistryLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	r, err := LoadFromFile(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("LoadFromFile on missing file: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("missing file should yield empty registry, got %d", r.Len())
	}
	if r.Path() == "" {
		t.Error("Path() should be set even on missing-file load")
	}
}

func TestRegistryLowercaseProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{"models":[{"name":"X","provider":"OpenAI","api_url":"https://api.openai.com/v1/chat/completions","external_model_name":"gpt-4o-mini","api_key":"sk"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := r.GetByName("X")
	if m.Provider != "openai" {
		t.Errorf("provider = %q, want lowercased 'openai'", m.Provider)
	}
}

func TestRegistryDefaultConfigPath(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")
	t.Setenv("XDG_CONFIG_HOME", "")
	got := DefaultConfigPath()
	want := "/tmp/home/.config/openagy/models.json"
	if got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got = DefaultConfigPath()
	want = "/tmp/xdg/openagy/models.json"
	if got != want {
		t.Errorf("DefaultConfigPath() with XDG = %q, want %q", got, want)
	}
}

// ---- Provider tests (mocked) ------------------------------------------

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
		_ = body // model echoed in reply
		w.Header().Set("content-type", "application/json")
		if body.Stream {
			// Stream a couple of deltas + a usage block + [DONE].
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi-from-openai\"}}]}\n\n")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n")
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return
		}
		// Non-streaming reply.
		_ = json.NewEncoder(w).Encode(openAIResponseBody{
			ID:    "chatcmpl-test",
			Model: body.Model,
			Choices: []struct {
				Index        int `json:"index"`
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
	// Stream path.
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

func TestProviderAgwSendsBearerDash(t *testing.T) {
	// agw.k0s.io expects literally "Bearer -" as the dev sentinel. We
	// verify the provider sends that exact value when api_key is "-".
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	m := Model{
		Name: "agw", Provider: "agw",
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

func TestProviderAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			// Two named events: a delta, then message_stop.
			fmt.Fprint(w, "event: content_block_delta\n")
			fmt.Fprint(w, "data: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hello-ant\"}}\n\n")
			fmt.Fprint(w, "event: message_stop\n")
			fmt.Fprint(w, "data: {}\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			ID:   "msg_test",
			Model: body.Model,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "text", Text: "hello-ant"}},
			StopReason: "end_turn",
			Usage:      struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 4, OutputTokens: 2},
		})
	}))
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

func TestAgentContextInstall(t *testing.T) {
	dir := t.TempDir()
	paths, err := InstallContext(dir, ModeLite)
	if err != nil {
		t.Fatalf("InstallContext lite: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("InstallContext returned %d paths, want 2", len(paths))
	}
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if len(body) < 1000 {
			t.Errorf("%s only %d bytes, expected >= 1000", p, len(body))
		}
		if !strings.Contains(string(body), "openagy") {
			t.Errorf("%s missing 'openagy' string", p)
		}
	}
	// Strip mode.
	if _, err := InstallContext(dir, ModeStrip); err != nil {
		t.Fatalf("InstallContext strip: %v", err)
	}
	stripBody, _ := os.ReadFile(filepath.Join(dir, "agent-context.md"))
	if !strings.Contains(string(stripBody), "Recovery timeline") {
		t.Error("strip mode should include 'Recovery timeline' section")
	}
	// Passthrough mode is a no-op (but not an error).
	paths, err = InstallContext(dir, ModePassthrough)
	if err != nil {
		t.Fatalf("InstallContext passthrough: %v", err)
	}
	if paths != nil {
		t.Errorf("passthrough should return nil paths, got %v", paths)
	}
}

func TestAgentContextSetGetMode(t *testing.T) {
	// Reset to known state.
	defer SetMode(string(ModeLite))
	if GetMode() != ModeLite {
		t.Fatalf("default GetMode() = %q, want %q", GetMode(), ModeLite)
	}
	if err := SetMode("strip"); err != nil {
		t.Fatal(err)
	}
	if GetMode() != ModeStrip {
		t.Errorf("after SetMode('strip') GetMode() = %q, want strip", GetMode())
	}
	if err := SetMode("BOGUS"); err == nil {
		t.Error("SetMode('BOGUS') should error")
	}
}

func TestNewProviderDispatch(t *testing.T) {
	// Make sure the NewProvider switch is exhaustive and never returns the
	// "unknown provider" error for the seven supported ones.
	supported := []string{"openai", "anthropic", "ollama", "openrouter", "agw", "google", "custom"}
	for _, prov := range supported {
		m := Model{
			Name: "x", Provider: prov,
			APIURL:           "http://localhost:0",
			ExternalModelName: "m",
		}
		// google is implemented as a stub; it's allowed to construct
		// successfully but the constructor should not return an error.
		if _, err := NewProvider(m); err != nil {
			t.Errorf("NewProvider(%q) = %v", prov, err)
		}
	}
	if _, err := NewProvider(Model{Name: "x", Provider: "weird", APIURL: "u", ExternalModelName: "m"}); err == nil {
		t.Error("NewProvider(weird) should error")
	}
}

func TestProviderOpenAIShapeAcceptsNoAPIKey(t *testing.T) {
	// ollama doesn't want an auth header even if api_key is set.
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

// ---- Live E2E (gated) ---------------------------------------------------

func TestAgwLiveE2E(t *testing.T) {
	if os.Getenv("AGY_LIVE") != "1" {
		t.Skip("set AGY_LIVE=1 to run live agw.k0s.io test")
	}
	// We want to verify that a real openai-shape provider, talking to the
	// real agw.k0s.io gateway with the dev sentinel, returns a real answer
	// (NOT "mock reply"). This is the cycle 33 headline claim.
	m := Model{
		Name:             "agw",
		Provider:         "agw",
		APIURL:           "https://agw.k0s.io/v1/chat/completions",
		ExternalModelName: "MiniMax-M3",
		APIKey:           "-",
		TimeoutSeconds:    30,
	}
	p, err := NewProvider(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "SMOKE"}},
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
	t.Logf("agw live response: %d tokens, content[0:120]=%q", resp.TotalTokens, truncate(resp.Content, 120))
}

// ---- Sanity: compile-time check that all 7 providers implement Provider

var (
	_ Provider = (*openAIShapeProvider)(nil)
	_ Provider = (*anthropicProvider)(nil)
	_ Provider = (*googleProvider)(nil)
)

// ---- helpers (test-only) -----------------------------------------------

// silentReader wraps io.Reader and discards the data, used by the future
// streaming tests where we don't care about the body.
type silentReader struct{ r io.Reader }

func (s *silentReader) Read(p []byte) (int, error) { return s.r.Read(p) }

// guard against unused warnings on helpers that aren't pulled in.
var _ = silentReader{}
var _ sync.Mutex
