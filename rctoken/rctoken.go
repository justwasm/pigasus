// Package rctoken — the remote-control surface's shared secret.
//
// The remote-control surface is reachable from the network on purpose:
// `pigasus remote-control serve` and `pigasus web` both bind 0.0.0.0
// (the persistent daemon refuses a loopback bind on purpose — see
// internal/persistentrc.ValidateAddr), because the point of both
// commands is to be reached from another machine.
//
// Until cycle rc-auth-bearer-required, nothing on that surface asked
// for a credential. The only gate was the Origin allowlist, and the
// allowlist lets an *empty* Origin through — the empty Origin is what
// curl, wscat and a python client send. So any host that could reach
// the port could drive the agent: read a file, delete a conversation,
// run a turn.
//
// 【一手】 the reference's own web bundle already sends a secret on
// every request: the header `x-codeium-csrf-token`. The CLI bundle
// sets it in three places — the header builder
// (`e["x-codeium-csrf-token"]=[b]`), the Connect middleware
// (`c.header.set("x-codeium-csrf-token",a)`) and the fallback
// (`n.header.set("x-codeium-csrf-token",G9())`). The clone ships the
// reference's own bundle, so the header is already on the wire; the
// clone just never looked at it.
//
// This package owns the secret's shape:
//
//   - Generate mints one (32 random bytes, base64url, no padding).
//   - Verify compares two in constant time.
//   - Presented pulls the secret out of a request, from every channel a
//     client can use (the reference's header, the standard
//     `Authorization: Bearer`, the cookie, the query string).
//
// It also owns the marker that tells a request apart from a request
// that arrived through the cloud tunnel, so the transport and the web
// UI agree on one spelling of it.
//
// Stdlib-only. No third-party deps.
package rctoken

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

const (
	// HeaderName is the reference's own header name.
	//
	// 【一手】 the CLI bundle: `e["x-codeium-csrf-token"]=[b]`.
	// It is the channel the reference's web UI uses, so a client that
	// is the reference's own UI needs no other channel at all.
	HeaderName = "x-codeium-csrf-token"

	// EnvVar names the environment variable that pins the secret.
	//
	// A pinned secret survives a restart, so an operator who shared
	// the URL with somebody does not have to share it again. An
	// unset (or blank) variable means "mint a fresh secret".
	EnvVar = "ANTIGRAVITY_RC_TOKEN"

	// QueryParam names the query parameter a browser client can
	// carry. A browser cannot set a header on a WebSocket handshake
	// and a link is the only way an operator can hand a secret to
	// somebody else, so the URL is the browser's channel.
	QueryParam = "token"

	// CookieName is the cookie the bundled UI reads the secret from
	// when the config script does not carry it.
	//
	// 【一手】 the CLI bundle: `document.cookie.split(";").find(a=>a.startsWith("csrfToken="))`.
	CookieName = "csrfToken"

	// AuthorizationHeader is the standard header a non-browser
	// client presents the secret in.
	AuthorizationHeader = "Authorization"

	// bearerPrefix is the `Authorization` scheme this package
	// accepts, lower-cased.
	bearerPrefix = "bearer "

	// tokenBytes is the entropy of a generated secret. 32 bytes is
	// the size the web bundle's own account-switcher token uses
	// (16 bytes of hex → 32 characters); 32 bytes of base64url is
	// 43 characters.
	tokenBytes = 32

	// minTokenLen is the shortest secret this package accepts from a
	// client. It keeps a one-character guess from being a valid
	// credential and keeps a stray query parameter from being
	// mistaken for a secret.
	minTokenLen = 16
)

// Generate mints a fresh secret.
//
// The entropy comes from crypto/rand; the encoding is base64url
// without padding, so the secret survives a query string, a header
// and a JSON string without escaping.
func Generate() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("rctoken: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Verify compares an expected secret with a presented one in constant
// time.
//
// Both sides are trimmed first: a secret that travelled through a
// header may carry whitespace, and a constant-time comparison of
// different lengths leaks the length through its early return. An
// empty expected secret never verifies — "no secret configured" is
// not "every secret is correct".
func Verify(expected, presented string) bool {
	exp := strings.TrimSpace(expected)
	got := strings.TrimSpace(presented)
	if exp == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(exp), []byte(got)) == 1
}

// Presented returns the secret a request carries, or "" when it
// carries none.
//
// The channels are tried in the order a client is most likely to use:
//
//  1. the reference's own header `x-codeium-csrf-token`;
//  2. `Authorization: Bearer <secret>`;
//  3. the `csrfToken` cookie;
//  4. the `token` query parameter.
//
// The first three are set by a client that knows the secret; the
// fourth is the browser's channel. A request may carry more than one
// (the bundled UI sends the header *and* the cookie); the first
// non-empty one wins, and a wrong first channel is not masked by a
// right second one — a request that presents a wrong secret is
// wrong, whatever else it carries.
func Presented(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get(HeaderName)); v != "" {
		return v
	}
	if v := bearerToken(r.Header.Get(AuthorizationHeader)); v != "" {
		return v
	}
	if c, err := r.Cookie(CookieName); err == nil && c != nil {
		if v := strings.TrimSpace(c.Value); v != "" {
			return v
		}
	}
	return strings.TrimSpace(r.URL.Query().Get(QueryParam))
}

// bearerToken extracts the secret from an `Authorization` header
// value. The scheme is matched case-insensitively (RFC 7235 says the
// scheme is case-insensitive); a header that carries no bearer scheme
// carries no secret.
func bearerToken(header string) string {
	v := strings.TrimSpace(header)
	if len(v) <= len(bearerPrefix) {
		return ""
	}
	if !strings.EqualFold(v[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(v[len(bearerPrefix):])
}

// FromEnv returns the secret pinned by the environment, or "" when the
// variable is unset or blank.
//
// A blank value is treated as unset on purpose: an operator who
// exports `ANTIGRAVITY_RC_TOKEN=` meant "no pinned secret", not "the
// empty secret".
func FromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvVar))
}

// IsWellFormed reports whether a string has the shape of a secret this
// package mints: long enough and made only of base64url characters.
//
// It guards the path where a secret crosses into a document: the web
// UI injects the secret into the page it serves, and a secret that
// came from an untrusted query string must not be able to close the
// JavaScript string it lands in.
func IsWellFormed(s string) bool {
	if len(s) < minTokenLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// ViaRemoteControlHeader marks a request that reached the local
// server through the remote-control tunnel.
//
// The reference sets it on every proxied request, and the clone's own
// tunnel sets it too (internal/rctunnel/httpproxy.go). The constant
// lives here so the transport and the web UI read one spelling of it.
const ViaRemoteControlHeader = "X-Jetski-Via-Remote-Control"

// IsTunnelRequest reports whether a request arrived through the
// remote-control tunnel.
//
// A tunnel request is authenticated by the cloud: the WebChannel
// client carries Google's own bearer token to the relay, and the
// relay hands the request to this process in-process. There is no
// local secret to check on that path, and the request never crosses
// the network socket the secret guards.
func IsTunnelRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(ViaRemoteControlHeader)), "true")
}

// IsLoopbackPeer reports whether a request arrived from the loopback
// interface.
//
// The peer address is the only part of a request a client cannot
// forge: an `Origin` header, a `Host` header and a query parameter
// are all caller-controlled, but `r.RemoteAddr` is the socket the
// connection came from.
//
// A request that arrives through the tunnel has no peer address at
// all (it is served in-process), so it is not a loopback peer — the
// caller checks the tunnel marker first.
func IsLoopbackPeer(r *http.Request) bool {
	if r == nil {
		return false
	}
	addr := r.RemoteAddr
	if addr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Not a host:port pair (a unix socket, a test that set a bare
		// address) — try the whole string as the host.
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
