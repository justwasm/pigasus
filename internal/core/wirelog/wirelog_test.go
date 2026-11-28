package wirelog

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestTransportRecordsTheExchange is the basic contract: one round trip in,
// one record out, with the facts the /logs view shows.
func TestTransportRecordsTheExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"model":"x"}` {
			t.Errorf("the server saw body %q; the recorder consumed it", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	st := New(10)
	tr := &Transport{Store: st}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/models:generate?alt=sse", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != `{"ok":true}` {
		t.Errorf("the caller's response body is %q; the recorder consumed it", got)
	}

	recs := st.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d exchanges, want 1", len(recs))
	}
	r := recs[0]
	if r.Seq != 1 {
		t.Errorf("Seq = %d, want 1", r.Seq)
	}
	if r.Method != http.MethodPost {
		t.Errorf("Method = %q", r.Method)
	}
	if r.Path != "/v1/models:generate" {
		t.Errorf("Path = %q, want the path without the query", r.Path)
	}
	if r.Status != 200 {
		t.Errorf("Status = %d, want 200", r.Status)
	}
	if !r.OK() {
		t.Error("OK() is false for a 200")
	}
	if r.RequestBody != `{"model":"x"}` {
		t.Errorf("RequestBody = %q", r.RequestBody)
	}
	if r.ResponseBody != `{"ok":true}` {
		t.Errorf("ResponseBody = %q", r.ResponseBody)
	}
	if r.RequestBytes != 13 || r.ResponseBytes != 11 {
		t.Errorf("sizes = %d/%d, want 13/11", r.RequestBytes, r.ResponseBytes)
	}
	if r.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", r.Duration)
	}
	if r.Host == "" {
		t.Error("Host is empty")
	}
}

// TestTransportDoesNotConsumeTheRequestBody is the property the retry paths
// depend on: after the recorder has read the body, the request must still
// carry it.
func TestTransportDoesNotConsumeTheRequestBody(t *testing.T) {
	tr := &Transport{Store: New(4), Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if string(body) != "payload" {
			t.Errorf("base saw body %q, want %q", body, "payload")
		}
		// A retry does this: the same request, sent again.
		req.Body = io.NopCloser(bytes.NewReader(body))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
	})}

	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/x", strings.NewReader("payload"))
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	// The original request must be reusable afterwards, which is what
	// GetBody is for.
	if req.GetBody == nil {
		t.Fatal("the request has no GetBody after the recorder read it")
	}
	again, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	b, _ := io.ReadAll(again)
	if string(b) != "payload" {
		t.Errorf("GetBody returned %q, want %q", b, "payload")
	}
}

// TestTransportRecordsATransportError pins the other shape of record: no
// response at all.
func TestTransportRecordsATransportError(t *testing.T) {
	tr := &Transport{Store: New(4), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})}
	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/x", nil)
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("the transport error was swallowed")
	}
	recs := tr.Store.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d, want 1", len(recs))
	}
	if recs[0].Status != 0 {
		t.Errorf("Status = %d, want 0 for a request with no response", recs[0].Status)
	}
	if recs[0].Err == "" {
		t.Error("Err is empty for a failed exchange")
	}
	if recs[0].OK() {
		t.Error("OK() is true for a failed exchange")
	}
}

// TestRedactionCoversEveryShapeTheCLIEmits is the credential test.
//
// A record is a diagnostics surface with a copy button: a token that
// reaches it is a token the user may paste into an issue. Each case below
// is a shape the tree actually produces.
func TestRedactionCoversEveryShapeTheCLIEmits(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"Authorization: Bearer ya29.abcdefghijklmnop", "ya29.abcdefghijklmnop"},
		{"x-api-key: AIzaSyABCDEFGHIJKLMNOPQRSTUV", "AIzaSyABCDEFGHIJKLMNOPQRSTUV"},
		{"api_key=sk-abcdefghijklmnopqrstuvwxyz", "sk-abcdefghijklmnopqrstuvwxyz"},
		{`{"token":"ya29.abcdefghijklmnop"}`, "ya29.abcdefghijklmnop"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if strings.Contains(got, c.mustNotContain) {
			t.Errorf("Redact(%q) = %q, still carries the credential", c.in, got)
		}
	}
}

// TestSecretHeadersAreDroppedWhole pins that a header whose value is the
// secret is replaced rather than sanitised: a partial redaction of a
// credential is still a credential.
func TestSecretHeadersAreDroppedWhole(t *testing.T) {
	h := http.Header{
		"Authorization": []string{"Bearer abcdefghijklmnop"},
		"Cookie":        []string{"session=abcdefghijklmnop"},
		"Content-Type":  []string{"application/json"},
	}
	got := redactHeaders(h)
	for _, name := range []string{"Authorization", "Cookie"} {
		if v := got.Get(name); v != "<redacted>" {
			t.Errorf("%s = %q, want <redacted>", name, v)
		}
	}
	if got.Get("Content-Type") != "application/json" {
		t.Error("a non-secret header was altered")
	}
	// The caller's map must not be mutated: the request goes on to the
	// base transport and needs its real headers.
	if h.Get("Authorization") == "<redacted>" {
		t.Error("redactHeaders mutated the request's own header map")
	}
}

// TestUserinfoInTheURLIsStripped pins the gateway case: a URL is allowed to
// carry basic-auth credentials in its authority.
func TestUserinfoInTheURLIsStripped(t *testing.T) {
	u, err := url.Parse("https://user:secret@gateway.example/v1/chat")
	if err != nil {
		t.Fatal(err)
	}
	got := stripUserinfo(u)
	if strings.Contains(got, "secret") || strings.Contains(got, "user:") {
		t.Errorf("stripUserinfo left credentials in %q", got)
	}
	if !strings.Contains(got, "gateway.example") {
		t.Errorf("stripUserinfo lost the host: %q", got)
	}
}

// TestBodyIsCappedAndSaysSo pins the truncation contract: a body over the
// cap is cut, the record says it was cut, and the size column still reports
// the real size.
func TestBodyIsCappedAndSaysSo(t *testing.T) {
	big := bytes.Repeat([]byte("a"), MaxBodyBytes+500)
	st := New(2)
	rec := st.Capture(Record{Method: "POST", Path: "/x"},
		CaptureBody(big), CaptureBody([]byte("small")))

	if !rec.RequestTruncated {
		t.Error("RequestTruncated is false for a body over the cap")
	}
	if rec.ResponseTruncated {
		t.Error("ResponseTruncated is true for a body under the cap")
	}
	if rec.RequestBytes != int64(len(big)) {
		t.Errorf("RequestBytes = %d, want the real size %d", rec.RequestBytes, len(big))
	}
	if !strings.Contains(rec.RequestBody, "truncated by wirelog") {
		t.Error("a truncated body does not say it was truncated")
	}
	if len(rec.RequestBody) > MaxBodyBytes+200 {
		t.Errorf("the capped body is %d bytes; the cap did not bite", len(rec.RequestBody))
	}
}

// TestTheTruncationNoteNamesTheRealSize is the bug a reader saw and could
// not believe: "the exchange carried 131073 bytes" for a two-megabyte
// request.
//
// 131073 is MaxBodyBytes+1 — the kept length, not the carried one. The note
// is the one number in the record a reader trusts most, and it was reporting
// how much the recorder held rather than how much was missing.
func TestTheTruncationNoteNamesTheRealSize(t *testing.T) {
	big := bytes.Repeat([]byte("a"), 2<<20)
	rec := New(2).Capture(Record{}, CaptureBody(big), Captured{})

	want := fmt.Sprintf("the exchange carried %d", len(big))
	if !strings.Contains(rec.RequestBody, want) {
		idx := strings.Index(rec.RequestBody, "truncated by wirelog")
		t.Errorf("the note reads %q, want it to say %q",
			strings.TrimSpace(rec.RequestBody[idx:]), want)
	}
	if strings.Contains(rec.RequestBody, "carried 131073") {
		t.Error("the note names the kept length, not the carried one")
	}
	if rec.RequestBytes != int64(len(big)) {
		t.Errorf("RequestBytes = %d, want %d", rec.RequestBytes, len(big))
	}
}

// TestExactlyAtTheCapIsNotTruncated is why the cap keeps one byte past
// itself: a reader that stops at the cap cannot tell a body of exactly
// MaxBodyBytes from one that was cut.
func TestExactlyAtTheCapIsNotTruncated(t *testing.T) {
	exact := bytes.Repeat([]byte("a"), MaxBodyBytes)
	rec := New(1).Capture(Record{}, CaptureBody(exact), Captured{})
	if rec.RequestTruncated {
		t.Error("a body of exactly MaxBodyBytes was reported as truncated")
	}
	if rec.RequestBytes != MaxBodyBytes {
		t.Errorf("RequestBytes = %d, want %d", rec.RequestBytes, MaxBodyBytes)
	}

	over := bytes.Repeat([]byte("a"), MaxBodyBytes+1)
	if !New(1).Capture(Record{}, CaptureBody(over), Captured{}).RequestTruncated {
		t.Error("a body one byte over the cap was not reported as truncated")
	}
}

// TestTheKeptBytesDoNotRetainTheWholeBody pins the ring's memory ceiling.
//
// CaptureBody copies the kept prefix rather than slicing it. A slice of a
// 10 MB body keeps the whole array alive for as long as the record does,
// and the package promises a ring of 500 records cannot exceed roughly
// 64 MiB.
func TestTheKeptBytesDoNotRetainTheWholeBody(t *testing.T) {
	big := bytes.Repeat([]byte("a"), 4<<20)
	c := CaptureBody(big)
	retained := len(c.Head) + len(c.Tail)
	if retained > MaxBodyBytes {
		t.Fatalf("retained %d bytes, want at most %d", retained, MaxBodyBytes)
	}
	if c.Total != int64(len(big)) {
		t.Errorf("Total = %d, want %d", c.Total, len(big))
	}
	// Mutating the source must not change the record: a shared array would.
	big[0] = 'Z'
	if c.Head[0] == 'Z' {
		t.Error("the kept bytes alias the source body; the whole body stays alive")
	}
}

// TestRingEvictsTheOldestAndKeepsSeqStable pins the two properties the view
// depends on: the ring is bounded, and a row's identity does not change
// because its neighbours aged out.
func TestRingEvictsTheOldestAndKeepsSeqStable(t *testing.T) {
	st := New(3)
	for i := 0; i < 5; i++ {
		st.Add(Record{Path: "/" + string(rune('a'+i))})
	}
	recs := st.Records()
	if len(recs) != 3 {
		t.Fatalf("the ring holds %d, want its capacity 3", len(recs))
	}
	if recs[0].Seq != 3 || recs[2].Seq != 5 {
		t.Errorf("Seq range = %d..%d, want 3..5", recs[0].Seq, recs[2].Seq)
	}
	if _, ok := st.Get(1); ok {
		t.Error("an evicted record is still readable")
	}
	if r, ok := st.Get(4); !ok || r.Seq != 4 {
		t.Errorf("Get(4) = %+v, %v", r, ok)
	}
}

// TestNilStoreIsANoOp is the contract that lets a caller with no store run
// the transport unwrapped.
func TestNilStoreIsANoOp(t *testing.T) {
	var st *Store
	if st.Len() != 0 || st.Records() != nil {
		t.Error("a nil store is not empty")
	}
	if _, ok := st.Get(1); ok {
		t.Error("a nil store answered a Get")
	}
	got := st.Add(Record{Path: "/x"})
	if got.Seq != 0 {
		t.Errorf("a nil store assigned Seq %d", got.Seq)
	}
	st.Reset() // must not panic
}

// TestTransportWithNoStoreIsAPassThrough pins that the wrapper costs nothing
// when diagnostics are off.
func TestTransportWithNoStoreIsAPassThrough(t *testing.T) {
	called := false
	tr := &Transport{Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 204, Body: http.NoBody, Header: http.Header{}}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/x", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("the base transport was not called")
	}
}

// TestRecordingNeverFailsTheRequest is the safety property: a panic inside
// the recorder must not cost the caller its response.
func TestRecordingNeverFailsTheRequest(t *testing.T) {
	// A store whose ring is fine but whose clock panics on the second
	// call, which is inside the recording path.
	calls := 0
	tr := &Transport{
		Store: New(2),
		Now: func() time.Time {
			calls++
			if calls > 2 {
				panic("clock exploded")
			}
			return time.Now()
		},
		Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
		}),
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/x", nil)
	resp, err := tr.RoundTrip(req)
	if err == nil {
		if resp == nil {
			t.Fatal("no response and no error")
		}
		resp.Body.Close()
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestALargeRequestBodyIsSentWhole is the regression for a bug that would
// have failed every long conversation.
//
// readAndRestore used to read the request body through the cap and put the
// capped bytes back. The request then carried its original Content-Length
// with a shorter body, and net/http refuses to send that at all:
//
//	http: ContentLength=204838 with Body length 131073
//
// so a turn whose prompt exceeded 128 KiB failed outright — the recorder
// broke the thing it was only supposed to observe. The cap belongs to the
// record, never to the bytes that go on the wire.
func TestALargeRequestBodyIsSentWhole(t *testing.T) {
	const size = 2 << 20 // well past MaxBodyBytes

	var serverSaw int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("the server could not read the body: %v", err)
		}
		serverSaw = len(body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := New(4)
	client := &http.Client{Transport: &Transport{Store: store}}

	payload := bytes.Repeat([]byte("a"), size)
	resp, err := client.Post(srv.URL, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("the recorded request failed: %v", err)
	}
	resp.Body.Close()

	if serverSaw != size {
		t.Errorf("the server received %d bytes, want the whole %d", serverSaw, size)
	}

	rec := store.Records()[0]
	if rec.RequestBytes != size {
		t.Errorf("RequestBytes = %d, want the carried size %d", rec.RequestBytes, size)
	}
	if !rec.RequestTruncated {
		t.Error("the record kept a 2 MiB body without saying it was cut")
	}
	if len(rec.RequestBody) > MaxBodyBytes+200 {
		t.Errorf("the kept body is %d bytes; the cap did not bite", len(rec.RequestBody))
	}
	if !strings.Contains(rec.RequestBody, fmt.Sprintf("the exchange carried %d", size)) {
		t.Error("the note does not name the size the exchange carried")
	}
}

// TestALargeResponseIsSentWholeAndReported — the response half of the same
// contract: the caller gets every byte, and the record reports the real
// size while keeping a capped copy.
func TestALargeResponseIsSentWholeAndReported(t *testing.T) {
	const size = 2 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("b"), size))
	}))
	defer srv.Close()

	store := New(4)
	client := &http.Client{Transport: &Transport{Store: store}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != size {
		t.Errorf("the caller received %d bytes, want %d", len(body), size)
	}

	rec := store.Records()[0]
	if rec.ResponseBytes != size {
		t.Errorf("ResponseBytes = %d, want %d", rec.ResponseBytes, size)
	}
	if !rec.ResponseTruncated {
		t.Error("a 2 MiB response was recorded without saying it was cut")
	}
}

// TestTheRecorderDoesNotTouchABodyItCanReplay pins the strongest form of
// the request-body contract: when the request carries GetBody, the recorder
// reads the replay copy and leaves the real body alone.
//
// That makes the failure above impossible in principle rather than by
// careful bookkeeping — there is no restored reader to get the length
// wrong, because the original was never read.
func TestTheRecorderDoesNotTouchABodyItCanReplay(t *testing.T) {
	payload := []byte(`{"model":"x"}`)
	req, err := http.NewRequest("POST", "http://example.test/v1", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	original := req.Body

	var serverSaw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverSaw, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	req.URL, _ = url.Parse(srv.URL)

	store := New(2)
	client := &http.Client{Transport: &Transport{Store: store}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if req.Body != original {
		t.Error("the recorder replaced req.Body; the replay path was not used")
	}
	if string(serverSaw) != string(payload) {
		t.Errorf("the server saw %q, want %q", serverSaw, payload)
	}
	rec := store.Records()[0]
	if rec.RequestBody != string(payload) {
		t.Errorf("the record holds %q, want %q", rec.RequestBody, payload)
	}
	if rec.RequestBytes != int64(len(payload)) {
		t.Errorf("RequestBytes = %d, want %d", rec.RequestBytes, len(payload))
	}
}

func TestTailBuffer(t *testing.T) {
	tb := newTailBuffer(10)
	tb.Write([]byte("hello"))
	if string(tb.Bytes()) != "hello" {
		t.Errorf("got %q, want 'hello'", string(tb.Bytes()))
	}
	tb.Write([]byte("world"))
	if string(tb.Bytes()) != "helloworld" {
		t.Errorf("got %q, want 'helloworld'", string(tb.Bytes()))
	}
	// Overwrite boundary
	tb.Write([]byte("123"))
	if string(tb.Bytes()) != "loworld123" {
		t.Errorf("got %q, want 'loworld123'", string(tb.Bytes()))
	}
	// Write larger than buffer size
	tb.Write([]byte("abcdefghijklmnop"))
	if string(tb.Bytes()) != "ghijklmnop" {
		t.Errorf("got %q, want 'ghijklmnop'", string(tb.Bytes()))
	}
}

// TestHeadAndTailPreservesStreamEnd pins requirement 1 & 2: when a stream
// exceeds MaxBodyBytes, the recorder preserves both the beginning (head)
// and the trailing metadata (tail), including finish_reason and usageMetadata.
func TestHeadAndTailPreservesStreamEnd(t *testing.T) {
	const headPrefix = `data: {"candidates":[{"content":{"parts":[{"text":"START_OF_STREAM..."}]}}]}\n\n`
	const trailingMeta = `data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":85000,"candidatesTokenCount":1250,"totalTokenCount":86250}}\n\n`

	// Fill middle so total is ~500 KB, well over MaxBodyBytes (128 KB).
	middleSize := 500 << 10
	middle := bytes.Repeat([]byte("0123456789abcdef"), middleSize/16)

	fullBody := append([]byte(headPrefix), middle...)
	fullBody = append(fullBody, []byte(trailingMeta)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Write in small chunks to exercise streaming tee
		chunkSize := 8192
		for i := 0; i < len(fullBody); i += chunkSize {
			end := i + chunkSize
			if end > len(fullBody) {
				end = len(fullBody)
			}
			_, _ = w.Write(fullBody[i:end])
		}
	}))
	defer srv.Close()

	store := New(4)
	client := &http.Client{Transport: &Transport{Store: store}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(fullBody) {
		t.Fatalf("client received %d bytes, want %d", len(got), len(fullBody))
	}

	recs := store.Records()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	rec := recs[0]

	if !rec.ResponseTruncated {
		t.Error("expected ResponseTruncated to be true for 500 KB stream")
	}
	if !strings.Contains(rec.ResponseBody, "START_OF_STREAM") {
		t.Error("ResponseBody missing head prefix")
	}
	if !strings.Contains(rec.ResponseBody, "finishReason") {
		t.Error("ResponseBody missing tail finishReason")
	}
	if !strings.Contains(rec.ResponseBody, "usageMetadata") {
		t.Error("ResponseBody missing tail usageMetadata")
	}

	// Verify token usage was extracted from the tail!
	if rec.PromptTokens != 85000 {
		t.Errorf("PromptTokens = %d, want 85000", rec.PromptTokens)
	}
	if rec.CompletionTokens != 1250 {
		t.Errorf("CompletionTokens = %d, want 1250", rec.CompletionTokens)
	}
	if rec.TotalTokens != 86250 {
		t.Errorf("TotalTokens = %d, want 86250", rec.TotalTokens)
	}
}

func TestExtractUsage(t *testing.T) {
	// Gemini format
	gemini := `data: {"usageMetadata":{"promptTokenCount":1234,"candidatesTokenCount":56,"totalTokenCount":1290}}`
	p, c, tot, ok := ExtractUsage(gemini)
	if !ok || p != 1234 || c != 56 || tot != 1290 {
		t.Errorf("Gemini usage = (%d, %d, %d, %v), want (1234, 56, 1290, true)", p, c, tot, ok)
	}

	// OpenAI format
	openai := `{"id":"chatcmpl-123","usage":{"prompt_tokens":4321,"completion_tokens":89,"total_tokens":4410}}`
	p, c, tot, ok = ExtractUsage(openai)
	if !ok || p != 4321 || c != 89 || tot != 4410 {
		t.Errorf("OpenAI usage = (%d, %d, %d, %v), want (4321, 89, 4410, true)", p, c, tot, ok)
	}

	// Anthropic format
	anthropic := `{"type":"message_delta","usage":{"output_tokens":75}}`
	p, c, tot, ok = ExtractUsage(anthropic)
	if !ok || c != 75 {
		t.Errorf("Anthropic usage = (%d, %d, %d, %v), want (0, 75, 75, true)", p, c, tot, ok)
	}
}
