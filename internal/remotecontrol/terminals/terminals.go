// Package terminals — terminals.go: the terminal-style JSON-RPC
// surface introduced in cycle rc-terminals.
//
// Why split (rather than embedding in registry.go): the handlers
// don't care about the on-disk format, and keeping the per-record
// types + sentinels in a single small file lets the storage layer
// (registry.go) and the per-method handlers (dispatcher.go) talk
// to a stable interface without re-touching each other when the
// wire shape evolves.
//
// Data model (mirrors the brief):
//
//   - Kind — the closed-set {"output","input"} the
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
//   - Event — the per-subscriber delivery shape. Kind is
//     "output" (a text chunk the cascade chose to log, OR the
//     bytes a Send caller just submitted — fanned out via
//     terminal/output notifications) or "close" (the terminal
//     was closed). Data carries the base64-decoded UTF-8
//     payload for output; close events carry no data. Seq is
//     the per-terminal monotonic sequence the subscriber uses
//     to detect drops.
//
//   - Registry — the in-memory registry the five per-method
//     handlers (Create / List / Send / Resize / Close) call
//     into. The host installs it as the domain's Store seam;
//     NewRegistry returns a fresh empty registry.
//
// Sentinel-error map (mirrors the rc-resources /
// rc-fastpush-toggle cycle pattern):
//
//	ErrNotFound → -32018 "Terminal not found"
//	ErrClosed  → -32019 "Terminal closed"
//	(other / I/O)       → -32603 "Internal error"
//
// -32602 ("Invalid params") is emitted directly from the
// per-method param-validation path before calling the
// registry; the helper doesn't see it.
//
// Stdlib-only. No third-party deps.
package terminals

import (
	"errors"
	"sync/atomic"
	"time"
)

// Kind is the closed-set the CreateRemoteAgentTerminal
// method accepts. Mirrors the brief pin: "output" (passive
// read-only consumer) and "input" (consumer may submit bytes
// via SendRemoteAgentTerminalInput). Unknown values are
// rejected as -32602 (the brief's pre-flight validation rule).
type Kind string

const (
	// Output is the passive read-only consumer
	// kind. Receive terminal/output notifications, but
	// SendRemoteAgentTerminalInput rejects bytes. This
	// matches a `tail -f` style user — useful for
	// dashboards that want to mirror a cascade's stream
	// without granting write permission.
	Output Kind = "output"

	// Input is the bidirectional consumer kind.
	// Receive terminal/output notifications AND may
	// submit bytes via SendRemoteAgentTerminalInput.
	// This matches a PTY-style user — useful for an
	// IDE companion that wants both reads and writes.
	Input Kind = "input"
)

// AllKinds is the canonical closed-set the
// CreateRemoteAgentTerminal handler accepts. Insertion order
// is intentional (matches the cycle precedent: cheapest /
// least-privileged first). A future cycle that adds (e.g.)
// "system" or "operator" extends this slice + the validator
// together — both are pinned by TestAllTerminalKinds so a
// drift fails the build.
var AllKinds = []Kind{Output, Input}

// IsValidKind reports whether k is one of the
// closed-set values in AllKinds. Returns false for
// the empty string (matches the rc-resources /
// rc-triggerKind precedent). A closed-set validator that
// returns false flips the dispatcher's error mapping to
// -32602.
func IsValidKind(k Kind) bool {
	for _, v := range AllKinds {
		if v == k {
			return true
		}
	}
	return false
}

// NotifOutputMethod is the JSON-RPC server-push
// notification method name the dispatcher uses for every
// "output chunk the cascade chose to log" frame, AND for
// every successful SendRemoteAgentTerminalInput (the bytes
// get echoed back to every other subscriber). The cycle
// brief pins "terminal/output" verbatim.
//
// Wired via (Registry).Subscribe → buffered chan;
// the cascade's Output chan is drained by a
// goroutine the dispatcher spawns when the terminal is
// attached to a cascade, forwarding each chunk as a
// terminal/output notification. Send propagates data via
// Dispatcher.BroadcastNotify (the same seam
// rc-permissions-grants added for the multi-conn
// "everything/everyone" fan-out) so cross-ws connections
// all see the same chunks.
const NotifOutputMethod = "terminal/output"

// NotifClosedMethod is the JSON-RPC server-push
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
const NotifClosedMethod = "terminal/closed"

// DefaultCols / DefaultRows are the
// canonical fallback dimensions CreateRemoteAgentTerminal
// applies when the dashboard omits cols / rows (or posts
// zero). Matches the brief's pin (80x24) and the conventional
// TTY default. The constants are exported so tests can pin
// them (TestDefaultTerminalDimensions) and a future CLI
// flag-parser can reuse the values without re-touching the
// cycle.
const (
	DefaultCols = 80
	DefaultRows = 24
)

// EventBufferCap is the per-subscriber channel
// capacity the registry hands out from Subscribe. The cycle
// brief pins 64 (a small bounded queue; subscribers that
// fall behind drop events on the floor with a counter bump
// — see the per-subscriber dropped counter). The constant
// is exported for the cycle's
// TestTerminalsRegistry_Subscribe_ReceivesEvents + future
// cycle-parity probes.
const EventBufferCap = 64

// Terminal is one row of the in-memory registry. Fields
// mirror the brief:
//
//   - TerminalID: opaque id (tml_<16 hex chars>).
//
//   - CascadeID: optional. Empty means the terminal is
//     unattached to any cascade (a host-level log tail).
//     Non-empty means the terminal is bound to that cascade;
//     the cascade's Output chan streams into the
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
	TerminalID string    `json:"terminal_id"`
	CascadeID  string    `json:"cascade_id,omitempty"`
	Kind       Kind      `json:"kind"`
	Cols       int       `json:"cols"`
	Rows       int       `json:"rows"`
	CreatedAt  time.Time `json:"created_at"`
	ClosedAt   time.Time `json:"closed_at,omitempty"`
}

// EventKind is the closed-set the cycle pins for
// per-subscriber delivery frames. "output" carries text
// chunks the cascade chose to log, OR the bytes a Send
// caller just submitted; "close" is emitted exactly once
// when the terminal is closed (either by a
// CloseRemoteAgentTerminal RPC or by MarkClosed cascading
// from a parent cascade closure).
type EventKind string

const (
	// EventOutput is an "output chunk"
	// event — the cascade chose to log something,
	// OR a Send caller just submitted bytes that
	// the registry fans back out as output. Data
	// carries the bytes; subscribers re-encode
	// them as base64 for the terminal/output
	// notification.
	EventOutput EventKind = "output"

	// EventClose is a "the terminal is
	// closed for new events" notification. No
	// Data. Always the last event a subscriber
	// sees on a given chan.
	EventClose EventKind = "close"
)

// Event is the per-subscriber delivery shape the
// registry hands out from Subscribe. Fields mirror the
// brief:
//
//   - Kind: "output" or "close" (the EventKind
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
type Event struct {
	Kind EventKind
	Data []byte
	Seq  uint64
}

// ErrNotFound is the sentinel the registry returns
// when a Send / Resize / Close / Subscribe call names a
// terminal_id the registry doesn't know. The dispatcher
// surfaces it as JSON-RPC -32018 "Terminal not found" so
// dashboards can distinguish unknown-id from a stale /
// already-closed terminal.
var ErrNotFound = errors.New("rc-terminals: terminal not found")

// ErrClosed is the sentinel the registry returns
// when a Send call targets an already-closed terminal. The
// dispatcher surfaces it as JSON-RPC -32019 "Terminal
// closed" so dashboards can render a clear "terminal is no
// longer accepting input" error path.
var ErrClosed = errors.New("rc-terminals: terminal closed")

// ErrInputRejected is the sentinel the registry
// returns when a Send call targets a Output-kind
// terminal (read-only consumer). The cycle's brief pins
// this as -32602 ("Invalid params"); the registry returns
// it directly so the dispatcher's error-mapping helper
// doesn't have to special-case the kind.
var ErrInputRejected = errors.New("rc-terminals: terminal kind is output; Send rejected")

// Store is the narrow view of the terminal registry the
// domain's five handlers use.
//
// The domain declares the interface it needs rather than
// naming a sibling's wider one: the host installs a registry
// (today the in-memory *Registry, tomorrow a persistent one)
// and the domain never learns which.
//
// The registry's own Subscribe — the in-process subscriber
// fan-out — is deliberately *not* part of this interface: no
// handler needs it, and the interface is what the host hands
// over. A caller that wants the subscriber fan-out holds the
// concrete *Registry.
type Store interface {
	// Create inserts a new terminal and returns it with the
	// server-stamped id and creation time.
	Create(t Terminal) (Terminal, error)

	// Get returns the terminal record for terminalID, or
	// (_, false) when the registry has no such row.
	Get(terminalID string) (Terminal, bool)

	// List returns the registry's rows, filtered by cascadeID
	// (empty means "every row"), sorted by creation time.
	List(cascadeID string) []Terminal

	// Send writes data to the terminal and fans it out to its
	// subscribers. It returns the byte count consumed.
	Send(terminalID string, data []byte) (int, error)

	// Resize changes the terminal's dimensions.
	Resize(terminalID string, cols, rows int) error

	// Close marks the terminal closed and releases its
	// subscribers.
	Close(terminalID string) error

	// AttachCascade binds the terminal to a cascade's output
	// chan and starts the drain that fans the cascade's chunks
	// into the terminal's subscribers. A nil chan detaches.
	AttachCascade(terminalID string, cascadeOutput <-chan []byte) error
}

// seqCounter is the atomic per-terminal sequence number
// the registry stamps on every output event. The dispatcher's
// goroutine-driven path uses the atomic Counter directly
// (no mutex) so the Send hot path stays lock-free.
type seqCounter struct {
	v atomic.Uint64
}

// next returns seq+1 atomically and yields the new value
// suitable for stamping on the outgoing event. The zero
// seed means the first event has Seq=0 (matching the brief).
func (s *seqCounter) next() uint64 {
	return s.v.Add(1) - 1
}
