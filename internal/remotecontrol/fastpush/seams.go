// Package fastpush — seams.go: what the host hands the
// fastpush domain.
//
// The domain owns the store, the wire shape of the one method
// that talks to it, and the notification it fans out on a
// change. It does not own the cascade registry, the transport
// that carries the notification, or the dispatcher that routes
// the method — those are the host's.
//
// Three seams, each a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the
//     package declares).
//   - Cascades resolves a cascade id to the narrow Cascade
//     view the domain needs — the toggle it flips. The host's
//     cascade registry satisfies it through a one-line adapter;
//     the domain never learns what a cascade is beyond the
//     toggle.
//   - Broadcast fans a notification out to every open
//     connection. nil is a no-op: an in-process caller with no
//     transport still gets the mutation, and the response
//     envelope echoes the post-state, so polling is always
//     possible.
//
// Stdlib-only. No third-party deps.
package fastpush

import (
	"context"
)

// Cascade is the narrow view of a live cascade the fastpush
// domain needs: the per-cascade push toggle it flips.
//
// The domain declares the interface it needs rather than
// importing the host's cascade type — the dependency runs from
// the domain to the host, never the other way round.
type Cascade interface {
	// SetFastpush flips the cascade's push toggle.
	SetFastpush(enabled bool)
}

// Seams are the host-supplied dependencies of the fastpush
// domain.
type Seams struct {
	// Store is the fastpush store. A nil Store makes the
	// domain's method answer -32603 ("no fastpush store
	// installed").
	Store Store

	// Cascades resolves a cascade id to the narrow Cascade
	// view. A nil Cascades (or an id the host does not know)
	// answers -32004 "Cascade not found".
	Cascades func(id string) (Cascade, bool)

	// Broadcast fans a notification out to every open
	// connection. nil is a no-op.
	Broadcast func(ctx context.Context, method string, params any) error
}
