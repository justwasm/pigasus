// activate.go — the switching half of the multi-account registry.
//
// The three operations here are the whole feature:
//
//   - Register: a sign-in (the TUI's login modal, the CLI's device flow)
//     becomes a known account.
//   - Activate: a known account becomes the active one, which means its
//     token is written to the shared credential locations the reference
//     reads.
//   - Remove: an account is forgotten.
//
// Everything writes through auth.SaveTokenUnified / auth.ClearTokenUnified
// so the installed `agy` sees exactly what the clone sees.
package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// Current returns the credential the clone and the reference share: the
// token in the shared locations (the macOS keychain first) and the
// account's email.
//
// The email comes from the token's id_token claim. A token the reference
// refreshed carries no id_token any more — the refresh response has no
// id_token — so the registry is consulted as well: the refresh token
// survives a refresh, so it identifies the account.
//
// An empty email means the credential carries no identity: an
// environment-provided token, or a token whose id_token has no email
// claim and that no registered account claims.
func Current() (*auth.AntigravityToken, string) {
	mu.Lock()
	defer mu.Unlock()
	return currentLocked()
}

// currentLocked is Current without the package lock.
func currentLocked() (*auth.AntigravityToken, string) {
	tok, err := auth.LoadTokenUnified()
	if err == nil && tok != nil {
		if email := strings.TrimSpace(tok.UserEmail()); email != "" {
			return tok, email
		}
		if email := accountEmailForToken(tok); email != "" {
			return tok, email
		}
	}
	st, err := loadLocked()
	if err == nil && st != nil && st.Active != "" {
		for _, a := range st.Accounts {
			if strings.EqualFold(a.Email, st.Active) {
				return a.Token, a.Email
			}
		}
		return tok, st.Active
	}
	return tok, ""
}

// accountEmailForToken finds the registered account a token belongs to by
// its refresh token. The refresh token is the stable half of the
// credential: the access token rotates every hour, the refresh token does
// not.
func accountEmailForToken(tok *auth.AntigravityToken) string {
	if tok == nil || tok.Token.RefreshToken == "" {
		return ""
	}
	st, err := loadLocked()
	if err != nil {
		return ""
	}
	for _, a := range st.Accounts {
		if a.Token != nil && a.Token.Token.RefreshToken == tok.Token.RefreshToken {
			return a.Email
		}
	}
	return ""
}

// Register adds the account behind tok to the registry and returns the
// stored entry.
//
// It reports whether the account was already known. Registering the
// account that is already active only refreshes its stored token and
// its last-used stamp, so it is safe to call on every start-up.
func Register(tok *auth.AntigravityToken) (Account, bool, error) {
	if tok == nil {
		return Account{}, false, errors.New("accounts: nil token")
	}
	email := strings.TrimSpace(tok.UserEmail())
	if email == "" {
		return Account{}, false, errors.New("accounts: the token carries no email")
	}
	return RegisterWithEmail(email, tok)
}

// RegisterWithEmail adds the account behind tok to the registry under the
// given email and returns the stored entry.
//
// It is the variant for a credential whose id_token carries no email
// claim — a token the reference refreshed loses its id_token — where the
// email comes from somewhere else: the userinfo probe or the
// environment.
func RegisterWithEmail(email string, tok *auth.AntigravityToken) (Account, bool, error) {
	if tok == nil {
		return Account{}, false, errors.New("accounts: nil token")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return Account{}, false, errors.New("accounts: no email for the account")
	}
	mu.Lock()
	defer mu.Unlock()
	st, err := loadLocked()
	if err != nil {
		return Account{}, false, err
	}
	acc, existed := st.Find(email)
	acc.Email = email
	acc.Token = tok
	if acc.AddedAt.IsZero() {
		acc.AddedAt = time.Now()
	}
	acc.LastUsed = time.Now()
	st.Put(acc)
	st.Active = email
	if err := st.saveLocked(); err != nil {
		return Account{}, existed, err
	}
	return acc, existed, nil
}

// Activate makes the named account the active one.
//
// The stored token is refreshed when it is about to expire, then written
// to the shared credential locations — the macOS keychain entry the
// reference reads and the three on-disk paths — so the clone's next
// request and the reference's next start-up both use this account.
//
// A failed switch leaves the session signed in as it was: the previous
// credential is restored before the error is returned.
func Activate(ctx context.Context, email string) (Account, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return Account{}, errors.New("accounts: no account given")
	}
	mu.Lock()
	defer mu.Unlock()
	st, err := loadLocked()
	if err != nil {
		return Account{}, err
	}
	acc, ok := st.Find(email)
	if !ok {
		return Account{}, fmt.Errorf("accounts: unknown account %q", email)
	}
	if acc.Token == nil {
		return Account{}, fmt.Errorf("accounts: account %q has no stored credentials", acc.Email)
	}

	// Keep the current credential so a failed switch can put it back.
	previous, _ := currentLocked()
	if previous == nil {
		previous = acc.Token
	}

	tok := acc.Token
	updated, _, rerr := tok.RefreshIfNeeded(ctx)
	if rerr != nil {
		restore(previous)
		return Account{}, fmt.Errorf("accounts: %s: %w", acc.Email, rerr)
	}
	if updated != nil {
		tok = updated
	}
	if err := auth.SaveTokenUnified(tok); err != nil {
		restore(previous)
		return Account{}, fmt.Errorf("accounts: %s: %w", acc.Email, err)
	}

	acc.Token = tok
	acc.LastUsed = time.Now()
	st.Put(acc)
	st.Active = acc.Email
	if err := st.saveLocked(); err != nil {
		return Account{}, err
	}
	return acc, nil
}

// Remove forgets an account.
//
// When the account was the active one the shared credential is cleared
// as well, so the sign-out is real: neither the clone nor the reference
// keeps using it. The registry keeps the other accounts, so the user can
// activate one of them and carry on without signing in again.
func Remove(email string) (Account, bool, error) {
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
	if !ok {
		return Account{}, false, nil
	}
	active := strings.EqualFold(st.Active, acc.Email) || strings.EqualFold(currentEmail(), acc.Email)
	st.Remove(acc.Email)
	if err := st.saveLocked(); err != nil {
		return acc, false, err
	}
	if active {
		if _, err := auth.ClearTokenUnified(); err != nil {
			return acc, true, err
		}
	}
	return acc, active, nil
}

// currentEmail returns the email of the account behind the shared
// credential, or "" when there is none.
func currentEmail() string {
	_, email := currentLocked()
	return email
}

// restore puts a credential back into the shared locations. It is
// best-effort: the caller is already returning an error.
func restore(tok *auth.AntigravityToken) {
	if tok == nil {
		return
	}
	_ = auth.SaveTokenUnified(tok)
}
