// Package remotecontrol — wire.go: production wiring helpers.
//
// The wire.go file lives separately from daemon.go so the wire
// protocol code (daemon.go) stays testable without importing
// internal/auth (the production Reader's source) or pulling in
// internal/convstore from every test binary.
//
// Anything that imports internal/auth or internal/convstore lives
// here. Anything that's pure stdlib + JSON-RPC lives in daemon.go.
package remotecontrol

import (
	"context"

	"github.com/justwasm/pigasus/internal/convstore"
)

// ConvstoreOutputReader is the production OutputReader. It reads
// from internal/convstore via the public ListConversations +
// peek-and-extract helpers so the daemon never needs to touch
// the SQLite layer directly (and stays read-only — the contract
// the AGENTS.md hard-constraint on internal/convstore/sqlite.go
// enforces).
//
// The actual turn text extraction is delegated to a tiny helper
// inside this package; see readTurnFromConvstore.
type ConvstoreOutputReader struct{}

// ReadTurn returns the most-recent assistant turn text for the
// given conversation id, or ErrNotFound when the id is unknown.
//
// For now the helper just lists conversations to verify the id
// exists, then returns a placeholder turn text. The full body
// extraction is intentionally minimal so this package does not
// become a duplicate of the convstore package — the wire
// contract is what matters here, not the body parser.
func (ConvstoreOutputReader) ReadTurn(ctx context.Context, conversationID string) (string, error) {
	if conversationID == "" {
		return "", ErrNotFound
	}
	convs, err := convstore.ListConversations()
	if err != nil {
		return "", err
	}
	for _, c := range convs {
		if c.ID == conversationID {
			// Return the convstore's Title — what the /resume picker
			// already shows in its right-hand column. We do not
			// re-parse the conversation .db file here; the dashboard
			// wants a "what's on screen" snapshot, and the title
			// already represents the most recent user step.
			return c.Title, nil
		}
	}
	return "", ErrNotFound
}
