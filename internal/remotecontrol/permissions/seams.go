// Package permissions — seams.go: what the host hands the
// permissions domain.
//
// The domain owns the permission state, the store, the reference's
// permission vocabulary, the wire shape of the four dashboard
// methods, the wire shape of the two language-server methods and
// the notification it fans out on a change. It does not own the
// transport that carries the notification, or the dispatcher that
// routes the methods — those are the host's.
//
// Two seams, each a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the package
//     declares).
//   - Broadcast fans a notification out to every open connection.
//     nil is a no-op: an in-process caller with no transport still
//     gets the mutation, and the response envelope echoes the
//     post-state, so polling is always possible.
//
// Stdlib-only. No third-party deps.
package permissions

import (
	"context"
)

// Seams are the host-supplied dependencies of the permissions
// domain.
type Seams struct {
	// Store is the permission store. A nil Store makes the
	// domain's methods answer -32603 ("no permission store
	// installed").
	Store Store

	// Broadcast fans a notification out to every open connection.
	// nil is a no-op.
	Broadcast func(ctx context.Context, method string, params any) error
}
