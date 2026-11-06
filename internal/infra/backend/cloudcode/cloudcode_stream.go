package cloudcode

import (
	"context"
	"net/http"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
	"github.com/justwasm/pigasus/internal/infra/backend/streampipe"
)

// cloudcode_stream.go — the transport policy around the CloudCode dialect.
//
// The dialect itself lives in the geminicode package: it owns the request
// envelope, the SSE frames and the thought-signature round trip. What lives
// here is the policy the application applies on top of it:
//
//   - a dropped connection is retried, with the reference's exponential
//     schedule, up to the shared budget (see gemini_api_key_retry.go);
//   - a stalled stream is aborted (see retry.NewIdleTimeoutReader);
//   - a non-2xx answer keeps the application's own HTTPError, so the
//     headless error reporter can classify it.
//
// The split is deliberate: the dialect is reusable on its own, and the
// policy is the application's to change.

// openWithRetry performs the request, retrying a dropped connection or
// retryable 429/5xx status error honoring Retry-After.
func (c *CloudCodeClient) openWithRetry(ctx context.Context, turn geminicode.Turn) (*http.Response, error) {
	return streampipe.ExecuteWithRetry(ctx, c.cfg.Endpoint, func(ctx context.Context) (*http.Response, error) {
		return c.open(ctx, turn)
	})
}

// open performs one request against the CloudCode endpoint.
func (c *CloudCodeClient) open(ctx context.Context, turn geminicode.Turn) (*http.Response, error) {
	resp, err := c.cc.Open(ctx, turn)
	if err != nil {
		return nil, toBackendError(err, c.cfg.Endpoint)
	}
	return resp, nil
}

// runCloudCodeStreamBodyWithRetry reads the stream, and re-issues the
// request when the connection drops mid-turn.
//
// The retry re-sends the same body: the turn carries a request id that was
// minted once, so the second attempt is the first attempt's request, not a
// new one.
func (c *CloudCodeClient) runCloudCodeStreamBodyWithRetry(ctx context.Context, resp *http.Response, turn geminicode.Turn, out chan<- backend.StreamChunk) {
	p := streampipe.Pipeline[geminicode.Turn]{
		EndpointURL: c.cfg.Endpoint,
		Open:        c.open,
		ReadBody:    c.readBodyOnce,
	}
	p.Run(ctx, resp, turn, out)
}

// readBodyOnce reads one attempt's SSE body into the caller's channel.
func (c *CloudCodeClient) readBodyOnce(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
	if resp == nil {
		return &retry.ConnectionDroppedError{Err: retry.ErrConnectionDropped, URL: c.cfg.Endpoint}
	}

	dec := geminicode.NewDecoder(c.cc)
	var emittedDone bool
	_, scanErr := streampipe.ScanSSELines(ctx, resp.Body, 4<<20, c.cfg.Endpoint, func(line string) (bool, error) {
		for _, chunk := range dec.Feed(line) {
			if chunk.Done {
				emittedDone = true
			}
			if !emitChunk(ctx, out, chunk) {
				return true, nil
			}
			if chunk.Done {
				break
			}
		}
		return emittedDone, nil
	})
	return c.finishScan(ctx, dec, scanErr, out, emittedDone)
}

// finishScan closes the stream: it emits the decoder's closing chunks and
// classifies the error the stream ended with.
//
// A dropped connection is returned to the caller without an error chunk —
// the caller may re-issue the request, and a chunk saying "the connection
// dropped" would be a lie if the re-issue succeeds.
func (c *CloudCodeClient) finishScan(ctx context.Context, dec *geminicode.Decoder, scanErr error, out chan<- backend.StreamChunk, emittedDone bool) error {
	chunks, err := dec.Finish(scanErr)
	for _, chunk := range chunks {
		if chunk.Done {
			emittedDone = true
		}
		if !emitChunk(ctx, out, chunk) {
			return nil
		}
	}
	if err == nil {
		return nil
	}
	classified := toBackendError(err, c.cfg.Endpoint)
	if retry.IsConnectionDropped(classified) {
		return classified
	}
	if emittedDone {
		return classified
	}
	select {
	case out <- backend.StreamChunk{Error: classified, Done: true}:
	case <-ctx.Done():
		out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
	}
	return classified
}
