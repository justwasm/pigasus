// Package plugins — seams.go: what the host hands the plugins
// domain.
//
// The domain owns the plugin registry, the store, the wire shape
// of the two dashboard methods and the notification it fans out on
// a disconnect. It does not own the transport that carries the
// notification, or the dispatcher that routes the methods — those
// are the host's.
//
// Two seams, each a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the package
//     declares). A nil Store makes the domain's methods answer
//     -32603 ("no plugin store installed").
//   - Broadcast fans a notification out to every open connection.
//     nil is a no-op: an in-process caller with no transport still
//     gets the mutation, and the response envelope echoes the
//     post-state, so polling is always possible.
//
// Stdlib-only. No third-party deps.
package plugins

import (
	"context"
)

// Seams are the host-supplied dependencies of the plugins domain.
type Seams struct {
	// Store is the plugin store. A nil Store makes the domain's
	// methods answer -32603 ("no plugin store installed").
	Store Store

	// Broadcast fans a notification out to every open connection.
	// nil is a no-op.
	Broadcast func(ctx context.Context, method string, params any) error
}
