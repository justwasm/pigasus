// compat_helpers.go — small UUID helper for installation_id
package auth

import (
	"fmt"

	"github.com/google/uuid"
)

// newUUIDv4 returns a RFC 4122 v4 UUID string.
func newUUIDv4() (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return u.String(), nil
}
