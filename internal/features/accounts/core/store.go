// Package accounts holds the clone's multi-account registry.
//
// # What it is
//
// The reference (`agy`) has exactly one credential: the OAuth token in
// the macOS login keychain (service "gemini", account "antigravity")
// plus three on-disk copies of the same blob. This package keeps a
// *list* of accounts so the clone can be signed in to several Google
// accounts at once and pick which one is active — without leaving the
// TUI session.
//
// # The shared credential
//
// The active account is the one whose token lives in the shared
// credential locations: auth.SaveTokenUnified writes the keychain entry
// the reference reads plus the three on-disk paths, and
// auth.LoadTokenUnified reads them back in that order. A switch
// therefore writes the target account's token there, so
//
//   - the clone picks it up on its next request (the CloudCode client
//     re-reads the keychain), and
//   - the installed `agy` picks it up too — on start-up, and on its next
//     token refresh, because it reads the same keychain entry.
//
// That sharing is the whole point: the clone is a superset of `agy`, and
// `agy` keeps working with whatever account the clone last selected.
//
// # The file
//
// ~/.config/openagy/accounts.json (0600, atomic write). The tokens live
// in it in plaintext, the same way the reference keeps its own token
// file in plaintext. The env override is OPENAGY_ACCOUNTS_CONFIG.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/internal/auth"
)

// Version is the store's schema version.
const Version = 1

// mu serializes the registry's read-modify-write cycles.
//
// The registry is written from several places — the TUI's panel, the
// `agy accounts` command, and the background quota fetcher, which runs
// in its own goroutine — so a load-modify-save cycle has to be atomic
// or a concurrent write is lost.
var mu sync.Mutex

// Account is one signed-in Google account the clone knows about.
//
// Token is the credential the account was signed in with. It is kept so
// the account can be re-activated without another sign-in: the token
// carries the refresh token, and a refresh produces a fresh access
// token for the same account.
//
// PlanTier and Quota are the account's *cached* answers: the tier the
// reference renders next to the email ("Google AI Pro") and the quota
// summary its /usage panel shows. They are read from the server, not
// derived, and they belong to this account — which is why they are
// cached per account and not once per session.
type Account struct {
	Email    string                 `json:"email"`
	PlanTier string                 `json:"plan_tier,omitempty"`
	Quota    *auth.QuotaSnapshot    `json:"quota,omitempty"`
	AddedAt  time.Time              `json:"added_at"`
	LastUsed time.Time              `json:"last_used"`
	Token    *auth.AntigravityToken `json:"token"`
}

// Store is the on-disk account registry.
type Store struct {
	Version  int       `json:"version"`
	Active   string    `json:"active,omitempty"`
	Accounts []Account `json:"accounts"`
}

// Path returns the conventional path of the account registry:
//
//	$XDG_CONFIG_HOME/openagy/accounts.json, or
//	$HOME/.config/openagy/accounts.json if HOME is set.
//
// The env override is OPENAGY_ACCOUNTS_CONFIG.
func Path() string {
	if p := strings.TrimSpace(os.Getenv("OPENAGY_ACCOUNTS_CONFIG")); p != "" {
		return p
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "openagy", "accounts.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "openagy", "accounts.json")
	}
	return ""
}

// Load reads the account registry.
//
// A missing file is an empty registry, not an error, so a first run does
// not have to create it. A malformed file is an error: the caller must
// not silently drop the accounts it holds.
func Load() (*Store, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked()
}

// loadLocked is Load without the package lock. The callers that hold
// the lock themselves (the read-modify-write cycles) use it.
func loadLocked() (*Store, error) {
	path := Path()
	if path == "" {
		return &Store{Version: Version}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{Version: Version}, nil
		}
		return nil, fmt.Errorf("accounts: read %s: %w", path, err)
	}
	var st Store
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("accounts: parse %s: %w", path, err)
	}
	if st.Version == 0 {
		st.Version = Version
	}
	return &st, nil
}

// Save writes the registry atomically with 0600 permissions, so the
// refresh tokens inside it stay readable by their owner only.
func (s *Store) Save() error {
	mu.Lock()
	defer mu.Unlock()
	return s.saveLocked()
}

// saveLocked is Save without the package lock.
func (s *Store) saveLocked() error {
	if s == nil {
		return errors.New("accounts: nil store")
	}
	path := Path()
	if path == "" {
		return errors.New("accounts: no account file path")
	}
	s.Version = Version
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("accounts: encode: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("accounts: create %s: %w", dir, err)
	}
	if err := atomicfile.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("accounts: write: %w", err)
	}
	return nil
}

// Find returns the account with the given email. The match is
// case-insensitive, because the email comes from an identity provider
// and the user types it by hand.
func (s *Store) Find(email string) (Account, bool) {
	email = strings.TrimSpace(email)
	if s == nil || email == "" {
		return Account{}, false
	}
	for _, a := range s.Accounts {
		if strings.EqualFold(a.Email, email) {
			return a, true
		}
	}
	return Account{}, false
}

// Put stores an account, replacing the entry with the same email.
//
// The AddedAt of a known account is immutable: it is the moment the
// account joined the registry, and re-registering an account (a fresh
// sign-in, a switch) does not restart the clock.
func (s *Store) Put(acc Account) {
	if s == nil || strings.TrimSpace(acc.Email) == "" {
		return
	}
	for i, a := range s.Accounts {
		if strings.EqualFold(a.Email, acc.Email) {
			acc.AddedAt = a.AddedAt
			s.Accounts[i] = acc
			return
		}
	}
	s.Accounts = append(s.Accounts, acc)
}

// Remove drops the account with the given email. It reports whether the
// account was there.
func (s *Store) Remove(email string) bool {
	email = strings.TrimSpace(email)
	if s == nil || email == "" {
		return false
	}
	for i, a := range s.Accounts {
		if strings.EqualFold(a.Email, email) {
			s.Accounts = append(s.Accounts[:i], s.Accounts[i+1:]...)
			if strings.EqualFold(s.Active, email) {
				s.Active = ""
			}
			return true
		}
	}
	return false
}

// Emails returns the account emails in store order.
func (s *Store) Emails() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.Accounts))
	for _, a := range s.Accounts {
		out = append(out, a.Email)
	}
	return out
}
