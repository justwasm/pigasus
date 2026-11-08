// pkce.go — PKCE verifier persistence for remote MCP OAuth.
//
// A PKCE flow spans two requests: the browser is sent to the
// authorization endpoint with S256(verifier) as `code_challenge`, and the
// CLI later exchanges the returned `code` at the token endpoint with the
// original `verifier`. A transient network failure between those two
// steps must not create a new verifier — the authorization server still
// holds the old challenge, so the exchange would fail with
// `invalid_grant` and the user would be signed out for no reason (the
// same failure class AGY 1.3.4 fixed, see policy.go).
//
// VerifierCache keeps one pending flow per sanitized MCP server URL, so a
// retry of the same sign-in reuses the verifier that matches the
// outstanding challenge. Flows expire after DefaultPKCEFlowTTL; an
// expired flow is discarded and the next Begin starts a fresh one.
//
// The cache is in-memory and process-local on purpose. Its lifetime is
// one sign-in attempt; nothing here is written to disk (the saved grant
// is signin.go's job), and the verifier is a secret that should not
// outlive the exchange.
package mcpoauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultPKCEFlowTTL is how long a pending flow stays usable. Ten
// minutes matches the typical authorization-code lifetime and gives the
// user time to finish a browser sign-in and a retry to land.
const DefaultPKCEFlowTTL = 10 * time.Minute

// ErrInvalidServerURL is returned when a server URL cannot be sanitized
// into a cache key (an unparseable URL or one without a host).
var ErrInvalidServerURL = errors.New("mcpoauth: invalid MCP server URL")

// randRead is crypto/rand.Read, injectable for tests.
var randRead = rand.Read

// PendingFlow is one in-flight PKCE authorization: the verifier the CLI
// keeps, the challenge the browser saw, and the state value that ties
// the redirect back to this request.
type PendingFlow struct {
	// ServerURL is the sanitized cache key (see SanitizeServerURL).
	ServerURL string
	// Verifier is the RFC 7636 code_verifier (43 base64url characters
	// from 32 random bytes).
	Verifier string
	// Challenge is BASE64URL(SHA256(Verifier)), the code_challenge
	// sent to the authorization endpoint with method S256.
	Challenge string
	// State is the opaque anti-CSRF value sent alongside the challenge.
	State string
	// CreatedAt is when the flow was generated.
	CreatedAt time.Time
	// ExpiresAt is CreatedAt plus the cache's TTL.
	ExpiresAt time.Time
}

// Expired reports whether the flow is no longer usable at now.
func (f *PendingFlow) Expired(now time.Time) bool {
	return f == nil || !now.Before(f.ExpiresAt)
}

// VerifierCache maps a sanitized MCP server URL to its pending PKCE
// flow. It is safe for concurrent use.
type VerifierCache struct {
	mu    sync.Mutex
	flows map[string]*PendingFlow
	now   func() time.Time
	ttl   time.Duration
}

// NewVerifierCache returns an empty cache with the default TTL and the
// wall clock.
func NewVerifierCache() *VerifierCache {
	return newVerifierCache(time.Now, DefaultPKCEFlowTTL)
}

// newVerifierCache is the test seam: it pins the clock and TTL.
func newVerifierCache(now func() time.Time, ttl time.Duration) *VerifierCache {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = DefaultPKCEFlowTTL
	}
	return &VerifierCache{flows: make(map[string]*PendingFlow), now: now, ttl: ttl}
}

// Begin returns the pending flow for serverURL, generating one when
// none exists or the stored one has expired. Repeated calls during a
// retry therefore return the same verifier and challenge, which is what
// keeps a reconnect from breaking the outstanding authorization.
func (c *VerifierCache) Begin(serverURL string) (*PendingFlow, error) {
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidServerURL, serverURL)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if flow, ok := c.flows[key]; ok {
		if !flow.Expired(now) {
			copy := *flow
			return &copy, nil
		}
		delete(c.flows, key)
	}

	flow, err := generatePendingFlow(key, now, c.ttl)
	if err != nil {
		return nil, err
	}
	c.flows[key] = flow
	copy := *flow
	return &copy, nil
}

// Get returns the pending flow for serverURL without creating one.
func (c *VerifierCache) Get(serverURL string) (*PendingFlow, bool) {
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	flow, ok := c.flows[key]
	if !ok || flow.Expired(c.now()) {
		return nil, false
	}
	copy := *flow
	return &copy, true
}

// Consume removes and returns the pending flow for serverURL. It is the
// exchange step: the caller redeems the authorization code with
// flow.Verifier, and the flow is single-use afterwards. An expired flow
// is removed and reported as absent.
func (c *VerifierCache) Consume(serverURL string) (*PendingFlow, bool) {
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	flow, ok := c.flows[key]
	if !ok {
		return nil, false
	}
	delete(c.flows, key)
	if flow.Expired(c.now()) {
		return nil, false
	}
	copy := *flow
	return &copy, true
}

// Prune drops every expired flow and returns how many it removed.
func (c *VerifierCache) Prune() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	removed := 0
	for key, flow := range c.flows {
		if flow.Expired(now) {
			delete(c.flows, key)
			removed++
		}
	}
	return removed
}

// Len reports the number of cached flows, including expired ones that
// have not been pruned yet.
func (c *VerifierCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.flows)
}

// generatePendingFlow builds one verifier / challenge / state triple.
func generatePendingFlow(serverURL string, now time.Time, ttl time.Duration) (*PendingFlow, error) {
	verifier, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: generate verifier: %w", err)
	}
	state, err := randomToken(16)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: generate state: %w", err)
	}
	return &PendingFlow{
		ServerURL: serverURL,
		Verifier:  verifier,
		Challenge: S256Challenge(verifier),
		State:     state,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}, nil
}

// S256Challenge returns BASE64URL(SHA256(verifier)) — the RFC 7636 S256
// code_challenge for a verifier.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// randomToken returns n random bytes as unpadded base64url.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := randRead(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
