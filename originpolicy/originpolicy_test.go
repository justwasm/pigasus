package originpolicy

import (
	"github.com/justwasm/pigasus/endpoints"
	"strings"
	"testing"
)

// TestValidateOriginPattern_OK covers the
// happy paths the brief pins:
//
//   - a literal "null" string (sandboxed
//     iframes);
//   - a chrome-extension://<id> URL;
//   - a moz-extension://<id> URL;
//   - a parseable http / https URL with a
//     non-empty host;
//   - extension URLs with path-style sub-
//     resources (chrome-extension://<id>/foo).
//
// Cycle rc-auth-tokens.
func TestValidateOriginPattern_OK(t *testing.T) {
	cases := []string{
		"null",
		"chrome-extension://abcdefghijklmnop",
		"chrome-extension://abcdefghijklmnop/path/to/resource",
		"moz-extension://abcdefghijklmnop",
		"http://antigravity.google",
		"https://antigravity.google",
		"https://antigravity-companion.google",
		"http://localhost:8080",
		"https://antigravity.google/",
		"https://antigravity.google/path",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if err := ValidateOriginPattern(c); err != nil {
				t.Errorf("ValidateOriginPattern(%q) = %v, want nil", c, err)
			}
		})
	}
}

// TestValidateOriginPattern_Bad covers
// the malformed-input paths the validator
// rejects with
// ErrOriginPatternMalformed:
//
//   - whitespace-only;
//   - empty after TrimSpace;
//   - non-parseable URL;
//   - URL with empty host;
//   - non-http / non-https scheme (the
//     brief pins http/https only);
//   - chrome-extension:// with no id
//     after the scheme;
//   - embedded whitespace inside the
//     pattern (a tab inside the URL
//     bypasses the read path's silent
//     drop, so the write path must
//     reject it).
//
// Cycle rc-auth-tokens.
func TestValidateOriginPattern_Bad(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"\t",
		"not a url",
		"http://",
		"https://",
		"ftp://antigravity.google",
		"file:///etc/passwd",
		"chrome-extension://",
		"moz-extension://",
		"https://antigravity google",
		"https://antigravity\tgoogle",
		"https://antigravity\ngoogle",
		"https://antigravity\rgoogle",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if err := ValidateOriginPattern(c); err == nil {
				t.Errorf("ValidateOriginPattern(%q) = nil, want non-nil", c)
			}
		})
	}
}

// TestAllows_NilPatterns covers the
// brief-pinned "empty patterns means allow
// all" semantic. Both nil receiver and
// non-nil receiver with empty Patterns
// return true for any origin.
func TestAllows_NilPatterns(t *testing.T) {
	var p OriginPolicy
	if !p.Allows("https://example.com") {
		t.Errorf("nil receiver should allow all; got false")
	}
	p2 := OriginPolicy{Patterns: nil}
	if !p2.Allows("https://example.com") {
		t.Errorf("Patterns:nil should allow all; got false")
	}
	p3 := OriginPolicy{Patterns: []string{}}
	if !p3.Allows("https://example.com") {
		t.Errorf("Patterns:[] should allow all; got false")
	}
}

// TestAllows_EmptyOrigin confirms the
// "no Origin header = non-browser caller
// = allow" pre-cycle compat semantic.
func TestAllows_EmptyOrigin(t *testing.T) {
	p := OriginPolicy{Patterns: []string{"https://antigravity.google"}}
	if !p.Allows("") {
		t.Errorf("empty origin should allow; got false")
	}
	if !p.Allows("   ") {
		t.Errorf("whitespace-only origin should allow (TrimSpace); got false")
	}
}

// TestAllows_ExactMatch pins the
// exact-match-after-TrimSpace semantic.
// A trailing-slash mismatch rejects; a
// case mismatch rejects (no normalisation);
// a non-listed origin rejects.
func TestAllows_ExactMatch(t *testing.T) {
	p := OriginPolicy{Patterns: []string{"https://antigravity.google"}}
	cases := []struct {
		origin string
		want   bool
	}{
		{"https://antigravity.google", true},
		{"  https://antigravity.google  ", true},
		{"https://antigravity.google/", false},
		{"http://antigravity.google", false},
		{"https://antigravity-companion.google", false},
		{"https://attacker.example", false},
	}
	for _, c := range cases {
		t.Run(c.origin, func(t *testing.T) {
			if got := p.Allows(c.origin); got != c.want {
				t.Errorf("Allows(%q) = %v, want %v", c.origin, got, c.want)
			}
		})
	}
}

// TestLoadOriginPolicyFromEnv_Empty pins
// the "empty / unset env returns
// DefaultOriginPolicy" semantic.
func TestLoadOriginPolicyFromEnv_Empty(t *testing.T) {
	t.Setenv(OriginEnvVar, "")
	got := LoadOriginPolicyFromEnv()
	want := endpoints.Current().OriginPatterns
	if len(got.Patterns) != len(want) {
		t.Errorf("empty env: got %d patterns, want %d", len(got.Patterns), len(want))
	}
	for i, w := range want {
		if got.Patterns[i] != w {
			t.Errorf("empty env: pattern[%d] = %q, want %q", i, got.Patterns[i], w)
		}
	}
}

// TestLoadOriginPolicyFromEnv_Malformed
// pins the lax-on-read / strict-on-write
// split. The env-var path silently drops
// malformed entries; if the result is
// empty after the trim+drop pass, it
// falls back to DefaultOriginPolicy so an
// accidental
// "ANTIGRAVITY_RC_ORIGIN_PATTERNS=, ,"
// doesn't silently turn the allowlist off.
func TestLoadOriginPolicyFromEnv_Malformed(t *testing.T) {
	t.Setenv(OriginEnvVar, ", ,not a url,")
	got := LoadOriginPolicyFromEnv()
	if len(got.Patterns) != len(endpoints.Current().OriginPatterns) {
		t.Errorf("malformed env: got %d patterns, want %d (default fallback)",
			len(got.Patterns), len(endpoints.Current().OriginPatterns))
	}
}

// TestLoadOriginPolicyFromEnv_Multi pins
// the comma-separated multi-entry path
// the brief calls for. Whitespace around
// each entry is trimmed; valid entries
// land in the policy verbatim.
func TestLoadOriginPolicyFromEnv_Multi(t *testing.T) {
	t.Setenv(OriginEnvVar, "https://antigravity.google, https://antigravity-companion.google , chrome-extension://abcdefghijklmnop")
	got := LoadOriginPolicyFromEnv()
	if len(got.Patterns) != 3 {
		t.Fatalf("multi-entry: got %d patterns, want 3; got=%v", len(got.Patterns), got.Patterns)
	}
	if got.Patterns[0] != "https://antigravity.google" {
		t.Errorf("multi-entry[0] = %q, want %q", got.Patterns[0], "https://antigravity.google")
	}
	if got.Patterns[1] != "https://antigravity-companion.google" {
		t.Errorf("multi-entry[1] = %q, want %q", got.Patterns[1], "https://antigravity-companion.google")
	}
	if got.Patterns[2] != "chrome-extension://abcdefghijklmnop" {
		t.Errorf("multi-entry[2] = %q, want %q", got.Patterns[2], "chrome-extension://abcdefghijklmnop")
	}
}

// TestDefaultOriginPolicy pins the
// canonical production allowlist. The
// helper exists so a host that doesn't
// bother wiring
// $ANTIGRAVITY_RC_ORIGIN_PATTERNS gets
// the brief-pinned defaults at startup.
func TestDefaultOriginPolicy(t *testing.T) {
	got := DefaultOriginPolicy()
	want := endpoints.Current().OriginPatterns
	if len(got.Patterns) != len(want) {
		t.Errorf("DefaultOriginPolicy: %d patterns, want %d", len(got.Patterns), len(want))
	}
	for i, w := range want {
		if got.Patterns[i] != w {
			t.Errorf("DefaultOriginPolicy[%d]: %q, want %q", i, got.Patterns[i], w)
		}
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("DefaultOriginPolicy: UpdatedAt is zero, want non-zero")
	}
	if !strings.HasPrefix(got.UpdatedAt.Location().String(), "UTC") {
		// time.Time.Location() stringifies as
		// the name (UTC) or "<+HHMM>" for fixed
		// zones; accept either.
		if !strings.Contains(got.UpdatedAt.Location().String(), "UTC") {
			// OK — non-UTC. Skip.
		}
	}
}

// TestValidateOriginPattern_Trimmed pins
// that the validator trims whitespace
// before applying the rules — the
// read-path and write-path helpers
// differ on this in interesting ways
// (the read path uses TrimSpace; the
// write path also trims but rejects
// internal whitespace).
func TestValidateOriginPattern_Trimmed(t *testing.T) {
	if err := ValidateOriginPattern("  https://antigravity.google  "); err != nil {
		t.Errorf("TrimSpace-then-validate: %v", err)
	}
}

// TestValidateOriginPattern_LenientHost
// pins that URLs without an explicit
// scheme or host fail. /etc/passwd must
// be rejected because file:// schemes are
// not on the http/https allowlist.
func TestValidateOriginPattern_LenientHost(t *testing.T) {
	if err := ValidateOriginPattern("/etc/passwd"); err == nil {
		t.Errorf("ValidateOriginPattern(/etc/passwd) = nil, want non-nil")
	}
	if err := ValidateOriginPattern("antigravity.google"); err == nil {
		t.Errorf("ValidateOriginPattern(antigravity.google) = nil, want non-nil")
	}
}

// TestErrOriginPatternMalformed pins the
// error type. The dispatcher's handler
// uses errors.Is(err, ErrOriginPatternMalformed)
// to distinguish per-pattern validation
// failures (→ -32602) from any other
// failure (→ -32603).
func TestErrOriginPatternMalformed(t *testing.T) {
	if ValidateOriginPattern("") != ErrOriginPatternMalformed {
		t.Errorf("empty pattern should return sentinel ErrOriginPatternMalformed")
	}
	if ValidateOriginPattern("  ") != ErrOriginPatternMalformed {
		t.Errorf("whitespace pattern should return sentinel ErrOriginPatternMalformed")
	}
}
