// cache.go — the per-account cache the /accounts panel shows.
//
// # What is cached
//
// Two answers the reference asks the server for, per *account*:
//
//   - the plan tier ("Google AI Pro", "Antigravity Starter Quota"),
//     read from loadCodeAssist;
//   - the quota summary (the two model groups with their weekly and
//     five-hour buckets), read from retrieveUserQuotaSummary.
//
// Neither belongs to the session: they belong to the account behind a
// credential. The registry keeps them next to the account's token, so
// the panel can render every account's tier and usage without a
// round trip per row — and so the /usage panel can reuse a snapshot
// the /accounts panel already fetched instead of querying twice.
//
// # The write discipline
//
// The tier and the quota are read from the server, so they are written
// back from the goroutine that read them. Every write goes through
// Update, which holds the package lock over the whole read-modify-write
// cycle: a concurrent write (a switch, a sign-in, the CLI's `agy
// accounts`) must not be lost.
package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// Get returns one registered account.
func Get(email string) (Account, bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return Account{}, false, errors.New("accounts: no account given")
	}
	mu.Lock()
	defer mu.Unlock()
	st, err := loadLocked()
	if err != nil {
		return Account{}, false, err
	}
	acc, ok := st.Find(email)
	return acc, ok, nil
}

// Update applies fn to the named account and writes the registry back.
//
// It is the only way to change a cached field: the read-modify-write
// cycle has to be atomic, and the callers (the panel's background
// fetcher) run in their own goroutine.
//
// An unknown account is an error: a cache entry for an account the
// registry does not hold would come back on the next read.
func Update(email string, fn func(*Account)) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("accounts: no account given")
	}
	if fn == nil {
		return errors.New("accounts: no update function")
	}
	mu.Lock()
	defer mu.Unlock()
	st, err := loadLocked()
	if err != nil {
		return err
	}
	acc, ok := st.Find(email)
	if !ok {
		return fmt.Errorf("accounts: unknown account %q", email)
	}
	fn(&acc)
	st.Put(acc)
	return st.saveLocked()
}

// SetPlanTier caches the account's plan tier. An empty tier clears the
// cached one: the server answered "no tier", and a stale tier would be
// worse than none.
func SetPlanTier(email, tier string) error {
	return Update(email, func(acc *Account) {
		acc.PlanTier = strings.TrimSpace(tier)
	})
}

// SetQuota caches the account's quota summary.
func SetQuota(email string, snap *auth.QuotaSnapshot) error {
	return Update(email, func(acc *Account) {
		acc.Quota = snap
	})
}

// SetToken replaces the account's stored credential.
//
// The panel's quota fetch refreshes an account's access token; the
// refreshed token has to go back into the registry, or the next fetch
// would refresh it again.
func SetToken(email string, tok *auth.AntigravityToken) error {
	return Update(email, func(acc *Account) {
		acc.Token = tok
	})
}
