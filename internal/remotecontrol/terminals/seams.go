// Package terminals — seams.go: what the host hands the
// terminals domain.
//
// The domain owns the terminal registry, the wire shape of the
// five dashboard methods and the notifications it fans out on a
// change. It does not own the cascade registry, the transport
// that carries the notifications, or the dispatcher that routes
// the methods — those are the host's.
//
// Three seams, each a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the package
//     declares). A nil Store makes the domain's methods answer
//     -32603 ("no terminals registry installed").
//   - Cascades resolves a cascade id to the narrow Cascade view
//     the domain needs — the existence check Create runs and the
//     output chan the terminal attaches to. The host's cascade
//     registry satisfies it through a one-line adapter; the
//     domain never learns what a cascade is beyond that.
//   - Broadcast fans a notification out to every open connection.
//     nil is a no-op: an in-process caller with no transport still
//     gets the mutation, and the response envelope echoes the
//     post-state, so polling is always possible.
//
// Stdlib-only. No third-party deps.
package terminals

import (
	"context"
)

// Cascade is the narrow view of a live cascade the terminals
// domain needs: the existence check CreateRemoteAgentTerminal
// runs, and the output chan a cascade-bound terminal attaches to.
//
// The domain declares the interface it needs rather than
// importing the host's cascade type — the dependency runs from
// the domain to the host, never the other way round.
type Cascade interface {
	// TerminalOutput returns the cascade's text-delta chan. A
	// nil chan means the cascade streams nothing.
	TerminalOutput() <-chan []byte
}

// Seams are the host-supplied dependencies of the terminals
// domain.
type Seams struct {
	// Store is the terminal registry. A nil Store makes the
	// domain's methods answer -32603 ("no terminals registry
	// installed").
	Store Store

	// Cascades resolves a cascade id to the narrow Cascade view.
	// A nil Cascades (or an id the host does not know) answers
	// -32004 "Cascade not found".
	Cascades func(id string) (Cascade, bool)

	// Broadcast fans a notification out to every open connection.
	// nil is a no-op.
	Broadcast func(ctx context.Context, method string, params any) error
}
