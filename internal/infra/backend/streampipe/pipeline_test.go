package streampipe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

func TestExecuteWithRetry_Success(t *testing.T) {
	ctx := context.Background()
	calls := 0
	val, err := ExecuteWithRetry(ctx, "http://test", func(ctx context.Context) (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil || val != "ok" || calls != 1 {
		t.Fatalf("unexpected result: val=%q err=%v calls=%d", val, err, calls)
	}
}

func TestExecuteWithRetry_RetryThenSuccess(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	ctx := context.Background()
	calls := 0
	val, err := ExecuteWithRetry(ctx, "http://test", func(ctx context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, retry.ErrConnectionDropped
		}
		return 42, nil
	})
	if err != nil || val != 42 || calls != 3 {
		t.Fatalf("unexpected result: val=%d err=%v calls=%d", val, err, calls)
	}
}

func TestExecuteWithRetry_DNSTimeoutAndNotifier(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	var progressReports []backend.RetryProgress
	ctx := backend.ContextWithRetryNotifier(context.Background(), func(p backend.RetryProgress) {
		progressReports = append(progressReports, p)
	})

	calls := 0
	dnsErr := errors.New("Post \"https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse\": dial tcp: lookup daily-cloudcode-pa.googleapis.com: i/o timeout")
	val, err := ExecuteWithRetry(ctx, "https://daily-cloudcode-pa.googleapis.com", func(ctx context.Context) (string, error) {
		calls++
		if calls < 4 {
			return "", retry.ClassifyTransportError(dnsErr, "https://daily-cloudcode-pa.googleapis.com")
		}
		return "recovered", nil
	})

	if err != nil || val != "recovered" || calls != 4 {
		t.Fatalf("expected 4 calls and success, got calls=%d val=%q err=%v", calls, val, err)
	}

	if len(progressReports) == 0 {
		t.Fatal("expected retry progress reports, got none")
	}
	// Check that first report had attempt 2 and maxAttempts 10
	if progressReports[0].Attempt != 2 || progressReports[0].MaxAttempts != 10 {
		t.Errorf("expected attempt 2 / 10, got %+v", progressReports[0])
	}
}

func TestPipeline_Run_MidTurnRetry(t *testing.T) {
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = 1 * time.Millisecond
	retry.MaxConnectionRetryInterval = 5 * time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	}()

	ctx := context.Background()
	openCalls := 0
	readCalls := 0

	p := Pipeline[string]{
		EndpointURL: "http://test",
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			openCalls++
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString("attempt")),
			}, nil
		},
		ReadBody: func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
			readCalls++
			if readCalls == 1 {
				// Simulating connection drop mid-stream
				return retry.ErrConnectionDropped
			}
			out <- backend.StreamChunk{Text: "recovered"}
			out <- backend.StreamChunk{Done: true}
			return nil
		},
	}

	out := make(chan backend.StreamChunk, 5)
	initialResp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString("initial")),
	}

	p.Run(ctx, initialResp, "req", out)
	close(out)

	var chunks []backend.StreamChunk
	for c := range out {
		chunks = append(chunks, c)
	}

	if len(chunks) != 2 || chunks[0].Text != "recovered" || !chunks[1].Done {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
	if readCalls != 2 || openCalls != 1 {
		t.Fatalf("expected readCalls=2 openCalls=1, got readCalls=%d openCalls=%d", readCalls, openCalls)
	}
}

func TestScanSSELines(t *testing.T) {
	ctx := context.Background()
	data := "data: line 1\n\ndata: line 2\ndata: [DONE]\n"
	body := io.NopCloser(bytes.NewBufferString(data))

	var lines []string
	done, err := ScanSSELines(ctx, body, 1024, "http://test", func(line string) (bool, error) {
		lines = append(lines, line)
		if line == "data: [DONE]" {
			return true, nil
		}
		return false, nil
	})
	if err != nil || !done {
		t.Fatalf("ScanSSELines returned done=%v, err=%v", done, err)
	}
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
}
