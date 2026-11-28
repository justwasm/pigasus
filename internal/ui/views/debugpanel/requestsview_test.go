package debugpanel

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/wirelog"
)

// reqFixture is a small session: one success, one server error, one
// transport failure.
func reqFixture() RequestState {
	base := time.Date(2026, 11, 28, 2, 30, 0, 0, time.UTC)
	return RequestState{
		Width: 140,
		Records: []wirelog.Record{
			{
				Seq: 1, Started: base, Duration: 850 * time.Millisecond,
				Method: "POST", Host: "cloudcode-pa.googleapis.com", Path: "/v1internal:streamGenerateContent",
				Status: 200, RequestBytes: 14_400, ResponseBytes: 2_100,
				RequestBody:    `{"model":"gemini"}`,
				ResponseBody:   `data: {"ok":true}`,
				RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
			},
			{
				Seq: 2, Started: base.Add(time.Second), Duration: 3 * time.Second, TTFB: 3 * time.Second,
				Method: "POST", Host: "cloudcode-pa.googleapis.com", Path: "/v1internal:streamGenerateContent",
				Status: 503, RequestBytes: 14_400, ResponseBytes: 90,
				ResponseBody: `{"error":"overloaded"}`,
			},
			{
				Seq: 3, Started: base.Add(2 * time.Second), Duration: 120 * time.Millisecond,
				Method: "POST", Host: "gateway.example", Path: "/v1/chat/completions",
				Status: 0, Err: "dial tcp: connection refused",
				RequestBytes: 900,
			},
		},
	}
}

// TestRequestRowsAreNewestFirst pins the ordering: the exchange a reader
// wants is the one that just happened.
func TestRequestRowsAreNewestFirst(t *testing.T) {
	rows := RequestRows(reqFixture())
	if len(rows) != 3 {
		t.Fatalf("rendered %d rows, want 3", len(rows))
	}
	if !strings.Contains(ansi.Strip(rows[0].Text), "#3") {
		t.Errorf("the first row is not the newest exchange: %q", ansi.Strip(rows[0].Text))
	}
	if !strings.Contains(ansi.Strip(rows[2].Text), "#1") {
		t.Errorf("the last row is not the oldest exchange: %q", ansi.Strip(rows[2].Text))
	}
}

// TestRequestRowsCarryTheAnalysisColumns is the feature the user asked for:
// method, status, endpoint, duration and size on one line.
func TestRequestRowsCarryTheAnalysisColumns(t *testing.T) {
	rows := RequestRows(reqFixture())
	// Row 0 is #3, the transport failure.
	third := ansi.Strip(rows[0].Text)
	for _, want := range []string{"#3", "POST", "ERR", "/v1/chat/completions", "120 ms"} {
		if !strings.Contains(third, want) {
			t.Errorf("the failed row is missing %q: %q", want, third)
		}
	}
	// Row 2 is #1, the success.
	first := ansi.Strip(rows[2].Text)
	for _, want := range []string{"#1", "200", "850 ms", "14.1 KB"} {
		if !strings.Contains(first, want) {
			t.Errorf("the successful row is missing %q: %q", want, first)
		}
	}
}

// TestRequestRowStatusColoursAreSemantic pins that the status cell reads
// without the number: a 2xx is the success colour, a 5xx the failure one.
func TestRequestRowStatusColoursAreSemantic(t *testing.T) {
	rows := RequestRows(reqFixture())
	// rows[1] is #2, the 503.
	if !strings.Contains(rows[1].Text, logErrorOpen) {
		t.Errorf("a 503 is not painted as a failure: %q", rows[1].Text)
	}
	// rows[2] is #1, the 200.
	if !strings.Contains(rows[2].Text, logSuccessOpen) {
		t.Errorf("a 200 is not painted as a success: %q", rows[2].Text)
	}
}

// TestRequestHeaderSummarisesTheSession is the "analysis" half: how many
// exchanges, how many failed, how many were slow, and which hosts.
func TestRequestHeaderSummarisesTheSession(t *testing.T) {
	head := strings.Join(RequestHeaderLines(reqFixture()), "\n")
	plain := ansi.Strip(head)
	// Two of the three failed: the 503 and the transport error. Counting
	// only one would be the bug this asserts against.
	for _, want := range []string{"3 exchange(s)", "2 failed", "1 slow", "cloudcode-pa.googleapis.com", "gateway.example"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the header is missing %q:\n%s", want, plain)
		}
	}
}

// TestSlowMeansSlowToStart pins the header's "slow" count: a streamed answer
// that took a while to finish is not the complaint that a silent endpoint
// is, and only the second is counted.
func TestSlowMeansSlowToStart(t *testing.T) {
	st := RequestState{Records: []wirelog.Record{
		// Fast to start, long to finish: a long answer, not a slow one.
		{Seq: 1, Status: 200, TTFB: 200 * time.Millisecond, Duration: 9 * time.Second},
		// Silent for three seconds before the first frame: this is slow.
		{Seq: 2, Status: 200, TTFB: 3 * time.Second, Duration: 3 * time.Second},
	}}
	plain := ansi.Strip(strings.Join(RequestHeaderLines(st), "\n"))
	if !strings.Contains(plain, "1 slow") {
		t.Errorf("the header counts %q; a 9 s stream that started in 200 ms is not slow", plain)
	}
}

// TestExchangeBlockNamesTheFirstByteOnlyForAStream pins the extra row: it is
// the reading that explains a hung turn, and it is only printed when it says
// something the total does not.
func TestExchangeBlockNamesTheFirstByteOnlyForAStream(t *testing.T) {
	streamed := requestFacts(wirelog.Record{
		Host: "h", Status: 200, Duration: 5 * time.Second, TTFB: 300 * time.Millisecond,
	})
	if !strings.Contains(streamed, "first byte") || !strings.Contains(streamed, "300 ms") {
		t.Errorf("a streamed exchange does not name its first byte:\n%s", streamed)
	}

	// A non-streamed exchange has TTFB equal to its duration (or zero):
	// printing the same number twice under two names would be noise.
	plainRec := requestFacts(wirelog.Record{Host: "h", Status: 200, Duration: 300 * time.Millisecond, TTFB: 300 * time.Millisecond})
	if strings.Contains(plainRec, "first byte") {
		t.Errorf("a non-streamed exchange repeats its duration as a first byte:\n%s", plainRec)
	}
}

// TestRequestHeaderOnAnEmptySessionSaysHowToFillIt pins the empty state: a
// blank table with no explanation reads as a broken feature.
func TestRequestHeaderOnAnEmptySessionSaysHowToFillIt(t *testing.T) {
	plain := ansi.Strip(strings.Join(RequestHeaderLines(RequestState{}), "\n"))
	if !strings.Contains(plain, "send a message") {
		t.Errorf("the empty header does not say what to do:\n%s", plain)
	}
}

// TestRequestsBodyCarriesBothHalves is the reader's contract: the whole
// request and the whole response, labelled, under the exchange's own
// numbers.
func TestRequestsBodyCarriesBothHalves(t *testing.T) {
	rec := reqFixture().Records[0]
	body := RequestsBody(rec)
	if body.Title != "#1 POST /v1internal:streamGenerateContent" {
		t.Errorf("title = %q", body.Title)
	}
	// exchange, request headers, request body, response headers,
	// response body — headers separated from bodies so the format key can
	// reformat the body.
	if len(body.Parts) != 5 {
		t.Fatalf("body has %d parts, want exchange + headers + bodies", len(body.Parts))
	}
	labels := make([]string, len(body.Parts))
	for i, p := range body.Parts {
		labels[i] = p.Label
	}
	wantLabels := []string{"exchange", "request headers", "request body", "response headers", "response body · sse"}
	for i, want := range wantLabels {
		if labels[i] != want {
			t.Fatalf("part %d is %q, want %q (all: %v)", i, labels[i], want, labels)
		}
	}
	// The exchange block leads: the identity and timing are the first
	// thing in the reader, not something to scroll a request body to find.
	if body.Parts[0].Label != "exchange" {
		t.Errorf("the body does not lead with the exchange block: %+v", body.Parts[0])
	}
	for _, want := range []string{"cloudcode-pa.googleapis.com", "850 ms", "14.1 KB"} {
		if !strings.Contains(body.Parts[0].Text, want) {
			t.Errorf("the exchange block is missing %q:\n%s", want, body.Parts[0].Text)
		}
	}
	// The request line and its headers are the headers part.
	if reqHead := body.Parts[1].Text; !strings.Contains(reqHead, "POST ") || !strings.Contains(reqHead, "Content-Type: application/json") {
		t.Errorf("the request headers part is wrong: %q", reqHead)
	}
	// The body is its own part, which is what makes the format key work.
	if body.Parts[2].Text != `{"model":"gemini"}` {
		t.Errorf("the request body part is %q", body.Parts[2].Text)
	}
	// The status line leads the response headers, so a reader sees the
	// outcome before the body.
	if !strings.Contains(body.Parts[3].Text, "200 OK") {
		t.Errorf("the response headers part does not lead with the status: %q", body.Parts[3].Text)
	}
	if body.Parts[4].Text != `data: {"ok":true}` {
		t.Errorf("the response body part is %q", body.Parts[4].Text)
	}
}

// TestTheRequestBodyPartParsesAsJSON pins why headers and bodies are separate
// parts: a combined block parses as nothing, and the reader's `t` key would
// be offered and then do nothing on the one body worth reformatting.
func TestTheRequestBodyPartParsesAsJSON(t *testing.T) {
	body := RequestsBody(reqFixture().Records[0])
	var found bool
	for _, p := range body.Parts {
		if !strings.Contains(p.Label, "request body") {
			continue
		}
		found = true
		if !structured(p.Text) {
			t.Errorf("the request body part does not parse as JSON: %q", p.Text)
		}
	}
	if !found {
		t.Fatal("the body has no request body part")
	}
}

// TestACappedBodySaysSoInItsLabel pins the honesty about the cap: a body the
// recorder cut must not read as the whole request.
func TestACappedBodySaysSoInItsLabel(t *testing.T) {
	rec := reqFixture().Records[0]
	rec.RequestTruncated = true
	body := RequestsBody(rec)
	for _, p := range body.Parts {
		if strings.HasPrefix(p.Label, "request body") {
			if !strings.Contains(p.Label, "capped") {
				t.Errorf("a capped body's label is %q", p.Label)
			}
			return
		}
	}
	t.Fatal("the body has no request body part")
}

// TestAnSSEBodyIsLabelledSSE pins the streamed case: a stream of data: frames
// parses as no single JSON document, so the label has to say what it is.
func TestAnSSEBodyIsLabelledSSE(t *testing.T) {
	rec := reqFixture().Records[0]
	rec.ResponseBody = "data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"
	body := RequestsBody(rec)
	for _, p := range body.Parts {
		if strings.HasPrefix(p.Label, "response body") {
			if !strings.Contains(p.Label, "sse") {
				t.Errorf("an SSE body's label is %q", p.Label)
			}
			return
		}
	}
	t.Fatal("the body has no response body part")
}

// TestRequestsBodyShowsATransportError pins that a request with no response
// still reads as an exchange, with the failure named.
func TestRequestsBodyShowsATransportError(t *testing.T) {
	rec := reqFixture().Records[2]
	body := RequestsBody(rec)
	joined := body.Parts[2].Text
	if !strings.Contains(joined, "connection refused") {
		t.Errorf("the transport error is missing from the response part: %q", joined)
	}
	// The exchange block names it too, so a reader who stops at the first
	// block still learns the exchange failed.
	if !strings.Contains(body.Parts[0].Text, "connection refused") {
		t.Errorf("the exchange block does not name the failure: %q", body.Parts[0].Text)
	}
}

// TestRequestRowIDRoundTrips pins the reader's key: the row ID is the only
// thing the reader is handed, so the Seq has to survive the round trip.
func TestRequestRowIDRoundTrips(t *testing.T) {
	for _, seq := range []int64{1, 42, 999} {
		got, ok := requestRowSeq(rowIDFor(seq))
		if !ok || got != seq {
			t.Errorf("requestRowSeq(rowIDFor(%d)) = %d, %v", seq, got, ok)
		}
	}
	if _, ok := requestRowSeq("wire/system"); ok {
		t.Error("a Wire row ID parsed as a request")
	}
}

// rowIDFor mirrors requestRow's ID format, so the test fails if the format
// changes without the parser.
func rowIDFor(seq int64) string {
	rows := RequestRows(RequestState{Records: []wirelog.Record{{Seq: seq}}})
	if len(rows) != 1 {
		panic("RequestRows returned no row for one record")
	}
	return rows[0].ID
}

// TestRequestRowOpensItsOwnBodyWithoutTheHost pins that the panel holds the
// body: a Requests row must not need a host seam to be readable.
func TestRequestRowOpensItsOwnBodyWithoutTheHost(t *testing.T) {
	// The panel holds the record, so no host seam is needed at all.
	p := New(Seams{Requests: func() RequestState { return reqFixture() }})
	p.SetView(ViewRequests)
	p.Lines() // one frame, so reqState is the ring's
	p.read("req/1", nil)

	if p.reader == nil {
		t.Fatal("Enter on a Requests row opened no body")
	}
	// The request body is the third block: the exchange's own numbers
	// lead, then the request line and its headers.
	joined := p.reader.body.Parts[2].Text
	if !strings.Contains(joined, `{"model":"gemini"}`) {
		t.Errorf("the reader did not open the record's body: %q", joined)
	}
}

// TestRequestRowThatAgedOutSaysSo pins the honest failure: a record evicted
// between the draw and the read must not open an empty body.
func TestRequestRowThatAgedOutSaysSo(t *testing.T) {
	p := New(Seams{Requests: func() RequestState { return reqFixture() }})
	p.SetView(ViewRequests)
	p.Lines()
	p.read("req/99", nil)
	if p.reader == nil {
		t.Fatal("no reader opened")
	}
	if !strings.Contains(p.reader.body.Parts[0].Text, "no longer in the buffer") {
		t.Errorf("an evicted record opened %q", p.reader.body.Parts[0].Text)
	}
}

// TestRequestsViewIsInTheTabCycle pins that the new view is reachable the
// same way as the others.
func TestRequestsViewIsInTheTabCycle(t *testing.T) {
	// Index the views rather than following Next(): Next wraps, so a
	// loop that advances by it never terminates.
	names := make([]string, 0, ViewCount())
	for i := 0; i < ViewCount(); i++ {
		names = append(names, View(i).Name())
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "Requests") {
		t.Errorf("the tab cycle is %q, with no Requests view", joined)
	}
	if got, ok := ViewByName("requests"); !ok || got != ViewRequests {
		t.Errorf(`ViewByName("requests") = %v, %v`, got, ok)
	}
	if got, ok := ViewByName("http"); !ok || got != ViewRequests {
		t.Errorf(`ViewByName("http") = %v, %v`, got, ok)
	}
}

// TestRequestsViewRendersInTheFrame pins that the view reaches the screen:
// the panel's frame is its own, with the header and the table.
func TestRequestsViewRendersInTheFrame(t *testing.T) {
	p := New(Seams{
		Width:  func() int { return 140 },
		Height: func() int { return 30 },
		Requests: func() RequestState {
			return reqFixture()
		},
	})
	p.SetView(ViewRequests)
	lines := p.Lines()
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Requests") {
		t.Error("the frame has no Requests header")
	}
	if !strings.Contains(plain, "/v1internal:streamGenerateContent") {
		t.Error("the frame has no exchange rows")
	}
	if !strings.Contains(plain, "exchange(s)") {
		t.Error("the frame has no summary")
	}
}

// TestEnterOnARequestOpensTheReaderOnTheFirstPress pins the leaf shape: the
// legend promises "enter read", so Enter must not spend the press folding
// a summary the table already shows.
func TestEnterOnARequestOpensTheReaderOnTheFirstPress(t *testing.T) {
	p := New(Seams{
		Width:    func() int { return 140 },
		Height:   func() int { return 30 },
		Requests: func() RequestState { return reqFixture() },
	})
	p.SetView(ViewRequests)
	p.Lines() // one frame, so the rows and the heading are on record

	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter was not claimed by the Requests view")
	}
	if !p.ReaderOpen() {
		t.Fatal("Enter on a Requests row opened no reader; the legend says enter reads")
	}
	if p.reader.id != "req/3" {
		t.Errorf("the reader opened %q, want the cursor's row req/3", p.reader.id)
	}
}

// TestRequestsRowsAreLeaves pins the shape the key handling depends on: a
// row with Detail would fold on Enter and need a second press to read.
func TestRequestsRowsAreLeaves(t *testing.T) {
	for _, row := range RequestRows(reqFixture()) {
		if row.Foldable() {
			t.Errorf("row %s is foldable; a Requests row is a leaf so Enter reads it", row.ID)
		}
	}
}

// TestRequestEndpointGainsTheHostOnAFailoverSession pins the column's rule:
// one host is the path alone, two hosts need the name that tells them apart.
func TestRequestEndpointGainsTheHostOnAFailoverSession(t *testing.T) {
	rows := RequestRows(reqFixture())
	// The fixture talks to cloudcode-pa and gateway.example, so both rows
	// name their host.
	joined := ansi.Strip(rows[0].Text + "\n" + rows[1].Text)
	if !strings.Contains(joined, "gateway.example/v1/chat/completions") {
		t.Errorf("a two-host session does not name the host:\n%s", joined)
	}

	one := RequestState{Width: 140, Records: []wirelog.Record{reqFixture().Records[0]}}
	solo := ansi.Strip(RequestRows(one)[0].Text)
	if strings.Contains(solo, "cloudcode-pa.googleapis.com") {
		t.Errorf("a single-host session repeats the host: %q", solo)
	}
	if !strings.Contains(solo, "/v1internal:streamGenerateContent") {
		t.Errorf("the single-host row lost its path: %q", solo)
	}
}

// TestWheelScrollsTheRequestsView pins that the wheel is wired: without the
// view's own arm it scrolled the Logs document instead.
func TestWheelScrollsTheRequestsView(t *testing.T) {
	recs := make([]wirelog.Record, 0, 60)
	for i := 1; i <= 60; i++ {
		recs = append(recs, wirelog.Record{
			Seq: int64(i), Method: "POST", Path: "/v1internal:streamGenerateContent",
			Status: 200, RequestBytes: 100,
		})
	}
	p := New(Seams{
		Width:    func() int { return 140 },
		Height:   func() int { return 20 },
		Requests: func() RequestState { return RequestState{Records: recs} },
	})
	p.SetView(ViewRequests)
	p.Lines()

	if !p.Scroll(5) {
		t.Fatal("the wheel reported no movement on a 60-row table")
	}
	if p.requests.window != 5 {
		t.Errorf("the Requests window is at %d, want 5", p.requests.window)
	}
	if p.scroll != 0 {
		t.Errorf("the wheel moved the Logs offset to %d instead of the table's window", p.scroll)
	}
}

// TestAFullRingStillRebuildsTheRows pins the memo key: keyed on the count,
// a full ring would freeze the table at its 500th exchange because every
// later arrival leaves the length unchanged.
func TestAFullRingStillRebuildsTheRows(t *testing.T) {
	const capacity = 500
	ring := make([]wirelog.Record, 0, capacity)
	for i := int64(1); i <= capacity; i++ {
		ring = append(ring, wirelog.Record{Seq: i, Method: "POST", Path: "/x", Status: 200})
	}
	var mu sync.Mutex
	p := New(Seams{
		Width:  func() int { return 140 },
		Height: func() int { return 20 },
		Requests: func() RequestState {
			mu.Lock()
			defer mu.Unlock()
			// Hand back the ring the panel should see, newest first in
			// the slice the panel copies.
			return RequestState{Records: append([]wirelog.Record(nil), ring...)}
		},
	})
	p.SetView(ViewRequests)
	p.Lines()

	// One arrival, one eviction: the length is unchanged, the newest Seq
	// is not.
	mu.Lock()
	ring = append(ring[1:], wirelog.Record{Seq: capacity + 1, Method: "POST", Path: "/newest", Status: 200})
	mu.Unlock()

	// Deliberately no Invalidate(): this is the case the key exists for.
	// The generation covers "something told me the facts moved"; the key
	// covers "the facts moved between two draws of one generation", which
	// is what a redraw from a key press or a resize does. A length-keyed
	// memo is blind to it exactly when the ring is full.
	plain := ansi.Strip(strings.Join(p.Lines(), "\n"))
	if !strings.Contains(plain, "#501") {
		t.Errorf("the table did not show the 501st exchange; the memo keyed on the ring's length:\n%s", plain)
	}
	if strings.Contains(plain, "#1 ") {
		t.Errorf("the table still shows the evicted exchange #1:\n%s", plain)
	}
}

// TestFormatDurationReadsInTheRightUnit pins the column's unit choice.
func TestFormatDurationReadsInTheRightUnit(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "—"},
		// A sub-10 ms exchange keeps its decimal: "0 ms" cannot be told
		// from a request that never got an answer.
		{400 * time.Microsecond, "0.4 ms"},
		{9_600 * time.Microsecond, "9.6 ms"},
		{850 * time.Millisecond, "850 ms"},
		{1500 * time.Millisecond, "1.50 s"},
		{90 * time.Second, "1.5 m"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
