// Package remotecontrol — notify.go: server-push JSON-RPC
// notifications on the dispatcher's surface.
//
// Cycle rc-streaming-protocol adds the dispatcher half of
// server-push: Dispatcher.Notify marshals a JSON-RPC 2.0
// notification frame (no "id" field, per spec §4.1) and hands
// the bytes to a transport that the caller registered with
// SetNotifier. The transport routes the frame to the connection
// identified by connID; the dispatcher is transport-agnostic.
//
// Wire format (one frame per call):
//
//	{ "jsonrpc": "2.0", "method": "<name>", "params": {…} }
//
// — note the absence of "id". That is the JSON-RPC 2.0 signal
// that no reply is expected (spec §4.1, "Notification"). The
// receiving peer MUST NOT respond.
//
// Why a callback (rather than the dispatcher holding connection
// pointers directly): remotetransport does NOT import
// remotecontrol (the constraint that breaks the import cycle
// through remotetransport/tcp_test.go), so the dispatcher
// cannot know the concrete type of a connection. A function
// callback is the boundary: the transport constructs a closure
// that captures its own connection registry, hands the closure
// to SetNotifier, and the dispatcher never sees the underlying
// type.
package remotecontrol

import (
	"context"
	"encoding/json"
)

// Notifier is the callback Dispatcher.Notify uses to push a
// JSON-RPC frame to a specific connection. connID is opaque to
// the dispatcher — it's whatever the transport handed out at
// accept time. frame is a single already-marshalled JSON value
// (one ws message for the WebSocket transport; one '\n'-
// delimited line for the Unix-socket transport — though only
// the WebSocket transport supports server-push today).
//
// A nil return means the frame was delivered. A non-nil error
// means the connection is gone (closed by the peer, write
// timed out, connID unknown, etc.) — the dispatcher does NOT
// retry; the caller is expected to drop the connection's
// server-push subscriptions out-of-band.
//
// Notifier is safe to call concurrently; the dispatcher takes
// a snapshot of the registered callback under its own mutex
// before invoking it.
//
// Declared as a type alias (not a named type) so the
// remotetransport Dispatcher interface can declare SetNotifier
// with the inline signature `func(ctx context.Context, connID
// string, frame []byte) error` and *remotecontrol.Dispatcher
// satisfies it with `SetNotifier(n Notifier)`. A named type
// would NOT match the interface method (Go requires parameter
// types to be identical at the type-identity level, and named
// function types are distinct from their unnamed underlying
// type). remotetransport can't import remotecontrol (cycle
// through tcp_test.go), so we can't share a named type either
// way; the alias is the cleanest split.
type Notifier = func(ctx context.Context, connID string, frame []byte) error

// SetNotifier registers (or clears, when n is nil) the callback
// Notify uses to deliver frames. The dispatcher takes a
// reference to n; the caller must keep n's closure (and any
// state it captures) alive for as long as server-push
// notifications might be sent.
//
// SetNotifier is typically called once, by the transport's
// constructor. TCPServer does this in NewTCPServer so a future
// turn-text-emitter can stream model chunks to connected
// dashboards via the dispatcher.
//
// SetNotifier is safe to call concurrently with Notify; an
// in-flight Notify call uses the callback that was registered
// when it started.
func (d *Dispatcher) SetNotifier(n Notifier) {
	if d == nil {
		return
	}
	d.notifyMu.Lock()
	d.notify = n
	d.notifyMu.Unlock()
}

// Notifier returns the currently-registered notifier callback.
// Exposed so tests can assert the wiring without poking at the
// dispatcher's internals.
func (d *Dispatcher) Notifier() Notifier {
	if d == nil {
		return nil
	}
	d.notifyMu.RLock()
	defer d.notifyMu.RUnlock()
	return d.notify
}

// Notify pushes one JSON-RPC notification frame to the
// connection identified by connID. Wire format:
//
//	{ "jsonrpc": "2.0", "method": "<name>", "params": {…} }
//
// (no "id" — the JSON-RPC 2.0 signal that no reply is expected.)
//
// Notify is best-effort: if the notifier returns an error
// (connection closed, write timed out, unknown connID, etc.),
// the error is returned to the caller and the dispatcher does
// NOT retry. A slow client can therefore pin the server's
// write goroutine; a follow-up cycle ("rc-streaming-protocol-
// buffered") should add a per-conn buffered channel with a
// quota so the server can drop slow clients instead of
// blocking on them.
//
// If no notifier is registered (SetNotifier was never called,
// or it was set to nil), Notify returns nil — there's nobody
// to send to. This keeps Notify usable from contexts where the
// transport doesn't support server-push (the Unix-socket
// transport).
func (d *Dispatcher) Notify(ctx context.Context, connID, method string, params any) error {
	if d == nil {
		return nil
	}
	d.notifyMu.RLock()
	n := d.notify
	d.notifyMu.RUnlock()
	if n == nil {
		return nil
	}
	frame := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		frame["params"] = params
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return n(ctx, connID, b)
}

// ConnBroadcaster is the callback
// (*Dispatcher).BroadcastNotify uses to deliver a
// notification frame to every open connection on a
// transport. The dispatcher doesn't know about
// transport-level conn registries (the package
// boundary forbids it — remotetransport cannot import
// remotecontrol, and the dispatcher cannot import
// remotetransport); the broadcaster seam is the
// indirect bridge. The transport wires it via
// SetBroadcaster at TCPServer construction; nil
// means "no broadcaster registered" — the broadcast
// becomes a no-op.
//
// The contract is symmetric with Notifier (above):
// marshal the frame once (the broadcaster is free to
// share the JSON bytes across conns), route it to
// each open ws connection, return nil on success or
// an error if every delivery failed. A partial-
// success run (some conns received the frame, others
// didn't) is acceptable — the broadcaster chooses
// the granularity. Today the broadcaster is invoked
// once per BroadcastNotify call; future cycles can
// rate-limit if a slow peer pins the dispatcher.
//
// Declared as a type alias (not a named type) for
// the same reason Notifier uses one: so
// remotetransport's Dispatcher interface method
// signature matches the concrete *remotecontrol.Dispatcher
// method, without remotetransport needing to import
// this package's named type.
type ConnBroadcaster = func(ctx context.Context, method string, params any) error

// SetBroadcaster registers (or clears, when b is
// nil) the callback BroadcastNotify uses to fan a
// frame out to every open connection. The transport
// typically calls this once, after NewTCPServer has
// built its conn registry.
//
// SetBroadcaster is safe to call concurrently with
// BroadcastNotify; an in-flight broadcast uses the
// callback that was registered when it started.
//
// Cycle rc-permissions-grants adds this seam. The
// permissions/changed notification per Set/Add/Remove
// is the first consumer; future cycles (rc-plugins,
// rc-triggers) that fan their own "X changed"
// notifications will share it.
func (d *Dispatcher) SetBroadcaster(b ConnBroadcaster) {
	if d == nil {
		return
	}
	d.broadcasterMu.Lock()
	d.broadcaster = b
	d.broadcasterMu.Unlock()
}

// BroadcastNotify pushes one JSON-RPC notification
// frame to every open connection on the registered
// transport. The dispatcher has no idea what "every
// connection" means in transport terms; that's the
// broadcaster's responsibility. A nil broadcaster
// (the default for in-process dispatchers and the
// Unix-socket transport) makes BroadcastNotify a
// no-op — mutations still land in the store, and the
// four Set/Add/Remove handlers echo the post-state
// in their response envelopes so polling is always
// possible.
//
// BroadcastNotify is best-effort in the same way
// Notify is: errors from the broadcaster are
// returned, but the dispatcher does NOT attempt
// per-conn retries. The broadcaster's contract is
// the same as a single Notify call (write to one
// conn, return err on failure) generalised to
// "write to every conn".
//
// Wire format (one frame per method call):
//
//	{ "jsonrpc": "2.0", "method": "<name>", "params": {…} }
//
// — same as Notify. The transport's broadcaster
// owns the marshalling so the bytes the dispatcher
// stamps for a per-connID path and the bytes the
// broadcaster sends to N conns are byte-for-byte
// identical. (We could centralise the marshal in
// the dispatcher, but the import-cycle boundary
// (remotetransport cannot import remotecontrol)
// means duplicating one map literal + Marshal call
// is cheaper than lifting the helper to a third
// package.)
func (d *Dispatcher) BroadcastNotify(ctx context.Context, method string, params any) error {
	if d == nil {
		return nil
	}
	d.broadcasterMu.RLock()
	b := d.broadcaster
	d.broadcasterMu.RUnlock()
	if b == nil {
		return nil
	}
	return b(ctx, method, params)
}
