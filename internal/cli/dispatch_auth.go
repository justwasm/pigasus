// Package cli — dispatch_auth.go: authentication, OAuth login, and logout handlers.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/oauth2"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// ---- 2. runLogin ----------------------------------------------------------

// runLogin runs the OAuth 2.0 device-authorization grant against the
// Google endpoint configured in auth.NewDeviceFlowConfig and persists
// the resulting token through auth.SaveTokenUnified.
//
// Cycle 170: the recovered build (1.3.5) does not perform an OAuth
// device-code flow at runtime (cycle 42 close-gap: it reads the token
// from $JETSKI_OAUTH_TOKEN or the keyring). The `login` subcommand is
// preserved for argv compatibility, and is now backed by a real
// implementation: it opens a browser-shaped prompt via the OnAuthRequest
// hook, waits for the user to approve the device code on Google's
// verification URL, then writes the access + refresh tokens to the
// canonical path so subsequent runs of the clone use the silent
// refresh path without re-prompting.
//
// Use --no-browser to skip the open-browser hint and just print the
// URL; useful in headless / SSH sessions where the caller's $DISPLAY
// cannot be reached.
func runLogin(d *dispatchContext) error {
	d.printOK("login")
	return RunDeviceFlowLogin(d.Ctx, d.Out, d.Err)
}

// RunDeviceFlowLogin runs the OAuth 2.0 device-authorization grant that
// the `login` subcommand (and now `remote-control serve`) drive
// against accounts.google.com, persists the resulting access + refresh
// tokens via auth.SaveTokenUnified, and writes the verification-URL
// prompt + final success line to out. Any non-fatal warning lands in
// err (currently only the "open browser" side-effect failure).
//
// On success the canonical token file (auth.TokenFilePath()) is
// populated so subsequent calls to auth.LoadTokenUnified() succeed
// without re-prompting the user. Returns nil on a successful save;
// an error otherwise.
//
// The function honours $OPENAGY_NO_BROWSER (the same env var the
// cli.OpenBrowser wrapper checks) so SSH / no-X callers can suppress
// the auto-open side-effect without re-plumbing a parameter.
//
// Cycle rc-serve-subcommand: this was extracted from the cycle-170
// runLogin implementation so the `remote-control serve` foreground
// handler can drive the same flow without going through the
// dispatchContext (which is internal to internal/cli/).
func RunDeviceFlowLogin(ctx context.Context, out, err io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Fprintf(out, "starting OAuth device-code flow against %s\n", endpoints.Current().OAuth2AuthHost())
	fmt.Fprintf(out, "client_id:    %s\n", auth.DeviceFlowClientID)
	fmt.Fprintf(out, "scopes:       %d\n", len(auth.DeviceFlowScopes))
	fmt.Fprintln(out, "")

	cfg := auth.NewDeviceFlowConfig("", nil)
	urlShown := make(chan struct{}, 1)
	opts := &auth.DeviceFlowOptions{
		OnAuthRequest: deviceFlowAuthPrompt(out, err, urlShown),
	}

	tok, _, derr := auth.DeviceFlow(ctx, cfg, opts)
	if derr != nil {
		return fmt.Errorf("login: device flow: %w", derr)
	}
	if tok == nil {
		return fmt.Errorf("login: device flow returned no token")
	}

	return saveDeviceFlowToken(tok, out, err)
}

func deviceFlowAuthPrompt(out, err io.Writer, urlShown chan<- struct{}) func(verificationURL, userCode string) {
	return func(verificationURL, userCode string) {
		rendered, rerr := RenderLoginPrompt(verificationURL, userCode)
		if rerr != nil {
			// Fall back to the prior fmt.Fprintf layout if the
			// renderer fails for any reason; the prompt must
			// never silently swallow the URL.
			fmt.Fprintln(out, "Open this URL in any browser:")
			fmt.Fprintf(out, "  %s\n", verificationURL)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Then enter this one-time code:")
			fmt.Fprintf(out, "  %s\n", userCode)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Waiting for you to approve…")
		} else {
			fmt.Fprint(out, rendered)
		}
		// Cycle 172: best-effort auto-open of the verification
		// URL. We honour $OPENAGY_NO_BROWSER so SSH / headless
		// callers can suppress the side-effect; --no-browser
		// would set noBrowser via the dispatchContext if it
		// were added in a future cycle.
		if BrowserAutoOpen(false) {
			if openErr := OpenBrowser(verificationURL); openErr != nil {
				fmt.Fprintf(err, "warning: open browser: %v\n", openErr)
			}
		}
		select {
		case urlShown <- struct{}{}:
		default:
		}
	}
}

func saveDeviceFlowToken(tok *oauth2.Token, out, err io.Writer) error {
	at := auth.TokenToAntigravityToken(tok)
	if serr := auth.SaveTokenUnified(at); serr != nil {
		return fmt.Errorf("login: save token: %w", serr)
	}
	// Cycle multi-account-switch: the signed-in account joins the
	// registry, so the TUI's /accounts panel can switch back to it.
	if _, _, aerr := accounts.Register(at); aerr != nil {
		fmt.Fprintf(err, "warning: register account: %v\n", aerr)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "✓ login succeeded")
	fmt.Fprintf(out, "access token: ya29...%s\n", tok.AccessToken[len(tok.AccessToken)-12:])
	if tok.RefreshToken != "" {
		fmt.Fprintf(out, "refresh token: 1//%s\n", tok.RefreshToken[len(tok.RefreshToken)-12:])
	}
	fmt.Fprintf(out, "saved to:      %s\n", auth.TokenFilePath())
	return nil
}

// ---- 3. runLogout ---------------------------------------------------------

// runLogout clears the on-disk OAuth token at the v4 binary's
// canonical path. This is the inverse of runLogin.
//
// The implementation calls os.Remove directly rather than going
// through auth.ClearToken (which doesn't exist in the cycle 41
// surface) so the file is genuinely gone, not zero-length. After
// removing, the next auth.NewTokenCache().Get() will return
// auth.ErrNoToken which is what the v4 binary does.
func runLogout(d *dispatchContext) error {
	d.printOK("logout")
	path := auth.TokenFilePath()
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(d.Out, "no token at %s (already logged out)\n", path)
			return nil
		}
		return fmt.Errorf("logout: remove %s: %w", path, err)
	}
	fmt.Fprintf(d.Out, "removed %s\n", path)
	return nil
}

// ---- 4. runAuth -----------------------------------------------------------

// runAuth prints a hint that mTLS is not used. The v1.3.5 binary does
// not use mTLS (cycle 49 close-gap: it sends the bearer token as a
// plain Authorization: Bearer header to the daily-cloudcode-pa
// endpoint). The `auth` subcommand is preserved for argv compatibility
// — it does NOT generate certs or front a proxy. Users who need
// bearer auth should set $JETSKI_OAUTH_TOKEN.
func runAuth(d *dispatchContext) error {
	d.printOK("auth")
	fmt.Fprintln(d.Out, "v"+ReferenceVersion+" binary: no mTLS at runtime.")
	fmt.Fprintln(d.Out, "Bearer token is sent as plain Authorization: Bearer header.")
	fmt.Fprintln(d.Out, "Set $JETSKI_OAUTH_TOKEN=<ya29...> and re-run.")
	if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		fmt.Fprintf(d.Out, "active token:    ya29...%s\n", tok.Token.AccessToken[len(tok.Token.AccessToken)-12:])
	} else {
		fmt.Fprintln(d.Out, "active token:    (none — populate ~/.config/antigravity/oauth-token or $JETSKI_OAUTH_TOKEN)")
	}
	return nil
}
