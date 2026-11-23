package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

func TestGeminiStream_HonorsMaxRetriesFromContext(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
	}))
	defer srv.Close()

	client := NewGeminiWithConfig(GeminiConfig{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "gemini-2.5-pro",
	})

	ctx := backend.ContextWithMaxRetries(context.Background(), 0)
	chunks, err := client.Stream(ctx, backend.StreamRequest{
		Model:    "gemini-2.5-pro",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotErr error
	for c := range chunks {
		if c.Error != nil {
			gotErr = c.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 attempt with MaxRetries=0, got %d", got)
	}
}

func TestGeminiStream_NeverRetriesAuthOrQuotaErrors(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 500 * time.Millisecond
	retry.MaxConnectionRetryInterval = 1 * time.Second
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "401 unauthorized",
			status: http.StatusUnauthorized,
			body:   `{"error":{"message":"invalid api key"}}`,
		},
		{
			name:   "429 quota exhausted",
			status: http.StatusTooManyRequests,
			body:   `{"error":{"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded for quota metric"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := NewGeminiWithConfig(GeminiConfig{
				BaseURL: srv.URL,
				APIKey:  "test-key",
				Model:   "gemini-2.5-pro",
			})

			start := time.Now()
			chunks, err := client.Stream(context.Background(), backend.StreamRequest{
				Model:    "gemini-2.5-pro",
				Messages: []backend.Message{{Role: "user", Content: "hi"}},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for range chunks {
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("expected 1 attempt for %s, got %d", tc.name, got)
			}
			if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
				t.Fatalf("expected no retry sleep for %s, took %v", tc.name, elapsed)
			}
		})
	}
}
