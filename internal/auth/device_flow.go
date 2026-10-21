// OAuth 2.0 device-authorization-grant bootstrap for the antigravity CLI.
//
// Cycle 170. The recovered build (1.3.5) does NOT run an OAuth device-code
// flow at runtime — it reads a pre-issued bearer from
// $JETSKI_OAUTH_TOKEN (or the keyring-loaded oauth-token file) and ships
// it on every request as `Authorization: Bearer …`. This file adds the
// missing piece: an interactive login that opens a browser, asks the user
// to enter a one-shot code, and persists the resulting refresh / access
// tokens through the same SaveTokenUnified path the rest of the auth
// package uses.
//
// Implementation
// --------------
// The flow is built on golang.org/x/oauth2 v0.37.0+, which now ships
// Config.DeviceAuth (RFC 8628 §3.1) and Config.DeviceAccessToken
// (RFC 8628 §3.4) for the Google endpoint. We layer two things on top:
//
//  1. The Google OAuth Config object with the public CLI client ID
//     (cycle 44 ship) and the seven scopes the v1.3.5 refresh grant uses.
//
//  2. A small polling loop around Config.DeviceAccessToken that respects
//     RFC 8628 §3.5's slow_down and authorization_pending semantics by
//     inspecting the wire error code on RetrieveError instead of bubbling
//     the raw response to the caller.
//
// Cycle scope notes
// -----------------
// This file ships the library only — the CLI subcommand and TUI prompt
// live in cmd/wagyu/. They wire up the same flow and persist the result
// through SaveTokenUnified, so any future caller gets the same on-disk
// token file format and the same RefreshIfNeeded behaviour.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// DeviceFlowScopes is the canonical scope list the recovered v1.3.5
// binary requests on every refresh. Cycle 42 close-gap shipped six of
// these; the device-code grant adds userinfo.profile so the resulting
// token matches what `agy profile` would receive in the browser path.
var DeviceFlowScopes = []string{
	ScopeOpenID,
	ScopeUserInfoEmail,
	"https://www.googleapis.com/auth/userinfo.profile",
	ScopeCloudPlatform,
	ScopeCCLog,
	ScopeAICode,
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// DeviceFlowClientID defaults to CLIClientID (the same public OAuth
// client ID used by the refresh-token grant in unified_config.go).
// Kept as a var so callers can override in tests / future clients.
var DeviceFlowClientID = CLIClientID

// DeviceFlowEndpoint is the oauth2.Endpoint the device-flow library
// posts to. Defaults to endpoints.Google; tests redirect this to a
// local httptest.Server so they never hit the public OAuth endpoint.
var DeviceFlowEndpoint = endpoints.Google

// DeviceFlowTimeout is the total time we are willing to wait for the
// user to complete the verification step. The DeviceAccessToken call
// polls internally at the server-supplied interval and only returns
// once the user approves, denies, or the device code expires — we
// bound that whole wait with this deadline.
const DeviceFlowTimeout = 10 * time.Minute

// DeviceAuthResponse is a thin alias for oauth2.DeviceAuthResponse so
// the rest of the package can import this file without dragging the
// x/oauth2 type through its public surface. The fields are stable
// across x/oauth2 versions.
type DeviceAuthResponse = oauth2.DeviceAuthResponse

// NewDeviceFlowConfig returns the *oauth2.Config used by the device
// grant. Callers can override ClientID / Scopes before passing it to
// DeviceFlow if they want a non-default profile.
func NewDeviceFlowConfig(clientID string, scopes []string) *oauth2.Config {
	if clientID == "" {
		clientID = DeviceFlowClientID
	}
	if len(scopes) == 0 {
		scopes = DeviceFlowScopes
	}
	return &oauth2.Config{
		ClientID: clientID,
		Endpoint: DeviceFlowEndpoint,
		Scopes:   scopes,
	}
}

// DeviceFlowOptions bundles the optional side-effects of a running flow:
//
//	OnAuthRequest  — fired once when the device code has been issued;
//	                 receives the user-visible URL and short code. The
//	                 caller typically prints it and / or opens a browser.
//
// The hook is optional; pass nil to skip. Per-poll callbacks are not
// exposed because Config.DeviceAccessToken polls internally and we do
// not see individual poll attempts from this layer.
type DeviceFlowOptions struct {
	OnAuthRequest func(verificationURL, userCode string)
}

// DeviceFlow runs the OAuth 2.0 device-authorization grant against the
// Google endpoint configured in NewDeviceFlowConfig. It blocks until
// the user has either approved or denied the request, or until ctx
// fires (or DeviceFlowTimeout, whichever is first).
//
// On success the returned *oauth2.Token has a populated AccessToken
// and (for first-time grants) a RefreshToken that the caller is
// expected to persist via SaveTokenUnified so subsequent runs can use
// the silent refresh path.
//
// Implementation note: Config.DeviceAccessToken already polls the
// /token endpoint internally at the server-supplied interval (default
// 5 s) and respects slow_down / authorization_pending per RFC 8628
// §3.5 — it only returns once the user approves, denies, or the device
// code expires. We therefore wrap a single DeviceAccessToken call here
// rather than building a second polling loop on top of it; doing both
// at once would double the wait time on the user's first request.
func DeviceFlow(ctx context.Context, cfg *oauth2.Config, opts *DeviceFlowOptions) (*oauth2.Token, *DeviceAuthResponse, error) {
	if cfg == nil {
		return nil, nil, errors.New("auth.DeviceFlow: nil config")
	}
	if opts == nil {
		opts = &DeviceFlowOptions{}
	}

	flowCtx, cancel := context.WithTimeout(ctx, DeviceFlowTimeout)
	defer cancel()

	da, err := cfg.DeviceAuth(flowCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("auth.DeviceFlow: device auth request: %w", err)
	}
	if da == nil || da.DeviceCode == "" {
		return nil, nil, errors.New("auth.DeviceFlow: empty device code in response")
	}

	if opts.OnAuthRequest != nil {
		url := da.VerificationURIComplete
		if url == "" {
			url = da.VerificationURI
		}
		opts.OnAuthRequest(url, da.UserCode)
	}

	tok, err := cfg.DeviceAccessToken(flowCtx, da)
	if err != nil {
		return nil, da, classifyDeviceError(err)
	}
	return tok, da, nil
}

// classifyDeviceError maps the wire-level RetrieveError codes from
// /token onto human-readable error messages so the CLI / TUI can show
// the user why the flow ended.
func classifyDeviceError(err error) error {
	var retErr *oauth2.RetrieveError
	if errors.As(err, &retErr) {
		switch retErr.ErrorCode {
		case "slow_down":
			return fmt.Errorf("auth.DeviceFlow: server asked to slow down: %w", err)
		case "authorization_pending":
			return fmt.Errorf("auth.DeviceFlow: device code already used: %w", err)
		case "access_denied":
			return fmt.Errorf("auth.DeviceFlow: user denied access at the verification URL")
		case "expired_token":
			return fmt.Errorf("auth.DeviceFlow: device code expired before user approved (codes typically live ~30 s after first poll)")
		}
	}
	return fmt.Errorf("auth.DeviceFlow: token poll: %w", err)
}

// TokenToAntigravityToken converts an *oauth2.Token from the device
// flow into the AntigravityToken shape the rest of the auth package
// writes to disk. Callers must populate AuthMethod / IDToken if those
// fields matter for their path; the device-code response only gives us
// the access / refresh pair and the expiry.
func TokenToAntigravityToken(tok *oauth2.Token) *AntigravityToken {
	if tok == nil {
		return nil
	}
	at := AntigravityAccessToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
	}
	if !tok.Expiry.IsZero() {
		at.Expiry = tok.Expiry.Format(time.RFC3339Nano)
	}
	return &AntigravityToken{
		Token:      at,
		AuthMethod: "consumer",
	}
}
