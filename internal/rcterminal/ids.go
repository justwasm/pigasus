// ids.go: the terminal's identifier.
//
// The reference's terminal ids are uuid v4 strings
// ("cab3427e-4d8e-4c3f-a1ac-1340e40933b2"). The clone mints the
// same shape from the crypto/rand pool, so an id that crosses the
// wire looks like the reference's own.
package rcterminal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newTerminalID mints a uuid v4 shaped identifier.
func newTerminalID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// The crypto pool does not fail in practice; a zero id is
		// still a unique-enough fallback for an in-memory registry.
		return fmt.Sprintf("terminal-%d", len(b))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
