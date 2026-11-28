// copy_redaction_test.go — what a copy key puts on the clipboard.
//
// The clipboard is the worst place for a credential to leak: the point of
// `c curl` and `C md` is that the text leaves the terminal and lands in an
// issue, a chat, a bug report. Every test here hands the exporters a Record
// that was NOT sanitised, because that is the property being pinned: the
// exporters redact for themselves rather than trusting the caller.
package debugpanel

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/wirelog"
)

// secretiveRecord is an exchange as it would look if nothing had sanitised
// it. A real record off the transport is already clean; this is the case a
// copy helper must survive anyway.
func secretiveRecord() wirelog.Record {
	return wirelog.Record{
		Seq:    1,
		Method: "POST",
		URL:    "https://api.example.com/v1internal:streamGenerateContent?key=QUERY-SECRET",
		Host:   "api.example.com",
		Path:   "/v1internal:streamGenerateContent",
		Status: 200,
		RequestHeaders: map[string][]string{
			"Authorization":       {"Bearer ya29.SECRETVALUE0000000000"},
			"X-Api-Key":           {"HEADERSECRETVALUE"},
			"Cookie":              {"session=COOKIESECRETVALUE"},
			"Content-Type":        {"application/json"},
			"X-Trace-Id":          {"trace AIzaSECRETVALUE000000000000"},
			"Proxy-Authorization": {"Basic PROXYSECRETVALUE"},
		},
		ResponseHeaders: map[string][]string{
			"Set-Cookie":   {"sid=SETCOOKIESECRETVALUE; HttpOnly"},
			"Content-Type": {"text/event-stream"},
		},
		RequestBody:   `{"authorization":"Bearer ya29.BODYSECRET0000000000"}`,
		ResponseBody:  `{"data":"resp ya29.RESPSECRET0000000000"}`,
		RequestBytes:  100,
		ResponseBytes: 200,
	}
}

var allSecrets = []string{
	"BEARER-SECRET",
	"HEADERSECRETVALUE",
	"COOKIE-SECRET",
	"PROXY-SECRET",
	"SETCOOKIE-SECRET",
	"BODY-SECRET",
	"SECRET",
}

// TestCurlCopiesNothingSecret — `c` never puts a credential on the
// clipboard, whichever place the record carries one.
func TestCurlCopiesNothingSecret(t *testing.T) {
	out := CurlCommand(secretiveRecord())
	for _, secret := range allSecrets {
		if strings.Contains(out, secret) {
			t.Errorf("curl leaks %q:\n%s", secret, out)
		}
	}
	// And it still says what it was asked to say, or the redaction is just
	// an empty string.
	for _, want := range []string{"curl -X POST", "api.example.com", "Content-Type: application/json"} {
		if !strings.Contains(out, want) {
			t.Errorf("curl lost %q:\n%s", want, out)
		}
	}
}

// TestMarkdownCopiesNothingSecret — `C` is billed as issue-ready, which is
// exactly what makes a leak here irreversible.
func TestMarkdownCopiesNothingSecret(t *testing.T) {
	out := MarkdownExchange(secretiveRecord())
	for _, secret := range allSecrets {
		if strings.Contains(out, secret) {
			t.Errorf("markdown leaks %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{"### HTTP Exchange #1", "Content-Type: application/json"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lost %q:\n%s", want, out)
		}
	}
}

// TestTheExportersAgreeOnWhatIsSecret — the two exporters share one rule,
// so a header cannot be redacted by one and printed by the other.
func TestTheExportersAgreeOnWhatIsSecret(t *testing.T) {
	r := secretiveRecord()
	curl, md := CurlCommand(r), MarkdownExchange(r)

	for name := range r.RequestHeaders {
		if !isSecretHeaderName(name) {
			continue
		}
		// The name is kept — a reader needs to know the request carried an
		// Authorization header — and the value is not.
		inCurl := strings.Contains(curl, name+": <redacted>")
		inMD := strings.Contains(md, name+": <redacted>")
		if inCurl != inMD {
			t.Errorf("header %q: curl redacts=%v, markdown redacts=%v", name, inCurl, inMD)
		}
		if !inCurl {
			t.Errorf("header %q is on the secret list but was not redacted:\n%s", name, curl)
		}
	}
}

// TestTheTransportAndTheClipboardNameTheSameSecrets — the recorder's rule
// and the exporters' rule are two lists in two packages, and a header added
// to one must not be forgotten in the other.
//
// The recorder's list is unexported, so it is asked through a real
// RoundTrip: a header it considers secret comes back as "<redacted>", and a
// header it does not comes back unchanged. Whatever that answer is, the
// clipboard must agree with it.
func TestTheTransportAndTheClipboardNameTheSameSecrets(t *testing.T) {
	names := []string{
		"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Api-Key",
		"Proxy-Authorization", "Cookie", "Set-Cookie",
		// Not secrets, and must not be redacted into uselessness.
		"Content-Type", "Accept", "User-Agent", "X-Trace-Id",
	}
	store := wirelog.New(4)
	tr := &wirelog.Transport{Store: store, Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}

	req, err := http.NewRequest("GET", "https://example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		req.Header.Set(n, "VALUE-OF-"+n)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	// The record lands when the response body is closed, because the body
	// is what the recording tee is counting: an unread body is an exchange
	// still in flight.
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	recs := store.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d exchanges, want 1", len(recs))
	}
	for _, n := range names {
		got := strings.Join(recs[0].RequestHeaders[n], ", ")
		recorderSaysSecret := got == "" || !strings.Contains(got, "VALUE-OF-"+n)
		if recorderSaysSecret != isSecretHeaderName(n) {
			t.Errorf("header %q: recorder treats it as secret=%v, the clipboard rule says %v",
				n, recorderSaysSecret, isSecretHeaderName(n))
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
