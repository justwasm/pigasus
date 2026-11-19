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
// ~/.openagy/accounts.json (0600, atomic write). The tokens live
// in it in plaintext, the same way the reference keeps its own token
// file in plaintext. The env override is OPENAGY_ACCOUNTS_CONFIG.
package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/configdir"
	"github.com/justwasm/pigasus/internal/features/auth"
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
	Email          string                 `json:"email"`
	PlanTier       string                 `json:"plan_tier,omitempty"`
	Quota          *auth.QuotaSnapshot    `json:"quota,omitempty"`
	AddedAt        time.Time              `json:"added_at"`
	LastUsed       time.Time              `json:"last_used"`
	Token          *auth.AntigravityToken `json:"token"`
	Disabled       bool                   `json:"disabled,omitempty"`
	DisabledReason string                 `json:"disabled_reason,omitempty"`
}

// Store is the on-disk account registry.
type Store struct {
	Version  int       `json:"version"`
	Active   string    `json:"active,omitempty"`
	Accounts []Account `json:"accounts"`

	// AccountPool is the pool's policy: which accounts a turn may run on and
	// how much quota one needs. It lives here because it is a statement about
	// these accounts, and because the file that is edited when an account is
	// added is the file that should say how accounts are used. A registry
	// without the section is a pool that has never been configured.
	AccountPool *accountpool.Config `json:"account_pool,omitempty"`

	// extra holds the top-level keys this build does not know, so a save
	// writes them back unchanged.
	//
	// A writer reads the whole registry, changes one account and writes the
	// whole file — the quota cache, the token refresher and the accounts API
	// all do. Without this, a build that predates a section deletes it: every
	// save re-emits only the fields it has, and the section a newer build
	// wrote disappears on the next quota read. That is not hypothetical — a
	// `remote-control serve` from before the pool moved here kept erasing
	// `account_pool` every few seconds while the Web UI polled it.
	//
	// So the registry is preserved rather than modelled: a key this build does
	// not understand survives a round trip, and an older binary that has this
	// code cannot destroy a newer section.
	extra map[string]json.RawMessage
}

// registryKeys are the top-level keys Store models.
var registryKeys = map[string]bool{
	"version":      true,
	"active":       true,
	"accounts":     true,
	"account_pool": true,
}

// UnmarshalJSON reads the registry and keeps the keys this build does not
// model in extra.
func (s *Store) UnmarshalJSON(data []byte) error {
	type plain Store
	var known plain
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	*s = Store(known)
	for key, raw := range all {
		if registryKeys[key] {
			continue
		}
		if s.extra == nil {
			s.extra = make(map[string]json.RawMessage, len(all))
		}
		s.extra[key] = raw
	}
	return nil
}

// MarshalJSON writes the registry, including the keys this build does not
// model.
func (s Store) MarshalJSON() ([]byte, error) {
	type plain Store
	known, err := json.Marshal(plain(s))
	if err != nil {
		return nil, err
	}
	if len(s.extra) == 0 {
		return known, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(known, &out); err != nil {
		return nil, err
	}
	for key, raw := range s.extra {
		if _, taken := out[key]; taken {
			continue
		}
		out[key] = raw
	}
	return json.Marshal(out)
}

// Path returns the conventional path of the account registry:
//
//	<the configuration home>/accounts.json — $HOME/.openagy/accounts.json
//	unless $OPENAGY_CONFIG_HOME says otherwise.
//
// The env override is OPENAGY_ACCOUNTS_CONFIG.
func Path() string {
	if p := strings.TrimSpace(os.Getenv("OPENAGY_ACCOUNTS_CONFIG")); p != "" {
		return p
	}
	return configdir.Path("accounts.json")
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
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, raw) {
		return nil
	}
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

var (
	statusHookMu sync.RWMutex
	statusHooks  []func(email string, disabled bool, reason string)
)

// RegisterStatusHook registers a callback that fires when an account is marked
// disabled or cleared.
func RegisterStatusHook(fn func(email string, disabled bool, reason string)) {
	statusHookMu.Lock()
	defer statusHookMu.Unlock()
	statusHooks = append(statusHooks, fn)
}

func notifyStatusHooks(email string, disabled bool, reason string) {
	statusHookMu.RLock()
	hooks := make([]func(string, bool, string), len(statusHooks))
	copy(hooks, statusHooks)
	statusHookMu.RUnlock()
	for _, fn := range hooks {
		fn(email, disabled, reason)
	}
}

// MarkDisabled sets an account's disabled status and reason and persists it.
func MarkDisabled(email, reason string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	mu.Lock()
	st, err := loadLocked()
	if err != nil {
		mu.Unlock()
		return err
	}
	found := false
	changed := false
	for i := range st.Accounts {
		if strings.EqualFold(st.Accounts[i].Email, email) {
			if !st.Accounts[i].Disabled || st.Accounts[i].DisabledReason != reason {
				changed = true
			}
			st.Accounts[i].Disabled = true
			st.Accounts[i].DisabledReason = reason
			found = true
			break
		}
	}
	if !found {
		mu.Unlock()
		return fmt.Errorf("account %q not found", email)
	}
	if err := st.saveLocked(); err != nil {
		mu.Unlock()
		return err
	}
	mu.Unlock()
	if changed {
		notifyStatusHooks(email, true, reason)
	}
	return nil
}

// ClearDisabled clears an account's disabled status and persists it.
func ClearDisabled(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	mu.Lock()
	st, err := loadLocked()
	if err != nil {
		mu.Unlock()
		return err
	}
	found := false
	changed := false
	for i := range st.Accounts {
		if strings.EqualFold(st.Accounts[i].Email, email) {
			if st.Accounts[i].Disabled {
				changed = true
			}
			st.Accounts[i].Disabled = false
			st.Accounts[i].DisabledReason = ""
			found = true
			break
		}
	}
	if !found {
		mu.Unlock()
		return fmt.Errorf("account %q not found", email)
	}
	if err := st.saveLocked(); err != nil {
		mu.Unlock()
		return err
	}
	mu.Unlock()
	if changed {
		notifyStatusHooks(email, false, "")
	}
	return nil
}
