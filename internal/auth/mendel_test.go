package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseMendelFlags decodes a ListExperiments answer.
//
// 【一手】 the wire form, captured from
// POST https://cloudcode-pa.googleapis.com/v1internal:listExperiments
// on 2026-10-26: an int64 is a JSON string, a float is a number, and
// a list is a message with a `values` field.
//
// A list is kept as the JSON text of the array, because that is what
// the reference's answer carries. 【一手】
func TestParseMendelFlags(t *testing.T) {
	body := []byte(`{
	  "experimentIds": [106260793, 106597020],
	  "flags": [
	    {"name": "remote-control-setting-enabled", "boolValue": true},
	    {"name": "remote-control-webrtc-mesh", "boolValue": false},
	    {"name": "remote-control-version", "stringValue": "v2"},
	    {"name": "remote-control-silence-timeout-seconds", "intValue": "60"},
	    {"name": "DuetAiLocalRag__cache_file_limit", "floatValue": 4194300},
	    {"name": "DuetAiLocalRag__substrings_to_identify_doc_prompts",
	     "stringListValue": {"values": ["document", "comment"]}},
	    {"name": "DuetAiRemoteRag__multi_query_tail_ns_for_generation", "int32ListValue": {}},
	    {"name": "with-an-id", "flagId": 123, "boolValue": true}
	  ]
	}`)

	flags, err := ParseMendelFlags(body)
	if err != nil {
		t.Fatalf("ParseMendelFlags: %v", err)
	}
	if len(flags) != 8 {
		t.Fatalf("the answer carries %d flags, want 8", len(flags))
	}

	if flags[0].Name != "remote-control-setting-enabled" || flags[0].Disabled {
		t.Errorf("the first flag is %+v, want the enabled setting flag", flags[0])
	}
	if !flags[1].Disabled {
		t.Error("the mesh flag is not disabled, want it disabled")
	}
	if flags[2].Value != "v2" || !flags[2].HasValue {
		t.Errorf("the version flag is %+v, want the value v2", flags[2])
	}
	if flags[3].Value != "60" || !flags[3].HasValue {
		t.Errorf("the timeout flag is %+v, want the value 60", flags[3])
	}
	if flags[4].Value != "4194300" {
		t.Errorf("the float flag carries %q, want %q", flags[4].Value, "4194300")
	}
	if !flags[5].HasList || flags[5].ListJSON != `["document","comment"]` {
		t.Errorf("the string list flag is %+v, want the JSON array of its values", flags[5])
	}
	if !flags[6].HasList || flags[6].ListJSON != "null" {
		t.Errorf("the empty list flag is %+v, want the null list", flags[6])
	}
	if flags[7].Name != "with-an-id" || flags[7].Disabled {
		t.Errorf("the last flag is %+v, want the enabled flag with an id", flags[7])
	}
}

// TestFetchMendelFlagsWithToken calls the backend through a stub.
func TestFetchMendelFlagsWithToken(t *testing.T) {
	var gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"flags":[{"name":"a","boolValue":true}]}`))
	}))
	defer srv.Close()

	old := mendelHostsOverride
	mendelHostsOverride = []string{srv.URL}
	t.Cleanup(func() { mendelHostsOverride = old })

	flags, err := FetchMendelFlagsWithToken(context.Background(), "tok")
	if err != nil {
		t.Fatalf("FetchMendelFlagsWithToken: %v", err)
	}
	if len(flags) != 1 || flags[0].Name != "a" {
		t.Fatalf("the flags are %+v, want one flag named a", flags)
	}
	if gotPath != "/v1internal:listExperiments" {
		t.Errorf("the path is %q, want %q", gotPath, "/v1internal:listExperiments")
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("the authorization is %q, want %q", gotAuth, "Bearer tok")
	}
	// The project matters: with it the remote-control flags come back
	// enabled. 【一手】
	if gotBody != `{"project":"aicode-consumers"}` {
		t.Errorf("the body is %q, want the project body", gotBody)
	}
}

// TestFetchMendelFlagsFailsWithoutAToken pins the guard.
func TestFetchMendelFlagsFailsWithoutAToken(t *testing.T) {
	if _, err := FetchMendelFlagsWithToken(context.Background(), "  "); err == nil {
		t.Fatal("the call succeeded without a token, want an error")
	}
}

// TestMendelHubUserAgent pins the hub identity's shape.
//
// 【一手】 2026-10-27, the reference's own language server sends
// `antigravity/hub/2.26.0 (aidev_client; os_type=darwin; arch=arm64;
// cl=1005016104)`, and the backend answers that identity the hub's
// flag set — the one with the desktop app's features on.
func TestMendelHubUserAgent(t *testing.T) {
	got := MendelHubUserAgent("1.4.7")
	if !strings.HasPrefix(got, "antigravity/hub/1.4.7 ") {
		t.Errorf("the identity is %q, want the hub product with the version", got)
	}
	if !strings.Contains(got, "aidev_client") {
		t.Errorf("the identity is %q, want the aidev_client marker", got)
	}
	if MendelHubUserAgent("") == "" {
		t.Errorf("an empty version gave an empty identity")
	}
	if MendelUserAgentCLI == got {
		t.Errorf("the hub identity is the CLI's own identity")
	}
}
