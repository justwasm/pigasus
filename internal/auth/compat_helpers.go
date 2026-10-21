// compat_helpers.go — small UUID helper for installation_id
package auth

import (
	"crypto/rand"
	"fmt"
)

// newUUIDv4 returns a RFC 4122 v4 UUID string. We avoid pulling
// in the google/uuid dep by hand-rolling 16 random bytes + version
// + variant nibble adjustment.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
