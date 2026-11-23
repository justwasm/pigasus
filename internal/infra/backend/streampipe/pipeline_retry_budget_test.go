package streampipe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

func TestExecuteWithRetry_HonorsMaxRetriesFromContext(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	// With MaxRetries=0, a retryable 503 fails after 1 attempt without retrying.
	calls := 0
	ctx := backend.ContextWithMaxRetries(context.Background(), 0)
	_, err := ExecuteWithRetry(ctx, "http://test", func(ctx context.Context) (string, error) {
		calls++
		return "", &apierror.HTTPError{StatusCode: http.StatusServiceUnavailable, Body: []byte(`{"error":"unavailable"}`)}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt with MaxRetries=0, got %d", calls)
	}

	// With MaxRetries=1, it attempts twice (initial + 1 retry).
	calls = 0
	ctx1 := backend.ContextWithMaxRetries(context.Background(), 1)
	_, err = ExecuteWithRetry(ctx1, "http://test", func(ctx context.Context) (string, error) {
		calls++
		return "", &apierror.HTTPError{StatusCode: http.StatusServiceUnavailable, Body: []byte(`{"error":"unavailable"}`)}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts with MaxRetries=1, got %d", calls)
	}
}

func TestExecuteWithRetry_NeverRetriesQuotaOrAuthErrors(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 500 * time.Millisecond
	retry.MaxConnectionRetryInterval = 1 * time.Second
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	cases := []struct {
		name string
		err  error
	}{
		{
			name: "401 unauthorized",
			err:  &apierror.HTTPError{StatusCode: http.StatusUnauthorized, Body: []byte(`{"error":"invalid api key"}`)},
		},
		{
			name: "402 payment required",
			err:  &apierror.HTTPError{StatusCode: http.StatusPaymentRequired, Body: []byte(`{"error":"billing required"}`)},
		},
		{
			name: "403 forbidden",
			err:  &apierror.HTTPError{StatusCode: http.StatusForbidden, Body: []byte(`{"error":"forbidden"}`)},
		},
		{
			name: "429 insufficient quota",
			err:  &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			start := time.Now()
			_, err := ExecuteWithRetry(context.Background(), "http://test", func(ctx context.Context) (string, error) {
				calls++
				return "", tc.err
			})
			elapsed := time.Since(start)
			if err == nil {
				t.Fatal("expected error")
			}
			if calls != 1 {
				t.Fatalf("expected 1 attempt (no retry) for %s, got %d", tc.name, calls)
			}
			if elapsed > 100*time.Millisecond {
				t.Fatalf("expected immediate abort without sleeping for %s, took %v", tc.name, elapsed)
			}
		})
	}
}

func TestPipelineRun_HonorsMaxRetriesFromContextAndAbortsQuota(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	openCalls := 0
	readCalls := 0
	p := Pipeline[string]{
		EndpointURL: "http://test",
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			openCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: ok\n")),
			}, nil
		},
		ReadBody: func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
			readCalls++
			return &retry.ConnectionDroppedError{URL: "http://test", Err: io.ErrUnexpectedEOF}
		},
	}

	initialResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: ok\n")),
	}
	out := make(chan backend.StreamChunk, 4)
	ctx := backend.ContextWithMaxRetries(context.Background(), 0)
	p.Run(ctx, initialResp, "req", out)
	close(out)

	var gotErr error
	for c := range out {
		if c.Error != nil {
			gotErr = c.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected stream error")
	}
	if readCalls != 1 || openCalls != 0 {
		t.Fatalf("expected 1 read attempt and 0 re-opens with MaxRetries=0, got read=%d open=%d", readCalls, openCalls)
	}

	// Quota error mid-stream must not retry even without MaxRetries override.
	openCalls = 0
	readCalls = 0
	pQuota := Pipeline[string]{
		EndpointURL: "http://test",
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			openCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: err\n")),
			}, nil
		},
		ReadBody: func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
			readCalls++
			return &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`)}
		},
	}
	initialResp2 := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: err\n")),
	}
	out2 := make(chan backend.StreamChunk, 4)
	pQuota.Run(context.Background(), initialResp2, "req", out2)
	close(out2)
	if readCalls != 1 || openCalls != 0 {
		t.Fatalf("expected 1 read attempt and 0 re-opens for quota error, got read=%d open=%d", readCalls, openCalls)
	}
}
