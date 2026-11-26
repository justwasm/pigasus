// scope_test.go — covers the AGY 1.5.17 changelog line:
//
//	"Improved MCP server OAuth sign-in in `/mcp` and `/plugin` to request
//	 `offline_access` when the server specifies scopes and its authorization
//	 server advertises `offline_access` …"
//
// No network: every discovery test drives an http.Client whose transport is an
// in-memory round trip (roundTripFunc), so the metadata document is served by
// the test rather than by a socket. The table cases are the bullet's own
// branches — both halves of the condition, the no-scopes case, and the failed
// lookup.

package mcpoauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// roundTripFunc adapts a function into an http.RoundTripper, so a test can
// answer a request without a listener.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// jsonClient returns a client that answers every request with body and status,
// and records how many requests it saw.
func jsonClient(status int, body string, calls *int32) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
}

// TestRequestedScopes is the bullet itself, as a table: the two-part condition,
// plus the branch where the metadata lookup is irrelevant because the server
// asked for nothing.
func TestRequestedScopes(t *testing.T) {
	tests := []struct {
		name       string
		server     []string
		advertised []string
		want       []string
	}{
		{
			name:       "scopes specified, offline_access advertised → appended",
			server:     []string{"read", "write"},
			advertised: []string{"openid", "offline_access"},
			want:       []string{"read", "write", "offline_access"},
		},
		{
			name:       "scopes specified, offline_access not advertised → unchanged",
			server:     []string{"read", "write"},
			advertised: []string{"openid", "email"},
			want:       []string{"read", "write"},
		},
		{
			name:       "scopes specified, nothing advertised → unchanged",
			server:     []string{"read"},
			advertised: nil,
			want:       []string{"read"},
		},
		{
			name:       "no scopes specified, offline_access advertised → nothing added",
			server:     nil,
			advertised: []string{"offline_access"},
			want:       nil,
		},
		{
			name:       "no scopes specified, nothing advertised → nothing added",
			server:     []string{},
			advertised: nil,
			want:       nil,
		},
		{
			name:       "empty-string scopes count as none",
			server:     []string{"", "   "},
			advertised: []string{"offline_access"},
			want:       nil,
		},
		{
			name:       "offline_access already requested is not duplicated",
			server:     []string{"read", "offline_access"},
			advertised: []string{"offline_access"},
			want:       []string{"read", "offline_access"},
		},
		{
			name:       "one space-separated string is split",
			server:     []string{"read write"},
			advertised: []string{"offline_access"},
			want:       []string{"read", "write", "offline_access"},
		},
		{
			name:       "duplicate server scopes collapse",
			server:     []string{"read", "read", "write"},
			advertised: []string{"offline_access"},
			want:       []string{"read", "write", "offline_access"},
		},
		{
			name:       "advertised scopes are matched exactly, not by prefix",
			server:     []string{"read"},
			advertised: []string{"offline_access_v2", "offline"},
			want:       []string{"read"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RequestedScopes(tc.server, tc.advertised)
			if len(got) != len(tc.want) {
				t.Fatalf("RequestedScopes(%q, %q) = %q, want %q", tc.server, tc.advertised, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("RequestedScopes(%q, %q) = %q, want %q", tc.server, tc.advertised, got, tc.want)
				}
			}
		})
	}
}

// TestRequestedScopes_DoesNotMutateInputs pins that the decision copies: a
// caller that reuses its scope slice (the server's configuration, loaded once)
// must not see offline_access leak into it.
func TestRequestedScopes_DoesNotMutateInputs(t *testing.T) {
	server := []string{"read"}
	advertised := []string{"offline_access"}
	got := RequestedScopes(server, advertised)
	_ = got

	if len(server) != 1 || server[0] != "read" {
		t.Fatalf("server scopes mutated: %q", server)
	}
	if len(advertised) != 1 || advertised[0] != "offline_access" {
		t.Fatalf("advertised scopes mutated: %q", advertised)
	}
}

// TestRequestedScopes_NoServerScopesIgnoresDiscovery is the second half of the
// same guarantee at the cache level: with no server scopes the metadata
// document is not even fetched, so a broken authorization server cannot break
// a sign-in that asks for nothing.
func TestRequestedScopes_NoServerScopesIgnoresDiscovery(t *testing.T) {
	var calls int32
	cache := NewMetadataCache(func(ctx context.Context, issuer string) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		return []string{"offline_access"}, nil
	})
	got := cache.RequestedScopesFor(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com", nil)
	if got != nil {
		t.Fatalf("RequestedScopesFor with no server scopes = %q, want nil", got)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("discovery ran %d times for a server with no scopes, want 0", n)
	}
}

// TestDiscoverAuthServerScopes covers the metadata document read: the advertised
// list is returned, an absent scopes_supported advertises nothing, and a bad
// document or status is an error the caller can treat as "not advertised".
func TestDiscoverAuthServerScopes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    []string
		wantErr bool
	}{
		{
			name:   "scopes_supported read",
			status: http.StatusOK,
			body:   `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/authorize","scopes_supported":["openid","offline_access"]}`,
			want:   []string{"openid", "offline_access"},
		},
		{
			name:   "scopes_supported absent advertises nothing",
			status: http.StatusOK,
			body:   `{"issuer":"https://auth.example.com","token_endpoint":"https://auth.example.com/token"}`,
			want:   nil,
		},
		{
			name:    "404 from the authorization server",
			status:  http.StatusNotFound,
			body:    `not found`,
			wantErr: true,
		},
		{
			name:    "malformed document",
			status:  http.StatusOK,
			body:    `{"scopes_supported":`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := jsonClient(tc.status, tc.body, nil)
			got, err := DiscoverAuthServerScopes(context.Background(), client, "https://auth.example.com")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DiscoverAuthServerScopes err = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DiscoverAuthServerScopes: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("scopes = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("scopes = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestDiscoverAuthServerScopes_RequestsWellKnownURL pins the URL the discovery
// actually fetches — RFC 8414's placement of the well-known segment, including
// the issuer-path form.
func TestDiscoverAuthServerScopes_RequestsWellKnownURL(t *testing.T) {
	tests := []struct {
		issuer string
		want   string
	}{
		{"https://auth.example.com", "https://auth.example.com/.well-known/oauth-authorization-server"},
		{"https://auth.example.com/", "https://auth.example.com/.well-known/oauth-authorization-server"},
		{"https://example.com/tenant1", "https://example.com/.well-known/oauth-authorization-server/tenant1"},
		{"auth.example.com", "https://auth.example.com/.well-known/oauth-authorization-server"},
	}
	for _, tc := range tests {
		var seen string
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			seen = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Request:    req,
			}, nil
		})}
		if _, err := DiscoverAuthServerScopes(context.Background(), client, tc.issuer); err != nil {
			t.Fatalf("%s: %v", tc.issuer, err)
		}
		if seen != tc.want {
			t.Errorf("issuer %q fetched %q, want %q", tc.issuer, seen, tc.want)
		}
	}

	if _, err := WellKnownAuthServerMetadataURL(""); err == nil {
		t.Errorf("WellKnownAuthServerMetadataURL(\"\") err = nil, want error")
	}
	if _, err := WellKnownAuthServerMetadataURL("::not a url::"); err == nil {
		t.Errorf("WellKnownAuthServerMetadataURL(invalid) err = nil, want error")
	}
}

// TestMetadataCache_RequestedScopesForAppendsWhenAdvertised is the changelog
// branch end to end: a server that specifies scopes and an authorization server
// whose document advertises offline_access.
func TestMetadataCache_RequestedScopesForAppendsWhenAdvertised(t *testing.T) {
	var calls int32
	client := jsonClient(http.StatusOK,
		`{"issuer":"https://auth.example.com","scopes_supported":["openid","offline_access"]}`, &calls)
	cache := NewMetadataCache(func(ctx context.Context, issuer string) ([]string, error) {
		return DiscoverAuthServerScopes(ctx, client, issuer)
	})

	got := cache.RequestedScopesFor(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com", []string{"read", "write"})
	want := []string{"read", "write", "offline_access"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("RequestedScopesFor = %q, want %q", got, want)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("metadata fetched %d times, want 1", n)
	}
}

// TestMetadataCache_RequestedScopesForNotAdvertised is the other branch: the
// document is served but does not list offline_access, so nothing is appended.
func TestMetadataCache_RequestedScopesForNotAdvertised(t *testing.T) {
	client := jsonClient(http.StatusOK, `{"issuer":"https://auth.example.com","scopes_supported":["openid","email"]}`, nil)
	cache := NewMetadataCache(func(ctx context.Context, issuer string) ([]string, error) {
		return DiscoverAuthServerScopes(ctx, client, issuer)
	})

	got := cache.RequestedScopesFor(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com", []string{"read"})
	if strings.Join(got, " ") != "read" {
		t.Fatalf("RequestedScopesFor = %q, want %q", got, "read")
	}
}

// TestMetadataCache_DiscoveryFailureIsNotAdvertised is the failure branch: the
// metadata fetch fails, so the rule behaves as "not advertised" and the
// sign-in's own scope list survives untouched.
func TestMetadataCache_DiscoveryFailureIsNotAdvertised(t *testing.T) {
	failures := []struct {
		name  string
		fetch ScopeFetcher
	}{
		{
			name: "transport error",
			fetch: func(ctx context.Context, issuer string) ([]string, error) {
				return nil, errors.New("dial tcp: no such host")
			},
		},
		{
			name: "authorization server answers 500",
			fetch: func(ctx context.Context, issuer string) ([]string, error) {
				client := jsonClient(http.StatusInternalServerError, `boom`, nil)
				return DiscoverAuthServerScopes(ctx, client, issuer)
			},
		},
		{
			name: "malformed document",
			fetch: func(ctx context.Context, issuer string) ([]string, error) {
				client := jsonClient(http.StatusOK, `not json`, nil)
				return DiscoverAuthServerScopes(ctx, client, issuer)
			},
		},
		{
			name:  "no fetcher at all",
			fetch: nil,
		},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewMetadataCache(tc.fetch)
			got := cache.RequestedScopesFor(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com", []string{"read", "write"})
			if strings.Join(got, " ") != "read write" {
				t.Fatalf("RequestedScopesFor after a failed lookup = %q, want %q", got, "read write")
			}
		})
	}
}

// TestMetadataCache_FailureIsCachedWithinTTL pins that a broken authorization
// server costs one attempt per TTL, not one per sign-in attempt — and that the
// error is still reported out of band to a caller that logs.
func TestMetadataCache_FailureIsCachedWithinTTL(t *testing.T) {
	var calls int32
	now := time.Now()
	cache := newMetadataCache(func() time.Time { return now }, time.Minute,
		func(ctx context.Context, issuer string) ([]string, error) {
			atomic.AddInt32(&calls, 1)
			return nil, errors.New("boom")
		})

	for i := 0; i < 3; i++ {
		got := cache.RequestedScopesFor(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com", []string{"read"})
		if strings.Join(got, " ") != "read" {
			t.Fatalf("attempt %d: scopes = %q, want %q", i, got, "read")
		}
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("failed discovery ran %d times inside the TTL, want 1", n)
	}

	_, err := cache.AdvertisedScopes(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com")
	if err == nil {
		t.Fatalf("AdvertisedScopes err = nil, want the cached failure")
	}

	// Past the TTL the lookup is retried.
	now = now.Add(2 * time.Minute)
	_, _ = cache.AdvertisedScopes(context.Background(), "https://mcp.example.com/mcp", "https://auth.example.com")
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("discovery ran %d times after the TTL, want 2", n)
	}
}

// TestMetadataCache_CachesPerServer pins the per-server cache: two servers get
// their own entry, and a cosmetic URL variant shares one.
func TestMetadataCache_CachesPerServer(t *testing.T) {
	var calls int32
	cache := NewMetadataCache(func(ctx context.Context, issuer string) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		return []string{"offline_access"}, nil
	})
	ctx := context.Background()

	a := cache.RequestedScopesFor(ctx, "https://a.example.com/mcp", "https://auth-a.example.com", []string{"read"})
	b := cache.RequestedScopesFor(ctx, "https://b.example.com/mcp", "https://auth-b.example.com", []string{"read"})
	for _, got := range [][]string{a, b} {
		if strings.Join(got, " ") != "read offline_access" {
			t.Fatalf("scopes = %q, want %q", got, "read offline_access")
		}
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("discovery ran %d times for two servers, want 2", n)
	}

	// Same server, cosmetic URL difference: served from cache.
	_ = cache.RequestedScopesFor(ctx, "HTTPS://A.Example.com/mcp?x=1#frag", "https://auth-a.example.com", []string{"read"})
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("discovery ran %d times after a URL variant, want 2", n)
	}
	if cache.Len() != 2 {
		t.Fatalf("cache length = %d, want 2", cache.Len())
	}

	// Invalidate drops one server and forces a re-read for it only.
	cache.Invalidate("https://a.example.com/mcp")
	if cache.Len() != 1 {
		t.Fatalf("cache length after Invalidate = %d, want 1", cache.Len())
	}
	_ = cache.RequestedScopesFor(ctx, "https://a.example.com/mcp", "https://auth-a.example.com", []string{"read"})
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("discovery ran %d times after Invalidate, want 3", n)
	}
}

// TestMetadataCache_InvalidServerURL pins the error path for a URL that cannot
// be a cache key.
func TestMetadataCache_InvalidServerURL(t *testing.T) {
	cache := NewMetadataCache(func(ctx context.Context, issuer string) ([]string, error) {
		return []string{"offline_access"}, nil
	})
	scopes, err := cache.AdvertisedScopes(context.Background(), "::not a url::", "https://auth.example.com")
	if !errors.Is(err, ErrInvalidServerURL) {
		t.Fatalf("AdvertisedScopes err = %v, want ErrInvalidServerURL", err)
	}
	if scopes != nil {
		t.Fatalf("AdvertisedScopes = %q, want nil", scopes)
	}
	// And the rule still answers with the server's own scopes.
	got := cache.RequestedScopesFor(context.Background(), "::not a url::", "https://auth.example.com", []string{"read"})
	if strings.Join(got, " ") != "read" {
		t.Fatalf("RequestedScopesFor on a bad server URL = %q, want %q", got, "read")
	}
}

// TestScopeString pins the wire form of a scope set: space-separated per
// RFC 6749 §3.3, and empty for an empty set (so a caller can omit the
// parameter rather than send `scope=`).
func TestScopeString(t *testing.T) {
	if got := ScopeString([]string{"read", "write", "offline_access"}); got != "read write offline_access" {
		t.Errorf("ScopeString = %q", got)
	}
	if got := ScopeString(nil); got != "" {
		t.Errorf("ScopeString(nil) = %q, want empty", got)
	}
}
