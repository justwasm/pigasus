// Package wirelog records the HTTP exchanges the CLI makes on the model's
// behalf, so /logs can show what actually went over the wire.
//
// # Why this exists
//
// The clone already had one way to see a request body: run the reference
// binary behind internal/infra/agyproxy and read the JSON it captures. That
// answers "what does the reference send", and it is the wrong tool for
// "what did *this* session send" — it needs a second process, it only sees
// traffic that went through the proxy, and its records are files on disk
// (14 GiB of them, on one machine, each carrying a live OAuth token).
//
// A Record here is one exchange the CLI itself performed. The CLI is the
// only process that knows the answer, so it keeps the answer itself.
//
// # Why in memory, and bounded
//
// The evidence is only useful while the session is live: a request log is
// read to answer "why did that turn fail" ten seconds ago, not last week.
// Keeping it in a ring in memory means no directory to prune, no token
// written to disk, and no way for the store to grow past its ceiling. The
// ceiling is a count of records, not bytes: a record's body is already
// bounded by MaxBodyBytes, so count × MaxBodyBytes is the worst case and
// it is a number a reader can hold in their head.
//
// # What it deliberately does not do
//
// It does not parse the body. The model wire has three dialects (CloudCode's
// protobuf-shaped JSON, the Gemini REST shape, and whatever an OpenAI- or
// Anthropic-compatible gateway speaks), and a package in tier 1 must not
// grow a parser per dialect to answer a question the reader can answer by
// looking. The body is kept as text; the analysis the /logs view shows
// (host, path, status, timing, size) comes from the request line and the
// response status, which every dialect shares.
package wirelog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/clilog"
)

const (
	// DefaultCapacity is how many exchanges the ring holds.
	//
	// A turn makes one request; a session with retries, failover and
	// subagents makes tens. 500 is several sessions' worth of the recent
	// past, and at MaxBodyBytes each the ring cannot exceed ~64 MiB.
	DefaultCapacity = 500

	// MaxBodyBytes caps the body text one record keeps. A system prompt is
	// tens of kilobytes and a streamed response can be megabytes; the
	// point of the record is to read the request, so the cap is generous
	// for the former and truncates the latter with a note that says so.
	MaxBodyBytes = 128 << 10

	// HeadBodyBytes is how many bytes are kept from the beginning of an
	// oversized body.
	HeadBodyBytes = 96 << 10

	// TailBodyBytes is how many bytes are kept from the end of an oversized
	// body, preserving the finish_reason, usageMetadata, and trailing error
	// frames of an SSE stream.
	TailBodyBytes = 32 << 10
)

// Record is one HTTP exchange.
//
// A record is immutable once Store.Add returns it, so the view can read one
// while the transport writes the next without a lock held across the body.
type Record struct {
	// Seq is the record's position in the session, from 1. It is the
	// stable key the view uses for a row's fold state, and it survives
	// the ring evicting older records: a row's identity does not change
	// because its neighbours aged out.
	Seq int64

	// Started is when the request was sent; Duration is how long the
	// exchange took end to end, including the response body.
	//
	// TTFB is how long the first response byte took. For a streamed turn
	// the two answer different questions: a large Duration with a small
	// TTFB is a long answer arriving steadily, while both large is an
	// endpoint that took its time to start. It is zero for a request that
	// got no response.
	Started  time.Time
	Duration time.Duration
	TTFB     time.Duration

	// Method and URL are the request line as sent. URL keeps the query
	// string (an endpoint puts its key in it, and the key is redacted —
	// see Redact) but not the fragment, which is never sent.
	Method string
	URL    string

	// Host is URL's host, and Path its path, split out because the view
	// groups and sorts by them and a reader should not have to parse a
	// URL to see the endpoint.
	Host string
	Path string

	// Status is the response status code, or 0 when no response arrived
	// (a transport error, a cancelled context).
	Status int

	// Err is the transport error's message, empty on success. A record
	// with Err set and Status 0 is a request that never got an answer.
	Err string

	// RequestHeaders and ResponseHeaders are the headers as sent and
	// received, already redacted.
	RequestHeaders  http.Header
	ResponseHeaders http.Header

	// RequestBody and ResponseBody are the bodies as text, capped at
	// MaxBodyBytes. RequestTruncated and ResponseTruncated say the cap
	// bit.
	RequestBody       string
	ResponseBody      string
	RequestTruncated  bool
	ResponseTruncated bool

	// RequestBytes and ResponseBytes are the bodies' real sizes, before
	// the cap. They are what the view's size column shows: the point of
	// the column is "how big was this really", and a capped body would
	// understate it.
	RequestBytes  int64
	ResponseBytes int64

	// PromptTokens and CompletionTokens are the token counts reported by
	// the provider for this exchange, or 0 if unparsed or not reported.
	// TotalTokens is prompt + completion (or provider total).
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// BytesPerPromptToken returns RequestBytes / PromptTokens, or 0 if prompt
// tokens or request bytes are not available.
func (r Record) BytesPerPromptToken() float64 {
	if r.PromptTokens <= 0 || r.RequestBytes <= 0 {
		return 0
	}
	return float64(r.RequestBytes) / float64(r.PromptTokens)
}

// OK reports whether the exchange completed with a 2xx.
func (r Record) OK() bool { return r.Status >= 200 && r.Status < 300 }

// Label is the one-line identity the view's rows and reader titles use.
func (r Record) Label() string {
	return fmt.Sprintf("#%d %s %s", r.Seq, r.Method, r.Path)
}

// Store is the session's ring of records.
//
// The zero value is not usable; construct with New. A nil *Store is a
// working no-op, which is what lets a caller that was handed no store run
// the transport unwrapped.
type Store struct {
	mu   sync.RWMutex
	recs []Record // ring, oldest first once full
	cap  int
	seq  int64
}

// New returns a store holding the last capacity records. A capacity below
// 1 is DefaultCapacity.
func New(capacity int) *Store {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &Store{cap: capacity}
}

// Add appends a record and returns it with its Seq assigned.
func (s *Store) Add(r Record) Record {
	if s == nil {
		return r
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	r.Seq = s.seq
	if len(s.recs) < s.cap {
		s.recs = append(s.recs, r)
		return r
	}
	// Full: drop the oldest. Shifting is O(n) but n is the capacity and
	// the operation happens once per request, not per frame.
	copy(s.recs, s.recs[1:])
	s.recs[len(s.recs)-1] = r
	return r
}

// Records returns the records oldest first, as a copy.
func (s *Store) Records() []Record {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, len(s.recs))
	copy(out, s.recs)
	return out
}

// Len is how many records are held.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.recs)
}

// Get returns the record with the given Seq.
func (s *Store) Get(seq int64) (Record, bool) {
	if s == nil {
		return Record{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// The ring is ordered by Seq, so a binary search is the honest
	// lookup: a view asking for an old row after a few hundred records
	// should not scan the whole ring.
	i := sort.Search(len(s.recs), func(i int) bool { return s.recs[i].Seq >= seq })
	if i < len(s.recs) && s.recs[i].Seq == seq {
		return s.recs[i], true
	}
	return Record{}, false
}

// Reset drops every record and restarts the numbering. It exists for the
// view's "clear" key and for tests; a session never calls it implicitly.
func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = nil
	s.seq = 0
}

// Shared is the process's store, which the transport installs into and the
// /logs view reads from.
//
// It is process-global because the two ends cannot see each other: the
// transport is built deep in tier 3 by backendselect, and the view is a
// tier 5 package the host wires. Passing a store down that path would mean
// threading a new parameter through every backend constructor to serve one
// diagnostics view, so the one thing they agree on is this package.
var (
	sharedOnce sync.Once
	shared     *Store
)

// Shared returns the process's store, creating it on first use.
func Shared() *Store {
	sharedOnce.Do(func() { shared = New(DefaultCapacity) })
	return shared
}

// SetShared replaces the process store. It exists for tests that want a
// fresh ring without racing another test's writes; production calls Shared.
func SetShared(s *Store) {
	sharedOnce.Do(func() {})
	shared = s
}

// Captured is one side of an exchange as the recorder holds it: the head
// and tail bytes it kept, and how many the exchange really carried.
//
// When an exchange exceeds MaxBodyBytes, the recorder preserves the first
// HeadBodyBytes (96 KiB) and trailing TailBodyBytes (32 KiB). Retaining the
// tail ensures that finish_reason, usageMetadata, and trailing error frames
// of an SSE stream survive truncation.
type Captured struct {
	// Head is the body's first bytes, up to HeadBodyBytes (or all of it if
	// the body was within MaxBodyBytes).
	Head []byte

	// Tail is the body's trailing bytes, up to TailBodyBytes, when the body
	// exceeded MaxBodyBytes.
	Tail []byte

	// Total is how many bytes the body really carried, before the cap.
	Total int64
}

// CaptureBody builds a Captured from a body held whole in memory.
//
// The kept slices are copied rather than sliced from b. A slice of a 10 MB body
// keeps the whole 10 MB array alive for as long as the record lives, and a
// ring of 500 such records is not the ~64 MiB ceiling the package comment
// promises. The copies are at most HeadBodyBytes + TailBodyBytes per record.
func CaptureBody(b []byte) Captured {
	total := int64(len(b))
	if total <= int64(MaxBodyBytes) {
		head := make([]byte, len(b))
		copy(head, b)
		return Captured{Head: head, Total: total}
	}
	head := make([]byte, HeadBodyBytes)
	copy(head, b[:HeadBodyBytes])
	tail := make([]byte, TailBodyBytes)
	copy(tail, b[len(b)-TailBodyBytes:])
	return Captured{Head: head, Tail: tail, Total: total}
}

// Capture caps the bodies for display, extracts token usage if present,
// and adds the record to the store.
//
// The sizes come from Captured.Total — the bytes the exchange carried, not
// the bytes the record kept.
func (s *Store) Capture(r Record, reqBody, respBody Captured) Record {
	r.RequestBytes = reqBody.Total
	r.ResponseBytes = respBody.Total
	r.RequestBody, r.RequestTruncated = capBody(reqBody)
	r.ResponseBody, r.ResponseTruncated = capBody(respBody)
	if p, c, tot, ok := ExtractUsage(r.ResponseBody); ok {
		r.PromptTokens = p
		r.CompletionTokens = c
		r.TotalTokens = tot
	}
	return s.Add(r)
}

// capBody returns the body as text, capped, and whether the cap bit.
//
// If the body exceeded MaxBodyBytes, Head and Tail are joined with a note
// explicitly stating how many bytes were omitted and the total carried.
func capBody(c Captured) (string, bool) {
	if c.Total <= int64(MaxBodyBytes) {
		return string(c.Head), false
	}
	retained := len(c.Head) + len(c.Tail)
	omitted := c.Total - int64(retained)
	note := fmt.Sprintf("\n\n… [%d bytes truncated by wirelog: retained %d head + %d tail out of %d total (the exchange carried %d)] …\n\n",
		omitted, len(c.Head), len(c.Tail), c.Total, c.Total)
	return string(c.Head) + note + string(c.Tail), true
}

// Redact removes credentials from a header set, a URL or a body.
//
// The exchange carries an OAuth bearer, an API key in a query string and
// sometimes a key in a header the provider invented. A diagnostics surface
// that shows them is a credential leak with a copy button, so every string
// a record keeps goes through here first.
//
// The body is passed through clilog.Sanitize, which already owns the
// patterns for the three shapes the CLI emits; a body is text and may
// quote a header, so it needs the same treatment.
func Redact(s string) string { return clilog.Sanitize(s) }

// RedactURL returns a URL with credentials removed from every place a URL
// can carry one.
//
// # Why this is not just Redact
//
// clilog.Sanitize is a set of patterns over a *log line*, and it redacts a
// query parameter only when the value happens to look like a key it knows
// (`AIza…`, `sk-…`, a long bearer). A URL is a structure, not a line, and it
// says which parameter is a credential in the parameter's own name:
//
//	?key=<redacted>          the CloudCode endpoint's API key
//	?access_token=<redacted> an OAuth grant
//
// The pattern path missed `?access_token=xyz` outright — it redacts nothing
// that short — and would have kept it in the record and in a `c`-copied curl
// command. So the query is parsed, and a parameter whose name is on the
// credential list is replaced whatever its value looks like; every other
// parameter keeps its value, sanitised.
func RedactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		// Not a URL we can take apart; fall back to the text rule so a
		// malformed string still gets whatever sanitising it can. The
		// fallback must not re-encode: url.Parse accepts almost any string
		// as a relative path, and round-tripping one through u.String()
		// percent-encodes the spaces and the token with them.
		return Redact(raw)
	}
	// Never keep userinfo: user:password@host is a credential by position,
	// not by name.
	u.User = nil
	if u.RawQuery != "" {
		// Rebuilt by hand rather than through url.Values.Encode: that would
		// percent-encode the placeholder into %3Credacted%3E, and a marker a
		// reader has to decode is a marker they will read as an opaque key.
		//
		// Each pair is decoded, judged by its parameter name, and
		// re-encoded, so the name survives verbatim and the value is
		// whatever the name says it should be.
		if _, err := url.ParseQuery(u.RawQuery); err != nil {
			u.RawQuery = Redact(u.RawQuery)
			return Redact(u.String())
		}
		var parts []string
		for _, pair := range strings.Split(u.RawQuery, "&") {
			name, rest, hasValue := strings.Cut(pair, "=")
			key, err := url.QueryUnescape(name)
			if err != nil {
				key = name
			}
			if hasValue && isSecretParam(key) {
				parts = append(parts, name+"=<redacted>")
				continue
			}
			if hasValue {
				if decoded, err := url.QueryUnescape(rest); err == nil {
					parts = append(parts, name+"="+url.QueryEscape(Redact(decoded)))
					continue
				}
			}
			parts = append(parts, Redact(pair))
		}
		u.RawQuery = strings.Join(parts, "&")
	}
	return Redact(u.String())
}

// isSecretParam reports whether a query parameter's name carries a
// credential. Names are matched case-insensitively against the spellings the
// model endpoints actually use.
func isSecretParam(name string) bool {
	switch strings.ToLower(name) {
	case "key", "api_key", "apikey", "api-key",
		"access_token", "accesstoken", "id_token", "refresh_token", "token",
		"auth", "authorization", "sig", "signature":
		return true
	}
	return false
}

// redactHeaders returns a copy of h with the values Redact knows about
// replaced. A header whose *name* says it holds a credential is dropped
// whole rather than sanitised: the value is the secret, and a partial
// redaction of it is still a leak.
func redactHeaders(h http.Header) http.Header {
	if len(h) == 0 {
		return nil
	}
	out := make(http.Header, len(h))
	for k, vs := range h {
		if isSecretHeader(k) {
			out[k] = []string{"<redacted>"}
			continue
		}
		cp := make([]string, len(vs))
		for i, v := range vs {
			cp[i] = Redact(v)
		}
		out[k] = cp
	}
	return out
}

// isSecretHeader reports whether a header name carries a credential.
func isSecretHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "x-api-key", "x-goog-api-key", "api-key",
		"proxy-authorization", "cookie", "set-cookie":
		return true
	}
	return false
}

// Fingerprint is a short, stable hash of a body, for the view's "is this
// the same request as the last one" question without printing the body.
func Fingerprint(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:6])
}
