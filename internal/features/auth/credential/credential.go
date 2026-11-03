// Package credential is the credential cascade the CloudCode transport uses
// when the caller names no token of its own.
//
// The cascade is: the macOS keychain entry first (it is the live token on
// this platform), then the unified on-disk token cache, then
// "no token". The on-disk copies are the fallback; the keychain is what
// the reference reads on darwin.
//
// It lives here, and not in the transport, because two packages need it:
// the transport itself (which uses it when the configuration names no
// token) and the account router (which must answer with *exactly* the
// same credential when its policy is off — a second implementation of the
// cascade would drift from the first).
package credential

import (
	"context"
	"errors"
	"sync"

	"github.com/justwasm/pigasus/internal/features/auth"
)

// DefaultTokenSource returns the transport's own token source — the one a
// client without an explicit token uses.
//
// It is exported so that a wrapper around the transport (the account
// router) can answer with *exactly* the same credential when its policy
// is off: the wrapper must not become a second implementation of the
// credential cascade, or the two would drift.
//
// The keychain's own failure is kept, and it is the *last* thing the
// cascade reports: a keyring that will not open is a more precise
// answer than "no token anywhere", but the on-disk copy still
// authenticates the turn — so the locked keyring is named only when
// nothing else could.
//
// 【文档】 official-docs/cli/troubleshooting.md:75 —
//
//	Error: failed to retrieve token: secret keyring is locked
func DefaultTokenSource() func(context.Context) (string, error) {
	cache := auth.NewUnifiedTokenCache()
	var kc keychainState
	return func(ctx context.Context) (string, error) {
		if tok := kc.token(ctx); tok != "" {
			return tok, nil
		}
		tok, err := cache.Get(ctx)
		if tok != nil && tok.Token.AccessToken != "" {
			return tok.Token.AccessToken, nil
		}
		if kcErr := kc.readError(); kcErr != nil && !errors.Is(kcErr, auth.ErrNoToken) {
			return "", kcErr
		}
		if err != nil {
			return "", err
		}
		return "", auth.ErrNoToken
	}
}

// keychainState is the cached keychain read.
//
// The read happens once and its outcome — the token *or* the failure —
// is remembered: a keyring that will not open stays closed for the
// whole session, and the reference names that failure once per session
// rather than once per turn.
type keychainState struct {
	mu   sync.Mutex
	tok  *auth.AntigravityToken
	err  error
	done bool
}

// read performs the read once and returns the failure it failed with.
func (k *keychainState) read() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.done {
		k.done = true
		k.tok, k.err = auth.LoadKeychainToken()
	}
	return k.err
}

// token returns the keychain's access token, refreshed when it is about
// to expire. An empty string means the keychain holds no usable token;
// the reason is in readError.
func (k *keychainState) token(ctx context.Context) string {
	if err := k.read(); err != nil {
		return ""
	}
	k.mu.Lock()
	tok := k.tok
	k.mu.Unlock()
	if tok == nil || tok.Token.AccessToken == "" {
		return ""
	}
	if updated, _, err := tok.RefreshIfNeeded(ctx); err == nil && updated != nil {
		k.mu.Lock()
		k.tok = updated
		k.mu.Unlock()
		tok = updated
	}
	return tok.Token.AccessToken
}

// readError returns the failure the keychain read produced, if any.
func (k *keychainState) readError() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.err
}
