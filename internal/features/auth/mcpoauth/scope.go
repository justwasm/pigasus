// scope.go — the AGY 1.5.17 changelog line this package owns:
//
//	"Improved MCP server OAuth sign-in in `/mcp` and `/plugin` to request
//	 `offline_access` when the server specifies scopes and its authorization
//	 server advertises `offline_access`, so remote MCP servers such as
//	 Vercel's issue refresh tokens instead of disconnecting when the initial
//	 access token expires."
//
// What the bullet is precise about
// --------------------------------
// The rule is a *two-part conjunction*, and both halves matter:
//
//  1. The MCP server **specifies scopes.** With no scope list there is
//     nothing to append to, and inventing a one-element `scope=offline_access`
//     request would be a different (and wrong) request: the server asked for
//     no scope, so the authorization server's default grant is what it wants.
//
//  2. The authorization server **advertises `offline_access`** in its
//     metadata (`scopes_supported` of
//     /.well-known/oauth-authorization-server, RFC 8414). A server that does
//     not advertise it may reject the whole request with `invalid_scope`, so
//     asking unconditionally trades "token expires" for "sign-in fails".
//
// Only when both hold is `offline_access` appended. The third state — a
// metadata fetch that fails — is deliberately treated as "not advertised":
// the sign-in must still work, it just may not get a refresh token.
//
// Where this lives relative to the rest of the flow
// -------------------------------------------------
// The clone has no MCP OAuth *authorization URL builder*. `mcp/client.go`
// documents that the transport's `OAuthHandler` slot is unfilled, and the
// language-server `StartMcpOAuthSignIn` handler answers with an empty `authUrl`
// (internal/remotecontrol/plugins_marketplace.go) — so there is no place today
// that composes `scope=` for a remote MCP server. The greps behind that claim:
//
//	grep -rn 'offline_access' --include='*.go' internal/     → no hits (before this file)
//	grep -rn 'AuthCodeURL\|AuthorizeURL\|AuthorizationURL'   → only Google sign-in
//	                                                          (internal/features/auth/oauth.go,
//	                                                          used by /login and /accounts)
//	grep -rn 'resource_metadata\|oauth-authorization-server'  → no hits in Go
//	grep -rn 'OAuthHandler'                                  → only comments saying it is unfilled
//
// This package is nonetheless the clone's owner of the MCP OAuth flow *state*:
// the PKCE verifier cache in pkce.go and the saved grant in signin.go. The rule
// therefore lives here, at the boundary the missing builder would call:
//
//	scopes := cache.RequestedScopesFor(ctx, serverURL, issuer, server.Scopes)
//	// …the builder puts ScopeString(scopes) in the request's `scope` parameter…
//
// RequestedScopes is the pure decision, MetadataCache adds the per-server
// cached discovery the decision needs. Neither is reachable from a live sign-in
// yet — there is no builder to reach them from — and that gap is reported
// rather than papered over with a fake URL path.
//
// Unexercised against a live server: the reference account is suspended, so no
// real remote MCP server (Vercel's or otherwise) could be signed into. The
// discovery path is covered by an in-process HTTP transport, not by a socket.
package mcpoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OfflineAccessScope is the OIDC scope that asks an authorization server for a
// refresh token. It is the scope this package appends, and the only one it
// ever adds on its own.
const OfflineAccessScope = "offline_access"

// DefaultMetadataTTL is how long a discovered authorization-server scope list
// is trusted. Ten minutes is short enough that a server that starts (or stops)
// advertising `offline_access` is picked up within a session, and long enough
// that a retry loop does not re-fetch the document per attempt.
const DefaultMetadataTTL = 10 * time.Minute

// maxMetadataBytes bounds the metadata document read, so a hostile or broken
// authorization server cannot make discovery allocate without limit.
const maxMetadataBytes = 1 << 20

// WellKnownAuthServerMetadataURL returns the RFC 8414 metadata URL for issuer.
//
// RFC 8414 §3.1 inserts `/.well-known/oauth-authorization-server` between the
// host and the issuer's path, so an issuer with a path keeps it:
//
//	https://auth.example.com          → https://auth.example.com/.well-known/oauth-authorization-server
//	https://example.com/tenant1       → https://example.com/.well-known/oauth-authorization-server/tenant1
//
// A relative or host-less issuer is an error: there is nowhere to fetch from.
func WellKnownAuthServerMetadataURL(issuer string) (string, error) {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return "", errors.New("mcpoauth: empty authorization server URL")
	}
	if !strings.Contains(issuer, "://") {
		issuer = "https://" + issuer
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("mcpoauth: invalid authorization server URL %q", issuer)
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = "/.well-known/oauth-authorization-server" + strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}

// metadataDocument is the subset of the RFC 8414 document this package reads.
// The other members (token_endpoint, registration_endpoint, …) belong to a
// request builder; the scope decision needs only `scopes_supported`.
type metadataDocument struct {
	Issuer          string   `json:"issuer"`
	ScopesSupported []string `json:"scopes_supported"`
}

// DiscoverAuthServerScopes fetches issuer's authorization-server metadata and
// returns its `scopes_supported` list.
//
// A nil client means http.DefaultClient. A non-2xx response, a malformed
// document or a transport error is returned as an error — the caller decides
// what a failed lookup means, and for the scope rule it means "not
// advertised".
//
// An absent `scopes_supported` is *not* an error: the member is only RECOMMENDED
// by RFC 8414, and a document without it simply advertises nothing.
func DiscoverAuthServerScopes(ctx context.Context, client *http.Client, issuer string) ([]string, error) {
	endpoint, err := WellKnownAuthServerMetadataURL(issuer)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: metadata fetch %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes))
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: metadata read %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode, URL: endpoint, Body: body}
	}
	var doc metadataDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("mcpoauth: metadata parse %s: %w", endpoint, err)
	}
	return NormalizeScopes(doc.ScopesSupported), nil
}

// NormalizeScopes cleans a scope list: every entry is trimmed, split on
// whitespace (a server may hand over one space-separated string), emptied
// entries are dropped, and duplicates are removed in first-seen order.
//
// The result is always a fresh slice, so a caller can append to it without
// mutating the input.
func NormalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, entry := range scopes {
		for _, scope := range strings.Fields(entry) {
			if _, dup := seen[scope]; dup {
				continue
			}
			seen[scope] = struct{}{}
			out = append(out, scope)
		}
	}
	return out
}

// ScopeString joins scopes the way an authorization request carries them:
// space-separated, per RFC 6749 §3.3.
func ScopeString(scopes []string) string {
	return strings.Join(scopes, " ")
}

// HasScope reports whether scopes contains scope.
func HasScope(scopes []string, scope string) bool {
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// RequestedScopes applies the 1.5.17 rule to one sign-in:
//
//	serverScopes: the scope list the MCP server specifies (may be empty),
//	advertised:   the scopes the authorization server advertises (may be nil
//	              or empty, and nil is what a failed discovery yields).
//
// It returns the scopes to put in the authorization request:
//
//   - no server scopes → nothing is added, whatever is advertised. The result
//     is empty: the server asked for no scope.
//   - server scopes, `offline_access` advertised → the server's scopes with
//     `offline_access` appended (once — an already-requested one is not
//     duplicated, and that also covers a server that asks for it itself).
//   - server scopes, `offline_access` not advertised → the server's scopes,
//     unchanged.
//
// The input slices are never mutated: the result is always a fresh slice. Both
// sides are normalized first, so "read  write" and `["read","write"]` agree.
func RequestedScopes(serverScopes, advertised []string) []string {
	requested := NormalizeScopes(serverScopes)
	if len(requested) == 0 {
		return nil
	}
	if !HasScope(NormalizeScopes(advertised), OfflineAccessScope) {
		return requested
	}
	if HasScope(requested, OfflineAccessScope) {
		return requested
	}
	return append(requested, OfflineAccessScope)
}

// ScopeFetcher returns the scopes an authorization server advertises. It is the
// discovery seam: production passes DiscoverAuthServerScopes bound to a client,
// tests pass a table.
type ScopeFetcher func(ctx context.Context, issuer string) ([]string, error)

// metadataEntry is one server's cached discovery result. scopes is nil for a
// negative result (the document was read and advertised no offline_access, or
// there was no document at all); err records a failed lookup so a retry can
// report it without re-fetching inside the TTL.
type metadataEntry struct {
	scopes    []string
	err       error
	fetchedAt time.Time
}

// MetadataCache discovers and caches the authorization-server scopes of the MCP
// servers in play, one entry per sanitized server URL.
//
// It exists so the scope rule costs at most one HTTP round-trip per server per
// TTL: `/plugin` and `/mcp` both re-run a sign-in on retry, and a reconnect
// storm must not turn into a metadata fetch per attempt.
//
// The cache never fails a sign-in. Every lookup degrades to "not advertised",
// which is exactly the branch that requests the server's own scopes.
type MetadataCache struct {
	mu      sync.Mutex
	entries map[string]*metadataEntry
	now     func() time.Time
	ttl     time.Duration
	// fetch is the discovery seam. nil disables discovery entirely: every
	// lookup answers "nothing advertised".
	fetch ScopeFetcher
}

// NewMetadataCache returns a cache that discovers through fetch with the
// default TTL.
//
// The issuer is resolved per server by the caller (see RequestedScopesFor):
// this package does not own the protected-resource-metadata hop that names the
// authorization server, because the clone has no protected-resource flow yet.
func NewMetadataCache(fetch ScopeFetcher) *MetadataCache {
	return newMetadataCache(time.Now, DefaultMetadataTTL, fetch)
}

// newMetadataCache is the test seam: it pins the clock, the TTL and the fetcher.
func newMetadataCache(now func() time.Time, ttl time.Duration, fetch ScopeFetcher) *MetadataCache {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = DefaultMetadataTTL
	}
	return &MetadataCache{
		entries: make(map[string]*metadataEntry),
		now:     now,
		ttl:     ttl,
		fetch:   fetch,
	}
}

// AdvertisedScopes returns the cached-or-discovered scope list of the
// authorization server at issuer for the MCP server at serverURL.
//
// A failed discovery is reported as (nil, err) the first time and then served
// from the cache as (nil, same err) until the entry expires: the caller that
// only needs the decision ignores both, and a caller that logs gets one network
// attempt per TTL rather than one per sign-in.
func (c *MetadataCache) AdvertisedScopes(ctx context.Context, serverURL, issuer string) ([]string, error) {
	if c == nil || c.fetch == nil {
		return nil, nil
	}
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return nil, ErrInvalidServerURL
	}

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && c.now().Sub(entry.fetchedAt) < c.ttl {
		scopes, err := cloneScopes(entry.scopes), entry.err
		c.mu.Unlock()
		return scopes, err
	}
	c.mu.Unlock()

	scopes, err := c.fetch(ctx, issuer)
	scopes = NormalizeScopes(scopes)

	c.mu.Lock()
	c.entries[key] = &metadataEntry{scopes: scopes, err: err, fetchedAt: c.now()}
	c.mu.Unlock()
	return cloneScopes(scopes), err
}

// RequestedScopesFor applies the 1.5.17 rule for one server: it looks the
// authorization server's scopes up (cached per server) and appends
// `offline_access` only when the server specifies scopes *and* the authorization
// server advertises the scope.
//
// A discovery failure is treated as "not advertised" — the returned slice is the
// server's own scopes and sign-in proceeds. This function therefore never fails:
// it has no error return, because there is no failure for a caller to handle.
func (c *MetadataCache) RequestedScopesFor(ctx context.Context, serverURL, issuer string, serverScopes []string) []string {
	// The short-circuit is the first half of the condition, and it also keeps
	// the cache from spending a request on a server that asked for nothing.
	if len(NormalizeScopes(serverScopes)) == 0 {
		return nil
	}
	advertised, err := c.AdvertisedScopes(ctx, serverURL, issuer)
	if err != nil {
		// Discovery failed: behave as "not advertised" (the sign-in must not
		// break just because the metadata document was unreachable).
		advertised = nil
	}
	return RequestedScopes(serverScopes, advertised)
}

// Invalidate drops the cached discovery for one server, so the next sign-in
// re-reads the metadata. `/mcp auth` uses it after a server's configuration
// changes (a new issuer usually means new scopes).
func (c *MetadataCache) Invalidate(serverURL string) {
	if c == nil {
		return
	}
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Len reports how many servers have a cached entry, expired or not.
func (c *MetadataCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// cloneScopes copies a scope slice so a caller cannot mutate the cache.
func cloneScopes(scopes []string) []string {
	if scopes == nil {
		return nil
	}
	out := make([]string, len(scopes))
	copy(out, scopes)
	return out
}
