package streampipe

// pipeline_resume_test.go pins the 1.5.17 resume behaviour: a mid-stream
// capacity failure must not replay or discard the text the caller already has.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

const capacityErrBody = `{"error":{"status":"UNAVAILABLE","message":"MODEL_CAPACITY_EXHAUSTED"}}`

// scriptedServer serves one body per request, in order, from scripts. The last
// script is reused for any request past the end.
func scriptedServer(t *testing.T, scripts ...string) *httptest.Server {
	t.Helper()
	reqs := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := scripts[len(scripts)-1]
		if reqs < len(scripts) {
			body = scripts[reqs]
		}
		reqs++
		if strings.HasPrefix(body, "STATUS ") {
			// "STATUS <code>" answers with a bare capacity error status, which
			// is what a continuation request that finds no capacity gets.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, capacityErrBody)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sseReadBody is the dialect reader the real transports plug in, reduced to the
// two things that matter here: text frames become chunks, and a failure frame
// becomes the typed error that ends the attempt.
//
// Like cloudcode's finishScan and the gateway's SSE readers, it reports a
// non-connection-drop failure by emitting a terminal error chunk *and*
// returning the error. That is the shape the tap has to hold back, because a
// terminal chunk handed over for an attempt that then resumes would tell the
// caller the stream died when it did not.
func sseReadBody(url string) func(context.Context, *http.Response, chan<- backend.StreamChunk) error {
	return func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := strings.TrimPrefix(sc.Text(), "data: ")
			var failure error
			switch line {
			case "":
				continue
			case "!capacity":
				failure = apierror.NewHTTPError(http.StatusServiceUnavailable, []byte(capacityErrBody), url)
			case "!server":
				failure = apierror.NewHTTPError(http.StatusInternalServerError, []byte(`{"error":{"message":"internal"}}`), url)
			case "!badrequest":
				failure = apierror.NewHTTPError(http.StatusBadRequest, []byte(`{"error":{"message":"bad request"}}`), url)
			case "!drop":
				// A connection drop is reported only through the return value:
				// the stream may still be resumed, so an error chunk would be a
				// lie.
				return &retry.ConnectionDroppedError{URL: url, Err: io.ErrUnexpectedEOF}
			default:
				out <- backend.StreamChunk{Text: line}
				continue
			}
			out <- backend.StreamChunk{Error: failure, Done: true}
			return failure
		}
		if err := sc.Err(); err != nil {
			return err
		}
		out <- backend.StreamChunk{Done: true}
		return nil
	}
}

// runPipeline drives Run against a real server and returns the chunk stream.
func runPipeline(t *testing.T, ctx context.Context, maxRetries int, srv *httptest.Server) []backend.StreamChunk {
	t.Helper()
	p := Pipeline[string]{
		EndpointURL: srv.URL,
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(req))
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				return nil, retry.ClassifyTransportError(err, srv.URL)
			}
			if resp.StatusCode >= 400 {
				raw, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				return nil, &apierror.HTTPError{StatusCode: resp.StatusCode, Body: raw, URL: srv.URL, Header: resp.Header}
			}
			return resp, nil
		},
		ReadBody: sseReadBody(srv.URL),
	}

	// The first attempt is opened by the caller of Run in the real transports.
	first, err := p.Open(ctx, "req")
	if err != nil {
		t.Fatalf("initial open: %v", err)
	}
	out := make(chan backend.StreamChunk, 64)
	ctx = backend.ContextWithMaxRetries(ctx, maxRetries)
	p.Run(ctx, first, "req", out)
	close(out)

	var chunks []backend.StreamChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	return chunks
}

func joinedText(chunks []backend.StreamChunk) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.Text)
	}
	return b.String()
}

func countDone(chunks []backend.StreamChunk) int {
	n := 0
	for _, c := range chunks {
		if c.Done {
			n++
		}
	}
	return n
}

func firstError(chunks []backend.StreamChunk) error {
	for _, c := range chunks {
		if c.Error != nil {
			return c.Error
		}
	}
	return nil
}

// TestPipelineResume_CapacityMidStreamResumesWithoutReplayingHex pins the
// changelog case: the first attempt delivers "Hello " and "world" and then
// fails with a 503 / MODEL_CAPACITY_EXHAUSTED. The retry re-streams the answer
// from the first token, and the caller must see the replayed prefix exactly
// once.
func TestPipelineResume_CapacityMidStreamResumesWithoutReplayingPrefix(t *testing.T) {
	fastRetries(t)
	srv := scriptedServer(t,
		"data: Hello \ndata: world\ndata: !capacity\n",
		"data: Hello \ndata: world\ndata: , done\n",
	)

	chunks := runPipeline(t, context.Background(), 3, srv)

	got := joinedText(chunks)
	if got != "Hello world, done" {
		t.Fatalf("delivered text = %q, want %q", got, "Hello world, done")
	}
	if strings.Contains(got, "Hello Hello") {
		t.Fatalf("the replayed prefix leaked to the caller: %q", got)
	}
	if n := countDone(chunks); n != 1 {
		t.Fatalf("Done chunks = %d, want 1 (the failed attempt's ending must not surface)", n)
	}
	if err := firstError(chunks); err != nil {
		t.Fatalf("caller saw an error for a stream that resumed: %v", err)
	}
}

// TestPipelineResume_CapacityOnReopenResumesWithoutReplayingPrefix covers the
// same failure arriving on the continuation request itself (a 503 status on the
// mid-stream reopen) rather than inside the SSE body.
func TestPipelineResume_CapacityOnReopenResumesWithoutReplayingPrefix(t *testing.T) {
	fastRetries(t)
	srv := scriptedServer(t,
		"data: Hello \ndata: world\ndata: !drop\n",
		"STATUS 503",
		"data: Hello \ndata: world\ndata: , done\n",
	)

	chunks := runPipeline(t, context.Background(), 4, srv)

	got := joinedText(chunks)
	if got != "Hello world, done" {
		t.Fatalf("delivered text = %q, want %q", got, "Hello world, done")
	}
	if strings.Contains(got, "Hello Hello") {
		t.Fatalf("the replayed prefix leaked to the caller: %q", got)
	}
	if n := countDone(chunks); n != 1 {
		t.Fatalf("Done chunks = %d, want 1", n)
	}
}

// TestPipelineResume_DivergingRetryDeliversNewText documents the ambiguity the
// resume seam cannot resolve: when the retry produces different text, the new
// text wins from the first byte where it stops matching. Dropping it would hide
// an entire answer behind a stream that looks complete.
func TestPipelineResume_DivergingRetryDeliversNewText(t *testing.T) {
	fastRetries(t)
	srv := scriptedServer(t,
		"data: Hello \ndata: !capacity\n",
		"data: Hi there, done\n",
	)

	chunks := runPipeline(t, context.Background(), 3, srv)

	got := joinedText(chunks)
	if got != "Hello Hi there, done" {
		t.Fatalf("delivered text = %q, want %q", got, "Hello Hi there, done")
	}
}

// TestPipelineResume_ExhaustionSurfacesTerminalError proves a stream that never
// gets capacity fails loudly instead of ending in silence.
func TestPipelineResume_ExhaustionSurfacesTerminalError(t *testing.T) {
	fastRetries(t)
	srv := scriptedServer(t, "data: Hello \ndata: !capacity\n")

	chunks := runPipeline(t, context.Background(), 2, srv)

	if len(chunks) == 0 {
		t.Fatal("no chunks: an exhausted budget must end the stream, not leave the caller waiting")
	}
	last := chunks[len(chunks)-1]
	if !last.Done {
		t.Fatalf("last chunk = %+v, want Done=true", last)
	}
	err := firstError(chunks)
	if err == nil {
		t.Fatal("no error chunk: exhaustion must be reported")
	}
	if !isMidStreamRetryable(err) {
		t.Fatalf("error = %v, want the model-capacity failure", err)
	}
	if got := joinedText(chunks); got != "Hello " {
		t.Fatalf("delivered text = %q, want the first attempt's progress to survive", got)
	}
}

// TestPipelineResume_NonCapacityErrorsUnchanged pins the behaviour for
// failures that describe *this answer* rather than the model's availability: a
// 400 keeps failing on the first attempt, and the failure surfaces as a
// terminal error chunk.
//
// A 500 deliberately moved out of this list: the gateway reports an upstream
// failure as an SSE `error` frame, so `gateway: upstream 500: Unknown error`
// used to end a turn with no retry at all even though the open path would have
// retried the same status (AGY 1.5.0). See
// TestPipelineResume_MidStream500IsRetried.
func TestPipelineResume_NonCapacityErrorsUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		frame string
	}{
		{name: "400", frame: "!badrequest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fastRetries(t)
			srv := scriptedServer(t, "data: Hello \ndata: "+tc.frame+"\n")

			chunks := runPipeline(t, context.Background(), 3, srv)

			err := firstError(chunks)
			if err == nil {
				t.Fatal("want a terminal error")
			}
			if isMidStreamRetryable(err) {
				t.Fatalf("error = %v, want a non-retryable failure", err)
			}
			if got := joinedText(chunks); got != "Hello " {
				t.Fatalf("delivered text = %q, want %q", got, "Hello ")
			}
			if n := countDone(chunks); n != 1 {
				t.Fatalf("Done chunks = %d, want 1", n)
			}
		})
	}
}

// TestPipelineResume_MidStream500IsRetried pins the user-reported failure: a
// turn died with `gateway: upstream 500: Unknown error` and never retried.
//
// The gateway turns an upstream error frame into an *apierror.HTTPError, the
// same type the status line produces, so a mid-stream 500 must be retried on
// exactly the schedule the open path uses — and the answer that arrives on the
// retry must reach the caller once, without the failed attempt's prefix.
func TestPipelineResume_MidStream500IsRetried(t *testing.T) {
	fastRetries(t)
	srv := scriptedServer(t,
		"data: Hello \ndata: !server\n",
		"data: Hello \ndata: world\ndata: , done\n",
	)

	chunks := runPipeline(t, context.Background(), 3, srv)

	if got := joinedText(chunks); got != "Hello world, done" {
		t.Fatalf("delivered text = %q, want %q", got, "Hello world, done")
	}
	if err := firstError(chunks); err != nil {
		t.Fatalf("caller saw an error for a stream that resumed: %v", err)
	}
	if n := countDone(chunks); n != 1 {
		t.Fatalf("Done chunks = %d, want 1", n)
	}
}

// TestPipelineResume_Exhausted500ReportsTheUpstreamError proves the retry has a
// floor: when the gateway answers 500 on every attempt the turn fails with the
// upstream error itself, not with a generic "max retries" wrapper, so the
// transcript row still names the cause the user can act on.
func TestPipelineResume_Exhausted500ReportsTheUpstreamError(t *testing.T) {
	fastRetries(t)
	srv := scriptedServer(t, "data: !server\n")

	chunks := runPipeline(t, context.Background(), 2, srv)

	err := firstError(chunks)
	if err == nil {
		t.Fatal("no error chunk: an exhausted budget must end the stream loudly")
	}
	var he *apierror.HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusInternalServerError {
		t.Fatalf("error = %v, want the upstream 500 to surface", err)
	}
	if n := countDone(chunks); n != 1 {
		t.Fatalf("Done chunks = %d, want 1", n)
	}
}
