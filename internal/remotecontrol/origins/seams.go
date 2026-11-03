// Package origins — seams.go: what the host hands the
// origin-policy domain.
//
// The domain owns the store, the wire shape of the one method
// that talks to it, and the notification it fans out on a
// change. It does not own the transport that carries the
// notification, the policy the transport enforces at Accept
// time, or the dispatcher that routes the method — those are the
// host's.
//
// Three seams, each a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the
//     package declares).
//   - Broadcast fans a notification out to every open
//     connection. nil is a no-op: an in-process caller with no
//     transport still gets the mutation, and the response
//     envelope echoes the post-state, so polling is always
//     possible.
//   - SetPolicy pushes the accepted patterns into the
//     transport's live snapshot, so an immediately-following
//     Accept sees the new allowlist. nil is a no-op.
//
// Stdlib-only. No third-party deps.
package origins

import (
	"context"

	"github.com/justwasm/pigasus/originpolicy"
)

// Seams are the host-supplied dependencies of the origin-policy
// domain.
type Seams struct {
	// Store is the origin-policy store. A nil Store makes the
	// domain's method answer -32603 ("no origin-policy store
	// installed").
	Store Store

	// Broadcast fans a notification out to every open
	// connection. nil is a no-op.
	Broadcast func(ctx context.Context, method string, params any) error

	// SetPolicy pushes the accepted patterns into the
	// transport's live snapshot. nil is a no-op.
	SetPolicy func(policy originpolicy.OriginPolicy)
}
