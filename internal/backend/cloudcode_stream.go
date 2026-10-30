package backend

import (
	"bufio"
	"context"
	"net/http"
	"time"

	"github.com/justwasm/pigasus/geminicode"
)

// cloudcode_stream.go — the transport policy around the CloudCode dialect.
//
// The dialect itself lives in the geminicode package: it owns the request
// envelope, the SSE frames and the thought-signature round trip. What lives
// here is the policy the application applies on top of it:
//
//   - a dropped connection is retried, with the reference's exponential
//     schedule, up to the shared budget (see gemini_api_key_retry.go);
//   - a stalled stream is aborted (see NewIdleTimeoutReader);
//   - a non-2xx answer keeps the application's own HTTPError, so the
//     headless error reporter can classify it.
//
// The split is deliberate: the dialect is reusable on its own, and the
// policy is the application's to change.

// openWithRetry performs the request, retrying a dropped connection.
func (c *CloudCodeClient) openWithRetry(ctx context.Context, turn geminicode.Turn) (*http.Response, error) {
	for attempt := 0; attempt <= MaxConnectionRetries; attempt++ {
		resp, err := c.open(ctx, turn)
		if err == nil {
			return resp, nil
		}
		if !IsConnectionDropped(err) || attempt >= MaxConnectionRetries {
			return nil, err
		}
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, &ConnectionDroppedError{Err: ErrConnectionDropped, URL: c.cfg.Endpoint}
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
func (c *CloudCodeClient) runCloudCodeStreamBodyWithRetry(ctx context.Context, resp *http.Response, turn geminicode.Turn, out chan<- StreamChunk) {
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	for attempt := 0; ; attempt++ {
		err := c.readBodyOnce(ctx, resp, out)
		if err == nil || !IsConnectionDropped(err) {
			return
		}
		if attempt >= MaxConnectionRetries {
			out <- StreamChunk{Error: err, Done: true}
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return
		case <-time.After(delay):
		}
		newResp, openErr := c.open(ctx, turn)
		if openErr != nil {
			if !IsConnectionDropped(openErr) {
				out <- StreamChunk{Error: openErr, Done: true}
				return
			}
			// The re-issue failed the same way the first one did. Keep the
			// budget and try again; readBodyOnce treats the missing
			// response as another drop.
			resp = nil
			continue
		}
		resp = newResp
	}
}

// readBodyOnce reads one attempt's SSE body into the caller's channel.
func (c *CloudCodeClient) readBodyOnce(ctx context.Context, resp *http.Response, out chan<- StreamChunk) error {
	if resp == nil {
		return &ConnectionDroppedError{Err: ErrConnectionDropped, URL: c.cfg.Endpoint}
	}
	idleR := NewIdleTimeoutReader(resp.Body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	dec := geminicode.NewDecoder(c.cc)
	var emittedDone bool
	for scanner.Scan() {
		for _, chunk := range dec.Feed(scanner.Text()) {
			if chunk.Done {
				emittedDone = true
			}
			if !emitChunk(ctx, out, chunk) {
				return nil
			}
			if chunk.Done {
				break
			}
		}
		if emittedDone {
			break
		}
	}
	return c.finishScan(ctx, dec, scanner.Err(), out, emittedDone)
}

// finishScan closes the stream: it emits the decoder's closing chunks and
// classifies the error the stream ended with.
//
// A dropped connection is returned to the caller without an error chunk —
// the caller may re-issue the request, and a chunk saying "the connection
// dropped" would be a lie if the re-issue succeeds.
func (c *CloudCodeClient) finishScan(ctx context.Context, dec *geminicode.Decoder, scanErr error, out chan<- StreamChunk, emittedDone bool) error {
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
	if IsConnectionDropped(classified) {
		return classified
	}
	if emittedDone {
		return classified
	}
	select {
	case out <- StreamChunk{Error: classified, Done: true}:
	case <-ctx.Done():
		out <- StreamChunk{Error: ctx.Err(), Done: true}
	}
	return classified
}
