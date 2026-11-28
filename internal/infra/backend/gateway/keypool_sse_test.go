package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retrytest"
)

func TestKeyPool_RotationCooldownAndRecovery(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	pool := NewKeyPool(
		[]KeyAccount{
			{ID: "acct-1", APIKey: "sk-1"},
			{ID: "acct-2", APIKey: "sk-2"},
		},
		WithKeyPoolClock(func() time.Time { return now }),
		WithKeyPoolCooldown(30*time.Second, 5*time.Minute),
	)
	if pool == nil || pool.Len() != 2 {
		t.Fatalf("expected pool len 2, got %v", pool)
	}

	id, key := pool.CurrentKey()
	if id != "acct-1" || key != "sk-1" {
		t.Fatalf("initial key = (%q, %q), want (acct-1, sk-1)", id, key)
	}

	// 429 on sk-1 rotates immediately to sk-2.
	hasAlt := pool.MarkFailure("sk-1", &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":"rate limit"}`)})
	if !hasAlt {
		t.Fatal("expected healthy alternative sk-2")
	}
	id, key = pool.CurrentKey()
	if id != "acct-2" || key != "sk-2" {
		t.Fatalf("rotated key = (%q, %q), want (acct-2, sk-2)", id, key)
	}

	// 401 on sk-2 puts both in cooldown; soonest-expiring (sk-1) is selected.
	now = now.Add(5 * time.Second)
	hasAlt = pool.MarkFailure("sk-2", &apierror.HTTPError{StatusCode: http.StatusUnauthorized, Body: []byte(`{"error":"bad key"}`)})
	if hasAlt {
		t.Fatal("expected no healthy alternative when all keys are in cooldown")
	}
	id, key = pool.CurrentKey()
	if id != "acct-1" || key != "sk-1" {
		t.Fatalf("soonest-expiring key = (%q, %q), want (acct-1, sk-1)", id, key)
	}

	// Advance past sk-1 cooldown and mark success -> sk-1 stays healthy.
	now = now.Add(30 * time.Second)
	pool.MarkSuccess("sk-1")
	id, key = pool.CurrentKey()
	if id != "acct-1" || key != "sk-1" {
		t.Fatalf("recovered key = (%q, %q), want (acct-1, sk-1)", id, key)
	}
}

func TestGatewayClient_RotatesAPIKeysOn401And429WithoutSleeping(t *testing.T) {
	var mu sync.Mutex
	var seenAuth []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		seenAuth = append(seenAuth, auth)
		mu.Unlock()

		switch auth {
		case "Bearer sk-bad":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		case "Bearer sk-quota":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`))
		case "Bearer sk-good":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok-from-sk-good\"}}]}\n\ndata: [DONE]\n\n"))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	pool := NewKeyPool([]KeyAccount{
		{ID: "bad", APIKey: "sk-bad"},
		{ID: "quota", APIKey: "sk-quota"},
		{ID: "good", APIKey: "sk-good"},
	})
	client := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL,
		APIKey:        "sk-bad",
		KeyPool:       pool,
		ExternalModel: "gpt-4o",
	})

	start := time.Now()
	ch, err := client.Stream(context.Background(), backend.StreamRequest{
		Model:    "gpt-4o",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text string
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("unexpected chunk error: %v", c.Error)
		}
		text += c.Text
	}
	if text != "ok-from-sk-good" {
		t.Fatalf("text = %q, want ok-from-sk-good", text)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("expected immediate key rotation without sleeping, took %v", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seenAuth) != 3 {
		t.Fatalf("expected 3 attempts across 3 keys, got %v", seenAuth)
	}
}

func TestGatewaySSE_TypedErrorClassification(t *testing.T) {
	// These subtests are about *how a mid-stream frame is classified*, and
	// every server below answers the same failing frame on every attempt. A
	// mid-stream 429 / 5xx is retried now (see retry_midstream_test.go), so the
	// budget is pinned to zero: one attempt, classified, reported. That is the
	// stronger assertion — it fails if the transport starts retrying something
	// this test says it should not. Retry behaviour has its own tests.
	noRetry := func() context.Context { return retrytest.NoRetries(context.Background()) }

	t.Run("OpenAI mid-stream error frame", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial \"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"You exceeded your current quota\",\"type\":\"insufficient_quota\",\"code\":\"insufficient_quota\"}}\n\n"))
		}))
		defer srv.Close()

		client := NewGatewayWithConfig(GatewayConfig{URL: srv.URL, APIKey: "sk-test", ExternalModel: "gpt-4o"})
		ch, err := client.Stream(noRetry(), backend.StreamRequest{
			Model:    "gpt-4o",
			Messages: []backend.Message{{Role: "user", Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		var gotErr error
		var text string
		for c := range ch {
			text += c.Text
			if c.Error != nil {
				gotErr = c.Error
			}
		}
		if text != "partial " {
			t.Fatalf("text = %q, want 'partial '", text)
		}
		var he *apierror.HTTPError
		if !errors.As(gotErr, &he) {
			t.Fatalf("expected *apierror.HTTPError, got %T: %v", gotErr, gotErr)
		}
		if he.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", he.StatusCode)
		}
		if !apierror.IsQuotaReachedErr(gotErr) {
			t.Fatalf("expected IsQuotaReachedErr=true for %v", gotErr)
		}
	})

	t.Run("Anthropic mid-stream overloaded_error and authentication_error", func(t *testing.T) {
		cases := []struct {
			name       string
			sse        string
			wantStatus int
			check      func(error) bool
		}{
			{
				name:       "overloaded_error",
				sse:        "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
				wantStatus: 529,
				check:      apierror.IsRetryableStatusErr,
			},
			{
				name:       "authentication_error",
				sse:        "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"invalid x-api-key\"}}\n\n",
				wantStatus: http.StatusUnauthorized,
				check:      apierror.IsAuthFailureErr,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte(tc.sse))
				}))
				defer srv.Close()

				client := NewGatewayWithConfig(GatewayConfig{
					URL:           srv.URL,
					APIKey:        "sk-ant",
					ExternalModel: "claude-sonnet-4-6",
					Type:          GatewayProtocolAnthropic,
				})
				// A mid-stream 5xx / 429 is retried now (see
				// retry_midstream_test.go), and this server answers the same
				// failing frame on every attempt, so the budget is pinned to zero.
				ch, err := client.Stream(noRetry(), backend.StreamRequest{
					Model:    "claude-sonnet-4-6",
					Messages: []backend.Message{{Role: "user", Content: "hi"}},
				})
				if err != nil {
					t.Fatalf("Stream: %v", err)
				}
				var gotErr error
				for c := range ch {
					if c.Error != nil {
						gotErr = c.Error
					}
				}
				var he *apierror.HTTPError
				if !errors.As(gotErr, &he) {
					t.Fatalf("expected *apierror.HTTPError, got %T: %v", gotErr, gotErr)
				}
				if he.StatusCode != tc.wantStatus {
					t.Fatalf("status = %d, want %d", he.StatusCode, tc.wantStatus)
				}
				if !tc.check(gotErr) {
					t.Fatalf("classification check failed for %v", gotErr)
				}
			})
		}
	})

	t.Run("Responses mid-stream rate_limit_exceeded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"code\":\"rate_limit_exceeded\",\"message\":\"Rate limit reached\"}\n\n"))
		}))
		defer srv.Close()

		client := NewGatewayWithConfig(GatewayConfig{
			URL:           srv.URL,
			APIKey:        "sk-resp",
			ExternalModel: "gpt-5",
			Type:          GatewayProtocolResponses,
		})
		ch, err := client.Stream(noRetry(), backend.StreamRequest{
			Model:    "gpt-5",
			Messages: []backend.Message{{Role: "user", Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		var gotErr error
		for c := range ch {
			if c.Error != nil {
				gotErr = c.Error
			}
		}
		var he *apierror.HTTPError
		if !errors.As(gotErr, &he) {
			t.Fatalf("expected *apierror.HTTPError, got %T: %v", gotErr, gotErr)
		}
		if he.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", he.StatusCode)
		}
		if !apierror.IsRetryableStatusErr(gotErr) {
			t.Fatalf("expected IsRetryableStatusErr=true for %v", gotErr)
		}
	})
}
