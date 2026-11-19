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
	"math"
	"strings"

	"github.com/justwasm/pigasus/internal/features/auth"
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
	before := acc
	fn(&acc)
	if !accountSubstantivelyChanged(before, acc) {
		return nil
	}
	st.Put(acc)
	return st.saveLocked()
}

// accountSubstantivelyChanged reports whether any durable data of the
// account (tier, credentials, disabled status, or actual quota figures)
// changed. It ignores client-side polling timestamps like Quota.FetchedAt,
// ensuring routine background quota polling does not churn disk writes.
func accountSubstantivelyChanged(a, b Account) bool {
	if a.Email != b.Email {
		return true
	}
	if a.PlanTier != b.PlanTier {
		return true
	}
	if a.Disabled != b.Disabled || a.DisabledReason != b.DisabledReason {
		return true
	}
	if !tokensEqual(a.Token, b.Token) {
		return true
	}
	if !quotasEqual(a.Quota, b.Quota) {
		return true
	}
	return false
}

func tokensEqual(a, b *auth.AntigravityToken) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.AuthMethod != b.AuthMethod || a.IDToken != b.IDToken || a.IDTokenEmail != b.IDTokenEmail {
		return false
	}
	if a.Token.AccessToken != b.Token.AccessToken ||
		a.Token.RefreshToken != b.Token.RefreshToken ||
		a.Token.Expiry != b.Token.Expiry {
		return false
	}
	return true
}

func quotasEqual(a, b *auth.QuotaSnapshot) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Description != b.Description {
		return false
	}
	if len(a.Groups) != len(b.Groups) {
		return false
	}
	for i := range a.Groups {
		gA := a.Groups[i]
		gB := b.Groups[i]
		if gA.Name != gB.Name || gA.Description != gB.Description {
			return false
		}
		if len(gA.Buckets) != len(gB.Buckets) {
			return false
		}
		for j := range gA.Buckets {
			bA := gA.Buckets[j]
			bB := gB.Buckets[j]
			if bA.ID != bB.ID || bA.DisplayName != bB.DisplayName || bA.Window != bB.Window {
				return false
			}
			if !bA.ResetTime.Equal(bB.ResetTime) {
				return false
			}
			if math.Abs(bA.RemainingFraction-bB.RemainingFraction) > 1e-6 {
				return false
			}
		}
	}
	return true
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
