// Package models — provider.go
//
// Maps registry provider strings to concrete Provider implementations.
package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

// NewProvider is the single entry point used by the registry, the CLI, and
// the agent loop. It maps the registry's `provider` string to one of the
// concrete implementations. The HTTP client is shared and configured once
// per process.
func NewProvider(m Model) (Provider, error) {
	timeout := time.Duration(m.Timeout()) * time.Second
	httpClient := &http.Client{
		Timeout: timeout,
	}
	label := strings.ToLower(strings.TrimSpace(m.Provider))
	switch label {
	case "openai", "openrouter", "ollama", "custom":
		return &openAIShapeProvider{
			model:      m,
			httpClient: httpClient,
			label:      label,
		}, nil
	case "anthropic":
		return &anthropicProvider{
			model:      m,
			httpClient: httpClient,
		}, nil
	case "google":
		return &googleProvider{model: m, httpClient: httpClient}, nil
	default:
		return nil, fmt.Errorf("models: unknown provider %q", m.Provider)
	}
}

// googleProvider is a placeholder.
type googleProvider struct {
	model      Model
	httpClient *http.Client
}

// Name implements Provider.
func (p *googleProvider) Name() string { return "google" }

// Chat implements Provider.
func (p *googleProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, errors.New(
		"google: Gemini native client is not implemented in this build. " +
			"Use provider=openai with api_url pointing at a Gemini-compatible OpenAI proxy " +
			"(e.g. " + endpoints.Current().GenerativeLanguage + "/v1beta/openai/chat/completions).")
}

// Stream implements Provider.
func (p *googleProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent, 1)
	out <- StreamEvent{
		Error: errors.New(
			"google: Gemini native client is not implemented in this build. " +
				"Use provider=openai with api_url pointing at a Gemini-compatible OpenAI proxy."),
		Done: true,
	}
	close(out)
	return out, nil
}

// truncate keeps error messages short enough to fit in a single log line.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
