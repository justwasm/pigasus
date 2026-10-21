// Package remotecontrol — terminals.go: the terminal-style
// JSON-RPC surface introduced in cycle rc-terminals.
//
// Why split (rather than embedding in terminals_registry.go):
// the dispatcher doesn't care about the on-disk format, and
// keeping the per-record types + sentinels in a single small
// file lets the storage layer (terminals_registry.go) and the
// per-method handlers (terminals_dispatcher.go) talk to a
// stable interface without re-touching each other when the
// wire shape evolves.
//
// Data model (mirrors the brief):
//
//   - TerminalKind — the closed-set {"output","input"} the
//     brief pins. "output" means the consumer is read-only
//     (mirrors a passive tail -f). "input" means the consumer
//     may submit input bytes via SendRemoteAgentTerminalInput.
//     Both kinds receive terminal/output notifications.
//
//   - Terminal — one row of the registry. Fields mirror the
//     brief: terminal_id (prefix tml_<hex>),
//     cascade_id (optional; absent means the terminal is
//     unattached to a cascade), kind, cols, rows, created_at,
//     closed_at (omitted while the terminal is open).
//
//   - TerminalEvent — the per-subscriber delivery shape. Kind is
//     "output" (a text chunk the cascade chose to log, OR the
//     bytes a Send caller just submitted — fanned out via
//     terminal/output notifications) or "close" (the terminal
//     was closed). Data carries the base64-decoded UTF-8
//     payload for output; close events carry no data. Seq is
//     the per-terminal monotonic sequence the subscriber uses
//     to detect drops.
//
//   - TerminalsRegistry — the seam the dispatcher's five
//     per-method handlers (Create / List / Send / Resize /
//     Close) call into. The dispatcher exposes a single
//     TerminalsRegistry field on Config; the registry is the
//     only thing the handlers know about. NewTerminalsRegistry
//     returns a fresh empty registry.
//
// Sentinel-error map (mirrors the rc-resources /
// rc-fastpush-toggle cycle pattern):
//
//	ErrTerminalNotFound → -32018 "Terminal not found"
//	ErrTerminalClosed  → -32019 "Terminal closed"
//	(other / I/O)       → -32603 "Internal error"
//
// -32602 ("Invalid params") is emitted directly from the
// per-method param-validation path before calling the
// registry; the helper doesn't see it.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"errors"
	"sync/atomic"
	"time"
)

// TerminalKind is the closed-set the CreateRemoteAgentTerminal
// method accepts. Mirrors the brief pin: "output" (passive
// read-only consumer) and "input" (consumer may submit bytes
// via SendRemoteAgentTerminalInput). Unknown values are
// rejected as -32602 (the brief's pre-flight validation rule).
type TerminalKind string

const (
	// TerminalOutput is the passive read-only consumer
	// kind. Receive terminal/output notifications, but
	// SendRemoteAgentTerminalInput rejects bytes. This
	// matches a `tail -f` style user — useful for
	// dashboards that want to mirror a cascade's stream
	// without granting write permission.
	TerminalOutput TerminalKind = "output"

	// TerminalInput is the bidirectional consumer kind.
	// Receive terminal/output notifications AND may
	// submit bytes via SendRemoteAgentTerminalInput.
	// This matches a PTY-style user — useful for an
	// IDE companion that wants both reads and writes.
	TerminalInput TerminalKind = "input"
)

// AllTerminalKinds is the canonical closed-set the
// CreateRemoteAgentTerminal handler accepts. Insertion order
// is intentional (matches the cycle precedent: cheapest /
// least-privileged first). A future cycle that adds (e.g.)
// "system" or "operator" extends this slice + the validator
// together — both are pinned by TestAllTerminalKinds so a
// drift fails the build.
var AllTerminalKinds = []TerminalKind{TerminalOutput, TerminalInput}

// IsValidTerminalKind reports whether k is one of the
// closed-set values in AllTerminalKinds. Returns false for
// the empty string (matches the rc-resources /
// rc-triggerKind precedent). A closed-set validator that
// returns false flips the dispatcher's error mapping to
// -32602.
func IsValidTerminalKind(k TerminalKind) bool {
	for _, v := range AllTerminalKinds {
		if v == k {
			return true
		}
	}
	return false
}

// TerminalStateVersion is the on-disk / wire format version
// the cycle pins. Bumped on breaking changes (renamed keys,
// removed fields); non-breaking additions (new optional
// fields with omitempty) leave it alone. The cycle doesn't
// persist TerminalState to disk today (the registry is
// in-memory only), but the constant is documented for
// forward-compat with a future persistence cycle.
const TerminalStateVersion = 1

// NotifTerminalOutputMethod is the JSON-RPC server-push
// notification method name the dispatcher uses for every
// "output chunk the cascade chose to log" frame, AND for
// every successful SendRemoteAgentTerminalInput (the bytes
// get echoed back to every other subscriber). The cycle
// brief pins "terminal/output" verbatim.
//
// Wired via (TerminalsRegistry).Subscribe → buffered chan;
// the cascade's TerminalOutput chan is drained by a
// goroutine the dispatcher spawns when the terminal is
// attached to a cascade, forwarding each chunk as a
// terminal/output notification. Send propagates data via
// Dispatcher.BroadcastNotify (the same seam
// rc-permissions-grants added for the multi-conn
// "everything/everyone" fan-out) so cross-ws connections
// all see the same chunks.
const NotifTerminalOutputMethod = "terminal/output"

// NotifTerminalClosedMethod is the JSON-RPC server-push
// notification method name the dispatcher fans out on a
// successful CloseRemoteAgentTerminal RPC. The cycle brief
// pins "terminal/closed" verbatim.
//
// Wired via Dispatcher.BroadcastNotify (the same seam
// rc-terminals uses for terminal/output). The originating
// recipient's response envelope is the brief's lean
// {ok, terminal_id, closed_at} shape; every other open ws
// conn receives the same {ok, terminal_id, closed_at}
// envelope via the terminal/closed broadcast.
const NotifTerminalClosedMethod = "terminal/closed"

// DefaultTerminalCols / DefaultTerminalRows are the
// canonical fallback dimensions CreateRemoteAgentTerminal
// applies when the dashboard omits cols / rows (or posts
// zero). Matches the brief's pin (80x24) and the conventional
// TTY default. The constants are exported so tests can pin
// them (TestDefaultTerminalDimensions) and a future CLI
// flag-parser can reuse the values without re-touching the
// cycle.
const (
	DefaultTerminalCols = 80
	DefaultTerminalRows = 24
)

// TerminalEventBufferCap is the per-subscriber channel
// capacity the registry hands out from Subscribe. The cycle
// brief pins 64 (a small bounded queue; subscribers that
// fall behind drop events on the floor with a counter bump
// — see the per-subscriber dropped counter). The constant
// is exported for the cycle's
// TestTerminalsRegistry_Subscribe_ReceivesEvents + future
// cycle-parity probes.
const TerminalEventBufferCap = 64

// Terminal is one row of the in-memory registry. Fields
// mirror the brief:
//
//   - TerminalID: opaque id (tml_<16 hex chars>).
//
//   - CascadeID: optional. Empty means the terminal is
//     unattached to any cascade (a host-level log tail).
//     Non-empty means the terminal is bound to that cascade;
//     the cascade's TerminalOutput chan streams into the
//     terminal's subscribers AND Send calls feed back
//     through the cascade's input chan (the cycle wires
//     that direction with the rc-cascade-message's
//     dispatcher path).
//
//   - Kind: closed-set "output" / "input".
//
//   - Cols, Rows: integer dimensions. Dashboard-supplied;
//     the registry defaults to 80/24 when zeros are
//     posted.
//
//   - CreatedAt: stamp the registry applied at Create
//     time (server clock).
//
//   - ClosedAt: stamp the registry applied at Close time.
//     Empty while the terminal is open. The omitempty
//     JSON tag keeps the field off the wire until Close
//     fires.
type Terminal struct {
	TerminalID string       `json:"terminal_id"`
	CascadeID  string       `json:"cascade_id,omitempty"`
	Kind       TerminalKind `json:"kind"`
	Cols       int          `json:"cols"`
	Rows       int          `json:"rows"`
	CreatedAt  time.Time    `json:"created_at"`
	ClosedAt   time.Time    `json:"closed_at,omitempty"`
}

// TerminalState is the on-disk / wire envelope the registry
// would carry if the cycle added persistence. Today this
// type is documented but unused — the registry is in-memory
// only. Future cycles that add persistence wire it up under
// the same name (mirrors the cycle-9 FSProjectStore /
// cycle-25 FSConversationConfigStore style).
//
// Wire shape (for reference; no consumer today):
//
//	{
//	  "version":    1,
//	  "terminals":  [Terminal, …],
//	  "updated_at": "<rfc3339-nano>"
//	}
type TerminalState struct {
	Version   int        `json:"version"`
	Terminals []Terminal `json:"terminals"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TerminalEventKind is the closed-set the cycle pins for
// per-subscriber delivery frames. "output" carries text
// chunks the cascade chose to log, OR the bytes a Send
// caller just submitted; "close" is emitted exactly once
// when the terminal is closed (either by a
// CloseRemoteAgentTerminal RPC or by MarkClosed cascading
// from a parent cascade closure).
type TerminalEventKind string

const (
	// TerminalEventOutput is an "output chunk"
	// event — the cascade chose to log something,
	// OR a Send caller just submitted bytes that
	// the registry fans back out as output. Data
	// carries the bytes; subscribers re-encode
	// them as base64 for the terminal/output
	// notification.
	TerminalEventOutput TerminalEventKind = "output"

	// TerminalEventClose is a "the terminal is
	// closed for new events" notification. No
	// Data. Always the last event a subscriber
	// sees on a given chan.
	TerminalEventClose TerminalEventKind = "close"
)

// TerminalEvent is the per-subscriber delivery shape the
// registry hands out from Subscribe. Fields mirror the
// brief:
//
//   - Kind: "output" or "close" (the TerminalEventKind
//     closed-set above).
//
//   - Data: bytes the cascade chose to log, OR bytes a
//     Send caller just submitted (the Send fan-out
//     mirrors what an echo-mode terminal would do —
//     dashboards that want a "real" non-echoing PTY
//     filter their own pre-write events out of the
//     resulting stream). Empty for "close" events.
//
//   - Seq: monotonic per-terminal sequence number;
//     starts at 0 for the first event the subscriber
//     sees, 1 for the second, etc. The terminal/output
//     notification exposes seq so a subscriber can
//     detect drops (a gap means the buffered chan
//     overran; the future rc-terminals-backpressure
//     cycle surfaces a metrics endpoint that exposes
//     drop counts).
type TerminalEvent struct {
	Kind TerminalEventKind
	Data []byte
	Seq  uint64
}

// ErrTerminalNotFound is the sentinel the registry returns
// when a Send / Resize / Close / Subscribe call names a
// terminal_id the registry doesn't know. The dispatcher
// surfaces it as JSON-RPC -32018 "Terminal not found" so
// dashboards can distinguish unknown-id from a stale /
// already-closed terminal.
var ErrTerminalNotFound = errors.New("remotecontrol: terminal not found")

// ErrTerminalClosed is the sentinel the registry returns
// when a Send call targets an already-closed terminal. The
// dispatcher surfaces it as JSON-RPC -32019 "Terminal
// closed" so dashboards can render a clear "terminal is no
// longer accepting input" error path.
var ErrTerminalClosed = errors.New("remotecontrol: terminal closed")

// ErrTerminalInputRejected is the sentinel the registry
// returns when a Send call targets a TerminalOutput-kind
// terminal (read-only consumer). The cycle's brief pins
// this as -32602 ("Invalid params"); the registry returns
// it directly so the dispatcher's error-mapping helper
// doesn't have to special-case the kind.
var ErrTerminalInputRejected = errors.New("remotecontrol: terminal kind is output; Send rejected")

// terminalSeq is the atomic per-terminal sequence number
// the registry stamps on every output event. The dispatcher's
// goroutine-driven path uses the atomic Counter directly
// (no mutex) so the Send hot path stays lock-free.
type terminalSeq struct {
	v atomic.Uint64
}

// next returns seq+1 atomically and yields the new value
// suitable for stamping on the outgoing event. The zero
// seed means the first event has Seq=0 (matching the brief).
func (s *terminalSeq) next() uint64 {
	return s.v.Add(1) - 1
}
