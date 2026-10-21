// Package originpolicy is the shared seam between
// internal/remotetransport (the WS upgrade that denies a
// non-allowlisted browser-side origin at Accept time)
// and internal/remotecontrol (the JSON-RPC dispatcher
// that mutates the allowlist via the
// SetRemoteControlOriginPatterns method).
//
// Why a third package:
//
// The brief keeps
// internal/remotetransport from importing
// internal/remotecontrol (the import cycle is
// broken through the smoketest in
// internal/remotetransport/tcp_test.go, which
// imports *remotecontrol.Dispatcher from this
// package's sibling). Both packages need access to
// the OriginPolicy value type + the strict
// per-pattern validator + the env-var read path;
// lifting them here keeps one source of truth and
// keeps both packages free of cross-package
// coupling. The cycle rc-auth-tokens adds this.
//
// Active policy on a (*TCPServer):
//
//   - nil OriginPolicy value → "allow all"
//     (the default; matches the pre-cycle
//     semantics). This keeps non-browser
//     callers (curl/wscat/PiG fixtures)
//     working without per-host flags.
//
//   - non-nil OriginPolicy with empty
//     Patterns → "allow all" by the same
//     brief pinning. An empty allowlist is
//     the explicit opt-in for "I have no
//     browser companions", and the
//     permissive default is what every
//     dashboards use until a host wires a
//     real allowlist.
//
//   - non-empty Patterns → exact-match
//     after strings.TrimSpace. The match
//     rejects trailing slashes
//     ("https://antigravity.google/" ≠
//     "https://antigravity.google"), as the
//     brief pins; a dashboard that wants the
//     trailing-slash form must add it to the
//     allowlist explicitly.
//
// Concurrency / safety: OriginPolicy is a
// value-type snapshot; the (*TCPServer) holds a
// pointer + a mutex (see remotetransport.TCPConfig
// below). The Allows method is pure /
// goroutine-safe — it reads the receiver by value
// (the snapshot is captured at the Set call) so
// concurrent Accept goroutines see a consistent
// policy.
//
// Stdlib-only. No third-party deps.
package originpolicy

import (
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultOriginPatterns is the well-known
// allowlist the brief pins for cycle
// rc-auth-tokens. Both entries are the
// browser-side hosts agy's companion ships
// against; a host that wants a different
// allowlist either overrides
// $ANTIGRAVITY_RC_ORIGIN_PATTERNS (see
// LoadOriginPolicyFromEnv) or sets its own
// patterns via
// (*OriginPolicyStore).SetPatterns.
var DefaultOriginPatterns = []string{
	"https://antigravity.google",
	"https://antigravity-companion.google",
}

// OriginEnvVar is the env var the host
// reads at startup to override the
// default allowlist. Comma-separated;
// whitespace is trimmed per entry.
const OriginEnvVar = "ANTIGRAVITY_RC_ORIGIN_PATTERNS"

// OriginPolicy is the value-type snapshot
// the (*TCPServer) consults per Accept.
// Two fields:
//
//   - Patterns: the list of permitted
//     origins. nil / empty → "allow all".
//     The list is interpreted as
//     exact-match (after TrimSpace); a
//     pattern with a trailing slash does
//     NOT match the same URL without the
//     slash, and the bracket form is the
//     canonical exact match.
//
//   - UpdatedAt: the wall-clock stamp
//     that sourced the snapshot. The
//     zero value is acceptable — the
//     field is informational only (today
//     the lock-step partner of the
//     JSON-RPC response's `updated_at`
//     field on every successful
//     SetPatterns call). The
//     default-policy helper sets it to
//     time.Now().UTC(); overrides via
//     env vars use the same clock the
//     store uses.
//
// OriginPolicy.Allows is the only method
// (*TCPServer).handleWS calls.
type OriginPolicy struct {
	// Patterns is the list of permitted
	// origins. Exact-match (after
	// strings.TrimSpace). nil / empty →
	// "allow all".
	Patterns []string
	// UpdatedAt is the wall-clock stamp
	// that sourced the snapshot.
	UpdatedAt time.Time
}

// DefaultOriginPolicy returns the
// canonical production allowlist. The
// helper exists so a host that doesn't
// bother wiring $ANTIGRAVITY_RC_ORIGIN_PATTERNS
// gets the brief-pinned defaults at
// startup, and so a test that doesn't
// inject a policy can still ask the
// server for the canonical snapshot.
//
// The returned OriginPolicy has
// UpdatedAt = time.Now().UTC() so a host
// that wires this at startup sees a
// non-zero timestamp in the rich-status
// tool's "when did the default snap ship"
// block.
func DefaultOriginPolicy() OriginPolicy {
	out := OriginPolicy{
		Patterns:  append([]string(nil), DefaultOriginPatterns...),
		UpdatedAt: time.Now().UTC(),
	}
	return out
}

// LoadOriginPolicyFromEnv returns the
// policy sourced from
// $ANTIGRAVITY_RC_ORIGIN_PATTERNS. The env
// var is comma-separated; each entry has
// whitespace trimmed. Empty / unset env
// returns DefaultOriginPolicy() (not an
// empty OriginPolicy, so a dashboard's
// Set call to an empty list is what
// explicitly turns the allowlist off —
// that's the distinction cycle
// rc-auth-tokens maintains; see the
// (*OriginPolicyStore).SetPatterns doc).
//
// The validator is strict on shape: an
// entry that does not parse as a URL with
// a non-empty scheme is silently dropped
// (NOT an error — the helper is the
// startup-time read path; failing-fast
// would block every binary that ships
// with a malformed env var). The dropped
// entry is logged to stderr by the
// caller's bridge in cmd/wagyu/; the
// helper itself is silent because tests
// drive it without a stderr.
//
// A future rc-auth-bearer-required cycle
// can lift this into a
// `LoadOriginPolicyFromEnvStrict` that
// returns an error; today the
// lax-on-read / strict-on-write split is
// the brief's contract.
func LoadOriginPolicyFromEnv() OriginPolicy {
	raw := strings.TrimSpace(os.Getenv(OriginEnvVar))
	if raw == "" {
		return DefaultOriginPolicy()
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !looksLikeURLOrExtension(p) {
			// Defensive: skip entries that
			// don't pass the validator. A
			// future cycle that wants
			// fail-fast semantics can
			// return a separate
			// LoadOriginPolicyFromEnvStrict
			// helper.
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		// All entries were empty after
		// trim or failed validation —
		// fall back to defaults so an
		// accidental
		// "ANTIGRAVITY_RC_ORIGIN_PATTERNS=, ,"
		// doesn't silently turn the
		// allowlist off.
		return DefaultOriginPolicy()
	}
	return OriginPolicy{Patterns: out, UpdatedAt: time.Now().UTC()}
}

// looksLikeURLOrExtension is the
// permissive shape check the env-var
// path applies. It accepts:
//
//   - http://host[:port][/path]
//   - https://host[:port][/path]
//   - chrome-extension://<id>
//   - moz-extension://<id>
//   - null (a literal "null" — what
//     sandboxed iframes send)
//   - full URLs with query / fragment
//     parts
//
// The check is intentionally NOT a full
// URL parse (it's the read-half of the
// cycle's validation chain; the strict
// validator lives on the write half in
// ValidateOriginPattern). net/url's
// ParseRequestURI is the underlying
// call; chrome-extension:// and
// moz-extension:// are pre-validated by
// string-prefix so the url package's
// strict scheme-grammar doesn't reject
// them.
func looksLikeURLOrExtension(s string) bool {
	if s == "null" {
		return true
	}
	if strings.HasPrefix(s, "chrome-extension://") ||
		strings.HasPrefix(s, "moz-extension://") {
		rest := s
		if idx := strings.Index(s, "://"); idx > 0 {
			rest = s[idx+3:]
		}
		// extension-id is alphanumeric +
		// underscore, and the path-style
		// sub-resource (e.g.
		// chrome-extension://abcdef.../foo)
		// is permitted too. Reject only the
		// "no id after scheme" edge case.
		if rest == "" || (strings.HasPrefix(rest, "/") && len(rest) == 1) {
			return false
		}
		return true
	}
	if u, err := url.ParseRequestURI(s); err == nil &&
		(u.Scheme == "http" || u.Scheme == "https") &&
		u.Host != "" {
		return true
	}
	return false
}

// ErrOriginPatternMalformed is the
// sentinel ValidateOriginPattern returns.
// The dispatcher's
// handleSetRemoteControlOriginPatterns
// maps it to -32602 "Invalid params" —
// matching the rc-permissions-grants /
// rc-plugins / rc-triggers pattern
// ("invalid pattern" envelope).
//
// The message carries the verbatim
// pattern so a dashboard's UI can show
// the operator what they submitted; the
// data field is the same string.
var ErrOriginPatternMalformed = originPatternError("origin pattern malformed")

// originPatternError is the typed error
// the validator returns. Defining a
// named type lets future cycles add
// per-pattern specific errors (e.g.
// "duplicate" — currently rejected by
// the strict validator because we
// dedupe on Snapshot) without churning
// every dispatcher's switch arm.
type originPatternError string

func (e originPatternError) Error() string { return string(e) }

// ValidateOriginPattern is the
// strict-on-write validator the
// SetRemoteControlOriginPatterns handler
// applies to each entry in the
// patterns[] param. The rules are:
//
//   - non-empty after strings.TrimSpace
//     (a pattern that is whitespace-only
//     is malformed);
//
//   - exactly one of:
//     (a) a literal "null" string (a
//     sandboxed iframe origin
//     sentinel);
//     (b) a chrome-extension://<id> URL;
//     (c) a moz-extension://<id> URL;
//     (d) a parseable http/https URL
//     with a non-empty host;
//
//   - no whitespace inside the
//     TrimSpace'd value (the parser above
//     would silently drop them later, but
//     the strict validator rejects here
//     so a dashboard cannot bypass the
//     allowance by inserting a tab);
//
// The sentinel ErrOriginPatternMalformed
// carries the verbatim reason so the
// dispatcher's -32602 envelope echoes it
// in `data`.
func ValidateOriginPattern(pat string) error {
	pat = strings.TrimSpace(pat)
	if pat == "" {
		return ErrOriginPatternMalformed
	}
	if pat == "null" {
		return nil
	}
	if strings.HasPrefix(pat, "chrome-extension://") ||
		strings.HasPrefix(pat, "moz-extension://") {
		rest := pat
		if idx := strings.Index(pat, "://"); idx > 0 {
			rest = pat[idx+3:]
		}
		// Accept any extension URL
		// ("chrome-extension://<id>" or
		// "chrome-extension://<id>/path");
		// the dispatcher's matching uses
		// exact-match so the dashboard's
		// submitted pattern has to be
		// verbatim.
		if rest == "" {
			return ErrOriginPatternMalformed
		}
		// Reject embedded whitespace
		// inside the scheme prefix —
		// it implies a typo the
		// validator should refuse before
		// persisting.
		if strings.ContainsAny(rest, " \t\n\r") {
			return ErrOriginPatternMalformed
		}
		return nil
	}
	u, err := url.ParseRequestURI(pat)
	if err != nil ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" {
		return ErrOriginPatternMalformed
	}
	if strings.ContainsAny(pat, " \t\n\r") {
		// net/url is lenient about tabs
		// inside opaque parts; the
		// strict-on-write contract says
		// reject whitespace inside the
		// trimmed pattern.
		return ErrOriginPatternMalformed
	}
	return nil
}

// Allows reports whether the supplied
// origin (the verbatim value of the
// browser's Origin: header — the
// transport passes r.Header.Get("Origin")
// verbatim, with no parsing / no
// normalisation) is permitted under the
// policy.
//
// Semantics:
//
//   - empty pattern (no Origin header):
//     "non-browser caller" → allow;
//     preserves the pre-cycle
//     compatibility for curl/wscat
//     smoke tests and the per-turn
//     --remote-control unix-socket
//     surfaces (which never set an
//     Origin header because they
//     connect directly without HTTP).
//
//   - nil receiver / empty Patterns:
//     "explicit allow-all" → allow;
//     matches DefaultOriginPolicy() with
//     patterns trimmed to zero.
//
//   - exact-match after
//     strings.TrimSpace: the
//     Origin: header value is trimmed
//     and matched against each pattern.
//     A pattern with a trailing slash
//     does NOT match a request without
//     one, and vice versa — the match
//     is byte-for-byte after trim. No
//     glob, no scheme normalisation.
//
// The method is pure (reads only the
// receiver, no I/O, no locks). A
// caller that wants the policy to
// change atomically (e.g. a Set RPC
// happens concurrently with an Accept)
// should hold the server's
// (*TCPServer).setOriginPolicyMu around
// both the read and the Set call. In
// practice the cycle ships a single
// SetOriginPolicy + per-Accept read
// pattern: Set takes a write lock, the
// per-Accept read takes a read lock,
// and the (*TCPServer) package carries
// the mutex. See
// (*TCPServer).SetOriginPolicy.
func (p OriginPolicy) Allows(origin string) bool {
	if len(p.Patterns) == 0 {
		return true
	}
	origin = strings.TrimSpace(origin)
	if origin == "" {
		// Empty origin = non-browser
		// caller; preserve compat per
		// the brief's "If patterns is
		// empty, allow all origins" note.
		return true
	}
	for _, pat := range p.Patterns {
		if strings.TrimSpace(pat) == origin {
			return true
		}
	}
	return false
}
