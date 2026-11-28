// transport.go — the http.RoundTripper that records what the CLI sends.
//
// # Where it sits
//
// Every backend client the tree builds does the same thing: `&http.Client{}`
// with no Transport, which means http.DefaultTransport. That is the single
// place all of the model traffic passes through, so wrapping the transport
// is one change instead of one per backend — and a backend added later is
// recorded without knowing this package exists.
//
// It is installed once, at dispatch (see internal/infra/backend/backendselect),
// so the same wrapping applies to a catalogue model served by CloudCode, a
// configured model served by a gateway, and the failover chain that
// alternates between them.
//
// # Why the request body is restored and the response body is not
//
// A RoundTripper must not consume the request body: the caller may reuse
// the request (the retry paths do), and net/http itself calls GetBody for a
// redirect. So the body is read into memory, recorded, and put back as a
// fresh reader before the request goes on. bytes.NewReader on the original
// path already sets GetBody, which is why restoring is enough and a
// GetBody of our own is not needed.
//
// It is restored *whole*, even past the record's cap. Restoring a capped
// read is a request shorter than its own Content-Length, and net/http
// rejects that instead of sending it — a diagnostics layer that fails the
// request it observes. The cap is applied to what the record keeps, never
// to what the transport sends.
//
// The response body is the opposite case: the caller is going to read it
// exactly once, and it is a live stream it renders frame by frame. Reading
// it here would stall every turn until its last byte, so the response is
// recorded through a tee instead — see streambody.go.
//
// # Why it never fails a request
//
// A diagnostics layer that can fail the thing it observes is worse than no
// diagnostics: a panic in the recorder would turn a working turn into a
// broken one, and a full store would become a network error. Every entry
// point here recovers, and the recorded path is the one that can be lost.
package wirelog

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Transport wraps an http.RoundTripper and records each exchange.
//
// A nil *Store disables recording and the wrapper becomes a pass-through,
// which is what a build with the diagnostics off gets for free.
type Transport struct {
	// Base is the transport to send through. Nil means
	// http.DefaultTransport.
	Base http.RoundTripper

	// Store is where records go. Nil means record nothing.
	Store *Store

	// Now is the clock, for tests. Nil means time.Now.
	Now func() time.Time
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.Store == nil {
		return base.RoundTrip(req)
	}

	started := t.now()

	// The request body, read for the record. A GET has none, and a request
	// that already streamed its body (a WebSocket upgrade) has none to
	// read either — both leave reqBody empty.
	reqBody, reqErr := captureRequest(req)
	if reqErr != nil {
		// The body could not be buffered. Sending a request whose body
		// we already consumed would corrupt it, so this is the one case
		// where recording changes behaviour: fail the request rather
		// than send a body that is now short.
		return nil, reqErr
	}

	resp, err := base.RoundTrip(req)

	rec := Record{
		Started:        started,
		Duration:       t.now().Sub(started),
		Method:         req.Method,
		URL:            RedactURL(stripUserinfo(req.URL)),
		RequestHeaders: redactHeaders(req.Header),
	}
	rec.Host, rec.Path = splitURL(req.URL)

	if err != nil {
		rec.Err = Redact(err.Error())
		// No response, so the exchange is already over: record it now
		// rather than handing back a body that will never be read.
		defer func() { _ = recover() }()
		t.Store.Capture(rec, reqBody, Captured{})
		return resp, err
	}

	rec.Status = resp.StatusCode
	rec.ResponseHeaders = redactHeaders(resp.Header)
	// RoundTrip returns when the headers do, so this is the time to first
	// byte. The stream's own end is stamped by the recording body.
	rec.TTFB = rec.Duration

	// The response body is recorded as the caller reads it. Draining it
	// here instead would buffer a whole SSE stream before the first frame
	// reached the screen — see streambody.go.
	resp.Body = newRecordingBody(resp.Body, t.Store, rec, reqBody, started, t.now)
	return resp, err
}

func (t *Transport) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// captureRequest reads the request's body for the record, leaving the
// request the transport will send untouched whenever it can.
//
// # Why it does not cap the bytes it restores
//
// The first version read the body through the cap and put the capped bytes
// back. The request then carried its original Content-Length with a shorter
// body, and net/http refuses to send that at all:
//
//	http: ContentLength=204838 with Body length 131073
//
// so a turn whose prompt exceeded MaxBodyBytes failed outright — the
// recorder broke the thing it was only supposed to observe. The cap belongs
// to the record, never to the bytes that go on the wire.
//
// # Why GetBody comes first
//
// Every backend in this tree builds its request from a bytes.Reader, so
// GetBody is set and hands back a second reader over the same bytes. Using
// it means the body the transport sends is not read, not closed and not
// replaced: the common path cannot corrupt the request even in principle.
//
// The read-and-restore path stays for a request built some other way. It
// reads whole, because a partial read there would be the same
// Content-Length mismatch by another route.
func captureRequest(req *http.Request) (Captured, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return Captured{}, nil
	}
	if req.GetBody != nil {
		if rc, err := req.GetBody(); err == nil {
			body, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr == nil {
				return CaptureBody(body), nil
			}
			// A broken replay reader is not a reason to fail the turn;
			// fall through and read the body itself.
		}
	}
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return Captured{}, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return CaptureBody(body), nil
}

// splitURL returns the host and path of u, with the path kept whole.
func splitURL(u *url.URL) (host, path string) {
	if u == nil {
		return "", ""
	}
	return u.Host, u.Path
}

// stripUserinfo removes a user:password@ from a URL before it is recorded.
// A gateway URL is allowed to carry basic-auth credentials in the authority
// (the reference's own --url flag accepts one), and those are a secret in
// exactly the way a bearer is.
func stripUserinfo(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.User != nil {
		cp := *u
		cp.User = nil
		return cp.String()
	}
	return u.String()
}
