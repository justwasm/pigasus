// quota.go — quota and access token retrieval for registered accounts.
package accounts

import (
	"context"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// RegisterCurrent adds the credential the session starts on to the registry.
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

// FetchQuota queries one account's quota and caches the snapshot.
func FetchQuota(ctx context.Context, email string) (*auth.QuotaSummary, error) {
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
