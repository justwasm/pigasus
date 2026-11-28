// streambody_test.go — the streaming contract.
//
// # Why these are separate from wirelog_test.go
//
// The transport's first implementation read the response body to EOF inside
// RoundTrip and handed the caller a fresh reader over the bytes. Every test
// in wirelog_test.go passed: the caller got the whole body, the record held
// the whole body, the sizes were right. What it broke was the only property
// no unit test was asking about — that a turn renders its frames as they
// land. A live scenario caught it: the answer appeared all at once, four
// and a half seconds after the request.
//
// So these tests are written from the caller's side of the stream: the
// recorder must not be able to tell the difference between a body it is
// recording and one it is not, until the body ends.
package wirelog

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// gatedBody is a response body that yields one chunk per release, so a test
// can prove the caller saw a chunk before the stream ended.
type gatedBody struct {
	mu      sync.Mutex
	chunks  [][]byte
	release chan struct{}
	closed  bool
}

func newGatedBody(chunks ...string) *gatedBody {
	g := &gatedBody{release: make(chan struct{}, len(chunks))}
	for _, c := range chunks {
		g.chunks = append(g.chunks, []byte(c))
	}
	return g
}

// Read blocks until the test releases the next chunk, then yields it.
func (g *gatedBody) Read(p []byte) (int, error) {
	g.mu.Lock()
	if len(g.chunks) == 0 {
		g.mu.Unlock()
		return 0, io.EOF
	}
	next := g.chunks[0]
	g.mu.Unlock()

	<-g.release

	g.mu.Lock()
	g.chunks = g.chunks[1:]
	g.mu.Unlock()
	return copy(p, next), nil
}

func (g *gatedBody) Close() error {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
	return nil
}

// TestResponseBodyStreamsThroughTheRecorder is the property the live
// scenario caught: the first chunk must reach the caller before the last
// chunk exists.
func TestResponseBodyStreamsThroughTheRecorder(t *testing.T) {
	body := newGatedBody("data: one\n\n", "data: two\n\n")
	tr := &Transport{Store: New(4), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
	})}

	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/stream", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	// RoundTrip has returned and the store is still empty: nothing has
	// been read yet, because nothing has been streamed yet.
	if n := tr.Store.Len(); n != 0 {
		t.Fatalf("the store holds %d records before the body was read; RoundTrip drained the stream", n)
	}

	// Release the first chunk and read it. A buffering recorder would
	// block here, so the release is what makes the test finish at all.
	body.release <- struct{}{}
	first := make([]byte, 64)
	n, err := resp.Body.Read(first)
	if err != nil && err != io.EOF {
		t.Fatalf("reading the first chunk: %v", err)
	}
	if got := string(first[:n]); got != "data: one\n\n" {
		t.Fatalf("the caller read %q, want the first chunk", got)
	}
	// The record is still not added: the exchange is not over.
	if n := tr.Store.Len(); n != 0 {
		t.Errorf("the store holds %d records mid-stream; the record was finalised early", n)
	}

	// Drain the rest, which ends the exchange.
	body.release <- struct{}{}
	rest, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	recs := tr.Store.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d exchanges, want 1", len(recs))
	}
	if want := "data: one\n\ndata: two\n\n"; recs[0].ResponseBody != want {
		t.Errorf("ResponseBody = %q, want the whole stream %q", recs[0].ResponseBody, want)
	}
	if len(rest) == 0 && recs[0].ResponseBody == "" {
		t.Error("neither the caller nor the record got the stream")
	}
}

// TestStreamedRecordReportsTheRealSizeNotTheCap pins the size column: the
// recorder keeps at most MaxBodyBytes+1 bytes of a stream, but the exchange
// carried more, and the table must say so.
func TestStreamedRecordReportsTheRealSizeNotTheCap(t *testing.T) {
	// A stream of 300 KB in 8 KB chunks: over the cap, and realistic for
	// a long answer.
	const chunk = 8 << 10
	const chunks = 40
	stream := bytes.Repeat([]byte("x"), chunk)
	tr := &Transport{Store: New(2), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(io.MultiReader(repeatReader(stream, chunks))),
			Header:     http.Header{},
		}, nil
	})}

	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/stream", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	recs := tr.Store.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d exchanges, want 1", len(recs))
	}
	want := int64(chunk * chunks)
	if recs[0].ResponseBytes != want {
		t.Errorf("ResponseBytes = %d, want the exchange's real size %d", recs[0].ResponseBytes, want)
	}
	if !recs[0].ResponseTruncated {
		t.Error("a 300 KB stream is not marked truncated")
	}
	if len(recs[0].ResponseBody) > MaxBodyBytes+200 {
		t.Errorf("the kept body is %d bytes; the cap did not bite", len(recs[0].ResponseBody))
	}
}

// TestEarlyCloseStillRecordsWhatWasRead pins the cancelled-turn case: a
// stream the caller abandons is still an exchange, with the bytes that did
// arrive.
func TestEarlyCloseStillRecordsWhatWasRead(t *testing.T) {
	tr := &Transport{Store: New(4), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("data: partial\n\ndata: never read\n\n")),
			Header:     http.Header{},
		}, nil
	})}

	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/stream", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	// Read a little, then close: what a cancelled turn does.
	buf := make([]byte, 13)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	recs := tr.Store.Records()
	if len(recs) != 1 {
		t.Fatalf("an abandoned stream recorded %d exchanges, want 1", len(recs))
	}
	if got := recs[0].ResponseBody; got != "data: partial" {
		t.Errorf("ResponseBody = %q, want the bytes that were read", got)
	}
}

// TestDoubleCloseRecordsOnce pins the once-guard: EOF and Close both end
// the exchange, and a body that is drained and then closed must not add two
// rows to the table.
func TestDoubleCloseRecordsOnce(t *testing.T) {
	tr := &Transport{Store: New(4), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/x", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body) // EOF finalises
	_ = resp.Body.Close()        // and so does this
	_ = resp.Body.Close()

	if n := tr.Store.Len(); n != 1 {
		t.Errorf("the store holds %d records for one exchange, want 1", n)
	}
}

// TestStreamDurationIsMeasuredToTheEnd pins what the duration column means
// for a stream: the time to the last byte, not the time to the headers.
func TestStreamDurationIsMeasuredToTheEnd(t *testing.T) {
	clock := time.Unix(0, 0)
	tr := &Transport{
		Store: New(4),
		Now:   func() time.Time { return clock },
		Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			// The headers arrive immediately.
			return &http.Response{
				StatusCode: 200,
				Body:       delayedBody{clock: &clock, after: 2 * time.Second, text: "done"},
				Header:     http.Header{},
			}, nil
		}),
	}
	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/stream", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	recs := tr.Store.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d, want 1", len(recs))
	}
	if recs[0].Duration != 2*time.Second {
		t.Errorf("Duration = %v, want the whole stream's 2s", recs[0].Duration)
	}
}

// delayedBody advances the test clock on its first Read, so a duration can
// be asserted without sleeping.
type delayedBody struct {
	clock *time.Time
	after time.Duration
	text  string
	done  bool
}

func (d delayedBody) Read(p []byte) (int, error) {
	if d.done {
		return 0, io.EOF
	}
	*d.clock = d.clock.Add(d.after)
	return copy(p, d.text), io.EOF
}

func (d delayedBody) Close() error { return nil }

// repeatReader yields the same chunk n times, for a stream over the cap
// without allocating it.
func repeatReader(chunk []byte, n int) io.Reader {
	return io.MultiReader(func() []io.Reader {
		readers := make([]io.Reader, n)
		for i := range readers {
			readers[i] = bytes.NewReader(chunk)
		}
		return readers
	}()...)
}
