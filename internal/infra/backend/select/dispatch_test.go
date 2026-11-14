// Package backendselect — dispatch_test.go
//
// A model the user configured must never be served by the shim, whichever
// spelling of its name the caller hands over. The 2026-11-14 report: the
// session's model was set to "DeepSeek-V4-Flash" (the display label, without
// the tier the catalogue spells into its rows); the picker's own lookup has no
// row for that string, the dispatch fell back to the raw name, the registry
// lookup found nothing, and every turn answered "mock reply from model=…".
package backendselect

import (
	"context"
	"flag"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

// noCatalogueLookup stands in for the picker's exact-label lookup when the
// label has no row: it answers "" for every name, which is what
// modelpicker.ResolveCatalogueID does for a tier-less label.
func noCatalogueLookup(string) string { return "" }

// newTierEchoServer serves the configured-model request shape tier_test.go
// already uses and records the body.
func newTierEchoServer(t *testing.T, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(tierEchoHandler(t, captured))
}

func TestBareDisplayLabelOfATieredModelReachesItsEndpoint(t *testing.T) {
	var captured map[string]any
	srv := newTierEchoServer(t, &captured)
	defer srv.Close()

	configtest.WriteProviders(t, `{"providers": {"deepseek": {
  "type": "openai",
  "api_url": "`+srv.URL+`/v1/chat/completions",
  "api_key": "-",
  "models": [
    {"id": "deepseek-v4-flash", "name": "DeepSeek-V4-Flash", "model": "DeepSeek-V4-Flash-Upstream",
     "reasoning_efforts": ["low", "high", "max"], "reasoning_effort": "high"}
  ]}}}`)

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("backend", "gateway", "")

	// The label as settings.json / the host's model menu spells it: no tier.
	client := Pick(fs, "DeepSeek-V4-Flash", noCatalogueLookup)
	stream, err := client.Stream(context.Background(), backend.StreamRequest{
		Model:    "DeepSeek-V4-Flash",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text string
	for chunk := range stream {
		text += chunk.Text
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
	}
	if text != "ok" {
		t.Fatalf("reply = %q: the model was not served by its configured endpoint", text)
	}
	if got := captured["model"]; got != "DeepSeek-V4-Flash-Upstream" {
		t.Errorf("model = %v, want DeepSeek-V4-Flash-Upstream", got)
	}
	if got := captured["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort = %v, want the model's default tier high", got)
	}
}

// The other spellings name the same model: the catalogue id, the display
// label with its tier, and the upstream id the provider documents.
func TestEverySpellingOfAConfiguredModelResolves(t *testing.T) {
	var captured map[string]any
	srv := newTierEchoServer(t, &captured)
	defer srv.Close()

	configtest.WriteProviders(t, `{"providers": {"deepseek": {
  "type": "openai",
  "api_url": "`+srv.URL+`/v1/chat/completions",
  "api_key": "-",
  "models": [
    {"id": "deepseek-v4-flash", "name": "DeepSeek-V4-Flash", "model": "DeepSeek-V4-Flash-Upstream",
     "reasoning_efforts": ["low", "high", "max"], "reasoning_effort": "high"}
  ]}}}`)

	for _, name := range []string{
		"DeepSeek-V4-Flash",
		"DeepSeek-V4-Flash (High)",
		"deepseek-v4-flash",
		"deepseek-v4-flash-high",
		"DeepSeek-V4-Flash-Upstream",
	} {
		if _, ok := resolveConfigured(name, noCatalogueLookup); !ok {
			t.Errorf("resolveConfigured(%q) found no model", name)
		}
	}
}

// A name nobody configured still reports itself, so the caller can say so.
func TestUnknownModelDoesNotResolve(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {}}`)
	if _, ok := resolveConfigured("not-a-model", noCatalogueLookup); ok {
		t.Fatal("resolveConfigured found a model for a name nobody configured")
	}
}

func TestModelSupportedAndWarningLogged(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {}}`)
	if ModelSupported("truly-unknown-model", noCatalogueLookup) {
		t.Fatal("ModelSupported should return false for unconfigured model")
	}
	var buf strings.Builder
	client := PickWithLog(nil, "truly-unknown-model", &buf, 0, noCatalogueLookup)
	if !backend.IsMock(client) {
		t.Fatal("expected mock backend for unconfigured model")
	}
	if !strings.Contains(buf.String(), "[warn] model \"truly-unknown-model\" is not configured") {
		t.Fatalf("expected warning in log, got %q", buf.String())
	}
}
