// tokenfreshness.go — AGY 1.5.17: "Fixed long-running agent turns failing
// with an UNAUTHENTICATED (401) error when a Google OAuth access token
// crossed its one-hour expiry; the CLI now refreshes tokens ahead of expiry
// and retries a pre-stream 401 automatically."
//
// The bullet has two independent halves, and they live at different layers:
//
//	Half 1 (pre-use refresh) — a decision about an *AntigravityToken* and a
//	    clock. It lives in this package, next to RefreshIfNeeded.
//
//	Half 2 (pre-stream 401 retry) — a decision about an *HTTP response*,
//	    which this package does not own. The transport owns it, so this file
//	    exports the pure predicate and the transport calls it.
//
// Both decisions take "now" as a parameter rather than reading the wall
// clock, so the tests are table-driven and need no time.Sleep.
package auth

import (
	"context"
	"errors"
	"time"
)

// AccessTokenRefreshMargin is how far ahead of its expiry an access token is
// refreshed before use.
//
// Justification for 5 minutes: Google access tokens live for 3600s. A single
// agent turn's handshake plus a slow first stream chunk is comfortably under
// a minute on a healthy network, but the margin is not about the handshake —
// it is about the tail. Any token already inside the margin when a turn
// starts can expire at an arbitrary point during that turn, and a mid-stream
// 401 is unrecoverable by design (see ShouldRetryAfterRefresh). 5 minutes
// covers a slow request plus a stalled stream with room to spare, at a cost
// of at most one extra refresh per hour: one cheap round trip against an
// already-warm token endpoint versus a failed turn.
//
// It matches the margin RefreshIfNeeded has used since cycle 44, so this
// constant is the single source of truth rather than a second opinion.
const AccessTokenRefreshMargin = 5 * time.Minute

// errNoAccessOrRefreshToken is the "nothing to work with" failure shared by
// the margin path and the forced path, so the two cannot drift.
var errNoAccessOrRefreshToken = errors.New("antigravity-token: no access_token and no refresh_token")

// NeedsRefreshAt reports whether this token should be refreshed before its
// next use, evaluated against the caller's clock instead of time.Now.
//
// A zero expiry means "we do not know when this token dies" — a token that
// came from AGY_OAUTH_TOKEN with no companion on-disk record, for example.
// That reports true (needs a refresh), which is the pre-existing and
// conservative behaviour of RefreshIfNeeded: an unknown lifetime is exactly
// the case where a mid-turn 401 is most likely, and the refresh is one cheap
// round trip. A token with no refresh_token is handed back unchanged by the
// refresh path rather than treated as fatal.
//
// A nil token reports false; callers treat nil as ErrNoToken rather than an
// expiry verdict.
func (t *AntigravityToken) NeedsRefreshAt(now time.Time) bool {
	if t == nil {
		return false
	}
	exp := t.ExpiryTime()
	if exp.IsZero() {
		return true
	}
	// Compare instants, not durations: exp.Add(-margin) is timezone-
	// independent, while time.Until(exp) would silently read the wall clock
	// and make this function untestable.
	return !now.Before(exp.Add(-AccessTokenRefreshMargin))
}

// RefreshIfNeededAt is RefreshIfNeeded with an injectable clock.
//
// RefreshIfNeeded keeps its signature for the existing call sites
// (UnifiedTokenCache.Get, credential.Load) and delegates here with
// time.Now(). Tests use this variant so the margin boundary is exercised
// deterministically rather than with sleeps.
func (t *AntigravityToken) RefreshIfNeededAt(ctx context.Context, now time.Time) (*AntigravityToken, bool, error) {
	if t == nil {
		return nil, false, ErrNoToken
	}
	if !t.NeedsRefreshAt(now) {
		return t, false, nil
	}
	return t.refresh(ctx)
}

// RefreshForced refreshes regardless of the remaining lifetime, applying the
// same rejection / transient rules as RefreshIfNeeded. This is what half 2
// calls after a 401: a 401 says the token is bad now, whatever its clock says.
func (t *AntigravityToken) RefreshForced(ctx context.Context) (*AntigravityToken, bool, error) {
	if t == nil {
		return nil, false, ErrNoToken
	}
	return t.refresh(ctx)
}

// RefreshIfNeededNow is the package-level forced refresh for callers outside
// this package (the transport's pre-stream 401 retry), so they do not have to
// duplicate the refresh / clear rules or reach into the margin.
func RefreshIfNeededNow(ctx context.Context, tok *AntigravityToken) (*AntigravityToken, bool, error) {
	if tok == nil {
		return nil, false, ErrNoToken
	}
	return tok.RefreshForced(ctx)
}

// refresh performs the refresh-token grant and applies the outcome. It is the
// single body shared by the margin path and the forced path, so the rejection
// and transient rules cannot drift between them.
func (t *AntigravityToken) refresh(ctx context.Context) (*AntigravityToken, bool, error) {
	if t.Token.RefreshToken == "" {
		if t.Token.AccessToken == "" {
			return t, false, errNoAccessOrRefreshToken
		}
		// Unrefreshable: hand the token back unchanged. The caller may still
		// get a usable (if short-lived) request out of it, and any 401 is
		// then half 2's problem.
		return t, false, nil
	}
	resp, err := RefreshTokenRealWithSecret(ctx, CLIClientID, publicClientSecret, t.Token.RefreshToken)
	if err != nil {
		werr := handleUnifiedRefreshError(err)
		if IsAuthRejected(werr) {
			// The credential is gone: the caller must sign in again, and
			// handing the stale token back would let it be used by accident.
			return nil, false, werr
		}
		// A transient failure — network error, 5xx, rate limit — keeps the
		// credential: the stale token is the best we have, and the user can
		// try again without re-authenticating.
		return t, false, werr
	}
	applyUnifiedRefreshResponse(t, resp)
	return t, true, nil
}

// ShouldRetryAfterRefresh answers the half-2 question for exactly one failure:
// is this 401 recoverable by refreshing the credential and re-issuing the
// request once?
//
// The three inputs are the whole decision, and each is load-bearing:
//
//   - statusCode must be 401. 403 is a permissions verdict (the account may
//     lack the scope) and refreshing does not change it. Every other status
//     belongs to another retry policy (see retry.IsRetryable and
//     isSandboxFailoverEligible).
//   - bytesStreamed must be 0. A 401 that arrives after content has begun to
//     stream is not recoverable by a retry: the client has already handed
//     output to the UI, so re-issuing the turn would duplicate it. Surfacing
//     is the only correct action.
//   - refreshAttempted must be false. This is the loop guard: a second
//     consecutive 401 after a successful refresh means the credential itself
//     is bad, not stale, and retrying again would spin forever.
//
// Call site (transport layer, outside this package):
//
//	internal/infra/backend/cloudcode.(*CloudCodeClient).open
//	(internal/infra/backend/cloudcode/cloudcode_stream.go)
//
// open is the right seam because it returns before any body byte is read, so
// it always has bytesStreamed == 0 on its failure path, and it already wraps
// pre-stream errors via toBackendError. The owner would add, on that error
// path only:
//
//	var he *geminicode.HTTPError
//	if errors.As(err, &he) && auth.ShouldRetryAfterRefresh(he.StatusCode, 0, c.refreshAttempted) {
//	    c.refreshAttempted = true                  // per-turn state
//	    if _, _, rerr := auth.RefreshIfNeededNow(ctx, c.tok); rerr == nil {
//	        return c.open(ctx, turn)               // exactly one retry
//	    }
//	}
//
// Post-stream 401s never pass through open: they surface from readBodyOnce /
// finishScan, by which point bytesStreamed > 0, so the predicate is false for
// them even if a caller wired it in there.
func ShouldRetryAfterRefresh(statusCode int, bytesStreamed int64, refreshAttempted bool) bool {
	return statusCode == 401 && bytesStreamed == 0 && !refreshAttempted
}
