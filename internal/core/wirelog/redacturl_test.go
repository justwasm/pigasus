// redacturl_test.go — a URL says which of its parts is a credential.
//
// The text sanitiser clilog.Sanitize is a set of patterns over a log line,
// and it redacts a query parameter only when the value happens to look like
// a key it knows. A URL is a structure: the parameter's *name* is the claim.
// These tests pin that the structure is what decides, so a short or
// unfamiliar-looking token cannot survive by being unrecognisable.
package wirelog

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestRedactURLHidesCredentialParameters — every spelling the model
// endpoints use, whatever the value looks like.
func TestRedactURLHidesCredentialParameters(t *testing.T) {
	cases := []struct{ raw, secret string }{
		{"https://h/p?key=short", "short"},
		{"https://h/p?key=QUERY-SECRET", "QUERY-SECRET"},
		{"https://h/p?api_key=abc123", "abc123"},
		{"https://h/p?apikey=abc123", "abc123"},
		{"https://h/p?access_token=xyz", "xyz"},
		{"https://h/p?refresh_token=r3fr3sh", "r3fr3sh"},
		{"https://h/p?id_token=1dt0k3n", "1dt0k3n"},
		{"https://h/p?sig=abcdef", "abcdef"},
		{"https://h/p?alt=json&key=another-secret", "another-secret"},
		{"https://h/p?Key=MiXeDcAsE", "MiXeDcAsE"},
	}
	for _, c := range cases {
		got := RedactURL(c.raw)
		if strings.Contains(got, c.secret) {
			t.Errorf("RedactURL(%q) leaked %q: %s", c.raw, c.secret, got)
		}
	}
}

// TestRedactURLKeepsTheRestOfTheQuery — redaction must not cost the reader
// the endpoint. A URL reduced to "?key=<redacted>" cannot be reproduced, and
// reproducing it is what the curl command is for.
func TestRedactURLKeepsTheRestOfTheQuery(t *testing.T) {
	got := RedactURL("https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse&key=SECRET")
	for _, want := range []string{"daily-cloudcode-pa.googleapis.com", "/v1internal:streamGenerateContent", "alt=sse"} {
		if !strings.Contains(got, want) {
			t.Errorf("RedactURL dropped %q: %s", want, got)
		}
	}
	if !strings.Contains(got, "key=<redacted>") {
		t.Errorf("the key parameter is not shown as redacted: %s", got)
	}
}

// TestRedactURLDropsUserinfo — a password in a URL is a credential by
// position, not by name.
func TestRedactURLDropsUserinfo(t *testing.T) {
	got := RedactURL("https://user:PASSWORD@example.com/path")
	if strings.Contains(got, "PASSWORD") || strings.Contains(got, "user:") {
		t.Errorf("RedactURL kept userinfo: %s", got)
	}
	if !strings.Contains(got, "example.com/path") {
		t.Errorf("RedactURL dropped the host or path: %s", got)
	}
}

// TestRedactURLIsIdempotent — the exporters may sanitise a record the
// transport already sanitised, so the second pass must not mangle the first
// pass's output.
func TestRedactURLIsIdempotent(t *testing.T) {
	once := RedactURL("https://h/p?alt=sse&key=SECRET&x=1")
	twice := RedactURL(once)
	if once != twice {
		t.Errorf("not idempotent:\n once: %s\ntwice: %s", once, twice)
	}
	// The placeholder must itself be stable through ParseQuery/Encode.
	if !strings.Contains(twice, "key=<redacted>") && !strings.Contains(twice, "key=%3Credacted%3E") {
		t.Errorf("the redaction marker was lost on the second pass: %s", twice)
	}
}

// TestRedactURLSurvivesGarbage — a string that is not a URL is still
// redacted as text rather than returned as-is.
func TestRedactURLSurvivesGarbage(t *testing.T) {
	got := RedactURL("not a url with Bearer ya29.SOMETOKENVALUE12345")
	if strings.Contains(got, "ya29.SOMETOKENVALUE12345") {
		t.Errorf("a non-URL leaked its token: %s", got)
	}
	if RedactURL("") != "" {
		t.Error("empty input did not round-trip")
	}
}

// TestTheRecorderKeepsNoSecretInTheURL — the end-to-end claim: a real
// exchange through the transport has no credential left in its URL.
func TestTheRecorderKeepsNoSecretInTheURL(t *testing.T) {
	store := New(4)
	tr := &Transport{Store: store, Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})}

	req, err := http.NewRequest("POST",
		"https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse&key=THE-REAL-KEY", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer ya29.THE-REAL-TOKEN")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	recs := store.Records()
	if len(recs) != 1 {
		t.Fatalf("recorded %d exchanges, want 1", len(recs))
	}
	if strings.Contains(recs[0].URL, "THE-REAL-KEY") {
		t.Errorf("the record kept the API key in its URL: %s", recs[0].URL)
	}
	if got := strings.Join(recs[0].RequestHeaders["Authorization"], ", "); strings.Contains(got, "THE-REAL-TOKEN") {
		t.Errorf("the record kept the bearer in its headers: %s", got)
	}
	// The endpoint survives, or the record is useless for diagnosis.
	if !strings.Contains(recs[0].URL, "streamGenerateContent") {
		t.Errorf("the record lost the endpoint: %s", recs[0].URL)
	}
}
