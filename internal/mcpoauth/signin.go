// signin.go — persistent saved sign-in store for remote MCP OAuth
// grants.
//
// AGY 1.3.4 changelog line: "saved sign-ins are now cleared only when
// the server or its authorization server actually rejects them." This
// file owns the persistence side of that contract. The decision side
// lives in policy.go.
//
// On-disk format: a single JSON object keyed by sanitized server URL,
// value is a Signin. The file lives at
//
//	$ANTIGRAVITY_HOME/mcp-oauth-signins.json
//
// (AntigravityConfigHome in internal/auth/compat.go) with 0600 perms,
// so the only reader is the OS user. Concurrent CLIs are serialized
// with an in-process mutex; cross-process races are unlikely because
// the CLI is single-instance per user, and a worst-case overwrite of
// identical records is harmless.
//
// The store is intentionally minimal: it does not refresh tokens, does
// not call out to the network, and does not encrypt the access token.
// Encryption belongs one layer up (the OS keychain or the equivalent
// Antigravity keychain path the rest of the auth package uses); this
// store is the on-disk mirror that lets the clone survive a restart
// without the user re-running `/mcp auth <name>`.

package mcpoauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Signin is one saved OAuth grant for one remote MCP server. Tokens
// are stored verbatim — the upstream transport is responsible for using
// them correctly (typically as `Authorization: Bearer …`). Expiry is
// kept as RFC 3339 for round-trip stability with the rest of the
// auth package; a zero value means "no expiry known".
type Signin struct {
	// ServerURL is the sanitized MCP server URL (scheme://host[:port]
	// + path, no query / fragment). It is the record's key in the
	// store's map.
	ServerURL string `json:"server_url"`
	// ClientID is the OAuth client_id used. For dynamically registered
	// clients this is the one returned by the registration endpoint;
	// for statically configured clients it is the static value.
	ClientID string `json:"client_id"`
	// AccessToken is the bearer token the clone sends in the
	// `Authorization` header on the next MCP request.
	AccessToken string `json:"access_token"`
	// RefreshToken is the long-lived token used to mint a fresh
	// AccessToken via the authorization server's token endpoint.
	// Empty when the grant does not include a refresh_token (e.g.
	// client_credentials grants).
	RefreshToken string `json:"refresh_token,omitempty"`
	// Scope is the space-separated set of granted scopes, kept so a
	// step-up or a token refresh can advertise the existing grant.
	Scope string `json:"scope,omitempty"`
	// ExpiresAt is the access-token expiry as RFC 3339. The empty
	// string means "do not know" — the next 401-driven refresh is
	// still safe; only proactive refresh is skipped.
	ExpiresAt string `json:"expires_at,omitempty"`
	// SavedAt is when this record was written. Useful for debugging
	// "why does the clone still have a token that should have
	// expired" — a stale SavedAt suggests the access token has not
	// been touched since the grant.
	SavedAt string `json:"saved_at"`
}

// Expired reports whether the access token's ExpiresAt has passed at
// time t. A zero ExpiresAt returns false (the "don't know" case):
// the store does not pretend a missing expiry has expired.
func (s *Signin) Expired(t time.Time) bool {
	if s == nil || s.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		return false
	}
	return !t.Before(exp)
}

// Store is the in-memory + on-disk saved-sign-in store. Construct
// with NewStore; the zero value is not usable.
type Store struct {
	// path is the absolute path of the JSON file backing the store.
	// Empty means the store is in-memory only — used by tests.
	path string
	// mu serializes reads / writes. The file itself is only touched
	// under mu; in-memory reads outside mu are safe for the snapshot
	// in `records`, but only because we always re-read under mu when
	// we want the latest.
	mu      sync.Mutex
	records map[string]*Signin
}

// NewStore opens or creates the saved-sign-in store at the canonical
// path (AntigravityConfigHome + "/mcp-oauth-signins.json"). A missing
// file is not an error — the store starts empty. I/O errors at load
// time are returned so the caller can log / surface them; once the
// store is constructed, every read / write is best-effort and returns
// errors so a transient disk failure does not crash the CLI.
func NewStore() (*Store, error) {
	return NewStoreAt(StorePath())
}

// NewStoreAt opens or creates the store at an explicit path. Passing
// an empty path makes the store in-memory only; tests use this.
func NewStoreAt(path string) (*Store, error) {
	s := &Store{path: path, records: map[string]*Signin{}}
	if path == "" {
		return s, nil
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// StorePath returns the canonical absolute path of the saved-sign-in
// file. Honours $ANTIGRAVITY_HOME (internal/auth/compat.go) and
// $HOME, matching the rest of the auth package.
func StorePath() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return filepath.Join(h, "mcp-oauth-signins.json")
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli", "mcp-oauth-signins.json")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli", "mcp-oauth-signins.json")
}

// Get returns the saved sign-in for serverURL, or (nil, false) when
// none is stored. serverURL is sanitized via SanitizeServerURL before
// the lookup so cosmetic URL differences (query string, fragment,
// case in the host) do not produce misses.
func (s *Store) Get(serverURL string) (*Signin, bool) {
	if s == nil {
		return nil, false
	}
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	signin, ok := s.records[key]
	if !ok {
		return nil, false
	}
	// Return a copy so callers cannot mutate the store's record.
	cp := *signin
	return &cp, true
}

// Save persists signin, keyed by signin.ServerURL (after sanitization).
// An empty AccessToken is rejected: storing an empty token would
// produce a no-op auth header and a guaranteed 401. Returns the
// sanitized key actually used.
func (s *Store) Save(signin *Signin) (string, error) {
	if s == nil {
		return "", errors.New("mcpoauth: nil store")
	}
	if signin == nil {
		return "", errors.New("mcpoauth: nil signin")
	}
	if signin.AccessToken == "" {
		return "", errors.New("mcpoauth: refusing to save sign-in with empty access_token")
	}
	key := SanitizeServerURL(signin.ServerURL)
	if key == "" {
		return "", fmt.Errorf("mcpoauth: refusing to save sign-in with unparseable server url %q", signin.ServerURL)
	}
	if signin.SavedAt == "" {
		signin.SavedAt = time.Now().UTC().Format(time.RFC3339)
	}
	cp := *signin
	cp.ServerURL = key
	s.mu.Lock()
	s.records[key] = &cp
	err := s.writeLocked()
	s.mu.Unlock()
	if err != nil {
		return key, fmt.Errorf("mcpoauth: save: %w", err)
	}
	return key, nil
}

// Clear deletes the saved sign-in for serverURL and returns whether
// one was present. A no-op (returning false, nil) when nothing was
// stored means the call is idempotent and a flaky caller does not
// have to distinguish "not present" from "cleared".
func (s *Store) Clear(serverURL string) (bool, error) {
	if s == nil {
		return false, nil
	}
	key := SanitizeServerURL(serverURL)
	if key == "" {
		return false, nil
	}
	s.mu.Lock()
	_, present := s.records[key]
	delete(s.records, key)
	err := s.writeLocked()
	s.mu.Unlock()
	if err != nil {
		return present, fmt.Errorf("mcpoauth: clear: %w", err)
	}
	return present, nil
}

// ClearAll drops every saved sign-in. Used by `/logout --mcp` and by
// tests that need a clean slate.
func (s *Store) ClearAll() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.records = map[string]*Signin{}
	err := s.writeLocked()
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("mcpoauth: clear-all: %w", err)
	}
	return nil
}

// List returns every saved sign-in, sorted by ServerURL for stable
// test output and stable `/mcp auth list` rendering.
func (s *Store) List() []*Signin {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	out := make([]*Signin, 0, len(s.records))
	for _, r := range s.records {
		cp := *r
		out = append(out, &cp)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServerURL < out[j].ServerURL
	})
	return out
}

// Count returns the number of saved sign-ins. Faster than len(List())
// when the caller only needs the count (e.g. for "0 saved" messages).
func (s *Store) Count() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// load reads the JSON file into s.records. Called under no lock by
// NewStoreAt (the store is not yet published).
func (s *Store) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("mcpoauth: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	var raw map[string]*Signin
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("mcpoauth: parse %s: %w", s.path, err)
	}
	if raw == nil {
		return nil
	}
	for k, v := range raw {
		if v == nil {
			continue
		}
		v.ServerURL = k
		s.records[k] = v
	}
	return nil
}

// writeLocked writes the JSON file. Caller must hold s.mu.
func (s *Store) writeLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Stable JSON output: encoding/json sorts map keys, so two stores
	// with the same records produce identical bytes (handy for diffing
	// and for the test fixture golden file).
	data, err := json.Marshal(s.records)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	return nil
}
