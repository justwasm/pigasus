// Package rcfrontend — token.go: the secret the web UI hands out,
// and the page it refuses to hand it to.
//
// Cycle rc-auth-bearer-required.
//
// The web UI is the reference's own bundle (internal/rcfrontend/
// assets/main.js), and that bundle reads its secret from
// `window.__APP_CONFIG__.csrfToken` and sends it on every request
// under the header `x-codeium-csrf-token`:
//
//	【一手】 the CLI bundle: `e["x-codeium-csrf-token"]=[b]`.
//
// So the page is where the secret travels. Two rules follow:
//
//  1. the page a *remote* peer gets must not carry the secret —
//     otherwise the secret is public to exactly the peers the
//     transport's gate turns away;
//  2. the page a peer that already knows the secret gets may carry
//     it — the peer proved it knows it.
//
// The rules are:
//
//   - a loopback peer is handed the secret (it is the operator's own
//     machine, and it is the machine that holds the secret);
//   - a tunnel request is handed the secret (the cloud authenticated
//     the request);
//   - a remote peer is handed the secret it already presented in the
//     query string — and only if that secret is the right one;
//   - a remote peer that presented nothing gets a page that says so,
//     not a page with a broken dashboard.
package rcfrontend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"

	"github.com/justwasm/pigasus/rctoken"
)

// csrfTokenPlaceholder is the marker the cached page carries where the
// secret goes. The page is cached across requests — the touch patches
// are expensive to apply — and the secret is per-request, so the
// cached bytes hold a placeholder and the real value is substituted on
// the way out.
const csrfTokenPlaceholder = "__PIGASUS_CSRF_TOKEN__"

// tokenForRequest returns the secret the page for this request
// carries, or "" when the page must not carry one.
//
// The rules are the ones the file header states. A remote peer is
// handed back the secret it presented — in the query string, in the
// cookie the transport handed it, or in the reference's own header —
// and only when that secret is the server's own, so a wrong guess is
// not laundered into the page.
func (s *Server) tokenForRequest(r *http.Request) string {
	if s.opts.CSRFToken == "" {
		return ""
	}
	if rctoken.IsTunnelRequest(r) {
		return s.opts.CSRFToken
	}
	if rctoken.IsLoopbackPeer(r) {
		return s.opts.CSRFToken
	}
	presented := rctoken.Presented(r)
	if presented != "" && rctoken.Verify(s.opts.CSRFToken, presented) {
		return presented
	}
	return ""
}

// pageAllowed reports whether the page may be served to this request.
//
// A server without a secret serves the page to everybody (the
// pre-cycle behaviour). A server with a secret serves it to the
// loopback, to the tunnel, and to a peer that presented the secret.
func (s *Server) pageAllowed(r *http.Request) bool {
	if s.opts.CSRFToken == "" {
		return true
	}
	if rctoken.IsTunnelRequest(r) {
		return true
	}
	if rctoken.IsLoopbackPeer(r) {
		return true
	}
	return s.tokenForRequest(r) != ""
}

// applyRequestToken substitutes the request's secret into the cached
// page.
//
// The substitution is a plain byte replacement: the placeholder is
// inside a JavaScript string literal, and the value that goes in is
// either the server's own secret (base64url) or a query parameter
// that was verified against it — so it cannot close the literal.
func (s *Server) applyRequestToken(cached []byte, r *http.Request) []byte {
	token := s.tokenForRequest(r)
	return bytes.Replace(cached, []byte(csrfTokenPlaceholder), []byte(token), -1)
}

// etagFor returns the ETag of a page body.
//
// The ETag is computed over the *substituted* body, because the body
// now differs per request: a cached body with a stale secret would be
// served as a 304 to a peer that presented a different one.
func etagFor(body []byte) string {
	h := sha256.Sum256(body)
	return `"` + hex.EncodeToString(h[:8]) + `"`
}

// writeTokenRequired answers a page request that may not be served.
//
// The body says what to do rather than showing a dashboard that
// cannot talk to the server: the peer has to come back with the
// secret in the query string.
func writeTokenRequired(w http.ResponseWriter, r *http.Request) {
	host := ""
	if r != nil {
		host = r.Host
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, tokenRequiredPage, html.EscapeString(host))
}

// tokenRequiredPage is the page a peer without the secret gets.
//
// It is a constant with one %s — the request's host — so the operator
// can copy the URL and paste the secret into it.
const tokenRequiredPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Remote control — token required</title>
</head>
<body style="font-family: system-ui, sans-serif; max-width: 38rem; margin: 4rem auto; line-height: 1.5">
<h1>Token required</h1>
<p>This remote-control instance only serves its dashboard to a client that knows the secret.</p>
<p>Open <code>http://%s/?token=&lt;secret&gt;</code> &mdash; the secret is the one the server printed when it started.</p>
</body>
</html>
`
