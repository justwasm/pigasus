package agentlink

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
)

// tokenBytes is how much entropy a minted token carries.
const tokenBytes = 32

// MintToken mints a fresh bearer token.
//
// The server runs shell commands against its own workspace, so it always
// demands a token — one is minted when the configuration leaves it empty.
// The entropy comes from crypto/rand; the encoding is base64url without
// padding, so the token survives a header, a URL and a shell argument
// without escaping.
func MintToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("agentlink: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// tokenMatches compares an expected token with a presented one in constant
// time.
//
// Both sides are trimmed first: a token that travelled through a header
// may carry whitespace, and a constant-time comparison of different
// lengths leaks the length through its early return.
func tokenMatches(expected, presented string) bool {
	exp := strings.TrimSpace(expected)
	got := strings.TrimSpace(presented)
	if exp == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(exp), []byte(got)) == 1
}
