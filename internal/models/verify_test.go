// Tests for the provider verification.
//
// The probe is the answer to "does this endpoint accept this credential?",
// so the tests pin three things: which request is made, what the outcome
// says, and that the credential is the caller's — the panel verifies a
// key the user has typed and has not saved yet.
//
// The probe is the one place in the configuration path that touches the
// network, so the tests use an httptest server rather than a real
// provider.
package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// probeServer answers every path with one status and records the request.
type probeServer struct {
	*httptest.Server
	status int
	body   string
	paths  []string
	auth   string
}

// newProbeServer starts a server that answers with status and body.
func newProbeServer(t *testing.T, status int, body string) *probeServer {
	t.Helper()
	ps := &probeServer{status: status, body: body}
	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.paths = append(ps.paths, r.URL.Path)
		ps.auth = r.Header.Get("Authorization")
		if ps.auth == "" {
			ps.auth = r.Header.Get("x-api-key")
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(ps.status)
		_, _ = w.Write([]byte(ps.body))
	}))
	t.Cleanup(ps.Close)
	return ps
}

func TestVerifyProvider_ChatProbeUsesTheNamedModel(t *testing.T) {
	srv := newProbeServer(t, 200, `{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`)
	p := ProviderConfig{ID: "gw", BaseURL: srv.URL + "/v1"}
	res := VerifyProvider(context.Background(), p, "sk-test", "MiniMax-M3")
	if !res.OK {
		t.Fatalf("OK = false, want true (%s)", res.Detail)
	}
	if len(srv.paths) != 1 || srv.paths[0] != "/v1/chat/completions" {
		t.Fatalf("paths = %v, want one call to /v1/chat/completions", srv.paths)
	}
	if srv.auth != "Bearer sk-test" {
		t.Fatalf("auth = %q, want the bearer token", srv.auth)
	}
}

func TestVerifyProvider_ChatProbeReportsARejectedCredential(t *testing.T) {
	srv := newProbeServer(t, 401, `{"error":"invalid api key"}`)
	p := ProviderConfig{ID: "gw", BaseURL: srv.URL + "/v1"}
	res := VerifyProvider(context.Background(), p, "sk-bad", "MiniMax-M3")
	if res.OK {
		t.Fatal("OK = true, want false")
	}
	if res.Status != 401 {
		t.Fatalf("Status = %d, want 401", res.Status)
	}
	if !strings.Contains(res.Detail, "credential was rejected") {
		t.Fatalf("Detail = %q, want it to name the rejected credential", res.Detail)
	}
	if UpstreamStatus(res.Err) != 401 {
		t.Fatalf("UpstreamStatus = %d, want 401", UpstreamStatus(res.Err))
	}
}

func TestVerifyProvider_ListingProbeWhenThereIsNoModel(t *testing.T) {
	srv := newProbeServer(t, 200, `{"data":[{"id":"a"},{"id":"b"}]}`)
	p := ProviderConfig{ID: "gw", BaseURL: srv.URL + "/v1"}
	res := VerifyProvider(context.Background(), p, "sk-test", "")
	if !res.OK {
		t.Fatalf("OK = false, want true (%s)", res.Detail)
	}
	if len(srv.paths) != 1 || srv.paths[0] != "/v1/models" {
		t.Fatalf("paths = %v, want one call to /v1/models", srv.paths)
	}
	if !strings.Contains(res.Detail, "2 model") {
		t.Fatalf("Detail = %q, want the model count", res.Detail)
	}
}

func TestVerifyProvider_ListingProbeReportsARejectedCredential(t *testing.T) {
	srv := newProbeServer(t, 403, `{"error":"forbidden"}`)
	p := ProviderConfig{ID: "gw", BaseURL: srv.URL + "/v1"}
	res := VerifyProvider(context.Background(), p, "sk-bad", "")
	if res.OK {
		t.Fatal("OK = true, want false")
	}
	if res.Status != 403 {
		t.Fatalf("Status = %d, want 403", res.Status)
	}
	if !strings.Contains(res.Detail, "credential was rejected") {
		t.Fatalf("Detail = %q, want it to name the rejected credential", res.Detail)
	}
}

func TestVerifyProvider_AnthropicListingPath(t *testing.T) {
	srv := newProbeServer(t, 200, `{"data":[]}`)
	p := ProviderConfig{ID: "gw", Type: ProviderTypeAnthropic, BaseURL: srv.URL + "/anthropic"}
	res := VerifyProvider(context.Background(), p, "sk-test", "")
	if !res.OK {
		t.Fatalf("OK = false, want true (%s)", res.Detail)
	}
	if len(srv.paths) != 1 || srv.paths[0] != "/anthropic/v1/models" {
		t.Fatalf("paths = %v, want one call to /anthropic/v1/models", srv.paths)
	}
	if srv.auth != "sk-test" {
		t.Fatalf("auth = %q, want the x-api-key header to carry the key", srv.auth)
	}
}

func TestVerifyProvider_ExpandsAnEnvironmentReference(t *testing.T) {
	t.Setenv("WAGYU_VERIFY_KEY", "sk-from-env")
	srv := newProbeServer(t, 200, `{"data":[]}`)
	p := ProviderConfig{ID: "gw", BaseURL: srv.URL + "/v1"}
	res := VerifyProvider(context.Background(), p, "$WAGYU_VERIFY_KEY", "")
	if !res.OK {
		t.Fatalf("OK = false, want true (%s)", res.Detail)
	}
	if srv.auth != "Bearer sk-from-env" {
		t.Fatalf("auth = %q, want the expanded key", srv.auth)
	}
}

func TestVerifyProvider_UsesTheCallersCredentialNotTheStoredOne(t *testing.T) {
	srv := newProbeServer(t, 200, `{"data":[]}`)
	p := ProviderConfig{ID: "gw", BaseURL: srv.URL + "/v1", APIKey: "sk-stored"}
	res := VerifyProvider(context.Background(), p, "sk-typed", "")
	if !res.OK {
		t.Fatalf("OK = false, want true (%s)", res.Detail)
	}
	if srv.auth != "Bearer sk-typed" {
		t.Fatalf("auth = %q, want the caller's credential", srv.auth)
	}
}

func TestVerifyProvider_UnreachableEndpoint(t *testing.T) {
	p := ProviderConfig{ID: "gw", BaseURL: "http://127.0.0.1:1/v1"}
	res := VerifyProvider(context.Background(), p, "sk-test", "")
	if res.OK {
		t.Fatal("OK = true, want false")
	}
	if !strings.Contains(res.Detail, "could not be reached") {
		t.Fatalf("Detail = %q, want it to say the endpoint is unreachable", res.Detail)
	}
}

func TestVerifyProvider_WithoutAnEndpoint(t *testing.T) {
	res := VerifyProvider(context.Background(), ProviderConfig{ID: "gw"}, "sk-test", "minimax-m3")
	if res.OK {
		t.Fatal("OK = true, want false")
	}
	if !strings.Contains(res.Detail, "no endpoint") {
		t.Fatalf("Detail = %q, want it to name the missing endpoint", res.Detail)
	}
}

func TestVerifyResult_Summary(t *testing.T) {
	ok := VerifyResult{OK: true, Detail: "the model answered"}
	if !strings.HasPrefix(ok.Summary(), "✓") {
		t.Fatalf("Summary = %q, want a ✓", ok.Summary())
	}
	bad := VerifyResult{Detail: "the credential was rejected (HTTP 401)"}
	if !strings.HasPrefix(bad.Summary(), "✗") {
		t.Fatalf("Summary = %q, want a ✗", bad.Summary())
	}
}

func TestListingURL(t *testing.T) {
	cases := []struct {
		name string
		p    ProviderConfig
		want string
	}{
		{
			name: "the openai base carries the version",
			p:    ProviderConfig{BaseURL: "https://host/v1"},
			want: "https://host/v1/models",
		},
		{
			name: "the anthropic base does not",
			p:    ProviderConfig{Type: ProviderTypeAnthropic, BaseURL: "https://host/anthropic"},
			want: "https://host/anthropic/v1/models",
		},
		{
			name: "a verbatim endpoint is recovered",
			p:    ProviderConfig{APIURL: "https://host/v1/chat/completions"},
			want: "https://host/v1/models",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listingURL(tc.p); got != tc.want {
				t.Fatalf("listingURL = %q, want %q", got, tc.want)
			}
		})
	}
}
