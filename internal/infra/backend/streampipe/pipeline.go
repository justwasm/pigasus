package streampipe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// ExecuteWithRetry runs an operation, retrying on connection drops or
// retryable HTTP 429/5xx status codes honoring Retry-After headers and
// exponential backoff.
func ExecuteWithRetry[T any](ctx context.Context, endpointURL string, op func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for attempt := 0; attempt <= retry.MaxConnectionRetries; attempt++ {
		if attempt > 0 {
			delay := retry.NextConnectionRetryDelay(attempt - 1)
			if rDelay := apierror.RetryAfterDurationErr(lastErr); rDelay > 0 {
				delay = rDelay
				if delay > retry.MaxConnectionRetryInterval {
					delay = retry.MaxConnectionRetryInterval
				}
			}
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := op(ctx)
		if err == nil {
			return result, nil
		}
		isRetryable := retry.IsConnectionDropped(err) || apierror.IsRetryableStatusErr(err)
		if !isRetryable {
			return zero, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = &retry.ConnectionDroppedError{URL: endpointURL, Err: errors.New("max connection retries exceeded")}
	}
	return zero, lastErr
}

// Pipeline manages the streaming execution and mid-turn retry for a typed request.
type Pipeline[Req any] struct {
	// EndpointURL is used for error wrapping and classification.
	EndpointURL string

	// Open issues one HTTP request for the given request payload.
	Open func(ctx context.Context, req Req) (*http.Response, error)

	// ReadBody reads one HTTP response body into chunks. It returns nil on clean
	// completion, or an error if the read failed (e.g., connection drop).
	ReadBody func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error

	// MaxRetries is the maximum connection drop retry budget (defaults to retry.MaxConnectionRetries).
	MaxRetries int
}

// Run executes the streaming pipeline with automatic mid-turn retry on connection drops.
func (p Pipeline[Req]) Run(ctx context.Context, initialResp *http.Response, req Req, out chan<- backend.StreamChunk) {
	resp := initialResp
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	maxRetries := p.MaxRetries
	if maxRetries <= 0 {
		maxRetries = retry.MaxConnectionRetries
	}

	for attempt := 0; ; attempt++ {
		err := p.ReadBody(ctx, resp, out)
		if err == nil || !retry.IsConnectionDropped(err) {
			return
		}
		if attempt >= maxRetries {
			out <- backend.StreamChunk{Error: err, Done: true}
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}

		delay := retry.NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
			return
		case <-time.After(delay):
		}

		newResp, openErr := p.Open(ctx, req)
		if openErr != nil {
			if !retry.IsConnectionDropped(openErr) {
				out <- backend.StreamChunk{Error: openErr, Done: true}
				return
			}
			resp = nil
			continue
		}
		resp = newResp
	}
}

// ScanSSELines reads an HTTP response body with an activity idle timeout,
// scanning SSE text lines and feeding them to handleLine. It returns
// (true, nil) if handleLine signaled done, or (false, err) if scanning failed.
func ScanSSELines(
	ctx context.Context,
	body io.ReadCloser,
	maxBufferSize int,
	endpointURL string,
	handleLine func(line string) (done bool, err error),
) (bool, error) {
	if maxBufferSize <= 0 {
		maxBufferSize = 1 << 20 // 1 MB default
	}
	idleR := retry.NewIdleTimeoutReader(body, retry.StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), maxBufferSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		line := scanner.Text()
		done, err := handleLine(line)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, retry.ClassifyTransportError(fmt.Errorf("sse read: %w", err), endpointURL)
	}
	return false, nil
}
