// retry_test.go — the CloudCode transport retries a dropped connection.
//
// 【一手】 AGY 1.4.1: "the CLI now retries the request instead" — the
// CloudCode client retries a socket that drops mid-stream.
//
// The test runs against an httptest server that scripts the failure
// modes — no network egress required.
package cloudcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// TestCloudCode_ConnectionDropRetry verifies that CloudCodeClient transparently
// retries on socket disconnect errors (AGY 1.4.1).
func TestCloudCode_ConnectionDropRetry(t *testing.T) {
	savedInitial := retry.InitialConnectionRetryInterval
	savedMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 5 * time.Millisecond
	retry.MaxConnectionRetryInterval = 20 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = savedInitial
		retry.MaxConnectionRetryInterval = savedMax
	}()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		if att == 1 {
			// Simulate connection dropped mid-stream (e.g. Wi-Fi change/sleep)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"part1 \"}]}}]}}\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			// Close connection abruptly without finishReason or [DONE]
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
			return
		}
		// Second attempt succeeds
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"complete response\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewCloudCodeWithConfig(CloudCodeConfig{
		Endpoint: srv.URL,
		Model:    "gemini-2.5-flash",
	})
	client.token = func(ctx context.Context) (string, error) {
		return "test-token", nil
	}

	ch, err := client.Stream(context.Background(), backend.StreamRequest{
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var gotText strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		gotText.WriteString(chunk.Text)
	}

	if attempts.Load() < 2 {
		t.Errorf("server saw %d attempts; want >= 2 (retry on drop)", attempts.Load())
	}
	if !strings.Contains(gotText.String(), "complete response") {
		t.Errorf("final text = %q, want 'complete response'", gotText.String())
	}
}
