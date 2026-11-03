// quota.go — quota and access token retrieval for registered accounts.
package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
)

// RegisterCurrent adds the credential the session starts on to the registry.
//
// The email comes from the caller: the userinfo probe resolves it when the
// token's id_token carries no email claim any more — a token the
// reference refreshed loses its id_token. A missing credential is not an
// error; the session simply starts signed out.
func RegisterCurrent(email string) {
	if strings.TrimSpace(email) == "" {
		return
	}
	tok, err := auth.LoadTokenUnified()
	if err != nil || tok == nil {
		return
	}
	_, _, _ = RegisterWithEmail(email, tok)
}

// QuotaTimeout is the budget one quota fetch gets.
//
// The call can be two round trips: a token refresh (the OAuth endpoint
// measured 9 s from this machine) and the quota call itself (6.9 s). A
// shorter budget turned a slow but successful fetch into a timeout.
//
// The budget belongs here, next to the fetch: a caller that brings its
// own deadline keeps it, and a caller that brings none gets this one.
const QuotaTimeout = 45 * time.Second

// QuotaFreshness is how long a cached quota snapshot is reused instead
// of being queried again.
//
// The quota moves in hours, not in seconds: the five-hour window creeps
// and the weekly window creeps. Two minutes is short enough that the
// numbers are current and long enough that opening /usage right after
// /accounts does not query the same account twice.
//
// The registry owns the snapshot, so the rule lives here and not in the
// surface that reads it.
const QuotaFreshness = 2 * time.Minute

// FreshQuota returns the account's cached quota summary when the
// registry's snapshot is still fresh enough to render.
//
// The snapshot is the registry's: the /accounts panel's fetch writes it
// and the /usage panel and the status line read it back, so the same
// account is never queried twice.
//
// A missing account, a missing snapshot, a stale snapshot and an
// unreadable registry all answer nil — the caller then asks the server.
func FreshQuota(email string) *auth.QuotaSummary {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	acc, ok, err := Get(email)
	if err != nil || !ok || acc.Quota == nil {
		return nil
	}
	if time.Since(acc.Quota.FetchedAt) > QuotaFreshness {
		return nil
	}
	return acc.Quota.Summary()
}

// FetchQuota queries one account's quota and caches the snapshot.
//
// The snapshot is written into the registry before the answer is
// returned, so every reader of the registry — the /accounts panel, the
// /usage panel, the status line — sees the same numbers.
func FetchQuota(ctx context.Context, email string) (*auth.QuotaSummary, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, QuotaTimeout)
		defer cancel()
	}
	acc, ok, err := Get(email)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("accounts: unknown account %q", email)
	}
	tok, err := AccessToken(ctx, email, acc)
	if err != nil {
		return nil, err
	}
	summary, err := auth.FetchQuotaSummaryWithToken(ctx, tok.Token.AccessToken)
	if err != nil {
		return nil, err
	}
	snap := summary.Snapshot()
	_ = Update(email, func(a *Account) {
		a.Token = tok
		a.Quota = snap
	})
	return summary, nil
}

// AccessToken returns the credential to query an account's quota with.
func AccessToken(ctx context.Context, email string, acc Account) (*auth.AntigravityToken, error) {
	shared, active := Current()
	if strings.EqualFold(active, email) && shared != nil {
		tok := shared
		updated, _, rerr := tok.RefreshIfNeeded(ctx)
		if rerr != nil {
			return nil, rerr
		}
		if updated != nil {
			tok = updated
		}
		return tok, nil
	}
	if acc.Token == nil {
		return nil, fmt.Errorf("accounts: %s has no stored credentials", email)
	}
	updated, _, rerr := acc.Token.RefreshQuiet(ctx)
	if rerr != nil {
		return nil, rerr
	}
	if updated != nil {
		return updated, nil
	}
	return acc.Token, nil
}
