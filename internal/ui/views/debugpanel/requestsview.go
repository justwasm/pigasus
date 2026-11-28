// requestsview.go — the /debug surface's Requests view.
//
// # What it answers
//
// The other three views answer questions about the conversation. This one
// answers questions about the transport: which endpoint did this session
// talk to, with what status, how long did it take, and how big was the
// exchange. Those are the questions a failing turn raises, and before this
// view the only way to answer them was to run the reference binary behind
// internal/infra/agyproxy and read its capture files.
//
// # Why it is a table, not a tree
//
// A request has no children: the fields are its columns. The tree shape the
// other two views use would indent a row's own facts under it and turn a
// sortable table into an outline. Flat rows keep the columns aligned, and
// the reader (Enter) is where a row's body — the request and response text
// — is shown whole.
package debugpanel

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/wirelog"
	"github.com/justwasm/pigasus/palette"
)

// RequestState is the Requests view's facts.
type RequestState struct {
	// Records is the session's exchanges, oldest first, as the host read
	// them from the recorder.
	Records []wirelog.Record

	// Width is the terminal width, for truncating the endpoint column.
	Width int
}

// requestColumns are the table's column widths, in cells, after the cursor
// and the sequence number.
//
// They are fixed rather than computed from the data: a column that resizes
// as requests arrive makes the table jump under the reader's eye, and every
// value here has a natural maximum (a status is three digits, a duration
// reads in milliseconds or seconds).
const (
	reqMethodWidth = 4 // GET/POST, the longest verb the CLI uses
	reqStatusWidth = 4 // " 200", " 503", "   —"
	reqDurWidth    = 8 // "999.9 ms", " 1.23 s"
	reqSizeWidth   = 9 // "12.3 KB"
	reqGap         = "  "

	// slowThreshold is when a response counts as slow to start. A first
	// token that takes a second is the top of normal for a large prompt;
	// past that the endpoint is thinking before it streams, which is what
	// a reader wants flagged.
	slowThreshold = time.Second
)

// RequestRows renders the session's exchanges as flat rows.
//
// Newest first: the request a reader wants is almost always the one that
// just happened, and the ring's oldest entries are the ones about to age
// out.
func RequestRows(st RequestState) []FoldRow {
	recs := append([]wirelog.Record(nil), st.Records...)
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Seq > recs[j].Seq })

	width := st.Width
	if width <= 0 {
		width = 100
	}
	// The endpoint column takes what is left after the fixed ones. The
	// floor keeps a narrow terminal from collapsing it to nothing: at
	// that point the row is mostly numbers, and the path is the part a
	// reader needs.
	endpointWidth := width - (2 + reqMethodWidth + 1 + reqStatusWidth + len(reqGap) +
		reqDurWidth + len(reqGap) + reqSizeWidth + len(reqGap))
	if endpointWidth < 16 {
		endpointWidth = 16
	}

	// The host column is only worth its cells when the session talked to
	// more than one: a single-endpoint session would repeat the same name
	// on every row and lose the path it is there to show.
	showHost := len(requestHosts(recs)) > 1

	rows := make([]FoldRow, 0, len(recs))
	for _, r := range recs {
		rows = append(rows, requestRow(r, endpointWidth, showHost))
	}
	return rows
}

// requestRow renders one exchange.
func requestRow(r wirelog.Record, endpointWidth int, showHost bool) FoldRow {
	status := palette.DimGrayOpen + "   —" + palette.DimGrayClose
	switch {
	case r.Err != "" && r.Status == 0:
		// A request that never got an answer is the loudest thing in the
		// table: it is the shape of a network failure, and it must not
		// read like a slow success.
		status = logErrorOpen + " ERR" + logColourClose
	case r.OK():
		status = logSuccessOpen + padLeft(fmt.Sprint(r.Status), reqStatusWidth) + logColourClose
	case r.Status >= 400:
		status = logErrorOpen + padLeft(fmt.Sprint(r.Status), reqStatusWidth) + logColourClose
	default:
		status = logRetryOpen + padLeft(fmt.Sprint(r.Status), reqStatusWidth) + logColourClose
	}

	dur := palette.DimGrayOpen + padLeft(formatDuration(r.Duration), reqDurWidth) + palette.DimGrayClose
	size := palette.DimGrayOpen + padLeft(HumanBytes64(r.RequestBytes), reqSizeWidth) + palette.DimGrayClose

	endpoint := requestEndpoint(r, showHost)
	if ansi.StringWidth(endpoint) > endpointWidth {
		endpoint = ansi.Truncate(endpoint, endpointWidth, "…")
	}

	line := palette.DimGrayOpen + fmt.Sprintf("#%d", r.Seq) + palette.DimGrayClose + " " +
		palette.SubtleOpen + padRight(r.Method, reqMethodWidth) + palette.DimGrayClose + " " +
		status + reqGap +
		endpoint + reqGap +
		dur + reqGap +
		size

	// The row is a leaf: it carries no Detail, so Enter does not fold it
	// and the reader opens on the first press. That is the difference
	// between this view and the two above it. A tree row folds before it
	// reads because the folded body is already on screen; here the body is
	// tens of kilobytes in memory, the inline summary would only repeat
	// the columns beside it, and a fold would put a second key between the
	// user and the exchange the legend promises Enter opens.
	return FoldRow{
		ID:   fmt.Sprintf("req/%d", r.Seq),
		Text: line,
		Flat: true,
	}
}

// requestEndpoint is the path, prefixed with the host when the session
// talked to more than one endpoint.
//
// A session usually talks to one host, and repeating "cloudcode-pa.
// googleapis.com" on every row costs the column that shows which RPC was
// called — but a session that failed over to a gateway has two, and then
// the host is the column that explains the difference. The host is always
// in the reader's exchange block, so nothing is lost either way.
func requestEndpoint(r wirelog.Record, showHost bool) string {
	switch {
	case r.Host == "":
		return r.Path
	case showHost:
		return r.Host + r.Path
	}
	return r.Path
}

// requestFacts is the exchange's own numbers, as one block of the reader.
//
// The table's columns carry the four measures that fit on a line; these
// are the ones that do not — the exact start time, the byte counts before
// the cap, the body's fingerprint — so a reader who opened the exchange to
// compare it with the last one has them without counting characters.
func requestFacts(r wirelog.Record) string {
	var b strings.Builder
	add := func(label, value string) {
		if value == "" {
			return
		}
		// Twelve cells, because "first byte" is the longest label and a
		// column that resized as records arrived would let the block's
		// two halves disagree about where the values begin.
		fmt.Fprintf(&b, "%s%s\n", padRight(label, 12), value)
	}
	add("host", r.Host)
	add("started", r.Started.Format("15:04:05.000"))
	add("took", formatDuration(r.Duration))
	if r.TTFB > 0 && r.TTFB != r.Duration {
		// Only for a stream, where the two differ: repeating the total
		// under a second name would be one number printed twice.
		add("first byte", formatDuration(r.TTFB))
	}
	add("sent", HumanBytes64(r.RequestBytes)+"  ("+wirelog.Fingerprint(r.RequestBody)+")")
	add("recv", HumanBytes64(r.ResponseBytes))
	if r.PromptTokens > 0 || r.CompletionTokens > 0 {
		usageStr := fmt.Sprintf("%d prompt", r.PromptTokens)
		if r.CompletionTokens > 0 {
			usageStr += fmt.Sprintf(" · %d completion", r.CompletionTokens)
		}
		if r.TotalTokens > 0 {
			usageStr += fmt.Sprintf(" · %d total", r.TotalTokens)
		}
		add("tokens", usageStr)

		if r.PromptTokens > 0 && r.RequestBytes > 0 {
			// The ratio is a diagnostic, so it must not label a value
			// "normal" while naming a band the value is outside of: the
			// first version printed "1.3 B/prompt token (normal ~3.0-4.0)",
			// which is a contradiction on one line and teaches the reader
			// to distrust every other figure in the table.
			//
			// The band is wide because the true ratio moves with the
			// dialect: CloudCode's JSON wrapper spends bytes on a project
			// id, a request id and escaped braces, while a bare Gemini REST
			// call is close to the ~3.5 B/token that tokenisers average.
			// What the ratio is *for* is the outlier — a gateway that
			// rewrote the prompt, or an endpoint charging for tokens it was
			// not sent — so it says which side of the band it fell on.
			ratio := r.BytesPerPromptToken()
			ratioStr := fmt.Sprintf("%.2f B/prompt token", ratio)
			switch {
			case ratio < 1.0:
				ratioStr += " (fewer bytes than tokens: check the report)"
			case ratio < 2.0:
				ratioStr += " (byte-lean: a JSON wrapper's overhead)"
			case ratio <= 6.0:
				ratioStr += " (in the usual 2–6 band)"
			case ratio <= 10.0:
				ratioStr += " (byte-heavy: padding or a rewritten prompt)"
			default:
				ratioStr += " (far above the usual band: check the report)"
			}
			add("ratio", ratioStr)
		}
	}
	if r.Err != "" {
		add("error", r.Err)
	}
	return strings.TrimRight(b.String(), "\n")
}

// RequestHeaderLines renders the view's pinned header.
func RequestHeaderLines(st RequestState) []string {
	recs := st.Records
	failed, slow := 0, 0
	var total time.Duration
	for _, r := range recs {
		if r.Err != "" || r.Status >= 400 {
			failed++
		}
		// "Slow" is slow to *start*, not slow to finish. A streamed
		// answer that took nine seconds to arrive steadily is not the
		// same complaint as one that sat silent for nine seconds before
		// its first frame, and only the second is worth counting: the
		// first is the answer's length, which the size column shows.
		if r.TTFB > slowThreshold {
			slow++
		}
		total += r.Duration
	}
	head := "\x1b[1m" + palette.SubtleOpen + "└ Requests" + "\x1b[0m"
	head += palette.DimGrayOpen + fmt.Sprintf("  ·  %d exchange(s)", len(recs))
	if failed > 0 {
		head += "  ·  " + logErrorOpen + fmt.Sprintf("%d failed", failed) + logColourClose + palette.DimGrayOpen + palette.DimGrayOpen
	}
	if slow > 0 {
		head += "  ·  " + logRetryOpen + fmt.Sprintf("%d slow", slow) + logColourClose + palette.DimGrayOpen + palette.DimGrayOpen
	}
	if len(recs) > 0 {
		head += fmt.Sprintf("  ·  avg %s", formatDuration(total/time.Duration(len(recs))))
	}
	head += palette.DimGrayClose

	// The endpoint line names the hosts the session talked to, which is
	// the "domain analysis" the table's path column cannot show on its
	// own.
	hosts := requestHosts(recs)
	sub := "   " + palette.DimGrayOpen
	if len(hosts) == 0 {
		sub += "no exchanges recorded yet — send a message and come back"
	} else {
		sub += strings.Join(hosts, "  ·  ")
	}
	sub += palette.DimGrayClose
	return []string{head, sub}
}

// requestHosts summarises the endpoints: host and how many exchanges went
// to it, most-used first.
func requestHosts(recs []wirelog.Record) []string {
	counts := map[string]int{}
	for _, r := range recs {
		h := r.Host
		if h == "" {
			h = "(no host)"
		}
		counts[h]++
	}
	type hostCount struct {
		host string
		n    int
	}
	list := make([]hostCount, 0, len(counts))
	for h, n := range counts {
		list = append(list, hostCount{h, n})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].host < list[j].host
	})
	out := make([]string, 0, len(list))
	for _, hc := range list {
		out = append(out, fmt.Sprintf("%s ×%d", hc.host, hc.n))
	}
	return out
}

// RequestLegend is the view's key hint, ordered like the other two so a
// narrow terminal keeps the key that leaves the surface.
func RequestLegend() string {
	return "enter read  ·  c copy curl  ·  C copy issue md  ·  y copy row  ·  tab next view"
}

// CurlCommand returns a reproducible curl command string for the exchange,
// with secrets redacted.
// CurlCommand returns a reproducible curl command string for the exchange,
// with secrets redacted.
//
// # Why it redacts again
//
// The transport already sanitises what it records, so a record off the wire
// is safe by construction. This function does not rely on that, and must
// not: the clipboard is the one place a leaked credential does real damage —
// the user pastes it into an issue, a chat, a bug report — and "the caller
// was supposed to have sanitised this" is not a property a copy helper can
// check. The redaction is cheap, it is idempotent, and it means this
// function is safe for any Record, including one a test or a future
// recorder built without going through the transport.
func CurlCommand(r wirelog.Record) string {
	var b strings.Builder
	method := r.Method
	if method == "" {
		method = "GET"
	}
	rawURL := r.URL
	if rawURL == "" {
		rawURL = r.Path
	}
	fmt.Fprintf(&b, "curl -X %s '%s'", method, wirelog.RedactURL(rawURL))
	for _, k := range sortedHeaderKeys(r.RequestHeaders) {
		fmt.Fprintf(&b, " \\\n  -H '%s: %s'", k, redactedHeaderValue(k, r.RequestHeaders[k]))
	}
	if r.RequestBody != "" {
		escaped := strings.ReplaceAll(wirelog.Redact(r.RequestBody), "'", `'\''`)
		fmt.Fprintf(&b, " \\\n  -d '%s'", escaped)
	}
	return b.String()
}

// isSecretHeaderName reports whether a header name holds a credential.
//
// It is a copy of wirelog's rule rather than a call to it because that one
// is unexported, and the alternative — exporting a predicate whose only
// caller is this view — would put a view's needs in the recorder's API. The
// two lists are pinned equal by TestTheSecretHeaderRuleMatchesTheRecorder,
// so a header added to one cannot be forgotten in the other.
func isSecretHeaderName(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "x-api-key", "x-goog-api-key", "api-key",
		"proxy-authorization", "cookie", "set-cookie":
		return true
	}
	return false
}

// redactedHeaderValue is what a header line shows in an exported exchange: a
// credential header name reads "<redacted>" whole, and every other value is
// sanitised in case it carries a token mid-string.
func redactedHeaderValue(name string, values []string) string {
	if isSecretHeaderName(name) {
		return "<redacted>"
	}
	return wirelog.Redact(strings.Join(values, ", "))
}

// MarkdownExchange returns a GitHub-issue-ready markdown snippet for the
// exchange.
//
// "Issue-ready" is the whole claim of this function, and it is what makes
// the redaction load-bearing: the snippet exists to be pasted into a public
// tracker. It redacts for itself rather than trusting that the record was
// sanitised upstream — see CurlCommand for why a copy helper cannot rely on
// its caller here.
func MarkdownExchange(r wirelog.Record) string {
	var b strings.Builder
	statusStr := fmt.Sprintf("%d %s", r.Status, httpStatusText(r.Status))
	if r.Err != "" {
		statusStr = fmt.Sprintf("Error: %s", wirelog.Redact(r.Err))
	}
	fmt.Fprintf(&b, "### HTTP Exchange #%d: %s %s\n\n", r.Seq, r.Method, wirelog.RedactURL(r.Path))
	fmt.Fprintf(&b, "- **Status**: %s\n", statusStr)
	fmt.Fprintf(&b, "- **Endpoint**: `%s`\n", wirelog.Redact(r.Host))
	fmt.Fprintf(&b, "- **Duration**: %s (TTFB: %s)\n", formatDuration(r.Duration), formatDuration(r.TTFB))
	if r.PromptTokens > 0 {
		fmt.Fprintf(&b, "- **Tokens**: %d prompt, %d completion\n",
			r.PromptTokens, r.CompletionTokens)
	}
	fmt.Fprintf(&b, "- **Bytes**: %s sent, %s received\n\n",
		HumanBytes64(r.RequestBytes), HumanBytes64(r.ResponseBytes))

	fmt.Fprintf(&b, "<details><summary>Request (%s)</summary>\n\n```http\n", HumanBytes64(r.RequestBytes))
	fmt.Fprintf(&b, "%s %s\n", r.Method, wirelog.RedactURL(r.URL))
	for _, k := range sortedHeaderKeys(r.RequestHeaders) {
		fmt.Fprintf(&b, "%s: %s\n", k, redactedHeaderValue(k, r.RequestHeaders[k]))
	}
	if r.RequestBody != "" {
		fmt.Fprintf(&b, "\n%s\n", wirelog.Redact(r.RequestBody))
	}
	fmt.Fprintf(&b, "```\n\n</details>\n\n")

	fmt.Fprintf(&b, "<details><summary>Response (%s)</summary>\n\n```http\n", HumanBytes64(r.ResponseBytes))
	if r.Status != 0 {
		fmt.Fprintf(&b, "HTTP/1.1 %d %s\n", r.Status, httpStatusText(r.Status))
	}
	for _, k := range sortedHeaderKeys(r.ResponseHeaders) {
		fmt.Fprintf(&b, "%s: %s\n", k, redactedHeaderValue(k, r.ResponseHeaders[k]))
	}
	if r.Err != "" {
		fmt.Fprintf(&b, "\nError: %s\n", wirelog.Redact(r.Err))
	}
	if r.ResponseBody != "" {
		fmt.Fprintf(&b, "\n%s\n", wirelog.Redact(r.ResponseBody))
	}
	fmt.Fprintf(&b, "```\n\n</details>\n")
	return b.String()
}

// requestRowSeq parses a Requests row's ID back into the record's Seq.
//
// The row ID is the only thing the reader is handed, so the number has to
// survive the round trip: it is written as "req/<seq>" and read back here
// rather than kept in a side map that a rebuild would empty.
func requestRowSeq(id string) (int64, bool) {
	const prefix = "req/"
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.ParseInt(id[len(prefix):], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// formatDuration renders a duration the way the table's column reads.
//
// Below 10 ms it keeps a decimal: a local mock answers in 0.4 ms and a
// real endpoint in 300 ms, and truncating the first to "0 ms" makes a fast
// exchange indistinguishable from one that never got an answer — which is
// the reading the ERR cell exists to prevent.
func formatDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2f s", d.Seconds())
	default:
		return fmt.Sprintf("%.1f m", d.Minutes())
	}
}

// HumanBytes64 is HumanBytes for the int64 the recorder keeps.
func HumanBytes64(n int64) string { return HumanBytes(int(n)) }

// RequestsBody is the reader's body for one exchange: the request and the
// response, whole, labelled.
//
// It is built from the record rather than read from the host because the
// record *is* the body: the store already holds the text, and a second read
// through a seam would be a second copy of the same bytes with a round trip
// between them.
func RequestsBody(r wirelog.Record) Body {
	var parts []ReaderPart
	// The summary leads, so the exchange's identity and its timing are the
	// first thing in the reader rather than something to scroll a 32 KB
	// request body to find.
	parts = append(parts, ReaderPart{Label: "exchange", Text: requestFacts(r)})

	// Headers and body are separate parts, not one block with a blank line
	// between them. The reader offers `t` for a part that parses as JSON,
	// and a block that opens with "POST /v1internal…" parses as nothing —
	// so a combined block would silently cost the format key the one body
	// in this panel big enough to need it.
	var reqHead strings.Builder
	fmt.Fprintf(&reqHead, "%s %s\n", r.Method, r.URL)
	for _, k := range sortedHeaderKeys(r.RequestHeaders) {
		fmt.Fprintf(&reqHead, "%s: %s\n", k, strings.Join(r.RequestHeaders[k], ", "))
	}
	parts = append(parts, ReaderPart{Label: "request headers", Text: strings.TrimRight(reqHead.String(), "\n")})
	if r.RequestBody != "" {
		parts = append(parts, ReaderPart{Label: requestBodyLabel(r), Text: r.RequestBody})
	}

	var respHead strings.Builder
	if r.Status != 0 {
		fmt.Fprintf(&respHead, "%d %s\n", r.Status, httpStatusText(r.Status))
	}
	for _, k := range sortedHeaderKeys(r.ResponseHeaders) {
		fmt.Fprintf(&respHead, "%s: %s\n", k, strings.Join(r.ResponseHeaders[k], ", "))
	}
	if r.Err != "" {
		fmt.Fprintf(&respHead, "\ntransport error: %s\n", r.Err)
	}
	parts = append(parts, ReaderPart{Label: "response headers", Text: strings.TrimRight(respHead.String(), "\n")})
	if r.ResponseBody != "" {
		parts = append(parts, ReaderPart{Label: responseBodyLabel(r), Text: r.ResponseBody})
	}

	return Body{Title: r.Label(), Parts: parts}
}

// requestBodyLabel names the request body's kind, and says when the cap cut
// it.
//
// The cap is the one thing a reader must be told here: a request body is
// JSON in every dialect the tree speaks, so a body that stops mid-object is
// a cut body, not a malformed one — and a reader who does not know the
// difference will look for a bug in the client instead of at the cap.
func requestBodyLabel(r wirelog.Record) string {
	if r.RequestTruncated {
		return "request body · capped"
	}
	return "request body"
}

// responseBodyLabel names the response body's kind, and says when the cap
// cut it.
//
// An SSE stream is many JSON frames with "data: " in front of each, so it
// does not parse as one document however complete it is: the label says
// "sse" and the reader prints the frames as they came, which is what a
// reader comparing them with a capture file wants anyway.
func responseBodyLabel(r wirelog.Record) string {
	switch {
	case r.ResponseTruncated:
		return "response body · sse, capped"
	case strings.HasPrefix(strings.TrimSpace(r.ResponseBody), "data: "):
		return "response body · sse"
	case structured(r.ResponseBody):
		return "response body"
	}
	return "response body"
}

// sortedHeaderKeys returns the header names in a stable order, so a body
// read twice reads the same.
func sortedHeaderKeys(h http.Header) []string {
	if len(h) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// httpStatusText names a status code, for the reader's first line.
func httpStatusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 204:
		return "No Content"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	}
	return ""
}
