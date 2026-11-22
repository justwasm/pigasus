package backendselect

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/infra/backend/failover"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

func TestBackendSelect_ModelConfiguredFallbacks(t *testing.T) {
	savedInitial := retry.InitialConnectionRetryInterval
	savedMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = savedInitial
		retry.MaxConnectionRetryInterval = savedMax
	}()

	var primaryCalls int32
	var secondaryCalls int32

	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		http.Error(w, `{"error":{"message":"RESOURCE_EXHAUSTED: quota exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer primarySrv.Close()

	secondarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondaryCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"secondary-ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer secondarySrv.Close()

	configtest.WriteProviders(t, `{"providers": {
		"primary-provider": {
			"type": "openai",
			"api_url": "`+primarySrv.URL+`/v1/chat/completions",
			"api_key": "k1",
			"models": [
				{"id": "model-primary", "name": "Primary Model", "fallbacks": ["model-secondary"]}
			]
		},
		"secondary-provider": {
			"type": "openai",
			"api_url": "`+secondarySrv.URL+`/v1/chat/completions",
			"api_key": "k2",
			"models": [
				{"id": "model-secondary", "name": "Secondary Model"}
			]
		}
	}}`)

	var logBuf bytes.Buffer
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("backend", "gateway", "")

	client := PickWithLog(fs, "model-primary", &logBuf, 5, noCatalogueLookup)
	ch, err := client.Stream(context.Background(), backend.StreamRequest{
		Model:    "model-primary",
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var text string
	for chunk := range ch {
		text += chunk.Text
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}
	}

	if text != "secondary-ok" {
		t.Errorf("got text %q, want 'secondary-ok'", text)
	}
	if atomic.LoadInt32(&primaryCalls) == 0 {
		t.Errorf("primaryCalls = 0, want >= 1")
	}
	if atomic.LoadInt32(&secondaryCalls) != 1 {
		t.Errorf("secondaryCalls = %d, want 1", atomic.LoadInt32(&secondaryCalls))
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "[failover]") {
		t.Errorf("expected failover diagnostic log, got:\n%s", logOutput)
	}
}

func TestBackendSelect_EnvFallbackChain(t *testing.T) {
	savedInitial := retry.InitialConnectionRetryInterval
	savedMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = savedInitial
		retry.MaxConnectionRetryInterval = savedMax
	}()

	var primaryCalls int32
	var secondaryCalls int32

	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		http.Error(w, `{"error":{"message":"RESOURCE_EXHAUSTED: rate limit exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer primarySrv.Close()

	secondarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondaryCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"env-fallback-ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer secondarySrv.Close()

	configtest.WriteProviders(t, `{"providers": {
		"prov1": {
			"type": "openai",
			"api_url": "`+primarySrv.URL+`/v1/chat/completions",
			"api_key": "k1",
			"models": [
				{"id": "model-a", "name": "Model A"}
			]
		},
		"prov2": {
			"type": "openai",
			"api_url": "`+secondarySrv.URL+`/v1/chat/completions",
			"api_key": "k2",
			"models": [
				{"id": "model-b", "name": "Model B"}
			]
		}
	}}`)

	t.Setenv("OPENAGY_FALLBACK_CHAIN", "model-b")

	var logBuf bytes.Buffer
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("backend", "gateway", "")

	client := PickWithLog(fs, "model-a", &logBuf, 5, noCatalogueLookup)
	ch, err := client.Stream(context.Background(), backend.StreamRequest{
		Model:    "model-a",
		Messages: []backend.Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	for c := range ch {
		text += c.Text
	}

	if text != "env-fallback-ok" {
		t.Errorf("got %q, want 'env-fallback-ok'", text)
	}
	if atomic.LoadInt32(&primaryCalls) == 0 {
		t.Errorf("primaryCalls = 0, want >= 1")
	}
	if atomic.LoadInt32(&secondaryCalls) != 1 {
		t.Errorf("secondaryCalls = %d, want 1", atomic.LoadInt32(&secondaryCalls))
	}
}

func TestBackendSelect_CircularAndSelfFallbacksDeduplicated(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {
		"prov1": {
			"type": "openai",
			"api_url": "https://example.com/v1/chat/completions",
			"api_key": "k1",
			"models": [
				{"id": "cycle-a", "name": "Cycle A", "fallbacks": ["cycle-a", "cycle-b", "cycle-a", "cycle-b"]},
				{"id": "cycle-b", "name": "Cycle B", "fallbacks": ["cycle-a"]}
			]
		}
	}}`)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("backend", "gateway", "")

	client := PickWithLog(fs, "cycle-a", nil, 5, noCatalogueLookup)
	fc, ok := client.(*failover.Client)
	if !ok {
		t.Fatalf("expected *failover.Client, got %T", client)
	}

	targets := fc.Targets()
	if len(targets) != 2 {
		t.Fatalf("expected exactly 2 targets after deduplication (primary + cycle-b), got %d: %+v", len(targets), targets)
	}
	if targets[0].Name != "configured:cycle-a" {
		t.Errorf("target 0 name = %q, want 'configured:cycle-a'", targets[0].Name)
	}
	if targets[1].Name != "fallback:cycle-b" {
		t.Errorf("target 1 name = %q, want 'fallback:cycle-b'", targets[1].Name)
	}
}
