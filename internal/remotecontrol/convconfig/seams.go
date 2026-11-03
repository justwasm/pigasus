// Package convconfig — seams.go: what the host hands the
// conversation-config domain.
//
// The domain owns the store, the validation rules, the wire
// shape of the one method that talks to it, and the
// notification it fans out on a change. It does not own the
// transport that carries the notification, or the dispatcher
// that routes the method — those are the host's.
//
// Two seams, each a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the
//     package declares).
//   - Broadcast fans a notification out to every open
//     connection. nil is a no-op: an in-process caller with no
//     transport still gets the mutation, and the response
//     envelope echoes the post-state, so polling is always
//     possible.
//
// Stdlib-only. No third-party deps.
package convconfig

import (
	"context"
)

// Seams are the host-supplied dependencies of the
// conversation-config domain.
type Seams struct {
	// Store is the conversation-config store. A nil Store
	// makes the domain's method answer -32603 ("no
	// conversation-config store installed").
	Store Store

	// Broadcast fans a notification out to every open
	// connection. nil is a no-op.
	Broadcast func(ctx context.Context, method string, params any) error
}
