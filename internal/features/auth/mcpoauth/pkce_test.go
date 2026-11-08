package mcpoauth

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestVerifierCache_Begin pins the generated flow's shape and the S256
// challenge.
func TestVerifierCache_Begin(t *testing.T) {
	cache := NewVerifierCache()
	flow, err := cache.Begin("https://mcp.example.com/sse")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if flow.ServerURL != "https://mcp.example.com/sse" {
		t.Errorf("ServerURL = %q", flow.ServerURL)
	}
	if len(flow.Verifier) != 43 {
		t.Errorf("verifier length = %d, want 43", len(flow.Verifier))
	}
	if flow.Challenge != S256Challenge(flow.Verifier) {
		t.Errorf("challenge = %q, want S256(verifier)", flow.Challenge)
	}
	if flow.State == "" {
		t.Errorf("state is empty")
	}
	if !flow.ExpiresAt.Equal(flow.CreatedAt.Add(DefaultPKCEFlowTTL)) {
		t.Errorf("expiry = %v, want created + TTL", flow.ExpiresAt)
	}
}

// TestVerifierCache_ReusesFlowDuringReconnect is the AGY 1.5.2 contract:
// a retry (reconnect) before the exchange gets the same verifier instead
// of invalidating the outstanding authorization.
func TestVerifierCache_ReusesFlowDuringReconnect(t *testing.T) {
	cache := NewVerifierCache()
	first, err := cache.Begin("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("first Begin: %v", err)
	}
	second, err := cache.Begin("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("second Begin: %v", err)
	}
	if first.Verifier != second.Verifier || first.Challenge != second.Challenge || first.State != second.State {
		t.Fatalf("reconnect generated a new flow:\n first=%+v\nsecond=%+v", first, second)
	}
	if cache.Len() != 1 {
		t.Errorf("cache length = %d, want 1", cache.Len())
	}

	// Cosmetic URL variants share a key (SanitizeServerURL).
	variant, err := cache.Begin("HTTPS://MCP.Example.com/mcp?x=1#frag")
	if err != nil {
		t.Fatalf("variant Begin: %v", err)
	}
	if variant.Verifier != first.Verifier {
		t.Errorf("sanitized variant got a new verifier")
	}
}

// TestVerifierCache_Consume pins the single-use exchange step.
func TestVerifierCache_Consume(t *testing.T) {
	cache := NewVerifierCache()
	flow, err := cache.Begin("https://mcp.example.com")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	got, ok := cache.Consume("https://mcp.example.com")
	if !ok || got.Verifier != flow.Verifier {
		t.Fatalf("Consume = (%+v, %v), want the started flow", got, ok)
	}
	if _, ok := cache.Consume("https://mcp.example.com"); ok {
		t.Errorf("second Consume returned a flow; want absent")
	}
	if _, ok := cache.Get("https://mcp.example.com"); ok {
		t.Errorf("Get after Consume returned a flow")
	}
}

// TestVerifierCache_Expiry makes expired flows unusable and lets the
// next Begin start over.
func TestVerifierCache_Expiry(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now := start
	cache := newVerifierCache(func() time.Time { return now }, time.Minute)

	first, err := cache.Begin("https://mcp.example.com")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	now = start.Add(30 * time.Second)
	if got, ok := cache.Get("https://mcp.example.com"); !ok || got.Verifier != first.Verifier {
		t.Fatalf("flow expired before its TTL")
	}

	now = start.Add(2 * time.Minute)
	if _, ok := cache.Get("https://mcp.example.com"); ok {
		t.Errorf("Get returned an expired flow")
	}
	if _, ok := cache.Consume("https://mcp.example.com"); ok {
		t.Errorf("Consume returned an expired flow")
	}
	if removed := cache.Prune(); removed != 0 {
		// Consume already removed it.
		t.Errorf("Prune removed %d after Consume", removed)
	}

	fresh, err := cache.Begin("https://mcp.example.com")
	if err != nil {
		t.Fatalf("Begin after expiry: %v", err)
	}
	if fresh.Verifier == first.Verifier {
		t.Errorf("Begin after expiry reused the expired verifier")
	}
	if _, err := cache.Begin("https://other.example.com"); err != nil {
		t.Fatalf("Begin other: %v", err)
	}

	now = start.Add(3 * time.Minute)
	if removed := cache.Prune(); removed != 2 {
		t.Errorf("Prune removed %d, want 2", removed)
	}
	if cache.Len() != 0 {
		t.Errorf("cache length = %d after Prune", cache.Len())
	}
}

// TestVerifierCache_InvalidURL refuses a URL that cannot be keyed.
func TestVerifierCache_InvalidURL(t *testing.T) {
	cache := NewVerifierCache()
	for _, raw := range []string{"", "   ", "://bad", "http://%zz"} {
		if _, err := cache.Begin(raw); !errors.Is(err, ErrInvalidServerURL) {
			t.Errorf("Begin(%q) error = %v, want ErrInvalidServerURL", raw, err)
		}
	}
	if _, ok := cache.Get(""); ok {
		t.Errorf("Get(\"\") returned a flow")
	}
	if _, ok := cache.Consume("://bad"); ok {
		t.Errorf("Consume of an invalid URL returned a flow")
	}
}

// TestVerifierCache_GeneratorFailure surfaces crypto/rand failures.
func TestVerifierCache_GeneratorFailure(t *testing.T) {
	prev := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("entropy exhausted") }
	defer func() { randRead = prev }()

	cache := NewVerifierCache()
	if _, err := cache.Begin("https://mcp.example.com"); err == nil {
		t.Fatalf("Begin succeeded with a failing generator")
	}
}

// TestVerifierCache_ConcurrentBegin pins that parallel reconnects share
// one flow.
func TestVerifierCache_ConcurrentBegin(t *testing.T) {
	cache := NewVerifierCache()
	const n = 32
	verifiers := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			flow, err := cache.Begin("https://mcp.example.com")
			if err != nil {
				t.Errorf("Begin: %v", err)
				return
			}
			verifiers[i] = flow.Verifier
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		if verifiers[i] != verifiers[0] {
			t.Fatalf("verifier %d = %q, want %q", i, verifiers[i], verifiers[0])
		}
	}
	if cache.Len() != 1 {
		t.Errorf("cache length = %d, want 1", cache.Len())
	}
	if strings.TrimSpace(verifiers[0]) == "" {
		t.Errorf("empty verifier")
	}
}
