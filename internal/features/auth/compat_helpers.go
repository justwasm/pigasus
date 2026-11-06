// compat_helpers.go — small UUID helper for installation_id
package auth

import (
	"uuid"
)

// newUUIDv4 returns a RFC 4122 v4 UUID string.
func newUUIDv4() (string, error) {
	return uuid.NewV4().String(), nil
}
