package agyprompt

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewConversationID returns an RFC-4122-style 8-4-4-4-12 hex UUID, the shape
// agy writes into <user_information>. It is the same generator cmd/wagyu uses
// for the /help panel (package main cannot be imported, so it is restated).
func NewConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		ts := uint64(time.Now().UnixNano())
		for i := 0; i < 16; i++ {
			b[i] = byte(ts >> (uint(i) * 4))
		}
	}
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
